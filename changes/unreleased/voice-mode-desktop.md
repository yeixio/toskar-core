### Added

- Voice mode in the desktop app (#795). The app's window opens live
  voice's WebSocket on the daemon itself, since the shell's proxy can't
  carry one, at the address the shell gives. It needs the Toskar app from
  1.14 or later.
