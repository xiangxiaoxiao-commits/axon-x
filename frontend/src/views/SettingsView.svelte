<script lang="ts">
  // Settings view (UX §3.4). First-run gate lands here when no provider has a
  // usable key. Sections: first-run guidance, Providers, Defaults, Routing table,
  // Embedding note. Keys are written only via the Go backend (Keychain); the
  // frontend never persists plaintext.
  import { onMount } from "svelte";
  import {
    ListProviders,
    SaveProvider,
    SetDefaults,
  } from "../../wailsjs/go/main/App.js";
  import type { main, provider } from "../../wailsjs/go/models";
  import ProviderCard from "../lib/settings/ProviderCard.svelte";
  import ProviderForm from "../lib/settings/ProviderForm.svelte";
  import SecretInput from "../lib/settings/SecretInput.svelte";

  // Parent re-checks provider/key presence after a successful save (first-run gate).
  export let onSaved: () => void = () => {};

  let providers: main.ProviderInfo[] = [];
  let loading = true;
  let loadError = "";

  // Provider form state: null = closed, "new" = add, otherwise editing that info.
  let formTarget: main.ProviderInfo | "new" | null = null;
  let saveError = "";

  // Defaults selection.
  let defaultProvider = "";
  let defaultModel = "";
  let defaultsMsg = "";
  let savingDefaults = false;

  // --- One-click provider presets (add flow only) ---------------------------
  // Each preset prefills protocol + baseURL so the user only pastes a key.
  // GLM is OpenAI-compatible, so protocol stays "openai" and SaveProvider needs
  // zero changes. "custom" keeps the manual entry path.
  type Preset = {
    id: string;
    label: string;
    desc: string;
    name: string; // suggested provider name
    protocol: string;
    baseUrl: string;
    models: string[]; // suggested model names (reference / datalist)
    note?: string;
  };

  const PRESETS: Preset[] = [
    {
      id: "openai",
      label: "OpenAI (GPT)",
      desc: "官方 GPT 系列",
      name: "OpenAI",
      protocol: "openai",
      baseUrl: "https://api.openai.com/v1",
      models: ["gpt-4o", "gpt-4o-mini", "gpt-4-turbo"],
    },
    {
      id: "anthropic",
      label: "Anthropic (Claude)",
      desc: "官方 Claude 系列",
      name: "Anthropic",
      protocol: "anthropic",
      baseUrl: "https://api.anthropic.com/v1",
      models: ["claude-3-5-sonnet", "claude-3-5-haiku", "claude-3-opus"],
    },
    {
      id: "glm",
      label: "智谱 GLM",
      desc: "OpenAI 兼容协议",
      name: "GLM",
      protocol: "openai",
      baseUrl: "https://open.bigmodel.cn/api/paas/v4",
      models: ["glm-4.6", "glm-4-plus", "glm-4-flash"],
      note: "智谱 GLM 走 OpenAI 兼容协议(protocol=openai),因此填写方式与 OpenAI 相同,只是 Base URL 不同。",
    },
    {
      id: "custom",
      label: "自定义",
      desc: "手动填写",
      name: "",
      protocol: "openai",
      baseUrl: "",
      models: [],
      note: "自行填写名称、协议与 Base URL。若为 OpenAI 兼容服务,协议选 openai。",
    },
  ];

  // Inline add-form state (edit flow still uses ProviderForm to avoid duplication).
  let selectedPreset = "";
  let newName = "";
  let newProtocol = "openai";
  let newBaseUrl = "";
  let newApiKey = "";
  let addError = "";
  let savingNew = false;

  $: activePreset = PRESETS.find((p) => p.id === selectedPreset) ?? null;
  // Unique suggested models across presets, for the default-model datalist.
  $: suggestedModels = Array.from(
    new Set(PRESETS.flatMap((p) => p.models)),
  );

  function resetAddForm() {
    selectedPreset = "";
    newName = "";
    newProtocol = "openai";
    newBaseUrl = "";
    newApiKey = "";
    addError = "";
    savingNew = false;
  }

  function applyPreset(preset: Preset) {
    addError = "";
    selectedPreset = preset.id;
    newProtocol = preset.protocol;
    newBaseUrl = preset.baseUrl;
    // Only overwrite the name for concrete presets; keep whatever the user typed
    // when switching to "custom".
    if (preset.id !== "custom") newName = preset.name;
  }

  async function saveNewProvider() {
    addError = "";
    const trimmedName = newName.trim();
    const trimmedUrl = newBaseUrl.trim();
    if (!trimmedName) {
      addError = "请填写 Provider 名称";
      return;
    }
    if (!trimmedUrl) {
      addError = "请填写 Base URL";
      return;
    }
    if (!newApiKey.trim()) {
      addError = "首次配置需要填写 API Key";
      return;
    }
    savingNew = true;
    try {
      const config = {
        name: trimmedName,
        protocol: newProtocol,
        baseUrl: trimmedUrl,
        keyRef: "",
      } as provider.Config;
      await SaveProvider(config, newApiKey.trim());
      formTarget = null;
      resetAddForm();
      await load();
      onSaved();
    } catch (err) {
      addError = `保存失败:${String(err)}`;
      savingNew = false;
    }
  }

  $: configured = providers.filter((p) => p.hasKey);
  $: needsSetup = configured.length === 0;
  // Semantic memory needs an OpenAI-compatible provider for embeddings.
  $: hasOpenAI = providers.some((p) => p.protocol === "openai" && p.hasKey);

  onMount(load);

  async function load() {
    loading = true;
    loadError = "";
    try {
      providers = await ListProviders();
      if (!defaultProvider && configured.length) {
        defaultProvider = configured[0].name;
      }
    } catch (e) {
      loadError = `加载 Providers 失败:${String(e)}`;
    }
    loading = false;
  }

  function openAdd() {
    saveError = "";
    resetAddForm();
    formTarget = "new";
  }

  function openEdit(info: main.ProviderInfo) {
    saveError = "";
    formTarget = info;
  }

  async function onFormSave(e: CustomEvent<{ config: provider.Config; apiKey: string }>) {
    saveError = "";
    try {
      await SaveProvider(e.detail.config, e.detail.apiKey);
      formTarget = null;
      await load();
      // Release the first-run gate if a usable provider now exists.
      onSaved();
    } catch (err) {
      saveError = `保存失败:${String(err)}`;
    }
  }

  async function saveDefaults() {
    defaultsMsg = "";
    if (!defaultProvider) {
      defaultsMsg = "请先选择默认 Provider";
      return;
    }
    if (!defaultModel.trim()) {
      defaultsMsg = "请填写默认模型";
      return;
    }
    savingDefaults = true;
    try {
      await SetDefaults(defaultProvider, defaultModel.trim());
      defaultsMsg = "已保存默认设置";
    } catch (e) {
      defaultsMsg = `保存失败:${String(e)}`;
    }
    savingDefaults = false;
  }
