### Changed

- Live voice mode keeps Whisper and the voices loaded between turns, so a
  turn no longer waits for them to start (#795).
- Live voice mode speaks in a more natural voice: Kokoro, for English,
  Spanish, French, Italian, and Portuguese. Its model downloads the first
  time; Piper reads until then, and reads the other languages.
