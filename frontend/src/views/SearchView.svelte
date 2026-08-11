<script lang="ts">
  // Fast keyword search across all Claude Code session content (substring, no
  // LLM). Build the index once, then search is instant.
  import { onMount, onDestroy } from "svelte";
  import { SearchSessions, IndexSearch } from "../../wailsjs/go/main/App.js";
  import { EventsOn, EventsOff } from "../../wailsjs/runtime/runtime.js";
  import type { search } from "../../wailsjs/go/models";
  import { currentProject } from "../lib/stores";

  let keyword = "";
  let hits: search.Hit[] = [];
  let searching = false;
  let indexing = false;
  let status = "";

  function fmtTime(ms: number): string {
    if (!ms) return "";
    return new Date(ms).toLocaleString("zh-CN", { month: "2-digit", day: "2-digit", hour: "2-digit", minute: "2-digit" });
  }

  onMount(() => {
    EventsOn("search:progress", (p: any) => { status = `建立索引中… 已处理 ${p.indexed} 个会话`; });
    EventsOn("search:done", (p: any) => { indexing = false; status = `索引完成：${p.indexed} 个会话已入库`; });
  });
  onDestroy(() => { EventsOff("search:progress"); EventsOff("search:done"); });

  async function doSearch() {
    const kw = keyword.trim();
    if (!kw) { hits = []; return; }
    searching = true; status = "";
    try { hits = await SearchSessions(kw, $currentProject); status = `${hits.length} 条匹配`; }
    catch (e: any) { status = "搜索失败: " + (e?.message || e); }
    finally { searching = false; }
  }

  async function buildIndex() {
    if (indexing) return;
    indexing = true; status = "开始建立索引…";
    try { await IndexSearch(); }
    catch (e: any) { indexing = false; status = "索引失败: " + (e?.message || e); }
  }

  // Highlight the keyword inside a snippet (case-insensitive).
  function highlight(text: string, kw: string): string {
    const k = kw.trim();
    if (!k) return escapeHtml(text);
    const lc = text.toLowerCase(), lk = k.toLowerCase();
    let out = "", i = 0;
    while (true) {
      const j = lc.indexOf(lk, i);
      if (j < 0) { out += escapeHtml(text.slice(i)); break; }
      out += escapeHtml(text.slice(i, j)) + "<mark>" + escapeHtml(text.slice(j, j + k.length)) + "</mark>";
      i = j + k.length;
    }
    return out;
  }
  function escapeHtml(s: string): string {
    return s.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;");
  }
</script>

<div class="search">
  <div class="bar">
    <input
      class="kw"
      placeholder="搜索所有历史对话（关键词）…"
      bind:value={keyword}
      on:keydown={(e) => e.key === "Enter" && doSearch()}
    />
    <button class="btn" on:click={doSearch} disabled={searching}>搜索</button>
    <button class="btn ghost" on:click={buildIndex} disabled={indexing} title="首次使用先建索引（之后搜索秒出）">
      {indexing ? "索引中…" : "建索引"}
    </button>
    {#if status}<span class="status">{status}</span>{/if}
  </div>

  <div class="results">
    {#each hits as h, i}
      <div class="hit neuron-lightup" style="animation-delay: {Math.min(i * 45, 500)}ms">
        <div class="hit-head">
          <span class="role {h.role}">{h.role === "user" ? "你" : "AI"}</span>
          <span class="title">{h.title || "(无标题)"}</span>
          <span class="time">{fmtTime(h.updatedAt)}</span>
        </div>
        <div class="snippet selectable">{@html highlight(h.snippet, keyword)}</div>
      </div>
    {/each}
    {#if !searching && keyword && hits.length === 0}
      <div class="empty">没搜到。若还没建过索引，先点「建索引」。</div>
    {/if}
    {#if !keyword}
      <div class="empty">输入关键词，搜索你所有历史对话里的内容。首次使用先点「建索引」。</div>
    {/if}
  </div>
</div>

<style>
  .search { display: flex; flex-direction: column; height: 100%; font-family: var(--font-mono); }
  .bar {
    display: flex; align-items: center; gap: 8px;
    padding: 10px 14px; border-bottom: 1px solid var(--border);
  }
  .kw {
    flex: 1; background: var(--bg-base); color: var(--text-primary);
    border: 1px solid var(--border); border-radius: var(--radius-control);
    font-family: var(--font-mono); font-size: 13px; padding: 6px 10px; outline: none;
  }
  .kw:focus { border-color: var(--accent); }
  .btn {
    background: var(--accent); color: #fff; border: none;
    border-radius: var(--radius-control); padding: 5px 14px; font-size: 12px; font-family: var(--font-mono);
  }
  .btn.ghost { background: transparent; border: 1px solid var(--border); color: var(--text-muted); }
  .status { font-size: 11px; color: var(--text-muted); }
  .results { flex: 1; overflow-y: auto; padding: 8px 14px; }
  .hit { padding: 10px 0; border-bottom: 1px solid var(--border); }
  .hit-head { display: flex; align-items: center; gap: 8px; font-size: 11px; color: var(--text-muted); margin-bottom: 4px; }
  .role { padding: 0 6px; border-radius: 3px; }
  .role.user { background: var(--accent); color: #fff; }
  .role.assistant { background: var(--bg-elevated); }
  .title { color: var(--text-primary); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; max-width: 50%; }
  .time { margin-left: auto; }
  .snippet { font-size: 13px; line-height: 1.6; color: var(--text-primary); white-space: pre-wrap; word-break: break-word; }
  .snippet :global(mark) { background: var(--warning); color: #000; border-radius: 2px; padding: 0 1px; }
  .empty { padding: 30px; color: var(--text-muted); font-size: 13px; }
</style>

