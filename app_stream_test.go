package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"axon/internal/config"
	"axon/internal/db"
	"axon/internal/model"
	"axon/internal/provider"
	"axon/internal/secret"
	"axon/internal/store/sqlite"
)

// testTimeout bounds every goroutine wait so a wiring bug fails fast instead of
// hanging the whole suite.
const testTimeout = 5 * time.Second

// --- test doubles ---------------------------------------------------------

// recordedEvent is one emit call captured by mockEmitter.
type recordedEvent struct {
	name string
	data streamEvent
}

// mockEmitter records emit calls. runStream emits from a goroutine, so all
// access is guarded by a mutex; a channel signals each emit so tests can wait
// for a specific event (e.g. the first delta) before acting.
type mockEmitter struct {
	mu     sync.Mutex
	events []recordedEvent
	signal chan struct{}
}

func newMockEmitter() *mockEmitter {
	return &mockEmitter{signal: make(chan struct{}, 64)}
}

// emit matches the signature of App.emit. The stream layer always passes a
// single streamEvent as data, which we unpack for convenient assertions.
func (m *mockEmitter) emit(event string, data ...interface{}) {
	m.mu.Lock()
	ev := recordedEvent{name: event}
	if len(data) > 0 {
		if se, ok := data[0].(streamEvent); ok {
			ev.data = se
		}
	}
	m.events = append(m.events, ev)
	m.mu.Unlock()
	// Non-blocking notify: the buffer is large enough for our scripts.
	select {
	case m.signal <- struct{}{}:
	default:
	}
}

// snapshot returns a copy of the recorded events.
func (m *mockEmitter) snapshot() []recordedEvent {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]recordedEvent, len(m.events))
	copy(out, m.events)
	return out
}

// count returns how many events of a given name were recorded.
func (m *mockEmitter) count(name string) int {
	n := 0
	for _, e := range m.snapshot() {
		if e.name == name {
			n++
		}
	}
	return n
}

// waitForEvent blocks until at least n events named `name` have been recorded,
// or the timeout elapses.
func (m *mockEmitter) waitForEvent(t *testing.T, name string, n int) {
	t.Helper()
	deadline := time.After(testTimeout)
	for {
		if m.count(name) >= n {
			return
		}
		select {
		case <-m.signal:
		case <-deadline:
			t.Fatalf("timeout waiting for %d %q event(s); got %d", n, name, m.count(name))
		}
	}
}

// fakeProvider is a network-free provider.Provider driven by a script function.
// The script runs in Chat's goroutine and pushes chunks/errors; it must send an
// error (if any) before returning so runStream's post-loop error check sees it.
type fakeProvider struct {
	script func(ctx context.Context, chunks chan<- provider.ChatChunk, errs chan<- error)
}

func (f *fakeProvider) Name() string { return "fake" }

func (f *fakeProvider) Chat(ctx context.Context, req provider.ChatRequest) (<-chan provider.ChatChunk, <-chan error) {
	// errs is buffered (size 1) like the real providers, so the script never
	// blocks delivering a terminal error.
	chunks := make(chan provider.ChatChunk)
	errs := make(chan error, 1)
	go func() {
		defer close(chunks)
		f.script(ctx, chunks, errs)
	}()
	return chunks, errs
}

// --- fixtures -------------------------------------------------------------

// newTestApp builds an App backed by a fresh, isolated SQLite store under
// t.TempDir() and a mock emitter. It never touches the real AppDataDir or
// Keychain.
func newTestApp(t *testing.T) (*App, *mockEmitter) {
	t.Helper()
	sqlDB, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	em := newMockEmitter()
	app := &App{
		ctx:     context.Background(),
		store:   sqlite.New(sqlDB),
		cancels: make(map[string]context.CancelFunc),
		emit:    em.emit,
	}
	return app, em
}

// seedAssistantPlaceholder creates a conversation, appends a user turn and a
// streaming assistant placeholder, returning the conversation id and the
// placeholder message id (the target runStream finalizes).
func seedAssistantPlaceholder(t *testing.T, app *App, userContent string) (string, int64) {
	t.Helper()
	ctx := context.Background()
	conv, err := app.store.CreateConversation(ctx, model.Conversation{Title: "stream test"})
	if err != nil {
		t.Fatalf("CreateConversation: %v", err)
	}
	if _, err := app.store.AppendMessage(ctx, model.Message{
		ConversationID: conv.ID,
		Role:           model.RoleUser,
		Content:        userContent,
		Status:         model.StatusComplete,
	}); err != nil {
		t.Fatalf("AppendMessage(user): %v", err)
	}
	asst, err := app.store.AppendMessage(ctx, model.Message{
		ConversationID: conv.ID,
		Role:           model.RoleAssistant,
		Model:          "fake-model",
		Status:         model.StatusStreaming,
	})
	if err != nil {
		t.Fatalf("AppendMessage(assistant placeholder): %v", err)
	}
	return conv.ID, asst.ID
}

