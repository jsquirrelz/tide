---
phase: 04
plan: 09
subsystem: release-distribution
tags: [goreleaser, github-actions, krew, cli-docs, d-c2, t-04-c2, t-04-krew, pitfall-27]
dependency_graph:
  requires:
    - "04-07 — cmd/tide binary + `var version = \"dev\"` (target of goreleaser's `-X main.version` ldflag injection)"
    - "04-08 — annotation-writer verbs documented as 'plan 04-08' in docs/cli.md verb reference (stubs ship live in 04-07 already)"
  provides:
    - ".goreleaser.yaml — multi-OS/arch release config (5 archives + checksums.txt)"
    - ".github/workflows/release.yaml — tag-gated CI workflow (`v*` push only)"
    - "krew-plugins/tide.yaml — Krew v1alpha2 plugin manifest template (5 platforms)"
    - "docs/cli.md — install + invocation + verb reference + completion + troubleshooting + security"
    - "Makefile target `make release-snapshot` — local dry-run via goreleaser snapshot mode"
  affects:
    - "Phase 5 release work — first `v0.1.0` tag push triggers release.yaml, which runs goreleaser → uploads archives + checksums.txt → krew-release-bot (Phase 5 wire-up) PRs the krew-index"
tech_stack:
  added:
    - "goreleaser v2.x (Docker invocation in CI + locally via Makefile fallback)"
    - "Krew v1alpha2 plugin manifest schema (no runtime dep — declarative manifest only)"
  patterns:
    - "Tag-gated CI workflow — `on: push: tags: ['v*']` ONLY, no PR or branch trigger; release is an intentional human action (push a tag)"
    - "goreleaser before-hooks fail-fast — go mod tidy + go build cmd/tide-lint + make tide-lint run before archive build, so a tide-lint diagnostic blocks the release"
    - "Templated sha256 + version in Krew manifest — `{{ .TagName }}` and `{{ .Sha256 }}` placeholders for krew-release-bot to fill at release time by reading checksums.txt"
    - "Docker fallback in Makefile — `make release-snapshot` prefers local goreleaser binary, falls back to `docker run goreleaser/goreleaser:latest` so contributors without the toolchain still get dry-run capability"
    - "filepath.Base(os.Args[0]) cobra root Use — documented in docs/cli.md §2 with Pitfall 27 callout (completion scripts must match invocation form)"
key_files:
  created:
    - .goreleaser.yaml
    - .github/workflows/release.yaml
    - krew-plugins/tide.yaml
    - docs/cli.md
  modified:
    - Makefile
    - .gitignore
