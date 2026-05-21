---
slug: push-lease-phase-revert
status: investigating
trigger: |
  User-supplied (post-cascade-12-runtime-gate): "Phase 03 cascade 13: push-lease Tests 3+4 fail with Phase=Initialized expected PushLeaseFailed". Surfaced when cascade-12 closed the patchJobToFailed K8s validation issue.
  
  Observed root cause shape: Project.Status.Phase reverts (or never advances) to "Initialized" between the test's forcePushReady(Phase=Complete) patch and the Eventually wait for Phase=PushLeaseFailed. The push Job IS dispatched (manager log confirms "created push Job") and patchJobToFailed succeeds (cascade-12 closed). Yet Phase=Initialized is observed at the 90s Eventually timeout.
created: 2026-05-21
updated: 2026-05-21
phase_context: Phase 03 follow-up (surfaced post-cascade-12)
related_artifacts:
  - .planning/quick/260521-hk4-phase-03-cascade-12-patchjobtofailed-mus/260521-hk4-SUMMARY.md  # cascade-12 unblocked the patch
  - .planning/debug/push-lease-pvc-pending.md  # cascade-11 sister investigation
goal: find_and_fix
---

# Debug: push-lease Project.Status.Phase reverts to Initialized after push-Job failure (cascade 13)

## Symptoms

**Expected behavior:** After `patchJobToFailed(pushLeaseNS, jobName)` patches the push Job's status to `Failed=True + FailureTarget=True`, the ProjectReconciler should observe the failed Job and transition `Project.Status.Phase` from `Complete` → `PushLeaseFailed` (per `internal/controller/project_controller.go:480-545` Step 5b). The default branch at line 530+ catches the case where the push-result envelope is missing or empty (which is exactly the test's situation — it mocks Job status but doesn't write an envelope to the PVC).

**Actual behavior:** The Eventually at `push_lease_test.go:130-139` (Test 3) and `:156-161` (Test 4) times out at 90s with `Status.Phase == "Initialized"` (not `"PushLeaseFailed"`). Tests 1 and 2 PASS (they don't call `patchJobToFailed`; they only assert on the dispatched Job's args).

**Error messages (verbatim from /tmp/cascade-12-isolation.log):**
```
[FAILED] Timed out after 90.001s.
The function passed to Eventually failed at push_lease_test.go:135 with:
Status.Phase must be PushLeaseFailed after push Job failure
Expected
    <string>: Initialized
to equal
    <string>: PushLeaseFailed
In [It] at: /Users/justinsearles/Projects/tide/test/integration/kind/push_lease_test.go:139
```

**Critical observables from /var/folders/.../kind-logs-tide-test/.../manager/0.log (post-cascade-12 run):**

For each of the 4 push-lease Project lifecycles (Tests 1-4, distinct UIDs each), the manager log shows:
```
"created clone Job" + "created push Job"  — SAME reconcile pass (push dispatch works)
... <90s of test waiting> ...
"Reconciler error: create init/clone job: ... unable to create new content in namespace push-lease-test because it is being terminated"
"project cleanup"
```

The manager log does NOT contain explicit "Phase transitioned to X" log lines — TIDE's controller logs are sparse (only on errors and one-shot events). The Phase=Initialized observation comes from `k8sClient.Get` in the test's Eventually loop, not from the log.

**Comparison: Tests 1+2 PASS, Tests 3+4 FAIL — the difference is `patchJobToFailed`:**

- Tests 1+2 do: applyFile → forcePushReady → waitForPushJob → assertions on Job.Spec.Containers[0].Args. **PASS at 32s and 35s respectively.**
- Tests 3+4 do: applyFile → forcePushReady → waitForPushJob → **patchJobToFailed** → Eventually Phase=PushLeaseFailed. **FAIL at 111s (timeout).**

The patchJobToFailed call is the only structural difference. The Phase transition the test expects (Complete → PushLeaseFailed) is the production-side response to a Failed push Job.

