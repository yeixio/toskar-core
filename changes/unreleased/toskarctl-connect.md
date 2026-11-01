### Added

- `toskarctl connect claude-code`, `codex`, `opencode`, or `aider` prints the
  settings for that coding agent to use a model on Toskar, with its own
  tools, and `--write` saves them: for Claude Code and OpenCode in the
  project folder, and for Codex as a `toskar` profile, so the agent's usual
  settings stay. It picks the best installed model for coding unless
  `--model` names one (#959).

### Fixed

- Claude Code's requests to `/v1/messages` were refused: a system turn
  partway through the conversation now becomes instructions, and the tools
  it adds join the request's. A raw request's window counts at most 8,192
  tokens of its `max_tokens`, since agents ask for 32,000 as a ceiling, and a
  model with a 32K window was refused (#959).
