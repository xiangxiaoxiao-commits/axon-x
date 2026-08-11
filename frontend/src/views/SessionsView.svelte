<script lang="ts">
  // Browse Claude Code's saved sessions: projects -> sessions -> transcript.
  // These live on disk under ~/.claude and are never lost when a tab closes.
  import { onMount } from "svelte";
  import {
    ListClaudeProjects, ListClaudeSessions, ReadClaudeSession,
  } from "../../wailsjs/go/main/App.js";
  import type { claudedata } from "../../wailsjs/go/models";

  let projects: claudedata.Project[] = [];
  let sessions: claudedata.SessionMeta[] = [];
  let messages: claudedata.SessionMessage[] = [];
  let curProject = "";
  let curSession = "";
  let filter = "";
  let loading = false;

  function fmtTime(ms: number): string {
    if (!ms) return "";
    return new Date(ms).toLocaleString("zh-CN", { month: "2-digit", day: "2-digit", hour: "2-digit", minute: "2-digit" });
  }

  onMount(async () => {
    try { projects = await ListClaudeProjects(); } catch (e) { console.error(e); }
    if (projects.length) selectProject(projects[0].slug);
  });

  async function selectProject(slug: string) {
    curProject = slug; curSession = ""; messages = []; sessions = [];
    try { sessions = await ListClaudeSessions(slug); } catch (e) { console.error(e); }
  }

  async function selectSession(id: string) {
    curSession = id; loading = true; messages = [];
    try { messages = await ReadClaudeSession(curProject, id); }
    catch (e) { console.error(e); }
    finally { loading = false; }
  }

  $: shownSessions = filter
    ? sessions.filter((s) => (s.title || "").toLowerCase().includes(filter.toLowerCase()))
    : sessions;
</script>

<div class="browser">
  <div class="col projects">
    <div class="col-head">项目</div>
    {#each projects as p}
      <button class="item" class:active={p.slug === curProject} on:click={() => selectProject(p.slug)}>
        <div class="item-title">{p.path}</div>
        <div class="item-sub">{p.sessionCount} 个会话 · {fmtTime(p.updatedAt)}</div>
      </button>
    {/each}
    {#if projects.length === 0}<div class="empty">没有找到 Claude Code 会话</div>{/if}
  </div>

  <div class="col sessions">
    <input class="filter" placeholder="搜索会话标题…" bind:value={filter} />
    {#each shownSessions as s}
      <button class="item" class:active={s.id === curSession} on:click={() => selectSession(s.id)}>
        <div class="item-title">{s.title || "(无标题)"}</div>
        <div class="item-sub">{s.messageCount} 条 · {fmtTime(s.updatedAt)}</div>
      </button>
    {/each}
    {#if curProject && shownSessions.length === 0}<div class="empty">无会话</div>{/if}
  </div>

  <div class="col transcript">
    {#if loading}
      <div class="empty">加载中…</div>
    {:else if messages.length}
      {#each messages as m}
        <div class="msg {m.role}">
          <span class="who">{m.role === "user" ? "›" : "●"}</span>
          <div class="text selectable">{m.text}</div>
        </div>
      {/each}
    {:else}
      <div class="empty">选择左侧的会话查看内容</div>
    {/if}
  </div>
</div>

<style>
  .browser {
    display: flex;
    height: 100%;
    font-family: var(--font-mono);
  }
  .col {
    overflow-y: auto;
    border-right: 1px solid var(--border);
  }
  .projects { flex: 0 0 240px; }
  .sessions { flex: 0 0 260px; }
  .transcript { flex: 1; padding: 16px; min-width: 0; }
  .col-head {
    padding: 8px 12px;
    font-size: 11px;
    color: var(--text-muted);
    border-bottom: 1px solid var(--border);
  }
  .filter {
    width: 100%;
    background: var(--bg-base);
    border: none;
    border-bottom: 1px solid var(--border);
    color: var(--text-primary);
    font-family: var(--font-mono);
    font-size: 12px;
    padding: 8px 12px;
    outline: none;
  }
  .item {
    display: block;
    width: 100%;
    text-align: left;
    background: transparent;
    border: none;
    border-bottom: 1px solid var(--border);
    padding: 8px 12px;
    color: var(--text-primary);
  }
  .item:hover { background: var(--bg-elevated); }
  .item.active { background: var(--bg-elevated); box-shadow: inset 2px 0 0 var(--accent); }
  .item-title {
    font-size: 12.5px;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .item-sub { font-size: 11px; color: var(--text-muted); margin-top: 2px; }
  .empty { padding: 16px; color: var(--text-muted); font-size: 12px; }
  .msg { display: flex; gap: 8px; padding: 6px 0; }
  .msg .who { flex: 0 0 14px; text-align: center; color: var(--text-muted); }
  .msg.user .who { color: var(--accent); }
  .msg .text {
    white-space: pre-wrap;
    word-break: break-word;
    font-size: 13px;
    line-height: 1.55;
    max-width: 820px;
  }
</style>

