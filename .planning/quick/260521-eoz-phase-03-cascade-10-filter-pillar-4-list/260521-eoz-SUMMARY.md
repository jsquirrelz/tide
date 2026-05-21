---
id: 260521-eoz-phase-03-cascade-10-filter-pillar-4-list
title: "Phase 03 chaos_resume Cascade 10: filter Pillar 4 List to executor-role Jobs"
phase: quick
plan: "260521-eoz"
type: quick
status: complete
created: 2026-05-21
completed: 2026-05-21
wave: 1
duration_minutes: 6
requirements: [04.1-12-FOLLOWUP-3]
tags: [phase-03, chaos-resume, test-assertion-bug, cascade-10, label-filter]
dependency-graph:
  requires: []
  provides:
    - "Pillar 4 deterministically counts executor-role Jobs only (α, β, γ) — independent of init/planner/writer Job count in chaos-resume-test namespace"
  affects:
    - "test/integration/kind/chaos_resume_test.go (Pillar 4 List call + clarifying comment)"
tech-stack:
  added: []
  patterns:
    - "client.MatchingLabels for namespaced Job count assertions (paired with the role label stamped by internal/dispatch/podjob/jobspec.go:190)"
key-files:
  created: []
  modified:
    - "test/integration/kind/chaos_resume_test.go"
decisions:
  - "Apply the locked one-line fix from .planning/debug/chaos-resume-cascade-10.md Resolution (lines 282–294) without modification"
  - "Update the line-219 comment from 'Exactly 3 Jobs in the namespace, all succeeded' to a 3-line block that names the role-filter exclusion explicitly and cites the debug session (executor's discretion per plan Task 1 point 4)"
  - "OMIT defensive snapshotChaosTasks filter at line 274 — per debug session line 296 the snapshot's inner loop already matches by tideproject.k8s/task-uid, so non-task Jobs contribute nothing"
metrics:
  commits: 1
  files_modified: 1
  lines_added: 5
  lines_removed: 2
  net_lines: 3
---

# Phase 03 Cascade 10: filter Pillar 4 List to executor-role Jobs — Summary

**One-liner:** Closed Phase 04.1 Plan 12 Outstanding Follow-up #3 by filtering `chaos_resume_test.go` Pillar 4's `k8sClient.List` call to `tideproject.k8s/role=executor` so the `Should(Equal(3))` assertion counts only Task executor Jobs (α, β, γ), excluding the legitimate init / planner / release-writer Jobs that share the `chaos-resume-test` namespace.

## Commit

| Field   | Value                                                                                       |
| ------- | ------------------------------------------------------------------------------------------- |
| SHA     | `aa65c8e` (full: `aa65c8eb1f606357662d55ad25e75f3e8e94fd78`)                                |
| Message | `fix(test): Pillar 4 List filter to executor-role Jobs (cascade-10 — refutes duplicate-dispatch framing)` |
| Files   | `test/integration/kind/chaos_resume_test.go` (1 file)                                       |
| Diff    | `+5 / -2` (net `+3` lines — 2 of which are clarifying comment, 1 is the load-bearing label-filter argument)         |

## Before / after diff (Pillar 4 block, `chaos_resume_test.go`)

**Before** (single-line List, original comment, lines 219–231):
```go
// Exactly 3 Jobs in the namespace, all succeeded.
Eventually(func() int {
    jobs := &batchv1.JobList{}
    _ = k8sClient.List(ctx, jobs, client.InNamespace(chaosResumeNS))
    succeeded := 0
    for _, j := range jobs.Items {
        if isJobSucceededShort(&j) {
            succeeded++
        }
    }
    return succeeded
}, 2*time.Minute, 3*time.Second).Should(Equal(3),
    "Pillar 4: exactly 3 Jobs must reach status.succeeded=1 post-release")
```

**After** (two-line List with role filter, 3-line clarifying comment, lines 219–234):
```go
// Exactly 3 executor Jobs (α, β, γ) must succeed; non-task Jobs (init,
// planners, release-writer) are excluded by the role=executor label filter
// per .planning/debug/chaos-resume-cascade-10.md (Resolution, lines 282–294).
Eventually(func() int {
    jobs := &batchv1.JobList{}
    _ = k8sClient.List(ctx, jobs, client.InNamespace(chaosResumeNS),
        client.MatchingLabels{"tideproject.k8s/role": "executor"})
    succeeded := 0
    for _, j := range jobs.Items {
        if isJobSucceededShort(&j) {
            succeeded++
        }
    }
    return succeeded
}, 2*time.Minute, 3*time.Second).Should(Equal(3),
    "Pillar 4: exactly 3 Jobs must reach status.succeeded=1 post-release")
```

