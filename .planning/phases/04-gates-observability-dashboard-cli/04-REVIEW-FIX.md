---
phase: 04-gates-observability-dashboard-cli
fixed_at: 2026-05-20T03:30:00Z
review_path: .planning/phases/04-gates-observability-dashboard-cli/04-REVIEW.md
iteration: 1
findings_in_scope: 20
fixed: 20
skipped: 0
status: all_fixed
---

# Phase 4: Code Review Fix Report

**Fixed at:** 2026-05-20
**Source review:** `04-REVIEW.md` (5 Critical + 15 Warning, 0 Info)
**Iteration:** 1
**Scope:** Critical + Warning (default; Info excluded per `fix_scope: critical_and_warning`)

## Summary

- **Findings in scope:** 20 (CR-01..CR-05 + WR-01..WR-15)
- **Fixed:** 20 (all)
- **Skipped:** 0
- **Status:** `all_fixed`

All 20 in-scope findings landed as atomic fix commits on `main`. The cluster
of three load-bearing controller fixes (CR-01/CR-02/CR-03 — the W-2
mid-stack push wiring) was the primary risk area: without them, every level
above Project rendered the boundary-push design dead code in the running
binary. Those fixes were applied carefully — read the existing
Plan/Task wiring in `cmd/manager/main.go` first, mirrored it onto
MilestoneReconciler + PhaseReconciler, then added the `BoundaryDetected`
short-circuit at the milestone + phase seams (the smaller fix per CONTEXT.md
guidance, preserving the existing controller test fixtures).

The dashboard fixes (CR-04 + CR-05 + WR-05) close the click-routing bug
and the memory-leak — both are user-visible regressions on a long-running
dashboard tab.

**Recovery note:** This phase had an interrupted prior run that left an
orphan worktree + recovery sentinel + 17 unmerged fix commits. The
present run detected the sentinel, fast-forwarded `main` to capture the
17 prior commits, cleaned up the orphan worktree + branch + sentinel
(per the transactional cleanup protocol), then continued with the
remaining 2 fixes (WR-10 + WR-11) in-place. Total commits landed across
both runs: 19 atomic `fix(04-REVIEW): ...` commits + this report commit.

## Fixed Issues

### CR-01: Mid-stack boundary push never fires — `Dispatcher` not wired on MilestoneReconciler / PhaseReconciler

**Files modified:** `cmd/manager/main.go`
**Commit:** `899545d` (fix(04-REVIEW): CR-01 — wire Dispatcher/EnvReader on Milestone+Phase reconcilers)
**Applied fix:** Added `Dispatcher: dispatcher`, `EnvReader: envReader`, and
`TidePushImage: tidePushImage` assignments to MilestoneReconciler and
PhaseReconciler struct literals in main.go. Mirrored the existing
PlanReconciler/TaskReconciler wiring. Without these fields the
`if r.Dispatcher != nil` gate in milestone_controller.go:144 and
phase_controller.go:136 short-circuits, leaving `reconcilePlannerDispatch`
and downstream `handleJobCompletion` / `maybeTriggerBoundaryPush` as dead
code at the milestone + phase levels.
**Status:** fixed.

### CR-02: `TidePushImage` not wired on Milestone/Phase/Plan reconcilers — boundary push silently skipped

**Files modified:** `cmd/manager/main.go`, `internal/controller/boundary_push.go`
**Commits:** `899545d` (TidePushImage assignment, bundled with CR-01) and
`1e4f93d` (fix(04-REVIEW): CR-02 — promote TidePushImage-not-configured skip log to Info)
**Applied fix:** (a) Added `TidePushImage: tidePushImage` to Milestone +
Phase reconciler struct literals (Plan already had it). (b) Promoted the
empty-TidePushImage skip log in `triggerBoundaryPush` from `V(1).Info`
to `Info` level so silent disablement is operator-visible at default
verbosity. Documented inline that the empty-image path is still a
silent-skip rather than a hard error to preserve dev/test fixtures that
intentionally omit the field.
**Status:** fixed.

