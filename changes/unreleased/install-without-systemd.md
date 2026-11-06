### Fixed

- On Linux without systemd, such as a container or WSL without it, the install script no longer waits a minute and reports a failed install that worked: it says nothing keeps Toskar running there and how to start it. `toskarctl chat` starts Toskar itself when the installer it ran put Toskar in place but couldn't start it.
