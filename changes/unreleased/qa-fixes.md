### Added

- A pending Access from anywhere checkout can be cancelled from API Access.
- A Skip to content link, the first thing on every page for keyboard users.

### Changed

- Access from anywhere leads with its switch: the plans and the router
  setting wait until it's on.
- Adding a nearby computer has one button; approving with a code is behind
  a link on its card.
- The sidebar's status counts other computers, says "Just this computer" on
  its own, and shows Knowledge only to admins.

### Fixed

- A key made in the "Allow access" dialog now lets you allow access.
- Removing a model from a computer asks first.
- Adding one nearby computer no longer shows every card as busy.
- Models calls this computer by its Toskar name, not the system's hostname.
- Page watch runs when the page changes, as its summary says.
- Enter in the Knowledge path field connects it.
- Tall onboarding steps scroll instead of cutting off the top.
- Sizes read "4 B", not "4 byte".
- Messages point to where settings are now, such as Tools → Connected
  services and Computers → Network & access.
- When the relay refuses this computer, API Access says why and what to do,
  instead of showing a code, and a subscription moved or refunded elsewhere
  shows as ended.
- Toskar re-registers with the relay as soon as it reconnects, and takes its
  record back when Access from anywhere is turned off.
- Toskar doesn't sell a subscription to a computer another device's
  subscription already covers.
- Ports mapped on the router are released when Toskar stops.
- A question the local knowledge answers doesn't search the web.

### Security

- Paired devices get a device token from the relay, which can look the
  computer up but can't open its tunnel or change its record, instead of
  the computer's own token.
- A relay token handed over by a device is checked with the relay first,
  and can't replace a longer one.
- Companion keys can't make companions of their own, and revoking a key
  revokes its companions.
- The remote listener's lockout no longer turns away valid keys.
- New API certificates don't carry the computer's name in their subject.
