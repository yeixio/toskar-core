### Fixed

- A small model that numbers its tool calls, such as
  `{"tool_call":{"id":"1",…}}`, gets its call run when only one tool
  fits it, or is shown the call with each tool that fits so it picks
  one. A turn that still ends on a call is answered without tools.
  Before, Qwen 3.5 4B asked about coffee shops repeated the numbered
  call until the turn ended with "The model didn't write an answer"
  (#1213).
