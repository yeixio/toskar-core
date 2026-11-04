### Changed

- `toskarctl chat` with no model installed now offers the one that suits this
  computer and installs it, instead of an error. While a task works, each step
  is a line in plain words, with a line when a command fails or takes a while
  (contract 1.84, #1017).
