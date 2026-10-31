### Added

- Performance shows how often speculative decoding's guesses pay off: with Advanced mode on, a reply's details say how many words the model guessed ahead and kept, and Activity shows the share kept across the replies shown, per model when filtered by one. `GET /api/v1/performance` has each reply's `draft_tokens` and `draft_accepted` (#835, contract 1.66).
