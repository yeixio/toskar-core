# API

The daemon listens on `http://127.0.0.1:7331` unless `config.json` or `YGGDRASIL_API_HOST` / `YGGDRASIL_API_PORT` say otherwise. A machine-readable description of the control-plane routes is [api/openapi.yaml](../api/openapi.yaml). This page records behavior that matters when you call the server.

## Authentication

Loopback binds (`127.0.0.1`, `::1`, `localhost`) do not require an API key. Any other API host, including `0.0.0.0` and `::`, requires `Authorization: Bearer YOUR_API_KEY` on every `/api/v1/*` and `/v1/*` request. The daemon refuses to listen on a non-loopback address until at least one API key exists. `GET /about` and `GET /source` stay open so a network user can obtain the corresponding source.

The web UI calls this setting local network access. Turning it on stores `api_host` as `0.0.0.0` and turns the key check on immediately. The process keeps the socket it bound at startup until it is restarted, so quit and reopen Yggdrasil before other computers can connect. The daemon also rejects a settings change that enables that bind when no key exists.

Create a key from the web UI or `POST /api/v1/api-keys`. The response includes the secret once. The database stores a bcrypt hash and a prefix. The plaintext key is not written to disk. Revoke with `DELETE /api/v1/api-keys/{id}` and rotate with `POST /api/v1/api-keys/{id}/rotate`. Do not put the key in a URL or query string. Those requests are rejected.

`YGGDRASIL_API_KEY`, when set, is hashed at startup if that secret is not already valid. The Docker image binds `0.0.0.0` and will not start until that variable is set or a key is already in the data directory. The cluster compose file sets a local test key for that reason.

A bearer token on plain HTTP does not encrypt traffic. It stops anonymous use of a trusted LAN. TLS or mTLS for remote access is not implemented.

Bifrost, on port 7332, is a separate server. Its protected routes require a paired-node token. Pairing routes are intentionally callable before trust exists. See [clustering.md](clustering.md).

## Corresponding source

`GET /about` and `GET /source` return the same JSON and do not require an API key:

```json
{
  "name": "Yggdrasil Core",
  "version": "1.2.1",
  "commit": "abc1234",
  "license": "AGPL-3.0-or-later",
  "source": "https://github.com/yeixio/yggdrasil-core/tree/v1.2.1"
}
```

`GET /api/v1/version` includes `license` and `source` as well. The Settings page links to that source URL.

A release build, which sets `version.Version` from the tag, points `source` at `https://github.com/yeixio/yggdrasil-core/tree/v<version>`. A development build with a known commit points at `.../tree/<commit>`. A build with neither points at the repository itself.

If you distribute or operate a modified Yggdrasil Core over a network, set the source URL so users can obtain the corresponding source for your modified version:

```text
-X github.com/yeixio/yggdrasil-core/internal/version.SourceURL=<url-of-your-corresponding-source>
```

The same text is printed by `yggdrasil-daemon -version` and by `yggctl version` or `yggctl about`.

## Control plane

Prefix: `/api/v1`

