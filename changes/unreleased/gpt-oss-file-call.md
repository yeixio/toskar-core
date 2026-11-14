### Fixed

- A file asked for in chat holds what the model wrote, when the model
  made the file with its own tool call, as gpt-oss does. The file held the
  call's text instead, or nothing at all.
