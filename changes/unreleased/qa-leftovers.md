### Added

- A subscription tied to another computer can be moved to this one from
  API Access.
- Automations warn when the chosen model can't use the tools they need.
- Setup offers "Set up later" when another page was opened, and goes there.

### Changed

- Tools shows a short description for people; the model keeps its own.
- The sidebar's Placement and Knowledge rows show real status, or stay out.
- Access from anywhere keeps its router settings out of the way while off,
  and says which port opens before it's turned on.
- The deploy list on Computers puts what fits first and leaves out the test
  model, and "Manage" is now "Details".
- The context meter beside Send shows its percentage.

### Fixed

- A push that reaches some devices retries only the ones that missed it.
- Routers that take only permanent UPnP mappings get one.
- A checkout page opened before a restart is still found after it.
- Computers: the waiting card goes once the other computer joins, can be
  stopped, and says when there's no answer; the latest error shows, can be
  dismissed, and names the computer; several removals show at once.
- API Access says HTTPS only when it's on, and shows no defaults under a
  load error.
- The Access from anywhere switch moves at once.
- Device keys never used, or not for 90 days, are marked.
- The Models Store's memory note no longer contradicts the fit.
- No startup screen flash when the service is already up.
- Settings choices show which one is picked, clearly in light mode too.
- Setup offers keep their details readable on phones.
