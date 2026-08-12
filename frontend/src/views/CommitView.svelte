<script lang="ts">
  // Commit view: the primary entry of Axon. Read a repo's changes, generate a
  // conventional commit message (with optional PR text) grounded in the
  // project's knowledge graph, let the user edit it, then commit locally.
  // Never pushes.
  import { onMount } from "svelte";
  import { currentProject, projects } from "../lib/stores";
  import type { gitx, main } from "../../wailsjs/go/models";
  import {
    RepoStatus, GenerateCommit, DoCommit, ListProviders, ListModels,
  } from "../../wailsjs/go/main/App.js";

  type Scope = "staged" | "unstaged" | "all";

  let dir = "";
  let status: gitx.RepoStatus | null = null;
  let repoErr = "";
  let loadingRepo = false;

  let scope: Scope = "staged";

  let providers: main.ProviderInfo[] = [];
  let providerName = ""; // "" => use the configured default provider
  let models: string[] = [];
  let model = ""; // "" => use the configured default model

  let withPR = false;
  let generating = false;
  let genErr = "";

  let draft: main.CommitDraft | null = null;
  let message = "";
  let prTitle = "";
  let prBody = "";

  let stageAll = false;
  let committing = false;
  let commitErr = "";
  let committedHash = "";

  $: curProjectPath = $projects.find((p) => p.slug === $currentProject)?.path ?? "";
  $: stagedCount = status?.changes?.filter((c) => c.staged).length ?? 0;
  $: noStaged = scope === "staged" && !status?.hasStaged;
  $: canGenerate = !!status?.isRepo && !generating && !noStaged;
  $: canCommit = !!status?.isRepo && !!message.trim() && !committing;

  // Lint the subject line against type(scope): description. Advisory only.
  const TYPES = ["feat", "fix", "refactor", "docs", "test", "chore"];
  $: subject = message.split("\n")[0] ?? "";
  $: lintHints = ((): string[] => {
    if (!subject.trim()) return [];
    const hints: string[] = [];
    const m = subject.match(/^(\w+)(\([^)]*\))?: .+/);
    if (!m) hints.push("首行建议遵循 type(scope): description");
    else if (!TYPES.includes(m[1])) hints.push(`type "${m[1]}" 不在 ${TYPES.join("/")} 中`);
    if (subject.length > 72) hints.push(`首行 ${subject.length} 字，建议 ≤ 72`);
    return hints;
  })();

  function statusClass(s: string): string {
    const c = s.trim().charAt(0).toUpperCase();
    if (c === "A") return "st-add";
    if (c === "M") return "st-mod";
    if (c === "D") return "st-del";
    if (c === "R") return "st-ren";
    return "st-unk";
  }

  async function loadRepo() {
    if (!dir.trim()) { status = null; repoErr = ""; return; }
    loadingRepo = true; repoErr = ""; committedHash = ""; commitErr = "";
    try {
      status = await RepoStatus(dir.trim());
      if (!status.isRepo) repoErr = "该目录不是 git 仓库";
    } catch (e: any) {
      status = null;
      repoErr = String(e?.message || e);
    } finally {
      loadingRepo = false;
    }
  }

  async function loadProviders() {
    try { providers = await ListProviders(); } catch (e) { console.error("providers", e); }
  }
  async function onProviderChange() {
    models = []; model = "";
    if (!providerName) return;
    try { models = await ListModels(providerName); } catch (e) { console.error("models", e); }
  }

  async function generate() {
    if (!canGenerate) return;
    generating = true; genErr = ""; committedHash = "";
    try {
      draft = await GenerateCommit(dir.trim(), scope, providerName, model, $currentProject, withPR);
      message = draft.message ?? "";
      prTitle = draft.prTitle ?? "";
      prBody = draft.prBody ?? "";
    } catch (e: any) {
      genErr = String(e?.message || e);
    } finally {
      generating = false;
    }
  }

  async function commit() {
    if (!canCommit) return;
    committing = true; commitErr = ""; committedHash = "";
    try {
      committedHash = await DoCommit(dir.trim(), message, stageAll);
      await loadRepo();
    } catch (e: any) {
      commitErr = String(e?.message || e);
    } finally {
      committing = false;
    }
  }

  function onKey(e: KeyboardEvent) {
    if (!(e.metaKey || e.ctrlKey)) return;
    if (e.key === "Enter" && e.shiftKey) { e.preventDefault(); commit(); }
    else if (e.key === "Enter") { e.preventDefault(); generate(); }
  }

  // Prefill the repo path from the globally-selected project once, then let the
  // user override. Only fills while the field is still empty.
  let prefilled = false;
  $: if (!prefilled && !dir && curProjectPath) { dir = curProjectPath; prefilled = true; loadRepo(); }

  onMount(loadProviders);