// getMessage returns the message with the given id from a conversation, failing
// the test if it is absent.
func getMessage(t *testing.T, app *App, convID string, msgID int64) model.Message {
	t.Helper()
	msgs, err := app.store.ListMessages(context.Background(), convID)
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	for _, m := range msgs {
		if m.ID == msgID {
			return m
		}
	}
	t.Fatalf("message %d not found in conversation %q", msgID, convID)
	return model.Message{}
}

// --- tests ----------------------------------------------------------------

// TestRunStreamPersistsCompletedReply verifies the happy path: deltas are
// emitted, the final content is the concatenation of deltas, token usage is
// persisted and the message is marked complete.
func TestRunStreamPersistsCompletedReply(t *testing.T) {
	app, em := newTestApp(t)
	convID, msgID := seedAssistantPlaceholder(t, app, "hi")

	prov := &fakeProvider{
		script: func(ctx context.Context, chunks chan<- provider.ChatChunk, errs chan<- error) {
			chunks <- provider.ChatChunk{Delta: "Hello"}
			chunks <- provider.ChatChunk{Delta: " world"}
			chunks <- provider.ChatChunk{Done: true, PromptTokens: 10, CompletionTokens: 5}
		},
	}

	app.runStream(context.Background(), prov, provider.ChatRequest{Model: "fake-model"}, convID, msgID)

	// Events: two deltas, one done, no error.
	if got := em.count(EventDelta); got != 2 {
		t.Errorf("delta events: got %d, want 2", got)
	}
	if got := em.count(EventDone); got != 1 {
		t.Errorf("done events: got %d, want 1", got)
	}
	if got := em.count(EventError); got != 0 {
		t.Errorf("error events: got %d, want 0", got)
	}

	// Persistence: content, status and tokens are finalized.
	m := getMessage(t, app, convID, msgID)
	if m.Content != "Hello world" {
		t.Errorf("content: got %q, want %q", m.Content, "Hello world")
	}
	if m.Status != model.StatusComplete {
		t.Errorf("status: got %q, want %q", m.Status, model.StatusComplete)
	}
	if m.PromptTokens != 10 || m.CompletionTokens != 5 {
		t.Errorf("tokens: got prompt=%d completion=%d, want 10/5", m.PromptTokens, m.CompletionTokens)
	}
}

// TestRunStreamStopKeepsPartialContent verifies NFR 6.3: stopping generation
// mid-stream keeps the already-generated text and marks the message
// interrupted instead of dropping the partial output.
func TestRunStreamStopKeepsPartialContent(t *testing.T) {
	app, em := newTestApp(t)
	convID, msgID := seedAssistantPlaceholder(t, app, "hi")

	// The fake emits one delta then blocks until the stream context is
	// cancelled, at which point it reports context.Canceled (like real
	// providers) and closes the channel.
	prov := &fakeProvider{
		script: func(ctx context.Context, chunks chan<- provider.ChatChunk, errs chan<- error) {
			select {
			case chunks <- provider.ChatChunk{Delta: "partial"}:
			case <-ctx.Done():
				errs <- ctx.Err()
				return
			}
			<-ctx.Done()
			errs <- ctx.Err()
		},
	}

	streamCtx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})
	go func() {
		app.runStream(streamCtx, prov, provider.ChatRequest{Model: "fake-model"}, convID, msgID)
		close(done)
	}()

	// Wait until the partial delta has actually been emitted, then stop.
	em.waitForEvent(t, EventDelta, 1)
	cancel()

	select {
	case <-done:
	case <-time.After(testTimeout):
		t.Fatal("runStream did not return after cancel")
	}

	// A user-initiated stop surfaces as a done event, not an error.
	if got := em.count(EventError); got != 0 {
		t.Errorf("error events: got %d, want 0 (stop is not an error)", got)
	}

	m := getMessage(t, app, convID, msgID)
	if m.Content != "partial" {
		t.Errorf("content: got %q, want %q (partial output must survive a stop)", m.Content, "partial")
	}
	if m.Status != model.StatusInterrupted {
		t.Errorf("status: got %q, want %q", m.Status, model.StatusInterrupted)
	}
}

