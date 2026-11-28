### Fixed

- Talking with visitors through a Reolink doorbell or camera works again when
  Toskar runs as a service on Linux. Toskar now starts reolink-cli's gateway
  itself even when reolink-cli's folder isn't on the service's PATH, and when
  the gateway can't start, the visit says why instead of asking you to start
  it by hand (#1498).
