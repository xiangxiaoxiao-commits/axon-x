package main

import (
	"context"
	"fmt"
	"log"

	"axon/internal/db"
	"axon/internal/model"
	"axon/internal/store"
	"axon/internal/store/sqlite"
)

// App is the Wails-bound application. Its exported methods are callable from
// the Svelte frontend; it delegates persistence to the store layer.
type App struct {
	ctx   context.Context
	store store.Store
}

// NewApp creates a new App application struct.
func NewApp() *App {
	return &App{}
}

// startup opens the archive database and wires up the store. A failure here is
// fatal: without durable storage the app cannot fulfill its core promise.
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx

	dataDir, err := db.AppDataDir()
	if err != nil {
		log.Fatalf("resolve app data dir: %v", err)
	}

	sqlDB, err := db.Open(dataDir)
	if err != nil {
		log.Fatalf("open database: %v", err)
	}

	a.store = sqlite.New(sqlDB)
	log.Printf("axon: archive ready at %s", dataDir)
}

// --- Conversation API (bound to frontend) ---

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

// --- Message API (bound to frontend) ---

// ListMessages returns all messages of a conversation in order.
func (a *App) ListMessages(conversationID string) ([]model.Message, error) {
	return a.store.ListMessages(a.ctx, conversationID)
}

// AppendMessage persists one message immediately (append-only).
// Real streaming/model calls arrive in Phase 2; for now this lets the
// frontend exercise the durable archive end to end.
func (a *App) AppendMessage(conversationID, role, content string) (model.Message, error) {
	return a.store.AppendMessage(a.ctx, model.Message{
		ConversationID: conversationID,
		Role:           role,
		Content:        content,
		Status:         model.StatusComplete,
	})
}

// Greet is a leftover smoke-test method kept until the frontend replaces the
// scaffold view; harmless and used to verify the Go<->JS bridge.
func (a *App) Greet(name string) string {
	return fmt.Sprintf("Hello %s, It's show time!", name)
}
