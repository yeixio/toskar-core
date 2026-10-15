-- Managing people without a browser (#592): an email address, a password
-- an Admin set that must be changed at the next sign-in, and a log of who
-- changed whom.
ALTER TABLE people ADD COLUMN email TEXT;
ALTER TABLE people ADD COLUMN must_change INTEGER NOT NULL DEFAULT 0;
CREATE UNIQUE INDEX IF NOT EXISTS people_email ON people (email COLLATE NOCASE) WHERE email IS NOT NULL;

CREATE TABLE IF NOT EXISTS people_activity (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    at TEXT NOT NULL,
    actor_id TEXT NOT NULL,
    actor_name TEXT NOT NULL,
    target_id TEXT NOT NULL,
    target_name TEXT NOT NULL,
    action TEXT NOT NULL,
    detail TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS people_activity_at ON people_activity (at);
