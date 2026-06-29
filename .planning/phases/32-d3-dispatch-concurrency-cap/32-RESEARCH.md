# Phase 32: D3 — Dispatch Concurrency Cap - Research

**Researched:** 2026-06-28
**Domain:** Kubernetes controller-runtime; in-process semaphore vs live Job-count cap; budget rollup idempotency hardening
**Confidence:** HIGH

---

<user_constraints>
## User Constraints (from CONTEXT.md)

### Locked Decisions

- **D-01 (Option B is locked):** The cap is a live `client.List` in-flight count-check before pool acquire, returning `ctrl.Result{RequeueAfter}` when count ≥ cap. The in-process semaphore does NOT cap in-flight running pods.
- **D-02 (single shared planner pool):** `cmd/manager/main.go:343` creates one `plannerPool` passed to all four planner controllers. The cap is global.
- **D-03 (no slot leak ordering):** The count-gate MUST sit before any `PlannerPool.Acquire`. Return `RequeueAfter` without acquiring.
- **D-04 (return shape):** Deferred dispatches return `ctrl.Result{RequeueAfter: ...}, nil` — never a Go error. Mirror `milestone_controller.go:375`.
- **D-05 (pools stay separate — CONCUR-03):** `executorConcurrency` and `executorPool` are untouched. `make lint` / crosspool analyzer must stay green.

### Claude's Discretion

- **RQ-1:** Does the List-count gate replace `PlannerPool` semaphore on the planner path, or do both coexist?
- **RQ-2:** Canonical default value to replace `16` in `internal/config/config.go:117` and `charts/tide/values.yaml`.
- **RQ-3:** Global cap vs per-level; namespace scope of the `client.List`; per-reconcile cost.
- **RQ-4:** Observability — log-line-only vs log + Prometheus metric.

### Deferred Ideas (OUT OF SCOPE)

- Per-level planner caps.
- Dashboard "stalled wave" visualization.
- kubectl observation experiment (now a verification gate only, not a planning prerequisite).
</user_constraints>

<phase_requirements>
## Phase Requirements

| ID | Description | Research Support |
|----|-------------|------------------|
| CONCUR-01 | In-flight planner Jobs bounded by configurable `plannerConcurrency` cap at steady state (counting running pods, not concurrent creation calls). | List-gate before Acquire covers all four dispatch sites. |
| CONCUR-02 | Default `plannerConcurrency` reduced from 16 to a single-node-safe value documented in `charts/tide/values.yaml`. | Resource footprint analysis + chart-is-FIXED-contract rule. |
| CONCUR-03 | Planner and executor pools remain separately sized; executor pool unchanged. | Crosspool analyzer enforces at compile time; cap touches only planner path. |
| CONCUR-04 | Deferred dispatches are observable and never silently truncate a wave. | Log line at min; `RequeueAfter` ensures eventual dispatch. |
</phase_requirements>

---

## Summary

Phase 32 adds a live `client.List` in-flight count-check to each of the four planner dispatch sites before their `PlannerPool.Acquire`, returning `ctrl.Result{RequeueAfter}` when the count of non-terminal planner Jobs meets or exceeds the configured cap. This is the only mechanism that correctly bounds in-flight running pods: the existing `PlannerPool` semaphore releases on reconcile return (milliseconds after `r.Create`), not on Job terminal state.

The four dispatch sites share an identical shape (Acquire → defer Release → Create → return). A single package-internal helper — `plannerInFlightCount(ctx, client, watchNamespace)` that lists Jobs by `tideproject.k8s/role=planner` and counts non-terminal ones — makes the gate DRY across all four and keeps it outside the crosspool analyzer's scope (no select statement referencing both pool names).

Alongside the D3 cap, Phase 32 carries the Phase 31 hardening debt: WR-02/WR-03 wrap the `*RolledUpUID` marker stamp in `retry.RetryOnConflict` with a re-fetch, and WR-01 corrects the misleading comment (or switches to `MergeFromWithOptimisticLock`) on the suppression patch. These are independent of the cap change and should ship as their own plan.

**Primary recommendation:** Implement a `plannerInFlightCount` helper (no pool change, no new CRD surface, no new dependency). Keep the semaphore for thundering-herd protection; the List-gate is the steady-state cap. Default value: `4` in both `config.go` and `values.yaml`, moved together.

---

## Architectural Responsibility Map

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|-------------|----------------|-----------|
| In-flight count gate | API / Backend (controller) | — | Planner dispatch is a controller concern; the gate checks cluster state via kube API |
| Pool semaphore (thundering-herd guard) | API / Backend (controller) | — | In-process, not cluster state; belongs in the reconciler loop |
| Prometheus metric (if added) | API / Backend (controller) | — | Registered in `internal/metrics` init(), scraped by controller-runtime /metrics |
| Config default (values.yaml) | CDN / Static (Helm chart) | API / Backend (config load) | Chart is the FIXED contract; binary default catches up to chart default |

---

## Standard Stack

No new external packages. All implementation uses:

