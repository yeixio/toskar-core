### Fixed

- The weekly prompt cache check no longer fails when a CPU reads the warm
  and cold prompts in different batches. Warm and cold must still write the
  same tokens, and each written token's probability must match within 0.2.
  Gaps on tokens the answer doesn't write are reported, not failed on:
  batching alone moves them by up to 0.15 on a CPU. The last turn's question
  no longer opens on a near-tie that the batch size alone decides (#838).
