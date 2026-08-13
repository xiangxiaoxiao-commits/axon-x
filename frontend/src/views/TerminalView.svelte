<script lang="ts">
  // Multi-tab embedded terminal: each tab is an independent PTY-backed shell
  // (xterm.js <-> Go backend, keyed by tab id). Resuming a Claude session opens
  // a new tab, so several sessions run side by side inside the app.
  import { onMount, onDestroy, tick } from "svelte";
  import { Terminal } from "@xterm/xterm";
  import { FitAddon } from "@xterm/addon-fit";
  import "@xterm/xterm/css/xterm.css";
  import { TermStart, TermStartResume, TermWrite, TermResize, TermStop } from "../../wailsjs/go/main/App.js";
  import { EventsOn, EventsOff } from "../../wailsjs/runtime/runtime.js";
  import { activeView, resumeRequest } from "../lib/stores";
  import { get } from "svelte/store";

  type Tab = { id: string; title: string; term: Terminal; fit: FitAddon; host?: HTMLElement; started: boolean };
  let tabs: Tab[] = [];
  let activeId = "";
  let seq = 0;

  const newId = () => `t${Date.now()}-${seq++}`;

  function b64ToStr(b64: string): string {
    const bin = atob(b64);
    const bytes = new Uint8Array(bin.length);
    for (let i = 0; i < bin.length; i++) bytes[i] = bin.charCodeAt(i);
    return new TextDecoder().decode(bytes);
  }

  function fitTab(t: Tab) {
    if (!t?.fit || !t.host) return;
    try { t.fit.fit(); TermResize(t.id, t.term.rows, t.term.cols); } catch {}
  }

  // Create a tab (optionally running a resume command) and make it active. The
  // xterm instance is opened into its host after the DOM node exists.
  async function openTab(title: string, cmd?: string) {
    const id = newId();
    const term = new Terminal({
      fontFamily: 'ui-monospace, "SF Mono", "JetBrains Mono", Menlo, monospace',
      fontSize: 13,
      theme: { background: "#0d1117", foreground: "#e6edf3", cursor: "#3b82f6", selectionBackground: "#30363d" },
      cursorBlink: true,
    });
    const fit = new FitAddon();
    term.loadAddon(fit);
    term.onData((d) => TermWrite(id, d));
    const t: Tab = { id, title, term, fit, started: false };
    tabs = [...tabs, t];
    activeId = id;
    await tick(); // wait for the host div to render
    if (t.host) { term.open(t.host); fitTab(t); term.focus(); }
    if (cmd) await TermStartResume(id, cmd); else await TermStart(id);
    t.started = true;
    fitTab(t);
  }

  async function closeTab(id: string, e?: Event) {
    e?.stopPropagation();
    TermStop(id);
    const t = tabs.find((x) => x.id === id);
    t?.term.dispose();
    tabs = tabs.filter((x) => x.id !== id);
    if (activeId === id) { activeId = tabs.length ? tabs[tabs.length - 1].id : ""; await refit(); }
  }

  async function selectTab(id: string) { activeId = id; await refit(); }

  async function refit() {
    await tick();
    const t = tabs.find((x) => x.id === activeId);
    if (t) { fitTab(t); t.term.focus(); }
  }

  // Consume a resume request coming from SessionsView.
  $: if ($resumeRequest) {
    const req = $resumeRequest; resumeRequest.set(null);
    openTab(req.title, req.cmd);
  }
  // Refit the active tab when this view becomes visible (hidden != unmounted).
  $: if ($activeView === "terminal") refit();

  onMount(() => {
    EventsOn("term:data", (ev: { id: string; data: string }) => {
      const t = tabs.find((x) => x.id === ev.id);
      t?.term.write(b64ToStr(ev.data));
    });
    EventsOn("term:exit", (ev: { id: string }) => {
      const t = tabs.find((x) => x.id === ev.id);
      t?.term.write("\r\n\x1b[31m[shell exited]\x1b[0m\r\n");
    });
    window.addEventListener("resize", refit);
    if (tabs.length === 0) openTab("shell"); // start with one plain shell
  });

  onDestroy(() => {
    window.removeEventListener("resize", refit);
    EventsOff("term:data");
    EventsOff("term:exit");
    for (const t of tabs) { TermStop(t.id); t.term.dispose(); }
  });
</script>

<div class="term-wrap">
  <div class="tabbar">
    {#each tabs as t (t.id)}
      <button class="tab" class:active={t.id === activeId} on:click={() => selectTab(t.id)} title={t.title}>
        <span class="tab-title">{t.title}</span>
        <span class="tab-close" role="button" tabindex="0"
          on:click={(e) => closeTab(t.id, e)}
          on:keydown={(e) => (e.key === "Enter" || e.key === " ") && closeTab(t.id, e)}>×</span>
      </button>
    {/each}
    <button class="tab add" title="新建终端" on:click={() => openTab("shell")}>＋</button>
  </div>
  <div class="panes">
    {#each tabs as t (t.id)}
      <div class="pane" class:hidden={t.id !== activeId} bind:this={t.host}></div>
    {/each}
    {#if tabs.length === 0}<div class="empty">点 ＋ 新建一个终端</div>{/if}
  </div>
</div>

<style>
  .term-wrap { height: 100%; display: flex; flex-direction: column; background: #0d1117; }
  .tabbar { display: flex; align-items: stretch; gap: 2px; padding: 4px 6px 0; background: var(--bg-surface); border-bottom: 1px solid var(--border); overflow-x: auto; }
  .tab {
    display: flex; align-items: center; gap: 6px; max-width: 200px;
    background: transparent; border: 1px solid transparent; border-bottom: none;
    border-radius: 6px 6px 0 0; color: var(--text-muted);
    font-family: var(--font-mono); font-size: 12px; padding: 5px 8px; white-space: nowrap;
  }
  .tab:hover { color: var(--text-primary); background: var(--bg-elevated); }
  .tab.active { color: var(--text-primary); background: #0d1117; border-color: var(--border); }
  .tab-title { overflow: hidden; text-overflow: ellipsis; }
  .tab-close { flex: 0 0 auto; opacity: .5; padding: 0 2px; border-radius: 3px; }
  .tab-close:hover { opacity: 1; background: var(--border); }
  .tab.add { color: var(--accent); font-size: 14px; padding: 5px 10px; }
  .panes { flex: 1; position: relative; padding: 8px; min-height: 0; }
  .pane { position: absolute; inset: 8px; }
  .pane.hidden { display: none; }
  .empty { color: var(--text-muted); font-size: 12px; padding: 16px; }
</style>