| Component | Location | Purpose |
|-----------|----------|---------|
| `sigs.k8s.io/controller-runtime/pkg/client` | existing | `client.List` with `MatchingLabels` for in-flight count |
| `k8s.io/api/batch/v1` | existing | `batchv1.JobList`, `isJobTerminal` helper |
| `k8s.io/client-go/util/retry` | existing (already used in `budget/tally.go`) | `retry.RetryOnConflict` for WR-02/WR-03 marker stamp |
| `github.com/prometheus/client_golang/prometheus` | existing | New gauge/counter for RQ-4 if a metric is added |
| `sigs.k8s.io/controller-runtime/pkg/metrics` | existing | `metrics.Registry.MustRegister` |

## Package Legitimacy Audit

No new external packages are introduced by this phase. All code uses existing imports already in `go.mod`. Audit is N/A.

---

## Architecture Patterns

### System Architecture Diagram

```
Reconcile() called (Milestone/Phase/Plan/Project planner dispatch site)
    │
    ├─ [existing] ImportSource hold check → RequeueAfter 5s if pending
    ├─ [existing] BudgetBlocked check → RequeueAfter 30s if blocked
    ├─ [existing] FailureHalt check → RequeueAfter 30s if halted
    │
    ├─ [NEW D3 gate] plannerInFlightCount(ctx, r.Client, r.WatchNamespace)
    │       │
    │       ├─ client.List(batchv1.JobList, MatchingLabels{role=planner})
    │       │    scoped to WatchNamespace (or all-ns if "")
    │       │
    │       ├─ count jobs where !isJobTerminal(job)
    │       │
    │       ├─ count >= cap?  ──YES──► log "planner dispatch deferred: cap reached"
    │       │                          return RequeueAfter(10s), nil
    │       │
    │       └─ count < cap   ──NO──► continue
    │
    ├─ [existing] PlannerPool.Acquire(ctx)      ← semaphore still present
    │   defer PlannerPool.Release()
    │
    ├─ [existing] build envelope
    ├─ [existing] r.Create(ctx, job)
    └─ [existing] r.Status().Patch(...)
```

**Data flow note:** The list-gate is before Acquire, so a parked reconcile returns without taking a semaphore slot (D-03 invariant preserved).

### Recommended Project Structure

No new directories. Changes land in:

```
internal/
├── controller/
│   ├── dispatch_helpers.go          # new: plannerInFlightCount helper
│   ├── dispatch_helpers_test.go     # extend with in-flight count unit tests
│   ├── milestone_controller.go      # insert gate before Acquire (~line 380)
│   ├── phase_controller.go          # insert gate before Acquire (~line 378)
│   ├── plan_controller.go           # insert gate before Acquire (~line 384)
│   ├── project_controller.go        # insert gate before Acquire (~line 1177)
│   ├── milestone_controller.go      # WR-02: wrap MilestoneRolledUpUID stamp in RetryOnConflict
│   ├── phase_controller.go          # WR-02: wrap PhaseRolledUpUID stamp in RetryOnConflict
│   └── plan_controller.go           # WR-02: wrap PlanRolledUpUID stamp in RetryOnConflict
├── config/
│   └── config.go:117                # change default from 16 to 4
└── metrics/
    └── registry.go                  # (optional, RQ-4) add tide_planner_dispatches_deferred_total

charts/tide/
└── values.yaml:78                   # change plannerConcurrency from 16 to 4
```

### Pattern 1: plannerInFlightCount Helper (DRY gate across all four dispatch sites)

**What:** A package-internal function that lists `batchv1.Job` objects filtered by `tideproject.k8s/role=planner` and returns the count of non-terminal jobs. Returns `(int, error)`.

**When to use:** Called at each of the four dispatch sites, before `PlannerPool.Acquire`.

**Why non-terminal instead of `Status.Active > 0`:** `Status.Active` is set by kube-controller-manager after the pod starts. Between `r.Create(job)` and pod start (~1-5s), `Status.Active` is 0. In a wide wave fan-out, multiple reconciles can fire "simultaneously" from the work queue, each seeing 0 Active jobs, and all pass the cap check — defeating it. Non-terminal counting (job exists AND `!isJobTerminal(job)`) catches newly-created jobs immediately because the informer cache reflects the object as soon as the watch event propagates (subsecond), regardless of whether the pod has started.

**Why `isJobTerminal` (not `CompletionTime`):** `isJobTerminal` already exists in `internal/controller/task_controller.go:1706` (also duplicated in `dispatch/podjob/backend.go:338`). It checks `batchv1.JobComplete` and `batchv1.JobFailed` conditions, which is the canonical K8s signal. `CompletionTime` can be nil even for failed jobs (e.g., if the job hit `backoffLimit`). Stay consistent with the codebase.

**Pitfall — `isJobTerminal` duplication:** The same function exists in two places. The planner should note that the helper should import or call the controller-package version (`task_controller.go:1706`), or a third copy should be extracted as a shared package-internal helper. Given everything lives in `package controller`, simply reusing the existing function body is fine; do not introduce a third copy.

