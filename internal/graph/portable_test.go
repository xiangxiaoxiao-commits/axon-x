package graph

import (
	"encoding/json"
	"strings"
	"testing"

	"axon/internal/sanitize"
)

// sampleGraph mixes portable business knowledge with the author's private
// context: absolute paths on two operating systems, a credential, a session id
// and provenance.
func sampleGraph() *Graph {
	return &Graph{
		ProjectSlug: "wanlian",
		Entities: []Entity{
			{
				Name:         "mono-glite",
				Type:         "module",
				Aliases:      []string{"glite", "/Users/alice/projects/mono-glite"},
				Embedding:    []float32{0.1, 0.2, 0.3},
				ObsSources:   []string{"7f8a9b0c-1d2e-4f3a-8b4c-5d6e7f8a9b0c"},
				Observations: []string{"连接器后端仓库，代码在 /Users/alice/projects/mono-glite"},
				Assertions: []Assertion{
					{
						Text:       "代码在 /Users/alice/projects/mono-glite，用 sk-ant-api03-AbCdEf0123456789 调模型",
						State:      RelationAsserted,
						Confidence: 0.9,
						Sources:    []string{"7f8a9b0c-1d2e-4f3a-8b4c-5d6e7f8a9b0c"},
						ObservedAt: 1786602946922,
					},
					{Text: "交付走 D:\\work\\glite-ui\\src\\index.ts 这条路径", State: RelationAsserted, Confidence: 0.8},
					{Text: "约束：跨域交付单必须带租户标识", State: RelationVerified, Confidence: 1},
				},
			},
			{
				Name:         "交付单",
				Type:         "concept",
				Observations: []string{"需求方提的交付请求", "由 user_id = 882311 创建"},
			},
			{
				// Nothing but a local path: contributes no shareable knowledge.
				Name:         "/Users/alice/scratch/tmp",
				Type:         "module",
				Observations: []string{"/Users/alice/scratch/tmp"},
			},
		},
		Relations: []Relation{
			{
				From: "交付单", To: "mono-glite", Label: "由…实现",
				Sources:  []string{"7f8a9b0c-1d2e-4f3a-8b4c-5d6e7f8a9b0c"},
				Evidence: []string{"见 /Users/alice/projects/mono-glite 的实现"},
			},
			{From: "交付单", To: "/Users/alice/scratch/tmp", Label: "示例"},
			{From: "mono-glite", To: "/Users/alice/projects/mono-glite", Label: "别名指向"},
		},
		SourceSessions: []string{"7f8a9b0c-1d2e-4f3a-8b4c-5d6e7f8a9b0c"},
	}
}

func buildSample(t *testing.T, level sanitize.Level) (*Graph, *Bundle) {
	t.Helper()
	src := sampleGraph()
	b, err := BuildBundle(src, BuildOptions{Title: "wanlian", Level: level, Home: "/Users/alice"})
	if err != nil {
		t.Fatal(err)
	}
	return src, b
}

// The author's graph must come out of an export exactly as it went in. A
// redaction that reached back into the source would silently damage the very
// knowledge the export was made from.
func TestBuildBundleLeavesTheSourceGraphUntouched(t *testing.T) {
	src := sampleGraph()
	before, err := json.Marshal(src)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := BuildBundle(src, BuildOptions{Title: "wanlian", Level: sanitize.LevelStrict, Home: "/Users/alice"}); err != nil {
		t.Fatal(err)
	}
	after, err := json.Marshal(src)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatalf("export mutated the source graph:\nbefore %s\nafter  %s", before, after)
	}
}

func TestBundleCarriesNoPersonalContext(t *testing.T) {
	_, b := buildSample(t, sanitize.LevelStandard)
	raw, err := b.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	for _, leak := range []string{
		"/Users/alice",
		"D:\\work",
		"sk-ant-api03-AbCdEf0123456789",
		"7f8a9b0c-1d2e-4f3a-8b4c-5d6e7f8a9b0c",
		"882311",
		"alice",
	} {
		if strings.Contains(text, leak) {
			t.Errorf("bundle leaks %q", leak)
		}
	}
	// Provenance fields must be absent entirely, not merely emptied.
	if strings.Contains(text, "obsSources") || strings.Contains(text, "sourceSessions") {
		t.Errorf("bundle still carries provenance fields: %s", text)
	}
	if b.Stats.OmittedEmbeddings == 0 || b.Stats.OmittedProvenance == 0 {
		t.Errorf("stats did not record omissions: %+v", b.Stats)
	}
}