| Method | Path | Purpose |
| --- | --- | --- |
| GET | `/health` | Process is up |
| GET | `/version` | Version, commit, license, and corresponding source |
| GET | `/hardware` | Host inventory |
| GET | `/models` | Catalog and installed models |
| POST | `/models/{id}/install` | Install a catalog model |
| POST | `/models/install-from-url` | Install a GGUF from a URL |
| POST | `/models/{id}/start` | Load a model |
| POST | `/models/{id}/stop` | Stop a model |
| GET | `/runtimes` | Registered runtimes and detection |
| POST | `/runtimes/{id}/install` | Install a runtime (`llamacpp`) |
| GET, POST | `/profiles` | List or create profiles |
| POST | `/chat` | Chat through a profile |
| POST | `/chat/stop` | Stop a conversation's running turn: `{"conversation_id": "..."}` returns `{"stopped": true}` when one was running |
| GET, POST | `/tasks` | Orchestration tasks |
| GET, POST | `/automations` | Scheduled prompts. Also `POST /automations/preview`, `GET/PATCH/DELETE /automations/{id}`, and `POST /automations/{id}/run|pause|resume` |
| GET | `/notifications` | The notification center: `{"notifications": [...], "unread": n}`. `?unread=1` lists unread ones. Also `POST /notifications/read` (`{"ids": [...]}`, no ids marks all) and `POST /notifications/{id}/dismiss` |
| GET | `/connectors` | Connected services and their status. Also `PUT /connectors/{id}` (`{"values": {...}}`), `POST /connectors/{id}/check`, and `DELETE /connectors/{id}` |
| GET, PUT | `/personalization` | How the person likes answers: `length`, `tone`, `format`, `units`, `about_me`, `instructions` |
| GET | `/capabilities` | The capability inventory. `?ask=` returns the abilities a question is about. Also `GET /capabilities/models/{id}`, which says which computers can run a model |
| GET | `/runs/{id}` | A run trace. Also `GET /runs` (`?conversation_id=`, `?limit=`), newest first |
| GET | `/egress` | What left this computer, newest first. `?conversation_id=` narrows to one chat |
| GET, PUT | `/privacy` | `{"retention_days": n, "last_30_days": {...}}`; PUT sets `retention_days`. Also `POST /privacy/delete-runs` |
| GET | `/mcp/servers` | MCP tool sources. Also `POST /mcp/servers` (add), `GET/PATCH/PUT/DELETE /mcp/servers/{id}`, `/check`, `/sign-in`, `/sign-out`, `/logs`, `/prompts`, `/resources`, and `GET /mcp/gallery`, `GET /mcp/import`, `POST /mcp/parse`, `GET /mcp/share`. See [MCP](mcp.md) |
| GET | `/nodes` | This computer and peers |
| POST | `/nodes/pair` | Start pairing |
| POST | `/nodes/{id}/pair/approve` | Approve a pairing offer |
| GET, POST | `/api-keys` | List metadata or create a key |
| GET, PATCH | `/settings` | Read or update settings |
| GET | `/diagnostics` | Diagnostic bundle |
| GET | `/events` | Server-sent event stream |
| GET, POST | `/benchmarks` | List or start a benchmark |
| GET, POST | `/knowledge/sources` | Connected knowledge (Mimir). Also `GET/PATCH/DELETE /knowledge/sources/{id}` and `POST /knowledge/sources/{id}/refresh` |
| POST | `/knowledge/search` | Passages that match a question |
| | | Uploads send `text`, or `content_base64` for binary files such as `.xlsx` and `.pdf`. The same field works for `/training/classify` and `/training/ais/{id}/materials`. |
| GET | `/knowledge/sources/{id}/content` | The copy kept for a pasted or uploaded source |
| GET, POST | `/training/ais` | Specialized AIs. Also `GET/PATCH/DELETE /training/ais/{id}` |
| POST | `/training/classify` | Recommend Training, Knowledge, or Both for material, before it is added |
| GET | `/training/base-models?goal=` | Trainable base models ranked for a job, with training fit |
| POST | `/training/ais/{id}/materials` | Add material. Also `DELETE /training/ais/{id}/materials/{mid}` and `POST /training/ais/{id}/conversations` |
| GET, POST | `/training/ais/{id}/examples` | Review examples with their flags. Also `PATCH/DELETE /training/ais/{id}/examples/{eid}` |
| GET | `/training/ais/{id}/plan` | What trains, what stays connected, and the training fit per computer |
| POST | `/training/ais/{id}/train` | Start a training job; an optional `{"node_id": "..."}` picks the computer. `GET /training/jobs/{id}`, `POST /training/jobs/{id}/cancel` |
| PUT | `/training/ais/{id}/test-prompts` | Replace the test set. `POST /training/ais/{id}/revisions/{n}/evaluate` compares base and specialized answers |
| POST | `/training/ais/{id}/revisions/{n}/deploy` | Deploy an evaluated revision. `POST /training/ais/{id}/undeploy` |
| POST | `/artifacts` | Upload a file to attach to a chat: `name` plus `text`, or `content_base64` for binary files. Also `GET/DELETE /artifacts/{id}`, `GET /artifacts/{id}/content` (the bytes, as a download), and `GET /conversations/{id}/artifacts` |
| GET, POST | `/memory` | Memories (Muninn). GET returns `memories` and `categories`. Also `PATCH/DELETE /memory/{id}`; PATCH takes `content`, `category`, and `enabled` |
| GET | `/training/deployed` | Deployed specialized AIs as models |
| POST | `/training/example` | Set up the example AI from the sample material, or return it if it exists. `GET /training/samples` returns the sample files |

