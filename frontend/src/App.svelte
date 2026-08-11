<script lang="ts">
  import { onMount } from "svelte";
  import { activeView, type View } from "./lib/stores";
  import SessionsView from "./views/SessionsView.svelte";
  import GraphView from "./views/GraphView.svelte";
  import MemoryManagerView from "./views/MemoryManagerView.svelte";
  import ReplView from "./views/ReplView.svelte";
  import TerminalView from "./views/TerminalView.svelte";
  import SettingsView from "./views/SettingsView.svelte";

  // Core purpose: browse Claude Code's saved sessions and manage its memory.
  // Chat/terminal are secondary. No first-run gate — the core reads local
  // files and needs no API key.
  const nav: [View, string, string][] = [
    ["sessions", "🗂", "会话"],
    ["graph", "🕸", "知识图谱"],
    ["memory", "🧠", "记忆"],
    ["chat", "❯", "对话"],
    ["terminal", "▮", "终端"],
    ["settings", "⚙", "设置"],
  ];

  // Keep the terminal mounted once opened so the shell survives tab switches.
  let terminalOpened = false;
  $: if ($activeView === "terminal") terminalOpened = true;

  let ready = false;
  onMount(() => {
    $activeView = "sessions";
    ready = true;
  });
  function refreshProviders() {}

  // Global keyboard shortcuts: Cmd/Ctrl+1..5 switch views.
  function onKey(e: KeyboardEvent) {
    if (!(e.metaKey || e.ctrlKey)) return;
    const map: Record<string, View> = {
      "1": "sessions",
      "2": "graph",
      "3": "memory",
      "4": "chat",
      "5": "terminal",
      "6": "settings",
    };
    if (map[e.key]) {
      e.preventDefault();
      $activeView = map[e.key];
    }
  }
</script>

<svelte:window on:keydown={onKey} />

<div class="shell">
  <nav class="rail">
    {#each nav as [view, icon, label]}
      <button
        class="rail-item"
        class:active={$activeView === view}
        title={label}
        on:click={() => ($activeView = view)}
      >
        <span class="icon">{icon}</span>
      </button>
    {/each}
    <div class="rail-spacer"></div>
  </nav>

  <div class="content">
    {#if ready}
      {#if $activeView === "sessions"}
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
      <!-- Terminal stays mounted once opened so the shell survives tab switches. -->
      {#if terminalOpened}
        <div class="term-layer" class:hidden={$activeView !== "terminal"}>
          <TerminalView />
        </div>
      {/if}
    {/if}
  </div>
</div>

<style>
  .shell {
    display: flex;
    height: 100vh;
  }
  .rail {
    width: 48px;
    flex: 0 0 48px;
    background: var(--bg-surface);
    border-right: 1px solid var(--border);
    display: flex;
    flex-direction: column;
    align-items: center;
    padding: 8px 0;
    gap: 4px;
  }
  .rail-item {
    width: 36px;
    height: 36px;
    border: none;
    background: transparent;
    border-radius: var(--radius-control);
    font-size: 18px;
    display: flex;
    align-items: center;
    justify-content: center;
    opacity: 0.6;
  }
  .rail-item:hover {
    background: var(--bg-elevated);
    opacity: 1;
  }
  .rail-item.active {
    background: var(--bg-elevated);
    opacity: 1;
    box-shadow: inset 2px 0 0 var(--accent);
  }
  .rail-spacer {
    flex: 1;
  }
  .status-dot {
    width: 8px;
    height: 8px;
    border-radius: 50%;
    background: var(--text-muted);
    margin-bottom: 8px;
  }
  .status-dot.ok {
    background: var(--success);
  }
  .content {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    position: relative;
  }
  .term-layer {
    position: absolute;
    inset: 0;
    background: var(--bg-base);
  }
  .term-layer.hidden {
    display: none;
  }
</style>
