-- The tokens speculative decoding guessed ahead for a reply, and how many
-- the model kept (#835), so Performance can say how often guesses pay off.
ALTER TABLE generation_metrics ADD COLUMN draft_tokens INTEGER NOT NULL DEFAULT 0;
ALTER TABLE generation_metrics ADD COLUMN draft_accepted INTEGER NOT NULL DEFAULT 0;
