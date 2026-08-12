<script lang="ts">
  // Neural hub: a procedurally grown neuron rendered under fluorescence. An organic
  // soma sprouts six dendritic feature trunks (one promoted to a thick apical shaft),
  // a fan of decorative dendrites, and a single thin straight axon. Every branch of
  // order >= 2 is furred with dendritic spines (stalk + head). The whole arbor is
  // composited into three depth layers (warm blurred background, sharp mid subject,
  // soft foreground bokeh) with pointer parallax, real bloom, film grain, vignette
  // and drifting dust. Clicking a feature charges the soma (coral flash), then a
  // coral comet travels that trunk's centre-line to the bouton before dispatch("go").
  import { type View } from "../lib/stores";
  import { createEventDispatcher, onMount } from "svelte";
  const dispatch = createEventDispatcher<{ go: View }>();

  // Commit is the product's main act: it takes the apical shaft (index 0, the
  // thickest, longest, most prominent trunk). The rest stay as secondary nodes.
  const features: [View, string, string][] = [
    ["commit", "提交", "读改动 + 懂业务，生成 commit"],
    ["search", "搜索", "关键词搜所有历史对话"],
    ["sessions", "会话", "浏览过往会话，永不丢失"],
    ["graph", "知识", "突触式探索项目知识"],
    ["chat", "对话", "带项目记忆的 AI 对话"],
    ["terminal", "终端", "内嵌真实 shell"],
    ["settings", "设置", "模型与密钥"],
  ];

  // --- canvas / geometry ---
  const W = 1000, H = 760, cx = 500, cy = 296, somaR = 48;
  const DEG = Math.PI / 180;
  const CORAL = "#FF6B4A", CORAL_HOT = "#FFD2C0";
  const f = (n: number) => n.toFixed(1);
  const clamp = (v: number, a: number, b: number) => Math.max(a, Math.min(b, v));

  // deterministic PRNG so the "organic" growth is stable across renders (built once)
  function makeRng(seed: number) {
    return () => {
      seed = (seed + 0x6d2b79f5) | 0;
      let t = Math.imul(seed ^ (seed >>> 15), 1 | seed);
      t = (t + Math.imul(t ^ (t >>> 7), 61 | t)) ^ t;
      return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
    };
  }
  const rand = makeRng(42);

  type Pt = { x: number; y: number; w: number };
  type Ribbon = { d: string; o: number };
  type Twig = { d: string; w: number; o: number };
  type Ball = { x: number; y: number; r: number; o: number };

  const ribbons: Ribbon[] = [];
  const twigs: Twig[] = [];
  const balls: Ball[] = [];

  // dendritic spines accumulate into just two DOM paths (one stroked stalk path,
  // one filled head path) so thousands of spines cost 2 elements + 1 glow pass.
  let spineStalks = "";
  let spineHeads = "";
  let spineCount = 0;
  const SPINE_CAP = 2600; // performance ceiling on furry detail

  // A1. fur a centre-line with spines: walk it at ~4-7px spacing, sprout a short
  // perpendicular stalk (alternating side, jittered +-25deg) capped by a small head.
  function addSpines(pts: Pt[], order: number) {
    if (order < 2 || pts.length < 2) return;
    const step = clamp(7 - order, 3, 7);      // denser fur on higher-order twigs
    const headR = clamp(0.6 + order * 0.12, 0.6, 1.2);
    const stalkLen = 2 + order * 0.4;
    let carry = 2 + rand() * 3, side = rand() < 0.5 ? 1 : -1;
    for (let i = 1; i < pts.length; i++) {
      const a = pts[i - 1], b = pts[i];
      let dx = b.x - a.x, dy = b.y - a.y;
      const seg = Math.hypot(dx, dy) || 1;
      dx /= seg; dy /= seg;
      let t = carry;
      while (t < seg) {
        if (spineCount >= SPINE_CAP) return;
        const px = a.x + dx * t, py = a.y + dy * t;
        side = -side;
        const jit = (rand() - 0.5) * 50 * DEG;   // +-25deg
        const na = Math.atan2(dy, dx) + side * Math.PI / 2 + jit;
        const L = stalkLen * (0.7 + rand() * 0.7);
        const hx = px + Math.cos(na) * L, hy = py + Math.sin(na) * L;
        spineStalks += `M ${f(px)} ${f(py)} L ${f(hx)} ${f(hy)} `;
        const r = headR * (0.75 + rand() * 0.5);
        // head drawn as a full circle via two arcs -> stays one fill path
        spineHeads += `M ${f(hx - r)} ${f(hy)} a ${f(r)} ${f(r)} 0 1 0 ${f(r * 2)} 0 a ${f(r)} ${f(r)} 0 1 0 ${f(-r * 2)} 0 `;
        spineCount++;
        t += step * (0.7 + rand() * 0.7);
      }
      carry = t - seg;
    }
  }
  // --- ribbon builder: turn a centre-line [{x,y,w}] into a filled tapering shape.
  function ribbonPath(pts: Pt[]): string {
    const n = pts.length;
    if (n < 2) return "";
    const nx: number[] = [], ny: number[] = [];
    for (let i = 0; i < n; i++) {
      const a = pts[Math.max(0, i - 1)], b = pts[Math.min(n - 1, i + 1)];
      let tx = b.x - a.x, ty = b.y - a.y;
      const len = Math.hypot(tx, ty) || 1;
      tx /= len; ty /= len;
      nx.push(-ty); ny.push(tx); // left normal
    }
    const L: [number, number][] = [], R: [number, number][] = [];
    for (let i = 0; i < n; i++) {
      const hw = pts[i].w / 2;
      L.push([pts[i].x + nx[i] * hw, pts[i].y + ny[i] * hw]);
      R.push([pts[i].x - nx[i] * hw, pts[i].y - ny[i] * hw]);
    }
    const edge = (e: [number, number][], start: boolean) => {
      let s = start ? `L ${f(e[0][0])} ${f(e[0][1])}` : `M ${f(e[0][0])} ${f(e[0][1])}`;
      for (let i = 1; i < e.length - 1; i++) {
        const mx = (e[i][0] + e[i + 1][0]) / 2, my = (e[i][1] + e[i + 1][1]) / 2;
        s += ` Q ${f(e[i][0])} ${f(e[i][1])} ${f(mx)} ${f(my)}`;
      }
      const last = e[e.length - 1];
      s += ` L ${f(last[0])} ${f(last[1])}`;
      return s;
    };
    return `${edge(L, false)} ${edge(R.reverse(), true)} Z`;
  }

  // smooth an OPEN centre-line into a path (ribbon centre-lines / conduction)
  function centrePath(pts: Pt[]): string {
    if (pts.length < 2) return "";
    let s = `M ${f(pts[0].x)} ${f(pts[0].y)}`;
    for (let i = 1; i < pts.length - 1; i++) {
      const mx = (pts[i].x + pts[i + 1].x) / 2, my = (pts[i].y + pts[i + 1].y) / 2;
      s += ` Q ${f(pts[i].x)} ${f(pts[i].y)} ${f(mx)} ${f(my)}`;
    }
    const last = pts[pts.length - 1];
    return s + ` L ${f(last.x)} ${f(last.y)}`;
  }

  // A2. build a meandering centre-line of `len` from (x,y) heading `ang`, with a
  // gentle length-based taper (~5% per segment) rather than a steep per-step cut.
  function meander(x: number, y: number, ang: number, len: number, w0: number, jit: number) {
    const segs = 3 + Math.floor(rand() * 2);
    const pts: Pt[] = [{ x, y, w: w0 }];
    let a = ang, w = w0;
    for (let s = 1; s <= segs; s++) {
      a += (rand() - 0.5) * 2 * jit * DEG; // random walk -> meander
      const sl = len / segs;
      x += Math.cos(a) * sl; y += Math.sin(a) * sl;
      w *= 0.95; // gentle intra-branch taper (A2)
      pts.push({ x, y, w });
    }
    return { pts, ang: a, w, x, y };
  }
  // a terminal bouton: fine twiglets each strung with glowing balls, outermost largest
  function makeBouton(x: number, y: number, ang: number, order: number, full: boolean) {
    const o = clamp(1 - order * 0.1, 0.32, 1);
    if (!full) { balls.push({ x, y, r: 1.4 + rand() * 1.6, o }); return; }
    const nt = 2 + Math.floor(rand() * 3);
    for (let t = 0; t < nt; t++) {
      const ta = ang + (t - (nt - 1) / 2) * (24 + rand() * 20) * DEG + (rand() - 0.5) * 0.3;
      const tl = 5 + rand() * 9;
      const ex = x + Math.cos(ta) * tl, ey = y + Math.sin(ta) * tl;
      twigs.push({ d: `M ${f(x)} ${f(y)} L ${f(ex)} ${f(ey)}`, w: 0.7, o: o * 0.9 });
      const nb = 2 + Math.floor(rand() * 3);
      for (let b = 1; b <= nb; b++) {
        const bt = b / nb;
        balls.push({
          x: x + Math.cos(ta) * tl * bt,
          y: y + Math.sin(ta) * tl * bt,
          r: 1 + bt * (1.4 + rand() * 2.6),
          o: clamp(o * (0.6 + bt * 0.4), 0.3, 1),
        });
      }
    }
  }

  // A2/A5. recursive fractal dendrite. Gentle taper, Rall 3/2 split at forks,
  // realistic bifurcation angles (main child 10-22deg, sibling 40-55deg), furred.
  function grow(x: number, y: number, ang: number, len: number, width: number, order: number, budget: { n: number }) {
    if (budget.n <= 0) return;
    budget.n--;
    const m = meander(x, y, ang, len, width, 12);
    const o = clamp(1 - order * 0.1, 0.32, 1);
    if (width < 1.6) {
      twigs.push({ d: centrePath(m.pts), w: Math.max(0.6, width * 0.85), o: o * 0.85 });
    } else {
      const tip = m.pts[m.pts.length - 1];
      m.pts.push({ x: tip.x, y: tip.y, w: tip.w * 0.4 }); // sharpen the tip
      ribbons.push({ d: ribbonPath(m.pts), o });
    }
    addSpines(m.pts, order); // A1
    // A2. denser tree: cap order at 9, terminate more reluctantly
    const pTerm = order >= 5 ? 0.18 + (order - 5) * 0.2 : 0.02;
    if (order > 9 || m.w < 0.55 || len < 6 || rand() < pTerm) {
      makeBouton(m.x, m.y, m.ang, order, rand() < 0.3);
      return;
    }
    const cw = m.w * 0.63; // Rall 3/2 law (kept)
    const tri = rand() < 0.08;
    const s = rand() < 0.5 ? 1 : -1;                 // which side the main child bends
    const main = s * (10 + rand() * 12);             // A5: 10-22deg
    const sib = -s * (40 + rand() * 15);             // A5: 40-55deg opposite
    const offs = tri ? [main, sib, -s * (18 + rand() * 10)] : [main, sib];
    for (let c = 0; c < offs.length; c++) {
      const na = m.ang + (offs[c] + (rand() - 0.5) * 10) * DEG;
      const nl = len * (c === 0 ? 0.9 : 0.74);
      grow(m.x, m.y, na, nl, c === 0 ? cw : cw * 0.92, order + 1, budget);
    }
  }
  type Feat = {
    view: View; label: string; hint: string;
    x: number; y: number; ang: number; lx: number; ly: number; centre: string;
  };

  function maxRadius(ang: number, pad: number) {
    const dx = Math.cos(ang), dy = Math.sin(ang);
    let r = 400;
    if (dx > 0.01) r = Math.min(r, (W - pad - cx) / dx);
    if (dx < -0.01) r = Math.min(r, (pad - cx) / dx);
    if (dy > 0.01) r = Math.min(r, (H - pad - cy) / dy);
    if (dy < -0.01) r = Math.min(r, (pad - cy) / dy);
    return r;
  }

  const exitAngles: number[] = [];
  const APICAL = 0; // A3: feature 0 (points up) becomes the thick apical shaft

  // A3. grow one feature trunk toward a target, sprouting side dendrites, ending in
  // a clickable bouton. The apical trunk is thicker/straighter/longer and its tip
  // explodes into a dense bifurcating tuft; others are thinner with broken symmetry.
  function growTrunk(view: View, label: string, hint: string, i: number): Feat {
    const apical = i === APICAL;
    const base = -90 + i * (360 / features.length);
    const jitter = apical ? 6 : 15;                          // A3 asymmetry
    const ang = (base + (rand() - 0.5) * jitter) * DEG;
    exitAngles.push(ang);
    const reachMul = apical ? 1.3 : (0.78 + rand() * 0.28);  // +-25% + apical boost
    const reach = Math.min(maxRadius(ang, 120), 300) * reachMul;
    const steps = apical ? 7 : 6, sl = reach / steps;
    const w0 = apical ? 11 : (7 + rand() * 1.8);
    const walk = apical ? 8 : 16;
    const pts: Pt[] = [];
    let x = cx + Math.cos(ang) * (somaR - 6), y = cy + Math.sin(ang) * (somaR - 6);
    let a = ang;
    pts.push({ x, y, w: w0 });
    for (let s = 1; s <= steps; s++) {
      a += (rand() - 0.5) * walk * DEG;
      x += Math.cos(a) * sl; y += Math.sin(a) * sl;
      // A2. power taper -> stays thick near the soma, thins late
      const w = 2.4 + (w0 - 2.4) * Math.pow(1 - s / steps, 1.4);
      pts.push({ x, y, w });
      if (s >= 2 && s <= steps - 1 && rand() < 0.9) {
        const side = rand() < 0.5 ? 1 : -1;
        grow(x, y, a + side * (42 + rand() * 26) * DEG, 48 + rand() * 22, w * 0.62, 2, { n: 12 });
      }
    }
    const centre = centrePath(pts);
    const tip = pts[pts.length - 1];
    ribbons.push({ d: ribbonPath([...pts.slice(0, -1), { x: tip.x, y: tip.y, w: 3 }]), o: 1 });
    // A3. apical tuft: fan of dense bifurcating dendrites bursting from the crown
    if (apical) {
      const nt = 4;
      for (let t = 0; t < nt; t++) {
        const ta = a + (t - (nt - 1) / 2) * 26 * DEG + (rand() - 0.5) * 0.2;
        grow(tip.x, tip.y, ta, 60 + rand() * 26, 3.4, 3, { n: 22 });
      }
    }
    const lx = tip.x + Math.cos(ang) * 30, ly = tip.y + Math.sin(ang) * 30 + 4;
    return { view, label, hint, x: tip.x, y: tip.y, ang: a, lx, ly, centre };
  }
  const nodes: Feat[] = features.map(([v, l, h], i) => growTrunk(v, l, h, i));

  // A4. the axon: one very thin (~1.8, no taper) very long straight fibre leaving the
  // soma opposite the apical shaft, with a few near-90deg collaterals and no spines,
  // ending in a small terminal bouton cluster. Smooth + thin = reads as an axon.
  (() => {
    const ang = (90 + (rand() - 0.5) * 16) * DEG; // opposite the up-pointing apical
    exitAngles.push(ang);
    const reach = Math.min(maxRadius(ang, 60), 360) * 1.6;
    const steps = 9, sl = reach / steps;
    const pts: Pt[] = [];
    let x = cx + Math.cos(ang) * (somaR - 6), y = cy + Math.sin(ang) * (somaR - 6);
    let a = ang;
    pts.push({ x, y, w: 1.8 });
    for (let s = 1; s <= steps; s++) {
      a += (rand() - 0.5) * 6 * DEG; // very straight
      x += Math.cos(a) * sl; y += Math.sin(a) * sl;
      pts.push({ x, y, w: 1.8 });
      if ((s === 3 || s === 6) && s < steps - 1) { // sparse ~90deg collaterals
        const side = rand() < 0.5 ? 1 : -1;
        const bx = x, by = y, ba = a + side * (82 + rand() * 12) * DEG;
        const bl = 22 + rand() * 16;
        const ex = bx + Math.cos(ba) * bl, ey = by + Math.sin(ba) * bl;
        twigs.push({ d: `M ${f(bx)} ${f(by)} L ${f(ex)} ${f(ey)}`, w: 1.4, o: 0.7 });
        makeBouton(ex, ey, ba, 5, false);
      }
    }
    twigs.push({ d: centrePath(pts), w: 1.8, o: 0.9 });
    makeBouton(x, y, a, 3, true); // axon terminal arbor
  })();

  // decorative primaries filling the gaps between features (now furry + denser)
  for (let i = 0; i < 5; i++) {
    const ang = (-54 + i * 72 + (rand() - 0.5) * 34) * DEG;
    exitAngles.push(ang);
    const x = cx + Math.cos(ang) * (somaR - 6), y = cy + Math.sin(ang) * (somaR - 6);
    grow(x, y, ang, 104 + rand() * 34, 5.5, 1, { n: 24 });
  }

  // --- organic soma: harmonic-perturbed polar radius + gaussian bumps at every exit
  const somaPath = (() => {
    const ph = [rand() * 6.28, rand() * 6.28, rand() * 6.28];
    const N = 120;
    const ring: [number, number][] = [];
    for (let k = 0; k < N; k++) {
      const th = (k / N) * Math.PI * 2;
      let r = 1 + 0.16 * Math.sin(2 * th + ph[0]) + 0.09 * Math.sin(3 * th + ph[1]) + 0.05 * Math.sin(5 * th + ph[2]);
      for (const ea of exitAngles) {
        const d = Math.abs(((th - ea + Math.PI) % (Math.PI * 2)) - Math.PI);
        r += 0.34 * Math.exp(-(d * d) / (2 * 0.18 * 0.18));
      }
      ring.push([cx + Math.cos(th) * somaR * r, cy + Math.sin(th) * somaR * r]);
    }
    let d = `M ${f(ring[0][0])} ${f(ring[0][1])}`;
    for (let k = 0; k < N; k++) {
      const p0 = ring[(k - 1 + N) % N], p1 = ring[k], p2 = ring[(k + 1) % N], p3 = ring[(k + 2) % N];
      const c1x = p1[0] + (p2[0] - p0[0]) / 6, c1y = p1[1] + (p2[1] - p0[1]) / 6;
      const c2x = p2[0] - (p3[0] - p1[0]) / 6, c2y = p2[1] - (p3[1] - p1[1]) / 6;
      d += ` C ${f(c1x)} ${f(c1y)} ${f(c2x)} ${f(c2y)} ${f(p2[0])} ${f(p2[1])}`;
    }
    return d + " Z";
  })();

  const gradR = (() => {
    let m = somaR * 1.5;
    for (const n of nodes) m = Math.max(m, Math.hypot(n.x - cx, n.y - cy));
    for (const b of balls) m = Math.max(m, Math.hypot(b.x - cx, b.y - cy));
    return m * 1.05;
  })();

  const twinkle = new Set<number>();
  for (let i = 0; i < balls.length; i++) if (rand() < 0.06) twinkle.add(i);

  // B3. floating dust motes (large soft blurred circles, slow drift)
  const dust = Array.from({ length: 11 }, () => ({
    x: rand() * W, y: rand() * H,
    r: 18 + rand() * 42,
    o: 0.02 + rand() * 0.045,
    dur: 26 + rand() * 34,
    dx: (rand() - 0.5) * 40, dy: (rand() - 0.5) * 40,
    delay: -rand() * 40,
  }));
  const elemCount = ribbons.length + twigs.length + balls.length + dust.length + 2;
  // --- interaction / firing state ---
  let hover = -1;
  let firing = -1;
  let arrived = false;
  let charging = false;
  const timers: ReturnType<typeof setTimeout>[] = [];

  // B1. pointer parallax: normalized -1..1 pointer position drives layer offsets
  let mx = 0, my = 0;
  function onMove(e: MouseEvent) {
    const t = e.currentTarget as HTMLElement;
    const r = t.getBoundingClientRect();
    mx = ((e.clientX - r.left) / r.width - 0.5) * 2;
    my = ((e.clientY - r.top) / r.height - 0.5) * 2;
  }
  function onLeave() { mx = 0; my = 0; hover = -1; }

  function activate(i: number) {
    if (firing !== -1) return;
    firing = i;
    arrived = false;
    charging = true; // soma flares first (build-up)
    timers.push(setTimeout(() => (charging = false), 260));
    timers.push(setTimeout(() => (arrived = true), 740)); // spike reaches bouton
    timers.push(setTimeout(() => {
      const v = nodes[i].view;
      firing = -1; arrived = false;
      dispatch("go", v);
    }, 860)); // setTimeout is the source of truth even if SMIL doesn't render
  }
  function onKey(e: KeyboardEvent, i: number) {
    if (e.key === "Enter" || e.key === " ") { e.preventDefault(); activate(i); }
  }
  onMount(() => () => timers.forEach(clearTimeout));
