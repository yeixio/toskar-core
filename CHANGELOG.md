# Changelog

All notable changes to Yggdrasil Core are documented in this file.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/). This project uses Git tags. Version numbers below are tags and notes that already exist in the repository. This file does not restate every historical fix.

## [Unreleased]

### Added

- Big requests are worked through in parts. "Compare Ollama, llama.cpp and MLX" looks each one up on the web side by side; "find three NAS drives, then compare price per TB, then make a spreadsheet" runs step by step, each step building on the last. A checklist shows the parts while they run, and one answer (or file) comes from all of them.
- Answers are checked before you see them. Calculations are recomputed, and figures in answers that use your files, knowledge, or web results must appear in the lines about the same thing. A wrong figure is sent back to the model to fix once. Anything still unconfirmed is called out under the answer ("could not confirm 20 in the sources"). An answer that only describes tools, instead of answering, is asked for again without tools.
- Small-model notes. On the Models page, models under 4B parameters say they can mix up facts and numbers from your files and knowledge, and suggest a larger model that fits the computer. In chat, an answer from a small model that used your files or knowledge carries the same note. Auto prefers a larger model for questions about your data.
- Files in and out of chat. Attach documents, spreadsheets, PDFs, and code files with the paperclip, by dropping them on the message box, or by pasting. The answer cites the file, and later questions in the chat can still use it, including files the assistant made ("add a column to that spreadsheet"). Ask for a file ("make a spreadsheet of these prices") and the answer comes with a download. Spreadsheets are real `.xlsx` workbooks. Files stay on this computer, under `artifacts/` in the data directory, and are deleted with their chat. Routes are under `/api/v1/artifacts`.
- Each page shows its Norse name above the title (Huginn for Chat, Mimir for Knowledge, Bifrost for Computers, and so on), and each tab in the sidebar has its Elder Futhark rune. Tab names are unchanged. Hover a name or tab to see what it means.
- Auto model. New chats use Auto, which picks an installed model for each message. Coding questions go to a coding model, questions about current information to a model that can use tools, and quick questions to a fast model that is already loaded when there is one. The answer's "What I did" says which model answered and why. `auto` is also a model in `/v1/models`.
- Current questions are looked up first. When a question needs current information (weather, news, prices, scores, links) and web search is allowed, Yggdrasil searches the web and reads the best page before the model answers, so small models answer from the page instead of guessing or picking the wrong tool.
- Quiet recovery. If a model fails before it answers, another installed model answers instead, and the answer says so, with a note when the model that answered is noticeably smaller. Auto skips a model that failed in the last 10 minutes.
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

- A model whose `llama-server` exits while loading, for example a damaged file, now fails at once instead of after a two-minute wait.
- More current-information questions are recognized (news, scores, prices, exchange rates, "near me"), and cues match whole words only.
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