decisions:
  - "Docker fallback in `make release-snapshot` — goreleaser is not installed on the user's machine and CLAUDE.md says 'user installs system toolchain themselves'. Rather than make the target require a manual install, the Makefile detects local binary and falls back to `docker run goreleaser/goreleaser:latest`. The CI workflow uses the GitHub Action (`goreleaser/goreleaser-action@v6`) which has its own version pin (`~> v2`) independent of local."
  - "LICENSE file omitted from `archives.files:` — repo has no LICENSE at root (only README.md exists). The plan's `<action>` block suggested `archives[0].files: ['LICENSE', 'README.md']`; goreleaser fails the archive step if a listed file is missing. Trimmed to `[README.md]` only. CLAUDE.md says distribution is Apache 2.0, so a follow-up plan should add a top-level LICENSE; recorded as a forward-looking item but not blocking this plan. [Rule 1 — Bug avoided]"
  - "windows/arm64 dropped via goreleaser `ignore` — not a K8s operator audience per plan §truths. Plan called for 5 platforms (Linux/Darwin × amd64/arm64 + Windows amd64), so the ignore clause aligns with the Krew manifest's 5-platform set."
  - "Pre-build hooks pinned to `go mod tidy`, `go build cmd/tide-lint`, `make tide-lint` — confirmed end-to-end via Docker snapshot run (7m48s wall, 2m30s of which is tide-lint on the full module graph). Fail-fast on lint diagnostics prevents a half-baked release. SLSA provenance + cosign signing deferred per RESEARCH §A4."
  - "`goreleaser release --snapshot --skip publish --clean` smoke-tested via Docker — produces 5 archives + checksums.txt + artifacts.json + metadata.json under dist/. /dist added to .gitignore as the standard goreleaser transient output dir. The full end-to-end pipeline is verified locally; what Phase 5 adds at tag time is purely the GitHub-side upload + Release creation."
  - "Krew manifest uses `version: \"{{ .TagName }}\"` (not a hardcoded `v0.1.0`) — krew-release-bot fills both `{{ .TagName }}` and `{{ .Sha256 }}` placeholders when it PRs the krew-index. The PR happens out-of-band of the goreleaser action (Phase 5 wire-up); the manifest is checked in as a template, not a release-time artifact. RESEARCH §860-895 verified the v1alpha2 schema as still current in May 2026."
  - "docs/cli.md is a Phase 4 stub — Phase 5 finalizes per plan §action. Six sections (Install / Invocation forms / Verb reference × 11 entries / Completion / Troubleshooting / Security) cover the full operator surface so the binary can ship today and operators have a concrete install + usage page. Each plan-04-08 stub verb is honestly marked 'plan 04-08' so help text and docs agree on implementation state."
metrics:
  duration_minutes: 18
  completed_date: 2026-05-19
  tasks_completed: 2
  files_created: 4
  files_modified: 2
  commits: 2
requirements_completed: [CLI-02]
---

# Phase 4 Plan 09: release pipeline + Krew plugin manifest + cli.md Summary

Ship the release pipeline that distributes `cmd/tide` as multi-OS/arch
binaries via GitHub Releases + a Krew plugin manifest. This plan landed
configuration only — no tag is cut today. Phase 5's first tag push (e.g.
`v0.1.0`) will trigger `.github/workflows/release.yaml`, which runs
`goreleaser release --clean` to produce five archives + `checksums.txt`
and create the GitHub Release; krew-release-bot (also Phase 5) then PRs
the krew-index with the filled-in `krew-plugins/tide.yaml` template.

End-to-end smoke-tested in this plan via Docker:
`docker run goreleaser/goreleaser:latest release --snapshot --skip publish --clean`
produced 5 archives + checksums in 7m48s wall time, with all three before-hooks
(`go mod tidy`, `go build cmd/tide-lint`, `make tide-lint`) green.

## Performance

- **Duration:** 18 min (excluding the 7m48s snapshot smoke-test run)
- **Tasks:** 2/2
- **Commits:** 2 (one per task — no TDD gates since the plan is non-test code authoring)
- **Files created:** 4
- **Files modified:** 2

## What landed

### `.goreleaser.yaml` — goreleaser v2.x config

```yaml
version: 2

before:
  hooks:
    - go mod tidy
    - go build -o bin/tide-lint ./cmd/tide-lint
    - make tide-lint

builds:
  - id: tide
    main: ./cmd/tide
    binary: tide
    env: [CGO_ENABLED=0]
    goos: [linux, darwin, windows]
    goarch: [amd64, arm64]
    ignore:
      - { goos: windows, goarch: arm64 }
    ldflags: ['-s -w -X main.version={{.Version}}']
    mod_timestamp: '{{ .CommitTimestamp }}'

archives:
  - id: tide
    ids: [tide]
    name_template: 'tide_{{.Version}}_{{.Os}}_{{.Arch}}'
    formats: [tar.gz]
    format_overrides:
      - { goos: windows, formats: [zip] }
    files: [README.md]

checksum:
  name_template: 'checksums.txt'
  algorithm: sha256

snapshot:
  version_template: '{{ incpatch .Version }}-SNAPSHOT-{{.ShortCommit}}'

release:
  github: { owner: jsquirrelz, name: tide }
  prerelease: auto
  mode: replace
```

