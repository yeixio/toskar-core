### Added

- Access from anywhere can be bought on the computer itself, without a phone: `POST /api/v1/remote-access/subscription/checkout` opens a Stripe checkout (monthly or yearly), Toskar turns access from anywhere on once it's paid and renews it as the subscription does, and `POST /api/v1/remote-access/subscription/manage` opens the billing page for changing the card or cancelling. `GET /api/v1/remote-access` says how the subscription stands (#639, contract 1.38).
