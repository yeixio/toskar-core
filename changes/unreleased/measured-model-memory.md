### Added

- A running model's measured memory: `GET /api/v1/models/running` gives
  `resident_bytes`, the system memory its process holds, beside the
  catalog's estimate, and quality results record it (contract 1.90).
