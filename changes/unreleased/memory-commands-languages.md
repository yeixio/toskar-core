### Fixed

- Memory commands work in all ten languages, not only English (#718): „Merk dir, dass …“, « Souviens-toi que … », „…, merk dir das“, 「…を覚えておいて」, “记住：…”, and the forget and \"what do you remember about me\" forms. The help text already told people to say them in their language, but only English was recognized, so a small model often said it would remember and saved nothing. Each language's words are in `i18n/memory/`.
