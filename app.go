package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"axon/internal/config"
	"axon/internal/db"
	"axon/internal/embed"
	"axon/internal/model"
	"axon/internal/provider"
	"axon/internal/secret"
	"axon/internal/store"
	"axon/internal/store/sqlite"
	"axon/internal/task"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// Chat stream event names emitted to the frontend during a completion.
const (
	EventDelta = "chat:delta"
	EventDone  = "chat:done"
	EventError = "chat:error"
)

// streamEvent is the payload for chat:* events.
type streamEvent struct {
	ConversationID   string `json:"conversationId"`
	MessageID        int64  `json:"messageId"`
	Delta            string `json:"delta,omitempty"`
	Error            string `json:"error,omitempty"`
	PromptTokens     int    `json:"promptTokens,omitempty"`
	CompletionTokens int    `json:"completionTokens,omitempty"`
}

// App is the Wails-bound application. Its exported methods are callable from
// the Svelte frontend; it wires persistence, secrets and model providers.
type App struct {
	ctx     context.Context
	store   store.Store
	secrets secret.Store
	cfg     *config.Manager

	// mu guards the cancel funcs of in-flight streams, keyed by conversation id,
	// so the frontend can stop generation per conversation.
	mu      sync.Mutex
	cancels map[string]context.CancelFunc

	// emit sends an event to the frontend. Injectable so streaming logic can be
	// tested without a live Wails runtime; defaults to wruntime.EventsEmit.
	emit func(event string, data ...interface{})

	// term is the embedded PTY shell session (Terminal tab).
	term termState

	// Task orchestration: persistence, a concurrency semaphore (buffered =
	// max parallel executions) and per-task cancel funcs guarded by taskMu.
	taskStore   task.Store
	taskSem     chan struct{}
	taskMu      sync.Mutex
	taskCancels map[string]context.CancelFunc

	// graphMu serializes manual knowledge-graph edits (load→modify→save) so
	// concurrent edits can't clobber each other's writes. Graph persistence is
	// file-level (atomic rename), so this is a best-effort guard for this
	// process's own edit paths.
	graphMu sync.Mutex

	// embedProbe memoizes whether a configured cloud embedding endpoint can
	// actually embed, so recall paths fall back to local without re-probing the
	// network on every call. Lazily created via embedProbeCache.
	embedProbe *embed.ProbeCache
}

// NewApp creates a new App application struct.
func NewApp() *App {
	return &App{
		cancels:     make(map[string]context.CancelFunc),
		taskSem:     make(chan struct{}, defaultTaskConcurrency),
		taskCancels: make(map[string]context.CancelFunc),
		embedProbe:  embed.NewProbeCache(),
	}
}

// startup opens the archive database, config and keychain. A failure to open
// the database is fatal: durable storage is the app's core promise.
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	if a.emit == nil {
		a.emit = func(event string, data ...interface{}) {
			wruntime.EventsEmit(a.ctx, event, data...)
		}
	}

	dataDir, err := db.AppDataDir()
	if err != nil {
		log.Fatalf("resolve app data dir: %v", err)
	}
	sqlDB, err := db.Open(dataDir)
	if err != nil {
		log.Fatalf("open database: %v", err)
	}
	a.store = sqlite.New(sqlDB)
	a.taskStore = sqlite.NewTaskStore(sqlDB)
	a.recoverStaleTasks() // reset tasks left mid-flight by a crash/quit

	cfg, err := config.Load(dataDir)
	if err != nil {
		log.Fatalf("load config: %v", err)
	}
	a.cfg = cfg
	a.secrets = secret.New()

	log.Printf("axon: ready at %s", dataDir)
}

// --- Conversation API ---

// NewConversation creates an empty conversation and returns it.
func (a *App) NewConversation(title, taskType, modelName string) (model.Conversation, error) {
	return a.store.CreateConversation(a.ctx, model.Conversation{
		Title:    title,
		TaskType: taskType,
		Model:    modelName,
	})
}

// ListConversations returns all conversations, newest activity first.
func (a *App) ListConversations() ([]model.Conversation, error) {
	return a.store.ListConversations(a.ctx)
}

// RenameConversation updates a conversation title.
func (a *App) RenameConversation(id, title string) error {
	return a.store.RenameConversation(a.ctx, id, title)
}

// DeleteConversation removes a conversation and its messages.
func (a *App) DeleteConversation(id string) error {
	return a.store.DeleteConversation(a.ctx, id)
}

