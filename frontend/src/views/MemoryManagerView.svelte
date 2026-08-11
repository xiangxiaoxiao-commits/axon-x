<script lang="ts">
  // View and edit what Claude Code remembers: the global CLAUDE.md and each
  // project's memory/*.md files. Edits are saved back to disk; Claude Code
  // loads them on the next session (no MCP needed).
  import { onMount } from "svelte";
  import {
    ListClaudeProjects, ListMemoryFiles, WriteMemoryFile, DeleteMemoryFile,
  } from "../../wailsjs/go/main/App.js";
  import type { claudedata } from "../../wailsjs/go/models";

  let projects: claudedata.Project[] = [];
  let curProject = "";
  let files: claudedata.MemoryFile[] = [];
  let selected: claudedata.MemoryFile | null = null;
  let draft = "";
  let dirty = false;
  let status = "";

  onMount(async () => {
    try { projects = await ListClaudeProjects(); } catch (e) { console.error(e); }
    await loadFiles(projects.length ? projects[0].slug : "");
  });

  async function loadFiles(slug: string) {
    curProject = slug; selected = null; draft = ""; dirty = false;
    try { files = await ListMemoryFiles(slug); } catch (e) { console.error(e); files = []; }
  }

  function select(f: claudedata.MemoryFile) {
    selected = f; draft = f.content; dirty = false; status = "";
  }

  async function save() {
    if (!selected) return;
    try {
      await WriteMemoryFile(selected.path, draft);
      selected.content = draft; dirty = false; status = "已保存 · Claude 下次会话会加载";
    } catch (e) { status = "保存失败: " + e; }
  }

  async function remove() {
    if (!selected || selected.scope === "instructions") return;
    if (!confirm("删除这个记忆文件？")) return;
    try {
      await DeleteMemoryFile(selected.path);
      await loadFiles(curProject);
      status = "已删除";
    } catch (e) { status = "删除失败: " + e; }
  }
</script>

<div class="mem">
  <div class="col sidebar">
    <select class="proj-select" bind:value={curProject} on:change={() => loadFiles(curProject)}>
      {#each projects as p}<option value={p.slug}>{p.path}</option>{/each}
    </select>
    <div class="files">
      {#each files as f}
        <button class="item" class:active={selected?.path === f.path} on:click={() => select(f)}>
          <span class="badge {f.scope}">{f.scope === "instructions" ? "规范" : "记忆"}</span>
          {f.name}
        </button>
      {/each}
      {#if files.length === 0}<div class="empty">这个项目还没有记忆文件</div>{/if}
    </div>
  </div>

  <div class="editor">
    {#if selected}
      <div class="ed-head">
        <span class="path">{selected.path}</span>
        <span class="spacer"></span>
        {#if status}<span class="status">{status}</span>{/if}
        {#if selected.scope !== "instructions"}
          <button class="btn danger" on:click={remove}>删除</button>
        {/if}
        <button class="btn" disabled={!dirty} on:click={save}>保存</button>
      </div>
      <textarea class="selectable" bind:value={draft} on:input={() => { dirty = true; }}></textarea>
    {:else}
      <div class="empty">选择左侧文件查看/编辑 Claude 记住的内容</div>
    {/if}
  </div>
</div>

<style>
  .mem { display: flex; height: 100%; font-family: var(--font-mono); }
  .sidebar { flex: 0 0 280px; border-right: 1px solid var(--border); overflow-y: auto; }
  .proj-select {
    width: 100%;
    background: var(--bg-base);
    color: var(--text-primary);
    border: none;
    border-bottom: 1px solid var(--border);
    font-family: var(--font-mono);
    font-size: 12px;
    padding: 8px;
    outline: none;
  }
  .item {
    display: flex;
    align-items: center;
    gap: 6px;
    width: 100%;
    text-align: left;
    background: transparent;
    border: none;
    border-bottom: 1px solid var(--border);
    padding: 8px 12px;
    color: var(--text-primary);
    font-size: 12.5px;
  }
  .item:hover { background: var(--bg-elevated); }
  .item.active { background: var(--bg-elevated); box-shadow: inset 2px 0 0 var(--accent); }
  .badge { font-size: 10px; padding: 1px 5px; border-radius: 3px; }
  .badge.instructions { background: var(--warning); color: #000; }
  .badge.memory { background: var(--accent); color: #fff; }
  .editor { flex: 1; display: flex; flex-direction: column; min-width: 0; }
  .ed-head {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 8px 12px;
    border-bottom: 1px solid var(--border);
    font-size: 11px;
  }
  .path { color: var(--text-muted); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; max-width: 50%; }
  .spacer { flex: 1; }
  .status { color: var(--success); }
  .btn {
    background: transparent;
    border: 1px solid var(--accent);
    color: var(--accent);
    border-radius: var(--radius-control);
    padding: 3px 12px;
    font-family: var(--font-mono);
    font-size: 12px;
  }
  .btn:disabled { border-color: var(--border); color: var(--text-muted); }
  .btn.danger { border-color: var(--danger); color: var(--danger); }
  textarea {
    flex: 1;
    background: var(--bg-base);
    color: var(--text-primary);
    border: none;
    outline: none;
    resize: none;
    font-family: var(--font-mono);
    font-size: 13px;
    line-height: 1.55;
    padding: 12px;
  }
  .empty { padding: 16px; color: var(--text-muted); font-size: 12px; }
</style>

