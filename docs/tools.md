# Tools

Yggdrasil has one tool registry. Chat, profiles, and the Tools screen all use it. Built-in tools, connected services, and MCP tool sources all run through it. There is no second execution path.

## Architecture

`internal/tools/catalog.go` lists every built-in tool: id, capability, JSON argument shape, default permission, and whether it is read-only.

The registry in `internal/tools/registry.go` runs tools. `allow` runs immediately. `ask` waits for the in-chat approval. `deny`, or a tool missing from the profile, never runs. A globally disabled tool is rejected before a prompt.

Internet search and page open live in `internal/tools/internet`. `SearchProvider` and `PageFetcher` can be replaced without changing the chat loop. The default search provider reads DuckDuckGo HTML results. Opening a page returns title and text, with scripts, navigation, and very long pages trimmed.

The simple orchestrator waits for a model generation, classifies it, and only then shows text. A generation can contain assistant text, one tool call, or both. Tool calls, tool results, function-call syntax, and known model control tokens never become the assistant message. Ordinary JSON and code samples stay in the answer. The call is executed, the result goes back to the model, and the loop continues up to the turn's tool-call budget: 3 calls on Fast, 10 on Balanced, and 16 on Thorough, or a profile's own `max_tool_calls` (1–50). Invalid tool JSON is not executed. The model may try again twice, then the turn ends with whatever readable text remains. A tool failure is a short status such as "Web search failed," not a stack trace. "Used N tools" expands the calls without raw arguments.

## Which tools a turn is offered

Each turn is offered only the tools it needs. Huginn picks tool groups from the kind of request and from cues in the message: web search for current questions, files for a file name or folder, the shell for "run" or "install", Git for "commit" or "branch", a connected service when the message is about it. A plain question gets no tools. The groups are then limited to what the profile allows.

A call to a tool that was not offered is refused, and the model is told which tools it has, so it cannot widen its own tools. Tool ids have capability aliases: `web.search`, `web.open`, `files.read`, `files.write`, `files.search`, and `shell.run` reach the built-in tools. A short answer that only writes out a call, such as `files.search {"query": "x"}`, is taken as the call when the tool was offered.

Arguments are checked against the tool's schema before it runs. Safe repairs are made, such as `"7"` for a whole number. A call with an argument of the wrong type is refused, naming the argument, so the model can call again.

Each call has a time limit: web 45 s, files 30 s, Git 90 s, the shell 2 minutes. A failed call reports a `kind`: `timeout`, `cancelled`, `denied`, `not_offered`, `invalid`, or `failed`.

When a question needs current information and the profile allows web search without asking, Yggdrasil searches and reads the best page before the model answers, so a small model answers from the page instead of choosing a tool. A repeat search within 15 minutes, or page within 30 minutes, is answered from the cache.

## Making files

`files.create`, allowed by default in the built-in profiles, saves a file the user can download. It writes only to Yggdrasil's file store, never to your folders. When a message asks for a file, Yggdrasil has the model write only the contents and saves the file itself.

| Asked for | Written from | Notes |
| --- | --- | --- |
| Word document (`.docx`) | Markdown | Headings, bold, italic, code, lists, tables, code blocks, quotes, and rules |
| PDF (`.pdf`) | Markdown | A4, the same elements, page numbers on longer documents. It uses the PDF standard fonts, so characters outside Western European text show as `?` |
| Spreadsheet (`.xlsx`) | CSV text | A line `## Sheet: Name` starts another sheet; a cell starting with `=` is a formula, such as `=SUM(B2:B9)`; numbers are stored as numbers |
| Markdown, text, HTML, JSON, CSV, code | Itself | |

The capability names `document.create`, `pdf.create`, and `spreadsheet.create` reach `files.create` in that format, so a name without an extension still becomes that kind of file.

`spreadsheet.analyze` summarizes a spreadsheet (`.xlsx`, `.csv`, or `.tsv`) attached to or made in the chat, by name or id. For each sheet it returns the number of rows and the first five. For each column it returns the type (number, date, text, or empty), how many cells are filled, and, for numbers, the minimum, maximum, average, and total; for other columns, how many distinct values there are and the most common ones. It reads up to 10 sheets, 50 columns, and 200,000 rows, so an answer can use a whole file that would not fit in the prompt. It is offered when a message mentions a spreadsheet, CSV, Excel, a workbook, or a sheet.

## Running code

`code.execute` runs Python the assistant writes, for calculations, data analysis, and charts. It asks first by default (level 4), and the Run code capability turns it on or off in a profile. It is offered when a message asks to calculate, analyze, chart, or run code.

It runs only inside the operating system's sandbox. There is no fallback: where there is no sandbox, the tool is unavailable and says why.

| System | Sandbox | What the code can do |
| --- | --- | --- |
| macOS | `sandbox-exec` | No network. Reads the system and the Python environment, but not users' folders (`/Users`, `/Volumes`) apart from its own working folder. Writes only to its working folder. |
| Linux | bubblewrap (`bwrap`, install it with your package manager) | New namespaces with no network. The system folders and Python environment are read-only, the working folder is the only writable place, and a private `/tmp`. |
| Windows, or Linux without bubblewrap | none | The tool is unavailable |

