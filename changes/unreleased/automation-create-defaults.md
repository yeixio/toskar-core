### Fixed

- An automation created through the API without a profile, model, or time zone now gets General Assistant, Auto, and this computer's time zone, instead of being saved and then failing every run. A profile that doesn't exist is refused when it's saved. `toskarctl automations create --name` no longer needs `--profile` and `--model`.
