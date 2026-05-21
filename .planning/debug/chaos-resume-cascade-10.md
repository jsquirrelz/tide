---
slug: chaos-resume-cascade-10
status: root-cause-locked
trigger: |
  User-supplied: "Phase 03 cascade 10: chaos_resume duplicate Job dispatch post-restart (chaos_resume_test.go:230)"
  Observed: Pillar 4 assertion fails in iter-5 — `len(jobs.Items where succeeded=1)` returned 6, expected 3.
  Failure is the deferred follow-up #3 from .planning/phases/04.1-pre-v1-audit-fixes-cross-phase-uat-closeout/04.1-12-SUMMARY.md.
  Cascade 6 (createNamespace) closed the SA/Secret namespace-local gap that previously masked this — iter-5 was the first iteration where the spec advanced past Pillar 1–3 + Pillar 5 (all PASS) and reached Pillar 4's job-count assertion.
created: 2026-05-21
updated: 2026-05-21
phase_context: 04.1 / Phase 03 follow-up
related_artifact: .planning/phases/04.1-pre-v1-audit-fixes-cross-phase-uat-closeout/04.1-12-SUMMARY.md
prior_debug_session: .planning/debug/credproxy-backoff-suppression.md  # different test, but same cascade-iteration pattern (Phase 02.2)
cascade_classification: test-assertion-bug  # NOT a production-side bug — the test assertion at line 230 counts all Jobs in namespace, but the namespace legitimately contains init+planner+writer+task Jobs (6-8 total), not 3
goal: find_and_fix
---

# Debug: chaos_resume Pillar 4 duplicate-Job count (cascade 10)

## Symptoms

**Expected behavior:** `chaos_resume_test.go:127` (D-D4 four pillars + algorithmic invariant hold across controller kill) should PASS in ≤ 6 minutes. Pillar 4 expects exactly **3 Jobs** in the `chaos-resume-test` namespace to reach `status.succeeded=1` post-release-signal — one per Task (α, β, γ). All three Tasks are in the 3-task chaos-resume fixture: α=testMode-success (auto-completes), β/γ=wait-for-signal (block until a release signal is written; pre-kill snapshot has both Running).

**Actual behavior:** Pillar 4's `Eventually(func() int { return count(jobs where succeeded=1) }, 2*time.Minute, 3*time.Second).Should(Equal(3))` times out at 120s with the count stabilizing at **6** (not 3). The failure shape is exact: 2× the expected job count, **but this is NOT a duplicate-dispatch shape** — see Evidence below for refutation.

**Error messages (verbatim from /tmp/04.1-12-iter5-clean-run.log lines 1849–1869):**
```
[FAILED] in [It] - chaos_resume_test.go:230 @ 05/21/26 00:17:51.468
• [FAILED] [172.413 seconds]
Chaos-resume: kill controller pod mid-wave (PERSIST-04 / TEST-04 / D-D4) [It] D-D4 four pillars + algorithmic invariant hold across controller kill [kind]

[FAILED] Timed out after 120.001s.
Pillar 4: exactly 3 Jobs must reach status.succeeded=1 post-release
Expected
    <int>: 6
to equal
    <int>: 3
```

**Pillar sequence pre-failure (verbatim from log lines 1830–1849, all PASS before Pillar 4):**
- Pillar 1 PASS: Job UID continuity for in-flight Tasks (β, γ unchanged across kill) @ 00:15:45.428
- Pillar 2 PASS: Task.Status.Attempt unchanged across kill (no spurious retry) @ 00:15:45.428
- Pillar 3 PASS: Completed-set preserved (α stays Succeeded with same CompletedAt) @ 00:15:45.428
- Pillar 5 PASS: Algorithmic invariant — pkg/dag.ComputeWaves post-restart matches golden @ 00:15:45.428
- Pillar 4 STARTED @ 00:15:45.432, FAILED @ 00:17:51.468 (release-signal-write to Eventually-timeout window: ~126s)