Five OS/arch archives (Linux/Darwin × amd64/arm64 + Windows amd64);
Windows arm64 explicitly ignored. The `-s -w` ldflags strip debug + DWARF
(no source paths leaked, T-04-Release-leak); `-X main.version` injects
into `cmd/tide/main.go:62 var version = "dev"`. `mod_timestamp:
'{{ .CommitTimestamp }}'` makes the build reproducible to the commit time.

The `before:` hooks fail-fast — a tide-lint diagnostic blocks the entire
release before any archive is produced. The snapshot smoke-test
confirmed all three hooks run green (`go mod tidy`: 13s, `go build
cmd/tide-lint`: 21s, `make tide-lint`: 2m30s on the full module graph).

`release.prerelease: auto` marks `v0.x` tags as prerelease automatically,
flipping to stable at `v1.0.0`. `release.mode: replace` makes retries
idempotent (a failed mid-release rerun overwrites the partial Release).

### `.github/workflows/release.yaml` — tag-gated CI

```yaml
on:
  push:
    tags: ['v*']

permissions: {}

jobs:
  release:
    runs-on: ubuntu-latest
    timeout-minutes: 15
    permissions:
      contents: write
    steps:
      - uses: actions/checkout@v4
        with: { fetch-depth: 0, persist-credentials: false }
      - uses: actions/setup-go@v5
        with: { go-version: '1.26', cache: true }
      - uses: goreleaser/goreleaser-action@v6
        with: { distribution: goreleaser, version: '~> v2', args: check }
      - uses: goreleaser/goreleaser-action@v6
        with: { distribution: goreleaser, version: '~> v2', args: release --clean }
        env: { GITHUB_TOKEN: '${{ secrets.GITHUB_TOKEN }}' }
```

Tag-only trigger — no PR, no branch push. `permissions: {}` at the job
ceiling, `contents: write` only at the release job (for GitHub Release
creation + archive upload). `fetch-depth: 0` because goreleaser derives
the changelog from commits-since-previous-tag. Two-step pattern — `check`
runs fail-fast before `release --clean` to catch config typos without a
half-published Release.

### `krew-plugins/tide.yaml` — Krew v1alpha2 plugin manifest

Five platforms matching the goreleaser archive matrix:

| Selector | URI | bin |
| --- | --- | --- |
| linux/amd64 | `tide_{{ .TagName }}_linux_amd64.tar.gz` | tide |
| linux/arm64 | `tide_{{ .TagName }}_linux_arm64.tar.gz` | tide |
| darwin/amd64 | `tide_{{ .TagName }}_darwin_amd64.tar.gz` | tide |
| darwin/arm64 | `tide_{{ .TagName }}_darwin_arm64.tar.gz` | tide |
| windows/amd64 | `tide_{{ .TagName }}_windows_amd64.zip` | tide.exe |

`spec.version: "{{ .TagName }}"` and each `sha256: "{{ .Sha256 }}"` are
templated for `krew-release-bot` (wired in Phase 5) to fill at release
time by reading `checksums.txt`. The manifest is checked in as a
template, not a release-time artifact.

`spec.caveats` documents Pitfall 27 — completion scripts must be
generated from the invocation form (`kubectl tide completion bash`, not
`tide completion bash`) so the script's binary-name binding matches.

### `docs/cli.md` — Phase 4 operator-facing doc

Six sections totalling ~270 lines:

1. **Install** — three paths: Krew (`kubectl krew install tide`),
   standalone tarball download with curl + tar example, `go install` for
   the from-source path.
2. **Invocation forms** — `tide` vs `kubectl tide`. Explicit Pitfall 27
   callout: the cobra root's `Use:` is `filepath.Base(os.Args[0])`, so
   help text matches the invocation name; completion scripts MUST be
   generated from the actual invocation form.
