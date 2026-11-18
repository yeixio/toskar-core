-- The actions an automation may take on its own (#586), such as Home
-- Assistant's light.turn_on on light.porch: a call that matches one runs
-- without asking, even from what a trigger delivered.
ALTER TABLE automations ADD COLUMN action_json TEXT;
