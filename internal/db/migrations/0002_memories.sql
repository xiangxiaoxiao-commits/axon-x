-- Phase 4 semantic memory.
-- One memory row per conversation: a generated summary plus its embedding.
-- Vectors are stored as a raw little-endian float32 BLOB and scored with
-- cosine similarity in Go (sufficient for a single-user desktop archive of a
-- few thousand memories; a vector index can replace this later without a
-- schema change).

CREATE TABLE memories (
    id               INTEGER PRIMARY KEY AUTOINCREMENT,
    conversation_id  TEXT NOT NULL UNIQUE,          -- one memory per conversation (upserted)
    summary          TEXT NOT NULL DEFAULT '',      -- LLM-generated summary (topic/outcome)
    embedding        BLOB NOT NULL,                 -- []float32 little-endian
    embed_model      TEXT NOT NULL DEFAULT '',      -- model that produced the embedding
    dim              INTEGER NOT NULL DEFAULT 0,     -- vector dimension (sanity check on decode)
    created_at       INTEGER NOT NULL,              -- unix epoch millis
    updated_at       INTEGER NOT NULL,
    FOREIGN KEY (conversation_id) REFERENCES conversations (id) ON DELETE CASCADE
);

CREATE INDEX idx_memories_conversation ON memories (conversation_id);
