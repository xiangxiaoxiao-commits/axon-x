<script lang="ts">
  // Memory view (UX §1 "记忆", §3.3). Manages the semantic memory library.
  //
  // Contract gap: the backend exposes no "ListMemories" binding (it is a store
  // internal), so we cannot enumerate existing memories directly. First version
  // presents memory management per conversation instead: every conversation is a
  // potential memory unit. Users can generate/refresh (SummarizeConversation),
  // delete (DeleteMemory), or backfill all missing ones (BackfillMemories).
  import { onMount } from "svelte";
  import { conversations } from "../lib/stores";
  import type { model } from "../../wailsjs/go/models";
  import {
    ListConversations,
    SummarizeConversation,
    DeleteMemory,
    BackfillMemories,
  } from "../../wailsjs/go/main/App.js";
  import { relativeTime } from "../lib/memory/format";

  // Per-conversation UI state, keyed by conversation id.
  type RowState = {
    summary: string; // last known summary text (from a generate/refresh)
    embedModel: string; // embedding model used, when known
    loading: boolean; // generate/refresh in flight
    deleting: boolean; // delete in flight
    expanded: boolean; // whether the summary panel is open
    error: string; // last per-row error message
  };

  const rows: Record<string, RowState> = {};
  let backfilling = false;
  let backfillMsg = "";
  let backfillError = "";

  function ensureRow(id: string): RowState {
    if (!rows[id]) {
      rows[id] = {
        summary: "",
        embedModel: "",
        loading: false,
        deleting: false,
        expanded: false,
        error: "",
      };
    }
    return rows[id];
  }

  onMount(async () => {
    // Refresh the shared conversation list so this view stands on its own.
    try {
      $conversations = await ListConversations();
    } catch (e) {
      console.error("load conversations", e);
    }
  });

  async function generate(c: model.Conversation) {
    const r = ensureRow(c.id);
    r.loading = true;
    r.error = "";
    rows[c.id] = r; // trigger reactivity
    try {
      const mem = await SummarizeConversation(c.id);
      r.summary = mem?.summary ?? "";
      r.embedModel = mem?.embedModel ?? "";
      r.expanded = true;
    } catch (e) {
      console.error("summarize", e);
      r.error = "生成记忆失败,请稍后重试。";
    } finally {
      r.loading = false;
      rows[c.id] = r;
    }
  }

  async function remove(c: model.Conversation) {
    const name = c.title || "未命名会话";
    if (!confirm(`删除「${name}」的记忆?删除后该会话将不再参与语义召回,此操作不可撤销。`)) {
      return;
    }
    const r = ensureRow(c.id);
    r.deleting = true;
    r.error = "";
    rows[c.id] = r;
    try {
      await DeleteMemory(c.id);
      r.summary = "";
      r.embedModel = "";
      r.expanded = false;
    } catch (e) {
      console.error("delete memory", e);
      r.error = "删除记忆失败,请稍后重试。";
    } finally {
      r.deleting = false;
      rows[c.id] = r;
    }
  }

  function toggleExpand(id: string) {
    const r = ensureRow(id);
    r.expanded = !r.expanded;
    rows[id] = r;
  }

  async function backfill() {
    backfilling = true;
    backfillMsg = "";
    backfillError = "";
    try {
      const n = await BackfillMemories();
      backfillMsg =
        n > 0 ? `已为 ${n} 个缺失记忆的会话生成摘要与向量。` : "所有会话都已有记忆,无需回填。";
    } catch (e) {
      console.error("backfill", e);
      backfillError = "回填失败,请稍后重试。";
    } finally {
      backfilling = false;
    }
  }
</script>

