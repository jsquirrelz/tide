---
phase: 10-task-execution-reliability-clone-idempotency-per-run-workspa
plan: "02"
subsystem: controller
tags: [fsgroup, security-context, push-helpers, pvc-permissions, tdd]
dependency_graph:
  requires: []
  provides: [FSGroup=1000 on buildCloneJob and buildPushJob pod specs]
  affects: [internal/controller/push_helpers.go, internal/controller/push_helpers_test.go]
tech_stack:
  added: []
  patterns: [TDD red-green, new(int64) pointer style mirroring existing file convention]
key_files:
  created: []
  modified:
    - internal/controller/push_helpers.go
    - internal/controller/push_helpers_test.go
decisions:
  - Use new(int64(1000)) for FSGroup pointer — matches file's existing new(int32(2)) BackoffLimit style; NOT ptr.To (that is jobspec.go's style)
  - Mirror exact SecurityContext placement from jobspec.go:403-406 — after ServiceAccountName in PodSpec literal
metrics:
  duration: "~8 minutes"
  completed: "2026-06-08"
  tasks_completed: 1
  files_modified: 2
---

# Phase 10 Plan 02: FSGroup=1000 PodSecurityContext on buildCloneJob and buildPushJob Summary

FSGroup=1000 added to both push/clone Job pod specs via TDD, fixing the `mkdir /workspace/envelopes/push: permission denied` runtime failure (SC-2).

## Tasks Completed

| Task | Name | Commit | Files |
|------|------|--------|-------|
| 1 (RED) | Add failing FSGroup tests | 334ccf7 | internal/controller/push_helpers_test.go |
| 1 (GREEN) | Wire SecurityContext in push_helpers.go | dd85e87 | internal/controller/push_helpers.go |

## Verification Results

- `go test ./internal/controller/ -run 'TestBuildCloneJobFSGroup|TestBuildPushJobFSGroup' -v -count=1`: PASS (both tests GREEN)
- `grep -c "FSGroup" internal/controller/push_helpers.go`: 2 (one per builder)
- `go vet ./internal/controller/...`: exits 0
- `grep "values.yaml" internal/controller/push_helpers.go`: empty (FIXED contract not touched)

## Deviations from Plan

None — plan executed exactly as written. The Ginkgo envtest suite fails in this dev environment due to missing `/usr/local/kubebuilder/bin/etcd` (pre-existing environment constraint, not caused by this change). The plan's verification criterion targets the pure unit tests which run correctly without envtest.

## TDD Gate Compliance

- RED gate: `test(10-02)` commit 334ccf7 — both tests fail before implementation
- GREEN gate: `feat(10-02)` commit dd85e87 — both tests pass after implementation
- REFACTOR: not needed (two field additions only, no cleanup required)

## Known Stubs

None.

## Threat Flags

None beyond the threat model in the plan. T-10-02-A (FSGroup chown exposing PVC data to other pods) is mitigated by the existing SubPath isolation on every VolumeMount — no new cross-subPath access introduced. T-10-02-B (chart values.yaml modification) does not apply — binary-only change confirmed by verification grep.

## Self-Check: PASSED
