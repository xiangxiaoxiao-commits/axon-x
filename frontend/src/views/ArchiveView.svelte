<script lang="ts">
  import { onMount } from "svelte";
  import { activeView, currentConversationID, conversations } from "../lib/stores";
  import { ListConversations, RecallMemories } from "../../wailsjs/go/main/App.js";
  import type { model } from "../../wailsjs/go/models";
  import SearchResultItem from "../lib/archive/SearchResultItem.svelte";
  import {
    fullTextSearch,
    semanticResults,
    mergeResults,
    applyFilters,
    type SearchResult,
    type DateRange,
  } from "../lib/archive/search";

  // Search inputs.
  let query = "";
  let useFullText = true;
  let useSemantic = true;

  // Filters.
  let modelFilter = "";
  let dateRange: DateRange = "all";

  // Data / async state.
  let convs: model.Conversation[] = [];
  let semanticDegraded = false; // true when RecallMemories returned empty / failed
  let semanticFailed = false; // true only when the call threw
  let searching = false;
  let searched = false;

  // Raw result sets before filtering.
  let ftResults: SearchResult[] = [];
  let semResults: SearchResult[] = [];

  $: byId = new Map(convs.map((c) => [c.id, c]));

  // Available models for the filter dropdown.
  $: models = Array.from(new Set(convs.map((c) => c.model).filter(Boolean))).sort();

  // Merge + filter for display. Recomputes when filters change.
  $: merged = mergeResults(useFullText ? ftResults : [], useSemantic ? semResults : []);
  $: results = applyFilters(merged, modelFilter, dateRange);

  // Semantic column is disabled when offline / no embedding provider.
  $: semanticUnavailable = semanticDegraded;

  onMount(async () => {
    // Reuse the sidebar list if already loaded, otherwise fetch.
    convs = $conversations.length ? $conversations : await loadConversations();
  });

  async function loadConversations(): Promise<model.Conversation[]> {
    try {
      const list = await ListConversations();
      $conversations = list;
      return list;
    } catch (e) {
      console.error("archive: load conversations", e);
      return [];
    }
  }

  async function runSearch() {
    const q = query.trim();
    searched = true;
    if (!q) {
      ftResults = [];
      semResults = [];
      return;
    }
    // Make sure we have the latest conversation list for full-text + enrichment.
    if (!convs.length) convs = await loadConversations();

    searching = true;
    // Full-text is synchronous and always available.
    ftResults = useFullText ? fullTextSearch(convs, q) : [];

    // Semantic search: degrade gracefully. RecallMemories returns [] when no
    // embedding provider is configured / offline; it may also throw.
    if (useSemantic) {
      try {
        const hits = await RecallMemories("", q);
        semResults = semanticResults(hits, byId);
        // Empty result set is the documented degraded signal (UX §5.4).
        semanticDegraded = hits.length === 0;
        semanticFailed = false;
      } catch (e) {
        console.error("archive: semantic recall", e);
        semResults = [];
        semanticDegraded = true;
        semanticFailed = true;
      }
    } else {
      semResults = [];
    }
    searching = false;
  }

  function onSubmit(e: Event) {
    e.preventDefault();
    runSearch();
  }

  function openConversation(id: string) {
    $currentConversationID = id;
    $activeView = "chat";
  }

  function clearFilters() {
    modelFilter = "";
    dateRange = "all";
  }

  const dateOptions: [DateRange, string][] = [
    ["7d", "近7天"],
    ["30d", "近30天"],
    ["all", "全部"],
  ];
</script>

