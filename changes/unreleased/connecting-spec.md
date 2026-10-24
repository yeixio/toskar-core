### Added

- `docs/connecting.md` says exactly how an app or SDK finds a computer,
  pairs with it, pins its certificate, and reaches it from anywhere, with
  test vectors in `api/connection-vectors.json` that Core's tests keep in
  step with its own code.
- The outside-network check also runs the official SDKs' conformance
  programs, as devices that pair at home and connect from away, both
  directly and through the relay.

### Fixed

- `docs/connecting.md` says to send the device's key with the health
  check, since a computer that asks for keys answers it only with one, and
  to check the certificate on resumed TLS sessions too.
