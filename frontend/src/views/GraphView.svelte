<script lang="ts">
  // Knowledge graph explorer, Obsidian-style. Pick a focus neuron and see its
  // 1- or 2-hop neighborhood laid out by a lightweight force simulation
  // (repulsion + edge springs + gravity). Node size encodes degree (hubs grow),
  // node color encodes entity type (with a legend). Click a node to re-focus,
  // drag to nudge. The focus node's facts float beside the canvas.
  import { onMount, onDestroy } from "svelte";
  import { BuildGraph, GetGraph, IndexProject, GenerateArticle, BuildGraphFromCode, BuildGraphFromObsidian, DeleteEntity, UpdateEntityObservations } from "../../wailsjs/go/main/App.js";
  import { EventsOn, EventsOff } from "../../wailsjs/runtime/runtime.js";
  import type { graph } from "../../wailsjs/go/models";
  import { currentProject } from "../lib/stores";
  import { marked } from "marked";

  let g: graph.Graph | null = null;
  let indexing = false;
  let progress = "";

  // "从代码建图" — scan a repo, extract code skeleton into the graph.
  let codeBuilding = false;
  let showRepoInput = false;
  let repoDir = "";

  // "吸收 Obsidian" — scan an Obsidian vault, chunk + embed notes and parse
  // [[wikilink]]s into the graph.
  let obsBuilding = false;
  let showVaultInput = false;
  let vaultDir = "";

  type Ent = { name: string; type: string; obs: string[]; sources: string[] };
  let byName: Record<string, Ent> = {};

  // Degree (connection count) per entity — drives node radius.
  let deg: Record<string, number> = {};
  let maxDeg = 0;
  // Adjacency for BFS neighborhood expansion.
  let adj = new Map<string, Set<string>>();

  // Focus + neighborhood.
  let focus = "";
  let depth: 1 | 2 = 1;
  let firing = "";
  let hover = "";

  type Node = {
    name: string; type: string; hop: number;
    x: number; y: number; vx: number; vy: number;
    r: number; fixed: boolean; i: number;
  };
  type Edge = { a: number; b: number; label: string };
  let nodes: Node[] = [];
  let edges: Edge[] = [];
  const W = 900, H = 640;
  const MAX_NODES = 140;

  // Warm, coordinated categorical palette (amber base, see NeuralHub).
  const TYPE_COLORS: Record<string, string> = {
    module: "#FBBF24", service: "#FB923C", concept: "#A3E635",
    decision: "#F472B6", constraint: "#F87171", person: "#38BDF8",
    tool: "#C084FC", file: "#2DD4BF", event: "#FCD34D",
  };
  const PALETTE = ["#FBBF24", "#FB923C", "#F472B6", "#A3E635", "#38BDF8", "#C084FC", "#2DD4BF", "#F87171", "#FCD34D", "#818CF8"];
  let colorOf: Record<string, string> = {};
  function nodeColor(t: string): string { return colorOf[t || "other"] || "#FBBF24"; }
  // Radius from degree: hubs are visibly larger. sqrt keeps growth gentle.
  function radiusFor(name: string): number {
    const d = deg[name] || 0;
    const t = maxDeg > 0 ? Math.sqrt(d) / Math.sqrt(maxDeg) : 0;
    return 5 + t * 12;
  }

  // Article mode.
  let mode: "graph" | "article" = "graph";
  let article = ""; let articleLoading = false;
  $: articleHtml = article ? (marked.parse(article, { async: false }) as string) : "";

  const reduceMotion = typeof matchMedia !== "undefined" && matchMedia("(prefers-reduced-motion: reduce)").matches;

  onMount(() => {
    EventsOn("graph:progress", (p: any) => {
      if (p?.projectSlug !== $currentProject) return;
      if (p?.phase === "code") {
        progress = p.error ? `跳过: ${p.error}` : `正在从代码建图… 文件 ${p.current ?? ""}`;
      } else if (p?.phase === "obsidian") {
        progress = p.error ? `跳过: ${p.error}` : `正在吸收 Obsidian 笔记… ${p.current ?? ""}`;
      } else {
        progress = p.error ? `跳过一个会话: ${p.error}` : `建立索引中… ${p.current}/${p.total}`;
      }
    });
    EventsOn("graph:done", async (p: any) => {
      if (p?.projectSlug !== $currentProject) return;
      if (p?.phase === "code") {
        codeBuilding = false; showRepoInput = false;
        progress = `代码建图完成：${p.entities} 个节点`;
      } else if (p?.phase === "obsidian") {
        obsBuilding = false; showVaultInput = false;
        progress = `吸收完成：${p.entities} 个节点`;
      } else {
        indexing = false; progress = `完成：${p.entities} 个节点`;
      }
      await load();
    });
  });
  onDestroy(() => {
    EventsOff("graph:progress"); EventsOff("graph:done");
    if (raf) cancelAnimationFrame(raf);
    window.removeEventListener("mousemove", onDrag);
    window.removeEventListener("mouseup", endDrag);
  });

  // Reload whenever the global project changes.
  let loadedFor = " ";
  $: if ($currentProject !== loadedFor) { loadedFor = $currentProject; focus = ""; progress = ""; load(); }

  // load(false) assembles from the session cache (BuildGraph); load(true) reads
  // the saved graph.json (GetGraph) so manual edits aren't overwritten by a
  // re-assembly. Used to refresh after delete/edit.
  async function load(fromStore = false) {
    const prevFocus = focus;
    try { g = fromStore ? await GetGraph($currentProject) : await BuildGraph($currentProject); }
    catch (e) { console.error(e); g = null; }
    byName = {}; deg = {}; adj = new Map(); maxDeg = 0; colorOf = {};
    if (!g?.entities?.length) { focus = ""; nodes = []; edges = []; return; }
    for (const e of g.entities) byName[e.name.toLowerCase()] = { name: e.name, type: e.type, obs: e.observations || [], sources: e.obsSources || [] };
    // Degree + adjacency in one pass over relations.
    const touch = (n: string) => { if (!adj.has(n)) adj.set(n, new Set()); };
    for (const r of g.relations || []) {
      touch(r.from); touch(r.to);
      if (r.from !== r.to) { adj.get(r.from)!.add(r.to); adj.get(r.to)!.add(r.from); }
      deg[r.from] = (deg[r.from] || 0) + 1; deg[r.to] = (deg[r.to] || 0) + 1;
    }
    for (const e of g.entities) { touch(e.name); maxDeg = Math.max(maxDeg, deg[e.name] || 0); }
    // Assign a color per distinct type: known types keep their hue, others cycle.
    let pi = 0;
    for (const e of g.entities) {
      const t = e.type || "other";
      if (colorOf[t]) continue;
      colorOf[t] = TYPE_COLORS[t] || PALETTE[pi++ % PALETTE.length];
    }
    // Keep the current focus across a refresh when it still exists; otherwise
    // start at the hub (most-connected node).
    if (prevFocus && byName[prevFocus.toLowerCase()]) { setFocus(prevFocus); return; }
    let best = g.entities[0].name, bd = -1;
    for (const e of g.entities) { const d = deg[e.name] || 0; if (d > bd) { bd = d; best = e.name; } }
    setFocus(best);
  }
  // Seeded RNG so a given focus lays out the same way each time (stable).
  function seeded(seed: number) {
    let s = seed >>> 0;
    return () => { s = (s * 1664525 + 1013904223) >>> 0; return s / 4294967296; };
  }
  function hash(str: string): number {
    let h = 2166136261;
    for (let i = 0; i < str.length; i++) { h ^= str.charCodeAt(i); h = Math.imul(h, 16777619); }
    return h >>> 0;
  }

  // BFS the neighborhood up to `depth` hops from `center`, capped at MAX_NODES.
  function neighborhood(center: string, d: number): { name: string; hop: number }[] {
    const seen = new Map<string, number>([[center, 0]]);
    let frontier = [center];
    for (let h = 1; h <= d; h++) {
      const next: string[] = [];
      for (const cur of frontier) {
        for (const nb of adj.get(cur) || []) {
          if (!seen.has(nb)) { seen.set(nb, h); next.push(nb); }
        }
      }
      // Prefer high-degree neighbors when a layer is huge (keeps hubs visible).
      next.sort((a, b) => (deg[b] || 0) - (deg[a] || 0));
      frontier = next;
    }
    let list = [...seen.entries()].map(([name, hop]) => ({ name, hop }));
    if (list.length > MAX_NODES) {
      // Drop the least-connected, farthest nodes first; always keep the center.
      list.sort((a, b) => a.hop - b.hop || (deg[b.name] || 0) - (deg[a.name] || 0));
      list = list.slice(0, MAX_NODES);
    }
    return list;
  }

  function setFocus(name: string) {
    focus = name;
    const rand = seeded(hash(name));
    const list = neighborhood(name, depth);
    const idx = new Map<string, number>();
    list.forEach((it, i) => idx.set(it.name, i));
    nodes = list.map((it, i) => {
      const ring = it.hop === 0 ? 0 : Math.min(W, H) * (it.hop === 1 ? 0.22 : 0.4);
      const a = rand() * Math.PI * 2;
      return {
        name: it.name, type: (byName[it.name.toLowerCase()]?.type) || "other", hop: it.hop,
        x: W / 2 + Math.cos(a) * ring + (rand() - 0.5) * 40,
        y: H / 2 + Math.sin(a) * ring + (rand() - 0.5) * 40,
        vx: 0, vy: 0, r: radiusFor(it.name), fixed: it.hop === 0, i,
      };
    });
    // Center the focus node.
    if (nodes[0]) { nodes[0].x = W / 2; nodes[0].y = H / 2; }
    // Edges among visible nodes (dedup undirected).
    const emap = new Map<string, Edge>();
    for (const r of g?.relations || []) {
      const ai = idx.get(r.from), bi = idx.get(r.to);
      if (ai === undefined || bi === undefined || ai === bi) continue;
      const key = ai < bi ? `${ai}-${bi}` : `${bi}-${ai}`;
      if (!emap.has(key)) emap.set(key, { a: ai, b: bi, label: r.label });
    }
    edges = [...emap.values()];
    startSim();
  }
  // --- Force-directed layout (lightweight Fruchterman-Reingold) ------------
  // Bounded tick budget + linear cooling => converges then STOPS (no forever
  // repaint). Reduced-motion runs it synchronously and paints once.
  let raf = 0;
  let simLeft = 0;
  const TOTAL_TICKS = 300;

  function startSim() {
    if (nodes.length === 0) return;
    if (reduceMotion) {
      for (let i = 0; i < TOTAL_TICKS; i++) tick(i / TOTAL_TICKS);
      nodes = nodes; // paint final
      return;
    }
    simLeft = TOTAL_TICKS;
    if (!raf) raf = requestAnimationFrame(frame);
  }

  function frame() {
    // A few ticks per frame keeps it snappy without hogging the main thread.
    for (let s = 0; s < 4 && (simLeft > 0 || dragging !== ""); s++) {
      const t = simLeft > 0 ? (TOTAL_TICKS - simLeft) / TOTAL_TICKS : 0.85;
      tick(t);
      if (simLeft > 0) simLeft--;
    }
    nodes = nodes; // reactive repaint
    if (simLeft > 0 || dragging !== "") raf = requestAnimationFrame(frame);
    else raf = 0;
  }

  function tick(progress01: number) {
    const n = nodes.length;
    // Ideal edge length from available area; temperature cools each tick.
    const k = Math.sqrt((W * H) / Math.max(n, 1)) * 0.55;
    const temp = (1 - progress01) * 22 + 2;
    // Repulsion (all pairs — cheap up to ~140 nodes).
    for (let i = 0; i < n; i++) {
      const a = nodes[i];
      for (let j = i + 1; j < n; j++) {
        const b = nodes[j];
        let dx = a.x - b.x, dy = a.y - b.y;
        let dist = Math.hypot(dx, dy) || 0.01;
        const rep = (k * k) / dist;
        const ux = dx / dist, uy = dy / dist;
        a.vx += ux * rep; a.vy += uy * rep;
        b.vx -= ux * rep; b.vy -= uy * rep;
      }
    }
    // Attraction along edges (springs).
    for (const e of edges) {
      const a = nodes[e.a], b = nodes[e.b];
      let dx = a.x - b.x, dy = a.y - b.y;
      let dist = Math.hypot(dx, dy) || 0.01;
      const att = (dist * dist) / k;
      const ux = dx / dist, uy = dy / dist;
      a.vx -= ux * att; a.vy -= uy * att;
      b.vx += ux * att; b.vy += uy * att;
    }
    // Gravity toward center + integrate with temperature cap.
    for (const p of nodes) {
      p.vx += (W / 2 - p.x) * 0.012;
      p.vy += (H / 2 - p.y) * 0.012;
      if (p.fixed || p.name === dragging) { p.vx = 0; p.vy = 0; continue; }
      const sp = Math.hypot(p.vx, p.vy) || 0.01;
      const cap = Math.min(sp, temp);
      p.x += (p.vx / sp) * cap;
      p.y += (p.vy / sp) * cap;
      p.vx *= 0.5; p.vy *= 0.5; // damping
      p.x = Math.max(24, Math.min(W - 24, p.x));
      p.y = Math.max(24, Math.min(H - 24, p.y));
    }
  }
  // --- Drag (screen -> SVG coords) -----------------------------------------
  let dragging = "";
  let dragMoved = false;
  let svgEl: SVGSVGElement;

  function svgPoint(clientX: number, clientY: number): { x: number; y: number } {
    if (!svgEl) return { x: 0, y: 0 };
    const pt = svgEl.createSVGPoint();
    pt.x = clientX; pt.y = clientY;
    const m = svgEl.getScreenCTM();
    if (!m) return { x: 0, y: 0 };
    const p = pt.matrixTransform(m.inverse());
    return { x: p.x, y: p.y };
  }

  function startDrag(name: string, ev: MouseEvent) {
    dragging = name; dragMoved = false;
    window.addEventListener("mousemove", onDrag);
    window.addEventListener("mouseup", endDrag);
    ev.preventDefault();
  }
  function onDrag(ev: MouseEvent) {
    if (!dragging) return;
    dragMoved = true;
    const p = svgPoint(ev.clientX, ev.clientY);
    const node = nodes.find((x) => x.name === dragging);
    if (node) { node.x = p.x; node.y = p.y; node.vx = 0; node.vy = 0; nodes = nodes; }
    if (!reduceMotion && !raf) raf = requestAnimationFrame(frame);
  }
  function endDrag() {
    const wasDragging = dragging, moved = dragMoved;
    dragging = ""; dragMoved = false;
    window.removeEventListener("mousemove", onDrag);
    window.removeEventListener("mouseup", endDrag);
    if (!moved && wasDragging && wasDragging !== focus) activate(wasDragging);
    else if (!reduceMotion) { simLeft = Math.max(simLeft, 40); if (!raf) raf = requestAnimationFrame(frame); }
  }

  // Click a node: fire, then re-center on it.
  function activate(name: string) {
    if (reduceMotion) { setFocus(name); return; }
    firing = name;
    setTimeout(() => { firing = ""; setFocus(name); }, 380);
  }
  function onKey(e: KeyboardEvent, name: string) {
    if (e.key === "Enter" || e.key === " ") { e.preventDefault(); if (name !== focus) activate(name); }
  }

  function setDepth(d: 1 | 2) { if (d === depth) return; depth = d; if (focus) setFocus(focus); }

  // Edge endpoints resolved to live node positions (reactive on nodes).
  $: edgeViews = edges.map((e) => {
    const a = nodes[e.a], b = nodes[e.b];
    return a && b ? { a: a.name, b: b.name, label: e.label, x1: a.x, y1: a.y, x2: b.x, y2: b.y } : null;
  }).filter((x): x is NonNullable<typeof x> => x !== null);

  // Legend: only the types actually visible right now.
  $: legend = [...new Set(nodes.map((n) => n.type))].map((t) => ({ type: t, color: nodeColor(t) }));

  // Show a label when it matters (focus / hovered / near / hub) — avoids a wall of text.
  function showLabel(n: Node): boolean {
    return n.name === focus || n.name === hover || n.hop <= 1 || n.r >= 12;
  }
  function edgeHot(a: string, b: string): boolean {
    const key = focus || hover;
    return !!key && (a === key || b === key);
  }

  $: focusEnt = byName[focus.toLowerCase()];

  async function indexProject() {
    if (indexing) return;
    indexing = true; progress = "建立索引中（首次较慢）…";
    try { await IndexProject($currentProject); } catch (e: any) { indexing = false; progress = "索引失败: " + (e?.message || e); }
  }

  function toggleRepoInput() {
    if (!$currentProject) { progress = "先在顶部选一个项目"; return; }
    showRepoInput = !showRepoInput;
  }
  async function buildFromCode() {
    if (codeBuilding) return;
    if (!$currentProject) { progress = "先在顶部选一个项目"; return; }
    const dir = repoDir.trim();
    if (!dir) { progress = "请填写仓库绝对路径"; return; }
    codeBuilding = true; progress = "正在从代码建图（首次较慢）…";
    try { await BuildGraphFromCode(dir, $currentProject); }
    catch (e: any) { codeBuilding = false; progress = "代码建图失败: " + (e?.message || e); }
  }

  function toggleVaultInput() {
    if (!$currentProject) { progress = "先在顶部选一个项目"; return; }
    showVaultInput = !showVaultInput;
  }
  async function buildFromObsidian() {
    if (obsBuilding) return;
    if (!$currentProject) { progress = "先在顶部选一个项目"; return; }
    const dir = vaultDir.trim();
    if (!dir) { progress = "请填写 Obsidian vault 绝对路径"; return; }
    obsBuilding = true; progress = "正在吸收 Obsidian 笔记（首次较慢）…";
    try { await BuildGraphFromObsidian(dir, $currentProject); }
    catch (e: any) { obsBuilding = false; progress = "吸收 Obsidian 失败: " + (e?.message || e); }
  }

  let term = "";
  function jump() {
    const t = term.trim().toLowerCase();
    if (!t || !g?.entities) return;
    const hit = g.entities.find((e) => e.name.toLowerCase().includes(t))
             || g.entities.find((e) => (e.observations || []).some((o) => o.toLowerCase().includes(t)));
    if (hit) setFocus(hit.name); else progress = `没找到含「${term}」的节点`;
  }

  async function genArticle() {
    if (articleLoading) return;
    mode = "article"; articleLoading = true; article = "";
    try { article = await GenerateArticle($currentProject, ""); }
    catch (e: any) { article = "生成失败: " + (e?.message || e); }
    finally { articleLoading = false; }
  }

  // --- Manual editing: correct / denoise the graph ------------------------
  // The facts panel toggles between read-only and an editable draft of the
  // focused entity's observations. Saving writes back via the backend, then we
  // refresh from graph.json (GetGraph) so edits aren't clobbered by re-assembly.
  let editing = false;
  let draft: string[] = [];
  let saving = false;
  let editErr = "";

  // Turn an observation's raw source into a short provenance label. Session ids
  // and code paths come from the backend; "manual" marks hand edits; empty means
  // the source is unknown (older caches).
  function sourceLabel(src: string): string {
    if (!src) return "";
    if (src === "manual") return "手动";
    if (src.startsWith("code:")) return "代码";
    if (src.startsWith("task:")) return "任务";
    return "会话";
  }

  function startEdit() {
    if (!focusEnt) return;
    draft = [...focusEnt.obs];
    editErr = ""; editing = true;
  }
  function cancelEdit() { editing = false; draft = []; editErr = ""; }
  function removeDraft(i: number) { draft = draft.filter((_, j) => j !== i); }
  function addDraft() { draft = [...draft, ""]; }

  async function saveObs() {
    if (saving || !focusEnt) return;
    saving = true; editErr = "";
    try {
      await UpdateEntityObservations($currentProject, focusEnt.name, draft.map((o) => o.trim()).filter(Boolean));
      editing = false; draft = [];
      await load(true); // refresh from the saved graph, keeping other edits
    } catch (e: any) { editErr = "保存失败: " + (e?.message || e); }
    finally { saving = false; }
  }

  async function removeEntity() {
    if (!focusEnt) return;
    if (!confirm(`删除实体「${focusEnt.name}」及其所有关系？此操作不可撤销。`)) return;
    saving = true; editErr = "";
    try {
      await DeleteEntity($currentProject, focusEnt.name);
      editing = false; draft = []; focus = "";
      await load(true); // deleted node is gone; focus falls back to the hub
    } catch (e: any) { editErr = "删除失败: " + (e?.message || e); saving = false; }
  }