```go
// Source: direct code read — mirrors pool.go PreCharge + isJobTerminal pattern
// in internal/controller/task_controller.go:1706

// plannerInFlightCount returns the number of non-terminal planner Jobs
// currently visible in the informer cache. An empty watchNamespace means
// list across all namespaces (matches the manager's cluster-scoped install posture).
func plannerInFlightCount(ctx context.Context, c client.Client, watchNamespace string) (int, error) {
    var jobs batchv1.JobList
    opts := []client.ListOption{
        client.MatchingLabels{"tideproject.k8s/role": "planner"},
    }
    if watchNamespace != "" {
        opts = append(opts, client.InNamespace(watchNamespace))
    }
    if err := c.List(ctx, &jobs, opts...); err != nil {
        return 0, err
    }
    count := 0
    for i := range jobs.Items {
        if !isJobTerminal(&jobs.Items[i]) {
            count++
        }
    }
    return count, nil
}
```

**Call site (identical at all four dispatch sites, before PlannerPool.Acquire):**

```go
// [NEW] D3 in-flight cap check — before pool acquire (D-03 ordering invariant).
if r.PlannerPool != nil {
    inFlight, err := plannerInFlightCount(ctx, r.Client, r.WatchNamespace)
    if err != nil {
        // List failure is transient (API server unreachable); requeue.
        return ctrl.Result{}, fmt.Errorf("planner in-flight count: %w", err)
    }
    if inFlight >= r.PlannerPool.Capacity() {
        logf.FromContext(ctx).V(1).Info("planner dispatch deferred: concurrency cap reached",
            "inFlight", inFlight, "cap", r.PlannerPool.Capacity())
        return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
    }
}

// [existing] PlannerPool.Acquire — semaphore for thundering-herd protection.
if r.PlannerPool != nil {
    if err := r.PlannerPool.Acquire(ctx); err != nil {
        return ctrl.Result{}, err
    }
    defer r.PlannerPool.Release()
}
```

**Note on `r.PlannerPool.Capacity()`:** `pool.Pool` does not currently expose a `Capacity()` method. The planner MUST add `func (p *Pool) Capacity() int { return cap(p.sem) }` to `internal/pool/pool.go`. This is a one-line addition; it does not change the crosspool analyzer behavior (the analyzer inspects `select` statements for both pool names, not method calls). Alternatively, thread `cfg.PlannerConcurrency` as a separate `PlannerConcurrency int` field on each reconciler struct — this avoids modifying `pool.Pool` but adds a field to four structs and four `main.go` wiring sites. The `Capacity()` method is cleaner and keeps the cap co-located with the pool.

### Pattern 2: WR-02/WR-03 Marker Stamp Hardening

**What:** Wrap the `*RolledUpUID` Status.Patch in `retry.RetryOnConflict` with a re-fetch, mirroring `budget.RollUpUsage` exactly.

**Why:** The marker stamp runs against the level object as fetched at Reconcile() start. Any future status write earlier in `handleJobCompletion` would leave the object stale, and the plain `MergeFrom` would silently overwrite or fail. Since the marker is the sole idempotency guard for exactly-once budget accrual, it deserves the same `RetryOnConflict` + `MergeFromWithOptimisticLock` treatment that `RollUpUsage` uses for the Project.

**Example (milestone, from WR-02 in 31-REVIEW.md):**

```go
// Source: 31-REVIEW.md WR-02 / mirrors budget.RollUpUsage in internal/budget/tally.go:57
if err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
    latest := &tideprojectv1alpha2.Milestone{}
    if err := r.Get(ctx, client.ObjectKeyFromObject(ms), latest); err != nil {
        return err
    }
    if latest.Status.MilestoneRolledUpUID == milestoneJobName {
        return nil // already set by a concurrent reconcile — idempotent
    }
    patch := client.MergeFromWithOptions(latest.DeepCopy(), client.MergeFromWithOptimisticLock{})
    latest.Status.MilestoneRolledUpUID = milestoneJobName
    return r.Status().Patch(ctx, latest, patch)
}); err != nil {
    logger.Error(err, "patch MilestoneRolledUpUID failed (non-fatal)", "milestone", ms.Name)
}
```

Apply the same pattern to `PhaseRolledUpUID` (phase_controller.go:529-533) and `PlanRolledUpUID` (plan_controller.go:605-609).

**WR-03 complement:** With `RetryOnConflict` wrapping the stamp, the window where "rolled up but marker unset" can survive a restart is materially smaller. For full WR-03 closure: if the retry budget is exhausted, return the error (requeue the reconcile) rather than logging-and-continuing. This ensures the marker is durably set before the reporter Job's TTL-GC window can trigger a second rollup opportunity.

### Pattern 3: WR-01 Suppression Patch Comment Fix

**File:** `project_controller.go:1154-1166`

The suppression patch uses `client.MergeFrom(project.DeepCopy())` (no `MergeFromWithOptimisticLock`). The inline comment incorrectly claims "Conflict is retryable". Either:
- Drop the misleading comment (patch is a server-side merge, errors are I/O only), or
- Switch to `MergeFromWithOptimisticLock{}` if concurrent write detection is genuinely wanted.

**Recommendation:** Switch to `MergeFromWithOptimisticLock{}` to match `RollUpUsage` and be internally consistent. The existing behavior is benign for these two fields, but the comment encodes a false invariant.

### Anti-Patterns to Avoid

