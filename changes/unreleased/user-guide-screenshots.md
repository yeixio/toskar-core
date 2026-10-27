### Changed

- The user guide's screenshots are captured from the web UI with demo data
  instead of by hand. A page names a screen from
  `docs/user-guide/screens.json`, and `scripts/capture-docs-screenshots.sh`
  saves it. CI captures them on every pull request that touches the
  interface, says which ones changed, and commits them on `main`, so the
  guide at a release shows that release's interface (#882).
