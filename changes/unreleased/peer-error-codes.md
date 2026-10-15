### Changed
- An error on another computer now reaches the app with its own code instead of as plain text (#588). Paired computers answer each other with coded errors, such as `MODEL_NOT_INSTALLED` from the computer running a model, and `PEER_UNAUTHORIZED` or `PEER_UNAVAILABLE` when one won't take a request. Computers on an older Toskar still get, and send, plain text.
