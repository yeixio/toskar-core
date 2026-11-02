### Fixed

- `toskarctl models install` no longer gives up after a minute while a large model is still downloading.
- `toskarctl chat --model` and `/model` say when a model isn't installed, rather than let another model answer without saying so, and a chat shows the answer's notice when another model answered.
