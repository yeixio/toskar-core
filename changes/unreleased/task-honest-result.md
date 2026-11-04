### Added

- A task in the terminal now keeps its own record of what it checked and
  changed. If the last check failed, nothing ran to check a change, or test
  files changed, the model is sent back once, and whatever is still in doubt
  shows under the answer (#1017).
