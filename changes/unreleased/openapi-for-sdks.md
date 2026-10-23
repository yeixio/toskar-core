### Fixed

- The API description (`api/openapi.yaml`) gives the client contract's
  version (1.55, not 1.4.0), and no longer lists one response twice.
- The API description now describes people, sign-in, invites, roles and
  profile pins fully, with named types, including fields it left out
  (an API key's pinned profile, and a person's email and portal).
- It describes connecting a device, too: the pairing code, the paired
  device's key and certificate, and its route for access from anywhere.
- It describes every type in the client contract (messages and their
  sources, steps, files and deliberation, run traces, events,
  notifications, and errors) from the code's own fields and comments, and
  the routes for chats, their summaries, and versions use them.
