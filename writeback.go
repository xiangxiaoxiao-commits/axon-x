package main

import (
	"log"
	"strings"
	"sync"
	"time"

	"axon/internal/db"
	"axon/internal/graph"
	"axon/internal/task"
)

// graphWriteMu serializes knowledge-graph writeback so concurrent task
// acceptances can't interleave their cache-save + graph-rebuild. Writeback is
// low-frequency (one per accepted task), so a single global lock is enough and
// avoids two goroutines rebuilding graph.json at the same time.
var graphWriteMu sync.Mutex

// writeBackTaskKnowledge distills the durable business knowledge from an accepted
// task — its rough input, the confirmed spec, and the accepted result — and
// merges it into the project's knowledge graph, so the graph grows as tasks are
// completed ("用得越多越懂"). Alias normalization in graph.Merge folds new facts
// into existing entities (e.g. facts about "支付服务" join its existing node).
//
// It is fire-and-forget: any missing precondition (no project bound, no
// OpenAI-protocol provider for extraction, nothing worth distilling) or any
// failure degrades silently. Accepting a task must always succeed regardless of
// whether writeback does.
func (a *App) writeBackTaskKnowledge(t task.Task, result string) {
	slug := strings.TrimSpace(t.ProjectSlug)
	if slug == "" {
		return // no graph bound to this task
	}
	// Distillation reuses the IndexProject model path, which needs an OpenAI-
	// protocol provider. Without one we can't extract — skip silently.
	name, ok := a.providerForProtocol("openai")
	if !ok {
		return
	}
	pc, ok := a.cfg.Provider(name)
	if !ok {
		return
	}
	prov, err := a.newProvider(pc)
	if err != nil {
		return
	}

	transcript := buildTaskTranscript(t, result)
	if strings.TrimSpace(transcript) == "" {
		return
	}
	// Reuse the same extract prompt + parse as session indexing.
	ex, err := a.extractFromText(a.ctx, prov, transcript)
	if err != nil {
		log.Printf("axon: writeback distill task %s: %v", t.ID, err)
		return
	}
	if len(ex.Entities) == 0 && len(ex.Relations) == 0 {
		return // nothing durable in this task
	}

	// Provenance: mark every distilled fact as coming from this task, using a
	// "task:" prefix so it is distinguishable from session ("<id>") and code
	// ("code:") sources when the UI resolves source titles.
	source := "task:" + t.ID
	stampObsSources(ex.Entities, source)

	// Make the new entities semantically searchable (best-effort, same as index).
	if emb, embErr := a.newEmbedder(); embErr == nil {
		a.embedEntities(emb, ex.Entities)
	}

	dataDir, err := db.AppDataDir()
	if err != nil {
		return
	}

	// Serialize graph writes. Persist the distilled knowledge as a cache entry
	// keyed by this task, then rebuild graph.json from all caches so the new
	// knowledge is alias-merged and immediately recallable/visible. The cache
	// entry is what makes writeback durable: assembleGraph rebuilds graph.json
	// from the cache on every recall, so a direct graph.json write would be
	// overwritten on the next MatchKnowledge. This mirrors IndexProject exactly.
	graphWriteMu.Lock()
	defer graphWriteMu.Unlock()

	if err := graph.SaveCache(dataDir, slug, &graph.SessionCache{
		SessionID: source,
		Mtime:     time.Now().UnixMilli(),
		Entities:  ex.Entities,
		Relations: ex.Relations,
	}); err != nil {
		log.Printf("axon: writeback save cache task %s: %v", t.ID, err)
		return
	}
	if _, err := a.assembleGraph(slug, ""); err != nil {
		log.Printf("axon: writeback rebuild graph %s: %v", slug, err)
		return
	}

	// Let the frontend know the graph grew from an accepted task.
	a.emit(EventGraphDone, map[string]any{
		"projectSlug": slug, "processed": 1, "phase": "writeback",
	})
}

// latestRunResult returns the most recent run's result for a task, or "" on any
// error or when there are no runs. Best-effort: writeback still proceeds with
// spec-only context if the result can't be loaded.
func latestRunResult(store task.Store, taskID string) string {
	runs, err := store.ListRuns(taskID)
	if err != nil || len(runs) == 0 {
		return ""
	}
	return runs[len(runs)-1].Result
}

// buildTaskTranscript assembles the text distilled on writeback: the rough
// input, the confirmed spec (goal/background/constraints/scope/accept criteria/
// steps) and the accepted result. Together they say "what was done, which
// business/modules it touched, and what decisions and constraints applied".
func buildTaskTranscript(t task.Task, result string) string {
	var b strings.Builder
	if s := strings.TrimSpace(t.Input); s != "" {
		b.WriteString("任务输入:\n")
		b.WriteString(s)
		b.WriteString("\n\n")
	}
	if spec := strings.TrimSpace(renderSpec(t.Spec)); spec != "" {
		b.WriteString("任务规格:\n")
		b.WriteString(spec)
		b.WriteString("\n\n")
	}
	if r := strings.TrimSpace(result); r != "" {
		b.WriteString("最终采纳的产出:\n")
		b.WriteString(r)
		b.WriteString("\n")
	}
	return b.String()
}
