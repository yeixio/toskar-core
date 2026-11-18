### Fixed

- Phones, tablets and TVs that didn't buy access from anywhere keep finding
  the computer away from home after its subscription renews: they get the
  newer device token from `GET /api/v1/remote-access/device-token`, from
  anywhere, instead of only at home (yeixio/toskar-apps#451, contract 1.99).
