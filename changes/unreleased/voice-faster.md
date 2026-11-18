### Fixed

- Live voice mode keeps Toskar's answers in the voice chat, so the next
  turn hears them and the chat shows both sides (#795). The answer was
  spoken but stopped before it was saved.
- Live voice answers start well over a second sooner on a Mac, about 0.9
  seconds from the end of a question to the first word heard, from 2.3
  (#795). Each chat turn rebuilt what Toskar knows about the computer's
  models and hardware, looking at the hardware five times; it now keeps
  that between turns, and refreshes its look at the hardware in the
  background instead of making a turn wait for it.
- Spoken answers are no longer asked for Markdown, which contradicted the
  spoken style (#795).
