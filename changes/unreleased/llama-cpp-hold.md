### Changed

- When the nightly llama.cpp canary finds a release computes differently
  from the known-good one, new installs take the newest release before it
  until a newer one passes (`runtimes/llama-cpp.json`). When the hold can't
  be read, installs take the newest release, as before (#833).
