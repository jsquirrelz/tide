---
phase: 40-deprecate-v1alpha1-api
plan: 07
subsystem: infra
tags: [makefile, ci, grep-gate, regression-guard, kubebuilder, crd]

# Dependency graph
requires:
  - phase: 40-deprecate-v1alpha1-api (plans 40-01..40-06)
    provides: v1alpha3-only CRD set, all consumers migrated, docs/samples swept, envelope decoupled to dispatch.tideproject.k8s/v1alpha1
provides:
  - "verify-no-legacy-api-refs Makefile gate: durable, CI-wired, self-match-proof, provably-alive regression guard against v1alpha1/v1alpha2 reintroduction"
  - "CI wiring in .github/workflows/ci.yaml alongside verify-no-aggregates"
  - "~25 real Phase-40 leftover fixes the gate's own sweep surfaced (stale doc comments, one broken kind-suite test, two broken kind_e2e fixtures)"
affects: [gsd-verify-work, future-crank-phases]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Self-match-proof grep gate: version tokens built via shell $$PAT concatenation so the recipe cannot flag its own definition"
    - "Negative-test literals (proving a validator/guard REJECTS a stale value) are file-scoped exclusions, not repo-wide content filters — a real reintroduction elsewhere still fails the gate"

key-files:
  created: []
  modified:
    - Makefile
    - .github/workflows/ci.yaml
    - test/integration/kind/baseref_crd_render_test.go
    - test/e2e/dashboard_test.go
    - test/e2e/gate_flow_test.go
    - images/tide-import/Dockerfile
    - images/tide-reporter/Dockerfile
    - internal/eval/doc.go
    - internal/credproxy/server.go
    - pkg/dispatch/childcrd.go
    - pkg/dispatch/doc.go
    - pkg/dispatch/envelope.go
    - pkg/dispatch/envelope_test.go
    - internal/controller/task_controller.go
    - internal/controller/task_controller_extracted_test.go
    - cmd/manager/main.go
    - cmd/manager/metrics_test.go
    - cmd/dashboard/api/informer_bridge.go
    - cmd/tide-import/main_test.go
    - krew-plugins/tide.yaml
    - api/v1alpha3/project_types.go
    - api/v1alpha3/task_types.go
    - api/v1alpha3/wave_types.go
    - api/v1alpha3/schema_test.go
    - test/schema/dogfood_manifests_test.go
    - examples/projects/dogfood/02-codex-runtime-project.yaml
    - examples/projects/dogfood/run-2/RUNBOOK.md
    - examples/projects/small/README.md

key-decisions:
  - "examples/projects/dogfood/salvage-{20260618,20260628}/ excluded from the gate by path (whole dirs) — dated historical dogfood-run archives, same D-12 preservation principle already established in plan 40-03 for this bundle's events.jsonl transcripts; verified every remaining match inside is historical narrative (captured envelope/events/plan-artifact content), not live/consumed config (the bundle's CR YAMLs, project.yaml, and seed-manifest.json were already converted to v1alpha3 in plan 40-03 and carry zero legacy references)"
  - "Two intentional negative-test literals (pkg/dispatch/envelope_test.go's bare pre-decoupling CRD-group string proving D-08 rejection; internal/controller/project_controller_v2_guard_test.go's wrong-value SchemaRevision proving D-04 rejection) are excluded via file-scoped patterns (path+content combined), not blanket content filters — so a real reintroduction of either literal anywhere else in the tree still fails the gate"
  - "Accurate schema-lineage comments that named a bare 'v1alpha2'/'v1alpha1' for historical context (api/v1alpha3/*.go doc comments, cmd/manager/main.go, cmd/dashboard/api/informer_bridge.go, pkg/dispatch/{doc,envelope,envelope_test}.go, krew-plugins/tide.yaml, the codex-runtime-project.yaml header, test/schema/dogfood_manifests_test.go) were reworded to drop the literal token while preserving full meaning, rather than adding more filters — keeps the gate's sanctioned-exclusion surface small and auditable"

requirements-completed: [CRANK-07]

# Metrics
duration: ~55min
completed: 2026-07-12
---

