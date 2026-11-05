### Added

- `toskarctl chat` starts Toskar when it isn't running on this computer, through its service if it has one, and says how to stop it. Toskar stays running after the chat. If Toskar isn't installed, chat asks first, shows the download's size, and runs the installer. With `TOSKAR_URL` set, it starts nothing.

### Fixed

- `toskarctl` looks for Toskar at `TOSKAR_API_PORT` when that's set, the same port Toskar itself uses.