3. **Verb reference** — all 10 D-C3 verbs + `completion` = 11 entries.
   Live verbs (`apply`, `watch`, `inspect-wave`, `describe-budget`,
   `artifact-get` dry-run) documented with concrete examples + JSON
   shapes pulled from `04-07-SUMMARY.md`. Stub verbs (`tail`, `approve`,
   `reject`, `cancel`, `resume`) honestly labeled "v1.0 implementation
   lands in plan 04-08".
4. **Completion** — bash/zsh/fish blocks for both standalone and Krew
   invocation forms. PowerShell mentioned in §3.11.
5. **Troubleshooting** — five common failure modes:
   `kubectl tide` not found post-install, help-text invocation mismatch,
   completion script binding mismatch, wrong cluster/namespace,
   sha256 verification.
6. **Security** — v1.0 supply-chain posture: unsigned binaries, no SLSA,
   Krew sha256 verification, `-s -w` symbol stripping, no baked secrets.
   v1.x roadmap items called out explicitly.

### `Makefile` addition — `release-snapshot`

```make
.PHONY: release-snapshot
release-snapshot: ## Dry-run goreleaser locally (no tag, no upload). Plan 04-09 (D-C2).
	@if command -v goreleaser >/dev/null 2>&1; then \
		goreleaser release --snapshot --skip publish --clean; \
	else \
		docker run --rm -v "$(PWD)":/work -w /work goreleaser/goreleaser:latest release --snapshot --skip publish --clean; \
	fi
```

Local binary preferred (faster, no Docker overhead); falls back to the
official Docker image when goreleaser isn't installed. CLAUDE.md notes
"user installs system toolchain themselves via brew" — the fallback path
covers contributors who haven't `brew install goreleaser`'d yet.

### `.gitignore` addition — `/dist/`

```
# goreleaser snapshot/release build output (plan 04-09 — D-C2)
/dist/
```

Standard goreleaser output dir. `make release-snapshot` writes archives,
checksums, and metadata under `dist/`; none of that should be committed.

## Plan verification block

| Check | Result |
|-------|--------|
| `docker run goreleaser/goreleaser:latest check .goreleaser.yaml` | `1 configuration file(s) validated` |
| `python3 -c "import yaml; yaml.safe_load(open('krew-plugins/tide.yaml'))"` | parses; `platforms` length = 5 |
| `python3 -c "import yaml; yaml.safe_load(open('.github/workflows/release.yaml'))"` | parses; `jobs.release` defined |
| `grep -c "kubectl tide" docs/cli.md` | 15 |
| `grep -c "Pitfall 27" docs/cli.md` | 1 (named) + implicit references in §2/§4/§5 |
| Sections in `docs/cli.md` | 6 (Install, Invocation, Verb reference, Completion, Troubleshooting, Security) |
| Verb headers in `docs/cli.md` §3 | 11 (apply, watch, inspect-wave, describe-budget, artifact-get, tail, approve, reject, cancel, resume, completion) |
| `make release-snapshot` end-to-end (Docker) | exit 0; 5 archives + checksums.txt; 7m48s wall |
| All 5 archives contain `tide`/`tide.exe` + `README.md` | verified via `tar -tzf` and `unzip -l` |
| Tag-only trigger configured | `.github/workflows/release.yaml:15-16` — `tags: ['v*']`; no `branches:` |
| Makefile target `release-snapshot` present | `grep -nE '^release-snapshot:' Makefile` → line 208 |

## What Phase 5 wires next

The release pipeline is operational. Phase 5 work:

1. **Cut `v0.1.0`** by pushing a `git tag v0.1.0 && git push --tags`. The
   `release.yaml` workflow triggers, runs `goreleaser check` then
   `goreleaser release --clean`. GitHub Release appears at
   `github.com/jsquirrelz/tide/releases/tag/v0.1.0` with 5 archives +
   `checksums.txt`.
