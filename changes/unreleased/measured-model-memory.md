### Added

- Models › Running shows the memory each model really holds, measured
  from its process, against the computer's memory, where it showed the
  catalog's estimate. `GET /api/v1/models/running` gives it as
  `resident_bytes`, and quality results record it (contract 1.90).
