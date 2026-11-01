### Added

- The chat API can send an answer as it's written. A client that sends
  `"live": true` with `"stream": true` gets the text as the model writes it,
  and a `replace` event when a tool call, a retry, or a check changes what was
  shown. Without it, the answer still comes whole (contract 1.77).
