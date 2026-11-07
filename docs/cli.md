# Command line

Toskar Core installs two programs:

- **`toskar`** runs Toskar: the API, the web UI, models, schedules, and the connection to other computers.
- **`toskarctl`** talks to a running daemon from a terminal or a script.

Their names from before the rename, `yggdrasil-daemon` and `yggctl`, are installed as links to them, so launchd agents, scripts, and MCP settings that run the old names keep working. The join command one computer prints for another says `yggctl join`, which works on that computer whatever version it runs.

## `toskar`

```bash
toskar                      # use the default data directory
toskar -data-dir ~/ygg-test # use another one
```

| Flag | Meaning |
| --- | --- |
| `-data-dir <path>` | Data directory. Defaults to the one for your OS (see [Configuration](configuration.md#data-directory)). |
| `-version` | Print the version, license, and corresponding source, then exit |
| `-python-envs` | Print, as JSON, the Python environments a sandboxed app bundles, then exit |

The daemon runs in the foreground and logs JSON to standard output and to `logs/daemon.log`. Stop it with Ctrl+C.

How it is started depends on how it was installed:

- **deb and rpm packages:** they install a systemd service, `toskar.service` (also reachable as `yggdrasil.service`), and enable and start it. It runs as the system user `yggdrasil`, so its data directory is `/var/lib/yggdrasil/.local/share/toskar`, or `/var/lib/yggdrasil/.local/share/yggdrasil` for an install from before the rename. Use `sudo systemctl status toskar`, `restart`, and `stop`. `journalctl -u toskar` shows its log. Removing the package stops and disables the service.
- **Homebrew and the headless archives:** they install the programs only. Run `toskar` from a terminal or your own service manager, or let `toskarctl chat` start it in the background. `install.sh ci` installs and starts one in a folder for a CI job ([Toskar in CI](ci.md)).

## `toskarctl`

`toskarctl` reaches the daemon at `TOSKAR_URL`, or `http://127.0.0.1:7331` when that is not set. When the daemon listens beyond this computer, set `TOSKAR_API_KEY` too. It is sent as the bearer token.

| Command | What it does |
| --- | --- |
| `toskarctl help [command]` | What each command does, with its flags and examples. `toskarctl <command> --help` is the same |
| `toskarctl version` (or `about`) | Version, license, and corresponding source |
| `toskarctl paths` | Default data, model, runtime, log, and database paths |
| `toskarctl ask [flags] [prompt]` | Send one prompt and print the answer, for scripts and CI jobs (below) |
| `toskarctl chat [flags] [message]` | Chat in the terminal, in the same chats as the apps (below) |
| `toskarctl connect <agent> [--model id] [--write]` | Print, or write, the settings for a coding agent to use a model on Toskar (below) |
| `toskarctl models list [--installed] [--json]` | The models, smallest first, with download size, fit on this computer, and whether each is installed |
| `toskarctl models install <id> [--node <id>]` | Download a model and wait until it's installed, printing its progress every tenth of the way |
| `toskarctl automations …` | Manage scheduled automations (below) |
| `toskarctl mcp` | Let an app that starts MCP servers as programs, such as Claude Desktop, use Toskar (below) |
| `toskarctl join-token create [--ttl 15m]` | Make a one-time join token and print the command that adds a computer to this network ([Clustering](clustering.md#joining-with-one-command)) |
| `toskarctl join-token list`, `revoke <id>` | List recent join tokens, or stop an unused one |
| `toskarctl join --server … --token … --fingerprint … [--name …] [--wait 60s]` | Join this computer to the network that made the command, optionally renaming it, and waiting for Toskar to start. Exits 0 when joined or already joined, 1 when refused, 2 for a usage error |
| `toskarctl network` | This computer's address, fingerprint, network, and paired computers |
| `toskarctl leave` | Leave the network: tell each paired computer, then forget them all |
| `toskarctl people …` | Add, change, and remove the people who use this Toskar (below) |
| `toskarctl completion <bash\|zsh\|fish>` | Print a shell completion script |

### Ask

`toskarctl ask` sends one prompt and prints the answer, so a script or a CI job can use Toskar as a step: summarize a bug report, label an issue, read a failing log.

```bash
toskarctl ask "Write a haiku about Juneau"
gh issue view 12 | toskarctl ask "Summarize this bug in two sentences"
toskarctl ask --file build.log "What's the first real error, and its likely cause?"
toskarctl ask --json-schema triage.json "Suggest labels for this issue" < issue.txt | jq -r '.labels[]'
```

Text piped in, or from `--file`, is sent as data to work on, apart from the prompt: it goes in a fence it can't close, and the model is told to follow only the prompt. Issue text and logs are untrusted, and this keeps an instruction inside them from becoming yours. Text piped in with no prompt is the prompt.

| Flag | Meaning |
| --- | --- |
| `--model id` | A model, `auto` (the default), or a specialized AI, `sai:<slug>` |
| `--profile id` | Answer as this profile instead |
| `--system text` | A system prompt |
| `--file path` | Data to work on, from a file (`-` for standard input) |
| `--json-schema path` | Answer with JSON that fits this JSON Schema. Toskar checks the answer, and asks the model once more with what was wrong when it doesn't fit |
| `--max-tokens n` | The longest answer, in tokens |
| `--temperature t` | Sampling temperature, such as `0` for the steadiest answers |
| `--no-thinking` | A reasoning model answers without thinking first: much faster, for a job that doesn't need it |
| `--timeout d` | How long to wait for the answer: 5 minutes unless set, such as `--timeout 20m` for a long log on a small computer |
| `--memory` | Let the answer use and add to memory. Off unless given, so a CI job never reads or writes a person's memories |
| `--knowledge` | Let the answer use connected knowledge. Off unless given |

The answer never uses tools. It goes through the OpenAI-compatible API (`/v1/chat/completions`), so it saves no conversation, and an API key's permissions apply: a key that may not use memory can't be given `--memory`. For a CI job, make the key with **For a CI job** on the API Access page: it can only ask, with no memories, knowledge, or tools, whatever the job sends ([API key permissions](api.md#openai-compatible-api)).

Exit status: 0 for an answer, 1 for a model or connection error, 2 for a usage error, and 3 for an answer that doesn't fit `--json-schema`.

A CI runner with no Toskar nearby can run one: [Toskar in CI](ci.md).

`ask` reads standard input whenever it isn't a terminal. Where it's an open pipe that never ends, such as some job runners and `ssh` without a command, give it `< /dev/null`.

### Chat in the terminal

`toskarctl chat` holds a conversation with Toskar in the terminal (#959). It's one of your chats, as in the apps: one begun here shows in History on your phone, and `--resume` continues one begun there. Answers show as they're written, and each ends with its speed in tokens a second. When Toskar changes what it showed, such as taking back a sentence before it uses a tool, `chat` rewrites it. If that text has scrolled out of view, the new text goes below it instead.

```bash
toskarctl chat
toskarctl chat "What's a good first car for a teenager?"
toskarctl chat --model qwen2.5-7b-q4 --file notes.md "Turn these into an agenda"
toskarctl chat --resume
toskarctl chat --resume 3f2a9c1e
```

| Flag | What it does |
| --- | --- |
| `--resume [id]` | Continue a chat. Without an id, it lists your 10 most recent chats to pick from, then shows the last exchange |
| `--model id` | A model, or `auto` (the default) |
| `--profile id` | Answer as this profile |
| `--file path` | Data to work on, sent with the first message |

While chatting, `/new` starts a new chat, `/model id` and `/profile id` change who answers the next message, `/attach path` sends a file as data with the next message, `/log` shows what ran in this chat with its result and who allowed it, `/help` lists these, and `/exit` or Ctrl-D leaves.

**Tools that ask first.** When the model wants a tool that asks before it runs, such as running code in the general assistant, `chat` shows the tool, why, what it would run, and, when Toskar is on another computer, which one it runs on, and waits: `y` runs it once, `n` doesn't, and `a` lets that tool run for the rest of this chat without asking. Your answer goes to Toskar the same way an app's does.

**Starting from nothing.** `toskarctl chat` needs no flags, profile, or config file. When Toskar isn't running on this computer, it starts it and says how to stop it. Toskar stays running in the background after the chat, so the next one opens right away and a long task doesn't depend on the terminal. An install with a service starts through that service. A Linux service needs an administrator, so `chat` gives the command instead. When Toskar isn't installed, it asks first, with the download's size, then runs the installer; piped in, it gives the command. With `TOSKAR_URL` set, it starts nothing, since you've chosen a Toskar. When Toskar has no model yet, it names the one that suits this computer, with its size and how well it fits, and installs it if you say yes. Piped in, it gives the install command instead. While a task works, each step is one line, such as `Running go test ./...`, with a line for a command that failed or took a while. `/log` has the details.

**A task reads freely and asks before it changes things.** In a task, reading inside the project and running named tests and builds, such as `go test` or `npm test`, don't ask. Changes do, whatever the profile allows for chat: writing a file, `git add` and `git commit`, installs, `make` and `npm run`, which run whatever the project says, and reading outside the project. `a` lets that tool go on for the rest of this chat. Tests run the project's own code too, so don't start a task in a project you don't trust. Commands that can't be undone always ask.

**Long commands.** In a task, a command can run for up to 30 minutes, and you see what it writes as it comes, without its colors. Everywhere else the limit is 2 minutes. Ctrl-C stops the work and what the command started, keeps what was written, and leaves you in the chat to say what to do next. A second Ctrl-C stops waiting for that to finish. At the prompt, Ctrl-C leaves the chat. Something a command starts in the background, such as a server, keeps running when the command ends.

**Toskar keeps its own record of a task.** A model can say something works when it doesn't. So Toskar notes which checks actually ran, such as `go test` or `npm test`, how they ended, and what changed, from the tools and from Git. If the last check failed, nothing ran to check a change, a change came after the last check, or test files changed, the model is sent back once to deal with it. Whatever's still in doubt then shows under the answer as "Toskar noticed", where the model can't leave it out. This recognizes common test and build commands, not every project's own, and it's in English for now. Outside a Git project, it only sees files changed with the file tool. No note doesn't prove the work is right: it means the checks Toskar could see passed.

**The model matters most for this work.** Small models often can't finish a task, and some claim they did. When nothing installed does well at it, `chat` says which model would and how to install it, or says this computer can't run it.

**Your project.** On the computer Toskar runs on, `chat` works in the folder you started it in: commands run there, and its file tools reach only that folder. It follows the project's own notes in `AGENTS.md`, `TOSKAR.md`, or `CLAUDE.md`, the first one it finds. Those set conventions, not permissions. When Toskar is on another computer, it uses its own folder there.

**Work goes on until it's done.** Asked to do something rather than asked a question, such as "Fix the failing test" or "Set up this project", `chat` works step by step: it runs something, reads the result, fixes what failed, and tries again. It stops when it's finished, when the same step keeps giving the same result, or after 100 steps, and then says what it did and what's left. A follow-up that goes on with the work, such as "go ahead and make that fix", "yes, do it", or "try again", works the same way. A question in the same chat is answered as a question. A profile's own limit on tool calls still applies.

**After reading something from outside,** such as a web page, a file outside the project, or the output of a command Toskar can't vouch for, a tool that changes things asks every time, even after `a`, so those words can't steer it on an earlier yes. Output from a known command, files in the project, and Git in it don't count, unless the project folder is your home folder or holds it, so `a` carries a fix-and-test loop along. That means `a` trusts the project: a file in it could say what to do, and known commands such as `make` run its code. Choose `a` only for a project you trust.

**Commands that always ask.** A terminal command that can delete or change things outside the chat, such as `rm`, `sudo`, or `git push --force`, asks every time and says it can't be undone. So does one Toskar can't read all of or doesn't recognize, such as a script or `curl … | sh`. `a` isn't offered for these, and a profile that allows the terminal doesn't skip them. An automation has nobody to ask, so it skips the command and reports that. Commands like `git status`, `go test`, or `npm run build` follow your answer as usual, and they run the project's own code, so allow them only for projects you trust.

**Piped in,** `chat` answers once, with what's piped as data to work on, and prints the command that continues the chat on standard error:

```bash
make 2>&1 | toskarctl chat "Why did this fail?"
```

With no one at the terminal to answer, a tool that asks first isn't run. Run traces mark these chats with the client `terminal` ([api.md](api.md)).

### Connect a coding agent

`toskarctl connect <agent>` prints the settings a coding agent needs to use a model on Toskar directly, with the agent's own tools (#959, #843). `--write` writes them instead. It writes only the agent's Toskar settings and keeps everything else in the file.

| Agent | Format it speaks | `--write` writes |
| --- | --- | --- |
| `claude-code` | Anthropic's Messages (`/v1/messages`) | `.claude/settings.local.json` in this folder, which Claude Code keeps out of git: `ANTHROPIC_BASE_URL`, `ANTHROPIC_AUTH_TOKEN`, and the model for `ANTHROPIC_MODEL` and its Opus, Sonnet, and Haiku defaults, so background work reaches Toskar too |
| `codex` | OpenAI's Responses (`/v1/responses`) | A `toskar` provider in `~/.codex/config.toml` (or `$CODEX_HOME`), and a `toskar` profile, `toskar.config.toml` beside it, as Codex 0.134 and later read profiles. Run `codex --profile toskar`; your default stays as it was. A `[profiles.toskar]` table from before is removed, since Codex refuses one now |
| `opencode` | Chat Completions, as `raw:<model>` | A `toskar` provider in `opencode.json` in this folder, using `@ai-sdk/openai-compatible`, and the default `model` |
| `aider` | Chat Completions, as `raw:<model>` | Nothing: Aider reads `OPENAI_API_BASE` and `OPENAI_API_KEY` from the environment, so put the printed lines in your shell profile |

Editors' agents and AI features take an OpenAI-compatible endpoint, so their model is `raw:<model>` (#960). Most keep it in their own settings screens, so `connect` prints what to enter there, and `--write` explains that:

| Editor | What it prints |
| --- | --- |
| `continue` | A `models:` entry for `~/.continue/config.yaml`: `provider: openai`, `apiBase`, `apiKey` (from `~/.continue/.env` when `TOSKAR_API_KEY` is set), chat, edit, and apply roles, `tool_use`, and `useLegacyCompletionsEndpoint: false`, since Continue edits through `/v1/completions` otherwise. Checked in VS Code: [Continue guide](integrations/continue.md) |
| `cline`, `roo-code` | The "OpenAI Compatible" provider's base URL, API key, model ID, and context window |
| `zed` | A `language_models.openai_compatible` provider for `settings.json`, whose model's `max_tokens` is the window it runs with on Toskar |
| `jetbrains` | AI Assistant's third-party "OpenAI-compatible" provider: URL, key, and model |
| `copilot` | Copilot Chat's own-model option: "Chat: Manage Language Models", "OpenAI Compatible", base URL, and key. Copilot lists the models from `/v1/models`, which shows `raw:` models to a key that may use them |

The editors' settings follow their docs as of November 2026. Continue's were checked in VS Code ([guide](integrations/continue.md)); the others weren't checked in a running editor, as Claude Code and Codex were.

- **The model:** `--model`, or the best installed one for an agent: one that calls tools, then one made for code, then one that fits Excellent or Good, then the largest.
- **The key:** on this computer none is needed, so agents get the stand-in `toskar-local`. For another computer, set `TOSKAR_URL` and `TOSKAR_API_KEY` to a key with raw access ("Drive models directly, for coding agents" in API Access). Printed settings name the variable instead of showing the key.
- **Window:** agents send long instructions and many tools, so a model gets a bigger window when it needs one and one fits (#962). When the tools take more than a quarter of the window, such as an agent with MCP servers, the model gets the most relevant ones and finds the rest with `tools_search`, which Toskar answers itself ([api.md](api.md), #843).
- **Step by step, tested with Qwen 3.5 9B, on this computer and another:** the [coding agents guide](integrations/coding-agents.md).
- **Checked by hand on Apple silicon with Qwen 2.5 7B:** Claude Code 2.1.310 read a file with its Read tool, and Codex CLI 0.138 ran `cat` with its `exec_command` tool, each answering from the file. A model this small can answer without using a tool when the request doesn't ask for one. With the MCP servers of a Claude Code setup loaded (about 17,000 tokens of tools), the tools were narrowed and the request ran, but the 7B mis-copied a long path into its Read call. A larger model does better at both.

### Automations

```bash
toskarctl automations list
toskarctl automations get AUTOMATION_ID
toskarctl automations parse "every morning at 8, tell me if the price is below $500" --zone America/Los_Angeles
toskarctl automations create --request "every morning at 8, tell me if the price is below $500" --zone America/Los_Angeles
toskarctl automations create --name "Morning price" --prompt "Check the price of …" \
  --profile general-assistant --model auto --schedule daily --at 08:00 --zone America/Los_Angeles \
  --notify condition --condition-kind threshold --condition-op below --condition-value 500
toskarctl automations update AUTOMATION_ID --at 07:30
toskarctl automations run AUTOMATION_ID
toskarctl automations pause AUTOMATION_ID
toskarctl automations resume AUTOMATION_ID
toskarctl automations delete AUTOMATION_ID
```

`parse` reads a request the way the Automations page does and prints what it understood: name, task, schedule, notification, and notes. `create --request` creates the automation from a request, with `--profile general-assistant` and `--model auto` unless you pass others; any other flag you pass changes what the request said. Without `--request`, `create` needs `--name`, `--prompt`, `--profile`, and `--model`. `--model auto` lets Auto pick a model for each run. `update` changes only the flags you pass.

| Flag | Values |
| --- | --- |
| `--schedule` | `once`, `daily`, `weekly`, or `interval` |
| `--at` | `HH:MM` for daily and weekly; an RFC 3339 time for `once` |
| `--every` | A duration for `interval`, such as `6h` or `30m` |
| `--weekday` | `0`–`6` for weekly; Sunday is `0` |
| `--zone` | An IANA time zone, such as `Europe/Berlin` (default `UTC`) |
| `--notify` | `always` (default), `condition`, `change`, `failure`, or `none` |
| `--condition-kind` | With `--notify condition`: `threshold`, `available`, or `significant` |
| `--condition-op`, `--condition-value` | With `threshold`: `below` or `above`, and the amount |
| `--tool` | A tool the automation may use unattended. Repeat it for more tools. Tools listed here were approved by you and run even if they change things. |
| `--disabled` | Create the automation paused |

### People

```bash
toskarctl people list [--role member] [--disabled true|false]
toskarctl people add "Ana" --role member --email ana@example.com
printf '%s\n' "$PASSWORD" | toskarctl people add "Ana" --username ana --password-stdin --must-change
toskarctl people show ana
toskarctl people set ana --role admin --email ana@work.example
toskarctl people set ana --disable
toskarctl people link ana
toskarctl people keys ana [--revoke KEY_ID]
toskarctl people devices ana [--revoke KEY_ID]
toskarctl people rm ana --transfer-to Owner
toskarctl people activity [ana]
```

A person is named by their ID, name, or username. When two people share a name, use the ID; the command lists them.

- **Adding:** without a password, `add` prints a one-time link where they choose a username and password, as the People page does. With `--username` and `--password-stdin`, they can sign in at once. `--must-change` has them choose their own password first.
- **Passwords** are read from standard input only, never from the command line, where they'd show in the process list and shell history. At a terminal, the password isn't shown as you type it, except on Windows.
- **Removing:** `rm` asks you to type their name, unless you pass `--yes`. Their chats, memories, automations, and files are deleted, or given to `--transfer-to`. Their keys and devices stop working.
- **Who may do what:** Admins manage Members and Visitors. The Owner also manages Admins, and nobody removes themselves or the Owner.
- **Signing in:** on this computer, `toskarctl` acts as the Owner. From elsewhere, it needs an Admin's key in `TOSKAR_API_KEY`, with `TOSKAR_URL`.
- **Output:** `--output json` prints what the API answered, for scripts. The command exits 1 when the API refuses and 2 for a usage mistake.

### MCP bridge

Apps such as Claude Desktop start MCP servers as programs. `toskarctl mcp` is that program: it reads MCP messages on standard input, sends each to the daemon's `/mcp` endpoint, and writes the replies to standard output. API Access → **Use Toskar in other AI apps** shows the exact configuration to paste for Claude Desktop, Claude Code, Cursor, VS Code, and other apps. See [MCP](mcp.md).

### Shell completion

```bash
toskarctl completion zsh > "${fpath[1]}/_toskarctl"   # zsh
toskarctl completion bash > /etc/bash_completion.d/toskarctl
toskarctl completion fish > ~/.config/fish/completions/toskarctl.fish
```

Homebrew and the deb and rpm packages install the completion scripts for you.