func TestBundleKeepsBusinessKnowledge(t *testing.T) {
	_, b := buildSample(t, sanitize.LevelStandard)
	byName := map[string]Entity{}
	for _, e := range b.Entities {
		byName[e.Name] = e
	}
	delivery, ok := byName["交付单"]
	if !ok {
		t.Fatalf("business entity 交付单 missing, got %v", keysOf(byName))
	}
	if len(delivery.Assertions) == 0 {
		t.Fatal("交付单 lost its facts")
	}

	// A constraint is exactly the kind of knowledge worth sharing, so it must
	// survive redaction intact.
	var found bool
	for _, e := range b.Entities {
		for _, a := range e.Assertions {
			if a.Text == "约束：跨域交付单必须带租户标识" {
				found = true
				if a.State != RelationVerified || a.Confidence != 1 {
					t.Errorf("governance lost: state=%q confidence=%v", a.State, a.Confidence)
				}
			}
		}
	}
	if !found {
		t.Fatal("the verified constraint did not survive the export")
	}

	// Paths keep their last element, which is where the meaning lives.
	var sawPathTail bool
	for _, e := range b.Entities {
		for _, a := range e.Assertions {
			if strings.Contains(a.Text, "<本地路径>/mono-glite") || strings.Contains(a.Text, "<本地路径>/index.ts") {
				sawPathTail = true
			}
		}
	}
	if !sawPathTail {
		t.Fatal("path placeholder lost the trailing directory/file name")
	}
}

// Redaction renames rather than deletes, so a path-only entity keeps a
// placeholder name instead of vanishing. What must not survive is an edge whose
// two ends collapsed onto the same node.
func TestRedactionKeepsNodesButDropsSelfLoops(t *testing.T) {
	_, b := buildSample(t, sanitize.LevelStandard)
	for _, r := range b.Relations {
		if r.From == "" || r.To == "" {
			t.Errorf("relation kept an empty endpoint: %+v", r)
		}
		if normKey(r.From) == normKey(r.To) {
			t.Errorf("relation became a self-loop: %+v", r)
		}
	}
	if b.Stats.DroppedRelations == 0 {
		t.Errorf("stats did not record the dropped self-loop: %+v", b.Stats)
	}
	for _, e := range b.Entities {
		if strings.Contains(e.Name, "alice") {
			t.Errorf("entity name still identifies the author: %q", e.Name)
		}
	}
}

// Malformed or whitespace-only knowledge is dropped rather than shipped as a
// nameless node, and the counters say so.
func TestEmptyKnowledgeIsCountedAsDropped(t *testing.T) {
	src := &Graph{
		ProjectSlug: "x",
		Entities: []Entity{
			{Name: "   ", Observations: []string{"无关"}},
			{Name: "有效实体", Observations: []string{"  ", "有内容的事实"}},
		},
	}
	b, err := BuildBundle(src, BuildOptions{Title: "x", Home: "/Users/alice"})
	if err != nil {
		t.Fatal(err)
	}
	if b.Stats.DroppedEntities != 1 {
		t.Errorf("DroppedEntities = %d, want 1", b.Stats.DroppedEntities)
	}
	if b.Stats.DroppedAssertions != 1 {
		t.Errorf("DroppedAssertions = %d, want 1", b.Stats.DroppedAssertions)
	}
	if len(b.Entities) != 1 || b.Entities[0].Name != "有效实体" {
		t.Fatalf("entities = %+v, want only the valid one", b.Entities)
	}
}

// Every relation endpoint must name an entity that is actually in the bundle,
// or the recipient gets a graph with dangling edges.
func TestEveryRelationEndpointResolvesWithinTheBundle(t *testing.T) {
	_, b := buildSample(t, sanitize.LevelStrict)
	known := make(map[string]bool, len(b.Entities))
	for _, e := range b.Entities {
		known[normKey(e.Name)] = true
		for _, al := range e.Aliases {
			known[normKey(al)] = true
		}
	}
	if len(b.Relations) == 0 {
		t.Fatal("no relations survived, so this test proves nothing")
	}
	for _, r := range b.Relations {
		if !known[normKey(r.From)] {
			t.Errorf("relation %+v points at unknown entity %q", r, r.From)
		}
		if !known[normKey(r.To)] {
			t.Errorf("relation %+v points at unknown entity %q", r, r.To)
		}
	}
}

