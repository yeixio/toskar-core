# Examples

Short programs that call Toskar Core with the official SDKs ([yeixio/toskar-sdks](https://github.com/yeixio/toskar-sdks)), and with curl through the OpenAI-compatible API.

They call this computer's Toskar Core, `http://127.0.0.1:7331`, which needs no key. To call another computer, set `TOSKAR_URL` and `TOSKAR_API_KEY` (an API key from Settings → API keys). Never commit a key. Chat needs an installed model; the events stream and pairing don't.

| | Python | JavaScript | Go |
| --- | --- | --- | --- |
| Stream an answer | [chat.py](python/chat.py) | [chat.mjs](javascript/chat.mjs) | [chat](go/chat/main.go) |
| Watch what happens | [events.py](python/events.py) | [events.mjs](javascript/events.mjs) | [events](go/events/main.go) |
| Connect as a device, at home or from anywhere | [pair.py](python/pair.py) | [pair.mjs](javascript/pair.mjs) | [pair](go/pair/main.go) |

The pairing examples connect the way the Toskar app does: with the code Connect a device shows, the computer's certificate pinned, and, with access from anywhere, through the computer's address record directly or through its relay ([docs/connecting.md](../docs/connecting.md)).

In CI, [ci/](ci/) has workflows that ask Toskar as a step, for GitHub Actions, GitLab CI, and Forgejo: label new issues, and explain a failed build from its log.

## Running them

**Python** 3.10 or newer:

```bash
pip install toskar
python examples/python/chat.py "Plan a three-day trip to Juneau"
```

Pairing from away from home also needs the `anywhere` extra: `pip install "toskar[anywhere]"`.

**JavaScript**, Node.js 18 or newer:

```bash
cd examples/javascript
npm install toskar
node chat.mjs "Plan a three-day trip to Juneau"
```

**Go** 1.24 or newer, from `examples/go`, a module of its own:

```bash
cd examples/go
go run ./chat "Plan a three-day trip to Juneau"
```

**curl**, through the OpenAI-compatible API, which existing OpenAI clients also use:

| | |
| --- | --- |
| [curl/models.sh](curl/models.sh) | `GET /v1/models`: auto, profiles as `profile:<id>`, and specialized AIs as `sai:<slug>` |
| [curl/chat.sh](curl/chat.sh) | `POST /v1/chat/completions`, streamed, ending with `data: [DONE]` (`TOSKAR_MODEL` picks the model; `auto` by default) |

## Keeping them working

`examples/test.py` runs every example against a throwaway Core and checks what it prints, on every pull request and each night (`.github/workflows/examples.yml`), and in toskar-sdks against each SDK change. Change an example and its check together:

```bash
python3 examples/test.py --core ./bin/toskar --sdks ../toskar-sdks --python ../toskar-sdks/python/.venv/bin/python
```

The full API is in [api/openapi.yaml](../api/openapi.yaml), and on [toskar.ai/developers](https://toskar.ai/developers). Known limits of the OpenAI-compatible API are in [docs/api.md](../docs/api.md).
