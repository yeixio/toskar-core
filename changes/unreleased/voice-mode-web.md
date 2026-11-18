### Added

- Voice mode in the web app (#795): choose Talk beside Send to talk with
  Toskar out loud. It hears when you stop, answers in a voice as it writes,
  and stops when you talk over it. The user guide's Talk with Toskar out
  loud page explains it.
- Live voice's WebSocket takes the API key as an
  `openai-insecure-api-key.<key>` subprotocol, as OpenAI's browser clients
  send it.
