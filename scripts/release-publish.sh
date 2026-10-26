#!/bin/sh
# Publishes the release commit at HEAD, such as the Homebrew formula or the
# frozen documentation, to main.
#
#   scripts/release-publish.sh <branch> <title> <body>
#
# main takes changes only by an approved pull request, except from a deploy
# key. With RELEASE_DEPLOY_KEY (a write deploy key only the Release
# workflow can use), the commit goes straight to main, rebased if main moved
# meanwhile, so a release needs no one to approve it. Without it, the commit
# goes to <branch> and a pull request opens for someone to approve.
set -eu

branch="$1"
title="$2"
body="$3"

if [ -n "${RELEASE_DEPLOY_KEY:-}" ]; then
	key="${RUNNER_TEMP:-/tmp}/release-deploy-key"
	umask 077
	printf '%s\n' "$RELEASE_DEPLOY_KEY" >"$key"
	trap 'rm -f "$key"' EXIT
	export GIT_SSH_COMMAND="ssh -i $key -o IdentitiesOnly=yes -o StrictHostKeyChecking=accept-new"
	remote="git@github.com:${GITHUB_REPOSITORY}.git"
	for attempt in 1 2 3; do
		if git push "$remote" HEAD:main; then
			echo "Published to main: $title"
			exit 0
		fi
		# main moved, such as the other release commit landing first.
		git fetch origin main
		git rebase origin/main
		echo "main moved; trying again ($attempt)"
	done
	echo "::error::Couldn't push to main after three tries: $title" >&2
	exit 1
fi

git push --force origin "HEAD:$branch"
# gh pr view matches a merged pull request for this head, so look for an open one.
if [ -z "$(gh pr list --head "$branch" --base main --state open --json number --jq '.[0].number')" ]; then
	gh pr create --base main --head "$branch" --title "$title" --body "$body"
fi
# The actions token opens the pull request but can't approve it. A required review must not fail the release.
if ! gh pr merge "$branch" --squash --admin; then
	echo "::warning::\"$title\" is open and needs an approving review. Set RELEASE_DEPLOY_KEY to publish to main without one (docs/release-checklist.md)."
fi
