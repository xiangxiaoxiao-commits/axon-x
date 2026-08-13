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
    GetEmbeddingConfig,
    SetEmbeddingConfig,
    SetEmbeddingMode,
    TestEmbedding,
    MCPStatus,
    InstallMCP,
    UninstallMCP,
  } from "../../wailsjs/go/main/App.js";
  import { BrowserOpenURL } from "../../wailsjs/runtime/runtime.js";
  import type { main, provider, mcpinstall } from "../../wailsjs/go/models";
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

  // Embedding (semantic memory) selection. Empty provider = fall back to the
  // first OpenAI-compatible provider (backward compatible).
  let embedProvider = "";
  let embedModel = "";
  let embedMsg = "";
  let savingEmbed = false;
  let testingEmbed = false;
  let embedTestMsg = "";
  let embedTestOk = false;
  // Recall backend: "semantic" (cloud model only, no fallback) or "keyword"
  // (local lexical embedder). Explicit — no silent degradation.
  let embedMode: "semantic" | "keyword" = "keyword";
  let modeMsg = "";
  // Common embedding models across vendors (datalist suggestions; free-typed OK).
  const EMBED_MODELS = [
    "text-embedding-3-small",
    "text-embedding-3-large",
    "embedding-3",
    "BAAI/bge-m3",
  ];

  // --- MCP one-click install (Claude Code integration) ----------------------
  // Registers axon's stdio knowledge server into Claude Code's user config so
  // any agent session can query this project's business knowledge over MCP.
  let mcp: mcpinstall.Status | null = null;
  let mcpBusy = false;
  let mcpMsg = "";
  let mcpErr = "";

  async function refreshMCP() {
    try {
      mcp = await MCPStatus();
    } catch (e) {
      mcpErr = `读取 MCP 状态失败:${String(e)}`;
    }
  }

  async function installMCP() {
    mcpMsg = "";
    mcpErr = "";
    mcpBusy = true;
    try {
      mcp = await InstallMCP();
      mcpMsg = "已接入 Claude Code。重启正在运行的 Claude Code 会话后即可使用 axon-knowledge 工具。";
    } catch (e) {
      mcpErr = `接入失败:${String(e)}`;
    }
    mcpBusy = false;
  }

  async function uninstallMCP() {
    mcpMsg = "";
    mcpErr = "";
    mcpBusy = true;
    try {
      mcp = await UninstallMCP();
      mcpMsg = "已从 Claude Code 移除。";
    } catch (e) {
      mcpErr = `移除失败:${String(e)}`;
    }
    mcpBusy = false;
  }

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
    keyUrl?: string; // where to create/manage the API key
    keyHint?: string; // short "how to get the key" guidance
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
      keyUrl: "https://platform.openai.com/api-keys",
      keyHint: "登录 OpenAI 平台 → API keys → Create new secret key,复制 sk- 开头的串(只显示一次)。",
    },
    {
      id: "anthropic",
      label: "Anthropic (Claude)",
      desc: "官方 Claude 系列",
      name: "Anthropic",
      protocol: "anthropic",
      baseUrl: "https://api.anthropic.com/v1",
      models: ["claude-3-5-sonnet", "claude-3-5-haiku", "claude-3-opus"],
      keyUrl: "https://console.anthropic.com/settings/keys",
      keyHint: "登录 Anthropic Console → Settings → API keys → Create Key,复制 sk-ant- 开头的串。",
    },
    {
      id: "glm",
      label: "智谱 GLM",
      desc: "OpenAI 兼容协议",
      name: "GLM",
      protocol: "openai",
      baseUrl: "https://open.bigmodel.cn/api/paas/v4",
      models: ["glm-4.6", "glm-4-plus", "glm-4-flash"],
      note: "智谱 GLM 走 OpenAI 兼容协议(protocol=openai),填写方式与 OpenAI 相同,只是 Base URL 不同。",
      keyUrl: "https://open.bigmodel.cn/usercenter/apikeys",
      keyHint: "登录智谱开放平台 → 用户中心 → API Keys,复制密钥(形如 xxx.yyy)。",
    },
    {
      id: "custom",
      label: "自定义",
      desc: "手动填写",
      name: "",
      protocol: "openai",
      baseUrl: "",
      models: [],
      note: "自行填写名称、协议与 Base URL。若为 OpenAI 兼容服务(如公司网关),协议选 openai。",
      keyHint: "从你的服务商 / 内部网关控制台获取 API Key 与 Base URL。",
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
  // Embeddings must go over an OpenAI-compatible endpoint.
  $: embedProviders = providers.filter(
    (p) => p.protocol === "openai" && p.hasKey,
  );

  onMount(() => {
    load();
    refreshMCP();
  });

  async function load() {
    loading = true;
    loadError = "";
    try {
      providers = await ListProviders();
      if (!defaultProvider && configured.length) {
        defaultProvider = configured[0].name;
      }
      const embed = await GetEmbeddingConfig();
      embedProvider = embed.provider;
      embedModel = embed.model;
      embedMode = embed.mode === "semantic" ? "semantic" : "keyword";
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

  // Opens the add-provider form with a preset pre-selected and scrolls up to
  // it. Used by the Embedding section's shortcut buttons so a user missing an
  // OpenAI-compatible provider can add GLM/OpenAI in one click.
  function openAddWithPreset(presetId: string) {
    openAdd();
    const preset = PRESETS.find((p) => p.id === presetId);
    if (preset) applyPreset(preset);
    // Wait for the form to render, then bring it into view.
    setTimeout(() => {
      document
        .querySelector(".add-form")
        ?.scrollIntoView({ behavior: "smooth", block: "start" });
    }, 0);
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

  // Switch recall backend and persist immediately (toggle-style UX).
  async function setMode(mode: "semantic" | "keyword") {
    if (mode === embedMode) return;
    modeMsg = "";
    embedTestMsg = "";
    const prev = embedMode;
    embedMode = mode; // optimistic
    try {
      await SetEmbeddingMode(mode);
      modeMsg =
        mode === "semantic"
          ? "已切换到语义模型。若云端调用失败，召回不会降级，会直接报错——请用下方「测试连接」确认可用。"
          : "已切换到关键词（本地词面向量），完全离线，不调用云端。";
    } catch (e) {
      embedMode = prev; // revert on failure
      modeMsg = `切换失败:${String(e)}`;
    }
  }

  async function saveEmbedding() {
    embedMsg = "";
    embedTestMsg = "";
    savingEmbed = true;
    try {
      await SetEmbeddingConfig(embedProvider, embedModel.trim());
      embedMsg = embedProvider
        ? "已保存 Embedding 配置"
        : "已清除,语义检索回退为默认 Provider";
    } catch (e) {
      embedMsg = `保存失败:${String(e)}`;
    }
    savingEmbed = false;
  }

  async function testEmbedding() {
    embedTestMsg = "";
    embedMsg = "";
    testingEmbed = true;
    try {
      // Persist current selection first so the test uses what the user sees.
      await SetEmbeddingConfig(embedProvider, embedModel.trim());
      await TestEmbedding();
      embedTestOk = true;
      embedTestMsg = "连接成功,Embedding 可用";
    } catch (e) {
      embedTestOk = false;
      embedTestMsg = `测试失败:${String(e)}`;
    }
    testingEmbed = false;
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

      <!-- MCP one-click install: the product's headline integration. Wires
           axon's knowledge graph into Claude Code so any agent session can
           query this project's business knowledge. -->
      <section>
        <h2>接入 Claude Code(MCP)</h2>
        <div class="mcp-card">
          <div class="mcp-desc">
            一键把 axon 的知识图谱接入 Claude Code。接入后,AI 在任意会话里都能通过
            <code>axon-knowledge</code> 工具查询本项目沉淀的业务知识(设计决策、约束、接口约定),
            无需你反复解释。
          </div>

          <div class="mcp-status">
            <span
              class="dot"
              class:on={mcp?.installed}
              class:off={!mcp?.installed}
            ></span>
            {#if mcp?.installed}
              <span class="mcp-state">已接入</span>
              <span class="mcp-path mono">{mcp.command}</span>
            {:else}
              <span class="mcp-state">未接入</span>
            {/if}
          </div>

          <div class="mcp-actions">
            {#if mcp?.installed}
              <button class="btn primary" type="button" on:click={installMCP} disabled={mcpBusy}>
                {mcpBusy ? "处理中…" : "重新接入 / 更新路径"}
              </button>
              <button class="btn ghost" type="button" on:click={uninstallMCP} disabled={mcpBusy}>
                移除接入
              </button>
            {:else}
              <button class="btn primary" type="button" on:click={installMCP} disabled={mcpBusy}>
                {mcpBusy ? "接入中…" : "一键接入 Claude Code"}
              </button>
            {/if}
          </div>

          {#if mcpMsg}<p class="mcp-ok">{mcpMsg}</p>{/if}
          {#if mcpErr}<p class="mcp-err">{mcpErr}</p>{/if}
        </div>
      </section>

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

            {#if activePreset?.keyHint}
              <div class="row">
                <span class="label">怎么拿 Key</span>
                <div class="key-help">
                  <span>{activePreset.keyHint}</span>
                  {#if activePreset.keyUrl}
                    <button
                      type="button"
                      class="link-btn"
                      on:click={() => activePreset?.keyUrl && BrowserOpenURL(activePreset.keyUrl)}
                    >
                      打开申请页 ↗
                    </button>
                  {/if}
                </div>
              </div>
            {/if}

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

      <!-- Embedding (semantic memory) config -->
      <section>
        <h2>召回方式</h2>

        <!-- Explicit recall backend switch: no silent degradation. -->
        <div class="mode-switch">
          <button
            type="button"
            class="mode-opt"
            class:active={embedMode === "keyword"}
            on:click={() => setMode("keyword")}
          >
            <span class="mode-title">🔤 关键词</span>
            <span class="mode-sub">本地词面向量,完全离线,不调用云端。速度快、零成本,但只按字面近似召回。</span>
          </button>
          <button
            type="button"
            class="mode-opt"
            class:active={embedMode === "semantic"}
            on:click={() => setMode("semantic")}
          >
            <span class="mode-title">🧠 语义模型</span>
            <span class="mode-sub">只用下方配置的云端 embedding 模型。按意思召回,精度高。<strong>失败不降级</strong>,会直接报错。</span>
          </button>
        </div>
        {#if modeMsg}<p class="mode-msg">{modeMsg}</p>{/if}

        {#if embedMode === "semantic"}
        <h2 class="sub-h2">Embedding 模型配置</h2>
        <div class="note" class:warn={!hasOpenAI}>
          语义模型走 OpenAI 兼容接口,所以这里只列 <strong>openai 协议</strong>的 Provider——
          <strong>智谱 GLM 也是 openai 协议</strong>,同样可用(模型填
          <code>embedding-3</code>);OpenAI 用 <code>text-embedding-3-small</code>。
        </div>

        {#if embedProviders.length === 0}
          <div class="note warn empty-embed">
            <div>还没有 openai 协议的 Provider,语义检索暂不可用。点下面按钮一键添加(会自动填好协议和地址,你只需粘贴 API Key):</div>
            <div class="empty-actions">
              <button class="btn" type="button" on:click={() => openAddWithPreset("glm")}>
                + 添加智谱 GLM
              </button>
              <button class="btn" type="button" on:click={() => openAddWithPreset("openai")}>
                + 添加 OpenAI
              </button>
            </div>
          </div>
        {:else}
          <div class="defaults">
            <div class="row">
              <label for="embed-prov">Embedding Provider</label>
              <select id="embed-prov" class="field" bind:value={embedProvider}>
                <option value="">(默认:第一个 openai Provider)</option>
                {#each embedProviders as p (p.name)}
                  <option value={p.name}>{p.name}</option>
                {/each}
              </select>
            </div>
            <div class="row">
              <label for="embed-model">Embedding 模型</label>
              <input
                id="embed-model"
                class="field mono"
                placeholder="text-embedding-3-small"
                list="embed-model-suggestions"
                bind:value={embedModel}
              />
              <datalist id="embed-model-suggestions">
                {#each EMBED_MODELS as m (m)}
                  <option value={m}></option>
                {/each}
              </datalist>
            </div>
            <div class="row actions-row">
              <button
                class="btn primary"
                type="button"
                on:click={saveEmbedding}
                disabled={savingEmbed}
              >
                {savingEmbed ? "保存中…" : "保存 Embedding 配置"}
              </button>
              <button
                class="btn"
                type="button"
                on:click={testEmbedding}
                disabled={testingEmbed}
              >
                {testingEmbed ? "测试中…" : "测试连接"}
              </button>
              {#if embedMsg}<span class="muted">{embedMsg}</span>{/if}
              {#if embedTestMsg}
                <span class:ok={embedTestOk} class:err={!embedTestOk}>
                  {embedTestMsg}
                </span>
              {/if}
            </div>
          </div>
        {/if}
        {/if}
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
  .ok {
    color: var(--accent);
    font-size: 13px;
  }
  .err {
    color: var(--danger);
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

  /* MCP install card */
  .mcp-card {
    display: flex;
    flex-direction: column;
    gap: 12px;
    background: var(--bg-surface);
    border: 1px solid var(--border);
    border-radius: var(--radius-card);
    padding: 16px;
  }
  .mcp-desc {
    font-size: 13px;
    color: var(--text-muted);
    line-height: 1.7;
  }
  .mcp-desc code {
    color: var(--text-primary);
    background: var(--bg-elevated);
    padding: 1px 5px;
    border-radius: 4px;
    font-size: 12px;
  }
  .mcp-status {
    display: flex;
    align-items: center;
    gap: 8px;
    font-size: 13px;
  }
  .dot {
    width: 8px;
    height: 8px;
    border-radius: 50%;
    flex: 0 0 8px;
  }
  .dot.on {
    background: var(--accent);
  }
  .dot.off {
    background: var(--text-muted);
  }
  .mcp-state {
    color: var(--text-primary);
    font-weight: 500;
  }
  .mcp-path {
    color: var(--text-muted);
    font-size: 12px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .mcp-actions {
    display: flex;
    gap: 8px;
  }
  .mcp-ok {
    margin: 0;
    color: var(--accent);
    font-size: 13px;
    line-height: 1.6;
  }
  .mcp-err {
    margin: 0;
    color: var(--danger);
    font-size: 13px;
  }

  /* Embedding empty-state: shortcut buttons to add a compatible provider */
  .empty-embed {
    display: flex;
    flex-direction: column;
    gap: 10px;
    line-height: 1.7;
  }
  .empty-actions {
    display: flex;
    gap: 8px;
    flex-wrap: wrap;
  }

  /* "How to get the key" hint under the API Key field */
  .key-help {
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    gap: 6px;
    font-size: 12px;
    color: var(--text-muted);
    line-height: 1.6;
  }
  .link-btn {
    background: transparent;
    border: none;
    padding: 0;
    color: var(--accent);
    font-size: 12px;
    cursor: pointer;
  }
  .link-btn:hover {
    text-decoration: underline;
  }

  /* Recall backend switch */
  .mode-switch {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: 10px;
  }
  .mode-opt {
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    gap: 6px;
    text-align: left;
    background: var(--bg-surface);
    border: 1px solid var(--border);
    border-radius: var(--radius-card);
    padding: 14px 16px;
    cursor: pointer;
  }
  .mode-opt:hover {
    border-color: var(--text-muted);
  }
  .mode-opt.active {
    border-color: var(--accent);
    background: color-mix(in srgb, var(--accent) 10%, transparent);
    box-shadow: inset 0 0 0 1px var(--accent);
  }
  .mode-title {
    font-size: 14px;
    font-weight: 600;
    color: var(--text-primary);
  }
  .mode-sub {
    font-size: 12px;
    color: var(--text-muted);
    line-height: 1.6;
  }
  .mode-msg {
    margin: 10px 0 0;
    font-size: 12.5px;
    color: var(--text-muted);
    line-height: 1.6;
  }
  .sub-h2 {
    font-size: 14px;
    margin: 20px 0 12px;
  }
</style>
