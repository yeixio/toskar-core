### Added

- A request that needs a bigger context window gets one. On
  `/v1/chat/completions`, a request that names a model and needs more than
  the model runs with makes it start again with the smallest size that
  holds it, up to its own window, if it still fits. Coding tools can ask up
  front with `toskar.context_window`. When nothing fits, the request answers
  `400 CONTEXT_TOO_LONG` naming the field. The model goes back to its usual
  window once it stops (contract 1.70, #962).

### Fixed

- A request to a model that wasn't running was checked against 8,192
  tokens, though the model would start with a bigger window (#962).