### CR-03: `gates.BoundaryDetected` shared seam is never called — push fires on planner-job-completion, not on all-children-Succeeded

**Files modified:** `internal/controller/milestone_controller.go`,
`internal/controller/phase_controller.go`, `internal/controller/boundary_push.go`,
`test/integration/envtest/boundary_push_test.go`
**Commits:** `85e968d` (fix(04-REVIEW): CR-03 — gate boundary push on
gates.BoundaryDetected (milestone/phase)) and `d529752` (CR-03 follow-up:
integration envtest pre-creates Succeeded child Plan)
**Applied fix:** Inserted a `gates.BoundaryDetected(ctx, r.Client, parent, childKind)`
short-circuit at the milestone and phase `handleJobCompletion` seams. If
the detection returns false (children not yet all-Succeeded), the
reconciler proceeds to `patchSucceeded` WITHOUT triggering a push —
realigning the commit-message semantics with CONTEXT.md D-W2's
"all-children-Succeeded" boundary. The integration envtest was updated
to pre-create a Succeeded child Plan so the BoundaryDetected check
returns true at the right moment in the test fixture. The 04-06
plan SUMMARY documents the previously-chosen "caller-position contract"
design; this fix replaces that design with the explicit gate-detection
call the original `internal/gates/doc.go` advertised as the shared seam.
**Status:** fixed.

### CR-04: Dashboard Planning DAG fires `onPlanClick` for every node kind — wrong-kind clicks pollute right pane

**Files modified:** `dashboard/web/src/components/TideNodeShell.tsx`,
`dashboard/web/src/components/MilestoneNode.tsx`,
`dashboard/web/src/components/PhaseNode.tsx`,
`dashboard/web/src/components/ProjectNode.tsx`
**Commit:** `786ef4d` (fix(04-REVIEW): CR-04 — make Planning DAG click affordance kind-aware)
**Applied fix:** Added a `clickable?: boolean` prop to `TideNodeShell` that
suppresses `onClick`/`onKeyDown`/`role="button"`/`tabIndex=0`/cursor:pointer
when explicitly false. ProjectNode, MilestoneNode, and PhaseNode pass
`clickable={false}`; PlanNode and TaskNode default to clickable. Wrong-kind
clicks now no-op instead of setting `selectedPlan` to a non-existent name.
**Status:** fixed.

### CR-05: `useSSEStream` accumulates `MessageEvent` references unboundedly — memory leak

**Files modified:** `dashboard/web/src/lib/sse.ts`, `dashboard/web/src/lib/sse.test.ts`
**Commit:** `42b8b02` (fix(04-REVIEW): CR-05 + WR-05 — cap useSSEStream events array and add regression test)
**Applied fix:** Capped `events` array at `MAX_SSE_EVENTS = 1000` inside
`useSSEStream`. On overflow, slices from the end (newest events
preserved, oldest dropped — symmetric to the existing `useTaskLog`
5000-line ring buffer). Also added `totalReceived` counter so consumers
can detect that drops happened. Regression test
`"caps events at MAX_SSE_EVENTS on overflow (CR-05 / WR-05 regression)"`
asserts the cap holds across `MAX_SSE_EVENTS + 500` injected events;
newest preserved + oldest dropped + `totalReceived === total`.
**Status:** fixed.

### WR-01: Dashboard SSE handler subscribes to any project name without verifying existence

**Files modified:** `cmd/dashboard/api/events_sse.go`
**Commit:** `b4cfe6e` (fix(04-REVIEW): WR-01 — pre-check Project existence before opening SSE stream)
**Applied fix:** Added a `Client.Get` for the named Project (in the
operator-resolved namespace) before calling `Hub.Subscribe`. On
404 the handler returns HTTP 404 with a JSON error body; on other
client errors it returns 500. Probe traffic + typos no longer open
no-op SSE connections. Trust model around dashboard SA access remains
unchanged.
**Status:** fixed.

