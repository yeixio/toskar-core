### Changed

- Catalog: each model names its `family` (its line, such as qwen) and its `generation` (such as 2.5), and can name the entries it `supersedes`, size for size. A test fails when a model has no family or generation, or supersedes one of another family, the same generation, a different job, or itself by way of others. Models still show their family as before, such as qwen 2.5 (#593).
