### Added

- A connected phone can connect its watch to the computer, from anywhere
  and with no code to type: `POST /api/v1/me/devices` (contract 1.33)
  gives the watch a key of its own, for the same person, so it shows in
  Devices and can be disconnected on its own (yeixio/toskar-apps#213). A
  person may have 20 connected devices.
