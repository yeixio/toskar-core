-- Who a profile talks with (#667): public for strangers, such as visitors
-- at a front door; empty for the owner and the people they share with.
ALTER TABLE profiles ADD COLUMN audience TEXT NOT NULL DEFAULT '';
