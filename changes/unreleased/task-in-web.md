### Added

- Web and desktop chats can do long tasks. An Admin chooses a folder for a chat
  from the composer's options, and the chat then shows each command's output as
  it's written. A task started in any app shows in the others: its steps, its
  output, and its questions, which any of them can answer, and any can stop it
  (#1017).
- `GET /conversations/{id}/turn` says what a running turn has done so far, and
  `tool.decided` says a waiting tool was answered (contract 1.86, #1017).

### Fixed

- In the terminal and in a chat with a folder, a change now asks however the
  message is worded. A message that didn't read as a task, such as "Use the
  terminal tool to...", ran known commands such as `touch` without asking when
  the profile allowed them (#1017).