Assistant messages from `GET /conversations/{id}/messages` carry `meta`: the `sources` an answer drew on (`web`, `knowledge`, or `file`, with title, URL or source name, and a snippet) and plain-language `steps` describing what was done. The same `meta` is on the `chat.complete` event.

Memory is on unless the setting `memory_enabled` is `false` or a conversation has `memory_off: true` (set with `PATCH /conversations/{id}`). A message that starts "Remember that…", "Forget…", or asks "What do you remember?" is answered without a model; the reply is saved as usual and the events `memory.saved` or `memory.deleted` follow. Other turns add the memories that fit the question to the instructions and list them as `memory` sources. A secret such as a password, key, or card number is not saved.

When a conversation's history passes half of the model's window, a summary of the older messages is written after the reply and replaces them in later turns. The messages stay saved. The `chat.summarized` event follows. The `chat.complete` payload's `context.summarized_messages` counts the messages the summary covers, and `pipeline_ms` is the time from the request to the first model call.

`POST /chat` takes `attachments`, a list of artifact ids. Chat reads documents, spreadsheets (`.csv`, `.tsv`, `.xlsx`), PDFs with a text layer, JSON, HTML, and code files. Images are not read yet. An attached file reaches the model as data in the user turn, the whole file when it fits and otherwise the parts that best match the question, and files attached or produced earlier in the chat add the passages that match later questions. The user message's `meta.files` lists its attachments.

The `files.create` tool, allowed by default in the built-in profiles, saves a file the user can download: a document, JSON, a spreadsheet (`.xlsx` is built from CSV text), or code. It writes only to Yggdrasil's file store. When a message asks for a file and the profile allows `files.create` without asking, Yggdrasil has the model write only the contents and saves the file itself, with a `chat.making_file` event. The answer's `meta.files` lists produced files. File content is served as an attachment with a sandboxing `Content-Security-Policy`, so HTML never runs on the API's origin.

When a model under 4B parameters answers from attached files or connected knowledge, `meta.notice` says it can mix up numbers and details and suggests a larger model. Auto treats a question about the user's files or knowledge as one that needs a careful answer, so it prefers a larger model that fits.

Each turn is offered only the tools it needs (spec §16). Huginn picks tool groups from the kind of request and cues in the message: web search for questions, files for a file name or folder, shell for "run" or "install", Git for "commit" or "branch". It then limits them to what the profile allows. A call to a tool that was not offered is refused, and the model is told which tools it has, so it cannot widen its own tools. The profile's Allow, Ask, and Deny still decide what runs. Tool ids have capability aliases, and `web.search`, `web.open`, `files.read`, `files.write`, `files.search`, and `shell.run` reach the built-in tools. A short answer that only writes out a call, such as `files.search {"query": "x"}`, is taken as the call when the tool was offered. Each call has a time limit (web 45 s, files 30 s, Git 90 s, shell 2 min), and `tool.failed` carries a `kind`: `timeout`, `cancelled`, `denied`, `not_offered`, `invalid`, or `failed`.

`POST /chat` takes `effort`: `auto` (the default), `fast`, `balanced`, or `thorough`. Effort sets a budget, not a number of calls the client sees:

- **Fast** answers in one go. It doesn't plan, a look-up uses search results without reading pages, figures are checked but not sent back for correction, and a turn may make 3 tool calls.
- **Balanced** plans requests with several parts, reads one page per look-up, corrects figures once, and allows 10 tool calls.
- **Thorough** reads two pages per look-up, corrects figures twice, allows 16 tool calls, and lets Auto pick the largest model that fits.
- **Auto** uses Fast for a quick question, Thorough for a request that needs a detailed answer, has several parts, or uses the user's files or knowledge, and Balanced otherwise.

The `chat.effort` event reports the effort used. A chosen effort is listed in the answer's `steps`.

Stopping a turn, with `POST /chat/stop` or by closing the stream, stops every model call, tool call, plan step, pending approval, and paired computer working on it. The part already written is saved as the answer, with `meta.notice` "Stopped before the answer was finished." A turn stopped in the middle of a plan keeps the notes of the parts that finished. A turn stopped before anything was written keeps a short note with the sources found so far. The `chat.stopped` event carries `conversation_id` and `kept`. A new message in the same chat stops a turn still running there.

A request with several parts is worked through in parts. "Compare A, B and C…" and "research A vs B" become one part per subject, looked up on the web side by side when web search is allowed without asking. "Do X, then Y, then Z" becomes parts in order, each seeing the notes before it. The final answer is written from the parts' notes, or a requested file is made from them. `plan.created` carries `steps` and `parallel`, and `plan.step` carries `index`, `step`, and `status` (`running`, `done`, or `failed`).

Before an answer is shown, its arithmetic is recomputed. When the turn used reference material or tool results, each figure must appear in, or follow from, the lines about the same subject. Dates and years are skipped. This check needs no model. Only an answer with issues is sent back to the model once, with the issues named (`chat.verifying`), and the revision is kept if it fixes some. `verify.done` reports `issues`, `fixed`, and `remaining`. Remaining figures become the answer's `meta.notice`. An answer that describes tool calls instead of answering is asked for again without tools.

A chat whose `model_id` is `auto` gets an installed model chosen for each message. The message is classified as a quick question, current information, coding, a detailed question, or a task on this computer. Auto then picks the largest suitable model that fits this computer's memory, keeps a quick question on a model that is already loaded, requires tool calling for current information and tasks, and skips a model that failed in the last 10 minutes. The `chat.model_routed` event carries `model_id`, `model_name`, and a plain-language `reason`, and the reason is the first of the answer's `steps`.

Before choosing a model, Auto checks the deployed specialized AIs. A chat goes to one when the message names it, or when at least two of the message's words, and at least 40% of them, are words the AI was trained on. Those words come from its name, its goal, and words that recur in its training questions. Requests that need current information, a task on this computer, or code never go to a specialized AI, because it answers without tools. An AI whose adapter file is missing on this computer, or whose base model is not installed, is skipped. Asking for it by id then fails with an explanation. When Auto routes to a specialized AI, `chat.model_routed` carries its `sai:` id and name.

A model can have a `support_role`: `embedding`, `reranker`, or `classifier`. These models serve Yggdrasil instead of chatting. The role comes from the catalog, or, for models installed by URL or from Hugging Face, from the model's name, purpose, or tags. Auto, fallback, and the default model never pick one. A chat that asks for one by id fails with an explanation.

When a question needs current information and the profile allows `internet.search` without asking, Yggdrasil searches the web and reads the best page before the model answers. The `chat.lookup` event carries the `query`. The results reach the model as data, and the model answers without web tools for that turn.

If the model fails before it shows or changes anything, the turn runs once more on another installed model. `chat.model_routed` then has `fallback: true`. The answer's steps say what happened, and `meta.notice` warns when the model that answered is noticeably smaller. A model whose `llama-server` exits while loading fails at once instead of after the 120-second readiness timeout.

A profile's `knowledge_sources` lists Mimir source ids. Chat searches them on every turn and adds the matching passages before the system prompt.

