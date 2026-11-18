### Fixed

- Live voice mode keeps Toskar's answers in the voice chat, so the next
  turn hears them and the chat shows both sides (#795). The answer was
  spoken but stopped before it was saved.
- Live voice answers start about a second sooner on a Mac (#795). Each chat
  turn rebuilt what Toskar knows about the computer's models and hardware,
  looking at the hardware five times; it now keeps that between turns, and
  one look at the hardware serves a turn.
- Spoken answers are no longer asked for Markdown, which contradicted the
  spoken style (#795).
