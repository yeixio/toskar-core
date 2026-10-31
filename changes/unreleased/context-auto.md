### Changed

- Chat models start with a bigger context window where it fits. Toskar
  used 8,192 tokens for every model; now each starts with 8,192, 16,384,
  or 32,768, the largest at which it's still an excellent fit and stays
  in graphics memory, and never past the model's own window. Small models
  on most computers get 32,768, so they read about four times as much at
  once, and a reasoning model has room to think. A model that only just
  fits keeps 8,192. The fit estimate in Models counts the bigger window,
  and a model running with another window is started again (#962).
