### Changed

- The weekly quality run checks the prompt cache against a real model:
  how much of a chat's later turn comes from the cache (at least 80%), and
  that an answer from a warm cache matches a cold one, token for token,
  with top-5 log probabilities within 0.05. The report gives the tokens
  read and cached each turn. `TOSKAR_QUALITY_RUN` picks the tests
  `scripts/quality-model.sh run` runs (#838).
