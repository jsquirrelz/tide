---
phase: 28-plan-import-core
reviewed: 2026-06-18T00:00:00Z
depth: standard
files_reviewed: 17
files_reviewed_list:
  - cmd/tide-import/main.go
  - cmd/tide-import/main_test.go
  - internal/controller/import_controller.go
  - internal/controller/import_jobspec.go
  - internal/controller/import_controller_test.go
  - internal/controller/import_guard_test.go
  - internal/controller/project_controller.go
  - internal/controller/milestone_controller.go
  - internal/controller/phase_controller.go
  - internal/controller/plan_controller.go
  - internal/controller/task_controller.go
  - cmd/manager/main.go
  - api/v1alpha2/import_types.go
  - api/v1alpha2/project_types.go
  - api/v1alpha2/shared_types.go
  - images/tide-import/Dockerfile
  - charts/tide/values.yaml
  - charts/tide/templates/deployment.yaml
findings:
  critical: 3
  warning: 4
  info: 3
  total: 10
status: issues_found
---

# Phase 28: Code Review Report

**Reviewed:** 2026-06-18
**Depth:** standard
**Files Reviewed:** 17
**Status:** issues_found

## Summary

Phase 28 implements envelope salvage (Approach B, UID-rewrite): a one-shot pre-reconcile
import state machine in `ImportReconciler` that materializes seed-derived Milestone/Phase/Plan
CRs with fresh UIDs, then dispatches a `tide-import` Job to copy + re-key the foreign
envelope tree on the shared PVC.

The **controller-side logic is solid**: seed-derived cycle detection runs `dag.ComputeWaves`
on the full Milestone/Phase/Plan node set *before* any `client.Create` (D-10 atomicity holds —
the cycle test proves zero partial CRs); the idempotency guard and `AlreadyExists`-as-success
paths are correct; the 5-site park guards are all positioned before `PlannerPool.Acquire`
(no slot leak); budget rollup is correctly suppressed when `spec.ImportSource != nil`; the
path-traversal `containedJoin` and Kind-allowlist + typed-roundtrip conversion in the binary
are sound and well-tested; `values.yaml` is additive-only; `deployment.yaml` mirrors the
`tideReporter` env pattern correctly.

**However, the actual Job-dispatch path is broken by three independent runtime defects, none
of which any test exercises** — every test runs with `ImportImage=""`, which takes the
dev-mode skip and never builds, mounts, or runs the real Job. When `TIDE_IMPORT_IMAGE` is set
in production (the chart default sets it), the import Job will fail at container start or at
first `cat`, so **no envelope will ever actually be imported**. These are the headline blockers.

## Structural Findings (fallow)

No structural pre-pass (`<structural_findings>`) was provided with this review.

## Narrative Findings (AI reviewer)

## Critical Issues

### CR-01: import Job command uses `sh -c` but the distroless base image has no shell

**File:** `internal/controller/import_jobspec.go:131-134`, `images/tide-import/Dockerfile:38`
**Confidence:** High
**Issue:** The Job container command is
```go
command := []string{
    "sh", "-c",
    "cat /rekey/rekey.json | tide-import --old-workspace=/old-workspace --new-workspace=/new-workspace",
}
```
but the runtime base image is `gcr.io/distroless/static:nonroot`, which contains **no `/bin/sh`**
(it ships only the static binary, no shell, no `cat`, no coreutils). The kubelet will fail to
start the container with `exec: "sh": executable file not found in $PATH`, and the Job will
crashloop to `BackoffLimitExceeded`. The reporter Job that this code claims to mirror uses
`Args:` against the binary ENTRYPOINT directly (`reporter_jobspec.go:197`) — it never invokes
a shell, precisely because the same distroless base has none. No test catches this: every
test sets `ImportImage=""` and skips Job creation entirely.
**Fix:** Drop the shell. Mount the rekey ConfigMap so the binary reads the file directly, or
pass the rekey path as a flag and have the binary `os.Open` it instead of reading stdin. Then:
```go
command := nil // use ENTRYPOINT
args := []string{
    "--old-workspace=/old-workspace",
    "--new-workspace=/new-workspace",
    "--rekey-file=/rekey/rekey", // see CR-03 for the key/path
}
```
and add a `--rekey-file` flag in `cmd/tide-import/main.go` that `os.ReadFile`s the table instead
of decoding `os.Stdin`. (If stdin must be preserved, the base image needs a shell — but that
contradicts the firewall-clean distroless decision.)

### CR-02: rekey table is written as a JSON object (map) but the binary decodes a JSON array

