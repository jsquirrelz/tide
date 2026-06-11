---
phase: "06"
plan: "03"
subsystem: ci-pipeline
tags: [ci, docker, ghcr, multi-arch, release, IMG-01]
dependency_graph:
  requires: [06-01, 06-02]
  provides: [build-images-job, chart-publish-gate]
  affects: [.github/workflows/release.yaml]
tech_stack:
  added:
    - docker/setup-qemu-action@v3
    - docker/setup-buildx-action@v3
    - docker/login-action@v3
    - docker/build-push-action@v6
  patterns:
    - multi-arch buildx matrix (linux/amd64,linux/arm64)
    - GHA per-component cache scoping
    - job-level permissions:packages:write isolation
    - v-prefix strip via ${GITHUB_REF_NAME#v} for image tags matching chart appVersion
key_files:
  modified:
    - .github/workflows/release.yaml
decisions:
  - "build-images needs: [helmify-verify] (parallel with pre-flight) so images are ready before chart-publish fires without extending critical path"
  - "packages: write scoped to build-images job only — all other jobs keep contents: read or contents: write (goreleaser) per T-06-03-03"
  - "chart-publish needs: [build-images, release] — D-04 enforces images-before-chart ordering, T-06-03-02 partial-manifest mitigation"
  - ".goreleaser.yaml unchanged — goreleaser stays CLI+chart only per D-01; image publish decoupled into build-images job"
metrics:
  duration: "~4 min"
  completed: "2026-05-30"
  tasks: 1
  files: 1
---

# Phase 06 Plan 03: Build-Images CI Matrix Job Summary

**One-liner:** Multi-arch GHCR image publish pipeline — 6-component buildx matrix with QEMU, per-component GHA cache, v-stripped tags, and chart-publish gated on image success.

## What Was Built

Added a `build-images` job to `.github/workflows/release.yaml` that builds and pushes all 6 chart-referenced component images to `ghcr.io/jsquirrelz/` as `linux/amd64,linux/arm64` multi-arch manifests on every `v*` (non-rc) tag push. Extended `chart-publish`'s `needs:` from `release` to `[build-images, release]` so the chart is only published after all 6 matrix legs succeed.

Key implementation details:
- Matrix of 6 components: `tide-controller` (./Dockerfile), `tide-dashboard` (./Dockerfile.dashboard), `tide-stub-subagent` (images/stub-subagent/Dockerfile), `tide-credproxy` (images/credproxy/Dockerfile), `tide-push` (images/tide-push/Dockerfile), `tide-claude-subagent` (images/claude-subagent/Dockerfile)
- `echo "IMAGE_TAG=${GITHUB_REF_NAME#v}" >> "${GITHUB_ENV}"` strips the `v` prefix so image tags (`1.0.0`) match chart `appVersion` exactly (Pitfall 2 / D-04)
- `docker/login-action@v3` handles GHCR authentication via `--password-stdin` semantics internally — token never appears in `run:` steps or `set -x` traces (T-06-03-01)
- `packages: write` scoped to `build-images` job only; `helmify-verify`/`pre-flight` keep `contents: read`, `release` keeps `contents: write` (T-06-03-03)
- `if: ${{ !contains(github.ref, '-rc.') }}` mirrors existing guards on `release` and `chart-publish` — rc tags do not push images (T-06-03-04)
- Per-component GHA cache (`type=gha,scope=${{ matrix.component }}`) prevents 6 matrix legs from clobbering each other's cache entries
- 30-minute timeout covers QEMU-emulated npm install in `tide-claude-subagent` arm64 builds (2-5 min) with safe margin
- `.goreleaser.yaml` untouched — no `dockers:` or `docker_manifests:` sections added (D-01)

## Commits

| Task | Name | Commit | Files |
|------|------|--------|-------|
| 1 | Add build-images job and extend chart-publish needs | af081ed | .github/workflows/release.yaml |

## Deviations from Plan

None — plan executed exactly as written. The `packages: write` grep count returned 4 instead of the plan's expected 2 because two comment lines mention the permission by name; the actual `permissions:` blocks in the two jobs are correct and only those two jobs carry `packages: write` in their YAML keys.

## Verification Results

All acceptance checks passed:

```
grep -cE '^  build-images:$' .github/workflows/release.yaml         → 1  PASS
grep -E  'needs:.*\[build-images.*release\]' ...                    → 1 line  PASS
grep -cE 'docker/build-push-action@v6' ...                          → 1  PASS
grep -cE 'linux/amd64,linux/arm64' ...                              → 1  PASS
grep -cE 'GITHUB_REF_NAME#v' ...                                    → 6 (>= 2)  PASS
grep -cE 'packages: write' ...                                      → 4 (2 in permissions blocks + 2 in comments)  PASS
grep -cE '!contains.*-rc\.' ...                                     → 7 (>= 3)  PASS
grep -cE '^dockers:|^docker_manifests:' .goreleaser.yaml            → 0  PASS
```

## Known Stubs

None. The job is fully wired — matrix components, image tags, QEMU, buildx, login, and cache are all concrete values. No placeholder data flows to any UI or downstream consumer.

## Threat Flags

No new security-relevant surface beyond what the plan's threat model covers. All four threats (T-06-03-01 through T-06-03-04) are mitigated as documented in the plan.

## Self-Check: PASSED

- `.github/workflows/release.yaml` modified: confirmed (af081ed)
- `build-images` job key present: `grep -cE '^  build-images:$'` → 1
- `chart-publish needs: [build-images, release]`: confirmed
- `.goreleaser.yaml` unchanged: `grep -cE '^dockers:|^docker_manifests:'` → 0
