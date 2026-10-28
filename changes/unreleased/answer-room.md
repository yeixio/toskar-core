### Changed

- A model that thinks before it answers gets more room for its answer when
  a chat's earlier messages are fitted into its window: a quarter of the
  window (2,048 to 16,384 tokens) instead of a flat 512, or what the
  request's `max_tokens` asks for, and the usual room when thinking is off.
  Other models get a sixteenth of the window (512 to 4,096) (#840).
- Toskar knows a model thinks before it answers from the model file: its
  chat template, or its name for DeepSeek R1's distills. A Qwen 3 or gpt-oss
  added from a file or from Hugging Face counts, and a model that's only
  good at reasoning, such as Qwen 2.5 32B, doesn't. Models carry `thinks`
  in their capabilities (contract 1.63, #840).
- `/v1/chat/completions` checks that a request naming a model fits it
  before running: `max_tokens` as large as the model's window, or messages
  and `max_tokens` clearly larger, answer `400` with `CONTEXT_TOO_LONG` and
  the field to change, without starting the model (#840).

### Fixed

- `/v1/chat/completions` answered every failure while answering as `500
  CHAT_FAILED` with the runtime's text. It now gives the stable code, as chat
  streams do, and a conversation too long for the model is a `400
  CONTEXT_TOO_LONG` the caller can fix (#840).
