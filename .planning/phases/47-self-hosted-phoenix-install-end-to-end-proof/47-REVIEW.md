---
phase: 47-self-hosted-phoenix-install-end-to-end-proof
reviewed: 2026-07-17T19:24:50Z
depth: standard
files_reviewed: 17
files_reviewed_list:
  - internal/controller/reporter_jobspec.go
  - internal/controller/dispatch_helpers.go
  - internal/controller/milestone_controller.go
  - internal/controller/phase_controller.go
  - internal/controller/plan_controller.go
  - internal/controller/project_controller.go
  - internal/controller/task_controller.go
  - cmd/manager/main.go
  - api/v1alpha3/milestone_types.go
  - api/v1alpha3/phase_types.go
  - api/v1alpha3/plan_types.go
  - api/v1alpha3/project_types.go
  - api/v1alpha3/task_types.go
  - pkg/git/remote.go
  - cmd/tide-push/main.go
  - docs/observability.md
  - examples/projects/medium/per-namespace-resources.yaml
findings:
  critical: 1
  warning: 1
  info: 2
  total: 4
status: resolved
resolution:
  critical_fixed: 1
  warning_fixed: 1
  info_deferred: 2
  fix_commits:
    - "37f4a46 fix(47): honor durable reporter-spawn marker on nil-Job re-entry (CR-01 re-fix)"
    - "271df2a test(47): pin the planner-Job TTL-GC re-entry window (CR-01 re-fix)"
  verified: "heavy envtest 46/46 green (make test-heavy); build+vet clean; regression-proven (pre-fix gate fails the new milestone spec)"
---

# Phase 47: Code Review Report

**Reviewed:** 2026-07-17T19:24:50Z
**Depth:** standard
**Files Reviewed:** 17
**Status:** issues_found

## Summary

Reviewed the four verification-gap fixes (plans 47-06 through 47-10): the CR-02
OTLP-headers `secretKeyRef` conversion, the CR-01 durable reporter-spawn markers,
the `deriveEffectiveLease` ancestry guard, and the doc/fixture fixes.

Three of the four fixes are sound:

- **CR-02 (security) is fully closed.** The decoded Phoenix bearer no longer
  reaches any Job PodSpec at any of the five spawn sites. `reporter_jobspec.go`
  emits `OTEL_EXPORTER_OTLP_HEADERS` as a `valueFrom.secretKeyRef` (Optional=true)
  against the fixed-name `tide-otlp-headers` mirror; `cmd/manager/main.go` reads
  `OTEL_EXPORTER_OTLP_HEADERS` only for presence and threads the Secret **name**
  (`controller.ReporterOTLPHeadersSecretName`), never the value, into both
  `PlannerReconcilerDeps` and `TaskReconcilerDeps`. The false RBAC-equivalence
  claim in `docs/observability.md` is corrected. No decoded-value path remains.
- **The `deriveEffectiveLease` ancestry guard is logically sound and cannot be
  tricked into force-pushing over genuine external divergence.** If the remote
  tip is an ancestor of local HEAD, HEAD provably contains the remote's history,
  so the fast-forward loses nothing; every other case (remote tip absent locally,
  or present-but-not-ancestor) fails closed with exit 11. Both the refresh path
  and the external-divergence-reject path are covered by tests.
