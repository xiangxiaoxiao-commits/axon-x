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
	"axon/internal/model"
	"axon/internal/provider"
	"axon/internal/routing"
	"axon/internal/secret"
	"axon/internal/store"
	"axon/internal/store/sqlite"

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

	// routes is the task-type -> model recommendation table.
	routes routing.Table

	// summaryTimers holds per-conversation idle timers; when one fires the
	// conversation is summarized into a memory in the background. Guarded by mu.
	summaryTimers map[string]*time.Timer
}

// NewApp creates a new App application struct.
func NewApp() *App {
	return &App{
		cancels:       make(map[string]context.CancelFunc),
		summaryTimers: make(map[string]*time.Timer),
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

	cfg, err := config.Load(dataDir)
	if err != nil {
		log.Fatalf("load config: %v", err)
	}
	a.cfg = cfg
	a.secrets = secret.NewKeychainStore()

	routes, err := routing.Default()
	if err != nil {
		log.Fatalf("load routing table: %v", err)
	}
	a.routes = routes

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

// --- Routing / task recommendation API (Phase 3) ---

// RoutingTable returns the full task-type recommendation table for display and
// editing in settings.
func (a *App) RoutingTable() routing.Table {
	return a.routes
}

// ClassifyTask heuristically guesses a task type from free-form input. The
// frontend uses this to preselect a task on the first message; the user can
// always override.
func (a *App) ClassifyTask(input string) string {
	return routing.Classify(input)
}

// Recommendation is what the frontend shows in the "recommendation bar":
// the chosen model plus reference cost/IQ/time, and whether a usable provider
// is actually configured for it.
type Recommendation struct {
	TaskType     string  `json:"taskType"`
	Tier         string  `json:"tier"` // "primary" | "alternate"
	Provider     string  `json:"provider"`
	Model        string  `json:"model"`
	Temperature  float64 `json:"temperature"`
	MaxTokens    int     `json:"maxTokens"`
	IQ           float64 `json:"iq"`
	CostUSD      float64 `json:"costUsd"`
	Minutes      float64 `json:"minutes"`
	ProviderName string  `json:"providerName"` // configured provider matched by protocol, if any
	Available    bool    `json:"available"`    // a configured provider with a stored key exists
}

// Recommend returns the recommendation for a task type and tier ("primary" or
// "alternate"), resolving which configured provider (if any) can serve it.
func (a *App) Recommend(taskType, tier string) (Recommendation, error) {
	profile, ok := a.routes.Profiles[taskType]
	if !ok {
		return Recommendation{}, fmt.Errorf("unknown task type %q", taskType)
	}
	rec := profile.Primary
	if tier == "alternate" {
		rec = profile.Alternate
	} else {
		tier = "primary"
	}

	out := Recommendation{
		TaskType:    taskType,
		Tier:        tier,
		Provider:    rec.Provider,
		Model:       rec.Model,
		Temperature: rec.Temperature,
		MaxTokens:   rec.MaxTokens,
		IQ:          rec.IQ,
		CostUSD:     rec.CostUSD,
		Minutes:     rec.Minutes,
	}

	// Match a configured provider by protocol (rec.Provider is a protocol name).
	if name, ok := a.providerForProtocol(rec.Provider); ok {
		out.ProviderName = name
		if pc, ok := a.cfg.Provider(name); ok {
			out.Available = a.secrets.Has(pc.KeyRef)
		}
	}
	return out, nil
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
// injectMemoryIDs are conversation ids whose summaries the user chose to inject
// as extra context for this turn (Phase 4). Empty means no injection.
func (a *App) SendMessage(conversationID, content, providerName, modelID string, temperature float64, maxTokens int, injectMemoryIDs []string) (int64, error) {
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
	// Prepend user-selected memories as a system message with clear attribution,
	// so the model can use past context and the user knows what was injected.
	if inj := a.buildMemoryInjection(injectMemoryIDs); inj != "" {
		reqMsgs = append(reqMsgs, provider.ChatMessage{Role: provider.RoleSystem, Content: inj})
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
		// Conversation produced a complete reply; (re)arm the idle timer so it
		// gets summarized into a memory once it settles (Phase 4).
		a.scheduleSummary(convID)
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
