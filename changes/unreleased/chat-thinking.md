### Added

- A chat can turn a reasoning model's thinking off, for answers that start
  straight away. With a model that thinks before it answers, the chat's
  options have Thinking, On or Off, and the choice is remembered. `POST
  /chat` takes `"thinking": "off"`, as `/v1/chat/completions` takes
  `reasoning_effort: "none"` (contract 1.61, #840).
