### Changed

- The weekly prompt cache check also checks that a new chat reads its
  system prompt from the cache, even after a chat with another one, as
  llama-server's in-memory prompt cache allows. A profile's long
  instructions are read once, not per chat: with Qwen 2.5 7B, 88 ms
  instead of 1.6 s for 2,100 tokens (#838).
