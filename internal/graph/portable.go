package graph

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"axon/internal/sanitize"
)

// Portable knowledge bundles are how a curated graph travels between people.
//
// Two properties matter more than anything else here:
//
//  1. The export is derived, never destructive. BuildBundle copies what it needs
//     and leaves the source Graph untouched, so sharing knowledge can never cost
//     the author their own graph.
//  2. What is omitted is as important as what is kept. A bundle carries only the
//     knowledge a colleague needs — entities, their facts, and the relations
//     between them. Verbatim conversation chunks, embeddings, session ids,
//     review history and exclusion records describe the author's machine and
//     their private working sessions, so they never leave it.
const (
	// BundleFormat identifies the file so an import can refuse anything else.
	BundleFormat = "axon.knowledge.bundle"
	// BundleVersion is the schema version of the bundle payload.
	BundleVersion = 1
	// ImportSourcePrefix marks knowledge that came in through a bundle, kept on
	// assertions so imported facts stay attributable and removable.
	ImportSourcePrefix = "import:"
)

// BundleStats counts what a bundle carries and what building it discarded.
type BundleStats struct {
	Entities     int `json:"entities"`
	Relations    int `json:"relations"`
	Observations int `json:"observations"`
	// ConsideredEntities is how many entities the source graph had, so a
	// recipient can see how much the scrubber removed.
	ConsideredEntities int `json:"consideredEntities,omitempty"`
	// Omitted* count fields dropped for privacy rather than for size.
	OmittedChunks     int `json:"omittedChunks,omitempty"`
	OmittedEmbeddings int `json:"omittedEmbeddings,omitempty"`
	OmittedProvenance int `json:"omittedProvenance,omitempty"`
	OmittedReviews    int `json:"omittedReviews,omitempty"`
	// Dropped* and *AfterScrub count knowledge lost to redaction: edges whose
	// endpoints lost their name, facts that were entirely personal, entities that
	// collided once their paths were stripped.
	DroppedRelations  int `json:"droppedRelations,omitempty"`
	DroppedAssertions int `json:"droppedAssertions,omitempty"`
	DroppedEntities   int `json:"droppedEntities,omitempty"`
	MergedAfterScrub  int `json:"mergedAfterScrub,omitempty"`
}

// Bundle is the portable, scrubbed form of a knowledge graph. It is deliberately
// self-describing: an importer can show its origin, size and redaction summary
// before a single fact is written.
type Bundle struct {
	Format     string         `json:"format"`
	Version    int            `json:"version"`
	Title      string         `json:"title,omitempty"` // human label, usually the namespace
	ExportedAt int64          `json:"exportedAt"`      // unix millis
	ScrubLevel string         `json:"scrubLevel"`      // standard | strict
	Stats      BundleStats    `json:"stats"`
	Redactions []sanitize.Hit `json:"redactions,omitempty"` // per-rule counts and samples
	Entities   []Entity       `json:"entities"`
	Relations  []Relation     `json:"relations"`
	Notes      []string       `json:"notes,omitempty"` // what this file deliberately leaves out
	// Digest covers the knowledge payload; it names the cache file an import
	// writes, so re-importing the same bundle replaces rather than duplicates.
	Digest string `json:"digest,omitempty"`
}

// BuildOptions configures one export.
type BuildOptions struct {
	// Title labels the bundle for the recipient. The namespace name is the
	// natural choice; it is business vocabulary, not personal data.
	Title string
	Level sanitize.Level
	// Terms and Home are forwarded to the scrubber. Home defaults to the real
	// home directory; it is only used to learn the login name to redact.
	Terms []string
	Home  string
	// OmittedChunks and OmittedReviews are supplied by the caller, which can see
	// the session caches and the review log that a Graph itself does not carry.
	OmittedChunks  int
	OmittedReviews int
}

