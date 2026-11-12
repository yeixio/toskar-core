### Added

- Projects keep related chats together with what they share: instructions,
  files, a folder on the Toskar computer, a default profile and model, and a
  memory of their own. A project's chats follow its instructions, search its
  files, and remember what they learn for the project's other chats, not
  your others. This release adds them to the API (`/api/v1/projects`, and
  `project_id` on chats), so the apps can list projects and start chats in
  them; the web and desktop pages come next (contract 1.87, #1136).
