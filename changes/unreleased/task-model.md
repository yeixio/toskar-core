### Changed

- Qwen 3.8 27B now counts as good for code, so recommendations and `toskarctl connect` can pick it. In tests it fixed a failing test where the smaller coding models mostly couldn't.
- `toskarctl chat` says which model to install when nothing installed does well at work such as fixing code.
