---
id: 260521-eoz-phase-03-cascade-10-filter-pillar-4-list
title: "Phase 03 chaos_resume Cascade 10: filter Pillar 4 List to executor-role Jobs"
type: quick
status: planned
created: 2026-05-21
phase: quick
plan: "260521-eoz"
wave: 1
depends_on: []
files_modified:
  - test/integration/kind/chaos_resume_test.go
autonomous: true
requirements: [04.1-12-FOLLOWUP-3]
must_haves:
  truths:
    - "Pillar 4's k8sClient.List call at chaos_resume_test.go:222 filters by client.MatchingLabels{\"tideproject.k8s/role\": \"executor\"} so it counts ONLY Task executor Jobs (α, β, γ), not Project init / planner / release-writer Jobs"
    - "The Should(Equal(3)) assertion at chaos_resume_test.go:230 stands unchanged — the count now correctly equals 3 because the filter excludes the 4-5 non-executor Jobs that legitimately share the chaos-resume-test namespace"
    - "test/integration/kind/chaos_resume_test.go compiles clean (go vet + go build, exit 0) with no new imports — client.MatchingLabels is already imported via sigs.k8s.io/controller-runtime/pkg/client"
    - "No production-side code is touched — the cascade-10 root cause is locked as a test-assertion-bug per .planning/debug/chaos-resume-cascade-10.md line 14 (cascade_classification) and line 280 (Resolution)"
  artifacts:
    - path: "test/integration/kind/chaos_resume_test.go"
      provides: "Pillar 4 List filtered by tideproject.k8s/role=executor; counts only Task Jobs"
      contains: "MatchingLabels{\"tideproject.k8s/role\": \"executor\"}"
  key_links:
    - from: "test/integration/kind/chaos_resume_test.go:222 List call"
      to: "internal/dispatch/podjob/jobspec.go:190 (labels[\"tideproject.k8s/role\"] = \"executor\" on Task Jobs)"
      via: "client.MatchingLabels list option"
      pattern: "MatchingLabels\\{\"tideproject\\.k8s/role\":\\s*\"executor\"\\}"
---

<objective>
Close Phase 04.1 Plan 12 Outstanding Follow-up #3 (Cascade 10): apply the locked one-line fix from `.planning/debug/chaos-resume-cascade-10.md` (Resolution, lines 282–294) to filter the Pillar 4 List call to executor-role Jobs only. The debug session's root-cause analysis refutes the original "duplicate Job dispatch post-restart" framing and locks the cascade as a test-assertion bug: the namespace legitimately contains 7-8 Jobs (init + 3 planner + 3 task + 1 writer), not 3, and the test author conflated "3 Task Jobs" with "3 Jobs total in namespace".

Purpose: unblock the chaos_resume_test (`kind && D-D4`) so Pillar 4 reports a deterministic count of executor Jobs only. This is the LAST Phase 04.1 cascade-map item in the chaos_resume column.

Output: minimal one-line modification (chaos_resume_test.go:222) adding `client.MatchingLabels{"tideproject.k8s/role": "executor"}` as a second argument to the existing `k8sClient.List` call. Net diff ≈ +1 line (or +2 with line-wrap formatting). No new imports. No production code. Runtime gating via `make test-int` is explicitly OUT of scope — code-shape correctness via `go vet` + `go build` is the bar, per the user's constraint.
</objective>

<execution_context>
@$HOME/.claude/get-shit-done/workflows/execute-plan.md
@$HOME/.claude/get-shit-done/templates/summary.md
</execution_context>

<context>
@CLAUDE.md
@.planning/STATE.md
@.planning/debug/chaos-resume-cascade-10.md
@test/integration/kind/chaos_resume_test.go
@internal/dispatch/podjob/jobspec.go

<interfaces>
<!-- The role label is stamped on Task Jobs by internal/dispatch/podjob/jobspec.go. -->
<!-- The executor uses client.MatchingLabels (already imported in chaos_resume_test.go). -->

From internal/dispatch/podjob/jobspec.go (label-stamping switch):
```go
// Line 174–183 — Planner branch:
case JobKindPlanner:
    // ...
    labels["tideproject.k8s/role"] = "planner"
    // ...

// Line 184–191 — Executor branch (Task Jobs):
default: // JobKindExecutor (and legacy callers with Kind=="")
    if opts.Task != nil {
        parentUID = string(opts.Task.UID)
    }
    jobName = JobName(opts.Task.UID, opts.Attempt)
    labels["tideproject.k8s/task-uid"] = string(opts.Task.UID)
    labels["tideproject.k8s/role"] = "executor"
```

