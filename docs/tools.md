# Tools

Yggdrasil has one tool registry. Chat, profiles, and the Tools screen all use it. Built-in tools, connected services, and MCP tool sources all run through it. There is no second execution path.

## Architecture

`internal/tools/catalog.go` lists every built-in tool: id, capability, JSON argument shape, default permission, and whether it is read-only.

The registry in `internal/tools/registry.go` runs tools. `allow` runs immediately. `ask` waits for the in-chat approval. `deny`, or a tool missing from the profile, never runs. A globally disabled tool is rejected before a prompt.

Internet search and page open live in `internal/tools/internet`. `SearchProvider` and `PageFetcher` can be replaced without changing the chat loop. The default search provider reads DuckDuckGo HTML results. Opening a page returns title and text, with scripts, navigation, and very long pages trimmed.

The simple orchestrator waits for a model generation, classifies it, and only then shows text. A generation can contain assistant text, one tool call, or both. Tool calls, tool results, function-call syntax, and known model control tokens never become the assistant message. Ordinary JSON and code samples stay in the answer. The call is executed, the result goes back to the model, and the loop stops after 10 calls. Invalid tool JSON is not executed. The model may try again twice, then the turn ends with whatever readable text remains. A tool failure is a short status such as “Web search failed,” not a stack trace. “Used N tools” expands the calls without raw arguments.

## Permissions

| Policy | Behavior |
|--------|----------|
| allow | Run with no dialog. This is the default for every enabled tool. |
| ask | Confirm in chat. Set this in advanced mode when you want a prompt. |
| deny | The tool is not offered and cannot run. |

## Model compatibility

Tool support is one of native, compatible, limited, or unsupported. Catalog models with `tool_calling: true` are compatible: they use Yggdrasil's JSON tool format. Models with `tool_calling: false` are unsupported and do not receive tools. A catalog entry can set `tool_call_support` to limited when a model is known to emit tool syntax that does not parse. In Automatic mode, a live question prefers an installed native or compatible model over a limited or unsupported one. Choosing “This computer” does not change the model.

## Adding a built-in tool

1. Implement `Tool` and register it in `NewRegistry`.
2. Add a `Definition` in `BuiltinCatalog`.
3. Add a default policy on the relevant profile presets.
4. If the argument can be nonsense, reject it in `implausibleCall` before any prompt.
5. Mention it in the capability it belongs to.

## MCP tool sources

MCP servers add tools to the same registry. They are listed on the Tools screen with `source` `mcp:<source>`, and they follow the same policies. See [MCP](mcp.md).

## Diagnostics

Tool start, complete, failure, rejected parse, and protocol sanitation records stay in memory for the process and appear under Diagnostics → Tool activity. Summaries include the query or URL, not file contents or secrets. Automatic model changes are recorded as `chat.model_routed`.
