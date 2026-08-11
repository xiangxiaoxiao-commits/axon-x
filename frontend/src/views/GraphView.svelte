<script lang="ts">
  // Synaptic knowledge explorer. Instead of dumping the whole graph (a
  // hairball), it shows ONE focus neuron + its direct neighbors. Clicking a
  // neighbor fires a signal down the axon and re-centers on it — you traverse
  // the network like propagating activation. The focus node's facts float
  // beside it so knowledge stays readable.
  import { onMount, onDestroy } from "svelte";
  import { ListClaudeProjects, GetGraph, BuildGraph, BuildGraphFocused, IndexProject, GenerateArticle } from "../../wailsjs/go/main/App.js";
  import { EventsOn, EventsOff } from "../../wailsjs/runtime/runtime.js";
  import type { graph, claudedata } from "../../wailsjs/go/models";
  import { marked } from "marked";

  let projects: claudedata.Project[] = [];
  let curProject = "";
  let g: graph.Graph | null = null;
  let indexing = false;
  let progress = "";

  // Focus/neighborhood state.
  let focus = "";                 // focus entity name
  let firing = "";                // neighbor currently firing (animating)
  type Ent = { name: string; type: string; obs: string[] };
  let byName: Record<string, Ent> = {};
  let neighbors: { name: string; label: string; x: number; y: number }[] = [];
  let W = 900, H = 640;

  // Article mode.
  let mode: "graph" | "article" = "graph";
  let article = ""; let articleLoading = false;
  $: articleHtml = article ? (marked.parse(article, { async: false }) as string) : "";

  onMount(async () => {
    try { projects = await ListClaudeProjects(); } catch (e) { console.error(e); }
    EventsOn("graph:progress", (p: any) => {
      if (p?.projectSlug !== curProject) return;
      progress = p.error ? `跳过一个会话: ${p.error}` : `建立索引中… ${p.current}/${p.total}`;
    });
    EventsOn("graph:done", async (p: any) => {
      if (p?.projectSlug !== curProject) return;
      indexing = false; progress = `完成：${p.entities} 个节点`;
      await load();
    });
    if (projects.length) selectProject(projects[0].slug);
  });
  onDestroy(() => { EventsOff("graph:progress"); EventsOff("graph:done"); });

  async function selectProject(slug: string) {
    curProject = slug; focus = ""; progress = "";
    await load();
  }

  async function load() {
    try { g = await BuildGraph(curProject); } catch (e) { console.error(e); g = null; }
    byName = {};
    if (g?.entities) for (const e of g.entities) byName[e.name.toLowerCase()] = { name: e.name, type: e.type, obs: e.observations || [] };
    // Start at the most-connected node (the hub).
    if (g?.entities?.length) {
      const deg: Record<string, number> = {};
      for (const r of g.relations || []) { deg[r.from] = (deg[r.from]||0)+1; deg[r.to] = (deg[r.to]||0)+1; }
      let best = g.entities[0].name, bd = -1;
      for (const e of g.entities) { const d = deg[e.name]||0; if (d > bd) { bd = d; best = e.name; } }
      setFocus(best);
    } else { focus = ""; neighbors = []; }
  }

  // Compute neighbors of `name` from relations, lay them on a circle.
  function setFocus(name: string) {
    focus = name;
    const set = new Map<string, string>(); // neighbor -> relation label
    for (const r of g?.relations || []) {
      if (r.from === name && r.to !== name) set.set(r.to, r.label);
      else if (r.to === name && r.from !== name) set.set(r.from, r.label);
    }
    const arr = [...set.entries()].slice(0, 14);
    const R = Math.min(W, H) * 0.34;
    neighbors = arr.map(([n, label], i) => {
      const a = (i / Math.max(arr.length, 1)) * Math.PI * 2 - Math.PI / 2;
      return { name: n, label, x: W/2 + Math.cos(a)*R, y: H/2 + Math.sin(a)*R };
    });
  }

  // Click a neighbor: fire the synapse, then re-center on it.
  function activate(name: string) {
    firing = name;
    setTimeout(() => { firing = ""; setFocus(name); }, 420);
  }

  $: focusEnt = byName[focus.toLowerCase()];

  async function indexProject() {
    if (indexing) return;
    indexing = true; progress = "建立索引中（首次较慢）…";
    try { await IndexProject(curProject); } catch (e: any) { indexing = false; progress = "索引失败: " + (e?.message || e); }
  }

  // Keyword jump: focus the best-matching node.
  let term = "";
  function jump() {
    const t = term.trim().toLowerCase();
    if (!t || !g?.entities) return;
    const hit = g.entities.find((e) => e.name.toLowerCase().includes(t))
             || g.entities.find((e) => (e.observations||[]).some((o) => o.toLowerCase().includes(t)));
    if (hit) setFocus(hit.name); else progress = `没找到含「${term}」的节点`;
  }

  async function genArticle() {
    if (articleLoading) return;
    mode = "article"; articleLoading = true; article = "";
    try { article = await GenerateArticle(curProject, ""); }
    catch (e: any) { article = "生成失败: " + (e?.message || e); }
    finally { articleLoading = false; }
  }