**Key implication of Pillar 1 passing:** Job UIDs for β and γ were observed unchanged across kill → restart at the snapshot timestamp 00:15:45.428. **NOTE:** earlier framing said "whatever produced the extra 3 Jobs happened AFTER that snapshot" — investigation refutes this. The extras are not duplicates of α/β/γ; they are legitimate other-Job-type (init + planner + writer) Jobs that the assertion fails to exclude.

**Timeline:**
- 04.1-12 iter-4 (closed Cascade 6): chaos_resume_test PASS — but `applyController` then masked Pillar 4 from running because the harness was failing earlier on the SA/Secret gap. Cascade 6's createNamespace fix unblocked the test to reach Pillar 4 for the first time on iter-4.
- 04.1-12 iter-4 (also): "chaos_resume passes in isolation" per 04.1-12-SUMMARY.md cascade map (line 79) — **investigation contradicts:** iter-4 isolation actually FAILED at line 252 (alpha-chaos→Failed); Pillar 4 was never observed PASSING in any iteration.
- 04.1-12 iter-5: Pillar 4 fails for the first time WITH the cascade-9 push_lease defer in place (suite ran 9 of 13 specs in 754s). Failure pattern is reproducible at least once.
- 2026-05-21 (just now, quick task 260521-ccz): cascade-9 SKIP gate removed + createNamespace recipe applied to push_lease. Runtime gate not re-run; cascade-10 is the next ladder rung.

**Reproduction:**
```bash
cd /Users/justinsearles/Projects/tide
# Recommend running chaos_resume in isolation first (matches iter-4 isolation-only-pass shape):
make test-int GINKGO_LABEL_FILTER='kind && D-D4' 2>&1 | tee /tmp/cascade-10-isolation.log
# OR full suite to match iter-5 conditions:
make test-int 2>&1 | tee /tmp/cascade-10-suite.log
```

## Available Evidence

| Artifact | Path | Use |
|----------|------|-----|
| iter-5 full test log | `/tmp/04.1-12-iter5-clean-run.log` | Failure context; lines 1830–1870 captured above |
| iter-4 chaos-only log | `/tmp/04.1-12-iter4-chaos-only.log` | Isolation-only test result — FAILED at line 252, not Pillar 4 |
| iter-4 full suite log | `/tmp/04.1-12-iter4-clean-run.log` | chaos_resume SKIPPED here ("CRDs not installed or controller not ready") |
| Kind logs export (iter-5) | `/var/folders/51/h7gq6p5x3592gvrbhrd985q80000gn/T/kind-logs-tide-test/tide-test-control-plane/pods/tide-system_tide-controller-manager-75647fb89d-tjr9k_2490d2bf-9203-4625-9d5b-00c21b4defba/manager/0.log` | Post-kill controller manager log (67 lines, only init + cleanup events) |
| kind cluster | `kubectl --context kind-tide-test` (cluster alive but `chaos-resume-test` ns deleted by AfterEach) | No live Tasks/Jobs to inspect; suitable for replay-only debugging |
| Phase 04.1-12 SUMMARY | `.planning/phases/04.1-pre-v1-audit-fixes-cross-phase-uat-closeout/04.1-12-SUMMARY.md` | Cascade map (lines 96–100), Outstanding Follow-up #3 (line 154), iter-by-iter wall-time trend |
| chaos_resume_test source | `test/integration/kind/chaos_resume_test.go` | Pillar 4 assertion at line 230; full spec at line 127 |
| Production reconcilers | `internal/controller/{plan,task,project,wave}_reconciler.go` | Suspect surface for duplicate-dispatch root cause — INVESTIGATED, NOT THE CAUSE |
| pkg/dag | `pkg/dag/*.go` | Pillar 5 PASS confirms wave-derivation is correct; dag is NOT a suspect |

## Current Focus

