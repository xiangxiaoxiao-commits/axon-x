<script lang="ts">
  // Sidebar conversation list, grouped by activity date (今天/昨天/更早).
  // Handles new / select / rename / delete. Subscribes to the shared stores.
  import { createEventDispatcher } from "svelte";
  import type { model } from "../../../wailsjs/go/models";

  export let conversations: model.Conversation[] = [];
  export let currentID: string | null = null;
  // Ids of conversations that hit semantic memory (⟳ marker). Optional.
  export let memoryHits: Set<string> = new Set();

  const dispatch = createEventDispatcher<{
    select: string;
    create: void;
    rename: { id: string; title: string };
    remove: string;
  }>();

  // Inline rename state.
  let editingID: string | null = null;
  let editTitle = "";

  // Normalize a timestamp that may be in seconds or milliseconds to ms.
  function toMs(ts: number): number {
    return ts < 1e12 ? ts * 1000 : ts;
  }

  type Group = { label: string; items: model.Conversation[] };

  // Bucket conversations into 今天 / 昨天 / 更早 by updatedAt.
  function group(list: model.Conversation[]): Group[] {
    const now = new Date();
    const startOfToday = new Date(now.getFullYear(), now.getMonth(), now.getDate()).getTime();
    const startOfYesterday = startOfToday - 86400_000;
    const today: model.Conversation[] = [];
    const yesterday: model.Conversation[] = [];
    const earlier: model.Conversation[] = [];
    for (const c of list) {
      const t = toMs(c.updatedAt);
      if (t >= startOfToday) today.push(c);
      else if (t >= startOfYesterday) yesterday.push(c);
      else earlier.push(c);
    }
    const out: Group[] = [];
    if (today.length) out.push({ label: "今天", items: today });
    if (yesterday.length) out.push({ label: "昨天", items: yesterday });
    if (earlier.length) out.push({ label: "更早", items: earlier });
    return out;
  }

  $: groups = group(conversations);

  function startRename(c: model.Conversation) {
    editingID = c.id;
    editTitle = c.title || "";
  }

  function commitRename() {
    if (editingID) {
      const title = editTitle.trim();
      if (title) dispatch("rename", { id: editingID, title });
    }
    editingID = null;
  }

  function onEditKey(e: KeyboardEvent) {
    if (e.key === "Enter") {
      e.preventDefault();
      commitRename();
    } else if (e.key === "Escape") {
      e.preventDefault();
      editingID = null;
    }
  }

  function confirmDelete(c: model.Conversation) {
    const name = c.title || "未命名会话";
    if (confirm(`删除会话「${name}」?此操作不可撤销。`)) {
      dispatch("remove", c.id);
    }
  }
</script>

<div class="sidebar">
  <div class="side-head">
    <button class="search" title="搜索会话 (⌘K)" disabled>⌕ 搜索会话</button>
    <button class="new" on:click={() => dispatch("create")} title="新会话 (⌘N)">+ 新会话</button>
  </div>

  <div class="list">
    {#if conversations.length === 0}
      <div class="empty">还没有会话。<br />⌘N 开始第一个对话。</div>
    {:else}
      {#each groups as g (g.label)}
        <div class="group-label">{g.label}</div>
        {#each g.items as c (c.id)}
          <div
            class="item"
            class:active={c.id === currentID}
            role="button"
            tabindex="0"
            on:click={() => dispatch("select", c.id)}
            on:keydown={(e) => (e.key === "Enter" ? dispatch("select", c.id) : null)}
          >
            <span class="dot" class:on={c.id === currentID}></span>
            {#if editingID === c.id}
              <!-- svelte-ignore a11y-autofocus -->
              <input
                class="rename-input"
                bind:value={editTitle}
                on:keydown={onEditKey}
                on:blur={commitRename}
                on:click|stopPropagation
                autofocus
              />
            {:else}
              <span class="title">{c.title || "未命名会话"}</span>
              {#if memoryHits.has(c.id)}<span class="mem" title="命中语义记忆">⟳</span>{/if}
              <span class="actions">
                <button
                  class="act"
                  title="重命名"
                  on:click|stopPropagation={() => startRename(c)}>✎</button
                >
                <button
                  class="act danger"
                  title="删除"
                  on:click|stopPropagation={() => confirmDelete(c)}>🗑</button
                >
              </span>
            {/if}
          </div>
        {/each}
      {/each}
    {/if}
  </div>
</div>

<style>
  .sidebar {
    width: 240px;
    flex: 0 0 240px;
    background: var(--bg-surface);
    border-right: 1px solid var(--border);
    display: flex;
    flex-direction: column;
    min-height: 0;
  }
  .side-head {
    padding: var(--space);
    border-bottom: 1px solid var(--border);
    display: flex;
    flex-direction: column;
    gap: var(--space);
  }
  .search {
    width: 100%;
    text-align: left;
    background: var(--bg-elevated);
    border: 1px solid var(--border);
    color: var(--text-muted);
    border-radius: var(--radius-control);
    padding: 6px 10px;
    font-size: 13px;
  }
  .new {
    width: 100%;
    background: var(--accent);
    color: var(--accent-fg);
    border: none;
    border-radius: var(--radius-control);
    padding: 6px 10px;
    font-size: 13px;
    font-weight: 500;
  }
  .new:hover {
    filter: brightness(1.1);
  }
  .list {
    flex: 1;
    overflow-y: auto;
    padding: var(--space) 6px;
    min-height: 0;
  }
  .empty {
    color: var(--text-muted);
    text-align: center;
    padding: 32px 12px;
    font-size: 13px;
    line-height: 1.8;
  }
  .group-label {
    color: var(--text-muted);
    font-size: 11px;
    text-transform: uppercase;
    letter-spacing: 0.04em;
    padding: 10px 8px 4px;
  }
  .item {
    display: flex;
    align-items: center;
    gap: 6px;
    padding: 6px 8px;
    border-radius: var(--radius-control);
    cursor: pointer;
    color: var(--text-primary);
  }
  .item:hover {
    background: var(--bg-elevated);
  }
  .item.active {
    background: var(--bg-elevated);
    box-shadow: inset 2px 0 0 var(--accent);
  }
  .dot {
    width: 6px;
    height: 6px;
    border-radius: 50%;
    background: transparent;
    flex: 0 0 6px;
  }
  .dot.on {
    background: var(--accent);
  }
  .title {
    flex: 1;
    font-size: 13px;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .mem {
    color: var(--accent);
    font-size: 12px;
  }
  .actions {
    display: none;
    gap: 2px;
  }
  .item:hover .actions {
    display: flex;
  }
  .act {
    background: transparent;
    border: none;
    color: var(--text-muted);
    font-size: 12px;
    padding: 2px 4px;
    border-radius: 4px;
  }
  .act:hover {
    background: var(--border);
    color: var(--text-primary);
  }
  .act.danger:hover {
    color: var(--danger);
  }
  .rename-input {
    flex: 1;
    background: var(--bg-base);
    border: 1px solid var(--accent);
    color: var(--text-primary);
    border-radius: 4px;
    padding: 3px 6px;
    font-size: 13px;
    font-family: inherit;
  }
</style>