</script>

<div class="wrap neural-bg">
  <div class="bar">
    <select bind:value={curProject} on:change={() => selectProject(curProject)}>
      {#each projects as p}<option value={p.slug}>{p.path}</option>{/each}
    </select>
    <input class="term" placeholder="跳到节点，如「支付模块」" bind:value={term}
      on:keydown={(e) => e.key === "Enter" && jump()} />
    <button class="b" on:click={jump}>跳转</button>
    <button class="b ghost" on:click={indexProject} disabled={indexing}>{indexing ? "索引中…" : "建索引"}</button>
    <button class="b" on:click={genArticle} disabled={articleLoading || !g?.entities?.length}>{articleLoading ? "生成中…" : "📖 阅读文章"}</button>
    {#if mode === "article"}<button class="b ghost" on:click={() => (mode = "graph")}>← 回图谱</button>{/if}
    {#if progress}<span class="prog">{progress}</span>{/if}
    <span class="spacer"></span>
    {#if g}<span class="stat">{g.entities?.length || 0} 神经元</span>{/if}
  </div>

  <div class="stage">
    {#if mode === "article"}
      <div class="article selectable">
        {#if articleLoading}<div class="empty">正在把知识写成文章…</div>{:else}{@html articleHtml}{/if}
      </div>
    {:else if !g || !g.entities?.length}
      <div class="empty">还没有知识。点上方「建索引」，我会读这个项目的所有会话并提炼知识。</div>
    {:else}
      <svg viewBox="0 0 {W} {H}" class="canvas" preserveAspectRatio="xMidYMid meet">
        <!-- axons from focus to each neighbor -->
        {#each neighbors as n}
          <line x1={W/2} y1={H/2} x2={n.x} y2={n.y}
            class="axon" class:fire={firing === n.name} />
          {#if n.label}<text x={(W/2+n.x)/2} y={(H/2+n.y)/2} class="axon-label">{n.label}</text>{/if}
        {/each}
        <!-- neighbor neurons -->
        {#each neighbors as n}
          <g class="neuron" class:fire={firing === n.name} on:click={() => activate(n.name)} role="button" tabindex="0">
            <circle cx={n.x} cy={n.y} r="7" />
            <text x={n.x} y={n.y - 14} text-anchor="middle">{n.name}</text>
          </g>
        {/each}
        <!-- focus neuron (center) -->
        <g class="neuron focus">
          <circle cx={W/2} cy={H/2} r="12" />
          <text x={W/2} y={H/2 - 20} text-anchor="middle">{focus}</text>
        </g>
      </svg>

      {#if focusEnt}
        <div class="facts">
          <div class="f-head">🧠 {focusEnt.name} <span class="f-type">{focusEnt.type}</span></div>
          <ul>
            {#each focusEnt.obs as o}<li class="selectable">{o}</li>{/each}
            {#if focusEnt.obs.length === 0}<li class="muted">（这个神经元还没有记录的事实）</li>{/if}
          </ul>
          <div class="f-hint">点周围的神经元，沿轴突继续探索 →</div>
        </div>
      {/if}
    {/if}
  </div>
</div>

<style>
  .wrap { display: flex; flex-direction: column; height: 100%; position: relative; font-family: var(--font-mono); }
  .bar {
    display: flex; align-items: center; gap: 8px; padding: 8px 12px;
    border-bottom: 1px solid var(--border); font-size: 12px; position: relative; z-index: 2;
  }
  .bar select, .term {
    background: var(--bg-elevated); color: var(--text-primary);
    border: 1px solid var(--border); border-radius: var(--radius-control);
    font-family: var(--font-mono); font-size: 12px; padding: 3px 8px;
  }
  .bar select { max-width: 280px; } .term { width: 200px; }
  .b { background: var(--accent); color: #fff; border: none; border-radius: var(--radius-control); padding: 4px 12px; font-size: 12px; font-family: var(--font-mono); }
  .b.ghost { background: transparent; border: 1px solid var(--border); color: var(--text-muted); }
  .prog, .stat { color: var(--text-muted); }
  .spacer { flex: 1; }
  .stage { flex: 1; position: relative; min-height: 0; overflow: hidden; z-index: 1; }
  .canvas { width: 100%; height: 100%; }
  .empty { padding: 40px; color: var(--text-muted); max-width: 560px; line-height: 1.6; }

  .axon { stroke: var(--border); stroke-width: 1.5; }
  .axon.fire { stroke: var(--accent); stroke-width: 3; filter: drop-shadow(0 0 4px var(--accent)); }
  .axon-label { fill: var(--text-muted); font-size: 9px; text-anchor: middle; }

  .neuron { cursor: pointer; }
  .neuron circle { fill: var(--accent); transition: r 0.2s; }
  .neuron text { fill: var(--text-primary); font-size: 11px; }
  .neuron:hover circle { r: 9; }
  .neuron.focus circle { fill: #fff; filter: drop-shadow(0 0 10px var(--accent)); }
  .neuron.focus text { fill: var(--text-primary); font-size: 13px; font-weight: 600; }
  .neuron.fire circle { fill: #fff; filter: drop-shadow(0 0 12px var(--accent)); animation: neuron-pop 0.42s ease-out; }
  @keyframes neuron-pop { 0% { r: 7; } 50% { r: 13; } 100% { r: 7; } }

  .facts {
    position: absolute; top: 16px; right: 16px; width: 320px; max-height: 82%;
    overflow-y: auto; background: var(--bg-surface); border: 1px solid var(--border);
    border-radius: var(--radius-card); padding: 14px; z-index: 2;
  }
  .f-head { font-weight: 600; margin-bottom: 8px; }
  .f-type { font-size: 10px; color: var(--text-muted); background: var(--bg-elevated); padding: 1px 6px; border-radius: 3px; }
  .facts ul { margin: 0; padding-left: 18px; }
  .facts li { font-size: 12.5px; line-height: 1.55; margin-bottom: 5px; }
  .facts li.muted { color: var(--text-muted); list-style: none; }
  .f-hint { margin-top: 10px; font-size: 11px; color: var(--text-muted); }

  .article { flex: 1; overflow-y: auto; padding: 24px 32px; max-width: 820px; margin: 0 auto; line-height: 1.7; font-family: var(--font-ui); font-size: 14px; position: relative; z-index: 1; }
  .article :global(h2) { border-bottom: 1px solid var(--border); padding-bottom: 4px; margin: 20px 0 8px; }
  .article :global(p) { margin: 0 0 12px; }
  .article :global(ul) { padding-left: 22px; }
</style>

