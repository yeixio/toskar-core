### Added

- A reasoning model can answer without thinking first, which is much
  faster for a question that doesn't need it: `reasoning_effort: "none"`
  on `/v1/chat/completions` (contract 1.60) and `toskarctl ask
  --no-thinking`. Toskar uses the model's own switch where its chat
  template has one, such as Qwen 3's, and otherwise a thinking budget of
  none, as for DeepSeek R1's distills, which always think. It also works
  on a paired computer, and a model that doesn't think is unaffected.