- **Listing without namespace scope when WatchNamespace is set:** If the manager is installed in namespace-scoped mode (`--watch-namespace=foo`), an unrestricted `client.List` would fail because the informer cache is namespace-scoped and won't have cross-namespace Job entries. Always pass `client.InNamespace(watchNamespace)` when `watchNamespace != ""`.
- **Counting `Status.Active > 0` only:** This misses newly-created but not-yet-started Jobs (1-5s gap between `r.Create` and kube-controller-manager setting `Status.Active=1`). Use `!isJobTerminal(job)` instead.
- **Placing the gate after `PlannerPool.Acquire`:** This violates D-03 ("park before Acquire, never after"). A slot leaked by parking post-Acquire is never returned.
- **Returning a Go error for cap-reached:** CONCUR-04 requires deferred dispatches, not failures. Return `ctrl.Result{RequeueAfter: 10*time.Second}, nil`. Only List errors return a Go error (transient, retryable).
- **Adding a `task` label to any Prometheus metric:** Forbidden by `tools/analyzers/metriccardinality` and `internal/metrics/doc.go`. The analyzer rejects it at compile time.
- **Unifying planner and executor pools:** crosspool analyzer (`make lint`) rejects any `select` statement that waits on both pool channel names. Keep separate.

---

## RQ-1 Resolution: Semaphore Fate

**Recommendation: Coexist. Keep the `PlannerPool` semaphore AND add the List-count gate.**

**Evidence and reasoning:**

The `PlannerPool` semaphore (chan struct{}, capacity = plannerConcurrency) currently caps concurrent `r.Create` calls within a single reconcile burst. Even after the List-gate is added, concurrent reconcile goroutines (`MaxConcurrentReconciles` > 1) can pass the List-count check simultaneously if they all fire before any of them completes `r.Create`. For example, with cap=4 and `MaxConcurrentReconciles.Milestone=1`, only one milestone reconcile fires at a time per level — but with `MaxConcurrentReconciles.Phase=2`, two phase reconciles can fire simultaneously, both check the List (count=3), both see 3 < 4, both proceed. The semaphore limits this thundering-herd to at most `plannerConcurrency` concurrent creates.

Removing the semaphore would require `MaxConcurrentReconciles` to equal 1 across all four levels (a throughput regression) or accept bounded overshoot from concurrent passes of the List gate. The bounded overshoot window is small (one Create per concurrent reconcile), so removal is theoretically viable, but:

1. `pool.Pool.PreCharge` is still needed at startup — the pool struct must exist.
2. Removing the Acquire/Release from four controllers, four main.go wiring sites, and the crosspool tests is more churn than value for this phase.
3. The semaphore and the List-gate serve different purposes (thundering-herd vs steady-state cap). Keeping both is the correct layered defense.

**Implementation:** The gate runs BEFORE `PlannerPool.Acquire`. No change to Acquire/Release semantics.

---

## RQ-2 Resolution: Default Value

**Recommendation: `4`.**

**Evidence:**

- Controller manager pod: 10m CPU request / 64 MiB memory request (values.yaml).
- Each planner pod (subagent + credproxy sidecar): no explicit resource requests in `internal/dispatch/podjob/jobspec.go`. In practice, the Claude Code CLI + credproxy pair consumes ~300-500 MiB RSS (from Phase 18 eval observations).
- A single-node kind cluster (the TIDE dev and test environment) has ~7.65 GiB RAM (from CLAUDE.md Operating Notes). With 4 planner pods at ~500 MiB each = ~2 GiB for planners, plus manager + system pods (~1 GiB), leaves ~4.5 GiB for executor Jobs and other workloads.
- `executorConcurrency` default is `4` (config.go:120). With `plannerConcurrency: 4`, a 5-phase wave dispatches 4 planners and parks the 5th — the 5th re-queues in 10s. This is single-node-safe and documented behavior.
- `2` is the conservative floor but would artificially slow even a modest plan tree; `4` is the right balance for single-node safety with a meaningful planning fan-out.
- The v2 requirement CONCUR-F1 (per-Project override CRD field) deferred — the chart-level cap is sufficient for v1.0.6.

**Change locations (must move together — chart is FIXED contract):**
- `internal/config/config.go:117`: `resolveField("plannerConcurrency", raw.PlannerConcurrency, 4, ...)` (was 16)
- `charts/tide/values.yaml:78`: `plannerConcurrency: 4` (was 16)
- Add chart comment: "# Single-node-safe default. The cap must be ≥ the widest expected planning wave; increase for multi-node clusters."

---

## RQ-3 Resolution: Cap Scope and List Selector

**Recommendation: Global cap (one list across all planner Jobs), no-namespace-restriction when `WatchNamespace == ""`.**

**Evidence:**

- `cmd/manager/main.go:343` creates ONE `plannerPool` passed to all four controllers. The semaphore is already global.
- `plannerPool.PreCharge` at line 350 lists with selector `"tideproject.k8s/role=planner"` without namespace restriction — and this is the precedent for counting planner Jobs globally.
- A per-level cap would require four separate pool instances, four separate `PlannerConcurrency` config fields, and four separate list selectors (e.g., `tideproject.k8s/level=milestone`) — more tuning surface than CONCUR-01..04 require.

**Selector:** `client.MatchingLabels{"tideproject.k8s/role": "planner"}` — all planner Jobs at all levels.

