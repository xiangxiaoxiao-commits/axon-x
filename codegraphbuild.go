package main

import (
	"fmt"
	"strings"
	"time"

	"axon/internal/codegraph"
	"axon/internal/db"
	"axon/internal/graph"
	"axon/internal/provider"
)

// maxLLMFiles caps how many key files get an LLM business pass per full build,
// so cost stays bounded regardless of repo size (see docs/TECH_CODEGRAPH.md §3.2).
const maxLLMFiles = 40

// maxCodeChars truncates a single file's content sent to the model. Code is
// denser than prose, so this is smaller than the conversation transcript cap.
const maxCodeChars = 8000

// codeBizPrompt asks the model to add ONLY business meaning to already-extracted
// code symbols: a responsibility observation and business aliases (Chinese/short
// names), never restating syntax. Output is strict JSON so it merges onto the
// static skeleton (aliases fold code entities into conversation entities).
const codeBizPrompt = `你是代码理解器。下面给你一个源码文件的路径、已静态解析出的符号骨架、以及文件内容(可能截断)。
只提炼「这个文件/其中符号在业务上负责什么」的持久知识，不要复述语法结构。

对已知实体补充：
- observations: 它在业务/系统里的职责(如"负责订单支付回调校验")，一句话一条
- aliases: 它的业务别名(中文名/简称/领域术语，如 PaymentService → ["支付服务","支付"])
纯技术样板(getter/DTO/自动生成)无业务含义可跳过。name 必须与给出的骨架实体名完全一致(带包前缀的照抄)。

只输出如下 JSON，不要任何解释或代码围栏：
{"entities":[{"name":"骨架里的实体名","type":"file|package|function|type","aliases":["别名"],"observations":["业务职责"]}],"relations":[]}`

// BuildGraphFromCode builds code knowledge from a repository and merges it into
// the project's knowledge graph — a second data source alongside conversation
// distillation, sharing storage, alias normalization and embeddings.
//
// Pipeline: static skeleton (free, full) -> LLM business enrichment on the
// top-N key files (budgeted; skipped entirely when no OpenAI provider is
// configured, degrading to skeleton-only, no error) -> embeddings -> merge into
// the existing graph and save. Code-sourced observations are stamped "code:"+path
// so provenance stays distinguishable from session ids.
func (a *App) BuildGraphFromCode(repoDir, projectSlug string) error {
	if strings.TrimSpace(repoDir) == "" || strings.TrimSpace(projectSlug) == "" {
		return fmt.Errorf("repoDir 和 projectSlug 不能为空")
	}
	dataDir, err := db.AppDataDir()
	if err != nil {
		return err
	}

	// Phase B: static skeleton (deterministic, free, full-repo).
	ents, rels, err := codegraph.BuildSkeleton(repoDir)
	if err != nil {
		return fmt.Errorf("extract code skeleton: %w", err)
	}
	a.emit(EventGraphProgress, map[string]any{
		"projectSlug": projectSlug, "current": 0, "total": 0,
		"phase": "code", "title": fmt.Sprintf("已抽取 %d 个代码实体", len(ents)),
	})

	// Phase C: LLM business enrichment on key files (optional/budgeted).
	files, _ := codegraph.ListSourceFiles(repoDir)
	if prov, ok := a.codeEnrichProvider(); ok {
		keys := codegraph.SelectKeyFiles(repoDir, files, rels, maxLLMFiles)
		byName := fileSymbols(rels)
		for i, f := range keys {
			a.emit(EventGraphProgress, map[string]any{
				"projectSlug": projectSlug, "current": i + 1, "total": len(keys),
				"phase": "code", "title": f,
			})
			biz, bErr := a.extractBizFromCode(prov, repoDir, f, byName)
			if bErr != nil {
				continue // best-effort: one file failing must not abort the build
			}
			stampObsSourcesCode(biz.Entities, f)
			// Business entities carry the same names as skeleton entities, so
			// appending lets graph.Merge fold observations/aliases onto them.
			ents = append(ents, biz.Entities...)
			rels = append(rels, biz.Relations...)
		}
	}

	// Raw-context channel: chunk every source file along declaration boundaries
	// so the repository's final-state code is recallable verbatim (not just the
	// distilled skeleton). Repo code is settled, so it is not noise-filtered.
	var codeChunks []graph.Chunk
	for _, f := range files {
		codeChunks = append(codeChunks, chunkCodeFile(f, codegraph.ReadFile(repoDir, f))...)
	}

	// Embeddings for HybridRAG recall (best-effort; skipped when no embedder).
	if emb, embErr := a.newEmbedder(); embErr == nil {
		a.embedEntities(emb, ents)
		a.embedChunks(emb, codeChunks)
	}

	// Persist code chunks in a dedicated cache so loadChunks picks them up on
	// recall. Keyed "code:chunks" (not a session id), rebuilt in full on each code
	// build. Entities/relations stay empty here — they are merged into graph.json
	// directly below, as before.
	if len(codeChunks) > 0 {
		_ = graph.SaveCache(dataDir, projectSlug, &graph.SessionCache{
			SessionID: "code:chunks", Mtime: time.Now().UnixMilli(),
			Schema: graph.CacheSchema, Chunks: codeChunks,
		})
	}

	// Phase D: merge into the existing (conversation-sourced) graph and save.
	// Alias normalization fuses code entities with conversation entities.
	g, err := graph.Load(dataDir, projectSlug)
	if err != nil {
		return err
	}
	g.Merge(ents, rels)
	g.UpdatedAt = time.Now().UnixMilli()
	if err := graph.Save(dataDir, g); err != nil {
		return err
	}

	a.emit(EventGraphDone, map[string]any{
		"projectSlug": projectSlug, "processed": len(ents), "phase": "code",
	})
	return nil
}

