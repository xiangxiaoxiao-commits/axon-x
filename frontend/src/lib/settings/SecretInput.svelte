<script lang="ts">
  // Masked API-key input. The plaintext key is never persisted in the frontend;
  // it is only held transiently in `value` until the parent calls SaveProvider.
  // Default type=password (脱敏); [显示] toggles a brief plaintext reveal.
  export let value = "";
  // Whether the backend already has a stored key for this provider.
  export let hasKey = false;

  let revealed = false;

  // Placeholder communicates "leave blank to keep the stored key".
  $: placeholder = hasKey ? "已配置,留空则不改动" : "粘贴 API Key,如 sk-…";
</script>

<div class="secret">
  <input
    class="field mono"
    type={revealed ? "text" : "password"}
    autocomplete="off"
    spellcheck="false"
    {placeholder}
    bind:value
  />
  <button
    type="button"
    class="toggle"
    title={revealed ? "隐藏" : "显示"}
    on:click={() => (revealed = !revealed)}
  >
    {revealed ? "隐藏" : "显示"}
  </button>
</div>

<style>
  .secret {
    display: flex;
    gap: 6px;
    align-items: center;
  }
  .field {
    flex: 1;
    min-width: 0;
    background: var(--bg-base);
    border: 1px solid var(--border);
    color: var(--text-primary);
    border-radius: var(--radius-control);
    padding: 6px 10px;
    font-size: 13px;
  }
  .field:focus {
    outline: none;
    border-color: var(--accent);
  }
  .mono {
    font-family: var(--font-mono);
  }
  .toggle {
    flex: 0 0 auto;
    background: var(--bg-elevated);
    border: 1px solid var(--border);
    color: var(--text-muted);
    border-radius: var(--radius-control);
    padding: 6px 10px;
    font-size: 12px;
  }
  .toggle:hover {
    color: var(--text-primary);
    border-color: var(--text-muted);
  }
</style>
