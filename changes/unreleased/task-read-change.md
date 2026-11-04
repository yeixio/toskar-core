### Changed

- A task in the terminal now asks before it changes anything, whatever the
  profile allows for chat: writing files, `git add` and `git commit`, installs,
  `make` and `npm run`, and reading outside the project. Reading in the project
  and running named tests and builds still don't ask (#1017).
