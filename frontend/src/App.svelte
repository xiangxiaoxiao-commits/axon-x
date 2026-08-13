<script lang="ts">
  import { onMount } from "svelte";
  import { activeView, currentProject, projects, loadProjects, type View } from "./lib/stores";
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
  onMount(async () => { ready = true; await loadProjects(); });
  function refreshProviders() {}

  function go(v: View) { $activeView = v; }
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
        <label class="proj-pick">
          <span class="proj-label">项目</span>
          <select bind:value={$currentProject}>
            <option value="">全部项目</option>
            {#each $projects as p}<option value={p.slug}>{p.path}</option>{/each}
          </select>
        </label>
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

  .proj-pick {
    display: flex; flex-direction: column; gap: 4px;
    padding: 10px 12px; border-top: 1px solid var(--border);
  }
  .proj-label { font-size: 11px; color: var(--text-muted); font-family: var(--font-mono); }
  .proj-pick select {
    background: var(--bg-elevated); color: var(--text-primary);
    border: 1px solid var(--border); border-radius: var(--radius-control);
    font-family: var(--font-mono); font-size: 12px; padding: 4px 8px; outline: none;
  }
  .proj-pick select:focus { border-color: var(--accent); }

  .view { flex: 1; min-width: 0; height: 100vh; position: relative; }
  .term-layer { position: absolute; inset: 0; }
  .term-layer.hidden { display: none; }
</style>
