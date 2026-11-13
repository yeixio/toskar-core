### Added

- Swarms for coding agents: `swarm:<profile>` as the model on
  `/v1/chat/completions`, `/v1/messages`, and `/v1/responses`. A request
  that brings the agent's own tools, as a subagent's does, goes as it is to
  the least busy of the profile's swarm models and computers, on this
  computer or a paired one, and its answer comes back unchanged, so
  subagents started at once run at once instead of waiting on one model. A
  request without tools runs a whole swarm and answers with its answer,
  with the agents' progress as reasoning. `/v1/models` lists `swarm:<id>`
  for profiles whose strategy is swarm (contract 1.88, #1135).
- `toskarctl connect claude-code --swarm <profile>` points Claude Code's
  subagents at a profile's swarm, and leaves its own model as it was.
- MCP's `run_swarm` tool runs a swarm from another app and returns its
  answer and a link to its chat.
