### Added

- `toskarctl models list` lists the models, smallest first, with their
  download size, fit on the computer, and whether each is installed
  (`--installed`, `--json`). `toskarctl models install <id>` downloads one
  and waits until it's installed, printing its progress, so a computer
  without a desktop can add a model from a terminal (#887).
