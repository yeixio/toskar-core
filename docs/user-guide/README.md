# User guide

`guide.json` is the source for the public user documentation on
toskar.ai/docs. Every feature has a detailed page here, in the same pull
request as the feature, with screenshots or tested examples (#882). How to
write one is in [STYLE.md](STYLE.md). Keep it to public information.

Each section has an `id`, a `title`, and `paragraphs`, the overview every
version of the site shows. Optional fields:

- `steps`: ordered steps.
- `code`: a code block.
- `images`: screenshots, each with `src`, `alt`, and `caption`. `src` is a
  repository path to a PNG under `docs/user-guide/images/`, such as
  `docs/user-guide/images/07-automations.png`.

A detailed section adds `summary`, `kind` (`task`, `concept`, or
`reference`), and `blocks`: headings, paragraphs, steps, lists, notes,
images, code with an optional `test` id, tables, and related sections.
STYLE.md lists their fields. Inline text may hold `` `code` `` and links to
`#section` or `https://` pages. There's no executable HTML or MDX.
`{{version}}` is replaced with the version being released.

The website reads the frozen snapshots in this repository:

- `docs/<version>.json`: snapshot with version, source tag, and commit
  provenance.
- `docs/index.json`: all available documentation versions.

A core release freezes a new snapshot from the tagged `guide.json` after the
packages publish, and pushes it to `main` with the release deploy key. The
publisher refuses to change an existing snapshot. The site polls the index
with five-minute server-side revalidation.

Checks, which CI runs:

```sh
python3 scripts/test_publish_docs.py
python3 scripts/test_check_user_guide.py
python3 scripts/check-user-guide.py
python3 scripts/publish-docs.py --destination /tmp/ygg-docs-check \
  --version 0.0.0-test --commit "$(git rev-parse HEAD)"
```

A pull request that adds a feature (an `### Added` changelog fragment) must
change `docs/user-guide/` too, unless it has the `no-user-docs` label with
the reason in its description: `scripts/check-user-guide.py pr`.
