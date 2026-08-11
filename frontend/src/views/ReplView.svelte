<script lang="ts">
  // Minimal REPL: one full-screen flow, a "❯" input at the bottom, slash
  // commands for everything else. No sidebar / topbar / panels.
  import { onMount, onDestroy, tick } from "svelte";
  import { conversations, currentConversationID, messages, streaming, activeView } from "../lib/stores";
  import type { model } from "../../wailsjs/go/models";
  import {
    ListConversations, ListMessages, NewConversation,
    SendMessage, StopGeneration, ListProviders, ListModels, RecallMemories,
  } from "../../wailsjs/go/main/App.js";
  import { EventsOn, EventsOff } from "../../wailsjs/runtime/runtime.js";
  import MessageBubble from "../lib/chat/MessageBubble.svelte";

  // Transient system/notice lines shown inline in the flow (command output).
  type Notice = { id: number; kind: "sys" | "err"; text: string };
  let notices: Notice[] = [];
  let noticeSeq = -1;
  function note(text: string, kind: "sys" | "err" = "sys") {
    notices = [...notices, { id: noticeSeq--, kind, text }];
    autoScroll();
  }

  let providerName = "";
  let currentModel = "";
  let models: string[] = [];
  let streamingMsgID = -1;
  let value = "";
  let scroller: HTMLElement;
  let inputEl: HTMLTextAreaElement;

  async function loadModels() {
    try {
      const provs = await ListProviders();
      const usable = provs.find((p) => p.hasKey) ?? provs[0];
      if (!usable) return;
      providerName = usable.name;
      models = await ListModels(providerName);
      if (models.length && !models.includes(currentModel)) currentModel = models[0];
    } catch (e) { console.error("load models", e); }
  }

  async function autoScroll() {
    await tick();
    if (scroller) scroller.scrollTop = scroller.scrollHeight;
  }

  // --- Slash commands ---
  async function runCommand(line: string) {
    const [cmd, ...rest] = line.slice(1).trim().split(/\s+/);
    const arg = rest.join(" ");
    switch (cmd) {
      case "help":
        note("命令: /new 新会话 · /model [名] 看或切模型 · /models 列模型 · /sessions 列会话 · /open <n> 打开 · /search <词> 搜历史 · /term 终端 · /settings 设置 · /clear 清屏");
        break;
      case "new":
        await newConv(); note("已开新会话"); break;
      case "model":
        if (!arg) { note(`当前模型: ${currentModel || "(未设)"} · provider: ${providerName}`); }
        else if (models.length && !models.includes(arg)) { note(`未知模型 ${arg}，用 /models 查看`, "err"); }
        else { currentModel = arg; note(`已切换模型: ${arg}`); }
        break;
      case "models":
        await loadModels();
        note(models.length ? "可用模型:\n" + models.join("\n") : "无可用模型（先 /settings 配 key）");
        break;
      case "sessions":
        await refreshConvs();
        note($conversations.length
          ? "会话:\n" + $conversations.map((c, i) => `${i + 1}. ${c.title || "(无标题)"}`).join("\n")
          : "还没有会话");
        break;
      case "open": {
        const n = parseInt(arg, 10);
        if (!n || n < 1 || n > $conversations.length) { note("用法: /open <序号>（先 /sessions）", "err"); break; }
        await openConv($conversations[n - 1].id); break;
      }
      case "search": {
        if (!arg) { note("用法: /search <关键词>", "err"); break; }
        const hits = await RecallMemories("", arg).catch(() => []);
        note(hits && hits.length
          ? "相关历史:\n" + hits.map((h: any) => `· ${h.title || h.conversationId}  (${Math.round((h.score || 0) * 100)}%)`).join("\n")
          : "没搜到相关历史（或未配 embedding）");
        break;
      }
      case "term": $activeView = "terminal"; break;
      case "settings": $activeView = "settings"; break;
      case "clear": notices = []; $messages = []; note("已清屏（历史仍在存档中）"); break;
      default: note(`未知命令 /${cmd}，输入 /help 查看`, "err");
    }
    autoScroll();
  }

  // --- Streaming events ---
  function onDelta(p: any) {
    if (!p || p.messageId !== streamingMsgID) return;
    $messages = $messages.map((m) => m.id === p.messageId ? ({ ...m, content: m.content + p.delta } as model.Message) : m);
    autoScroll();
  }
  function onDone(p: any) {
    if (!p || p.messageId !== streamingMsgID) return;
    $messages = $messages.map((m) => m.id === p.messageId ? ({ ...m, status: "complete" } as model.Message) : m);
    streamingMsgID = -1; $streaming = false; refreshConvs();
  }
  function onError(p: any) {
    if (!p || p.messageId !== streamingMsgID) return;
    note(p.error || "请求失败", "err");
    streamingMsgID = -1; $streaming = false;
  }

  async function refreshConvs() { try { $conversations = await ListConversations(); } catch {} }
  async function newConv() {
    const c = await NewConversation("", "", "");
    await refreshConvs(); $currentConversationID = c.id; $messages = []; notices = [];
  }
  async function openConv(id: string) {
    $currentConversationID = id; notices = [];
    try { $messages = await ListMessages(id); } catch { $messages = []; }
    note(`已打开会话`); autoScroll();
  }

  // --- Submit ---
  async function submit() {
    const text = value.trim();
    if (!text || $streaming) return;
    value = "";
    if (text.startsWith("/")) { await runCommand(text); return; }

    let convID = $currentConversationID;
    if (!convID) { await newConv(); convID = $currentConversationID!; }

    const now = Date.now();
    $messages = [...$messages, { id: -now, conversationId: convID, role: "user", content: text, model: "", promptTokens: 0, completionTokens: 0, status: "complete", createdAt: now } as model.Message];
    autoScroll();
    try {
      $streaming = true;
      const aid = await SendMessage(convID, text, providerName, currentModel, 0.3, 4096, []);
      streamingMsgID = aid;
      $messages = [...$messages, { id: aid, conversationId: convID, role: "assistant", content: "", model: currentModel, promptTokens: 0, completionTokens: 0, status: "streaming", createdAt: Date.now() } as model.Message];
      autoScroll();
    } catch (e: any) { note(String(e?.message || e), "err"); $streaming = false; }
  }

  function onKey(e: KeyboardEvent) {
    if (e.key === "Enter" && !e.shiftKey) { e.preventDefault(); submit(); }
  }
  function stop() { if ($currentConversationID) StopGeneration($currentConversationID); }

  onMount(async () => {
    EventsOn("chat:delta", onDelta); EventsOn("chat:done", onDone); EventsOn("chat:error", onError);
    await loadModels(); await refreshConvs();
    if ($conversations.length) await openConv($conversations[0].id);
    note("输入消息开始对话，或 /help 看命令。⌘J 切换终端。");
    inputEl?.focus();
  });
  onDestroy(() => { EventsOff("chat:delta"); EventsOff("chat:done"); EventsOff("chat:error"); });
