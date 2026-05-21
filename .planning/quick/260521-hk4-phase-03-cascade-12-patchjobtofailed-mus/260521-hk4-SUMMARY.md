---
id: 260521-hk4-phase-03-cascade-12-patchjobtofailed-must-set-failuretarget
title: "Phase 03 Cascade 12: patchJobToFailed must set FailureTarget=True before Failed=True (K8s 1.31+)"
type: quick
status: complete
created: 2026-05-21
completed: 2026-05-21
phase: quick
plan: "260521-hk4"
wave: 1
depends_on: []
requirements: [cascade-12-failuretarget]
key-files:
  modified:
    - test/integration/kind/push_lease_test.go
commits:
  - sha: 03af69b
    message: "fix(test): set FailureTarget=True before Failed=True in patchJobToFailed (cascade-12, K8s 1.31+)"
tech-stack:
  patterns:
    - "K8s 1.31+ Job status conditions ordering (FailureTarget antecedent → Failed consequent)"
metrics:
  files_modified: 1
  lines_added: 10
  lines_removed: 0
---

# Phase 03 Cascade 12: patchJobToFailed must set FailureTarget=True before Failed=True (K8s 1.31+) Summary

Closed cascade-12 by inserting an antecedent `FailureTarget=True` condition entry above the existing `Failed=True` entry inside `patchJobToFailed`'s status patch, satisfying the K8s 1.31+ Job status validator without touching production controllers.

## Decision Recap

Fixed test-side (`patchJobToFailed` helper) over any production-side change. Per the plan's `<context>` framing and CLAUDE.md Working Rule #1 (Observe First): TIDE controllers in `internal/controller/` never manually patch Jobs to `Failed` — that transition is owned by the K8s Job controller's BackoffLimit-driven Pod-failure machinery, which already authors both `FailureTarget=True` and `Failed=True` in the correct order (since K8s 1.31). The test helper is the only code path that bypasses real Pod-failure plumbing by direct `status` patching, so it is the only place the K8s 1.31+ ordering invariant needs to be authored explicitly. Production code is correct and untouched.

## Constant Resolution

`batchv1.JobFailureTarget` resolved cleanly through the existing `batchv1` import at the top of `push_lease_test.go` (the same import that line 251 already used for `batchv1.JobFailed`). No new import statement added, no goimports churn. Verified via `go vet ./test/integration/kind/...` and `go build ./test/integration/kind/...` — both exit 0 with no compile errors. This confirms the plan's Observe-First read of `k8s.io/api/batch/v1/types.go`: `JobFailureTarget` is a sibling `JobConditionType` constant of `JobFailed` in the same const block.

## Optional Refactor Decision

Did NOT factor out a `leaseRejectedConditionFields()` helper. The two map literals duplicate 5 of 6 fields, but the plan explicitly de-scoped this cleanup under `<context>` "Out of scope" and `<action>` "Anti-pattern guard rails" — cascade-12 is a correctness fix, not a refactoring pass. Author's discretion exercised conservatively: minimal diff, easy review. A future micro-quick task can extract the helper if another condition-array site emerges.

## Diff Stat

```
 test/integration/kind/push_lease_test.go | 10 ++++++++++
 1 file changed, 10 insertions(+)
```

Net change matches the plan's estimate (~12 added; actual was 10 added — the explanatory comment is 2 lines and the new map entry is 8 lines including its open/close braces).

## Verify Command Result

Plan verify command run verbatim from `<verify>` block of Task 1:

```
go vet ./test/integration/kind/... \
  && go build ./test/integration/kind/... \
  && [ "$(grep -c 'batchv1.JobFailureTarget' test/integration/kind/push_lease_test.go)" = "1" ] \
  && [ "$(grep -c 'batchv1.JobFailed' test/integration/kind/push_lease_test.go)" -ge "1" ] \
  && grep -qE 'K8s 1\.31\+ requires FailureTarget=True' test/integration/kind/push_lease_test.go
```

Exit code: **0**.

Individual gate observations:
- `go vet ./test/integration/kind/...` → exit 0
- `go build ./test/integration/kind/...` → exit 0
- `grep -c 'batchv1.JobFailureTarget' test/integration/kind/push_lease_test.go` → **1**
- `grep -c 'batchv1.JobFailed' test/integration/kind/push_lease_test.go` → **1** (existing entry preserved)
- `grep -qE 'K8s 1\.31\+ requires FailureTarget=True' test/integration/kind/push_lease_test.go` → exit 0 (comment landed)

## Runtime Gate — Deferred to Orchestrator

Per the executor constraints for this quick task, `make test-int` Stage 1 (`GINKGO_FOCUS='Push lease semantics'`, ~5–7 min) and Stage 2 (full 13-spec suite, ~18 min) were NOT run inside this worktree. The orchestrator runs the runtime gate after worktree merge, captures `/tmp/cascade-12-isolation.log` and `/tmp/cascade-12-fullsuite.log`, and authors VERIFICATION.md with `gate_decision: APPROVED|BLOCKED`. Cascade-12 closure check (`grep -cE 'cannot set Failed=True condition without the FailureTarget=true condition' /tmp/cascade-12-*.log` must return 0) is gated on that runtime artifact, not on this code-shape SUMMARY.

## Cascade Chain Status

Code-shape side of cascade-12 closed. Runtime-side closure pending the orchestrator's `make test-int` gate. Per CLAUDE.md Operating Notes "Don't predict chain terminator": even if Stage 1 + Stage 2 both go 13/13 green, that establishes the cascade-9 → 10 → 11 → 12 resolution chain reached the push-lease runtime gate APPROVED state, but does NOT prove the next phase boundary is clear. If Stage 2 surfaces a new cascade class, route to a follow-up debug session per Working Rule #1 (Observe First — read the new run's log + frontmatter before hypothesizing).

## Self-Check: PASSED

- FOUND: `test/integration/kind/push_lease_test.go` (modified, +10 lines)
- FOUND: commit `03af69b` on `worktree-agent-ade06c508bf27eed4`
- FOUND: `batchv1.JobFailureTarget` × 1 occurrence in target file
- FOUND: `batchv1.JobFailed` × 1 occurrence (existing entry preserved)
- FOUND: explanatory comment `K8s 1.31+ requires FailureTarget=True` in target file

## Deviations from Plan

None — plan executed exactly as written. No Rule 1/2/3 auto-fixes triggered, no Rule 4 architectural questions raised. Single-task plan, single atomic commit, verify command passed verbatim on first attempt.

## Out of Scope (Footer)

The following items were explicitly out of scope per the plan and remain untouched:

- `internal/controller/` — no production controller code modified. Cascade-12 is purely test-side.
- All other `test/integration/kind/*_test.go` files (suite, failure, projects_pvc, chaos_resume, credproxy, output, up_stack).
- `test/integration/kind/testdata/*.yaml` fixtures.
- `charts/tide/values.yaml` (FIXED contract per CLAUDE.md Phase 02.2 anti-pattern).
- `test/integration/kind/cluster.yaml` kind cluster spec.
- Optional `leaseRejectedConditionFields()` helper factoring — deferred to a future micro-quick if another condition-array test helper emerges.
- Cascade-10 (chaos-resume second-stage failure at `chaos_resume_test.go:230`, if surfaced by Stage 2) — separate Phase 03 follow-up.
- `make test-int` runtime gate execution — owned by the orchestrator after worktree merge.
