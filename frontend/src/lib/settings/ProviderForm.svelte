<script lang="ts">
  // Add / edit a provider. Emits `save` with the config + (possibly empty) key.
  // An empty apiKey means "keep the stored key untouched" (backend contract).
  import { createEventDispatcher } from "svelte";
  import type { main, provider } from "../../../wailsjs/go/models";
  import SecretInput from "./SecretInput.svelte";

  // When editing an existing provider, pass it in; null means a fresh add.
  export let existing: main.ProviderInfo | null = null;

  const dispatch = createEventDispatcher<{
    save: { config: provider.Config; apiKey: string };
    cancel: void;
  }>();

  const editing = existing !== null;

  let name = existing?.name ?? "";
  let protocol = existing?.protocol ?? "openai";
  // baseURL convention includes /v1.
  let baseUrl =
    existing?.baseUrl ??
    (protocol === "anthropic"
      ? "https://api.anthropic.com/v1"
      : "https://api.openai.com/v1");
  let keyRef = existing?.keyRef ?? "";
  let apiKey = "";
  let error = "";
  let saving = false;

  // Suggest a sensible default baseURL when protocol changes on a fresh add.
  function onProtocolChange() {
    if (editing) return;
    if (protocol === "anthropic") baseUrl = "https://api.anthropic.com/v1";
    else baseUrl = "https://api.openai.com/v1";
  }

  async function submit() {
    error = "";
    const trimmedName = name.trim();
    const trimmedUrl = baseUrl.trim();
    if (!trimmedName) {
      error = "请填写 Provider 名称";
      return;
    }
    if (!trimmedUrl) {
      error = "请填写 Base URL";
      return;
    }
    // A brand-new provider needs a key to be usable.
    if (!editing && !apiKey.trim()) {
      error = "首次配置需要填写 API Key";
      return;
    }
    saving = true;
    const config = {
      name: trimmedName,
      protocol,
      baseUrl: trimmedUrl,
      keyRef,
    } as provider.Config;
    dispatch("save", { config, apiKey: apiKey.trim() });
  }
</script>

<div class="form">
  <div class="row">
    <label for="pf-name">名称</label>
    <input
      id="pf-name"
      class="field"
      placeholder="如 OpenAI / Anthropic / 自定义"
      bind:value={name}
      disabled={editing}
    />
  </div>

  <div class="row">
    <label for="pf-proto">协议</label>
    <select id="pf-proto" class="field" bind:value={protocol} on:change={onProtocolChange}>
      <option value="openai">openai (OpenAI-compatible)</option>
      <option value="anthropic">anthropic</option>
    </select>
  </div>

  <div class="row">
    <label for="pf-url">Base URL</label>
    <input
      id="pf-url"
      class="field mono"
      placeholder="https://api.openai.com/v1"
      bind:value={baseUrl}
    />
  </div>

  <div class="row">
    <label for="pf-key">API Key</label>
    <SecretInput bind:value={apiKey} hasKey={existing?.hasKey ?? false} />
  </div>

  <p class="lock">🔒 密钥存储于 macOS Keychain,不写入配置文件</p>

  {#if error}
    <p class="error">{error}</p>
  {/if}

  <div class="actions">
    <button class="btn ghost" type="button" on:click={() => dispatch("cancel")}>取消</button>
    <button class="btn primary" type="button" on:click={submit} disabled={saving}>
      {saving ? "保存中…" : editing ? "更新" : "保存"}
    </button>
  </div>
</div>

<style>
  .form {
    display: flex;
    flex-direction: column;
    gap: 12px;
    background: var(--bg-surface);
    border: 1px solid var(--border);
    border-radius: var(--radius-card);
    padding: 16px;
  }
  .row {
    display: grid;
    grid-template-columns: 80px 1fr;
    align-items: center;
    gap: 12px;
  }
  label {
    color: var(--text-muted);
    font-size: 13px;
  }
  .field {
    width: 100%;
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
  .field:disabled {
    opacity: 0.6;
  }
  .mono {
    font-family: var(--font-mono);
  }
  .lock {
    margin: 0;
    color: var(--text-muted);
    font-size: 12px;
  }
  .error {
    margin: 0;
    color: var(--danger);
    font-size: 13px;
  }
  .actions {
    display: flex;
    justify-content: flex-end;
    gap: 8px;
  }
  .btn {
    border-radius: var(--radius-control);
    padding: 6px 14px;
    font-size: 13px;
    border: 1px solid var(--border);
  }
  .btn.ghost {
    background: transparent;
    color: var(--text-muted);
  }
  .btn.ghost:hover {
    color: var(--text-primary);
    border-color: var(--text-muted);
  }
  .btn.primary {
    background: var(--accent);
    color: var(--accent-fg);
    border-color: var(--accent);
    font-weight: 500;
  }
  .btn.primary:hover {
    filter: brightness(1.1);
  }
  .btn.primary:disabled {
    opacity: 0.6;
  }
</style>
