---
created: 2026-07-03T21:00:00.000Z
title: GPG-sign TIDE Bot commits so git hosts show them as Verified
area: git
files:
  - internal/harness/commit.go:58
  - pkg/git/integrate.go:56
  - cmd/tide-push/main.go:134
---

## Problem

TIDE Bot commits show as unverified on GitHub (raised after the first
external-repo run, 2026-07-03). Two causes:

1. No signing anywhere — no `CommitOptions.SignKey` / `Signer` at any of the
   three commit sites (executor task commits in `internal/harness/commit.go`,
   integrate commits in `pkg/git/integrate.go`, boundary/push commits in
   `cmd/tide-push`).
2. The default identity `tide-bot@tideproject.k8s` is a non-routable domain
   no git-host account can verify. `TIDE_BOT_NAME`/`TIDE_BOT_EMAIL` env
   overrides exist in harness + integrate, but `cmd/tide-push/main.go:134`
   hardcodes the email — boundary-push commits can't even adopt a real
   identity today.

Verified badges matter for org repos with signed-commit expectations (and
branch protection rules that require signatures would hard-block TIDE pushes
entirely).

## Solution

Portable GPG signing (works for GitHub, GitLab, and Gitea Verified badges —
no git-host coupling, unlike the GitHub-API auto-sign route which violates
the no-hard-coded-git-host constraint):

- New optional Secret ref (e.g. `git.signingKeySecretRef`, data key
  `GIT_SIGNING_KEY` = armored GPG private key) alongside the existing
  creds pattern. Absent = current unsigned behavior.
- Sign at all three commit sites via go-git `CommitOptions.SignKey` using
  pure-Go openpgp (ProtonMail/go-crypto) — no gpg binary in images.
- Make bot identity uniformly configurable: plumb TIDE_BOT_NAME/EMAIL into
  cmd/tide-push (currently hardcoded), and consider surfacing both + the
  signing ref in chart values / Project spec. Committer email MUST match a
  verified email on the machine account that holds the public key.
- Operator doc: machine-user account + key generation + public-key upload
  recipe (docs/project-authoring.md).
- Chart is the FIXED contract — schema/values additions ride a chart bump.
