### Added

- Auto-compact has a name and controls (#342):
  - A long chat's older messages are sent to the model as a summary, and
    every message stays saved. Now you can read the summary and correct
    it, compact a chat now, start a new chat from its summary, or keep a
    chat verbatim.
  - Settings › Chat chooses when it starts: Early, Normal, or Late.
  - A turn that wouldn't fit compacts first, instead of leaving the oldest
    messages out.
  - API: `GET` and `PUT /api/v1/conversations/{id}/summary`, `POST
    …/compact`, `POST …/from-summary`, a chat's `compact_off`, and the
    `compact_at` setting (contract 1.35).