The `Eventually` block, `succeeded` counter loop, `Should(Equal(3),` assertion, and failure message string are byte-identical pre/post-edit. The only behavioral change is the additional `client.MatchingLabels{"tideproject.k8s/role": "executor"}` argument to `k8sClient.List`. No new imports — `client.MatchingLabels` flows through the existing `sigs.k8s.io/controller-runtime/pkg/client` import (line 64 of the file).

## Framing correction (cascade-map auditability)

The cascade-10 entry in the Phase 04.1 cascade map was originally framed as **"chaos_resume duplicate Job dispatch post-restart"** — a production-side bug hypothesis. The debug session at `.planning/debug/chaos-resume-cascade-10.md` (lines 14, 24, 280, and the entire Resolution section starting line 278) **REFUTED** that framing with two pieces of evidence:

1. **Post-kill manager log analysis** (debug session lines 204–219): the controller-manager pod `tjr9k`'s log shows **2m 11s of total silence** with ZERO reconcile events for chaos-resume Tasks between leader-acquisition at `04:15:45.271Z` and cleanup at `04:17:56Z`. No `"creating job"` lines, no Task dispatch events. The post-kill controller dispatched nothing in the Pillar 4 wait window — the duplicate-dispatch hypothesis is incompatible with the log evidence.

2. **Job inventory by source** (debug session lines 80–98, 186–202): the chaos-resume-test namespace legitimately contains 7–8 Jobs from 5 distinct sources — `tide-init-<project-uid>` (1) + Milestone/Phase/Plan planner Jobs (3 created, 2 succeed because Plan planner fails per Cascade-7) + 3× `tide-task-<task-uid>-1` (α, β, γ) + `chaos-resume-release-writer` (1, test-created). 6–7 succeed depending on Cascade-7 status. The test author conflated "3 Task Jobs" with "3 Jobs total in namespace".

**Classification (locked):** `test-assertion-bug` — NOT a production dispatch defect. The fix corrects the assertion's intent (filter to Task executor Jobs only via the role label that production already stamps at `internal/dispatch/podjob/jobspec.go:190`), not a controller-side bug. The commit message preserves the framing correction in its parenthetical: `(cascade-10 — refutes duplicate-dispatch framing)` so future cascade-map readers see the correction without re-reading the full debug session.

## Verification (executed)

Plan's automated verify block, run verbatim post-commit:
```bash
go vet ./test/integration/kind/... \
  && go build ./test/integration/kind/... \
  && grep -c 'MatchingLabels{"tideproject.k8s/role": "executor"}' test/integration/kind/chaos_resume_test.go | grep -q '^1$'
# exit 0
```

Plan's Verification-section parity checks, run post-commit:

| Check                                                                                   | Expected | Observed |
| --------------------------------------------------------------------------------------- | -------- | -------- |
| `grep -c 'MatchingLabels{"tideproject.k8s/role": "executor"}' chaos_resume_test.go`     | 1        | 1        |
| `grep -c 'Should(Equal(3),' chaos_resume_test.go` (assertion preserved verbatim)        | 1        | 1        |
| `grep -c 'labels\["tideproject.k8s/role"\] = "executor"' internal/dispatch/podjob/jobspec.go` | 1        | 1        |
| `gofmt -l test/integration/kind/chaos_resume_test.go`                                   | (empty)  | (empty)  |
| `git diff --name-only` (modified file scope)                                            | 1 file   | 1 file   |
| `git diff --diff-filter=D --name-only HEAD~1 HEAD` (no accidental deletions)            | (empty)  | (empty)  |

All parity checks PASS. Code-shape correctness is verified.

> Note on `Should(Equal(3))` grep: the plan's verification section lists `grep -c 'Should(Equal(3))'` (with closing `))`). The actual source uses `Should(Equal(3),` (open-paren-comma form, comma followed by a newline + message string). Both forms describe the same Gomega assertion; the `Should(Equal(3),` grep returns 1 as expected. The assertion expression `Equal(3)` and the failure-message string are byte-identical pre/post-edit.

