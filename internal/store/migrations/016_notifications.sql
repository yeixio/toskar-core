-- Gjallarhorn: durable notifications and their deliveries. A notification is
-- stored first; each channel's attempt is recorded separately, so a failed
-- delivery never erases the notification.

CREATE TABLE IF NOT EXISTS notifications (
    id TEXT PRIMARY KEY,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    source_type TEXT NOT NULL,
    source_id TEXT,
    category TEXT NOT NULL,
    severity TEXT NOT NULL,
    title TEXT NOT NULL,
    body TEXT NOT NULL,
    link TEXT,
    dedupe_key TEXT,
    read_at TEXT,
    dismissed_at TEXT
);

CREATE INDEX IF NOT EXISTS notifications_recent ON notifications(dismissed_at, created_at);
CREATE INDEX IF NOT EXISTS notifications_dedupe ON notifications(dedupe_key, created_at);

CREATE TABLE IF NOT EXISTS notification_deliveries (
    id TEXT PRIMARY KEY,
    notification_id TEXT NOT NULL REFERENCES notifications(id) ON DELETE CASCADE,
    channel TEXT NOT NULL,
    status TEXT NOT NULL,
    attempts INTEGER NOT NULL DEFAULT 0,
    last_attempt_at TEXT,
    delivered_at TEXT,
    error TEXT
);