// BuildBundle copies a graph into a scrubbed, portable bundle.
//
// The source graph is only read. Everything downstream works on copies, so a
// failed or partial export cannot corrupt the graph the author keeps using.
func BuildBundle(src *Graph, opts BuildOptions) (*Bundle, error) {
	if src == nil {
		return nil, fmt.Errorf("build bundle: no source graph")
	}
	level := opts.Level
	if level == "" {
		level = sanitize.LevelStandard
	}
	scrubber := sanitize.New(sanitize.Options{Level: level, Home: opts.Home, Terms: opts.Terms})

	stats := BundleStats{ConsideredEntities: len(src.Entities)}

	// --- Entities: keep the name, type, aliases and facts; drop the rest. ---
	entities := make([]Entity, 0, len(src.Entities))
	// scrubbedName maps the original name onto its redacted form so relation
	// endpoints can be rewritten to match. A redaction that renames an entity
	// must not leave edges pointing at the old name.
	scrubbedName := make(map[string]string, len(src.Entities))
	type pending struct {
		entity Entity
		key    string
	}
	kept := make([]pending, 0, len(src.Entities))

	for _, e := range src.Entities {
		// Work on a copy: the caller's graph must come out of this unchanged.
		c := e
		c.Name = strings.TrimSpace(scrubber.Text(c.Name))
		if c.Name == "" {
			// The name was nothing but personal detail (a bare local path, say).
			stats.DroppedEntities++
			continue
		}
		c.Aliases = scrubber.Strings(c.Aliases)

		if len(c.Embedding) > 0 {
			// A vector is derived from the author's own text; it identifies the
			// source material without being readable, so it does not travel.
			stats.OmittedEmbeddings++
			c.Embedding = nil
		}
		if len(c.ObsSources) > 0 {
			stats.OmittedProvenance += len(c.ObsSources)
			c.ObsSources = nil
		}

		assertions := copyAssertions(e)
		keptAssertions := make([]Assertion, 0, len(assertions))
		seenText := make(map[string]bool, len(assertions))
		for _, a := range assertions {
			if len(a.Sources) > 0 {
				stats.OmittedProvenance += len(a.Sources)
				a.Sources = nil
			}
			a.Text = strings.TrimSpace(scrubber.Text(a.Text))
			if a.Text == "" {
				stats.DroppedAssertions++
				continue
			}
			key := strings.ToLower(a.Text)
			if seenText[key] {
				continue // two facts collapsed into one after redaction
			}
			seenText[key] = true
			keptAssertions = append(keptAssertions, a)
		}
		if len(keptAssertions) == 0 {
			// An entity whose every fact was personal has nothing left to share.
			stats.DroppedEntities++
			continue
		}

		// Normalisation rebuilds Observations/ObsSources as the compatibility
		// projection, so the bundle stays readable by older readers without
		// ObsSources ever being populated again.
		c.Aliases = dedupeStrings(c.Aliases)
		c.Observations = nil
		c.Assertions = keptAssertions
		NormalizeEntityAssertions(&c)
		c.ObsSources = nil
		stats.Observations += len(c.Assertions)

		key := normKey(c.Name)
		kept = append(kept, pending{entity: c, key: key})
		scrubbedName[e.Name] = c.Name
		// Aliases may also be used as relation endpoints by older data.
		for _, al := range e.Aliases {
			if s := scrubber.Text(al); s != "" {
				scrubbedName[al] = s
			}
		}
	}

	// Redaction can make two names identical ("<本地路径>/x.go" from two
	// different directories). Merge those rather than emitting duplicate nodes.
	byName := make(map[string]int, len(kept))
	for _, k := range kept {
		if i, ok := byName[k.key]; ok {
			stats.MergedAfterScrub++
			absorb(&entities[i], k.entity)
			continue
		}
		byName[k.key] = len(entities)
		entities = append(entities, k.entity)
	}

	// --- Relations: drop provenance, rewrite endpoints, drop the unlinked. ---
	relations := make([]Relation, 0, len(src.Relations))
	for _, r := range src.Relations {
		c := r
		if len(c.Sources) > 0 {
			stats.OmittedProvenance += len(c.Sources)
			c.Sources = nil
		}
		from, okFrom := resolveEndpoint(c.From, scrubbedName, entities)
		to, okTo := resolveEndpoint(c.To, scrubbedName, entities)
		if !okFrom || !okTo || normKey(from) == normKey(to) {
			// Either endpoint lost the text that named it, or the edge became a
			// self-loop once the paths collapsed. Neither is worth shipping.
			stats.DroppedRelations++
			continue
		}
		c.From, c.To = from, to
		c.Label = strings.TrimSpace(scrubber.Text(c.Label))
		c.Evidence = scrubber.Strings(c.Evidence)
		relations = append(relations, c)
	}

	stats.OmittedChunks = opts.OmittedChunks
	stats.OmittedReviews = opts.OmittedReviews

	b := &Bundle{
		Format:     BundleFormat,
		Version:    BundleVersion,
		Title:      strings.TrimSpace(scrubber.Text(opts.Title)),
		ExportedAt: time.Now().UnixMilli(),
		ScrubLevel: string(level),
		Stats:      stats,
		// Shareable() strips the pre-redaction samples: the report travels with
		// the file, so it must not carry back what it just removed.
		Redactions: scrubber.Report().Shareable().Hits,
		Entities:   entities,
		Relations:  relations,
		Notes:      bundleNotes(),
	}
	b.Stats.Entities = len(b.Entities)
	b.Stats.Relations = len(b.Relations)
	b.Digest = b.computeDigest()
	return b, nil
}