// ListMessages returns all messages of a conversation in order.
func (a *App) ListMessages(conversationID string) ([]model.Message, error) {
	return a.store.ListMessages(a.ctx, conversationID)
}

// --- Settings / Provider API ---

// ProviderInfo is a safe view of a provider config for the frontend: it never
// includes the API key, only whether one is stored.
type ProviderInfo struct {
	provider.Config
	HasKey bool `json:"hasKey"`
}

// ListProviders returns configured providers with key-presence flags (no keys).
func (a *App) ListProviders() []ProviderInfo {
	cfg := a.cfg.Get()
	out := make([]ProviderInfo, 0, len(cfg.Providers))
	for _, p := range cfg.Providers {
		out = append(out, ProviderInfo{Config: p, HasKey: a.secrets.Has(p.KeyRef)})
	}
	return out
}

// SaveProvider persists a provider config and, when apiKey is non-empty, stores
// the key in the Keychain under the provider's KeyRef. The key never lands in
// the config file. A blank apiKey leaves any existing stored key untouched.
func (a *App) SaveProvider(p provider.Config, apiKey string) error {
	if p.Name == "" || p.Protocol == "" || p.BaseURL == "" {
		return fmt.Errorf("provider name, protocol and baseURL are required")
	}
	if p.KeyRef == "" {
		p.KeyRef = "provider:" + p.Name
	}
	if apiKey != "" {
		if err := a.secrets.Set(p.KeyRef, apiKey); err != nil {
			return fmt.Errorf("store api key: %w", err)
		}
	}
	return a.cfg.UpsertProvider(p)
}

// SetDefaults sets the default provider and model.
func (a *App) SetDefaults(providerName, modelID string) error {
	return a.cfg.SetDefaults(providerName, modelID)
}

