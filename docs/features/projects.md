# Projects

Design note for [#1136](https://github.com/yeixio/toskar-core/issues/1136). Swarms ([#1135](https://github.com/yeixio/toskar-core/issues/1135)) build on it.

## Goal

A project keeps related chats together with what they share: instructions, files, an optional folder on the computer, a default profile and model, and optionally a memory of its own. It's the place to come back to for a piece of work, the way Claude's Projects keep many threads together. A swarm's agents are threads in the project they ran in.

## Decisions

These answer the issue's open questions.

1. **A chat's folder stays a chat setting.** A chat in a project with a folder works in the project's folder unless the chat names its own. Chats keep their `project` field (contract 1.85) as it is, so nothing has to be migrated, and a chat moved out of a project keeps the folder it named itself.
2. **Project memory is on by default** for a new project, and can be switched off per project. What a project's chats learn is saved to the project. They still read the person's own memory, but what they learn doesn't leak into the person's other chats. With memory off, the project's chats neither read nor save project memory; the person's own memory follows the chat's Memory control as today.
3. **Sharing waits** for the teams release (1.15.0). In this version a project belongs to the person who made it, like their chats.

## Naming

`project` already names a chat's folder: the `conversations.project` column, `Conversation.Project`, the `X-Toskar-Project` header, and `turnopts.WithProject`. Those keep their names and meaning, for compatibility. The new thing is always a **project with an id**: the `projects` table, `project_id` on chats, and `/api/v1/projects`. In code, the folder helpers stay `turnopts.Project*`, and the new package is `internal/projects`. In user-facing text, a chat's folder is called its folder, and a project is called a project.

## Data

New migration `073_projects.sql`:

```sql
CREATE TABLE projects (
  id           TEXT PRIMARY KEY,
  person_id    TEXT NOT NULL DEFAULT 'owner',
  name         TEXT NOT NULL,
  instructions TEXT NOT NULL DEFAULT '',
  folder       TEXT NOT NULL DEFAULT '',   -- like conversations.project
  profile_id   TEXT NOT NULL DEFAULT '',
  model_id     TEXT NOT NULL DEFAULT '',
  memory_off   INTEGER NOT NULL DEFAULT 0,
  knowledge_json TEXT NOT NULL DEFAULT '[]', -- linked knowledge source ids
  created_at   TEXT NOT NULL,
  updated_at   TEXT NOT NULL
);
CREATE INDEX projects_person ON projects(person_id, updated_at);
ALTER TABLE conversations ADD COLUMN project_id TEXT NOT NULL DEFAULT '';
CREATE INDEX conversations_project ON conversations(project_id, updated_at);
ALTER TABLE knowledge_sources ADD COLUMN project_id TEXT NOT NULL DEFAULT '';
ALTER TABLE memories ADD COLUMN project_id TEXT NOT NULL DEFAULT '';
```

- **Ownership:** every query filters on `person_id = auth.PersonID(ctx)`, as the conversations repository does. Another person's project reads as not found.
- **Files:** files added to a project become one knowledge source of kind `text` with that `project_id`, made with the project and indexed by Mimir as any source is. Sources with a `project_id` are left out of the Knowledge page's list and of profile pickers, and are deleted with the project. Existing sources, such as a folder of documents, are linked through `knowledge_json`, without being copied.
- **Memory:** a memory with a `project_id` belongs to that project. `muninn.Relevant` takes the project id. In a project chat it returns the person's memories (`project_id = ''`) and that project's; outside one, only the person's. A memory saved during a project chat with project memory on gets the project's id. The Memory page shows a project's memories under it and can move one to personal memory.

## In a turn

`RunChat` loads the chat's project, when it has one, and then:

- **Profile and model:** the request's, else the chat's, else the project's, else the person's default (#1133).
- **Instructions:** a "Project instructions" block in `TurnInstructions`, after the specialized AI's instructions and before the person's own style (`personalBlock`). The Admin's topic rules stay first, and nothing after them changes them. The person wrote the instructions, so they're trusted like their style. Text a chat reads, such as files, pages and tool results, still goes through `untrusted.Text` and can't add to them.
- **Knowledge:** `knowledgeBlock` searches the project's own source and linked sources as well as the profile's, all as reference material, never in the system prompt.
- **Folder:** `chatFolder` uses the header's folder, else the chat's `project`, else the project's `folder`, under the same rule as today: only for an Admin, and only on the web, desktop and terminal. Setting a project's folder needs Admin, like a chat's, because it gives tools that folder. A project folder's `AGENTS.md`, `TOSKAR.md` or `CLAUDE.md` is read into task guidance as a chat folder's is.

## API (contract 1.87)

| Method and path | What it does | Role |
| --- | --- | --- |
| `GET /api/v1/projects` | The person's projects, newest activity first, with chat counts | Visitor and up, like conversations |
| `POST /api/v1/projects` | Make one: `name`, and optionally `instructions`, `profile_id`, `model_id`, `memory_off`, `knowledge` | Visitor and up |
| `GET /api/v1/projects/{id}` | One project | Visitor and up |
| `PATCH /api/v1/projects/{id}` | Change fields. `folder` needs Admin and must be a folder (`NOT_A_FOLDER`) | Visitor and up (`folder`: Admin) |
| `DELETE /api/v1/projects/{id}` | Delete it, its files' source, and its memories. Its chats move out unless `?chats=delete` | Visitor and up |
| `POST /api/v1/projects/{id}/files` | Add a file to the project's knowledge | Visitor and up |
| `DELETE /api/v1/projects/{id}/files/{name}` | Remove one | Visitor and up |

- `Conversation` gains `project_id`. `POST /conversations` and `PATCH /conversations/{id}` take it, and `GET /conversations?project_id=` lists one project's chats. A chat can be in one project at most.
- `POST /chat` with a new chat takes `project_id` too.
- Phone keys get the read routes and new chats in a project (`deviceRoutes`), so the apps can list and use projects. Portals' guests don't.
- Updated with the contract bump: `api/openapi.yaml`, `docs/api.md`, `tests/contract/testdata/contract.json`, the SDKs (toskar-sdks sync), and tested curl examples.

## Web and desktop

- **History drawer:** projects listed above the dated groups, each opening to its chats. Pins stay as they are, in the browser.
- **Project page** (`/projects/{id}`): its chats with New chat; Instructions; Files, plus linked Knowledge; Folder (Admin); default profile and model; Memory on or off; Delete.
- **In a chat:** a line under the title says which project it's in, with Move to project and Remove from project. The folder control (`ChatFolder`) shows "From the project: …" when the folder comes from the project.
- New chat from a project page starts in it.

## Terminal

- `toskarctl chat --project <name or id>` starts or resumes in a project. Without `--folder`, the current folder is still sent as today, so a code project's chats work where you are.
- `/project` in a chat shows the project; `/project <name>` moves the chat into one.
- `toskarctl projects` lists them.

## Phone, tablet, TV and car

These are tracked in a toskar-apps issue: list projects and their chats, and start a new chat in one. The car and the TV only list and continue chats.

## Swarms

A swarm (#1135) runs in a project: the coordinator's chat and each agent's thread are chats in it, linked by a `swarm_id` that #1135 adds. Without a project, a swarm makes one named after the job.

## Order of work

1. Core: migration, `internal/projects` store, API and roles, chats' `project_id`, turn wiring (profile, model, instructions, knowledge, folder), project memory, contract 1.87, unit and API tests.
2. Web and desktop: drawer, project page, move a chat, folder from the project.
3. Terminal: `--project`, `/project`, `toskarctl projects`.
4. User guide section with screenshots, and curl examples checked in CI.
5. toskar-apps issue for the phone and other apps, and the SDK sync.
6. Swarms build on it.

## Tests that show it works

- A project's instructions reach a turn after the Admin's topics and before the person's style, and a file's text can't change them.
- A chat in a project with a folder runs a command in that folder, for an Admin on the web, and not for a Member or a phone.
- Something learned in a project chat is recalled in another chat in that project, and not in a chat outside it. With project memory off, nothing is saved to the project.
- Another person can't see, change, or chat in someone's project.
- Deleting a project keeps its chats unless asked, and removes its files' source and its memories.