</script>

<svelte:window on:keydown={onKey} />

<div class="commit">
  <div class="col">
    <!-- Repo -->
    <section class="card">
      <div class="row">
        <span class="k">仓库</span>
        <input
          class="path"
          type="text"
          placeholder="粘贴 git 仓库路径，或在顶栏选择项目"
          bind:value={dir}
          on:keydown={(e) => { if (e.key === "Enter") { e.preventDefault(); loadRepo(); } }}
        />
        <button class="btn" on:click={loadRepo} disabled={loadingRepo || !dir.trim()}>
          {loadingRepo ? "读取中…" : "读取"}
        </button>
      </div>
      {#if repoErr}
        <p class="hint err">{repoErr}</p>
      {:else if status?.isRepo}
        <p class="repo-meta">
          <span class="branch">⑂ {status.branch}</span>
          <span class="root selectable">{status.root}</span>
          <span class="staged-n">{stagedCount} 个已暂存</span>
        </p>
      {:else if !dir.trim()}
        <p class="hint">填入或选择一个仓库路径开始。</p>
      {/if}
    </section>

    <!-- Changes -->
    {#if status?.isRepo}
      <section class="card">
        <div class="row between">
          <span class="k">变更 · {status.changes?.length ?? 0}</span>
          <div class="seg">
            {#each [["staged","已暂存"],["unstaged","未暂存"],["all","全部"]] as [v, lbl]}
              <button class="seg-b" class:on={scope === v} on:click={() => (scope = v as Scope)}>{lbl}</button>
            {/each}
          </div>
        </div>
        {#if status.changes?.length}
          <ul class="files">
            {#each status.changes as c}
              <li class="file">
                <span class="badge {statusClass(c.status)}">{c.status.trim() || "?"}</span>
                <span class="fpath selectable">{c.path}</span>
                <span class="tags">
                  {#if c.staged}<span class="tag staged">staged</span>{/if}
                  {#if c.unstaged}<span class="tag unstaged">unstaged</span>{/if}
                </span>
              </li>
            {/each}
          </ul>
        {:else}
          <p class="hint">工作区干净，没有改动。</p>
        {/if}
        {#if noStaged}
          <p class="hint warn">没有已暂存的改动，切到「未暂存 / 全部」或先 git add。</p>
        {/if}
      </section>

      <!-- Generate -->
      <section class="card">
        <div class="row wrap">
          <label class="k-field">
            <span class="k">模型</span>
            <select bind:value={providerName} on:change={onProviderChange}>
              <option value="">默认</option>
              {#each providers as p}
                <option value={p.name} disabled={!p.hasKey}>{p.name}{p.hasKey ? "" : "（无 key）"}</option>
              {/each}
            </select>
          </label>
          <label class="k-field">
            <span class="k">版本</span>
            <select bind:value={model} disabled={!providerName || !models.length}>
              <option value="">默认</option>
              {#each models as m}<option value={m}>{m}</option>{/each}
            </select>
          </label>
          <div class="k-field">
            <span class="k">项目背景</span>
            <span class="proj-cur selectable">{curProjectPath || "纯 diff（未选项目）"}</span>
          </div>
          <label class="chk">
            <input type="checkbox" bind:checked={withPR} />
            同时生成 PR 描述
          </label>
        </div>
        <div class="row">
          <button class="btn primary" on:click={generate} disabled={!canGenerate}>
            {#if generating}<span class="axon-signal"><i></i><i></i><i></i></span> 生成中…{:else}生成 commit message{/if}
          </button>
          <span class="kbd">⌘↵ 生成 · ⌘⇧↵ 提交</span>
        </div>
        {#if genErr}<p class="hint err">{genErr}</p>{/if}
      </section>

      <!-- Result -->
      {#if draft}
        <section class="card">
          {#if draft.warnings?.length}
            <div class="warn-box">
              <strong>⚠ 密钥/敏感信息警告</strong>
              <ul>{#each draft.warnings as w}<li>{w}</li>{/each}</ul>
            </div>
          {/if}
          <div class="row between">
            <span class="k">Commit message（可编辑）</span>
            {#if lintHints.length}
              <span class="lint">{lintHints.join(" · ")}</span>
            {:else}
              <span class="lint ok">符合规范</span>
            {/if}
          </div>
          <textarea class="msg" bind:value={message} rows="6" spellcheck="false"></textarea>

          {#if withPR}
            <div class="row"><span class="k">PR 标题（可编辑）</span></div>
            <input class="path" type="text" bind:value={prTitle} />
            <div class="row"><span class="k">PR 描述（可编辑）</span></div>
            <textarea class="msg" bind:value={prBody} rows="8" spellcheck="false"></textarea>
          {/if}

          {#if draft.truncated}
            <p class="hint warn">diff 过大已截断，message 可能遗漏部分改动。</p>
          {/if}
          {#if draft.usedKnowledge?.length}
            <p class="prov">参考了：{#each draft.usedKnowledge as u}<span class="chip">{u}</span>{/each}</p>
          {/if}
          {#if draft.knowledgeSources?.length}
            <p class="prov src">来源会话：{draft.knowledgeSources.join("、")}</p>
          {/if}
        </section>
      {/if}

      <!-- Commit -->
      <section class="card commit-bar">
        <label class="chk">
          <input type="checkbox" bind:checked={stageAll} />
          提交前 stage 全部改动（默认只提交已暂存）
        </label>
        <div class="row between">
          <span class="local">仅本地提交，绝不 push</span>
          <button class="btn go" on:click={commit} disabled={!canCommit}>
            {committing ? "提交中…" : "提交"}
          </button>
        </div>
        {#if committedHash}
          <p class="hint ok">已提交 · {committedHash}</p>
        {/if}
        {#if commitErr}<p class="hint err">{commitErr}</p>{/if}
      </section>
    {/if}
  </div>
</div>
<style>
  .commit {
    height: 100%; overflow-y: auto;
    background: var(--bg-base); font-family: var(--font-ui);
    padding: 20px 16px;
  }
  .col { max-width: 860px; margin: 0 auto; display: flex; flex-direction: column; gap: 14px; }

  .card {
    background: var(--bg-surface); border: 1px solid var(--border);
    border-radius: var(--radius-card); padding: 14px;
    display: flex; flex-direction: column; gap: 10px;
  }
  .row { display: flex; align-items: center; gap: 10px; }
  .row.between { justify-content: space-between; }
  .row.wrap { flex-wrap: wrap; }

  .k { font-size: 11px; color: var(--text-muted); font-family: var(--font-mono); white-space: nowrap; }

  .path {
    flex: 1; background: var(--bg-elevated); color: var(--text-primary);
    border: 1px solid var(--border); border-radius: var(--radius-control);
    font-family: var(--font-mono); font-size: 13px; padding: 7px 10px; outline: none;
  }
  .path:focus { border-color: var(--accent); }

  .btn {
    background: var(--bg-elevated); color: var(--text-primary);
    border: 1px solid var(--border); border-radius: var(--radius-control);
    font-size: 13px; padding: 7px 14px; white-space: nowrap;
  }
  .btn:hover:not(:disabled) { border-color: var(--accent); }
  .btn:disabled { opacity: 0.45; cursor: default; }
  .btn.primary { background: var(--accent); color: var(--accent-fg); border-color: var(--accent); }
  .btn.go { background: var(--success); color: #0d1117; border-color: var(--success); font-weight: 600; }

  .repo-meta { display: flex; align-items: center; gap: 12px; margin: 0; flex-wrap: wrap; font-size: 12px; }
  .branch { color: var(--accent); font-family: var(--font-mono); }
  .root { color: var(--text-muted); font-family: var(--font-mono); font-size: 11.5px; }
  .staged-n { color: var(--text-primary); font-family: var(--font-mono); }

  .hint { margin: 0; font-size: 12px; color: var(--text-muted); }
  .hint.err { color: var(--danger); }
  .hint.warn { color: var(--warning); }
  .hint.ok { color: var(--success); font-family: var(--font-mono); }

  .seg { display: inline-flex; border: 1px solid var(--border); border-radius: var(--radius-control); overflow: hidden; }
  .seg-b { background: var(--bg-elevated); color: var(--text-muted); border: none; padding: 5px 12px; font-size: 12px; }
  .seg-b.on { background: var(--accent); color: var(--accent-fg); }

  .files { list-style: none; margin: 0; padding: 0; display: flex; flex-direction: column; gap: 4px; max-height: 220px; overflow-y: auto; }
  .file { display: flex; align-items: center; gap: 8px; font-family: var(--font-mono); font-size: 12.5px; }
  .fpath { flex: 1; color: var(--text-primary); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .badge { width: 18px; text-align: center; border-radius: 3px; font-size: 11px; font-weight: 700; }
  .st-add { background: rgba(63,185,80,0.18); color: var(--success); }
  .st-mod { background: rgba(210,153,34,0.18); color: var(--warning); }
  .st-del { background: rgba(248,81,73,0.18); color: var(--danger); }
  .st-ren { background: rgba(59,130,246,0.18); color: var(--accent); }
  .st-unk { background: var(--bg-elevated); color: var(--text-muted); }
  .tags { display: flex; gap: 4px; }
  .tag { font-size: 10px; padding: 1px 5px; border-radius: 3px; }
  .tag.staged { background: rgba(63,185,80,0.16); color: var(--success); }
  .tag.unstaged { background: rgba(210,153,34,0.16); color: var(--warning); }

  .k-field { display: flex; align-items: center; gap: 6px; }
  select {
    background: var(--bg-elevated); color: var(--text-primary);
    border: 1px solid var(--border); border-radius: var(--radius-control);
    font-family: var(--font-mono); font-size: 12px; padding: 5px 8px; outline: none; max-width: 220px;
  }
  select:focus { border-color: var(--accent); }
  select:disabled { opacity: 0.5; }
  .proj-cur { font-family: var(--font-mono); font-size: 11.5px; color: var(--text-primary); max-width: 240px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .chk { display: flex; align-items: center; gap: 5px; font-size: 12px; color: var(--text-primary); }
  .kbd { font-family: var(--font-mono); font-size: 11px; color: var(--text-muted); }

  .msg {
    width: 100%; resize: vertical; background: var(--bg-elevated); color: var(--text-primary);
    border: 1px solid var(--border); border-radius: var(--radius-control); caret-color: var(--accent);
    font-family: var(--font-mono); font-size: 13px; line-height: 1.55; padding: 10px; outline: none;
  }
  .msg:focus { border-color: var(--accent); }
  .lint { font-size: 11px; color: var(--warning); font-family: var(--font-mono); }
  .lint.ok { color: var(--success); }

  .warn-box {
    background: rgba(248,81,73,0.12); border: 1px solid var(--danger);
    border-radius: var(--radius-control); padding: 8px 10px; color: var(--danger); font-size: 12.5px;
  }
  .warn-box ul { margin: 4px 0 0; padding-left: 18px; }

  .prov { margin: 0; font-size: 11.5px; color: var(--text-muted); display: flex; flex-wrap: wrap; align-items: center; gap: 5px; }
  .prov.src { font-family: var(--font-mono); }
  .chip { background: var(--bg-elevated); border: 1px solid var(--border); border-radius: 10px; padding: 1px 8px; font-size: 11px; color: var(--accent); }

  .commit-bar { border-color: var(--success); }
  .local { font-size: 11.5px; color: var(--success); font-family: var(--font-mono); }
</style>


