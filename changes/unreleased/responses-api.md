### Added

- Coding agents that speak OpenAI's Responses format can use a model on
  this computer. `POST /v1/responses` converts their instructions, input
  items, function calls and their outputs, pictures, and function tools to
  the model's format, and the answer back as output items, streamed as the
  `response.*` events or not. It's raw, with the same permission as
  `raw:<model>`. Toskar keeps no responses, so an agent sends the whole
  conversation each time (contract 1.73, #843).
