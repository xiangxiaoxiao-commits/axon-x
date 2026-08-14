<script lang="ts">
  // Browse Claude Code's saved sessions: projects -> sessions -> transcript.
  // These live on disk under ~/.claude and are never lost when a tab closes.
  import {
    ListClaudeSessions, ReadClaudeSession, ClaudeSessionProgress, ResumeCommand,
    SessionDistilledKnowledge, ExcludeObservation, UnexcludeObservation,
  } from "../../wailsjs/go/main/App.js";
  import type { claudedata, main } from "../../wailsjs/go/models";
  import { currentProject, activeView, resumeRequest } from "../lib/stores";

  let sessions: claudedata.SessionMeta[] = [];
  let messages: claudedata.SessionMessage[] = [];
  let progress: claudedata.SessionProgress | null = null;
  let knowledge: main.SessionKnowledge | null = null; // what this session distilled
  let curSession = "";
  let curCwd = "";
  let filter = "";
  let loading = false;
  // Active model filter: "" = all models. Set from the top dropdown.
  let modelFilter = "";

  function fmtTime(ms: number): string {
    if (!ms) return "";
    return new Date(ms).toLocaleString("zh-CN", { month: "2-digit", day: "2-digit", hour: "2-digit", minute: "2-digit" });
  }

  // modelFamily reduces a raw model id (e.g. "claude-opus-4-8",
  // "claude-3-5-sonnet-20241022") to a short family label for grouping/badges.
  // Unknown ids fall back to a trimmed form so nothing is silently dropped.
  function modelFamily(id: string): string {
    if (!id) return "未知";
    const s = id.toLowerCase();
    if (s.includes("opus")) return "Opus";
    if (s.includes("sonnet")) return "Sonnet";
    if (s.includes("haiku")) return "Haiku";
    if (s.includes("gpt")) return "GPT";
    if (s.includes("gemini")) return "Gemini";
    return id.replace(/^claude-/, "").slice(0, 12);
  }

  // Stable accent per family so a badge's color is recognizable at a glance.
  function modelColor(fam: string): string {
    switch (fam) {
      case "Opus": return "#a371f7";
      case "Sonnet": return "#3fb950";
      case "Haiku": return "#d29922";
      case "GPT": return "#2f81f7";
      case "Gemini": return "#db61a2";
      default: return "var(--text-muted)";
    }
  }

  // Time bucket for grouping: 今天 / 昨天 / 近 7 天 / 更早. Buckets are computed
  // against local midnight so "今天" means calendar-today, not last-24h.
  function timeBucket(ms: number): string {
    if (!ms) return "更早";
    const now = new Date();
    const startOfToday = new Date(now.getFullYear(), now.getMonth(), now.getDate()).getTime();
    const day = 86400000;
    if (ms >= startOfToday) return "今天";
    if (ms >= startOfToday - day) return "昨天";
    if (ms >= startOfToday - 7 * day) return "近 7 天";
    return "更早";
  }
  const BUCKET_ORDER = ["今天", "昨天", "近 7 天", "更早"];

  // Reload the session list whenever the global project changes.
  let loadedFor = " ";
  $: if ($currentProject !== loadedFor) { loadedFor = $currentProject; loadSessions(); }

  async function loadSessions() {
    curSession = ""; messages = []; progress = null; sessions = [];
    if (!$currentProject) return; // "all projects" has no single session list
    try { sessions = await ListClaudeSessions($currentProject); } catch (e) { console.error(e); }
  }

  async function selectSession(s: claudedata.SessionMeta) {
    curSession = s.id; curCwd = s.cwd; loading = true; messages = []; progress = null; knowledge = null;
    try {
      // Progress first (cheap tail) so the "where I left off" card shows fast,
      // then the distilled knowledge, then the full transcript.
      progress = await ClaudeSessionProgress($currentProject, s.id);
      knowledge = await SessionDistilledKnowledge($currentProject, s.id);
      messages = await ReadClaudeSession($currentProject, s.id);
    } catch (e) { console.error(e); }
    finally { loading = false; }
  }

  // Toggle whether one distilled fact is excluded from the graph. Updates the
  // local view optimistically, then persists + rebuilds via the backend.
  async function toggleExclude(entityName: string, obs: main.DistilledObservation) {
    const next = !obs.excluded;
    obs.excluded = next; knowledge = knowledge; // trigger reactivity
    try {
      if (next) await ExcludeObservation($currentProject, entityName, obs.text);
      else await UnexcludeObservation($currentProject, entityName, obs.text);
    } catch (e) {
      obs.excluded = !next; knowledge = knowledge; // revert on failure
      console.error(e);
    }
  }

  let resumeErr = "";

  // Open the session in a NEW in-app terminal tab running
  // `cd <cwd> && claude --resume <id>`. Multiple resumes = multiple tabs.
  async function resume(s: claudedata.SessionMeta, e: Event) {
    e.stopPropagation(); // don't also trigger selectSession
    resumeErr = "";
    try {
      const cmd = await ResumeCommand(s.cwd, s.id);
      const title = s.title ? s.title.slice(0, 20) : s.id.slice(0, 8);
      resumeRequest.set({ title, cmd });
      activeView.set("terminal");
    } catch (err) {
      resumeErr = String(err);
      console.error(err);
    }
  }

  // Tail of the last assistant reply, so the card previews the ending, not a
  // wall of text.
  function tail(text: string, n = 600): string {
    const t = (text || "").trim();
    return t.length > n ? "…" + t.slice(-n) : t;
  }

  // Distinct model families present in the current project, for the filter
  // dropdown (with a count each). Ordered by frequency, most-used first.
  $: modelOptions = (() => {
    const counts = new Map<string, number>();
    for (const s of sessions) {
      const fam = modelFamily(s.model);
      counts.set(fam, (counts.get(fam) || 0) + 1);
    }
    return [...counts.entries()].sort((a, b) => b[1] - a[1]);
  })();

  // Apply title search + model filter, then keep newest-first (backend already
  // sorts by updatedAt desc, so a stable filter preserves that order).
  $: shownSessions = sessions.filter((s) => {
    if (filter && !(s.title || "").toLowerCase().includes(filter.toLowerCase())) return false;
    if (modelFilter && modelFamily(s.model) !== modelFilter) return false;
    return true;
  });

  // Group the shown sessions into time buckets, dropping empty buckets. Within a
  // bucket, newest-first is inherited from shownSessions.
  $: grouped = BUCKET_ORDER
    .map((label) => ({ label, items: shownSessions.filter((s) => timeBucket(s.updatedAt) === label) }))
    .filter((g) => g.items.length > 0);
