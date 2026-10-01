-- What left this computer (spec §63): each web search, page fetch, paired
-- computer, external server, or connected service a run sent data to.
CREATE TABLE IF NOT EXISTS egress (
    id TEXT PRIMARY KEY,
    at TEXT NOT NULL,
    kind TEXT NOT NULL,
    destination TEXT NOT NULL,
    detail TEXT NOT NULL DEFAULT '',
    source TEXT NOT NULL DEFAULT '',
    conversation_id TEXT,
    task_id TEXT
);
CREATE INDEX IF NOT EXISTS idx_egress_at ON egress(at);
CREATE INDEX IF NOT EXISTS idx_egress_conversation ON egress(conversation_id);

-- Memories and knowledge sources marked this computer only are never sent
-- to a paired computer, so work that uses them runs here.
ALTER TABLE memories ADD COLUMN local_only INTEGER NOT NULL DEFAULT 0;
ALTER TABLE knowledge_sources ADD COLUMN local_only INTEGER NOT NULL DEFAULT 0;