**File:** `internal/controller/import_controller.go:370,404,452,499,511` vs `cmd/tide-import/main.go:121-125`
**Confidence:** High
**Issue:** The controller marshals the rekey table as a **map keyed by FQ-name**, where each
value is a `rekeyEntry{OldUID, NewUID}` with **no `fqName` field**:
```go
rekeyTable := make(map[string]rekeyEntry) // {"ms-x":{"oldUID":"...","newUID":"..."}}
rekeyJSON, _ := json.Marshal(rekeyTable)
```
But the binary decodes stdin into a **slice** of a *different* `rekeyEntry` that *does* carry
`fqName`:
```go
var table []rekeyEntry // expects [{"fqName":...,"oldUID":...,"newUID":...}]
json.NewDecoder(stdin).Decode(&table)
```
Decoding a JSON object `{...}` into a Go slice `[]rekeyEntry` fails with
`json: cannot unmarshal object into Go value of type []rekeyEntry` → the binary returns
`exitInvariant` (2) on the first line. Even if CR-01 and CR-03 are fixed, the import Job can
never parse its input. The two `rekeyEntry` types (controller `import_controller.go:133` and
binary `main.go:80`) have silently diverged. Note the binary keys the source directory off
`entry.OldUID`/`entry.NewUID`, so the controller MUST emit those per row.
**Fix:** Make the controller emit the array shape the binary consumes:
```go
type rekeyRow struct {
    FQName string `json:"fqName"`
    OldUID string `json:"oldUID"`
    NewUID string `json:"newUID"`
}
var rekeyTable []rekeyRow
// ...
rekeyTable = append(rekeyTable, rekeyRow{FQName: fqName, OldUID: msSeed.OldUID, NewUID: string(ms.UID)})
```
(The envtest at `import_controller_test.go:229-234` asserts the *map* shape — that assertion
must be updated to match whichever shape both sides agree on.)

### CR-03: rekey ConfigMap key is `"rekey"` but the Job reads `/rekey/rekey.json`

**File:** `internal/controller/import_controller.go:521-523`, `internal/controller/import_jobspec.go:133,164-173,197-202`
**Confidence:** High
**Issue:** The ConfigMap is created with key `"rekey"`:
```go
Data: map[string]string{ "rekey": string(rekeyJSON) },
```
The volume mounts the whole ConfigMap at `MountPath: "/rekey"` with no `items`/`KeyToPath`
remap, so the on-disk file is `/rekey/rekey` (filename == key). But the command reads
`cat /rekey/rekey.json`. The file `/rekey/rekey.json` does not exist → `cat` fails with
"No such file or directory" (a third independent dispatch-path failure). The doc comments at
`import_jobspec.go:78,97` assert "mounted as a file at /rekey/rekey.json" which the mount does
not produce.
**Fix:** Make the key and the read path agree. Either set the CM key to `"rekey.json"`:
```go
Data: map[string]string{ "rekey.json": string(rekeyJSON) },
```
or (preferred, paired with CR-01) pass `--rekey-file=/rekey/rekey` and drop the `.json`. If you
keep the whole-CM mount, the filename is always the key — pick one and align all three sites
(CM key, mount, binary read path).

## Warnings

### WR-01: dead functions `copyDirNoClobber` and `rewriteTaskUID` will fail `staticcheck` (CI lint)

**File:** `cmd/tide-import/main.go:360-396` (`copyDirNoClobber`), `436-458` (`rewriteTaskUID`)
**Confidence:** High
**Issue:** `copyDirNoClobber` is only ever called by itself (the recursive call at line 377);
`run()` uses `copyDirNoClobberExcluding`. `rewriteTaskUID` is never called at all — its own doc
comment admits "the main run() path now uses atomicWriteJSON on the fully-converted envelope
instead." Neither is referenced by tests. `staticcheck`'s `U1000` (enabled per CLAUDE.md's
golangci-lint config: "gosec, errcheck, staticcheck on") will flag both as unused, failing CI
lint. CLAUDE.md is explicit: "Run the linter and fix offenses before declaring complete."
**Fix:** Delete both functions. If `rewriteTaskUID` is retained for a future flag, wire it or
add an explicit `//nolint:unused` with a justification comment.

### WR-02: completeness guard accepts a planner envelope with `ChildCount==0` and populated `ChildCRDs`

**File:** `cmd/tide-import/main.go:265-275`
**Confidence:** Medium
**Issue:** The doc comment (line 169) states the rule is "exitCode != 0 OR `len(ChildCRDs) != ChildCount`
→ incomplete," but the implementation only flags the mismatch when `ChildCount > 0`:
```go
if env.ChildCount > 0 && len(env.ChildCRDs) != env.ChildCount {
    return false
}
```
`pkg/dispatch/envelope.go` marks `ChildCount` as `omitempty` and notes it is "Zero for
executor-level." A salvaged on-disk `out.json` from a planner that authored children but whose
`ChildCount` was not stamped (or was zero) would have `ChildCount==0, len(ChildCRDs)==5` and
**pass** the completeness guard, importing a child set the guard was meant to validate. The
guard's stated invariant and its code disagree.
**Fix:** Decide the contract explicitly. If `ChildCount==0` with non-empty `ChildCRDs` is
genuinely impossible for a complete envelope, assert it:
```go
if len(env.ChildCRDs) != env.ChildCount {
    return false
}
```
If `ChildCount==0` is a legitimate "unstamped" sentinel, document that and keep the `> 0`
guard — but then the line-169 comment is wrong and should be corrected.

