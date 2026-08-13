<script lang="ts">
  // Embedded real shell via xterm.js wired to the Go PTY backend.
  import { onMount, onDestroy } from "svelte";
  import { Terminal } from "@xterm/xterm";
  import { FitAddon } from "@xterm/addon-fit";
  import "@xterm/xterm/css/xterm.css";
  import { TermStart, TermWrite, TermResize, TermStop } from "../../wailsjs/go/main/App.js";
  import { EventsOn, EventsOff } from "../../wailsjs/runtime/runtime.js";
  import { activeView, pendingResume } from "../lib/stores";
  import { get } from "svelte/store";

  let host: HTMLElement;
  let term: Terminal;
  let fit: FitAddon;
  let shellReady = false;

  // Flush a queued resume command once the shell has printed its first output
  // (i.e. it's ready to read input). Writing the whole command with its trailing
  // newline makes it run. Clearing the store marks it consumed.
  function flushPendingResume() {
    if (!shellReady) return;
    const cmd = get(pendingResume);
    if (!cmd) return;
    pendingResume.set("");
    TermWrite(cmd);
    term?.focus();
  }
  // React to a resume requested while this view is already mounted.
  $: if ($pendingResume && shellReady) flushPendingResume();

  // Decode base64 shell output (bytes preserved across the JSON bridge).
  function b64ToStr(b64: string): string {
    const bin = atob(b64);
    const bytes = new Uint8Array(bin.length);
    for (let i = 0; i < bin.length; i++) bytes[i] = bin.charCodeAt(i);
    return new TextDecoder().decode(bytes);
  }

  function doFit() {
    if (!fit || !term) return;
    try {
      fit.fit();
      TermResize(term.rows, term.cols);
    } catch {}
  }

  onMount(async () => {
    term = new Terminal({
      fontFamily: 'ui-monospace, "SF Mono", "JetBrains Mono", Menlo, monospace',
      fontSize: 13,
      theme: {
        background: "#0d1117",
        foreground: "#e6edf3",
        cursor: "#3b82f6",
        selectionBackground: "#30363d",
      },
      cursorBlink: true,
    });
    fit = new FitAddon();
    term.loadAddon(fit);
    term.open(host);
    doFit();

    EventsOn("term:data", (b64: string) => {
      term.write(b64ToStr(b64));
      // First output means the shell is up and reading input; safe to inject.
      if (!shellReady) { shellReady = true; flushPendingResume(); }
    });
    EventsOn("term:exit", () => {
      term.write("\r\n\x1b[31m[shell exited]\x1b[0m\r\n");
      shellReady = false;
    });

    // Forward keystrokes to the shell.
    term.onData((d) => TermWrite(d));

    await TermStart();
    doFit();
    term.focus();

    window.addEventListener("resize", doFit);
  });

  // Refit whenever the terminal becomes visible again (it's hidden, not
  // unmounted, on tab switch, so xterm can't measure while display:none).
  $: if ($activeView === "terminal" && fit) queueMicrotask(doFit);

  onDestroy(() => {
    window.removeEventListener("resize", doFit);
    EventsOff("term:data");
    EventsOff("term:exit");
    // Mounted for the app's lifetime; stop the shell only on real teardown.
    TermStop();
    term?.dispose();
  });
</script>

<div class="term-wrap">
  <div class="term-host" bind:this={host}></div>
</div>

<style>
  .term-wrap {
    height: 100%;
    background: #0d1117;
    padding: 8px;
  }
  .term-host {
    height: 100%;
  }
</style>
