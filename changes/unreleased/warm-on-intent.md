### Added

- `POST /api/v1/models/warm` starts loading the model a chat would use and answers at once, so a phone, tablet, watch, or TV can have the computer load it while someone types or speaks, instead of after the question arrives (#498, contract 1.26). It never loads beside a model that's answering, nor one that wouldn't fit beside those already loaded.
