### Fixed

- "Allow for session" on a tool's approval now lets that tool run for the
  rest of the chat without asking again. It was ignored for tools set to
  Ask, and where it did apply it covered every chat and person until
  Toskar restarted. A tool that asks because the chat read someone else's
  writing still asks each time. `toskarctl chat` offers it as
  `[a]lways in this chat` (#959).
