---
phase: quick-260617-qqh
plan: 01
subsystem: controller
tags: [bug-fix, tdd, envtest, project-controller, planner-dispatch, reporter-spawn]
dependency_graph:
  requires: []
  provides: [project-planner-completion-fix]
  affects: [internal/controller/project_controller.go]
tech_stack:
  added: []
  patterns: [reconcileProjectPlannerDispatch reorder, terminal-state-before-idempotency]
key_files:
  created:
    - internal/controller/project_planner_completion_test.go
  modified:
    - internal/controller/project_controller.go
decisions:
  - "Reorder only: terminal-state check (Step 2) moved before idempotency guard (Step 2b) within reconcileProjectPlannerDispatch; no structural changes to handleProjectJobCompletion or surrounding logic"
  - "Idempotency guard now applies only to the non-Running dispatch path; Running branch handles both terminal and non-terminal Job states internally"
  - "Test uses separate project names per spec (qqh-proj-primary, qqh-proj-control) to prevent cross-spec state leakage in the shared envtest namespace"
  - "Cache-terminal-sync wait added before the second reconcile (Eventually isJobTerminal) because reconciler reads Job through cache-backed mgrClient"
metrics:
  duration: "~20 min"
  completed: "2026-06-17"
  tasks: 2
  files: 2
---

# Quick Task 260617-QQH: Fix project controller planner completion while Job still exists

One-liner: Reordered reconcileProjectPlannerDispatch so the Running-branch terminal-state check precedes the blanket idempotency guard, mirroring milestone_controller.go and eliminating the ~10-min TTL stall before reporter spawn + budget rollup.

## Tasks Completed

| Task | Type | Commit | Description |
|------|------|--------|-------------|
| 1 | RED test | `9df95fe` | `internal/controller/project_planner_completion_test.go` — envtest proving terminal Job completion while Job exists |
| 2 | Fix | `2a5e0dc` | `internal/controller/project_controller.go` — move Step 2 before Step 1b/Step 2b |

## Root Cause (Diagnosed Before This Task)

`reconcileProjectPlannerDispatch` had:

1. Step 1: terminal short-circuit (Complete/InitFailed) — correct
2. **Step 1b: idempotency guard — `if job exists → return nil`** — this fired unconditionally whenever the planner Job was present, regardless of its state
3. Step 2: Running-branch terminal-state check — **unreachable while Job still existed**

Net effect: after the planner Job completed, `reconcileProjectPlannerDispatch` returned nil (Step 1b) without calling `handleProjectJobCompletion`. The reporter Job was never spawned and `Budget.CostSpentCents` stayed at 0 until the Job's 10-min `ttlSecondsAfterFinished=600` GC window expired and Step 2 → NotFound branch fired the TTL fallback.

## Fix (Task 2)

Moved Step 2 (the Running-branch terminal check) BEFORE Step 1b (now Step 2b), mirroring `milestone_controller.go:reconcilePlannerDispatch` Step 2 (~286-301) before Step 2b (~304-326).

New ordering in `reconcileProjectPlannerDispatch`:
1. SigningKey guard (unchanged)
2. Step 1: terminal short-circuit Complete/InitFailed (unchanged)
3. **Step 2: if PhaseRunning → check Job terminal state first**:
   - Job present + terminal → `handleProjectJobCompletion(&job)` (immediate, no TTL wait)
   - Job present + non-terminal → `return nil` (in-flight; do nothing)
   - Job absent (TTL/GC fallback) → `handleProjectJobCompletion(nil)`
   - Other error → return error
4. **Step 2b: idempotency guard** (non-Running path only; no behavioral change for non-Running callers)
5. Steps 3+ (BillingHalt, BudgetBlocked, pool, envelope, Job Create, Phase patch) — UNCHANGED

`handleProjectJobCompletion` body: UNCHANGED. `milestone_controller.go`: UNCHANGED. `charts/tide/values.yaml`: UNCHANGED.

## Verify Commands and Observed Output

### RED → GREEN transition (Task 1 + Task 2)

**RED (primary spec fails on current code, before fix):**
```
go test ./internal/controller/... -count=1 --ginkgo.focus="QQH-01"

[FAILED] Timed out after 5.000s.
tide-reporter-<uid> Job must be created on planner Job completion while Job still exists
Expected success, but got an error:
    Job.batch "tide-reporter-af24b583-541e-4d99-9f25-7460554dbdea" not found
Ran 1 of 139 Specs in 13.323 seconds
FAIL! -- 0 Passed | 1 Failed | 0 Pending | 138 Skipped
```