</script>

<div class="wrap neural-bg">
  <div class="bar">
    <input class="term" placeholder="跳到节点，如「支付模块」" bind:value={term}
      on:keydown={(e) => e.key === "Enter" && jump()} />
    <button class="b" on:click={jump}>跳转</button>
    <button class="b ghost" on:click={indexProject} disabled={indexing}>{indexing ? "索引中…" : "建索引"}</button>
    <button class="b ghost" on:click={toggleRepoInput} disabled={codeBuilding}>{codeBuilding ? "建图中…" : "🧬 从代码建图"}</button>
    {#if showRepoInput}
      <input class="term repo" placeholder="仓库绝对路径，如 /Users/me/project" bind:value={repoDir}
        on:keydown={(e) => e.key === "Enter" && buildFromCode()} />
      <button class="b" on:click={buildFromCode} disabled={codeBuilding}>{codeBuilding ? "…" : "开始扫描"}</button>
    {/if}
    <button class="b ghost" on:click={toggleVaultInput} disabled={obsBuilding}>{obsBuilding ? "吸收中…" : "📓 吸收 Obsidian"}</button>
    {#if showVaultInput}
      <input class="term repo" placeholder="Obsidian vault 绝对路径，如 /Users/xiangxiao/Documents/Obsidian Vault" bind:value={vaultDir}
        on:keydown={(e) => e.key === "Enter" && buildFromObsidian()} />
      <button class="b" on:click={buildFromObsidian} disabled={obsBuilding}>{obsBuilding ? "…" : "开始"}</button>
    {/if}
    <button class="b" on:click={genArticle} disabled={articleLoading || !g?.entities?.length}>{articleLoading ? "生成中…" : "📖 阅读文章"}</button>
    {#if mode === "graph"}
      <span class="depth">
        跳数
        <button class="pill" class:on={depth === 1} on:click={() => setDepth(1)}>1</button>
        <button class="pill" class:on={depth === 2} on:click={() => setDepth(2)}>2</button>
      </span>
    {/if}
    {#if mode === "article"}<button class="b ghost" on:click={() => (mode = "graph")}>← 回图谱</button>{/if}
    {#if progress}<span class="prog">{progress}</span>{/if}
    <span class="spacer"></span>
    {#if g}<span class="stat">{g.entities?.length || 0} 神经元 · 显示 {nodes.length}</span>{/if}
  </div>

  <div class="stage">
    {#if mode === "article"}
      <div class="article selectable">
        {#if articleLoading}<div class="empty">正在把知识写成文章…</div>{:else}{@html articleHtml}{/if}
      </div>
    {:else if !g || !g.entities?.length}
      <div class="empty">还没有知识。点上方「建索引」，我会读这个项目的所有会话并提炼知识。</div>
    {:else}
      <svg bind:this={svgEl} viewBox="0 0 {W} {H}" class="canvas" preserveAspectRatio="xMidYMid meet">
        <!-- edges -->
        {#each edgeViews as e (e.a + '|' + e.b)}
          <line x1={e.x1} y1={e.y1} x2={e.x2} y2={e.y2}
            class="axon" class:fire={firing === e.a || firing === e.b} class:hot={edgeHot(e.a, e.b)} />
        {/each}
        {#each edgeViews as e (e.a + '|' + e.b)}
          {#if e.label && edgeHot(e.a, e.b)}
            <text x={(e.x1 + e.x2) / 2} y={(e.y1 + e.y2) / 2} class="axon-label">{e.label}</text>
          {/if}
        {/each}
        <!-- nodes -->
        {#each nodes as n (n.name)}
          <g class="neuron" class:focus={n.name === focus} class:fire={firing === n.name}
            role="button" tabindex="0" aria-label={n.name}
            on:mousedown={(ev) => startDrag(n.name, ev)}
            on:mouseenter={() => (hover = n.name)} on:mouseleave={() => (hover = "")}
            on:keydown={(e) => onKey(e, n.name)}>
            <circle cx={n.x} cy={n.y} r={n.r} fill={nodeColor(n.type)}
              class:hub={n.name === focus} />
            {#if showLabel(n)}
              <text x={n.x} y={n.y - n.r - 5} text-anchor="middle" class="n-label">{n.name}</text>
            {/if}
          </g>
        {/each}
      </svg>

      {#if legend.length}
        <div class="legend">
          {#each legend as l}
            <span class="l-item"><i style="background:{l.color}"></i>{l.type}</span>
          {/each}
        </div>
      {/if}

      {#if focusEnt}
        <div class="facts">
          <div class="f-head">🧠 {focusEnt.name}
            <span class="f-type" style="border-color:{nodeColor(focusEnt.type)}; color:{nodeColor(focusEnt.type)}">{focusEnt.type}</span>
          </div>
          <div class="f-meta">度数 {deg[focusEnt.name] || 0} · {depth} 跳邻域</div>

          {#if editing}
            <ul class="edit">
              {#each draft as o, i}
                <li>
                  <textarea class="obs-edit" rows="2" bind:value={draft[i]}></textarea>
                  <button class="x" title="删除这条" on:click={() => removeDraft(i)}>×</button>
                </li>
              {/each}
              {#if draft.length === 0}<li class="muted">（还没有事实，点下面「+ 添加一条」）</li>{/if}
            </ul>
            <div class="edit-actions">
              <button class="b sm ghost" on:click={addDraft}>+ 添加一条</button>
              <span class="spacer"></span>
              <button class="b sm ghost" on:click={cancelEdit} disabled={saving}>取消</button>
              <button class="b sm" on:click={saveObs} disabled={saving}>{saving ? "保存中…" : "保存"}</button>
            </div>
          {:else}
            <ul>
              {#each focusEnt.obs as o, i}
                <li class="selectable">
                  {o}
                  {#if sourceLabel(focusEnt.sources[i] || "")}
                    <span class="src" class:manual={focusEnt.sources[i] === "manual"}>{sourceLabel(focusEnt.sources[i] || "")}</span>
                  {/if}
                </li>
              {/each}
              {#if focusEnt.obs.length === 0}<li class="muted">（这个神经元还没有记录的事实）</li>{/if}
            </ul>
            <div class="edit-actions">
              <button class="b sm ghost" on:click={startEdit}>✏️ 编辑事实</button>
              <span class="spacer"></span>
              <button class="b sm danger" on:click={removeEntity} disabled={saving}>🗑 删除此实体</button>
            </div>
          {/if}

          {#if editErr}<div class="edit-err">{editErr}</div>{/if}
          <div class="f-hint">点节点重新聚焦，拖动可微调布局 →</div>
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
  .term {
    background: var(--bg-elevated); color: var(--text-primary);
    border: 1px solid var(--border); border-radius: var(--radius-control);
    font-family: var(--font-mono); font-size: 12px; padding: 3px 8px; width: 200px;
  }
  .term.repo { width: 280px; }
  .b { background: #FBBF24; color: #1c1206; border: none; border-radius: var(--radius-control); padding: 4px 12px; font-size: 12px; font-family: var(--font-mono); font-weight: 600; }
  .b.ghost { background: transparent; border: 1px solid var(--border); color: var(--text-muted); font-weight: 400; }
  .depth { display: inline-flex; align-items: center; gap: 4px; color: var(--text-muted); }
  .pill { background: transparent; border: 1px solid var(--border); color: var(--text-muted); border-radius: 999px; width: 22px; height: 22px; font-size: 11px; font-family: var(--font-mono); }
  .pill.on { background: #FBBF24; border-color: #FBBF24; color: #1c1206; font-weight: 700; }
  .prog, .stat { color: var(--text-muted); }
  .spacer { flex: 1; }
  .stage { flex: 1; position: relative; min-height: 0; overflow: hidden; z-index: 1; }
  .canvas { width: 100%; height: 100%; }
  .empty { padding: 40px; color: var(--text-muted); max-width: 560px; line-height: 1.6; }

  .axon { stroke: var(--border); stroke-width: 1.2; }
  .axon.hot { stroke: #FDE68A; stroke-width: 1.6; }
  .axon.fire { stroke: #FBBF24; stroke-width: 3; filter: drop-shadow(0 0 4px #FBBF24); }
  .axon-label { fill: var(--text-muted); font-size: 9px; text-anchor: middle; }

  .neuron { cursor: pointer; }
  .neuron circle { transition: filter 0.2s; stroke: rgba(0,0,0,0.35); stroke-width: 1; }
  .neuron:hover circle { filter: drop-shadow(0 0 6px currentColor); }
  .n-label { fill: var(--text-primary); font-size: 11px; pointer-events: none; }
  .neuron.focus circle.hub { stroke: #FFF9EC; stroke-width: 2.5; filter: drop-shadow(0 0 10px #FBBF24); }
  .neuron.focus .n-label { font-size: 13px; font-weight: 600; }
  .neuron.fire circle { filter: drop-shadow(0 0 12px #FBBF24); animation: neuron-pop 0.38s ease-out; }
  @keyframes neuron-pop { 0% { transform: scale(1); } 50% { transform: scale(1.35); } 100% { transform: scale(1); } }
  .neuron.fire { transform-box: fill-box; transform-origin: center; }

  .legend {
    position: absolute; left: 14px; bottom: 14px; z-index: 2;
    display: flex; flex-wrap: wrap; gap: 6px 12px; max-width: 380px;
    background: var(--bg-surface); border: 1px solid var(--border);
    border-radius: var(--radius-card); padding: 8px 10px; font-size: 11px; color: var(--text-muted);
  }
  .l-item { display: inline-flex; align-items: center; gap: 5px; }
  .l-item i { width: 9px; height: 9px; border-radius: 50%; display: inline-block; }

  .facts {
    position: absolute; top: 16px; right: 16px; width: 320px; max-height: 82%;
    overflow-y: auto; background: var(--bg-surface); border: 1px solid var(--border);
    border-radius: var(--radius-card); padding: 14px; z-index: 2;
  }
  .f-head { font-weight: 600; margin-bottom: 4px; }
  .f-type { font-size: 10px; background: var(--bg-elevated); padding: 1px 6px; border-radius: 3px; border: 1px solid; }
  .f-meta { font-size: 11px; color: var(--text-muted); margin-bottom: 8px; }
  .facts ul { margin: 0; padding-left: 18px; }
  .facts li { font-size: 12.5px; line-height: 1.55; margin-bottom: 5px; }
  .facts li.muted { color: var(--text-muted); list-style: none; }
  .f-hint { margin-top: 10px; font-size: 11px; color: var(--text-muted); }

  /* Provenance tag on each fact. */
  .src { font-size: 9px; color: var(--text-muted); border: 1px solid var(--border); border-radius: 3px; padding: 0 4px; margin-left: 6px; white-space: nowrap; }
  .src.manual { color: #FBBF24; border-color: #FBBF24; }

  /* Inline editing of facts. */
  .facts ul.edit { list-style: none; padding-left: 0; }
  .facts ul.edit li { display: flex; gap: 6px; align-items: flex-start; margin-bottom: 6px; }
  .obs-edit {
    flex: 1; background: var(--bg-elevated); color: var(--text-primary);
    border: 1px solid var(--border); border-radius: var(--radius-control);
    font-family: var(--font-mono); font-size: 12px; padding: 4px 6px; resize: vertical;
  }
  .x { background: transparent; border: 1px solid var(--border); color: var(--text-muted); border-radius: var(--radius-control); width: 22px; height: 22px; font-size: 14px; line-height: 1; cursor: pointer; flex: none; }
  .x:hover { color: #F87171; border-color: #F87171; }
  .edit-actions { display: flex; align-items: center; gap: 6px; margin-top: 8px; }
  .b.sm { padding: 3px 10px; font-size: 11px; }
  .b.danger { background: transparent; border: 1px solid #F87171; color: #F87171; font-weight: 400; }
  .b.danger:hover:not(:disabled) { background: #F87171; color: #1c1206; }
  .edit-err { margin-top: 8px; font-size: 11px; color: #F87171; }

  /* Fill the stage and scroll on its own. .stage isn't a flex container, so
     flex:1 wouldn't apply — absolute inset lets the article take the full
     height and scroll, while max-width + auto margins keep the text centered. */
  .article { position: absolute; inset: 0; overflow-y: auto; padding: 24px 32px; line-height: 1.7; font-family: var(--font-ui); font-size: 14px; z-index: 1; }
  .article > :global(*) { max-width: 820px; margin-left: auto; margin-right: auto; }
  .article :global(h2) { border-bottom: 1px solid var(--border); padding-bottom: 4px; margin: 20px 0 8px; }
  .article :global(p) { margin: 0 0 12px; }
  .article :global(ul) { padding-left: 22px; }

  @media (prefers-reduced-motion: reduce) {
    .neuron.fire circle { animation: none; }
    .neuron circle { transition: none; }
  }
</style>
