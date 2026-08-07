<script lang="ts">
  // Read-only summary of one configured provider. [编辑] opens the form.
  import { createEventDispatcher } from "svelte";
  import type { main } from "../../../wailsjs/go/models";

  export let info: main.ProviderInfo;

  const dispatch = createEventDispatcher<{ edit: void }>();
</script>

<div class="card">
  <div class="head">
    <span class="name">{info.name}</span>
    <span class="status" class:on={info.hasKey}>
      {info.hasKey ? "● 已连接" : "○ 未配置"}
    </span>
  </div>
  <div class="meta">
    <span class="tag mono">{info.protocol}</span>
    <span class="url mono">{info.baseUrl}</span>
  </div>
  <div class="actions">
    <button class="btn" type="button" on:click={() => dispatch("edit")}>编辑</button>
  </div>
</div>

<style>
  .card {
    background: var(--bg-surface);
    border: 1px solid var(--border);
    border-radius: var(--radius-card);
    padding: 12px 14px;
    display: flex;
    flex-direction: column;
    gap: 8px;
  }
  .head {
    display: flex;
    align-items: center;
    justify-content: space-between;
  }
  .name {
    font-size: 14px;
    font-weight: 500;
  }
  .status {
    font-size: 12px;
    color: var(--text-muted);
  }
  .status.on {
    color: var(--success);
  }
  .meta {
    display: flex;
    align-items: center;
    gap: 8px;
    min-width: 0;
  }
  .tag {
    font-size: 11px;
    color: var(--text-muted);
    background: var(--bg-elevated);
    border-radius: var(--radius-control);
    padding: 1px 6px;
  }
  .url {
    font-size: 12px;
    color: var(--text-muted);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .mono {
    font-family: var(--font-mono);
  }
  .actions {
    display: flex;
    justify-content: flex-end;
  }
  .btn {
    background: transparent;
    border: 1px solid var(--border);
    color: var(--text-muted);
    border-radius: var(--radius-control);
    padding: 4px 12px;
    font-size: 12px;
  }
  .btn:hover {
    color: var(--text-primary);
    border-color: var(--text-muted);
  }
</style>
