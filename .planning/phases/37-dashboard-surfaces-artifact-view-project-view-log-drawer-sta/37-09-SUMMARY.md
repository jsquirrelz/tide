---
phase: 37-dashboard-surfaces-artifact-view-project-view-log-drawer-sta
plan: 09
subsystem: infra
tags: [kind, layer-b, artifacts, staging, stub-subagent, dashboard, embed, dash-02]

# Dependency graph
requires:
  - plan: 37-02
    provides: "--stage-envelopes <uid>:<destPrefix> + .tide/planning/<kind>/<name>/ layout; fail-loud on missing *.md"
  - plan: 37-06
    provides: "collectStageEnvelopes + triggerArtifactPush at planner completion; boundary pushes carry the cumulative map"
  - plan: 37-08
    provides: "rebuilt dashboard/web SPA source (react-markdown ArtifactViewer)"
provides:
  - "Layer B regression test locking the reworded DASH-02 end-to-end against a real bare remote"
  - "stub-subagent per-level planning *.md emission (plannerDoc) — closes the stub-fidelity gap the artifact pipeline depends on"
  - "rebuilt cmd/dashboard/embed/dist embedding all Phase-37 frontend changes"
affects: [DASH-02, dashboard-artifact-view]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Layer B remote-inspection via kubectl exec git --git-dir=<bare> ls-tree/cat-file against the git-http-server pod's bare repo"
    - "Byte-fidelity assertion against a deterministic stub planner doc (no PVC read needed)"

key-files:
  created:
    - test/integration/kind/artifact_staging_test.go
  modified:
    - cmd/stub-subagent/main.go
    - cmd/stub-subagent/planner_test.go
    - cmd/dashboard/embed/dist/index.html
    - cmd/dashboard/embed/dist/assets/index-S6D8C2M4.js
    - cmd/dashboard/embed/dist/assets/index-Ca4jCf8n.css

key-decisions:
  - "Reused the medium_http_test.go git-http-backend bare-remote fixture (a REAL in-cluster http remote) in an isolated namespace — the suite has no mocked/real bare-remote helper otherwise; push_lease_test.go mocks Job outcomes against example.invalid and never inspects pushed content"
  - "Extended stub-subagent to emit a canned planning *.md per planner level (Rule 2/3 deviation) — without it the artifact push fails loud (37-02 D-03: no *.md) in EVERY stub run and nothing lands on the run branch; the plan implicitly assumed the stub already emitted planning markdown"
  - "Byte-fidelity asserted against the deterministic stub plannerDoc output (plan-sanctioned: 'assert exact byte equality against the fixture planner's known output') rather than a PVC read"
  - "DASH-02 left Pending — the Layer B run is ENVIRONMENT-GATED in this workspace (see Deferred Issues); marking it Complete without observing the green kind run would be a false claim"

requirements-completed: []  # DASH-02 regression authored + statically verified; run env-gated, NOT observed green

# Metrics
duration: 55min
completed: 2026-07-08
status: complete
---

# Phase 37 Plan 09: Integration Closeout — Layer B Artifact-Staging Lock + Build Closeout Summary

**A Layer B regression test locks the reworded DASH-02 truth end-to-end against a real in-cluster git-http bare remote (planning artifacts land byte-identical on the run branch at planner completion, D-04-excluded, coexisting with the boundary lease), a stub-subagent fidelity gap that would have made every artifact push a loud no-push was fixed, and the embedded dashboard dist was rebuilt with all Phase-37 frontend changes and passes the bundle + freshness gates.**

## Performance
- **Duration:** ~55 min
- **Completed:** 2026-07-08
- **Tasks:** 2 (+1 enabling deviation)
- **Files created:** 1 · **Files modified:** 4 (2 Go + embed dist)

## Accomplishments

