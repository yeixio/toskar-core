### Added

- Toskar Pro's 14-day trial and Unlock (yeixio/toskar-apps#336): the Pro desktop app from the Mac App Store or the Microsoft Store tells Core where its purchase stands, `GET /api/v1/license` reports it, and once a trial has ended without the Unlock, installing models, creating automations, and adding people wait for it (`UNLOCK_REQUIRED`). Everything already set up keeps working. Core on its own is unaffected.
