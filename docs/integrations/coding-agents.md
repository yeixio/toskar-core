# Claude Code and Codex with Toskar

[Claude Code](https://docs.anthropic.com/en/docs/claude-code) and [Codex CLI](https://github.com/openai/codex) are coding agents for the terminal. With Toskar as their model provider, they run on a model on your own computer, or on another computer at home, with no account or API key from Anthropic or OpenAI. The agent keeps its own tools, such as reading files and running commands. Toskar runs the model.

**Tested with** Toskar built from main ahead of 1.13.0, Claude Code 2.1.310, Codex CLI 0.138.0, and Qwen 3.5 9B (Q4_K_M), on macOS 27.2 with an Apple M5 Pro and 24 GB of memory. With Qwen 3.5, use Toskar 1.13.0 or later: 1.12.3 and earlier fail with "System message must be at the beginning" (see [If it doesn't work](#if-it-doesnt-work)).

## What you need

- Toskar running, with a chat model that calls tools. Qwen 3.5 9B, Toskar's recommendation for most computers with 16 GB or more, is the one tested here.
- Claude Code or Codex CLI, installed as their own docs say.
- `toskarctl`, which comes with Toskar ([Command line](../cli.md)).

The agents talk to Toskar in their own formats: Claude Code in Anthropic's Messages format (`/v1/messages`), and Codex in OpenAI's Responses format (`/v1/responses`). Both drive the model directly, which Toskar calls raw access ([API](../api.md)).

## Claude Code on this computer

1. In your project's folder, write Claude Code's settings for Toskar:

   ```sh
   toskarctl connect claude-code --write
   ```

   This writes `.claude/settings.local.json` in the folder, which Claude Code keeps out of git, and leaves the rest of the file as it was. Without `--write`, it prints the same settings for your shell instead:

   ```sh
   export ANTHROPIC_BASE_URL=http://127.0.0.1:7331
   export ANTHROPIC_AUTH_TOKEN=toskar-local
   export ANTHROPIC_MODEL=qwen3.5-9b-q4
   export ANTHROPIC_DEFAULT_OPUS_MODEL=qwen3.5-9b-q4
   export ANTHROPIC_DEFAULT_SONNET_MODEL=qwen3.5-9b-q4
   export ANTHROPIC_DEFAULT_HAIKU_MODEL=qwen3.5-9b-q4
   ```

   It picks the best installed model for agents. Add `--model <id>` to choose another. On this computer no key is needed, so `toskar-local` is a stand-in. Every model role points at the same model, so Claude Code's background work reaches Toskar too.

2. Run `claude` in the folder, and ask as you would.

Asked "Read tally.go and tally_test.go and tell me why the test fails. Don't change anything.", Claude Code read both files with its Read tool and answered, in 37 seconds with the model already loaded:

```text
The test fails because `Sum` skips the first element of the slice.

In `tally.go`, line 6, the loop starts at `i := 1`, which means `nums[0]` is never added to the total.

For the test input `[]int{2, 3, 4}`:
- Expected: 2 + 3 + 4 = **9**
- Actual: 3 + 4 = **7** (skipping `nums[0]` which is 2)

The fix would be to change `i := 1` to `i := 0`.
```

## Codex on this computer

1. Write Codex's settings for Toskar:

   ```sh
   toskarctl connect codex --write
   ```

   This adds a `toskar` provider to `~/.codex/config.toml` (or `$CODEX_HOME`), and a `toskar` profile beside it. Your default model stays as it was. The provider is:

   ```toml
   [model_providers.toskar]
   name = "Toskar"
   base_url = "http://127.0.0.1:7331/v1"
   env_key = "TOSKAR_API_KEY"
   wire_api = "responses"
   ```

   And the profile, `~/.codex/toskar.config.toml`:

   ```toml
   model = "qwen3.5-9b-q4"
   model_provider = "toskar"
   ```

2. Codex reads the key from `TOSKAR_API_KEY`. On this computer any value works:

   ```sh
   export TOSKAR_API_KEY=toskar-local
   ```

3. Run `codex --profile toskar`.

Asked the same question, Codex ran `cat tally.go` and `cat tally_test.go` in its read-only sandbox, and answered in 20 seconds that the loop starts at `i := 1`, so the 2 is never added and the sum is 7, not 9.

## Toskar on another computer

Use a model on a stronger computer, such as a desktop with a graphics card, from your laptop.

On the computer that runs Toskar:

1. Open API Access and turn on **Local network access**. It needs a key first, so make one.
2. Under the key's permissions, turn on **Drive models directly, for coding agents**. Only Admins can give it. Agents drive the model themselves, so they need it.

On the computer with the agent, set Toskar's address and the key, then connect as above:

```sh
export TOSKAR_URL=http://192.168.1.20:7331
export TOSKAR_API_KEY=<your key>
toskarctl connect claude-code --write
```

Printed settings name `$TOSKAR_API_KEY` rather than showing the key. With `--write`, Claude Code's settings hold the key itself, in `.claude/settings.local.json`, which stays out of git. Codex always reads it from the environment. Both agents were checked this way, from this Mac's own network address with a key, and answered as above.

## Subagents at once, with a swarm

Claude Code starts subagents for parts of a job, often several at a time. On one model on one computer they wait for each other. A profile's swarm spreads them over your computers and models instead (#1135):

1. In Profiles & Orchestration, give a profile the Swarm strategy and its models, ideally on more than one computer, or more than one model.
2. Point the subagents at it, by the profile's id or its name with dashes, such as `coding-swarm`:

   ```sh
   toskarctl connect claude-code --swarm coding-swarm --write
   ```

   This adds `CLAUDE_CODE_SUBAGENT_MODEL=swarm:coding-swarm`. Claude Code's own model, and its fast model for background requests, stay as they were.

Each subagent request brings Claude Code's tools, so it goes as it is to the least busy of the profile's computers and models, and its answer comes back unchanged. Two subagents started together run at the same time on different ones. Toskar's log names the computer and model for each.

Any agent can use `swarm:<profile>` as a model name in the same way, such as Codex's `model` in its `toskar` profile, or an OpenAI-compatible agent's model field with `swarm:` in place of `raw:`. Requests that bring tools are spread. A request without tools runs a whole swarm on its last message, which takes longer, and answers with the swarm's answer, with the agents' progress as its reasoning. That's why `--swarm` leaves the fast model alone: its requests, such as naming a chat, have no tools. On another computer, the key needs raw access for spread requests, and a whole swarm needs a Member or an Admin ([API](../api.md#swarms-from-editor-agents-1135)).

## Choosing a model

- **Give the model's id, not a profile or `auto`.** Agents send their own tools and instructions, and Toskar passes them straight to the model.
- **The model needs to call tools.** `toskarctl connect` picks one that does. Qwen 3.5 9B read the files in every try here, with both agents.
- **Long tool lists are narrowed.** Agents send many tools. When they would take more than a quarter of the model's window, Toskar offers the most relevant ones and lets the model find the rest with `tools_search` ([Command line](../cli.md#connect-a-coding-agent)).
- **The first answer waits for the model to load,** which takes longer on a computer with less memory. After that, answers here took 13 to 37 seconds.

## If it doesn't work

| What you see | What to do |
| --- | --- |
| `500` "System message must be at the beginning", after a few minutes of retries | Toskar 1.12.3 and earlier send Claude Code's mid-conversation system messages as they are, and Qwen 3.5 refuses them. Update to Toskar 1.13.0 or later |
| `401` "authorization required" | The model is on another computer: set `TOSKAR_API_KEY`, and connect again so the settings use it |
| `403` "this key can't drive a model directly" | Give the key **Drive models directly, for coding agents** in API Access |
| Claude Code prints `unrecognized_model` | It's only a warning: Claude Code doesn't know the model's name, and uses it anyway |
| The agent answers without reading the files | Ask it to read them by name, or use a larger model |