Other Jobs in the chaos-resume-test namespace (per debug session lines 95–98, 200) have NO `tideproject.k8s/role` label:
- `tide-init-<project-uid>` — Project init Job (project_controller.go:286), no role label
- `chaos-resume-release-writer` — test-created writer Job (chaos_resume_test.go:442), no role label

So filtering by `role=executor` deterministically yields the 3 Task Jobs (α, β, γ) and nothing else.

From test/integration/kind/chaos_resume_test.go (current Pillar 4 block — lines 210–231):
```go
// Pillar 4: Observed completion across kill — write release files, then
// wait for β + γ to reach Succeeded and the Wave to follow.
By("Pillar 4: Observed completion across kill — release β + γ, both reach Succeeded")
writeChaosReleaseSignals(preKill["beta-chaos"], preKill["gamma-chaos"])

// Eventually β + γ reach Succeeded post-release.
waitForChaosTaskPhase("beta-chaos", "Succeeded", 4*time.Minute)
waitForChaosTaskPhase("gamma-chaos", "Succeeded", 4*time.Minute)

// Exactly 3 Jobs in the namespace, all succeeded.
Eventually(func() int {
    jobs := &batchv1.JobList{}
    _ = k8sClient.List(ctx, jobs, client.InNamespace(chaosResumeNS))   // <-- LINE 222: add role filter
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

The fix targets line 222 only. The comment on line 219 ("Exactly 3 Jobs in the namespace, all succeeded.") is now slightly inaccurate — it should read "Exactly 3 executor Jobs (α, β, γ) must succeed; other Jobs in the namespace (init, planners, writer) are excluded by role filter." Executor's discretion whether to update the comment; the assertion message string at line 230–231 is the source of truth surfaced to test consumers and can stay as is.
</interfaces>

**Cascade 10 framing correction (per debug session lines 14, 24, 280):**
The original cascade title "chaos_resume duplicate Job dispatch post-restart" was REFUTED by investigation. The post-kill manager log (`/var/folders/.../tjr9k/manager/0.log`) shows 2m 11s of total silence with zero reconcile events between leader-acquisition and cleanup — there is NO duplicate dispatch. The count of 6 (vs expected 3) is explained by legitimate non-task Jobs: 1× init + 2× successful planners (Milestone + Phase) + 3× Task Jobs (α, β, γ) = 6. The Plan planner Job FAILS due to Cascade-7 (credproxy ANTHROPIC_API_KEY missing when opts.Project=nil), which is why we see 6 succeeded and not 7. cascade_classification: `test-assertion-bug`.

**Why the optional secondary filter (snapshotChaosTasks line 274) is OMITTED:**
Per debug session line 296, the snapshot's inner loop at line 293–298 already matches by `tideproject.k8s/task-uid` label, so non-executor Jobs (which have no task-uid label) silently fail the match and contribute nothing. Adding a role filter to the List call would be defensive but not load-bearing. Per the user's constraint to keep cascade-10 a "one-line test fix" and the locked diagnosis explicitly identifying line 222 as the only required change, OMIT the secondary filter. Document the omission in success_criteria so the auditability trail is preserved.

**Out of scope** (do NOT touch in this quick task):
- Cascade 7 (Plan planner credproxy ANTHROPIC_API_KEY missing when opts.Project=nil) — separate Phase 03 follow-up. Confirmed in debug session lines 222–240. The chaos_resume test should NOT depend on Cascade 7 being fixed — the role filter cleanly excludes planner Jobs from the count regardless of their succeed/fail state.
- Running `make test-int` end-to-end — runtime gating is not the bar; code-shape correctness via `go vet` + `go build` is. User retains the runtime gate decision.
- Modifying the `Should(Equal(3))` assertion text or message — it correctly expects 3 executor Jobs.
- Item 2 Layer B 429 storm spec authoring — separate follow-up.
- Defensive snapshotChaosTasks filter at line 274 — per debug session line 296 not load-bearing.
- `chaos_resume_test.go` line 219 comment text update — executor's discretion; the assertion message at line 230–231 is the source of truth.
</context>

<tasks>

<task type="auto">
  <name>Task 1: Filter Pillar 4 List to executor-role Jobs (chaos_resume_test.go:222)</name>
  <files>test/integration/kind/chaos_resume_test.go</files>
  <action>
In `test/integration/kind/chaos_resume_test.go`, modify the Pillar 4 `Eventually` block's `k8sClient.List` call at line 222.

CURRENT (single-line form):
```go
_ = k8sClient.List(ctx, jobs, client.InNamespace(chaosResumeNS))
```

TARGET (two-line form with role label filter):
```go
_ = k8sClient.List(ctx, jobs, client.InNamespace(chaosResumeNS),
    client.MatchingLabels{"tideproject.k8s/role": "executor"})