// Cross-platform: a graph authored on either OS must clean up the other's path
// syntax, because knowledge travels between a Mac and a Windows machine.
func TestScrubbingIsPlatformAgnostic(t *testing.T) {
	src := &Graph{
		ProjectSlug: "x",
		Entities: []Entity{{
			Name: "构建",
			Observations: []string{
				"Mac 上在 /Users/alice/proj 构建",
				`Windows 上在 D:\work\proj 构建`,
				`UNC 上在 \\buildhost\share\proj 构建`,
			},
		}},
	}
	b, err := BuildBundle(src, BuildOptions{Title: "x", Home: "/Users/alice"})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := b.Marshal()
	for _, leak := range []string{"/Users/alice", `D:\work`, `\\buildhost`} {
		if strings.Contains(string(raw), leak) {
			t.Errorf("bundle leaks %q regardless of the authoring OS", leak)
		}
	}
}

func TestBundleRoundTripIsStableAcrossPlatforms(t *testing.T) {
	_, b := buildSample(t, sanitize.LevelStandard)
	raw, err := b.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	// Simulate a Windows file: CRLF line endings plus an editor-added BOM.
	win := append([]byte{0xEF, 0xBB, 0xBF}, []byte(strings.ReplaceAll(string(raw), "\n", "\r\n"))...)
	parsed, err := ParseBundle(win)
	if err != nil {
		t.Fatalf("ParseBundle rejected a BOM/CRLF file: %v", err)
	}
	if parsed.Digest != b.Digest {
		t.Fatalf("digest changed across platforms: %q vs %q", parsed.Digest, b.Digest)
	}
	if parsed.SessionID() != b.SessionID() {
		t.Fatalf("session id changed across platforms: %q vs %q", parsed.SessionID(), b.SessionID())
	}
}

func TestParseBundleRejectsForeignAndNewerFiles(t *testing.T) {
	if _, err := ParseBundle([]byte(`{"format":"something.else"}`)); err == nil {
		t.Fatal("ParseBundle accepted a foreign file")
	}
	if _, err := ParseBundle([]byte(`{"format":"` + BundleFormat + `","version":999}`)); err == nil {
		t.Fatal("ParseBundle accepted a newer schema version")
	}
	if _, err := ParseBundle([]byte("not json")); err == nil {
		t.Fatal("ParseBundle accepted garbage")
	}
}

