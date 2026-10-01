# Privacy

Yggdrasil Core does not send usage telemetry by default.

A search of this repository found no analytics or crash-reporting client (no Sentry, PostHog, Mixpanel, Amplitude, Google Analytics, or a metrics upload). The `internal/tools/plausible.go` file checks whether a tool call looks like a real path or command. It is not the Plausible analytics product. Metrics stored by the daemon stay in the local SQLite database.

If that changes, this document should name what is collected, where it is sent, the default, and how to turn it off.

## What is stored on this computer

The data directory (see [Configuration](../README.md#configuration)) holds:

| Path | Contents |
| --- | --- |
| `config.json` | Bind addresses, node name, node id, discovery, static peers. Written with mode `0600`. |
| `yggdrasil.db` | Models, profiles, conversations, messages, summaries of long conversations, memories you asked Yggdrasil to keep, tasks, settings, API key hashes, paired-node records. Chat history and task history are saved unless those settings are turned off. Both default to on. Memories are listed on the Memory page, where you can edit, pause, or delete them, or turn memory off. |
| `models/` | GGUF files you install. |
| `artifacts/` | Files attached to chats and files the assistant produced, one folder per chat. Deleting a chat deletes its files. The index is in `yggdrasil.db`. |
| `knowledge/` | Copies of content (text, spreadsheets, PDFs) you pasted or uploaded as connected knowledge. Linked files and folders stay where they are. The search index is in `yggdrasil.db`. |
| `training/` | Trained adapters (`adapters/`), temporary job files (`jobs/`, removed when a job ends), and downloaded training weights (`hf-cache/`). Examples are in `yggdrasil.db`. |
| `runtimes/` | Runtime binaries, including `llama-server` after you install llama.cpp, and `python/` with the trainer environment after the first training run. |
| `logs/daemon.log` | JSON logs, also written to standard output. |
| `secrets/` | Node identity material. Directory mode `0700`, files mode `0600`. API keys are stored as hashes in `yggdrasil.db`, not as plaintext files. |

The diagnostic bundle is written to omit secrets, private keys, and API key material. Do not assume a log file has been redacted. Remove tokens and personal text before you paste a log into an issue.

## What can leave the machine

Nothing leaves because the daemon started.

Traffic is sent only when a feature that talks to the network is used:

| Action | Where data goes |
| --- | --- |
| Install or update llama.cpp | `api.github.com` and the GitHub release download for `ggml-org/llama.cpp` |
| Search or install a Hugging Face model | Hugging Face Hub |
| First training run | The `astral-sh/uv` GitHub release, Python builds that uv fetches, and pinned packages from PyPI |
| Training a base model the first time | Hugging Face Hub, for the base model's training weights. Your examples are not uploaded. |
| Training on a paired computer | That computer receives the training examples (with the AI's instructions) over Bifrost and returns the adapter. It deletes its copy when the job ends. |
| Install a model from a URL | The host in that URL |
| Internet tools (`internet.search`, `internet.open`) | DuckDuckGo's public HTML search, then the page URL the tool opens. These tools run only when the profile allows them. |
| External OpenAI runtime | The base URL configured for `external-openai`, with the API key configured for that runtime if one is set |
| Bifrost discovery and pairing | Other computers on the local network, or the static peers you listed |
| LAN API | Any client that can reach port 7331 after you enable LAN API |

Remote generation through `external-openai` sends the prompt to that server. A paired computer that Norn selects receives the role prompt over Bifrost.

## LAN behavior

- Port 7331 stays on `127.0.0.1` until LAN API is enabled.
- Discovery defaults to on. Bifrost (port 7332) is then bound on all interfaces so peers can connect.
- mDNS advertises the node on the local link.
- Pairing routes on port 7332 answer before a peer is trusted. Other Bifrost routes require a token from a paired node.

## API exposure

On the default loopback bind, the control API and the OpenAI-compatible API do not require a key. That is safe only while the port is not reachable from other machines.

Any other bind requires `Authorization: Bearer YOUR_API_KEY` on `/api/v1/*` and `/v1/*`. The daemon will not start that listener unless a key exists. Enabling local network access stores `0.0.0.0` and refuses the change when no key exists. The new socket applies the next time the daemon starts. The key check follows the configured host, so it applies as soon as the setting is saved.

`YGGDRASIL_API_HOST=0.0.0.0` is how the Docker image listens. Set `YGGDRASIL_API_KEY` or the process exits before it accepts connections. See [API](api.md).

An API key on plain HTTP does not encrypt traffic. It limits who can call the API. TLS or mTLS for remote access is not implemented.

## Logging

Logs are JSON on stdout and in `logs/daemon.log`. They include startup fields such as the node id, data directory, and listen addresses. They are not shipped anywhere by this code.

## Secrets

API keys use the prefix `ygg_`. The database stores a bcrypt hash and a prefix. The secret is returned once when the key is created or rotated and is not written to disk. Node certificates and pairing material live under `secrets/`. Do not commit a data directory, and do not attach `secrets/` to a bug report.
