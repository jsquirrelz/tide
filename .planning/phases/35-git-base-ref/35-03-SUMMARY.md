---
phase: 35-git-base-ref
plan: 03
subsystem: project-reconciler
tags: [clone-classification, baseref-halt, basesha-stamp, envtest, generation-scoped]
status: complete
requires:
  - "35-01: GitConfig.BaseRef (spec) + GitStatus.BaseSHA (status) in both API versions"
  - "35-02: CloneResult envelope (reason baseref-unresolvable / baseSHA), EnsureRunBranch(baseRef) contract"
provides:
  - "tidev1alpha2.ReasonBaseRefUnresolvable condition Reason constant"
  - "CloneOptions.BaseRef + buildCloneJob --base-ref/--project-uid args + clone-container termination-message wiring"
  - "pushResultEnvelope.BaseSHA/.BaseRef (CloneResult keys)"
  - "generation-scoped clone-dispatch halt (baseRefHalted third gate)"
  - "read-before-flip baseSHA stamp on clone success (cloneEnvelopeReadCutoff=60s)"
  - "WR-03 baseref-unresolvable classification with stale-envelope guard"
affects:
  - "35-04: kind happy-path e2e for baseRef + docs consume this controller behavior"
tech-stack:
  added: []
  patterns:
    - "classify-don't-retry via generation-scoped typed condition (billing-halt precedent extended to clone path)"
    - "read-before-flip status stamp bounded by a sub-TTL cutoff (RESEARCH Pattern 2)"
key-files:
  created:
    - internal/controller/project_baseref_halt_test.go
  modified:
    - api/v1alpha2/shared_types.go
    - internal/controller/push_helpers.go
    - internal/controller/push_helpers_test.go
    - internal/controller/project_controller.go
    - internal/controller/project_clone_idempotency_test.go
decisions:
  - "Condition Type=CloneFailed, Reason=BaseRefUnresolvable (reuse the existing condition type; new Reason distinguishes the halt class from CloneJobFailed's delete-and-re-dispatch class)"
  - "cloneEnvelopeReadCutoff = 60s (well under the clone Job's 300s TTLSecondsAfterFinished) — the read-before-flip requeue must resolve before TTL GC or the re-clone/no-fresh-envelope stall of Pitfall 6 recurs"
  - "Stale-envelope guard: halt only when envelope.baseRef == current spec.git.baseRef; a mismatch (operator edited spec while the failed Job lingers) falls through to delete-and-re-dispatch so the corrected ref gets a fresh clone"
  - "ReasonBaseRefUnresolvable is v1alpha2-only — v1alpha1 declares no ConditionCloneFailed constant (only a doc comment), so no P9 mirror needed"
metrics:
  duration: "~15 min"
  completed: "2026-07-07"
  tasks: 2
  files_changed: 6
---

# Phase 35 Plan 03: Git Base Ref (controller classification + baseSHA stamp) Summary