func TestCacheCarriesKnowledgeAndNoChunks(t *testing.T) {
	_, b := buildSample(t, sanitize.LevelStandard)
	c := b.Cache()
	if len(c.Chunks) != 0 {
		t.Errorf("imported cache carries %d chunks; a bundle must ship no verbatim text", len(c.Chunks))
	}
	if c.SessionID != "import:"+b.Digest {
		t.Errorf("SessionID = %q, want import:<digest>", c.SessionID)
	}
	if c.Schema != CacheSchema {
		t.Errorf("Schema = %d, want %d", c.Schema, CacheSchema)
	}
	if len(c.Entities) != len(b.Entities) {
		t.Errorf("cache entities = %d, want %d", len(c.Entities), len(b.Entities))
	}
	// The cache is written through svg... through SaveCache, which sanitises the
	// file name; the ":" must not survive into a Windows path component.
	if got := sanitizeFileName(c.SessionID); strings.ContainsAny(got, `:<>|"?*\`) {
		t.Errorf("sanitized cache name %q still holds characters Windows rejects", got)
	}
}

// An import deliberately drops the author's sources so their machine's session
// ids never travel (ImportBundle clears them before calling Cache). That must not
// leave imported knowledge unattributable — ImportSourcePrefix exists precisely
// so imported facts stay traceable to the pack that brought them in. The stamp
// must also not re-grade the facts the pack shipped.
func TestImportedCacheStampsItsOwnProvenance(t *testing.T) {
	_, b := buildSample(t, sanitize.LevelStandard)
	// Reproduce the post-scrub state: the author's sources are gone.
	for i := range b.Entities {
		for j := range b.Entities[i].Assertions {
			b.Entities[i].Assertions[j].Sources = nil
		}
		b.Entities[i].ObsSources = nil
	}
	for i := range b.Relations {
		b.Relations[i].Sources = nil
	}
	c := b.Cache()
	want := b.SessionID()
	if !strings.HasPrefix(want, ImportSourcePrefix) {
		t.Fatalf("SessionID = %q, want the %q prefix", want, ImportSourcePrefix)
	}

	if len(c.Relations) == 0 {
		t.Fatal("sample bundle carried no relations to check")
	}
	for _, r := range c.Relations {
		if len(r.Sources) != 1 || r.Sources[0] != want {
			t.Fatalf("imported relation sources = %v, want [%s]", r.Sources, want)
		}
	}
	stamped := 0
	for _, e := range c.Entities {
		for _, a := range e.Assertions {
			if len(a.Sources) == 1 && a.Sources[0] == want {
				stamped++
			}
		}
	}
	if stamped == 0 {
		t.Error("no imported assertion carried the bundle's provenance")
	}

	// A verified, high-confidence fact must survive with its grade intact: the
	// import attaches a source, it does not re-rate the author's evidence.
	graded := &Bundle{
		Format: BundleFormat, Version: BundleVersion, Digest: "deadbeefcafe0000",
		Entities: []Entity{{Name: "A", Assertions: []Assertion{{
			Text: "代码确认的事实", State: RelationVerified, Confidence: 1,
		}}}},
		Relations: []Relation{{From: "A", To: "A", Label: "依赖",
			State: RelationVerified, Confidence: 1}},
	}
	gc := graded.Cache()
	got := gc.Entities[0].Assertions[0]
	if got.State != RelationVerified || got.Confidence != 1 {
		t.Errorf("import re-graded a fact: state=%q confidence=%v", got.State, got.Confidence)
	}
	if gc.Relations[0].State != RelationVerified || gc.Relations[0].Confidence != 1 {
		t.Errorf("import re-graded a relation: %+v", gc.Relations[0])
	}
}

func TestSafeFileComponentHandlesWindowsRules(t *testing.T) {
	cases := []struct{ in, want string }{
		{"wanlian", "wanlian"},
		{"wanlian:3.2.1", "wanlian_3.2.1"},
		{"trailing. ", "trailing"},
		{"a<b>c|d?e*f", "a_b_c_d_e_f"},
		{"CON", "CON_"},
		{"  ", ""},
		{"子/目录", "子-目录"},
	}
	for _, c := range cases {
		if got := SafeFileComponent(c.in); got != c.want {
			t.Errorf("SafeFileComponent(%q) = %q, want %q", c.in, got, c.want)
		}
	}
	if got := PortableFileName("wanlian:3.2"); got != "wanlian_3.2.axon-graph.json" {
		t.Errorf("PortableFileName() = %q", got)
	}
}

func TestStrictLevelFlipsMoreRedactions(t *testing.T) {
	src := sampleGraph()
	src.Entities[1].Assertions = []Assertion{{Text: "需求单 PLJI-2928 的验收在 glite-gateway:10443 上做"}}
	standard, err := BuildBundle(src, BuildOptions{Title: "w", Level: sanitize.LevelStandard, Home: "/Users/alice"})
	if err != nil {
		t.Fatal(err)
	}
	strict, err := BuildBundle(src, BuildOptions{Title: "w", Level: sanitize.LevelStrict, Home: "/Users/alice"})
	if err != nil {
		t.Fatal(err)
	}
	hasTicket := func(b *Bundle) bool {
		raw, _ := b.Marshal()
		return strings.Contains(string(raw), "PLJI-2928")
	}
	if !hasTicket(standard) {
		t.Error("standard level removed a ticket id, which is business context and should be kept")
	}
	if hasTicket(strict) {
		t.Error("strict level kept a ticket id")
	}
	if strict.Stats.Entities == 0 {
		t.Error("strict export produced nothing")
	}
}

func keysOf(m map[string]Entity) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
