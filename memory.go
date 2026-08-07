package main

import (
	"context"
	"fmt"
	"log"
	"sort"
	"strings"
	"time"

	"axon/internal/embed"
	"axon/internal/model"
	"axon/internal/provider"
)

// Memory tuning. Kept simple and explicit; these can move to config later.
const (
	// memoryTopK is how many past memories are considered for a recall.
	memoryTopK = 3
	// memoryMinScore is the cosine-similarity floor below which a memory is not
	// surfaced, to avoid injecting irrelevant context (NFR 6.4).
	memoryMinScore = 0.35
	// idleSummarizeDelay is how long a conversation must be quiet before it is
	// summarized into a memory in the background.
	idleSummarizeDelay = 30 * time.Second
	// summaryMaxChars bounds how much of a conversation is sent to the LLM for
	// summarization, keeping cost predictable.
	summaryMaxChars = 6000
)

// newEmbedder builds an embedder from the first OpenAI-protocol provider that
// has a stored key. Embeddings require an OpenAI-compatible endpoint (Anthropic
// has no embeddings API), so semantic memory degrades gracefully when none is
// configured: callers treat a nil embedder as "semantic features unavailable".
func (a *App) newEmbedder() (embed.Embedder, error) {
	name, ok := a.providerForProtocol("openai")
	if !ok {
		return nil, fmt.Errorf("no OpenAI-compatible provider configured for embeddings")
	}
	pc, _ := a.cfg.Provider(name)
	key, err := a.secrets.Get(pc.KeyRef)
	if err != nil {
		return nil, fmt.Errorf("resolve embedding api key: %w", err)
	}
	// Empty model -> embedder default (text-embedding-3-small).
	return embed.NewOpenAI(pc.BaseURL, key, ""), nil
}

// RecallMemories embeds the query and returns the top matching past memories
// above the score floor, excluding the current conversation. It is best-effort:
// if embeddings are unavailable (offline / no provider) it returns nil so the
// UI can simply show nothing rather than error (graceful degradation, NFR 6.4).
func (a *App) RecallMemories(currentConversationID, query string) ([]model.MemoryHit, error) {
	if strings.TrimSpace(query) == "" {
		return nil, nil
	}
	emb, err := a.newEmbedder()
	if err != nil {
		return nil, nil // semantic recall unavailable; not an error to the user
	}
	qv, err := emb.Embed(a.ctx, query)
	if err != nil {
		log.Printf("axon: recall embed failed: %v", err)
		return nil, nil
	}

	memories, err := a.store.ListMemories(a.ctx)
	if err != nil {
		return nil, fmt.Errorf("list memories: %w", err)
	}

	hits := make([]model.MemoryHit, 0, len(memories))
	for _, m := range memories {
		if m.ConversationID == currentConversationID {
			continue
		}
		score := embed.Cosine(qv, m.Embedding)
		if score < memoryMinScore {
			continue
		}
		title := ""
		if c, err := a.store.GetConversation(a.ctx, m.ConversationID); err == nil {
			title = c.Title
		}
		hits = append(hits, model.MemoryHit{Memory: m, Score: score, Title: title})
	}

	sort.Slice(hits, func(i, j int) bool { return hits[i].Score > hits[j].Score })
	if len(hits) > memoryTopK {
		hits = hits[:memoryTopK]
	}
	return hits, nil
}

// summaryPrompt instructs the model to produce a compact, recall-friendly
// summary. Kept in English for stable model behavior; content stays in the
// conversation's own language.
const summaryPrompt = `Summarize this conversation for future retrieval. In 3-5 sentences, capture: the problem or task, the key decisions or solution, and any important entities (files, libraries, error types). Write in the same language as the conversation. Output only the summary, no preamble.`

// SummarizeConversation generates a summary of a conversation, embeds it, and
// upserts it as a memory. It uses the default provider for the LLM call and an
// OpenAI-compatible provider for the embedding. Returns the stored memory.
func (a *App) SummarizeConversation(conversationID string) (model.Memory, error) {
	msgs, err := a.store.ListMessages(a.ctx, conversationID)
	if err != nil {
		return model.Memory{}, fmt.Errorf("load messages: %w", err)
	}
	if len(msgs) == 0 {
		return model.Memory{}, fmt.Errorf("conversation %q has no messages to summarize", conversationID)
	}

	// Build a bounded transcript for the summarizer.
	var b strings.Builder
	for _, m := range msgs {
		if m.Role == model.RoleSystem {
			continue
		}
		b.WriteString(m.Role)
		b.WriteString(": ")
		b.WriteString(m.Content)
		b.WriteString("\n\n")
		if b.Len() > summaryMaxChars {
			break
		}
	}

	// Summarize via the default provider (a cheap model is fine).
	cfg := a.cfg.Get()
	pc, ok := a.cfg.Provider(cfg.DefaultProvider)
	if !ok {
		return model.Memory{}, fmt.Errorf("no default provider configured for summarization")
	}
	prov, err := a.newProvider(pc)
	if err != nil {
		return model.Memory{}, err
	}
	summary, err := collectReply(a.ctx, prov, provider.ChatRequest{
		Model: cfg.DefaultModel,
		Messages: []provider.ChatMessage{
			{Role: provider.RoleSystem, Content: summaryPrompt},
			{Role: provider.RoleUser, Content: b.String()},
		},
		Temperature: 0.2,
		MaxTokens:   512,
	})
	if err != nil {
		return model.Memory{}, fmt.Errorf("summarize: %w", err)
	}
	summary = strings.TrimSpace(summary)
	if summary == "" {
		return model.Memory{}, fmt.Errorf("summarizer returned empty summary")
	}

	// Embed the summary.
	emb, err := a.newEmbedder()
	if err != nil {
		return model.Memory{}, fmt.Errorf("embedder unavailable: %w", err)
	}
	vec, err := emb.Embed(a.ctx, summary)
	if err != nil {
		return model.Memory{}, fmt.Errorf("embed summary: %w", err)
	}

	return a.store.UpsertMemory(a.ctx, model.Memory{
		ConversationID: conversationID,
		Summary:        summary,
		Embedding:      vec,
		EmbedModel:     emb.Model(),
	})
}

