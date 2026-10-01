-- Files attached to chats and files the assistant produced (AI experience
-- spec §28). The bytes live under the data directory; this is the index.

CREATE TABLE IF NOT EXISTS artifacts (
    id TEXT PRIMARY KEY,
    conversation_id TEXT REFERENCES conversations(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    mime_type TEXT NOT NULL,
    kind TEXT NOT NULL,
    size_bytes INTEGER NOT NULL,
    producer TEXT NOT NULL,
    source_task TEXT,
    path TEXT NOT NULL,
    created_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS artifacts_conversation ON artifacts(conversation_id, created_at);