// codeEnrichProvider resolves an OpenAI-protocol provider for the business pass,
// returning ok=false when none is configured so the caller degrades to
// skeleton-only without erroring.
func (a *App) codeEnrichProvider() (provider.Provider, bool) {
	name, ok := a.providerForProtocol("openai")
	if !ok {
		return nil, false
	}
	pc, _ := a.cfg.Provider(name)
	prov, err := a.newProvider(pc)
	if err != nil {
		return nil, false
	}
	return prov, true
}

// extractBizFromCode runs one business-enrichment call over a single file,
// feeding the model the file's path, its known skeleton symbols and the
// (truncated) content, and parsing the JSON reply with the shared parser.
func (a *App) extractBizFromCode(prov provider.Provider, repoDir, rel string, byName map[string][]string) (extracted, error) {
	content := readCodeFile(repoDir, rel)
	if strings.TrimSpace(content) == "" {
		return extracted{}, fmt.Errorf("empty or unreadable file")
	}
	var b strings.Builder
	fmt.Fprintf(&b, "文件路径: %s\n\n", rel)
	if syms := byName[rel]; len(syms) > 0 {
		b.WriteString("已解析出的符号骨架:\n")
		for _, s := range syms {
			fmt.Fprintf(&b, "- %s\n", s)
		}
		b.WriteString("\n")
	}
	b.WriteString("文件内容:\n")
	b.WriteString(content)

	reply, err := collectReply(a.ctx, prov, provider.ChatRequest{
		Model: graphModel,
		Messages: []provider.ChatMessage{
			{Role: provider.RoleSystem, Content: codeBizPrompt},
			{Role: provider.RoleUser, Content: b.String()},
		},
		Temperature: 0.1,
		MaxTokens:   2000,
	})
	if err != nil {
		return extracted{}, err
	}
	return parseExtracted(reply), nil
}

// fileSymbols maps each file to the symbol names it contains, derived from the
// "包含" (contains) relations whose source is the file, so the business prompt
// can show the model the file's skeleton.
func fileSymbols(rels []graph.Relation) map[string][]string {
	idx := map[string][]string{}
	for _, r := range rels {
		if r.Label == "包含" && strings.HasSuffix(r.From, ".go") {
			idx[r.From] = append(idx[r.From], r.To)
		}
	}
	return idx
}

// readCodeFile reads a file for the LLM pass, truncated to maxCodeChars.
func readCodeFile(repoDir, rel string) string {
	src := codegraph.ReadFile(repoDir, rel)
	if len(src) > maxCodeChars {
		src = src[:maxCodeChars] + "\n// ... (truncated)"
	}
	return src
}

// stampObsSourcesCode marks each entity's observations as code-sourced with a
// "code:<path>" prefix, mirroring stampObsSources but distinguishing code from
// session provenance so resolveSessionTitles can render it as "代码".
func stampObsSourcesCode(entities []graph.Entity, rel string) {
	src := "code:" + rel
	for i := range entities {
		s := make([]string, len(entities[i].Observations))
		for j := range s {
			s[j] = src
		}
		entities[i].ObsSources = s
	}
}
