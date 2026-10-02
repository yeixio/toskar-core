# Changelog

All notable changes to Yggdrasil Core are documented in this file.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/). This project uses Git tags. Version numbers below are tags and notes that already exist in the repository. This file does not restate every historical fix.

## [Unreleased]

### Added

- Groundwork for Yggdrasil in other languages (#56). The UI's text now comes from a shared translation catalog in `i18n/`, used by the web UI, the desktop app, and the iPhone app. Settings has an App language: System default, or a language from the catalog; the daemon keeps it (`ui_locale`), so every app shows the same language. A system in a language without a catalog yet sees English text with its own date and number formats. The navigation is the first part translated. In advanced mode, the `en-XA` pseudo-locale shows every translated string accented and padded, so text that is not translated or does not fit stands out. Tests check the catalog in CI: valid JSON, no duplicate keys, the same keys, placeholders, and plural forms as English, and no key used in the code missing from English.
- The desktop app's menus, tray, and closing screen come from the shared catalog (`i18n/locales/<language>/desktop.json`) and follow the App language; the web UI tells the desktop shell when it changes. With an older desktop app the menus stay in English.
- Word documents and PDFs. `files.create` writes `.docx` and `.pdf` from Markdown, with headings, styled text, lists, tables, code, and quotes; `document.create` and `pdf.create` reach it in that format. PDFs use the standard fonts, so characters outside Western European text show as `?`.
- Spreadsheets with several sheets and formulas: in the CSV for an `.xlsx`, a line `## Sheet: Name` starts another sheet and a cell starting with `=` is a formula. `spreadsheet.create` reaches `files.create` in that format.
- `spreadsheet.analyze` summarizes a spreadsheet in the chat column by column (type, counts, minimum, maximum, average, total, or the most common values) with the first rows, so answers can use a whole file.
- Tool descriptors. Every tool has a version, an input schema, its outputs, a permission level from 1 (low risk, on this computer) to 4 (runs commands or code), its requirements, time limit, provider, and health. `GET /api/v1/tools/{id}` returns one; the Tools page shows them.
- Tool audit. Every tool call is recorded with what became of it, how it was allowed, how long it took, and what it was about. `GET /api/v1/tools/runs` lists them, and the Tools page shows each tool's recent calls. Records expire with run records.

- Profile strategies. A profile can work Auto (the default), as a Single model, as Planner + workers, or as a Team. Team now runs on the same pipeline as every other chat: a planner splits the request, workers write notes for each part, the answering model writes the answer with tools, memory, and knowledge, and a reviewer checks it. Quick questions are still answered directly. Programming uses the Team strategy.
- Workers on other computers. With the Team strategy or a worker model, each part of a plan has its own worker, which Norn can place on a paired computer, and parts on different computers are written at the same time. The chat timeline and run details show each worker's model and computer.
- Model roles. A profile can assign primary, fast, coding, planner, worker, and reviewer models, each with an optional computer. With the chat on Auto, a profile's coding model answers coding requests and its fast model answers quick questions. A fallback order lists the models to try when the answering model fails.
- Planning: Always asks the planner model to split a request that has no obvious parts.
- Placement rules per profile. Each paired computer can be Preferred or Never use, and Only this computer keeps every turn here.
- Profiles & Orchestration. The Profiles page is renamed, and its editor is grouped into Profile, Models, Tools, Memory, Orchestration, and Execution. Each built-in profile has Reset to defaults (`POST /api/v1/profiles/{id}/reset`), and Duplicate now copies a profile's orchestration and knowledge sources.
- Run details label each role (Planner, Worker 1, Answer, Reviewer), list them in that order, and count the model calls.
- Retry on another computer. When a model fails before showing anything, the turn runs again with the same model on another online computer that has it, before trying another model. Retries (1–3) sets how many times.
- The network advertisement (`_localai._tcp`) now says where the API is, as `api_port` in its TXT record. The service's own port is the computer-to-computer port, so an app that finds Yggdrasil on the network, such as the iPhone app, had to assume the default API port.
- Email and webhook notifications. Settings → Email, push, and webhooks sends notifications to your own SMTP server or to a webhook, with the categories and lowest severity each one receives. Webhooks are signed (HMAC-SHA256, a secret per destination, shown once) and must use HTTPS outside your network; SMTP passwords are kept in the secrets directory. A Test button sends one right away. Deliveries are recorded in What left this computer.
- Push notifications through ntfy, on ntfy.sh or your own server, for Android, iPhone, and browsers, with no Yeix-hosted service. Severity sets the priority, tapping opens Yggdrasil when its address is set, and on ntfy.sh only a generic notice is sent unless you choose full content.
- Delivery retries. A failed email, push, or webhook delivery is retried after 1, 5, and 30 minutes, without rerunning the task; a failure that retrying cannot fix stops at once and says what to change.
- Quiet hours. Desktop notices, email, push, and webhooks wait overnight and go out when quiet hours end; errors still go out unless you choose Hold everything.
- Health notifications on changes only: a paired computer going offline and coming back, and a model that crashes twice within 30 minutes (at most once an hour, with a hint when it is out of memory). They go to the bell and to destinations that take the Health category.
- The notification center filters by category, counts repeats ("3 times"), and says when a notification is held or was not delivered everywhere. `GET /api/v1/notifications/{id}` shows each channel's delivery. The client contract is now 1.1.

### Changed

- The separate Team orchestrator is gone. Profiles saved with it, and API requests that name it, move to the Team strategy with their roles and models; `coordinator` becomes `planner`. A profile's role models are now used in chat: before, the model chosen in the chat replaced them all, except in Team profiles.

### Fixed

- Yggdrasil Desktop: answer sources and other links that leave the app now open in the default browser. Signing in to an MCP tool source now works too: it opens in the browser and returns to the daemon's address, where before it was sent back to the app's own `wails://` address, which no browser can reach. Saving a chat file, an exported GGUF model, or a training sample now asks where to save and writes the file; a large model streams straight to disk. The desktop app's web view can't open windows or download files, so these go through the desktop shell (Yggdrasil Desktop 1.4 or later). In a browser, nothing changes.
- Yggdrasil Desktop on Windows: live progress, tool approval prompts, notifications, and chat replies as they are written now appear. Wails hands the app's web view a proxied response only once it ends, and the daemon's event stream never ends, so none of it arrived; replies showed all at once. The desktop shell now reads the event stream and relays each event to the page, and replies take their text from that relay (Yggdrasil Desktop with the event relay). The desktop app uses the relay on every platform; in a browser, nothing changes.

## [1.4.0] - 2026-10-02

Stable release of 1.4.0. It contains everything in [1.4.0-beta.1](#140-beta1---2026-10-01), the AI experience platform and the remaining Train Your Own AI items, plus the changes below. Existing API routes, configuration, and data are compatible with 1.3: the changes add routes, optional fields, and database tables, and migrations run automatically. NVIDIA (CUDA) training, PostgreSQL and MySQL knowledge sources, and the Mac App Store sandbox have not been tested on that hardware or in that build. Binaries and the apt repository are not signed.

### Added

- Documentation for 1.4.0. New [Configuration](docs/configuration.md) (data directory layout, every `config.json` key, environment variable, and setting) and [Command line](docs/cli.md) (`yggdrasil-daemon` flags, the Linux systemd service, and every `yggctl` command) pages. The API reference now lists every route, grouped by area, and every event type. The user guide adds sections on memory, knowledge, tools, notifications, profiles, privacy, and diagnostics.

### Changed

- `api/openapi.yaml` now describes every route: the control plane, the OpenAI-compatible API, and the MCP server, with request bodies, responses, the error shape, and bearer authentication. It previously covered 51 of them. A test fails when the daemon serves a route the spec does not describe, or the spec describes one the daemon does not serve.
- Rewrote the architecture, privacy, and troubleshooting pages for 1.4.0, and updated tools, capabilities, compatibility, and the README. Removed statements that no longer matched the code, such as where retrieved knowledge goes in a prompt and which `/chat` fields exist.
- Each release carries its own screenshots. After a release is published, the Screenshots workflow captures the README stills and the walkthrough from that release's interface and attaches them as `screenshot-<name>` files, which yggdrasil.yeix.io shows. Started by hand with a tag, it attaches them to an existing release.

### Fixed

- Chat answers keep their indentation. Nested lists stay nested and code blocks keep their indentation; only extra spaces in the middle of a line are collapsed.
- The Screenshots workflow passes again. Screenshot validation accepts the lowercase opaque value that ImageMagick 6 on Ubuntu prints, a screen that does not become ready is loaded once more before the run fails, and a failure now prints the page's errors and keeps what the page showed.

## [1.4.0-beta.1] - 2026-10-01

Beta pre-release of 1.4.0. It adds the AI experience platform and the remaining Train Your Own AI items. Existing API routes, configuration, and data are compatible: the changes add routes, optional fields, and database tables, and migrations run automatically. NVIDIA (CUDA) training, PostgreSQL and MySQL knowledge sources, and the Mac App Store sandbox have not been tested on that hardware or in that build. Binaries and the apt repository are not signed.

### Added

- Sandboxed Mac App Store builds can train on a paired computer. A sandboxed copy of Yggdrasil no longer tries to download Python; it says training can't run on this computer and chooses a paired computer running Yggdrasil Core. A store build can also ship the training and text-recognition environments beside the daemon, and `yggdrasil-daemon -python-envs` lists what to bundle.
- Versioned client contract. Events, run traces, and answer metadata (citations, steps, files) carry a contract version (`1.0`), every API response has a `Yggdrasil-Contract` header, and `/api/v1/version` describes the contract. Fields are only ever added within a major version; a test fails if one is removed or renamed. An app built for another major version gets a clear 426 that says which side to update.
- Caching with declared policies. Every cache says what it keeps, how long, what clears it, where it applies, and how private it is; credentials are never cached. Repeat web searches and page reads within minutes are answered from memory, so nothing leaves the computer again. The capability inventory is cached and refreshed when models, computers, or tools change, and Hugging Face searches use the same cache. Diagnostics lists the caches in advanced mode, run details show cache hits, and deleting run records clears personal caches. Routes are under `/api/v1/caches`.
- Train on NVIDIA GPUs. A computer with an NVIDIA GPU can now train specialized AIs with PyTorch, using LoRA, or QLoRA when the GPU's memory is tight. Training fit uses the GPU's own memory, and a Mac can send training to a paired NVIDIA PC. The first run installs PyTorch with the CUDA libraries for the computer's driver (about 4 GB).
- Knowledge from databases and web APIs. Connect a SELECT query on a SQLite file, PostgreSQL, or MySQL, or a URL that returns JSON, CSV, or text, and each row or item becomes a passage. Yggdrasil only reads: queries run read-only. Data older than the chosen interval (5 minutes to a day) is fetched again when a question uses it, and if a fetch fails the last data keeps answering. Passwords and tokens are stored apart from the database and never shown again.
- Export a specialized AI as one GGUF file. The Deploy step merges the trained revision into its base model, so LM Studio, Ollama, llama.cpp, and other GGUF tools can run it without Yggdrasil. The file is a little larger than the base model, and the AI's instructions are shown to copy as the system prompt. A notification says when a large export is ready.
- Quality test set. Ten representative requests, each with the behavior it must have, run against the stub model on every change and against real models with `make quality-real` or a weekly self-hosted workflow. The behavior checked includes a simple question staying direct, a price question using knowledge, a risky command asking first, a long conversation remembering an early fact, a current question being looked up, and a request with parts being planned. Running it against Llama 3.2 1B led to three fixes. Plain questions no longer offer tools, which the small model misused. Short capability questions are answered from the inventory. An answer that claims a change no tool made is now called out.
- Scanned PDFs in Knowledge. Pages without a text layer are read with text recognition, so scanned manuals, warranties, and price sheets become searchable and are cited by page. The first scanned PDF installs the recognizer (about 110 MB) on this computer; nothing is sent elsewhere. A scanned PDF attached to a chat explains how to connect it on the Knowledge page instead.
- Capability inventory. Yggdrasil keeps track of which models, computers, tools (built in, connected, and MCP), connected services, providers, and files exist right now, and what they let it do. Ask "Can you generate an image?" or "Which computer can run Qwen 2.5 14B?" and the answer comes from that inventory instead of a guess. Diagnostics lists every ability, with how it works or what would add it. Route: `/api/v1/capabilities`.
- Structured results. `/v1/chat/completions` supports `response_format` (`json_object` and `json_schema`). Answers are checked against the schema, safely repaired, and asked for once more if needed; JSON that still does not fit gets a 422 that lists the problems. Tool arguments are checked and repaired before a tool runs. Price and significance automations read their result's JSON with the same repairs, so `"$1,299"` counts as a price, and ask the model once when it is missing.
- Profiles & Orchestration. In advanced mode, a profile has an Orchestration section that sets its reasoning level and planning, as well as workers, parallelism, verification, tool calls, memory, context budget, fallback, and a time limit. Blank keeps each default.
- Run details. Every chat, API request, and automation is traced. In advanced mode, each answer has "Run details", showing the strategy, effort, models with their computer, load time, time to first token, tokens per second, and cached tokens, as well as tools with timings, plan workers, verification passes, retries, context size, and latency. Routes are `/api/v1/runs`.
- What left this computer. Settings lists every web search, page read, paired computer, external server, and connected service that a chat, automation, API request, or training run sent data to, with a 30-day summary. Memories and knowledge sources can be marked "This computer only"; chats that use them run here even when a paired computer would otherwise answer. Run records (stored prompts and tool results) are kept for 30 days by default, with a choice of 7 days to keeping them, and can be deleted at once. Routes are `/api/v1/egress` and `/api/v1/privacy`.
- MCP, both ways. **Tools → Add tools** adds MCP servers as tool sources: pick one from the gallery (Folders, Browser, Notion, Linear, Jira & Confluence, Sentry, GitHub, Context7, DeepWiki, SQLite, PostgreSQL, Brave Search, and more) and answer a question or two; paste any app's settings, a web address, or a command line; or bring the servers already set up in Claude Desktop, Claude Code, Cursor, VS Code, Windsurf, Gemini CLI, or LM Studio. Services with a sign-in open one in a browser window (OAuth with PKCE and app registration). Their tools work in chat and automations like connected services: reading runs, changes ask first, results are untrusted data, and secrets stay in the secrets directory and are scrubbed from results. Servers on this computer start when a tool is needed and stop when idle, get only a safe environment, and say plainly what to install when Node.js or uv is missing. Each source has a log, per-tool Ask first and on/off, ready-made prompts that start a chat, resources, and an opt-in for servers to ask the AI for help. Other apps can use Yggdrasil too: `/mcp` offers `ask_local_ai`, `list_local_models`, and `search_my_knowledge` within an API key's permissions, `yggctl mcp` bridges apps that start a program, and API Access gives the settings to copy for each app. Routes are under `/api/v1/mcp`. See [MCP](docs/mcp.md).
- The API gets the same assistant. `/v1/chat/completions` uses the whole conversation, including the system prompt, not only the last message. `reasoning_effort` sets the effort. An optional `yggdrasil` object opts into memory and connected knowledge, narrows tools, chooses effort and placement, and streams progress and tool activity. Answers carry their sources and steps. Each API key has permissions on the API Access page (memory, knowledge, tools, placement) that requests can narrow but never widen. API requests no longer use your memories unless they ask.
- Personalization. In Settings, choose answer length, tone, format, and units, and add a note about yourself and how you like answers. It applies to every chat, automation, and API request. It is kept apart from permissions: a preference or memory that tries to grant one ("you can always push without asking") is refused, with a pointer to Tool permissions.
- Knowledge search by meaning. Install an embedding model, such as Nomic Embed Text v1.5 (new in the catalog, 146 MB), and Mimir finds passages that answer a question even when they use different words: "warranty" finds your guarantee policy. Word matches still count, and passages both searches find rank first. Passages are embedded in the background and only again when their text changes; a chat, an automation, or training goes first. The Knowledge page says when a source is searchable by meaning. An installed reranker model reorders the best passages. Without an embedding model, search works as before.
- Connected services: GitHub and Home Assistant. Connect them in Settings with a token. The AI can then search and read issues and pull requests, comment (after asking), check lights and sensors, and control devices (after asking). Tokens stay on this computer outside the database, are never shown again or given to the AI, and are scrubbed from anything a service returns. Settings explains the narrowest access to grant. Routes are under `/api/v1/connectors`.
- Auto knows your specialized AIs. A question about what one was trained for (or one that names it) goes to that AI, and "What I did" says why. Questions that need the web, your files, or code still go to a general model, because specialized AIs answer without tools. An AI whose trained adapter is missing from this computer is skipped and explains itself.
- Embedding, reranker, and classifier models are recognized as supporting models. They are labeled on the Models page, and they are left out of the chat and automation model menus. Auto and fallback never pick them to answer.
- Sharing the computer. Chat comes first, then automations, then benchmarks, then training. An automation waits for your chat to finish instead of loading a model alongside it, a benchmark no longer unloads the model a chat is using, and training frees memory only once nothing else is running. Waiting work says what it is waiting for. A chat during training is still answered, with a note that training is using the computer and about how long is left. An automation that runs out of memory twice in a row is paused and tells you why, instead of failing every day.
- Notifications. The bell next to the Yggdrasil name shows unread notices: finished and failed automations, tools an automation skipped, finished or failed model downloads, deployed AIs, and newly paired computers. Click one to go to it, mark all read, or dismiss. Notices are kept by the daemon, so ones that arrived while the window was closed are waiting. Desktop notices are still posted for automations, and each delivery is recorded. Routes are under `/api/v1/notifications`.
- Automations use what chat uses: Auto can pick the model for each run, and memories and connected knowledge apply. An automation can notify only when it fails. Tools are approved when the automation is saved, including tools that change things, which the form lists apart. A run that reaches a tool you did not approve skips it, finishes, and tells you which tool to approve, instead of failing.
- Redrawn Yggdrasil mark: vector source, interface colors, small-size version, theme-aware favicon. The sources are in `docs/brand/logo/`, and `make icons` renders the Linux icons from them.
- Ratatoskr, the Yggdrasil mascot. He appears at the moments that matter: thinking while a reply is written, delivering work to a paired computer, celebrating a finished download or deploy, dropping his acorn on an error, and asleep when no model is loaded. He is a still frame when the system asks for reduced motion.
- Only the tools a request needs. A weather question is offered web search, not the terminal or Git; a request about a file or a commit gets file or Git tools. A tool that was not offered is refused, so the model cannot reach beyond it. Tool calls have time limits and report why they failed. `web.search`, `files.read`, `shell.run`, and similar capability names reach the built-in tools, and a call a small model writes out as text is still run.
- Effort in chat: Auto, Fast, Balanced, or Thorough. Fast answers in one go; Thorough reads more pages, uses the largest model that fits, and checks figures twice. Auto keeps quick questions fast and gives questions about your data, and requests with several parts, more care. The choice is remembered.
- Stop means stop. Stop ends every model call, tool, plan step, approval, and paired computer working on the reply, from any window or through `POST /api/v1/chat/stop`, and keeps what was already written, marked as stopped.
- Big requests are worked through in parts. "Compare Ollama, llama.cpp and MLX" looks each one up on the web side by side; "find three NAS drives, then compare price per TB, then make a spreadsheet" runs step by step, each step building on the last. A checklist shows the parts while they run, and one answer (or file) comes from all of them.
- Answers are checked before you see them. Calculations are recomputed, and figures in answers that use your files, knowledge, or web results must appear in the lines about the same thing. A wrong figure is sent back to the model to fix once. Anything still unconfirmed is called out under the answer ("could not confirm 20 in the sources"). An answer that only describes tools, instead of answering, is asked for again without tools.
- Small-model notes. On the Models page, models under 4B parameters say they can mix up facts and numbers from your files and knowledge, and suggest a larger model that fits the computer. In chat, an answer from a small model that used your files or knowledge carries the same note. Auto prefers a larger model for questions about your data.
- Files in and out of chat. Attach documents, spreadsheets, PDFs, and code files with the paperclip, by dropping them on the message box, or by pasting. The answer cites the file, and later questions in the chat can still use it, including files the assistant made ("add a column to that spreadsheet"). Ask for a file ("make a spreadsheet of these prices") and the answer comes with a download. Spreadsheets are real `.xlsx` workbooks. Files stay on this computer, under `artifacts/` in the data directory, and are deleted with their chat. Routes are under `/api/v1/artifacts`.
- Each page shows its Norse name above the title (Ratatoskr for Chat, Mimir for Knowledge, Bifrost for Computers, and so on), and each tab in the sidebar has its Elder Futhark rune. Tab names are unchanged. Click a Norse name, the mascot, or the logo for a short lore entry: who it is in the myths and what it is in Yggdrasil.
- Auto model. New chats use Auto, which picks an installed model for each message. Coding questions go to a coding model, questions about current information to a model that can use tools, and quick questions to a fast model that is already loaded when there is one. The answer's "What I did" says which model answered and why. `auto` is also a model in `/v1/models`.
- Current questions are looked up first. When a question needs current information (weather, news, prices, scores, links) and web search is allowed, Yggdrasil searches the web and reads the best page before the model answers, so small models answer from the page instead of guessing or picking the wrong tool.
- Quiet recovery. If a model fails before it answers, another installed model answers instead, and the answer says so, with a note when the model that answered is noticeably smaller. Auto skips a model that failed in the last 10 minutes.
- Memory. Say "Remember that…" in any chat, and Yggdrasil keeps it across chats, restarts, and model changes; "Forget…" and "What do you remember?" work too. Answers that used a memory list it as a source. The Memory page lists memories by category, where you can add, edit, pause, or delete them, and turn memory off. Each chat has a Memory on/off switch. Passwords, keys, and card numbers are not saved. Routes are under `/api/v1/memory`.
- Long conversations keep working. When a chat's history passes half of the model's window, older messages are summarized after the reply and the summary takes their place; the messages stay saved. The context meter shows how many messages the summary covers.
- Chat answers show their sources (web pages, knowledge passages, and files) and a "What I did" summary of searches, pages read, and knowledge used. Errors are explained in plain language with a next step and a retry; technical detail is under Details. Knowledge lookups show progress while the answer is prepared.
- Mimir connected knowledge. Connect a file, a folder, pasted content, an Excel workbook (each sheet is a table), or a PDF with a text layer (each page is cited); chat adds the passages that match each question. File and folder sources reindex when the files change. A profile lists `knowledge_sources`. Routes are under `/api/v1/knowledge`.
- Train your own AI. The Train page builds a specialized AI from a base model, examples, instructions, and connected knowledge. Yggdrasil recommends Training, Knowledge, or Both for each piece of material, flags weak examples, estimates training fit separately from inference fit, trains a LoRA or QLoRA adapter with MLX on Apple Silicon, and compares base and specialized answers before deployment. A deployed AI is the model `sai:<name>` in chat and `/v1/chat/completions`. The first training run installs a private Python environment under `runtimes/python` and downloads the base model's training weights from Hugging Face. Norn can train on a paired computer that has more memory, and the Review step lets you pick the computer; the adapter returns to the computer that owns the AI. The Train page and the Profiles editor attach existing knowledge sources, files, and folders. "Try an example" sets up a sample tire shop assistant with notes on each step, and the Material step shows sample files in each format.
- `yggctl completion <bash|zsh|fish>` prints a completion script for `yggctl` commands, `automations` subcommands, and their flags. Homebrew, the deb and rpm packages, and the macOS and Linux archives install or include the scripts.

### Changed

- Context budgets count tokens with the running model's own tokenizer. Which earlier messages fit, when a long conversation is summarized, and the context gauge's sections use llama-server's count instead of guessing four characters per token, a guess that is often well off for code and for languages other than English. Counts are cached for an hour and cleared with run records. When the model is not running on this computer, the estimate is used and the gauge still shows "~".
- Connected knowledge and retrieved content reach the model as labelled data in the user turn, not in the system prompt, and the model is told not to follow instructions inside it. After a turn reads untrusted content, a tool that changes something (write, terminal, Git commit or push) asks first even when the profile allows it. Knowledge search drops passages that score far below the best match, and table columns such as `in_stock` read as "in stock".

### Fixed

- The context gauge undercounted every turn after the first: it counted only the prompt tokens llama-server processed, not those it reused from its cache. It now shows the whole prompt.
- A daemon started with `--data-dir` stays in that directory when its `config.json` does not list `data_dir`. Before, it used the default data directory, so a second or test daemon could open your real database.
- The database refuses to start with two migrations of the same number, instead of silently skipping one.
- The chat page no longer reopens its event stream on almost every render, which dropped events such as a plan's checklist. The first message of a new chat no longer disappears while the reply is being written.
- A model whose `llama-server` exits while loading, for example a damaged file, now fails at once instead of after a two-minute wait.
- More current-information questions are recognized (news, scores, prices, exchange rates, "near me"), and cues match whole words only.
- Chat starts faster when a paired computer is offline. Peer health is checked in the background every 10 seconds and reused for 20; a check that has to run during a turn waits at most 1.5 seconds. Before, each turn waited the full timeout for an offline peer.
- A fast reply, such as a memory confirmation, no longer shows twice.

## [1.3.1] - 2026-09-29

Patch release. The API is unchanged. Binaries and the apt repository are not signed.

### Fixed

- A saved schedule turns on “Keep running in background,” so closing the window does not stop the daemon that runs it.

## [1.3.0] - 2026-09-29

Minor release. The API, CLI, and database migration are backward-compatible. Binaries and the apt repository are not signed.

### Added

- Scheduled automations. The daemon runs a saved prompt on a one-time, daily, weekly, or interval schedule, keeps a history of each occurrence, retries timeouts and connection failures, and can post an operating-system notice when the result matches the notification rule. Read-only tools can run while the window is closed. Automations are available in the desktop UI and through `yggctl automations`. `GET /api/v1/automations` is new.

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
