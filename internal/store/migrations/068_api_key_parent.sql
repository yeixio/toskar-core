-- A companion device's key remembers the device that connected it, such as
-- a watch its phone (#766), so a phone disconnecting itself takes its watch
-- along. Empty for every other key.
ALTER TABLE api_keys ADD COLUMN parent_key_id TEXT NOT NULL DEFAULT '';
