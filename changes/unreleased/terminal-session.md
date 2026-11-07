### Fixed

- Qwen 3.5 can change files in a task. Writing a whole file, it closed its call one brace short, so Toskar dropped the request without asking, and the model said it had made the change. A call left open at its end is now closed, unless it was cut off partway (#959).
- Finding files understands patterns such as `*test*` or `*.go`, as models write them. Before, it looked for those characters in the name and found nothing (#959).

### Changed

- The user guide's Terminal page shows a recorded session: Qwen 3.5 9B finding a failing test's cause and asking before it changes the file. CI runs a whole `toskarctl chat` session against a real Toskar with the scripted model (#959).
