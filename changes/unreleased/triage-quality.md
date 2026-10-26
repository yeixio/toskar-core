### Added

- A triage set in the quality tests: 30 of Toskar's own issues with the
  labels maintainers gave them. It measures how well a model suggests them
  the way the Triage workflow asks (#455), as precision, recall, and how
  often the issue's type is right, and it runs in the weekly real-model
  run. `toskarctl ask`'s fencing of data moved to `internal/askdata`, so
  the test sends exactly what the workflow does.
