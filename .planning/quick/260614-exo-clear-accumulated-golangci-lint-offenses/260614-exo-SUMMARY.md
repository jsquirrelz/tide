---
quick_id: 260614-exo
slug: clear-accumulated-golangci-lint-offenses
mode: quick
completed: 2026-06-14
status: complete
commits:
  - e309d84  # fix(lint): production-code offenses
  - f4af5c2  # chore(lint): test-file offenses
key-files:
  modified:
    - cmd/claude-subagent/main.go
    - cmd/dashboard/api/prometheus.go
    - cmd/manager/main.go
    - cmd/tide/approve.go
    - cmd/tide/artifact_get_run.go
    - cmd/tide/resume.go
    - internal/config/config_test.go
    - internal/controller/file_touch_gate_test.go
    - internal/controller/milestone_controller.go
    - internal/controller/milestone_gates_test.go
    - internal/controller/phase_controller.go
    - internal/controller/phase_gates_test.go
    - internal/controller/plan_controller.go
    - internal/controller/task_controller.go
    - internal/credproxy/server.go
    - internal/owner/label_test.go
    - internal/reporter/materialize_test.go
    - test/integration/envtest/gates_test.go
    - test/integration/kind/push_lease_test.go
---

# Quick Task 260614-exo: Clear accumulated golangci-lint offenses — Summary

Cleared all 31 accumulated golangci-lint offenses (Phase 12–17 drift) so `make lint`
exits 0 again. No production behavior changed — every fix is a nolint directive, dead-code
removal, const extraction, line wrap, or test-style adjustment. The full `make lint` gate
(golangci-lint + DAG/dispatch/import-firewall gates) now passes from the committed tree.

## Final gate status

- `./bin/golangci-lint run ./...` → **0 issues, exit 0**
- `make lint` → **exit 0** (DAG-05, SUB-01 import firewalls + 0 golangci issues)
- `go build ./...` → clean
- `go vet ./cmd/... ./internal/...` → clean
- Unit tests for touched packages (config, owner, reporter, credproxy, cmd/tide, cmd/dashboard) → all `ok`
- `internal/controller`, `test/integration/{envtest,kind}` test binaries compile (envtest/kind need a live cluster to run; compilation confirms the refactors are sound)

## What changed, by linter

### Production code (commit e309d84 — `fix(lint)`)

| Linter | Fix |
| --- | --- |
| gocyclo (6) | `//nolint:gocyclo` with flat-state-machine rationale above `reconcilePlannerDispatch` / `handle*JobCompletion` in milestone/phase/plan controllers. Mirrors `project_controller.go:422/564`. No refactor — these are the just-shipped gate/halt/budget state machines with fresh regression coverage. |
| unparam (2) | `//nolint:unparam` on `patchPlanFailed` (ctrl.Result) and `emitTaskMetrics` (error). Both returns ARE consumed by callers in `return r.x(...)` / `if err := r.x(...); err != nil` reconcile chains (plan_controller.go:960; task_controller.go:869/902/963 + 4 test assertions), so the return is kept, not dropped. |
| unused (4) | Removed dead `patchMilestoneFailed`, `patchPhaseFailed`, `patchTaskFailed` (zero callers — park-not-fail migration leftovers; grep-verified) and the `inspectorPodRunnerFunc` type (zero references). |
| errcheck (2) | `defer func() { _ = resp.Body.Close() }()` in prometheus.go:119; `_ = resp.Body.Close()` in credproxy/server.go:176. |
| goconst (1) | Extracted `const outputPathsViolation = "output-paths-violation"` in task_controller.go; replaced all 3 literals. (Note: PLAN listed goconst as 2 — `"unknown"` does NOT fire on the current tree, so only `output-paths-violation` was extracted.) |
| lll (5) | Wrapped >120-col lines: claude-subagent/main.go:61 (func signature), manager/main.go:378 (trailing comment moved above), approve.go:204/238 (split format string), resume.go:121 (split string concat). |
| modernize (1) | `slices.Contains` for the `..` traversal guard in artifact_get_run.go. |

### Test files (commit f4af5c2 — `chore(lint)`)

| Linter | Fix |
| --- | --- |
| copyloopvar (2) | Deleted redundant `tc := tc` (Go 1.22+) in owner/label_test.go:56, reporter/materialize_test.go:358. |
| ginkgolinter (5) | `Expect(jobsAfter.Items).To(HaveLen(n), msg)` over `Expect(len(jobsAfter.Items)).To(Equal(n), msg)` in milestone_gates_test, phase_gates_test (×3), envtest/gates_test. (PLAN said 4; the tree has 5 — one extra in phase_gates_test.) |
| modernize (1) | `slices.Contains(freshB.Spec.DependsOn, tA.Name)` in file_touch_gate_test.go:304. |
| gofmt (1) | Ran gofmt on config_test.go — folds in the pre-existing trailing-newline working-tree change as instructed. |
| staticcheck S1039 (1) | Dropped the verb-free `fmt.Sprintf` wrapper on `terminationMsg` in push_lease_test.go:274 (byte-identical raw literal). |
| ineffassign (1) | Discarded the unread happy-path `podOut` (`_, podErr := ...`) in push_lease_test.go:325; the fallback branch now declares its own `podOut, fallbackErr :=` so the error assertion still reports the kubectl output. |

## Deviations from Plan

The PLAN's offense counts were authored before the final state; the live `golangci-lint run`
reported a slightly different mix. Both deviations are documentation-only — every offense
the linter actually reported was fixed, and `make lint` is green.

1. **[Rule 1 — count correction] goconst: 1, not 2.** `"unknown"` did not fire on the current
   tree (it is not a goconst hit under the current threshold/usage). Only `output-paths-violation`
   was extracted to a const, exactly as the linter demanded.
2. **[Rule 1 — count correction] ginkgolinter: 5, not 4.** phase_gates_test.go carried three
   `len().To(Equal())` hits (lines 283/429/547), not two. All five across the three files were
   converted to `HaveLen`.

## Guardrails honored

- `charts/` — untouched.
- `.golangci.yml` — untouched.
- `.planning/config.json` (`_auto_chain_active`) — left modified in the working tree, NOT staged
  or committed.
- The pre-existing `internal/config/config_test.go` trailing-newline change was folded into the
  gofmt commit, not reverted.
- gocyclo reconcilers — nolint only, no refactor.
- Docs artifacts (PLAN/SUMMARY/STATE) and ROADMAP — left for the orchestrator.

## Self-review

Diff is 19 files, +54/−95, all within the targeted offense set. No file deletions (only
function-body removals inside files). The two non-mechanical edits (`slices.Contains` traversal
guard, push_lease_test ineffassign rework) preserve exact behavior — traversal guard is logically
identical, and the test's fallback-error assertion still surfaces the kubectl output. Build, vet,
and touched-package unit tests all pass.

## Self-Check: PASSED

- Both commits present: `e309d84`, `f4af5c2` (verified via `git rev-parse`).
- `make lint` re-run from the committed tree: exit 0.
- No charts/, .golangci.yml, or staged .planning/config.json changes.
