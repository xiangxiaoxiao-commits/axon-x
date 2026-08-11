<script lang="ts">
  // Embedded real shell via xterm.js wired to the Go PTY backend.
  import { onMount, onDestroy } from "svelte";
  import { Terminal } from "@xterm/xterm";
  import { FitAddon } from "@xterm/addon-fit";
  import "@xterm/xterm/css/xterm.css";
  import { TermStart, TermWrite, TermResize, TermStop } from "../../wailsjs/go/main/App.js";
  import { EventsOn, EventsOff } from "../../wailsjs/runtime/runtime.js";

  let host: HTMLElement;
  let term: Terminal;
  let fit: FitAddon;

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

    EventsOn("term:data", (b64: string) => term.write(b64ToStr(b64)));
    EventsOn("term:exit", () => term.write("\r\n\x1b[31m[shell exited]\x1b[0m\r\n"));

    // Forward keystrokes to the shell.
    term.onData((d) => TermWrite(d));

    await TermStart();
    doFit();
    term.focus();

    window.addEventListener("resize", doFit);
  });

  onDestroy(() => {
    window.removeEventListener("resize", doFit);
    EventsOff("term:data");
    EventsOff("term:exit");
    // Keep the shell alive across tab switches; stop only on teardown.
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