<div class="archive">
  <!-- Search bar -->
  <form class="searchbar" on:submit={onSubmit}>
    <span class="glyph">⌕</span>
    <input
      class="search-input"
      type="text"
      placeholder="搜索所有会话(全文 + 语义)"
      bind:value={query}
    />
    <button class="search-btn" type="submit">搜索</button>
  </form>

  <div class="body">
    <!-- Filter panel -->
    <aside class="filters">
      <div class="filters-title">筛选</div>

      <label class="check">
        <input type="checkbox" bind:checked={useFullText} />
        全文
      </label>
      <label class="check" class:disabled={semanticUnavailable}>
        <input type="checkbox" bind:checked={useSemantic} />
        语义
        {#if semanticUnavailable}<span class="need">需要网络</span>{/if}
      </label>

      <div class="field">
        <span class="label">模型</span>
        <select bind:value={modelFilter}>
          <option value="">全部</option>
          {#each models as m}
            <option value={m}>{m}</option>
          {/each}
        </select>
      </div>

      <div class="field">
        <span class="label">日期</span>
        <select bind:value={dateRange}>
          {#each dateOptions as [val, text]}
            <option value={val}>{text}</option>
          {/each}
        </select>
      </div>

      <button class="clear" type="button" on:click={clearFilters}>清空筛选</button>
    </aside>

    <!-- Results -->
    <section class="results">
      {#if searching}
        <div class="hint">搜索中…</div>
      {:else if !searched}
        <div class="hint">输入关键词开始检索。全文匹配会话标题,语义查找相关历史。</div>
      {:else}
        <div class="results-head">
          <span>{results.length} 条结果 · 按相关度排序</span>
        </div>

        {#if useSemantic && semanticUnavailable}
          <div class="degrade">
            {#if semanticFailed}
              语义搜索需要网络。全文搜索仍可用。
            {:else}
              语义搜索需要配置 OpenAI provider / 需要网络。全文搜索仍可用。
            {/if}
          </div>
        {/if}

        {#if results.length === 0}
          <div class="empty">
            {#if query.trim() === ""}
              归档为空。你的对话会自动保存在这里,关窗也不会丢。
            {:else}
              没有匹配的结果。试试其他关键词或放宽筛选。
            {/if}
          </div>
        {:else}
          <div class="result-list">
            {#each results as r (r.conversationId)}
              <SearchResultItem result={r} onOpen={openConversation} />
            {/each}
          </div>
        {/if}
      {/if}
    </section>
  </div>
</div>

<style>
  .archive {
    display: flex;
    flex-direction: column;
    height: 100%;
    background: var(--bg-base);
  }
  .searchbar {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 12px 16px;
    border-bottom: 1px solid var(--border);
    background: var(--bg-surface);
  }
  .glyph {
    color: var(--text-muted);
    font-size: 16px;
  }
  .search-input {
    flex: 1;
    min-width: 0;
    background: var(--bg-base);
    border: 1px solid var(--border);
    border-radius: var(--radius-control);
    color: var(--text-primary);
    padding: 8px 10px;
    font-size: 14px;
    font-family: var(--font-ui);
  }
  .search-input:focus {
    outline: none;
    border-color: var(--accent);
  }
  .search-btn {
    border: 1px solid var(--accent);
    background: var(--accent);
    color: var(--accent-fg);
    border-radius: var(--radius-control);
    padding: 8px 14px;
    font-size: 13px;
  }
  .search-btn:hover {
    opacity: 0.9;
  }
  .body {
    flex: 1;
    display: flex;
    min-height: 0;
  }
  .filters {
    flex: 0 0 200px;
    border-right: 1px solid var(--border);
    padding: 16px;
    display: flex;
    flex-direction: column;
    gap: 12px;
    background: var(--bg-surface);
    overflow-y: auto;
  }
  .filters-title {
    font-weight: 600;
    color: var(--text-primary);
  }
  .check {
    display: flex;
    align-items: center;
    gap: 6px;
    color: var(--text-primary);
    font-size: 13px;
  }
  .check.disabled {
    color: var(--text-muted);
  }
  .check .need {
    font-size: 11px;
    color: var(--warning);
    margin-left: 4px;
  }
  .field {
    display: flex;
    flex-direction: column;
    gap: 4px;
  }
  .field .label {
    font-size: 12px;
    color: var(--text-muted);
  }
  select {
    background: var(--bg-base);
    border: 1px solid var(--border);
    border-radius: var(--radius-control);
    color: var(--text-primary);
    padding: 6px 8px;
    font-size: 13px;
    font-family: var(--font-ui);
  }
  select:focus {
    outline: none;
    border-color: var(--accent);
  }
  .clear {
    margin-top: 4px;
    align-self: flex-start;
    background: transparent;
    border: 1px solid var(--border);
    color: var(--text-muted);
    border-radius: var(--radius-control);
    padding: 6px 10px;
    font-size: 12px;
  }
  .clear:hover {
    background: var(--bg-elevated);
    color: var(--text-primary);
  }
  .results {
    flex: 1;
    min-width: 0;
    padding: 16px;
    overflow-y: auto;
  }
  .results-head {
    color: var(--text-muted);
    font-size: 13px;
    margin-bottom: 12px;
  }
  .hint {
    color: var(--text-muted);
    font-size: 13px;
    padding: 24px 0;
    text-align: center;
  }
  .degrade {
    background: var(--bg-elevated);
    border: 1px solid var(--border);
    border-left: 3px solid var(--warning);
    color: var(--text-muted);
    border-radius: var(--radius-control);
    padding: 8px 12px;
    font-size: 13px;
    margin-bottom: 12px;
  }
  .empty {
    color: var(--text-muted);
    font-size: 14px;
    text-align: center;
    padding: 48px 24px;
    line-height: 1.6;
  }
  .result-list {
    display: flex;
    flex-direction: column;
    gap: 10px;
    max-width: var(--read-max);
  }
</style>
