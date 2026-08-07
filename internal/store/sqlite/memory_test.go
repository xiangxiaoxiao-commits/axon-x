package sqlite_test

import (
	"context"
	"sort"
	"testing"
	"time"

	"axon/internal/embed"
	"axon/internal/model"
	"axon/internal/store/sqlite"
)

// mustUpsertMemory upserts a memory and fails the test on error.
func mustUpsertMemory(t *testing.T, s *sqlite.Store, m model.Memory) model.Memory {
	t.Helper()
	got, err := s.UpsertMemory(context.Background(), m)
	if err != nil {
		t.Fatalf("UpsertMemory(%q): %v", m.ConversationID, err)
	}
	return got
}

// findMemory returns the memory for a conversation from a ListMemories result,
// failing the test if it is absent.
func findMemory(t *testing.T, memories []model.Memory, conversationID string) model.Memory {
	t.Helper()
	for _, m := range memories {
		if m.ConversationID == conversationID {
			return m
		}
	}
	t.Fatalf("memory for conversation %q not found in %d memories", conversationID, len(memories))
	return model.Memory{}
}

// TestUpsertMemoryIdempotent verifies that a second upsert for the same
// conversation replaces content in place (one row per conversation), keeps
// created_at, advances updated_at and derives dim from the embedding.
func TestUpsertMemoryIdempotent(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()

	c := mustCreate(t, s, "conv with memory")

	first := mustUpsertMemory(t, s, model.Memory{
		ConversationID: c.ID,
		Summary:        "first summary",
		Embedding:      []float32{0.1, 0.2, 0.3},
		EmbedModel:     "test-embed-v1",
	})
	if first.Dim != 3 {
		t.Errorf("first upsert dim: got %d, want 3", first.Dim)
	}
	if first.CreatedAt == 0 || first.UpdatedAt == 0 {
		t.Errorf("expected non-zero timestamps, got created=%d updated=%d", first.CreatedAt, first.UpdatedAt)
	}

	// Ensure the clock advances so updated_at can strictly increase.
	time.Sleep(5 * time.Millisecond)

	second := mustUpsertMemory(t, s, model.Memory{
		ConversationID: c.ID,
		Summary:        "second summary",
		Embedding:      []float32{0.4, 0.5, 0.6, 0.7},
		EmbedModel:     "test-embed-v2",
	})

	// Same row (conversation_id UNIQUE): id and created_at are preserved.
	if second.ID != first.ID {
		t.Errorf("upsert changed id: first=%d second=%d (expected same row)", first.ID, second.ID)
	}
	if second.CreatedAt != first.CreatedAt {
		t.Errorf("created_at changed on update: first=%d second=%d", first.CreatedAt, second.CreatedAt)
	}
	if second.UpdatedAt <= first.UpdatedAt {
		t.Errorf("updated_at did not advance: first=%d second=%d", first.UpdatedAt, second.UpdatedAt)
	}
	if second.Dim != 4 {
		t.Errorf("second upsert dim: got %d, want 4", second.Dim)
	}

	// ListMemories must return exactly one row, holding the latest content.
	memories, err := s.ListMemories(ctx)
	if err != nil {
		t.Fatalf("ListMemories: %v", err)
	}
	if len(memories) != 1 {
		t.Fatalf("expected 1 memory after two upserts, got %d", len(memories))
	}
	got := memories[0]
	if got.Summary != "second summary" {
		t.Errorf("summary not updated: got %q, want %q", got.Summary, "second summary")
	}
	if got.EmbedModel != "test-embed-v2" {
		t.Errorf("embed model not updated: got %q, want %q", got.EmbedModel, "test-embed-v2")
	}
	if got.Dim != len(got.Embedding) {
		t.Errorf("dim %d != embedding length %d", got.Dim, len(got.Embedding))
	}
}

// TestMemoryVectorRoundTrip verifies a known embedding survives BLOB encoding
// and decoding through the real database unchanged, element by element.
func TestMemoryVectorRoundTrip(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()

	c := mustCreate(t, s, "vector conv")
	want := []float32{-1.5, 0, 0.25, 3.75, -0.001, 42}

	mustUpsertMemory(t, s, model.Memory{
		ConversationID: c.ID,
		Summary:        "vec",
		Embedding:      want,
		EmbedModel:     "test",
	})

	memories, err := s.ListMemories(ctx)
	if err != nil {
		t.Fatalf("ListMemories: %v", err)
	}
	got := findMemory(t, memories, c.ID).Embedding
	if len(got) != len(want) {
		t.Fatalf("embedding length: got %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("embedding element %d: got %v, want %v", i, got[i], want[i])
		}
	}
}

// TestConversationsWithoutMemory verifies only conversations lacking a memory
// row are returned.
func TestConversationsWithoutMemory(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()

	withMem := mustCreate(t, s, "has memory")
	without1 := mustCreate(t, s, "no memory 1")
	without2 := mustCreate(t, s, "no memory 2")

	mustUpsertMemory(t, s, model.Memory{
		ConversationID: withMem.ID,
		Summary:        "summarized",
		Embedding:      []float32{1, 0, 0},
	})

	ids, err := s.ConversationsWithoutMemory(ctx)
	if err != nil {
		t.Fatalf("ConversationsWithoutMemory: %v", err)
	}

	got := map[string]bool{}
	for _, id := range ids {
		got[id] = true
	}
	if len(ids) != 2 {
		t.Fatalf("expected 2 conversations without memory, got %d (%v)", len(ids), ids)
	}
	if !got[without1.ID] || !got[without2.ID] {
		t.Errorf("expected %q and %q, got %v", without1.ID, without2.ID, ids)
	}
	if got[withMem.ID] {
		t.Errorf("conversation %q has a memory and must be excluded", withMem.ID)
	}
}

