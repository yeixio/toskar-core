### Added

- `toskarctl ask` sends one prompt and prints the answer, for scripts and
  CI jobs: `gh issue view 12 | toskarctl ask "Summarize this bug"`. Piped
  text is sent as data apart from the prompt, `--json-schema` answers with
  JSON that fits a schema, and memory, knowledge, and tools are off unless
  asked for. It's built on the official Go SDK.

### Fixed

- An API request that turns tools off can use a model on the external
  server again. It was refused as if its profile kept work on this
  computer.