**Timeline (Test 3, from /tmp/cascade-12-isolation.log):**
```
12:53:36  Project created; reconciler creates init Job + clone Job + push Job (all in one pass)
12:54:12  STEP: Wait for the push Job to exist, then patch Job.Status to Failed
12:54:16  STEP: Eventually Project.Status.Phase=PushLeaseFailed + LeaseFailureCount==1
12:55:46  [FAILED] Timed out after 90.001s. <string>: Initialized
12:55:52  Reconciler error: namespace push-lease-test being terminated (AfterEach delete)
```

The 90s window (12:54:16 → 12:55:46) is what we need to instrument. Phase is observed as `Initialized`, NOT `Complete` (which `forcePushReady` patched) and NOT `PushLeaseFailed` (which the controller SHOULD have patched). So Phase reverted Complete → Initialized somewhere.

**Reproduction:**

```bash
cd /Users/justinsearles/Projects/tide

# Cheapest repro (matches /tmp/cascade-12-isolation.log shape):
make test-int 2>&1 | tee /tmp/cascade-13-fullsuite.log
# Note: GINKGO_FOCUS does not actually filter through make test-int —
# the whole suite runs; Tests 3+4 are at end of the run.

# To instrument: add a parallel watch in a second shell during the failing
# window:
kubectl --context kind-tide-test get project -n push-lease-test push-lease -o yaml -w &
# This streams the Project's full state including every Status.Phase change.
# The test waits at 12:54:16 → 12:55:46 — the watch will print Phase
# transitions in that window IF any are happening.
```

## Available Evidence

| Artifact | Path | Use |
|----------|------|-----|
| Cascade-12 isolation log | `/tmp/cascade-12-isolation.log` | Tests 1-4 outcomes; line 1952+ for Test 3, line 1971+ for Test 4 |
| Latest manager log | `/var/folders/51/h7gq6p5x3592gvrbhrd985q80000gn/T/kind-logs-tide-test/tide-test-control-plane/pods/tide-system_tide-controller-manager-7bfb8db9cf-762xz_*/manager/0.log` | 4 push-lease Project lifecycles with "created push Job" lines + "Reconciler error" lines on namespace termination |
| Push-lease test spec | `test/integration/kind/push_lease_test.go` | Test 3 at line 116-145, Test 4 at line 145-185; `patchJobToFailed` at line 245-285 (post-cascade-12); `forcePushReady` at line 199-221 |
| ProjectReconciler entry | `internal/controller/project_controller.go` | Line 273-275 (Phase guard letting through Initialized/Running/PushLeaseFailed/Complete to `reconcilePhase3Lifecycle`); line 300 (handleInitJobCompletion patches Phase=Initialized on init-Job-Succeeded); line 440 (push dispatch gate); line 480-545 (push-Job-failed branch with switch on envelope.Reason; default catches empty reason and patches Phase=PushLeaseFailed) |
| Push-lease fixture | `test/integration/kind/testdata/push-lease-project.yaml` | Project has `git.repoURL`, `git.credsSecretRef`, etc. — push dispatch gates satisfied |

## Current Focus

