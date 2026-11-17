<script lang="ts">
  // Global Cmd+K / Ctrl+K command palette:
  // - Empty query: quick actions & navigation shortcuts
  // - Typing: instant search across knowledge entities (all selected/available namespaces) + projects/namespaces
  import { onMount, tick } from "svelte";
  import {
    activeView,
    commandPaletteOpen,
    currentProject,
    namespaces,
    selectedNamespaces,
    effectiveNamespaces,
    projects,
    projectOptions,
    jumpToEntity,
    jumpToSession,
    requestIndex,
    type View,
  } from "../lib/stores";
  import { BuildMultiGraph, SearchSessions } from "../../wailsjs/go/main/App.js";

  type PaletteItem = {
    id: string;
    category: "action" | "entity" | "namespace" | "session";
    title: string;
    subtitle: string;
    badge?: string;
    run: () => void;
  };

  let query = "";
  let inputEl: HTMLInputElement;
  let activeIdx = 0;
  let cachedNodes: { name: string; type: string; aliases?: string[]; observations?: string[]; namespace?: string; sources?: string[] }[] = [];
  let sessionHits: any[] = [];
  let searchTimer: any = null;

  const ACTIONS: PaletteItem[] = [
    {
      id: "nav-graph",
      category: "action",
      title: "切换到「知识库」",
      subtitle: "浏览、查阅、管理项目知识百科卡片",
      badge: "⌘1",
      run: () => { $activeView = "graph"; close(); },
    },
    {
      id: "nav-sessions",
      category: "action",
      title: "切换到「历史会话」",
      subtitle: "查看与检索 Claude Code / Codex / WorkBuddy 历史对话",
      badge: "⌘2",
      run: () => { $activeView = "sessions"; close(); },
    },
    {
      id: "nav-terminal",
      category: "action",
      title: "切换到「内置终端」",
      subtitle: "在内置终端中继续会话或执行命令",
      badge: "⌘3",
      run: () => { $activeView = "terminal"; close(); },
    },
    {
      id: "nav-settings",
      category: "action",
      title: "打开「设置与 MCP 接入」",
      subtitle: "配置 API Key、知识提炼模型与一键接入 Claude Code",
      badge: "⌘4",
      run: () => { $activeView = "settings"; close(); },
    },
    {
      id: "act-index",
      category: "action",
      title: "从历史对话增量提炼知识",
      subtitle: "扫描当前项目新增对话并提炼知识百科卡片",
      badge: "提炼",
      run: () => { $activeView = "graph"; requestIndex(); close(); },
    },
  ];

  $: if ($commandPaletteOpen) {
    openPalette();
  }

  async function openPalette() {
    query = "";
    activeIdx = 0;
    sessionHits = [];
    await tick();
    inputEl?.focus();
    await loadAllGraphNodes();
  }

  async function loadAllGraphNodes() {
    try {
      const targetNs = $effectiveNamespaces.length > 0
        ? $effectiveNamespaces
        : $namespaces.map((n) => n.name);
      if (targetNs.length === 0) {
        cachedNodes = [];
        return;
      }
      const g: any = await BuildMultiGraph(targetNs);
      cachedNodes = (g?.nodes ?? []).map((n: any) => ({
        name: n.name,
        type: n.type || "concept",
        aliases: n.aliases || [],
        observations: n.observations || [],
        sources: Object.values(n.obsSources || {}).filter(Boolean) as string[],
      }));
    } catch {
      cachedNodes = [];
    }
  }

  function close() {
    $commandPaletteOpen = false;
  }

  // Trigger lightweight session search when query >= 2 chars
  $: {
    clearTimeout(searchTimer);
    const q = query.trim();
    if ($commandPaletteOpen && q.length >= 2) {
      searchTimer = setTimeout(async () => {
        try {
          const hits = await SearchSessions(q, "");
          sessionHits = (hits || []).slice(0, 6);
        } catch {
          sessionHits = [];
        }
      }, 150);
    } else {
      sessionHits = [];
    }
  }

  $: items = buildItems(query, cachedNodes, $namespaces, $projects, sessionHits);
  $: if (activeIdx >= items.length) activeIdx = Math.max(0, items.length - 1);

  function buildItems(
    rawQ: string,
    nodes: typeof cachedNodes,
    nsList: typeof $namespaces,
    projList: typeof $projects,
    sHits: any[],
  ): PaletteItem[] {
    const q = rawQ.trim().toLowerCase();
    if (!q) {
      const nsItems: PaletteItem[] = nsList.slice(0, 8).map((ns) => ({
        id: `ns-${ns.name}`,
        category: "namespace",
        title: `切换项目/知识库：${ns.name}`,
        subtitle: `${ns.entities} 个知识实体`,
        badge: "知识域",
        run: () => {
          selectedNamespaces.set([ns.name]);
          currentProject.set(ns.name);
          close();
        },
      }));
      return [...ACTIONS, ...nsItems];
    }

    const out: PaletteItem[] = [];

    // 1. Matching entities (name, alias, type, observation)
    for (const n of nodes) {
      const nameHit = n.name.toLowerCase().includes(q);
      const aliasHit = (n.aliases || []).find((a) => a.toLowerCase().includes(q));
      const obsHit = (n.observations || []).find((o) => o.toLowerCase().includes(q));
      if (nameHit || aliasHit || obsHit) {
        out.push({
          id: `ent-${n.name}`,
          category: "entity",
          title: n.name,
          subtitle: obsHit
            ? obsHit
            : aliasHit
            ? `别名: ${aliasHit}`
            : (n.observations?.[0] || "点击在知识图谱中定位该实体"),
          badge: n.type,
          run: () => {
            jumpToEntity(n.name);
            close();
          },
        });
        if (out.length >= 12) break;
      }
    }

    // 2. Matching namespaces / projects
    const opts = projectOptions(projList);
    for (const ns of nsList) {
      if (ns.name.toLowerCase().includes(q)) {
        out.push({
          id: `ns-${ns.name}`,
          category: "namespace",
          title: `切换知识域：${ns.name}`,
          subtitle: `${ns.entities} 个实体`,
          badge: "知识域",
          run: () => {
            selectedNamespaces.set([ns.name]);
            currentProject.set(ns.name);
            close();
          },
        });
      }
    }
    for (const o of opts) {
      if (o.label.toLowerCase().includes(q) && !out.some((x) => x.id === `ns-${o.key}`)) {
        out.push({
          id: `proj-${o.key}`,
          category: "namespace",
          title: `切换项目：${o.key}`,
          subtitle: `${o.label} · ${o.sessions} 个会话`,
          badge: "项目",
          run: () => {
            currentProject.set(o.key);
            if (nsList.some((n) => n.name === o.key)) selectedNamespaces.set([o.key]);
            close();
          },
        });
      }
    }

    // 3. Matching sessions from SQLite index
    for (const h of sHits) {
      out.push({
        id: `sess-${h.sessionId}-${out.length}`,
        category: "session",
        title: h.title || h.sessionId,
        subtitle: h.snippet || "",
        badge: "会话",
        run: () => {
          jumpToSession(h.sessionId, h.projectSlug, false);
          close();
        },
      });
    }

    // 4. Matching actions
    for (const a of ACTIONS) {
      if (a.title.toLowerCase().includes(q) || a.subtitle.toLowerCase().includes(q)) {
        out.push(a);
      }
    }

    return out.slice(0, 20);
  }

  function onKeydown(e: KeyboardEvent) {
    if (e.key === "Escape") {
      e.preventDefault();
      close();
    } else if (e.key === "ArrowDown") {
      e.preventDefault();
      activeIdx = items.length ? (activeIdx + 1) % items.length : 0;
    } else if (e.key === "ArrowUp") {
      e.preventDefault();
      activeIdx = items.length ? (activeIdx - 1 + items.length) % items.length : 0;
    } else if (e.key === "Enter" && items[activeIdx]) {
      e.preventDefault();
      items[activeIdx].run();
    }
  }
