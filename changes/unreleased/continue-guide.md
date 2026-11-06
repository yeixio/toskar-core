### Added

- A step-by-step guide for Continue in VS Code with Toskar as its model provider, on this computer and on another one with a key, tested with Continue 2.0.0 (`docs/integrations/continue.md`, #1061), linked from the user guide's Terminal page.

### Fixed

- `toskarctl connect continue` adds `useLegacyCompletionsEndpoint: false` to the model it prints, so Continue's edit tool and Apply go through chat instead of `/v1/completions`, which Toskar doesn't have. Before this, they failed. With `TOSKAR_API_KEY` set, the key is read from Continue's `~/.continue/.env` (#1061).
