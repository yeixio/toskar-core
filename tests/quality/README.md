# Quality test set

A fixed set of representative requests, each with the behavior it must have
(spec §64). Routing, retrieval, planning, verification, approvals, and history
all change answers; these cases check that changes keep the behavior people
rely on.

`cases.json` holds the cases. Each has a `message` (and optional `history`
and `setup`: knowledge files and tool policies), what it checks (`what`), the
`platforms` it runs on (`core`, `phone`, or both; core when absent), and
`expect`:

| Check | Meaning |
| --- | --- |
| `effort` | The run's effort, such as `Fast` |
| `plan`, `min_workers` | Whether it was worked through in parts, and how many |
| `lookup` | Whether the web was looked up first |
| `no_tools_run`, `not_run` | No tool, or none of these tools, ran |
| `approval_for` | These tools asked first (stub only: a real model may not try) |
| `sources` | The answer cites these kinds, such as `knowledge` or `web` |
| `verified`, `notice_matches` | The answer was checked; the notice says this |
| `steps_match` | A "What I did" step matches |
| `prompt_contains` | The model was sent this text (stub only) |
| `answer_matches` | The answer matches; `answer_real_only` skips it for the stub |
| `no_false_claims` | An answer that claims a change nothing made carries a notice |
| `no_destructive_advice` | The answer does not hand the person a command that deletes or wipes files (`rm -rf`, `del /s`, `mkfs`…) to run themselves |
| `no_deflection` | The answer does not send the person off to search, or claim to browse, by `deflection`, instead of answering; with the facts `answer_matches` asks for, a pointer to a site for more is fine (real models only) |

`stub` scripts what the stub model says, one reply per model call. `stub_only`
cases check scripted behavior a real model may not produce. `fixtures` cases
need the in-process run's stand-in image and video tools, which save a tiny
picture or clip at once; they run with the stub and a served model, not
against a daemon. `tools_run` lists tools that must run.

`web.json` holds the web pages every run reads instead of the live web: a
search gets the results of the first site whose `match` matches it, and a page
is found by its URL. Its facts are fixtures, so an answer that matches them
came from the page, not from the model's memory.

The iPhone app (yeixio/toskar-apps, `mobile/quality`) keeps a copy of both
files and runs the `phone` cases through its on-device chat with the same
expectations and pages, so the phone is held to what core does. Its
quality run checks out this folder and fails when its copies differ.

`text.json` holds inputs and core's outputs for the text helpers the phone
keeps its own copy of: whether a message needs the web, how it is sorted,
small talk, deflections, search queries, and memory commands.
`TestTextVectors` (in `internal/orchestrator/builtin/simple`) holds core to
it, and the phone's run holds the phone to it. After changing one of those
helpers, rewrite the outputs and port the change to the phone:

```sh
TOSKAR_UPDATE_TEXT_VECTORS=1 go test ./internal/orchestrator/builtin/simple -run TestTextVectors
```

