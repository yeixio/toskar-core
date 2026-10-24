### Fixed

- The API description (`api/openapi.yaml`) gives the client contract's
  version (1.55, not 1.4.0), and no longer lists one response twice.
- The API description now describes people, sign-in, invites, roles and
  profile pins fully, with named types, including fields it left out
  (an API key's pinned profile, and a person's email and portal).
- It describes connecting a device, too: the pairing code, the paired
  device's key and certificate, and its route for access from anywhere.
- It describes every type in the client contract (messages and their
  sources, steps, files and deliberation, run traces, events,
  notifications, and errors) from the code's own fields and comments, and
  the routes for chats, their summaries, and versions use them.
- It describes the chat stream's events (token, done, error_code, and
  error) and the events stream, so a client can read each event as a type.
  Chat answers' metrics include `cached_tokens` (contract 1.56).
- It describes models, runtimes, and ratings fully: the model list,
  recommendations, browsing, installing, importing, running models, and
  where a model can run.
- It describes automations, tasks, knowledge, training, portals, joining
  computers, logs, diagnostics, caches, connectors, MCP servers, news,
  and updates with named types, from the code.
- It describes memory, notifications and their destinations, API keys,
  webhooks, speech, tools, and more of training with named types; every
  answer the tests see now matches it.
- Every JSON answer in the API description is a named type, including
  diagnostics, transcription, tool activity and providers, and the
  OpenAI-compatible model list and chat completions, ready for SDKs to be
  generated from.
- Every route in the API description is tagged by area, such as Chat,
  Models, or Automations, so the API reference and SDKs group them.
