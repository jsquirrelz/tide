---
quick_id: 260614-exo
slug: clear-accumulated-golangci-lint-offenses
description: Clear accumulated golangci-lint offenses so `make lint` exits clean
created: 2026-06-14
mode: quick
must_haves:
  truths:
    - "`make lint` exits 0 (the Lint CI job and the ci 'Phase 1 gates' job go green)"
    - "No production behavior changes — fixes are nolint directives, dead-code removal, const extraction, line wrapping, and test-style adjustments only"
  artifacts:
    - ".golangci-clean tree: zero offenses from golangci-lint run"
  key_links:
    - internal/controller/project_controller.go (the nolint convention to mirror)
---

# Quick Task 260614-exo: Clear accumulated golangci-lint offenses

## Goal

`make lint` exits 0. ~33 offenses accumulated in Phase 12–17 code because `make lint` was never in the per-phase verification path (Lint + ci CI jobs red on main since 2026-06-11). This is a lint-clearing chore matching the repo precedent (commit 97741ef "chore(lint): clear 25 accumulated offenses"). The chart-reproducibility fix is already committed separately — **do NOT touch charts/**.

## Convention to follow

`internal/controller/project_controller.go` already carries `//nolint:gocyclo` (lines 422, 564) and `//nolint:unparam` (line 1065) on its sibling reconcile functions with rationale comments. Mirror that exactly — do NOT refactor the gate/halt/budget reconcile state machines (just shipped with fresh regression coverage; splitting obscures the contract).

## Task 1 — Production offenses

**files:** internal/controller/{milestone,phase,plan,task}_controller.go, cmd/dashboard/api/prometheus.go, internal/credproxy/server.go, cmd/tide/{approve,resume,artifact_get_run}.go, cmd/claude-subagent/main.go, cmd/manager/main.go

- **gocyclo (6)** — add `//nolint:gocyclo // <flat state-machine rationale>` directly above each func, mirroring project_controller.go:
  - milestone_controller.go `reconcilePlannerDispatch` (~237), `handleJobCompletion` (~495)
  - phase_controller.go `reconcilePlannerDispatch` (~232), `handleJobCompletion` (~446)
  - plan_controller.go `reconcilePlannerDispatch` (~240), `handlePlannerJobCompletion` (~478)
- **unparam (2)** — plan_controller.go `patchPlanFailed` (~810, result 0 always nil), task_controller.go `emitTaskMetrics` (~1057, error always nil): grep callers first. If a caller uses the return in a `return r.foo(...)` reconcile chain → `//nolint:unparam` with the project_controller.go:1065 rationale. If NO caller relies on it → drop the unused return value.
- **unused dead code (4)** — grep-verify zero callers, then REMOVE (park-not-fail migration leftovers): milestone_controller.go `patchMilestoneFailed` (~806), phase_controller.go `patchPhaseFailed` (~720), task_controller.go `patchTaskFailed` (~757), cmd/tide/artifact_get_run.go type `inspectorPodRunnerFunc` (~59).
- **errcheck (2)** — `_ = resp.Body.Close()` at cmd/dashboard/api/prometheus.go:119 and internal/credproxy/server.go:176.
- **goconst (2)** — task_controller.go: extract a package const for "unknown" (6×, ~1012) and "output-paths-violation" (3×, ~842). Name them sensibly (e.g. `unknownModel`/`reasonUnknown`, `outputPathsViolation`). Note: "unknown" is NOT in the .golangci goconst ignore list, so it must be extracted.
- **lll (5)** — wrap to ≤120 cols: cmd/claude-subagent/main.go:61, cmd/manager/main.go:378, cmd/tide/approve.go:204 & 238, cmd/tide/resume.go:121.
- **modernize (1)** — cmd/tide/artifact_get_run.go:105: replace the loop with `slices.Contains`.

**verify:** `./bin/golangci-lint run ./...` reports zero offenses in production packages.
**done:** all production-code offenses cleared, no behavior change.

## Task 2 — Test-file offenses

`_test.go` is excluded from lll/unparam/goconst/dupl/errcheck/gocyclo, but copyloopvar/ginkgolinter/modernize/gofmt/staticcheck/ineffassign still fire there.

- **copyloopvar (2)** — delete redundant `tc := tc`: internal/owner/label_test.go:56, internal/reporter/materialize_test.go:358.
- **ginkgolinter (4)** — replace `Expect(len(x.Items)).To(Equal(n), msg)` with `Expect(x.Items).To(HaveLen(n), msg)`: internal/controller/milestone_gates_test.go:517, phase_gates_test.go:283/429/547, test/integration/envtest/gates_test.go:426.
- **modernize/slicescontains (1)** — internal/controller/file_touch_gate_test.go:304: use `slices.Contains`.
- **gofmt (1)** — internal/config/config_test.go: run gofmt (folds in the pre-existing trailing-newline working-tree change).
- **staticcheck S1039 (1)** — test/integration/kind/push_lease_test.go:274: drop the unnecessary `fmt.Sprintf`.
- **ineffassign (1)** — test/integration/kind/push_lease_test.go:325: remove/fix the ineffectual assignment to `podOut`.

**verify:** `make lint` exits 0 (full run: golangci-lint + import-firewall gates).
**done:** zero offenses; lint green.

## Out of scope / guardrails

- Do NOT touch `charts/` (separate committed fix).
- Do NOT modify `.golangci.yml` (the config is correct; the code is what drifted).
- Leave `.planning/config.json` (`_auto_chain_active` flag) alone.
- Do NOT refactor the gocyclo reconcilers — nolint only.