# Phase 40 Plan 07: Legacy API-Version Regression Gate + Full Suite Close Summary

**Added a self-match-proof `verify-no-legacy-api-refs` Makefile gate wired into CI, proved it alive with a live seed/remove cycle, and fixed ~25 real Phase-40 leftovers the gate's own sweep surfaced — including one silently-broken kind-suite test and two kind_e2e fixtures that would have been rejected outright by the now-v1alpha3-only cluster.**

## Performance

- **Duration:** ~55 min
- **Completed:** 2026-07-12T01:59:13Z
- **Tasks:** 2 (Task 1 complete with commit; Task 2 verification-only, no file changes, partially blocked — see below)
- **Files modified:** 28 (Task 1 commit `a39baa6`)

## Accomplishments

- **Durable regression gate.** `make verify-no-legacy-api-refs` encodes the phase's terminal end-state assertion ("zero v1alpha1/v1alpha2 references outside a sanctioned set") as a permanent, CI-wired check instead of a one-off closeout grep — the same failure class the dead `verify-no-aggregates` glob (D-12) illustrated for this exact phase.
- **Provably alive.** Seeded a scratch file with the old CRD group-version string (`tideproject.k8s/` + `v1alpha1` concatenated), ran the gate, observed exit 1 with the seed listed; removed the seed, ran again, observed exit 0. Both observations recorded below; the scratch file was never committed and is not present in the final tree.
- **Self-match-proof.** Every version-token pattern in the recipe is built at runtime via shell `$$PAT` concatenation. `grep -A20 'verify-no-legacy-api-refs:' Makefile | grep -c 'v1alpha1\|v1alpha2'` → `0`.
- **Real residual fixes, not blanket exclusions.** The gate's own sweep (run iteratively while designing the filter set) surfaced ~25 genuine Phase-40 leftovers — most notably one kind-suite test asserting the old transitional 2-version CRD shape (would have failed the very next `make test-int`) and two `kind_e2e` fixtures applying `apiVersion: tideproject.k8s/v1alpha1` Project/Milestone manifests with no `schemaRevision` (would be rejected outright by a v1alpha3-only cluster). All fixed at the source; see Deviations.
- **Narrow, justified exclusion set.** Beyond the plan's three pre-specified line filters (dispatch envelope group, migration-doc path mentions, external Krew group) and four pre-specified path exclusions (docs/migration, docs/audit, docs/superpowers, AGENTS.md), added exactly two more path exclusions (dated dogfood salvage archives, D-12) and two file-scoped negative-test-literal exclusions — every addition carries a one-line justification in the Makefile's comment block.

## Task Commits

