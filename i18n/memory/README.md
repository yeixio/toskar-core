# Memory command words

The words memory commands are read with in each language other than
English (#718), one file per language: "Merk dir, dass …", "…を覚えておいて",
"记住：…". Core (`internal/muninn/commandwords.go`) and the Toskar app
(`mobile/src/memory.ts`, which gets these files with the catalog) build the
same patterns from them, and `tests/quality/text.json` (`memory_command`)
holds both to the same answers. English is in the code, with typo correction.

A message is checked against every language, since the app doesn't know
which one it's written in, so avoid single words that mean something else in
another language ("note", "guarda").

```ts
export interface MemoryCommandWords {
  /** False for Japanese, Korean, and Chinese: phrases match without word boundaries. */
  spaced: boolean
  /** Words that may come first: "bitte", "por favor". */
  polite: string[]
  remember: Phrases
  forget: Phrases
  /** Whole questions that list what's remembered: "Was weißt du über mich". */
  list: string[]
  /** What isn't a memory on its own: "das", "これ" ("remember that"). */
  nothing: string[]
}

export interface Phrases {
  /** Before the memory: "merk dir", "souviens-toi", "记住". */
  before: string[]
  /** What may join them: "dass", "que", "qu'", "：". */
  joiners: string[]
  /** After it: "merk dir das" (after a comma), "を覚えておいて", "기억해 줘". */
  after: string[]
}
```

Remembering is checked before forgetting, so "Vergiss nicht, dass …" (don't
forget that) isn't "Vergiss …" (forget). A spaced language's "after" phrase
needs a comma, period, or dash before it; a question isn't a memory.

To add a language, copy a file of a language like it, translate the phrases,
and add its cases to `internal/muninn/commandwords_test.go` and
`tests/quality/text.json`.