Taught the ProjectReconciler to (1) plumb `spec.git.baseRef` to the clone Job,
(2) read the CloneResult envelope off the clone pod's termination message,
(3) classify the `baseref-unresolvable` failure into a generation-scoped,
TTL-GC-proof halt (classify-don't-retry), and (4) stamp `status.git.baseSHA`
from the success envelope in the same patch that flips `CloneComplete`
(read-before-flip). This is where 35-02's envelope contract meets 35-01's CRD
fields.

## What shipped

**Task 1 — plumbing, classification, halt gate, baseSHA stamp (commits `19e472d` RED, `ae4c026` GREEN)**
- `api/v1alpha2/shared_types.go`: `ReasonBaseRefUnresolvable = "BaseRefUnresolvable"` beside `ConditionCloneFailed`, commented to distinguish it (halts) from `CloneJobFailed` (delete-and-re-dispatch). v1alpha2-only — v1alpha1 has no such constant.
- `push_helpers.go`: `CloneOptions.BaseRef`; `buildCloneJob` now appends `--project-uid=<uid>` unconditionally (keys the clone envelope PVC copy), `--base-ref=<ref>` when set, and carries `TerminationMessagePath:/dev/termination-log` + `FallbackToLogsOnError` on the clone container (verbatim parity with `buildPushJob` — without it the controller could never read a clone envelope).
- `project_controller.go`, four coordinated edits:
  1. `pushResultEnvelope` gains `BaseSHA` (`baseSHA`) + `BaseRef` (`baseRef`) — `readPushEnvelope` reused as-is with `cloneJobName`.
  2. Dispatch guard third gate: `baseRefHalted` (via `meta.FindStatusCondition`) skips clone dispatch when `CloneFailed=True && Reason==BaseRefUnresolvable && cond.ObservedGeneration == project.Generation`; also wires `cloneOpts.BaseRef = project.Spec.Git.BaseRef`.
  3. Set-on-success arm → read-before-flip: reads the envelope FIRST; on ok, one `MergeFrom` patch sets `BaseSHA`, `CloneComplete=true`, and `CloneFailed=False`(`CloneSucceeded`, ObservedGeneration); on not-ok, requeues 10s unless the Job's `completionTime` is older than `cloneEnvelopeReadCutoff` (60s), in which case it flips with empty baseSHA and logs degraded provenance.
  4. WR-03 arm → classification branch BEFORE the existing delete: on envelope `reason=="baseref-unresolvable"` AND `env.BaseRef == current spec baseRef`, stamps `CloneFailed=True/BaseRefUnresolvable/ObservedGeneration` with the Argo-CD-canonical message and returns with NO delete and NO requeue; every other combination (stale ref, other reason, unparseable) falls through to the unchanged delete-and-re-dispatch + `CloneJobFailed`.

**Task 2 — envtest behavioral lock (commit `cc34c3b`)**
- `internal/controller/project_baseref_halt_test.go` (Ginkgo, `Label("envtest")`), seven specs: A classify+no-delete; B TTL-GC-proof halt (delete Job → no re-dispatch); C release-on-edit (generation bump → fresh clone); D baseSHA+CloneComplete stamped in one snapshot + unpruned spec round-trip; E1 requeue-within-cutoff; E2 flip-empty-past-cutoff; F auth-failed keeps delete-and-re-dispatch.
- Adapted the sibling `BYPASS-02 clone idempotency` Spec 2 to the new read-before-flip contract (see Deviations).

## Contract handed to plan 35-04 (verifier / e2e notes)

- **Condition identifiers:** Type `CloneFailed` (`tidev1alpha2.ConditionCloneFailed`), Reason `BaseRefUnresolvable` (`tidev1alpha2.ReasonBaseRefUnresolvable`). Message: `unable to resolve '<ref>' to a commit SHA; fix spec.git.baseRef to re-attempt the clone`.
- **Cutoff constant:** `cloneEnvelopeReadCutoff = 60 * time.Second` (must stay well under the 300s clone Job TTL).
- **Stale-envelope (mismatched-spec) fall-through:** when the failed envelope's `baseRef` no longer matches `spec.git.baseRef` (operator edited the spec while the failed Job lingers), the controller does NOT halt — it delete-and-re-dispatches so the corrected ref clones fresh. 35-04's kind happy-path should base a run off a real non-default branch/tag and assert `status.git.baseSHA` is stamped (40-hex) and the run branch tips at it; an unresolvable-ref e2e should see `CloneFailed/BaseRefUnresolvable` and NO second `tide-clone-*` Job after the first fails.
- **Behavioral halt is the condition, not Job existence** — do not assert on Job presence to prove the halt; assert on the condition + absence of re-dispatch.

## Verification (observed)

- `go test ./internal/controller/ -run TestBuildCloneJob -count=1` — **ok** (3 new buildCloneJob tests: base-ref present/absent, project-uid always, termination wiring).
- Focused envtest `-ginkgo.focus='baseRef classification'` — **Ran 7 of 198 (with sibling), all PASS** (Specs A/B/C/D/E1/E2/F).
- `make test` (full unit tier) — **MAKE_EXIT=0**; `grep -nE '^--- FAIL|^FAIL\s'` on the log → none. (First run surfaced a real regression in the sibling idempotency Spec 2 — fixed, see Deviations — then green.)
- `go build ./internal/... ./api/... ./pkg/... ./cmd/tide-push/...` — exit 0; `go vet ./internal/controller/ ./api/v1alpha2/` — clean; `gofmt -l` on all touched files — clean.
- Build scoped to touched packages; the pre-existing `cmd/tide-demo-init` `//go:embed` failure on base `main` is unrelated and untouched.
- Grep-level source sanity: dispatch guard references `ReasonBaseRefUnresolvable` + `cond.ObservedGeneration == project.Generation`; success arm sets `BaseSHA`+`CloneComplete`+`CloneFailed=False` in one patch and references `cloneEnvelopeReadCutoff`; WR-03 classified branch has no `r.Delete` before its return and interpolates `env.BaseRef` into the message.

## Deviations from Plan

### Auto-fixed regressions

**1. [Rule 1 — Bug] Read-before-flip broke sibling idempotency Spec 2**
- **Found during:** Task 2 (`make test` full-tier run).
- **Issue:** `BYPASS-02 clone idempotency` Spec 2 (`project_clone_idempotency_test.go:206`) marked the clone Job Succeeded with NO envelope pod and asserted `CloneComplete` flips immediately. The plan's new read-before-flip contract (edit 3) requeues within the cutoff when no envelope is readable, so the old assertion timed out. This is the intended contract change; the sibling test asserted the superseded behavior.
- **Fix:** Provided a minimal success `CloneResult` envelope pod in Spec 2 so the happy path flips immediately (the realistic path); the no-envelope requeue/flip behavior is separately locked by the new suite's Spec E1/E2. Added `encoding/json` import.
- **Files modified:** `internal/controller/project_clone_idempotency_test.go`
- **Commit:** `cc34c3b`

### Within-latitude choices

- **Spec E split into two `It` blocks (E1/E2).** A Job's `startTime`/`completionTime` are immutable once set on an unsuspended Job, so a single fabricated Job cannot be observed both fresh (requeue branch) and past-cutoff (flip branch). Each spec fabricates its own Job and sets the completion time exactly once — same coverage, respects Job status validation. The plan permitted "backdate CompletionTime … prefer backdating"; backdating a second time is what the immutability rejects, hence the split.
- **CloneComplete-clear condition Reason `CloneSucceeded`** chosen for the success-arm `CloneFailed=False` stamp (plan said "Reason \"CloneSucceeded\"" — used verbatim).

## Threat surface

No new surface beyond the plan's `<threat_model>`. T-35-03 mitigated: the ref reaches the condition message only through the 35-01 admission Pattern (no control chars/newlines) and is interpolated once into a fixed template. T-35-05 mitigated: any envelope parse failure returns `(zero,false)` from `readPushEnvelope` → the generic delete-and-re-dispatch path, never the halt and never the stamp.

## Self-Check: PASSED
