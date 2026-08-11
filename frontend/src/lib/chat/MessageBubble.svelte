<script lang="ts">
  // Terminal-style message line. User turns are prefixed "›"; assistant turns
  // "●". While an assistant reply is streaming with no text yet, an explicit
  // "generating" indicator shows so a slow (reasoning) model never looks stuck.
  import { marked } from "marked";
  import type { model } from "../../../wailsjs/go/models";

  export let message: model.Message;
  export let streaming = false;

  $: isUser = message.role === "user";
  $: isEmptyStreaming = streaming && !message.content;

  function render(content: string): string {
    try {
      return marked.parse(content, { async: false, breaks: true }) as string;
    } catch {
      return escapeHtml(content);
    }
  }
  function escapeHtml(s: string): string {
    return s.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;");
  }

  $: html = isUser ? "" : render(message.content);

  function fmtTime(ts: number): string {
    if (!ts) return "";
    const ms = ts < 1e12 ? ts * 1000 : ts;
    return new Date(ms).toLocaleTimeString("zh-CN", { hour: "2-digit", minute: "2-digit" });
  }

  function onBodyClick(e: MouseEvent) {
    const target = e.target as HTMLElement;
    if (!target.classList.contains("copy-code")) return;
    const pre = target.previousElementSibling as HTMLElement | null;
    const code = pre?.querySelector("code")?.textContent ?? pre?.textContent ?? "";
    navigator.clipboard.writeText(code).then(() => {
      const old = target.textContent;
      target.textContent = "copied";
      setTimeout(() => (target.textContent = old), 1200);
    });
  }

  function withCopyButtons(raw: string): string {
    return raw.replace(/<\/pre>/g, '</pre><button class="copy-code">copy</button>');
  }
  $: finalHtml = withCopyButtons(html);
</script>

<div class="line" class:user={isUser}>
  <span class="prefix">{isUser ? "›" : "●"}</span>
  <div class="content">
    <div class="meta">
      <span class="who">{isUser ? "you" : "assistant"}</span>
      {#if !isUser && message.model}<span class="model">{message.model}</span>{/if}
      <span class="time">{fmtTime(message.createdAt)}</span>
    </div>
    {#if isUser}
      <div class="text selectable">{message.content}</div>
    {:else if isEmptyStreaming}
      <div class="generating">
        信号传导中
        <span class="axon-signal"><i></i><i></i><i></i></span>
      </div>
    {:else}
      <!-- svelte-ignore a11y-no-static-element-interactions a11y-click-events-have-key-events -->
      <div class="body selectable" on:click={onBodyClick}>
        {@html finalHtml}{#if streaming}<span class="cursor"></span>{/if}
      </div>
    {/if}
  </div>
</div>

<style>
  .line {
    display: flex;
    gap: 8px;
    padding: 6px 0;
    font-family: var(--font-mono);
    font-size: 13px;
  }
  .prefix {
    flex: 0 0 auto;
    width: 14px;
    text-align: center;
    color: var(--text-muted);
    user-select: none;
  }
  .line.user .prefix {
    color: var(--accent);
  }
  .content {
    flex: 1;
    min-width: 0;
  }
  .meta {
    display: flex;
    align-items: center;
    gap: 8px;
    font-size: 11px;
    color: var(--text-muted);
    margin-bottom: 2px;
  }
  .who {
    color: var(--text-primary);
    font-weight: 600;
  }
  .line.user .who {
    color: var(--accent);
  }
  .time {
    margin-left: auto;
  }
  .text {
    white-space: pre-wrap;
    word-break: break-word;
    color: var(--text-primary);
  }
  .generating {
    color: var(--text-muted);
    font-style: italic;
  }
  .dots::after {
    content: "";
    animation: dots 1.2s steps(4, end) infinite;
  }
  @keyframes dots {
    0% { content: ""; }
    25% { content: "."; }
    50% { content: ".."; }
    75% { content: "..."; }
  }
  .cursor {
    display: inline-block;
    width: 7px;
    height: 1em;
    background: var(--accent);
    margin-left: 1px;
    vertical-align: text-bottom;
    animation: blink 1s step-start infinite;
  }
  @keyframes blink {
    50% { opacity: 0; }
  }
  .body {
    word-break: break-word;
    line-height: 1.55;
    color: var(--text-primary);
  }
  .body :global(p) { margin: 0 0 6px; }
  .body :global(p:last-child) { margin-bottom: 0; }
  .body :global(pre) {
    background: var(--bg-base);
    border: 1px solid var(--border);
    border-radius: var(--radius-control);
    padding: 10px;
    overflow-x: auto;
    font-size: 12.5px;
    margin: 6px 0 0;
  }
  .body :global(code) { font-size: 0.92em; }
  .body :global(:not(pre) > code) {
    background: var(--bg-elevated);
    border-radius: 4px;
    padding: 1px 5px;
  }
  .body :global(.copy-code) {
    background: var(--bg-elevated);
    border: 1px solid var(--border);
    color: var(--text-muted);
    border-radius: 4px;
    font-size: 11px;
    font-family: var(--font-mono);
    padding: 2px 8px;
    margin: 4px 0 6px;
  }
  .body :global(.copy-code:hover) { color: var(--text-primary); border-color: var(--accent); }
  .body :global(a) { color: var(--accent); }
  .body :global(ul), .body :global(ol) { margin: 0 0 6px; padding-left: 20px; }
  .body :global(blockquote) {
    border-left: 3px solid var(--border);
    margin: 0 0 6px;
    padding-left: 12px;
    color: var(--text-muted);
  }
</style>