```yaml
hypothesis: |
  ROOT CAUSE LOCKED: The "duplicate Job dispatch post-restart" framing was incorrect. The test
  assertion at chaos_resume_test.go:230 is buggy — `Should(Equal(3))` counts ALL Jobs in the
  `chaos-resume-test` namespace, but the namespace legitimately contains 6 succeeded Jobs (not 3):
    1× tide-init-{project-uid}  — Project init Job (busybox mkdir, no credproxy)
    1× tide-milestone-{ms-uid}-1 — Milestone planner Job (stub-subagent w/ empty TestMode → success)
    1× tide-phase-{phase-uid}-1  — Phase planner Job (same as above)
    0× tide-plan-{plan-uid}-1    — Plan planner Job FAILS due to Cascade-7 (Plan-pod credproxy crash
                                    when opts.Project=nil → ANTHROPIC_API_KEY unset → CrashLoopBackOff →
                                    Job.Status.Failed=1, NOT counted)
    3× tide-task-{task-uid}-1    — α, β, γ Task Jobs (all succeed via stub-subagent)
    1× chaos-resume-release-writer — test-created Job that writes /workspace/envelopes/{β,γ}/release
                                    via busybox; succeeds via BackoffLimit=2.
    TOTAL: 7 created, 6 succeeded (Plan planner fails per Cascade-7).
    
  Test author conflated "3 Task Jobs" with "3 Jobs total in namespace". The assertion needs to
  filter by Task Job label (`tideproject.k8s/role=executor` per internal/dispatch/podjob/jobspec.go:190
  + `tideproject.k8s/task-uid` IsSet) to count only Task Jobs.

test: |
  Two-part verification:
  
  (a) Grep all Job-creating sites in internal/controller/ and confirm which would fire for the
      chaos-resume fixture (no git block, full Project→Milestone→Phase→Plan→Task hierarchy).
  
  (b) Check post-kill manager log to confirm whether the controller dispatched DUPLICATES (the
      user's framing) OR whether the count is explained by legitimate non-task Jobs.

expecting: |
  (a) Job sources expected to fire in chaos-resume-test: tide-init, Milestone planner, Phase
      planner, Plan planner, 3× tide-task, 1× release-writer = 7 Jobs. NO clone/push (fixture
      has no git block). Plan planner likely FAILS (Cascade-7).
  
  (b) Post-kill manager log shows ZERO Task dispatch events between leader-acquisition and
      cleanup. Confirms "no duplicate dispatch post-restart" — refutes user's framing.

observed: |
  (a) CONFIRMED by grep of internal/controller/*.go:
      - project_controller.go:286 (init Job, no credproxy)
      - project_controller.go:423 (clone Job — gated on Spec.Git != nil; chaos-resume has no git → NOT fired)
      - project_controller.go:460 (push Job — same gating → NOT fired)
      - milestone_controller.go:315 (Milestone planner Job — fires unconditionally if Dispatcher wired)
      - phase_controller.go:259 (Phase planner Job — fires unconditionally)
      - plan_controller.go:305 (Plan planner Job — fires unconditionally)
      - task_controller.go:616 (Task executor Job — 3 instances)
      - chaos_resume_test.go:442 (writer Job — created by the test itself)
  
  (b) CONFIRMED by /var/folders/.../tjr9k/manager/0.log AND live kubectl logs:
      67 total log lines. Between leader-acquisition at 04:15:45.271Z and cleanup at 04:17:56Z
      (2m 11s window), ZERO reconcile events for chaos-resume Tasks. ZERO "creating job" log
      lines. The post-kill controller dispatched NOTHING in this window. The duplicate-dispatch
      hypothesis is REFUTED.
      
      Note: β/γ reached Status.Phase=Succeeded (per the waitForChaosTaskPhase calls at lines 216-217
      passing within the test's window) — this is consistent with the task_controller.go happy-path
      having NO info-level log line (only V(1) and Error). The dispatch-success log line at line 621
      ("job already exists") only fires on AlreadyExists race, not the steady-state Create. So the
      controller IS reconciling silently — it's just that the dispatch happy-path is silent in
      INFO-level logs. The same pattern holds in the working-controller container log at
      tide-controller-manager-5f978766bb-t6q7p (97 lines for ~8s of activity, all errors/info-on-
      requeue, no "creating job" entries).

next_action: |
  Apply the fix to chaos_resume_test.go line 230 — change the assertion from
  `client.InNamespace(chaosResumeNS)` (lists ALL Jobs) to
  `client.InNamespace(chaosResumeNS), client.MatchingLabels{"tideproject.k8s/role": "executor"}`
  (lists only Task Jobs). The assertion stays `Should(Equal(3))` which now correctly checks
  exactly 3 executor Jobs (α, β, γ) succeed.
  
  Note: The debug file's "Out of Scope" entry (line 122) said "Modifying chaos_resume_test.go
  assertions to accept count=6 — that's accepting the bug, not fixing it." This was based on
  the (incorrect) framing that there was a duplicate-dispatch bug. The investigation refuted
  that framing; the assertion itself is the bug. Modifying the assertion to filter by role
  label is NOT "accepting the bug" — it's correcting a test-author error that conflated
  "3 Task Jobs" with "3 Jobs total".

reasoning_checkpoint: "Surface to user before applying the fix — the framing change (production bug → test assertion bug) is load-bearing and the Out-of-Scope entry needs revision."
tdd_checkpoint: ""
specialist_hint: "general"
```

