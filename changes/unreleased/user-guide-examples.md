### Changed

- The user guide's code examples are tested. A code block with a test id
  runs in CI against a throwaway Core, with the repository's other examples,
  in sh (with the real curl and toskarctl), Python, TypeScript, or Go. Its
  output must match what the page shows, with `…` for what changes. A test
  id on a block CI can't run fails the user guide check (#882).
