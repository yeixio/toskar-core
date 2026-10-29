### Fixed

- A clip's length now comes from what you asked for. "Just one second
  long" made two seconds when the model left the length out or passed the
  default. Toskar reads the length from your message, such as "one
  second", "3s", "half a minute", "zwei Sekunden", or "3秒", and a clip
  asked for longer than clips can be is made as long as they can be, with
  a note saying so (#789).
