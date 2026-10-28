# llama.cpp canary

Toskar installs the newest llama.cpp release that fits the computer. A bug in
one backend (Metal, Vulkan, the CPU) reaches people as worse answers, with no
error. The canary checks each night that the newest release computes what a
known-good tag does, on the backends Toskar installs (#833).

`llama-perplexity`, which ships in every release archive, scores
[text.txt](text.txt) with a small fixed model, Llama 3.2 1B Q4_K_M. The text
is about 11,700 tokens of Toskar's own docs, Go and TypeScript code, and
German, Japanese and Chinese strings. It scores the text in 1,024-token
chunks.

The good tag's next-token probabilities are the base. Each run checks:

| Check | Against | Tolerance |
| --- | --- | --- |
| Golden | The good tag here scores the perplexity it did when the golden was made. A miss means the machine changed, such as a new graphics driver. | `release.ppl` |
| Release | The new release on the same backend: mean KL divergence, how often the top token is the same, and the perplexity change | `release` |
| CPU | On a GPU lane, the new release on the CPU against the good tag on the GPU | `cpu` |

[goldens.json](goldens.json) holds the good tag, the model (by commit and
SHA-256), the tolerances, and each lane's golden perplexity. A lane is a
backend on one kind of hardware, and can have its own tolerances.

## Measurements

b11946 against b11174 on the home runners (2026-10-28):

| Lane | Run | Mean KLD | Same top | PPL change |
| --- | --- | --- | --- | --- |
| Metal (M5 Pro) | b11174 against itself | 0.000000 | 100% | +0.086% |
| Metal | b11946 | 0.000002 | 99.95% | +0.087% |
| Metal | b11946 on the CPU against b11174 on Metal | 0.0025 | 97.49% | +0.299% |
| CPU (M5 Pro) | b11946 | 0.000000 | 99.98% | +0.087% |
| Vulkan (RX 7900 XTX) | b11946 | 0.0008 | 98.33% | +0.176% |
| Vulkan | b11946 on the CPU against b11174 on Vulkan | 0.0026 | 97.44% | +0.415% |
| CPU (Ryzen, x64) | b11946 | 0.0028 | 97.24% | +0.167% |
| Metal | b11946, 8-bit KV cache | 0.0002 | 99.22% | +0.105% |
| Metal | b11946, 4-bit KV cache | 0.067 | 86.50% | +5.69% |

The perplexity of a run against its own base is about 0.09% higher because
the base's probabilities are saved compressed. A 4-bit KV cache stands in
for a broken kernel: it fails all three gates, and the 8-bit cache passes.

A release can change how a kernel rounds without being wrong: b11946 on the
x64 CPU differs from b11174 about as much as a GPU differs from the CPU.
The release tolerance (mean KLD 0.005, 96% same top, 0.5% perplexity)
leaves room for that, and is still a thirteenth of the 4-bit cache's KL
divergence. The CPU tolerance (0.01, 95%, 1%) is looser, since a GPU and the
CPU round differently on every release.

## Running it

[scripts/llama-canary.sh](../../scripts/llama-canary.sh) downloads the
release builds (gh must be signed in) and the model, then runs the test:

```sh
CANARY_BUILD=macos-arm64 CANARY_LANE=metal-arm64 CANARY_GPU=1 scripts/llama-canary.sh run
```

`CANARY_NEW_TAG` picks a release; the newest is the default.
`TOSKAR_CANARY_REPORT` writes the measurements as Markdown. Without the
script's variables the test skips, so `go test ./...` runs only the parser's
unit tests. A run writes the good tag's probabilities, about 1.4 GB, and
removes them when it ends.

The [llama.cpp canary workflow](../../.github/workflows/llama-canary.yml)
runs it each night on the home runners: Metal and the CPU on the Mac, Vulkan
(the AMD card, in Debian 13 with Mesa, as the daemon's GPU image) and the CPU
on Linux. A failure opens an issue with the numbers, or comments on the open
one.

## Changing the goldens

Goldens change only through this script and a reviewed pull request. To
measure a lane on the good tag:

```sh
CANARY_BUILD=macos-arm64 CANARY_LANE=metal-arm64 CANARY_GPU=1 scripts/llama-canary.sh refresh
```

It writes the lane's perplexity into goldens.json. For the home runners,
start the workflow by hand with "refresh": the run's summary shows the
change to commit. To move the good tag, change `good_tag` and refresh every
lane in the same pull request.
