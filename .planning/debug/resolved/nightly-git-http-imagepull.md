---
slug: nightly-git-http-imagepull
status: resolved
trigger: "nightly-integration red since 2026-06-11 — Layer B kind suite (make test-int) fails"
created: 2026-06-14
updated: 2026-06-14
---

# Debug Session: nightly-git-http-imagepull

## Symptoms

- **Expected:** nightly-integration's "Heavy kind suites" job passes `make test-int` (Layer A envtest + Layer B kind).
- **Actual:** Red since 2026-06-11 (green Jun 10). Layer A (38 envtest specs) passes; Layer B fails on ONE spec — `medium_http_test.go:283 "git-http server Deployment is Available"` — `kubectl wait` times out after 120s; the downstream "medium Project reaches Complete" spec is then Skipped. `make test-int` exits 2.
- **Runtime evidence (kind-logs artifact, run 27493107044):** the `git-http-server` Deployment pod is stuck `ImagePullBackOff`:
  `failed to pull "ghcr.io/jsquirrelz/tide-git-http-server:1.0.0": failed to fetch anonymous token: 403 Forbidden`.

## Root Cause

`tide-git-http-server` is a **private** GHCR package (the v1.0.0 public-flip covered the 7 prod images + 2 charts, not this test fixture). The deployment uses `imagePullPolicy: IfNotPresent`, and the test's `loadImageIfNeeded()` only `kind load`s an image if it exists **locally**. The nightly "SC-1 image smoke" step builds `tide-push`, `tide-claude-subagent`, and `tide-demo-init` locally — but **omitted `tide-git-http-server`**. So that fixture alone falls through to a GHCR pull, which 403s on the private package → ImagePullBackOff → deployment never Available → spec timeout.

Broke 2026-06-11 because that is when the `:1.0.0` images were first pushed to GHCR (v1.0.0 release); before that the suite resolved the fixture differently. The three sibling fixtures pass precisely because they are built locally and never pulled — `tide-demo-init` is *also* private yet works for exactly this reason.

NOT caused by any v1.0.1 code change; the controller logs in the dump (admission webhook denials, etc.) are expected test behavior.

## Fix

Add `docker build -t ghcr.io/jsquirrelz/tide-git-http-server:1.0.0 -f images/tide-git-http-server/Dockerfile .` to the nightly image-prep step, mirroring the three existing fixture builds. Convention-consistent; no outward-facing GHCR-visibility change required for the test to pass.

files_changed: [.github/workflows/nightly-integration.yml]
verification: workflow_dispatch re-run of nightly-integration goes green on the Layer B medium_http spec (the git-http-server pod now loads from the locally-built image instead of pulling the private GHCR package).

## Open follow-up (separate from nightly green)

The public `examples/projects/medium/` references `ghcr.io/jsquirrelz/tide-git-http-server:1.0.0` and `tide-demo-init:1.0.0`. If external users are meant to **pull** these (vs build locally), both packages should be made public — a release/publishing-posture decision, tracked toward the v1.0.1 release, not required for nightly.

## Evidence

- timestamp: 2026-06-14 — kind-logs (run 27493107044) kubelet.log: `ErrImagePull`/`ImagePullBackOff` on `git-http-server-*` pod, `403 Forbidden` anonymous token for `tide-git-http-server:1.0.0`. `demo-remote-init` pod (tide-demo-init) ran OK in the same cluster.
- timestamp: 2026-06-14 — `.github/workflows/nightly-integration.yml` lines 100-105 build push/claude-subagent/demo-init only; `medium_http_test.go:183` `loadImageIfNeeded` skips images not present locally. `images/tide-git-http-server/Dockerfile` exists.

## Eliminated

- Not a Phase 12-17 code regression (Layer A green; controller behaves correctly).
- Not the Phase 15 receive-pack fixture change (that affects the later push spec, not Deployment Availability).
