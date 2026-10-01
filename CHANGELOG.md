# Changelog

All notable changes to Yggdrasil Core are documented in this file.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/). This project uses Git tags. Version numbers below are tags and notes that already exist in the repository. This file does not restate every historical fix.

## [Unreleased]

### Added

- Memory. Say "Remember that…" in any chat, and Yggdrasil keeps it across chats, restarts, and model changes; "Forget…" and "What do you remember?" work too. Answers that used a memory list it as a source. The Memory page lists memories by category, where you can add, edit, pause, or delete them, and turn memory off. Each chat has a Memory on/off switch. Passwords, keys, and card numbers are not saved. Routes are under `/api/v1/memory`.
- Long conversations keep working. When a chat's history passes half of the model's window, older messages are summarized after the reply and the summary takes their place; the messages stay saved. The context meter shows how many messages the summary covers.
- Chat answers show their sources (web pages, knowledge passages, and files) and a "What I did" summary of searches, pages read, and knowledge used. Errors are explained in plain language with a next step and a retry; technical detail is under Details. Knowledge lookups show progress while the answer is prepared.
- Mimir connected knowledge. Connect a file, a folder, pasted content, an Excel workbook (each sheet is a table), or a PDF with a text layer (each page is cited); chat adds the passages that match each question. File and folder sources reindex when the files change. A profile lists `knowledge_sources`. Routes are under `/api/v1/knowledge`.
- Train your own AI. The Train page builds a specialized AI from a base model, examples, instructions, and connected knowledge. Yggdrasil recommends Training, Knowledge, or Both for each piece of material, flags weak examples, estimates training fit separately from inference fit, trains a LoRA or QLoRA adapter with MLX on Apple Silicon, and compares base and specialized answers before deployment. A deployed AI is the model `sai:<name>` in chat and `/v1/chat/completions`. The first training run installs a private Python environment under `runtimes/python` and downloads the base model's training weights from Hugging Face. Norn can train on a paired computer that has more memory, and the Review step lets you pick the computer; the adapter returns to the computer that owns the AI. The Train page and the Profiles editor attach existing knowledge sources, files, and folders. "Try an example" sets up a sample tire shop assistant with notes on each step, and the Material step shows sample files in each format.
- `yggctl completion <bash|zsh|fish>` prints a completion script for `yggctl` commands, `automations` subcommands, and their flags. Homebrew, the deb and rpm packages, and the macOS and Linux archives install or include the scripts.
- Targeted for 1.3.0: scheduled automations. The daemon runs a saved prompt on a one-time, daily, weekly, or interval schedule, keeps a history of each occurrence, retries timeouts and connection failures, and can post an operating-system notice when the result matches the notification rule. Read-only tools can run while the window is closed. Automations are available in the desktop UI and through `yggctl automations`. `GET /api/v1/automations` is new. This is a minor release because the API, CLI, and database migration are backward-compatible.

### Changed

- Connected knowledge and retrieved content reach the model as labelled data in the user turn, not in the system prompt, and the model is told not to follow instructions inside it. After a turn reads untrusted content, a tool that changes something (write, terminal, Git commit or push) asks first even when the profile allows it. Knowledge search drops passages that score far below the best match, and table columns such as `in_stock` read as "in stock".

### Fixed

- Chat starts faster when a paired computer is offline. Peer health is checked in the background every 10 seconds and reused for 20; a check that has to run during a turn waits at most 1.5 seconds. Before, each turn waited the full timeout for an offline peer.
- A fast reply, such as a memory confirmation, no longer shows twice.
- Targeted for 1.3.1: a saved schedule turns on “Keep running in background,” so closing the window does not stop the daemon that runs it.

## [1.2.1] - 2026-09-28

Patch release. The API is unchanged. Binaries and the apt repository are not signed.

### Added

- Branching and release strategy guide in `docs/development/branching-and-release-strategy.md`.

### Changed

- Flat logo in the web UI, favicons, home-screen icons, and Linux package icons.
- `make start` builds the UI and daemon and runs them. `make help` lists targets.

## [1.2.0] - 2026-09-28

First stable release. It follows `v1.2.0-beta.3`. Binaries and the apt repository are not signed.

### Added

- Unsigned Windows amd64 headless archive on the GitHub Release.
- Homebrew formula written and merged from the release workflow.
- Coverage badge, golangci-lint, and ESLint in CI.
- Repository guides for contributors, security reports, privacy, architecture, compatibility, and issue forms.
- Feature specifications and the distributed-inference research brief. Those documents are plans, not shipped behavior.

### Changed

- Web UI uses Tailwind 4.
- The README leads with the local-AI goal, the demo, and packaged install paths.

### Fixed

- llama.cpp health errors include the install location when the runtime binary is missing.

## Earlier releases

Notes checked into `docs/releases/` for versions that have them:

- [1.0.0-beta.1](docs/releases/1.0.0-beta.1.md)
- [0.1.0-beta.1](docs/releases/0.1.0-beta.1.md)
- [0.1.0-alpha.28](docs/releases/0.1.0-alpha.28.md)
- [0.1.0-alpha.27](docs/releases/0.1.0-alpha.27.md)
- [0.1.0-alpha.26](docs/releases/0.1.0-alpha.26.md)
- [0.1.0-alpha.25](docs/releases/0.1.0-alpha.25.md)
- [0.1.0-alpha.24](docs/releases/0.1.0-alpha.24.md)
- [0.1.0-alpha.23](docs/releases/0.1.0-alpha.23.md)
- [0.1.0-alpha.22](docs/releases/0.1.0-alpha.22.md)
- [0.1.0-alpha.21](docs/releases/0.1.0-alpha.21.md)

Tags also exist for the 1.1 and 1.2 beta lines. Their user-guide snapshots are under `docs/`. Published release bodies are on [GitHub Releases](https://github.com/yeixio/yggdrasil-core/releases). This changelog does not invent entries for those tags.
