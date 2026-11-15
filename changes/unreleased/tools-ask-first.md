### Fixed

- Writing files, running terminal commands, and Git add and commit ask
  first again in the built-in profiles, and Allow for session still lets
  them run for the rest of a chat. Git push asks every time, since it
  sends your code off the computer. Before, General Assistant and
  Programming let all of them run without asking (#1206).
  In a task, commands that only read or test, such as `go test`, still
  run without asking, so a fix-and-test loop can.
- Settings → Tool permissions now takes effect. Each switch is a limit
  for every profile on this computer: a profile can be stricter, never
  looser, and Deny holds in automations too. Before, nothing read them
  (#1206).
- The AI is told that tools ask first only when they do, and is told to
  change only what was asked when some run without asking (#1206).