</script>

<div class="settings">
  <div class="inner">
    <h1>设置</h1>

    {#if loading}
      <p class="muted">加载中…</p>
    {:else}
      {#if loadError}
        <div class="banner error">{loadError}</div>
      {/if}

      <!-- First-run guidance (UX §5.1). -->
      {#if needsSetup}
        <div class="banner guide">
          <div class="guide-title">◇ 先连接一个模型来开始对话</div>
          <div class="guide-sub">
            配置一个 Provider 的 API Key 后即可开始。密钥安全存储于 macOS Keychain。
          </div>
          {#if formTarget === null}
            <button class="btn primary" type="button" on:click={openAdd}>配置 API Key →</button>
          {/if}
        </div>
      {/if}

      <!-- Providers -->
      <section>
        <div class="sec-head">
          <h2>Providers</h2>
          {#if formTarget === null}
            <button class="btn" type="button" on:click={openAdd}>+ 添加 Provider</button>
          {/if}
        </div>

        {#if saveError}
          <div class="banner error">{saveError}</div>
        {/if}

        {#if formTarget === "new"}
          <div class="add-form">
            <div class="preset-head">选择一个预设,自动填好协议和 Base URL,再补 Key 即可</div>
            <div class="presets">
              {#each PRESETS as preset (preset.id)}
                <button
                  type="button"
                  class="preset"
                  class:active={selectedPreset === preset.id}
                  on:click={() => applyPreset(preset)}
                >
                  <span class="preset-label">{preset.label}</span>
                  <span class="preset-desc">{preset.desc}</span>
                </button>
              {/each}
            </div>

            {#if activePreset?.note}
              <div class="preset-note">{activePreset.note}</div>
            {/if}

            <div class="row">
              <label for="add-name">名称</label>
              <input
                id="add-name"
                class="field"
                placeholder="如 OpenAI / Claude / GLM"
                bind:value={newName}
              />
            </div>

            <div class="row">
              <label for="add-proto">协议</label>
              <select id="add-proto" class="field" bind:value={newProtocol}>
                <option value="openai">openai (OpenAI-compatible)</option>
                <option value="anthropic">anthropic</option>
              </select>
            </div>

            <div class="row">
              <label for="add-url">Base URL</label>
              <input
                id="add-url"
                class="field mono"
                placeholder="https://api.openai.com/v1"
                bind:value={newBaseUrl}
              />
            </div>

            <div class="row">
              <label for="add-key">API Key</label>
              <SecretInput bind:value={newApiKey} hasKey={false} />
            </div>

            {#if activePreset && activePreset.models.length}
              <div class="row">
                <span class="label">建议模型</span>
                <div class="model-chips">
                  {#each activePreset.models as m (m)}
                    <span class="chip mono">{m}</span>
                  {/each}
                  <span class="chip-hint">保存后可在下方「默认设置」填入</span>
                </div>
              </div>
            {/if}

            <p class="lock">🔒 密钥存储于 macOS Keychain,不写入配置文件</p>

            {#if addError}
              <p class="add-error">{addError}</p>
            {/if}

            <div class="add-actions">
              <button
                class="btn ghost"
                type="button"
                on:click={() => {
                  formTarget = null;
                  resetAddForm();
                }}>取消</button
              >
              <button
                class="btn primary"
                type="button"
                on:click={saveNewProvider}
                disabled={savingNew}
              >
                {savingNew ? "保存中…" : "保存"}
              </button>
            </div>
          </div>
        {:else if formTarget !== null}
          <ProviderForm
            existing={formTarget}
            on:save={onFormSave}
            on:cancel={() => (formTarget = null)}
          />
        {/if}

        {#if providers.length === 0 && formTarget === null}
          <p class="muted">还没有配置任何 Provider。</p>
        {:else}
          <div class="cards">
            {#each providers as p (p.name)}
              <ProviderCard info={p} on:edit={() => openEdit(p)} />
            {/each}
          </div>
        {/if}
      </section>

      <!-- Defaults -->
      <section>
        <h2>默认设置</h2>
        <div class="defaults">
          <div class="row">
            <label for="def-prov">默认 Provider</label>
            <select id="def-prov" class="field" bind:value={defaultProvider} disabled={needsSetup}>
              {#if needsSetup}
                <option value="">先配置一个 Provider</option>
              {/if}
              {#each configured as p (p.name)}
                <option value={p.name}>{p.name}</option>
              {/each}
            </select>
          </div>
          <div class="row">
            <label for="def-model">默认模型</label>
            <input
              id="def-model"
              class="field mono"
              placeholder="如 gpt-5.6-sol"
              list="model-suggestions"
              bind:value={defaultModel}
              disabled={needsSetup}
            />
            <datalist id="model-suggestions">
              {#each suggestedModels as m (m)}
                <option value={m}></option>
              {/each}
            </datalist>
          </div>
          <div class="row actions-row">
            <button
              class="btn primary"
              type="button"
              on:click={saveDefaults}
              disabled={needsSetup || savingDefaults}
            >
              {savingDefaults ? "保存中…" : "保存默认设置"}
            </button>
            {#if defaultsMsg}<span class="muted">{defaultsMsg}</span>{/if}
          </div>
        </div>
      </section>

      <!-- Embedding note -->
      <section>
        <h2>语义记忆(Embedding)</h2>
        <div class="note" class:warn={!hasOpenAI}>
          语义记忆检索使用 OpenAI-compatible Provider 的
          <code>text-embedding-3-small</code> 生成向量。
          {#if hasOpenAI}
            已检测到可用的 openai 协议 Provider。
          {:else}
            当前尚未配置 openai 协议的 Provider,语义记忆将不可用。请添加一个 protocol 为 openai 的 Provider。
          {/if}
        </div>
      </section>

    {/if}
  </div>
</div>

<style>
  .settings {
    height: 100%;
    overflow-y: auto;
    padding: 24px;
  }
  .inner {
    max-width: var(--read-max);
    margin: 0 auto;
    display: flex;
    flex-direction: column;
    gap: 28px;
  }
  h1 {
    font-size: 20px;
    margin: 0;
  }
  h2 {
    font-size: 15px;
    margin: 0 0 12px;
  }
  section {
    display: flex;
    flex-direction: column;
  }
  .sec-head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    margin-bottom: 12px;
  }
  .sec-head h2 {
    margin: 0;
  }
  .cards {
    display: flex;
    flex-direction: column;
    gap: 10px;
    margin-top: 12px;
  }
  .muted {
    color: var(--text-muted);
    font-size: 13px;
  }
  .banner {
    border-radius: var(--radius-card);
    padding: 12px 14px;
    font-size: 13px;
    margin-bottom: 12px;
  }
  .banner.error {
    background: color-mix(in srgb, var(--danger) 12%, transparent);
    border: 1px solid var(--danger);
    color: var(--danger);
  }
  .banner.guide {
    background: var(--bg-surface);
    border: 1px solid var(--accent);
    display: flex;
    flex-direction: column;
    gap: 8px;
    align-items: flex-start;
  }
  .guide-title {
    font-size: 15px;
    font-weight: 600;
  }
  .guide-sub {
    color: var(--text-muted);
  }
  .defaults {
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
    grid-template-columns: 100px 1fr;
    align-items: center;
    gap: 12px;
  }
  .actions-row {
    grid-template-columns: 1fr;
    display: flex;
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
  .note {
    background: var(--bg-surface);
    border: 1px solid var(--border);
    border-radius: var(--radius-card);
    padding: 12px 14px;
    font-size: 13px;
    color: var(--text-muted);
    line-height: 1.7;
  }
  .note code {
    color: var(--text-primary);
    background: var(--bg-elevated);
    padding: 1px 5px;
    border-radius: 4px;
    font-size: 12px;
  }
  .note.warn {
    border-color: var(--warning);
  }
  .btn {
    background: transparent;
    border: 1px solid var(--border);
    color: var(--text-muted);
    border-radius: var(--radius-control);
    padding: 6px 14px;
    font-size: 13px;
  }
  .btn:hover {
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
  .btn.ghost {
    background: transparent;
    color: var(--text-muted);
  }
  .btn.ghost:hover {
    color: var(--text-primary);
    border-color: var(--text-muted);
  }

  /* Add-provider form + presets */
  .add-form {
    display: flex;
    flex-direction: column;
    gap: 12px;
    background: var(--bg-surface);
    border: 1px solid var(--border);
    border-radius: var(--radius-card);
    padding: 16px;
  }
  .preset-head {
    color: var(--text-muted);
    font-size: 13px;
  }
  .presets {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(150px, 1fr));
    gap: 10px;
  }
  .preset {
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    gap: 4px;
    text-align: left;
    background: var(--bg-base);
    border: 1px solid var(--border);
    border-radius: var(--radius-card);
    padding: 10px 12px;
    cursor: pointer;
  }
  .preset:hover {
    border-color: var(--text-muted);
  }
  .preset.active {
    border-color: var(--accent);
    background: color-mix(in srgb, var(--accent) 10%, transparent);
  }
  .preset-label {
    font-size: 13px;
    font-weight: 500;
    color: var(--text-primary);
  }
  .preset-desc {
    font-size: 12px;
    color: var(--text-muted);
  }
  .preset-note {
    background: var(--bg-elevated);
    border: 1px solid var(--border);
    border-left: 3px solid var(--accent);
    border-radius: var(--radius-control);
    padding: 8px 12px;
    font-size: 12px;
    color: var(--text-muted);
    line-height: 1.6;
  }
  .add-form .row {
    grid-template-columns: 80px 1fr;
  }
  .add-form .label {
    color: var(--text-muted);
    font-size: 13px;
  }
  .model-chips {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 6px;
  }
  .chip {
    font-size: 12px;
    color: var(--text-primary);
    background: var(--bg-elevated);
    border: 1px solid var(--border);
    border-radius: var(--radius-control);
    padding: 2px 8px;
  }
  .chip-hint {
    font-size: 12px;
    color: var(--text-muted);
  }
  .lock {
    margin: 0;
    color: var(--text-muted);
    font-size: 12px;
  }
  .add-error {
    margin: 0;
    color: var(--danger);
    font-size: 13px;
  }
  .add-actions {
    display: flex;
    justify-content: flex-end;
    gap: 8px;
  }
</style>