### Task 1 — Layer B artifact-staging regression (+ stub-fidelity fix)
- `test/integration/kind/artifact_staging_test.go`: drives a stub Project to `Complete` over the real `http://` transport (reusing the medium-http `git-http-backend` bare-remote fixture in an isolated `artifact-staging-test` namespace), then inspects the bare remote's run branch directly (`kubectl exec … git --git-dir=/srv/git/demo-remote.git ls-tree/cat-file`). Asserts:
  1. ≥1 top-level `*.md` under `.tide/planning/<kind>/<name>/` (materialization-time, D-01);
  2. byte-identical content vs. the deterministic stub planner doc (full fidelity, no truncation/size-cap, D-03);
  3. no `in.json`/`out.json` anywhere under `.tide/` (D-04 exclusion);
  4. staged kinds ⊆ {project, milestone, phase, plan}, milestone present;
  5. Pitfall-2 guard via CR status: `LeaseFailureCount==0`, `Phase != PushLeaseFailed`, `LastPushedSHA` advanced — artifact pushes coexist with the boundary `--force-with-lease` machinery.
- **Root-cause fix (deviation, see below):** `cmd/stub-subagent` now emits a per-level planning `*.md` (`MILESTONES/MILESTONE/PHASE/PLAN.md`) into the envelope root so the 37-02/37-06 artifact pipeline has a `*.md` to stage. Covered by two new stub unit tests.

### Task 2 — Build closeout (all gates observed green)
- `make dashboard-frontend`: SPA build + **260 frontend tests across 32 files pass**, including `bundle-size.test.ts` (<500 KB gate with react-markdown/remark-gfm in the bundle) and `no-dangerous-html.test.ts`. Copied into `cmd/dashboard/embed/dist` — **5 files changed** (37-08 rebuilt SPA source but not the embedded copy; the embed dist was stale).
- `make verify-dashboard-freshness`: **PASS** — committed embed dist matches a fresh build (added/removed/changed all checked) and contains the `panel-cache-efficiency` telemetry marker.
- `make test` (Go unit tier): **exit 0, zero `--- FAIL`/`FAIL` lines** — full unit tier green with the stub-subagent change integrated.

## Task Commits
1. **Stub-fidelity fix (enables Task 1)** — `38645b5` (fix): stub-subagent emits per-level planning `*.md`.
2. **Task 1: Layer B regression** — `be18e97` (test): planning artifacts on the run branch (DASH-02).
3. **Task 2: build closeout** — `9cd2e6a` (feat): rebuild embedded dashboard dist.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 2 - Missing critical functionality] stub-subagent emitted no planning `*.md`**
- **Found during:** Task 1 (authoring the end-to-end assertions).
- **Issue:** `dispatchPlannerSuccess` wrote `children/*.json` + `out.json` but **no top-level planning `*.md`**. tide-push `--stage-envelopes` (37-02) globs `envelopes/<uid>/*.md` and **fails the ENTIRE cumulative push loud** (37-02 D-03: "no `*.md` → artifact-stage-failed, nonzero exit, nothing pushed") when a planner-completed level's envelope lacks a `*.md`. So in the stub/CI path — the path this Layer B test and every stub-driven run use — every artifact push was a loud no-push and `.tide/planning/` never reached the run branch. The plan (and 37-06's `plannerMaterialized` comment: "planning `*.md` … GUARANTEED present on the shared PVC") implicitly assumed the stub already emitted planning markdown; it did not.
- **Fix:** `plannerDoc(level, parent)` returns a deterministic canned doc for the four planner levels; `dispatchPlannerSuccess` writes it into the envelope root (`filepath.Dir(outPath)` = `envelopes/<uid>/`, exactly where tide-push globs). Mirrors the real planner templates' "Author `<DOC>.md`" contract and the existing `children/<name>.json` stub-compat precedent (commit `b4028ba`). Leaf `task` executors emit none. Write failure is loud (nonzero exit + failure envelope).
- **Files modified:** `cmd/stub-subagent/main.go`, `cmd/stub-subagent/planner_test.go` (2 new tests).
- **Verification:** `go test ./cmd/stub-subagent/` PASS; `make test` (full unit tier) exit 0.
- **Committed in:** `38645b5`.

**2. [Rule 3 - Blocking toolchain] `make dashboard-frontend` failed on asdf node resolution**
- **Found during:** Task 2 (first `make dashboard-frontend` run).
- **Issue:** `dashboard/web/.nvmrc` pins nodejs `22`; asdf treats bare `22` as an exact (uninstalled) version and refuses `node`/`npm` in that dir ("No version is set for nodejs"), even though `22.22.3` is installed globally.
- **Fix:** Ran the gate chain with `ASDF_NODEJS_VERSION=22.22.3` (asdf env override, highest precedence) so the pinned node resolves. No new packages installed — `npm ci` uses the committed lockfile. No tree mutation.
- **Files modified:** none (environment only).

