### Changed

- Answers that repeat or edit text already in the chat come faster: chat
  models guess the next words from the chat and check several at once
  (llama.cpp's n-gram speculation), when the installed llama.cpp has it.
  On a 7B model on a Mac, editing pasted code was 42% faster and editing a
  pasted letter 99% faster, other answers 0 to 7% faster, and every answer
  was the same as before. It needs no extra model or memory. A run's
  details show how many guessed words were kept, and `TOSKAR_SPECULATIVE=off`
  turns it off (#835).
