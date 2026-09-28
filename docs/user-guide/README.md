# User guide

`guide.json` is the source for the public user documentation. Update it in the
same change as user-facing behavior. Keep it limited to public information.
It supports plain-text sections, ordered steps, and code blocks; no executable
HTML or MDX. `{{version}}` is replaced with the version being released.

The website reads the frozen snapshots in this repository:

- `docs/<version>.json`: snapshot with version, source tag, and commit provenance.
- `docs/index.json`: all available documentation versions.

A core release freezes a new snapshot from the tagged `guide.json` after the
packages publish. The release job opens a pull request onto `main` because
`main` requires one, then merges it. The publisher refuses to change an
existing snapshot. The site polls the index with five-minute server-side
revalidation.

Checks:

```sh
python3 scripts/test_publish_docs.py
python3 scripts/publish-docs.py --destination /tmp/ygg-docs-check \
  --version 0.0.0-test --commit "$(git rev-parse HEAD)"
```
