---
phase: 05-distribution-self-hosting-acceptance
plan: 16
subsystem: release-pipeline
tags: [release-pipeline, helmify-verify, dry-run-gate, oci-publish, ghcr]
requirements: [DIST-01, DIST-02, DIST-05]
dependency-graph:
  requires:
    - charts/tide (Plan 05-05 v1.0.0 bump)
    - charts/tide-crds (Plan 05-05 v1.0.0 bump + Plan 05-14 resource-policy:keep)
    - hack/scripts/dry-run-v1.sh (Plan 05-15)
    - Makefile dry-run-v1 target (Plan 05-15)
    - .github/workflows/ci.yaml helm-lint job (Phase 1 — analog for helmify-verify)
    - .github/workflows/live-e2e.yml (Phase 04.1 P2.4 — no-cron posture analog)
  provides:
    - v* tag-triggered release pipeline with chart-tree reproducibility + parent-version-filtered rc dry-run gate + Helm OCI publish to ghcr.io
    - v*-rc.* tag-triggered dry-run gate (release-candidate verification of operator install flow)
  affects:
    - release-time path for goreleaser binaries (now gated on helmify-verify + pre-flight passing)
    - chart distribution surface (charts now ship to ghcr.io/jsquirrelz/tide-charts/{tide-crds,tide} via OCI alongside the existing GitHub Release tarballs)
tech-stack:
  added: []   # no new tooling — uses existing helm CLI + gh CLI + actions/upload-artifact
  patterns:
    - tag-triggered + workflow_dispatch (no cron) per Phase 04.1 P2.4 lock
    - parent-version-filtered rc match per MEDIUM-9 revision (eliminates cross-version rc false-pass)
    - sibling-container DinD via /var/run/docker.sock mount (no --privileged) — invoked through make dry-run-v1
    - if: !contains(github.ref, '-rc.') job-level gating (preferred over workflow-level rc-tag exclusion that would still match v* glob)
    - helm push to oci://ghcr.io after release succeeds (RESEARCH Topic 2 — plain helm CLI, not appany/helm-oci-chart-releaser)
key-files:
  created:
    - .github/workflows/dry-run.yaml
  modified:
    - .github/workflows/release.yaml
decisions:
  - "MEDIUM-9 parent-version filter — pre-flight job extracts MAJOR.MINOR.PATCH from ${GITHUB_REF_NAME#v} and filters git tag --list 'v${PARENT_VERSION}-rc.*' rather than 'git tag --list v*-rc.* | head -1' to prevent a stale rc from a different version from false-passing the release gate."
  - "Plain helm CLI for OCI push (5 shell lines: login → package → push → logout) per RESEARCH Topic 2 — wrapper actions (appany/helm-oci-chart-releaser) add no value at v1 (no GPG signing; provenance is v1.x per .goreleaser.yaml:113)."
  - "tide-crds pushed before tide — tide's Chart.yaml dependencies block references tide-crds; reverse-order push would briefly leave operators with a dangling dependency."
  - "Workflow-level permissions: {} preserved on both files; job-level scopes added narrowly (helmify-verify contents:read, pre-flight contents:read + actions:read for gh run list, release contents:write unchanged, chart-publish contents:read + packages:write per Pitfall 3, dry-run contents:read)."
  - "if: !contains(github.ref, '-rc.') on release + chart-publish jobs rather than using a separate workflow file — keeps both rc and full tags handled by one release.yaml while ensuring rc tags ONLY trigger helmify-verify (binaries never build for rc tags)."
  - "Two-workflow pattern adopted (release.yaml + dry-run.yaml) per RESEARCH Topic 9 — alternative single-workflow-with-conditionals had a needs: semantic problem (conditionally-skipped upstream jobs satisfy needs: as 'success' unless using always() + explicit result check)."
metrics:
  duration: "~14 min wall-clock (read context + 2 writes + verify + 2 commits + summary)"
  completed: "2026-05-23T13:49:13Z"
  tasks: 2
  files-changed: 2
  files-created: 1
  files-modified: 1
  net-lines-added: 332   # release.yaml: +222 -5 net 217; dry-run.yaml: +110 net 110; total ~327 (raw insertions 332)
