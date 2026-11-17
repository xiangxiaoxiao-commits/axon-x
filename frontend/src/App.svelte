<script lang="ts">
  import { onMount, onDestroy } from "svelte";
  import {
    activeView,
    currentProject,
    projects,
    loadProjects,
    namespaces,
    selectedNamespaces,
    effectiveNamespaces,
    loadNamespaces,
    clusters,
    clusterFor,
    loadClusters,
    projectOptions,
    nsLoadError,
    sidebarCollapsed,
    commandPaletteOpen,
    type View,
  } from "./lib/stores";
  import GraphView from "./views/GraphView.svelte";
  import SettingsView from "./views/SettingsView.svelte";
  import AboutView from "./views/AboutView.svelte";
  import SessionsView from "./views/SessionsView.svelte";
  import TerminalView from "./views/TerminalView.svelte";
  import CommandPalette from "./views/CommandPalette.svelte";

  const NAV: { view: View; label: string; icon: string; shortcut: string }[] = [
    { view: "graph", label: "知识库", icon: "✦", shortcut: "⌘1" },
    { view: "sessions", label: "会话", icon: "⇆", shortcut: "⌘2" },
    { view: "terminal", label: "终端", icon: "❯", shortcut: "⌘3" },
    { view: "settings", label: "设置", icon: "⚙", shortcut: "⌘4" },
  ];

  let ready = false;
  let showAbout = false;
  let pickerFilter = "";
  // Expanded by default: indexable-but-empty projects are the only actionable
  // thing in the sidebar when the knowledge base is fresh.
  let showUnindexedProjects = true;

  onMount(async () => {
    ready = true;
    window.addEventListener("keydown", onGlobalKeydown);
    try {
      await loadProjects();
      await loadNamespaces();
      await loadClusters();
    } catch (e) {
      // Surface bootstrap failures instead of rendering an empty sidebar that
      // looks like "there is no knowledge".
      document.title = "Axon-x — 加载失败: " + String(e);
    }
  });

  onDestroy(() => {
    window.removeEventListener("keydown", onGlobalKeydown);
  });

  function onGlobalKeydown(e: KeyboardEvent) {
    const mod = e.metaKey || e.ctrlKey;
    if (!mod) return;
    const k = e.key.toLowerCase();
    if (k === "k") {
      e.preventDefault();
      $commandPaletteOpen = !$commandPaletteOpen;
      return;
    }
    if (k === "b") {
      e.preventDefault();
      $sidebarCollapsed = !$sidebarCollapsed;
      return;
    }
    if (k >= "1" && k <= "4") {
      const idx = parseInt(k, 10) - 1;
      if (NAV[idx]) {
        e.preventDefault();
        $activeView = NAV[idx].view;
      }
    }
  }

  function refreshProviders() {}

  function go(v: View) {
    $activeView = v;
  }

  // Unified workspace items: combine knowledge namespaces with Claude Code projects
  type UnifiedItem = {
    key: string;
    entities: number;
    sessions: number;
    hasGraph: boolean;
    extraMerged: string[];
    mergedTotalEntities: number;
    label: string;
  };

  $: options = projectOptions($projects);
  $: unifiedItems = buildUnifiedItems($namespaces, options, $clusters, pickerFilter);
  $: graphItems = unifiedItems.filter((i) => i.hasGraph);
  $: unindexedItems = unifiedItems.filter((i) => !i.hasGraph);

  // A reset knowledge base has no loaded projects at all, so the one actionable
  // group would be collapsed behind a chevron and the app would look empty.
  let autoExpandedUnindexed = false;
  $: if (!autoExpandedUnindexed && $namespaces.length > 0 && graphItems.length === 0) {
    autoExpandedUnindexed = true;
    showUnindexedProjects = true;
  }

  function buildUnifiedItems(
    nsList: typeof $namespaces,
    projOpts: typeof options,
    clusterList: typeof $clusters,
    filterText: string,
  ): UnifiedItem[] {
    const q = filterText.trim().toLowerCase();
    const map = new Map<string, UnifiedItem>();

    for (const ns of nsList) {
      // Cross-project knowledge is not a project: it is shown inside the
      // knowledge view as its own section, never as a selectable workspace.
      if (ns.name === "_global_") continue;
      const cl = clusterFor(ns.name, clusterList);
      const extra = (cl?.slugs ?? [ns.name]).filter((s) => s !== ns.name);
      map.set(ns.name, {
        key: ns.name,
        entities: ns.entities,
        sessions: ns.sessions ?? 0,
        // A namespace with zero cards is an indexable project that has not been
        // distilled yet: it belongs in the collapsed "no knowledge yet" group,
        // not in the main list pretending to be a loaded knowledge base.
        hasGraph: ns.entities > 0,
        extraMerged: extra,
        mergedTotalEntities: cl?.entities ?? ns.entities,
        // Knowledge is scoped by directory, so the directory is what the picker
        // shows; the internal namespace name stays as the key.
        label: shortPath(ns.path) || ns.name,
      });
    }

    for (const o of projOpts) {
      const existing = map.get(o.key);
      if (existing) {
        // Keep the directory label already set above; the Claude option's
        // "namespace · path" form is noisier and hides the directory scoping.
        existing.sessions += o.sessions;
      } else {
        map.set(o.key, {
          key: o.key,
          entities: 0,
          sessions: o.sessions,
          hasGraph: false,
          extraMerged: [],
          mergedTotalEntities: 0,
          label: shortPath(o.label.split(" · ").pop()) || o.label,
        });
      }
    }

    return [...map.values()].filter((it) => {
      if (!q) return true;
      return it.key.toLowerCase().includes(q) || it.label.toLowerCase().includes(q);
    });
  }

  // Only the last two path segments; the full path stays in the tooltip.
  function shortPath(p?: string): string {
    if (!p) return "";
    const parts = p.replace(/\/+$/, "").split("/").filter(Boolean);
    if (parts.length <= 2) return p;
    return "…/" + parts.slice(-2).join("/");
  }

  // Primary click: switch both currentProject & selectedNamespaces to this workspace
  function selectPrimary(item: UnifiedItem) {
    currentProject.set(item.key);
    if (item.hasGraph) {
      selectedNamespaces.set([item.key]);
    } else {
      selectedNamespaces.set([]);
    }
  }
