---
phase: 53-chart-config-dashboard-provenance-surfacing
plan: 09
subsystem: testing
tags: [kind, helm, ginkgo, golangci-lint, vitest, gocyclo, goconst]

# Dependency graph
requires:
  - phase: 53-05
    provides: verify-posture-configmap.yaml (D-05 sticky marker) + deployment.yaml ARGS53 install/upgrade posture logic
  - phase: 53-06
    provides: D-04 enablement AND-gate at the three verifier dispatch chokepoints (raised gateChecks' cyclomatic complexity, fixed here)
  - phase: 53-08
    provides: verifier_concurrency_test.go (Plan 51-08's ESC-04 spec, whose VerifierImage-gap assumptions this plan's fix updates)
  - phase: 53-10
    provides: verdict-final findings-push trigger (further raised gateChecks' complexity, fixed here)
  - phase: 53-11
    provides: prior wave's tracking/state close-out this plan builds on
provides:
  - "test/integration/kind/verify_posture_sticky_test.go — live kind proof of CFG-02 sticky verify-tier posture across a real helm install/upgrade cycle"
  - "D-10 phase-gate closeout: lint / chart-reproducibility / dashboard-freshness / frontend-suite / full test-int all green in-phase, zero known ci.yaml-only debt"
affects: [release-preflight, dashboard-provenance, verify-tier-config]

tech-stack:
  added: []
  patterns:
    - "Isolated throwaway helm release (own release name + namespace, Serial-decorated) for a live install/upgrade posture proof, never touching the shared suite release"
    - "Immediate post-install/upgrade deletion of a throwaway release's own ValidatingWebhookConfiguration to close a cluster-wide admission-webhook blast-radius gap namespace isolation alone doesn't cover"
    - "BeElementOf(Verifying, Succeeded, Failed) instead of exact Equal(Verifying) when polling a transient phase that a fast reconcile loop can race past between poll intervals"

key-files:
  created:
    - test/integration/kind/verify_posture_sticky_test.go
  modified:
    - internal/controller/task_controller.go
    - cmd/stub-subagent/main.go
    - dashboard/web/src/components/ArtifactViewer.test.tsx
    - test/integration/kind/verifier_concurrency_test.go

key-decisions:
  - "Closed a webhook-blast-radius gap the plan's own namespace/release isolation didn't cover: the chart's ValidatingWebhookConfiguration is cluster-scoped, failurePolicy=Fail, no namespaceSelector — deletes the throwaway release's own uniquely-named webhook config immediately after every helm install/upgrade, before any other work, rather than leaving it registered for the whole test."
  - "Fixed the pre-existing (Phase-50-era, not phase-53-caused) goconst violation in cmd/stub-subagent/main.go and the phase-53-introduced (53-06/53-10) gocyclo violation in task_controller.go's gateChecks — Task 2's own <files> field explicitly authorizes fixing any red gate regardless of origin, and the plan's acceptance criteria requires gates 1-4 exit 0 with no archaeology carve-out (unlike gate 5)."
  - "Root-fixed the ArtifactViewer.test.tsx flake (a real React effect-ordering race in the jsdom test environment, not touched by any phase-53 commit) rather than treating it as pre-existing debt to defer — NO FLAKE TOLERANCE is the project's own stated philosophy for exactly this class of problem (Makefile test-int comment), and it was blocking a clean one-pass gates-1-4 run."
  - "Updated verifier_concurrency_test.go's stale KNOWN GAP assumptions after git-archaeologying that an earlier phase-53 plan (CFG-01 chart wiring + cmd/manager/main.go env read) had already closed the VerifierImage gap the spec's header comment still described as open — the fast verify loop now races past the transient Verifying phase between 3s polls, so the assertion needed BeElementOf(Verifying, Succeeded, Failed) instead of exact equality."

requirements-completed: [CFG-02]

duration: 165min
completed: 2026-07-21
---

# Phase 53 Plan 09: D-06 Live Sticky-Posture Proof + D-10 Phase-Gate Closeout Summary

**Live kind-cluster proof that CFG-02's install/upgrade posture stickiness holds against a real cluster, plus a full D-10 ci.yaml-gate sweep that surfaced and root-fixed one phase-53 gocyclo regression, one pre-existing goconst violation, one real React test-environment race, and one stale test assumption from an earlier phase-53 plan closing the VerifierImage gap.**

## Performance

- **Duration:** ~165 min (includes three full ~33min `make test-int` kind-suite runs)
- **Started:** 2026-07-21T06:00:00Z (approx)
- **Completed:** 2026-07-21T08:26:00Z
- **Tasks:** 2
- **Files modified:** 5 (1 created, 4 modified)

## Accomplishments

- Live-proved CFG-02's core claim on a real kind cluster: a fresh `helm install` mints the `tide-verify-posture` marker and renders `--verify-levels-json`; a same-release `helm upgrade` with no override keeps it ON (sticky); deleting the marker and upgrading again (simulating a pre-v1.0.9 install) turns the tier OFF and confirms an upgrade can never mint new lineage (`IsInstall=false`) — all three legs run in ~19s isolated from the shared suite release.
- Discovered and closed a genuine DoS gap in the isolation design itself: the throwaway release's own `ValidatingWebhookConfiguration` is cluster-scoped with `failurePolicy: Fail` and no `namespaceSelector`, so it would intercept Plan/Project/Wave admission requests cluster-wide while its unhealthy backend has no ready endpoint. Closed by deleting it immediately after every helm operation on the throwaway release.
- Ran the full D-10 gate suite (`make lint`, `make verify-chart-reproducible`, `make verify-dashboard-freshness`, `npm test`, `make test-int`) and root-fixed everything red instead of deferring to release pre-flight, per the v1.0.8 release-cascade lesson this D-10 task exists to enforce.
- Root-caused and fixed a real React effect-ordering race in `ArtifactViewer.test.tsx` (not a phase-53 regression, but blocking a clean gates-1-4 run) rather than accepting the ~50% flake rate.
- Git-archaeologied a Layer B kind failure to its true cause: an earlier phase-53 plan had already closed the `VerifierImage` chart-wiring gap `verifier_concurrency_test.go`'s header comment still described as open, so the now-fast verify loop could race a Task past the transient `Verifying` phase between polls — fixed the assertion and the stale documentation together.

## Task Commits

1. **Task 1: Isolated kind sticky-posture proof** - `977d94da` (test)
2. **Task 2: D-10 phase-gate closeout** - `51786947` (fix)

_No separate plan-metadata commit — this SUMMARY + STATE/ROADMAP updates are handled by the orchestrator after wave merge (worktree mode)._

## Files Created/Modified

- `test/integration/kind/verify_posture_sticky_test.go` — new Ginkgo spec (Label("kind"), Serial), proves CFG-02's sticky posture across install/upgrade on an isolated `tide-posture` release; also closes the webhook-blast-radius gap described above.
- `internal/controller/task_controller.go` — extracted `handleVerifyHaltedTerminal` and `handleFailedTerminal` out of `gateChecks` (gocyclo 32 > 30, introduced by 53-06's D-04 enablement AND-gate and 53-10's verdict-final findings-push retry). Pure refactor: doc comments moved with the code, no behavior change, full Layer A envtest suite (56/56) reconfirmed green after the split.
- `cmd/stub-subagent/main.go` — promoted the `"success"` literal (goconst: 6 occurrences) to `testModeSuccess`; pre-existing since Phase 50, unrelated to phase 53's own changes but blocking `make lint`.
- `dashboard/web/src/components/ArtifactViewer.test.tsx` — wrapped the JSON-tab click in a `waitFor` retry loop to absorb a real (not phase-53-caused) React effect-ordering race between the component's "reset to first *.md" effect and the test's synchronous click.
- `test/integration/kind/verifier_concurrency_test.go` — updated the first `Eventually`'s assertion from exact `Equal(Verifying)` to `BeElementOf(Verifying, Succeeded, Failed)`, and corrected the header/NOTE comments that described the (now-closed) VerifierImage gap as still open.

## Decisions Made

See `key-decisions` in frontmatter — four decisions, each with rationale: (1) webhook-blast-radius closure beyond the plan's declared namespace/release isolation, (2) fixing both phase-53-caused and pre-existing red gates per Task 2's explicit authorization, (3) root-fixing the dashboard test race rather than deferring it, (4) archaeology-driven fix of the stale verifier-concurrency spec.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 2 - Missing critical functionality] Closed a cluster-wide webhook admission DoS gap in the sticky-posture test's own isolation design**
- **Found during:** Task 1 (isolated kind sticky-posture proof), during authoring, before the first live run
- **Issue:** The plan's declared mitigation for T-53-21 (isolated release+namespace+Serial) does not cover the chart's `ValidatingWebhookConfiguration`, which is cluster-scoped (`failurePolicy: Fail`, no `namespaceSelector`) — a second copy from the throwaway `tide-posture` release would intercept Plan/Project/Wave admission cluster-wide while its manager pod has no ready endpoint, capable of rejecting unrelated background reconciliation from the shared suite release.
- **Fix:** `deletePostureWebhookConfig()` runs immediately after every helm install/upgrade on the throwaway release, before any other work, deleting only that release's uniquely-named webhook config object. Helm re-applies it on the next operation (still part of the release manifest), so the delete repeats each time, bounding the exposure window to milliseconds.
- **Files modified:** test/integration/kind/verify_posture_sticky_test.go
- **Verification:** Live-verified against kind-tide-test: `kubectl get validatingwebhookconfigurations` shows only the shared `tide-validating-webhook-configuration` before/after the spec runs; the spec's own 3-leg proof passed in ~19s both standalone and inside the full `make test-int` run.
- **Committed in:** `977d94da` (Task 1 commit)

**2. [Rule 3 - Blocking] Fixed task_controller.go's gateChecks gocyclo violation (32 > 30)**
- **Found during:** Task 2 (D-10 gate 1, `make lint`)
- **Issue:** `gateChecks` exceeded the gocyclo threshold — git-archaeologied to phase-53's own 53-06 (D-04 enablement AND-gate) and 53-10 (verdict-final findings-push retry) commits, confirming it was a phase-53-caused regression, not pre-existing.
- **Fix:** Extracted the VerifyHalted and Failed terminal-branch bodies into `handleVerifyHaltedTerminal`/`handleFailedTerminal`, keeping all doc comments intact. Zero behavior change.
- **Files modified:** internal/controller/task_controller.go
- **Verification:** `./bin/golangci-lint run ./internal/controller/...` → 0 issues; full `internal/controller` envtest suite (`go test ./internal/controller/... -timeout 20m -ginkgo.v`) → 280/280 specs pass (confirmed both standalone and inside the two full `make test-int` runs).
- **Committed in:** `51786947` (Task 2 commit)

**3. [Rule 3 - Blocking] Fixed cmd/stub-subagent/main.go's goconst violation ("success" × 6)**
- **Found during:** Task 2 (D-10 gate 1, `make lint`)
- **Issue:** `make lint` failed on this file even though it predates phase 53 (last touched Phase 50) — git-archaeologied via `git log` to confirm no phase-53 commit touches it. Task 2's `<files>` field explicitly authorizes fixing any red gate encountered regardless of origin, and its acceptance criteria requires gates 1-4 to all exit 0 with no archaeology carve-out.
- **Fix:** Promoted the repeated literal to `const testModeSuccess = "success"`, replacing all 4 non-test-file usages.
- **Files modified:** cmd/stub-subagent/main.go
- **Verification:** `go build`/`go test ./cmd/stub-subagent/...` pass; `./bin/golangci-lint run ./cmd/stub-subagent/...` → 0 issues; full `make lint` → 0 issues.
- **Committed in:** `51786947` (Task 2 commit)

**4. [Rule 1 - Bug] Fixed a real React effect-ordering race in ArtifactViewer.test.tsx**
- **Found during:** Task 2 (D-10 gate 3/4, `make verify-dashboard-freshness` / `npm test`)
- **Issue:** ~50% flake rate observed across 4 consecutive runs (2 pass, 2 fail) on the SAME unmodified file. Root cause: the component's "reset selected to first *.md" `useEffect` (dependency `[data]`) is scheduled from the same async `load()` that first populates `data`; in the jsdom test environment that passive effect can still be pending when the test's synchronous `fireEvent.click` fires on the JSON tab, occasionally flushing afterward and reverting the click's `setSelected(1)` back to `0`. Not observable in a real browser (effects flush well before human reaction time) and not caused by any phase-53 commit (file last touched Phase 36-37) — but blocking a clean gates-1-4 run and matching the project's own NO FLAKE TOLERANCE philosophy.
- **Fix:** Wrapped the click in a `waitFor` retry loop that re-clicks until `aria-selected="true"` sticks, converging once the stale effect has actually settled. No production code change.
- **Files modified:** dashboard/web/src/components/ArtifactViewer.test.tsx
- **Verification:** 5/5 consecutive standalone runs pass; 2/2 consecutive full `npm test` suite runs pass (317/317 tests each); the combined gates-1-4 sequential run also passed clean.
- **Committed in:** `51786947` (Task 2 commit)

**5. [Rule 1 - Bug] Fixed verifier_concurrency_test.go's exact-equality race against a now-fast verify loop**
- **Found during:** Task 2 (D-10 gate 5, `make test-int` Layer B)
- **Issue:** `Eventually(...).Should(Equal(Verifying))` timed out after 3 minutes because the Task had already reached `Succeeded` (a permanent state) before the poll observed it. Git-archaeology: the file's own header comment described an open `VerifierImage` wiring gap as the reason the verify loop could never actually complete; confirmed via `grep -n VerifierImage cmd/manager/main.go` and `charts/tide/templates/deployment.yaml` that an earlier phase-53 plan (CFG-01) had already closed that gap. With the gap closed, the trivial `true` gate command now completes the whole loop fast enough to race past the transient `Verifying` phase between 3s polls — a phase-53-caused behavior change surfacing in a file phase 53 hadn't directly touched yet.
- **Fix:** Changed the assertion to `BeElementOf(Verifying, Succeeded, Failed)` (any phase proving dispatch was attempted) and corrected the stale header/NOTE comments to describe the gap as closed.
- **Files modified:** test/integration/kind/verifier_concurrency_test.go
- **Verification:** Standalone `--ginkgo.focus='ESC-04'` run passed (193s); the spec also passed inside the final full `make test-int` run.
- **Committed in:** `51786947` (Task 2 commit)

---

**Total deviations:** 5 auto-fixed (1 Rule 2 missing-critical, 2 Rule 3 blocking, 2 Rule 1 bug)
**Impact on plan:** All five were necessary to reach a genuinely clean D-10 gate run rather than a reported-green-but-actually-red one (the exact v1.0.8 release-cascade failure mode this plan exists to close out). No scope creep — none touched `charts/tide/values.yaml` or any other FIXED-contract file, and none changed production behavior except the isolation-safety fix in the new test file itself.

## Issues Encountered

- Two Layer A envtest specs (`gates_test.go`'s GATE-04 descent hold, `spec_conformance_test.go`'s MS03 milestone gate composition) failed on the FIRST `make test-int` attempt, immediately after the heavy `test-int-kind-prep` image-build/load phase. Re-ran them in isolation (`--ginkgo.focus='GATE-04 descent hold|MS03'`) with a warm cluster and no competing load: 4/4 passed cleanly. Git-archaeologied both `gates_test.go`/`spec_conformance_test.go` and `phase_controller.go`/`milestone_controller.go`: none touched by any phase-53 commit. This matches the project's own already-tracked "layer-a-envtest-flakes-pr9 [investigating]" debug session (STATE.md Deferred Items) — a pre-existing, separately-owned CPU-contention flake class, not a phase-53 regression. Confirmed via the SECOND full `make test-int` run, which passed Layer A 56/56 cleanly.

## User Setup Required

None - no external service configuration required.

## Next Phase Readiness

- ROADMAP SC2 is now fully proven: the render pair (53-05, `helm template` covering both install/upgrade legs) plus this plan's live sticky proof (the one behavior `helm template` cannot exercise).
- D-10 phase-gate closeout is complete: `make lint`, `make verify-chart-reproducible`, `make verify-dashboard-freshness`, `npm test`, and `make test-int` are all green in-phase — zero known ci.yaml-only debt carried into release pre-flight for v1.0.9's Phase 53 close.
- Phase 53 (all 11 plans) is now ready for phase-level verification / milestone close.

---
*Phase: 53-chart-config-dashboard-provenance-surfacing*
*Completed: 2026-07-21*

## Self-Check: PASSED

- FOUND: test/integration/kind/verify_posture_sticky_test.go
- FOUND: .planning/phases/53-chart-config-dashboard-provenance-surfacing/53-09-SUMMARY.md
- FOUND: 977d94da (Task 1 commit)
- FOUND: 51786947 (Task 2 commit)
- FOUND: a6455e5e (SUMMARY commit)