// TestMemoryCascadeDelete verifies deleting a conversation also removes its
// memory via ON DELETE CASCADE (foreign_keys=ON).
func TestMemoryCascadeDelete(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()

	c := mustCreate(t, s, "cascade conv")
	mustUpsertMemory(t, s, model.Memory{
		ConversationID: c.ID,
		Summary:        "will cascade",
		Embedding:      []float32{0.1, 0.2},
	})

	// Sanity: the memory exists first.
	memories, err := s.ListMemories(ctx)
	if err != nil {
		t.Fatalf("ListMemories: %v", err)
	}
	if len(memories) != 1 {
		t.Fatalf("expected 1 memory before delete, got %d", len(memories))
	}

	if err := s.DeleteConversation(ctx, c.ID); err != nil {
		t.Fatalf("DeleteConversation: %v", err)
	}

	memories, err = s.ListMemories(ctx)
	if err != nil {
		t.Fatalf("ListMemories after delete: %v", err)
	}
	if len(memories) != 0 {
		t.Errorf("expected 0 memories after cascade delete, got %d (foreign_keys likely OFF)", len(memories))
	}
}

// TestDeleteMemory verifies removing a specific conversation's memory, and that
// deleting a non-existent memory is a no-op (not an error).
func TestDeleteMemory(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()

	keep := mustCreate(t, s, "keep")
	drop := mustCreate(t, s, "drop")
	mustUpsertMemory(t, s, model.Memory{ConversationID: keep.ID, Summary: "keep", Embedding: []float32{1, 0}})
	mustUpsertMemory(t, s, model.Memory{ConversationID: drop.ID, Summary: "drop", Embedding: []float32{0, 1}})

	if err := s.DeleteMemory(ctx, drop.ID); err != nil {
		t.Fatalf("DeleteMemory(%q): %v", drop.ID, err)
	}

	memories, err := s.ListMemories(ctx)
	if err != nil {
		t.Fatalf("ListMemories: %v", err)
	}
	if len(memories) != 1 {
		t.Fatalf("expected 1 memory after delete, got %d", len(memories))
	}
	if memories[0].ConversationID != keep.ID {
		t.Errorf("wrong memory survived: got %q, want %q", memories[0].ConversationID, keep.ID)
	}

	// Deleting a memory that does not exist must not error.
	if err := s.DeleteMemory(ctx, "no-such-conversation"); err != nil {
		t.Errorf("DeleteMemory on missing conversation should be a no-op, got %v", err)
	}
}

// TestMemorySimilarityRanking is an end-to-end stand-in for RecallMemories: it
// stores several memories with known embeddings, then ranks them against a
// query vector using embed.Cosine (the same scoring used in production) and
// asserts the most similar memory ranks first. This exercises the load + score
// + sort path over the real database without any network embedder.
func TestMemorySimilarityRanking(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()

	// query direction; the "near" memory is a small perturbation of it.
	query := []float32{0.9, 0.1, 0.4, 0.2}

	type fixture struct {
		title     string
		embedding []float32
	}
	fixtures := []fixture{
		{"near", []float32{0.85, 0.15, 0.45, 0.25}},
		{"orthogonal", []float32{-0.2, 0.9, -0.3, 0.1}},
		{"opposite", []float32{-0.9, -0.1, -0.4, -0.2}},
	}

	byConv := map[string]string{} // conversation id -> title
	for _, f := range fixtures {
		c := mustCreate(t, s, f.title)
		byConv[c.ID] = f.title
		mustUpsertMemory(t, s, model.Memory{
			ConversationID: c.ID,
			Summary:        f.title,
			Embedding:      f.embedding,
		})
	}

	memories, err := s.ListMemories(ctx)
	if err != nil {
		t.Fatalf("ListMemories: %v", err)
	}
	if len(memories) != len(fixtures) {
		t.Fatalf("expected %d memories, got %d", len(fixtures), len(memories))
	}

	// Score every memory against the query and sort by descending similarity,
	// mirroring the top-k ranking RecallMemories performs.
	type scored struct {
		title string
		score float32
	}
	ranked := make([]scored, 0, len(memories))
	for _, m := range memories {
		ranked = append(ranked, scored{
			title: byConv[m.ConversationID],
			score: embed.Cosine(query, m.Embedding),
		})
	}
	sort.Slice(ranked, func(i, j int) bool { return ranked[i].score > ranked[j].score })

	if ranked[0].title != "near" {
		t.Errorf("expected 'near' to rank first, got %q (ranking: %+v)", ranked[0].title, ranked)
	}
	// The unrelated memories must score strictly below the near one.
	for _, r := range ranked[1:] {
		if r.score >= ranked[0].score {
			t.Errorf("%q (%v) should score below 'near' (%v)", r.title, r.score, ranked[0].score)
		}
	}
}
