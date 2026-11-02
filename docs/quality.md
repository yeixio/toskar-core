# How Toskar measures quality

This page explains how the quality results are measured, so you can judge them and tell us what's wrong with them. The results are in [toskar-model-data](https://github.com/yeixio/toskar-model-data/blob/main/quality/results.json). They're measurements, not a ranking: we don't combine them into one score.

Every case, check, and threshold is public, in [`tests/quality`](../tests/quality). Its [README](../tests/quality/README.md) has the full detail.

**See something wrong or missing? [Suggest a change](https://github.com/yeixio/toskar-core/issues/new?template=quality_suggestion.yml).** A case we should add, a check that's unfair, or a better way to score are all welcome, and so is a pull request.

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

- **A score is cases passed, out of cases run,** per set. There's no weighting, and no overall number.
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

## Where it runs

Quality runs every week on our own computers, a few models at a time, and each results row names the computer and date. **Scores from different computers or dates aren't the same test,** so compare rows with care. Speed depends on the computer. Model scores can move too, as Toskar changes.

## What we know is weak

- **Few runs.** One run per case can swing a score by a case or more, and most results come from one run.
- **Few computers.** Mostly one AMD graphics card and one Mac so far.
- **Judging by a model.** Media judges have been wrong in both directions, and a real fault can pass if the first judge misses it.
- **Rules can be too narrow or too loose.** A right answer worded differently can fail, and a wrong one can pass.
- **Real tasks.** "Fix a failing test" is one small task, run with a script that isn't in `tests/quality` yet. It's a first look, and [#1034](https://github.com/yeixio/toskar-core/issues/1034) adds a proper set.
- **Passing isn't the same as good.** These check what we thought to check. They say little about writing style, or how much you'd enjoy using a model.

## Run it yourself

`make quality` runs the sets with a scripted stand-in for a model, which tests Toskar and needs no download. To measure a real model, the [README](../tests/quality/README.md#running-it) has the commands. [#1037](https://github.com/yeixio/toskar-core/issues/1037) is about making this one step, with results you can choose to share.
