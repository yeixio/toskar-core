-- Profiles & Orchestration (spec §40): a profile's own effort, planning,
-- workers, parallelism, verification, memory, context budget, fallback,
-- and time limit. JSON; NULL keeps every default.
ALTER TABLE profiles ADD COLUMN orchestration_json TEXT;
