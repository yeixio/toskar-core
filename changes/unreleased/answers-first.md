### Changed

- A catalog model can answer first: it thinks only when a chat asks it to.
  Smaller reasoning models such as Qwen 3.5 spent their whole window
  thinking and wrote no answer. For these models the chat's Thinking
  option starts Off, and the choice is remembered apart from the other
  models'. `POST /chat` takes `"thinking": "on"`, and any
  `reasoning_effort` other than `none` asks them to think. Model
  capabilities have `answers_first` (contract 1.68, #593).
