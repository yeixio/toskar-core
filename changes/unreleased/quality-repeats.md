### Changed

- The quality set can run each case more than once and say how sure its
  pass rate is. `TOSKAR_QUALITY_REPEATS` runs each case with a seed per
  run, and the report gives the pass rate with a 95% interval, the flaky
  cases, and, from three or more runs, pass@3 and majority of 3.
  `TOSKAR_QUALITY_SAVE` keeps every answer, and `TOSKAR_QUALITY_RESCORE`
  checks them again after a check changes, without a model. A report says
  when a run didn't cover every case. The weekly real-model run uses three
  repeats (#834).

### Fixed

- A real model's turn that failed, such as one where it wrote no answer,
  failed the whole quality run even when enough cases passed. The 1.11.2
  release stopped this way at 64 of 70 (91%), over its 90% bar. It now
  counts as one failed case (#834).
- A long chat could fail with "Context size has been exceeded" when two
  requests to its model ran at once, such as a turn and the chat's summary,
  or Deliberate's drafts. llama-server ran four requests sharing one
  window's room while telling each it had all of it. A model now takes one
  request at a time with the whole window, and the others wait their turn.
  The weekly runs caught it on Qwen 2.5 7B and 32B (#834).
- A long chat's summary could be an answer to its last question instead of
  a summary, so it lost what was said earlier, such as where the person's
  sister lives. The summary request now asks for the summary after the
  conversation, and to keep what the person said about themselves and the
  people and places in their life (#834).
