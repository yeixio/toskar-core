-- Every failure a record shows carries a stable code (#588): the error's
-- own, or one for where it happened, and the values its text needs (JSON).
ALTER TABLE automation_runs ADD COLUMN error_code TEXT;
ALTER TABLE automation_runs ADD COLUMN error_details TEXT;
ALTER TABLE automations ADD COLUMN last_error_code TEXT;
ALTER TABLE automations ADD COLUMN last_error_details TEXT;
ALTER TABLE knowledge_sources ADD COLUMN error_code TEXT;
ALTER TABLE knowledge_sources ADD COLUMN error_details TEXT;
ALTER TABLE tasks ADD COLUMN error_code TEXT;
ALTER TABLE tasks ADD COLUMN error_details TEXT;
