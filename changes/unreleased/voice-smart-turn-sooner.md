### Fixed

- Voice mode answers sooner when you've finished speaking. The check
  for whether you've finished now runs while the pause goes on, so a
  finished question is answered before the whole pause has gone by. The
  check also loads when voice mode opens, and it reads the audio the way
  the model was trained to. Before, it ran only after the pause, which
  made the first sound of an answer up to 2 seconds later. (#1460)