## Evidence

### Grep of all Job-creating sites in production controllers

Run:
```bash
grep -nE 'r\.Create\(|c\.Create\(' internal/controller/*.go | grep -v '_test.go' | grep -E 'job|Job'
```

Result (`internal/controller/`):
```
boundary_push.go:120:  c.Create(ctx, pushJob)
phase_controller.go:259: r.Create(ctx, job)        # Phase planner Job (always)
milestone_controller.go:315: r.Create(ctx, job)    # Milestone planner Job (always)
project_controller.go:286: r.Create(ctx, job)      # init Job (always)
project_controller.go:423: r.Create(ctx, cloneJob) # clone Job (gated on Spec.Git)
project_controller.go:460: r.Create(ctx, pushJob)  # push Job (gated on Spec.Git)
task_controller.go:616: r.Create(ctx, job)         # Task executor Job (3 per fixture)
task_controller.go:1000: r.Create(ctx, job)        # alternate Task dispatcher (ensureJob — not on critical path)
plan_controller.go:305: r.Create(ctx, job)         # Plan planner Job (always)
```

### Job-creator gating in the chaos-resume fixture

Fixture (`test/integration/kind/testdata/chaos-resume-three-task.yaml`) has:
- Project with NO `git:` block → clone/push Jobs do NOT fire (production code lines 421 and 440 require `project.Spec.Git != nil && project.Spec.Git.RepoURL != ""`).
- Full Milestone/Phase/Plan/Task hierarchy → all 3 planner Jobs fire (the reconcilers dispatch on Status.Phase, not on child presence).
- 3 Tasks → 3 Task Jobs fire.
- Test itself creates `chaos-resume-release-writer` Job in `writeChaosReleaseSignals` (chaos_resume_test.go:442).

Steady-state Jobs in `chaos-resume-test` namespace:
1. `tide-init-<project-uid>` — init Job, no credproxy
2. `tide-milestone-<ms-uid>-1` — Milestone planner Job
3. `tide-phase-<phase-uid>-1` — Phase planner Job
4. `tide-plan-<plan-uid>-1` — Plan planner Job (likely FAILED per Cascade-7)
5. `tide-task-<alpha-uid>-1` — α Task Job
6. `tide-task-<beta-uid>-1` — β Task Job
7. `tide-task-<gamma-uid>-1` — γ Task Job
8. `chaos-resume-release-writer` — test-created writer Job

