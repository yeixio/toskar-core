-- Mimir database and API sources. remote_json says how to reach the source
-- (driver and query, or URL) and how often to refresh it. Passwords,
-- connection strings, and request headers are kept in the secrets directory,
-- not here. For these sources, signature holds the time of the last fetch.

ALTER TABLE knowledge_sources ADD COLUMN remote_json TEXT;
