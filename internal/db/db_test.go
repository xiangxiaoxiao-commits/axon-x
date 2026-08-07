package db_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"axon/internal/db"
	"axon/internal/model"
	"axon/internal/store/sqlite"
)

// Environment variables used to drive the crash-recovery child process. The
// test binary re-invokes itself with these set (see TestMain) so we can commit
// data and then SIGKILL the process without any chance of a clean Close.
const (
	envCrashDir = "AXON_CRASH_TEST_DIR"
)

// Fixed data written by the crash child and asserted by the parent, so both
// sides agree on exactly what durable state must survive the kill.
const (
	crashConvTitle = "crash-recovery-conversation"
	crashUserMsg   = "user message written before crash"
	crashAsstMsg   = "assistant reply written before crash"
)

// TestMain intercepts execution: when envCrashDir is set this process is the
// crash "writer" child, so it runs that branch and never returns. Otherwise it
// runs the normal test suite.
func TestMain(m *testing.M) {
	if dir := os.Getenv(envCrashDir); dir != "" {
		runCrashWriter(dir)
		return // unreachable: runCrashWriter kills the process.
	}
	os.Exit(m.Run())
}

// runCrashWriter opens the database, commits a conversation plus two messages,
// then hard-kills its own process with SIGKILL. It must never call Close or
// exit normally, so recovery is exercised against a real crash.
func runCrashWriter(dir string) {
	ctx := context.Background()

	sqlDB, err := db.Open(dir)
	if err != nil {
		// Exit non-signaled so the parent can distinguish a setup failure.
		os.Exit(2)
	}

	s := sqlite.New(sqlDB)

	conv, err := s.CreateConversation(ctx, model.Conversation{Title: crashConvTitle})
	if err != nil {
		os.Exit(3)
	}
	if _, err := s.AppendMessage(ctx, model.Message{
		ConversationID: conv.ID,
		Role:           model.RoleUser,
		Content:        crashUserMsg,
	}); err != nil {
		os.Exit(4)
	}
	if _, err := s.AppendMessage(ctx, model.Message{
		ConversationID: conv.ID,
		Role:           model.RoleAssistant,
		Content:        crashAsstMsg,
	}); err != nil {
		os.Exit(5)
	}

	// Everything above is committed. Simulate a crash / power loss: no Close,
	// no deferred cleanup, no normal exit.
	_ = syscall.Kill(os.Getpid(), syscall.SIGKILL)

	// Should never be reached; block forever just in case the signal is slow.
	select {}
}

// TestWALEnabled verifies db.Open puts the database in WAL journal mode
// (Phase 1 acceptance: PRAGMA journal_mode == "wal").
func TestWALEnabled(t *testing.T) {
	sqlDB, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer sqlDB.Close()

	var mode string
	if err := sqlDB.QueryRow(`PRAGMA journal_mode`).Scan(&mode); err != nil {
		t.Fatalf("query journal_mode: %v", err)
	}
	if strings.ToLower(mode) != "wal" {
		t.Errorf("journal_mode: got %q, want %q", mode, "wal")
	}
}

// TestForeignKeysEnabled verifies foreign key enforcement is on, which the
// cascade-delete behavior depends on.
func TestForeignKeysEnabled(t *testing.T) {
	sqlDB, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer sqlDB.Close()

	var on int
	if err := sqlDB.QueryRow(`PRAGMA foreign_keys`).Scan(&on); err != nil {
		t.Fatalf("query foreign_keys: %v", err)
	}
	if on != 1 {
		t.Errorf("foreign_keys: got %d, want 1", on)
	}
}

// TestMigrationIdempotent verifies opening the same data directory twice does
// not error and records migration version 1 exactly once.
func TestMigrationIdempotent(t *testing.T) {
	dir := t.TempDir()

	first, err := db.Open(dir)
	if err != nil {
		t.Fatalf("first Open: %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("close first: %v", err)
	}

	second, err := db.Open(dir)
	if err != nil {
		t.Fatalf("second Open (should be idempotent): %v", err)
	}
	defer second.Close()

	var count int
	if err := second.QueryRow(
		`SELECT COUNT(*) FROM schema_migrations WHERE version = 1`,
	).Scan(&count); err != nil {
		t.Fatalf("query schema_migrations: %v", err)
	}
	if count != 1 {
		t.Errorf("schema_migrations version=1 rows: got %d, want 1", count)
	}
}

// TestCrashRecovery is the key Phase 1 durability test (US-1.1 / NFR 6.3).
// A child process commits a conversation and two messages, then SIGKILLs
// itself. The parent reopens the same database in a fresh process and asserts
// the committed data survived intact.
func TestCrashRecovery(t *testing.T) {
	dir := t.TempDir()

	// Re-invoke this test binary; TestMain routes it into runCrashWriter.
	cmd := exec.Command(os.Args[0])
	cmd.Env = append(os.Environ(), envCrashDir+"="+dir)
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr

	err := cmd.Run()
	if err == nil {
		t.Fatal("crash writer exited cleanly; expected it to be killed by SIGKILL")
	}

	exitErr, ok := err.(*exec.ExitError)
	if !ok {
		t.Fatalf("expected *exec.ExitError, got %T: %v", err, err)
	}
	status, ok := exitErr.Sys().(syscall.WaitStatus)
	if !ok {
		t.Fatalf("expected syscall.WaitStatus, got %T", exitErr.Sys())
	}
	if !status.Signaled() || status.Signal() != syscall.SIGKILL {
		t.Fatalf("child did not die by SIGKILL: signaled=%v signal=%v exitcode=%d",
			status.Signaled(), status.Signal(), status.ExitStatus())
	}

	// Sanity: the WAL file should exist on disk after the killed writer.
	if _, statErr := os.Stat(filepath.Join(dir, "axon.db")); statErr != nil {
		t.Fatalf("axon.db missing after crash: %v", statErr)
	}

	// Reopen in this (fresh) process and verify durability.
	sqlDB, err := db.Open(dir)
	if err != nil {
		t.Fatalf("reopen after crash: %v", err)
	}
	defer sqlDB.Close()

	s := sqlite.New(sqlDB)
	ctx := context.Background()

	convs, err := s.ListConversations(ctx)
	if err != nil {
		t.Fatalf("ListConversations after crash: %v", err)
	}
	if len(convs) != 1 {
		t.Fatalf("expected 1 conversation after crash, got %d", len(convs))
	}
	if convs[0].Title != crashConvTitle {
		t.Errorf("conversation title: got %q, want %q", convs[0].Title, crashConvTitle)
	}

	msgs, err := s.ListMessages(ctx, convs[0].ID)
	if err != nil {
		t.Fatalf("ListMessages after crash: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages after crash, got %d", len(msgs))
	}
	if msgs[0].Content != crashUserMsg {
		t.Errorf("message[0] content: got %q, want %q", msgs[0].Content, crashUserMsg)
	}
	if msgs[1].Content != crashAsstMsg {
		t.Errorf("message[1] content: got %q, want %q", msgs[1].Content, crashAsstMsg)
	}
}
