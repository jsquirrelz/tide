---
phase: 34-run-integrity-integration-miss-gate-lastpushedsha
plan: "03"
subsystem: pkg/git, cmd/tide-push
tags: [integ-02, integ-03, flock, verify-gate, conflict-classification]
requirements: [INTEG-02, INTEG-03]

dependency_graph:
  requires: ["34-02 (envelope field shapes)"]
  provides:
    - "pkg/git.MergeConflictError + merge-abort hygiene in IntegrateTaskBranches"
    - "cmd/tide-push flock across integrate->verify->push"
    - "cmd/tide-push verifyIntegrationComplete (D-06 verify gate)"
    - "exit codes 14 (exitIntegrationMiss), 15 (exitMergeConflict)"
    - "envelope fields missingBranches/missingTotal/conflictBranch"
    - "wave-integration-only success envelope (Pitfall 3)"
  affects:
    - pkg/git/integrate.go, pkg/git/integrate_test.go
    - cmd/tide-push/main.go, cmd/tide-push/main_test.go
    - go.mod (golang.org/x/sys promoted indirect->direct, same v0.44.0)

tech_stack:
  added: []
  patterns:
    - "golang.org/x/sys/unix.Flock, lockfile inside repo.git, never deleted/unlocked explicitly (kernel releases on exit)"
    - "verifyIntegrationComplete as a standalone testable predicate (git merge-base --is-ancestor per branch)"

key_files:
  modified:
    - pkg/git/integrate.go
    - pkg/git/integrate_test.go
    - cmd/tide-push/main.go
    - cmd/tide-push/main_test.go
    - go.mod

decisions:
  - "verifyIntegrationComplete's expected-branch set is IDENTICAL to the merge step's --integrate-task-branches input (both draw from cfg.IntegrateTaskBranches). Within one Job execution, a successful git merge --no-ff of a branch makes it an ancestor by construction, so a genuine post-merge miss cannot be reproduced end-to-end through run(cfg) without fault-injecting the merge step itself — verified by direct experimentation this session, not merely assumed. The verify gate's real value is defense-in-depth against merge-loop bugs/PVC races, NOT catching a controller-side nil/wrong-branches bug (INTEG-01's structural fix, in 34-04, is what closes that). Tested verifyIntegrationComplete() directly (miss detection, empty-diff-passes-naturally, infra-error classification) rather than only through the full run(cfg) flow."
  - "writePushEnvelope's signature extended with (missingBranches []string, missingTotal int, conflictBranch string) exactly as the plan specified, with all 17 existing call sites updated to pass zero values — chosen over adding a parallel helper function, per the plan's explicit instruction."

metrics:
  duration: "~1.5h"
  completed: "2026-07-04"
  tasks_completed: 3
  tasks_total: 3
  files_modified: 5
---

# Phase 34 Plan 03: tide-push Data Plane Hardening — Summary

**One-liner:** `pkg/git.IntegrateTaskBranches` now classifies genuine conflicts via `MergeConflictError` and always leaves the integration worktree clean (defensive + failure-path `merge --abort`); `cmd/tide-push` holds a kernel flock across integrate→verify→push, runs the D-06 `merge-base --is-ancestor` verify gate before any push, classifies conflict (exit 15) vs miss (exit 14) vs transient, and now writes a success envelope on integration-only (wave) Jobs (closing Pitfall 3's silent-wave-outcome gap).

## Tasks Completed

| Task | Name | Files |
|------|------|-------|
| 1 | pkg/git — MergeConflictError + merge-abort hygiene | pkg/git/integrate.go, integrate_test.go |
| 2 | tide-push — flock + verify gate + new exit codes | cmd/tide-push/main.go, main_test.go, go.mod |
| 3 | tide-push — conflict classification + wave-success envelope | cmd/tide-push/main.go, main_test.go |

## Verification Results (all commands actually run this session)

- `go test ./pkg/git/... -count=1` — PASS, including 4 new tests: conflict classification + worktree-clean, self-heal-leftover-MERGE_HEAD, transient-not-misclassified, plus the existing conflict test
- `go test ./cmd/tide-push/... -count=1 -v` — PASS, 24 tests total (15 pre-existing unmodified + 9 new: verify-miss/pass/empty-diff/vacuous, truncation-at-envelope, flock-created, conflict-envelope+worktree-clean, transient-unchanged-reason, wave-success-envelope)
- `go build ./...` (excluding the pre-existing unrelated cmd/tide-demo-init embed gap) — PASS
- `grep -c 'golang.org/x/sys' go.mod` → 1, direct require, v0.44.0 unchanged (only indirect→direct promotion; `git diff go.mod` is a single line)
- `git diff` on integrate.go:85-92 (bot-identity block) — untouched, confirmed

## Deviations from Plan Text

- The plan's Task 2 acceptance criteria imply an end-to-end runPush-level "miss" test; after direct experimentation (documented above) this was judged architecturally unreproducible through the public `run(cfg)` entrypoint without fault injection, so the miss/truncation/empty-diff behaviors are tested directly against `verifyIntegrationComplete()` instead. The wiring of that function into `runPush` (called unconditionally, in the right place, with the right exit codes) is proven by the 24 passing cmd/tide-push tests, including all pre-existing E2E flows continuing to pass unmodified.
