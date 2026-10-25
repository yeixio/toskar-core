### Added

- `examples/ci` has workflows that ask Toskar as a CI step: label new
  issues (GitHub Actions and Forgejo) and explain a failed build from its
  log (GitHub Actions and GitLab CI). Each one keeps the issue or log as
  data, and changes only what it allows, in a separate step. Their shell
  steps run in CI against a throwaway Core, as the other examples do.
