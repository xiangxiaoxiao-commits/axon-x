// Pure helpers for archive search: highlighting, merging full-text and
// semantic results, and filtering by model / date range.
import type { model } from "../../../wailsjs/go/models";

export type ResultSource = "fulltext" | "semantic";

export interface SearchResult {
  conversationId: string;
  title: string;
  model: string;
  updatedAt: number; // unix ms
  snippet: string; // summary片段 (semantic) or "" (fulltext title match)
  sources: ResultSource[];
  score: number; // semantic similarity 0..1, 0 when full-text only
  terms: string[]; // matched keywords for highlight
}

export type DateRange = "7d" | "30d" | "all";

// Split text into segments marking which ones match any of the terms
// (case-insensitive). Used to render <mark> highlights safely without
// injecting raw HTML.
export interface Segment {
  text: string;
  hit: boolean;
}

export function highlightSegments(text: string, terms: string[]): Segment[] {
  const clean = terms.map((t) => t.trim()).filter((t) => t.length > 0);
  if (!text || clean.length === 0) return [{ text, hit: false }];
  // Longest first so overlapping terms prefer the widest match.
  const sorted = [...clean].sort((a, b) => b.length - a.length);
  const escaped = sorted.map((t) => t.replace(/[.*+?^${}()|[\]\\]/g, "\\$&"));
  const re = new RegExp(`(${escaped.join("|")})`, "ig");
  const segs: Segment[] = [];
  let last = 0;
  let m: RegExpExecArray | null;
  while ((m = re.exec(text)) !== null) {
    if (m.index > last) segs.push({ text: text.slice(last, m.index), hit: false });
    segs.push({ text: m[0], hit: true });
    last = m.index + m[0].length;
    if (m.index === re.lastIndex) re.lastIndex++; // guard against zero-length
  }
  if (last < text.length) segs.push({ text: text.slice(last), hit: false });
  return segs;
}

// Full-text: match the query (as a whole, case-insensitive) against
// conversation titles. Keeps it simple per spec — title contains matching.
export function fullTextSearch(
  conversations: model.Conversation[],
  query: string
): SearchResult[] {
  const q = query.trim().toLowerCase();
  if (!q) return [];
  const out: SearchResult[] = [];
  for (const c of conversations) {
    const title = c.title || "";
    if (title.toLowerCase().includes(q)) {
      out.push({
        conversationId: c.id,
        title,
        model: c.model || "",
        updatedAt: c.updatedAt,
        snippet: "",
        sources: ["fulltext"],
        score: 0,
        terms: [query.trim()],
      });
    }
  }
  return out;
}

// Convert semantic MemoryHit[] into SearchResult[], enriching model/updatedAt
// from the loaded conversation records when available.
export function semanticResults(
  hits: model.MemoryHit[],
  byId: Map<string, model.Conversation>
): SearchResult[] {
  return hits.map((h) => {
    const conv = byId.get(h.conversationId);
    return {
      conversationId: h.conversationId,
      title: h.title || conv?.title || "(无标题)",
      model: conv?.model || "",
      updatedAt: conv?.updatedAt || h.updatedAt,
      snippet: h.summary || "",
      sources: ["semantic"],
      score: h.score,
      terms: [],
    };
  });
}

// Merge full-text and semantic results by conversationId, combining sources.
export function mergeResults(
  fulltext: SearchResult[],
  semantic: SearchResult[]
): SearchResult[] {
  const map = new Map<string, SearchResult>();
  for (const r of fulltext) map.set(r.conversationId, { ...r });
  for (const r of semantic) {
    const ex = map.get(r.conversationId);
    if (ex) {
      ex.sources = Array.from(new Set([...ex.sources, ...r.sources]));
      ex.score = Math.max(ex.score, r.score);
      if (!ex.snippet && r.snippet) ex.snippet = r.snippet;
    } else {
      map.set(r.conversationId, { ...r });
    }
  }
  const merged = Array.from(map.values());
  // Relevance ordering: semantic score first, then recency.
  merged.sort((a, b) => {
    if (b.score !== a.score) return b.score - a.score;
    return b.updatedAt - a.updatedAt;
  });
  return merged;
}

// Filter by model (empty = all) and date range, using conversation fields.
export function applyFilters(
  results: SearchResult[],
  modelFilter: string,
  range: DateRange,
  now: number = Date.now()
): SearchResult[] {
  let cutoff = 0;
  if (range === "7d") cutoff = now - 7 * 24 * 60 * 60 * 1000;
  else if (range === "30d") cutoff = now - 30 * 24 * 60 * 60 * 1000;
  return results.filter((r) => {
    if (modelFilter && r.model !== modelFilter) return false;
    if (cutoff && r.updatedAt && r.updatedAt < cutoff) return false;
    return true;
  });
}
