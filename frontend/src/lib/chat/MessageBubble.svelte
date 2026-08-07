<script lang="ts">
  // A single chat message. User messages are right-aligned plain text;
  // assistant messages are left-aligned cards with rendered Markdown.
  import { marked } from "marked";
  import type { model } from "../../../wailsjs/go/models";

  export let message: model.Message;
  // True while this assistant message is actively streaming.
  export let streaming = false;

  $: isUser = message.role === "user";

  // Render Markdown for assistant messages. marked escapes text and code
  // content by default (no raw HTML passthrough needed here). This is a local
  // single-user desktop app, so the residual XSS surface is low.
  function render(content: string): string {
    try {
      return marked.parse(content, { async: false, breaks: true }) as string;
    } catch {
      return escapeHtml(content);
    }
  }

  function escapeHtml(s: string): string {
    return s
      .replace(/&/g, "&amp;")
      .replace(/</g, "&lt;")
      .replace(/>/g, "&gt;");
  }

  $: html = isUser ? "" : render(message.content);

  function fmtTime(ts: number): string {
    if (!ts) return "";
    const ms = ts < 1e12 ? ts * 1000 : ts;
    const d = new Date(ms);
    return d.toLocaleTimeString("zh-CN", { hour: "2-digit", minute: "2-digit" });
  }

  // Delegated copy handler: click any [复制] button injected below <pre> blocks.
  function onBodyClick(e: MouseEvent) {
    const target = e.target as HTMLElement;
    if (!target.classList.contains("copy-code")) return;
    const pre = target.previousElementSibling as HTMLElement | null;
    const code = pre?.querySelector("code")?.textContent ?? pre?.textContent ?? "";
    navigator.clipboard.writeText(code).then(() => {
      const old = target.textContent;
      target.textContent = "已复制";
      setTimeout(() => (target.textContent = old), 1200);
    });
  }

  // Append a copy button after each <pre> in the rendered HTML.
  function withCopyButtons(raw: string): string {
    return raw.replace(/<\/pre>/g, "</pre><button class=\"copy-code\">复制</button>");
  }

  $: finalHtml = withCopyButtons(html);
</script>

<div class="row" class:user={isUser}>
  {#if isUser}
    <div class="bubble user selectable">{message.content}</div>
  {:else}
    <div class="card">
      <div class="meta">
        <span class="role">Assistant</span>
        {#if message.model}<span class="model">{message.model}</span>{/if}
        <span class="time">{fmtTime(message.createdAt)}</span>
      </div>
      <!-- svelte-ignore a11y-no-static-element-interactions a11y-click-events-have-key-events -->
      <div class="body selectable" on:click={onBodyClick}>
        {@html finalHtml}{#if streaming}<span class="cursor"></span>{/if}
      </div>
    </div>
  {/if}
</div>

<style>
  .row {
    display: flex;
    justify-content: flex-start;
    margin: 12px 0;
  }
  .row.user {
    justify-content: flex-end;
  }
  .bubble.user {
    max-width: 72%;
    background: var(--bg-elevated);
    border: 1px solid var(--border);
    border-radius: var(--radius-card);
    padding: 8px 12px;
    white-space: pre-wrap;
    word-break: break-word;
  }
  .card {
    max-width: 100%;
    width: 100%;
    background: var(--bg-surface);
    border: 1px solid var(--border);
    border-radius: var(--radius-card);
    padding: 10px 14px;
  }
  .meta {
    display: flex;
    align-items: center;
    gap: 8px;
    margin-bottom: 6px;
    font-size: 12px;
    color: var(--text-muted);
  }
  .role {
    font-weight: 600;
    color: var(--text-primary);
  }
  .model {
    font-family: var(--font-mono);
    font-size: 11px;
  }
  .time {
    margin-left: auto;
  }
  .body {
    word-break: break-word;
    line-height: 1.6;
  }
  .cursor {
    display: inline-block;
    width: 7px;
    height: 1em;
    background: var(--accent);
    margin-left: 2px;
    vertical-align: text-bottom;
    animation: blink 1s step-start infinite;
  }
  @keyframes blink {
    50% {
      opacity: 0;
    }
  }

  /* Markdown element styling (scoped via :global for {@html} content). */
  .body :global(p) {
    margin: 0 0 8px;
  }
  .body :global(p:last-child) {
    margin-bottom: 0;
  }
  .body :global(pre) {
    background: var(--bg-base);
    border: 1px solid var(--border);
    border-radius: var(--radius-control);
    padding: 12px;
    overflow-x: auto;
    font-family: var(--font-mono);
    font-size: 13px;
    margin: 8px 0 0;
  }
  .body :global(code) {
    font-family: var(--font-mono);
    font-size: 0.92em;
  }
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
    padding: 3px 8px;
    margin: 4px 0 8px;
  }
  .body :global(.copy-code:hover) {
    color: var(--text-primary);
    border-color: var(--accent);
  }
  .body :global(a) {
    color: var(--accent);
  }
  .body :global(ul),
  .body :global(ol) {
    margin: 0 0 8px;
    padding-left: 20px;
  }
  .body :global(h1),
  .body :global(h2),
  .body :global(h3) {
    margin: 12px 0 6px;
    line-height: 1.3;
  }
  .body :global(blockquote) {
    border-left: 3px solid var(--border);
    margin: 0 0 8px;
    padding-left: 12px;
    color: var(--text-muted);
  }
  .body :global(table) {
    border-collapse: collapse;
    margin: 0 0 8px;
  }
  .body :global(th),
  .body :global(td) {
    border: 1px solid var(--border);
    padding: 4px 8px;
  }
</style>
