# Phase 14: Budget Enforcement + Pricing - Research

**Researched:** 2026-06-11
**Domain:** Budget enforcement, pricing table, in-process reservation accounting, K8s controller predicates
**Confidence:** HIGH

---

<user_constraints>
## User Constraints (from CONTEXT.md)

### Locked Decisions

**D-01 — Pricing table:** Correct AND extend the compiled table from verified ground truth
(cached 2026-06-04, per-MTok input/output):
- `claude-fable-5` $10/$50
- `claude-opus-4-8` $5/$25
- `claude-opus-4-7` $5/$25 (FIXING existing $15/$75 entry — Opus 4.1-era pricing)
- `claude-opus-4-6` $5/$25
- `claude-sonnet-4-6` $3/$15 (already correct)
- `claude-haiku-4-5` $1/$5 (already correct)
Cache rates: cache-read = 0.1× input, cache-write = 1.25× input (5-min TTL).
Keep conservative-default fallback for unknown IDs.

**D-02 — Helm-values pricing overrides:** `pricing.overrides` chart value (map of
model-ID → {inputCentsPerMTok, outputCentsPerMTok, …}) merged OVER compiled table at
controller startup. Additive chart change; document in values.yaml.

**D-03 — Pricing-drift process:** (a) Weekly GitHub Action fetches
platform.claude.com/docs/en/pricing.md, diffs against compiled table, opens/updates a
deduped labeled GitHub issue on drift. (b) Release-checklist line. Fetch-diff script in
`hack/`.