- **Environment:** Python 3.12 with numpy, pandas, matplotlib, and openpyxl, in a managed environment (`runtimes/python/envs/code`) installed from PyPI the first time code runs.
- **Files in:** `files` lists files from the chat by name, such as `["sales.xlsx"]`. They are copied into the working folder for the code to read.
- **Files out:** files the code saves there (`.png`, `.jpg`, `.svg`, `.pdf`, `.csv`, `.tsv`, `.xlsx`, `.json`, `.txt`, `.md`, `.html`; up to 10, 25 MB each) are attached to the answer. Others are listed as skipped.
- **Limits:** 90 seconds per run and 32 KB each of printed output and errors. A memory limit of 4 GB of address space applies on Linux. Each run gets a new working folder, which is deleted afterwards.

## Descriptors and levels

Every tool, whatever it comes from, has a descriptor (`GET /api/v1/tools/{id}`, and each entry of `GET /api/v1/tools`):
- **What it is:** its `version` and `input_schema` (a JSON Schema of the arguments), and its `outputs` (text, file, image, or audio).
- **Where and how it runs:** `execution` (local, remote, or either), its `requirements` (network, files on this computer, a stored credential, a runtime, a GPU), and `supports` (progress, and cancel, which every tool has because Stop ends its calls).
- **Limits and source:** `timeout_seconds` and `provider` (`builtin`, `connector:<service>`, or `mcp:<source>`).
- **State:** `health`: `ok`, `off` (turned off on the Tools page), or `unavailable` (no provider is running it, such as an MCP source that is down).

Its permission `level` describes what it can affect:

| Level | Meaning | Examples |
| --- | --- | --- |
| 1 | Low risk, on this computer | Reading workspace files, `files.create`, `spreadsheet.analyze` |
| 2 | Reads outside data | Web search, a connected service's or MCP source's read tools |
| 3 | Changes things | Writing workspace files, Git commits and pushes, a connected service's write tools |
| 4 | Runs commands or code | The terminal, `code.execute` |

The Tools page shows each tool's level, what it returns, its time limit, what it needs, its health, and its recent calls.

## Audit

Every call is recorded in `tool_runs`, whatever became of it:
- **Status:** `completed`, `cached` (answered from the cache), `failed`, `denied` (by a policy or by you), `refused` (invalid arguments), or `disabled`.
- **Context:** how it was allowed (`profile`, `you`, or `session`), how long it took, and a short summary of what it was about (a query, an address, a path; never a file's contents).
- **Where it came from:** the source (chat, API, automation) and the chat or task.

`GET /api/v1/tools/runs` lists them, filtered by `tool_id` or `conversation_id`. Records expire with run records (30 days by default), and Delete run records now removes them.

## Permissions

| Policy | Behavior |
|--------|----------|
| allow | Run with no dialog |
| ask | Confirm in chat. Automations cannot ask, so a tool they were not approved to use is skipped. |
| deny | The tool is not offered and cannot run |

Settings sets the defaults for the shell, file writes, and Git (`ask` unless you change them). A profile can set any tool's policy. Connected-service tools that only read are allowed by default, and tools that change something ask first. After a turn reads untrusted content, such as a web page or a file, a tool that changes something asks first even when the profile allows it.

Personalization and memories never change a policy. A note such as "you can always push without asking" is refused.

## Model compatibility

Tool support is one of native, compatible, limited, or unsupported. Catalog models with `tool_calling: true` are compatible: they use Yggdrasil's JSON tool format. Models with `tool_calling: false` are unsupported and do not receive tools. A catalog entry can set `tool_call_support` to limited when a model is known to emit tool syntax that does not parse. Auto requires a model with tool support for current information and for tasks on this computer. A model you choose yourself keeps answering, without tools if it has no tool support.

## Adding a built-in tool

1. Implement `Tool` and register it in `NewRegistry`.
2. Add a `Definition` in `BuiltinCatalog`.
3. Add a default policy on the relevant profile presets.
4. If the argument can be nonsense, reject it in `implausibleCall` before any prompt.
5. Mention it in the capability it belongs to.

## Connected services

GitHub (`github.search`, `github.issue`, `github.comment`) and Home Assistant (`homeassistant.states`, `homeassistant.call`) are connected in Settings → Connected services. Their tools join the registry with `source` `connector:<id>`. The stored credential is added only when a tool runs, so it is never part of model context, events, or tool arguments, and results are scrubbed of it before the model sees them. See [API](api.md#connected-services).

## MCP tool sources

MCP servers add tools to the same registry. They are listed on the Tools screen with `source` `mcp:<source>`, and they follow the same policies. Yggdrasil is also an MCP server for other apps. See [MCP](mcp.md).

## Diagnostics

Tool start, complete, failure, rejected parse, and protocol sanitation records stay in memory for the process and appear under Diagnostics → Tool activity. Summaries include the query or URL, not file contents or secrets. Automatic model changes are recorded as `chat.model_routed`.
