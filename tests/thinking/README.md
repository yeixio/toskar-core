# Thinking per family

Models that think before they answer mark and switch their thinking their
own way (#840):

| Family | How it thinks | How Toskar turns it off |
| --- | --- | --- |
| Qwen 3 | `<think>` in its template, with `enable_thinking` | `chat_template_kwargs.enable_thinking: false` |
| DeepSeek R1 distills | its base model's template; llama.cpp opens the `<think>` | `thinking_budget_tokens: 0` |
| gpt-oss | an analysis channel (`<\|channel\|>`) | `thinking_budget_tokens: 0` |

Toskar sends both for every model when thinking is off
(`internal/runtimes/llamacpp/client.go`). It knows a model thinks from its
file (`gguf.Inspect`): the chat template's marks, or the name for R1's
distills, whose templates don't say. A model that thinks gets more room for
its answer, and its chat gets the Thinking setting.

[thinking_test.go](thinking_test.go) runs a small model of each family
([families.json](families.json), pinned by commit and SHA-256) on a
llama.cpp release, started as the daemon starts it, and checks:

- its file says it thinks;
- with thinking on, the thinking comes apart from the answer, which is right
  and has no thinking tags;
- with thinking off, streamed and not, there is no thinking, the answer is
  right with no tags, and it takes fewer tokens.

## Measurements

On the home Mac (M5 Pro, Metal), 2026-10-28, asking 17 + 25:

| Family | Release | Thinking on | Thinking off |
| --- | --- | --- | --- |
| Qwen 3 0.6B | b11946 | 752 tokens (725 thinking) | 13 tokens |
| DeepSeek R1 distill 1.5B | b11946 | 250 tokens (91 thinking) | 22 tokens |
| gpt-oss 20B | b11946 | 49 tokens (38 thinking) | 11 tokens |

All three also pass on b11963. With the client's thinking-off fields
removed, every family fails: each thinks anyway, and Qwen 3's unstreamed
answer thought to the 2,048-token limit and never answered.

## Running it

```sh
CANARY_BUILD=macos-arm64 scripts/llama-canary.sh thinking
```

It downloads the newest release (or `CANARY_NEW_TAG`) and the three models,
about 14 GB, into `CANARY_CACHE`; gh must be signed in.
`TOSKAR_THINKING_REPORT` writes the results as Markdown. Without the
script's variables the test skips, so `go test ./...` only checks
families.json.

The [llama.cpp canary workflow](../../.github/workflows/llama-canary.yml)
runs it each night on the home Mac, after the canary. A failure opens the
canary's issue, or comments on it, but doesn't hold installs back: thinking
that won't turn off is slower, not wrong.
