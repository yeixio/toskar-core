### Added

- Diagnostics shows how much of a chat's later turns each model skipped
  reading again, from its prompt cache, over the last week. A model that
  reads most of a long chat again every turn is marked low, since its
  answers start later as a chat grows. `GET /diagnostics/prompt-cache`
  gives the same per model (contract 1.62). The user guide's new page, Why
  do answers in a long chat start slowly?, explains it (#838).
