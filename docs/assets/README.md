# Assets

Web UI screenshots used by the README are generated into [../screenshots/](../screenshots/) by `scripts/capture-screenshots.sh`. Leave them there so the capture workflow and the README stay on the same path.

Store canvases under `screenshots/appstore/` are local build output and are gitignored. Do not copy proprietary desktop marketing into this repository.

`make screenshots` also captures the same six screens for the store sizes and writes them under `screenshots/appstore/`:

| Set | Pixels | Why |
| --- | --- | --- |
| `2880x1800`, `2560x1600` | desktop | Mac App Store canvases, letterboxed from the README stills |
| `apple-iphone/1320x2868` | 6.9-inch iPhone | App Store phone set. Apple scales this to smaller iPhones |
| `apple-ipad/2064x2752` | 13-inch iPad | App Store tablet set. Apple scales this to smaller iPads |
| `play-phone/1080x1920` | phone | Google Play phone slot, 9:16 |
| `play-tablet-7/1080x1920` | 7-inch tablet | Google Play 7-inch slot, 9:16 |
| `play-tablet-10/1620x2880` | 10-inch tablet | Google Play 10-inch slot, 9:16 |

Phone captures hide the desktop sidebar. Tablet captures keep it. Store PNGs have no alpha channel.

`make screenshots` then holds each settled still to write `demo.mp4` and `demo.gif` beside them. That walkthrough uses the screenshot demo data. A live recording of a real model, a second computer, and Norn placement is still the open item in [launch-decisions.md](../launch-decisions.md).
