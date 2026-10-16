-- Push to the Toskar apps (#460): a paired device's sealed relay handle,
-- by its key. The relay made the handle from the device's push token; this
-- computer never sees the token. Revoking the key ends its pushes.
CREATE TABLE IF NOT EXISTS push_registrations (
    key_id TEXT PRIMARY KEY,
    person_id TEXT NOT NULL DEFAULT '',
    handle TEXT NOT NULL,
    platform TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
