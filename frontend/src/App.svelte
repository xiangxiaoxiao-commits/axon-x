<script lang="ts">
  import { onMount } from "svelte";
  import { activeView, currentProject, projects, loadProjects, namespaces, selectedNamespaces, loadNamespaces, type View } from "./lib/stores";
  import GraphView from "./views/GraphView.svelte";
  import SettingsView from "./views/SettingsView.svelte";
  import AboutView from "./views/AboutView.svelte";
  import SessionsView from "./views/SessionsView.svelte";
  import TerminalView from "./views/TerminalView.svelte";

  // Navigation is a plain sidebar (VSCode/Linear style): a narrow vertical rail
  // of icon + label entries. The product is an agent-context enhancer: build a
  // per-project knowledge graph and feed it to Claude Code over MCP. Kept
  // intentionally focused — browse/curate business knowledge, and configure
  // providers + MCP one-click install.
  const NAV: { view: View; label: string; icon: string }[] = [
    { view: "graph", label: "知识", icon: "⊹" },
    { view: "sessions", label: "会话", icon: "⇆" },
    { view: "terminal", label: "终端", icon: "❯" },
    { view: "settings", label: "设置", icon: "⚙" },
  ];

  let ready = false;
  let showAbout = false;
  onMount(async () => { ready = true; await loadNamespaces(); await loadProjects(); });
  function refreshProviders() {}

  function go(v: View) { $activeView = v; }

  function toggleNs(name: string) {
    // Single-select: each namespace is an independent graph, click to switch.
    selectedNamespaces.set([name]);
    currentProject.set(name);
  }
</script>

<div class="app">
  {#if ready}
    <div class="shell">
      <nav class="sidebar">
        <button class="brand" on:click={() => (showAbout = true)} title="查看功能与原理">axon<span class="brand-hint">ⓘ</span></button>
        <div class="nav">
          {#each NAV as n}
            <button class="nav-item" class:active={$activeView === n.view} on:click={() => go(n.view)}>
              <span class="ic">{n.icon}</span>
              <span class="tx">{n.label}</span>
            </button>
          {/each}
        </div>
        <div class="ns-pick">
          <span class="ns-label">图谱</span>
          <div class="ns-tags">
            {#each $namespaces as ns}
              <button class="ns-tag" class:on={$selectedNamespaces.includes(ns.name)}
                on:click={() => toggleNs(ns.name)} title="{ns.entities} 实体">
                {ns.name}<span class="ns-cnt">{ns.entities}</span>
              </button>
            {/each}
          </div>
        </div>
      </nav>

      <main class="view">
        {#if $activeView === "settings"}
          <SettingsView onSaved={refreshProviders} />
        {:else if $activeView === "sessions"}
          <SessionsView />
        {:else if $activeView !== "terminal"}
          <GraphView />
        {/if}
        <!-- Terminal stays mounted so its shell (and any running claude
             --resume session) survives tab switches; we only hide it. -->
        <div class="term-layer" class:hidden={$activeView !== "terminal"}>
          <TerminalView />
        </div>
      </main>
    </div>
  {/if}

  {#if showAbout}
    <AboutView onClose={() => (showAbout = false)} />
  {/if}
</div>

<style>
  .app { height: 100vh; overflow: hidden; background: var(--bg-base); }
  .shell { height: 100vh; display: flex; }

  .sidebar {
    width: 176px; flex: 0 0 176px; display: flex; flex-direction: column;
    background: var(--bg-surface); border-right: 1px solid var(--border);
  }
  .brand {
    display: flex; align-items: center; gap: 6px;
    padding: 14px 16px 10px; font-family: var(--font-mono); font-weight: 700;
    font-size: 14px; letter-spacing: 1px; color: var(--accent);
    background: transparent; border: none; cursor: pointer; text-align: left;
  }
  .brand:hover { filter: brightness(1.15); }
  .brand-hint { font-size: 11px; opacity: .55; font-weight: 400; }
  .brand:hover .brand-hint { opacity: 1; }
  .nav { flex: 1; overflow-y: auto; padding: 4px 8px; }
  .nav-item {
    width: 100%; display: flex; align-items: center; gap: 10px;
    padding: 8px 10px; margin-bottom: 2px; border-radius: var(--radius-control);
    background: transparent; border: none; color: var(--text-muted);
    font-size: 13px; text-align: left;
  }
  .nav-item:hover { background: var(--bg-elevated); color: var(--text-primary); }
  .nav-item.active { background: var(--bg-elevated); color: var(--text-primary); box-shadow: inset 2px 0 0 var(--accent); }
  .nav-item .ic { width: 16px; text-align: center; font-size: 14px; }
  .nav-item.active .ic { color: var(--accent); }

  .ns-pick {
    display: flex; flex-direction: column; gap: 4px;
    padding: 10px 12px; border-top: 1px solid var(--border);
  }
  .ns-label { font-size: 11px; color: var(--text-muted); font-family: var(--font-mono); }
  .ns-tags { display: flex; flex-wrap: wrap; gap: 4px; }
  .ns-tag {
    display: inline-flex; align-items: center; gap: 3px;
    background: var(--bg-elevated); color: var(--text-muted);
    border: 1px solid var(--border); border-radius: var(--radius-control);
    font-family: var(--font-mono); font-size: 11px; padding: 2px 7px;
    cursor: pointer; transition: all 0.15s;
  }
  .ns-tag:hover { border-color: var(--accent); color: var(--text-primary); }
  .ns-tag.on { background: var(--accent); color: #1c1206; border-color: var(--accent); font-weight: 600; }
  .ns-cnt { font-size: 9px; opacity: 0.7; }

  .view { flex: 1; min-width: 0; height: 100vh; position: relative; }
  .term-layer { position: absolute; inset: 0; }
  .term-layer.hidden { display: none; }
</style>
