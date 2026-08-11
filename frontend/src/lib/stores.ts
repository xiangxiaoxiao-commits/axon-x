// Global UI state for Axon. Kept small: which view is active, the conversation
// list, the current conversation and its messages.
import { writable } from "svelte/store";
import type { model } from "../../wailsjs/go/models";

export type View = "sessions" | "graph" | "memory" | "chat" | "terminal" | "settings";

// Active main view (Rail navigation).
export const activeView = writable<View>("chat");

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
