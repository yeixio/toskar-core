-- Gungnir audit (§13): every tool call, whatever became of it. summary is
-- the short description shown for a call (a query, an address, a path),
-- never a file's contents or a credential. Records expire with run records.

CREATE TABLE IF NOT EXISTS tool_runs (
    id TEXT PRIMARY KEY,
    at TEXT NOT NULL,
    tool_id TEXT NOT NULL,
    status TEXT NOT NULL,
    approval TEXT,
    duration_ms INTEGER NOT NULL DEFAULT 0,
    summary TEXT,
    error TEXT,
    source TEXT,
    conversation_id TEXT,
    task_id TEXT
);

CREATE INDEX IF NOT EXISTS tool_runs_at ON tool_runs(at);
CREATE INDEX IF NOT EXISTS tool_runs_tool ON tool_runs(tool_id, at);