- **The medium-fixture gap (#4) is closed** via the README RWO delete/recreate
  step; `charts/tide/values.yaml` was not hand-edited by these plans.

The **CR-01 fix is incomplete** and re-opens the exact duplicate-reporter window
it exists to close (see CR-01 below). This is the primary finding.

## Critical Issues

### CR-01: Reporter-spawn marker gate re-opens after the planner Job's 600s TTL-GC (duplicate reporter reintroduced)

**File:**
- `internal/controller/milestone_controller.go:657-663`
- `internal/controller/phase_controller.go:610-616`
- `internal/controller/plan_controller.go:654-660`
- `internal/controller/project_controller.go:1907-1913`
- (contract documented in `api/v1alpha3/milestone_types.go:94-95`)

**Issue:** The durable marker's `spawnKey` mixes two incompatible key spaces:

```go
milestoneJobName := fmt.Sprintf("tide-milestone-%s-1", ms.UID)
spawnKey := milestoneJobName
if completedJob != nil {
    spawnKey = string(completedJob.UID)   // per-attempt UID when the Job is present
}
// ... else spawnKey stays the deterministic NAME when the Job is nil
if ms.Status.MilestoneReporterSpawnedUID == spawnKey { /* skip */ }
```

The stored marker is a Job **UID** (stamped on the first completion, while the
planner Job is still present), but the `spawnKey` recomputed on a later reconcile
becomes the deterministic **name** once the planner Job is gone. A UID never
equals `tide-<level>-<uid>-1`, so the gate mismatches and re-opens.

This fires deterministically in real runs:

1. `t=0` — planner Job completes (present, terminal). `handleJobCompletion` runs
   with `completedJob != nil` → `spawnKey = JobUID` → reporter #1 spawned →
   marker stamped `= JobUID`.
2. The level stays `Running` while its children execute, re-entering
   `handleJobCompletion` every 5s (milestone_controller.go:851-874 requeue loop /
   the approve-gate hold), for the **entire** child-execution duration.
3. `t≈600s` — the planner Job hits `DefaultTTLSecondsAfterFinished = 600`
   (`internal/dispatch/podjob/jobspec.go:75`) and is GC'd. The next reconcile
   takes the `Get → NotFound → handleJobCompletion(ms, nil)` branch
   (milestone_controller.go:286-289) → `completedJob == nil` → `spawnKey`
   falls back to the deterministic **name** → `marker (JobUID) != name` → **gate
   re-opens → reporter #2 is created** (reporter #1 was already GC'd at its own
   300s TTL, so the inner name-based check in `spawnReporterIfNeeded` does not
   stop it) → marker re-stamped `= name`.

The result is a second `tide-reporter` Job with freshly-recomputed
`ReporterOptions` re-materializing children and re-synthesizing the planner's LLM
message spans — exactly the "sustained-reconcile parent re-Creates a second
reporter" failure `47-VERIFICATION.md` gap #2 describes and that produced the
partial-enrichment proof result (115/386 spans). The fix reduces the frequency
(from every-300s to once-per-completion at the 600s boundary) but does **not**
close the window: any non-leaf milestone/phase/plan and the project level whose
children take >600s (i.e., essentially every real run) still spawns one duplicate
reporter. The Task site is unaffected — it early-returns on `completedJob == nil`
and always keys on the live UID (`task_controller.go:1085,1102`).

**Fix:** Make the gate honor a set marker regardless of the Job's
presence/absence, rather than recomputing a name-derived key that can never match
a UID-derived stored value. Treat a non-empty marker on a nil-Job reconcile as
"already spawned":

```go
// spawnKey is only meaningful when we can observe the live Job UID.
// Once ANY marker is set, a later nil-Job reconcile must NOT recompute a
// name-derived key and re-open the gate.
alreadySpawned := ms.Status.MilestoneReporterSpawnedUID != "" &&
    (completedJob == nil ||
        ms.Status.MilestoneReporterSpawnedUID == string(completedJob.UID))
if alreadySpawned {
    isFirstCompletion = false
} else {
    // spawn; stamp with string(completedJob.UID) when non-nil, else the
    // deterministic name (the genuine "restart, Job already GC'd, never
    // spawned" case where the marker is still empty).
}
```

Apply identically at all four planner-level sites. Add an envtest that leaves the
level `Running`, deletes the planner Job (simulating the 600s TTL-GC), reconciles,
and asserts no second `tide-reporter-<uid>` Job is created and the marker is
unchanged.

## Warnings

### WR-01: Planner budget-rollup retry is coupled to `isFirstCompletion`; the correct CR-01 fix must decouple it

**File:**
- `internal/controller/milestone_controller.go:719-720`
- `internal/controller/phase_controller.go:669-670`
- `internal/controller/plan_controller.go:731-732`
- `internal/controller/project_controller.go` (planner-rollup site)

**Issue:** The exactly-once budget rollup is gated `if isFirstCompletion &&
envReadOK && project != nil`, and only inside that block is the durable
`*RolledUpUID` marker checked/stamped. `isFirstCompletion` is true only on the
one reconcile that (re-)spawns the reporter. So if `RollUpUsage` fails transiently
on that reconcile, `*RolledUpUID` stays unset but every subsequent reconcile has
`isFirstCompletion == false` and never retries — the rollup is silently lost.
Today the only thing that resurrects it is the buggy 600s re-spawn from CR-01
(which flips `isFirstCompletion` true again). A correct CR-01 fix removes that
accidental retry, making the transient-failure loss permanent — so this must be
addressed together with CR-01, not after.

