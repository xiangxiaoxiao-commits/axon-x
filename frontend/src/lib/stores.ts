// Global UI state for Axon. Kept small: which view is active, the conversation
// list, the current conversation and its messages.
import { writable, get } from "svelte/store";
import type { model, claudedata } from "../../wailsjs/go/models";
import { ListClaudeProjects, ListNamespaces } from "../../wailsjs/go/main/App.js";

export type View = "tasks" | "commit" | "hub" | "search" | "sessions" | "graph" | "memory" | "chat" | "terminal" | "settings";

// Active main view. The knowledge graph is the primary entry (the product's
// main job: curate the per-project knowledge fed to agents over MCP); settings
// is the only other sidebar entry.
export const activeView = writable<View>("graph");

// --- Resume-in-terminal signal ------------------------------------------
// SessionsView asks the terminal to open a NEW tab running a resume command by
// pushing a request here and switching activeView to "terminal". TerminalView
// watches this, opens a tab, and clears it. A store (not a prop) because the two
// views never share a parent.
export type ResumeRequest = { title: string; cmd: string };
export const resumeRequest = writable<ResumeRequest | null>(null);

// --- Global project selection -------------------------------------------
// One project chosen once, shared by every view. Empty string means "all
// projects" (used by cross-project search).
export const currentProject = writable<string>("");

// The Claude Code project list, loaded once on startup.
export const projects = writable<claudedata.Project[]>([]);

// --- Named namespaces (new) -------------------------------------------
// All available knowledge-graph namespaces. Populated on startup from
// ListNamespaces (scans graphcache/).
export type NamespaceInfo = { name: string; entities: number };
export const namespaces = writable<NamespaceInfo[]>([]);

// Which namespaces are currently selected for graph viewing (multi-select).
export const selectedNamespaces = writable<string[]>([]);

// Load namespaces from the backend and default-select the first one.
export async function loadNamespaces(): Promise<void> {
  try {
    const list: NamespaceInfo[] = await ListNamespaces();
    namespaces.set(list);
    const current = get(selectedNamespaces);
    if (current.length === 0 && list.length > 0) {
      // Default: select the first non-global namespace, or just the first.
      const first = list.find((ns) => ns.name !== "_global_") || list[0];
      selectedNamespaces.set([first.name]);
      currentProject.set(first.name);
    }
  } catch (e) {
    console.error("load namespaces", e);
  }
}

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