**D-04 — BudgetBlocked condition:** `BudgetBlocked` condition on
`Project.Status.Conditions` (mirroring Phase 13's BillingHalt shape). Set the moment
the cap first blocks any dispatch. Existing `Project.Status.Phase=BudgetExceeded`
machinery STAYS. Regression test reproduces run-1 silence: cap trips →
Project carries BudgetBlocked.

**D-05 — Reservation at dispatch:** Pre-charge each session's ESTIMATED cost at Job
creation; dispatch blocks when `spent + reserved ≥ cap`; reservation settles to actual
cost on completion (releases on terminal failure). Reservations are in-process +
rederivable on restart from in-flight Jobs (never persisted aggregates in CRD status,
per PERSIST-02 / `make verify-no-aggregates`).

### Claude's Discretion
- Root-cause of run-1's silent BudgetExceeded path
- Per-level reservation estimate source (historical average? helm-configured estimate? cap-derived ceiling?)
- Reservation bookkeeping placement
- Whether `tide resume` interacts with BudgetBlocked

### Deferred Ideas (OUT OF SCOPE)
- Provider-key/org credit balance on dashboard (COST-02)
- Cache-aware cost optimization strategy (COST-01)
- Per-namespace/per-level budget sub-caps
</user_constraints>

---

<phase_requirements>
## Phase Requirements

| ID | Description | Research Support |
|----|-------------|------------------|
| BUDGET-01 | Pricing table resolves current model IDs (claude-opus-4-8, claude-fable-5, …) without falling to conservative default | D-01: correct/extend priceTable in pricing.go; D-02: Helm override plumbing |
| BUDGET-02 | Budget-cap enforcement surfaces `BudgetBlocked` on Project status, visible to kubectl and dashboard | Root-cause: silent-path analysis (see §Architecture Patterns); D-04: new condition constant + project_controller change + regression test |
| BUDGET-03 | In-flight overshoot past the budget cap is bounded (run-1 overshot ~$40) | D-05: reservation store in internal/budget/; new `ReservationStore` type + dispatch-gate extension + restart rederivation |
</phase_requirements>

---

## Summary

Phase 14 fixes three distinct but related budget trustworthiness gaps found in dogfood run 1. All three changes are surgical and build on established patterns from Phase 13's BillingHalt work.

**BUDGET-01 (pricing table)** is the simplest: three wrong/missing entries in the `priceTable` map in `internal/subagent/anthropic/pricing.go`. The same file needs a provider-agnostic override mechanism — a map passed at construction time that is merged over the compiled table — so operators can correct price drift without a TIDE release (D-02). The override map flows in from Helm via the same manager-flag pattern used for `--rate-limit-default-rpm`.

**BUDGET-02 (BudgetBlocked condition)** requires root-causing why run 1 saw neither `Project.Status.Phase=BudgetExceeded` nor any Project-level signal when the cap halted task dispatch. The root cause is fully observable by code inspection (see §Architecture Patterns): `handleBudgetGate` in the ProjectReconciler is only called from `reconcileProjectPhase2`, but `reconcileProjectPhase2`'s budget check only runs when the Project transitions — it is NOT re-triggered when `RollUpUsage` patches `Status.Budget.CostSpentCents` on the Project, because Status subresource patches do not increment `metadata.generation` and the ProjectReconciler's watch filter is `GenerationChangedPredicate || AnnotationChangedPredicate`. The fix has two components: (a) move the BudgetBlocked condition set-point to the TaskReconciler (which already reads the project on every dispatch), and (b) add a Project-level `BudgetBlocked` condition constant mirroring BillingHalt.

**BUDGET-03 (reservation overshoot)** requires a new in-process `ReservationStore` — a `sync.Map` keyed on Task UID → estimated cents — in `internal/budget/`. The dispatch gate checks `IsCapExceeded(spent + reserved)` before creating a Job and calls `Reserve(taskUID, estimatedCents)`. Task completion calls `Settle(taskUID, actualCents)`, terminal failure calls `Release(taskUID)`. On manager restart, the store is rederived from in-flight Jobs (same pattern as `PreCharge` in `precharge.go`).

**Primary recommendation:** Root-cause is in the controller watch predicate gap; fix by moving budget-block detection into the TaskReconciler's existing dispatch gate (step 4) AND adding a `checkBudgetGate` helper that mirrors `checkBillingHalt`'s pattern — called at each of the five dispatch-entry sites.

---

## Architectural Responsibility Map

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|-------------|----------------|-----------|
| Pricing table (compiled) | Provider tier (`internal/subagent/anthropic/`) | — | Provider firewall: Anthropic-specific rates stay behind the Subagent interface |
| Pricing override injection | Controller/config tier (`cmd/manager/main.go` + Helm) | Provider tier (consumes) | Overrides are provider-agnostic config flowing in; anthropic package merges them |
| Budget tally accumulation | TaskReconciler (calls `budget.RollUpUsage`) | Project.Status.Budget | Task completion is the only point where actual spend is known |
| Budget cap detection (gate) | TaskReconciler (existing step 4 gate) | ProjectReconciler (re-enqueue trigger) | TaskReconciler reads project on every dispatch; already has the context |
| BudgetBlocked condition on Project | TaskReconciler (first to observe cap breach via `IsCapExceeded`) | — | Must set it at the detection point, not in a separate reconciler that's not triggered |
| Reservation accounting | `internal/budget/ReservationStore` (in-process) | On-restart rederivation from in-flight Jobs | Same pattern as rate-limiter bucket store — never persisted |
| Pricing drift check (CI) | GitHub Actions workflow | `hack/` script | Scheduled automation; human reviews the issue before any table change |

---

## Root Cause Analysis: Silent BudgetExceeded Path (BUDGET-02)

**[VERIFIED: code inspection]** The silent path is a watch-predicate gap, not a logic bug in `handleBudgetGate`.

### Code path trace

1. `RollUpUsage` (called by TaskReconciler on task completion, line 857 of `task_controller.go`) patches `project.Status.Budget.CostSpentCents` via `c.Status().Patch(...)` on the Project object.

2. The ProjectReconciler's `SetupWithManager` wires:
   ```go
   For(&tideprojectv1alpha1.Project{},
       builder.WithPredicates(predicate.Or(
           predicate.GenerationChangedPredicate{},
           predicate.AnnotationChangedPredicate{},
       )),
   ).
   Owns(&batchv1.Job{}).
   Owns(&tideprojectv1alpha1.Milestone{}).
   ```
   A Status subresource patch (`c.Status().Patch(...)`) does NOT increment `metadata.generation`. The `GenerationChangedPredicate` is NOT triggered. The ProjectReconciler is therefore NOT re-enqueued after `RollUpUsage` increments `CostSpentCents`.

3. `handleBudgetGate` is ONLY called from `reconcileProjectPhase2`. The Project is only re-reconciled when its Spec changes (generation bump) or annotations change, or when an owned Job/Milestone changes. None of those events are triggered by a budget tally update.

4. **Consequence:** After run-1's $100 cap was crossed by accumulated task completions, the Project was never re-reconciled by the ProjectReconciler to call `handleBudgetGate` and set `Status.Phase=BudgetExceeded`. The TaskReconciler step 4 gate (line 348) checks `project.Status.Phase == "BudgetExceeded"` — but that phase was never set, so the gate never fired. New tasks continued dispatching until the wave was exhausted (~$40 overshoot).

### Fix shape

**Option A (correct approach):** Move cap detection into the TaskReconciler's step 4 gate, calling `budget.IsCapExceeded(project)` directly (the project is already fetched), and setting `BudgetBlocked` on the Project as a condition (not just on the Task). This is the mirror of how `setBillingHaltIfNeeded` stamps BillingHalt from the TaskReconciler.

**Option B (fragile):** Add a Status-change predicate to the ProjectReconciler. This would work but is noisy — every status patch on the Project (from any reconciler) would re-enqueue it.

**Decision:** Use Option A. It has precedent in Phase 13's BillingHalt pattern and is the correct architectural assignment (cap detection belongs at the dispatch gate, not in a separate reconcile pass).

### New condition: `ConditionBudgetBlocked`

The existing `ConditionBudgetExceeded = "BudgetExceeded"` is a Project condition. The CONTEXT.md D-04 decision introduces a new `BudgetBlocked` condition (distinct name from `BudgetExceeded`). Inspecting the codebase:
- `task_controller.go:351` already sets `Type: "BudgetBlocked"` as a string literal on Tasks — but this has no corresponding constant in `api/v1alpha1/shared_types.go`.
- The Project only uses `ConditionBudgetExceeded = "BudgetExceeded"` today.

The D-04 decision says "BudgetBlocked condition on Project.Status.Conditions" — this is the **new** condition on the Project (not on Tasks). The constant `ConditionBudgetBlocked = "BudgetBlocked"` needs to be added to `shared_types.go`.

---

## Architecture Patterns

### System Architecture Diagram

```
Task Completion (EnvelopeOut)
      │
      ▼
TaskReconciler.handleJobCompletion()
      │
      ├─► budget.RollUpUsage(project, usage)  ── patches Project.Status.Budget.CostSpentCents
      │                                           (Status subresource → no generation bump)
      │
      └─► [PHASE 14 NEW] setBudgetBlockedIfNeeded(project)
               │
               ├─ budget.IsCapExceeded(project)?
               │       YES ──► stamp BudgetBlocked=True on Project.Status.Conditions
               │               (mirrors setBillingHaltIfNeeded pattern)
               │
               └─ NO: no-op

Next Task dispatch (gateChecks step 4):
      │
      ├─► [PHASE 14 CHANGED] check checkBudgetBlocked(project) (condition-based, like checkBillingHalt)
      │       BudgetBlocked=True ──► park task, 30s requeue, no Job
      │
      └─► [PHASE 14 NEW] budget.ReservationStore.Check(project, estimatedCents)
               spent + reserved >= cap ──► park task, no Job
               else ──► Reserve(taskUID, estimatedCents) ──► create Job

Task Completion (terminal):
      ├─ Success: Reserve.Settle(taskUID, actualCents)
      └─ Failure: Reserve.Release(taskUID)

Manager Restart:
      └─► budget.RederiveReservations(inFlightJobs) 
               reads tideproject.k8s/estimated-cost label on Job
               populates ReservationStore
```

### Recommended Project Structure

```
internal/subagent/anthropic/
├── pricing.go              # extend priceTable + add PricingOverrides merge
internal/budget/
├── reservation.go          # new: ReservationStore (sync.Map, task-uid→cents)
├── reservation_test.go     # new: unit tests for reserve/settle/release/rederive
├── cap.go                  # extend: IsCapExceededWithReservations(project, store)
internal/controller/
├── project_controller.go   # extend: setBudgetBlockedIfNeeded (mirrors billing_halt.go)
├── budget_blocked.go       # new: checkBudgetBlocked + setBudgetBlockedIfNeeded
├── budget_blocked_test.go  # new: unit tests
├── budget_blocked_regression_test.go  # new: envtest regression (run-1 scenario)
api/v1alpha1/
├── shared_types.go         # add: ConditionBudgetBlocked, ReasonBudgetCapReached
charts/tide/
├── values.yaml             # add: pricing.overrides map
hack/
├── check-pricing-drift.sh  # new: fetch+diff script
.github/workflows/
├── pricing-drift.yaml      # new: weekly scheduled workflow
```

### Pattern 1: In-Process Reservation Store (mirrors rate-limiter bucket store)

```go
// Source: mirrors internal/budget/bucket.go Store pattern
type ReservationStore struct {
    m sync.Map // taskUID (string) → int64 (estimated cents)
}

func (s *ReservationStore) Reserve(taskUID string, estimatedCents int64) {
    s.m.Store(taskUID, estimatedCents)
}

func (s *ReservationStore) Settle(taskUID string) {
    s.m.Delete(taskUID)
}

func (s *ReservationStore) Release(taskUID string) {
    s.m.Delete(taskUID)
}

func (s *ReservationStore) TotalReserved() int64 {
    var total int64
    s.m.Range(func(_, v any) bool {
        total += v.(int64) //nolint:forcetypeassert
        return true
    })
    return total
}
```

The cap check at dispatch time becomes:
```go
// Source: architectural decision D-05, mirrors budget.IsCapExceeded
if project.Spec.Budget.AbsoluteCapCents > 0 {
    committed := project.Status.Budget.CostSpentCents + reservationStore.TotalReserved()
    if committed >= project.Spec.Budget.AbsoluteCapCents {
        // stamp BudgetBlocked, park, 30s requeue
        return taskGateResult{shouldHalt: true, result: ctrl.Result{RequeueAfter: 30 * time.Second}}, nil
    }
}
```

### Pattern 2: Reservation Rederivation on Restart

On manager startup, scan in-flight Jobs for a `tideproject.k8s/estimated-cost` label
(stamped at Job creation time alongside the existing `tideproject.k8s/provider-secret-uid`
label). This is the same pattern as `PreCharge` in `precharge.go`.

The estimated-cost label must be added to the Job at dispatch time by the TaskReconciler.
Integer cents value; omit if zero.

### Pattern 3: Pricing Overrides Merge (provider-firewalled)

```go
// Source: internal/subagent/anthropic/pricing.go + internal/budget/bucket.go pattern
// In Anthropic struct Options:
type Options struct {
    ClaudeBinary  string
    WorkspaceRoot string
    // PricingOverrides is a provider-agnostic map (model-ID → modelPrice)
    // merged OVER priceTable at New() time. Keys not in priceTable are added;
    // keys in priceTable are overridden.
    PricingOverrides map[string]modelPrice
}

func New(opts Options) *Anthropic {
    // merge overrides into a local copy of priceTable
    effective := maps.Clone(priceTable)
    for k, v := range opts.PricingOverrides {
        effective[k] = v
    }
    // store effective table on struct (not mutate the package-level var)
}
```

The Helm `pricing.overrides` map flows via a new manager flag `--pricing-override-json`
(JSON-encoded map[string]PriceEntry) or via a config YAML path — the existing `--config`
flag and config YAML pattern in `cmd/manager/main.go` is the cleanest hook.

### Pattern 4: setBudgetBlockedIfNeeded (mirrors billing_halt.go)

```go
// Source: mirrors internal/controller/billing_halt.go setBillingHaltIfNeeded
func setBudgetBlockedIfNeeded(ctx context.Context, c client.Client, project *tidev1alpha1.Project) error {
    if project == nil {
        return nil
    }
    if !budget.IsCapExceeded(project) {
        return nil
    }
    // Check if already set (idempotent).
    existing := meta.FindStatusCondition(project.Status.Conditions, tidev1alpha1.ConditionBudgetBlocked)
    if existing != nil && existing.Status == metav1.ConditionTrue {
        return nil
    }
    patch := client.MergeFrom(project.DeepCopy())
    meta.SetStatusCondition(&project.Status.Conditions, metav1.Condition{
        Type:   tidev1alpha1.ConditionBudgetBlocked,
        Status: metav1.ConditionTrue,
        Reason: tidev1alpha1.ReasonBudgetCapReached,
        Message: fmt.Sprintf("Cost spent %d cents (+ %d reserved) exceeds cap %d cents",
            project.Status.Budget.CostSpentCents, reservedCents, project.Spec.Budget.AbsoluteCapCents),
        LastTransitionTime: metav1.Now(),
    })
    return c.Status().Patch(ctx, project, patch)
}
```

### Anti-Patterns to Avoid

- **Adding `ReservedCents` to `Project.Status.Budget`:** Violates PERSIST-02. `make verify-no-aggregates` would not catch this specific field (the grep pattern is `Schedule|Waves\[\]|IndegreeMap|CachedDag|DerivedDag`) but the no-aggregates doctrine explicitly says the indegree-map pattern applies equally to reservation state. Keep reservations in-process.
- **Using a new Status-change predicate on ProjectReconciler:** Noisy; every status patch re-enqueues. The fix belongs at the TaskReconciler dispatch gate.
- **Mutating the package-level `priceTable`:** Not safe for concurrent use without a mutex. Clone it per `Anthropic` instance in `New()`.
- **Adding `tideproject.k8s/bypass-budget` clearing to `tide resume`:** The bypass annotation path and cap raise via Spec edit already exist. `tide resume` clears `BillingHalt` (provider billing recovery) — it must NOT conflate budget cap policy with billing auth. The CONTEXT explicitly says "don't invent a second unlock path without reason."

---

## Standard Stack

Phase 14 adds no new external dependencies. All work is in existing packages.

### Core Changes (no new packages)

| Package | Current Version | Change |
|---------|-----------------|--------|
| `internal/subagent/anthropic/pricing.go` | — | extend priceTable; add override merge |
| `internal/budget/` | — | add `reservation.go` |
| `internal/controller/` | — | add `budget_blocked.go`; extend task dispatch gate |
| `api/v1alpha1/shared_types.go` | — | add `ConditionBudgetBlocked`, `ReasonBudgetCapReached` |
| `charts/tide/values.yaml` | — | add `pricing.overrides:` stanza |
| `.github/workflows/` | — | add `pricing-drift.yaml` |
| `hack/` | — | add `check-pricing-drift.sh` |

**No new Go module dependencies.** The drift-check script uses `curl` + `diff` (shell) — no new tools required.

---

## Package Legitimacy Audit

No new external packages are introduced. Section N/A.

---

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| Condition idempotency | Custom condition-set logic | `meta.SetStatusCondition` (already used everywhere) | Already handles idempotent condition updates by Type |
| Concurrent map for reservations | `map[string]int64` + mutex | `sync.Map` (used in `bucket.go`) | Established pattern; TaskReconciler and manager shutdown concurrent |
| Pricing drift detection | Custom HTTP client + parser | Shell script with `curl` + `diff` | GitHub Action + shell is the project's CI standard; a Go binary is overkill |
| Override map merging | Custom deep-merge | `maps.Clone` + loop merge | Flat map (model-ID → struct); no nesting |

---

## Reservation Estimate Source (Claude's Discretion)

**Recommendation: Use the CONFIGURED PER-LEVEL model price × a fixed token budget.**

Three options evaluated:

**Option A — Per-level historical average (from completed tasks):** Requires reading historical Task CRDs; adds query complexity; historical data may be from a different codebase.

**Option B — Helm-configured per-level estimate (explicit cents per dispatch):** Operator sets `budget.reservePerDispatchCents` in values.yaml. Simple, predictable, operator-tunable. Downside: operator must know typical session cost.

**Option C — Model price × configured token ceiling (recommended):** Use the model price from `priceTable` × `Spec.Budget.EstimatedTokensPerTask` (a new Spec field, defaulting to a helm-injected value like 200,000 tokens). This is self-calibrating: if the operator changes the model, the estimate adjusts automatically. The default of 200,000 tokens at claude-haiku-4-5 rates = $0.22 per session — close to the run-1 session cost of ~$0.64 at sonnet rates. At sonnet-4-6 rates: $0.60, close to actual.

**Rationale for Option C:** The estimate does NOT need to be accurate for correctness — it just bounds overshoot. A slight over-estimate prevents dispatch of sessions that would exceed the cap; a slight under-estimate allows some overshoot (bounded to at most 1 session's error, not a full wave). Option C gives a sensible default without requiring operator tuning. The Spec field can default to 0 (= use helm default), with a helm default of 200,000.

**Restart rederivation:** On restart, scan in-flight Jobs for `tideproject.k8s/estimated-cost` label (stamped at Job creation). If missing (pre-Phase-14 Jobs), treat as 0 reserved (conservative: may dispatch more than cap strictly allows, but no worse than pre-Phase-14 behavior).

---

## Common Pitfalls

### Pitfall 1: Status Patch Does Not Re-Enqueue (the run-1 root cause)
**What goes wrong:** After `RollUpUsage` patches `CostSpentCents`, the ProjectReconciler is not re-enqueued because Status subresource patches don't bump `metadata.generation`.
**Why it happens:** `GenerationChangedPredicate` only fires on Spec/metadata changes, not Status changes.
**How to avoid:** Cap detection must live in the TaskReconciler's dispatch gate (which runs on every reconcile of a Task). Do not rely on the ProjectReconciler to poll for cap breach.
**Warning signs:** If a test sets `CostSpentCents > Cap` and the Project doesn't show `BudgetBlocked` within one task-reconcile cycle, the gate is in the wrong place.

### Pitfall 2: Mutating the Package-Level priceTable
**What goes wrong:** Two concurrent `Anthropic` instances (goroutines from the manager's controller pool) both modify `priceTable` during construction, creating a data race.
**Why it happens:** `priceTable` is a package-level `var`; Go's race detector catches map mutations from concurrent goroutines.
**How to avoid:** `maps.Clone(priceTable)` in `New()`, then merge overrides into the clone. Store the effective table on the `Anthropic` struct, never write to the package-level var after init.
**Warning signs:** `go test -race` reports a concurrent map write.

### Pitfall 3: Reservation Store Not Rederived on Restart
**What goes wrong:** After manager restart, the reservation store is empty. In-flight Jobs are consuming budget capacity but no reservation is held. New dispatches are allowed even if `spent + in-flight` would exceed the cap.
**Why it happens:** Reservations are in-process only; they are not persisted in CRD status.
**How to avoid:** On manager startup (before the controller starts reconciling), call `RederiveReservations` (analogous to `PreCharge`) to scan in-flight Jobs for the `tideproject.k8s/estimated-cost` label and pre-populate the store.
**Warning signs:** A restart during a large wave allows overshoot equal to the total estimated cost of in-flight sessions.

### Pitfall 4: BudgetBlocked vs BillingHalt Composability
**What goes wrong:** The two halts block for different reasons. If `checkBudgetBlocked` is added to the dispatch gate, it must be SEPARATE from `checkBillingHalt` — both may be true simultaneously (operator is out of budget AND Anthropic rejected the credit card).
**Why it happens:** Phase 13 added BillingHalt as a third dispatch-entry hold. Phase 14's BudgetBlocked is a fourth hold. They are NOT mutually exclusive.
**How to avoid:** Add `checkBudgetBlocked` as a separate hold AFTER `checkBillingHalt` in the dispatch gate sequence. Both return `shouldHalt=true, RequeueAfter=30s`.
**Warning signs:** Tests that clear one condition but not the other should still show the remaining hold active.

### Pitfall 5: Reservation Stampede on Manager Restart
**What goes wrong:** `RederiveReservations` over-estimates: it counts all active Jobs, but some may have been dispatched before Phase 14 (no estimated-cost label). This could cause the cap check to block dispatch when budget has not actually been exceeded.
**Why it happens:** `tideproject.k8s/estimated-cost` label absent on pre-Phase-14 Jobs.
**How to avoid:** If the label is absent, treat the reservation as 0 (not the model-default estimate). Document this in the restart logic. The risk is under-reservation on restart, not over-reservation.
**Warning signs:** Dispatch is blocked immediately after upgrade to Phase-14 binary despite budget headroom.

### Pitfall 6: conservativeTier Points to Wrong Entry After Table Update
**What goes wrong:** `conservativeTier` is initialized as `priceTable["claude-opus-4-7"]` at package init. After D-01 corrects `claude-opus-4-7` from $15/$75 to $5/$25, the conservative tier will be $5/$25, not the most-expensive entry.
**Why it happens:** `conservativeTier` references a specific model ID that was previously the most expensive entry.
**How to avoid:** After adding `claude-fable-5` ($10/$50) and `claude-opus-4-8` ($5/$25), reassign `conservativeTier` to the model with the highest output rate. `claude-fable-5` at $50/MTok output is the new most-expensive entry.
**Warning signs:** An unknown model is budget-tracked at $5/$25 (Opus 4.7 rate) instead of $10/$50 (Fable 5 rate).

---

## Code Examples

### Setting BudgetBlocked Condition (mirrors billing_halt.go)

```go
// Source: mirrors internal/controller/billing_halt.go setBillingHaltIfNeeded
// New file: internal/controller/budget_blocked.go

// checkBudgetBlocked returns true if the Project has a BudgetBlocked=True condition.
// Nil-safe; returns false for nil project.
func checkBudgetBlocked(project *tideprojectv1alpha1.Project) bool {
    if project == nil {
        return false
    }
    for _, c := range project.Status.Conditions {
        if c.Type == tideprojectv1alpha1.ConditionBudgetBlocked &&
            c.Status == metav1.ConditionTrue {
            return true
        }
    }
    return false
}
```

### Corrected priceTable Entries

```go
// Source: D-01 ground truth (verified 2026-06-04 via claude-api skill reference)
// internal/subagent/anthropic/pricing.go

"claude-fable-5": {
    inputCentsPerMTok:      1000, // $10/MTok input
    outputCentsPerMTok:     5000, // $50/MTok output
    cacheReadCentsPerMTok:  100,  // 0.10× input
    cacheWriteCentsPerMTok: 1250, // 1.25× input
},
"claude-opus-4-8": {
    inputCentsPerMTok:      500,  // $5/MTok input
    outputCentsPerMTok:     2500, // $25/MTok output
    cacheReadCentsPerMTok:  50,
    cacheWriteCentsPerMTok: 625,
},
// CORRECTED from $15/$75 (Opus 4.1-era) to $5/$25:
"claude-opus-4-7": {
    inputCentsPerMTok:      500,
    outputCentsPerMTok:     2500,
    cacheReadCentsPerMTok:  50,
    cacheWriteCentsPerMTok: 625,
},
"claude-opus-4-6": {
    inputCentsPerMTok:      500,
    outputCentsPerMTok:     2500,
    cacheReadCentsPerMTok:  50,
    cacheWriteCentsPerMTok: 625,
},
// conservativeTier reassignment:
var conservativeTier = priceTable["claude-fable-5"] // now the most expensive
```

### Dispatch gate integration (task_controller.go gateChecks)

```go
// Source: existing gateChecks in task_controller.go, step 4 replacement
// Step 4: Budget gate — check BudgetBlocked condition AND reservation headroom.
if checkBudgetBlocked(project) && !budget.IsBypassed(project, time.Now()) {
    // stamp per-task BudgetBlocked condition (existing behavior preserved)
    patch := client.MergeFrom(task.DeepCopy())
    meta.SetStatusCondition(&task.Status.Conditions, metav1.Condition{
        Type:               tideprojectv1alpha1.ConditionBudgetBlocked,
        Status:             metav1.ConditionTrue,
        Reason:             tideprojectv1alpha1.ConditionBudgetBlocked, // constant, not "BudgetExceeded"
        Message:            "Project budget cap exceeded; task dispatch halted",
        LastTransitionTime: metav1.Now(),
    })
    if err := r.Status().Patch(ctx, task, patch); err != nil {
        return taskGateResult{}, err
    }
    return taskGateResult{shouldHalt: true, result: ctrl.Result{RequeueAfter: 30 * time.Second}}, nil
}
// Step 4b: Reservation headroom check (D-05 pre-charge gate).
estimatedCents := r.estimateTaskCost(project, task)
if !r.ReservationStore.HasHeadroom(project, estimatedCents) {
    return taskGateResult{shouldHalt: true, result: ctrl.Result{RequeueAfter: 30 * time.Second}}, nil
}
```

---

## State of the Art

| Old Approach | Current Approach | When Changed | Impact |
|--------------|------------------|--------------|--------|
| TaskReconciler checks `project.Status.Phase == "BudgetExceeded"` | TaskReconciler calls `checkBudgetBlocked(project)` (condition-based) | Phase 14 | Condition survives Phase transitions; no longer brittle to Phase value |
| `priceTable` has 3 entries (haiku, sonnet, opus-4-7 — wrong price) | priceTable has 6 entries (haiku, sonnet, opus-4-6, -4-7, -4-8, fable-5) + override merge | Phase 14 | No more `pricing: unknown model` log lines; no Helm release required for price drift |
| `conservativeTier = priceTable["claude-opus-4-7"]` at $5/$25 (post-fix) | `conservativeTier = priceTable["claude-fable-5"]` at $10/$50 | Phase 14 | Conservative fallback is actually the most expensive known model |
| No reservation: spend check is post-hoc (after all wave Jobs dispatched) | Pre-charge at dispatch; check `spent + reserved >= cap` | Phase 14 | Overshoot bounded to estimate error per session, not full-wave cost |

**Deprecated:**
- `project.Status.Phase == "BudgetExceeded"` as the dispatch gate condition: fragile (Phase can be changed by other paths). Replace with the BudgetBlocked condition check.

---

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|-------|---------|---------------|
| A1 | `claude-fable-5` is the correct model ID string as it appears in `in.Provider.Model` at dispatch time | Standard Stack / priceTable | Wrong key → table miss → conservative fallback; regression test would catch this if tested with real model ID strings |
| A2 | The `--config` YAML path is the correct mechanism for injecting `pricing.overrides` into the controller (vs. a dedicated JSON flag) | Architecture Patterns (pricing override) | May need a dedicated CLI flag if config YAML is not used for dynamic overrides; check how existing `--config` is consumed in `cmd/manager/main.go` before implementing |
| A3 | `maps.Clone` is available in the Go version used by this project (Go 1.21+) | Code Examples | Go 1.26 per CLAUDE.md — confirmed available [ASSUMED: training knowledge] |

---

## Open Questions

1. **Pricing override injection mechanism: `--config` YAML vs. dedicated flag**
   - What we know: `cmd/manager/main.go` parses `--config /etc/tide/config.yaml` but the current usage was not fully inspected in this session.
   - What's unclear: Does the config YAML have a well-defined schema already, or is `pricing.overrides` a new top-level key? A dedicated `--pricing-overrides-json` flag might be simpler.
   - Recommendation: Planner reads `cmd/manager/main.go` and the config.yaml schema before deciding. Either approach is valid; consistency with existing patterns matters.

2. **Five dispatch sites vs. TaskReconciler only for BudgetBlocked**
   - What we know: Phase 13 added BillingHalt checks at all FIVE dispatch sites (milestone, phase, plan, project, task reconcilers). The CONTEXT says BudgetBlocked mirrors BillingHalt shape.
   - What's unclear: Should BudgetBlocked also be checked at all five sites, or only the task reconciler (where `RollUpUsage` runs)?
   - Recommendation: Mirror BillingHalt exactly — add `checkBudgetBlocked` at all five sites. Planner-level dispatch also burns budget via planner Jobs; blocking at all five sites prevents any new spending once the cap is hit.

---

## Environment Availability

Phase 14 is code-only changes (no new external services). Skipping this section — all work is in existing Go packages.

---

## Validation Architecture

### Test Framework

| Property | Value |
|----------|-------|
| Framework | Go testing + Ginkgo v2 + Gomega (envtest) |
| Config file | `internal/controller/suite_test.go` (Ginkgo bootstrap) |
| Quick run command | `go test ./internal/budget/... ./internal/subagent/anthropic/... -count=1` |
| Full suite command | `make test-int-fast` |

### Phase Requirements → Test Map

| Req ID | Behavior | Test Type | Automated Command | File Exists? |
|--------|----------|-----------|-------------------|-------------|
| BUDGET-01 | `estimatedCostCents("claude-fable-5", usage)` returns correct cost (not 0 or conservative fallback) | unit | `go test ./internal/subagent/anthropic/... -run TestEstimatedCostCents -count=1` | ❌ Wave 0 (extend `pricing_test.go`) |
| BUDGET-01 | `estimatedCostCents("claude-opus-4-7", ...)` returns $5/$25 not $15/$75 | unit | same | ❌ Wave 0 |
| BUDGET-01 | `conservativeTier` is fable-5 rate ($10/$50), not old opus-4-7 ($5/$25 post-fix) | unit | same | ❌ Wave 0 |
| BUDGET-02 | Cap trips → `BudgetBlocked=True` on Project.Status.Conditions within one task-reconcile cycle | envtest | `go test ./test/integration/envtest/... --ginkgo.label-filter='phase14,budget-blocked' -timeout=10m` | ❌ Wave 0 |
| BUDGET-02 | Run-1 regression: cap $100, task completion increments CostSpentCents above cap → BudgetBlocked visible on Project | envtest | same | ❌ Wave 0 |
| BUDGET-02 | BudgetBlocked gate parks task (30s requeue, no Job created, no Failed status) | envtest | same | ❌ Wave 0 |
| BUDGET-03 | `ReservationStore.Reserve` + `TotalReserved` + `Settle` + `Release` work correctly | unit | `go test ./internal/budget/... -run TestReservation -count=1` | ❌ Wave 0 |
| BUDGET-03 | `HasHeadroom` returns false when `spent + reserved >= cap` | unit | same | ❌ Wave 0 |
| BUDGET-03 | Restart rederivation: Jobs with `tideproject.k8s/estimated-cost` label → store pre-populated | unit | same | ❌ Wave 0 |

### Sampling Rate
- **Per task commit:** `go test ./internal/budget/... ./internal/subagent/anthropic/... -count=1`
- **Per wave merge:** `make test-int-fast`
- **Phase gate:** `make test` (unit) + `make test-int-fast` (envtest) green before `/gsd:verify-work`

### Wave 0 Gaps
- [ ] Extend `internal/subagent/anthropic/pricing_test.go` — add cases for new model IDs + corrected opus-4-7 price
- [ ] `internal/budget/reservation_test.go` — unit tests for `ReservationStore`
- [ ] `internal/controller/budget_blocked_regression_test.go` — envtest regression (run-1 scenario: cap trips → BudgetBlocked on Project)
- [ ] `internal/controller/budget_blocked_test.go` — unit tests for `checkBudgetBlocked`, `setBudgetBlockedIfNeeded`

---

## Security Domain

### Applicable ASVS Categories

| ASVS Category | Applies | Standard Control |
|---------------|---------|-----------------|
| V2 Authentication | no | — |
| V3 Session Management | no | — |
| V4 Access Control | yes (budget bypass) | existing bypass annotation pattern; no new access control paths |
| V5 Input Validation | yes | pricing override map must validate `inputCentsPerMTok > 0`; reject negative or zero prices |
| V6 Cryptography | no | — |

### Known Threat Patterns

| Pattern | STRIDE | Standard Mitigation |
|---------|--------|---------------------|
| Operator sets `pricing.overrides` with 0 prices to make sessions appear free | Tampering | Validate at controller startup: reject overrides with zero or negative cent values |
| Race condition: two TaskReconciler goroutines both check reservation headroom, both see headroom, both dispatch → overshoot by one session | Tampering | `sync.Map` `LoadOrStore` for atomic reserve; check-then-act is inherently racy — accept bounded overshoot (one session's estimate), document as by-design |

---

## Sources

### Primary (HIGH confidence)
- `internal/subagent/anthropic/pricing.go` — inspected directly; confirms 3-entry table, conservativeTier, estimatedCostCents call site
- `internal/budget/bucket.go`, `cap.go`, `tally.go`, `precharge.go` — inspected directly; confirms Store pattern, IsCapExceeded, RollUpUsage, PreCharge restart pattern
- `internal/controller/task_controller.go` lines 330-365 — inspected directly; confirms existing gate structure, step 4 BudgetExceeded check, BudgetBlocked per-Task condition
- `internal/controller/project_controller.go` lines 258-314, 1157-1233 — inspected directly; confirms handleBudgetGate, reconcileProjectPhase2, SetupWithManager watch predicates
- `internal/controller/billing_halt.go` — inspected directly; confirms setBillingHaltIfNeeded pattern for D-04 to mirror
- `api/v1alpha1/shared_types.go` — inspected directly; confirms no ConditionBudgetBlocked constant exists yet
- `cmd/tide/resume.go` — inspected directly; confirms `tide resume` does NOT touch BudgetExceeded/BudgetBlocked (no interaction needed)

### Secondary (MEDIUM confidence)
- `D-01` ground truth from CONTEXT.md (claude-api skill model table cached 2026-06-04) — canonical price source per planning session; drift-check script re-verifies

### Tertiary (LOW confidence)
None.

---

## Metadata

**Confidence breakdown:**
- Standard stack: HIGH — all work is in existing packages with established patterns
- Architecture (root cause): HIGH — verified by direct code inspection of watch predicates and reconcile flow
- Pitfalls: HIGH — derived from code inspection; Pitfall 1 is the verified root cause
- Reservation estimate source: MEDIUM — Option C is a recommendation; planner may choose Option B (simpler)

**Research date:** 2026-06-11
**Valid until:** 2026-07-11 (stable internal package; only drift risk is new Anthropic model IDs)