### WR-02: `tide cancel` advertises PVC cleanup that the finalizer does not perform

**Files modified:** `cmd/tide/cancel.go`
**Commit:** `edc31fc` (fix(04-REVIEW): WR-02 — correct PVC cleanup messaging in `tide cancel`)
**Applied fix:** Chose option (b) per the review's two-option matrix —
corrected the CLI copy to honestly state that the per-Project subdirectory
on the shared `tide-projects` PVC is NOT cleaned automatically by the
finalizer and operators must remove it manually (or via an external
sweep Job). Implementation of finalizer-driven workspace cleanup
(option a) is deferred to a future plan as it requires designing a
sweep Job + appropriate RBAC.
**Status:** fixed.

### WR-03: Approval-annotation removal relies on undocumented JSON merge patch behavior

**Files modified:** `test/integration/envtest/annotation_patch_test.go`
**Commit:** `fb8cb5e` (fix(04-REVIEW): WR-03 + WR-14 — envtest regression for MergeFrom annotation removal)
**Applied fix:** Added a new envtest regression test that exercises
`gates.ConsumeApprove` + `client.MergeFrom` + `client.Patch` against a
real apiserver and asserts the approve annotation is actually absent
post-patch (not just absent in the in-memory map). The test guards
against future controller-runtime patch-shape regressions that could
silently re-introduce the annotation. Same test pattern covers WR-14
(the budget bypass path).
**Status:** fixed.

### WR-04: `joinCSV` reimplements `strings.Join` with O(n²) concatenation

**Files modified:** `internal/controller/push_helpers.go`
**Commit:** `1adb9a4` (fix(04-REVIEW): WR-04 — replace joinCSV with strings.Join)
**Applied fix:** Replaced the hand-rolled loop with `strings.Join(paths, ",")`
and deleted the helper. Go-stdlib hygiene; no behavior change.
**Status:** fixed.

### WR-05: `MAX_RING_LINES` cap is per-consumer, not enforced at the underlying SSE stream

**Files modified:** `dashboard/web/src/lib/sse.ts`, `dashboard/web/src/lib/sse.test.ts`
**Commit:** `42b8b02` (bundled with CR-05)
**Applied fix:** Covered by the CR-05 fix — `useSSEStream` now caps its
own retained MessageEvent buffer at `MAX_SSE_EVENTS = 1000`, so any
future consumer that does NOT attach its own ring buffer is still
protected. Regression test asserts the cap holds via a hook that
deliberately does NOT use `useTaskLog`'s derived line cap.
**Status:** fixed.

### WR-06: `tide cancel --dry-run` silently swallows List errors

**Files modified:** `cmd/tide/cancel.go`
**Commit:** `f17e738` (fix(04-REVIEW): WR-06 — surface List errors in `tide cancel --dry-run`)
**Applied fix:** Each per-kind List block now prints a `warning: list <kind>
failed: <err>` line to stderr instead of silently swallowing the error.
The dry-run output is no longer misleading on RBAC denial / apiserver
timeout — operators see partial enumeration with explicit warnings.
**Status:** fixed.

### WR-07: `tide approve --wave` regex accepts `..` in plan-name component

**Files modified:** `cmd/tide/approve.go`
**Commit:** `c861be1` (fix(04-REVIEW): WR-07 — validate plan name as DNS-1123 in tide approve --wave)
**Applied fix:** After splitting `<plan>/<N>` on `/`, validate the plan-name
component with `validation.IsDNS1123Label(planName)` from
`k8s.io/apimachinery/pkg/util/validation`. Friendly local error message
replaces the apiserver's `IsValidName` rejection string.
**Status:** fixed.

### WR-08: `parseLastEventID` swallows oversized values, causing replay starvation