---

# Phase 5 Plan 16: Release-Pipeline Extension (helmify-verify, pre-flight, chart-publish, dry-run gate) Summary

One-liner: Extends `.github/workflows/release.yaml` with three new gating jobs (helmify-verify, pre-flight, chart-publish) and creates `.github/workflows/dry-run.yaml` firing on `v*-rc.*` tags — the v1.0 release pipeline now gates goreleaser on chart-tree reproducibility + parent-version-filtered rc dry-run success + Helm OCI publish to ghcr.io.

## What Shipped

### 1. release.yaml — 3 new jobs added (existing release job preserved verbatim, just gated)

**helmify-verify** (lines 39-89 of release.yaml): Mirror of `ci.yaml`'s `helm-lint` job. Runs `make helm` + `git diff --exit-code charts/` — fails the release if the committed `charts/` tree diverges from a fresh helmify+augment regeneration. Defense-in-depth on top of CI's per-PR helm-lint check (D-X2 verify-only). Permissions: `contents: read` only. Runs on every `v*` tag including rc tags (the gate is cheap).

**pre-flight** (lines 91-167 of release.yaml): MEDIUM-9 revision — parent-version-filtered rc dry-run verification.
- Extracts `MAJOR.MINOR.PATCH` from `${GITHUB_REF_NAME#v}` (e.g. release tag `v1.0.0` → `PARENT_VERSION=1.0.0`).
- Filters `git tag --list "v${PARENT_VERSION}-rc.*" --sort=-version:refname | head -1` to find the highest matching rc tag (e.g. `v1.0.0-rc.2` if both `v1.0.0-rc.1` and `v1.0.0-rc.2` exist).
- Verifies dry-run.yaml run for that exact rc tag concluded `success` via `gh run list --workflow dry-run.yaml --branch "${MATCHING_RC}" --limit 1 --json conclusion --jq '.[0].conclusion'`.
- Fails the release if no matching rc tag exists OR the matched rc's dry-run didn't succeed.
- `if: !contains(github.ref, '-rc.')` so this job is SKIPPED on rc tag pushes (rc tags only fire dry-run.yaml + helmify-verify, never goreleaser).
- Permissions: `contents: read` + `actions: read` (the latter required by `gh run list --workflow`).
- Contract documented in workflow comments: rc tags must carry the parent MAJOR.MINOR.PATCH they precede (single-maintainer discipline); cross-version rc tags excluded by design.

**release** (lines 169-217 of release.yaml): EXISTING goreleaser job, preserved verbatim. Modified ONLY to add the two gate lines:
- `if: ${{ !contains(github.ref, '-rc.') }}` — binaries never build for rc tags.
- `needs: [helmify-verify, pre-flight]` — gate chain.

**chart-publish** (lines 219-265 of release.yaml): Helm OCI publish to ghcr.io/jsquirrelz/tide-charts/{tide-crds,tide}. Runs after `release` succeeds.
- `permissions: { contents: read, packages: write }` — `packages: write` is REQUIRED for ghcr.io OCI push per Pitfall 3 (the existing release job only had `contents: write`; adding `packages: write` there would over-grant the goreleaser step).
- `helm registry login` uses `--password-stdin` (no token on command line; threat T-05-16-02 mitigation).
- Push order: tide-crds first (tide depends on it via Chart.yaml `dependencies:` block); reverse order would briefly leave operators with a dangling dependency.
- `helm package ... --version "${GITHUB_REF_NAME#v}" --app-version "${GITHUB_REF_NAME#v}"` — belt-and-braces version stamping on top of the v1.0.0 bump from Plan 05-05.
- `helm registry logout` has `if: always()` — token invalidated even if push failed.

### 2. dry-run.yaml — NEW workflow firing on v*-rc.* tags

