### Fixed

- Voice mode answers as soon as before when you've finished speaking. The
  check for whether you've finished now runs while you pause, loads when
  voice mode opens, and reads the audio the way its model was trained to.
  Before, it ran after the pause and often heard a finished question as
  unfinished, which made answers start up to 2 seconds later. (#1460)
