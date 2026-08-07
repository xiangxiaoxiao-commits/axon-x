<script lang="ts">
  import { onMount } from "svelte";
  import { activeView, hasProvider, conversations, type View } from "./lib/stores";
  import { ListProviders, ListConversations } from "../wailsjs/go/main/App.js";
  import ChatView from "./views/ChatView.svelte";
  import ArchiveView from "./views/ArchiveView.svelte";
  import MemoryView from "./views/MemoryView.svelte";
  import SettingsView from "./views/SettingsView.svelte";

  // Rail items: [view, icon, label]. Icons are simple glyphs for now.
  const nav: [View, string, string][] = [
    ["chat", "💬", "聊天"],
    ["archive", "📥", "归档"],
    ["memory", "🧠", "记忆"],
    ["settings", "⚙", "设置"],
  ];

  let ready = false;

  onMount(async () => {
    await refreshProviders();
    try {
      $conversations = await ListConversations();
    } catch (e) {
      console.error("load conversations", e);
    }
    // First-run gate: with no usable provider, force the settings view (UX §5.1).
    if (!$hasProvider) $activeView = "settings";
    ready = true;
  });

  async function refreshProviders() {
    try {
      const provs = await ListProviders();
      $hasProvider = provs.some((p) => p.hasKey);
    } catch (e) {
      console.error("load providers", e);
      $hasProvider = false;
    }
  }

  // Global keyboard shortcuts: ⌘1..4 switch views.
  function onKey(e: KeyboardEvent) {
    if (!(e.metaKey || e.ctrlKey)) return;
    const map: Record<string, View> = { "1": "chat", "2": "archive", "3": "memory", "4": "settings" };
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
    <div class="status-dot" class:ok={$hasProvider} title={$hasProvider ? "已连接" : "未配置"}></div>
  </nav>

  <div class="content">
    {#if ready}
      {#if $activeView === "chat"}
        <ChatView />
      {:else if $activeView === "archive"}
        <ArchiveView />
      {:else if $activeView === "memory"}
        <MemoryView />
      {:else if $activeView === "settings"}
        <SettingsView onSaved={refreshProviders} />
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
  }
</style>
