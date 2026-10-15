package graph

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
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

// TestMigrateStaleCachesUpgradesOrphanedCache covers the gap IndexProject cannot
// reach: a cache whose source session no longer exists still feeds every
// assembled graph, so it must be upgradeable without rediscovering a session.
func TestMigrateStaleCachesUpgradesOrphanedCache(t *testing.T) {
	dir := t.TempDir()
	// A cache exactly as an older version wrote it: schema 2, relations with no
	// source and labels that predate the relation schema.
	old := &SessionCache{
		SessionID: "orphan-session",
		Mtime:     100,
		Schema:    2,
		Entities: []Entity{
			{Name: "支付服务", Observations: []string{"幂等键用订单号"}},
		},
		Relations: []Relation{
			{From: "支付服务", To: "订单库", Label: "包含字段"},
			{From: "支付服务", To: "订单库", Label: "依赖"},
		},
	}
	if err := SaveCache(dir, "proj", old); err != nil {
		t.Fatalf("SaveCache: %v", err)
	}

	upgraded, err := MigrateStaleCaches(dir, "proj", time.UnixMilli(500))
	if err != nil {
		t.Fatalf("MigrateStaleCaches: %v", err)
	}
	if upgraded != 1 {
		t.Fatalf("upgraded = %d, want 1", upgraded)
	}

	got, ok := LoadCacheRaw(dir, "proj", "orphan-session")
	if !ok {
		t.Fatal("cache vanished after migration")
	}
	if got.Schema != CacheSchema {
		t.Errorf("schema = %d, want %d", got.Schema, CacheSchema)
	}
	if len(got.Relations) != 2 {
		t.Fatalf("relations = %d, want 2", len(got.Relations))
	}
	byLabel := map[string]Relation{}
	for _, r := range got.Relations {
		byLabel[r.Label] = r
		if len(r.Sources) != 1 || r.Sources[0] != "orphan-session" {
			t.Errorf("relation %q sources = %v, want [orphan-session]", r.Label, r.Sources)
		}
	}
	folded, ok := byLabel["包含"]
	if !ok {
		t.Fatalf("alias label was not folded onto the schema: %+v", got.Relations)
	}
	// Folding must preserve what the model actually said.
	if len(folded.Evidence) != 1 || folded.Evidence[0] != "包含字段" {
		t.Errorf("original label was not kept as evidence: %+v", folded.Evidence)
	}
	// The already-canonical edge keeps its label with no invented evidence.
	if len(byLabel["依赖"].Evidence) != 0 {
		t.Errorf("canonical label gained spurious evidence: %+v", byLabel["依赖"].Evidence)
	}
	// Entity assertions are stamped so they stop being sourceless.
	if len(got.Entities) != 1 || len(got.Entities[0].Assertions) != 1 {
		t.Fatalf("entity assertions not normalized: %+v", got.Entities)
	}
	if src := got.Entities[0].Assertions[0].Sources; len(src) != 1 || src[0] != "orphan-session" {
		t.Errorf("assertion sources = %v, want [orphan-session]", src)
	}

	// Idempotent: a second pass finds nothing stale and adds no duplicates.
	again, err := MigrateStaleCaches(dir, "proj", time.UnixMilli(600))
	if err != nil {
		t.Fatalf("second MigrateStaleCaches: %v", err)
	}
	if again != 0 {
		t.Errorf("second pass upgraded %d caches, want 0", again)
	}
	reloaded, _ := LoadCacheRaw(dir, "proj", "orphan-session")
	for _, r := range reloaded.Relations {
		if len(r.Sources) != 1 {
			t.Errorf("migration duplicated sources on second pass: %+v", r.Sources)
		}
		if len(r.Evidence) > 1 {
			t.Errorf("migration duplicated evidence on second pass: %+v", r.Evidence)
		}
	}
}

