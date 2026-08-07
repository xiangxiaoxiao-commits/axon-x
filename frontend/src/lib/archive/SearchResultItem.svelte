<script lang="ts">
  import type { SearchResult } from "./search";
  import { highlightSegments } from "./search";
  import { relativeTime } from "./time";

  export let result: SearchResult;
  export let onOpen: (id: string) => void;

  $: titleSegs = highlightSegments(result.title, result.terms);
  $: isSemantic = result.sources.includes("semantic");
  $: isFulltext = result.sources.includes("fulltext");
  $: percent = Math.round(result.score * 100);
</script>

<div class="item selectable">
  <div class="head">
    <span class="dot" class:semantic={isSemantic}></span>
    <span class="title">
      {#each titleSegs as seg}
        {#if seg.hit}<mark>{seg.text}</mark>{:else}{seg.text}{/if}
      {/each}
    </span>
    <span class="time">{relativeTime(result.updatedAt)}</span>
  </div>

  {#if result.snippet}
    <div class="snippet">{result.snippet}</div>
  {/if}

  <div class="meta">
    {#if result.model}
      <span class="model">{result.model}</span>
    {/if}
    {#if isSemantic}
      <span class="tag semantic">语义相近 {percent}%</span>
    {/if}
    {#if isFulltext}
      <span class="tag fulltext">命中关键词</span>
    {/if}
    <button class="open" on:click={() => onOpen(result.conversationId)}>打开会话</button>
  </div>
</div>

<style>
  .item {
    padding: 12px;
    border: 1px solid var(--border);
    border-radius: var(--radius-card);
    background: var(--bg-surface);
    display: flex;
    flex-direction: column;
    gap: 6px;
  }
  .item:hover {
    background: var(--bg-elevated);
  }
  .head {
    display: flex;
    align-items: center;
    gap: 8px;
  }
  .dot {
    width: 8px;
    height: 8px;
    flex: 0 0 8px;
    border-radius: 50%;
    background: var(--text-muted);
  }
  .dot.semantic {
    background: var(--accent);
  }
  .title {
    flex: 1;
    min-width: 0;
    font-weight: 500;
    color: var(--text-primary);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .title mark {
    background: rgba(210, 153, 34, 0.35);
    color: var(--text-primary);
    border-radius: 2px;
    padding: 0 1px;
  }
  .time {
    flex: 0 0 auto;
    color: var(--text-muted);
    font-size: 12px;
    font-family: var(--font-mono);
  }
  .snippet {
    color: var(--text-muted);
    font-size: 13px;
    line-height: 1.5;
    display: -webkit-box;
    -webkit-line-clamp: 2;
    -webkit-box-orient: vertical;
    overflow: hidden;
  }
  .meta {
    display: flex;
    align-items: center;
    gap: 8px;
    flex-wrap: wrap;
  }
  .model {
    font-family: var(--font-mono);
    font-size: 12px;
    color: var(--text-muted);
  }
  .tag {
    font-size: 11px;
    padding: 1px 6px;
    border-radius: 999px;
    border: 1px solid var(--border);
    color: var(--text-muted);
  }
  .tag.semantic {
    color: var(--accent);
    border-color: var(--accent);
  }
  .tag.fulltext {
    color: var(--warning);
    border-color: var(--warning);
  }
  .open {
    margin-left: auto;
    border: 1px solid var(--accent);
    background: var(--accent);
    color: var(--accent-fg);
    border-radius: var(--radius-control);
    padding: 4px 10px;
    font-size: 12px;
  }
  .open:hover {
    opacity: 0.9;
  }
</style>
