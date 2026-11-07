# Lookup words

The words that offer chat a tool that reads something Toskar already keeps,
one file per language: the AI news (`news.read`) and the latest results of
the person's automations (`automations.results`), as in "read me today's AI
news" or "what's in my briefing" said in the car (yeixio/toskar-apps#142).
Core reads them in `internal/huginn/lookups.go`.

A message is checked against every language, since chat doesn't know which
one it's written in, so avoid phrases that mean something else in another
language. A tool is only offered by these words: a phrase that matches too
much offers a read-only tool the model may not need, and one that's missing
leaves the model to answer without the data.

Phrases are lowercase and matched against the message after it is
lowercased and NFKC-normalized (full-width ＡＩ becomes ai, ’ becomes ').
A space in a phrase matches any run of spaces, and `…` matches up to 40
characters. In a language written with spaces between words, a phrase
matches only whole words, so list each form ("meine automatisierung",
"meiner automatisierungen"). In a language written without them (Japanese,
Chinese, Korean with its attached particles), spaces are optional and
phrases match anywhere.

```ts
export interface LookupWords {
  /** Whether words are separated by spaces: false for Japanese, Chinese, and Korean. */
  spaced: boolean
  /** Asks for the AI news: "ai news", "ki-nachrichten", "aiニュース". */
  news: string[]
  /** Asks for an automation's latest result: "my briefing", "mis automatizaciones", "自动化结果". */
  results: string[]
}
```

To add a language, copy a file of a language like it, translate the
phrases, and add its cases to `internal/huginn/lookups_test.go`.
