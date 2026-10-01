-- What each API key may do with the assistant (spec §62): use memory and
-- connected knowledge, which tools, and whether it may choose placement.
-- JSON; NULL means the defaults.
ALTER TABLE api_keys ADD COLUMN permissions TEXT;
