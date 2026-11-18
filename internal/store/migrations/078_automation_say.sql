-- Where an automation speaks each run's answer (#667), such as a Reolink
-- doorbell's speaker, with its quiet hours.
ALTER TABLE automations ADD COLUMN say_json TEXT;
