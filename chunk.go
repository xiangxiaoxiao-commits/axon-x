package main

import (
	"fmt"
	"log"
	"strings"

	"axon/internal/claudedata"
	"axon/internal/embed"
	"axon/internal/graph"
	"axon/internal/retrieve"
)

// Chunking + raw-context retrieval tuning. These govern the "raw context"
// channel that runs alongside the entity/relation ("structure") channel.
const (
	// chunkTargetChars is the target size of a conversation/task chunk (~450
	// tokens for Chinese ≈ 600 chars). Q&A-style retrieval favors 256–512 token
	// blocks; we sit at the upper end so a turn's reasoning stays intact.
	chunkTargetChars = 600
	// chunkMinChars drops residual blocks below this (greetings, one-liners) so
	// they don't pollute the vector space.
	chunkMinChars = 40
	// codeChunkTargetChars bounds a code chunk; code is denser than prose so a
	// single oversized function is split near this size at blank-line boundaries.
	codeChunkTargetChars = 1600

	// injectStructBudgetChars bounds the structure section (~800 tokens).
	injectStructBudgetChars = 1600
	// injectChunkBudgetChars bounds the raw-context section (~2000 tokens).
	injectChunkBudgetChars = 4000
)

// isNoiseChunk reports whether a candidate block is pure noise not worth
// indexing: too short after trimming, a bare acknowledgement, or clearly a tool
// log / stack trace rather than substantive discussion. This is the core defense
// against feeding un-distilled conversational filler into the vector space.
func isNoiseChunk(text string) bool {
	t := strings.TrimSpace(text)
	if len([]rune(stripSpeaker(t))) < chunkMinChars {
		return true
	}
	body := strings.ToLower(stripSpeaker(t))
	// Bare acknowledgements / pleasantries.
	switch strings.TrimRight(body, "。.!！~ ") {
	case "好的", "好", "继续", "谢谢", "多谢", "ok", "okay", "thanks", "thank you", "收到", "嗯", "行":
		return true
	}
	// Tool-output / log heuristics: a block dominated by log-ish lines (stack
	// frames, timestamps, shell prompts) carries no durable knowledge.
	if looksLikeLog(body) {
		return true
	}
	return false
}

// stripSpeaker removes a leading "user:"/"assistant:" prefix for length/noise
// checks so the prefix itself doesn't inflate a block past the min threshold.
func stripSpeaker(t string) string {
	for _, p := range []string{"user:", "assistant:"} {
		if strings.HasPrefix(strings.ToLower(t), p) {
			return strings.TrimSpace(t[len(p):])
		}
	}
	return t
}

// looksLikeLog flags blocks where most lines look like log/stack-trace/shell
// output rather than prose or decision-making.
func looksLikeLog(body string) bool {
	lines := strings.Split(body, "\n")
	if len(lines) < 3 {
		return false
	}
	logish := 0
	for _, ln := range lines {
		ln = strings.TrimSpace(ln)
		if ln == "" {
			continue
		}
		if strings.HasPrefix(ln, "at ") || strings.HasPrefix(ln, "$ ") ||
			strings.HasPrefix(ln, "> ") || strings.Contains(ln, "traceback") ||
			strings.Contains(ln, "goroutine ") || strings.Contains(ln, "	at ") ||
			strings.HasPrefix(ln, "error:") || strings.HasPrefix(ln, "warning:") {
			logish++
		}
	}
	return logish*2 >= len(lines) // majority of non-empty lines are log-ish
}

// chunkTranscript splits a full session into verbatim conversation chunks. It
// aggregates consecutive turns up to ~chunkTargetChars, keeping speaker prefixes
// and never cutting mid-message, carries one turn of overlap between blocks, and
// drops noise blocks. It runs over the COMPLETE message list (not the
// LLM-distillation-bounded transcript) so the raw channel covers the whole
// session. sessionID becomes each chunk's Source; kind is "chat".
func chunkTranscript(sessionID string, msgs []claudedata.SessionMessage) []graph.Chunk {
	// Render turns with speaker prefixes so the model knows who said what.
	turns := make([]string, 0, len(msgs))
	for _, m := range msgs {
		if strings.TrimSpace(m.Text) == "" {
			continue
		}
		turns = append(turns, m.Role+": "+strings.TrimSpace(m.Text))
	}
	return assembleChunks(turns, sessionID, "chat", chunkTargetChars)
}

// chunkTaskTranscript splits an accepted task's transcript (input + spec +
// result) into verbatim task chunks. It reuses the paragraph splitter so spec
// fields and result paragraphs stay intact. Source is "task:<id>", kind "task".
func chunkTaskTranscript(taskID, transcript string) []graph.Chunk {
	paras := splitParagraphs(transcript)
	return assembleChunks(paras, "task:"+taskID, "task", chunkTargetChars)
}