- `on: push: tags: ['v*-rc.*']` + `workflow_dispatch: {}` — NO `schedule:` cron (Phase 04.1 P2.4 lock).
- `permissions: {}` workflow-level + job-level `contents: read` (deny-by-default).
- `timeout-minutes: 35` — 30-min D-D3 budget (DRY_RUN_TIMEOUT_SECONDS in `hack/scripts/dry-run-v1.sh`) + 5-min runner cold-start buffer.
- Steps: checkout (persist-credentials:false) → setup-go (1.26) → install kind v0.31.0 (pinned to match ci.yaml + live-e2e.yml) → install helm v3.16.3 (pinned) → `make dry-run-v1` → upload-artifact (if: always(), name `dry-run-evidence-${{ github.ref_name }}`, path glob `/tmp/tide-dry-run-*/{dry-run-report.json,transcript.log}`, retention-days: 90 per D-D4).
- `make dry-run-v1` invokes `hack/scripts/dry-run-v1.sh` (Plan 05-15) which does sibling-container DinD (mount `/var/run/docker.sock`, no `--privileged`) per RESEARCH Pitfall 6.
- Artifact name parameterized on `github.ref_name` so multiple rc tags (rc.1, rc.2, ...) coexist without overwriting.

## Acceptance criteria — all PASS

| Criterion | Status | Evidence |
| --- | --- | --- |
| release.yaml extended with helmify-verify + chart-publish jobs | PASS | `grep -q "helmify-verify:"` + `grep -q "chart-publish:"` returns 0 |
| dry-run.yaml created (v*-rc.* tag + workflow_dispatch only, NO schedule) | PASS | `grep -q "'v\*-rc\.\*'"` + `grep -q "workflow_dispatch"` + `grep -E '^\s*schedule:'` returns NONE |
| release.yaml chart-publish job uses Helm OCI (`helm push ... oci://ghcr.io/...`) | PASS | `grep -q "oci://ghcr.io/jsquirrelz/tide-charts"` returns 0 |
| release.yaml chart-publish job requires `packages: write` | PASS | `grep -q "packages: write"` returns 0 |
| dry-run.yaml `timeout-minutes ≤ 35` | PASS | `grep -q "timeout-minutes: 35"` returns 0 (exactly at budget+buffer ceiling) |
| Pre-flight rc-tag matching filters by parent semver (MEDIUM-9 fix) | PASS | `grep -q "PARENT_VERSION\|GITHUB_REF_NAME#v"` + `grep -q "git tag --list"` returns 0 |
| Workflows lint clean | PASS | `python3 -c "import yaml; yaml.safe_load(...)"` exits 0 for both files (actionlint + yamllint not installed in env; PyYAML 6.0.3 used) |
| Each task committed individually | PASS | 66122dc (release.yaml) + c4d0639 (dry-run.yaml) — see commits table below |
| SUMMARY.md created | PASS | This file |

## Threat Mitigations Applied

| Threat | Mitigation in this plan |
| --- | --- |
| T-05-16-01 (Elevation of Privilege — chart-publish job over-grants) | `permissions: contents: read + packages: write` — minimum scopes for `helm push`; helmify-verify has `contents: read` only, no write |
| T-05-16-02 (Information Disclosure — GITHUB_TOKEN in helm registry login) | `--password-stdin` (never on command line); login → push → logout with `if: always()` logout safety net |
| T-05-16-03 (Tampering — chart tree drift on release) | helmify-verify runs `make helm` + `git diff --exit-code charts/`; fails release on drift |
| T-05-16-04 (Tampering — release without prior parent-version-matched rc dry-run) | pre-flight extracts MAJOR.MINOR.PATCH from release tag + filters `git tag --list "v${PARENT_VERSION}-rc.*"` (MEDIUM-9 revision); fails release if missing or rc dry-run didn't succeed |
| T-05-16-07 (Tampering — cross-version rc tag false-pass) | MEDIUM-9 parent-version filter + workflow comments documenting single-maintainer rc-tagging discipline |

T-05-16-05 (DinD socket access in dry-run runner) and T-05-16-06 (90-day artifact retention) were `accept` dispositions in the threat register — applied via the existing DinD pattern in `hack/scripts/dry-run-v1.sh` (no `--privileged`) + the explicit `retention-days: 90` on the upload-artifact step.

