<script lang="ts">
  // Knowledge Base Wiki & Card Catalog View.
  // Replaces the unreadable force-directed graph with a clean, scannable
  // encyclopedia of project knowledge cards + instant fact editing & source tracing.
  import { onMount, onDestroy } from "svelte";
  import { get } from "svelte/store";
  import {
    BuildGraph, GetGraph, IndexProject, DeleteEntity, RenameEntity,
    RememberManualEntity, UpdateEntityObservations, UndoLastGraphReview,
    BuildMultiGraph, MoveEntity, PruneStaleKnowledge,
  } from "../../wailsjs/go/main/App.js";
  import { EventsOn, EventsOff } from "../../wailsjs/runtime/runtime.js";
  import type { graph } from "../../wailsjs/go/models";
  import {
    currentProject, indexRequest, selectedNamespaces, effectiveNamespaces,
    namespaces, loadNamespaces, jumpToSession, entityJumpRequest,
  } from "../lib/stores";
  import PortablePanel from "../lib/PortablePanel.svelte";

  let g: graph.Graph | null = null;
  // Entities from the reserved _global_ namespace: conventions that hold in any
  // project. Kept separate from `g` on purpose (see the rendering section).
  let globalEntities: graph.Entity[] = [];
  let showGlobal = false;
  let focus = "";
  let filter = "";
  let typeFilter = "";
  let loading = false;
  let indexing = false;
  // Mirrors the backend graph:progress payload keys (current / total / title /
  // error). Kept separate from the UI-only counters so the progress line can
  // never render "0/0" because of a renamed field.
  let progress = { done: 0, total: 0, current: "", indexed: 0, skipped: 0, failed: 0, lastError: "" };
  // Authoritative count from graph:done (sessions actually distilled).
  let processedCount = 0;
  let indexDoneBanner = "";

  // Portable Import/Export modal
  let showPortable = false;

  // Quick-add entity modal
  let showAddEntity = false;
  let addEntName = "";
  let addEntType = "concept";
  let addEntObs = "";
  let addEntBusy = false;

  // Inline editing inside detail drawer
  let renamingFocus = false;
  let renameDraft = "";
  let editingObsIdx = -1;
  let editingObsDraft = "";
  let addingNewFact = false;
  let newFactDraft = "";

  // Undo toast
  let undoToast = "";
  let undoTimer: any = null;

  // Batch selection mode
  let batchMode = false;
  let batchSelected = new Set<string>();
  let batchBusy = false;

  function showUndo(msg: string) {
    undoToast = msg;
    clearTimeout(undoTimer);
    undoTimer = setTimeout(() => { undoToast = ""; }, 6500);
  }

  async function triggerUndo() {
    try {
      const ns = primaryNs();
      await UndoLastGraphReview(ns);
      undoToast = "";
      await reloadCurrent();
    } catch (e) {
      alert(String(e));
    }
  }

  const TYPE_META: Record<string, { color: string; label: string; order: number }> = {
    rule:       { color: "#f87171", label: "规则 / 红线", order: 1 },
    decision:   { color: "#c084fc", label: "架构决策",   order: 2 },
    service:    { color: "#38bdf8", label: "服务 / 模块", order: 3 },
    bug:        { color: "#fb923c", label: "问题 / 踩坑", order: 4 },
    concept:    { color: "#34d399", label: "业务概念",   order: 5 },
    person:     { color: "#f472b6", label: "角色 / 团队", order: 6 },
    technology: { color: "#818cf8", label: "技术组件",   order: 7 },
    project:    { color: "#fbbf24", label: "项目 / 仓库", order: 8 },
    file:       { color: "#a3e635", label: "关键文件",   order: 9 },
  };
  const DEFAULT_COLOR = "#94a3b8";
  const colorOf = (t: string) => TYPE_META[(t || "").toLowerCase()]?.color || DEFAULT_COLOR;
  const labelOf = (t: string) => TYPE_META[(t || "").toLowerCase()]?.label || t || "未分类";
  const orderOf = (t: string) => TYPE_META[(t || "").toLowerCase()]?.order || 99;

  function primaryNs(): string {
    if ($selectedNamespaces.length > 0) return $selectedNamespaces[0];
    return $currentProject;
  }

  $: activeNsKey = $effectiveNamespaces.join(",") || $currentProject;
  let loadedNsKey = " ";
  $: if (activeNsKey !== loadedNsKey) {
    loadedNsKey = activeNsKey;
    loadByNamespaces();
  }

  let handledIndexReq = 0;
  $: if ($indexRequest > handledIndexReq) {
    handledIndexReq = $indexRequest;
    if ($currentProject && !indexing) runIndex();
  }

  $: if ($entityJumpRequest && g) {
    const req = $entityJumpRequest;
    const hit = (g.entities || []).find(
      (n) => n.name === req.entityName || n.name.toLowerCase() === req.entityName.toLowerCase(),
    );
    if (hit) {
      focus = hit.name;
    }
    entityJumpRequest.set(null);
  }

  let loadToken = 0;
  async function loadByNamespaces() {
    // Every call takes a ticket; only the newest one is allowed to write state.
    // Distillation triggers several reloads at once (namespace list changes,
    // project switch), and without this an older, emptier reply could land last
    // and leave the catalog blank right after a successful index run.
    const mine = ++loadToken;
    focus = "";
    batchSelected = new Set();
    // `g` is intentionally kept until the new graph arrives: clearing it first
    // made every reload (and every distillation) flash the "not distilled yet"
    // empty state before the cards came back.
    const nsList = targets();
    if (nsList.length === 0) return;
    loading = true;
    try {
      // Project knowledge and cross-project knowledge are fetched separately and
      // never merged: the UI has to be able to say which is which.
      const [res, glob] = await Promise.all([
        BuildMultiGraph(nsList),
        BuildMultiGraph(["_global_"]).catch(() => null),
      ]);
      if (mine !== loadToken) return;
      g = res;
      globalEntities = glob?.entities ?? [];
      pickDefaultFocus();
    } catch (e) {
      console.error(e);
    } finally {
      if (mine === loadToken) loading = false;
    }
  }

  // Which namespaces to read. Always assembled from graphcache via
  // BuildMultiGraph: GetGraph reads the legacy graphs/<ns>.json snapshot, which
  // distillation never writes, so using it left the catalog empty right after a
  // successful run.
  function targets(): string[] {
    const ns = $effectiveNamespaces;
    if (ns.length > 0) return ns;
    return $currentProject ? [$currentProject] : [];
  }

  async function reloadCurrent() {
    const mine = ++loadToken;
    const nsList = targets();
    let res: graph.Graph | null = null;
    if (nsList.length > 0) {
      res = await BuildMultiGraph(nsList);
    }
    if (mine !== loadToken) return;
    g = res;
    if (focus && !(g?.entities || []).some((n) => n.name === focus)) {
      pickDefaultFocus();
    }
  }

  function pickDefaultFocus() {
    const nodes = g?.entities || [];
    if (!nodes.length) { focus = ""; return; }
    if (focus && nodes.some((n) => n.name === focus)) return;
    const sorted = [...nodes].sort((a, b) => (b.observations?.length || 0) - (a.observations?.length || 0));
    focus = sorted[0].name;
  }

  async function runIndex() {
    const slug = $currentProject;
    if (!slug || indexing) return;
    // Distilling a directory with hundreds of sessions takes the better part of
    // an hour; warn before starting something the user cannot cancel.
    const meta = get(namespaces).find((n) => n.name === slug);
    const planned = meta?.sessions ?? 0;
    if (planned > 60) {
      const ok = window.confirm(
        `「${meta?.path || slug}」有 ${planned} 个会话，提炼预计需要较长时间（数十分钟）。\n\n建议先用「从对话提炼知识」跑一次增量，或确认继续？`,
      );
      if (!ok) return;
    }
    indexing = true;
    indexDoneBanner = "";
    progress = { done: 0, total: 0, current: "", indexed: 0, skipped: 0, failed: 0, lastError: "" };
    processedCount = 0;
    // Snapshot entity counts so the namespace this run writes into can be
    // identified even when its name differs from the project key.
    const before = new Map(get(namespaces).map((n) => [n.name, n.entities]));
    const growth = new Map<string, number>();
    try {
      // IndexProject resolves to nothing — it must NOT be assigned to `g`.
      // Assigning its void result was why a finished run blanked the catalog.
      await IndexProject(slug);
      await loadNamespaces();
      for (const n of get(namespaces)) {
        const delta = n.entities - (before.get(n.name) ?? 0);
        if (delta > 0) growth.set(n.name, delta);
      }

      // Distillation may create (or fill) this project's own namespace. Select
      // it explicitly so the catalog shows what was just produced, instead of
      // whatever knowledge base happened to be open before.
      const list = get(namespaces);
      const mine = list.find((n) => n.name === slug && n.entities > 0);
      // A directory mapped through .axon-project can land under a different
      // name than the slug; fall back to the namespace whose entity count just
      // changed. Never jump to an unrelated (if larger) knowledge base — that
      // reads as "my distillation showed someone else's cards".
      const grown = [...growth.entries()].sort((a, b) => b[1] - a[1])[0]?.[0];
      const targetName =
        mine?.name || (grown ? list.find((n) => n.name === grown && n.entities > 0)?.name : "") || slug;
      if (targetName) {
        currentProject.set(targetName);
        selectedNamespaces.set([targetName]);
      }

      // Force the reactive loader to treat this as a fresh key, then await the
      // reload ourselves so the catalog reflects the run we just finished.
      loadedNsKey = " ";
      await loadByNamespaces();

      if (progress.total === 0) {
        indexDoneBanner = "没有读到可提炼的对话记录（该目录下暂无 Claude/Codex/WorkBuddy 会话）";
      } else if (processedCount === 0 && progress.failed === 0) {
        indexDoneBanner = `全部 ${progress.total} 个会话此前已提炼过，无新增`;
      } else if (processedCount === 0) {
        indexDoneBanner = `${progress.failed} 个会话提炼失败${progress.lastError ? "：" + progress.lastError : ""}`;
      } else {
        indexDoneBanner = `提炼完成：成功 ${processedCount} 个会话` +
          (progress.failed ? `，失败 ${progress.failed} 个` : "");
      }
    } catch (e) {
      // A missing provider is the common cause here; show it inline instead of
      // an OS alert so the page keeps whatever catalog was already on screen.
      indexDoneBanner = `提炼失败：${String(e)}`;
      progress.lastError = String(e);
    } finally {
      indexing = false;
    }
  }

  async function removeEntity(name: string, e?: Event) {
    e?.stopPropagation();
    const ns = primaryNs();
    if (!ns || !name) return;
    try {
      // DeleteEntity returns nothing; reload afterwards instead of assigning it.
      await DeleteEntity(ns, name);
      if (focus === name) focus = "";
      await reloadCurrent();
      await loadNamespaces();
      showUndo(`已删除知识卡片「${name}」`);
    } catch (err) {
      alert(String(err));
    }
  }

  function toggleBatchItem(name: string, e?: Event) {
    e?.stopPropagation();
    const next = new Set(batchSelected);
    if (next.has(name)) next.delete(name);
    else next.add(name);
    batchSelected = next;
  }

  async function batchDeleteSelected() {
    const ns = primaryNs();
    if (!ns || batchSelected.size === 0 || batchBusy) return;
    batchBusy = true;
    const targets = [...batchSelected];
    try {
      for (const name of targets) {
        await DeleteEntity(ns, name);
      }
      batchSelected = new Set();
      await reloadCurrent();
      await loadNamespaces();
      showUndo(`已批量删除 ${targets.length} 张知识卡片`);
    } catch (e) {
      alert(String(e));
    } finally {
      batchBusy = false;
    }
  }

  function startRenameFocus() {
    if (!focusNode) return;
    renamingFocus = true;
    renameDraft = focusNode.name;
  }

  async function saveRenameFocus() {
    const ns = primaryNs();
    const next = renameDraft.trim();
    if (!ns || !focusNode || !next || next === focusNode.name) {
      renamingFocus = false;
      return;
    }
    const oldName = focusNode.name;
    renamingFocus = false;
    try {
      await RenameEntity(ns, oldName, next);
      focus = next;
      await reloadCurrent();
      showUndo(`已将「${oldName}」重命名为「${next}」`);
    } catch (e) {
      alert(String(e));
    }
  }

  function startEditObs(idx: number, text: string) {
    editingObsIdx = idx;
    editingObsDraft = text;
  }

  async function saveEditObs(idx: number) {
    const ns = primaryNs();
    if (!ns || !focusNode) return;
    const nextText = editingObsDraft.trim();
    const obsList = [...(focusNode.observations || [])];
    if (!nextText) {
      obsList.splice(idx, 1);
    } else {
      obsList[idx] = nextText;
    }
    editingObsIdx = -1;
    try {
      await UpdateEntityObservations(ns, focusNode.name, obsList);
      await reloadCurrent();
    } catch (e) {
      alert(String(e));
    }
  }

  async function deleteSingleObs(idx: number) {
    const ns = primaryNs();
    if (!ns || !focusNode) return;
    const obsList = [...(focusNode.observations || [])];
    obsList.splice(idx, 1);
    try {
      await UpdateEntityObservations(ns, focusNode.name, obsList);
      await reloadCurrent();
      showUndo("已删除 1 条要点");
    } catch (e) {
      alert(String(e));
    }
  }

  async function saveNewFact() {
    const ns = primaryNs();
    const text = newFactDraft.trim();
    if (!ns || !focusNode || !text) {
      addingNewFact = false;
      newFactDraft = "";
      return;
    }
    const obsList = [...(focusNode.observations || []), text];
    addingNewFact = false;
    newFactDraft = "";
    try {
      await UpdateEntityObservations(ns, focusNode.name, obsList);
      await reloadCurrent();
    } catch (e) {
      alert(String(e));
    }
  }

  async function submitQuickAddEntity() {
    const ns = primaryNs();
    const name = addEntName.trim();
    if (!ns || !name || addEntBusy) return;
    addEntBusy = true;
    const obs = addEntObs
      .split("\n")
      .map((s) => s.trim())
      .filter(Boolean);
    try {
      // The binding takes (namespace, ManualEntityInput) — a positional list of
      // four strings was silently rejected before, so "新增卡片" saved nothing.
      await RememberManualEntity(ns, {
        name,
        type: addEntType,
        aliases: [],
        observations: obs,
        relations: [],
      } as any);
      showAddEntity = false;
      addEntName = "";
      addEntObs = "";
      await reloadCurrent();
      await loadNamespaces();
      focus = name;
    } catch (e) {
      alert(String(e));
    } finally {
      addEntBusy = false;
    }
  }

  onMount(() => {
    // Backend payload keys: current (1-based index), total, title, error, phase.
    // Mapping them explicitly keeps the progress line from reading 0/0.
    EventsOn("graph:progress", (p: any) => {
      if (!p) return;
      progress = {
        done: typeof p.current === "number" ? p.current : progress.done,
        total: typeof p.total === "number" ? p.total : progress.total,
        current: p.title || progress.current,
        indexed: progress.indexed,
        skipped: progress.skipped,
        failed: p.error ? progress.failed + 1 : progress.failed,
        lastError: p.error || progress.lastError,
      };
    });
    // graph:done carries the authoritative counts (processed / failed), which
    // differ from the progress cursor whenever sessions were cache-skipped.
    EventsOn("graph:done", (p: any) => {
      if (!p) return;
      processedCount = typeof p.processed === "number" ? p.processed : processedCount;
      if (typeof p.failed === "number") progress.failed = p.failed;
    });
  });
  onDestroy(() => {
    EventsOff("graph:progress");
  });

  // Type statistics for filter chips
  $: typeCounts = (() => {
    const counts = new Map<string, number>();
    for (const n of g?.entities || []) {
      const t = (n.type || "concept").toLowerCase();
      counts.set(t, (counts.get(t) || 0) + 1);
    }
    return [...counts.entries()].sort((a, b) => orderOf(a[0]) - orderOf(b[0]));
  })();

  // Filtered & sorted knowledge cards
  $: filteredNodes = (g?.entities || [])
    .filter((n) => {
      if (typeFilter && (n.type || "concept").toLowerCase() !== typeFilter) return false;
      if (!filter.trim()) return true;
      const q = filter.trim().toLowerCase();
      if (n.name.toLowerCase().includes(q)) return true;
      if ((n.aliases || []).some((a) => a.toLowerCase().includes(q))) return true;
      if ((n.observations || []).some((o) => o.toLowerCase().includes(q))) return true;
      return false;
    })
    .sort((a, b) => {
      const oa = orderOf(a.type);
      const ob = orderOf(b.type);
      if (oa !== ob) return oa - ob;
      return (b.observations?.length || 0) - (a.observations?.length || 0);
    });

  // Group filtered cards by category for encyclopedia layout
  $: groupedSections = groupByType(filteredNodes);

  // Cross-project knowledge lives in the reserved _global_ namespace and is
  // rendered as its own section: it applies everywhere, so showing it inline
  // with project cards is exactly how project A's facts get recited in B.
  $: globalFiltered = globalEntities
    .filter((n) => {
      if (typeFilter && (n.type || "concept").toLowerCase() !== typeFilter) return false;
      if (!filter.trim()) return true;
      const q = filter.trim().toLowerCase();
      return (
        n.name.toLowerCase().includes(q) ||
        (n.aliases || []).some((a) => a.toLowerCase().includes(q)) ||
        (n.observations || []).some((o) => o.toLowerCase().includes(q))
      );
    })
    .sort((a, b) => (b.observations?.length || 0) - (a.observations?.length || 0));
  $: globalSections = groupByType(globalFiltered);

  function groupByType(list: graph.Entity[]) {
    const map = new Map<string, graph.Entity[]>();
    for (const n of list) {
      const t = (n.type || "concept").toLowerCase();
      if (!map.has(t)) map.set(t, []);
      map.get(t)!.push(n);
    }
    return [...map.entries()].map(([type, items]) => ({
      type,
      label: labelOf(type),
      color: colorOf(type),
      items,
    }));
  }

  $: focusNode =
    (g?.entities || []).find((n) => n.name === focus) ||
    globalEntities.find((n) => n.name === focus) ||
    null;
  $: isGlobalFocus = !!globalEntities.find((n) => n.name === focus);
  $: focusEdges = (g?.relations || []).filter((e) => e.from === focus || e.to === focus);

  // Card age: how long ago its newest fact was learned. Environment facts rot
  // once the environment is recycled, so age is shown rather than hidden.
  function ageDaysOf(n: graph.Entity): number {
    let newest = 0;
    for (const a of n.assertions || []) {
      if ((a.observedAt || 0) > newest) newest = a.observedAt || 0;
    }
    if (!newest) return -1;
    return Math.floor((Date.now() - newest) / 86400000);
  }
  const envish = (n: graph.Entity) => {
    const text = n.name + "\n" + (n.observations || []).join("\n");
    return /(\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3})|(\b(dev|sit|uat|prod|stg|pre)-?\d)|(\b(feature|release|hotfix)\/)/i.test(text);
  };

  // Stale-knowledge pruning: preview first, delete only on confirmation.
  let prunePreview: any = null;
  let pruneBusy = false;
  async function previewPrune() {
    const ns = primaryNs();
    if (!ns || pruneBusy) return;
    pruneBusy = true;
    try {
      prunePreview = await PruneStaleKnowledge(ns, 0, true);
    } catch (e) {
      alert(String(e));
    } finally {
      pruneBusy = false;
    }
  }
  async function confirmPrune() {
    const ns = primaryNs();
    if (!ns || pruneBusy) return;
    pruneBusy = true;
    try {
      const res = await PruneStaleKnowledge(ns, 0, false);
      prunePreview = null;
      await loadByNamespaces();
      await loadNamespaces();
      showUndo(`已清理 ${res.removed} 张过期卡片${res.backupDir ? "（已备份）" : ""}`);
    } catch (e) {
      alert(String(e));
    } finally {
      pruneBusy = false;
    }
  }

  // Reclassify the focused card. Automatic scope detection only says where a
  // fact was learned; whether it belongs to this project is the owner's call.
  async function toggleScope() {    const ns = primaryNs();
    if (!ns || !focus) return;
    const toGlobal = !isGlobalFocus;
    try {
      await MoveEntity(ns, focus, toGlobal ? "_global_" : ns);
      await loadNamespaces();
      await loadByNamespaces();
      showUndo(toGlobal ? `已将「${focus}」标为跨项目通用知识` : `已将「${focus}」移回本项目`);
    } catch (e) {
      alert(String(e));
    }
  }
