<script lang="ts">
  // Chat view: two panes (session list + message stream/input) with streaming.
  import { onMount, onDestroy, tick } from "svelte";
  import { conversations, currentConversationID, messages, streaming } from "../lib/stores";
  import type { model, main } from "../../wailsjs/go/models";
  import {
    ListConversations,
    ListMessages,
    NewConversation,
    DeleteConversation,
    RenameConversation,
    SendMessage,
    StopGeneration,
    ClassifyTask,
    Recommend,
    ListProviders,
    ListModels,
  } from "../../wailsjs/go/main/App.js";
  import { EventsOn, EventsOff } from "../../wailsjs/runtime/runtime.js";
  import SessionList from "../lib/chat/SessionList.svelte";
  import MessageBubble from "../lib/chat/MessageBubble.svelte";
  import MessageInput from "../lib/chat/MessageInput.svelte";
  import RecommendationBar from "../lib/chat/RecommendationBar.svelte";

  // Id of the assistant message currently streaming (-1 = none).
  let streamingMsgID = -1;
  let errorText = "";
  let scroller: HTMLElement;
  let input: MessageInput;

  // Model selection: which provider + model this chat uses.
  let providerName = "";
  let currentModel = "";
  let models: string[] = [];
  let modelsLoading = false;

  async function loadModels() {
    try {
      const provs = await ListProviders();
      const usable = provs.find((p) => p.hasKey) ?? provs[0];
      if (!usable) return;
      providerName = usable.name;
      modelsLoading = true;
      models = await ListModels(providerName);
      if (models.length && !models.includes(currentModel)) currentModel = models[0];
    } catch (e) {
      console.error("load models", e);
    } finally {
      modelsLoading = false;
    }
  }

  // --- Streaming event payloads (from the Go backend). ---
  type DeltaEvt = { conversationId: string; messageId: number; delta: string };
  type DoneEvt = {
    conversationId: string;
    messageId: number;
    promptTokens: number;
    completionTokens: number;
  };
  type ErrorEvt = { conversationId: string; messageId: number; error: string };

  function onDelta(p: DeltaEvt) {
    if (!p || p.messageId !== streamingMsgID) return;
    $messages = $messages.map((m) =>
      m.id === p.messageId ? ({ ...m, content: m.content + p.delta } as model.Message) : m,
    );
    autoScroll();
  }

  function onDone(p: DoneEvt) {
    if (!p || p.messageId !== streamingMsgID) return;
    $messages = $messages.map((m) =>
      m.id === p.messageId
        ? ({
            ...m,
            status: "complete",
            promptTokens: p.promptTokens,
            completionTokens: p.completionTokens,
          } as model.Message)
        : m,
    );
    finishStreaming();
    // Reconcile persisted state (real ids, titles, order).
    refreshConversations();
  }

  function onError(p: ErrorEvt) {
    if (!p || p.messageId !== streamingMsgID) return;
    errorText = p.error || "请求失败";
    // Keep whatever content was already streamed.
    $messages = $messages.map((m) =>
      m.id === p.messageId ? ({ ...m, status: "error" } as model.Message) : m,
    );
    finishStreaming();
  }

  function finishStreaming() {
    streamingMsgID = -1;
    $streaming = false;
  }

  onMount(() => {
    EventsOn("chat:delta", onDelta);
    EventsOn("chat:done", onDone);
    EventsOn("chat:error", onError);
    loadModels();
  });

  onDestroy(() => {
    EventsOff("chat:delta");
    EventsOff("chat:done");
    EventsOff("chat:error");
  });

  async function refreshConversations() {
    try {
      $conversations = await ListConversations();
    } catch (e) {
      console.error("list conversations", e);
    }
  }

  // --- Conversation selection ---
  async function selectConversation(id: string) {
    if (id === $currentConversationID) return;
    $currentConversationID = id;
    resetRecommendation();
    errorText = "";
    try {
      $messages = await ListMessages(id);
    } catch (e) {
      console.error("list messages", e);
      $messages = [];
    }
    autoScroll();
  }

  async function createConversation() {
    try {
      const c = await NewConversation("", "", "");
      await refreshConversations();
      $currentConversationID = c.id;
      $messages = [];
      resetRecommendation();
      errorText = "";
      await tick();
      input?.focus();
    } catch (e) {
      console.error("new conversation", e);
      errorText = "创建会话失败";
    }
  }

  async function renameConversation(e: CustomEvent<{ id: string; title: string }>) {
    try {
      await RenameConversation(e.detail.id, e.detail.title);
      await refreshConversations();
    } catch (err) {
      console.error("rename", err);
    }
  }

  async function removeConversation(e: CustomEvent<string>) {
    const id = e.detail;
    try {
      await DeleteConversation(id);
      if (id === $currentConversationID) {
        $currentConversationID = null;
        $messages = [];
      }
      await refreshConversations();
    } catch (err) {
      console.error("delete", err);
    }
  }

  // --- Recommendation (light hint, UX §3.2) ---
  let rec: main.Recommendation | null = null;
  let recTaskType = "";
  let recLoading = false;
  let classifyTimer: ReturnType<typeof setTimeout> | undefined;

  function resetRecommendation() {
    rec = null;
    recTaskType = "";
    recLoading = false;
    clearTimeout(classifyTimer);
  }

  // Classify only on the conversation's first message (UX §3.2).
  function onTyping(e: CustomEvent<string>) {
    const text = e.detail.trim();
    if ($messages.length > 0 || rec) return;
    clearTimeout(classifyTimer);
    if (text.length < 4) {
      recLoading = false;
      return;
    }
    classifyTimer = setTimeout(() => classify(text), 600);
  }

  async function classify(text: string) {
    recLoading = true;
    try {
      recTaskType = await ClassifyTask(text);
      rec = await Recommend(recTaskType, "primary");
    } catch (err) {
      console.error("classify/recommend", err);
      rec = null;
    } finally {
      recLoading = false;
    }
  }

  // --- Sending ---
  async function send(content: string) {
    if (!content || $streaming) return;
    errorText = "";

    // Ensure a conversation exists.
    let convID = $currentConversationID;
    if (!convID) {
      try {
        const c = await NewConversation("", "", "");
        await refreshConversations();
        convID = c.id;
        $currentConversationID = convID;
        $messages = [];
      } catch (e) {
        console.error("new conversation on send", e);
        errorText = "创建会话失败";
        return;
      }
    }

    const now = Date.now(); // unix millis, matching the backend
    // Optimistic user bubble (temp id, reconciled on chat:done).
    const userMsg = {
      id: -Date.now(),
      conversationId: convID,
      role: "user",
      content,
      model: "",
      promptTokens: 0,
      completionTokens: 0,
      status: "complete",
      createdAt: now,
    } as model.Message;
    $messages = [...$messages, userMsg];
    resetRecommendation();
    autoScroll();

    try {
      $streaming = true;
      const assistantID = await SendMessage(convID, content, providerName, currentModel, 0.3, 4096, []);
      streamingMsgID = assistantID;
      // Assistant placeholder that delta events will fill.
      const assistantMsg = {
        id: assistantID,
        conversationId: convID,
        role: "assistant",
        content: "",
        model: "",
        promptTokens: 0,
        completionTokens: 0,
        status: "streaming",
        createdAt: Date.now(), // unix millis, matching the backend
      } as model.Message;
      $messages = [...$messages, assistantMsg];
      autoScroll();
    } catch (e) {
      console.error("send message", e);
      errorText = typeof e === "string" ? e : "发送失败";
      finishStreaming();
    }
  }

  async function stop() {
    if (!$currentConversationID) return;
    try {
      await StopGeneration($currentConversationID);
    } catch (e) {
      console.error("stop", e);
    }
    finishStreaming();
  }

  // --- Auto scroll to bottom ---
  async function autoScroll() {
    await tick();
    if (scroller) scroller.scrollTop = scroller.scrollHeight;
  }

  // ⌘N handled globally would collide with view switching; keep local hint only.
  $: hasCurrent = !!$currentConversationID;
