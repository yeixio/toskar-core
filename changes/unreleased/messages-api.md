### Added

- Coding agents that speak Anthropic's Messages format can use a model on
  this computer. `POST /v1/messages` converts their system prompt, tool
  calls, tool results, pictures, and tools to the model's format and the
  answer back, streamed as Anthropic's events or not, and
  `POST /v1/messages/count_tokens` estimates a request. It's raw, with the
  same permission as `raw:<model>`, and takes the key as `x-api-key` too
  (contract 1.72, #843).
