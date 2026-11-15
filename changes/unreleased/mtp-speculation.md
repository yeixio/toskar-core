### Added

- `TOSKAR_MTP=on` lets a model with its own multi-token prediction head,
  such as Qwen 3.5 and 3.8's `-mtp` files, guess ahead with it, with no
  draft model. It's off by default: on Qwen 3.5 4B it was 38% faster on
  average, but changed 2 of 9 answers and cut one short.
