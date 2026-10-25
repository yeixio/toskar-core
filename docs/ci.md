# Toskar in CI

A CI job can ask Toskar as one of its steps, such as summarizing a bug report, suggesting an issue's labels, or reading a failing log (#455). It runs on your own computers or runners and your own models, so issue text, code, and logs never go to a hosted AI, and there's no bill per token.

Three pieces fit together:

- **[`toskarctl ask`](cli.md#ask)** sends one prompt and prints the answer. Text piped in is data apart from the prompt, `--json-schema` answers with JSON a later step can parse, and memory, knowledge, and tools are off.
- **A key for a CI job**, made with **For a CI job** on the API Access page, only asks: no memories, knowledge, or tools, whatever the job sends ([API key permissions](api.md#openai-compatible-api)).
- **`install.sh ci`** runs Toskar on the runner itself, for a runner with no Toskar nearby.

In GitHub Actions, the [Ask Toskar action](https://github.com/yeixio/toskar-action) puts these together in one step, connected to a Toskar or started on the runner. The rest of this page is for other CI systems, and for jobs that want the pieces themselves.

## A Toskar the runner can reach

On a self-hosted runner beside a computer running Toskar, or with Toskar reachable over the network, set its address and a CI key as the job's secrets, and ask:

```yaml
- run: gh issue view "$ISSUE" | toskarctl ask "Summarize this bug in two sentences"
  env:
    TOSKAR_URL: ${{ secrets.TOSKAR_URL }}
    TOSKAR_API_KEY: ${{ secrets.TOSKAR_API_KEY }}
```

`toskarctl` is in every Toskar install. On a runner without one, `install.sh ci --no-start` puts it on PATH, without starting a Toskar:

```sh
curl -fsSL https://github.com/yeixio/toskar-core/releases/latest/download/install.sh | sh -s -- ci --no-start
```

## Toskar on the runner

`install.sh ci` installs the release's headless archive in a folder (`~/.toskar-ci` unless `--dir` says otherwise), with no root and no service. It starts Toskar there in the background, on port 17331 (`--port`) so it never meets a Toskar the runner already has, then installs llama.cpp and the model and waits for them:

```sh
curl -fsSL https://github.com/yeixio/toskar-core/releases/latest/download/install.sh | sh -s -- ci --model llama-3.2-1b-q4
```

In GitHub Actions it sets `TOSKAR_URL` and puts `toskarctl` on PATH for the job's next steps. Elsewhere it prints the two `export` lines to run. Toskar's log is in the folder, as `toskar.log`.

The model is a download the first time, about 0.8 GB for `llama-3.2-1b-q4`. Cache the folder's `data/models` and `data/runtimes` between runs to skip it. GitHub's hosted runners run small models, about 1 to 4 billion parameters, on the CPU, much slower than on a computer with a graphics card. Time a run with your own prompts, and give `toskarctl ask` a `--timeout` to match.

**GitHub Actions**

```yaml
- uses: actions/cache@v6
  with:
    path: |
      ~/.toskar-ci/data/models
      ~/.toskar-ci/data/runtimes
    key: toskar-${{ runner.os }}-llama-3.2-1b-q4
- run: curl -fsSL https://github.com/yeixio/toskar-core/releases/latest/download/install.sh | sh -s -- ci --model llama-3.2-1b-q4
- run: gh issue view "$ISSUE" | toskarctl ask --timeout 10m "Summarize this bug in two sentences"
```

**GitLab CI**

```yaml
summarize:
  cache:
    key: toskar-llama-3.2-1b-q4
    paths: [.toskar-ci/data/models, .toskar-ci/data/runtimes]
  script:
    - curl -fsSL https://github.com/yeixio/toskar-core/releases/latest/download/install.sh | sh -s -- ci --dir "$CI_PROJECT_DIR/.toskar-ci" --model llama-3.2-1b-q4 > toskar.env
    - eval "$(grep '^export ' toskar.env)"
    - toskarctl ask --file build.log "What's the first real error, and its likely cause?"
```

**Forgejo and Gitea Actions** take the GitHub Actions steps as they are.

Whole workflows, for issue triage and explaining a failed build, are in [examples/ci](../examples/ci/), for GitHub Actions, GitLab CI, and Forgejo.

## Untrusted input

An issue's text, a review, or a log is written by someone else. Keep it in the data, apart from the prompt, as `toskarctl ask` does with what's piped in, and give the job a CI key, so an instruction inside the text can't reach anyone's memories or make a tool act. When a job writes back, such as labels or a comment, ask for structured output with `--json-schema`, check the answer against the labels you allow, and write it in a separate step, never from the answer's free text.
