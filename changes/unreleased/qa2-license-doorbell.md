### Fixed

- A model's license dialog names the model instead of showing
  `{{model}}` (#1338).
- An automation that a doorbell's button starts greets whoever rang,
  even when the picture shows no one, instead of saying nothing. A run
  that finds nobody to talk to notifies "Nobody was in the picture, so
  nothing was said at the door." instead of "Nothing". A visit's end
  mark is never said aloud, in any case (#1339).
