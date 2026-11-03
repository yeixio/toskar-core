# How Toskar measures quality

This page explains how the quality results are measured, so you can judge them and tell us what's wrong with them. The results are in [toskar-model-data](https://github.com/yeixio/toskar-model-data/blob/main/quality/results.json). They're measurements, not a ranking: we don't combine them into one score.

Every case, check, and threshold is public, in [`tests/quality`](../tests/quality). Its [README](../tests/quality/README.md) has the full detail.

**See something wrong or missing? [Suggest a change](https://github.com/yeixio/toskar-core/issues/new?template=quality_suggestion.yml).** A case we should add, a check that's unfair, or a better way to score are all welcome, and so is a pull request.

**Have a computer we don't? [Run it and send us your results](#add-your-computers-results).** We only have a couple of computers, and every result helps people with hardware like yours.

## What's measured

Each set is a fixed list of requests, each with what it must do. The model answers through Toskar, the same way it does in a chat, so these measure a model working in Toskar, not the model alone.

| Set | What it asks | How it's checked | Cases |
|---|---|---|---|
| **Quality** | Everyday requests: tools, web lookups, approvals, and languages | Rules on what happened: which tools ran, what the answer says, and whether it claims something it didn't do | [`cases.json`](../tests/quality/cases.json) |
| **Topics** | A shop's assistant asked off-topic questions and jailbreaks, and on-topic ones | Whether it held what it should, and wrongly refused what it shouldn't | [`topics.json`](../tests/quality/topics.json) |
| **Files** | Reading every file type Toskar reads, and making every one it makes | Whether the file reached the model, and whether a made file, once opened, holds what was asked | [`files.json`](../tests/quality/files.json) |
| **Reasoning** | Math, logic, and trick questions with one right answer | The final answer matches, and isn't the tempting mistake | [`reasoning.json`](../tests/quality/reasoning.json) |
| **Triage** | Real issues from this project | The labels suggested against the ones maintainers gave | [`triage.json`](../tests/quality/triage.json) |
| **Media** | Making pictures, clips, and speech, and reading pictures | File checks, then another model judges, as below | [`media.json`](../tests/quality/media.json) |

A results row can show fewer cases than the file holds, since some don't apply to every model. A model that sees pictures skips the case about having no model that sees them, for example.

Web pages are fixed copies, so an answer that has the right facts got them from the page, and every run reads the same thing.

## How it's scored

- **Each set's score is cases passed, out of cases run.** There's no weighting.
- **Only complete results are published.** A row has all of Quality, Topics, Files, and Reasoning, or says the model doesn't run on that computer. Other sets, such as Media and Triage, are measured for our own checks and stay out of the results.
- **A row's Score is the plain average of the pass rates of those four,** from 0 to 100, shown with the method's version, such as 95 (v1). Speed stays beside it and not in it, since a fast wrong answer shouldn't beat a slow right one. It's a score for these sets and not a verdict on the best model: they're easy enough that good models bunch near the top, and a small model can pass a large one.
- **A model's answers vary from run to run.** Run more than once, a set reports its pass rate with a 95% interval (Wilson) and which cases were flaky.
- **A set only fails when the numbers say so.** Below the bar, but with the interval reaching it, passes with a note saying more runs would tell. These bars decide whether a release or a change passes, not how a model looks in the results.
- **Measured without a bar:** reasoning and triage. Their numbers are only reported.
- **A case whose turn fails,** such as a model writing no answer, counts as failed.

| Set | The bar for a real model |
|---|---|
| Quality | 90% of runs |
| Files | The same as Quality |
| Topics | Holds at least 80% of what's off topic, and wrongly refuses at most 15% |
| Media | 75%, since another model does the judging |

**Media** is judged like this. A file must first be whole and the right size. Another model that sees then says whether it shows what was asked and whether any part is corrupted. A picture or clip fails as corrupted only if a second, larger model agrees. Whether a clip moves is measured from how much its frames differ, because the models asked got that wrong. A judge that makes a file instead of answering fails the case.

## Whether it runs

Each new row says how the model ran on that computer, from the runtime's own report:

| Runs | What it means |
|---|---|
| **Fully** | Every layer on the graphics card or chip |
| **Partly** | Some layers on the card and the rest in system memory, as a large model or a mixture-of-experts model's experts can be. It works, usually slower |
| **On the processor** | Nothing on a graphics card |
| **Doesn't run** | It ran out of memory loading or answering. The row has no scores, and says so. Any other failure says nothing about the model, so it isn't published |

## How fast it ran

Speed is measured apart from quality, and never counts toward a Score. New rows have all of these, from three runs of a document of about 4,000 tokens, new each run so nothing comes from a cache:

| Figure | What it tells you |
|---|---|
| **Reads**, in tokens a second | How fast it takes in a long chat or document. Often the real wait, and very different between computers |
| **Writes**, in tokens a second | How fast the answer appears once it starts |
| **First token** | How long you wait after sending, with that long prompt |
| **Start to finish**, in tokens a second | The answer's tokens over the whole wait, which is lower than Writes, and closer to how it feels |
| **GPU layers**, such as 15 of 65 | How much of the model ran on the graphics card. Partly on the card is much slower |
| **Memory** | What the model took on the graphics card |
| **Quantization and context window** | Which file of the model ran, and how much it could hold in mind |

A Score stays comparable because the same cases run each time. Speed depends on the computer, what else it's doing, and these settings.

## Where it runs

Quality runs every week on our own computers, and each run adds its scores to the results on its own. It works through [one public list](https://github.com/yeixio/toskar-model-data/blob/main/quality/models.json), a few at a time, whichever have gone longest without a result. The list is about 15 models, popular on Hugging Face and in Toskar's catalog, with a spread of sizes. We check it by hand about monthly, so popularity alone doesn't send everyone's time to an odd fine-tune. Each row names the computer, its operating system, and the date. **Scores from different computers or dates aren't the same test,** so compare rows with care. Speed depends on the computer. Model scores can move too, as Toskar changes.

## What we know is weak

- **Few runs.** One run per case can swing a score by a case or more, and most results come from one run.
- **Few computers.** Mostly one AMD graphics card and one Mac so far.
- **Judging by a model.** Media judges have been wrong in both directions, and a real fault can pass if the first judge misses it.
- **Rules can be too narrow or too loose.** A right answer worded differently can fail, and a wrong one can pass.
- **Real tasks.** "Fix a failing test" is one small task, run with a script that isn't in `tests/quality` yet. It's a first look, and [#1034](https://github.com/yeixio/toskar-core/issues/1034) adds a proper set.
- **Passing isn't the same as good.** These check what we thought to check. They say little about writing style, or how much you'd enjoy using a model.

## Add your computer's results

Our results come from very few computers. If you have a different graphics card, a laptop, or a model we haven't measured, your run shows people something we can't.

You'll need a checkout of this repository and Go. Then:

```sh
scripts/quality-score.sh qwen2.5-7b-q4
```

`toskarctl models list` shows the model IDs. It downloads the model if it needs to. It can take from minutes to hours, so an evening or overnight is a good time.

**What it does to your computer.** It runs its own Toskar, with its own data folder and ports, in an empty folder, so your everyday Toskar and chats aren't touched. Web lookups read fixed test pages. A real model does run, with real tools: every tool that asks first gets no, and that includes destructive commands, which always ask. Known commands, such as `ls` or `git status`, do run, in that empty folder. On a computer you can't risk, use a virtual machine or a container, as we do.

A run of a small model took about 25 minutes on an M5 Pro with 24 GB. A large model takes much longer.

**To send your results,** [open the results form](https://github.com/yeixio/toskar-core/issues/new?template=quality_results.yml) and paste `result.json` from the `quality-score-<model>` folder. Reports with every answer are there too. **What you share is public:** `result.json` has your model, scores, date, graphics card or processor, and memory. Look before you paste, since the reports hold the model's answers and the log has paths from your computer.

Each one is checked automatically: the model has to be on our list, the computer's name has to look like one, and the numbers have to agree with each other and be believable. Anything unusual waits for a person. These checks catch mistakes and spam, and can't prove a run was real, which is why others' results show as Community and ours as Toskar. Results go in with the quality version they ran. Every row says who ran it: Toskar for our own runs, your GitHub username if you give one, or Community. A username shows only if it matches who sent the form, so nobody can post as someone else. A result can be left out if it looks wrong or doesn't match its reports. We can't check how a run was done, so treat these as what they are: one person's run. [#1037](https://github.com/yeixio/toskar-core/issues/1037) is about making this one step, inside Toskar.

To check Toskar itself, with no model and no download, `make quality` runs the sets with a scripted stand-in. The [README](../tests/quality/README.md#running-it) has every other way to run it.

## Versions

Each result says which version of this method measured it (`method_version`). **Compare scores within one version:** a new version can move every score without any model changing.

The version is in [`version.json`](../tests/quality/version.json). It goes up when a score could change: a case is added, changed, or removed, a check or how a set is scored changes, or the fixed web pages change. A test fails if a set changes without a new version. A check changed in code can't be caught that way, so that's on us, and you can hold us to it.

Older results stay, with their version. We don't rescale them. If saved answers exist, we can check them again under a new version.

| Version | Since | What changed |
|---|---|---|
| 1 | November 2026 | The first version named, as this page describes it |
| 0 | Before that | Results measured before versions existed. Sets and checks changed during this time, including how clips are judged (#1027), so don't treat these as one test |