Knowledge search matches words (BM25). When an embedding model is installed (one with `support_role` `embedding`, such as `nomic-embed-text-v1.5-q8` in the catalog), it also matches meaning, so "What warranty do you offer?" finds a passage about a five-year guarantee. The embedding model runs in its own `llama-server` started with `--embedding`, loaded when first needed and unloaded by the idle sweeper like any model; it appears in `GET /models/running` with `mode` `embedding`. Passages are embedded in the background after a source is added or reindexed, and the vectors are kept in the daemon database. A reindex keeps the vectors of passages whose text did not change, and installing a different embedding model embeds every passage again. A source with more than 20,000 passages is searched by words only. Search never waits for embedding: it uses the vectors that exist.

Word and meaning matches are combined with reciprocal rank fusion. A passage found only by meaning must be similar enough to the question, and close to the best match. When words found something, it must also be at least as similar to the question as the best word match, so a question that names one product does not bring in every similar row. When a reranker model is installed (`support_role` `reranker`), it reorders the top 16 passages. Each hit from `POST /knowledge/search` has `match`: `keyword`, `semantic`, or `both`. `score` orders hits within one search only: BM25 for word-only search, the fused rank when meaning is used, and the reranker's score for passages it ordered. A knowledge source reports `embedded_count` and `embedding_model`. With no embedding model installed, if it cannot start, or while training is using the computer, search uses words only.

Notifications come from Gjallarhorn. Each one is stored first and then delivered to its channels. A notification has `category` (`automation`, `approval`, `model`, `training`, `health`, or `system`), `severity` (`info`, `success`, `warning`, or `error`), `title`, `body`, and a `link` back to its source in the app, such as `/automations?id=…`. Each channel's attempt is recorded in `deliveries`. A delivery is `delivered`, `failed`, or `suppressed`; for example, desktop notices are suppressed when `notify_task_finish` is off. A repeat with the same source within 10 minutes is folded into the first notification and marked unread again. The `notification.created` event carries `id`, `category`, `severity`, `title`, `body`, and `link`. Finished and failed automations post to the desktop too. Model downloads, training deploys, and pairings stay in the app.

An automation's `notification.mode` is `condition`, `change`, `always`, `failure` (only failed runs), or `none`. An automation runs on the same stack as chat: `model_id` `auto` picks a model for each run, and memories and connected knowledge are used the same way. Tools follow the unattended policy, because nobody is there to approve them. Tools listed in the automation's `tools` were approved when it was saved, and they run even if they change things. With no `tools`, only read-only tools the profile allows without asking can run. A tool the profile denies never runs. When a run reaches a tool that was not approved, the tool is skipped and the run continues. The run's `automation.completed` event lists the tool in `skipped`, and an `approval` notification says which tools to approve.

Chat, automations, knowledge indexing, benchmarks, and training share this computer in that order of priority. A chat or API request, including a request from a paired computer, never waits. An automation run waits while a chat is running, and for 20 seconds after one, so it does not load a model between someone's messages. A benchmark also waits for automations, and it waits again before each model, because loading a model unloads the others. Background embedding of knowledge passages waits for chat and automations before each small batch, and does not start while training runs. Training waits for all of them before it unloads models to free memory. While work waits, `work.waiting` carries `class`, `label`, and `reason` ("Waiting for your chat to finish"), and the benchmark's progress or the training job's detail shows the reason. A chat that arrives during training is still answered. Its steps say `Training "…" is using this computer (about N minutes left)`. If its model runs out of memory, the error says so, with the estimate and what to do. Out-of-memory failures are not retried. An automation that runs out of memory twice in a row is paused, and its notification explains why.