```

Implementation details:
1. ADD `client.MatchingLabels{"tideproject.k8s/role": "executor"}` as a third argument to the existing `k8sClient.List` call. The label value `"executor"` is stamped on Task Jobs by `internal/dispatch/podjob/jobspec.go:190`; planner Jobs get `"planner"` (line 179); other Jobs (init/clone/push/writer) have no `tideproject.k8s/role` label at all. The filter is exclusive — only executor Jobs match.

2. NO IMPORT CHANGES NEEDED. The file already imports `sigs.k8s.io/controller-runtime/pkg/client` (used by `client.InNamespace`, `client.ObjectKey`, etc. throughout the file). `client.MatchingLabels` is exported from the same package.

3. PRESERVE the surrounding `Eventually` block, the `succeeded` counter loop, the `Should(Equal(3))` assertion (line 230), and the failure message at line 230–231 EXACTLY AS-IS. The fix is one logical edit at line 222 only.

4. OPTIONALLY update the comment on line 219 ("Exactly 3 Jobs in the namespace, all succeeded.") to clarify the filter — e.g., "Exactly 3 executor Jobs (α, β, γ) must succeed; non-task Jobs (init, planners, release-writer) are excluded by role filter." This is executor's discretion. If unsure, leave the comment unchanged — the assertion message at line 230–231 already documents intent ("Pillar 4: exactly 3 Jobs must reach status.succeeded=1 post-release") and the fix is self-documenting via the explicit label predicate.

5. Do NOT modify `snapshotChaosTasks` (line 272 ff.). Its inner loop at lines 293–298 already matches by `tideproject.k8s/task-uid` label, so non-task Jobs contribute nothing. Adding a role filter there would be defensive but not load-bearing per debug session line 296 — omit it to keep the cascade-10 fix scoped to the single Pillar 4 line.

6. Do NOT touch any other file. Atomic single-file commit.

Commit message (executor task protocol):
`fix(test): Pillar 4 List filter to executor-role Jobs (cascade-10 — refutes duplicate-dispatch framing)`
  </action>
  <verify>
    <automated>cd /Users/justinsearles/Projects/tide && go vet ./test/integration/kind/... && go build ./test/integration/kind/... && grep -c 'MatchingLabels{"tideproject.k8s/role": "executor"}' test/integration/kind/chaos_resume_test.go | grep -q '^1$'</automated>
  </verify>
  <done>
- `go vet ./test/integration/kind/...` passes (exit 0).
- `go build ./test/integration/kind/...` passes (exit 0).
- `grep -c 'MatchingLabels{"tideproject.k8s/role": "executor"}' test/integration/kind/chaos_resume_test.go` returns exactly `1`.
- The `Should(Equal(3))` assertion at line 230 (or its current line after the edit shifts) is UNCHANGED in text and value.
- No new imports were added (the file's import block is unchanged).
- Diff scope: ONLY `test/integration/kind/chaos_resume_test.go` modified.
- `git diff --stat test/integration/kind/chaos_resume_test.go` shows roughly net `+1` to `+2` lines (single-line List call expanded to two-line form with the extra argument).
- Commit message matches the stub: `fix(test): Pillar 4 List filter to executor-role Jobs (cascade-10 — refutes duplicate-dispatch framing)`.
  </done>
</task>

</tasks>

<verification>

**Filter-shape parity check** (run after the task lands):

```bash
# Should return exactly 1 — the Pillar 4 List call now filters by role=executor:
grep -c 'MatchingLabels{"tideproject.k8s/role": "executor"}' /Users/justinsearles/Projects/tide/test/integration/kind/chaos_resume_test.go