</script>

<div class="chat">
  <SessionList
    conversations={$conversations}
    currentID={$currentConversationID}
    on:select={(e) => selectConversation(e.detail)}
    on:create={createConversation}
    on:rename={renameConversation}
    on:remove={removeConversation}
  />

  <div class="main">
    <div class="topbar">
      <span class="tb-label">model</span>
      <select class="model-select" bind:value={currentModel} disabled={$streaming || modelsLoading}>
        {#if models.length === 0}
          <option value="">{modelsLoading ? "加载中…" : (currentModel || "未配置")}</option>
        {:else}
          {#each models as m}
            <option value={m}>{m}</option>
          {/each}
        {/if}
      </select>
      <button class="tb-refresh" title="拉取模型列表" on:click={loadModels} disabled={$streaming || modelsLoading}>↻</button>
      <span class="tb-provider">{providerName}</span>
    </div>
    {#if hasCurrent}
      <div class="stream" bind:this={scroller}>
        <div class="stream-inner">
          {#if $messages.length === 0}
            <div class="hint">开始输入来发送第一条消息。</div>
          {:else}
            {#each $messages as m (m.id)}
              <MessageBubble message={m} streaming={m.id === streamingMsgID} />
            {/each}
          {/if}
        </div>
      </div>
    {:else}
      <div class="empty">
        <div class="empty-card">
          <div class="logo">◇ Axon</div>
          <p>⌘N 开始第一个对话</p>
          <button class="cta" on:click={createConversation}>+ 新会话</button>
        </div>
      </div>
    {/if}

    <div class="dock">
      {#if errorText}
        <div class="error-banner">
          ⚠ {errorText}
          <button class="dismiss" on:click={() => (errorText = "")}>✕</button>
        </div>
      {/if}
      {#if hasCurrent}
        <RecommendationBar taskType={recTaskType} {rec} loading={recLoading} />
        <MessageInput
          bind:this={input}
          streaming={$streaming}
          disabled={$streaming}
          on:send={(e) => send(e.detail)}
          on:sendRecommended={(e) => send(e.detail)}
          on:typing={onTyping}
          on:stop={stop}
        />
      {/if}
    </div>
  </div>
</div>

<style>
  .chat {
    display: flex;
    height: 100%;
    min-height: 0;
  }
  .main {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
    min-height: 0;
  }
  .topbar {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 6px 12px;
    border-bottom: 1px solid var(--border);
    background: var(--bg-surface);
    font-family: var(--font-mono);
    font-size: 12px;
  }
  .tb-label {
    color: var(--text-muted);
  }
  .model-select {
    background: var(--bg-elevated);
    color: var(--text-primary);
    border: 1px solid var(--border);
    border-radius: var(--radius-control);
    font-family: var(--font-mono);
    font-size: 12px;
    padding: 3px 8px;
    max-width: 260px;
  }
  .tb-refresh {
    background: var(--bg-elevated);
    color: var(--text-muted);
    border: 1px solid var(--border);
    border-radius: var(--radius-control);
    padding: 3px 8px;
  }
  .tb-refresh:hover:not(:disabled) {
    color: var(--text-primary);
    border-color: var(--accent);
  }
  .tb-provider {
    margin-left: auto;
    color: var(--text-muted);
  }
  .stream {
    flex: 1;
    overflow-y: auto;
    min-height: 0;
  }
  .stream-inner {
    max-width: var(--read-max);
    margin: 0 auto;
    padding: 16px;
  }
  .hint {
    color: var(--text-muted);
    text-align: center;
    padding: 40px 0;
    font-size: 13px;
  }
  .empty {
    flex: 1;
    display: flex;
    align-items: center;
    justify-content: center;
  }
  .empty-card {
    text-align: center;
    color: var(--text-muted);
    border: 1px solid var(--border);
    border-radius: var(--radius-card);
    padding: 32px 40px;
    background: var(--bg-surface);
  }
  .logo {
    font-size: 20px;
    color: var(--text-primary);
    margin-bottom: 8px;
  }
  .empty-card p {
    margin: 8px 0 16px;
  }
  .cta {
    background: var(--accent);
    color: var(--accent-fg);
    border: none;
    border-radius: var(--radius-control);
    padding: 8px 16px;
    font-size: 13px;
  }
  .dock {
    flex: 0 0 auto;
    padding: 12px 16px 16px;
    border-top: 1px solid var(--border);
  }
  .dock > :global(*) {
    max-width: var(--read-max);
    margin-left: auto;
    margin-right: auto;
  }
  .error-banner {
    display: flex;
    align-items: center;
    gap: 8px;
    background: rgba(248, 81, 73, 0.12);
    border: 1px solid var(--danger);
    color: var(--danger);
    border-radius: var(--radius-control);
    padding: 8px 12px;
    margin-bottom: var(--space);
    font-size: 13px;
  }
  .dismiss {
    margin-left: auto;
    background: transparent;
    border: none;
    color: var(--danger);
    font-size: 13px;
  }
</style>
