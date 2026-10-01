# Architecture

Yggdrasil Core is one daemon process, `yggdrasil-daemon`, plus the web UI it serves. Desktop and mobile applications are separate clients. They are not built from this repository.

```text
client (web UI, curl, another program, desktop, mobile)
        |
        |  HTTP  /api/v1 and /v1     default 127.0.0.1:7331
        v
  yggdrasil-daemon
        |
        +-- profiles, tasks, tools
        +-- Mimir knowledge, training (specialized AIs)
        +-- model catalog and downloads
        +-- Norn placement
        |
        +-- runtime adapters
        |     +-- llamacpp (local llama-server)
        |     +-- external-openai (configured remote server)
        |
        +-- Bifrost  :7332
              discovery, pairing, paired-node calls
```

Local state is a SQLite database, model files, runtime binaries, logs, and a secrets directory under the OS data path. See [Privacy](privacy.md).

## Names used in the code

Some Norse names appear in comments and logs. Others are not packages in this repository. The table uses the names as they exist today.

| Name | Role people expect | In this repository | Status |
| --- | --- | --- | --- |
| Yggdrasil Core | Daemon, API, local web UI | `cmd/daemon`, `internal/app`, `internal/api`, `web/` | Implemented |
| Bifrost | Discovery, pairing, node protocol | `internal/discovery`, `internal/nodes`, `internal/auth` | Implemented |
| Norn | Scheduling and workload placement | `internal/scheduler` | Implemented |
| Heimdall | Health and diagnostics | `internal/events` calls its bus a Heimdall event stream. Diagnostics and model health live in `internal/diagnostics` and `internal/models/health`. | Partial |
| Huginn | Agent execution | No package uses this name. Chat and tasks run through the `simple` and `team` orchestrators. | Planned as a named subsystem. Orchestrators are implemented. |
| Muninn | Persistent memory and context | `internal/muninn`: memories ("Remember that…") with FTS5 recall, per-chat and global Memory Off, and summaries of long conversations. Conversations and messages are rows in SQLite. | Implemented (keyword recall; no embeddings yet) |
| Mimir | Knowledge and retrieval | `internal/mimir`: file, folder, and pasted sources, SQLite FTS5 search, retrieval into chat. Sources reindex when their files change. | Implemented (keyword search; no embeddings yet) |
| Gungnir | Tool and task execution | No package uses this name. Tools are implemented in `internal/tools` (internet, filesystem, terminal, git). Tasks are implemented in `internal/tasks`. | Planned as a named subsystem. Tools and tasks are implemented. |

## Request path

1. A client calls `/api/v1/chat`, `/api/v1/tasks`, or `/v1/chat/completions`.
2. The app resolves a profile. Built-in profiles include General Assistant (`simple`), Programming (`team`), and Research (`simple`).
3. The task manager asks Norn where a role should run. Norn scores paired nodes that are online and have the model.
4. The chosen machine starts the model on a runtime adapter. The default local runtime is llama.cpp.
5. Tokens stream back as events. The web UI subscribes to `GET /api/v1/events`.

The OpenAI handler sends the last user message into that same chat path. It does not forward the rest of the message array. See [API](api.md).

## Bifrost

Bifrost is the internal HTTP server on port 7332.

- mDNS service type `_localai._tcp` on domain `local.`
- optional static peers (`YGGDRASIL_STATIC_PEERS` or `config.json`) when mDNS is not available, including Docker
- pairing offer and approval before a peer is trusted
- certificate-backed bearer tokens on protected routes such as remote chat and model control

Discovery is on by default. When it is on and the internal bind address is still loopback, startup rebinds Bifrost to `0.0.0.0` so peers on the LAN can connect. Pairing routes on that port are reachable without a token until a peer is trusted. Protected routes reject unsigned calls.

## Norn

Norn is a deterministic placer. Given the nodes the daemon knows about, which models they have, and a role, it picks a node and records a `scheduler.placement` event. It does not split one model across computers. Cross-machine work that exists today is placement of whole roles, as in the Team pipeline. User schedules are a separate daemon loop in `internal/automations`. Norn places the model for a due automation the same way it places a chat role.

## Orchestrators

| Id | Behavior | Status |
| --- | --- | --- |
| `simple` | One model, optional tool loop | Implemented |
| `team` | Coordinator, then worker, then reviewer | Implemented |

Team can place those roles on different paired computers. A manual script for that is [two-machine-team-demo.md](two-machine-team-demo.md).

## Heimdall, as far as it exists

The event bus publishes structured events for tasks, models, tools, nodes, placement, and chat tokens. `GET /api/v1/events` is a server-sent stream of that bus. `GET /api/v1/diagnostics` builds a zip that omits secrets. A model health monitor can stop a model that stops responding. There is no separate telemetry pipeline.

## Specialized AIs

`internal/training` builds a specialized AI from a base model, a LoRA adapter trained on the user's examples, system instructions, and Mimir knowledge sources. Trainers implement `training.Trainer`. The MLX trainer runs on Apple Silicon in a Python environment the daemon manages under `runtimes/python` (`internal/pyenv`), and exports the adapter as a GGUF LoRA. Each computer runs one training job at a time. Norn can place a job on a paired computer: the examples go over Bifrost (`/internal/v1/training/`), and the adapter comes back to the computer that owns the AI, which evaluates and serves it.

llama-server loads every deployed adapter for a base model at scale 0. Each request names the adapter to apply, or none for the base model, so one process serves the base model and each specialized AI built on it. A `sai:<slug>` model id in chat or `/v1/chat/completions` adds the AI's instructions and knowledge and applies its adapter. See [features/train-your-own-ai.md](features/train-your-own-ai.md).

## Runtimes

Runtime adapters implement `pkg/pluginapi.Runtime`: detect, install, start, stop, and health. The process that actually generates tokens is outside the daemon (`llama-server`, or an HTTP server you already run). See [runtimes.md](runtimes.md).

## What is not in this process

- Yggdrasil Desktop and Yggdrasil Mobile
- semantic (embedding) retrieval; Mimir searches by keyword
- training on NVIDIA GPUs
- splitting a single model across machines
