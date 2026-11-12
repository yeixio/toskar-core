-- Projects (#1136): chats kept together with what they share. A project
-- belongs to the person who made it. project_id is the new thing; a chat's
-- folder stays in conversations.project.
CREATE TABLE projects (
  id           TEXT PRIMARY KEY,
  person_id    TEXT NOT NULL DEFAULT 'owner',
  name         TEXT NOT NULL,
  instructions TEXT NOT NULL DEFAULT '',
  folder       TEXT NOT NULL DEFAULT '',
  profile_id   TEXT NOT NULL DEFAULT '',
  model_id     TEXT NOT NULL DEFAULT '',
  memory_off   INTEGER NOT NULL DEFAULT 0,
  knowledge_json TEXT NOT NULL DEFAULT '[]',
  created_at   TEXT NOT NULL,
  updated_at   TEXT NOT NULL
);
CREATE INDEX projects_person ON projects(person_id, updated_at);
ALTER TABLE conversations ADD COLUMN project_id TEXT NOT NULL DEFAULT '';
CREATE INDEX conversations_project ON conversations(project_id, updated_at);
ALTER TABLE knowledge_sources ADD COLUMN project_id TEXT NOT NULL DEFAULT '';
ALTER TABLE memories ADD COLUMN project_id TEXT NOT NULL DEFAULT '';
