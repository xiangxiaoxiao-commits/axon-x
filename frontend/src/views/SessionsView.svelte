<script lang="ts">
  // Browse Claude Code's saved sessions: projects -> sessions -> transcript.
  // These live on disk under ~/.claude and are never lost when a tab closes.
  import {
    ListClaudeSessions, ReadClaudeSession, ClaudeSessionProgress, ResumeCommand,
  } from "../../wailsjs/go/main/App.js";
  import type { claudedata } from "../../wailsjs/go/models";
  import { currentProject, activeView, pendingResume } from "../lib/stores";

  let sessions: claudedata.SessionMeta[] = [];
  let messages: claudedata.SessionMessage[] = [];
  let progress: claudedata.SessionProgress | null = null;
  let curSession = "";
  let curCwd = "";
  let filter = "";
  let loading = false;

  function fmtTime(ms: number): string {
    if (!ms) return "";
    return new Date(ms).toLocaleString("zh-CN", { month: "2-digit", day: "2-digit", hour: "2-digit", minute: "2-digit" });
  }

  // Reload the session list whenever the global project changes.
  let loadedFor = " ";
  $: if ($currentProject !== loadedFor) { loadedFor = $currentProject; loadSessions(); }

  async function loadSessions() {
    curSession = ""; messages = []; progress = null; sessions = [];
    if (!$currentProject) return; // "all projects" has no single session list
    try { sessions = await ListClaudeSessions($currentProject); } catch (e) { console.error(e); }
  }

  async function selectSession(s: claudedata.SessionMeta) {
    curSession = s.id; curCwd = s.cwd; loading = true; messages = []; progress = null;
    try {
      // Progress first (cheap tail) so the "where I left off" card shows fast,
      // then the full transcript.
      progress = await ClaudeSessionProgress($currentProject, s.id);
      messages = await ReadClaudeSession($currentProject, s.id);
    } catch (e) { console.error(e); }
    finally { loading = false; }
  }

  // Hand the ready-to-run command to the terminal and switch to it. The
  // terminal injects it once its shell is ready.
  async function resume(s: claudedata.SessionMeta, e: Event) {
    e.stopPropagation(); // don't also trigger selectSession
    try {
      pendingResume.set(await ResumeCommand(s.cwd, s.id));
      activeView.set("terminal");
    } catch (err) { console.error(err); }
  }

  // Tail of the last assistant reply, so the card previews the ending, not a
  // wall of text.
  function tail(text: string, n = 600): string {
    const t = (text || "").trim();
    return t.length > n ? "…" + t.slice(-n) : t;
  }

  $: shownSessions = filter
    ? sessions.filter((s) => (s.title || "").toLowerCase().includes(filter.toLowerCase()))
    : sessions;
</script>

<div class="browser">
  <div class="col sessions">
    <input class="filter" placeholder="搜索会话标题…" bind:value={filter} />
    {#each shownSessions as s}
      <div class="item" class:active={s.id === curSession}>
        <button class="item-main" on:click={() => selectSession(s)}>
          <div class="item-title">{s.title || "(无标题)"}</div>
          <div class="item-sub">{s.messageCount} 条 · {fmtTime(s.updatedAt)}</div>
        </button>
        <button class="resume" title="在终端恢复此会话" on:click={(e) => resume(s, e)}>▶ 恢复</button>
      </div>
    {/each}
    {#if !$currentProject}<div class="empty">在顶部选择一个项目查看它的会话</div>
    {:else if shownSessions.length === 0}<div class="empty">无会话</div>{/if}
  </div>

  <div class="col transcript">
    {#if curSession && progress}
      <div class="progress-card">
        <div class="pc-head">
          <span class="pc-label">最后进度</span>
          {#if curCwd}<span class="pc-cwd selectable">{curCwd}</span>{/if}
          <button class="resume sm" on:click={(e) => resume(sessions.find((x) => x.id === curSession), e)}>▶ 恢复</button>
        </div>
        {#if progress.lastUser}
          <div class="pc-block"><span class="pc-role user">你</span><div class="pc-text selectable">{progress.lastUser}</div></div>
        {/if}
        {#if progress.lastAssistant}
          <div class="pc-block"><span class="pc-role asst">AI</span><div class="pc-text selectable">{tail(progress.lastAssistant)}</div></div>
        {/if}
      </div>
    {/if}
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
  .sessions { flex: 0 0 300px; }
  .transcript { flex: 1; padding: 16px; min-width: 0; }
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
    display: flex;
    align-items: center;
    gap: 6px;
    width: 100%;
    border-bottom: 1px solid var(--border);
    color: var(--text-primary);
  }
  .item:hover { background: var(--bg-elevated); }
  .item:hover .resume { opacity: 1; }
  .item.active { background: var(--bg-elevated); box-shadow: inset 2px 0 0 var(--accent); }
  .item-main {
    flex: 1;
    min-width: 0;
    text-align: left;
    background: transparent;
    border: none;
    padding: 8px 12px;
    color: inherit;
  }
  .resume {
    flex: 0 0 auto;
    margin-right: 10px;
    background: transparent;
    border: 1px solid var(--border);
    border-radius: var(--radius-control);
    color: var(--accent);
    font-family: var(--font-mono);
    font-size: 11px;
    padding: 3px 8px;
    opacity: 0;
    transition: opacity .12s;
  }
  .resume:hover { background: var(--accent); color: var(--bg-base); border-color: var(--accent); }
  .resume.sm { opacity: 1; margin-right: 0; }
  .item-title {
    font-size: 12.5px;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .item-sub { font-size: 11px; color: var(--text-muted); margin-top: 2px; }
  .empty { padding: 16px; color: var(--text-muted); font-size: 12px; }
  .progress-card {
    border: 1px solid var(--border);
    border-radius: var(--radius-control);
    background: var(--bg-surface);
    padding: 10px 12px;
    margin-bottom: 16px;
  }
  .pc-head { display: flex; align-items: center; gap: 8px; margin-bottom: 8px; }
  .pc-label { font-size: 11px; font-weight: 600; color: var(--accent); letter-spacing: .5px; }
  .pc-cwd { flex: 1; min-width: 0; font-size: 11px; color: var(--text-muted); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
  .pc-block { display: flex; gap: 8px; padding: 4px 0; }
  .pc-role { flex: 0 0 22px; font-size: 11px; text-align: center; padding-top: 1px; }
  .pc-role.user { color: var(--accent); }
  .pc-role.asst { color: var(--text-muted); }
  .pc-text {
    white-space: pre-wrap;
    word-break: break-word;
    font-size: 12.5px;
    line-height: 1.5;
    color: var(--text-primary);
    max-height: 160px;
    overflow-y: auto;
  }
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