</script>

{#if $commandPaletteOpen}
  <!-- svelte-ignore a11y-click-events-have-key-events -->
  <!-- svelte-ignore a11y-no-static-element-interactions -->
  <div class="cp-backdrop" on:click={close}>
    <div class="cp-modal" on:click|stopPropagation on:keydown={onKeydown}>
      <div class="cp-head">
        <span class="cp-icon">⌘</span>
        <input
          bind:this={inputEl}
          bind:value={query}
          class="cp-input"
          placeholder="搜索知识实体、事实、项目会话，或执行快捷命令…"
        />
        <span class="cp-esc" on:click={close}>ESC</span>
      </div>
      <div class="cp-list">
        {#if items.length === 0}
          <div class="cp-empty">未找到匹配的实体、会话或命令</div>
        {:else}
          {#each items as item, idx}
            <button
              class="cp-item"
              class:active={idx === activeIdx}
              on:mouseenter={() => (activeIdx = idx)}
              on:click={item.run}
            >
              <div class="cp-item-main">
                <span class="cp-title">{item.title}</span>
                {#if item.subtitle}
                  <span class="cp-sub">{item.subtitle}</span>
                {/if}
              </div>
              {#if item.badge}
                <span class="cp-badge cat-{item.category}">{item.badge}</span>
              {/if}
            </button>
          {/each}
        {/if}
      </div>
      <div class="cp-foot">
        <span>↑↓ 选择</span>
        <span>↵ 跳转/执行</span>
        <span>⌘1~6 切换视图</span>
        <span>⌘B 折叠侧栏</span>
      </div>
    </div>
  </div>
{/if}

<style>
  .cp-backdrop {
    position: fixed; inset: 0; z-index: 999;
    background: rgba(5, 6, 8, 0.68);
    backdrop-filter: blur(6px);
    display: flex; align-items: flex-start; justify-content: center;
    padding-top: 11vh;
  }
  .cp-modal {
    width: 640px; max-width: 92vw;
    background: var(--bg-glass-elevated);
    backdrop-filter: blur(20px);
    border: 1px solid var(--border-strong);
    border-radius: var(--radius-lg);
    box-shadow: var(--shadow-lg);
    overflow: hidden;
    display: flex; flex-direction: column;
    font-family: var(--font-ui);
  }
  .cp-head {
    display: flex; align-items: center; gap: 11px;
    padding: 14px 18px;
    border-bottom: 1px solid var(--border);
    background: rgba(12, 13, 16, 0.45);
  }
  .cp-icon {
    font-size: 15px; color: var(--accent); font-weight: 700;
  }
  .cp-input {
    flex: 1; background: transparent; border: none; outline: none;
    color: var(--text-primary); font-size: 14px;
    box-shadow: none !important;
  }
  .cp-esc {
    font-family: var(--font-mono); font-size: 10px; padding: 2px 7px; border-radius: 5px;
    background: var(--bg-base); border: 1px solid var(--border);
    color: var(--text-muted); cursor: pointer;
  }
  .cp-esc:hover { color: var(--text-primary); border-color: var(--border-strong); }
  .cp-list {
    max-height: 390px; overflow-y: auto;
    padding: 7px;
  }
  .cp-empty {
    padding: 32px; text-align: center; color: var(--text-muted); font-size: 13px;
  }
  .cp-item {
    width: 100%; display: flex; align-items: center; justify-content: space-between; gap: 12px;
    padding: 9px 13px; border: 1px solid transparent; border-radius: var(--radius-control);
    background: transparent; color: var(--text-primary);
    text-align: left; cursor: pointer;
  }
  .cp-item.active {
    background: linear-gradient(90deg, rgba(245, 158, 11, 0.14) 0%, rgba(245, 158, 11, 0.03) 100%);
    border-color: rgba(245, 158, 11, 0.25);
    box-shadow: inset 2.5px 0 0 var(--accent);
  }
  .cp-item-main {
    display: flex; flex-direction: column; gap: 3px; min-width: 0; flex: 1;
  }
  .cp-title {
    font-size: 13px; font-weight: 600; color: var(--text-primary);
    white-space: nowrap; overflow: hidden; text-overflow: ellipsis;
  }
  .cp-sub {
    font-size: 11.5px; color: var(--text-muted);
    white-space: nowrap; overflow: hidden; text-overflow: ellipsis;
  }
  .cp-badge {
    font-family: var(--font-mono); font-size: 10px; padding: 2px 8px; border-radius: 99px;
    background: var(--bg-base); border: 1px solid var(--border);
    color: var(--text-muted); flex-shrink: 0;
  }
  .cp-badge.cat-entity {
    color: var(--accent-hover); background: var(--accent-soft); border-color: var(--accent-border);
  }
  .cp-badge.cat-namespace {
    color: var(--cyan); background: var(--cyan-soft); border-color: var(--cyan-border);
  }
  .cp-badge.cat-session {
    color: var(--success); background: var(--success-soft); border-color: rgba(52, 211, 153, 0.35);
  }
  .cp-foot {
    display: flex; align-items: center; gap: 18px;
    padding: 8px 18px; border-top: 1px solid var(--border);
    background: rgba(12, 13, 16, 0.65); font-family: var(--font-mono); font-size: 10.5px; color: var(--text-muted);
  }
</style>
