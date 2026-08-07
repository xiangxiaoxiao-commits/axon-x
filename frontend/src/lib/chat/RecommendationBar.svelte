<script lang="ts">
  // Light recommendation hint (UX §3.2). Non-modal: shows the classified task
  // type and recommended model. Doing nothing and sending just uses the default.
  import type { main } from "../../../wailsjs/go/models";

  export let taskType = "";
  export let rec: main.Recommendation | null = null;
  export let loading = false;

  // High-cost tier warning threshold (UX §3.2: single call > $10).
  $: highCost = !!rec && rec.costUsd > 10;
</script>

{#if loading}
  <div class="bar">
    <span class="tip">💡 正在评估任务类型…</span>
  </div>
{:else if rec}
  <div class="bar" class:warn={highCost}>
    <span class="tip">
      💡 推荐
      <span class="model">{rec.model}</span>
      {#if taskType}<span class="task">· {taskType}</span>{/if}
      <span class="cost">~${rec.costUsd.toFixed(2)}</span>
      {#if rec.minutes}<span class="mins">· ~{Math.round(rec.minutes)}m</span>{/if}
      {#if !rec.available}<span class="na">· 该模型不可用</span>{/if}
    </span>
    {#if highCost}<span class="hint">高成本档位,确认?</span>{/if}
  </div>
{/if}

<style>
  .bar {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 5px 12px;
    font-size: 12px;
    color: var(--text-muted);
    background: var(--bg-surface);
    border: 1px solid var(--border);
    border-radius: var(--radius-control);
    margin-bottom: var(--space);
  }
  .bar.warn {
    border-color: var(--warning);
    color: var(--warning);
  }
  .model {
    font-family: var(--font-mono);
    color: var(--text-primary);
  }
  .cost {
    font-family: var(--font-mono);
  }
  .na {
    color: var(--danger);
  }
  .hint {
    margin-left: auto;
    color: var(--warning);
  }
</style>
