### Fixed

- Models like Qwen 3.5 and 3.8, which keep a full cache in only one layer
  in four, no longer have their memory overestimated. The catalog's
  `full_attention_interval`, or the model file's own layer list, counts
  the cache only for those layers, so the fit, the context gauge's memory,
  and the Auto window are right for them: a Qwen 3.8 27B gets a 32K window
  on a 24 GB card instead of 8K (#962).