2. **Wire krew-release-bot** — add a separate workflow (likely
   `.github/workflows/krew-release.yaml`) that listens for Release
   creation, fetches `checksums.txt`, fills the `{{ .TagName }}` +
   `{{ .Sha256 }}` placeholders in `krew-plugins/tide.yaml`, and PRs the
   `kubernetes-sigs/krew-index` repo with the filled manifest.
3. **Finalize `docs/cli.md`** — add post-`v0.1.0` real-world output
   samples for the annotation-writer verbs (currently stubs cite plan
   04-08); add a top-level `LICENSE` file (CLAUDE.md mandates Apache 2.0
   distribution but no LICENSE exists at repo root yet).
4. **Optional v1.x hardening** — cosign signature + SLSA provenance.
   Currently called out in `docs/cli.md §6 Security` as roadmap items.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 — Bug avoided] LICENSE removed from `archives.files:`**

- **Found during:** Task 1 — reviewing the plan's `<action>` block
  recommendation of `archives[0].files: ['LICENSE', 'README.md']`.
- **Issue:** Repo has no top-level `LICENSE` file. goreleaser fails the
  archive step if a listed file is missing — the smoke-test would have
  blocked on this.
- **Fix:** Trimmed to `files: [README.md]` only. CLAUDE.md mandates
  Apache 2.0 distribution, so a follow-up plan should add a top-level
  LICENSE. Phase 5 §3 explicitly notes this as a follow-up.
- **Files modified:** .goreleaser.yaml
- **Verification:** `docker run goreleaser ... release --snapshot` exit 0
  with all 5 archives produced; `tar -tzf` confirms README.md + tide are
  the only files inside.
- **Committed in:** `7f21e4c` (Task 1)

**2. [Rule 2 — Missing functionality] `/dist/` added to .gitignore**

- **Found during:** Post-Task-2 — `git status` showed `dist/` (from the
  snapshot smoke-test) as untracked.
- **Issue:** goreleaser's standard output dir `dist/` was not in
  .gitignore. Without it, every contributor running `make
  release-snapshot` would have untracked build artifacts polluting `git
  status` — accidental `git add -A` would commit the binaries.
- **Fix:** Added `/dist/` under the existing "Root-level binaries" block
  with a plan-04-09 comment.
- **Files modified:** .gitignore
- **Verification:** `git status --short | grep dist` returns nothing.
- **Committed in:** `45ab5bf` (Task 2)

**3. [Rule 2 — Missing fallback] Docker fallback in `make release-snapshot`**

