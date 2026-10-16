// Global UI state for Axon. Kept small: which view is active, the conversation
// list, the current conversation and its messages.
import { writable, derived, get } from "svelte/store";
import type { model, claudedata } from "../../wailsjs/go/models";
import { ListClaudeProjects, ListNamespaces, ListClusters } from "../../wailsjs/go/main/App.js";

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

// The key a project is addressed by: its .axon-project namespace when it has
// one, otherwise its Claude Code directory name. A namespace usually spans many
// Claude directories (one per repository), so several projects collapse onto one
// key — which is exactly what indexing and the graph want.
export function projectKey(p: claudedata.Project): string {
  return p.namespace || p.slug;
}

// Resolve a currentProject value back to the project it came from. Tolerates a
// bare Claude directory slug as well, so a stale selection keeps working.
export function findProject(list: claudedata.Project[], key: string): claudedata.Project | undefined {
  return list.find((p) => projectKey(p) === key) || list.find((p) => p.slug === key);
}

// One entry per distinct key, newest activity first, with the session count
// summed across the directories that share it.
export type ProjectOption = { key: string; label: string; sessions: number; updatedAt: number };

export function projectOptions(list: claudedata.Project[]): ProjectOption[] {
  const byKey = new Map<string, ProjectOption>();
  for (const p of list) {
    const key = projectKey(p);
    const cur = byKey.get(key);
    if (cur) {
      cur.sessions += p.sessionCount;
      cur.updatedAt = Math.max(cur.updatedAt, p.updatedAt);
      continue;
    }
    byKey.set(key, {
      key,
      // For a namespace, show it plus the first directory it covers, so the
      // picker says both "which knowledge" and "where on disk".
      label: p.namespace ? `${p.namespace} · ${p.path}` : p.path,
      sessions: p.sessionCount,
      updatedAt: p.updatedAt,
    });
  }
  return [...byKey.values()].sort((a, b) => b.updatedAt - a.updatedAt);
}

// --- Named namespaces (new) -------------------------------------------
// All available knowledge-graph namespaces. Populated on startup from
// ListNamespaces (scans graphcache/).
export type NamespaceInfo = { name: string; entities: number };
export const namespaces = writable<NamespaceInfo[]>([]);

// Which namespaces are currently selected for graph viewing (multi-select).
export const selectedNamespaces = writable<string[]>([]);

// --- Derived knowledge domains -------------------------------------------
// Namespaces are bookkeeping: a directory gets one, an imported bundle takes its
// own title. Nothing checked whether two of them held the same knowledge, so the
// same project routinely ended up split across several (this machine had
// "wanlian" and "Wanlian1" sharing 99.7% of their entities) and every lookup had
// to guess which to use.
//
// A cluster is the knowledge-level view: namespaces that share entities are one
// domain. Derived from content, so there is nothing for the user to maintain.
export type ClusterInfo = {
  name: string;
  slugs: string[];
  entities: number;
  relations: number;
  soleSource: number;
  redundant: boolean;
};
export const clusters = writable<ClusterInfo[]>([]);

// Pairwise overlaps behind the grouping, kept so the reason two namespaces were
// put together is inspectable rather than a silent decision.
export type ClusterLinkInfo = { a: string; b: string; shared: number; smaller: number; overlap: number; merged: boolean };
export const clusterLinks = writable<ClusterLinkInfo[]>([]);

export async function loadClusters(refresh = false): Promise<void> {
  try {
    const view: any = await ListClusters(refresh);
    clusters.set(view?.clusters ?? []);
    clusterLinks.set(view?.links ?? []);
  } catch (e) {
    console.error("load clusters", e);
  }
}

// The namespaces a graph build should actually read: everything the selection
// belongs to. Selecting one namespace pulls in the rest of its domain, which is
// what removes the manual multi-select — asking for "wanlian" also brings
// "Wanlian1" without the user knowing either name.
//
// Falls back to the raw selection when clustering has not run yet, so graph
// viewing never breaks on a missing derived index.
export const effectiveNamespaces = derived(
  [selectedNamespaces, clusters],
  ([$sel, $clusters]): string[] => {
    if ($sel.length === 0) return [];
    const out: string[] = [];
    for (const ns of $sel) {
      const cluster = $clusters.find((c) => c.slugs.includes(ns));
      for (const s of cluster ? cluster.slugs : [ns]) {
        if (!out.includes(s)) out.push(s);
      }
    }
    return out;
  },
);

// The domain a namespace belongs to, for labelling the picker.
export function clusterFor(slug: string, list: ClusterInfo[]): ClusterInfo | undefined {
  return list.find((c) => c.slugs.includes(slug));
}

// Load namespaces from the backend and default-select the first one.
export async function loadNamespaces(): Promise<void> {
  try {
    const list: NamespaceInfo[] = await ListNamespaces();
    namespaces.set(list);
    const current = get(selectedNamespaces);
    if (current.length === 0 && !get(currentProject) && list.length > 0) {
      // Default: select the first non-global namespace, or just the first.
      const first = list.find((ns) => ns.name !== "_global_") || list[0];
      selectedNamespaces.set([first.name]);
      // Keep project selection independent from graph namespace selection.
      // The app may already have selected a Claude project; in that case the
      // project picker must not be overwritten by the namespace bootstrap.
    }
  } catch (e) {
    console.error("load namespaces", e);
  }
}

// Load the project list from disk and default the selection to the newest one
// (unless one is already chosen). Safe to call more than once.
export async function loadProjects(): Promise<void> {
  try {
    const list = await ListClaudeProjects();
    projects.set(list);
    if (!get(currentProject) && list.length) currentProject.set(projectKey(list[0]));
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
