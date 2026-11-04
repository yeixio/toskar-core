### Fixed

- `git stash`, `git switch -f` and `--discard-changes`, `git commit --amend`,
  `git pull --rebase` and `--force`, and `git fetch --prune` no longer run
  without asking in a chat that allows known commands. A new set of attempts to
  get around the terminal's safeguards runs in CI and found them (#1017).
