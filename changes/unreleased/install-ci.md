### Added

- `install.sh ci` runs Toskar for a CI job on the runner itself: it
  installs the release in a folder, with no root or service, starts it,
  installs a model, and sets `TOSKAR_URL` and `toskarctl` for the job's
  next steps, in GitHub Actions, GitLab, Forgejo, or anywhere with a shell.
  [Toskar in CI](docs/ci.md) puts it together with `toskarctl ask` and a
  key for a CI job.
- Releases include Linux headless archives (amd64 and arm64), the same
  programs as the packages, for installs without root.
