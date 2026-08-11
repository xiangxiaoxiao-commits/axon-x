<script lang="ts">
  // Per-project knowledge graph distilled from conversations. Nodes = entities,
  // edges = relations. A tiny force layout positions nodes; click a node to see
  // its observations. "构建/更新" runs extraction over sessions via gpt-5.6-sol.
  import { onMount, onDestroy } from "svelte";
  import { ListClaudeProjects, GetGraph, BuildGraph, BuildGraphFocused, IndexProject, GenerateArticle } from "../../wailsjs/go/main/App.js";
  import { EventsOn, EventsOff } from "../../wailsjs/runtime/runtime.js";
  import { marked } from "marked";
  import type { graph, claudedata } from "../../wailsjs/go/models";

  let projects: claudedata.Project[] = [];
  let curProject = "";
  let g: graph.Graph | null = null;
  let building = false;
  let progress = "";
  let selected: any = null;
  let term = "";
  let focused = false; // true when the current graph is a term-focused result
  let mode: "graph" | "article" = "graph";
  let article = "";
  let articleLoading = false;

  // Generate a readable article from the current (optionally focused) knowledge.
  async function genArticle() {
    if (articleLoading) return;
    mode = "article"; articleLoading = true; article = "";
    try { article = await GenerateArticle(curProject, focused ? term : ""); }
    catch (e: any) { article = "生成失败: " + (e?.message || e); }
    finally { articleLoading = false; }
  }
  $: articleHtml = article ? (marked.parse(article, { async: false }) as string) : "";

  // Layout state: node positions keyed by name.
  type Node = { name: string; type: string; obs: string[]; x: number; y: number; vx: number; vy: number };
  let nodes: Node[] = [];
  let edges: { from: string; to: string; label: string }[] = [];
  let W = 900, H = 640;
  let raf = 0;

  onMount(async () => {
    try { projects = await ListClaudeProjects(); } catch (e) { console.error(e); }
    EventsOn("graph:progress", (p: any) => {
      if (p?.projectSlug !== curProject) return;
      progress = p.error ? `跳过一个会话: ${p.error}` : `提炼中… ${p.current}/${p.total}${p.title ? " · " + p.title : ""}`;
    });
    EventsOn("graph:done", async (p: any) => {
      if (p?.projectSlug !== curProject) return;
      building = false; progress = `完成：${p.entities} 个节点 · ${p.relations} 条关系`;
      await loadGraph();
    });
    if (projects.length) selectProject(projects[0].slug);
  });
  onDestroy(() => { EventsOff("graph:progress"); EventsOff("graph:done"); cancelAnimationFrame(raf); });

  async function selectProject(slug: string) {
    curProject = slug; selected = null; progress = "";
    await loadGraph();
  }

  async function loadGraph() {
    try { g = await GetGraph(curProject); } catch (e) { console.error(e); g = null; }
    layout();
  }

  // Focused build: extract only knowledge related to `term`. Uses the return
  // value directly (focused graphs aren't persisted), so it works regardless
  // of the done-event.
  async function buildFocused() {
    if (building) return;
    if (!term.trim()) { progress = "请先输入一个关注的词"; return; }
    building = true; selected = null; progress = "围绕「" + term + "」提炼中…";
    try {
      g = await BuildGraphFocused(curProject, term.trim());
      focused = true;
      layout();
      progress = `围绕「${term}」：${g?.entities?.length || 0} 节点 · ${g?.relations?.length || 0} 关系`;
    } catch (e: any) {
      progress = "失败: " + (e?.message || e);
    } finally {
      building = false;
    }
  }

  // One-time (incremental) indexing: distill each session into a cache. Slow
  // the first time; afterwards focus/full assembly is instant and free.
  let indexing = false;
  async function indexProject() {
    if (indexing) return;
    indexing = true; progress = "建立索引中（首次较慢，之后秒出）…";
    try {
      await IndexProject(curProject);
      // graph:done (phase=index) updates progress; then show the full graph.
      g = await BuildGraph(curProject); focused = false; layout();
    } catch (e: any) {
      progress = "索引失败: " + (e?.message || e);
    } finally {
      indexing = false;
    }
  }

  // Full graph from cache (instant).
  async function build() {
    if (building) return;
    building = true; focused = false; progress = "组装中…";
    try { g = await BuildGraph(curProject); layout(); progress = `${g?.entities?.length || 0} 节点 · ${g?.relations?.length || 0} 关系`; }
    catch (e: any) { progress = "失败: " + (e?.message || e); }
    finally { building = false; }
  }

  // Seed nodes/edges and run a short force simulation.
  function layout() {
    cancelAnimationFrame(raf);
    if (!g || !g.entities?.length) { nodes = []; edges = []; return; }
    nodes = g.entities.map((e, i) => ({
      name: e.name, type: e.type, obs: e.observations || [],
      x: W / 2 + Math.cos(i) * 120 + (Math.random() - 0.5) * 40,
      y: H / 2 + Math.sin(i) * 120 + (Math.random() - 0.5) * 40,
      vx: 0, vy: 0,
    }));
    edges = (g.relations || []).map((r) => ({ from: r.from, to: r.to, label: r.label }));
    let ticks = 0;
    const step = () => {
      tick(); ticks++;
      nodes = nodes; // trigger reactivity
      if (ticks < 220) raf = requestAnimationFrame(step);
    };
    raf = requestAnimationFrame(step);
  }

  // Minimal force-directed step: repulsion between nodes, spring on edges,
  // centering pull. No external dependency.
  function tick() {
    const idx: Record<string, Node> = {};
    nodes.forEach((n) => (idx[n.name.toLowerCase()] = n));
    for (let i = 0; i < nodes.length; i++) {
      for (let j = i + 1; j < nodes.length; j++) {
        const a = nodes[i], b = nodes[j];
        let dx = a.x - b.x, dy = a.y - b.y;
        let d2 = dx * dx + dy * dy || 1;
        const f = 4000 / d2;
        const d = Math.sqrt(d2);
        a.vx += (dx / d) * f; a.vy += (dy / d) * f;
        b.vx -= (dx / d) * f; b.vy -= (dy / d) * f;
      }
    }
    for (const e of edges) {
      const a = idx[e.from.toLowerCase()], b = idx[e.to.toLowerCase()];
      if (!a || !b) continue;
      const dx = b.x - a.x, dy = b.y - a.y;
      const d = Math.sqrt(dx * dx + dy * dy) || 1;
      const f = (d - 140) * 0.02;
      a.vx += (dx / d) * f; a.vy += (dy / d) * f;
      b.vx -= (dx / d) * f; b.vy -= (dy / d) * f;
    }
    for (const n of nodes) {
      n.vx += (W / 2 - n.x) * 0.002;
      n.vy += (H / 2 - n.y) * 0.002;
      n.vx *= 0.85; n.vy *= 0.85;
      n.x += n.vx; n.y += n.vy;
      n.x = Math.max(30, Math.min(W - 30, n.x));
      n.y = Math.max(30, Math.min(H - 30, n.y));
    }
  }

  function posOf(name: string): Node | undefined {
    return nodes.find((n) => n.name.toLowerCase() === name.toLowerCase());
  }