// splitParagraphs breaks text on blank lines into trimmed, non-empty paragraphs.
func splitParagraphs(text string) []string {
	var out []string
	for _, p := range strings.Split(text, "\n\n") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// assembleChunks packs pre-split units (turns/paragraphs) into chunks near
// target size, dropping noise, with a one-unit overlap so a reasoning thread
// split across a boundary is still recoverable from one block. Units larger than
// target become their own chunk (never cut mid-unit).
func assembleChunks(units []string, source, kind string, target int) []graph.Chunk {
	var chunks []graph.Chunk
	var cur []string
	curLen := 0
	seq := 0
	flush := func() {
		if len(cur) == 0 {
			return
		}
		text := strings.Join(cur, "\n\n")
		if !isNoiseChunk(text) {
			chunks = append(chunks, graph.Chunk{
				ID:     fmt.Sprintf("%s#%d", source, seq),
				Text:   text,
				Source: source,
				Kind:   kind,
			})
			seq++
		}
		// One-unit overlap: seed the next chunk with the last unit of this one.
		last := cur[len(cur)-1]
		cur = nil
		curLen = 0
		if len([]rune(last)) < target {
			cur = append(cur, last)
			curLen = len(last)
		}
	}
	for _, u := range units {
		u = strings.TrimSpace(u)
		if u == "" {
			continue
		}
		if curLen > 0 && curLen+len(u) > target {
			flush()
		}
		cur = append(cur, u)
		curLen += len(u)
		if curLen >= target {
			flush()
		}
	}
	// Final partial block (no overlap carry needed).
	if len(cur) > 0 {
		text := strings.Join(cur, "\n\n")
		if !isNoiseChunk(text) {
			chunks = append(chunks, graph.Chunk{
				ID:     fmt.Sprintf("%s#%d", source, seq),
				Text:   text,
				Source: source,
				Kind:   kind,
			})
		}
	}
	return chunks
}

// chunkCodeFile splits one source file into verbatim code chunks along top-level
// declaration boundaries (blank-line separated blocks), packing small
// declarations together up to codeChunkTargetChars and emitting oversized ones
// on their own. Source is "code:<rel>", kind "code". Unlike conversation text,
// repository code is the settled final state, so it is not noise-filtered beyond
// the min-size guard.
func chunkCodeFile(rel, content string) []graph.Chunk {
	if strings.TrimSpace(content) == "" {
		return nil
	}
	source := "code:" + rel
	blocks := splitParagraphs(content) // blank-line separated: functions/types/imports
	var chunks []graph.Chunk
	var cur []string
	curLen := 0
	seq := 0
	emit := func(text string) {
		text = strings.TrimSpace(text)
		if len([]rune(text)) < chunkMinChars {
			return
		}
		chunks = append(chunks, graph.Chunk{
			ID:     fmt.Sprintf("%s#%d", source, seq),
			Text:   "// " + rel + "\n" + text,
			Source: source,
			Kind:   "code",
		})
		seq++
	}
	flush := func() {
		if len(cur) > 0 {
			emit(strings.Join(cur, "\n\n"))
			cur = nil
			curLen = 0
		}
	}
	for _, b := range blocks {
		if curLen > 0 && curLen+len(b) > codeChunkTargetChars {
			flush()
		}
		cur = append(cur, b)
		curLen += len(b)
		if curLen >= codeChunkTargetChars {
			flush()
		}
	}
	flush()
	return chunks
}

// embedChunks fills each chunk's Embedding in place using the batch API. On any
// batch failure it logs and leaves embeddings empty (those chunks simply won't be
// recallable), so one bad call never aborts indexing.
func (a *App) embedChunks(emb embed.Embedder, chunks []graph.Chunk) {
	if emb == nil || len(chunks) == 0 {
		return
	}
	texts := make([]string, len(chunks))
	for i := range chunks {
		texts[i] = chunks[i].Text
	}
	vecs, err := emb.EmbedBatch(a.ctx, texts)
	if err != nil {
		log.Printf("axon: embed %d chunks failed: %v", len(chunks), err)
		return
	}
	for i := range chunks {
		if i < len(vecs) {
			chunks[i].Embedding = vecs[i]
		}
	}
}

// loadChunks gathers every embedded chunk across a project's caches into one
// flat slice for vector recall. Chunks without an embedding are skipped (they
// can't be semantically matched). Order is irrelevant; recall re-ranks by score.
func (a *App) loadChunks(dataDir, projectSlug string) []graph.Chunk {
	caches, err := graph.LoadAllCache(dataDir, projectSlug)
	if err != nil {
		return nil
	}
	var out []graph.Chunk
	for _, c := range caches {
		for _, ch := range c.Chunks {
			if len(ch.Embedding) > 0 {
				out = append(out, ch)
			}
		}
	}
	return out
}

// rrfFuse delegates to the shared implementation; kept as a thin wrapper so
// existing package tests continue to exercise it here.
func rrfFuse(lists ...[]string) []string { return retrieve.RRFFuse(lists...) }