```yaml
hypothesis: |
  Three live sub-hypotheses, in declining order of likelihood:
  
  H1 (most likely, "init-completion re-patch race"): handleInitJobCompletion at project_controller.go:300 patches Phase=Initialized whenever the init Job is observed Succeeded. If the reconciler re-enters this code path AFTER the test's forcePushReady patch (e.g., on a Job-watch event for the init Job's final status update — controller-runtime emits events on cache updates even for the same final state during informer resync), Phase gets clobbered back to Initialized. The Phase guard at line 273-275 admits Initialized/Running/PushLeaseFailed/Complete to reconcilePhase3Lifecycle, but the path that CALLS handleInitJobCompletion (somewhere earlier in the reconcile body) might not have a similar guard against re-patching when Phase is already past Initialized.
  
  H2 ("clone-job-failed reset"): the fixture's Project has spec.git.repoURL=https://example.invalid/..., which the clone Job tries to clone. Clone fails (invalid URL or no network). If the controller has a code path that resets Phase to Initialized when the clone Job fails (or any other intermediate Job lifecycle event), that would explain the revert. Need to grep for clone-Job-failed Phase transitions.
  
  H3 ("Status patch race"): forcePushReady patches Phase=Complete via kubectl --subresource=status. If the controller has stale cache when its next reconcile fires and uses a get-modify-patch pattern (rather than MergeFrom on the observed state), it could re-write Phase based on its stale view, effectively reverting the test's patch. Less likely because most controller code uses client.MergeFrom on the current observed state, but worth verifying.

test: |
  Stage 1 — instrumented run with Phase-watch:
  Make a small wrapper around `make test-int` that runs `kubectl get project -A -o yaml -w` in a parallel shell, captures all Phase transitions to /tmp/cascade-13-phase-trace.log. After the run, grep for `phase:` lines per push-lease Project UID and trace the Phase transition graph for Test 3 and Test 4 windows.
  
  Stage 2 — code-path tracing:
  Grep `internal/controller/project_controller.go` for all paths that call handleInitJobCompletion. Check whether there's a Phase guard preventing re-entry when Phase > Initialized. If absent, that's H1 confirmed.
  
  Stage 3 — fix design (gated on Stage 2 outcome):
  - If H1: add a Phase guard around the handleInitJobCompletion call site so it doesn't re-fire when Phase is already past Initialized (Complete/PushLeaseFailed/PushLeakBlocked/etc.).
  - If H2: investigate the clone-Job-failed code path and either remove it or gate it on Phase.
  - If H3: switch the relevant Status patch to MergeFrom on a freshly-fetched Project (or use a different optimistic-concurrency strategy).

expecting: |
  Stage 1: Phase trace will show Complete → Initialized → (back to Initialized on every reconcile) for the failing tests. If trace shows Phase staying at Complete and never going through PushLeaseFailed, H1 is wrong and the controller is failing to observe the failed Job (cache lag or watch-event-missing).
  
  Stage 2: handleInitJobCompletion has 1-2 call sites. The simplest fix is to add `if project.Status.Phase != "" && project.Status.Phase != PhasePending { return nil }` (or similar idempotency guard) at the top of handleInitJobCompletion so it's a no-op when Phase has already advanced.
  
  Stage 3: fix is likely production-side (internal/controller/project_controller.go), small (~5-15 lines), quick-task scope.

observed: |
  (populated by debugger)

next_action: |
  Phase 1 (cheapest, observation): run an isolation push-lease run with parallel kubectl watch on Project status. Capture the Phase trace.
  
  Phase 2 (code-trace): grep all handleInitJobCompletion call sites in project_controller.go. Verify whether any guard exists against re-firing when Phase > Initialized.
  
  Phase 3 (hypothesis selection): match observed Phase trace + code structure against H1/H2/H3. Eliminate or strengthen.
  
  Phase 4 (fix scope): determine fix shape. Most likely Option A: idempotency guard at handleInitJobCompletion entry. Alternative Option B: guard at the call site. Alternative Option C: change the Phase guard at line 273 to short-circuit BEFORE handleInitJobCompletion fires when Phase is already advanced.

reasoning_checkpoint: ""
tdd_checkpoint: ""
specialist_hint: ""
```

## Evidence

(populated by debugger)

## Eliminated Hypotheses

(populated by debugger)

## Out of Scope (for this session)

- Cascade 7-bis (phase_controller.go symmetric nil-Project race) — separate follow-up.
- Cascade 7-ter (milestone_controller.go latent nil-deref) — separate follow-up.
- Removing the nil-Project guard at `internal/dispatch/podjob/jobspec.go:266-272` — defense-in-depth follow-up.
- Item 2 Layer B 429 storm spec authoring.
- Modifying `charts/tide/values.yaml` — chart is FIXED contract.
- Running full `make test-int` end-to-end during root-cause investigation. Use the isolation pattern (`make test-int` since GINKGO_FOCUS doesn't filter, but at least one run is enough — 17 min wall).
- Modifying `chaos_resume_test.go` or `pvcPrewarmPod` — those are settled in previous cascades.

## Resolution

(populated when fix lands)
