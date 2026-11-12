### Added

- Projects keep related chats together with what they share: instructions,
  files, a folder on the Toskar computer, a default profile and model, and a
  memory of their own. A project's chats follow its instructions, search its
  files, and remember what they learn for the project's other chats, not
  your others. In the web and desktop apps, Chat's history lists your
  projects above your chats, each opening to its chats, and New project
  makes one. A project's page has its chats with New chat, its
  instructions, files and linked knowledge, a folder (Admins), its default
  profile and model, and its memory switch. A chat says which project it's
  in, under its title, with Move to project and Remove from project. The
  API has them too (`/api/v1/projects`, and `project_id` on chats), so the
  apps can list projects and start chats in them (contract 1.87, #1136).
