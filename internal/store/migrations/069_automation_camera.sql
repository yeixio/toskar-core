-- The cameras an automation looks at as each run starts (#667), such as a
-- driveway's through Home Assistant: their pictures, taken as the run
-- starts, are shown to a model that can see.
ALTER TABLE automations ADD COLUMN camera_json TEXT;