</script>

<div class="kb-page">
  <header class="kb-bar">
    <div class="kb-bar-left">
      <div class="search-box">
        <span class="s-ic">⌕</span>
        <input
          class="s-input"
          placeholder="搜索规则、架构决策、模块或任意内容…"
          bind:value={filter}
        />
        {#if filter}
          <button class="s-clear" on:click={() => (filter = "")}>×</button>
        {/if}
      </div>

      {#if g?.entities?.length}
        <span class="kb-stat">
          共 <b>{filteredNodes.length}</b> / {g.entities.length} 张知识卡
        </span>
      {/if}
    </div>

    <div class="kb-bar-right">
      <button
        class="btn ghost"
        on:click={() => { showAddEntity = true; addEntName = ""; addEntObs = ""; }}
        disabled={!primaryNs()}
        title="手动新增一条业务规则、决策或知识卡片"
      >
        ＋ 新增卡片
      </button>

      {#if g?.entities?.length}
        <button
          class="btn ghost"
          class:active-mode={batchMode}
          on:click={() => { batchMode = !batchMode; batchSelected = new Set(); }}
          title="勾选多张无用卡片一键清理"
        >
          {batchMode ? "退出多选" : "批量清理"}
        </button>

        <button
          class="btn ghost"
          on:click={previewPrune}
          disabled={pruneBusy}
          title="环境类知识（IP、集群、分支、镜像）会随环境回收而失效，超过 21 天即可清理；其余卡片 60 天未更新"
        >
          🧹 清理过期
        </button>
      {/if}

      <button
        class="btn ghost"
        on:click={() => (showPortable = true)}
        title="导出当前知识库备份，或导入已有知识包"
      >
        ⇄ 备份 / 导入
      </button>

      <button
        class="btn primary"
        on:click={runIndex}
        disabled={!$currentProject || indexing}
        title="扫描当前项目新增的 AI 对话，自动提炼业务规则与架构知识"
      >
        {#if indexing}
          提炼中 ({progress.done}/{progress.total || "…"})…
        {:else}
          ✦ 从对话提炼知识
        {/if}
      </button>
    </div>
  </header>

  {#if typeCounts.length > 0}
    <div class="type-bar">
      <button
        class="type-chip"
        class:on={typeFilter === ""}
        on:click={() => (typeFilter = "")}
      >
        全部 <span class="tc-n">{g?.entities?.length || 0}</span>
      </button>
      {#each typeCounts as [t, cnt]}
        <button
          class="type-chip"
          class:on={typeFilter === t}
          style="--tc: {colorOf(t)}"
          on:click={() => (typeFilter = typeFilter === t ? "" : t)}
        >
          <span class="tc-dot" style="background: {colorOf(t)}"></span>
          {labelOf(t)}
          <span class="tc-n">{cnt}</span>
        </button>
      {/each}

      {#if batchMode && batchSelected.size > 0}
        <div class="batch-actions">
          <span>已选 {batchSelected.size} 项</span>
          <button class="btn danger sm" disabled={batchBusy} on:click={batchDeleteSelected}>
            {batchBusy ? "删除中…" : `🗑 删除所选 (${batchSelected.size})`}
          </button>
        </div>
      {/if}
    </div>
  {/if}

  {#if indexing}
    <div class="progress-banner">
      <span class="p-spin">✦</span>
      <span>
        正在从对话提炼知识：{progress.done}/{progress.total || "…"}
        {#if progress.current} · <code>{progress.current}</code>{/if}
        {#if progress.failed}（失败 {progress.failed}）{/if}
      </span>
    </div>
  {/if}

  {#if indexDoneBanner && !indexing}
    <div class="done-banner" class:is-error={progress.lastError}>
      <span>{progress.lastError ? "!" : "✓"} {indexDoneBanner}</span>
      <button class="banner-close" on:click={() => { indexDoneBanner = ""; progress.lastError = ""; }}>×</button>
    </div>
  {/if}

  <div class="kb-body">
    {#if !$currentProject && $effectiveNamespaces.length === 0}
      <div class="kb-empty">
        <div class="empty-title">请先在左侧选择一个项目知识库</div>
        <div class="empty-sub">选择左侧已有卡片的知识库即可直接查阅；或选择一个会话目录点击右上角「✦ 从对话提炼知识」</div>
      </div>
    {:else if loading}
      <div class="kb-empty">
        <div class="empty-title">正在加载知识百科…</div>
      </div>
    {:else if !g || !g.entities?.length}
      <div class="kb-empty">
        <div class="empty-icon">✦</div>
        <div class="empty-title">「{$currentProject || primaryNs()}」暂未提炼知识卡片</div>
        <div class="empty-sub">
          该项目目前还没有沉淀下来的知识卡片。你可以直接点击下方按钮从历史 AI 对话中自动提炼，或者在左侧切换到已有卡片的知识库（如 wanlian、axon 等）。
        </div>
        <div class="empty-actions">
          <button
            class="btn primary"
            on:click={runIndex}
            disabled={!$currentProject || indexing}
          >
            {#if indexing}
              正在从对话提炼 ({progress.done}/{progress.total})…
            {:else}
              ✦ 立即从该项目对话提炼知识
            {/if}
          </button>
          <button
            class="btn ghost"
            on:click={() => { showAddEntity = true; addEntName = ""; addEntObs = ""; }}
          >
            ＋ 手动新增第一张卡片
          </button>
        </div>
      </div>
    {:else if groupedSections.length === 0}
      <div class="kb-empty">
        <div class="empty-title">没有匹配「{filter}」的知识卡片</div>
        <div class="empty-sub">试试更换关键词，或点击上方「全部」分类</div>
      </div>
    {:else}
      <div class="catalog-pane">
        {#each groupedSections as sec (sec.type)}
          <section class="cat-section">
            <div class="cat-header">
              <span class="cat-dot" style="background: {sec.color}"></span>
              <h3 class="cat-title">{sec.label}</h3>
              <span class="cat-count">{sec.items.length}</span>
            </div>

            <div class="card-grid">
              {#each sec.items as n (n.name)}
                {@const isFocused = n.name === focus}
                {@const isChecked = batchSelected.has(n.name)}
                {@const obs = n.observations || []}
                <div
                  class="k-card"
                  class:active={isFocused}
                  class:checked={isChecked}
                  style="--card-accent: {sec.color}"
                  on:click={() => (batchMode ? toggleBatchItem(n.name) : (focus = n.name))}
                  role="button"
                  tabindex="0"
                  on:keydown={(e) => e.key === "Enter" && (focus = n.name)}
                >
                  <div class="kc-head">
                    {#if batchMode}
                      <input
                        type="checkbox"
                        class="kc-check"
                        checked={isChecked}
                        on:click|stopPropagation={() => toggleBatchItem(n.name)}
                      />
                    {/if}
                    <span class="kc-title">{n.name}</span>
                    <span class="kc-badge" style="color: {sec.color}; border-color: {sec.color}44">
                      {sec.label}
                    </span>
                    {#if ageDaysOf(n) >= 0}
                      <span
                        class="kc-age"
                        class:stale={ageDaysOf(n) > 21 && envish(n)}
                        title={ageDaysOf(n) > 21 && envish(n) ? "环境相关信息，可能已随环境回收而失效" : "最近一次学到的距今"}
                      >{ageDaysOf(n)}d</span>
                    {/if}
                    <button
                      class="kc-del"
                      title="删除此卡片"
                      on:click={(e) => removeEntity(n.name, e)}
                    >
                      ×
                    </button>
                  </div>

                  {#if n.aliases?.length}
                    <div class="kc-aliases">别名：{n.aliases.join(" / ")}</div>
                  {/if}

                  {#if obs.length > 0}
                    <ul class="kc-facts">
                      {#each obs.slice(0, 3) as f}
                        <li>{f}</li>
                      {/each}
                    </ul>
                    {#if obs.length > 3}
                      <div class="kc-more">还有 {obs.length - 3} 条要点，点击查看完整卡片 →</div>
                    {/if}
                  {:else}
                    <div class="kc-empty-obs">暂无具体要点记录</div>
                  {/if}
                </div>
              {/each}
            </div>
          </section>
        {/each}

        {#if globalFiltered.length > 0}
          <section class="global-section">
            <button class="global-toggle" on:click={() => (showGlobal = !showGlobal)}>
              <span class="g-tag">通用</span>
              <span class="g-title">跨项目通用知识</span>
              <span class="g-n">{globalFiltered.length}</span>
              <span class="g-hint">在任意项目都成立 · 与上方项目知识分开存放</span>
              <span class="g-caret">{showGlobal ? "▾" : "▸"}</span>
            </button>

            {#if showGlobal}
              {#each globalSections as sec (sec.type)}
                <div class="g-group">
                  <div class="g-group-head">
                    <span class="cat-dot" style="background: {sec.color}"></span>
                    {sec.label}
                    <span class="cat-count">{sec.items.length}</span>
                  </div>
                  <div class="card-grid">
                    {#each sec.items as n (n.name)}
                      {@const obs = n.observations || []}
                      <div
                        class="k-card global-card"
                        class:active={n.name === focus}
                        style="--card-accent: {sec.color}"
                        on:click={() => (focus = n.name)}
                        role="button"
                        tabindex="0"
                        on:keydown={(e) => e.key === "Enter" && (focus = n.name)}
                      >
                        <div class="kc-head">
                          <span class="kc-title">{n.name}</span>
                          <span class="kc-badge" style="color: {sec.color}; border-color: {sec.color}44">
                            {sec.label}
                          </span>
                        </div>
                        {#if obs.length > 0}
                          <ul class="kc-facts">
                            {#each obs.slice(0, 3) as f}
                              <li>{f}</li>
                            {/each}
                          </ul>
                          {#if obs.length > 3}
                            <div class="kc-more">还有 {obs.length - 3} 条要点 →</div>
                          {/if}
                        {:else}
                          <div class="kc-empty-obs">暂无具体要点记录</div>
                        {/if}
                      </div>
                    {/each}
                  </div>
                </div>
              {/each}
            {/if}
          </section>
        {/if}
      </div>

      {#if focusNode}
        <aside class="detail-pane">
          <div class="dp-head">
            <div class="dp-title-row">
              <span class="dp-dot" style="background: {colorOf(focusNode.type)}"></span>
              {#if renamingFocus}
                <input
                  class="dp-rename-input"
                  bind:value={renameDraft}
                  on:keydown={(e) => {
                    if (e.key === "Enter") saveRenameFocus();
                    if (e.key === "Escape") renamingFocus = false;
                  }}
                />
                <button class="btn sm primary" on:click={saveRenameFocus}>保存</button>
                <button class="btn sm ghost" on:click={() => (renamingFocus = false)}>取消</button>
              {:else}
                <h2 class="dp-title selectable">{focusNode.name}</h2>
                <button class="icon-btn" title="重命名" on:click={startRenameFocus}>✎</button>
              {/if}
              <button class="icon-btn close" title="收起详情" on:click={() => (focus = "")}>×</button>
            </div>

            <div class="dp-meta">
              <span class="dp-type-tag" style="color: {colorOf(focusNode.type)}; border-color: {colorOf(focusNode.type)}55">
                {labelOf(focusNode.type)}
              </span>
              {#if isGlobalFocus}
                <span class="dp-scope-tag global">跨项目通用</span>
              {:else}
                <span class="dp-scope-tag">本项目专属</span>
              {/if}
              {#if focusNode.aliases?.length}
                <span class="dp-alias">别名：{focusNode.aliases.join("、")}</span>
              {/if}
              <button class="dp-del-btn" on:click={() => removeEntity(focusNode.name)}>
                🗑 删除卡片
              </button>
            </div>

            <div class="dp-scope-row">
              <button class="btn sm ghost" on:click={toggleScope}>
                {#if isGlobalFocus}
                  ↓ 移回本项目（仅本项目适用）
                {:else}
                  ↑ 标为通用知识（跨项目共享）
                {/if}
              </button>
              <span class="dp-scope-hint">
                {#if isGlobalFocus}
                  当前在通用库，任何项目检索都会看到它
                {:else}
                  只在本项目成立，别的项目查不到它
                {/if}
              </span>
            </div>
          </div>

          <div class="dp-section">
            <div class="dp-sec-head">
              <span>核心要点 ({focusNode.observations?.length || 0})</span>
              <button class="dp-add-fact-btn" on:click={() => { addingNewFact = true; newFactDraft = ""; }}>
                ＋ 补充要点
              </button>
            </div>

            {#if addingNewFact}
              <div class="fact-editor">
                <textarea
                  rows="2"
                  placeholder="输入要补充的业务规则、实现细节或注意事项…"
                  bind:value={newFactDraft}
                ></textarea>
                <div class="fe-actions">
                  <button class="btn sm primary" on:click={saveNewFact}>保存</button>
                  <button class="btn sm ghost" on:click={() => (addingNewFact = false)}>取消</button>
                </div>
              </div>
            {/if}

            {#if focusNode.observations?.length}
              <ul class="dp-fact-list">
                {#each focusNode.observations as f, idx}
                  {@const srcId = focusNode.obsSources?.[idx]}
                  <li class="dp-fact-item">
                    {#if editingObsIdx === idx}
                      <div class="fact-editor">
                        <textarea rows="3" bind:value={editingObsDraft}></textarea>
                        <div class="fe-actions">
                          <button class="btn sm primary" on:click={() => saveEditObs(idx)}>保存</button>
                          <button class="btn sm ghost" on:click={() => (editingObsIdx = -1)}>取消</button>
                        </div>
                      </div>
                    {:else}
                      <div class="dp-fact-text selectable">{f}</div>
                      <div class="dp-fact-foot">
                        {#if srcId && srcId !== "manual"}
                          <button
                            class="src-link"
                            title="跳转到产生这条知识的原始 AI 对话"
                            on:click={() => jumpToSession(srcId, $currentProject, true)}
                          >
                            ⇆ 来源对话 {srcId.slice(0, 8)} ↗
                          </button>
                        {:else if srcId === "manual"}
                          <span class="src-manual">手动录入</span>
                        {/if}
                        <div class="dp-fact-ops">
                          <button class="op-link" on:click={() => startEditObs(idx, f)}>修改</button>
                          <button class="op-link danger" on:click={() => deleteSingleObs(idx)}>删除</button>
                        </div>
                      </div>
                    {/if}
                  </li>
                {/each}
              </ul>
            {:else}
              <div class="dp-empty-note">暂无要点，可点击右上角「＋ 补充要点」添加</div>
            {/if}
          </div>

          {#if focusEdges.length > 0}
            <div class="dp-section">
              <div class="dp-sec-head">
                <span>关联卡片 ({focusEdges.length})</span>
              </div>
              <div class="rel-list">
                {#each focusEdges as e}
                  {@const other = e.from === focus ? e.to : e.from}
                  {@const dir = e.from === focus ? "→" : "←"}
                  <button class="rel-chip" on:click={() => (focus = other)}>
                    <span class="rel-pred">{e.label}</span>
                    <span class="rel-dir">{dir}</span>
                    <span class="rel-target">{other}</span>
                  </button>
                {/each}
              </div>
            </div>
          {/if}
        </aside>
      {/if}
    {/if}
  </div>
</div>

{#if showPortable}
  <!-- PortablePanel brings its own backdrop and close handler. Wrapping it in
       another modal hid its close button and left two stacked overlays (clicks
       landed on the inner, empty onClose); the wrapper also passed the wrong
       prop names, so the panel never had a namespace to export. -->
  <PortablePanel
    namespace={primaryNs()}
    onClose={() => (showPortable = false)}
    onChanged={async (ns: string, focus: boolean) => {
      await loadNamespaces();
      if (focus && ns) selectedNamespaces.set([ns]);
      await loadByNamespaces();
    }}
  />
{/if}

{#if prunePreview}
  <div class="modal-backdrop" on:click={() => (prunePreview = null)} role="presentation">
    <div class="modal-card prune-card" on:click|stopPropagation role="dialog">
      <div class="modal-head">
        <h3>清理过期知识</h3>
        <button class="icon-btn close" on:click={() => (prunePreview = null)}>×</button>
      </div>

      {#if prunePreview.candidates?.length}
        <p class="prune-lead">
          以下 <b>{prunePreview.candidates.length}</b> 张卡片的知识已过期（共 {prunePreview.total} 张）。
          环境类（IP / 集群 / 分支 / 镜像）超过 21 天即失效，其余超过 60 天未更新。
        </p>
        <ul class="prune-list">
          {#each prunePreview.candidates.slice(0, 60) as c}
            <li>
              <span class="pl-name">{c.name}</span>
              <span class="pl-age" class:env={c.reason === "environment"}>{c.ageDays} 天</span>
              <span class="pl-reason">{c.reason === "environment" ? "环境" : "久未更新"}</span>
              {#if c.sample}<span class="pl-sample">{c.sample}</span>{/if}
            </li>
          {/each}
        </ul>
        {#if prunePreview.candidates.length > 60}
          <p class="prune-more">…另有 {prunePreview.candidates.length - 60} 张</p>
        {/if}
        <div class="modal-actions">
          <button class="btn ghost" on:click={() => (prunePreview = null)}>先不动</button>
          <button class="btn primary" on:click={confirmPrune} disabled={pruneBusy}>
            {pruneBusy ? "清理中…" : `删除这 ${prunePreview.candidates.length} 张（自动备份）`}
          </button>
        </div>
      {:else}
        <p class="prune-lead">目前没有超过有效期的卡片，知识都是新鲜的。</p>
        <div class="modal-actions">
          <button class="btn ghost" on:click={() => (prunePreview = null)}>知道了</button>
        </div>
      {/if}
    </div>
  </div>
{/if}

{#if showAddEntity}
  <div class="modal-backdrop" on:click={() => (showAddEntity = false)} role="presentation">
    <div class="modal-card" on:click|stopPropagation role="dialog">
      <div class="modal-head">
        <h3>新增知识卡片</h3>
        <button class="icon-btn close" on:click={() => (showAddEntity = false)}>×</button>
      </div>
      <div class="form-row">
        <label for="ent-name-in">卡片标题 / 概念名称</label>
        <input id="ent-name-in" placeholder="例如：订单可见性校验规则 / jewel-market" bind:value={addEntName} />
      </div>
      <div class="form-row">
        <label for="ent-type-in">所属分类</label>
        <select id="ent-type-in" bind:value={addEntType}>
          {#each Object.entries(TYPE_META) as [k, v]}
            <option value={k}>{v.label} ({k})</option>
          {/each}
        </select>
      </div>
      <div class="form-row">
        <label for="ent-obs-in">核心要点（每行一条）</label>
        <textarea
          id="ent-obs-in"
          rows="4"
          placeholder="每行填写一条具体规则、结论或踩坑经验…"
          bind:value={addEntObs}
        ></textarea>
      </div>
      <div class="modal-foot">
        <button class="btn ghost" on:click={() => (showAddEntity = false)}>取消</button>
        <button class="btn primary" disabled={!addEntName.trim() || addEntBusy} on:click={submitQuickAddEntity}>
          {addEntBusy ? "保存中…" : "保存卡片"}
        </button>
      </div>
    </div>
  </div>
{/if}

{#if undoToast}
  <div class="undo-toast">
    <span>{undoToast}</span>
    <button class="undo-btn" on:click={triggerUndo}>↩ 撤销</button>
  </div>
{/if}

<style>
  .kb-page {
    display: flex;
    flex-direction: column;
    height: 100%;
    background: var(--bg-base);
    font-family: var(--font-ui);
    color: var(--text-primary);
    overflow: hidden;
  }

  /* Top toolbar */
  .kb-bar {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 14px;
    padding: 12px 22px;
    background: var(--bg-surface);
    border-bottom: 1px solid var(--border);
    flex-shrink: 0;
  }
  .kb-bar-left {
    display: flex;
    align-items: center;
    gap: 14px;
    flex: 1;
    min-width: 0;
  }
  .search-box {
    position: relative;
    display: flex;
    align-items: center;
    width: 340px;
    max-width: 100%;
    background: var(--bg-base);
    border: 1px solid var(--border);
    border-radius: var(--radius-control);
    padding: 0 10px;
    transition: border-color 0.15s, box-shadow 0.15s;
  }
  .search-box:focus-within {
    border-color: var(--accent);
    box-shadow: 0 0 0 2px var(--accent-soft);
  }
  .s-ic {
    color: var(--text-muted);
    font-size: 14px;
    margin-right: 7px;
  }
  .s-input {
    flex: 1;
    background: transparent;
    border: none;
    color: var(--text-primary);
    font-size: 13px;
    padding: 7px 0;
    outline: none;
  }
  .s-clear {
    background: transparent;
    border: none;
    color: var(--text-muted);
    font-size: 15px;
    padding: 0 4px;
    cursor: pointer;
  }
  .kb-stat {
    font-size: 12px;
    color: var(--text-secondary);
    white-space: nowrap;
  }
  .kb-stat b {
    color: var(--text-primary);
    font-family: var(--font-mono);
  }
  .kb-bar-right {
    display: flex;
    align-items: center;
    gap: 8px;
    flex-shrink: 0;
  }

  /* Buttons */
  .btn {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    font-size: 12.5px;
    font-weight: 500;
    padding: 6px 12px;
    border-radius: var(--radius-control);
    border: 1px solid var(--border);
    cursor: pointer;
    transition: all 0.15s ease;
    white-space: nowrap;
  }
  .btn:disabled {
    opacity: 0.45;
    cursor: not-allowed;
  }
  .btn.ghost {
    background: var(--bg-elevated);
    color: var(--text-primary);
  }
  .btn.ghost:hover:not(:disabled) {
    border-color: var(--border-strong);
    background: rgba(255, 255, 255, 0.06);
  }
  .btn.ghost.active-mode {
    border-color: var(--accent);
    color: var(--accent);
    background: var(--accent-soft);
  }
  .btn.primary {
    background: linear-gradient(135deg, #f59e0b 0%, #d97706 100%);
    color: #090a0d;
    font-weight: 600;
    border-color: rgba(245, 158, 11, 0.5);
    box-shadow: 0 2px 10px rgba(245, 158, 11, 0.22);
  }
  .btn.primary:hover:not(:disabled) {
    filter: brightness(1.07);
  }
  .btn.danger {
    background: rgba(248, 113, 113, 0.15);
    color: #f87171;
    border-color: rgba(248, 113, 113, 0.35);
  }
  .btn.sm {
    font-size: 11.5px;
    padding: 4px 10px;
  }

  /* Category filter bar */
  .type-bar {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: 7px;
    padding: 9px 22px;
    background: rgba(19, 21, 26, 0.72);
    border-bottom: 1px solid var(--border);
    flex-shrink: 0;
  }
  .type-chip {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    font-size: 12px;
    padding: 4px 11px;
    border-radius: 999px;
    background: var(--bg-elevated);
    border: 1px solid var(--border);
    color: var(--text-secondary);
    cursor: pointer;
    transition: all 0.14s;
  }
  .type-chip:hover {
    color: var(--text-primary);
    border-color: var(--border-strong);
  }
  .type-chip.on {
    background: rgba(245, 158, 11, 0.14);
    border-color: var(--tc, var(--accent));
    color: #ffffff;
    font-weight: 600;
  }
  .tc-dot {
    width: 7px;
    height: 7px;
    border-radius: 50%;
  }
  .tc-n {
    font-family: var(--font-mono);
    font-size: 11px;
    opacity: 0.75;
  }
  .batch-actions {
    margin-left: auto;
    display: flex;
    align-items: center;
    gap: 10px;
    font-size: 12px;
    color: var(--accent);
  }

  /* Banners */
  .progress-banner, .done-banner {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 10px;
    padding: 8px 22px;
    font-size: 12.5px;
    border-bottom: 1px solid var(--border);
    flex-shrink: 0;
  }
  .progress-banner {
    background: rgba(245, 158, 11, 0.1);
    color: var(--accent);
  }
  .done-banner {
    background: rgba(52, 211, 153, 0.1);
    color: #34d399;
  }
  .done-banner.is-error {
    background: rgba(248, 113, 113, 0.12);
    color: #f87171;
  }
  .banner-close {
    background: transparent;
    border: none;
    color: inherit;
    font-size: 16px;
    cursor: pointer;
  }

  /* Main body layout */
  .kb-body {
    flex: 1;
    display: flex;
    min-height: 0;
    overflow: hidden;
  }
  .kb-empty {
    flex: 1;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    padding: 40px;
    text-align: center;
  }
  .empty-icon {
    font-size: 28px;
    color: var(--accent);
    margin-bottom: 12px;
    opacity: 0.85;
  }
  .empty-title {
    font-size: 16px;
    font-weight: 600;
    color: var(--text-primary);
    margin-bottom: 8px;
  }
  .empty-sub {
    font-size: 13px;
    color: var(--text-muted);
    max-width: 460px;
    line-height: 1.65;
  }
  .empty-actions {
    margin-top: 18px;
    display: flex;
    align-items: center;
    gap: 10px;
  }

  /* Encyclopedia Card Catalog */
  .catalog-pane {
    flex: 1;
    overflow-y: auto;
    padding: 22px 26px 48px;
    min-width: 0;
  }
  .cat-section {
    margin-bottom: 28px;
  }
  .cat-header {
    display: flex;
    align-items: center;
    gap: 8px;
    margin-bottom: 12px;
    padding-bottom: 7px;
    border-bottom: 1px solid var(--border);
  }
  .cat-dot {
    width: 9px;
    height: 9px;
    border-radius: 50%;
  }
  .cat-title {
    margin: 0;
    font-size: 14px;
    font-weight: 600;
    letter-spacing: 0.01em;
    color: var(--text-primary);
  }
  .cat-count {
    font-family: var(--font-mono);
    font-size: 11.5px;
    color: var(--text-muted);
    background: var(--bg-elevated);
    padding: 1px 7px;
    border-radius: 999px;
  }

  .card-grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(300px, 1fr));
    gap: 12px;
  }
  .k-card {
    display: flex;
    flex-direction: column;
    background: var(--bg-surface);
    border: 1px solid var(--border);
    border-left: 3px solid var(--card-accent, var(--accent));
    border-radius: var(--radius-card);
    padding: 13px 15px;
    cursor: pointer;
    transition: transform 0.14s ease, border-color 0.14s ease, box-shadow 0.14s ease, background 0.14s ease;
  }
  .k-card:hover {
    background: var(--bg-elevated);
    border-color: var(--border-strong);
    border-left-color: var(--card-accent, var(--accent));
    transform: translateY(-1px);
    box-shadow: var(--shadow-sm);
  }
  .k-card.active {
    background: linear-gradient(180deg, rgba(245, 158, 11, 0.1) 0%, rgba(27, 30, 38, 0.95) 100%);
    border-color: var(--accent);
    border-left-width: 4px;
    box-shadow: 0 4px 18px rgba(0, 0, 0, 0.35);
  }
  .k-card.checked {
    border-color: var(--accent);
    background: rgba(245, 158, 11, 0.08);
  }
  .kc-head {
    display: flex;
    align-items: center;
    gap: 7px;
    margin-bottom: 8px;
  }
  .kc-title {
    flex: 1;
    font-size: 14px;
    font-weight: 600;
    color: #ffffff;
    line-height: 1.35;
    word-break: break-word;
  }
  .kc-badge {
    font-size: 10.5px;
    padding: 1px 7px;
    border-radius: 999px;
    border: 1px solid;
    white-space: nowrap;
    flex-shrink: 0;
  }
  .kc-del {
    opacity: 0;
    background: transparent;
    border: none;
    color: var(--text-muted);
    font-size: 15px;
    padding: 0 3px;
    cursor: pointer;
    transition: opacity 0.12s, color 0.12s;
  }
  .k-card:hover .kc-del {
    opacity: 1;
  }
  .kc-del:hover {
    color: #f87171;
  }
  .kc-aliases {
    font-size: 11px;
    color: var(--text-muted);
    margin-bottom: 6px;
  }
  .kc-facts {
    margin: 0;
    padding-left: 16px;
    font-size: 12.5px;
    color: var(--text-secondary);
    line-height: 1.55;
  }
  .kc-facts li {
    margin-bottom: 4px;
    display: -webkit-box;
    -webkit-line-clamp: 2;
    -webkit-box-orient: vertical;
    overflow: hidden;
  }
  .kc-more {
    margin-top: 6px;
    font-size: 11.5px;
    color: var(--accent);
    font-weight: 500;
  }
  .kc-empty-obs {
    font-size: 12px;
    color: var(--text-muted);
    font-style: italic;
  }

  /* Card age: knowledge rots, so its age is visible rather than implicit. */
  .kc-age {
    font-family: var(--font-mono);
    font-size: 10px;
    color: var(--text-muted);
    border: 1px solid var(--border);
    border-radius: 999px;
    padding: 1px 6px;
    flex-shrink: 0;
  }
  .kc-age.stale {
    color: #fbbf24;
    border-color: rgba(251, 191, 36, 0.45);
    background: rgba(251, 191, 36, 0.1);
  }

  .prune-card { max-width: 640px; }
  .prune-lead {
    font-size: 12.5px;
    color: var(--text-secondary);
    line-height: 1.65;
    margin: 0 0 12px;
  }
  .prune-list {
    list-style: none;
    margin: 0;
    padding: 0;
    max-height: 46vh;
    overflow: auto;
    border: 1px solid var(--border);
    border-radius: 6px;
  }
  .prune-list li {
    display: flex;
    align-items: baseline;
    gap: 8px;
    padding: 7px 10px;
    border-bottom: 1px solid var(--border);
    font-size: 12px;
  }
  .prune-list li:last-child { border-bottom: none; }
  .pl-name { font-weight: 600; color: var(--text-primary); flex-shrink: 0; }
  .pl-age {
    font-family: var(--font-mono);
    font-size: 10.5px;
    color: var(--text-muted);
    flex-shrink: 0;
  }
  .pl-age.env { color: #fbbf24; }
  .pl-reason {
    font-size: 10px;
    color: var(--text-muted);
    border: 1px solid var(--border);
    border-radius: 999px;
    padding: 0 6px;
    flex-shrink: 0;
  }
  .pl-sample {
    color: var(--text-muted);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .prune-more { font-size: 11.5px; color: var(--text-muted); margin: 8px 0 0; }

  /* Cross-project knowledge: visually set apart so it is never read as a fact
     about the current project. */
  .global-section {
    margin-top: 32px;
    padding-top: 18px;
    border-top: 1px dashed var(--border-strong);
  }
  .global-toggle {
    width: 100%;
    display: flex;
    align-items: center;
    gap: 9px;
    background: transparent;
    border: none;
    padding: 4px 0 12px;
    color: var(--text-secondary);
    cursor: pointer;
    text-align: left;
  }
  .global-toggle:hover { color: var(--text-primary); }
  .g-tag {
    font-size: 10px;
    font-weight: 700;
    letter-spacing: .04em;
    padding: 2px 7px;
    border-radius: 99px;
    color: var(--cyan);
    background: var(--cyan-soft);
    border: 1px solid var(--cyan-border);
  }
  .g-title { font-size: 13.5px; font-weight: 600; color: var(--text-primary); }
  .g-n {
    font-family: var(--font-mono);
    font-size: 11px;
    color: var(--text-muted);
    background: var(--bg-elevated);
    padding: 1px 7px;
    border-radius: 99px;
  }
  .g-hint { font-size: 11.5px; color: var(--text-muted); }
  .g-caret { margin-left: auto; color: var(--text-muted); }
  .g-group { margin-bottom: 18px; }
  .g-group-head {
    display: flex;
    align-items: center;
    gap: 7px;
    font-size: 12px;
    color: var(--text-secondary);
    margin-bottom: 9px;
  }
  .k-card.global-card {
    border-left-color: var(--cyan);
    background: rgba(56, 189, 248, 0.04);
  }
  .k-card.global-card:hover {
    border-left-color: var(--cyan);
    background: rgba(56, 189, 248, 0.08);
  }

  /* Right Detail Drawer */
  .detail-pane {
    width: 400px;
    flex-shrink: 0;
    background: var(--bg-surface);
    border-left: 1px solid var(--border);
    overflow-y: auto;
    padding: 20px 22px 36px;
    display: flex;
    flex-direction: column;
    gap: 20px;
  }
  .dp-head {
    border-bottom: 1px solid var(--border);
    padding-bottom: 14px;
  }
  .dp-title-row {
    display: flex;
    align-items: center;
    gap: 8px;
  }
  .dp-dot {
    width: 10px;
    height: 10px;
    border-radius: 50%;
    flex-shrink: 0;
  }
  .dp-title {
    margin: 0;
    flex: 1;
    font-size: 17px;
    font-weight: 700;
    color: #ffffff;
    line-height: 1.35;
    word-break: break-word;
  }
  .dp-rename-input {
    flex: 1;
    background: var(--bg-base);
    border: 1px solid var(--accent);
    border-radius: var(--radius-control);
    color: var(--text-primary);
    padding: 5px 8px;
    font-size: 14px;
  }
  .icon-btn {
    background: transparent;
    border: none;
    color: var(--text-muted);
    font-size: 14px;
    padding: 4px 6px;
    border-radius: 4px;
    cursor: pointer;
  }
  .icon-btn:hover {
    color: var(--text-primary);
    background: var(--bg-elevated);
  }
  .icon-btn.close {
    font-size: 18px;
  }
  .dp-meta {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: 8px;
    margin-top: 10px;
  }
  .dp-type-tag {
    font-size: 11px;
    padding: 2px 8px;
    border-radius: 999px;
    border: 1px solid;
  }
  .dp-alias {
    font-size: 11.5px;
    color: var(--text-muted);
  }
  .dp-del-btn {
    margin-left: auto;
    background: transparent;
    border: 1px solid rgba(248, 113, 113, 0.3);
    color: #f87171;
    font-size: 11.5px;
    padding: 2px 8px;
    border-radius: 5px;
    cursor: pointer;
  }
  .dp-del-btn:hover {
    background: rgba(248, 113, 113, 0.14);
  }
  .dp-scope-tag {
    font-size: 11px;
    padding: 2px 8px;
    border-radius: 999px;
    color: var(--text-muted);
    border: 1px solid var(--border);
    background: var(--bg-base);
  }
  .dp-scope-tag.global {
    color: var(--cyan);
    border-color: var(--cyan-border);
    background: var(--cyan-soft);
  }
  .dp-scope-row {
    display: flex;
    align-items: center;
    gap: 9px;
    margin-top: 10px;
    flex-wrap: wrap;
  }
  .dp-scope-hint {
    font-size: 11px;
    color: var(--text-muted);
    line-height: 1.5;
  }

  .dp-section {
    display: flex;
    flex-direction: column;
    gap: 10px;
  }
  .dp-sec-head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    font-size: 12px;
    font-weight: 600;
    color: var(--text-secondary);
    text-transform: uppercase;
    letter-spacing: 0.04em;
  }
  .dp-add-fact-btn {
    background: var(--bg-elevated);
    border: 1px solid var(--border);
    color: var(--accent);
    font-size: 11.5px;
    padding: 3px 9px;
    border-radius: 5px;
    cursor: pointer;
  }
  .dp-add-fact-btn:hover {
    border-color: var(--accent);
  }

  .dp-fact-list {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 10px;
  }
  .dp-fact-item {
    background: var(--bg-elevated);
    border: 1px solid var(--border);
    border-radius: var(--radius-control);
    padding: 11px 13px;
  }
  .dp-fact-text {
    font-size: 13px;
    line-height: 1.6;
    color: var(--text-primary);
    word-break: break-word;
  }
  .dp-fact-foot {
    display: flex;
    align-items: center;
    justify-content: space-between;
    margin-top: 8px;
    padding-top: 6px;
    border-top: 1px dashed rgba(255, 255, 255, 0.06);
    font-size: 11px;
  }
  .src-link {
    background: transparent;
    border: none;
    color: var(--cyan);
    font-family: var(--font-mono);
    font-size: 11px;
    padding: 0;
    cursor: pointer;
  }
  .src-link:hover {
    text-decoration: underline;
  }
  .src-manual {
    color: var(--text-muted);
  }
  .dp-fact-ops {
    display: flex;
    gap: 8px;
    margin-left: auto;
  }
  .op-link {
    background: transparent;
    border: none;
    color: var(--text-muted);
    font-size: 11px;
    padding: 0;
    cursor: pointer;
  }
  .op-link:hover {
    color: var(--text-primary);
  }
  .op-link.danger:hover {
    color: #f87171;
  }

  .fact-editor {
    display: flex;
    flex-direction: column;
    gap: 8px;
  }
  .fact-editor textarea {
    width: 100%;
    background: var(--bg-base);
    border: 1px solid var(--accent);
    border-radius: var(--radius-control);
    color: var(--text-primary);
    font-size: 12.5px;
    padding: 8px;
    font-family: var(--font-ui);
  }
  .fe-actions {
    display: flex;
    gap: 6px;
    justify-content: flex-end;
  }

  .rel-list {
    display: flex;
    flex-direction: column;
    gap: 6px;
  }
  .rel-chip {
    display: flex;
    align-items: center;
    gap: 7px;
    text-align: left;
    background: var(--bg-elevated);
    border: 1px solid var(--border);
    border-radius: var(--radius-control);
    padding: 7px 10px;
    font-size: 12.5px;
    color: var(--text-primary);
    cursor: pointer;
    transition: border-color 0.14s, background 0.14s;
  }
  .rel-chip:hover {
    border-color: var(--accent);
    background: rgba(245, 158, 11, 0.08);
  }
  .rel-pred {
    font-size: 11px;
    color: var(--text-muted);
    background: var(--bg-base);
    padding: 1px 6px;
    border-radius: 4px;
  }
  .rel-dir {
    color: var(--accent);
    font-family: var(--font-mono);
  }
  .rel-target {
    font-weight: 600;
    color: #ffffff;
  }

  /* Modals */
  .modal-backdrop {
    position: fixed;
    inset: 0;
    background: rgba(5, 6, 8, 0.72);
    backdrop-filter: blur(6px);
    display: flex;
    align-items: center;
    justify-content: center;
    z-index: 90;
  }
  .modal-card {
    width: 460px;
    max-width: 92vw;
    max-height: 86vh;
    overflow-y: auto;
    background: var(--bg-surface);
    border: 1px solid var(--border-strong);
    border-radius: var(--radius-modal);
    padding: 20px 22px;
    box-shadow: var(--shadow-lg);
  }
  .modal-card.wide {
    width: 640px;
  }
  .modal-head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    margin-bottom: 16px;
  }
  .modal-head h3 {
    margin: 0;
    font-size: 15px;
    font-weight: 600;
  }
  .form-row {
    display: flex;
    flex-direction: column;
    gap: 6px;
    margin-bottom: 14px;
  }
  .form-row label {
    font-size: 12px;
    color: var(--text-secondary);
  }
  .form-row input, .form-row select, .form-row textarea {
    background: var(--bg-base);
    border: 1px solid var(--border);
    border-radius: var(--radius-control);
    color: var(--text-primary);
    padding: 8px 10px;
    font-size: 13px;
    font-family: var(--font-ui);
  }
  .modal-foot {
    display: flex;
    justify-content: flex-end;
    gap: 8px;
    margin-top: 16px;
  }

  /* Undo Toast */
  .undo-toast {
    position: fixed;
    bottom: 22px;
    right: 26px;
    display: flex;
    align-items: center;
    gap: 12px;
    background: var(--bg-elevated);
    border: 1px solid var(--accent);
    border-radius: var(--radius-control);
    padding: 10px 16px;
    font-size: 13px;
    color: var(--text-primary);
    box-shadow: var(--shadow-lg);
    z-index: 95;
  }
  .undo-btn {
    background: var(--accent);
    color: #090a0d;
    border: none;
    border-radius: 5px;
    padding: 3px 9px;
    font-size: 12px;
    font-weight: 600;
    cursor: pointer;
  }
</style>