## Deviations from Plan

None — both tasks executed exactly per the PLAN.md `<action>` specifications. The plan was prescriptive (canonical YAML body provided in the action blocks for both jobs) and aligned cleanly with RESEARCH §"Helm OCI Publish" + §"Topic 1" + §"Topic 9" + PATTERNS §"P6.1" + the existing ci.yaml `helm-lint` analog.

The verbatim YAML shapes from RESEARCH lines 530-571 (chart-publish) + lines 1069-1098 (helmify-verify) + lines 1302-1330 (two-workflow pattern + parent-version filter) were lifted into release.yaml + dry-run.yaml with the documentation comment expansion (the canonical RESEARCH snippets are minimal; the committed workflows include the cross-workflow contract docs that operators reading the files will need).

## Workflow job inventory

**.github/workflows/release.yaml** — 4 jobs:
1. `helmify-verify` (timeout 5m, contents:read)
2. `pre-flight` (timeout 3m, contents:read + actions:read, conditional on non-rc)
3. `release` (timeout 15m, contents:write, conditional on non-rc, gated on helmify-verify + pre-flight)
4. `chart-publish` (timeout 5m, contents:read + packages:write, conditional on non-rc, gated on release)

**.github/workflows/dry-run.yaml** — 1 job:
1. `dry-run` (timeout 35m = 30-min budget + 5-min buffer, contents:read)

## YAML lint exit codes

```
$ python3 -c "import yaml; yaml.safe_load(open('.github/workflows/release.yaml'))" ; echo $?
0

$ python3 -c "import yaml; yaml.safe_load(open('.github/workflows/dry-run.yaml'))" ; echo $?
0
```

actionlint + yamllint not installed in the worktree env; PyYAML 6.0.3 used as the validator per the plan's `<verify>` block specifying `python3 -c "import yaml; yaml.safe_load(...)"`.

## MEDIUM-9 parent-version filter verification

```
$ grep -A 3 'PARENT_VERSION' .github/workflows/release.yaml | head -20
          PARENT_VERSION="${RELEASE_TAG#v}"  # strip leading 'v' -> "1.0.0"

          # Find the latest rc tag matching this specific parent version.
          # Pattern: v1.0.0-rc.* (NOT v1.0.1-rc.*, NOT v2.0.0-rc.*).
          # --sort=-version:refname picks the highest rc.N for that parent.
          MATCHING_RC=$(git tag --list "v${PARENT_VERSION}-rc.*" --sort=-version:refname | head -1)
```

Behavior matrix:
- Release `v1.0.0`, rc tags `v1.0.0-rc.1`, `v1.0.0-rc.2`, `v2.0.0-rc.1` → matches `v1.0.0-rc.2` (correct: highest rc for parent 1.0.0).
- Release `v1.0.0`, rc tags `v2.0.0-rc.1` only → matches nothing → fails (correct: stale rc from different version doesn't satisfy the gate; pre-MEDIUM-9 implementation would have false-passed against `v2.0.0-rc.1`).
- Release `v1.0.0`, no rc tags → matches nothing → fails (correct: enforces rc dry-run discipline).

## Commits

| Hash | Subject |
| --- | --- |
| `66122dc` | feat(05-16): extend release.yaml with helmify-verify, pre-flight, chart-publish jobs |
| `c4d0639` | feat(05-16): add dry-run.yaml workflow firing on v*-rc.* tags |

## Self-Check: PASSED

- File `.github/workflows/release.yaml` exists and is YAML-valid (217-line net diff including documentation comments).
- File `.github/workflows/dry-run.yaml` exists and is YAML-valid (110 lines).
- Commit `66122dc` exists in git log (release.yaml task).
- Commit `c4d0639` exists in git log (dry-run.yaml task).
- All success criteria from the PLAN.md `<success_criteria>` block met (job counts, no schedule cron, packages: write present, MEDIUM-9 filter applied, YAML lint clean).
- No Rule-1/Rule-2/Rule-3 deviations recorded; no pre-existing issues out of scope encountered.
