# API

The daemon listens on `http://127.0.0.1:7331` unless `config.json` or `YGGDRASIL_API_HOST` / `YGGDRASIL_API_PORT` say otherwise. This page lists every route and records behavior that matters when you call the server. [api/openapi.yaml](../api/openapi.yaml) describes every route below in OpenAPI 3.0. A test fails when a route is added to the daemon without it. [CLI](cli.md) and [Configuration](configuration.md) cover the command line and settings.

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
  "version": "1.4.0",
  "commit": "abc1234",
  "license": "AGPL-3.0-or-later",
  "source": "https://github.com/yeixio/yggdrasil-core/tree/v1.4.0"
}
```

`GET /api/v1/version` includes `license` and `source` as well. The Settings page links to that source URL.

A release build, which sets `version.Version` from the tag, points `source` at `https://github.com/yeixio/yggdrasil-core/tree/v<version>`. A development build with a known commit points at `.../tree/<commit>`. A build with neither points at the repository itself.

If you distribute or operate a modified Yggdrasil Core over a network, set the source URL so users can obtain the corresponding source for your modified version:

```text
-X github.com/yeixio/yggdrasil-core/internal/version.SourceURL=<url-of-your-corresponding-source>
```

The same text is printed by `yggdrasil-daemon -version` and by `yggctl version` or `yggctl about`.

## Route reference

Control-plane routes are under `/api/v1`. The OpenAI-compatible routes are under `/v1`, and Yggdrasil's MCP server is `/mcp` (see [MCP](mcp.md)). `GET /about` and `GET /source` are at the root.

### System

