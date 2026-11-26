### Changed

- An app that brings its own tools, such as Home Assistant's Assist, n8n's
  AI Agent, or Open WebUI's tools, can ask for auto or a profile: its tools
  reach the model that turn would use, so it can turn on a light or run its
  calculator, and the answer streams. Before, only `raw:<model>` did. It
  needs a key that may drive models directly, which requests from this
  computer without a key have. Answers to auto or a model's id also carry
  the name asked for instead of an empty one. (#1450)
