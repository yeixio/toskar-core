### Added

- Guess ahead, Auto or Off for each model: with Advanced mode on, a model's More actions menu on Models turns speculative decoding off for that model, from its next start. `PUT /api/v1/models/{id}/speculation` sets it, and each chat model installed on the computer says `speculation` (#835, contract 1.67).