// bundleNotes documents, inside the file itself, what a bundle deliberately
// leaves out — so a recipient reading the JSON knows the omissions are by
// design rather than a broken export.
func bundleNotes() []string {
	return []string{
		"只包含知识本体（实体、事实、关系），不含原始对话、向量、来源会话 ID、审核与剔除历史",
		"已按剪除级别去除本地路径、凭据、账号 ID 等个人信息；<本地路径>、<ID>、<已脱敏> 等是占位符",
		"与本机路径、操作系统无关：路径占位符只保留最后一级名字，Mac 与 Windows 之间可互相导入",
	}
}

// absorb folds a collided entity into an existing one after redaction. It is the
// bundle-local equivalent of Graph.Merge for a single node: union the facts and
// keep both spellings discoverable.
func absorb(dst *Entity, src Entity) {
	if dst.Type == "" {
		dst.Type = src.Type
	}
	dst.Aliases = dedupeStrings(append(append([]string(nil), dst.Aliases...), src.Aliases...))
	dst.Assertions = append(dst.Assertions, src.Assertions...)
	NormalizeEntityAssertions(dst)
	dst.ObsSources = nil
	dst.Embedding = nil
}

// resolveEndpoint maps a relation endpoint onto a name that exists in the
// bundle. Endpoints may be spelled with a pre-redaction name (or an alias), so
// the redaction mapping is consulted as a fallback.
func resolveEndpoint(name string, scrubbed map[string]string, entities []Entity) (string, bool) {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return "", false
	}
	if canonical, ok := findEntityName(entities, trimmed); ok {
		return canonical, true
	}
	if mapped, ok := scrubbed[name]; ok && mapped != "" {
		return findEntityName(entities, mapped)
	}
	return "", false
}

// findEntityName locates the bundle entity addressed by a name or alias.
func findEntityName(entities []Entity, name string) (string, bool) {
	key := normKey(name)
	if key == "" {
		return "", false
	}
	for i := range entities {
		if normKey(entities[i].Name) == key {
			return entities[i].Name, true
		}
		for _, al := range entities[i].Aliases {
			if normKey(al) == key {
				return entities[i].Name, true
			}
		}
	}
	return "", false
}

// copyAssertions reads an entity's governed facts without touching the original.
// Entities are copied by value, but their slices share the backing array, so the
// assertions are copied element-wise before any field is rewritten.
func copyAssertions(e Entity) []Assertion {
	out := make([]Assertion, 0, len(e.Assertions)+len(e.Observations))
	out = append(out, e.Assertions...)
	if len(e.Assertions) == 0 {
		for i, text := range e.Observations {
			a := Assertion{Text: text, State: RelationAsserted, Confidence: 0.85}
			if i < len(e.ObsSources) && strings.TrimSpace(e.ObsSources[i]) != "" {
				a.Sources = []string{e.ObsSources[i]}
			}
			out = append(out, a)
		}
	}
	return out
}

func dedupeStrings(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" || seen[strings.ToLower(s)] {
			continue
		}
		seen[strings.ToLower(s)] = true
		out = append(out, s)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// computeDigest hashes the knowledge payload. The digest names the cache file an
// import writes, which makes re-importing the same file idempotent while
// leaving other imports (and all locally distilled knowledge) untouched.
func (b *Bundle) computeDigest() string {
	type payload struct {
		Entities  []Entity   `json:"entities"`
		Relations []Relation `json:"relations"`
	}
	data, err := json.Marshal(payload{Entities: b.Entities, Relations: b.Relations})
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])[:16]
}

// SessionID is the cache key this bundle occupies in a namespace.
func (b *Bundle) SessionID() string {
	id := b.Digest
	if id == "" {
		// Should not happen, but a bundle without a payload still needs a stable
		// name rather than a random one.
		id = fmt.Sprintf("v%d-%d", b.Version, b.ExportedAt)
	}
	return ImportSourcePrefix + id
}

// Cache converts a bundle into the SessionCache an import writes. Chunks stay
// empty on purpose: a bundle deliberately carries no verbatim conversation.
//
// Provenance is re-stamped with the bundle's own id. ImportBundle clears the
// author's sources so their machine's session ids never travel, which otherwise
// leaves every imported fact unattributable — and the whole point of
// ImportSourcePrefix is that imported knowledge stays traceable to the pack that
// brought it in and removable as a unit. Trust state and confidence are left
// exactly as the pack recorded them: an import must not silently re-grade facts.
func (b *Bundle) Cache() *SessionCache {
	source := b.SessionID()
	entities := append([]Entity(nil), b.Entities...)
	relations := append([]Relation(nil), b.Relations...)
	// observedAt 0 and empty state/confidence mean "attach the source, change
	// nothing else" (see StampEntityAssertions).
	StampEntityAssertions(entities, source, 0, "", 0)
	StampRelationProvenance(relations, source)
	return &SessionCache{
		SessionID: source,
		Schema:    CacheSchema,
		Entities:  entities,
		Relations: relations,
	}
}

