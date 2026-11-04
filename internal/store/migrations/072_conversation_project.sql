-- The folder a chat works in (#1017), so a task goes on from any app.
ALTER TABLE conversations ADD COLUMN project TEXT NOT NULL DEFAULT '';
