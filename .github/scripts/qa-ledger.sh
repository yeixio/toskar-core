#!/usr/bin/env bash
# Lists the closed QA issues that no test mentions (the QA ledger).
#
#   .github/scripts/qa-ledger.sh
#
# Every bug a QA pass files gets a test that fails without its fix, and the
# test names the issue, such as "// Regression: toskar-core#1258" or "#1258".
# This finds the closed QA issues no test file names, writes them to the job
# summary, and keeps one open issue, "QA fixes without a regression test",
# listing them; it closes that issue when the list is empty. Issues closed
# as not planned or duplicates need no test.
#
# Needs GH_TOKEN with issues: write. Test files are what git tracks under a
# tests or test folder, or named *_test.go, *.test.ts or *.test.tsx, or
# test_*.py; QA_LEDGER_TESTS can add more git pathspecs.
set -euo pipefail

repo="${GITHUB_REPOSITORY:?}"
name="${repo#*/}"
title="QA fixes without a regression test"

tests=('*_test.go' '*.test.ts' '*.test.tsx' '*.test.mjs' 'test_*.py' '*/test_*.py' 'tests/*' '*/tests/*' 'test/*' '*/test/*')
# shellcheck disable=SC2206 # QA_LEDGER_TESTS is a list of pathspecs.
tests+=(${QA_LEDGER_TESTS:-})

missing=()
while IFS=$'\t' read -r number issue_title; do
  [ -n "$number" ] || continue
  # "toskar-core#1258", or a bare "#1258" that isn't another repository's.
  if git grep -Eq "(${name}#|(^|[^A-Za-z0-9._/-])#)${number}([^0-9]|\$)" -- "${tests[@]}"; then
    continue
  fi
  missing+=("- [ ] #${number} ${issue_title}")
done < <(gh issue list -R "$repo" --label QA --state closed --limit 500 \
  --json number,title,stateReason --jq '.[] | select(.stateReason == "COMPLETED") | [.number, .title] | @tsv')

existing="$(gh issue list -R "$repo" --state open --search "\"$title\" in:title" --json number,title \
  --jq ".[] | select(.title == \"$title\") | .number" | head -n 1)"

if [ "${#missing[@]}" -eq 0 ]; then
  echo "Every closed QA issue has a regression test." | tee -a "${GITHUB_STEP_SUMMARY:-/dev/null}"
  if [ -n "$existing" ]; then
    gh issue close "$existing" -R "$repo" --comment "Every closed QA issue now has a test that names it."
  fi
  exit 0
fi

body="$(printf '%s\n' \
  "These QA issues were closed as fixed, but no test names them, so nothing stops them from coming back." \
  "Add a test that fails without the fix and mentions the issue, such as \`// Regression: ${name}#123\`." \
  "This list is kept by the QA ledger workflow." "" "${missing[@]}")"
{ echo "### $title"; echo; printf '%s\n' "${missing[@]}"; } >> "${GITHUB_STEP_SUMMARY:-/dev/null}"
printf '%s\n' "${missing[@]}"
if [ -n "$existing" ]; then
  gh issue edit "$existing" -R "$repo" --body "$body"
else
  gh issue create -R "$repo" --title "$title" --body "$body"
fi
