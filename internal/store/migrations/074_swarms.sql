-- Swarms (#1135): several agents working at once, each in a chat of its own
-- in the coordinator chat's project. A swarm belongs to the person who
-- started it, as chats do.
CREATE TABLE swarms (
  id              TEXT PRIMARY KEY,
  person_id       TEXT NOT NULL DEFAULT 'owner',
  project_id      TEXT NOT NULL,
  conversation_id TEXT NOT NULL,             -- the coordinator's chat
  mode            TEXT NOT NULL,             -- split | race | spread
  status          TEXT NOT NULL,             -- running | merging | done | stopped | failed
  request         TEXT NOT NULL DEFAULT '',
  budget_json     TEXT NOT NULL DEFAULT '{}',
  result_json     TEXT NOT NULL DEFAULT '{}', -- merge outcome, winner, totals
  created_at      TEXT NOT NULL,
  finished_at     TEXT NOT NULL DEFAULT ''
);
CREATE INDEX swarms_person ON swarms(person_id, created_at);
ALTER TABLE conversations ADD COLUMN swarm_id TEXT NOT NULL DEFAULT '';
ALTER TABLE conversations ADD COLUMN swarm_role TEXT NOT NULL DEFAULT ''; -- '' (coordinator or none) or agent:N
