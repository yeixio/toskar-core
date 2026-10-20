-- A camera an automation looks at when it runs (#667), such as a front door
-- through Home Assistant: its picture, taken as the run starts, is shown to
-- a model that can see.
ALTER TABLE automations ADD COLUMN camera_json TEXT;