// CachePath is where a session's cache lives. Exported so an importer can
// report and remove the file it created without duplicating the naming rules.
func CachePath(dataDir, slug, sessionID string) string {
	return filepath.Join(cacheDir(dataDir, slug), sanitizeFileName(sessionID)+".json")
}

// DeleteCache removes one session's cache file, used to undo an import.
func DeleteCache(dataDir, slug, sessionID string) error {
	p := CachePath(dataDir, slug, sessionID)
	if err := os.Remove(p); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("删除缓存失败: %w", err)
	}
	return nil
}

// Marshal renders the bundle as the file that gets handed to a colleague.
//
// HTML escaping is disabled so the placeholders stay readable as "<本地路径>"
// instead of "\u003c本地路径\u003e": the file is meant to be inspectable, and a
// Chinese-speaking recipient may well open it in an editor to check what was
// stripped. Encoding is plain UTF-8 with no BOM, which every platform accepts.
func (b *Bundle) Marshal() ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(b); err != nil {
		return nil, fmt.Errorf("编码知识包失败: %w", err)
	}
	return buf.Bytes(), nil
}

// ParseBundle reads and validates a bundle file.
//
// Cross-platform tolerance is deliberate: a file written on Windows may arrive
// with CRLF line endings, and an editor there may have prepended a UTF-8 BOM.
// Both are legal to accept, and rejecting them would look like a corrupt file.
func ParseBundle(raw []byte) (*Bundle, error) {
	raw = bytes.TrimPrefix(raw, []byte{0xEF, 0xBB, 0xBF})
	var b Bundle
	if err := json.Unmarshal(raw, &b); err != nil {
		return nil, fmt.Errorf("解析知识包失败: %w", err)
	}
	if b.Format != BundleFormat {
		return nil, fmt.Errorf("不是 axon 知识包（format=%q）", b.Format)
	}
	if b.Version > BundleVersion {
		return nil, fmt.Errorf("知识包版本 %d 比当前程序支持的 %d 更新，请先升级 axon", b.Version, BundleVersion)
	}
	// A bundle could arrive from anywhere; re-derive the digest rather than
	// trusting the field, so a tampered file cannot overwrite an unrelated cache.
	b.Digest = b.computeDigest()
	return &b, nil
}

// WriteBundleFile writes a bundle atomically so an interrupted save cannot leave
// a half-written file behind.
func WriteBundleFile(path string, b *Bundle) error {
	if path == "" {
		return fmt.Errorf("write bundle: empty path")
	}
	data, err := b.Marshal()
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("write bundle: %w", err)
	}
	return os.Rename(tmp, path)
}

// ReadBundleFile loads a bundle from disk.
func ReadBundleFile(path string) (*Bundle, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("读取知识包失败: %w", err)
	}
	return ParseBundle(data)
}

// windowsReserved are device names that cannot be used as file or directory
// names on Windows, whatever the extension.
var windowsReserved = map[string]bool{
	"CON": true, "PRN": true, "AUX": true, "NUL": true,
	"COM1": true, "COM2": true, "COM3": true, "COM4": true, "COM5": true,
	"COM6": true, "COM7": true, "COM8": true, "COM9": true,
	"LPT1": true, "LPT2": true, "LPT3": true, "LPT4": true, "LPT5": true,
	"LPT6": true, "LPT7": true, "LPT8": true, "LPT9": true,
}

// SafeFileComponent makes a string usable as a single path component on both
// macOS and Windows. A namespace becomes a directory name and a bundle title
// becomes a file name, so both have to survive NTFS rules — including the
// easily-missed ones: characters like ':' are illegal, and a trailing dot or
// space makes Windows reject the name outright even though macOS allows it.
func SafeFileComponent(name string) string {
	cleaned := sanitizeFileName(strings.TrimSpace(name))
	cleaned = strings.Trim(cleaned, ". ")
	// Collapse separators and control characters that sanitizeFileName leaves be.
	cleaned = strings.Map(func(r rune) rune {
		if r < 0x20 || r == '/' {
			return '-'
		}
		return r
	}, cleaned)
	if cleaned == "" {
		return ""
	}
	if windowsReserved[strings.ToUpper(cleaned)] {
		return cleaned + "_"
	}
	return cleaned
}

// PortableFileName suggests a file name for exporting a namespace that is valid
// on both platforms.
func PortableFileName(title string) string {
	base := SafeFileComponent(title)
	if base == "" {
		base = "knowledge"
	}
	return base + ".axon-graph.json"
}

// Summary renders a one-line description for logs and reports.
func (b *Bundle) Summary() string {
	return fmt.Sprintf("%d 个实体、%d 条关系、%d 条事实",
		b.Stats.Entities, b.Stats.Relations, b.Stats.Observations)
}