**8 created, 7 succeeded.** Cascade-7 plausibly drops 1 to give the observed 6.

### Post-kill manager log analysis

Path: `/var/folders/51/h7gq6p5x3592gvrbhrd985q80000gn/T/kind-logs-tide-test/tide-test-control-plane/pods/tide-system_tide-controller-manager-75647fb89d-tjr9k_2490d2bf-9203-4625-9d5b-00c21b4defba/manager/0.log`

Total log lines: 67. Live `kubectl logs` confirms same 67 lines (file is not truncated).

Activity timeline:
- 04:15:28Z: container start, controller-runtime sources initialize
- 04:15:45.271Z: `Successfully acquired lease`
- 04:15:45.272-477Z: controller workers start (task w/16, project w/1, wave w/8, milestone w/1, phase w/2, plan w/4)
- 04:15:45.477Z → 04:17:56.900Z: **2m 11s of TOTAL silence — zero reconcile events**
- 04:17:56.900Z+: cleanup events (Plan, then Tasks, then Phase, then Project, then Milestone) as namespace tears down

The 2m 11s silent window encompasses the entire Pillar 4 wait. **NO duplicate-dispatch evidence.**

Cross-reference: the post-kill controller pod `tjr9k` is STILL ALIVE at the time of investigation (9h post-failure per `kubectl get pods -n tide-system`). Its log has not advanced past the cleanup events.

### Cascade-7 confirmation in code (Plan planner credproxy crash)

`internal/dispatch/podjob/jobspec.go:259-273` (BuildJobSpec credproxy EnvFrom):
```go
EnvFrom: func() []corev1.EnvFromSource {
    srcs := []corev1.EnvFromSource{
        {SecretRef: &corev1.SecretEnvSource{LocalObjectReference: corev1.LocalObjectReference{Name: "tide-signing-key"}}},
    }
    // Only add the provider secret when a name is set — K8s rejects empty SecretRef.Name.
    // Guard against nil Project (e.g. planner Jobs created before Project resolves).
    if opts.Project != nil && opts.Project.Spec.ProviderSecretRef != "" {
        srcs = append(srcs, ...)
    }
    return srcs
}(),
```

`internal/controller/plan_controller.go:244` calls `resolveProjectForPlan` which can return nil. When nil is passed to `BuildJobSpec`, the credproxy container's EnvFrom only includes `tide-signing-key`, not the provider secret. Credproxy initializes without ANTHROPIC_API_KEY → fails to start → Job fails.

