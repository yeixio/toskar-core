### Added

- Coding agents and editors can drive a model directly. On
  `/v1/chat/completions`, `"model": "raw:<model id>"` sends the request to
  a model on this computer as it came, with the agent's own `tools`,
  `tool_choice`, `stop`, and `logprobs`, and returns the model's
  `tool_calls`, streamed or not, without Toskar's assistant. Its window is
  sized as other requests' are. A request from this computer may do this
  without a key; for a key, an Admin turns on Drive models directly, for
  coding agents, in its permissions (contract 1.71, #843).
