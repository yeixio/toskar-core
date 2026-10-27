# User guide style

Every feature has a detailed page in the user guide, in the same release
that ships it, with screenshots or examples that CI checks
(yeixio/toskar-core#882). This is how to write one.
`scripts/check-user-guide.py` checks the parts a script can check. Reviewers
check the rest.

## Who reads it

Write for someone who has never seen Toskar: a doctor's office manager, a
small-business owner, a parent setting it up for the family. They know
email and a web browser. They don't know what a model, a token, or a port
is until the page tells them, in a sentence, when it first matters.

Developers read the same pages for the API and `toskarctl`. Give them the
exact request in a tested example, after the plain explanation, not
instead of it.

## Kinds of page

Each detailed section has a `kind`, which sets its outline.

**task:** how to do one thing.

1. `summary`: one sentence saying what you'll be able to do.
2. A `list` titled **What you need**, such as an installed model, an Admin
   account, or a second computer. Link to the page that covers each one.
3. Short `steps`, one action each, in the words on the screen. Bold isn't
   available, so write button names as they appear: choose Fill in the
   details.
4. A screenshot of what you'll see once it worked.
5. A `heading` **Troubleshooting**, then what goes wrong and how to fix
   it, by symptom.

**concept:** what something is and when to use it (profiles, Norn,
Deliberate).

1. `summary`.
2. What it is, in a paragraph with an everyday comparison.
3. When it helps, and when it doesn't: a `table` or two short lists.
4. A screenshot or a tested example of it at work.
5. `related` task pages.

**reference:** settings, limits, API fields. Use tables, one row per item:
what it is, the default, and what changing it does. Add a tested example
for every API field.

## Voice

- Plain words, short sentences. A sentence over 45 words fails the check,
  and the average stays under 25.
- Second person, present tense: "Choose Models", not "The user selects
  Models".
- Say what happens, then why it matters: "Toskar keeps the chat on the
  computer that already has it loaded, so the next answer starts sooner."
- Name devices by what they are: this computer, this device, your phone,
  tablet, TV, or watch. Never a brand. The check fails on iPhone, Android,
  and the like.
- Every claim about behavior is true of the release the page describes. If
  a test doesn't cover it, check it against the app before writing it.
- Limits are part of the page, not small print: what it can't do, what it
  needs, and what leaves this computer.

## Blocks

A section keeps `id`, `title`, and `paragraphs`, the overview that every
version of the site shows. A detailed section adds `summary`, `kind`, and
`blocks`, in reading order:

| Block | Fields | Use |
|---|---|---|
| `heading` | `text` | A subheading on the page |
| `paragraph` | `text` | Prose |
| `steps` | `items` | Numbered actions |
| `list` | `items`, optional `title` | What you need, limits, options |
| `note` | `tone` (`tip` or `warning`), `text` | One thing worth calling out |
| `image` | `src`, `alt`, `caption` | A screenshot (below) |
| `code` | `language`, `code`, optional `test` and `output` | An example (below) |
| `table` | `columns`, `rows` | Settings, limits, comparisons |
| `related` | `sections` | Other pages to read next |

Inline, text may use `` `code` `` and links: `[Models](#models)` for
another section, or `[text](https://…)` for a page elsewhere. There's no
HTML, bold, or italics.

## Screenshots

Screenshots are captured from the real web UI with demo data, never taken by
hand, so they follow the interface. A detailed page's `image` block names a
`screen` instead of a file:

```json
{ "type": "image", "screen": "automations-create",
  "alt": "New automation form filled in from a sentence",
  "caption": "Fill in the details turns the sentence into a schedule and a task." }
```

Each screen is in `docs/user-guide/screens.json`:
- `path` and `query` open it in screenshot mode.
- `readyText` is on the page once it has loaded.
- `actions` (`click`, `fill`, `wait`) reach the state the page describes, such
  as a dialog open.

The demo data is in `internal/screenshot`. Add what a page needs there.
`scripts/capture-docs-screenshots.sh` (or `DOCS_SCREENS=<id>` for one screen)
saves `docs/user-guide/images/<screen>.png`, and CI does the same on every
pull request that touches the UI. It lists the screenshots that changed, and
on `main` it commits them, so the guide at a release tag shows that
release's interface.

Show the result of the steps, with realistic data and no personal details.
`alt` says what the screenshot shows, for someone who can't see it, and
`caption` says what to notice. A screen no page uses, or an image no page
shows, fails the check.

## Examples

A `code` block with a `test` id runs in CI against a throwaway Core, and its
`output`, when given, is compared (#882). An example without a `test` id
doesn't count toward the bar. Use one only for something CI can't run, such
as a command for another operating system, and say so in the text.

## Before you open the pull request

- [ ] `python3 scripts/check-user-guide.py` passes.
- [ ] You followed the page from a fresh install, and it worked as written.
- [ ] The changelog fragment for the feature and its page are in the same
      pull request.
