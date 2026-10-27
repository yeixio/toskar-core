## What changed?

## Why?

## Related issue

Fixes #

## Testing

- [ ] Unit tests
- [ ] Integration tests
- [ ] Manual testing
- [ ] Existing tests pass

## Platforms tested

- [ ] macOS
- [ ] Windows
- [ ] Linux
- [ ] Not platform-specific

## Hardware tested

CPU:

GPU:

RAM/VRAM:

## Breaking changes

## Security implications

## User docs

Every feature ships its page in `docs/user-guide/guide.json`, with screenshots or tested examples ([STYLE.md](../docs/user-guide/STYLE.md)).

- [ ] The user guide covers this change, and `python3 scripts/check-user-guide.py` passes
- [ ] No user-facing change
- [ ] No page needed (label `no-user-docs`), because:

## Other documentation updated

## Checklist

The agreement is [CLA.md](../CLA.md).

- [ ] I agree to the Toskar Contributor License Agreement.
- [ ] Code formatted
- [ ] Tests added/updated where appropriate
- [ ] No secrets or personal data included
- [ ] Public APIs documented
- [ ] Relevant docs updated
- [ ] A changelog fragment in `changes/unreleased/` for anything users or integrators notice (not an edit to `CHANGELOG.md`)
- [ ] New or changed text is in `i18n/locales/en/`, not written into the code; for translations, the language and how it was checked are in the description
- [ ] New or changed UI follows [docs/design.md](../docs/design.md): tokens, loading/failed/empty states, keyboard, 390px wide, both themes; `node scripts/screenshots/a11y.mjs` passes
