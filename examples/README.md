# Examples

Short programs that call Toskar Core with the official SDKs ([yeixio/toskar-sdks](https://github.com/yeixio/toskar-sdks)), and with curl through the OpenAI-compatible API.

They call this computer's Toskar Core, `http://127.0.0.1:7331`, which needs no key. To call another computer, set `TOSKAR_URL` and `TOSKAR_API_KEY` (an API key from Settings → API keys). Never commit a key. Chat needs an installed model; the events stream and pairing don't.

| | Python | JavaScript | Go |
| --- | --- | --- | --- |
| Stream an answer | [chat.py](python/chat.py) | [chat.mjs](javascript/chat.mjs) | [chat](go/chat/main.go) |
| Watch what happens | [events.py](python/events.py) | [events.mjs](javascript/events.mjs) | [events](go/events/main.go) |
| Connect as a device, at home or from anywhere | [pair.py](python/pair.py) | [pair.mjs](javascript/pair.mjs) | [pair](go/pair/main.go) |

The pairing examples connect the way the Toskar app does: with the code Connect a device shows, the computer's certificate pinned, and, with access from anywhere, through the computer's address record directly or through its relay ([docs/connecting.md](../docs/connecting.md)).

## Running them

**Python** 3.10 or newer:

```bash
pip install "git+https://github.com/yeixio/toskar-sdks#subdirectory=python"
python examples/python/chat.py "Plan a three-day trip to Juneau"
```

Pairing from away from home also needs the `anywhere` extra: `pip install "toskar[anywhere] @ git+https://github.com/yeixio/toskar-sdks#subdirectory=python"`.

**JavaScript**, Node.js 18 or newer. Until the package is on npm, build it and link it:

```bash
git clone https://github.com/yeixio/toskar-sdks
(cd toskar-sdks/typescript && npm install && npm run build && npm link)
cd examples/javascript && npm link toskar
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

The full API is in [api/openapi.yaml](../api/openapi.yaml), and on [toskar.ai/developers](https://toskar.ai/developers). Known limits of the OpenAI-compatible API are in [docs/api.md](../docs/api.md).
