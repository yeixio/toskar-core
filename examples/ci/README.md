# Toskar in CI

Workflows that ask your own Toskar as a CI step ([Toskar in CI](../../docs/ci.md)). Copy one into your repository and set its secrets.

| | |
| --- | --- |
| [github/triage.yml](github/triage.yml) | GitHub Actions: suggests labels for each new issue, and adds the ones you allow ([triage.schema.json](github/triage.schema.json)) |
| [github/failing-log.yml](github/failing-log.yml) | GitHub Actions: when CI fails, reads the failed steps' log and says what went wrong, on the run and its pull request |
| [gitlab/gitlab-ci.yml](gitlab/gitlab-ci.yml) | GitLab CI: when the build fails, a later job reads its log and explains it |
| [forgejo/triage.yml](forgejo/triage.yml) | Forgejo and Gitea Actions: suggests labels for each new issue, in a comment for a person to apply |

The GitHub workflows use the [Ask Toskar action](https://github.com/yeixio/toskar-action). The others use the same pieces from a shell: `install.sh ci` and `toskarctl ask`.

**Secrets.** `TOSKAR_URL` is a Toskar the runner can reach, and `TOSKAR_API_KEY` is a key made with **For a CI job** on its API Access page. That key can only ask, with no memories, knowledge, or tools. Without them, the GitHub workflows start Toskar on the runner, with a small model on the CPU.

**Untrusted text.** An issue or a log is written by someone else, so each workflow treats it as data, never as the prompt:
- The step that reads it has no permission to write.
- An answer that changes something, such as labels, is JSON checked against the labels you allow, in a separate step.
- An explanation is posted only as text, marked as Toskar's reading.

## Keeping them working

`examples/test.py` runs each workflow's shell steps as written, against a throwaway Core and this checkout's `toskarctl`, with `gh` and `curl` standing in. It checks that each workflow:
- adds only the allowed labels,
- posts the explanation where it says,
- leaves alone what it shouldn't touch.

actionlint checks the GitHub workflows, and the Ask Toskar action's own CI tests the action. Change a workflow and its check together.