// collectReply runs a non-streaming-style collection over the provider's stream,
// concatenating all deltas into the full reply text.
func collectReply(ctx context.Context, prov provider.Provider, req provider.ChatRequest) (string, error) {
	chunks, errs := prov.Chat(ctx, req)
	var b strings.Builder
	for chunk := range chunks {
		b.WriteString(chunk.Delta)
	}
	select {
	case err := <-errs:
		if err != nil {
			return "", err
		}
	default:
	}
	return b.String(), nil
}

// BackfillMemories summarizes every conversation that lacks a memory, returning
// how many were processed. Best-effort per conversation: a failure on one is
// logged and skipped so one bad conversation does not abort the batch (F4.8).
func (a *App) BackfillMemories() (int, error) {
	ids, err := a.store.ConversationsWithoutMemory(a.ctx)
	if err != nil {
		return 0, fmt.Errorf("list conversations without memory: %w", err)
	}
	done := 0
	for _, id := range ids {
		if _, err := a.SummarizeConversation(id); err != nil {
			log.Printf("axon: backfill memory for %s failed: %v", id, err)
			continue
		}
		done++
	}
	return done, nil
}

// DeleteMemory removes the memory for a conversation (F4.9 management).
func (a *App) DeleteMemory(conversationID string) error {
	return a.store.DeleteMemory(a.ctx, conversationID)
}

// MemoryEntry is a memory plus its source conversation title, for the memory
// management view (F4.9).
type MemoryEntry struct {
	model.Memory
	Title string `json:"title"`
}

// ListMemories returns all stored memories with their source conversation
// titles, so the memory view can browse the library directly. Embeddings are
// omitted from the JSON (the Memory type tags them json:"-").
func (a *App) ListMemories() ([]MemoryEntry, error) {
	memories, err := a.store.ListMemories(a.ctx)
	if err != nil {
		return nil, fmt.Errorf("list memories: %w", err)
	}
	out := make([]MemoryEntry, 0, len(memories))
	for _, m := range memories {
		title := m.ConversationID
		if c, err := a.store.GetConversation(a.ctx, m.ConversationID); err == nil && c.Title != "" {
			title = c.Title
		}
		out = append(out, MemoryEntry{Memory: m, Title: title})
	}
	return out, nil
}

// buildMemoryInjection composes a system message from the summaries of the
// selected conversations, with source attribution. Returns "" when nothing is
// selected or resolvable, so callers can skip injection entirely.
func (a *App) buildMemoryInjection(conversationIDs []string) string {
	if len(conversationIDs) == 0 {
		return ""
	}
	memories, err := a.store.ListMemories(a.ctx)
	if err != nil {
		log.Printf("axon: load memories for injection: %v", err)
		return ""
	}
	byConv := make(map[string]string, len(memories))
	for _, m := range memories {
		byConv[m.ConversationID] = m.Summary
	}

	var b strings.Builder
	b.WriteString("Relevant context from the user's past conversations. Use it if helpful; cite which one when you rely on it.\n\n")
	added := 0
	for _, id := range conversationIDs {
		summary, ok := byConv[id]
		if !ok || strings.TrimSpace(summary) == "" {
			continue
		}
		title := id
		if c, err := a.store.GetConversation(a.ctx, id); err == nil && c.Title != "" {
			title = c.Title
		}
		fmt.Fprintf(&b, "- [%s]: %s\n", title, summary)
		added++
	}
	if added == 0 {
		return ""
	}
	return b.String()
}

// scheduleSummary (re)starts the idle timer for a conversation. When the
// conversation stays quiet for idleSummarizeDelay, it is summarized into a
// memory in the background. Each new message resets the timer so only settled
// conversations are summarized.
func (a *App) scheduleSummary(conversationID string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.summaryTimers == nil {
		a.summaryTimers = make(map[string]*time.Timer)
	}
	if t, ok := a.summaryTimers[conversationID]; ok {
		t.Stop()
	}
	a.summaryTimers[conversationID] = time.AfterFunc(idleSummarizeDelay, func() {
		a.mu.Lock()
		delete(a.summaryTimers, conversationID)
		a.mu.Unlock()
		if _, err := a.SummarizeConversation(conversationID); err != nil {
			log.Printf("axon: idle summarize %s: %v", conversationID, err)
		}
	})
}
