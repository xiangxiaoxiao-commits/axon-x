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

<div class="composer">
  <textarea
    bind:this={el}
    bind:value
    on:input={onInput}
    on:keydown={onKey}
    placeholder="输入消息…  (⏎ 发送 · ⇧⏎ 换行)"
    rows="1"
  ></textarea>
  <div class="actions">
    {#if streaming}
      <button class="stop" on:click={() => dispatch("stop")}>■ 停止</button>
    {:else}
      <button class="send" disabled={disabled || !value.trim()} on:click={() => doSend(false)}
        >↑ 发送</button
      >
    {/if}
  </div>
</div>

<style>
  .composer {
    display: flex;
    align-items: flex-end;
    gap: var(--space);
    background: var(--bg-surface);
    border: 1px solid var(--border);
    border-radius: var(--radius-card);
    padding: var(--space);
  }
  .composer:focus-within {
    border-color: var(--accent);
  }
  textarea {
    flex: 1;
    resize: none;
    background: transparent;
    border: none;
    outline: none;
    color: var(--text-primary);
    font-family: inherit;
    font-size: 14px;
    line-height: 1.5;
    max-height: 200px;
    overflow-y: auto;
  }
  .actions {
    flex: 0 0 auto;
  }
  .send,
  .stop {
    border: none;
    border-radius: var(--radius-control);
    padding: 6px 14px;
    font-size: 13px;
    font-weight: 500;
  }
  .send {
    background: var(--accent);
    color: var(--accent-fg);
  }
  .send:disabled {
    background: var(--bg-elevated);
    color: var(--text-muted);
    cursor: not-allowed;
  }
  .stop {
    background: var(--danger);
    color: #fff;
  }
  .stop:hover {
    filter: brightness(1.1);
  }
</style>