**Label verification:** `internal/dispatch/podjob/jobspec.go:217` stamps `labels["tideproject.k8s/role"] = "planner"` on every `JobKindPlanner` Job. `pool.PreCharge` uses the string form `"tideproject.k8s/role=planner"` which parses to the same selector. Both are confirmed. [VERIFIED: direct code read]

**Namespace scope:**
- If `r.WatchNamespace == ""` (default cluster-scoped install): list all namespaces (no `client.InNamespace`). This matches how `pool.PreCharge` lists at startup.
- If `r.WatchNamespace != ""` (namespace-scoped install): add `client.InNamespace(watchNamespace)`. Otherwise the informer cache (namespace-scoped) won't have cross-namespace entries and the List will return empty, defeating the gate.

**Per-reconcile List cost:**
- The controller-runtime cached client's `List` operation reads from the in-process informer cache, not the API server. It is an in-memory map scan, sub-millisecond for O(100) Jobs. The planner path already runs one or more `r.Get` calls per reconcile — one additional cache scan adds negligible overhead.
- Cache staleness: the informer is eventually consistent. A Job created by another goroutine's reconcile in the same millisecond may not yet be reflected. This is the thundering-herd scenario handled by the semaphore (see RQ-1). The gap is bounded to `MaxConcurrentReconciles` concurrent creates, which is small (≤4 for plan-level reconciles).

---

## RQ-4 Resolution: Observability

**Recommendation: Log line only for v1.0.6. Prometheus metric is v2 (already deferred as OBS-01).**

**Evidence:**
- REQUIREMENTS.md §"v2 Requirements": `OBS-01: Prometheus pool-saturation gauge for deferred planner dispatches (logging is sufficient for v1.0.6)`.
- CONCUR-04 minimum: "observable (log line at minimum)".
- Adding a metric requires `internal/metrics/registry.go` changes, `metriccardinality` analyzer compliance, and wiring. This is not blocked (gauge at level `{level}` would pass the analyzer), but it adds a plan and is explicitly deferred.

**Log line shape (V(1) — verbose, not always emitted):**

```go
logf.FromContext(ctx).V(1).Info("planner dispatch deferred: concurrency cap reached",
    "inFlight", inFlight, "cap", r.PlannerPool.Capacity(),
    "<level>", <obj>.Name)
```

V(1) is consistent with the existing import-hold log at `milestone_controller.go:373` (`"import pending; holding planner dispatch"` at V(1)). This keeps the log non-noisy at default verbosity and queryable with `-v=1`.

**If a metric is added later (v2):** A `tide_planner_dispatches_deferred_total` counter or `tide_planner_in_flight` gauge with label `level ∈ {project, milestone, phase, plan}` would pass `metriccardinality` (no "task" label). The `level` label alphabet already appears in `tide_dispatch_latency_seconds`. Register in `internal/metrics/registry.go` init(), follow the existing `MustRegister` pattern.

---

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| Counting non-terminal jobs | Custom Job state machine | `isJobTerminal()` (already in `task_controller.go:1706`) | Checks `batchv1.JobComplete` and `batchv1.JobFailed` conditions — the canonical K8s terminal signal |
| Retry on Status patch conflict | Custom retry loop with sleep | `retry.RetryOnConflict(retry.DefaultRetry, ...)` from `k8s.io/client-go/util/retry` | Already imported in `internal/budget/tally.go:57`; controller-runtime standard pattern |
| Optimistic lock on Status patch | Double-fetch pattern | `client.MergeFromWithOptions(latest.DeepCopy(), client.MergeFromWithOptimisticLock{})` | Same pattern RollUpUsage uses; embeds resourceVersion in the patch so conflicts surface |
| In-flight cap enforcement | External queue (Kueue, Volcano) | List-count gate + existing semaphore | Adds external dependency; out of scope per REQUIREMENTS.md §"Out of Scope" |

---

## Common Pitfalls

### Pitfall 1: Counting `Status.Active > 0` instead of `!isJobTerminal`

**What goes wrong:** After `r.Create(job)`, the Job object is in the informer cache but `Status.Active == 0` for ~1-5 seconds while kube-controller-manager creates the pod. Concurrent reconciles all see 0 Active jobs, all pass the cap, and all call `r.Create`. With 10 reconciles firing in a wide wave, 10 Jobs get created regardless of cap.

**Why it happens:** `Status.Active` reflects pod-level state managed by kube-controller-manager, not the Job object's existence.

**How to avoid:** Count `!isJobTerminal(job)` — a job that exists and hasn't finished is in-flight, regardless of whether its pod has started.

**Warning signs:** `kubectl get jobs -l tideproject.k8s/role=planner` shows more jobs than the cap when a wide wave dispatches.

### Pitfall 2: Gate after Acquire (slot leak)

**What goes wrong:** If the count check happens after `PlannerPool.Acquire`, a reconcile that finds the cap reached must return `RequeueAfter` — but it has already taken a semaphore slot. `defer Release()` eventually fires, but the slot is "held" for the entire requeue period (10s), reducing available slots for legitimate dispatches.

**Why it happens:** Copy-paste from the `failure_halt.go` pattern without checking the ordering invariant.

**How to avoid:** D-03 / Phase 31 ordering invariant: gate BEFORE Acquire. This is already documented in the code comments at `project_controller.go:1090` and `milestone_controller.go:369`.