</script>

<div class="graph">
  <div class="bar">
    <select bind:value={curProject} on:change={() => selectProject(curProject)}>
      {#each projects as p}<option value={p.slug}>{p.path}</option>{/each}
    </select>
    <input
      class="term"
      placeholder="输入关注的词，如「支付模块」"
      bind:value={term}
      on:keydown={(e) => e.key === "Enter" && buildFocused()}
      disabled={building}
    />
    <button class="build" on:click={buildFocused} disabled={building || indexing}>按词聚焦</button>
    <button class="build ghost" on:click={build} disabled={building || indexing} title="从缓存组装全量图（秒出）">全量</button>
    <button class="build ghost" on:click={indexProject} disabled={building || indexing} title="过一遍会话建立索引缓存（首次慢，之后秒出）">
      {indexing ? "索引中…" : "建索引"}
    </button>
    <button class="build" on:click={genArticle} disabled={articleLoading || !g || !g.entities?.length} title="把当前知识写成一篇可阅读的文章">
      {articleLoading ? "生成中…" : "📖 阅读文章"}
    </button>
    {#if mode === "article"}
      <button class="build ghost" on:click={() => (mode = "graph")}>← 回图谱</button>
    {/if}
    {#if progress}<span class="progress">{progress}</span>{/if}
    <span class="spacer"></span>
    {#if g}<span class="stat">{g.entities?.length || 0} 节点 · {g.relations?.length || 0} 关系</span>{/if}
  </div>

  <div class="stage">
    {#if mode === "article"}
      <div class="article selectable">
        {#if articleLoading}
          <div class="empty">正在把知识写成文章…</div>
        {:else}
          {@html articleHtml}
        {/if}
      </div>
    {:else if !g || !g.entities?.length}
      <div class="empty">
        {building ? "正在从对话里提炼知识…" : "还没有知识图谱。点上方「构建知识图谱」，我会读这个项目的所有会话，只把有价值的项目知识提炼进来。"}
      </div>
    {:else}
      <div class="list">
        <div class="list-head">提炼出的知识（{g.entities.length}）</div>
        {#each g.entities as e}
          <button class="ent" class:sel={selected?.name === e.name} on:click={() => (selected = { name: e.name, type: e.type, obs: e.observations || [] })}>
            <div class="ent-name">{e.name} <span class="ent-type">{e.type}</span></div>
            {#each (e.observations || []) as o}<div class="ent-obs selectable">· {o}</div>{/each}
          </button>
        {/each}
      </div>
      <svg viewBox="0 0 {W} {H}" class="canvas">
        {#each edges as e}
          {@const a = posOf(e.from)}
          {@const b = posOf(e.to)}
          {#if a && b}
            <line x1={a.x} y1={a.y} x2={b.x} y2={b.y} class="edge" />
            <text x={(a.x + b.x) / 2} y={(a.y + b.y) / 2} class="edge-label">{e.label}</text>
          {/if}
        {/each}
        {#each nodes as n}
          <g class="node" class:sel={selected?.name === n.name} on:click={() => (selected = n)} role="button" tabindex="0">
            <circle cx={n.x} cy={n.y} r="6" />
            <text x={n.x + 9} y={n.y + 4}>{n.name}</text>
          </g>
        {/each}
      </svg>
      {#if selected}
        <div class="detail">
          <div class="d-head">
            <span class="d-name">{selected.name}</span>
            <span class="d-type">{selected.type}</span>
            <button class="d-close" on:click={() => (selected = null)}>✕</button>
          </div>
          <ul>
            {#each selected.obs as o}<li>{o}</li>{/each}
            {#if selected.obs.length === 0}<li class="muted">（暂无观察）</li>{/if}
          </ul>
        </div>
      {/if}
    {/if}
  </div>
</div>

<style>
  .graph { display: flex; flex-direction: column; height: 100%; font-family: var(--font-mono); }
  .bar {
    display: flex; align-items: center; gap: 10px;
    padding: 8px 12px; border-bottom: 1px solid var(--border); font-size: 12px;
  }
  .bar select {
    background: var(--bg-elevated); color: var(--text-primary);
    border: 1px solid var(--border); border-radius: var(--radius-control);
    font-family: var(--font-mono); font-size: 12px; padding: 3px 8px; max-width: 320px;
  }
  .build {
    background: var(--accent); color: #fff; border: none;
    border-radius: var(--radius-control); padding: 4px 12px; font-size: 12px;
  }
  .build:disabled { background: var(--bg-elevated); color: var(--text-muted); }
  .progress { color: var(--text-muted); }
  .spacer { flex: 1; }
  .stat { color: var(--text-muted); }
  .term {
    background: var(--bg-base); color: var(--text-primary);
    border: 1px solid var(--border); border-radius: var(--radius-control);
    font-family: var(--font-mono); font-size: 12px; padding: 4px 10px; width: 220px; outline: none;
  }
  .term:focus { border-color: var(--accent); }
  .build.ghost { background: transparent; border: 1px solid var(--border); color: var(--text-muted); }
  .stage { flex: 1; position: relative; min-height: 0; overflow: hidden; display: flex; }
  .empty { padding: 40px; color: var(--text-muted); max-width: 560px; line-height: 1.6; }
  .list { flex: 0 0 320px; overflow-y: auto; border-right: 1px solid var(--border); }
  .list-head { padding: 8px 12px; font-size: 11px; color: var(--text-muted); border-bottom: 1px solid var(--border); }
  .ent { display: block; width: 100%; text-align: left; background: transparent; border: none; border-bottom: 1px solid var(--border); padding: 8px 12px; color: var(--text-primary); }
  .ent:hover { background: var(--bg-elevated); }
  .ent.sel { background: var(--bg-elevated); box-shadow: inset 2px 0 0 var(--accent); }
  .ent-name { font-size: 12.5px; font-weight: 600; }
  .ent-type { font-size: 10px; color: var(--text-muted); font-weight: 400; }
  .ent-obs { font-size: 11.5px; color: var(--text-muted); line-height: 1.5; margin-top: 2px; }
  .canvas { flex: 1; height: 100%; }
  .article {
    flex: 1;
    overflow-y: auto;
    padding: 24px 32px;
    max-width: 820px;
    margin: 0 auto;
    line-height: 1.7;
    font-family: var(--font-ui);
    font-size: 14px;
  }
  .article :global(h1), .article :global(h2), .article :global(h3) {
    line-height: 1.3; margin: 20px 0 8px;
  }
  .article :global(h2) { border-bottom: 1px solid var(--border); padding-bottom: 4px; }
  .article :global(p) { margin: 0 0 12px; }
  .article :global(ul), .article :global(ol) { margin: 0 0 12px; padding-left: 22px; }
  .article :global(li) { margin-bottom: 4px; }
  .article :global(code) { font-family: var(--font-mono); background: var(--bg-elevated); padding: 1px 5px; border-radius: 4px; }
  .article :global(strong) { color: var(--text-primary); }
  .edge { stroke: var(--border); stroke-width: 1; }
  .edge-label { fill: var(--text-muted); font-size: 9px; }
  .node circle { fill: var(--accent); }
  .node text { fill: var(--text-primary); font-size: 11px; }
  .node.sel circle { fill: var(--warning); }
  .node { cursor: pointer; }
  .detail {
    position: absolute; top: 12px; right: 12px; width: 300px;
    background: var(--bg-surface); border: 1px solid var(--border);
    border-radius: var(--radius-card); padding: 12px; max-height: 80%; overflow-y: auto;
  }
  .d-head { display: flex; align-items: center; gap: 8px; margin-bottom: 8px; }
  .d-name { font-weight: 600; }
  .d-type { font-size: 10px; background: var(--bg-elevated); padding: 1px 6px; border-radius: 3px; color: var(--text-muted); }
  .d-close { margin-left: auto; background: none; border: none; color: var(--text-muted); }
  .detail ul { margin: 0; padding-left: 18px; }
  .detail li { font-size: 12px; line-height: 1.5; margin-bottom: 4px; }
  .detail li.muted { color: var(--text-muted); list-style: none; }
</style>