**GREEN (both specs pass after fix):**
```
go test ./internal/controller/... -count=1 --ginkgo.focus="QQH-01"

ok  	github.com/jsquirrelz/tide/internal/controller	9.812s
```

(1 primary spec + 1 control spec — both PASS)

### Full `make test` result

```
make test 2>&1 | tee /tmp/qqh-fix-test.log | tail -40; echo "MAKE_EXIT=${PIPESTATUS[0]}"
```

MAKE_EXIT from tee pipeline: `make test` exited non-zero due to a pre-existing failure in `api/v1alpha1` (unrelated to this task).

```
grep -nE '^--- FAIL|^FAIL\s' /tmp/qqh-fix-test.log

22:--- FAIL: TestDogfoodManifests_StrictDecode (0.00s)
25:--- FAIL: TestDogfoodManifests_RequiredFields (0.00s)
30:FAIL	github.com/jsquirrelz/tide/api/v1alpha1	2.651s
```

These failures (`TestDogfoodManifests_StrictDecode`, `TestDogfoodManifests_RequiredFields`) are pre-existing: verified by running `go test ./api/v1alpha1/...` on the pre-fix `main` HEAD (commit `9df95fe`) without any project_controller changes — same failures observed. Root cause: `examples/projects/dogfood/02-codex-runtime-project.yaml` contains `failureProfile` which is unknown in the v1alpha1 API (it's a v1alpha2 field). Zero files in `api/v1alpha1/` or `examples/` were touched by this task.

**Controller package: PASSED:**
```
ok  	github.com/jsquirrelz/tide/internal/controller	59.132s	coverage: 71.3% of statements
```

No other controller spec regressed.

### Scope verification

```
git diff internal/controller/project_controller.go
```
Diff touches only `reconcileProjectPlannerDispatch` (lines 957-1000 in the pre-fix file); `handleProjectJobCompletion` and all downstream steps are unchanged.

```
git diff internal/controller/milestone_controller.go
git diff charts/tide/values.yaml
```
Both empty — no changes.

## Deviations from Plan

**1. [Rule 1 - Bug] Cache-timing issue in RED test required an additional Eventually gate**
- Found during: Task 1 (RED test debugging)
- Issue: `makeFakeJobTerminal` updates Job status through `mgrClient`, but the reconciler reads Job status through the same cache-backed `mgrClient`. If the cache hasn't reflected the terminal status before the second `reconcileProjectPlannerDispatch` call, `isJobTerminal` returns false even with the fix applied — making the fixed code appear to still fail.
- Fix: Added `Eventually(isJobTerminal, 5s, 100ms)` wait between `makeFakeJobTerminal` and the second reconcile call. This correctly models production behavior (the informer cache reflects status updates before the next reconcile trigger).
- Files modified: `internal/controller/project_planner_completion_test.go`

**2. [Rule 1 - Bug] Cross-spec state leakage with shared project name**
- Found during: Task 1 (RED test debugging)
- Issue: Initial implementation used a single project name (`test-proj-qqh-completion`) shared between both `It` blocks. When Ginkgo ran the control spec first, AfterEach cleanup raced with the primary spec's `createProject`, causing the Phase assertion to fail with empty-string instead of the expected reporter-absent failure.
- Fix: Gave each `It` block a unique project name (`qqh-proj-primary`, `qqh-proj-control`) via separate `Describe` blocks with independent `BeforeEach`/`AfterEach`.
- Files modified: `internal/controller/project_planner_completion_test.go`

## Known Stubs

None.

## Threat Flags

None — this is a pure reorder within an existing reconcile state machine; no new network endpoints, auth paths, or schema changes introduced.

## Self-Check: PASSED

- `internal/controller/project_planner_completion_test.go`: exists, commit `9df95fe` (test) + `2a5e0dc` (updated with cache-wait)
- `internal/controller/project_controller.go`: commit `2a5e0dc` — diff verified touches only `reconcileProjectPlannerDispatch`
- Commits confirmed: `git log --oneline -5` shows both `9df95fe` and `2a5e0dc` on `main`
- Controller suite: `ok internal/controller 59.132s` — all 141 specs pass
- Pre-existing `api/v1alpha1` failures documented and confirmed pre-existing
