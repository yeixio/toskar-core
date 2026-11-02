### Fixed

- After a chat reads something from outside, such as a web page, a tool that changes things asks again even if you answered "always in this chat". Before, only a profile that allowed the tool outright was held to this.

### Added

- `toskarctl chat` has `/log`: what ran in this chat, its result, and who allowed it. The record keeps a command whole, and a command that fails counts as failed.
