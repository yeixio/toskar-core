# Unreleased changes

Each pull request describes its change here, in a file of its own, instead
of editing `CHANGELOG.md`. Pull requests then never conflict over the
changelog. When a release is cut, `scripts/changelog.py release <version>`
gathers these files into a dated section of `CHANGELOG.md` and removes them.

## Writing one

Name the file after the change, such as `ntfy-push.md` or `fix-pdf-spaces.md`.
Use [Keep a Changelog](https://keepachangelog.com/en/1.1.0/) headings
(`Added`, `Changed`, `Deprecated`, `Removed`, `Fixed`, `Security`) and one
bullet per change, written for the people who use Toskar:

```markdown
### Added

- Push notifications through ntfy, on ntfy.sh or your own server.

### Fixed

- PDFs keep the spaces between bold and plain words.
```

A bullet can continue on indented lines. `scripts/changelog.py check`, run
in CI, checks every file; `scripts/changelog.py preview` shows the next
release's section.
