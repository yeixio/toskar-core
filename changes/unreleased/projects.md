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
- The phone and tablet apps list your projects above Computer in the chat
  list, with New chat in this project, and the TV and the car continue a
  project's chats; projects are still made and changed on the computer
  (toskar-apps#432, #1136).
- The Memory page lists what a project's chats learned under the project's
  name, after your own memories, and Move to personal memory in a memory's
  menu makes it yours in every chat. The API does the same with
  `PATCH /api/v1/memory/{id}` and an empty `project_id`, and a memory says
  which project it belongs to with `project_id` (contract 1.87, #1136).
- In the terminal, `toskarctl projects` lists your projects with how many
  chats are in each, and `toskarctl chat --project <name or id>` starts a
  chat in one, or with `--resume` continues one of its chats. The chat still
  works in the folder you're in, before the project's. While chatting,
  `/project` says which project the chat is in, `/project <name>` moves it
  into one, and `/project none` takes it out (#1136).
- The user guide's Projects page is a full how-to: making a project, its
  page, moving chats in and out, what it remembers, and projects in the
  terminal and the API, with screenshots of History, a project's page, a
  chat in a project, and the Memory page, and examples CI runs (#1136).
