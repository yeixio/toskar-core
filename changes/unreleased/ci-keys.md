### Added

- **For a CI job** on the API Access page makes a key that only asks: it
  reaches the OpenAI-compatible chat, with no memories, knowledge, or
  tools, so what a job sends, such as an issue's text or a build log, can't
  reach your data or make anything happen. Anything else answers
  `403 KEY_CHAT_ONLY`, MCP included. A key's permissions can also be set
  to chat only later, and `POST /api/v1/api-keys` takes `"preset": "ci"`
  or `permissions` from the start (contract 1.57).