Phase 04.1-12 SUMMARY line 152 documents this exact issue (Outstanding Follow-up #1, also tagged as "Cascade 7" in the cascade map at line 97).

## Eliminated Hypotheses

### H1 (REJECTED): Release-signal-write side effect causing post-restart re-dispatch

**Hypothesis was:** When β/γ's release files are written at 00:15:45.432, the post-restart controller observes a state-change event on the Task and dispatches a NEW Job despite Status.JobUID being set.

**Evidence against:**
- Post-kill manager log shows ZERO reconcile events for chaos-resume Tasks between 04:15:45Z and 04:17:56Z.
- `task_controller.go:273-280` terminal short-circuit (Status.Phase=Succeeded → halt) and Running-branch delegate to `checkRunningState` (which never re-dispatches; it only halts or transitions to terminal via Job-completion watch).
- No path in `task_controller.go` calls Job Create when Status.Phase=Running and JobUID is set.

### H2 (REJECTED): Watch-lag duplicate dispatch (Pitfall P11)

**Hypothesis was:** Post-restart controller's cache lags behind the API server when it begins reconciling β/γ, sees JobUID="" briefly, dispatches a Job, then the cache catches up but the duplicate Job is already created.

**Evidence against:**
- Pillar 1 PASSED (Job UIDs unchanged across kill) — the post-restart cache observed β/γ correctly within 143ms of leader-lease acquisition.
- Post-kill manager log shows ZERO dispatch events.
- `task_controller.go:401-407` `checkRunningState` short-circuits to `shouldHalt=true` when Job is not yet visible (apierrors.IsNotFound) — does NOT dispatch a new Job.

### H3 (REJECTED): testMode=wait-for-signal lifecycle bug

**Hypothesis was:** wait-for-signal Tasks may be treated as "in dispatch loop" by the controller even after their Job exists. A check on `t.Spec.Dev.TestMode` may not short-circuit a re-dispatch path when JobUID is already set.

**Evidence against:**
- `task_controller.go` does NOT branch on `Spec.Dev.TestMode` in the dispatch path. TestMode is only injected into the EnvelopeIn for stub-subagent consumption (line 1039-1041).
- The dispatch path gates on `Status.Phase` (line 273, 278) — TestMode is a Job-side concept, not a controller dispatch-gate.

## Out of Scope (for this session)

- Cascade 7 (plan-pod credproxy ANTHROPIC_API_KEY missing) — **separate follow-up**, but its existence EXPLAINS one of the missing planner-Job successes. The chaos-resume test should not depend on Cascade-7 being fixed; the test assertion should filter to count only Task Jobs.
- Item 2 Layer B 429 storm spec authoring — separate follow-up.
- Layer A envtest TestGateApproveFlow flake — known pre-existing intermittent, not regressed.
- **PREVIOUSLY OUT-OF-SCOPE, NOW CORRECTED:** "Modifying chaos_resume_test.go assertions to accept count=6 — that's accepting the bug, not fixing it." This out-of-scope item was based on an incorrect framing. Investigation refuted the duplicate-dispatch framing; the assertion itself is the bug (it counts all Jobs in namespace instead of filtering to Task Jobs). The fix is a test-side filter change.
- Re-running `make test-int` end-to-end during the find-root-cause phase. Use `make test-int GINKGO_LABEL_FILTER='kind && D-D4'` to verify the fix in isolation.

## Resolution

**Root cause (LOCKED):** The Pillar 4 assertion at `chaos_resume_test.go:230` is buggy. It counts ALL Jobs in the `chaos-resume-test` namespace via `client.List(ctx, jobs, client.InNamespace(chaosResumeNS))`. The namespace legitimately contains 7-8 Jobs (init + 3 planner + 3 task + 1 writer), of which 6-7 succeed depending on Cascade-7 status. The test author conflated "3 Task Jobs" with "3 Jobs total in namespace".

**Fix proposal (single-line test change):**

Replace the Pillar 4 List call at `chaos_resume_test.go:222` from:
```go
_ = k8sClient.List(ctx, jobs, client.InNamespace(chaosResumeNS))
```
to:
```go
_ = k8sClient.List(ctx, jobs, client.InNamespace(chaosResumeNS),
    client.MatchingLabels{"tideproject.k8s/role": "executor"})
```

The `tideproject.k8s/role` label is set on Task Jobs by `internal/dispatch/podjob/jobspec.go:190` (`labels["tideproject.k8s/role"] = "executor"`). Planner Jobs get `=planner` (line 179). Init/clone/push/writer Jobs have no labels. The filter cleanly counts only Task Jobs, and the existing `Should(Equal(3))` assertion stands without modification.

Optional secondary improvement (defensive — also filter the snapshot): apply the same filter at `snapshotChaosTasks` line 274 to avoid future ambiguity, though this isn't load-bearing (the snapshot's iteration already matches by task-uid label).

**Verification plan:**
1. Apply the one-line fix.
2. Run `make test-int GINKGO_LABEL_FILTER='kind && D-D4'` (chaos_resume only, ~3-5 min wall) to verify Pillar 4 passes.
3. If isolation passes, run full `make test-int` (~18 min) to confirm no regression.
4. Commit on `main` with message describing the framing correction.

**Scope:** Quick-task scope. Touches `test/integration/kind/chaos_resume_test.go` only. No production-side change.