`topics.json` holds the topic controls' cases (#345): a tire shop's
assistant with Enforce, on-topic questions and small talk it must answer,
and off-topic ones it must hold, jailbreaks included: "ignore your
instructions", role-play, "my boss said it's OK", a claimed developer mode,
other languages, instructions in a pasted document, a task wrapped in the
topic, and a long run of small steps away from it. `topic_held` checks the
run trace: held before answering, or the answer replaced with the set reply.
They run without web tools, since `web.json` has no tire pages. Core only:
the phone has no topic controls, so its copies don't include them.
`TestTopicQuality` reports the share of off-topic cases held and of on-topic
ones wrongly refused; the stub must get every case right, and a real model
must hold at least 80% (`TOSKAR_QUALITY_TOPIC_MIN_HELD`) and wrongly refuse
at most 15% (`TOSKAR_QUALITY_TOPIC_MAX_REFUSED`). `TOSKAR_QUALITY_TOPIC_REPORT`
writes its report. The weekly real-model run includes it. A model that passes is tagged `topic-check` in `manifests/models/catalog.json`, which makes it a candidate to run the topic checks (#457). Against a daemon with a tagged model installed, the rates are a gate, since that model runs the checks. With none installed, the answering model checks itself: the rates are measured and reported, which is how a model earns the tag, but don't fail the run. gemma-2-9b, qwen2.5-14b, and qwen2.5-32b earned it in #510's runs; qwen2.5-7b, which wrongly refused 2 or 3 of 9, didn't.

`reasoning.json` holds questions with one right answer (#459,
[deliberate.md](../../docs/deliberate.md)): math word problems, logic
puzzles, two-step facts, and trick questions, each with a `kind`. A case's
`answer` must match the final answer (after the reply's last "Final
answer", or its last line), and when the reply also matches `wrong`, the
tempting mistake, whichever comes first decides. Every question asks for a
`Final answer:` line, the same way in every mode, and runs without web
tools. `TestReasoningCases` checks the set: each `solution` must score
right. `TestReasoningQuality` measures a model, by kind: how many it got
right, and the seconds and tokens a question took. A real model isn't held
to a pass rate; the numbers are the baseline Deliberate is compared
against. The stub runs one case of each kind, to check the plumbing.
`TOSKAR_QUALITY_DELIBERATE` is the mode measured: `single`, or a
Deliberate setting such as `always`, which each case's profile gets (real
models only; the stub measures `single`). The Quality workflow's
**Run workflow** has a `deliberate` input for it; the weekly run measures
`single`. `TOSKAR_QUALITY_REASONING_REPORT` writes a Markdown
report, and `TOSKAR_QUALITY_REASONING_JSON` the results. The weekly
real-model run includes it.

`files.json` is the files set (#510): every type Toskar reads, attached to
a message and asked about, and every type it makes, asked for and then
opened. A case's `attach` lists files sent with its message: `make` builds
one with Toskar's own writers (`pdf`, `docx`, and `pptx` from Markdown, a
deck's slides split by lines of `---`; `xlsx` from CSV), or `content` is the
file as it is. It's uploaded the way the apps upload, after the same check.
`file_text` names, by extension, what a file the assistant made must hold:
the file is opened the way Toskar reads an attachment, not only named.
`TestFileCasesCover` checks that every attachment builds and reads, and that
every readable and writable type has a case. `TestFilesQuality` runs the set:
the stub must pass every case (the file reached the model, and a made file
holds what was asked), and a real model the same share as the chat set.
`TOSKAR_QUALITY_FILES_REPORT` writes its report, and the weekly real-model
run includes it.

`media.json` is the media set (#510): pictures, edits, video, and speech
Toskar makes, and pictures it reads. A case's `judge` checks the file the
turn made: a picture must decode, with each side at least `min_side` on a
real daemon; a WAV must be whole and at least half a second long. Against a
real daemon, a model that sees (`TOSKAR_QUALITY_JUDGE_MODEL`, gemma-3-4b by
default) is then asked whether the picture, or the clip's sampled frames,
shows `shows`, and speech is transcribed back and compared with `says` by
word error rate (`max_wer`, 25% by default). `moves` asks whether a
clip's frames change, so a frozen clip fails, and `clean` whether any part
of a picture, or any frame, is garbled or corrupted (blocks of wrong color,
noise, stripes, smears) or a frame flashes or jumps apart from its
neighbors, so a broken decode fails even when it shows the right thing
(#553). `os` runs a case only on that system: `make_video_mac_decode` is
the regression case for Apple's video decode path, run with the media set
on a Mac. `make: png` draws an
attachment from a description, such as `size=512 bg=white shape=square
color=blue count=3`, for edits and for the reading cases, whose
`image_sent` checks (stub only) that the picture reached the model.
Pictures for the vision cases (#449) are drawn too, so they're our own and
the same every run (`pictures_test.go`): `make: text` writes lines split by
`|` in a pixel font (`w=640 h=360 bg=green color=white text=MAIN ST|EXIT
12`), `make: bars` a bar chart (`labels=MON,TUE values=4,9`), and `make:
dialog` an error dialog (`title=ERROR text=DISK IS FULL`). `make: file`
attaches a file kept in the repository, such as the clip in `fixtures/`,
which `fixtures/make-fixtures.sh` draws with ffmpeg. `follow_up` is a
second message in the same chat with nothing attached; the case checks its
answer, and `image_sent` checks the picture still reached the model then.
`setup.no_vision` runs a case with no model that can see pictures: the stub
gets no seeing stand-in, and a daemon with a vision model installed skips
it. Its `cant_see` checks the picture was answered honestly, with an offer
to set up a model that sees or the notice that the answer didn't look at
it (`picture_without_a_model_that_sees` in `cases.json`, which the CPU run
on Llama 3.2 1B covers).
The report counts the cases about what the model sees, the ones that check
`image_sent` or show a video, in a **Sees: N of M** line.
`real_only` cases need tools the stub has no stand-in for, and `recording`
attaches text read by Toskar's own voice. The stub must pass every case it
runs; a real daemon at least 75% (`TOSKAR_QUALITY_MEDIA_MIN_PASS`), since
another model does the judging. `TOSKAR_QUALITY_MEDIA_INSTALL` sets up
picture and video making, the judge, and speech first. The weekly run
includes it once, with the recommended models.
`TOSKAR_QUALITY_MEDIA_IMAGE_MODEL` (the workflow's `image_model` input)
tests another picture model or editor, such as one marked untested (#548);
FLUX.2 [klein] 4B is still set up, for edits and, beside an editor, for new
pictures. `TOSKAR_QUALITY_MEDIA_VIDEO_MODEL` (`video_model`) does the same for
clips, with Wan 2.2 TI2V 5B kept for clips from pictures. A model that needs
a license (#551) is tested with `TOSKAR_QUALITY_HF_TOKEN`, the token of a
Hugging Face account that accepted it (the workflow's `HF_TOKEN` secret);
the set confirms the license for that run.

## Running it

- **Stub model:** `make quality`, also part of `go test ./...` in CI. It runs
  in-process with the web replaced by fixed pages, so it needs no model and no
  network.
- **A real model, as a release does:** `scripts/quality-model.sh run` downloads
  llama.cpp (`LLAMA_CPP_TAG`, with `gh`) and Llama 3.2 1B
  (`QUALITY_MODEL_GGUF_URL`), starts llama-server, and runs the cases in-process
  with every model call sent to it (`TOSKAR_QUALITY_MODEL_URL`) and the web
  answered from `web.json`. At least 90% of cases must pass
  (`TOSKAR_QUALITY_MIN_PASS`); `TOSKAR_QUALITY_REPORT` writes every answer to a
  Markdown report. The Release workflow runs this before it publishes, and the
  Quality workflow runs it on pull requests that change how chat decides.
- **Real models on a daemon:** start a daemon with models installed, and with
  `TOSKAR_WEB_FIXTURES=tests/quality/web.json` so its web tools read the
  pages above instead of the internet (it logs a warning when they do), then run
  `make quality-real`, setting `TOSKAR_QUALITY_URL` if the daemon is not at
  `http://127.0.0.1:7331`, and `TOSKAR_QUALITY_KEY` if it needs an API key.
  `TOSKAR_QUALITY_INSTALL=recommended` first installs the models the daemon
  recommends for its hardware. Each case adds a profile and knowledge, which are removed afterwards, and a
  chat, which is kept so a failure can be read.
- **On a schedule or by hand:** `.github/workflows/quality.yml` runs the
  real-model set weekly on a self-hosted runner labelled `toskar-models`, when
  the repository variable `QUALITY_RUNNER` is `docker`. It builds the daemon
  image from the commit under test, starts it with a throwaway data folder and
  a key made for the run, and runs the tests in a Go container beside it, all
  under the runner's rootless Docker. Models and runtimes stay in the Docker
  volumes `toskar-quality-models` and `toskar-quality-runtimes`, so only the
  first run downloads them. **Run workflow** takes a `ref` to test a branch
  before it merges; the workflow itself always comes from main. Extra
  `docker run` flags for the daemon, such as GPU access, go in the variable
  `QUALITY_DOCKER_FLAGS`. The daemon is held to `QUALITY_MEMORY` (default
  `32g`, no swap) and `QUALITY_CPUS` (default three quarters of the cores),
  and picks its models to fit; each run first removes containers an
  interrupted run left behind. `QUALITY_GPU=vulkan` builds the image with
  Vulkan and Mesa's drivers (the Dockerfile's `gpu` target) and passes in the AMD or Intel
  card (`--device /dev/dri`); the runner's user must be able to open
  `/dev/dri/renderD*`. The report names the card or CPU the run used.
  The job runs once per model in `QUALITY_MODELS` (a JSON list; the
  **Run workflow** `models` input overrides it), one at a time:
  `recommended` is what the daemon picks for a new user, and a catalog id
  such as `qwen2.5-32b-q4` tests that model (`TOSKAR_QUALITY_MODEL`), which
  is installed first and asked for by every chat.

Add a case when a change fixes a behavior, so it stays fixed.
