-- A notification's web page (#460): what an automation watched, such as a
-- product page, so a tap on the phone opens it without reaching the
-- computer. Link stays the place in Toskar.
ALTER TABLE notifications ADD COLUMN url TEXT;
