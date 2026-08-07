-- Phase 1 initial schema.
-- Design goals: append-only durability, forward-compatible migrations,
-- conversations keyed by UUID (portable/exportable), messages by autoincrement
-- (preserves intra-conversation ordering and is index-friendly).

-- Conversations: one chat thread.
CREATE TABLE conversations (
    id           TEXT PRIMARY KEY,              -- UUID v4
    title        TEXT NOT NULL DEFAULT '',      -- auto-generated from first message (F1.5)
    task_type    TEXT NOT NULL DEFAULT '',      -- daily_development | hard_problems | ... (Phase 3)
    model        TEXT NOT NULL DEFAULT '',      -- last model used in this conversation
    created_at   INTEGER NOT NULL,              -- unix epoch millis
    updated_at   INTEGER NOT NULL               -- unix epoch millis; drives sidebar ordering
);

CREATE INDEX idx_conversations_updated_at ON conversations (updated_at DESC);

-- Messages: one turn (user or assistant). Append-only.
CREATE TABLE messages (
    id                INTEGER PRIMARY KEY AUTOINCREMENT,
    conversation_id   TEXT NOT NULL,
    role              TEXT NOT NULL,             -- 'user' | 'assistant' | 'system'
    content           TEXT NOT NULL DEFAULT '',
    model             TEXT NOT NULL DEFAULT '',  -- model that produced an assistant message
    prompt_tokens     INTEGER NOT NULL DEFAULT 0,
    completion_tokens INTEGER NOT NULL DEFAULT 0,
    -- 'complete' once fully persisted; 'streaming'/'interrupted' capture partial
    -- assistant output so nothing is lost on stop/crash (NFR 6.3).
    status            TEXT NOT NULL DEFAULT 'complete',
    created_at        INTEGER NOT NULL,          -- unix epoch millis
    FOREIGN KEY (conversation_id) REFERENCES conversations (id) ON DELETE CASCADE
);

CREATE INDEX idx_messages_conversation ON messages (conversation_id, id);
