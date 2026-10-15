-- What each role and person may use (#589): a role's changes from the
-- shipped defaults, and a person's Allow or Block in place of their role's.
ALTER TABLE people ADD COLUMN capabilities_json TEXT;

CREATE TABLE IF NOT EXISTS role_capabilities (
    role TEXT PRIMARY KEY,
    capabilities_json TEXT NOT NULL
);