</script>

<div class="app">
  {#if ready}
    <div class="shell">
      <nav class="sidebar" class:collapsed={$sidebarCollapsed}>
        <div class="brand-row">
          {#if !$sidebarCollapsed}
            <button class="brand" on:click={() => (showAbout = true)} title="查看功能与原理">
              axon<span class="brand-hint">ⓘ</span>
            </button>
          {/if}
          <button
            class="collapse-btn"
            on:click={() => ($sidebarCollapsed = !$sidebarCollapsed)}
            title={$sidebarCollapsed ? "展开侧边栏 (⌘B)" : "折叠侧边栏 (⌘B)"}
          >
            {$sidebarCollapsed ? "»" : "«"}
          </button>
        </div>

        {#if !$sidebarCollapsed}
          <button class="cmdk-trigger" on:click={() => ($commandPaletteOpen = true)} title="全局检索实体、会话与快捷命令 (⌘K)">
            <span class="cmdk-ic">⌕</span>
            <span class="cmdk-tx">快速检索 / 命令…</span>
            <kbd class="cmdk-kbd">⌘K</kbd>
          </button>
        {:else}
          <button class="nav-item collapsed-cmdk" on:click={() => ($commandPaletteOpen = true)} title="快速检索 / 命令 (⌘K)">
            <span class="ic">⌕</span>
          </button>
        {/if}

        <div class="nav">
          {#each NAV as n}
            <button
              class="nav-item"
              class:active={$activeView === n.view}
              on:click={() => go(n.view)}
              title={`${n.label} (${n.shortcut})`}
            >
              <span class="ic">{n.icon}</span>
              {#if !$sidebarCollapsed}
                <span class="tx">{n.label}</span>
                <span class="sc">{n.shortcut}</span>
              {/if}
            </button>
          {/each}
        </div>

        {#if !$sidebarCollapsed}
          <div class="ns-pick">
            <div class="ns-head">
              <span class="ns-label">
                项目知识库
                {#if $clusters.some((c) => c.slugs.length > 1)}
                  <span class="ns-auto" title="共享实体的同域命名空间已自动合并">自动归组</span>
                {/if}
              </span>
            </div>

            {#if $namespaces.length + options.length > 5}
              <input
                class="ns-filter"
                placeholder="筛选项目…"
                bind:value={pickerFilter}
              />
            {/if}

            <div class="ns-list">
              {#each graphItems as item}
                {@const isAutoMerged = !$selectedNamespaces.includes(item.key) && $effectiveNamespaces.includes(item.key)}
                <div
                  class="ws-item"
                  class:on={$selectedNamespaces.includes(item.key)}
                  class:auto-on={isAutoMerged}
                  class:proj-only={!$selectedNamespaces.includes(item.key) && $currentProject === item.key}
                  on:click={() => selectPrimary(item)}
                  role="button"
                  tabindex="0"
                  on:keydown={(e) => e.key === "Enter" && selectPrimary(item)}
                  title={item.extraMerged.length
                    ? `${item.label}\n与 ${item.extraMerged.join("、")} 同域自动合并（共 ${item.mergedTotalEntities} 条卡片）`
                    : `${item.label}\n${item.entities} 条知识卡片${item.sessions ? ` · ${item.sessions} 会话` : ""}`}
                >
                  <span class="ws-name">{item.label}</span>
                  <span class="ws-badges">
                    <span class="ws-cnt" title="知识卡片数">{item.entities}</span>
                    {#if item.extraMerged.length}
                      <span class="ws-merge" title="自动并入 {item.extraMerged.join('、')}">+{item.extraMerged.length}</span>
                    {/if}
                  </span>
                </div>
              {/each}

              {#if unindexedItems.length > 0}
                <button
                  class="unindexed-toggle"
                  on:click={() => (showUnindexedProjects = !showUnindexedProjects)}
                >
                  <span>可提炼的项目 ({unindexedItems.length})</span>
                  <span>{showUnindexedProjects ? "▾" : "▸"}</span>
                </button>
                {#if showUnindexedProjects || pickerFilter.trim()}
                  {#each unindexedItems as item}
                    <button
                      class="ws-item unindexed"
                      class:proj-only={$currentProject === item.key}
                      on:click={() => selectPrimary(item)}
                      title={`${item.label}（${item.sessions} 个会话，尚未提炼知识）`}
                    >
                      <span class="ws-name">{item.label && item.label.length <= 34 ? item.label : item.key}</span>
                      {#if item.sessions}<span class="ws-cnt">{item.sessions} 会话</span>{/if}
                    </button>
                  {/each}
                {/if}
              {/if}
            </div>
            {#if $nsLoadError}
              <div class="ns-error">知识库列表加载失败：{$nsLoadError}</div>
            {/if}
          </div>
        {/if}
      </nav>

      <main class="view">
        {#if $activeView === "settings"}
          <SettingsView onSaved={refreshProviders} />
        {:else if $activeView === "sessions"}
          <SessionsView />
        {:else if $activeView !== "terminal"}
          <GraphView />
        {/if}
        <div class="term-layer" class:hidden={$activeView !== "terminal"}>
          <TerminalView />
        </div>
      </main>
    </div>
  {/if}

  <CommandPalette />

  {#if showAbout}
    <AboutView onClose={() => (showAbout = false)} />
  {/if}
</div>

<style>
  .app { height: 100vh; overflow: hidden; background: var(--bg-base); }
  .shell { height: 100vh; display: flex; }

  .sidebar {
    width: 226px; flex: 0 0 226px; display: flex; flex-direction: column;
    background: linear-gradient(180deg, #13161d 0%, #0d0f14 100%);
    border-right: 1px solid var(--border);
    box-shadow: inset -1px 0 0 rgba(255, 255, 255, 0.025);
    transition: width 0.2s cubic-bezier(0.22, 1, 0.36, 1), flex-basis 0.2s cubic-bezier(0.22, 1, 0.36, 1);
  }
  .sidebar.collapsed {
    width: 56px; flex: 0 0 56px;
  }

  .brand-row {
    display: flex; align-items: center; justify-content: space-between;
    padding: 16px 12px 10px 16px;
  }
  .sidebar.collapsed .brand-row {
    justify-content: center; padding: 16px 4px 10px;
  }
  .brand {
    display: flex; align-items: center; gap: 7px;
    padding: 2px 0; font-family: var(--font-ui); font-weight: 800;
    font-size: 16px; letter-spacing: 0.8px;
    background: linear-gradient(135deg, #fde68a 0%, var(--accent) 60%, #fb923c 100%);
    -webkit-background-clip: text;
    -webkit-text-fill-color: transparent;
    border: none; cursor: pointer; text-align: left;
  }
  .brand:hover { filter: brightness(1.15); }
  .brand-hint {
    font-size: 11px; opacity: .65; font-weight: 400;
    -webkit-text-fill-color: var(--text-muted);
  }
  .brand:hover .brand-hint { opacity: 1; }

  .collapse-btn {
    background: transparent; border: 1px solid transparent; color: var(--text-muted);
    border-radius: 6px; width: 24px; height: 24px; font-size: 13px;
    display: flex; align-items: center; justify-content: center; cursor: pointer;
  }
  .collapse-btn:hover {
    background: var(--bg-elevated); border-color: var(--border); color: var(--text-primary);
  }

  .cmdk-trigger {
    margin: 2px 12px 10px; padding: 8px 11px;
    display: flex; align-items: center; gap: 8px;
    background: rgba(27, 30, 38, 0.72); border: 1px solid var(--border);
    border-radius: var(--radius-control); color: var(--text-muted);
    font-size: 12px; cursor: pointer;
    box-shadow: var(--shadow-sm);
  }
  .cmdk-trigger:hover {
    border-color: var(--accent-border); color: var(--text-primary);
    background: var(--bg-elevated);
  }
  .cmdk-ic { font-size: 13px; color: var(--accent); font-weight: 700; }
  .cmdk-tx { flex: 1; text-align: left; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
  .cmdk-kbd {
    font-family: var(--font-mono); font-size: 9.5px; padding: 1px 5px; border-radius: 4px;
    background: var(--bg-base); border: 1px solid var(--border); color: var(--text-muted);
  }
  .collapsed-cmdk {
    margin: 2px auto 6px; width: 38px !important; justify-content: center;
  }

  .nav { padding: 2px 10px 6px; }
  .sidebar.collapsed .nav { padding: 2px 8px; }
  .nav-item {
    width: 100%; display: flex; align-items: center; gap: 10px;
    padding: 8.5px 12px; margin-bottom: 3px; border-radius: var(--radius-control);
    background: transparent; border: 1px solid transparent; color: var(--text-muted);
    font-size: 13px; text-align: left; cursor: pointer;
  }
  .sidebar.collapsed .nav-item {
    justify-content: center; padding: 9px 0;
  }
  .nav-item:hover {
    background: rgba(255, 255, 255, 0.04); color: var(--text-primary);
  }
  .nav-item.active {
    background: linear-gradient(90deg, rgba(245, 158, 11, 0.16) 0%, rgba(245, 158, 11, 0.03) 100%);
    color: var(--text-primary);
    border-color: rgba(245, 158, 11, 0.25);
    font-weight: 600;
    box-shadow: inset 2.5px 0 0 var(--accent);
  }
  .nav-item .ic { width: 17px; text-align: center; font-size: 14px; flex-shrink: 0; }
  .nav-item.active .ic {
    color: var(--accent);
    text-shadow: 0 0 10px rgba(245, 158, 11, 0.45);
  }
  .nav-item .tx { flex: 1; }
  .nav-item .sc { font-size: 9.5px; opacity: 0; color: var(--text-muted); font-family: var(--font-mono); transition: opacity 0.14s; }
  .nav-item:hover .sc { opacity: 0.72; }

  .ns-pick {
    flex: 1; min-height: 0;
    display: flex; flex-direction: column; gap: 8px;
    padding: 14px 12px; border-top: 1px solid var(--border);
  }
  .ns-head {
    display: flex; align-items: center; justify-content: space-between; gap: 4px;
  }
  .ns-label {
    font-size: 11px; font-weight: 600; color: var(--text-muted);
    letter-spacing: 0.4px;
    display: flex; align-items: center; gap: 6px;
  }
  .ns-auto {
    font-size: 9.5px; font-weight: 500; padding: 1px 6px; border-radius: 99px;
    background: var(--accent-soft); border: 1px solid var(--accent-border); color: var(--accent-hover);
  }
  .ns-filter {
    width: 100%; background: var(--bg-base); color: var(--text-primary);
    border: 1px solid var(--border); border-radius: var(--radius-control);
    font-size: 11.5px; padding: 6px 9px; outline: none;
  }
  .ns-filter:focus { border-color: var(--accent); }

  .ns-list {
    flex: 1; min-height: 0; overflow-y: auto;
    display: flex; flex-direction: column; gap: 4px;
    padding-right: 2px;
  }
  .ws-item {
    width: 100%; display: flex; align-items: center; justify-content: space-between; gap: 6px;
    background: rgba(27, 30, 38, 0.48); color: var(--text-secondary);
    border: 1px solid transparent; border-radius: var(--radius-control);
    font-size: 12px; padding: 7px 10px;
    cursor: pointer; transition: all 0.14s ease; text-align: left;
  }
  .ws-item:hover {
    border-color: var(--border); background: var(--bg-elevated); color: var(--text-primary);
  }
  .ws-item.on {
    background: linear-gradient(90deg, rgba(245, 158, 11, 0.16) 0%, rgba(245, 158, 11, 0.04) 100%);
    color: #ffffff; border-color: rgba(245, 158, 11, 0.32); font-weight: 600;
    box-shadow: inset 2.5px 0 0 var(--accent);
  }
  .ws-item.auto-on {
    border-color: var(--accent-border); color: var(--text-primary);
    background: var(--accent-soft);
  }
  .ws-item.proj-only {
    border-color: var(--cyan-border); color: var(--cyan);
    background: var(--cyan-soft);
  }
  .ws-name {
    flex: 1; min-width: 0; white-space: nowrap; overflow: hidden; text-overflow: ellipsis;
    font-family: var(--font-ui); font-size: 12.5px; font-weight: 500;
  }
  .ws-badges {
    display: inline-flex; align-items: center; gap: 4px; flex-shrink: 0;
    font-family: var(--font-mono);
  }
  .ws-cnt {
    font-size: 10.5px; color: var(--text-muted);
    background: rgba(255, 255, 255, 0.05);
    padding: 1px 6px; border-radius: 99px;
  }
  .ws-item.on .ws-cnt {
    color: var(--accent-hover);
    background: rgba(245, 158, 11, 0.16);
  }
  .ws-merge { font-size: 9.5px; color: var(--accent); font-weight: 700; }

  .unindexed-toggle {
    margin-top: 6px; padding: 5px 4px;
    display: flex; align-items: center; justify-content: space-between;
    background: transparent; border: none; color: var(--text-muted);
    font-size: 10.5px; cursor: pointer;
  }
  .unindexed-toggle:hover { color: var(--text-primary); }
  .ns-error {
    margin-top: 6px; padding: 6px 8px; border-radius: 5px;
    background: rgba(248, 113, 113, 0.12); border: 1px solid rgba(248, 113, 113, 0.35);
    color: #f87171; font-size: 10.5px; line-height: 1.5; word-break: break-all;
  }
  .ws-item.unindexed {
    opacity: 0.82; font-size: 11px;
  }

  .view { flex: 1; min-width: 0; height: 100vh; position: relative; }
  .term-layer { position: absolute; inset: 0; }
  .term-layer.hidden { display: none; }
</style>