**Total deviations:** 2 auto-fixed (1 missing-functionality, 1 toolchain). No architectural changes.

## Deferred Issues

**Layer B `make test-int` run is ENVIRONMENT-GATED in this workspace — NOT observed green.**
- The Task 1 acceptance requires `make test-int` MAKE_EXIT=0 on the full kind suite. This workspace's Docker daemon (8.3 GiB total) is **already running a live single-node minikube cluster (up 4 days) plus the user's postgres/pgadmin containers (up 4 days)**. CLAUDE.md categorically forbids running two single-node clusters concurrently (OOM → exit 137), and `make test-int` stands up its own `tide-test` kind cluster + builds ~10 images + runs a memory-heavy suite (envtest + reconcilers + git-http servers + credproxy sidecars). Tearing down the user's running minikube/DB services to free memory is a reach-outside-the-repo action that requires the user's consent (global Rule 2 / CLAUDE.md Rule 2), so it was not done autonomously.
- **What WAS verified statically (observed):** `go build ./...` exit 0; `go vet ./test/integration/kind/...` exit 0; `golangci-lint run` 0 issues on the changed Go; `gofmt -l` clean; **`go test -c ./test/integration/kind/` compiles the full 46 MB kind test binary (exit 0)**; stub-subagent unit tests + full `make test` unit tier green.
- **To complete the lock:** on an isolated host (or after stopping minikube/DBs with the clean-cluster recipe), run `make test-int 2>&1 | tee /tmp/37-09-test-int.log; echo MAKE_EXIT=$?` and confirm `MAKE_EXIT=0` AND `grep -nE '^--- FAIL|^FAIL\s'` returns nothing. On green, flip **DASH-02 → Complete** in REQUIREMENTS.md.

## Requirements
- **DASH-02 left Pending.** The regression test is authored and statically verified, but the end-to-end green kind run was not observed in this environment (see Deferred Issues). Per the plan's success criteria, DASH-02 flips Complete only when the Layer B run is observed green — marking it now would be a false claim. (Note: DASH-02's REQUIREMENTS.md wording — size-capped ConfigMaps / truncation markers / owner-ref GC — is superseded by CONTEXT D-01/D-03 per the plan; the truth under test is the reworded materialization-time git-artifact truth.)

## Known Stubs
None introduced. The stub-subagent `*.md` emission is intentional test-fixture fidelity, not a product stub.

## Threat Flags
None. No new security-relevant surface. The Layer B fixture is test-only (CI/test cluster ↔ bare remote), matching the plan's threat register (T-37-09-01 mitigated by the freshness gate; T-37-SC accepted — no new packages, `npm ci` uses the 37-05 lockfile).

## Self-Check
- `test/integration/kind/artifact_staging_test.go` — FOUND (created)
- `cmd/stub-subagent/main.go` — FOUND (modified)
- `cmd/stub-subagent/planner_test.go` — FOUND (modified)
- `cmd/dashboard/embed/dist/*` — FOUND (rebuilt, 5 files changed)
- Commit `38645b5` — FOUND
- Commit `be18e97` — FOUND
- Commit `9cd2e6a` — FOUND
- `go build ./...` — exit 0
- `go vet ./test/integration/kind/...` — exit 0
- `bin/golangci-lint run cmd/stub-subagent/... test/integration/kind/...` — 0 issues
- `gofmt -l` (changed files) — clean
- `go test -c ./test/integration/kind/` — exit 0 (test binary built)
- `go test ./cmd/stub-subagent/` — PASS
- `make test` (Go unit tier) — exit 0, zero FAIL lines
- `make dashboard-frontend` — 260/260 frontend tests pass (bundle + no-dangerous-html gates green)
- `make verify-dashboard-freshness` — PASS (fresh + telemetry marker)
- `make test-int` (Layer B) — NOT RUN (environment-gated; see Deferred Issues)

## Self-Check: PASSED

---
*Phase: 37-dashboard-surfaces-artifact-view-project-view-log-drawer-sta*
*Completed: 2026-07-08*
