-- A device's push registration goes with its key (#460): revoking the key,
-- from Devices, API Access, or removing a person, removes it, and ones left
-- by keys revoked before this are cleared.
DELETE FROM push_registrations WHERE key_id IN (SELECT id FROM api_keys WHERE revoked_at IS NOT NULL);
DELETE FROM push_registrations WHERE key_id NOT IN (SELECT id FROM api_keys);
CREATE TRIGGER IF NOT EXISTS push_follows_key
AFTER UPDATE OF revoked_at ON api_keys
WHEN NEW.revoked_at IS NOT NULL
BEGIN
    DELETE FROM push_registrations WHERE key_id = NEW.id;
END;
