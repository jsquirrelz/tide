---
status: resolved
trigger: "TestTaskReconciler_HaltsAtMaxAttempts: expected Phase=Failed but got empty string after second reconcileN(3)"
created: 2026-05-12
updated: 2026-05-12
---

## Current Focus

hypothesis: TBD
test: TBD
expecting: TBD
next_action: Run reproducer; instrument hypothesis around Phase 1-3 short-circuit before max-attempts gate at line 294

## Symptoms

expected: After test resets Status.Phase="" / Attempt=0 and re-runs reconcileN(3), Task should land Phase=Failed, Reason=ExceededAttempts because attempt counter is 2 > MaxAttemptsPerTask=1
actual: task.Status.Phase is empty string ("") — never set to Failed
errors: Gomega assertion `Expect(task.Status.Phase).To(Equal("Failed"))` fails at line 752
reproduction: cd /Users/justinsearles/Projects/tide && go test -short -count=1 -run "TestControllers" ./internal/controller/... 2>&1 | tail -30
started: introduced by phase 02 plan 02-09

## Eliminated

## Evidence

- timestamp: 2026-05-12 (initial read)
  checked: task_controller.go Reconcile flow
  found: reconcileDispatch has Step 1 terminal-short-circuit (Succeeded/Failed → early return). After test resets Phase="", task is NOT terminal. Step 2 fires only when Phase=="Running". So with Phase=="", reconcileDispatch falls through to Steps 3–7. Step 5 (indegree) returns 0 because DependsOn is empty (test uses single task). Step 7 (nextAttempt) lists Jobs by label tideproject.k8s/task-uid; if pre-existing Job-1 visible → returns 2 > maxAttempts=1 → Step 7 should patch Phase=Failed.
  implication: Logic SHOULD work IF nextAttempt actually sees Job-1 via mgrClient cache.

- timestamp: 2026-05-12
  checked: First reconcileN(4) before pre-creating Job
  found: First reconcileN runs through Reconcile 4×. First reconcile adds finalizer (Update + return early). Second reconcile adds owner ref (parent Plan doesn't exist — skip ref) and calls reconcileDispatch. reconcileDispatch resolves Project (succeeds), indegree=0, no rate limit (no ProviderSecretRef), nextAttempt initially sees no Jobs → returns 1, attempt(1) NOT > maxAttempts(1), continues to Steps 8–12: builds envelope, status patches Attempt=1+StartedAt, creates Job tide-task-{uid}-1, then patches Status.Phase=Running.
  implication: After first reconcileN(4), Task.Status.Phase=="Running" and Job-1 exists (created via r.Create on the cached client — should reflect in cache quickly).

- timestamp: 2026-05-12
  checked: Test then does k8sClient.Get(ctx, name, &task) — note that uses DIRECT client not mgrClient cache, so it gets fresh from API server. Then pre-creates Job-1 (which fails with AlreadyExists since reconcile already created it — test does `_ = k8sClient.Create(ctx, preExistingJob)` and ignores the result).
  found: After the first reconcileN, Job-1 already exists. The pre-create is essentially a no-op.
  implication: The relevant state going into second reconcileN(3) is: Task with Phase="" (just reset), Job-1 exists.

- timestamp: 2026-05-12
  checked: Test sets `task.Status.Phase = ""` via k8sClient.Status().Patch — direct client (uncached). But mgrClient cache may still see old Phase=="Running" briefly.
  found: This is a cache-coherence concern. Then reconcileN(3) is called with r using mgrClient. First reconcile in this batch: r.Get reads from cache. If cache still has Phase=="Running", reconcileDispatch enters Step 2 (Job lookup for Phase==Running path), uses podjob.JobName(task.UID, task.Status.Attempt) but cached task has Attempt==1, so lookup finds Job-1, calls isJobTerminal(&job) → likely false (Job has no Complete/Failed condition set in test) → returns nil with no action. By the time the second/third reconcile in the batch runs, cache may have synced — but Step 2 still returns early on Phase=Running without progressing.
  implication: If cache still shows Phase=Running on ALL 3 reconciles, the loop never gets to Step 7 max-attempts gate. **HYPOTHESIS**: cache lag on the status patch from "Running" → "" causes all 3 reconciles in second reconcileN(3) to go through the Phase=="Running" branch and return early.

## Resolution

root_cause: Test race (cache lag), not a reconciler logic bug. The test patches Task.Status.Phase="" / Attempt=0 via the direct k8sClient (which writes straight to the API server), but the reconciler reads via mgrClient (the manager's cached client). The cache has not yet ingested the status patch when reconcileN(r, name, 3) starts, so all three reconciles see the stale cached Phase="Running" from the prior reconcileN(4). With Phase=="Running", reconcileDispatch hits Step 2 (the running-Job branch at line 190), looks up the Job for the cached Attempt=1, finds Job-1 (no Complete/Failed condition → isJobTerminal=false), and returns nil. The reconciler never reaches Step 7's max-attempts gate at line 294, so Phase is never patched to "Failed". When the test then re-Gets via k8sClient, it reads the API-server state — which is still the empty Phase the test patched in moments earlier (since no reconcile has overwritten it). Result: assertion sees Phase="" instead of "Failed". The same suite_test.go helper `markTaskSucceeded` already encodes this pattern: after a status patch, Eventually-poll mgrClient.Get until the cache reflects the new value. The failing test omitted that wait.

The 02-09 plan's invariants (max-attempts gate at line 294, indegree per-task, nextAttempt via Job-list-by-task-uid label) are correct and unchanged.

fix: Insert an Eventually-poll on mgrClient.Get between the Status().Patch and reconcileN(3) — matching the same cache-sync pattern used by suite_test.go's markTaskSucceeded helper. The poll waits until the cached Task reflects Phase="" AND Attempt=0, then reconcileN(3) proceeds with a coherent cache.

verification: Full controller suite (`go test -short -count=1 ./internal/controller/...`) passes 38/39 (1 envtest leader-election spec correctly skipped under -short). Test runs deterministically in both full-suite and focused mode.
files_changed:
  - internal/controller/task_controller_test.go (test cache-sync wait)
