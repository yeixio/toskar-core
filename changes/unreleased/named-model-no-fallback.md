### Changed

- A request to `/v1/chat/completions` that names a model gets that model,
  or an error saying why. When the model couldn't answer, such as one that
  failed to load, another installed model used to answer in its place
  without saying so, which made results that compare or measure models
  wrong. `auto` and profiles still fall back, and `"toskar": {"fallback":
  true}` allows it for a named model too (contract 1.59). `toskarctl ask
  --model` gets the model it names.