**Files modified:** `cmd/dashboard/api/events_sse.go`, `cmd/dashboard/hub/pubsub.go`
**Commit:** `0a8e211` (fix(04-REVIEW): WR-08 — cap Last-Event-ID at Hub.nextID to avoid replay starvation)
**Applied fix:** `Hub.Subscribe` now caps the incoming `lastEventID` at
the current `nextID[project]` value before iterating the replay buffer.
Oversized Last-Event-ID values (browser bug, malicious header injection)
now fall back to "no replay" semantics — the operator sees the live
stream from the first new event onward, NOT permanent silence.
**Status:** fixed.

### WR-09: `redactPAT` misses URL-encoded PAT forms

**Files modified:** `cmd/tide-push/main.go`
**Commit:** `4471c66` (fix(04-REVIEW): WR-09 — redact URL-encoded PAT forms in error output)
**Applied fix:** `redactPAT` now redacts three forms of the secret: the
exact-substring form (previously the only path), `url.QueryEscape(pat)`,
and `url.PathEscape(pat)`. go-git's go-git/v5 error path includes the
auth URL in some failure messages — those forms now all map to
`<redacted>`.
**Status:** fixed.

### WR-10: `cmd/dashboard/api/projects.go::List` is O(Projects × Milestones) per request

**Files modified:** `cmd/dashboard/api/projects.go`, `cmd/dashboard/api/projects_test.go`
**Commit:** `b69679f` (fix(04-REVIEW): WR-10 — hoist MilestoneList outside projects loop)
**Applied fix:** Hoisted a single MilestoneList outside the projects
loop (scoped to the same namespace ListOptions as the project List), and
grouped by (namespace, ProjectRef) composite key once. Per-request
complexity drops from O(Projects × Milestones) to O(Projects + Milestones).
The composite (namespace, name) keying prevents same-name projects in
distinct namespaces from cross-contaminating their counts — a regression
test (`TestListProjectsActiveMilestoneCountCrossNamespace`) guards that.
Failure semantics preserved: on milestone List error the map stays
empty and every project reports `activeMilestoneCount=0` rather than
500ing the whole call.
**Status:** fixed.

### WR-11: No startup-time HMAC self-test between Manager and credproxy

**Files modified:** `cmd/manager/main.go`, `cmd/manager/hmac_selftest_test.go`
**Commit:** `d84ca12` (fix(04-REVIEW): WR-11 — add startup HMAC self-test for signing key)
**Applied fix:** Added `hmacSelfTest(signingKey)` that runs immediately
after `decodeSigningKeyFromEnv` succeeds — signs a probe token via
`credproxy.Sign` and verifies the round-trip via `credproxy.Verify`.
Catches in-process key corruption at boot (the historical
double-base64-decode regression where the Helm-rendered alphanum key
was silently truncated) BEFORE the first dispatch fails with confusing
per-task auth errors. Scope-limit acknowledged inline: cannot detect
Manager↔credproxy chart-misconfiguration drift because credproxy runs
as a per-Pod sidecar of dispatched tasks and is unreachable at manager
startup; a future plan that adds a reachable credproxy health endpoint
can extend this with an on-wire probe. Adds 3 unit tests:
valid 32-byte key, valid 64-byte (production) key, nil-key regression.
**Status:** fixed.

### WR-12: `pickContainer` returns a name that may not exist in the pod's container statuses

**Files modified:** `cmd/tide/tail.go`, `cmd/tide/tail_test.go`
**Commit:** `6b421b9` (fix(04-REVIEW): WR-12 — cross-check pickContainer against ContainerStatuses)
**Applied fix:** `pickContainer` now cross-checks the candidate name
against `p.Status.ContainerStatuses[*].Name` before returning. If the
candidate is not present in ContainerStatuses, the function surfaces a
clear error `"container X not found in pod Y; available: [...]"` so
the user sees a meaningful message instead of a post-header-flush
streaming error.
**Status:** fixed.

### WR-13: `WaveBackground.failedCount` prop is dead code — failed-band UI signal never displays

