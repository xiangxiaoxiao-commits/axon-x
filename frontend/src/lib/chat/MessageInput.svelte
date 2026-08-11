<script lang="ts">
  // Auto-growing textarea. Enter sends, Shift+Enter inserts a newline.
  // Cmd/Ctrl+Enter dispatches "sendWithRecommended" (采用推荐并发送).
  import { createEventDispatcher, tick } from "svelte";

  export let disabled = false;
  export let streaming = false;

  const dispatch = createEventDispatcher<{
    send: string;
    sendRecommended: string;
    stop: void;
    typing: string;
  }>();

  let value = "";
  let el: HTMLTextAreaElement;

  async function autoGrow() {
    await tick();
    if (!el) return;
    el.style.height = "auto";
    el.style.height = Math.min(el.scrollHeight, 200) + "px";
  }

  function onInput() {
    autoGrow();
    dispatch("typing", value);
  }

  function doSend(recommended = false) {
    const text = value.trim();
    if (!text || disabled) return;
    dispatch(recommended ? "sendRecommended" : "send", text);
    value = "";
    autoGrow();
  }

  function onKey(e: KeyboardEvent) {
    if (e.key === "Enter" && !e.shiftKey) {
      if (e.metaKey || e.ctrlKey) {
        e.preventDefault();
        doSend(true);
        return;
      }
      e.preventDefault();
      doSend(false);
    }
  }

  export function focus() {
    el?.focus();
  }
</script>

<div class="composer" class:streaming>
  <span class="prompt">{streaming ? "…" : "$"}</span>
  <textarea
    bind:this={el}
    bind:value
    on:input={onInput}
    on:keydown={onKey}
    placeholder={streaming ? "生成中… 按 ⏎ 之前请先停止" : "输入消息，⏎ 发送 · ⇧⏎ 换行"}
    rows="1"
  ></textarea>
  <div class="actions">
    {#if streaming}
      <button class="stop" on:click={() => dispatch("stop")}>■ stop</button>
    {:else}
      <button class="send" disabled={disabled || !value.trim()} on:click={() => doSend(false)}
        >send ↵</button
      >
    {/if}
  </div>
</div>

<style>
  .composer {
    display: flex;
    align-items: flex-start;
    gap: 8px;
    background: var(--bg-base);
    border: 1px solid var(--border);
    border-radius: var(--radius-control);
    padding: 8px 10px;
    font-family: var(--font-mono);
  }
  .composer:focus-within {
    border-color: var(--accent);
  }
  .prompt {
    flex: 0 0 auto;
    color: var(--accent);
    font-family: var(--font-mono);
    font-size: 13px;
    line-height: 1.6;
    user-select: none;
  }
  .composer.streaming .prompt {
    color: var(--warning);
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
    font-size: 13px;
    line-height: 1.6;
    max-height: 200px;
    overflow-y: auto;
  }
  textarea::placeholder {
    color: var(--text-muted);
  }
  .actions {
    flex: 0 0 auto;
  }
  .send,
  .stop {
    background: transparent;
    border: 1px solid var(--border);
    border-radius: var(--radius-control);
    padding: 4px 12px;
    font-family: var(--font-mono);
    font-size: 12px;
  }
  .send {
    color: var(--accent);
    border-color: var(--accent);
  }
  .send:disabled {
    color: var(--text-muted);
    border-color: var(--border);
    cursor: not-allowed;
  }
  .stop {
    color: var(--danger);
    border-color: var(--danger);
  }
  .stop:hover {
    background: var(--danger);
    color: #fff;
  }
</style>