</script>

<div class="repl">
  <div class="flow" bind:this={scroller}>
    <div class="flow-inner">
      {#each $messages as m (m.id)}
        <MessageBubble message={m} streaming={m.id === streamingMsgID} />
      {/each}
      {#each notices as n (n.id)}
        <div class="notice {n.kind}">{n.text}</div>
      {/each}
    </div>
  </div>
  <div class="prompt-row">
    <span class="sigil">{$streaming ? "…" : "❯"}</span>
    <textarea
      bind:this={inputEl}
      bind:value
      on:keydown={onKey}
      placeholder={$streaming ? "生成中…（回车前先 /stop 或点停止）" : "输入消息，或 /help"}
      rows="1"
    ></textarea>
    {#if $streaming}
      <button class="stop" on:click={stop}>■ stop</button>
    {/if}
  </div>
</div>

<style>
  .repl {
    display: flex;
    flex-direction: column;
    height: 100%;
    background: var(--bg-base);
    font-family: var(--font-mono);
  }
  .flow {
    flex: 1;
    overflow-y: auto;
    min-height: 0;
    padding: 12px 16px;
  }
  .flow-inner {
    max-width: 900px;
    margin: 0 auto;
  }
  .notice {
    white-space: pre-wrap;
    font-size: 12.5px;
    padding: 4px 0 4px 22px;
    color: var(--text-muted);
  }
  .notice.err {
    color: var(--danger);
  }
  .prompt-row {
    display: flex;
    align-items: flex-start;
    gap: 8px;
    border-top: 1px solid var(--border);
    padding: 10px 16px;
    background: var(--bg-base);
  }
  .sigil {
    color: var(--accent);
    font-size: 14px;
    line-height: 1.6;
    user-select: none;
  }
  textarea {
    flex: 1;
    resize: none;
    background: transparent;
    border: none;
    outline: none;
    color: var(--text-primary);
    caret-color: var(--accent);
    font-family: var(--font-mono);
    font-size: 14px;
    line-height: 1.6;
    max-height: 180px;
  }
  textarea::placeholder {
    color: var(--text-muted);
  }
  .stop {
    background: transparent;
    border: 1px solid var(--danger);
    color: var(--danger);
    border-radius: var(--radius-control);
    padding: 4px 12px;
    font-family: var(--font-mono);
    font-size: 12px;
  }
</style>