func TestMigrateStaleCachesSkipsCurrentSchema(t *testing.T) {
	dir := t.TempDir()
	current := &SessionCache{
		SessionID: "fresh", Mtime: 1, Schema: CacheSchema,
		Relations: []Relation{{From: "A", To: "B", Label: "依赖", Sources: []string{"fresh"}}},
	}
	if err := SaveCache(dir, "proj", current); err != nil {
		t.Fatalf("SaveCache: %v", err)
	}
	upgraded, err := MigrateStaleCaches(dir, "proj", time.UnixMilli(500))
	if err != nil {
		t.Fatalf("MigrateStaleCaches: %v", err)
	}
	if upgraded != 0 {
		t.Errorf("a current-schema cache was rewritten (upgraded = %d)", upgraded)
	}
}

// TestMigrateStaleCachesRewritesTheFileItRead guards against a duplicate-writing
// bug: SaveCache derives the file name from SessionID via sanitizeFileName, but
// real caches exist under names that sanitizing would rewrite ("mcp:1234.json"
// -> "mcp_1234.json"). Rewriting such a cache through SaveCache would leave the
// stale original in place AND add a second copy of its knowledge to every later
// graph assembly, doubling those relations.
func TestMigrateStaleCachesRewritesTheFileItRead(t *testing.T) {
	dir := t.TempDir()
	proj := filepath.Join(dir, "graphcache", "proj")
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	legacy := filepath.Join(proj, "mcp:1234-abcd.json")
	raw, err := json.Marshal(&SessionCache{
		SessionID: "mcp:1234-abcd", Mtime: 1, Schema: 2,
		Relations: []Relation{{From: "A", To: "B", Label: "部署到"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(legacy, raw, 0o644); err != nil {
		t.Fatal(err)
	}

	upgraded, err := MigrateStaleCaches(dir, "proj", time.UnixMilli(9))
	if err != nil {
		t.Fatalf("MigrateStaleCaches: %v", err)
	}
	if upgraded != 1 {
		t.Fatalf("upgraded = %d, want 1", upgraded)
	}

	entries, err := os.ReadDir(proj)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Fatalf("migration created duplicate cache files: %v", names)
	}

	written, err := os.ReadFile(legacy)
	if err != nil {
		t.Fatalf("original file was not rewritten in place: %v", err)
	}
	var got SessionCache
	if err := json.Unmarshal(written, &got); err != nil {
		t.Fatal(err)
	}
	if got.Schema != CacheSchema {
		t.Errorf("schema = %d, want %d", got.Schema, CacheSchema)
	}
	if len(got.Relations) != 1 || got.Relations[0].Label != "部署于" {
		t.Errorf("label not folded: %+v", got.Relations)
	}
	if len(got.Relations[0].Sources) != 1 {
		t.Errorf("sources not stamped: %+v", got.Relations[0].Sources)
	}
}

func TestMigrateAllStaleCachesCoversEveryNamespace(t *testing.T) {
	dir := t.TempDir()
	for _, slug := range []string{"alpha", "beta"} {
		if err := SaveCache(dir, slug, &SessionCache{
			SessionID: "s-" + slug, Mtime: 1, Schema: 2,
			Relations: []Relation{{From: "A", To: "B", Label: "部署到"}},
		}); err != nil {
			t.Fatalf("SaveCache(%s): %v", slug, err)
		}
	}
	total, perNamespace := MigrateAllStaleCaches(dir, time.UnixMilli(500))
	if total != 2 || perNamespace["alpha"] != 1 || perNamespace["beta"] != 1 {
		t.Fatalf("total=%d perNamespace=%v, want 2 across alpha+beta", total, perNamespace)
	}
	// Migrating one namespace must not have touched the other's data.
	got, ok := LoadCacheRaw(dir, "beta", "s-beta")
	if !ok || got.Relations[0].Label != "部署于" {
		t.Fatalf("beta not migrated correctly: %+v", got)
	}
}