Connected services add tools. Today they are GitHub (`github.search`, `github.issue`, `github.comment`) and Home Assistant (`homeassistant.states`, `homeassistant.call`).
- **Connecting:** `PUT /connectors/{id}` checks the values with the service before storing anything, and returns the account it connected as. A blank secret field keeps the stored value.
- **Storage:** credentials are stored in the `secrets` directory of the data directory, not in the database. They are added to a request only when a tool runs, so they are never part of model context, events, or tool arguments.
- **Responses:** the API never returns a secret value; `values` shows a stored token as its last four characters only. Results and errors are scrubbed of any credential value before the model sees them.
- **Policies:** connected tools join the tool catalog with `source` `connector:<id>`. Reading is allowed and changes ask first, unless a profile sets its own policy for the tool.
- **Selection:** they are offered when a message is about the service. That means it names the service, or uses words like issues or pull requests for GitHub and lights or sensors for Home Assistant. It also counts when the message uses words the service taught: Home Assistant's device names, learned when it connects and whenever all devices are read. Tools that change something are offered only when the message asks for a change ("turn on", "comment").
- **Fetched first:** a read tool marked `prefetch` (Home Assistant's device list) is called before the model answers a message about its service, when the profile allows it without asking. Its data, written as plain lines, replaces the web look-up for that turn.
- **Untrusted data:** what they return is treated as untrusted data (§58), and links in results become sources.

MCP tool sources add tools the same way, with `source` `mcp:<source>`; secrets are kept in `secrets/mcp-<source>.json`. `/mcp` (outside `/api/v1`) is Yggdrasil's own MCP server for other apps, checked like `/v1`, and `/mcp/oauth/callback` is where a tool source's sign-in returns. See [MCP](mcp.md).

Personalization shapes how answers look in every chat, automation, and API request. It has four choices: `length` (`brief`, `balanced`, `detailed`), `tone` (`friendly`, `neutral`, `direct`), `format` (`prose`, `lists`), and `units` (`metric`, `imperial`). It also has two short notes, `about_me` and `instructions`, of up to 1,500 characters each. It is stored as a setting and added to the model's instructions as style guidance, after a specialized AI's own instructions. It is kept apart from permissions: what a tool may do comes only from profiles and Settings. A personalization note or a memory that tries to grant a permission is refused with 400, for example "you can always push without asking" or "don't ask before running commands". The refusal says where permissions are set. A memory that states a preference, such as "I use the terminal a lot", is saved and changes no policy.

Each run records what left this computer.
- **Record kinds:** `web_search` (the query), `web_page` (the address), `paired_computer` (the prompt and context, or training examples), `external_server` (a chat sent to a server that is not on this computer), and `connector` (the service and what it was asked; long text such as a comment's body is left out).
- **Record fields:** `source` (`chat`, `api`, `automation`, `training`), plus `conversation_id` and `task_id` when there are any.

Memories and knowledge sources have `local_only`. Set it with `PATCH /memory/{id}` or the knowledge update, `{"local_only": true}`. A turn that uses a local-only memory or a passage from a local-only source runs on this computer, even when placement would have chosen a paired computer, and its steps say so.

Run records hold prompts and tool results: tasks and their steps, automation run results, and the egress record.
- **Retention:** they are kept for `retention_days` (30 by default; 0 keeps them) and removed daily.
- **Delete now:** `POST /privacy/delete-runs` removes them now and returns how many.
- **Exceptions:** each automation keeps its latest successful result, which the `change` notification mode compares against, and work that may still be running is never removed.
- **Chats:** chats are not run records; the `save_chat_history` setting covers them.

Every chat turn, API request, and automation run is traced. A chat or API run's id is the turn's task id, and the answer's `meta.run_id` names it. A run has:
- `strategy`: how it was handled, such as "Looked up the web first" or "Worked through 3 parts side by side". It also includes the routing reason and a fallback, when there was one.
- `effort`.
- `models`: per model, role, and computer. Each entry has the calls, `load_ms` (a real model start of 150 ms or more), `first_token_ms`, `ttft_ms`, tokens in and out, `cached_tokens` (from llama.cpp's cache), and tokens per second.
- `tools`: calls, failures, and total time.
- `nodes`, `workers` and `parallel` for a plan, verification passes with issues and fixes, and `retries`.
- `context_tokens` of `context_limit`, `latency_ms`, and `pipeline_ms`.
- `status`: `completed`, `failed`, or `stopped`.

In advanced mode, an answer has "Run details". Runs are run records, so the retention and delete action above apply to them.

A profile's `orchestration` object holds its advanced controls. Every field is optional; empty keeps the default, which follows the chat's effort.

| Field | Values | Effect |
| --- | --- | --- |
| `effort` | `fast`, `balanced`, `thorough` | The profile's effort when a chat leaves effort on Auto |
| `planning` | `on`, `off` | Work through requests with several parts in parts, whatever the effort |
| `max_workers` | 2–8 | Most parts in a plan |
| `parallel` | `on`, `off` | `off` works through parts one at a time |
| `verification` | `off`, `check`, `correct`, `thorough` | `off` skips the figure check; `check` reports only; `correct` and `thorough` allow one or two correction passes |
| `max_tool_calls` | 1–50 | Most tool calls in one turn |
| `memory` | `off` | Keeps persistent memory out of the profile's chats |
| `context_share` | 0.1–0.9 | Most of the model's window earlier messages may use |
| `fallback` | `off` | Shows a failure instead of answering on another model |
| `timeout_seconds` | 10–3600 | Stops a turn that runs longer; the answer so far is kept and says it reached the time limit |

Invalid values are refused with 400. In advanced mode, the profile editor has an Orchestration section, alongside model roles, tools, knowledge, and placement.

Structured results are checked the same way elsewhere:
- **Tool arguments:** they are checked against each tool's schema before the tool runs. Safe repairs are made, such as `"7"` for a whole number, or JSON data where text is expected. A call with an argument of the wrong type is refused with `kind` `invalid`, naming the argument, so the model can call again.
- **Automations:** a condition automation's result must end with the JSON its condition reads: `{"price": number}` for a threshold, or `{"significant": boolean}`. That JSON is read with the same repairs. When it is missing or wrong, the model is asked once to supply it from its own answer. Notices show the prose, never the JSON.

The capability inventory lists what exists right now:
- `models` (with the computers they are on and whether they are running), `nodes` (online, memory, whether they can train), and `tools` from every source (built in, connected services, MCP), enabled or not;
- `connectors`, `providers` (runtimes and MCP tool sources, with health), and stored `artifacts`;
- `abilities`, each with `available`, the tools or models it comes `via`, and a `note` saying how it works or what would make it possible. Abilities are worked out from tools and models by what they do, so a new MCP tool that sends email counts as email without code.

A chat question about what Yggdrasil can do, such as "Can you generate an image?", "Do you have access to my email?", or "Which computer can run Qwen 2.5 14B?", gets the matching facts as trusted instructions. The model answers from them instead of guessing, and the answer's steps say so. In the app, Diagnostics shows the same list.

Three behaviors come from the quality test set (`tests/quality`):
- **Plain questions:** a plain question is answered without tools; the message must ask for a search, a file, a command, and so on.
- **Capability questions:** a short question about what Yggdrasil can do is answered straight from the capability inventory, without a model.
- **False claims:** an answer that says it changed something, when no tool that changes things ran, gets the notice "Nothing was changed: no tool ran to do this, whatever the answer says."

Model, node, tool, conversation, and log routes follow the same prefix. The OpenAPI file is the route list to diff when a handler changes.

## OpenAI-compatible API

Implemented routes:

| Method | Path |
| --- | --- |
| GET | `/v1/models` |
| POST | `/v1/chat/completions` |

No other `/v1` routes are registered. Embeddings, image generation, and the legacy completions API are not implemented.

### `GET /v1/models`

Returns `auto` and profiles, not raw files on disk:

```json
{
  "object": "list",
  "data": [
    {"id": "auto", "object": "model", "owned_by": "yggdrasil"},
    {"id": "profile:general-assistant", "object": "model", "owned_by": "yggdrasil"}
  ]
}
```

Built-in profile ids include `general-assistant`, `programming`, and `research`.

### `POST /v1/chat/completions`

```json
{
  "model": "profile:general-assistant",
  "messages": [{"role": "user", "content": "Hello"}],
  "stream": false
}
```

`model` may be `profile:<id>`, `auto`, or a bare id. A bare id is used as a profile id when that profile exists, and otherwise as a model override. `auto` picks an installed model for each request, as in chat.

The API gets the same assistant as chat: planning, web look-ups, connected services, answer checks, personalization, and specialized AIs.
- **Messages:** the whole `messages` array is used. The last `user` message is the turn, and earlier `user` and `assistant` messages are its history. `system` (and `developer`) messages are the calling app's instructions. They cannot change what tools may do.
- **Effort:** `reasoning_effort` maps to effort. `minimal` and `low` give Fast, `medium` gives Balanced, and `high` gives Thorough.

The optional `yggdrasil` object holds the assistant's own controls:

```json
{
  "model": "auto",
  "messages": [{"role": "user", "content": "What did we decide about the release?"}],
  "yggdrasil": {
    "memory": true,
    "knowledge": true,
    "knowledge_sources": ["<mimir source id>"],
    "tools": ["internet.search", "internet.open"],
    "effort": "thorough",
    "placement": "local",
    "progress": true
  }
}
```

| Field | Meaning |
| --- | --- |
| `memory` | Use the person's memories. API requests use them only when they ask, unless the key says otherwise. "Remember that …" saves a memory only when memory is on for the request. |
| `knowledge` | Use the profile's connected knowledge. `knowledge_sources` adds sources. |
| `tools` | Narrow the profile's tools to these ids. A request can never add a tool or loosen a policy. |
| `effort` | `auto`, `fast`, `balanced`, or `thorough`; it takes precedence over `reasoning_effort`. |
| `placement` | `local` or `automatic`. |
| `progress` | With `"stream": true`, progress and tool activity arrive as chunks with an empty `delta` and a `yggdrasil.event`, such as `{"type": "tool.started", "tool_id": "internet.search"}`. Before `[DONE]`, a last such chunk carries `yggdrasil.sources`, `steps`, `notice`, and `files`. Clients that ignore unknown fields see a plain OpenAI stream. |

`response_format` asks for JSON:
- **Types:** `{"type": "json_object"}`, or `{"type": "json_schema", "json_schema": {"schema": {...}}}` with a JSON Schema. Yggdrasil checks `type`, `properties`, `required`, `enum`, and `items`.
- **Constrained output:** the model is told the shape. On this computer, llama.cpp also constrains the reply to the schema with a grammar. Such a turn is one reply: the web look-up still runs first, but there is no plan, tool call, file, or figure check.
- **Repairs:** the answer's JSON is found (fenced or not) and safely repaired: trailing commas, curly quotes, `"$1,299"` for a number, `"yes"` for a boolean.
- **Retry:** an answer that still does not fit is asked for once more, with the problems named.
- **Result:** the response content is compact JSON. When nothing fits, the request fails with 422 and lists the problems. A streamed request gets the JSON as one chunk.

A non-streaming response has a `yggdrasil` object with the answer's `sources`, `steps`, `notice`, and `files` when there are any.

Each API key has `permissions` that a request can only narrow:
- `memory` and `knowledge` are `never`, `on_request`, or `always`. The defaults are `on_request` for memory and `always` for knowledge.
- `tools` is `profile` (the default), `read_only`, or `none`.
- `placement` (true by default) lets a request choose where it runs.

Asking for something a key does not allow returns 403 and says what was refused. Change a key's permissions with `PUT /api/v1/api-keys/{id}/permissions` or on the API Access page; rotating a key keeps them. When the API listens beyond this computer, every request needs a key, so its limits always apply. On this computer a key is optional; its limits apply when the app sends it, and a request without one gets the defaults.

### Streaming

`"stream": true` responds with `Content-Type: text/event-stream`. Each event is `data: {json}` and the stream ends with `data: [DONE]`.

### Known differences

- `temperature` and `max_tokens` are accepted and ignored.
- `tool` messages and assistant `tool_calls` in the history are skipped.
- Tool definitions in the OpenAI request are not passed through. Tool use is controlled by the Yggdrasil profile.
- The non-streaming `id` is the fixed string `chatcmpl-ygg`.
- A model must already be installed and startable. The HTTP call does not download one for you.

Examples that match this behavior are in [examples/](../examples/).
