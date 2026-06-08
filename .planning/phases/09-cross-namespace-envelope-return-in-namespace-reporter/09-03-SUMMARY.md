---
phase: 09-cross-namespace-envelope-return-in-namespace-reporter
plan: "03"
subsystem: dispatch-envelope / subagent-runtime
tags: [cross-namespace, prompt-path, traversal-defense, defect-10b, in-pod-read]
dependency_graph:
  requires: [09-01, 09-02]
  provides: [EnvelopeIn.PromptPath, in-pod-prompt-read]
  affects: [pkg/dispatch/envelope.go, internal/controller/task_controller.go, internal/subagent/anthropic/subagent.go]
tech_stack:
  added: []
  patterns: [pure-filesystem-traversal-defense, TDD-red-green, in-pod-prompt-resolution]
key_files:
  created: []
  modified:
    - pkg/dispatch/envelope.go
    - internal/controller/task_controller.go
    - cmd/manager/main.go
    - internal/subagent/anthropic/subagent.go
    - internal/subagent/anthropic/subagent_test.go
    - internal/controller/task_controller_extracted_test.go
    - internal/controller/task_controller_test.go
    - internal/controller/suite_test.go
decisions:
  - "PromptPath in EnvelopeIn is omitempty; Prompt remains for planner dispatches (backward-compatible split)"
  - "readPromptArtifact lives in the anthropic package (pure filesystem, no K8s import) mirroring FilesystemEnvelopeReader.ReadPrompt defense"
  - "WorkspaceRoot is the base for in-pod PromptPath resolution (not WorkspaceRoot/<uid>/workspace) because subPath already places the pod inside the project subtree"
  - "PromptReader interface and FilesystemEnvelopeReader.ReadPrompt kept in podjob/backend.go — still tested there; Manager no longer wires it"
metrics:
  duration: "~20min"
  completed: "2026-06-08"
  tasks: 2
  files: 8
---

# Phase 09 Plan 03: In-Pod Prompt Read via EnvelopeIn.PromptPath Summary

Fixes defect #10b (same cross-namespace bug class as #11/#12): adds `EnvelopeIn.PromptPath` so the Manager stamps the workspace-relative path on the dispatch envelope instead of reading the prompt off its own (wrong-namespace) PVC; the in-pod anthropic runner reads the prompt artifact from its own namespace PVC and renders `{{.Prompt}}` in-pod with full path-traversal defense.

## Tasks Completed

| # | Task | Commit | Type |
|---|------|--------|------|
| 1 | Add EnvelopeIn.PromptPath; Manager stops reading the prompt | 45239d1 | feat |
| 2 (RED) | Failing tests for in-pod PromptPath read | 8dd6430 | test |
| 2 (GREEN) | In-pod prompt read in anthropic.Run with traversal defense | 7e26a1b | feat |

## What Changed

**`pkg/dispatch/envelope.go`**
- Added `PromptPath string json:"promptPath,omitempty"` to `EnvelopeIn`.
- `Prompt` is now empty for executor dispatches (set only for planner dispatches with inline outcome prompts).
- Full doc comment on `PromptPath` explains the in-pod read contract and the Manager-must-not-read prohibition (defect #10b).

**`internal/controller/task_controller.go`**
- `buildEnvelopeIn`: removed `r.Deps.PromptReader.ReadPrompt(...)` call and `Prompt: prompt` assignment.
- Replaced with `PromptPath: task.Spec.PromptPath` on the `EnvelopeIn`.
- Removed `PromptReader podjob.PromptReader` field from `TaskReconcilerDeps`.

**`cmd/manager/main.go`**
- Removed `promptReader` variable and `PromptReader: promptReader` from `TaskReconcilerDeps` wiring.

**`internal/subagent/anthropic/subagent.go`**
- Added step 2.5 in `Run()`: if `in.PromptPath != ""`, calls `readPromptArtifact(a.opts.WorkspaceRoot, in.PromptPath)` and sets `in.Prompt` before the template render.
- Added `readPromptArtifact(base, promptPath string) (string, error)` — pure-filesystem, no K8s deps — with full T-09-05 traversal defense:
  - Empty promptPath → error
  - `filepath.IsAbs(promptPath)` → rejected
  - `filepath.Clean` + `".."` prefix check → traversal rejected
  - `full != base && !strings.HasPrefix(full, base+sep)` second-line defense
  - Empty `.spec.prompt` → hard error (defect #4 class)
- Added `promptArtifact` type (mirrors `childPromptFile` shape in `podjob`).

**Tests updated**
- `internal/controller/suite_test.go`: removed `fakePromptReader` helper (no longer needed — Manager no longer reads prompt).
- `internal/controller/task_controller_extracted_test.go`: renamed `TestBuildEnvelopeIn_PromptFromPVC` → `TestBuildEnvelopeIn_PromptPath`; now asserts `envIn.PromptPath == promptPath` and `envIn.Prompt == ""`.
- `internal/controller/task_controller_test.go`: removed `PromptReader: newFakePromptReader()` from all `TaskReconcilerDeps` instances.
- `internal/subagent/anthropic/subagent_test.go`: added 6 new `TestPromptPath_*` tests covering all behavior cases from the plan.

## Verification

```
go test ./internal/subagent/anthropic/ -run 'Prompt' -count=1   # PASS (7 tests)
go test ./internal/controller/ -run 'EnvelopeIn|BuildEnvelope|Task' -short -count=1  # PASS
go test ./pkg/dispatch/... -short -count=1  # PASS
grep -c 'PromptReader.ReadPrompt' internal/controller/task_controller.go  # 0
grep -q 'PromptPath' pkg/dispatch/envelope.go  # yes
grep -qE 'PromptPath:\s*task\.Spec\.PromptPath' internal/controller/task_controller.go  # yes
```

Note: `internal/controller` Ginkgo suite requires kubebuilder envtest binaries not present in this environment — pre-existing constraint, not introduced by this plan.

## Deviations from Plan

### Auto-fixed Issues

None — plan executed exactly as written.

The plan instructed to "Drop the PromptReader field from TaskReconcilerDeps if nothing else references it (grep first)." Grepped — confirmed only `buildEnvelopeIn` used it — dropped it. This was within plan scope.

## TDD Gate Compliance

- RED gate: commit `8dd6430` — `test(09-03): add failing tests for in-pod PromptPath read in anthropic.Run` — 4 failing tests confirmed
- GREEN gate: commit `7e26a1b` — `feat(09-03): in-pod prompt read in anthropic.Run with traversal defense (T-09-05)` — all 7 tests pass

## Threat Surface Scan

No new network endpoints, auth paths, or schema changes introduced. The `readPromptArtifact` function mitigates T-09-05 (path traversal via PromptPath → read files outside workspace). T-09-06 (Manager reading cross-ns prompt silently fails) is resolved by removing the Manager-side read entirely.

## Self-Check: PASSED

- pkg/dispatch/envelope.go: FOUND
- internal/subagent/anthropic/subagent.go: FOUND
- internal/controller/task_controller.go: FOUND
- commit 45239d1: FOUND
- commit 8dd6430: FOUND
- commit 7e26a1b: FOUND