Note the `*RolledUpUID` guard keys on the stable **name**, so it correctly
prevents double-counting across the CR-01 re-spawn; the problem is purely the
missed-retry direction, not double-count.

**Fix:** Check the `*RolledUpUID` marker on every reconcile independently of
`isFirstCompletion` (mirroring how `*SpanEmittedUID` is gated in the same files):

```go
if envReadOK && project != nil && ms.Status.MilestoneRolledUpUID != milestoneJobName {
    if rollErr := budget.RollUpUsage(...); rollErr == nil {
        // RetryOnConflict stamp MilestoneRolledUpUID = milestoneJobName
    }
}
```

This retries until success, then the marker stops it — no dependency on the
reporter-spawn re-entry.

## Info

### IN-01: `deriveEffectiveLease` present-but-not-ancestor reject branch is untested

**File:** `cmd/tide-push/main.go:1024-1036`

**Issue:** `TestRunPushModeRejectsExternalRemoteAdvance` (main_test.go:511)
exercises only the "remote tip not present in the local object DB"
reject path (`repo.CommitObject(remoteTip)` error, line 1008-1015). The second
divergence branch — remote tip **present** locally (e.g. a shared object) but
`!remoteTipCommit.IsAncestor(headCommit)` (line 1024-1036) — has no test. The
logic is correct, but this is the subtler of the two fail-closed paths and is the
one most likely to regress under future refactors.

**Fix:** Add a test where the divergent remote commit shares the local object DB
but is not an ancestor of HEAD (e.g. a sibling commit off the same base), and
assert exit 11 / `lease-rejected` with the remote tip left untouched.

### IN-02: Remote-read failure degrades to a bounded lease-flap

**File:** `cmd/tide-push/main.go:989-993`

**Issue:** On `RemoteBranchTip` error, `deriveEffectiveLease` falls back to the
possibly-stale `cfg.LastPushedSHA`. If the read failure is persistent (auth /
network), the subsequent `--force-with-lease` push keeps failing non-fast-forward
and the controller keeps retrying — the original stale-lease flap, gated behind a
transient transport fault. This is an acceptable conservative degrade (the same
transport fault would fail the push regardless, and `classifyPushError` surfaces
it), but worth noting: the flap is only fully eliminated when the remote read
succeeds. No change required; documented for awareness.

---

## Resolution (applied 2026-07-17)

- **CR-01 (Critical) — FIXED** in `37f4a46`. The gate at all four planner-level
  sites (milestone/phase/plan/project) now uses an `alreadySpawned` predicate —
  `marker != "" && (completedJob == nil || marker == spawnKey)` — so a non-empty
  durable marker is honored on the nil-Job (600s planner-Job TTL-GC) re-entry
  instead of recomputing a name-derived key that can never equal a stored UID.
  The Task site was already immune and is untouched. Proven by `271df2a`, which
  adds planner-Job TTL-GC re-entry specs (milestone shared-helper shape + project
  inline arm) that delete the planner Job (`completedJob == nil`) while the level
  is still Running and assert no duplicate reporter + unchanged marker; the
  pre-fix gate fails the new milestone spec (regression-proven).
- **WR-01 (Warning) — FIXED** in `37f4a46`. The exactly-once budget rollup is
  decoupled from `isFirstCompletion` at all four sites; the durable `*RolledUpUID`
  marker is now the sole guard, so a transient `RollUpUsage` failure retries on a
  later reconcile instead of being silently lost. `out.Usage`/`envReadOK` verified
  in-scope and valid on nil-Job reconciles at each site.
- **IN-01 / IN-02 (Info) — DEFERRED.** IN-01 (add a test for the
  present-but-not-ancestor lease-reject branch) and IN-02 (documented conservative
  degrade on remote-read failure) are non-blocking; the `deriveEffectiveLease`
  logic is already correct. Left for a follow-up hardening pass.
- **Verified:** `make build`+`go vet` clean; `make test-heavy` **46/46 heavy specs
  green, 0 failed**; `make test` unit tier green.

---

_Reviewed: 2026-07-17T19:24:50Z_
_Reviewer: Claude (gsd-code-reviewer)_
_Depth: standard_
_Resolution applied: 2026-07-17 (post-review fix, verified)_