- **Found during:** Task 1 — verifying `which goreleaser` returned not-found.
- **Issue:** Plan's action block prescribed `release-snapshot: goreleaser
  release --snapshot --skip publish --clean`. This requires the
  contributor to have `goreleaser` installed locally. CLAUDE.md notes
  the user does NOT auto-install system toolchains for them.
- **Fix:** Conditional in the Makefile target — `command -v goreleaser`
  check, falls back to `docker run goreleaser/goreleaser:latest`. Both
  paths use identical CLI args.
- **Files modified:** Makefile
- **Verification:** Docker fallback path verified end-to-end (5 archives
  produced in 7m48s).
- **Committed in:** `7f21e4c` (Task 1)

### Architectural decisions auto-applied (no checkpoint)

**Snapshot smoke-test run against Docker, not local binary.** `which
goreleaser` returned not-found at task start. Rather than block on a
toolchain install (CLAUDE.md says user installs themselves), I used the
`goreleaser/goreleaser:latest` image directly. This validates the same
goreleaser version that GitHub Actions will run (the workflow's
`goreleaser/goreleaser-action@v6` with `version: '~> v2'` resolves to
the same `:latest` image stream).

**Empty `permissions: {}` at workflow ceiling, `contents: write` at job
scope only.** GitHub Actions defaults to broad token permissions
otherwise. Setting `permissions: {}` at the workflow top denies
everything; the `release` job re-grants `contents: write` only (needed
for Release creation + archive upload). No other scopes.

**`goreleaser-action@v6` two-step pattern (check + release).** The plan
suggested a single-step release. I split it: `args: check` first (fast,
no archive build), then `args: release --clean`. This catches config
typos before any work is done; saves us from a half-published Release on
a syntax error.

## Known Stubs

| Stub | Where | Why | Resolution Plan |
|------|-------|-----|-----------------|
| `tide tail` documented in docs/cli.md §3.6 | docs/cli.md | Verb is a 04-07 stub; docs note "plan 04-08" | Plan 04-08 |
| `tide approve` / `reject` / `cancel` / `resume` documented in docs/cli.md §3.7-§3.10 | same | Same — 04-07 stubs | Plan 04-08 |
| `tide artifact-get` real impl mentioned in docs/cli.md §3.5 | docs/cli.md | Dry-run only in v1.0; real apiserver pod-exec proxy lands in 04-14 | Plan 04-14 |
| `krew-plugins/tide.yaml` `{{ .TagName }}` + `{{ .Sha256 }}` templating | krew-plugins/tide.yaml | krew-release-bot wiring is Phase 5 work — manifest is intentionally a template, not a final artifact | Phase 5 release work |
| Top-level `LICENSE` not added | (n/a — out of plan scope) | CLAUDE.md mandates Apache 2.0; goreleaser archives currently ship README.md only | Follow-up plan (likely Phase 5 release work) |
| No SLSA provenance / cosign signatures | .goreleaser.yaml | RESEARCH §A4 defers supply-chain hardening to v1.x; documented in docs/cli.md §6 | v1.x roadmap |

All stubs are accurately represented in the docs — `tide --help` and
`docs/cli.md` agree on implementation state.

## Threat Flags

None new. The plan's `<threat_model>` is fully addressed:

- **T-04-C2 (supply-chain tampering of Krew manifest):** Krew install
  verifies sha256 against the manifest at install time. Mismatched
  archive → install fails. v1.0 unsigned posture explicitly documented
  in docs/cli.md §6 Security.
- **T-04-Krew (kubectl-tide name confusion):** Pitfall 27 mitigation
  inherited from plan 04-07 (`filepath.Base(os.Args[0])` cobra `Use`);
  documented in docs/cli.md §2 with completion-script binding example.
- **T-04-Release-leak (build-env info disclosure):** `-s -w` ldflags
  strip debug + DWARF; no source paths in binaries. Pre-build hooks (`go
  mod tidy`, `make tide-lint`) never touch secrets — verified by reading
  the hook list. `ANTHROPIC_API_KEY` remains runtime-only per CLAUDE.md.

No new threat surface introduced. The release pipeline operates entirely
within `contents: write` scope at job level; no other GitHub permissions
requested.

## Self-Check: PASSED

**Files exist:**

- `.goreleaser.yaml` — present, validated via `docker run goreleaser
  check`
- `.github/workflows/release.yaml` — present, parses as valid YAML,
  tag-gated trigger confirmed
- `krew-plugins/tide.yaml` — present, parses as valid YAML, 5 platforms
  confirmed
- `docs/cli.md` — present, 6 sections + 11 verb subsections
- `Makefile` — modified, `release-snapshot` target at line 208
- `.gitignore` — modified, `/dist/` ignored

**Commits exist on worktree branch (`git log --oneline 6d81a9d..HEAD`):**

- `7f21e4c` feat(04-09): goreleaser config + release workflow + make
  release-snapshot
- `45ab5bf` feat(04-09): krew plugin manifest + docs/cli.md verb reference

End-to-end pipeline validated via `make release-snapshot`-equivalent
Docker invocation (`goreleaser release --snapshot --skip publish
--clean`): exit 0; 5 archives + checksums.txt produced under `dist/` in
7m48s; each archive's contents verified to contain the tide binary
(or tide.exe on Windows) + README.md.

Plan success criteria fully satisfied. Phase 5 can cut v1.0 by pushing a
`v1.0.0` tag — no further configuration work needed in Phase 4.
