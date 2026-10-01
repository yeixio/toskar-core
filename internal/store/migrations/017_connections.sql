-- Connected services (spec §32). Only what is safe to show is stored here:
-- which account a service is connected as and when it was checked. The
-- credentials themselves live in the secrets directory, never in SQLite.

CREATE TABLE IF NOT EXISTS connections (
    service_id TEXT PRIMARY KEY,
    account TEXT NOT NULL DEFAULT '',
    connected_at TEXT NOT NULL,
    checked_at TEXT NOT NULL,
    status TEXT NOT NULL,
    error TEXT,
    -- Words learned from the service, such as device names, that show a
    -- message is about it. JSON array.
    cues TEXT
);
