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
| GET, POST | `/tasks` | Orchestration tasks |
| GET, POST | `/automations` | Scheduled prompts. Also `POST /automations/preview`, `GET/PATCH/DELETE /automations/{id}`, and `POST /automations/{id}/run|pause|resume` |
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

`POST /chat` takes `attachments`, a list of artifact ids. Chat reads documents, spreadsheets (`.csv`, `.tsv`, `.xlsx`), PDFs with a text layer, JSON, HTML, and code files. Images are not read yet. An attached file reaches the model as data in the user turn, the whole file when it fits and otherwise the parts that best match the question, and files attached earlier in the chat add the passages that match later questions. The user message's `meta.files` lists its attachments.

The `files.create` tool, allowed by default in the built-in profiles, saves a file the user can download: a document, JSON, a spreadsheet (`.xlsx` is built from CSV text), or code. It writes only to Yggdrasil's file store. When a message asks for a file and the profile allows `files.create` without asking, Yggdrasil has the model write only the contents and saves the file itself, with a `chat.making_file` event. The answer's `meta.files` lists produced files. File content is served as an attachment with a sandboxing `Content-Security-Policy`, so HTML never runs on the API's origin.

A chat whose `model_id` is `auto` gets an installed model chosen for each message. The message is classified as a quick question, current information, coding, a detailed question, or a task on this computer. Auto then picks the largest suitable model that fits this computer's memory, keeps a quick question on a model that is already loaded, requires tool calling for current information and tasks, and skips a model that failed in the last 10 minutes. The `chat.model_routed` event carries `model_id`, `model_name`, and a plain-language `reason`, and the reason is the first of the answer's `steps`.

When a question needs current information and the profile allows `internet.search` without asking, Yggdrasil searches the web and reads the best page before the model answers. The `chat.lookup` event carries the `query`. The results reach the model as data, and the model answers without web tools for that turn.

If the model fails before it shows or changes anything, the turn runs once more on another installed model. `chat.model_routed` then has `fallback: true`. The answer's steps say what happened, and `meta.notice` warns when the model that answered is noticeably smaller. A model whose `llama-server` exits while loading fails at once instead of after the 120-second readiness timeout.

A profile's `knowledge_sources` lists Mimir source ids. Chat searches them on every turn and adds the matching passages before the system prompt.

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

### Streaming

`"stream": true` responds with `Content-Type: text/event-stream`. Each event is `data: {json}` and the stream ends with `data: [DONE]`.

### Known differences

- Only the last message with `"role": "user"` is sent into the chat path. Earlier turns, system prompts, and assistant messages in the same request are not forwarded.
- `temperature` and `max_tokens` are accepted and ignored.
- Tool definitions in the OpenAI request are not passed through. Tool use is controlled by the Yggdrasil profile.
- The non-streaming `id` is the fixed string `chatcmpl-ygg`.
- A model must already be installed and startable. The HTTP call does not download one for you.

Examples that match this behavior are in [examples/](../examples/).
