-- Who accepted a model's license terms in Toskar, and when (#593): a
-- record for the Admin, kept when the model is removed.
CREATE TABLE model_license_acceptances (
  id           INTEGER PRIMARY KEY AUTOINCREMENT,
  model_id     TEXT NOT NULL,
  license_id   TEXT NOT NULL,
  license_name TEXT NOT NULL,
  license_url  TEXT NOT NULL,
  person_id    TEXT NOT NULL,
  person_name  TEXT NOT NULL DEFAULT '',
  accepted_at  TEXT NOT NULL
);
CREATE INDEX model_license_acceptances_model ON model_license_acceptances(model_id, person_id);
