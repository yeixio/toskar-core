### Added

- Choose a model's context window. With Advanced mode on, a model's More
  actions menu has Context window: Auto, or a size from 4K up to what the
  model was made for, each marked with how well the model fits at it. A
  bigger window suits long documents and coding tools; a smaller one frees
  memory. A running model reloads with it on its next message.
  `PUT /models/{id}/context` sets it, and models list `context_window`,
  `context_choice`, and `context_options` (contract 1.69, #962).
