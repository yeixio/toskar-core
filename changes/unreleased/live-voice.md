### Added

- Live voice mode's connection (#795, contract 1.100): a WebSocket at
  `/api/v1/voice/realtime` that speaks OpenAI's Realtime protocol. Toskar
  hears when you stop talking, answers through your chat, speaks the answer
  a sentence at a time, and stops when you talk over it. Each answer reports
  how long you waited. See [docs/voice.md](docs/voice.md).
