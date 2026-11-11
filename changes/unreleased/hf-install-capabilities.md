### Fixed

- Models installed from Hugging Face or by address now say whether they
  call tools and think, with their family and context window, read from
  the model file. Models already installed this way are read once at
  start, so a model such as Qwen 3.8 shows up in editor model pickers that
  list only models that call tools.