### WR-03: `ImportReconciler.SharedPVCName` hardcoded to "tide-projects" in manager; skews from chart override

**File:** `cmd/manager/main.go:570` (also pre-existing at line 396 for ProjectReconciler)
**Confidence:** Medium
**Issue:** The chart exposes `workspaces.pvc.name` (default `tide-projects`,
`charts/tide/templates/projects-pvc.yaml:5`), so an operator can rename the shared PVC. But
the manager wires `SharedPVCName: "tide-projects"` as a string literal rather than threading
the configured name through a flag/env. If an operator sets `workspaces.pvc.name=foo`, the
import Job will mount a non-existent `tide-projects` PVC and the Pod will hang `Pending`
(unschedulable, volume not found). CLAUDE.md flags chart-vs-binary skew as a recurring
ship-blocker class. This mirrors an existing hardcode at line 396, so it is consistent with the
current codebase — but Phase 28 propagates the latent bug to a new surface.
**Fix:** Thread the PVC name from a `--workspaces-pvc-name` flag / env (chart-injected) into
both `ProjectReconciler.PVCName`/`SharedPVCName` and `ImportReconciler.SharedPVCName`, instead
of the literal. At minimum, add a TODO referencing the chart key so the skew is visible.

### WR-04: import Job has no `ActiveDeadlineSeconds`; a hung copy ties up the PVC mount indefinitely

**File:** `internal/controller/import_jobspec.go:113-145`
**Confidence:** Low
**Issue:** The Job sets `BackoffLimit=2` and `TTLSecondsAfterFinished=300` but no
`ActiveDeadlineSeconds`. The binary's `run()` honors `ctx` cancellation only between table
rows (`main.go:130`), and a single large recursive `copyDirNoClobberExcluding` call (which
reads whole files into memory, `main.go:406`) is not cancellable mid-copy. A stuck NFS mount or
an enormous envelope tree could keep the pod (and its RW PVC mount) alive well past any
reasonable bound, blocking the reconcile poll loop's 5s requeue from ever observing terminal
state. The reporter Job is short-lived by nature; the import Job can copy an entire prior run.
**Fix:** Add a bounded `ActiveDeadlineSeconds` to the Job spec (e.g. 600s, sized to the largest
expected salvage) so a hung copy is force-terminated and surfaces as `JobFailed` →
`ReasonImportFailed` rather than an indefinite stall.

## Info

### IN-01: `currentImportState` overloads the condition `Message` field for state tracking (fragile)

**File:** `internal/controller/import_controller.go:236-265`
**Confidence:** Medium
**Issue:** In-progress state is distinguished by string-matching `cond.Message == "CreatingCRs"`
/ `"CopyingEnvelopes"` (lines 250-255) AND `cond.Reason` (lines 258-263). The `Message` field
is human-facing and easily changed; using it as a state-machine discriminant is brittle — a
future copyedit of the message string silently breaks state transitions. The reason-based
checks at 258-263 are also unreachable for `ReasonImportFailed`/`ReasonCyclicPlanDetected`
because lines 245-256 already `return` for those reasons.
**Fix:** Track import phase in a dedicated, machine-owned field — either a typed `Reason`
constant per phase (e.g. `ReasonImportCreatingCRs`) or a `Status.Import.Phase` enum — rather
than parsing `Message`. This also removes the dead reason-checks at 258-263.

### IN-02: `importStatePhase` constants are gofmt-misaligned and `importStateFailed` is effectively unused as a transition target

**File:** `internal/controller/import_controller.go:76-81`
**Confidence:** Low
**Issue:** The const block's alignment (`importStateCreatingCRs       importStatePhase`) is not
gofmt-canonical for the group (trailing-space alignment varies), and `gofmt -l` would rewrite
it. Minor, but CLAUDE.md requires running the formatter before declaring complete.
**Fix:** Run `gofmt -w internal/controller/import_controller.go`.

### IN-03: idempotency-test asserts no Job via the wrong object type (dead/misleading test code)

**File:** `internal/controller/import_controller_test.go:526-532`
**Confidence:** Medium
**Issue:** The "no import Job created" assertion lists `corev1.PodList` and a dummy
`corev1.ConfigMapList`, never actually checking for a `batchv1.Job` with the deterministic name
`jobName` (which is computed but then discarded via `_ = jobName`). The test comment concedes
"With ImportImage='' no Job is created" — so the assertion is a no-op that proves nothing about
Job non-creation. It will pass regardless of whether a Job exists.
**Fix:** Either list `batchv1.JobList` and assert the deterministic name is absent, or delete
the misleading block and rely on the `ImportImage==""` skip being covered elsewhere.

---

_Reviewed: 2026-06-18_
_Reviewer: Claude (gsd-code-reviewer)_
_Depth: standard_