# Should return exactly 1 — the underlying assertion is unchanged:
grep -c 'Should(Equal(3))' /Users/justinsearles/Projects/tide/test/integration/kind/chaos_resume_test.go

# Should return exactly 1 — the production-side label-stamping site is the wire from filter to evidence:
grep -c 'labels\["tideproject.k8s/role"\] = "executor"' /Users/justinsearles/Projects/tide/internal/dispatch/podjob/jobspec.go

# Should compile cleanly:
cd /Users/justinsearles/Projects/tide && go vet ./test/integration/kind/...
cd /Users/justinsearles/Projects/tide && go build ./test/integration/kind/...
```

**Optional bonus** (not blocking):

```bash
# goimports / gofmt parity (recommended but not gating):
cd /Users/justinsearles/Projects/tide && gofmt -l test/integration/kind/chaos_resume_test.go
# Should output nothing

# Optional isolation runtime gate (NOT required for plan completion — user retains gate decision):
# cd /Users/justinsearles/Projects/tide && make test-int GINKGO_LABEL_FILTER='kind && D-D4' 2>&1 | tee /tmp/cascade-10-isolation.log
```

</verification>

<success_criteria>

1. `test/integration/kind/chaos_resume_test.go`:
   - Line 222 (Pillar 4 `k8sClient.List` call) now includes `client.MatchingLabels{"tideproject.k8s/role": "executor"}` as an additional argument.
   - `Should(Equal(3))` assertion at line 230 is UNCHANGED in text and value.
   - No new imports added (`client.MatchingLabels` flows through the existing controller-runtime `client` import).
   - No other lines modified in the file (modulo optional 1-line comment update at line 219, executor's discretion).

2. `snapshotChaosTasks` defensive filter (debug session line 296): INTENTIONALLY OMITTED from this plan. The snapshot's inner loop at lines 293–298 already matches by `tideproject.k8s/task-uid` label, making the role filter non-load-bearing. The omission is documented here for cascade-map auditability. If a future iteration shows snapshot leakage of non-task Jobs (no current evidence), a separate follow-up can add the defensive filter.

3. Compilation: `go vet ./test/integration/kind/...` and `go build ./test/integration/kind/...` both pass with exit 0.

4. One atomic commit on `main` (or worktree branch, per executor protocol) with the commit message stub: `fix(test): Pillar 4 List filter to executor-role Jobs (cascade-10 — refutes duplicate-dispatch framing)`.

5. `make test-int` was NOT run as part of this quick task (per the user's explicit constraint). The user runs it separately if they want the runtime gate on Phase 03 chaos_resume. The OPTIONAL isolation command `make test-int GINKGO_LABEL_FILTER='kind && D-D4'` is documented in the Verification section as a user-discretion gate.

6. Cascade-10 root cause framing is correctly preserved in the commit message ("refutes duplicate-dispatch framing") so future cascade-map readers see the framing correction without having to re-read the full debug session.

</success_criteria>

<output>
After completion, create `.planning/quick/260521-eoz-phase-03-cascade-10-filter-pillar-4-list/260521-eoz-SUMMARY.md` per the standard quick-task summary template. The summary should record:

- The single commit SHA.
- The before/after diff line-count for chaos_resume_test.go (expect net +1 to +2 lines).
- A note that Cascade 10 from Plan 04.1-12 Outstanding Follow-up #3 is now CLOSED at the source level (the test-assertion bug is corrected; runtime verification deferred to the user's next `make test-int GINKGO_LABEL_FILTER='kind && D-D4'` or full-suite run).
- An explicit "Framing correction" note: the original cascade-10 title "chaos_resume duplicate Job dispatch post-restart" was REFUTED by the debug session (lines 14, 24, 280); the actual root cause is a test-assertion bug where the Pillar 4 List counted all Jobs in the namespace instead of filtering by role label. The fix corrects the assertion's intent, not a production dispatch defect.
- An explicit "Out of scope" footer listing the remaining Phase 03 follow-ups (Cascade 7 plan-pod credproxy `ANTHROPIC_API_KEY` missing when opts.Project=nil, Item 2 Layer B 429 storm spec authoring) so the cascade map stays auditable, plus the omitted defensive snapshotChaosTasks filter as a documented non-issue.
</output>
</content>
</invoke>