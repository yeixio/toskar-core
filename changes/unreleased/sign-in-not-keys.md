### Fixed

- Setting up people no longer looks like it needs an API key (#711). Turning on local network access asked for a key, even though people you add sign in with their username and password. API Access now says who connects how (people with a password, phones and tablets with a code, apps and scripts with a key) and lets you turn it on once anyone could connect. People offers **Turn on network access** right under a new person's link. The user guide's People section has the steps in the order that works.

### Added

- `GET /api/v1/settings` tells Admins who could connect from another computer, in `network_access`: people, devices, keys, and whether a sign-in service is set up (#711, contract 1.42).
