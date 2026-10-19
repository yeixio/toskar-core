### Added

- Pictures and clips show how far they've got (#706): whether the model is loading, which step it's on with about how long is left, and when it's finishing, beside a frame where the result will appear. On a long wait, a line says what Ratatoskr is up to, such as running down Yggdrasil for more paint (from a user's idea). Screen readers hear the facts, not the jokes.
- `tool.progress` events with `stage`, `step`, `steps`, and `seconds_left` while a picture or clip is made on this computer (#706, contract 1.44).

### Fixed

- The web app now shows "Reasoning…" while a model thinks (#701) and "Compacting earlier messages…" before a long turn (#342): it never subscribed to those events, so neither line appeared. A test now checks that the web subscribes to every event the chat page handles.
