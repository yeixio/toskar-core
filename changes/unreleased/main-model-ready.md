### Changed

- The first reply no longer waits for a sleeping model as often. Toskar keeps
  your main chat model loaded while it needs at most half of the computer's
  memory, and loads it when Toskar starts. The web app and the desktop app
  also start loading the model when a chat opens and when you start typing.
- Chat asks an Admin once whether the main chat model should be **Always
  ready** (kept loaded, with the memory it uses) or **managed by Toskar**
  (memory is freed when needed). The same choice is in Settings › AI behavior
  › Model lifecycle as **Main chat model**.