// TestRunStreamErrorKeepsPartialContent verifies a mid-stream failure emits a
// chat:error event, marks the message interrupted and still persists whatever
// was accumulated before the error.
func TestRunStreamErrorKeepsPartialContent(t *testing.T) {
	app, em := newTestApp(t)
	convID, msgID := seedAssistantPlaceholder(t, app, "hi")

	streamErr := errors.New("upstream exploded")
	// The fake sends one delta, then reports an error without a Done chunk.
	// The error is delivered before the goroutine returns (closing chunks), so
	// runStream's post-loop check observes it.
	prov := &fakeProvider{
		script: func(ctx context.Context, chunks chan<- provider.ChatChunk, errs chan<- error) {
			chunks <- provider.ChatChunk{Delta: "partial before error"}
			errs <- streamErr
		},
	}

	app.runStream(context.Background(), prov, provider.ChatRequest{Model: "fake-model"}, convID, msgID)

	if got := em.count(EventError); got != 1 {
		t.Errorf("error events: got %d, want 1", got)
	}
	if got := em.count(EventDone); got != 0 {
		t.Errorf("done events: got %d, want 0", got)
	}
	// The error message reaches the UI payload.
	var sawErr bool
	for _, e := range em.snapshot() {
		if e.name == EventError && e.data.Error == streamErr.Error() {
			sawErr = true
		}
	}
	if !sawErr {
		t.Errorf("expected error event carrying %q", streamErr.Error())
	}

	m := getMessage(t, app, convID, msgID)
	if m.Content != "partial before error" {
		t.Errorf("content: got %q, want %q (partial output must survive an error)", m.Content, "partial before error")
	}
	if m.Status != model.StatusInterrupted {
		t.Errorf("status: got %q, want %q", m.Status, model.StatusInterrupted)
	}
}

// memSecrets is an in-memory secret.Store for the SendMessage integration test,
// so no real Keychain is touched.
type memSecrets struct {
	m map[string]string
}

func (s *memSecrets) Set(ref, value string) error { s.m[ref] = value; return nil }
func (s *memSecrets) Get(ref string) (string, error) {
	v, ok := s.m[ref]
	if !ok {
		return "", secret.ErrNotFound
	}
	return v, nil
}
func (s *memSecrets) Delete(ref string) error { delete(s.m, ref); return nil }
func (s *memSecrets) Has(ref string) bool     { _, ok := s.m[ref]; return ok }

// TestSendMessagePersistsUserAndPlaceholder exercises SendMessage end to end
// against a stub OpenAI server that blocks, so the synchronous landing (user
// turn persisted, streaming assistant placeholder created, auto-title applied)
// can be asserted deterministically while the stream is still in flight.
func TestSendMessagePersistsUserAndPlaceholder(t *testing.T) {
	// Server holds the response open until the test tears down, keeping the
	// assistant message in the streaming state during assertions.
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))

	sqlDB, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}

	cfgDir := t.TempDir()
	cfg, err := config.Load(cfgDir)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	if err := cfg.UpsertProvider(provider.Config{
		Name:     "test-openai",
		Protocol: "openai",
		BaseURL:  srv.URL,
		KeyRef:   "provider:test-openai",
	}); err != nil {
		t.Fatalf("UpsertProvider: %v", err)
	}
	if err := cfg.SetDefaults("test-openai", "gpt-test"); err != nil {
		t.Fatalf("SetDefaults: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	em := newMockEmitter()
	app := &App{
		ctx:     ctx,
		store:   sqlite.New(sqlDB),
		secrets: &memSecrets{m: map[string]string{"provider:test-openai": "sk-test"}},
		cfg:     cfg,
		cancels: make(map[string]context.CancelFunc),
		emit:    em.emit,
	}
	t.Cleanup(func() {
		cancel()       // abort the in-flight stream
		close(release) // unblock the server handler if still parked
		srv.Close()
		_ = sqlDB.Close()
	})

	conv, err := app.store.CreateConversation(ctx, model.Conversation{})
	if err != nil {
		t.Fatalf("CreateConversation: %v", err)
	}

	asstID, err := app.SendMessage(conv.ID, "hello there", "test-openai", "gpt-test")
	if err != nil {
		t.Fatalf("SendMessage: %v", err)
	}

	msgs, err := app.store.ListMessages(ctx, conv.ID)
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages (user + assistant placeholder), got %d", len(msgs))
	}

	user := msgs[0]
	if user.Role != model.RoleUser || user.Content != "hello there" || user.Status != model.StatusComplete {
		t.Errorf("user message unexpected: %+v", user)
	}

	asst := msgs[1]
	if asst.ID != asstID {
		t.Errorf("returned id %d does not match assistant message id %d", asstID, asst.ID)
	}
	if asst.Role != model.RoleAssistant {
		t.Errorf("assistant role: got %q, want %q", asst.Role, model.RoleAssistant)
	}
	if asst.Model != "gpt-test" {
		t.Errorf("assistant model: got %q, want %q", asst.Model, "gpt-test")
	}
	if asst.Status != model.StatusStreaming {
		t.Errorf("assistant placeholder status: got %q, want %q", asst.Status, model.StatusStreaming)
	}

	// Auto-title (F1.5) derives the title from the first user message.
	got, err := app.store.GetConversation(ctx, conv.ID)
	if err != nil {
		t.Fatalf("GetConversation: %v", err)
	}
	if got.Title != "hello there" {
		t.Errorf("auto-title: got %q, want %q", got.Title, "hello there")
	}
}
