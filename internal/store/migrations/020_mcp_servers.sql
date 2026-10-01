-- MCP tool sources. The spec is stored without secret values: tokens,
-- passwords, and headers live in the secrets directory, never in SQLite.
-- Tools are the list the server gave when last checked, so the catalog is
-- complete at startup without starting every server.

CREATE TABLE IF NOT EXISTS mcp_servers (
    id TEXT PRIMARY KEY,
    spec TEXT NOT NULL,
    enabled INTEGER NOT NULL DEFAULT 1,
    allow_sampling INTEGER NOT NULL DEFAULT 0,
    always_offer INTEGER NOT NULL DEFAULT 0,
    -- JSON object of tool name to allow or ask, for tools the person changed.
    policies TEXT,
    -- JSON array of tools as the server listed them.
    tools TEXT,
    -- JSON: server name and version, instructions, resources, prompts.
    info TEXT,
    status TEXT NOT NULL,
    error TEXT,
    added_at TEXT NOT NULL,
    checked_at TEXT
);