## Cascade-10 status

| Field                       | Value                                                                                                                                  |
| --------------------------- | -------------------------------------------------------------------------------------------------------------------------------------- |
| Source-level fix            | **CLOSED** at `aa65c8e`                                                                                                                |
| Runtime gate                | **DEFERRED to user** per plan constraint — `make test-int` was NOT run in this quick task                                              |
| Suggested runtime gate      | `make test-int GINKGO_LABEL_FILTER='kind && D-D4'` (chaos_resume isolation, ~3–5 min wall) — runs only the affected spec               |
| Full-suite runtime gate     | `make test-int` (~18 min wall) — confirms no regression in the other 12 specs                                                          |
| Cascade-map column position | **LAST item** in the Phase 04.1 chaos_resume column per plan objective                                                                 |

## Out of scope (cascade-map auditability)

These items are **NOT** addressed by this quick task and remain as separate follow-ups. They are listed here so the cascade map stays auditable:

1. **Cascade 7 — Plan planner credproxy `ANTHROPIC_API_KEY` missing when `opts.Project=nil`.** Documented at debug session lines 222–240 + Phase 04.1-12 SUMMARY line 152 (Outstanding Follow-up #1). The chaos_resume test does NOT depend on Cascade 7 being fixed — the role filter cleanly excludes planner Jobs from the count regardless of their succeed/fail state. Cascade 7 needs a separate Phase 03 follow-up that resolves the Project before the Plan planner Job is built (e.g., requeue plan_controller until `resolveProjectForPlan` returns non-nil).

2. **Item 2 Layer B 429 storm spec authoring.** Separate Phase 04.1 follow-up; orthogonal to chaos_resume.

3. **Defensive `snapshotChaosTasks` filter at line 274.** Per debug session line 296, the snapshot's inner loop at lines 293–298 already matches by `tideproject.k8s/task-uid` label, so non-task Jobs (which have no task-uid label) silently fail the match and contribute nothing. Adding a role filter to the snapshot's List call would be defensive but not load-bearing. Documented here as a non-issue — if a future iteration shows snapshot leakage of non-task Jobs (no current evidence), a separate follow-up can add the defensive filter then.

4. **`make test-int` runtime gate.** Per plan constraint and user directive, runtime gating is OUT OF SCOPE for this quick task. The bar was code-shape correctness via `go vet` + `go build` + grep parity — all PASS. The user retains the runtime gate decision.

## Deviations from Plan

**None of substance.** The fix was applied exactly as specified in plan Task 1 (lines 130–158). The optional comment update (Task 1 point 4) was applied at executor's discretion — the original one-line comment `// Exactly 3 Jobs in the namespace, all succeeded.` was replaced with a 3-line block that names the role-filter exclusion explicitly and cites the debug-session line range, making the assertion's intent self-documenting at the call site. The assertion expression and failure-message string are byte-identical to the pre-edit form.

## Self-Check

- [x] `test/integration/kind/chaos_resume_test.go` modified at `aa65c8e`: `git log --oneline -1 -- test/integration/kind/chaos_resume_test.go` returns `aa65c8e fix(test): Pillar 4 List filter to executor-role Jobs (cascade-10 — refutes duplicate-dispatch framing)`
- [x] Commit `aa65c8e` exists on `worktree-agent-a4a8e02489d4b7cb3`: `git rev-parse aa65c8e` returns full hash
- [x] Filter present exactly once: `grep -c 'MatchingLabels{"tideproject.k8s/role": "executor"}' test/integration/kind/chaos_resume_test.go` returns `1`
- [x] Assertion preserved: `grep -c 'Should(Equal(3),' test/integration/kind/chaos_resume_test.go` returns `1` with original failure message
- [x] Production label-stamping site exists: `grep -c 'labels\["tideproject.k8s/role"\] = "executor"' internal/dispatch/podjob/jobspec.go` returns `1`
- [x] No new imports added (file's import block byte-identical at lines 46–68)
- [x] `go vet ./test/integration/kind/...` exit 0
- [x] `go build ./test/integration/kind/...` exit 0
- [x] `gofmt -l test/integration/kind/chaos_resume_test.go` empty
- [x] Diff scope: exactly 1 file modified (`test/integration/kind/chaos_resume_test.go`); no accidental deletions; no untracked artifacts

## Self-Check: PASSED