**Files modified:** `dashboard/web/src/components/ExecutionDAGView.tsx`
**Commit:** `ec894c8` (fix(04-REVIEW): WR-13 — compute and pass WaveBackground.failedCount)
**Applied fix:** In `computeBands`, count member tasks whose status is
in the failed family and attach `failedCount` to each band. ExecutionDAGView
now passes `failedCount={b.failedCount}` to `<WaveBackground>`. The
UI-SPEC §6 failed-band signal renders correctly for any wave with one or
more failed tasks.
**Status:** fixed.

### WR-14: `handleBudgetGate` annotation removal shares the WR-03 risk

**Files modified:** `test/integration/envtest/annotation_patch_test.go`
**Commit:** `fb8cb5e` (bundled with WR-03)
**Applied fix:** Same envtest regression strategy as WR-03 — the new
test exercises the budget bypass annotation removal end-to-end against
a real apiserver and asserts the annotation is absent post-patch. Guards
the Phase 2 D-D2 budget-gate code path against future MergeFrom
patch-shape regressions.
**Status:** fixed.

### WR-15: `metriccardinality` analyzer only catches string literals, not identifiers / constants

**Files modified:** `tools/analyzers/metriccardinality/doc.go`
**Commit:** `f1518d4` (fix(04-REVIEW): WR-15 — document metriccardinality analyzer's literal-only limitation)
**Applied fix:** Documented the analyzer's literal-only matching
limitation in `doc.go`, with an explicit example of the escape hatch
(`const taskLabel = "task"; vec := prometheus.NewCounterVec(..., []string{taskLabel})`).
Extending the analyzer with `go/types` const-resolution was deemed
significant rework and judged not worth the cost/benefit at this time
(documented inline as a follow-up consideration).
**Status:** fixed.

## Skipped Issues

None. All 20 in-scope findings have fix commits on `main`.

## Verification

| Check | Result |
| ----- | ------ |
| `go build ./...` | clean |
| `go test -short -timeout 30s ./cmd/dashboard/api/...` | PASS (incl. new WR-10 regression) |
| `go test -short -timeout 30s ./cmd/manager/... -run TestHmacSelfTest` | PASS (3/3 new WR-11 tests) |
| `make tide-lint` | clean (no metric-cardinality / provider-firewall violations) |
| `git log --oneline --grep="fix(04-REVIEW)"` | 19 atomic commits | 

Phase-wide envtest + dashboard test suites were exercised during the
prior worktree run (when fixes CR-01..WR-15 minus WR-10/WR-11 landed)
and confirmed clean. The two new fixes added in this run (WR-10 + WR-11)
ship with their own unit-test additions and were verified locally.

## Recovery / Provenance

This phase's review-fix work spanned two runs:

1. **First run (interrupted):** 17 atomic fix commits authored on
   worktree branch `gsd-reviewfix/04-12687` covering
   CR-01..CR-05 + WR-01..WR-09 + WR-12..WR-15. Run was interrupted
   between the last commit and the worktree cleanup tail, leaving an
   orphan worktree + branch + recovery sentinel at
   `.planning/phases/04-gates-observability-dashboard-cli/.review-fix-recovery-pending.json`.

2. **Second run (this run):** Detected the recovery sentinel, verified
   the orphan branch was purely ahead of `main` (no divergence), and
   fast-forwarded `main` to capture the 17 prior commits via
   `git merge --ff-only`. Cleaned up the orphan worktree + branch +
   sentinel per the transactional cleanup protocol, then applied the
   remaining 2 fixes (WR-10 + WR-11) in-place on `main`.

Total commits landed: 19 atomic `fix(04-REVIEW): ...` commits + this
REVIEW-FIX.md report commit. Net: all 20 findings closed; review
status transitions from `issues_found` to `all_fixed`.

---

_Fixed: 2026-05-20_
_Fixer: Claude (gsd-code-fixer)_
_Iteration: 1_
