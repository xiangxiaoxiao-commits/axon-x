package graph

import (
	"testing"
)

func TestSaveLoadCache_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	c := &SessionCache{
		SessionID: "sess-1",
		Mtime:     100,
		Schema:    CacheSchema,
		Entities:  []Entity{{Name: "支付服务", Type: "service"}},
		Chunks:    []Chunk{{ID: "sess-1#0", Text: "hello", Source: "sess-1", Kind: "chat", Embedding: []float32{0.1, 0.2}}},
	}
	if err := SaveCache(dir, "proj", c); err != nil {
		t.Fatalf("SaveCache: %v", err)
	}
	got, ok := LoadCache(dir, "proj", "sess-1", 100)
	if !ok {
		t.Fatal("LoadCache returned not-ok for a fresh, current-schema cache")
	}
	if len(got.Chunks) != 1 || got.Chunks[0].Text != "hello" {
		t.Errorf("chunks not round-tripped: %+v", got.Chunks)
	}
	if len(got.Chunks[0].Embedding) != 2 {
		t.Errorf("embedding not round-tripped: %+v", got.Chunks[0].Embedding)
	}
}

func TestLoadCache_StaleOnMtimeChange(t *testing.T) {
	dir := t.TempDir()
	c := &SessionCache{SessionID: "s", Mtime: 100, Schema: CacheSchema}
	if err := SaveCache(dir, "proj", c); err != nil {
		t.Fatalf("SaveCache: %v", err)
	}
	if _, ok := LoadCache(dir, "proj", "s", 200); ok {
		t.Error("LoadCache should be stale when mtime differs")
	}
}

func TestLoadCache_StaleOnOldSchema(t *testing.T) {
	dir := t.TempDir()
	// Pre-chunk cache: Schema 0 (older than CacheSchema) must be treated as stale
	// so IndexProject reprocesses it to back-fill chunks.
	c := &SessionCache{SessionID: "s", Mtime: 100, Schema: 0}
	if err := SaveCache(dir, "proj", c); err != nil {
		t.Fatalf("SaveCache: %v", err)
	}
	if _, ok := LoadCache(dir, "proj", "s", 100); ok {
		t.Error("LoadCache should be stale when schema is older than CacheSchema")
	}
	// But LoadCacheRaw still returns it so its entities can be reused on back-fill.
	if _, ok := LoadCacheRaw(dir, "proj", "s"); !ok {
		t.Error("LoadCacheRaw should return the old-schema cache for reuse")
	}
}