| Method | Path | Purpose |
| --- | --- | --- |
| GET | `/health` | The process is up |
| GET | `/version` | Version, commit, license, corresponding source, and client `contract` |
| GET | `/hardware` | This computer's CPU, memory, disk, and accelerators |
| GET, PATCH | `/settings` | Read or change settings (see [Configuration](configuration.md#settings)). `PUT` is accepted as `PATCH`. |
| POST | `/settings/reset` | Clear application state; `delete_models=true` also removes model files |
| GET, POST | `/diagnostics` | Build the diagnostic bundle, which omits secrets. `?include_conversations=true` adds chats. |
| GET | `/logs` | Log files. `GET /logs/{name}` returns one; `?tail_bytes=` limits it to the end. |
| GET | `/events` | Server-sent event stream (see [Events](#events)) |
| GET | `/capabilities` | The capability inventory. `?ask=` returns the abilities a question is about. `GET /capabilities/models/{id}` says which computers can run a model. |
| GET | `/caches` | Every cache with its policy and counts. `POST /caches/{name}/clear` clears one. |
| GET | `/performance` | Recent generation runs with timings. `?sort=`, `?order=`, `?limit=` |

### Chat

| Method | Path | Purpose |
| --- | --- | --- |
| POST | `/chat` | Send a message through a profile. Takes `conversation_id`, `message`, `profile_id`, `model_id` (`auto` for Auto), `effort`, `attachments` (artifact ids), `execution` (where it runs), and `stream`. Progress arrives on `/events`. |
| POST | `/chat/stop` | Stop a conversation's running turn: `{"conversation_id": "..."}` returns `{"stopped": true}` when one was running |
| GET, POST | `/conversations` | List or create chats |
| PATCH, DELETE | `/conversations/{id}` | Change a chat's `title`, `profile_id`, `model_id`, or `memory_off`, or delete it with its files |
| GET | `/conversations/{id}/messages` | A chat's messages, with each answer's `meta` |
| GET | `/conversations/{id}/artifacts` | Files attached to or made in a chat |
| POST | `/artifacts` | Upload a file to attach: `name` plus `text`, or `content_base64` for binary files |
| GET, DELETE | `/artifacts/{id}` | A file's details, or delete it. `GET /artifacts/{id}/content` returns the bytes as a download. |
| GET, POST | `/memory` | Memories (Muninn). GET returns `memories` and `categories`. |
| PATCH, DELETE | `/memory/{id}` | Change a memory's `content`, `category`, `enabled`, or `local_only`, or delete it |
| GET, PUT | `/personalization` | How answers should look: `length`, `tone`, `format`, `units`, `about_me`, `instructions` |
| GET | `/runs` | Run traces, newest first. `?conversation_id=`, `?limit=`. `GET /runs/{id}` returns one. |
| GET, POST | `/tasks` | Orchestration tasks. `GET /tasks/{id}` returns one with its steps. |

### Models and runtimes

| Method | Path | Purpose |
| --- | --- | --- |
| GET | `/models` | The catalog and installed models, with `support_role` and fit |
| GET | `/models/fit` | How well each catalog model fits each computer |
| GET | `/models/recommend` | A recommended setup. `?purpose=` (`general`, `coding`, `research`) |
| GET | `/models/browse` | Search Hugging Face GGUF models. `?q=`, `?limit=` |
| POST | `/models/{id}/install` | Install a catalog model |
| POST | `/models/install-from-url` | Install a GGUF from a URL |
| DELETE | `/models/{id}` | Remove an installed model |
| POST | `/models/{id}/start`, `/models/{id}/stop` | Load or unload a model |
| GET | `/models/running` | Loaded models, with `mode` (`embedding` or `reranking` for supporting models) |
| GET | `/runtimes` | Runtimes and their detection |
| POST | `/runtimes/{id}/install` | Install a runtime (`llamacpp`) |

### Profiles and tools

| Method | Path | Purpose |
| --- | --- | --- |
| GET, POST | `/profiles` | List or create profiles |
| GET, PATCH, DELETE | `/profiles/{id}` | Read, change, or delete a profile, including its roles, tools, `knowledge_sources`, and `orchestration` |
| GET | `/tools` | Every tool from every source, with its `source`, permission, and whether it is enabled |
| POST | `/tools/{id}/enabled` | Turn a tool on or off everywhere |
| POST | `/tools/{id}/test` | Run a tool with `args` and return its result |
| POST | `/tools/decide` | Answer an Ask approval: `request_id`, `allow`, and `allow_session` |
| GET | `/tools/activity` | Recent tool calls, kept in memory for this process |
| GET | `/connectors` | Connected services and their status |
| PUT, DELETE | `/connectors/{id}` | Connect a service (`{"values": {...}}`), or disconnect it. `POST /connectors/{id}/check` tests the stored credential. |
| GET, POST | `/mcp/servers` | MCP tool sources, or add one |
| GET, PATCH, PUT, DELETE | `/mcp/servers/{id}` | Read, change, replace, or remove a source |
| POST | `/mcp/servers/{id}/check`, `/sign-in`, `/sign-out` | Test a source, or start or end its browser sign-in |
| GET | `/mcp/servers/{id}/logs`, `/prompts`, `/resources` | A source's log, prompts, and resources. `POST /mcp/servers/{id}/prompts/{name}` fills in a prompt. |
| GET | `/mcp/gallery` | Known MCP servers to add |
| GET | `/mcp/import` | MCP servers set up in other apps on this computer, with secrets hidden |
| POST | `/mcp/parse` | Read pasted MCP configuration into sources, and what each still needs |
| GET | `/mcp/share` | What other apps need to use Yggdrasil over MCP: `url`, `command`, `args`, `needs_key` |

### Knowledge

| Method | Path | Purpose |
| --- | --- | --- |
| GET, POST | `/knowledge/sources` | Connected knowledge (Mimir), or add a source: `kind` `path`, `text`, `database`, or `api` |
| GET, PATCH, DELETE | `/knowledge/sources/{id}` | Read, change (`name`, `text`, `local_only`, `remote`), or remove a source |
| POST | `/knowledge/sources/{id}/refresh` | Reindex now |
| GET | `/knowledge/sources/{id}/content` | The copy kept for a pasted or uploaded source |
| POST | `/knowledge/search` | Passages that match a question |

Uploads send `text`, or `content_base64` for binary files such as `.xlsx` and `.pdf`. The same field works for `/training/classify` and `/training/ais/{id}/materials`.

### Training

| Method | Path | Purpose |
| --- | --- | --- |
| GET | `/training/backends` | Trainers, and whether this computer can use each, with the reason |
| GET | `/training/base-models` | Trainable base models ranked for a job. `?goal=` |
| POST | `/training/classify` | Recommend Training, Knowledge, or Both for material, before it is added |
| GET, POST | `/training/ais` | Specialized AIs, or create one |
| GET, PATCH, DELETE | `/training/ais/{id}` | Read, change, or delete a specialized AI and its adapters and exports |
| POST | `/training/ais/{id}/materials` | Add material. `DELETE /training/ais/{id}/materials/{mid}` removes it. |
| POST | `/training/ais/{id}/conversations` | Add saved chats as material |
| GET, POST | `/training/ais/{id}/examples` | Examples with their flags, or add one. `PATCH`/`DELETE /training/ais/{id}/examples/{eid}` |
| GET | `/training/ais/{id}/plan` | What trains, what stays connected, and the training fit per computer |
| POST | `/training/ais/{id}/train` | Start a training job; an optional `{"node_id": "..."}` picks the computer |
| GET | `/training/jobs` | Training jobs. `GET /training/jobs/{id}` returns one; `POST /training/jobs/{id}/cancel` cancels it. |
| PUT | `/training/ais/{id}/test-prompts` | Replace the test set |
| POST | `/training/ais/{id}/revisions/{n}/evaluate` | Compare base and specialized answers |
| POST | `/training/ais/{id}/revisions/{n}/deploy` | Deploy an evaluated revision. `POST /training/ais/{id}/undeploy` |
| GET, POST, DELETE | `/training/ais/{id}/revisions/{n}/export` | Merge a revision into one standalone GGUF, see its status, or delete the file. `GET /training/ais/{id}/revisions/{n}/export/file` downloads it. |
| GET | `/training/deployed` | Deployed specialized AIs as models |
| POST | `/training/example` | Set up the example AI from the sample material, or return it. `GET /training/samples` returns the sample files. |

### Automations and notifications

| Method | Path | Purpose |
| --- | --- | --- |
| GET, POST | `/automations` | Scheduled prompts, or create one |
| POST | `/automations/preview` | Run an automation once without saving it |
| GET, PATCH, DELETE | `/automations/{id}` | An automation with its history, change it, or delete it |
| POST | `/automations/{id}/run`, `/pause`, `/resume` | Run now, pause, or resume |
| GET | `/notifications` | The notification center: `{"notifications": [...], "unread": n}`. `?unread=1` lists unread ones. |
| POST | `/notifications/read` | Mark notifications read: `{"ids": [...]}`; no ids marks all |
| POST | `/notifications/{id}/dismiss` | Dismiss one |

### Privacy

| Method | Path | Purpose |
| --- | --- | --- |
| GET | `/egress` | What left this computer, newest first. `?conversation_id=` narrows to one chat. |
| GET, PUT | `/privacy` | `{"retention_days": n, "last_30_days": {...}}`; PUT sets `retention_days` |
| POST | `/privacy/delete-runs` | Remove run records now, and return how many |

### Computers

| Method | Path | Purpose |
| --- | --- | --- |
| GET | `/nodes` | This computer and the others Yggdrasil knows |
| POST | `/nodes/refresh` | Look for computers again |
| POST | `/nodes/pair` | Ask a discovered computer to pair |
| GET | `/nodes/pairing/pending` | Pairing requests waiting for approval on this computer |
| POST | `/nodes/{id}/pair/approve` | Approve a pairing request |
| POST | `/nodes/pair/claim` | Pair with a code shown on the other computer: `node_id` and `code` |
| POST | `/nodes/pairing/offer` | Receive a pairing offer from another computer |
| GET | `/nodes/pairing/outbound/{code}` | The status of a pairing this computer started |
| POST | `/nodes/{id}/revoke` | Remove a paired computer |

### API keys and benchmarks

| Method | Path | Purpose |
| --- | --- | --- |
| GET, POST | `/api-keys` | List key metadata, or create a key (the secret is returned once) |
| DELETE | `/api-keys/{id}` | Revoke a key |
| POST | `/api-keys/{id}/rotate` | Replace a key's secret; its permissions are kept |
| PUT | `/api-keys/{id}/permissions` | Set what requests with the key may use |
| GET, POST | `/benchmarks` | Benchmark runs, or start one |
| GET | `/benchmarks/workloads` | The workloads a benchmark can run |
| GET | `/benchmarks/{id}` | One benchmark. `POST /benchmarks/{id}/cancel` stops it. |

## Chat turns

Assistant messages from `GET /conversations/{id}/messages` carry `meta`: the `sources` an answer drew on (`web`, `knowledge`, or `file`, with title, URL or source name, and a snippet) and plain-language `steps` describing what was done. The same `meta` is on the `chat.complete` event.

`POST /chat` takes `attachments`, a list of artifact ids. Chat reads documents, spreadsheets (`.csv`, `.tsv`, `.xlsx`), PDFs with a text layer, JSON, HTML, and code files. Images are not read yet. A scanned PDF attached to a chat is refused with a pointer to the Knowledge page, which reads scanned pages once with text recognition, instead of on every turn. An attached file reaches the model as data in the user turn, the whole file when it fits and otherwise the parts that best match the question, and files attached or produced earlier in the chat add the passages that match later questions. The user message's `meta.files` lists its attachments.

The `files.create` tool, allowed by default in the built-in profiles, saves a file the user can download: a document, JSON, a spreadsheet (`.xlsx` is built from CSV text), or code. It writes only to Yggdrasil's file store. When a message asks for a file and the profile allows `files.create` without asking, Yggdrasil has the model write only the contents and saves the file itself, with a `chat.making_file` event. The answer's `meta.files` lists produced files. File content is served as an attachment with a sandboxing `Content-Security-Policy`, so HTML never runs on the API's origin.

When a model under 4B parameters answers from attached files or connected knowledge, `meta.notice` says it can mix up numbers and details and suggests a larger model. Auto treats a question about the user's files or knowledge as one that needs a careful answer, so it prefers a larger model that fits.

### Auto and specialized AIs

A chat whose `model_id` is `auto` gets an installed model chosen for each message. The message is classified as a quick question, current information, coding, a detailed question, or a task on this computer. Auto then picks the largest suitable model that fits this computer's memory, keeps a quick question on a model that is already loaded, requires tool calling for current information and tasks, and skips a model that failed in the last 10 minutes. The `chat.model_routed` event carries `model_id`, `model_name`, and a plain-language `reason`, and the reason is the first of the answer's `steps`.

Before choosing a model, Auto checks the deployed specialized AIs. A chat goes to one when the message names it, or when at least two of the message's words, and at least 40% of them, are words the AI was trained on. Those words come from its name, its goal, and words that recur in its training questions. Requests that need current information, a task on this computer, or code never go to a specialized AI, because it answers without tools. An AI whose adapter file is missing on this computer, or whose base model is not installed, is skipped. Asking for it by id then fails with an explanation. When Auto routes to a specialized AI, `chat.model_routed` carries its `sai:` id and name.

A model can have a `support_role`: `embedding`, `reranker`, or `classifier`. These models serve Yggdrasil instead of chatting. The role comes from the catalog, or, for models installed by URL or from Hugging Face, from the model's name, purpose, or tags. Auto, fallback, and the default model never pick one. A chat that asks for one by id fails with an explanation.

If the model fails before it shows or changes anything, the turn runs once more on another installed model. `chat.model_routed` then has `fallback: true`. The answer's steps say what happened, and `meta.notice` warns when the model that answered is noticeably smaller. A model whose `llama-server` exits while loading fails at once instead of after the 120-second readiness timeout.

### Effort, plans, checks, and Stop

`POST /chat` takes `effort`: `auto` (the default), `fast`, `balanced`, or `thorough`. Effort sets a budget, not a number of calls the client sees:

- **Fast** answers in one go. It doesn't plan, a look-up uses search results without reading pages, figures are checked but not sent back for correction, and a turn may make 3 tool calls.
- **Balanced** plans requests with several parts, reads one page per look-up, corrects figures once, and allows 10 tool calls.
- **Thorough** reads two pages per look-up, corrects figures twice, allows 16 tool calls, and lets Auto pick the largest model that fits.
- **Auto** uses Fast for a quick question, Thorough for a request that needs a detailed answer, has several parts, or uses the user's files or knowledge, and Balanced otherwise.

The `chat.effort` event reports the effort used. A chosen effort is listed in the answer's `steps`.

A request with several parts is worked through in parts. "Compare A, B and C…" and "research A vs B" become one part per subject, looked up on the web side by side when web search is allowed without asking. "Do X, then Y, then Z" becomes parts in order, each seeing the notes before it. The final answer is written from the parts' notes, or a requested file is made from them. `plan.created` carries `steps` and `parallel`, and `plan.step` carries `index`, `step`, and `status` (`running`, `done`, or `failed`).

Before an answer is shown, its arithmetic is recomputed. When the turn used reference material or tool results, each figure must appear in, or follow from, the lines about the same subject. Dates and years are skipped. This check needs no model. Only an answer with issues is sent back to the model once, with the issues named (`chat.verifying`), and the revision is kept if it fixes some. `verify.done` reports `issues`, `fixed`, and `remaining`. Remaining figures become the answer's `meta.notice`. An answer that describes tool calls instead of answering is asked for again without tools.

Stopping a turn, with `POST /chat/stop` or by closing the stream, stops every model call, tool call, plan step, pending approval, and paired computer working on it. The part already written is saved as the answer, with `meta.notice` "Stopped before the answer was finished." A turn stopped in the middle of a plan keeps the notes of the parts that finished. A turn stopped before anything was written keeps a short note with the sources found so far. The `chat.stopped` event carries `conversation_id` and `kept`. A new message in the same chat stops a turn still running there.

### Context, memory, and look-ups

Window budgets are counted in tokens with the answering model's tokenizer (llama-server's `/tokenize`) while that model runs on this computer, and estimated at about four characters per token otherwise; `context.estimated` is `true` only when the gauge used the estimate. `context.prompt_tokens` is the whole prompt, including tokens llama-server reused from its cache, and tokens no section accounts for, such as the chat template, count as `instructions`. When a conversation's history passes half of the model's window, a summary of the older messages is written after the reply and replaces them in later turns. The messages stay saved. The `chat.summarized` event follows. The `chat.complete` payload's `context.summarized_messages` counts the messages the summary covers, and `pipeline_ms` is the time from the request to the first model call.

Memory is on unless the setting `memory_enabled` is `false` or a conversation has `memory_off: true` (set with `PATCH /conversations/{id}`). A message that starts "Remember that…", "Forget…", or asks "What do you remember?" is answered without a model; the reply is saved as usual and the events `memory.saved` or `memory.deleted` follow. Other turns add the memories that fit the question to the instructions and list them as `memory` sources. A secret such as a password, key, or card number is not saved.

When a question needs current information and the profile allows `internet.search` without asking, Yggdrasil searches the web and reads the best page before the model answers. The `chat.lookup` event carries the `query`. The results reach the model as data, and the model answers without web tools for that turn.

### Tools

Each turn is offered only the tools it needs (spec §16). Huginn picks tool groups from the kind of request and cues in the message: web search for questions, files for a file name or folder, shell for "run" or "install", Git for "commit" or "branch". It then limits them to what the profile allows. A call to a tool that was not offered is refused, and the model is told which tools it has, so it cannot widen its own tools. The profile's Allow, Ask, and Deny still decide what runs. Tool ids have capability aliases, and `web.search`, `web.open`, `files.read`, `files.write`, `files.search`, and `shell.run` reach the built-in tools. A short answer that only writes out a call, such as `files.search {"query": "x"}`, is taken as the call when the tool was offered. Each call has a time limit (web 45 s, files 30 s, Git 90 s, shell 2 min), and `tool.failed` carries a `kind`: `timeout`, `cancelled`, `denied`, `not_offered`, `invalid`, or `failed`.

See [Tools](tools.md) for policies and the tool loop.

## Knowledge

A profile's `knowledge_sources` lists Mimir source ids. Chat searches them on every turn and adds the matching passages to the user turn as labelled reference material, never to the system prompt, and the model is told not to follow instructions inside them (§58). A specialized AI's knowledge sources are searched when it answers, and an API request can add sources with `yggdrasil.knowledge_sources`.

A knowledge source can read current data from a database or a web API instead of a file. `POST /knowledge/sources` takes `kind` `database` with `remote` `{"driver": "sqlite", "database": "~/shop.db", "query": "SELECT …"}`, or `driver` `postgres` or `mysql` with `connection_string`. It takes `kind` `api` with `remote` `{"url": "https://…", "items": "data.products", "headers": {"Authorization": "Bearer …"}}`. `refresh_minutes` (1 to 10080, default 60) sets how old the data may get.

- **Queries:** a query must be one `SELECT`, `WITH`, or `VALUES` statement. It runs in a read-only transaction, and a SQLite file is opened read-only, so Yggdrasil never changes the database. Each row becomes a passage labelled with its column names, and a query that returns more than 200,000 rows is refused.
- **APIs:** a JSON response that is a list of objects, or holds exactly one such list (or the list at `items`), becomes one passage per object. Other JSON, CSV, HTML, and text are read like files. Requests time out after 30 seconds, and responses are limited to 20 MB.
- **Credentials:** connection strings and header values are kept in the secrets directory, not the database. They are never returned: a source reports `remote` with the driver, query, URL, `items`, `header_names`, and `refresh_minutes`. `PATCH /knowledge/sources/{id}` with `remote` changes the settings. A blank `connection_string` or header value keeps the stored one, and the credentials are deleted with the source.
- **Refreshing:** when a search uses a source whose data is older than `refresh_minutes`, Mimir fetches it again in the background, and that search uses the data already indexed. `POST /knowledge/sources/{id}/refresh` fetches at once. If a fetch fails, the source is `failed` with the reason (credentials removed), and search keeps using the last data that was fetched.

Knowledge sources read scanned PDFs with text recognition (OCR). Pages with a text layer are read as before, and only the pages without one are recognized, so a scanned appendix in a digital document is read too. Recognition runs RapidOCR in a private Python environment that is installed under `runtimes/python/envs/ocr` the first time a scanned page needs it. The install is about 110 MB to download and 290 MB on disk, and the recognition models come with it, so nothing else is downloaded. A page takes about a second on an M5 Pro. Recognized text is remembered by file content, so a folder source does not recognize its scanned PDFs again when another file changes. On the Train page, a scanned PDF has no examples to train on, so it is recommended as knowledge.

Knowledge search matches words (BM25). When an embedding model is installed (one with `support_role` `embedding`, such as `nomic-embed-text-v1.5-q8` in the catalog), it also matches meaning, so "What warranty do you offer?" finds a passage about a five-year guarantee. The embedding model runs in its own `llama-server` started with `--embedding`, loaded when first needed and unloaded by the idle sweeper like any model; it appears in `GET /models/running` with `mode` `embedding`. Passages are embedded in the background after a source is added or reindexed, and the vectors are kept in the daemon database. A reindex keeps the vectors of passages whose text did not change, and installing a different embedding model embeds every passage again. A source with more than 20,000 passages is searched by words only. Search never waits for embedding: it uses the vectors that exist.

Word and meaning matches are combined with reciprocal rank fusion. A passage found only by meaning must be similar enough to the question, and close to the best match. When words found something, it must also be at least as similar to the question as the best word match, so a question that names one product does not bring in every similar row. When a reranker model is installed (`support_role` `reranker`), it reorders the top 16 passages. Each hit from `POST /knowledge/search` has `match`: `keyword`, `semantic`, or `both`. `score` orders hits within one search only: BM25 for word-only search, the fused rank when meaning is used, and the reranker's score for passages it ordered. A knowledge source reports `embedded_count` and `embedding_model`. With no embedding model installed, if it cannot start, or while training is using the computer, search uses words only.

## Training and export

See [Train Your Own AI](features/train-your-own-ai.md) for the workflow.

A trained revision can be exported as one GGUF file that llama.cpp, LM Studio, Ollama, and other GGUF tools load without the adapter. `POST /training/ais/{id}/revisions/{n}/export` starts the merge and returns `202` with the status, or `200` when the file already exists. The merge runs `llama-export-lora` from the installed llama.cpp in the background, and it takes seconds for a small model and a minute or two for a large one. Tensors the adapter changed are written as F16, and the rest keep the base model's quantization, so the file is a little larger than the base model. The status has `state` (`none`, `exporting`, `ready`, or `failed`), `filename` (such as `tire-bot-r2.gguf`), `size_bytes` (the estimate while exporting), `error`, and the AI's `instructions`, which are not part of the file and are needed as the system prompt elsewhere. Exporting is refused when there is not enough free disk space, when the revision's adapter or base model is not on this computer, and in builds that ship only `llama-server`. `training.export.completed` and `training.export.failed` carry `ai_id`, `name`, and `revision`, and post a notification. Exports are deleted with their AI.

## Automations and notifications

An automation's `notification.mode` is `condition`, `change`, `always`, `failure` (only failed runs), or `none`. An automation runs on the same stack as chat: `model_id` `auto` picks a model for each run, and memories and connected knowledge are used the same way. Tools follow the unattended policy, because nobody is there to approve them. Tools listed in the automation's `tools` were approved when it was saved, and they run even if they change things. With no `tools`, only read-only tools the profile allows without asking can run. A tool the profile denies never runs. When a run reaches a tool that was not approved, the tool is skipped and the run continues. The run's `automation.completed` event lists the tool in `skipped`, and an `approval` notification says which tools to approve.

Notifications come from Gjallarhorn. Each one is stored first and then delivered to its channels. A notification has `category` (`automation`, `approval`, `model`, `training`, `health`, or `system`), `severity` (`info`, `success`, `warning`, or `error`), `title`, `body`, and a `link` back to its source in the app, such as `/automations?id=…`. Each channel's attempt is recorded in `deliveries`. A delivery is `delivered`, `failed`, or `suppressed`; for example, desktop notices are suppressed when `notify_task_finish` is off. A repeat with the same source within 10 minutes is folded into the first notification and marked unread again. The `notification.created` event carries `id`, `category`, `severity`, `title`, `body`, and `link`. Finished and failed automations post to the desktop too. Model downloads, training deploys, and pairings stay in the app.

## Sharing the computer

Chat, automations, knowledge indexing, benchmarks, and training share this computer in that order of priority. A chat or API request, including a request from a paired computer, never waits. An automation run waits while a chat is running, and for 20 seconds after one, so it does not load a model between someone's messages. A benchmark also waits for automations, and it waits again before each model, because loading a model unloads the others. Background embedding of knowledge passages waits for chat and automations before each small batch, and does not start while training runs. Training waits for all of them before it unloads models to free memory. While work waits, `work.waiting` carries `class`, `label`, and `reason` ("Waiting for your chat to finish"), and the benchmark's progress or the training job's detail shows the reason. A chat that arrives during training is still answered. Its steps say `Training "…" is using this computer (about N minutes left)`. If its model runs out of memory, the error says so, with the estimate and what to do. Out-of-memory failures are not retried. An automation that runs out of memory twice in a row is paused, and its notification explains why.

## Connected services

Connected services add tools. Today they are GitHub (`github.search`, `github.issue`, `github.comment`) and Home Assistant (`homeassistant.states`, `homeassistant.call`).
- **Connecting:** `PUT /connectors/{id}` checks the values with the service before storing anything, and returns the account it connected as. A blank secret field keeps the stored value.
- **Storage:** credentials are stored in the `secrets` directory of the data directory, not in the database. They are added to a request only when a tool runs, so they are never part of model context, events, or tool arguments.
- **Responses:** the API never returns a secret value; `values` shows a stored token as its last four characters only. Results and errors are scrubbed of any credential value before the model sees them.
- **Policies:** connected tools join the tool catalog with `source` `connector:<id>`. Reading is allowed and changes ask first, unless a profile sets its own policy for the tool.
- **Selection:** they are offered when a message is about the service. That means it names the service, or uses words like issues or pull requests for GitHub and lights or sensors for Home Assistant. It also counts when the message uses words the service taught: Home Assistant's device names, learned when it connects and whenever all devices are read. Tools that change something are offered only when the message asks for a change ("turn on", "comment").
- **Fetched first:** a read tool marked `prefetch` (Home Assistant's device list) is called before the model answers a message about its service, when the profile allows it without asking. Its data, written as plain lines, replaces the web look-up for that turn.
- **Untrusted data:** what they return is treated as untrusted data (§58), and links in results become sources.

## MCP

MCP tool sources add tools the same way, with `source` `mcp:<source>`; secrets are kept in `secrets/mcp-<source>.json`. `/mcp` (outside `/api/v1`) is Yggdrasil's own MCP server for other apps, checked like `/v1`, and `/mcp/oauth/callback` is where a tool source's sign-in returns. See [MCP](mcp.md).

## Personalization

Personalization shapes how answers look in every chat, automation, and API request. It has four choices: `length` (`brief`, `balanced`, `detailed`), `tone` (`friendly`, `neutral`, `direct`), `format` (`prose`, `lists`), and `units` (`metric`, `imperial`). It also has two short notes, `about_me` and `instructions`, of up to 1,500 characters each. It is stored as a setting and added to the model's instructions as style guidance, after a specialized AI's own instructions. It is kept apart from permissions: what a tool may do comes only from profiles and Settings. A personalization note or a memory that tries to grant a permission is refused with 400, for example "you can always push without asking" or "don't ask before running commands". The refusal says where permissions are set. A memory that states a preference, such as "I use the terminal a lot", is saved and changes no policy.

## Privacy and run records

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

## Profiles and orchestration

A profile's `orchestration` object holds its advanced controls. Every field is optional; empty keeps the default, which follows the chat's effort.

| Field | Values | Effect |
| --- | --- | --- |
| `strategy` | `single`, `planned`, `team` | How a request is worked through. Empty is Auto. `single`: one model, no plan. `planned`: a request with several parts is always worked through in parts. `team`: a planner splits the request, workers do the parts, and a reviewer checks the answer; quick questions are still answered directly |
| `effort` | `fast`, `balanced`, `thorough` | The profile's effort when a chat leaves effort on Auto |
| `planning` | `on`, `off`, `always` | `on` works through requests with several parts in parts, whatever the effort; `always` also asks the planner model to split a request with no obvious parts |
| `max_workers` | 2–8 | Most parts in a plan |
| `parallel` | `on`, `off` | `off` works through parts one at a time |
| `verification` | `off`, `check`, `correct`, `thorough` | `off` skips the figure check; `check` reports only; `correct` and `thorough` allow one or two correction passes |
| `max_tool_calls` | 1–50 | Most tool calls in one turn |
| `memory` | `off` | Keeps persistent memory out of the profile's chats |
| `context_share` | 0.1–0.9 | Most of the model's window earlier messages may use |
| `fallback` | `off` | Shows a failure instead of answering on another model |
| `fallback_models` | up to 8 model ids | Tried in order when the answering model fails, before Yggdrasil picks another installed model |
| `retries` | 1–3 | Tries after a model fails before showing anything (default 1). Each try first runs the same model on another online computer that has it, then another model |
| `timeout_seconds` | 10–3600 | Stops a turn that runs longer; the answer so far is kept and says it reached the time limit |

Invalid values are refused with 400. In advanced mode, the profile editor has an Orchestration section, alongside model roles, tools, knowledge, and placement.

A profile's `roles` assign models, and optionally computers, to these roles. A role without a model uses the model the chat chose, or Auto's pick.

| Role | Used for |
| --- | --- |
| `assistant` | Writing the answer (the primary model) |
| `fast` | Quick questions, when the chat is on Auto |
| `coding` | Coding requests, when the chat is on Auto |
| `planner` | Splitting a request into parts |
| `worker` | Each part of a plan. With the Team strategy or a worker model, each part gets its own slot (`worker:1`, `worker:2`, …) that Norn can place on another computer, and parts on different computers are written at the same time |
| `reviewer` | Checking the answer |

A profile's `node_policy` holds its placement rules:

| Field | Values | Effect |
| --- | --- | --- |
| `mode` | `automatic`, `prefer_local`, `manual` | Where roles run by default; `manual` relies on role pins |
| `preferred_nodes` | computer ids | Favored when they have the model |
| `denied_nodes` | computer ids | Never used |
| `remote` | `off` | Every turn stays on this computer |

A computer cannot be both preferred and denied. A role pinned to a computer still follows its pin; a worker slot such as `worker:2` follows the `worker` role's pin.

`orchestrator_id` is `simple` for every profile. A profile sent with the older `team` orchestrator, or with the role names `coordinator` and `researcher`, is stored with the Team strategy and the roles `planner` and `assistant`. Profiles saved by older versions are migrated the same way at startup.

## Structured results

Structured results are checked the same way elsewhere:
- **Tool arguments:** they are checked against each tool's schema before the tool runs. Safe repairs are made, such as `"7"` for a whole number, or JSON data where text is expected. A call with an argument of the wrong type is refused with `kind` `invalid`, naming the argument, so the model can call again.
- **Automations:** a condition automation's result must end with the JSON its condition reads: `{"price": number}` for a threshold, or `{"significant": boolean}`. That JSON is read with the same repairs. When it is missing or wrong, the model is asked once to supply it from its own answer. Notices show the prose, never the JSON.

## Capability inventory

The capability inventory lists what exists right now:
- `models` (with the computers they are on and whether they are running), `nodes` (online, memory, whether they can train), and `tools` from every source (built in, connected services, MCP), enabled or not;
- `connectors`, `providers` (runtimes and MCP tool sources, with health), and stored `artifacts`;
- `abilities`, each with `available`, the tools or models it comes `via`, and a `note` saying how it works or what would make it possible. Abilities are worked out from tools and models by what they do, so a new MCP tool that sends email counts as email without code.

A chat question about what Yggdrasil can do, such as "Can you generate an image?", "Do you have access to my email?", or "Which computer can run Qwen 2.5 14B?", gets the matching facts as trusted instructions. The model answers from them instead of guessing, and the answer's steps say so. In the app, Diagnostics shows the same list.

Three behaviors come from the quality test set (`tests/quality`):
- **Plain questions:** a plain question is answered without tools; the message must ask for a search, a file, a command, and so on.
- **Capability questions:** a short question about what Yggdrasil can do is answered straight from the capability inventory, without a model.
- **False claims:** an answer that says it changed something, when no tool that changes things ran, gets the notice "Nothing was changed: no tool ran to do this, whatever the answer says."

## Caches

Every cache declares its policy: `key`, `ttl`, `invalidation`, `scope`, and `privacy` (`public` or `personal`). Secret data, such as credentials, is never cached; a cache that would hold it is refused when it is created.

| Cache | Key | Kept | Cleared when | Privacy |
| --- | --- | --- | --- | --- |
| `web_search` | the query, in lower case | 15 min | age; run records deleted | personal |
| `web_pages` | the page address | 30 min | age; run records deleted | personal |
| `capabilities` | one inventory snapshot | 30 s | a model downloads, loads, or unloads; a computer pairs or goes on- or offline; a tool is turned on or off | public |
| `model_search` | Hugging Face search text and limit | 5 min | age | public |
| `knowledge_index` | source and passage (on disk) | until invalidated | files change; reindex; source removed | personal |

A repeat web search or page read is answered from the cache. The tool does not run, nothing leaves this computer (so no egress record is written), and the run trace counts it in `cache_hits`. In-memory caches are bounded, dropping the least recently used entry first, and can be cleared one at a time. "Delete run records now" also clears every personal in-memory cache.

## Client contract

The desktop app, mobile apps, and other clients read a versioned contract: events, run traces, answers with their citations, steps, and files, artifacts, notifications, and egress records. The version is `major.minor`, now `1.0`.
- **Where it appears:** every event has `contract`, and so do answer metadata and run traces. Metadata saved before the contract existed has no `contract` and reads as 1.0. Every response carries the `Yggdrasil-Contract` header, and `GET /api/v1/version` has `contract` (`version`, `major`).
- **Minor versions** add fields or event types. Clients ignore what they do not know, so an older client keeps working.
- **Major versions** remove something or change its meaning. A client may send `Yggdrasil-Client-Contract: 1.0`. A client built for another major version gets 426 with code `CONTRACT_MISMATCH`, and the message says whether to update the app or Yggdrasil. A client that sends no header is served as before.
- **Compatibility test:** `tests/contract` records the contract's fields. It fails when one is removed or renamed within a major version, and when one is added without a minor version bump (`UPDATE_CONTRACT=1 go test ./tests/contract` records the new fields).

## Events

`GET /api/v1/events` is a server-sent event stream. Each event has `type`, `payload`, a timestamp, and `contract`. Clients should ignore types they do not know.

| Type | Sent when |
| --- | --- |
| `chat.token` | A piece of an answer is written |
| `chat.complete`, `chat.error`, `chat.stopped` | A turn finishes, fails, or is stopped. `chat.complete` carries the answer's `meta` and `context`. |
| `chat.model_routed` | A model is chosen for a turn, with `reason`, and `fallback: true` after a failure |
| `chat.effort`, `chat.lookup`, `chat.verifying`, `chat.making_file`, `chat.summarized` | Effort is set, the web is looked up first, figures are being checked, a requested file is being made, or older messages were summarized |
| `plan.created`, `plan.step` | A request with several parts is planned, and each part runs |
| `verify.action`, `verify.done` | The answer check corrects something, and its result |
| `tool.requested`, `tool.started`, `tool.completed`, `tool.failed`, `tool.parsed` | A tool call waits for approval, runs, finishes, fails (with `kind`), or is read from text |
| `knowledge.retrieved`, `knowledge.failed` | Knowledge passages are added to a turn, or the search fails |
| `memory.saved`, `memory.deleted` | A memory is saved or forgotten from a chat |
| `orchestration.role` | A planner, worker, or reviewer starts, with the computer it runs on |
| `plan.planner`, `answer.reviewed` | The planner split a request (`parts`); the reviewer checked an answer (`changed`) |
| `task.created`, `task.started`, `task.completed`, `task.failed` | An orchestration task changes state |
| `scheduler.placement` | Norn places work on a computer |
| `work.waiting` | Work waits for higher-priority work, with `class`, `label`, and `reason` |
| `automation.started`, `automation.completed`, `automation.failed` | An automation runs. `automation.completed` lists skipped tools in `skipped`. |
| `notification.created` | A notification is stored |
| `model.download.started`, `.progress`, `.completed`, `.failed` | A model downloads |
| `model.load.started`, `model.load.completed`, `model.unloaded` | A model loads, or the idle sweeper unloads it |
| `model.health.degraded`, `model.health.failed` | A loaded model stops answering health checks |
| `model.cleanup.started`, `.completed`, `.failed` | Model files are removed |
| `node.discovered`, `node.online`, `node.offline`, `node.paired` | Another computer is found, comes online, goes offline, or pairs |
| `training.job`, `training.eval`, `training.deployed` | A training job changes, an evaluation runs, or an AI is deployed |
| `training.export.completed`, `training.export.failed` | A GGUF export finishes or fails |

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
