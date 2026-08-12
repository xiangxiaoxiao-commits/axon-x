<script lang="ts">
  // Task orchestration main view. Three columns in spirit: a task list (left)
  // and a status-driven detail panel (center/right). Multiple tasks can run in
  // parallel; the list reflects each task's live status via task:* events, and
  // switching the detail selection never touches background work.
  import { onMount, onDestroy, tick } from "svelte";
  import { marked } from "marked";
  import { currentProject, projects } from "../lib/stores";
  import type { task } from "../../wailsjs/go/models";
  import {
    ListTasks, GetTask, CreateTask, EnrichTask, UpdateSpec, RunTask,
    ReviewTask, CancelTask, DeleteTask, ListTaskRuns,
    ListProviders, ListModels,
  } from "../../wailsjs/go/main/App.js";
  import { EventsOn, EventsOff } from "../../wailsjs/runtime/runtime.js";

  // --- List + selection -----------------------------------------------------
  let tasks: task.Task[] = [];
  let selectedId = "";
  let creating = false; // showing the "new task" draft form
  $: selected = tasks.find((t) => t.id === selectedId) ?? null;

  // --- Model / provider picker ----------------------------------------------
  let providers: { name: string; hasKey: boolean }[] = [];
  let models: string[] = [];
  // Draft form fields (new task).
  let draftInput = "";
  let draftProvider = "";
  let draftModel = "";
  let draftProject = "";

  // --- Editable spec (review_spec) ------------------------------------------
  type EditSpec = {
    goal: string; background: string;
    constraints: string[]; scope: string[];
    acceptCriteria: string[]; missedPoints: string[]; steps: string[];
  };
  let spec: EditSpec = emptySpec();
  let specTaskId = ""; // which task the form currently holds
  let specSaved = true;
  const listFields: { key: keyof EditSpec; label: string; hint?: boolean }[] = [
    { key: "constraints", label: "约束" },
    { key: "scope", label: "涉及范围" },
    { key: "acceptCriteria", label: "验收标准" },
    { key: "missedPoints", label: "易遗漏点", hint: true },
    { key: "steps", label: "建议步骤" },
  ];

  // --- Execution streaming (per task, so parallel runs don't clobber) -------
  let streamBuf: Record<string, string> = {};

  // --- Result review ---------------------------------------------------------
  let runs: task.Run[] = [];
  let viewRunId: number | null = null;
  let showFeedback = false;
  let feedback = "";
  let busy = false;

  function emptySpec(): EditSpec {
    return { goal: "", background: "", constraints: [], scope: [], acceptCriteria: [], missedPoints: [], steps: [] };
  }

  // --- Loading ---------------------------------------------------------------
  async function refresh() {
    try { tasks = await ListTasks(); } catch (e) { console.error("list tasks", e); }
  }

  async function loadModels() {
    try {
      const provs = await ListProviders();
      providers = provs.map((p) => ({ name: p.name, hasKey: p.hasKey }));
      const usable = provs.find((p) => p.hasKey) ?? provs[0];
      if (usable) {
        draftProvider = usable.name;
        models = await ListModels(usable.name);
        if (models.length) draftModel = models[0];
      }
    } catch (e) { console.error("load models", e); }
  }

  async function onDraftProvider() {
    try { models = await ListModels(draftProvider); draftModel = models[0] ?? ""; }
    catch { models = []; draftModel = ""; }
  }

  function loadSpecInto(t: task.Task) {
    const s = t.spec ?? ({} as task.Spec);
    spec = {
      goal: s.goal ?? "", background: s.background ?? "",
      constraints: [...(s.constraints ?? [])], scope: [...(s.scope ?? [])],
      acceptCriteria: [...(s.acceptCriteria ?? [])], missedPoints: [...(s.missedPoints ?? [])],
      steps: [...(s.steps ?? [])],
    };
    specTaskId = t.id; specSaved = true;
  }

  async function loadRuns(taskID: string) {
    try {
      runs = await ListTaskRuns(taskID);
      const done = [...runs].reverse().find((r) => r.status === "done") ?? runs[runs.length - 1];
      viewRunId = done ? done.id : null;
    } catch (e) { console.error("list runs", e); runs = []; viewRunId = null; }
  }

  $: viewedRun = runs.find((r) => r.id === viewRunId) ?? null;

  // --- Selection -------------------------------------------------------------
  async function select(id: string) {
    creating = false;
    selectedId = id;
    let t: task.Task | null = null;
    try { t = await GetTask(id); upsert(t); } catch (e) { console.error("get task", e); return; }
    if (t.status === "review_spec") loadSpecInto(t);
    if (t.status === "review_result" || t.status === "accepted") await loadRuns(id);
  }

  function upsert(t: task.Task) {
    const i = tasks.findIndex((x) => x.id === t.id);
    if (i >= 0) { tasks[i] = t; tasks = [...tasks]; }
    else tasks = [t, ...tasks];
  }

  function startCreate() {
    creating = true; selectedId = "";
    draftInput = "";
    draftProject = $currentProject || "";
  }

  // --- Actions ---------------------------------------------------------------
  async function enrich() {
    if (busy) return;
    busy = true;
    try {
      let id = selectedId;
      if (creating) {
        if (!draftInput.trim()) { busy = false; return; }
        const t = await CreateTask(draftInput, draftProvider, draftModel, draftProject);
        upsert(t); selectedId = t.id; id = t.id; creating = false;
      }
      await EnrichTask(id);
      const t = await GetTask(id); upsert(t);
    } catch (e) { console.error("enrich", e); } finally { busy = false; }
  }

  async function saveSpec(): Promise<boolean> {
    if (!selected) return false;
    try { await UpdateSpec(selected.id, spec as unknown as task.Spec); specSaved = true; return true; }
    catch (e) { console.error("save spec", e); return false; }
  }

  async function execute() {
    if (!selected || busy) return;
    busy = true;
    try {
      if (!(await saveSpec())) return;
      streamBuf[selected.id] = ""; streamBuf = { ...streamBuf };
      await RunTask(selected.id);
      const t = await GetTask(selected.id); upsert(t);
    } catch (e) { console.error("run", e); } finally { busy = false; }
  }

  async function cancel() {
    if (!selected) return;
    try { await CancelTask(selected.id); } catch (e) { console.error("cancel", e); }
  }

  async function accept() {
    if (!selected || busy) return;
    busy = true;
    try { await ReviewTask(selected.id, "accept", ""); const t = await GetTask(selected.id); upsert(t); }
    catch (e) { console.error("accept", e); } finally { busy = false; }
  }

  function openFeedback() { feedback = ""; showFeedback = true; }
  async function submitReject() {
    if (!selected || busy) return;
    busy = true;
    try {
      streamBuf[selected.id] = ""; streamBuf = { ...streamBuf };
      await ReviewTask(selected.id, "reject", feedback);
      showFeedback = false;
      const t = await GetTask(selected.id); upsert(t);
    } catch (e) { console.error("reject", e); } finally { busy = false; }
  }

  async function retry() {
    if (!selected || busy) return;
    busy = true;
    try {
      if (selected.failedStage === "enrich") await EnrichTask(selected.id);
      else { streamBuf[selected.id] = ""; streamBuf = { ...streamBuf }; await RunTask(selected.id); }
      const t = await GetTask(selected.id); upsert(t);
    } catch (e) { console.error("retry", e); } finally { busy = false; }
  }

  async function remove(id: string) {
    try {
      await DeleteTask(id);
      if (selectedId === id) { selectedId = ""; creating = false; }
      await refresh();
    } catch (e) { console.error("delete", e); }
  }

  // --- List field editing ----------------------------------------------------
  function addItem(key: keyof EditSpec) {
    (spec[key] as string[]) = [...(spec[key] as string[]), ""];
    spec = { ...spec }; specSaved = false;
  }
  function removeItem(key: keyof EditSpec, i: number) {
    const arr = [...(spec[key] as string[])]; arr.splice(i, 1);
    (spec[key] as string[]) = arr; spec = { ...spec }; specSaved = false;
  }
  function touchSpec() { specSaved = false; }

  // --- Events ----------------------------------------------------------------
  async function onStatus(p: any) {
    if (!p?.taskId) return;
    const i = tasks.findIndex((t) => t.id === p.taskId);
    if (i >= 0) { tasks[i] = { ...tasks[i], status: p.status } as task.Task; tasks = [...tasks]; }
    else { try { upsert(await GetTask(p.taskId)); } catch {} }
    // If the change lands on the open task, refresh the relevant panel data.
    if (p.taskId === selectedId) {
      try {
        const t = await GetTask(p.taskId); upsert(t);
        if (p.status === "review_spec") loadSpecInto(t);
        else if (p.status === "review_result" || p.status === "accepted") await loadRuns(p.taskId);
      } catch {}
    }
  }
  function onSpec(p: any) {
    if (!p?.taskId) return;
    if (p.taskId === selectedId && p.spec) {
      loadSpecInto({ ...(selected as task.Task), spec: p.spec, status: "review_spec" } as task.Task);
    }
  }
  function onDelta(p: any) {
    if (!p?.taskId) return;
    streamBuf[p.taskId] = (streamBuf[p.taskId] ?? "") + (p.delta ?? "");
    streamBuf = { ...streamBuf };
  }
  async function onDone(p: any) {
    if (!p?.taskId) return;
    if (p.taskId === selectedId) { try { upsert(await GetTask(p.taskId)); await loadRuns(p.taskId); } catch {} }
  }
  async function onErr(p: any) {
    if (!p?.taskId) return;
    if (p.taskId === selectedId) { try { upsert(await GetTask(p.taskId)); } catch {} }
  }

  // --- Rendering helpers -----------------------------------------------------
  const STATUS_LABEL: Record<string, string> = {
    draft: "草稿", enriching: "补全中", review_spec: "待确认", queued: "排队中",
    executing: "执行中", review_result: "待审阅", accepted: "已采纳", failed: "失败",
  };
  function badgeClass(s: string): string {
    if (s === "executing" || s === "enriching") return "b-run";
    if (s === "review_spec" || s === "review_result") return "b-todo";
    if (s === "accepted") return "b-ok";
    if (s === "failed") return "b-err";
    return "b-idle";
  }
  function renderMd(s: string): string {
    try { return marked.parse(s || "", { async: false, breaks: true }) as string; }
    catch { return (s || "").replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;"); }
  }
  function projPath(slug: string): string { return $projects.find((p) => p.slug === slug)?.path ?? slug; }

  onMount(async () => {
    EventsOn("task:status", onStatus);
    EventsOn("task:spec", onSpec);
    EventsOn("task:delta", onDelta);
    EventsOn("task:done", onDone);
    EventsOn("task:error", onErr);
    await Promise.all([refresh(), loadModels()]);
  });
  onDestroy(() => {
    EventsOff("task:status"); EventsOff("task:spec"); EventsOff("task:delta");
    EventsOff("task:done"); EventsOff("task:error");
  });
</script>

<div class="tasks">
  <!-- Left: task list -->
  <aside class="list">
    <button class="new-btn" on:click={startCreate}>+ 新建任务</button>
    <div class="items">
      {#each tasks as t (t.id)}
        <button class="item" class:active={t.id === selectedId} on:click={() => select(t.id)}>
          <span class="title">{t.title || "(无标题)"}</span>
          <span class="badge {badgeClass(t.status)}">
            {#if t.status === "executing" || t.status === "enriching"}
              <span class="axon-signal"><i></i><i></i><i></i></span>
            {/if}
            {STATUS_LABEL[t.status] ?? t.status}
          </span>
        </button>
      {/each}
      {#if !tasks.length}
        <p class="empty">还没有任务，点上面新建一个。</p>
      {/if}
    </div>
  </aside>

  <!-- Center/right: detail panel -->
  <section class="detail">
    {#if creating || (selected && selected.status === "draft")}
      <!-- New task / draft: rough input + model + optional project -->
      <div class="pane">
        <h2>{creating ? "新建任务" : "草稿"}</h2>
        {#if creating}
          <label class="fld">
            <span class="lbl">粗略描述</span>
            <textarea class="raw" bind:value={draftInput} rows="6"
              placeholder="用一句/一段话描述任务，比如：给订单服务加个超时重试"></textarea>
          </label>
          <div class="row">
            <label class="fld inline">
              <span class="lbl">Provider</span>
              <select bind:value={draftProvider} on:change={onDraftProvider}>
                {#each providers as p}<option value={p.name}>{p.name}{p.hasKey ? "" : "（无 key）"}</option>{/each}
              </select>
            </label>
            <label class="fld inline">
              <span class="lbl">模型</span>
              <select bind:value={draftModel}>
                {#each models as m}<option value={m}>{m}</option>{/each}
              </select>
            </label>
          </div>
          <label class="fld inline">
            <span class="lbl">业务项目（可选）</span>
            <select bind:value={draftProject}>
              <option value="">不关联</option>
              {#each $projects as p}<option value={p.slug}>{p.path}</option>{/each}
            </select>
          </label>
        {:else if selected}
          <div class="raw-view selectable">{selected.input}</div>
          <p class="meta-line">模型：{selected.model || "默认"} · 项目：{selected.projectSlug ? projPath(selected.projectSlug) : "未关联"}</p>
        {/if}
        <div class="actions">
          <button class="primary" on:click={enrich} disabled={busy || (creating && !draftInput.trim())}>补全信息</button>
          {#if selected}<button class="danger-ghost" on:click={() => remove(selected.id)}>删除</button>{/if}
        </div>
      </div>

    {:else if selected && selected.status === "enriching"}
      <div class="pane center">
        <div class="loading">
          <span class="axon-signal"><i></i><i></i><i></i></span>
          <p>正在补全信息…</p>
        </div>
      </div>

    {:else if selected && selected.status === "review_spec"}
      <!-- Core: editable spec review form -->
      <div class="pane">
        <h2>确认规格</h2>
        {#if selected.failedStage === "enrich"}
          <p class="warn-note">自动补全失败，请手动填写下面的规格。</p>
        {/if}
        <details class="raw-fold">
          <summary>原始描述</summary>
          <div class="raw-view selectable">{selected.input}</div>
        </details>

        <!-- Knowledge provenance: what business knowledge the graph injected into
             this enrichment, and where it came from. Builds trust that the AI
             actually referenced the project's business context. -->
        {#if selected.spec?.injectedKnowledge?.length}
          <div class="kb-bar">
            <div class="kb-line">
              <span class="kb-icon">🧠</span>
              <span class="kb-label">已注入业务知识</span>
              <span class="kb-tags">
                {#each selected.spec.injectedKnowledge as name}<span class="kb-tag">{name}</span>{/each}
              </span>
            </div>
            <!-- Recall method: shows whether the AI recalled via true semantics
                 (vectors) or degraded to literal keyword matching. Builds trust. -->
            {#if (selected.spec?.recallMethod === "semantic" || selected.spec?.recallMethod === "hybrid") && selected.spec?.recallLocal}
              <div class="kb-method kb-method-local">
                🟡 本地语义召回（未配云端 embedding，精度有限）
                <span class="kb-method-hint">去「设置」配置 embedding 可启用满血语义检索</span>
              </div>
            {:else if selected.spec?.recallMethod === "semantic" || selected.spec?.recallMethod === "hybrid"}
              <div class="kb-method kb-method-semantic">🔵 语义向量召回（可信度高）</div>
            {:else if selected.spec?.recallMethod === "keyword"}
              <div class="kb-method kb-method-keyword">
                🔤 关键词匹配（降级 —— 未配置 embedding，语义检索未生效）
                <span class="kb-method-hint">去「设置」配置 embedding 可启用语义检索</span>
              </div>
            {/if}
            {#if selected.spec?.knowledgeSources?.length}
              <div class="kb-src">来源：{selected.spec.knowledgeSources.join("、")}</div>
            {/if}
          </div>
        {:else}
          <div class="kb-bar kb-empty">
            <span class="kb-icon">💡</span>
            {selected.spec?.recallMethod === "none"
              ? "未召回到相关知识"
              : "本次未注入业务知识 —— 绑定一个知识项目可让 AI 更懂你的业务"}
          </div>
        {/if}

        <label class="fld">
          <span class="lbl">目标</span>
          <textarea bind:value={spec.goal} on:input={touchSpec} rows="2"></textarea>
        </label>
        <label class="fld">
          <span class="lbl">背景 / 上下文</span>
          <textarea bind:value={spec.background} on:input={touchSpec} rows="3"></textarea>
        </label>

        {#each listFields as f}
          <div class="fld">
            <span class="lbl" class:hint={f.hint}>{f.label}{#if f.hint} · AI 补充的易遗漏点{/if}</span>
            {#each spec[f.key] as _, i}
              <div class="li-row">
                <input bind:value={spec[f.key][i]} on:input={touchSpec} />
                <button class="li-del" title="删除" on:click={() => removeItem(f.key, i)}>×</button>
              </div>
            {/each}
            <button class="li-add" on:click={() => addItem(f.key)}>+ 添加</button>
          </div>
        {/each}

        <div class="actions sticky">
          <button on:click={saveSpec} disabled={busy}>{specSaved ? "已保存" : "保存"}</button>
          <button class="primary" on:click={execute} disabled={busy}>执行</button>
          <button class="danger-ghost" on:click={() => remove(selected.id)}>删除</button>
        </div>
      </div>

    {:else if selected && selected.status === "queued"}
      <div class="pane center">
        <div class="loading"><p>排队中（已达并发上限，等待空位）…</p></div>
      </div>

    {:else if selected && selected.status === "executing"}
      <!-- Streaming execution output -->
      <div class="pane">
        <div class="exec-head">
          <h2>执行中 <span class="axon-signal"><i></i><i></i><i></i></span></h2>
          <button class="danger-ghost" on:click={cancel}>取消</button>
        </div>
        <p class="meta-line">模型：{selected.model || "默认"}</p>
        <pre class="stream selectable">{streamBuf[selected.id] ?? ""}{#if !(streamBuf[selected.id])}（等待模型输出…）{/if}</pre>
      </div>

    {:else if selected && (selected.status === "review_result" || selected.status === "accepted")}
      <!-- Core: result review + iteration history -->
      <div class="pane">
        <div class="exec-head">
          <h2>{selected.status === "accepted" ? "已采纳" : "审阅结果"}
            {#if selected.status === "accepted"}<span class="badge b-ok">已采纳</span>{/if}
          </h2>
          {#if runs.length > 1}
            <select class="run-pick" bind:value={viewRunId}>
              {#each runs as r}<option value={r.id}>第 {r.seq} 轮{r.status === "done" ? "" : "（" + r.status + "）"}</option>{/each}
            </select>
          {/if}
        </div>
        <p class="meta-line">模型：{viewedRun?.model || selected.model || "默认"}{#if viewedRun?.feedback} · 本轮反馈：{viewedRun.feedback}{/if}</p>
        <div class="result selectable">{@html renderMd(viewedRun?.result ?? streamBuf[selected.id] ?? "")}</div>

        {#if selected.status === "review_result"}
          <div class="actions sticky">
            <button class="primary" on:click={accept} disabled={busy}>采纳</button>
            <button on:click={openFeedback} disabled={busy}>打回重跑</button>
          </div>
        {/if}
      </div>

    {:else if selected && selected.status === "failed"}
      <div class="pane">
        <h2>失败</h2>
        <p class="warn-note">{selected.failedStage === "enrich" ? "补全阶段出错。" : "执行阶段出错。"}可重试。</p>
        <div class="actions">
          <button class="primary" on:click={retry} disabled={busy}>重试</button>
          <button class="danger-ghost" on:click={() => remove(selected.id)}>删除</button>
        </div>
      </div>

    {:else}
      <div class="pane center">
        <p class="hint-empty">从左侧选择一个任务，或点“+ 新建任务”开始。</p>
      </div>
    {/if}

    {#if showFeedback}
      <div class="modal-back" on:click={() => (showFeedback = false)} on:keydown role="presentation">
        <div class="modal" on:click|stopPropagation on:keydown role="presentation">
          <h3>打回并重跑</h3>
          <p class="modal-hint">填写反馈，会连同规格与上一版产出一起发给模型重跑。</p>
          <textarea bind:value={feedback} rows="4" placeholder="哪里不对、要怎么改…"></textarea>
          <div class="actions">
            <button on:click={() => (showFeedback = false)}>取消</button>
            <button class="primary" on:click={submitReject} disabled={busy || !feedback.trim()}>提交重跑</button>
          </div>
        </div>
      </div>
    {/if}
  </section>
</div>

<style>
  .tasks { display: flex; height: 100%; min-height: 0; background: var(--bg-base); }

  /* Left list */
  .list {
    width: 280px; flex: 0 0 280px; display: flex; flex-direction: column;
    border-right: 1px solid var(--border); background: var(--bg-surface); min-height: 0;
  }
  .new-btn {
    margin: 10px; padding: 8px 12px; border-radius: var(--radius-control);
    background: var(--accent); color: var(--accent-fg); border: none; font-weight: 600; font-size: 13px;
  }
  .new-btn:hover { filter: brightness(1.08); }
  .items { flex: 1; overflow-y: auto; padding: 0 8px 12px; min-height: 0; }
  .item {
    width: 100%; display: flex; align-items: center; gap: 8px; justify-content: space-between;
    text-align: left; background: transparent; border: 1px solid transparent;
    border-radius: var(--radius-control); padding: 9px 10px; margin-bottom: 4px; color: var(--text-primary);
  }
  .item:hover { background: var(--bg-elevated); }
  .item.active { background: var(--bg-elevated); border-color: var(--accent); }
  .item .title {
    font-size: 13px; flex: 1; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
  }
  .empty { color: var(--text-muted); font-size: 12.5px; padding: 12px 6px; }

  /* Status badges */
  .badge {
    flex: 0 0 auto; display: inline-flex; align-items: center; gap: 4px;
    font-size: 11px; padding: 2px 7px; border-radius: 999px; white-space: nowrap;
    border: 1px solid var(--border); color: var(--text-muted);
  }
  .badge.b-idle { color: var(--text-muted); }
  .badge.b-run { color: var(--accent); border-color: var(--accent); }
  .badge.b-todo { color: var(--warning); border-color: var(--warning); background: rgba(210,153,34,0.08); }
  .badge.b-ok { color: var(--success); border-color: var(--success); }
  .badge.b-err { color: var(--danger); border-color: var(--danger); }

  /* Detail */
  .detail { flex: 1; min-width: 0; overflow-y: auto; position: relative; }
  .pane { max-width: 820px; margin: 0 auto; padding: 20px 28px 40px; }
  .pane.center { display: flex; align-items: center; justify-content: center; min-height: 60vh; }
  h2 { font-size: 16px; margin: 0 0 14px; display: flex; align-items: center; gap: 10px; }
  h2 .badge { font-size: 11px; }

  .fld { display: block; margin-bottom: 14px; }
  .fld.inline { display: inline-flex; flex-direction: column; gap: 4px; }
  .row { display: flex; gap: 16px; flex-wrap: wrap; margin-bottom: 14px; }
  .lbl { display: block; font-size: 12px; color: var(--text-muted); margin-bottom: 4px; }
  .lbl.hint { color: var(--warning); font-weight: 600; }

  textarea, input, select {
    width: 100%; background: var(--bg-elevated); color: var(--text-primary);
    border: 1px solid var(--border); border-radius: var(--radius-control);
    font-family: var(--font-ui); font-size: 13px; padding: 8px 10px; outline: none;
  }
  textarea { resize: vertical; line-height: 1.5; }
  textarea.raw { font-family: var(--font-mono); }
  textarea:focus, input:focus, select:focus { border-color: var(--accent); }
  .fld.inline select { min-width: 200px; }

  .raw-view {
    background: var(--bg-elevated); border: 1px solid var(--border); border-radius: var(--radius-control);
    padding: 10px 12px; font-family: var(--font-mono); font-size: 12.5px; white-space: pre-wrap;
    color: var(--text-primary); line-height: 1.5;
  }
  .raw-fold { margin-bottom: 16px; }
  .raw-fold summary { cursor: pointer; color: var(--text-muted); font-size: 12px; margin-bottom: 8px; }
  .meta-line { color: var(--text-muted); font-size: 12px; margin: 0 0 14px; }
  .warn-note { color: var(--warning); font-size: 12.5px; margin: 0 0 14px; }

  /* Knowledge provenance bar (review_spec) */
  .kb-bar {
    background: var(--bg-elevated); border: 1px solid var(--border);
    border-left: 3px solid var(--accent); border-radius: var(--radius-control);
    padding: 9px 12px; margin: 0 0 16px; font-size: 12.5px;
  }
  .kb-line { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
  .kb-icon { flex: 0 0 auto; }
  .kb-label { color: var(--text-primary); font-weight: 600; }
  .kb-tags { display: inline-flex; gap: 6px; flex-wrap: wrap; }
  .kb-tag {
    font-size: 11.5px; padding: 1px 8px; border-radius: 999px;
    background: rgba(59, 130, 246, 0.12);
    border: 1px solid var(--accent); color: var(--accent);
  }
  .kb-src { color: var(--text-muted); font-size: 11.5px; margin-top: 6px; }
  .kb-method {
    margin-top: 7px; font-size: 11.5px; font-weight: 600;
    display: flex; align-items: center; gap: 6px; flex-wrap: wrap;
  }
  .kb-method-semantic { color: #22c55e; }
  .kb-method-keyword { color: #eab308; }
  .kb-method-local { color: #eab308; }
  .kb-method-hint { font-weight: 400; color: var(--text-muted); }
  .kb-empty {
    border-left-color: var(--border); color: var(--text-muted);
    display: flex; align-items: center; gap: 8px;
  }

  /* List-type spec fields */
  .li-row { display: flex; gap: 6px; margin-bottom: 6px; }
  .li-row input { flex: 1; }
  .li-del {
    flex: 0 0 auto; width: 30px; background: var(--bg-elevated); border: 1px solid var(--border);
    border-radius: var(--radius-control); color: var(--text-muted); font-size: 16px; line-height: 1;
  }
  .li-del:hover { color: var(--danger); border-color: var(--danger); }
  .li-add {
    background: transparent; border: 1px dashed var(--border); color: var(--text-muted);
    border-radius: var(--radius-control); font-size: 12px; padding: 5px 10px;
  }
  .li-add:hover { color: var(--accent); border-color: var(--accent); }

  /* Actions */
  .actions { display: flex; gap: 10px; margin-top: 18px; align-items: center; }
  .actions.sticky {
    position: sticky; bottom: 0; margin-top: 22px; padding: 12px 0;
    background: linear-gradient(to top, var(--bg-base) 70%, transparent);
  }
  .actions button, .pane button.primary {
    padding: 8px 16px; border-radius: var(--radius-control); font-size: 13px; font-weight: 600;
    border: 1px solid var(--border); background: var(--bg-elevated); color: var(--text-primary);
  }
  button.primary { background: var(--accent); color: var(--accent-fg); border-color: var(--accent); }
  button.primary:hover:not(:disabled) { filter: brightness(1.08); }
  .actions button:hover:not(:disabled) { border-color: var(--accent); }
  button:disabled { opacity: 0.5; cursor: default; }
  .danger-ghost { color: var(--danger) !important; border-color: transparent !important; background: transparent !important; }
  .danger-ghost:hover { border-color: var(--danger) !important; }

  /* Execution / result */
  .exec-head { display: flex; align-items: center; justify-content: space-between; gap: 12px; }
  .stream {
    background: var(--bg-elevated); border: 1px solid var(--border); border-radius: var(--radius-control);
    padding: 12px 14px; font-family: var(--font-mono); font-size: 12.5px; white-space: pre-wrap;
    line-height: 1.5; color: var(--text-primary); min-height: 120px; margin: 0;
  }
  .run-pick { width: auto; min-width: 140px; font-size: 12px; padding: 4px 8px; }
  .result { line-height: 1.6; color: var(--text-primary); }
  .result :global(pre) {
    background: var(--bg-elevated); border: 1px solid var(--border); border-radius: var(--radius-control);
    padding: 10px; overflow-x: auto; font-size: 12.5px;
  }
  .result :global(:not(pre) > code) { background: var(--bg-elevated); border-radius: 4px; padding: 1px 5px; }
  .result :global(a) { color: var(--accent); }
  .result :global(ul), .result :global(ol) { padding-left: 20px; }

  .loading { text-align: center; color: var(--text-muted); }
  .loading p { margin-top: 12px; }
  .hint-empty { color: var(--text-muted); }

  /* Feedback modal */
  .modal-back {
    position: absolute; inset: 0; background: rgba(0,0,0,0.5);
    display: flex; align-items: center; justify-content: center; z-index: 60;
  }
  .modal {
    background: var(--bg-surface); border: 1px solid var(--border); border-radius: var(--radius-card);
    padding: 20px; width: 460px; max-width: 90%;
  }
  .modal h3 { margin: 0 0 8px; font-size: 15px; }
  .modal-hint { color: var(--text-muted); font-size: 12px; margin: 0 0 12px; }
</style>
