<script lang="ts">
  // First-run checklist shown in the knowledge view's empty state. It turns the
  // (previously implicit) correct order — configure a model, pick a recall mode,
  // build the graph, hook it into Claude Code — into four visible, clickable
  // steps, each with a done/todo state so a new user is never stuck on a blank
  // canvas wondering what to do.
  import { activeView, requestIndex } from "../lib/stores";

  // Status flags resolved by the parent (it owns the backend calls / project).
  export let hasProvider = false; // a provider with a stored key exists
  export let hasEmbedMode = false; // recall mode explicitly chosen (always true once loaded)
  export let hasGraph = false; // current project already has entities
  export let mcpInstalled = false; // axon-knowledge registered in Claude Code
  export let projectSelected = false; // a project is chosen in the sidebar
  export let indexing = false;

  $: steps = [
    {
      n: 1,
      done: hasProvider,
      title: "配置一个模型 Provider",
      desc: "去设置添加 OpenAI / 智谱 GLM / Anthropic 或你的网关，粘贴 API Key。建索引和语义召回都需要它。",
      action: "去设置配置",
      run: () => ($activeView = "settings"),
    },
    {
      n: 2,
      done: hasProvider && hasEmbedMode,
      title: "选择召回方式",
      desc: "关键词（离线、零成本）或语义模型（精度高、需云端 embedding）。在设置里切换。",
      action: "去选择",
      run: () => ($activeView = "settings"),
    },
    {
      n: 3,
      done: hasGraph,
      title: "建立知识图谱",
      desc: projectSelected
        ? "读这个项目的历史对话并提炼知识。也可以从代码或 Obsidian 笔记建图。"
        : "先在左下角选一个项目，再建索引。",
      action: indexing ? "建索引中…" : "建索引",
      run: () => projectSelected && !indexing && requestIndex(),
      disabled: !projectSelected || indexing,
    },
    {
      n: 4,
      done: mcpInstalled,
      title: "接入 Claude Code",
      desc: "一键把图谱接给 AI，让它在写代码时自动读懂你的项目。",
      action: "去接入",
      run: () => ($activeView = "settings"),
    },
  ];

  $: doneCount = steps.filter((s) => s.done).length;
</script>

<div class="ob">
  <div class="ob-head">
    <div class="ob-title">开始使用 axon</div>
    <div class="ob-sub">
      让 AI 读懂你的项目，只需四步。已完成 {doneCount}/4。
    </div>
    <div class="bar"><i style="width:{(doneCount / 4) * 100}%"></i></div>
  </div>

  <div class="steps">
    {#each steps as s (s.n)}
      <div class="step" class:done={s.done}>
        <div class="mark">{s.done ? "✓" : s.n}</div>
        <div class="body">
          <div class="s-title">{s.title}</div>
          <div class="s-desc">{s.desc}</div>
        </div>
        {#if !s.done}
          <button class="s-act" on:click={s.run} disabled={s.disabled}>{s.action}</button>
        {/if}
      </div>
    {/each}
  </div>
</div>

<style>
  .ob { max-width: 560px; padding: 40px; font-family: var(--font-ui); }
  .ob-head { margin-bottom: 20px; }
  .ob-title { font-size: 20px; font-weight: 700; color: var(--text-primary); }
  .ob-sub { margin-top: 6px; color: var(--text-muted); font-size: 13px; }
  .bar { margin-top: 12px; height: 4px; background: var(--bg-elevated); border-radius: 999px; overflow: hidden; }
  .bar i { display: block; height: 100%; background: var(--accent); transition: width .3s; }

  .steps { display: flex; flex-direction: column; gap: 10px; }
  .step {
    display: flex; align-items: flex-start; gap: 12px;
    background: var(--bg-surface); border: 1px solid var(--border);
    border-radius: var(--radius-card); padding: 14px 16px;
  }
  .step.done { opacity: .62; }
  .mark {
    flex: 0 0 24px; height: 24px; line-height: 24px; text-align: center;
    border-radius: 50%; font-size: 12px; font-weight: 700;
    background: var(--bg-elevated); color: var(--text-muted);
  }
  .step.done .mark { background: var(--accent); color: var(--accent-fg); }
  .body { flex: 1; min-width: 0; }
  .s-title { font-size: 14px; font-weight: 600; color: var(--text-primary); }
  .s-desc { margin-top: 3px; font-size: 12.5px; color: var(--text-muted); line-height: 1.6; }
  .s-act {
    flex: 0 0 auto; align-self: center;
    background: var(--accent); color: var(--accent-fg); border: none;
    border-radius: var(--radius-control); padding: 6px 14px; font-size: 12.5px;
    font-weight: 500; cursor: pointer; white-space: nowrap;
  }
  .s-act:hover:not(:disabled) { filter: brightness(1.1); }
  .s-act:disabled { opacity: .5; cursor: default; }
</style>