</script>

<div class="hub" on:mousemove={onMove} on:mouseleave={onLeave} role="presentation">
  <svg viewBox="0 0 {W} {H}" class="canvas" preserveAspectRatio="xMidYMid meet" aria-label="神经中枢导航">
    <defs>
      <radialGradient id="bg" cx="50%" cy="40%" r="80%">
        <stop offset="0%" stop-color="#14100c" />
        <stop offset="100%" stop-color="#0c0a09" />
      </radialGradient>
      <!-- soma-centred fluorescence in user space so colour tracks true distance -->
      <radialGradient id="fluoro" gradientUnits="userSpaceOnUse" cx={cx} cy={cy} r={gradR}>
        <stop offset="0%" stop-color="#FFF4D6" />
        <stop offset="20%" stop-color="#FDE68A" />
        <stop offset="58%" stop-color="#FBBF24" />
        <stop offset="100%" stop-color="#92580E" />
      </radialGradient>
      <radialGradient id="soma-body" cx="42%" cy="36%" r="72%">
        <stop offset="0%" stop-color="#FFF9EC" />
        <stop offset="30%" stop-color="#FDD46A" />
        <stop offset="100%" stop-color="#7A4A12" />
      </radialGradient>
      <!-- B3b. vignette: transparent centre -> deep warm near-black corners -->
      <radialGradient id="vig" cx="50%" cy="45%" r="72%">
        <stop offset="52%" stop-color="#080604" stop-opacity="0" />
        <stop offset="100%" stop-color="#080604" stop-opacity="0.45" />
      </radialGradient>

      <!-- dendrite glow: applied once per group (cheap) -->
      <filter id="glow" x="-50%" y="-50%" width="200%" height="200%">
        <feGaussianBlur in="SourceGraphic" stdDeviation="2.5" result="b1" />
        <feGaussianBlur in="SourceGraphic" stdDeviation="6" result="b2" />
        <feMerge>
          <feMergeNode in="b2" /><feMergeNode in="b1" /><feMergeNode in="SourceGraphic" />
        </feMerge>
      </filter>
      <!-- B1. background depth: deep warm-brown blurred silhouette from alpha -->
      <filter id="depth" x="-40%" y="-40%" width="180%" height="180%">
        <feGaussianBlur in="SourceAlpha" stdDeviation="4.5" result="b" />
        <feFlood flood-color="#3a2410" result="c" />
        <feComposite in="c" in2="b" operator="in" />
      </filter>
      <!-- B1. foreground bokeh: soft defocus, colour kept -->
      <filter id="fore" x="-40%" y="-40%" width="180%" height="180%">
        <feGaussianBlur in="SourceGraphic" stdDeviation="6" />
      </filter>
      <!-- B2. real bloom: lift luminance then 3-stage blur merge -->
      <filter id="bloom" x="-90%" y="-90%" width="280%" height="280%">
        <feComponentTransfer result="hi">
          <feFuncR type="linear" slope="1.4" /><feFuncG type="linear" slope="1.4" /><feFuncB type="linear" slope="1.4" />
        </feComponentTransfer>
        <feGaussianBlur in="hi" stdDeviation="3" result="b1" />
        <feGaussianBlur in="hi" stdDeviation="9" result="b2" />
        <feGaussianBlur in="hi" stdDeviation="22" result="b3" />
        <feMerge>
          <feMergeNode in="b3" /><feMergeNode in="b2" /><feMergeNode in="b1" /><feMergeNode in="SourceGraphic" />
        </feMerge>
      </filter>
      <!-- B3a. desaturated film grain to kill gradient banding -->
      <filter id="grain">
        <feTurbulence type="fractalNoise" baseFrequency="0.9" numOctaves="2" stitchTiles="stitch" result="n" />
        <feColorMatrix in="n" type="saturate" values="0" />
      </filter>

      <!-- coarse arbor (ribbons only): used by the blurred depth layers, where fine
           twigs/spines would be erased by blur anyway -> cheap parallax re-raster -->
      <g id="arbor-coarse">
        {#each ribbons as r}
          <path d={r.d} fill="url(#fluoro)" opacity={r.o} />
        {/each}
      </g>
      <!-- full arbor (coarse + twigs + spines): only the sharp mid subject needs it -->
      <g id="arbor">
        <use href="#arbor-coarse" />
        {#each twigs as t}
          <path d={t.d} fill="none" stroke="url(#fluoro)" stroke-width={t.w} stroke-linecap="round" opacity={t.o} />
        {/each}
        <path d={spineStalks} fill="none" stroke="url(#fluoro)" stroke-width="0.7" stroke-linecap="round" opacity="0.8" />
        <path d={spineHeads} fill="url(#fluoro)" opacity="0.9" />
      </g>

      <!-- invisible centre-lines the conduction spike follows -->
      {#each nodes as n, i}
        <path id={"trunk-" + i} d={n.centre} fill="none" />
      {/each}
    </defs>

    <rect x="0" y="0" width={W} height={H} fill="url(#bg)" />

    <!-- B3c. drifting dust motes (soft, deep behind the neuron) -->
    <g class="dust" filter="url(#fore)">
      {#each dust as m, i}
        <circle cx={m.x} cy={m.y} r={m.r} fill="#FBBF24" opacity={m.o}
          class="mote" style="--dx:{f(m.dx)}px; --dy:{f(m.dy)}px; --dur:{m.dur}s; animation-delay:{m.delay}s" />
      {/each}
    </g>

    <!-- whole neuron breathes very slowly -->
    <g class="breathe" style="transform-origin:{cx}px {cy}px">
      <!-- B1. background depth layer: cold, blurred, offset by parallax -->
      <g class="layer back" style="transform:translate({mx * 8}px,{my * 8}px) scale(1.35); transform-origin:{cx}px {cy}px">
        <use href="#arbor-coarse" filter="url(#depth)" />
      </g>

      <!-- B1. mid subject: sharp, glowing, additive over the dark ground -->
      <g class="glowgroup">
        <g class="arbor" filter="url(#glow)">
          <use href="#arbor" />
        </g>
        <g class="boutons" filter="url(#glow)">
          {#each balls as b, i}
            <circle cx={b.x} cy={b.y} r={b.r} fill="url(#fluoro)" opacity={b.o}
              class={twinkle.has(i) ? "twinkle" : ""}
              style={twinkle.has(i) ? `animation-delay:${(i % 7) * 0.9}s` : ""} />
          {/each}
        </g>
        <!-- B2. soma with bloom + overexposed pure-white core -->
        <g class="soma" filter="url(#bloom)" class:charge={charging}>
          <path d={somaPath} fill="url(#soma-body)" />
          <path d={somaPath} fill="none" stroke="#FFE9B0" stroke-width="1.2" opacity="0.5" />
          <circle cx={cx - somaR * 0.26} cy={cy - somaR * 0.3} r={somaR * 0.24} fill="#FFF9EC" opacity="0.8" />
          <circle cx={cx} cy={cy} r="14" fill="#FFFFFF" class="core" />
          {#if charging}
            <circle cx={cx} cy={cy} r="20" fill={CORAL} class="charge-hot" />
          {/if}
          {#if firing !== -1}
            <circle cx={cx} cy={cy} class="ripple" fill="none" stroke="#FF6B4A" stroke-width="2.5" />
          {/if}
        </g>
      </g>

      <!-- B1. foreground bokeh: strong opposite parallax, defocused, screen-blended -->
      <g class="layer fore" style="transform:translate({mx * -22}px,{my * -22}px) scale(1.12); transform-origin:{cx}px {cy}px">
        <use href="#arbor-coarse" filter="url(#fore)" />
      </g>
    </g>

    <!-- feature terminals: enlarged glowing boutons + labels (labels crisp) -->
    {#each nodes as n, i}
      <g class="node" class:fire={arrived && firing === i}
        style="transform-origin:{n.x}px {n.y}px"
        role="button" tabindex="0" aria-label={n.label}
        on:click={() => activate(i)}
        on:keydown={(e) => onKey(e, i)}
        on:mouseenter={() => (hover = i)}
        on:mouseleave={() => (hover = -1)}>
        <circle cx={n.x} cy={n.y} r="18" fill="url(#fluoro)" class="node-halo" filter="url(#bloom)" />
        <circle cx={n.x} cy={n.y} r="8.5" fill="url(#soma-body)" />
        <circle cx={n.x} cy={n.y} r="3.4" fill={arrived && firing === i ? CORAL_HOT : "#FFF4D6"}
          class:hot={arrived && firing === i} opacity="0.95" />
      </g>
    {/each}

    <!-- B3. vignette then grain sit above the neuron but below crisp text -->
    <rect x="0" y="0" width={W} height={H} fill="url(#vig)" class="vignette" />
    <rect x="0" y="0" width={W} height={H} filter="url(#grain)" class="grain" />

    <g class="labels">
      {#each nodes as n, i}
        <text x={n.lx} y={n.ly} text-anchor="middle" class="node-lbl" class:on={hover === i}>{n.label}</text>
        {#if hover === i}
          <text x={n.lx} y={n.ly + 18} text-anchor="middle" class="hint">{n.hint}</text>
        {/if}
      {/each}
    </g>

    <!-- B4. conduction spike: coral comet (hot coral core, coral tail) -->
    {#if firing !== -1}
      {#key firing}
        <g filter="url(#bloom)">
          {#each [0, 0.05, 0.1] as lag, k}
            <circle r={5 - k * 1.4} fill={k === 0 ? CORAL_HOT : CORAL} opacity={1 - k * 0.3}>
              <animateMotion dur="0.62s" begin={lag + "s"} fill="freeze"
                calcMode="spline" keyPoints="0;1" keyTimes="0;1" keySplines="0.5 0 0.3 1">
                <mpath href={"#trunk-" + firing} />
              </animateMotion>
            </circle>
          {/each}
        </g>
      {/key}
    {/if}
  </svg>
  <div class="tagline">点亮一个神经元 · 信号沿树突传导至终扣</div>
</div>

<style>
  .hub {
    position: relative; height: 100%; overflow: hidden;
    display: flex; align-items: center; justify-content: center;
    background: #0c0a09;
  }
  .canvas { width: 100%; height: 100%; }

  /* B1. depth layers: parallax offsets transition smoothly, additive over ground */
  .layer { transform-box: view-box; transition: transform 0.35s cubic-bezier(.22,.61,.36,1); }
  .layer.back { opacity: 0.2; }
  .layer.fore { opacity: 0.12; mix-blend-mode: screen; }
  /* B2. additive fluorescence: bright fibres brighten the dark ground */
  .glowgroup { mix-blend-mode: screen; }

  /* B3. grain sits as a soft-light veil; vignette is plain multiply-ish overlay */
  .grain { opacity: 0.025; mix-blend-mode: soft-light; pointer-events: none; }
  .vignette { pointer-events: none; }
  .dust { pointer-events: none; }
  .mote { transform-box: fill-box; transform-origin: center; animation: drift var(--dur) ease-in-out infinite alternate; }
  @keyframes drift {
    from { transform: translate(0, 0); }
    to { transform: translate(var(--dx), var(--dy)); }
  }

  /* slow whole-neuron breathing */
  .breathe { animation: breathe 8s ease-in-out infinite; }
  @keyframes breathe {
    0%, 100% { transform: scale(1); opacity: 0.94; }
    50% { transform: scale(1.03); opacity: 1; }
  }

  .twinkle { animation: twinkle 3.6s ease-in-out infinite; transform-box: fill-box; transform-origin: center; }
  @keyframes twinkle { 0%, 100% { opacity: 0.35; } 50% { opacity: 1; } }

  /* B2. overexposed white core pulses gently */
  .core { animation: corepulse 4s ease-in-out infinite; }
  @keyframes corepulse { 0%, 100% { opacity: 0.85; } 50% { opacity: 1; } }
  .charge-hot { animation: chargehot 0.28s ease-out; }
  @keyframes chargehot { 0% { opacity: 0; } 40% { opacity: 0.9; } 100% { opacity: 0; } }

  .soma { transition: filter 0.2s; }
  .soma.charge { animation: charge 0.28s ease-out; }
  @keyframes charge {
    0% { filter: url(#bloom) brightness(1); }
    45% { filter: url(#bloom) brightness(1.9); }
    100% { filter: url(#bloom) brightness(1); }
  }
  .ripple { animation: ripple 0.85s cubic-bezier(0,.55,.45,1) forwards; }
  @keyframes ripple { 0% { r: 46px; opacity: 0.7; } 100% { r: 150px; opacity: 0; } }

  .node { cursor: pointer; transition: transform 0.25s cubic-bezier(.45,.05,.55,.95); transform-box: view-box; }
  .node-halo { opacity: 0.5; transition: opacity 0.25s; }
  .node:hover, .node:focus-visible { transform: scale(1.18); outline: none; }
  .node:hover .node-halo, .node:focus-visible .node-halo { opacity: 1; }
  .node.fire { animation: pop 0.4s ease-out; }
  .node .hot { filter: drop-shadow(0 0 6px var(--coral, #FF6B4A)); }
  @keyframes pop { 0% { transform: scale(1); } 45% { transform: scale(1.5); } 100% { transform: scale(1); } }

  .node-lbl {
    fill: #FFF4D6; font-family: var(--font-ui); font-size: 15px; font-weight: 600;
    pointer-events: none; opacity: 0.9; transition: opacity 0.2s;
    text-shadow: 0 1px 4px rgba(0,0,0,0.8);
  }
  .node-lbl.on { opacity: 1; }
  .hint { fill: #E8C88A; font-family: var(--font-ui); font-size: 12px; pointer-events: none; }

  .tagline {
    position: absolute; bottom: 26px; left: 0; right: 0; text-align: center;
    color: #8a6a45; font-family: var(--font-mono); font-size: 12px; letter-spacing: 0.5px;
  }

  @media (prefers-reduced-motion: reduce) {
    .breathe, .twinkle, .ripple, .soma.charge, .core, .charge-hot, .mote { animation: none; }
    .node, .layer { transition: none; }
  }
</style>

