### Changed

- The weekly prompt cache check also runs Gemma 3 4B, whose sliding window
  is the catalog's smallest, so a llama.cpp update that stops reusing such a
  model's cache fails it. Its chat is now about 2,300 tokens, longer than
  that window, with the model writing its own answers, and warm and cold
  answers are compared by probability (within 0.05) rather than log
  probability, which rounding moves for unlikely words. Measured on
  llama.cpp b11149, every catalog model tried reused 97–99% of a long chat,
  and `--swa-full` added nothing, so Toskar doesn't turn it on (#838).
