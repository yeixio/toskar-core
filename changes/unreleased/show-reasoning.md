### Added

- A reasoning model's thinking is no longer a silent wait (#701). The chat says **Reasoning…** while it thinks, the answer's steps say how long it thought ("Thought for 12 s before answering"), and **Thinking** under the answer opens what it thought. The model's thinking was thrown away before.
- `chat.reasoning` events (`started`, then `done` with `ms`), the answer's `meta.reasoning` (`ms` and `text`), and `include_reasoning` on the OpenAI-compatible API, which returns the thinking as `reasoning_content` (#701, contract 1.43). External servers that send `reasoning` or `reasoning_content` work too.
