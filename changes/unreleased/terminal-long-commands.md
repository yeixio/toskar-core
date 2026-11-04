### Changed

- In a terminal task, a command can run for up to 30 minutes, with its output
  showing as it comes. Ctrl-C stops the work and stays in the chat. Stopping a
  command now stops what it started too, and something left running in the
  background no longer holds up the answer (contract 1.83, #1017).