// EmbeddingConfig is the current semantic-memory embedding selection for the UI.
type EmbeddingConfig struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`
	// Mode is "semantic" or "keyword" (empty defaults to keyword). It selects
	// the recall backend explicitly; there is no silent cloud->local fallback.
	Mode string `json:"mode"`
}

// GetEmbeddingConfig returns the configured embedding provider/model so the
// settings UI can prefill its embedding section.
func (a *App) GetEmbeddingConfig() EmbeddingConfig {
	cfg := a.cfg.Get()
	mode := cfg.EmbeddingMode
	if mode == "" {
		mode = config.EmbeddingModeKeyword // empty means keyword (offline default)
	}
	return EmbeddingConfig{Provider: cfg.EmbeddingProvider, Model: cfg.EmbeddingModel, Mode: mode}
}

// SetEmbeddingMode persists the recall backend selection: "semantic" (cloud
// model only, no fallback) or "keyword" (local lexical embedder).
func (a *App) SetEmbeddingMode(mode string) error {
	switch mode {
	case config.EmbeddingModeSemantic, config.EmbeddingModeKeyword:
	default:
		return fmt.Errorf("invalid embedding mode %q (want %q or %q)", mode, config.EmbeddingModeSemantic, config.EmbeddingModeKeyword)
	}
	return a.cfg.SetEmbeddingMode(mode)
}

// SetEmbeddingConfig persists the provider/model used for semantic-memory
// embeddings. An empty providerName clears the explicit selection and restores
// the fallback (first OpenAI-compatible provider). A non-empty provider must be
// configured and use the openai protocol.
func (a *App) SetEmbeddingConfig(providerName, model string) error {
	providerName = strings.TrimSpace(providerName)
	model = strings.TrimSpace(model)
	if providerName != "" {
		pc, ok := a.cfg.Provider(providerName)
		if !ok {
			return fmt.Errorf("provider %q not configured", providerName)
		}
		if pc.Protocol != "openai" {
			return fmt.Errorf("embedding provider must use the openai protocol; %q is %q", providerName, pc.Protocol)
		}
	}
	return a.cfg.SetEmbedding(providerName, model)
}

// TestEmbedding verifies the current embedding configuration by issuing one
// embed call, returning an error the UI can surface. Unlike newEmbedder (the
// recall path, which silently falls back to local so recall never breaks), this
// explicit entry point reports misconfiguration and cloud-embedding failures so
// the user can actively discover a bad setup. When no cloud embedding is
// configured it confirms the local fallback works.
func (a *App) TestEmbedding() error {
	cloud, _, err := a.buildCloudEmbedder()
	if err != nil {
		return err
	}
	emb := cloud
	if cloud == nil {
		emb = embed.NewLocal()
	}
	ctx, cancel := context.WithTimeout(a.ctx, 20*time.Second)
	defer cancel()
	if _, err := emb.Embed(ctx, "axon embedding connectivity test"); err != nil {
		return err
	}
	return nil
}

// newProvider builds a live Provider for the given config, resolving its key
// from the Keychain at call time (never persisted elsewhere).
func (a *App) newProvider(pc provider.Config) (provider.Provider, error) {
	key, err := a.secrets.Get(pc.KeyRef)
	if err != nil {
		if errors.Is(err, secret.ErrNotFound) {
			return nil, fmt.Errorf("no API key configured for provider %q", pc.Name)
		}
		return nil, fmt.Errorf("resolve api key: %w", err)
	}
	switch pc.Protocol {
	case "openai":
		return provider.NewOpenAI(pc.BaseURL, key), nil
	case "anthropic":
		return provider.NewAnthropic(pc.BaseURL, key), nil
	default:
		return nil, fmt.Errorf("unknown provider protocol %q", pc.Protocol)
	}
}

// ListModels returns the model ids a configured provider exposes, so the
// settings UI can offer a dropdown of real models instead of a free-typed name.
// For OpenAI-compatible providers it queries {baseURL}/models; for Anthropic it
// returns a curated list (its models endpoint is less universally available).
func (a *App) ListModels(providerName string) ([]string, error) {
	pc, ok := a.cfg.Provider(providerName)
	if !ok {
		return nil, fmt.Errorf("provider %q not configured", providerName)
	}
	if pc.Protocol == "anthropic" {
		return []string{
			"claude-opus-4-8", "claude-sonnet-5", "claude-haiku-4-5",
		}, nil
	}
	prov, err := a.newProvider(pc)
	if err != nil {
		return nil, err
	}
	op, ok := prov.(*provider.OpenAIProvider)
	if !ok {
		return nil, fmt.Errorf("provider %q does not support listing models", providerName)
	}
	ctx, cancel := context.WithTimeout(a.ctx, 20*time.Second)
	defer cancel()
	return op.ListModels(ctx)
}

// providerForProtocol returns the name of a configured provider whose protocol
// matches, preferring the default provider when it qualifies.
func (a *App) providerForProtocol(protocol string) (string, bool) {
	cfg := a.cfg.Get()
	if cfg.DefaultProvider != "" {
		if pc, ok := a.cfg.Provider(cfg.DefaultProvider); ok && pc.Protocol == protocol {
			return pc.Name, true
		}
	}
	for _, pc := range cfg.Providers {
		if pc.Protocol == protocol {
			return pc.Name, true
		}
	}
	return "", false
}

// --- Chat (streaming) ---

// SendMessage persists the user's message, then streams an assistant reply from
// the selected provider. Deltas are emitted as chat:delta events; completion as
// chat:done; failures as chat:error. The assistant message is persisted as a
// 'streaming' placeholder first and finalized (or marked interrupted) at the
// end, so a stop or crash never loses partial output (NFR 6.3).
//
// providerName/modelID may be empty to use configured defaults. It returns the
// assistant message id so the frontend can correlate stream events.
// injectContext, when non-empty, is prepended as a system message — used to
// feed matched knowledge-graph background into the reply (chat injection).
func (a *App) SendMessage(conversationID, content, providerName, modelID string, temperature float64, maxTokens int, injectContext string) (int64, error) {
	cfg := a.cfg.Get()
	if providerName == "" {
		providerName = cfg.DefaultProvider
	}
	if modelID == "" {
		modelID = cfg.DefaultModel
	}
	pc, ok := a.cfg.Provider(providerName)
	if !ok {
		return 0, fmt.Errorf("provider %q not configured", providerName)
	}
	prov, err := a.newProvider(pc)
	if err != nil {
		return 0, err
	}

	// Persist the user turn immediately.
	if _, err := a.store.AppendMessage(a.ctx, model.Message{
		ConversationID: conversationID,
		Role:           model.RoleUser,
		Content:        content,
		Status:         model.StatusComplete,
	}); err != nil {
		return 0, fmt.Errorf("persist user message: %w", err)
	}
	a.maybeTitle(conversationID, content)

	// Build the prompt from full history so the model has context.
	history, err := a.store.ListMessages(a.ctx, conversationID)
	if err != nil {
		return 0, fmt.Errorf("load history: %w", err)
	}
	reqMsgs := make([]provider.ChatMessage, 0, len(history)+1)
	// Knowledge-graph background matched from the user's message.
	if strings.TrimSpace(injectContext) != "" {
		reqMsgs = append(reqMsgs, provider.ChatMessage{Role: provider.RoleSystem, Content: injectContext})
	}
	for _, m := range history {
		reqMsgs = append(reqMsgs, provider.ChatMessage{Role: m.Role, Content: m.Content})
	}

	// Persist a streaming placeholder for the assistant reply.
	asst, err := a.store.AppendMessage(a.ctx, model.Message{
		ConversationID: conversationID,
		Role:           model.RoleAssistant,
		Model:          modelID,
		Status:         model.StatusStreaming,
	})
	if err != nil {
		return 0, fmt.Errorf("persist assistant placeholder: %w", err)
	}
	a.store.TouchConversation(a.ctx, conversationID, modelID, "")

	// Per-conversation cancellable context for stop-generation.
	streamCtx, cancel := context.WithCancel(a.ctx)
	a.mu.Lock()
	if prev, ok := a.cancels[conversationID]; ok {
		prev() // supersede any in-flight stream for this conversation
	}
	a.cancels[conversationID] = cancel
	a.mu.Unlock()

	go a.runStream(streamCtx, prov, provider.ChatRequest{
		Model:       modelID,
		Messages:    reqMsgs,
		Temperature: temperature,
		MaxTokens:   maxTokens,
	}, conversationID, asst.ID)
	return asst.ID, nil
}

// StopGeneration cancels the in-flight stream for a conversation, if any.
func (a *App) StopGeneration(conversationID string) {
	a.mu.Lock()
	cancel := a.cancels[conversationID]
	a.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// runStream consumes the provider stream, emits events to the frontend, and
// persists the final content. It accumulates deltas so the assistant message is
// saved even if the stream is stopped or errors mid-way.
func (a *App) runStream(ctx context.Context, prov provider.Provider, req provider.ChatRequest, convID string, msgID int64) {
	defer func() {
		a.mu.Lock()
		delete(a.cancels, convID)
		a.mu.Unlock()
	}()

	chunks, errs := prov.Chat(ctx, req)
	var b strings.Builder
	var prompt, completion int

	for chunk := range chunks {
		if chunk.Delta != "" {
			b.WriteString(chunk.Delta)
			a.emit(EventDelta, streamEvent{
				ConversationID: convID, MessageID: msgID, Delta: chunk.Delta,
			})
		}
		if chunk.Done {
			prompt, completion = chunk.PromptTokens, chunk.CompletionTokens
		}
	}

	// The stream channel is closed; check whether it ended due to an error.
	var streamErr error
	select {
	case streamErr = <-errs:
	default:
	}

	// Persist whatever we accumulated. Status reflects how the stream ended.
	status := model.StatusComplete
	if streamErr != nil {
		if errors.Is(streamErr, context.Canceled) {
			status = model.StatusInterrupted // user stopped generation
		} else {
			status = model.StatusInterrupted // real failure; keep partial text
		}
	}
	if err := a.store.UpdateMessageContent(a.ctx, msgID, b.String(), prompt, completion, status); err != nil {
		log.Printf("axon: finalize message %d: %v", msgID, err)
	}

	switch {
	case streamErr != nil && errors.Is(streamErr, context.Canceled):
		a.emit(EventDone, streamEvent{
			ConversationID: convID, MessageID: msgID,
			PromptTokens: prompt, CompletionTokens: completion,
		})
	case streamErr != nil:
		// Log with detail; send a readable message to the UI (no secrets — the
		// provider layer already sanitized it).
		log.Printf("axon: stream error for conv %s: %v", convID, streamErr)
		a.emit(EventError, streamEvent{
			ConversationID: convID, MessageID: msgID, Error: streamErr.Error(),
		})
	default:
		a.emit(EventDone, streamEvent{
			ConversationID: convID, MessageID: msgID,
			PromptTokens: prompt, CompletionTokens: completion,
		})
	}
}

// maybeTitle sets a conversation title from the first user message when it is
// still untitled (F1.5). Best-effort: errors are logged, not fatal.
func (a *App) maybeTitle(convID, firstContent string) {
	c, err := a.store.GetConversation(a.ctx, convID)
	if err != nil || strings.TrimSpace(c.Title) != "" {
		return
	}
	title := strings.TrimSpace(firstContent)
	if len(title) > 60 {
		title = title[:60] + "..."
	}
	if title == "" {
		title = "New conversation " + time.Now().Format("2006-01-02 15:04")
	}
	if err := a.store.RenameConversation(a.ctx, convID, title); err != nil {
		log.Printf("axon: auto-title conv %s: %v", convID, err)
	}
}
