# Translations

Yggdrasil's text in every language: the web UI that the desktop app shows,
the desktop shell's menus, and the iPhone app. The iPhone app copies this
folder when it builds, so all of them share one catalog.

```text
i18n/
  languages.json            languages that can be chosen, with their direction
  locales/<language>/       one folder per BCP 47 tag, such as en, es, pt-BR
    common.json             navigation, status, and words used everywhere
    settings.json           and one file per area of the app
    desktop.json            the desktop app's own menus, tray, and closing screen
    mobile.json             the iPhone app's text
```

The desktop app's shell copies this folder when it builds and reads
`desktop.json`; the web UI tells it the language, so its menus and tray
match the page.

The iPhone app keeps a copy of `languages.json` and every `mobile.json` in
`mobile/src/i18n/catalog.json` (yeixio/yggdrasil-desktop), which its build
refreshes from the matching core. Its App language follows the computer it
is connected to unless it is set on the phone.

English (`en`) is the source. Every other language has the same files and
keys; a key that is missing falls back to English, so a language can ship
before it is complete.

## Keys

- Keys are stable names, never the English text: `nav.models`, not
  `"Models"`. Changing the English wording keeps the key.
- Placeholders use `{{name}}`, and must match English in every language.
- Plurals use i18next suffixes: `key_one`, `key_other`, and `key_zero`,
  `key_two`, `key_few`, or `key_many` where a language needs them. English
  has `_one` and `_other`.

## Adding a language

1. Add it to `languages.json` with its native name, `dir` (`ltr` or `rtl`),
   and `status`: `machine`, `partial`, `reviewed`, or `community`.
2. Copy `locales/en` to `locales/<tag>` and translate the values.
3. Hermes on iOS has no `Intl.PluralRules`, so the iPhone app loads plural
   rules for each language: add the language's line to
   `mobile/src/i18n/plurals.ts` in yeixio/yggdrasil-desktop.
4. Run `pnpm test` in `web/`. It checks every file against English: valid
   JSON, no duplicate keys, no keys English lacks, the same placeholders,
   and complete plural forms.

## Pseudo-locale

`en-XA` is generated from English, not stored: accented, padded about 30%,
and bracketed, such as `[!! Mööödéééls !!]`. Choose it in Settings in
advanced mode to find text that is not translated or does not fit.
