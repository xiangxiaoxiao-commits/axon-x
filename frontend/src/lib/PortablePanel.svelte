<script lang="ts">
  // Export / import panel: hand a curated knowledge graph to a colleague without
  // handing over your machine's context.
  //
  // The panel exists mainly to make the removal *visible*. A privacy filter you
  // cannot inspect is one you cannot trust, so the preview shows the rule that
  // fired, how many times, and — for real facts in this graph — the text before
  // and after redaction.
  import { onMount } from "svelte";
  import {
    PreviewExport, ExportGraph, InspectBundle, ImportBundle, ListImportedBundles, RemoveImportedBundle,
  } from "../../wailsjs/go/main/App.js";
  import type { main, sanitize } from "../../wailsjs/go/models";

  export let namespace = "";
  export let onClose: () => void = () => {};
  // Called after an import so the caller can reload a graph that just changed.
  // focus=true asks the knowledge view to switch to the namespace that received
  // the knowledge (import); false is for changes the user should stay put for.
  export let onChanged: (ns: string, focus: boolean) => void = () => {};

  let tab: "export" | "import" = "export";
  let level: "standard" | "strict" = "standard";
  let busy = false;
  let error = "";
  let status = "";

  // --- export state ---
  let preview: main.ExportPreview | null = null;
  let exported: main.ExportResult | null = null;
  let openRule = "";

  // --- import state ---
  let info: main.BundleInfo | null = null;
  let targetNamespace = "";
  let reembed = true;
  let imported: main.ImportResult | null = null;
  let importedList: main.ImportedBundle[] = [];

  onMount(refreshImported);

  async function refreshImported() {
    if (!namespace) return;
    try {
      importedList = (await ListImportedBundles(namespace)) || [];
    } catch {
      importedList = [];
    }
  }

  async function run<T>(fn: () => Promise<T>): Promise<T | null> {
    busy = true; error = ""; status = "";
    try {
      return await fn();
    } catch (e: any) {
      error = String(e?.message || e);
      return null;
    } finally {
      busy = false;
    }
  }

  async function doPreview() {
    if (!namespace) { error = "先选一个命名空间"; return; }
    const p = await run(() => PreviewExport(namespace, level));
    if (p) { preview = p; exported = null; status = ""; }
  }

  async function doExport() {
    if (!namespace) { error = "先选一个命名空间"; return; }
    const r = await run(() => ExportGraph(namespace, level));
    if (!r) return;
    if (r.canceled) { status = "已取消"; return; }
    exported = r;
    status = `已导出 ${formatBytes(r.sizeBytes)}`;
  }

  async function pickBundle() {
    const i = await run(() => InspectBundle());
    if (!i) return;
    if (!i.path) { status = "已取消"; return; }
    info = i;
    imported = null;
    targetNamespace = i.suggestedNamespace || "";
  }

  async function doImport() {
    if (!info?.path) return;
    const r = await run(() => ImportBundle(info!.path, targetNamespace, reembed));
    if (!r) return;
    imported = r;
    await refreshImported();
    onChanged(r.namespace, true);
  }

  async function doRemove(id: string) {
    const r = await run(async () => { await RemoveImportedBundle(namespace, id); return true; });
    if (!r) return;
    status = "已移除该知识包的知识";
    await refreshImported();
    onChanged(namespace, false);
  }

  function formatBytes(n: number): string {
    if (!n) return "0 B";
    if (n < 1024) return `${n} B`;
    if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`;
    return `${(n / 1024 / 1024).toFixed(1)} MB`;
  }

  function formatTime(ms: number): string {
    if (!ms) return "未知";
    try { return new Date(ms).toLocaleString(); } catch { return "未知"; }
  }

  function droppedRows(s: main.ExportPreview["stats"] | undefined) {
    if (!s) return [];
    return [
      { label: "原始对话片段", n: s.omittedChunks || 0 },
      { label: "向量", n: s.omittedEmbeddings || 0 },
      { label: "来源会话 / 溯源", n: s.omittedProvenance || 0 },
      { label: "审核记录", n: s.omittedReviews || 0 },
    ].filter((r) => r.n > 0);
  }

  function lossRows(s: main.ExportPreview["stats"] | undefined) {
    if (!s) return [];
    return [
      { label: "因重名合并的实体", n: s.mergedAfterScrub || 0 },
      { label: "丢端点的关系", n: s.droppedRelations || 0 },
      { label: "被清空的事实", n: s.droppedAssertions || 0 },
      { label: "被丢弃的实体", n: s.droppedEntities || 0 },
    ].filter((r) => r.n > 0);
  }

  // Only the facts that actually changed are worth showing.
  function changedFacts(e: main.PreviewEntity) {
    return (e.facts || []).filter((f) => f.before && f.before !== f.after);
  }
  $: changedEntities = (preview?.entities || []).filter((e) => e.redacted).slice(0, 8);
</script>

<div class="backdrop" role="presentation" on:click={onClose}
     on:keydown={(e) => { if (e.key === "Escape") onClose(); }}>
  <div class="panel" role="dialog" aria-modal="true" tabindex="-1" aria-label="知识图谱导出与导入"
       on:click|stopPropagation on:keydown|stopPropagation>
    <header>
      <div class="tabs">
        <button class="tab" class:on={tab === "export"} on:click={() => (tab = "export")}>⇪ 导出</button>
        <button class="tab" class:on={tab === "import"} on:click={() => (tab = "import")}>⇩ 导入</button>
      </div>
      <button class="x" on:click={onClose} title="关闭">✕</button>
    </header>

    {#if tab === "export"}
      <section class="body">
        <p class="lede">
          把 <strong>{namespace || "（未选命名空间）"}</strong> 的知识整理成一个文件，交给同事导入。
          只带走实体、事实与关系；原始对话、向量、来源会话 ID 与审核历史都留在你这边，本地图谱不受任何影响。
        </p>

        <div class="row">
          <span class="lbl">剪除级别</span>
          <div class="seg">
            <button class="seg-b" class:on={level === "standard"} on:click={() => { level = "standard"; preview = null; }}>
              标准
            </button>
            <button class="seg-b" class:on={level === "strict"} on:click={() => { level = "strict"; preview = null; }}>
              严格
            </button>
          </div>
          <span class="hint">
            {level === "standard"
              ? "去除本地路径、密钥、邮箱手机、账号 ID、内网地址"
              : "在标准之上，再去除工单号与内部主机名端口"}
          </span>
        </div>

        <div class="row">
          <button class="primary" on:click={doPreview} disabled={busy || !namespace}>
            {busy ? "处理中…" : "预览剪除结果"}
          </button>
          <button class="ghost" on:click={doExport} disabled={busy || !namespace}>选择保存位置并导出…</button>
        </div>

        {#if preview}
          <div class="stats">
            <span class="chip">{preview.stats.entities} 个实体</span>
            <span class="chip">{preview.stats.relations} 条关系</span>
            <span class="chip">{preview.stats.observations} 条事实</span>
            <span class="chip muted">原图 {preview.stats.consideredEntities || preview.stats.entities} 个实体</span>
          </div>

          {#if droppedRows(preview.stats).length}
            <h4>不随文件带走的</h4>
            <ul class="plain">
              {#each droppedRows(preview.stats) as r}
                <li><span class="n">{r.n}</span> {r.label}</li>
              {/each}
            </ul>
          {/if}

          {#if lossRows(preview.stats).length}
            <h4>因剪除而损失的</h4>
            <ul class="plain warn">
              {#each lossRows(preview.stats) as r}
                <li><span class="n">{r.n}</span> {r.label}</li>
              {/each}
            </ul>
          {/if}

          <h4>剪除明细（{preview.redactions.length} 类命中）</h4>
          {#if preview.redactions.length === 0}
            <p class="hint">没有命中任何规则——这份图谱本来就没有可识别的个人信息。</p>
          {:else}
            <div class="rules">
              {#each preview.redactions as h}
                <button class="rule" on:click={() => (openRule = openRule === h.rule ? "" : h.rule)}>
                  <span class="r-name">{h.rule}</span>
                  <span class="r-count">{h.count}</span>
                  <span class="r-desc">{h.description}</span>
                </button>
                {#if openRule === h.rule && h.samples?.length}
                  <div class="samples">
                    {#each h.samples as s}
                      <div class="smp"><code class="del">{s.before}</code><span class="arrow">→</span><code class="keep">{s.after}</code></div>
                    {/each}
                  </div>
                {/if}
              {/each}
            </div>
          {/if}

          {#if changedEntities.length}
            <h4>知识抽查（导出后长这样）</h4>
            <div class="facts">
              {#each changedEntities as e}
                <div class="fact-ent">
                  <div class="fe-name">{e.name}{#if e.type}<span class="fe-type">{e.type}</span>{/if}</div>
                  {#each changedFacts(e) as f}
                    <div class="smp"><code class="del">{f.before}</code><span class="arrow">→</span><code class="keep">{f.after}</code></div>
                  {/each}
                </div>
              {/each}
            </div>
          {/if}

          <h4>文件里会写明</h4>
          <ul class="plain muted-list">
            {#each preview.notes || [] as n}<li>{n}</li>{/each}
          </ul>
          <p class="hint">建议文件名：<code>{preview.suggestedName}</code>（Mac / Windows 都可用的名字）</p>
        {/if}

        {#if exported}
          <div class="ok">
            <div class="ok-t">已导出到</div>
            <div class="path selectable">{exported.path}</div>
            <div class="ok-s">
              {exported.stats.entities} 个实体 · {exported.stats.relations} 条关系 · {exported.stats.observations} 条事实 ·
              {formatBytes(exported.sizeBytes)} · 剪除级别 {exported.level === "strict" ? "严格" : "标准"}
            </div>
          </div>
        {/if}
      </section>
    {:else}
      <section class="body">
        <p class="lede">
          选择同事给你的知识包（<code>*.axon-graph.json</code>）。导入会新增一份知识，不会覆盖或修改你已有的任何内容；
          随时可以在下面移除。
        </p>

        <div class="row">
          <button class="primary" on:click={pickBundle} disabled={busy}>{busy ? "处理中…" : "选择知识包…"}</button>
        </div>

        {#if info}
          <div class="stats">
            <span class="chip">{info.title || "未命名"}</span>
            <span class="chip">{info.stats.entities} 个实体</span>
            <span class="chip">{info.stats.relations} 条关系</span>
            <span class="chip">{info.stats.observations} 条事实</span>
            <span class="chip muted">格式 v{info.version} · {info.scrubLevel === "strict" ? "严格剪除" : "标准剪除"}</span>
            <span class="chip muted">导出时间 {formatTime(info.exportedAt)}</span>
          </div>

          {#if info.redactions?.length}
            <h4>导出方已剪除</h4>
            <ul class="plain">
              {#each info.redactions as h}<li><span class="n">{h.count}</span> {h.rule}</li>{/each}
            </ul>
          {/if}
          {#if info.notes?.length}
            <ul class="plain muted-list">{#each info.notes as n}<li>{n}</li>{/each}</ul>
          {/if}

          <div class="row">
            <span class="lbl">导入到命名空间</span>
            <input class="inp" bind:value={targetNamespace} placeholder="如 wanlian-共享" />
          </div>
          <div class="row">
            <label class="ck">
              <input type="checkbox" bind:checked={reembed} />
              <span>导入时生成本机向量（语义检索可用；会把知识文本发给你配置的 embedding 服务）</span>
            </label>
          </div>
          <div class="row">
            <button class="primary" on:click={doImport} disabled={busy || !targetNamespace.trim()}>导入</button>
          </div>
        {/if}

        {#if imported}
          <div class="ok">
            <div class="ok-t">已导入到「{imported.namespace}」，已切换到该命名空间</div>
            <div class="ok-s">
              新增 {imported.added.entities} 个实体 · {imported.added.relations} 条关系 · {imported.added.observations} 条事实
              {#if imported.mergedInto}· 已与既有知识合并{:else}· 该命名空间是新建的{/if}
              {#if imported.replaced}· 覆盖了此前同一份知识包的导入{/if}
              {#if imported.embeddedCount}· 已生成 {imported.embeddedCount} 个向量{/if}
            </div>
            {#if imported.embedError}<div class="warn-line">向量未生成：{imported.embedError}</div>{/if}
          </div>
        {/if}

        {#if namespace}
          <h4>「{namespace}」已导入的知识包</h4>
          {#if importedList.length === 0}
            <p class="hint">还没有导入过知识包。</p>
          {:else}
            <div class="rules">
              {#each importedList as b}
                <div class="rule static">
                  <span class="r-name mono">{b.id}</span>
                  <span class="r-desc">{b.entities} 个实体 · {b.relations} 条关系 · {b.observations} 条事实</span>
                  <button class="rm" on:click={() => doRemove(b.id)} disabled={busy} title="删除该知识包带来的知识">移除</button>
                </div>
              {/each}
            </div>
          {/if}
        {/if}
      </section>
    {/if}

    {#if error}<div class="err">{error}</div>{/if}
    {#if status && !error}<div class="note">{status}</div>{/if}
  </div>
</div>

<style>
  .backdrop {
    position: fixed; inset: 0; background: rgba(0, 0, 0, .55);
    display: flex; align-items: center; justify-content: center; z-index: 60;
  }
  .panel {
    width: min(920px, 92vw); max-height: 88vh; display: flex; flex-direction: column;
    background: var(--bg-surface); border: 1px solid var(--border);
    border-radius: var(--radius-card); overflow: hidden;
  }
  header {
    display: flex; align-items: center; justify-content: space-between;
    padding: 10px 14px; border-bottom: 1px solid var(--border); background: var(--bg-elevated);
  }
  .tabs { display: flex; gap: 6px; }
  .tab {
    background: transparent; border: 1px solid transparent; color: var(--text-muted);
    padding: 5px 12px; border-radius: var(--radius-control); cursor: pointer; font-size: 13px;
  }
  .tab.on { background: var(--bg-surface); border-color: var(--border); color: var(--text-primary); }
  .x { background: transparent; border: 0; color: var(--text-muted); cursor: pointer; font-size: 14px; padding: 4px 8px; }
  .x:hover { color: var(--text-primary); }

  .body { padding: 14px 16px 20px; overflow-y: auto; display: flex; flex-direction: column; gap: 12px; }
  .lede { margin: 0; color: var(--text-muted); font-size: 13px; line-height: 1.6; }
  .lede strong { color: var(--text-primary); }

  h4 { margin: 6px 0 0; font-size: 13px; color: var(--text-primary); font-weight: 600; }

  .row { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; }
  .lbl { font-size: 13px; color: var(--text-muted); min-width: 84px; }
  .hint { font-size: 12px; color: var(--text-muted); }

  .seg { display: inline-flex; border: 1px solid var(--border); border-radius: var(--radius-control); overflow: hidden; }
  .seg-b { background: transparent; border: 0; color: var(--text-muted); padding: 5px 14px; cursor: pointer; font-size: 13px; }
  .seg-b.on { background: var(--bg-elevated); color: var(--text-primary); }

  button.primary {
    background: var(--accent); border: 1px solid var(--accent); color: var(--accent-fg);
    padding: 6px 14px; border-radius: var(--radius-control); cursor: pointer; font-size: 13px;
  }
  button.primary:disabled { opacity: .5; cursor: default; }
  button.ghost {
    background: transparent; border: 1px solid var(--border); color: var(--text-primary);
    padding: 6px 14px; border-radius: var(--radius-control); cursor: pointer; font-size: 13px;
  }
  button.ghost:disabled { opacity: .5; cursor: default; }

  .inp {
    flex: 1; min-width: 200px; background: var(--bg-base); border: 1px solid var(--border);
    color: var(--text-primary); border-radius: var(--radius-control); padding: 6px 9px; font-size: 13px;
  }
  .ck { display: flex; align-items: flex-start; gap: 8px; font-size: 12px; color: var(--text-muted); cursor: pointer; }

  .stats { display: flex; flex-wrap: wrap; gap: 6px; }
  .chip {
    background: var(--bg-elevated); border: 1px solid var(--border); color: var(--text-primary);
    border-radius: 999px; padding: 2px 10px; font-size: 12px;
  }
  .chip.muted { color: var(--text-muted); }

  ul.plain { margin: 0; padding-left: 18px; font-size: 13px; color: var(--text-muted); }
  ul.plain li { margin: 2px 0; }
  ul.plain .n { color: var(--text-primary); font-weight: 600; }
  ul.plain.warn li { color: var(--warning); }
  .muted-list { opacity: .8; }

  .rules { display: flex; flex-direction: column; gap: 4px; }
  .rule {
    display: grid; grid-template-columns: 1fr auto; gap: 2px 12px; text-align: left;
    background: var(--bg-elevated); border: 1px solid var(--border); border-radius: var(--radius-control);
    padding: 7px 10px; cursor: pointer; color: var(--text-primary);
  }
  .rule.static { grid-template-columns: auto 1fr auto; align-items: center; cursor: default; }
  .r-name { font-size: 13px; }
  .mono { font-family: var(--font-mono); font-size: 12px; }
  .r-count { font-size: 12px; color: var(--accent); font-weight: 600; }
  .r-desc { grid-column: 1 / -1; font-size: 12px; color: var(--text-muted); }
  .rm {
    background: transparent; border: 1px solid var(--border); color: var(--danger);
    border-radius: var(--radius-control); padding: 3px 9px; cursor: pointer; font-size: 12px;
  }

  .samples, .facts { display: flex; flex-direction: column; gap: 6px; }
  .smp {
    display: flex; align-items: flex-start; gap: 8px; font-size: 12px;
    padding: 6px 10px; background: var(--bg-base); border: 1px solid var(--border); border-radius: var(--radius-control);
  }
  .smp code { flex: 1; white-space: pre-wrap; word-break: break-all; font-family: var(--font-mono); }
  .del { color: var(--danger); text-decoration: line-through; opacity: .85; }
  .keep { color: var(--success); }
  .arrow { color: var(--text-muted); }
  .fact-ent { display: flex; flex-direction: column; gap: 4px; }
  .fe-name { font-size: 13px; color: var(--text-primary); margin-top: 4px; }
  .fe-type { margin-left: 8px; font-size: 11px; color: var(--text-muted); }

  .ok {
    background: rgba(63, 185, 80, .08); border: 1px solid var(--success);
    border-radius: var(--radius-control); padding: 10px 12px;
  }
  .ok-t { font-size: 13px; color: var(--success); font-weight: 600; }
  .ok-s { font-size: 12px; color: var(--text-muted); margin-top: 4px; }
  .path {
    font-family: var(--font-mono); font-size: 12px; color: var(--text-primary);
    margin-top: 4px; word-break: break-all;
  }
  .warn-line { font-size: 12px; color: var(--warning); margin-top: 4px; }

  .err {
    padding: 8px 14px; background: rgba(248, 81, 73, .12); color: var(--danger);
    border-top: 1px solid var(--danger); font-size: 13px;
  }
  .note {
    padding: 8px 14px; background: var(--bg-elevated); color: var(--text-muted);
    border-top: 1px solid var(--border); font-size: 12px;
  }
</style>
