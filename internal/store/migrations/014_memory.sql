-- Muninn: persistent memory and long-conversation context.

CREATE TABLE IF NOT EXISTS memories (
    id TEXT PRIMARY KEY,
    content TEXT NOT NULL,
    category TEXT NOT NULL,
    source_type TEXT NOT NULL,
    source_ref TEXT,
    enabled INTEGER NOT NULL DEFAULT 1,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE VIRTUAL TABLE IF NOT EXISTS memories_fts USING fts5(
    content,
    memory_id UNINDEXED,
    tokenize = 'unicode61 remove_diacritics 2'
);

-- Memory Off for one conversation. Memory is on unless this is set.
ALTER TABLE conversations ADD COLUMN memory_off INTEGER NOT NULL DEFAULT 0;

-- A summary of a conversation's older messages, used when the full history
-- no longer fits the model's window. The messages themselves are kept.
CREATE TABLE IF NOT EXISTS conversation_summaries (
    conversation_id TEXT PRIMARY KEY REFERENCES conversations(id) ON DELETE CASCADE,
    through_message_id TEXT NOT NULL,
    through_created_at TEXT NOT NULL,
    message_count INTEGER NOT NULL,
    summary TEXT NOT NULL,
    model_id TEXT,
    updated_at TEXT NOT NULL
);
