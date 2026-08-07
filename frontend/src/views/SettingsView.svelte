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
    RoutingTable,
  } from "../../wailsjs/go/main/App.js";
  import type { main, provider, routing } from "../../wailsjs/go/models";
  import ProviderCard from "../lib/settings/ProviderCard.svelte";
  import ProviderForm from "../lib/settings/ProviderForm.svelte";

  // Parent re-checks provider/key presence after a successful save (first-run gate).
  export let onSaved: () => void = () => {};

  let providers: main.ProviderInfo[] = [];
  let table: routing.Table | null = null;
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
    try {
      table = await RoutingTable();
    } catch (e) {
      // Routing table is informational; don't block the page on it.
      console.error("load routing table", e);
    }
    loading = false;
  }

  function openAdd() {
    saveError = "";
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

  function fmtCost(v: number): string {
    if (!v && v !== 0) return "—";
    return `$${v.toFixed(2)}`;
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

        {#if formTarget !== null}
          <ProviderForm
            existing={formTarget === "new" ? null : formTarget}
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
              bind:value={defaultModel}
              disabled={needsSetup}
            />
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

      <!-- Routing table (read-only) -->
      <section>
        <h2>路由规则</h2>
        <p class="muted small">任务类型 → 模型映射(只读,来自 routing.json)。</p>
        {#if table && table.order && table.order.length}
          <div class="table">
            <div class="tr th">
              <span>任务类型</span>
              <span>主模型</span>
              <span class="num">IQ</span>
              <span class="num">成本</span>
              <span class="num">耗时</span>
            </div>
            {#each table.order as key (key)}
              {#if table.profiles[key]}
                <div class="tr">
                  <span>{table.profiles[key].title || key}</span>
                  <span class="mono">{table.profiles[key].primary?.model || "—"}</span>
                  <span class="num mono">{table.profiles[key].primary?.iq ?? "—"}</span>
                  <span class="num mono">{fmtCost(table.profiles[key].primary?.costUsd)}</span>
                  <span class="num mono">
                    {table.profiles[key].primary?.minutes
                      ? `~${table.profiles[key].primary.minutes}m`
                      : "—"}
                  </span>
                </div>
              {/if}
            {/each}
          </div>
        {:else}
          <p class="muted">暂无路由数据。</p>
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
  .small {
    margin: 0 0 12px;
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
  .table {
    border: 1px solid var(--border);
    border-radius: var(--radius-card);
    overflow: hidden;
  }
  .tr {
    display: grid;
    grid-template-columns: 1.4fr 1.6fr 0.6fr 0.7fr 0.7fr;
    gap: 8px;
    padding: 8px 12px;
    font-size: 13px;
    border-bottom: 1px solid var(--border);
  }
  .tr:last-child {
    border-bottom: none;
  }
  .tr.th {
    background: var(--bg-elevated);
    color: var(--text-muted);
    font-size: 12px;
  }
  .num {
    text-align: right;
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
</style>