**Warning signs:** `make lint` / crosspool analyzer won't catch this, but `plannerPool.sem` fills up faster than expected; effective concurrency drops below cap.

### Pitfall 3: `client.List` returning empty in namespace-scoped install

**What goes wrong:** If `r.WatchNamespace != ""` but the List call doesn't pass `client.InNamespace(watchNamespace)`, the cached informer (namespace-scoped) returns an empty `JobList`. The gate sees 0 in-flight jobs, always passes, and the cap is never enforced.

**Why it happens:** The default install is cluster-scoped (`WatchNamespace: ""`), so tests pass, but namespace-scoped installs break silently.

**How to avoid:** Always check `if r.WatchNamespace != ""` and append `client.InNamespace(r.WatchNamespace)` to the List options. Mirror the exact same pattern used at `project_controller.go:949` and `milestone_controller.go:317`.

### Pitfall 4: RequeueAfter too short causes hot-polling

**What goes wrong:** A 1-second `RequeueAfter` causes each capped reconcile to re-check every second. With 20 capped Milestones, 20 reconciles/second hit the List gate, even though planner Jobs complete every 5-30 minutes.

**Why it happens:** Copying the import-hold's 5s interval for a scenario with much longer holding times.

**How to avoid:** Use 10s for the concurrency-cap requeue (longer than import-hold's 5s; planner Jobs run for minutes, not seconds). The existing `billing_halt.go` and `budget_blocked.go` use 30s for conditions that rarely clear quickly; 10s is a reasonable middle ground for an in-flight count that changes whenever a planner Job completes.

### Pitfall 5: WR-02 — non-fatal swallow of marker-stamp failure

**What goes wrong:** If the marker stamp fails and the error is logged-and-swallowed, the `*RolledUpUID` field is empty. If the reporter Job then TTL-GCs and a reconcile re-fires with `isFirstCompletion=true`, the budget is double-counted.

**Why it happens:** Current code: `logger.Error(pErr, "patch ... failed (non-fatal)", ...)` — the reconcile continues.

**How to avoid:** WR-02 `RetryOnConflict` reduces the failure probability to near-zero. For WR-03 closure: if retry budget is exhausted, return the error to requeue rather than continuing.

### Pitfall 6: crosspool analyzer false positive from a helper function

**What goes wrong:** If the helper function references both "planner" and "executor" identifiers in the same expression (e.g., a function named `plannerOrExecutorPool`), the crosspool analyzer might fire.

**Why it happens:** The crosspool analyzer is identifier-based (string contains "planner" or "executor"), not type-based.

**How to avoid:** Name the helper `plannerInFlightCount` — only contains "planner", not "executor". The analyzer only rejects `select` statements, not function calls, so a properly-named helper won't trigger it regardless. But naming hygiene matters for future readers.

---

## Code Examples

### In-flight Count Helper (verified pattern)

```go
// Source: direct code read — pool.go PreCharge + isJobTerminal in task_controller.go:1706
// Place in internal/controller/dispatch_helpers.go (existing file)

// plannerInFlightCount returns the count of non-terminal planner Jobs visible
// in the informer cache. Used by the D3 concurrency cap gate before PlannerPool.Acquire.
// An empty watchNamespace counts across all namespaces (cluster-scoped install).
func plannerInFlightCount(ctx context.Context, c client.Client, watchNamespace string) (int, error) {
    var jobs batchv1.JobList
    opts := []client.ListOption{
        client.MatchingLabels{"tideproject.k8s/role": "planner"},
    }
    if watchNamespace != "" {
        opts = append(opts, client.InNamespace(watchNamespace))
    }
    if err := c.List(ctx, &jobs, opts...); err != nil {
        return 0, err
    }
    n := 0
    for i := range jobs.Items {
        if !isJobTerminal(&jobs.Items[i]) {
            n++
        }
    }
    return n, nil
}
```

### Pool Capacity Method (needed for gate)

```go
// Source: direct code read of pool.go — cap() is the Go built-in for buffered channel capacity
// Add to internal/pool/pool.go

// Capacity returns the maximum number of concurrent acquisitions this Pool permits.
func (p *Pool) Capacity() int {
    return cap(p.sem)
}
```

### WR-02 RetryOnConflict Marker Stamp (milestone — apply analogously to phase/plan)

```go
// Source: 31-REVIEW.md WR-02; mirrors budget.RollUpUsage in internal/budget/tally.go:57
milestoneJobName := fmt.Sprintf("tide-milestone-%s-1", ms.UID)
if isFirstCompletion && envReadOK && project != nil {
    if ms.Status.MilestoneRolledUpUID != milestoneJobName {
        if rollErr := budget.RollUpUsage(ctx, r.Client, project, out.Usage); rollErr != nil {
            logger.Error(rollErr, "milestone planner budget rollup failed (non-fatal)", "milestone", ms.Name)
        } else {
            if err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
                latest := &tideprojectv1alpha2.Milestone{}
                if err := r.Get(ctx, client.ObjectKeyFromObject(ms), latest); err != nil {
                    return err
                }
                if latest.Status.MilestoneRolledUpUID == milestoneJobName {
                    return nil // already set
                }
                patch := client.MergeFromWithOptions(latest.DeepCopy(), client.MergeFromWithOptimisticLock{})
                latest.Status.MilestoneRolledUpUID = milestoneJobName
                return r.Status().Patch(ctx, latest, patch)
            }); err != nil {
                logger.Error(err, "patch MilestoneRolledUpUID failed (non-fatal)", "milestone", ms.Name)
            }
        }
    }
}
```

---

## Runtime State Inventory

Not applicable — this phase is a controller code edit. No stored data, no live service config, no OS-registered state. No rename/refactor involved.

---

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|-------|---------|---------------|
| A1 | Each planner pod (subagent + credproxy) consumes ~300-500 MiB RAM. | RQ-2 Default Value | If pods are lighter, `4` is conservative but safe; if heavier, `4` may still OOM a single-node cluster. `2` is the safe floor if unsure. |
| A2 | The informer cache propagates a newly-created Job before a subsequent reconcile for a *different* object can fire its List | RQ-3 List Cost / Pitfall 1 | If propagation is slower than expected, the non-terminal count undercounts briefly; the thundering-herd semaphore limits the overshoot. |

---

## Open Questions

1. **`pool.Pool.Capacity()` vs `PlannerConcurrency int` reconciler field**
   - What we know: `pool.Pool.sem` is private; `cap(p.sem)` is the Go built-in.
   - What's unclear: planner prefers `pool.Pool.Capacity()` (encapsulated) or a new `PlannerConcurrency int` field on each reconciler (more explicit, no pool.go change).
   - Recommendation: Add `Capacity()` to `pool.Pool`. One-line change; keeps the cap tied to the pool object. Avoids four new reconciler fields and four new main.go wiring assignments.

2. **`RequeueAfter` interval for cap-reached parking**
   - What we know: import-hold uses 5s; billing-halt uses 30s; budget-blocked uses 30s.
   - What's unclear: optimal interval for a condition (planner Job completion) that changes every ~5-30 minutes.
   - Recommendation: `10 * time.Second`. Not so short as to hot-poll, not so long as to stall a wave for a full minute. Can be tuned as a constant.

3. **WR-04 single-patch invariant test (IN SCOPE per CONTEXT D-06)**
   - What we know: The test asserts end state only, not single-patch atomicity.
   - What's unclear: Whether the plan should include an additional test assertion.
   - Recommendation: Yes, add a patch-count assertion in `adoption_lifecycle_test.go`. Scope it as a small addition in the WR plan — not a separate phase.

---

## Environment Availability

Step 2.6: SKIPPED — this phase is purely controller code changes with no new external tools, CLI utilities, or services. `retry.RetryOnConflict` is already in `go.mod` (used in `internal/budget/tally.go`).

---

## Validation Architecture

`workflow.nyquist_validation: true` in `.planning/config.json`. Section required.

### Test Framework

| Property | Value |
|----------|-------|
| Framework | Ginkgo v2 + Gomega (controller package envtest suite) |
| Config file | `internal/controller/suite_test.go` — `BeforeSuite` starts envtest + manager |
| Quick run command | `go test ./internal/controller/... -run TestControllers -v` |
| Full suite command | `make test-int` |

### Phase Requirements → Test Map

| Req ID | Behavior | Test Type | Automated Command | File Exists? |
|--------|----------|-----------|-------------------|-------------|
| CONCUR-01 | In-flight count gate parks a (N+1)th dispatch when N Jobs are non-terminal | unit (fake client) | `go test ./internal/controller/... -run TestPlannerInFlightCount` | ❌ Wave 0 |
| CONCUR-01 | Gate fires at ALL four dispatch sites (milestone, phase, plan, project) | unit (fake client) | `go test ./internal/controller/... -run TestConcurrencyCapGate` | ❌ Wave 0 |
| CONCUR-02 | Default `plannerConcurrency` is `4` after config Load with no override | unit | `go test ./internal/config/... -run TestDefaultPlannerConcurrency` | ❌ Wave 0 (add to config_test.go) |
| CONCUR-03 | `executorPool` is untouched; crosspool analyzer passes | static | `make lint` | ✅ existing |
| CONCUR-04 | Deferred dispatch returns `RequeueAfter`, not an error; not dropped | unit (fake client) | included in `TestConcurrencyCapGate` | ❌ Wave 0 |
| WR-02 | `MilestoneRolledUpUID` stamp uses `RetryOnConflict`; double-count prevented after TTL-GC | envtest | existing `child_rollup_idempotency_test.go` (extend) | ✅ extend |
| WR-02 | Same for `PhaseRolledUpUID`, `PlanRolledUpUID` | envtest | extend `child_rollup_idempotency_test.go` | ✅ extend |

### Sampling Rate

- **Per task commit:** `go test ./internal/controller/... -run TestPlannerInFlightCount -v`
- **Per wave merge:** `go test ./internal/controller/... && go test ./internal/pool/...`
- **Phase gate:** Full `make test-int` green before `/gsd:verify-work`

### Wave 0 Gaps

- [ ] `internal/controller/dispatch_helpers_test.go` — `TestPlannerInFlightCount` unit test with fake client: 3 non-terminal Jobs, cap=3, expect park; 2 non-terminal + 1 terminal, cap=3, expect pass; 0 Jobs, cap=4, expect pass.
- [ ] `internal/controller/dispatch_helpers_test.go` — `TestConcurrencyCapGate`: construct a `MilestoneReconciler` with fake pool (capacity=1) and `plannerInFlightCount` returning 1; assert reconcile returns `RequeueAfter > 0, nil` and does NOT call `PlannerPool.Acquire`.
- [ ] Add `pool.Pool.Capacity()` test to `internal/pool/pool_test.go`.
- [ ] Extend `internal/config/config_test.go` with `TestDefaultPlannerConcurrency` asserting `Load` with empty file produces `PlannerConcurrency=4`.

*(If no gaps: "None — existing test infrastructure covers all phase requirements")*

---

## Security Domain

`security_enforcement` is absent from `.planning/config.json` — treated as enabled.

### Applicable ASVS Categories

| ASVS Category | Applies | Standard Control |
|---------------|---------|-----------------|
| V2 Authentication | No | — |
| V3 Session Management | No | — |
| V4 Access Control | No | — |
| V5 Input Validation | Partial | `resolveField` in `config.go` validates `plannerConcurrency >= 1`; the new default `4` passes. |
| V6 Cryptography | No | — |

### Known Threat Patterns for this Stack

| Pattern | STRIDE | Standard Mitigation |
|---------|--------|---------------------|
| DoS via planning cascade OOM | Denial of Service | The D3 cap itself is the mitigation |
| Double-count budget accrual (WR-02/03) | Tampering | `RetryOnConflict` + `MergeFromWithOptimisticLock` |
| Slot leak (park after Acquire) | Denial of Service (resource exhaustion) | D-03 ordering invariant: gate before Acquire |

---

## Project Constraints (from CLAUDE.md)

Directives enforced by `./CLAUDE.md` that are relevant to this phase:

- **`values.yaml` is a FIXED contract** — binary catches up to chart, never reverse. Both `config.go` and `values.yaml` must change together, chart first.
- **Layered Kahn in stdlib** — not applicable to this phase.
- **Native K8s Jobs** — in-process pool + count-check, no external queue (Kueue/Volcano explicitly excluded in REQUIREMENTS.md Out of Scope).
- **CRD `.status` only** — no new persistence; the cap is in-process + live List only.
- **Pools sized separately** — crosspool analyzer enforces at compile time.
- **`MaxConcurrentReconciles` is NOT the D3 lever** — must stay strictly greater than `plannerConcurrency` (documented in STATE.md).
- **GSD workflow enforcement** — all changes through GSD plan execution.

---

## Sources

### Primary (HIGH confidence)

- Direct code read: `internal/pool/pool.go` — `Pool.Acquire`, `Pool.Release`, `Pool.PreCharge`, `countActive` (Status.Active > 0 counting)
- Direct code read: `internal/config/config.go:117` — `resolveField("plannerConcurrency", ..., 16, ...)`
- Direct code read: `internal/controller/milestone_controller.go:380-386` — four identical dispatch sites
- Direct code read: `internal/controller/task_controller.go:1706-1715` — `isJobTerminal`
- Direct code read: `internal/dispatch/podjob/jobspec.go:217` — `tideproject.k8s/role=planner` label stamp
- Direct code read: `internal/budget/tally.go:57-89` — `RollUpUsage` `RetryOnConflict` + `MergeFromWithOptimisticLock` pattern
- Direct code read: `internal/metrics/registry.go` — metric registration convention and `metriccardinality` cardinality discipline
- Direct code read: `tools/analyzers/crosspool/analyzer.go` — analyzer scope (select statements, identifier-based)
- Direct code read: `tools/analyzers/metriccardinality/analyzer.go` — forbids `"task"` label in Vec constructors
- Direct code read: `charts/tide/values.yaml:78` — `plannerConcurrency: 16`
- Direct code read: `cmd/manager/main.go:343,350,445,475,501` — single shared `plannerPool`, PreCharge label selector
- Direct code read: `.planning/phases/31-.../31-REVIEW.md` — WR-01/WR-02/WR-03/WR-04 hardening items verbatim

### Secondary (MEDIUM confidence)

- CLAUDE.md "Constrained-VM full-suite recipe" — 7.65 GiB single-node kind cluster, basis for RQ-2 footprint reasoning.
- Memory note in MEMORY.md — Phase 18 eval: real token baselines, credproxy+tide-eval in golang:1.26.3 container — implies per-planner memory in the ~300-500 MiB range [ASSUMED for the specific figure].

---

## Metadata

**Confidence breakdown:**
- Standard stack: HIGH — all code verified by direct read; no new external packages
- Architecture: HIGH — dispatch sites, pool API, label selector, ordering invariant all confirmed in source
- Pitfalls: HIGH — Pitfall 1 (Active vs non-terminal) derived from first-principles analysis of K8s Job lifecycle; others confirmed by direct code read
- WR-02/03 hardening: HIGH — 31-REVIEW.md WR-02 provides exact code pattern; budget/tally.go provides the model

**Research date:** 2026-06-28
**Valid until:** 2026-07-28 (controller-runtime/client semantics are stable; no fast-moving surfaces)