1. **Task 1: verify-no-legacy-api-refs Makefile gate + CI wiring + liveness proof + residual-hit fixes** - `a39baa6` (feat)
2. **Task 2: Full phase gate — verify suite + kind Layer B** - no commit (verification-only per plan's `<files>none</files>`; partially blocked, see below)

**Plan metadata:** (this commit, made by the orchestrator after merge)

## Files Created/Modified

See frontmatter `key-files.modified` for the full list (28 files). Grouped by why:

- `Makefile`, `.github/workflows/ci.yaml` — the gate itself and its CI wiring (the plan's core deliverable).
- `test/integration/kind/baseref_crd_render_test.go` — fixed a test asserting the old 2-version-block CRD shape; renamed `TestHelmTideCRDsRenderBaseRefBothVersions` → `TestHelmTideCRDsRenderBaseRef`, counts `2`→`1`.
- `test/e2e/dashboard_test.go`, `test/e2e/gate_flow_test.go` — bumped `kind_e2e` fixture manifests from `tideproject.k8s/v1alpha1` (no `schemaRevision`) to `tideproject.k8s/v1alpha3` + `schemaRevision: v1alpha3`.
- `images/tide-import/Dockerfile`, `images/tide-reporter/Dockerfile` — build-context comments claiming these binaries import `api/v1alpha2`/`api/v1alpha1`; both actually import `api/v1alpha3` today (verified via grep on `cmd/tide-import/main.go` and `cmd/tide-reporter/main.go`).
- `internal/eval/doc.go`, `internal/credproxy/server.go`, `pkg/dispatch/childcrd.go`, `internal/controller/task_controller.go` (×2) — import-firewall / type-translation doc comments still naming `api/v1alpha1` as the package to avoid importing or translate from; updated to `api/v1alpha3`.
- `cmd/manager/metrics_test.go` — comment cited `api/v1alpha1/aggregates_guard_test.go` as a sibling static-assertion pattern; that file was deleted in plan 40-05 (its concern is now carried by the hardened `verify-no-aggregates` gate itself, per 40-05-SUMMARY.md). Reworded.
- `cmd/tide-import/main_test.go`, `internal/controller/task_controller_extracted_test.go` — minor stale-literal/unrealistic-fixture polish (a comment naming `v1alpha2` for a version-agnostic round-trip test; an envelope test fixture using a syntactically-unrealistic bare `"v1alpha1"` apiVersion, bumped to the real envelope literal).
- `examples/projects/dogfood/run-2/RUNBOOK.md`, `examples/projects/small/README.md` — forward-facing operator docs (a still-relevant runbook for a not-yet-completed dogfood run, and a troubleshooting example) carrying stale version literals that would mislead a future operator now that the cluster is v1alpha3-only.
- `api/v1alpha3/*.go`, `cmd/manager/main.go`, `cmd/dashboard/api/informer_bridge.go`, `pkg/dispatch/{doc,envelope,envelope_test}.go`, `krew-plugins/tide.yaml`, `examples/projects/dogfood/02-codex-runtime-project.yaml`, `test/schema/dogfood_manifests_test.go` — accurate schema-lineage/history comments reworded to drop the bare literal token while preserving full meaning (see key-decisions).

## Decisions Made

See frontmatter `key-decisions`. Summary: two new path exclusions (dated dogfood salvage archives) and two file-scoped negative-test-literal exclusions were added to the plan's pre-specified sanctioned set, each justified in the Makefile's own comment block; everything else that merely *mentioned* a legacy version for legitimate historical reasons was reworded to drop the literal rather than filtered, keeping the gate's exclusion surface small.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] Broken kind-suite test asserting the retired 2-version CRD shape**
- **Found during:** Task 1 (residual-hit sweep, before writing the Makefile gate)
- **Issue:** `test/integration/kind/baseref_crd_render_test.go`'s `TestHelmTideCRDsRenderBaseRefBothVersions` asserted `baseRef:`/`baseSHA:` each render exactly twice ("once per version block: v1alpha1 served:false + v1alpha2 served:true"). The Project CRD is now single-version (v1alpha3 only); a live `helm template` render confirmed only 1 occurrence today. This test would have failed the next `make test-int` run.
- **Fix:** Renamed to `TestHelmTideCRDsRenderBaseRef`; assertions and comment updated to expect count `1` (the sole v1alpha3 version block).
- **Files modified:** `test/integration/kind/baseref_crd_render_test.go`
- **Verification:** `go test ./test/integration/kind/... -run TestHelmTideCRDsRenderBaseRef -v` → PASS (no cluster required, plain `helm template` test).
- **Committed in:** `a39baa6`

**2. [Rule 1 - Bug] kind_e2e fixtures applying a rejected apiVersion**
- **Found during:** Task 1 residual-hit sweep
- **Issue:** `test/e2e/dashboard_test.go` and `test/e2e/gate_flow_test.go` (both `//go:build kind_e2e`) applied `apiVersion: tideproject.k8s/v1alpha1` Project (and, in gate_flow, Milestone) manifests with no `schemaRevision`, expecting `kindApplyYAML(...).To(Succeed())`. Since v1alpha1 no longer exists as a served CRD version, a real kind cluster would reject these applies outright.
- **Fix:** Bumped both fixtures to `apiVersion: tideproject.k8s/v1alpha3` + `spec.schemaRevision: v1alpha3` for the Project manifests; bumped the Milestone's apiVersion too.
- **Files modified:** `test/e2e/dashboard_test.go`, `test/e2e/gate_flow_test.go`
- **Verification:** `go build -tags=kind_e2e ./test/e2e/...` and `go vet -tags=kind_e2e ./test/e2e/...` both clean. Not runnable end-to-end in this environment (kind_e2e requires a live cluster; see Issues Encountered) — compile-and-shape verified only.
- **Committed in:** `a39baa6`

**3. [Rule 1 - Bug] Stale "imports: api/v1alphaN" Dockerfile build-context comments**
- **Found during:** Task 1 residual-hit sweep
- **Issue:** `images/tide-import/Dockerfile` claimed the binary imports `api/v1alpha2`; `images/tide-reporter/Dockerfile` claimed `api/v1alpha1`. Both packages were deleted in plan 40-05. Verified via grep that both `cmd/tide-import/main.go` and `cmd/tide-reporter/main.go` actually import `api/v1alpha3` today.
- **Fix:** Updated both Dockerfile comments to `api/v1alpha3`.
- **Files modified:** `images/tide-import/Dockerfile`, `images/tide-reporter/Dockerfile`
- **Verification:** grep-confirmed against actual current imports; no build-COPY-path change needed since `COPY api/ api/` already copies the whole (now single-version) api/ tree.
- **Committed in:** `a39baa6`

**4. [Rule 1 - Bug] Stale import-firewall/type-translation doc comments naming a deleted package**
- **Found during:** Task 1 residual-hit sweep
- **Issue:** Four production doc comments described a *current* architectural rule ("this package MUST NOT import api/v1alpha1", "translates api/v1alpha1.Caps → pkg/dispatch.Caps") naming a package that no longer exists.
- **Fix:** Updated `internal/eval/doc.go`, `internal/credproxy/server.go` (×2 sites), `pkg/dispatch/childcrd.go`, and `internal/controller/task_controller.go` (×2 sites) to name `api/v1alpha3`.
- **Files modified:** as listed
- **Verification:** `go build ./...`, `go vet ./...` clean; comment-only changes, no behavior change.
- **Committed in:** `a39baa6`

**5. [Rule 1 - Bug] Stale reference to a deleted test file**
- **Found during:** Task 1 residual-hit sweep
- **Issue:** `cmd/manager/metrics_test.go` cited `api/v1alpha1/aggregates_guard_test.go` as a sibling "static-assertion pattern" file. That file was deleted in plan 40-05 (its own SUMMARY documents the concern moved to the hardened `verify-no-aggregates` gate).
- **Fix:** Reworded to reference only the still-existing `cmd/manager/rbac_docs_test.go` sibling and note the retirement.
- **Files modified:** `cmd/manager/metrics_test.go`
- **Verification:** comment-only; `go vet ./...` clean.
- **Committed in:** `a39baa6`

**6. [Rule 1 - Bug] Stale operator-facing docs (runbook + troubleshooting example)**
- **Found during:** Task 1 residual-hit sweep
- **Issue:** `examples/projects/dogfood/run-2/RUNBOOK.md` (a still-live runbook for the not-yet-completed dogfood run #2) had a comment naming "v1alpha1→v1alpha2 envelope-conversion risk" and a `kubectl apply` comment saying "fresh v1alpha2 Project" — both stale now that `run-2/project.yaml` is v1alpha3 and the cluster is v1alpha3-only. `examples/projects/small/README.md`'s troubleshooting section showed an example `kubectl apply` rejection error citing `tideproject.k8s/v1alpha1`, which no longer matches what a real v1alpha3-only cluster would say.
- **Fix:** Updated both to v1alpha3-accurate wording.
- **Files modified:** `examples/projects/dogfood/run-2/RUNBOOK.md`, `examples/projects/small/README.md`
- **Verification:** doc-only; visually reviewed against the actual current `project.yaml`.
- **Committed in:** `a39baa6`

---

**Total deviations:** 6 auto-fixed groups (all Rule 1 — bugs: broken/stale test or doc content the phase's own end-state should have already corrected).
**Impact on plan:** All fixes are corrections of Phase-40-caused staleness (files this phase's earlier plans touched but didn't fully sweep), not new scope. No architectural changes. The two `kind_e2e` fixture fixes and the kind-suite test fix are genuine ship-blockers that would have surfaced on the next full/nightly run had they not been caught here.

## Issues Encountered

**Docker Desktop daemon unresponsive — kind Layer B (`make test-int`) could not be run in this environment.**

Task 2's plan explicitly requires a fresh-cluster `make test-int` run as the phase's terminal evidence. Attempting this:
- `kind get clusters` failed: `docker ps -a ... request returned 500 Internal Server Error`.
- Direct `docker ps` hung indefinitely; even `timeout 10 docker ps` (SIGTERM) did not terminate it — only `timeout -s KILL 8 docker ps` (SIGKILL) did, confirming the daemon socket is genuinely wedged, not transiently slow.
- `docker desktop status` reported `Status: running` (the Desktop app's own supervisor thinks it's healthy) despite the engine API being unresponsive — a split-brain state, not a "not started" state.
- `.worktrees/pr3-debug` is still a registered git worktree (branch `claude/run-integrity-integration-miss-gate-...`) matching a known, still-open hazard noted in this repo's CLAUDE.md: "something in that session's kind/fixture runs intermittently flips the shared .git/config core.bare to true... if it recurs after the pr3 session ends, open a /gsd:debug session." Heavy kind/Docker churn from that session is a plausible (unconfirmed) contributor to the current Docker daemon hang.

Restarting Docker Desktop would be a destructive, host-wide action (kills all running containers, including any concurrent worktree-agent's kind clusters) and reaches outside this plan's repo scope — per this repo's CLAUDE.md ("`kind delete cluster` outside an explicit clean-rerun sequence" requires confirmation) and the global working rules ("actions hard to undo or reach outside the repo" require confirmation), I did not restart it unilaterally.

**What WAS verified and is green** (everything that doesn't require Docker):
- `make verify-no-aggregates && make verify-no-legacy-api-refs && make verify-dispatch-imports && make verify-chart-reproducible` — all four exit 0.
- `go build ./...` — clean (after `make demo-fixture`, the pre-existing gitignored-embed generation step; unrelated to this plan).
- `go vet ./...` — clean.
- `make test` (unit + envtest/Layer-A tier) — **every package `ok`**, including `internal/controller` (83.7s) which carries the bulk of the envtest suite.
- `ls api/` → `v1alpha3` (exactly).
- `grep -c 'name: v1alpha' config/crd/bases/*.yaml` → six `1`s (single-version CRDs, confirmed).
- `go build -tags=kind_e2e ./test/e2e/...` and `go vet -tags=kind_e2e ./test/e2e/...` — clean (the two fixed e2e fixtures compile correctly with the tag).

**What is NOT verified:** the actual fresh-cluster `make test-int` run (Ginkgo Layer B kind suite + the plain go-test contract tests it bundles) and the `make test-e2e-kind` nightly suite. This is an infrastructure gap, not a code defect — recommend re-running `make test-int` once Docker Desktop is confirmed healthy (restart it, verify `docker ps` responds, then re-run), before `/gsd:verify-work` treats Phase 40's kind Layer B evidence as closed. If the hang recurs after the `pr3-debug` worktree session ends, this repo's own CLAUDE.md already recommends opening a `/gsd:debug` session for it — that investigation is out of scope for this plan.

## User Setup Required

None - no external service configuration required. Operator action needed only to unblock the deferred kind Layer B verification: confirm Docker Desktop is healthy (`docker ps` responds) and re-run `make test-int`, reading `MAKE_EXIT` and grepping `^--- FAIL|^FAIL\s` per this repo's verdict protocol — not just the Ginkgo summary.

## Next Phase Readiness

The durable regression gate and CI wiring are complete and merged; all fast/unit verification is green; the phase's structural end-state (6 single-version CRDs, `api/` v1alpha3-only, zero legacy references outside a small justified set) is confirmed. The one open item is the fresh-cluster kind Layer B run, blocked on this environment's Docker Desktop daemon being unresponsive — not a code or plan defect. `/gsd:verify-work` should re-run `make test-int` once Docker recovers before signing off the phase's kind-suite evidence; everything else in this plan's acceptance criteria is met and evidenced above.

---
*Phase: 40-deprecate-v1alpha1-api*
*Completed: 2026-07-12*
