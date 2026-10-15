-- Auto-compact (#342): a summary the person corrected, which later
-- summaries keep, and a chat kept verbatim, never compacted.
ALTER TABLE conversation_summaries ADD COLUMN edited INTEGER NOT NULL DEFAULT 0;
ALTER TABLE conversations ADD COLUMN compact_off INTEGER NOT NULL DEFAULT 0;
