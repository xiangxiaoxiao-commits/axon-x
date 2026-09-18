<script lang="ts">
  // Browse Claude Code's saved sessions: projects -> sessions -> transcript.
  // These live on disk under ~/.claude and are never lost when a tab closes.
  import {
    ListClaudeSessions, ReadClaudeSession, ClaudeSessionProgress, ResumeCommand,
    SessionDistilledKnowledge, ExcludeObservation, UnexcludeObservation,
    EditObservation, ExcludeSession, UnexcludeSession,
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
  // The list's reach. Empty means "follow the project selected in the header",
  // which is a namespace; the chips below can widen it to the sessions no
  // namespace claims, or to everything.
  let scopeOverride = "";
  let scopeOptions: main.ScopeOption[] = [];
  let listTotal = 0;
  let listUnassigned = 0;
  // Guards against an older request landing after a newer one: switching scope
  // or session twice in quick succession must not let the first reply win.
  let loadToken = 0;
  // Loading the session list is not instant (a few hundred transcripts are read
  // from disk), so the list needs its own flag: without it the page claimed
  // "无会话" for several seconds, which reads as a broken feature.
  let sessionsLoading = false;
  let sessionsLoaded = false;
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
    if (s.includes("gpt") || s === "codex") return "GPT";
    if (s.includes("gemini")) return "Gemini";
    if (s === "workbuddy") return "WorkBuddy";
    if (s === "codebuddy") return "CodeBuddy";
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

  // The effective scope: the header project unless a chip overrides it.
  $: scope = scopeOverride || $currentProject;

  // Switching the project in the header is the primary control, so it drops any
  // temporary widening — the page follows the header again. Written without
  // reading scopeOverride so this cannot become a cyclic dependency.
  let lastProject = "";
  $: if ($currentProject !== lastProject) { lastProject = $currentProject; scopeOverride = ""; }

  // Reload the session list whenever the effective scope changes.
  let loadedFor = " ";
  $: if (scope !== loadedFor) { loadedFor = scope; loadSessions(); }

  async function loadSessions() {
    const mine = ++loadToken;
    curSession = ""; messages = []; progress = null; sessions = [];
    // A model filter chosen for one scope rarely means anything in the next one;
    // keeping it would show an empty list that reads as a broken page.
    modelFilter = "";
    if (!scope) return; // nothing selected in the header and no override
    sessionsLoading = true;
    try {
      const res = await ListClaudeSessions(scope);
      if (mine !== loadToken) return; // a newer load already replaced this one
      sessions = res?.sessions || [];
      scopeOptions = res?.scopes || [];
      listTotal = res?.total || 0;
      listUnassigned = res?.unassigned || 0;
    } catch (e) {
      console.error(e);
      if (mine !== loadToken) return;
      sessions = [];
    } finally {
      if (mine === loadToken) {
        sessionsLoading = false;
        sessionsLoaded = true;
      }
    }
  }

  async function selectSession(s: claudedata.SessionMeta) {
    const id = s.id;
    // Only the newest selection may write back: clicking twice quickly used to
    // leave the first session's progress card under the second session's title.
    const fresh = () => curSession === id;
    curSession = id; curCwd = s.cwd; loading = true; messages = []; progress = null; knowledge = null;
    // Progress is a tail read now (milliseconds), so there is no reason to
    // serialise it: ask for both side panels at once and read the transcript
    // while they land.
    const side = Promise.all([
      ClaudeSessionProgress(s.projectSlug, id).then((p) => { if (fresh()) progress = p; }),
      SessionDistilledKnowledge($currentProject, id).then((k) => { if (fresh()) knowledge = k; }),
    ]).catch((e) => console.error(e));
    try {
      const msgs = (await ReadClaudeSession(s.projectSlug, id)) || [];
      if (fresh()) messages = msgs;
    } catch (e) { console.error(e); }
    finally { if (fresh()) loading = false; }
    await side;
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

  // Inline edit state: which fact is being edited + its draft text.
  let editingKey = "";
  let editDraft = "";
  const factKey = (entName: string, i: number) => `${entName}::${i}`;

  function startEdit(entName: string, i: number, obs: main.DistilledObservation) {
    editingKey = factKey(entName, i);
    editDraft = obs.text;
  }
  function cancelEdit() { editingKey = ""; editDraft = ""; }

  // Save a corrected fact: rewrites it in the session cache (survives re-index).
  async function saveEdit(entityName: string, obs: main.DistilledObservation) {
    const next = editDraft.trim();
    if (!next || next === obs.text) { cancelEdit(); return; }
    const old = obs.text;
    obs.text = next; knowledge = knowledge; // optimistic
    cancelEdit();
    try {
      await EditObservation($currentProject, curSession, entityName, old, next);
    } catch (e) {
      obs.text = old; knowledge = knowledge; // revert
      console.error(e);
    }
  }

  // Toggle excluding the WHOLE session (all its knowledge is off-topic).
  async function toggleSessionExclude() {
    if (!knowledge) return;
    const next = !knowledge.sessionExcluded;
    knowledge.sessionExcluded = next; knowledge = knowledge;
    try {
      if (next) await ExcludeSession($currentProject, curSession);
      else await UnexcludeSession($currentProject, curSession);
    } catch (e) {
      knowledge.sessionExcluded = !next; knowledge = knowledge;
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

  // Group by working directory (cwd). Sessions without cwd go into "(未知目录)".
  // Within each group, newest-first is inherited from shownSessions.
  $: grouped = (() => {
    const map = new Map<string, typeof shownSessions>();
    for (const s of shownSessions) {
      const dir = s.cwd || "(未知目录)";
      // Shorten display: use last 2 path segments.
      if (!map.has(dir)) map.set(dir, []);
      map.get(dir)!.push(s);
    }
    // Sort groups: most recently active first.
    return [...map.entries()]
      .map(([dir, items]) => ({ label: shortDir(dir), fullPath: dir, items }))
      .sort((a, b) => (b.items[0]?.updatedAt || 0) - (a.items[0]?.updatedAt || 0));
  })();

  function shortDir(dir: string): string {
    if (dir === "(未知目录)") return dir;
    const parts = dir.replace(/\\/g, "/").split("/").filter(Boolean);
    return parts.length <= 2 ? dir : "…/" + parts.slice(-2).join("/");
  }
</script>

<div class="browser">
  <div class="col sessions">
    <input class="filter" placeholder="搜索会话标题…" bind:value={filter} />
    {#if scopeOptions.length > 1 || scopeOverride}
      <div class="scope-bar">
        {#each scopeOptions as o}
          <button class="sc-chip" class:on={o.key === scope} on:click={() => (scopeOverride = o.key === $currentProject ? "" : o.key)}>
            {o.label} <span class="sc-n">{o.count}</span>
          </button>
        {/each}
      </div>
    {/if}
    {#if $currentProject && modelOptions.length > 1}
      <div class="model-filter">
        <button class="mf-chip" class:on={modelFilter === ""} on:click={() => (modelFilter = "")}>
          全部 <span class="mf-n">{sessions.length}</span>
        </button>
        {#each modelOptions as [fam, n]}
          <button class="mf-chip" class:on={modelFilter === fam}
            style="--mc:{modelColor(fam)}" on:click={() => (modelFilter = modelFilter === fam ? "" : fam)}>
            {fam} <span class="mf-n">{n}</span>
          </button>
        {/each}
      </div>
    {/if}
    {#each grouped as grp (grp.fullPath)}
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
    {#if !scope}
      <div class="empty">在顶部选择一个项目查看它的会话</div>
    {:else if sessionsLoading}
      <div class="empty">正在读取会话…<span class="empty-sub">首次需要几秒（本机 {listTotal || "数百"} 个会话）；之后会立刻打开</span></div>
    {:else if shownSessions.length === 0}
      <div class="empty">
        {#if filter || modelFilter}
          没有匹配的会话<span class="empty-sub">当前范围共 {sessions.length} 个会话，试试清空搜索或切回「全部」模型</span>
        {:else if scope === "*"}
          无会话<span class="empty-sub">没读到任何 {modelFamily("codex")}/Claude 会话记录</span>
        {:else if scope === "@unassigned"}
          没有未归属的会话<span class="empty-sub">本机 {listTotal} 个会话都能对应到某个命名空间</span>
        {:else if sessionsLoaded}
          这个项目没有会话<span class="empty-sub">该命名空间下没读到会话；点上方「全部」可以看到本机全部 {listTotal} 个会话，其中 {listUnassigned} 个不属于任何命名空间</span>
        {:else}
          正在读取会话…
        {/if}
      </div>
    {/if}
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
      <div class="kn-card" class:session-excluded={knowledge.sessionExcluded}>
        <div class="kn-head">
          <span class="kn-label">🧠 本会话产出的知识</span>
          {#if !knowledge.indexed}
            <span class="kn-hint">未建索引，此会话还没被总结</span>
          {:else if knowledge.entities.length === 0}
            <span class="kn-hint">这次没有总结出知识</span>
          {:else}
            <button class="kn-sess-btn" class:on={knowledge.sessionExcluded}
              title={knowledge.sessionExcluded ? "恢复整个会话的知识" : "整段对话无用，剔除本会话全部知识（重建也不回来）"}
              on:click={toggleSessionExclude}>
              {knowledge.sessionExcluded ? "↩ 恢复整个会话" : "🗑 整会话剔除"}
            </button>
          {/if}
        </div>
        {#if knowledge.sessionExcluded}
          <div class="kn-sess-note">整个会话已剔除，以下知识都不会进入图谱。</div>
        {/if}
        {#each knowledge.entities as ent}
          <div class="kn-ent">
            <div class="kn-ent-name">{ent.name}<span class="kn-ent-type">{ent.type}</span></div>
            {#each ent.observations as obs, i}
              <div class="kn-obs" class:excluded={obs.excluded || knowledge.sessionExcluded}>
                {#if editingKey === factKey(ent.name, i)}
                  <textarea class="kn-edit" bind:value={editDraft} rows="2"
                    on:keydown={(e) => { if (e.key === "Escape") cancelEdit(); if (e.key === "Enter" && (e.metaKey || e.ctrlKey)) saveEdit(ent.name, obs); }}></textarea>
                  <button class="kn-btn ok" on:click={() => saveEdit(ent.name, obs)}>✓ 保存</button>
                  <button class="kn-btn" on:click={cancelEdit}>取消</button>
                {:else}
                  <span class="kn-obs-text selectable">{obs.text}</span>
                  <button class="kn-btn" title="修正这条知识的内容" disabled={knowledge.sessionExcluded}
                    on:click={() => startEdit(ent.name, i, obs)}>✎ 修改</button>
                  <button class="kn-btn" title={obs.excluded ? "恢复这条知识" : "不要这条知识（从图谱剔除，重建也不回来）"}
                    disabled={knowledge.sessionExcluded}
                    on:click={() => toggleExclude(ent.name, obs)}>
                    {obs.excluded ? "↩ 恢复" : "✕ 剔除"}
                  </button>
                {/if}
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
  .scope-bar {
    display: flex;
    flex-wrap: wrap;
    gap: 5px;
    padding: 8px 10px;
    border-bottom: 1px solid var(--border);
  }
  .sc-chip {
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
  .sc-chip:hover { color: var(--text-primary); }
  .sc-chip.on {
    color: var(--accent);
    border-color: color-mix(in srgb, var(--accent) 55%, transparent);
    background: color-mix(in srgb, var(--accent) 14%, transparent);
  }
  .sc-n { opacity: .6; }
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
  .empty-sub { display: block; margin-top: 6px; font-size: 12px; opacity: .7; }

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
  .kn-obs.excluded .kn-btn { opacity: 1; }
  .kn-btn:hover { border-color: #f85149; color: #f85149; }
  .kn-btn.ok:hover { border-color: var(--accent); color: var(--accent); }
  .kn-btn:disabled { opacity: .3; pointer-events: none; }
  .kn-edit {
    flex: 1; font-family: var(--font-mono); font-size: 12.5px; line-height: 1.5;
    background: var(--bg-base); color: var(--text-primary);
    border: 1px solid var(--accent); border-radius: 4px; padding: 4px 6px; resize: vertical;
  }
  .kn-sess-btn {
    margin-left: auto; font-size: 11px; padding: 2px 8px;
    background: transparent; border: 1px solid var(--border); border-radius: 4px;
    color: var(--text-muted); cursor: pointer;
  }
  .kn-sess-btn:hover { border-color: #f85149; color: #f85149; }
  .kn-sess-btn.on { color: var(--accent); border-color: var(--accent); }
  .kn-sess-note { font-size: 11.5px; color: #f85149; margin: 4px 0 8px; }
  .kn-card.session-excluded { opacity: .7; }
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
