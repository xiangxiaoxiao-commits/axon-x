<script lang="ts">
  import { onMount } from "svelte";
  import { activeView, currentProject, projects, loadProjects, type View } from "./lib/stores";
  import NeuralHub from "./views/NeuralHub.svelte";
  import SearchView from "./views/SearchView.svelte";
  import SessionsView from "./views/SessionsView.svelte";
  import GraphView from "./views/GraphView.svelte";
  import MemoryManagerView from "./views/MemoryManagerView.svelte";
  import ReplView from "./views/ReplView.svelte";
  import TerminalView from "./views/TerminalView.svelte";
  import SettingsView from "./views/SettingsView.svelte";

  // Navigation is a neural hub, not a menu rail: home is a synapse network of
  // feature-neurons; entering a feature shows it full-screen with a small
  // "back to nucleus" node in the corner.
  let terminalOpened = false;
  $: if ($activeView === "terminal") terminalOpened = true;

  let ready = false;
  onMount(async () => { $activeView = "hub"; ready = true; await loadProjects(); });
  function refreshProviders() {}

  // Views that get the shared top bar (project picker + back). Hub is
  // navigation; terminal doesn't care about projects.
  $: showTopbar = $activeView !== "hub" && $activeView !== "terminal";

  function toHub() { $activeView = "hub"; }
  function onKey(e: KeyboardEvent) {
    if (e.key === "Escape" && $activeView !== "hub") { $activeView = "hub"; }
  }
</script>

<svelte:window on:keydown={onKey} />

<div class="app">
  {#if ready}
    {#if $activeView === "hub"}
      <NeuralHub on:go={(e) => ($activeView = e.detail)} />
    {:else}
      <div class="shell">
        {#if showTopbar}
          <div class="topbar">
            <button class="back" title="返回中枢 (Esc)" on:click={toHub} aria-label="返回中枢">
              <span class="dot"></span>
            </button>
            <label class="proj-pick">
              <span class="proj-label">项目</span>
              <select bind:value={$currentProject}>
                <option value="">全部项目</option>
                {#each $projects as p}<option value={p.slug}>{p.path}</option>{/each}
              </select>
            </label>
          </div>
        {/if}
        <div class="view" class:with-topbar={showTopbar}>
          {#if $activeView === "search"}
            <SearchView />
          {:else if $activeView === "sessions"}
            <SessionsView />
          {:else if $activeView === "graph"}
            <GraphView />
          {:else if $activeView === "memory"}
            <MemoryManagerView />
          {:else if $activeView === "chat"}
            <ReplView />
          {:else if $activeView === "settings"}
            <SettingsView onSaved={refreshProviders} />
          {/if}
        </div>
      </div>
    {/if}
    <!-- Terminal stays mounted once opened so the shell survives switches. -->
    {#if terminalOpened}
      <div class="term-layer" class:hidden={$activeView !== "terminal"}>
        <button class="back" title="返回中枢 (Esc)" on:click={toHub} aria-label="返回中枢"><span class="dot"></span></button>
        <div class="view"><TerminalView /></div>
      </div>
    {/if}
  {/if}
</div>

<style>
  .app { height: 100vh; overflow: hidden; position: relative; background: var(--bg-base); }
  .shell { height: 100vh; display: flex; flex-direction: column; }
  .view { height: 100vh; }
  .view.with-topbar { height: auto; flex: 1; min-height: 0; }
  .term-layer { position: absolute; inset: 0; background: var(--bg-base); }
  .term-layer.hidden { display: none; }

  /* Shared top bar: back synapse + global project picker. */
  .topbar {
    display: flex; align-items: center; gap: 12px;
    padding: 8px 12px; border-bottom: 1px solid var(--border);
    background: var(--bg-surface); position: relative; z-index: 40;
  }
  .topbar .back { position: static; top: auto; left: auto; }
  .proj-pick { display: flex; align-items: center; gap: 6px; }
  .proj-label { font-size: 11px; color: var(--text-muted); font-family: var(--font-mono); }
  .proj-pick select {
    background: var(--bg-elevated); color: var(--text-primary);
    border: 1px solid var(--border); border-radius: var(--radius-control);
    font-family: var(--font-mono); font-size: 12px; padding: 4px 8px;
    max-width: 340px; outline: none;
  }
  .proj-pick select:focus { border-color: var(--accent); }

  /* Back-to-hub synapse: a small pulsing neuron in the top-left corner. */
  .back {
    position: absolute; top: 10px; left: 10px; z-index: 50;
    width: 30px; height: 30px; border-radius: 50%;
    background: var(--bg-elevated); border: 1px solid var(--border);
    display: flex; align-items: center; justify-content: center; padding: 0;
  }
  .back:hover { border-color: var(--accent); }
  .back .dot { width: 10px; height: 10px; border-radius: 50%; background: var(--accent); box-shadow: 0 0 8px var(--accent); }
  .back:hover .dot { animation: back-pulse 0.9s ease-in-out infinite; }
  @keyframes back-pulse { 0%,100% { transform: scale(1); } 50% { transform: scale(1.35); } }
</style>
