### Fixed

- The weekly prompt cache check no longer fails when a CPU reads the warm
  and cold prompts in different batches. Warm and cold must write the same
  tokens, each with the same probability within 0.2, until they part at a
  near-tie that batching can tip; gaps on tokens neither writes are
  reported, not failed on. Batching alone moves probabilities by up to 0.15
  on a CPU and, at a near-tie, the word written (#838).

### Changed

- The prompt cache check also asks, warm and cold, which city the store is
  in, which only the chat's first sentence says. A cache that lost the
  start of a chat answers another city, so this catches it on every model,
  Gemma 3's sliding window included (#838).
