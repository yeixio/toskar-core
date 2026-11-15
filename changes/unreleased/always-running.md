### Added

- A chat model can be kept running on a computer (#966, contract 1.94): `PUT /api/v1/models/{id}/always-running`. It loads when Toskar starts and again within a minute if it stops, such as after a crash, and isn't unloaded when idle or to make room for a picture or clip. A training run still frees the memory, and the model comes back after it. Stopping it yourself keeps it stopped until you start it again or Toskar starts. The Models page's switch for it follows.