<div class="memory">
  <header class="head">
    <div class="titles">
      <h1>记忆</h1>
      <p class="sub">
        记忆用于跨会话检索。提问时系统会自动召回相关历史,把过去处理过的类似问题带回当前对话。
      </p>
    </div>
    <div class="head-actions">
      <button class="backfill" on:click={backfill} disabled={backfilling}>
        {backfilling ? "回填中…" : "回填所有缺失记忆"}
      </button>
    </div>
  </header>

  {#if backfillMsg}
    <div class="banner ok">{backfillMsg}</div>
  {/if}
  {#if backfillError}
    <div class="banner err">{backfillError}</div>
  {/if}

  <div class="body">
    {#if $conversations.length === 0}
      <div class="empty">还没有对话,聊几句后这里会出现可归纳的记忆。</div>
    {:else}
      <p class="hint">
        以下按会话列出记忆单元。为某个会话生成记忆后,它才会参与语义召回。
      </p>
      <ul class="list">
        {#each $conversations as c (c.id)}
          {@const r = ensureRow(c.id)}
          <li class="row">
            <div class="row-main">
              <div class="meta">
                <span class="title selectable">{c.title || "未命名会话"}</span>
                <span class="time">{relativeTime(c.updatedAt)}</span>
              </div>
              <div class="row-actions">
                {#if r.summary}
                  <button class="ghost" on:click={() => toggleExpand(c.id)}>
                    {r.expanded ? "收起" : "查看摘要"}
                  </button>
                {/if}
                <button class="ghost" on:click={() => generate(c)} disabled={r.loading || r.deleting}>
                  {#if r.loading}
                    生成中…
                  {:else if r.summary}
                    刷新记忆
                  {:else}
                    生成记忆
                  {/if}
                </button>
                <button
                  class="ghost danger"
                  on:click={() => remove(c)}
                  disabled={r.loading || r.deleting}
                >
                  {r.deleting ? "删除中…" : "删除记忆"}
                </button>
              </div>
            </div>

            {#if r.error}
              <div class="row-error">{r.error}</div>
            {/if}

            {#if r.summary && r.expanded}
              <div class="summary selectable">
                <p class="summary-text">{r.summary}</p>
                {#if r.embedModel}
                  <div class="summary-foot">embedding: {r.embedModel}</div>
                {/if}
              </div>
            {/if}
          </li>
        {/each}
      </ul>
    {/if}
  </div>
</div>

<style>
  .memory {
    height: 100%;
    display: flex;
    flex-direction: column;
    min-height: 0;
    background: var(--bg-base);
  }
  .head {
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    gap: var(--space);
    padding: 16px 20px;
    border-bottom: 1px solid var(--border);
  }
  .titles {
    min-width: 0;
    max-width: var(--read-max);
  }
  h1 {
    margin: 0 0 4px;
    font-size: 16px;
    font-weight: 600;
    color: var(--text-primary);
  }
  .sub {
    margin: 0;
    font-size: 13px;
    color: var(--text-muted);
    line-height: 1.6;
  }
  .head-actions {
    flex: 0 0 auto;
  }
  .backfill {
    background: var(--accent);
    color: var(--accent-fg);
    border: none;
    border-radius: var(--radius-control);
    padding: 8px 14px;
    font-size: 13px;
    font-weight: 500;
    white-space: nowrap;
  }
  .backfill:hover:not(:disabled) {
    filter: brightness(1.1);
  }
  .backfill:disabled {
    opacity: 0.6;
    cursor: default;
  }
  .banner {
    margin: 12px 20px 0;
    padding: 8px 12px;
    border-radius: var(--radius-control);
    font-size: 13px;
    border: 1px solid var(--border);
  }
  .banner.ok {
    color: var(--success);
    border-color: color-mix(in srgb, var(--success) 40%, var(--border));
    background: color-mix(in srgb, var(--success) 8%, transparent);
  }
  .banner.err {
    color: var(--danger);
    border-color: color-mix(in srgb, var(--danger) 40%, var(--border));
    background: color-mix(in srgb, var(--danger) 8%, transparent);
  }
  .body {
    flex: 1;
    overflow-y: auto;
    padding: 16px 20px 32px;
    min-height: 0;
  }
  .empty {
    color: var(--text-muted);
    text-align: center;
    padding: 64px 16px;
    font-size: 14px;
    line-height: 1.8;
  }
  .hint {
    margin: 0 0 12px;
    font-size: 12px;
    color: var(--text-muted);
    max-width: var(--read-max);
  }
  .list {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: var(--space);
    max-width: var(--read-max);
  }
  .row {
    background: var(--bg-surface);
    border: 1px solid var(--border);
    border-radius: var(--radius-card);
    padding: 12px 14px;
  }
  .row-main {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--space);
  }
  .meta {
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: 2px;
  }
  .title {
    font-size: 14px;
    color: var(--text-primary);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .time {
    font-size: 12px;
    color: var(--text-muted);
    font-family: var(--font-mono);
  }
  .row-actions {
    flex: 0 0 auto;
    display: flex;
    gap: 4px;
  }
  .ghost {
    background: var(--bg-elevated);
    border: 1px solid var(--border);
    color: var(--text-primary);
    border-radius: var(--radius-control);
    padding: 5px 10px;
    font-size: 12px;
    white-space: nowrap;
  }
  .ghost:hover:not(:disabled) {
    border-color: var(--accent);
  }
  .ghost:disabled {
    opacity: 0.5;
    cursor: default;
  }
  .ghost.danger:hover:not(:disabled) {
    border-color: var(--danger);
    color: var(--danger);
  }
  .row-error {
    margin-top: 8px;
    font-size: 12px;
    color: var(--danger);
  }
  .summary {
    margin-top: 10px;
    padding: 10px 12px;
    background: var(--bg-base);
    border: 1px solid var(--border);
    border-radius: var(--radius-control);
  }
  .summary-text {
    margin: 0;
    font-size: 13px;
    line-height: 1.7;
    color: var(--text-primary);
    white-space: pre-wrap;
  }
  .summary-foot {
    margin-top: 8px;
    font-size: 11px;
    color: var(--text-muted);
    font-family: var(--font-mono);
  }
</style>
