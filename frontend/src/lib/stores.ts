// Global UI state for Axon. Kept small: which view is active, the conversation
// list, the current conversation and its messages.
import { writable, get } from "svelte/store";
import type { model, claudedata } from "../../wailsjs/go/models";
import { ListClaudeProjects } from "../../wailsjs/go/main/App.js";

export type View = "tasks" | "commit" | "hub" | "search" | "sessions" | "graph" | "memory" | "chat" | "terminal" | "settings";

// Active main view. The knowledge graph is the primary entry (the product's
// main job: curate the per-project knowledge fed to agents over MCP); settings
// is the only other sidebar entry.
export const activeView = writable<View>("graph");

// --- Resume-in-terminal signal ------------------------------------------
// SessionsView asks to reopen a Claude session by putting the ready-to-run
// shell command here and switching activeView to "terminal". TerminalView
// consumes it once the shell is ready, writes it to the PTY, then clears it.
// A store (not a prop) because the two views never share a parent.
export const pendingResume = writable<string>("");

// --- Global project selection -------------------------------------------
// One project chosen once, shared by every view. Empty string means "all
// projects" (used by cross-project search).
export const currentProject = writable<string>("");

// The Claude Code project list, loaded once on startup.
export const projects = writable<claudedata.Project[]>([]);

// Load the project list from disk and default the selection to the first
// project (unless one is already chosen). Safe to call more than once.
export async function loadProjects(): Promise<void> {
  try {
    const list = await ListClaudeProjects();
    projects.set(list);
    if (!get(currentProject) && list.length) currentProject.set(list[0].slug);
  } catch (e) {
    console.error("load projects", e);
  }
}

// All conversations (sidebar), newest activity first.
export const conversations = writable<model.Conversation[]>([]);

// Currently open conversation id, or null when none.
export const currentConversationID = writable<string | null>(null);

// Messages of the current conversation.
export const messages = writable<model.Message[]>([]);

// Whether a provider with a stored key exists; drives the first-run gate.
export const hasProvider = writable<boolean>(false);

// True while an assistant reply is streaming.
export const streaming = writable<boolean>(false);

// --- Onboarding cross-view signals --------------------------------------
// Bumped to ask the knowledge view to start indexing the current project, so
// the onboarding checklist (which may render inside the graph view) and the
// settings view can both trigger it without prop-drilling. Consumers watch for
// changes and act once.
export const indexRequest = writable<number>(0);
export function requestIndex(): void {
  indexRequest.update((n) => n + 1);
}
