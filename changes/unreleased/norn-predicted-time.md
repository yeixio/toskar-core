### Changed

- With paired computers, a chat goes to the computer expected to answer
  soonest. Toskar learns how fast each one reads prompts and writes answers
  with each model from the answers it gives and from benchmarks, and counts
  the work in flight on each, so a long request goes where prompts are read
  fastest and a short one where answers are written fastest. A chat's next
  message stays on the computer that has the chat loaded unless another
  would be much faster. The placement reason shows each part (#836).