</script>

<div class="browser">
  <div class="col sessions">
    <input class="filter" placeholder="搜索会话标题…" bind:value={filter} />
    {#if $currentProject && modelOptions.length > 1}
      <div class="model-filter">
        <button class="mf-chip" class:on={modelFilter === ""} on:click={() => (modelFilter = "")}>全部</button>
        {#each modelOptions as [fam, n]}
          <button class="mf-chip" class:on={modelFilter === fam}
            style="--mc:{modelColor(fam)}" on:click={() => (modelFilter = modelFilter === fam ? "" : fam)}>
            {fam} <span class="mf-n">{n}</span>
          </button>
        {/each}
      </div>
    {/if}
    {#each grouped as grp (grp.label)}
      <div class="group-head">{grp.label} <span class="gh-n">{grp.items.length}</span></div>
      {#each grp.items as s (s.id)}
        <div class="item" class:active={s.id === curSession}>
          <button class="item-main" on:click={() => selectSession(s)}>
            <div class="item-title">{s.title || "(无标题)"}</div>
            <div class="item-sub">
              <span class="badge" style="--mc:{modelColor(modelFamily(s.model))}">{modelFamily(s.model)}</span>
              {s.messageCount} 条 · {fmtTime(s.updatedAt)}
            </div>
          </button>
          <button class="resume" title="在 app 内新终端标签恢复此会话" on:click={(e) => resume(s, e)}>▶ 恢复</button>
        </div>
      {/each}
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
          <button class="resume sm" title="在 app 内新终端标签恢复此会话" on:click={(e) => { const s = sessions.find((x) => x.id === curSession); if (s) resume(s, e); }}>▶ 恢复</button>
        </div>
        {#if resumeErr}<div class="pc-err">恢复失败：{resumeErr}</div>{/if}
        {#if progress.lastUser}
          <div class="pc-block"><span class="pc-role user">你</span><div class="pc-text selectable">{progress.lastUser}</div></div>
        {/if}
        {#if progress.lastAssistant}
          <div class="pc-block"><span class="pc-role asst">AI</span><div class="pc-text selectable">{tail(progress.lastAssistant)}</div></div>
        {/if}
      </div>
    {/if}

    {#if curSession && knowledge}
      <div class="kn-card">
        <div class="kn-head">
          <span class="kn-label">🧠 本会话产出的知识</span>
          {#if !knowledge.indexed}
            <span class="kn-hint">未建索引，此会话还没被总结</span>
          {:else if knowledge.entities.length === 0}
            <span class="kn-hint">这次没有总结出知识</span>
          {/if}
        </div>
        {#each knowledge.entities as ent}
          <div class="kn-ent">
            <div class="kn-ent-name">{ent.name}<span class="kn-ent-type">{ent.type}</span></div>
            {#each ent.observations as obs}
              <div class="kn-obs" class:excluded={obs.excluded}>
                <span class="kn-obs-text selectable">{obs.text}</span>
                <button class="kn-btn" title={obs.excluded ? "恢复这条知识" : "不要这条知识（从图谱剔除，重建也不会回来）"}
                  on:click={() => toggleExclude(ent.name, obs)}>
                  {obs.excluded ? "↩ 恢复" : "✕ 剔除"}
                </button>
              </div>
            {/each}
          </div>
        {/each}
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
  .item-sub {
    font-size: 11px;
    color: var(--text-muted);
    margin-top: 3px;
    display: flex;
    align-items: center;
    gap: 6px;
  }
  .badge {
    flex: 0 0 auto;
    font-size: 10px;
    line-height: 1;
    padding: 2px 5px;
    border-radius: var(--radius-control);
    color: var(--mc);
    border: 1px solid color-mix(in srgb, var(--mc) 45%, transparent);
    background: color-mix(in srgb, var(--mc) 12%, transparent);
  }
  .model-filter {
    display: flex;
    flex-wrap: wrap;
    gap: 5px;
    padding: 8px 10px;
    border-bottom: 1px solid var(--border);
  }
  .mf-chip {
    font-family: var(--font-mono);
    font-size: 11px;
    padding: 3px 8px;
    border-radius: var(--radius-control);
    border: 1px solid var(--border);
    background: transparent;
    color: var(--text-muted);
    cursor: pointer;
    transition: all .12s;
  }
  .mf-chip:hover { color: var(--text-primary); }
  .mf-chip.on {
    color: var(--mc, var(--accent));
    border-color: color-mix(in srgb, var(--mc, var(--accent)) 55%, transparent);
    background: color-mix(in srgb, var(--mc, var(--accent)) 14%, transparent);
  }
  .mf-n { opacity: .6; }
  .group-head {
    position: sticky;
    top: 0;
    z-index: 1;
    padding: 6px 12px;
    font-size: 11px;
    font-weight: 600;
    letter-spacing: .5px;
    color: var(--text-muted);
    background: var(--bg-base);
    border-bottom: 1px solid var(--border);
  }
  .gh-n { opacity: .5; font-weight: 400; }
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
  .pc-err { font-size: 11.5px; color: #f85149; margin-bottom: 6px; }

  .kn-card {
    border: 1px solid var(--border); border-radius: 8px;
    background: var(--bg-elevated); padding: 12px 14px; margin-bottom: 16px;
  }
  .kn-head { display: flex; align-items: baseline; gap: 10px; margin-bottom: 8px; }
  .kn-label { font-size: 12.5px; font-weight: 600; }
  .kn-hint { font-size: 11.5px; color: var(--text-muted); }
  .kn-ent { margin: 8px 0; }
  .kn-ent-name { font-size: 12.5px; font-weight: 600; margin-bottom: 3px; }
  .kn-ent-type {
    font-size: 10.5px; color: var(--text-muted); font-weight: 400;
    margin-left: 6px; padding: 1px 5px; border: 1px solid var(--border); border-radius: 4px;
  }
  .kn-obs {
    display: flex; align-items: center; gap: 8px;
    padding: 4px 0 4px 10px; border-left: 2px solid var(--border);
  }
  .kn-obs-text { flex: 1; font-size: 12.5px; line-height: 1.5; }
  .kn-obs.excluded .kn-obs-text { text-decoration: line-through; color: var(--text-muted); }
  .kn-btn {
    flex: 0 0 auto; font-size: 11px; padding: 2px 8px;
    background: transparent; border: 1px solid var(--border); border-radius: 4px;
    color: var(--text-muted); cursor: pointer; opacity: 0; transition: opacity .12s;
  }
  .kn-obs:hover .kn-btn { opacity: 1; }
  .kn-obs.excluded .kn-btn { opacity: 1; color: var(--accent); border-color: var(--accent); }
  .kn-btn:hover { border-color: #f85149; color: #f85149; }
  .kn-obs.excluded .kn-btn:hover { border-color: var(--accent); color: var(--accent); }
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

