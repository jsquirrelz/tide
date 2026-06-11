# Phase 14: Budget Enforcement + Pricing - Pattern Map

**Mapped:** 2026-06-11
**Files analyzed:** 11 new/modified files
**Analogs found:** 11 / 11

## File Classification

| New/Modified File | Role | Data Flow | Closest Analog | Match Quality |
|-------------------|------|-----------|----------------|---------------|
| `internal/subagent/anthropic/pricing.go` | provider-utility | transform | `internal/subagent/anthropic/pricing.go` (self — extend) | exact |
| `internal/subagent/anthropic/pricing_test.go` | test | transform | `internal/subagent/anthropic/pricing_test.go` (self — extend) | exact |
| `internal/budget/reservation.go` | store | CRUD | `internal/budget/bucket.go` | exact |
| `internal/budget/reservation_test.go` | test | CRUD | `internal/budget/precharge_test.go` + `cap_test.go` | exact |
| `internal/controller/budget_blocked.go` | controller-helper | request-response | `internal/controller/billing_halt.go` | exact |
| `internal/controller/budget_blocked_test.go` | test | request-response | `internal/controller/billing_halt_test.go` | exact |
| `internal/controller/budget_blocked_regression_test.go` | test (envtest) | event-driven | `internal/controller/billing_halt_regression_test.go` | exact |
| `internal/controller/task_controller.go` (modify) | controller | request-response | `internal/controller/task_controller.go` lines 336–365 (self — extend) | exact |
| `api/v1alpha1/shared_types.go` (modify) | model | — | `api/v1alpha1/shared_types.go` lines 216–239 (self — extend) | exact |
| `charts/tide/values.yaml` (modify) | config | — | `charts/tide/values.yaml` lines 115–121 (`rateLimits.defaults` block) | exact |
| `hack/check-pricing-drift.sh` | utility | — | `hack/scripts/verify-license.sh` | role-match |
| `.github/workflows/pricing-drift.yaml` | config (CI) | event-driven | `.github/workflows/nightly-integration.yml` (schedule + curl pattern) | role-match |

---

## Pattern Assignments

### `internal/subagent/anthropic/pricing.go` (extend existing)

**Analog:** self (existing file) + pattern from `internal/budget/bucket.go`

**Current priceTable to replace** (`internal/subagent/anthropic/pricing.go` lines 54–82):
```go
var priceTable = map[string]modelPrice{
	"claude-haiku-4-5": {
		inputCentsPerMTok:      100,
		outputCentsPerMTok:     500,
		cacheReadCentsPerMTok:  10,
		cacheWriteCentsPerMTok: 125,
	},
	"claude-sonnet-4-6": {
		inputCentsPerMTok:      300,
		outputCentsPerMTok:     1500,
		cacheReadCentsPerMTok:  30,
		cacheWriteCentsPerMTok: 375,
	},
	// WRONG — must be corrected:
	"claude-opus-4-7": {
		inputCentsPerMTok:      1500, // D-01: fix to 500
		outputCentsPerMTok:     7500, // D-01: fix to 2500
		cacheReadCentsPerMTok:  150,  // D-01: fix to 50
		cacheWriteCentsPerMTok: 1875, // D-01: fix to 625
	},
}
var conservativeTier = priceTable["claude-opus-4-7"] // must become ["claude-fable-5"]
```

**New entries to add (D-01 corrected table):**
```go
"claude-fable-5": {
    inputCentsPerMTok:      1000, // $10/MTok
    outputCentsPerMTok:     5000, // $50/MTok
    cacheReadCentsPerMTok:  100,  // 0.10× input
    cacheWriteCentsPerMTok: 1250, // 1.25× input
},
"claude-opus-4-8": {
    inputCentsPerMTok:      500,
    outputCentsPerMTok:     2500,
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
// conservativeTier must point to the new most-expensive entry:
var conservativeTier = priceTable["claude-fable-5"]
```

**Override merge pattern (D-02) — copy from `internal/budget/bucket.go` lines 53–91 sync.Map pattern + `maps.Clone`:**

The `Anthropic` struct in `subagent.go` already has an `Options` struct. Add `PricingOverrides` to it and merge in `New()`:
```go
// In Options struct (subagent.go):
PricingOverrides map[string]modelPrice // provider-agnostic override map (D-02)

// In New(opts Options):
effective := maps.Clone(priceTable)
for k, v := range opts.PricingOverrides {
    effective[k] = v
}
// Store effective on struct; pass to estimatedCostCents via a receiver field.
// NEVER mutate the package-level priceTable var (Pitfall 2 in RESEARCH.md).
```

**imports pattern** (`internal/subagent/anthropic/pricing.go` lines 36–40):
```go
import (
    "fmt"
    "os"

    pkgdispatch "github.com/jsquirrelz/tide/pkg/dispatch"
)
// D-02 extension adds: "maps"
```

---

### `internal/subagent/anthropic/pricing_test.go` (extend existing)

**Analog:** self (existing file, lines 26–121)

**Existing test structure to follow** (lines 26–35):
```go
func TestEstimatedCostCents(t *testing.T) {
    t.Run("haiku_input_only", func(t *testing.T) {
        u := pkgdispatch.Usage{InputTokens: 1_000_000}
        got := estimatedCostCents("claude-haiku-4-5", u)
        if got != 100 {
            t.Errorf("haiku input=1M: want 100 cents, got %d", got)
        }
    })
    // ...
```

**New cases to add (BUDGET-01 regression):**
- `"fable5_input_output"` — `estimatedCostCents("claude-fable-5", Usage{Input: 1M, Output: 1M})` → 6000 cents ($10 + $50)
- `"opus47_corrected"` — `estimatedCostCents("claude-opus-4-7", Usage{Output: 1M})` → 2500 cents, NOT 7500 (regression assertion for D-01 fix)
- `"opus48_present"` — new model ID in table
- `"conservative_tier_is_fable5"` — `estimatedCostCents("unknown", Usage{Output: 1M})` must equal fable5's output rate (5000), not 7500 (old opus-4-7)
- `"override_merge"` — construct an `Anthropic` with a `PricingOverrides` that replaces haiku's price; verify new price is used without mutating priceTable

---

### `internal/budget/reservation.go` (new file)

**Analog:** `internal/budget/bucket.go` (exact pattern — same sync.Map store, same in-process rederivable pattern)

**License header + package declaration** (`internal/budget/bucket.go` lines 1–22):
```go
/*
Copyright 2026 TIDE Authors.
...Apache License 2.0 boilerplate...
*/

// Package budget — see doc.go for package overview.
package budget
```

**Store struct pattern** (`internal/budget/bucket.go` lines 52–91):
```go
// Store is a sync.Map-backed in-process cache ...
type Store struct {
    m sync.Map
}

func NewStore() *Store {
    return &Store{}
}

// ForSecret — LoadOrStore pattern for concurrent-safe lazy init:
actual, _ := s.m.LoadOrStore(secretUID, lim)
return actual.(*rate.Limiter) //nolint:forcetypeassert
```

**Rederivation pattern** (`internal/budget/precharge.go` lines 54–92):
```go
func PreCharge(ctx context.Context, c client.Client, store *Store, defaults Limits, window time.Duration) error {
    var jobs batchv1.JobList
    if err := c.List(ctx, &jobs, client.HasLabels{secretUIDLabel}); err != nil {
        return err
    }
    now := time.Now()
    for _, job := range jobs.Items {
        if job.Status.Active <= 0 {
            continue // skip terminated
        }
        // ... label-based restore
    }
    return nil
}
```

**New `reservation.go` full pattern (follow bucket.go, replace sync.Map value type):**
```go
// reservedCostLabel is the K8s label key stamped on every dispatch Job at
// Job-create time. RederiveReservations uses this label to restore the
// in-process store after a manager restart.
const reservedCostLabel = "tideproject.k8s/estimated-cost"

// ReservationStore is a sync.Map-backed in-process pre-charge store.
// Keys are task UIDs (string); values are estimated cost in cents (int64).
// Never persisted in CRD status — rederivable from in-flight Job labels
// (same pattern as budget.PreCharge for rate-limiter buckets).
type ReservationStore struct {
    m sync.Map // task UID → int64 estimated cents
}

func NewReservationStore() *ReservationStore { return &ReservationStore{} }

func (s *ReservationStore) Reserve(taskUID string, estimatedCents int64) {
    s.m.Store(taskUID, estimatedCents)
}

// Settle removes the reservation on task completion (actual cost already
// rolled up by RollUpUsage — reservation is no longer needed).
func (s *ReservationStore) Settle(taskUID string) {
    s.m.Delete(taskUID)
}

// Release removes the reservation on terminal failure (actual cost = 0
// from a failed session — release the reserved headroom).
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

// HasHeadroom returns true if (spent + reserved + estimatedCents) < cap.
// Returns true when cap is 0 or negative (unlimited). Nil-safe.
func (s *ReservationStore) HasHeadroom(project *tidev1alpha1.Project, estimatedCents int64) bool {
    if project == nil || project.Spec.Budget.AbsoluteCapCents <= 0 {
        return true
    }
    committed := project.Status.Budget.CostSpentCents + s.TotalReserved()
    return committed+estimatedCents < project.Spec.Budget.AbsoluteCapCents
}

// RederiveReservations scans active Jobs with the reservedCostLabel label
// and pre-populates the store. Called once at manager startup before the
// controller starts reconciling (same pattern as budget.PreCharge).
// Jobs without the label (pre-Phase-14) are treated as 0 reserved (conservative
// restart behavior: may allow slight overshoot, no worse than pre-Phase-14).
func RederiveReservations(ctx context.Context, c client.Client, store *ReservationStore) error {
    var jobs batchv1.JobList
    if err := c.List(ctx, &jobs, client.HasLabels{reservedCostLabel}); err != nil {
        return err
    }
    for _, job := range jobs.Items {
        if job.Status.Active <= 0 {
            continue // skip terminated
        }
        rawCents := job.Labels[reservedCostLabel]
        // parse string → int64; skip if zero or malformed
        ...
        taskUID := job.Labels["tideproject.k8s/task-uid"]
        store.Reserve(taskUID, cents)
    }
    return nil
}
```

**imports for reservation.go:**
```go
import (
    "context"
    "strconv"

    batchv1 "k8s.io/api/batch/v1"
    "sigs.k8s.io/controller-runtime/pkg/client"

    tidev1alpha1 "github.com/jsquirrelz/tide/api/v1alpha1"
)
```

---

### `internal/budget/reservation_test.go` (new file)

**Analog:** `internal/budget/cap_test.go` (unit, pure-Go, no fake client) + `internal/budget/precharge_test.go` (fake client for rederivation tests)

**Package + import pattern** (`internal/budget/cap_test.go` lines 17–29):
```go
package budget

import (
    "testing"
    "time"

    metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

    tidev1alpha1 "github.com/jsquirrelz/tide/api/v1alpha1"
)
```

**Test helper pattern** (`internal/budget/precharge_test.go` lines 37–64):
```go
func newBudgetFakeClient(t *testing.T, objs ...client.Object) client.Client { ... }
func makeJobWithUID(name, secretUID string, active int32, createdAgo time.Duration) *batchv1.Job { ... }
```

**Table-driven unit test pattern** (`internal/budget/cap_test.go` lines 33–72):
```go
func TestIsCapExceeded(t *testing.T) {
    cases := []struct { name string; ... }{ {...}, ... }
    for _, tc := range cases {
        t.Run(tc.name, func(t *testing.T) { ... })
    }
}
```

**Cases to cover:**
- `TestReservationStore_ReserveAndTotal` — Reserve × 3, TotalReserved == sum
- `TestReservationStore_Settle` — Reserve, Settle, TotalReserved == 0
- `TestReservationStore_Release` — Reserve, Release, TotalReserved == 0
- `TestReservationStore_HasHeadroom_UnderCap` — spent+reserved+estimate < cap → true
- `TestReservationStore_HasHeadroom_AtCap` — spent+reserved+estimate == cap → true (at, not over)
- `TestReservationStore_HasHeadroom_OverCap` — spent+reserved+estimate > cap → false
- `TestReservationStore_HasHeadroom_ZeroCap` — cap=0 → always true
- `TestReservationStore_HasHeadroom_NilProject` → true
- `TestRederiveReservations_PopulatesFromLabel` — active Job with label → store non-empty
- `TestRederiveReservations_SkipsTerminated` — active=0 → not added
- `TestRederiveReservations_SkipsMissingLabel` — pre-Phase-14 Job (no label) → not added

---

### `internal/controller/budget_blocked.go` (new file)

**Analog:** `internal/controller/billing_halt.go` (exact mirror — same file structure, same function signatures, different condition type)

**File header pattern** (`internal/controller/billing_halt.go` lines 1–52):
```go
/*
Copyright 2026 TIDE Authors.
...Apache License 2.0...
*/

// budget_blocked.go — BudgetBlocked condition helpers for BUDGET-02 (Phase 14).
//
// D-04: When the TaskReconciler's dispatch gate observes cap breach, it calls
// setBudgetBlockedIfNeeded to stamp BudgetBlocked=True on the Project. All
// five dispatch gates call checkBudgetBlocked before dispatching; if blocked
// they park with a 30s requeue.
//
// This is the fourth dispatch-entry hold (after CheckRejected, checkParentApproval,
// checkBillingHalt). BudgetBlocked and BillingHalt are NOT mutually exclusive —
// both may be true simultaneously. Add checkBudgetBlocked AFTER checkBillingHalt
// in every dispatch gate sequence.
package controller
```

**imports pattern** (`internal/controller/billing_halt.go` lines 42–52):
```go
import (
    "context"

    "k8s.io/apimachinery/pkg/api/meta"
    metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
    "sigs.k8s.io/controller-runtime/pkg/client"

    tideprojectv1alpha1 "github.com/jsquirrelz/tide/api/v1alpha1"
    "github.com/jsquirrelz/tide/internal/budget"
)
```

**checkBudgetBlocked function** (mirror `billing_halt.go:checkBillingHalt` lines 78–89):
```go
// checkBudgetBlocked returns true if the Project has a BudgetBlocked=True condition.
// Nil-safe: a nil project returns false.
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

**setBudgetBlockedIfNeeded function** (mirror `billing_halt.go:setBillingHaltIfNeeded` lines 104–135):
```go
// setBudgetBlockedIfNeeded stamps BudgetBlocked=True on project when the cap is
// exceeded. Idempotent (exits early if already set). Nil project is a safe no-op.
// Called by the TaskReconciler after RollUpUsage confirms cap breach.
func setBudgetBlockedIfNeeded(ctx context.Context, c client.Client, project *tideprojectv1alpha1.Project, reservedCents int64) error {
    if project == nil {
        return nil
    }
    if !budget.IsCapExceeded(project) {
        return nil
    }
    // Idempotent check — avoid a spurious patch if already set.
    existing := meta.FindStatusCondition(project.Status.Conditions, tideprojectv1alpha1.ConditionBudgetBlocked)
    if existing != nil && existing.Status == metav1.ConditionTrue {
        return nil
    }
    patch := client.MergeFrom(project.DeepCopy())
    meta.SetStatusCondition(&project.Status.Conditions, metav1.Condition{
        Type:   tideprojectv1alpha1.ConditionBudgetBlocked,
        Status: metav1.ConditionTrue,
        Reason: tideprojectv1alpha1.ReasonBudgetCapReached,
        Message: fmt.Sprintf(
            "Cost spent %d cents (+ %d reserved) exceeds cap %d cents; dispatch halted project-wide",
            project.Status.Budget.CostSpentCents,
            reservedCents,
            project.Spec.Budget.AbsoluteCapCents,
        ),
        LastTransitionTime: metav1.Now(),
    })
    return c.Status().Patch(ctx, project, patch)
}
```

---

### `internal/controller/budget_blocked_test.go` (new file)

**Analog:** `internal/controller/billing_halt_test.go` (same package, same fake-client pattern)

**Package + imports pattern** (`internal/controller/billing_halt_test.go` lines 17–32):
```go
package controller

import (
    "context"
    "testing"

    "k8s.io/apimachinery/pkg/api/meta"
    metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
    "k8s.io/apimachinery/pkg/types"
    "sigs.k8s.io/controller-runtime/pkg/client/fake"

    tideprojectv1alpha1 "github.com/jsquirrelz/tide/api/v1alpha1"
)
```

**Fake client construction pattern** (`internal/controller/billing_halt_test.go` lines 124–137):
```go
s := fakeSchemeWithAll(t)
project := &tideprojectv1alpha1.Project{
    ObjectMeta: metav1.ObjectMeta{Name: "my-project", Namespace: "default"},
    Spec: tideprojectv1alpha1.ProjectSpec{TargetRepo: "https://example.com/repo.git"},
}
c := fake.NewClientBuilder().WithScheme(s).
    WithObjects(project).
    WithStatusSubresource(project).
    Build()
```

**Condition assertion pattern** (`internal/controller/billing_halt_test.go` lines 143–165):
```go
var got tideprojectv1alpha1.Project
if err := c.Get(context.Background(), types.NamespacedName{...}, &got); err != nil {
    t.Fatalf("get project: %v", err)
}
cond := meta.FindStatusCondition(got.Status.Conditions, tideprojectv1alpha1.ConditionBudgetBlocked)
if cond == nil { t.Fatal("expected BudgetBlocked condition") }
if cond.Status != metav1.ConditionTrue { t.Errorf(...) }
if cond.Reason != tideprojectv1alpha1.ReasonBudgetCapReached { t.Errorf(...) }
```

**Cases to cover (mirror billing_halt_test.go structure):**
- `TestCheckBudgetBlocked_TrueWhenConditionPresent`
- `TestCheckBudgetBlocked_FalseWhenConditionAbsent`
- `TestCheckBudgetBlocked_FalseWhenConditionFalse`
- `TestCheckBudgetBlocked_FalseForNilProject`
- `TestSetBudgetBlockedIfNeeded_SetsCondition` — cap exceeded → condition stamped
- `TestSetBudgetBlockedIfNeeded_CapNotExceeded_NoOp`
- `TestSetBudgetBlockedIfNeeded_Idempotent` — already set → no second patch
- `TestSetBudgetBlockedIfNeeded_NilProject_NoOp`

---

### `internal/controller/budget_blocked_regression_test.go` (new file)

**Analog:** `internal/controller/billing_halt_regression_test.go` (envtest, Ginkgo/Gomega)

**File header + imports pattern** (`internal/controller/billing_halt_regression_test.go` lines 1–51):
```go
// Plan 14-XX Task Y (RED) — BudgetBlocked dispatch-entry hold regression tests.
//
// BUDGET-02: run-1 regression — cap $100, wide wave; after tasks complete and
// CostSpentCents exceeds cap, Project must carry BudgetBlocked=True within
// one task-reconcile cycle (not silently absent as in run-1).
package controller

import (
    "context"
    . "github.com/onsi/ginkgo/v2"
    . "github.com/onsi/gomega"
    "k8s.io/apimachinery/pkg/api/meta"
    metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
    "k8s.io/apimachinery/pkg/types"
    "sigs.k8s.io/controller-runtime/pkg/client"
    tideprojectv1alpha1 "github.com/jsquirrelz/tide/api/v1alpha1"
)
```

**Stamp helper pattern** (`internal/controller/billing_halt_regression_test.go` lines 55–75, adapt):
```go
// stampBudgetSpend patches project.Status.Budget.CostSpentCents to simulate
// task completion rolling up spend past the cap.
func stampBudgetSpend(ctx context.Context, projectName string, spentCents int64) {
    var proj tideprojectv1alpha1.Project
    Expect(k8sClient.Get(ctx, types.NamespacedName{Name: projectName, Namespace: "default"}, &proj)).To(Succeed())
    sp := client.MergeFrom(proj.DeepCopy())
    proj.Status.Budget.CostSpentCents = spentCents
    Expect(k8sClient.Status().Patch(ctx, &proj, sp)).To(Succeed())
    Eventually(func() bool {
        var p tideprojectv1alpha1.Project
        if err := mgrClient.Get(ctx, types.NamespacedName{Name: projectName, Namespace: "default"}, &p); err != nil {
            return false
        }
        return p.Status.Budget.CostSpentCents == spentCents
    }, 5*time.Second, 50*time.Millisecond).Should(BeTrue())
}
```

**Run-1 regression spec shape** (`billing_halt_regression_test.go` lines 652–713 as structural template):
```go
var _ = Describe("BudgetBlocked run-1 regression: cap trips → BudgetBlocked on Project",
    Label("envtest", "phase14", "budget-blocked", "regression"), func() {
    // Scenario: cap=$100 ($10000 cents). After task completion rolls up
    // $10001 cents, the NEXT task dispatch reconcile must:
    //   (a) call setBudgetBlockedIfNeeded → Project carries BudgetBlocked=True
    //   (b) task is parked (no Job), 30s requeue
    //   (c) Project NOT in "Failed" phase
    ...
})
```

**Reconciler constructor pattern** (`billing_halt_regression_test.go` lines 938–998):
```go
// newBBTaskReconciler builds a TaskReconciler for BudgetBlocked specs.
// Must wire ReservationStore (new Phase 14 field on TaskReconciler).
func newBBTaskReconciler(envReader EnvReader) *TaskReconciler {
    return &TaskReconciler{
        Client:           mgrClient,
        Scheme:           k8sClient.Scheme(),
        Dispatcher:       &stubDispatcher{},
        SigningKey:        testSigningKey,
        CredproxyImage:   testCredproxyImage,
        BudgetStore:       budget.NewStore(),
        ReservationStore:  budget.NewReservationStore(), // Phase 14 addition
        EnvReader:         envReader,
        HelmProviderDefaults: ProviderDefaults{Image: testSubagentImage},
    }
}
```

---

### `internal/controller/task_controller.go` (modify — dispatch gate extension)

**Analog:** self, lines 336–365 (existing dispatch holds pattern)

**Existing hold #3 (BillingHalt) to insert BudgetBlocked after** (`task_controller.go` lines 336–344):
```go
// Phase 13 HALT-01 / D-05: third dispatch-entry hold (after CheckRejected +
// parent-approval); park, never fail; cleared by tide resume.
if checkBillingHalt(project) {
    logf.FromContext(ctx).V(1).Info("dispatch held: project billing halt",
        "task", task.Name, "project", project.Name)
    return taskGateResult{shouldHalt: true, result: ctrl.Result{RequeueAfter: 30 * time.Second}}, nil
}
```

**New hold #4 to add immediately after (mirrors hold #3 shape exactly):**
```go
// Phase 14 BUDGET-02 / D-04: fourth dispatch-entry hold — BudgetBlocked condition.
// Cap detection happens here (not in ProjectReconciler) because Status patches from
// RollUpUsage do NOT increment metadata.generation and thus do NOT re-enqueue the
// ProjectReconciler (watch-predicate gap root cause, 14-RESEARCH.md §Root Cause).
if err := setBudgetBlockedIfNeeded(ctx, r.Client, project, r.ReservationStore.TotalReserved()); err != nil {
    logf.FromContext(ctx).Error(err, "setBudgetBlockedIfNeeded failed (non-fatal)")
}
if checkBudgetBlocked(project) && !budget.IsBypassed(project, time.Now()) {
    logf.FromContext(ctx).V(1).Info("dispatch held: project budget blocked",
        "task", task.Name, "project", project.Name,
        "spent", project.Status.Budget.CostSpentCents,
        "cap", project.Spec.Budget.AbsoluteCapCents)
    return taskGateResult{shouldHalt: true, result: ctrl.Result{RequeueAfter: 30 * time.Second}}, nil
}

// Phase 14 BUDGET-03 / D-05: reservation headroom check.
estimatedCents := r.estimateTaskCost(project, task)
if !r.ReservationStore.HasHeadroom(project, estimatedCents) {
    logf.FromContext(ctx).V(1).Info("dispatch held: insufficient reservation headroom",
        "task", task.Name, "spent", project.Status.Budget.CostSpentCents,
        "reserved", r.ReservationStore.TotalReserved(),
        "estimate", estimatedCents,
        "cap", project.Spec.Budget.AbsoluteCapCents)
    return taskGateResult{shouldHalt: true, result: ctrl.Result{RequeueAfter: 30 * time.Second}}, nil
}
```

**Existing Step 4 to REPLACE** (task_controller.go lines 347–361 — the fragile `project.Status.Phase == "BudgetExceeded"` check):
```go
// BEFORE (remove this):
if project.Status.Phase == "BudgetExceeded" && !budget.IsBypassed(project, time.Now()) {
    // ... per-task condition stamp + halt
}
```
The old check is subsumed by the new `checkBudgetBlocked` gate above. The per-Task condition stamp (Type: "BudgetBlocked" on the Task) from lines 350–356 should be preserved or moved into a helper — see RESEARCH §Pitfall 4.

**Job creation site — add estimated-cost label** (wherever the Job is created for the task, add):
```go
if estimatedCents > 0 {
    labels[reservedCostLabel] = strconv.FormatInt(estimatedCents, 10)
}
r.ReservationStore.Reserve(string(task.UID), estimatedCents)
```

**Task completion (handleJobCompletion) — add settle/release:**
```go
// On success:
r.ReservationStore.Settle(string(task.UID))
// On terminal failure:
r.ReservationStore.Release(string(task.UID))
```

**The same `checkBudgetBlocked` hold must be added at the other four dispatch sites** (milestone, phase, plan, project reconcilers) following the exact same pattern used for `checkBillingHalt` in each of those files.

---

### `api/v1alpha1/shared_types.go` (modify — add constants)

**Analog:** self, lines 216–239 (the Phase 13 `ConditionBillingHalt` block)

**Pattern to follow** (lines 216–239):
```go
// Phase 13 condition + reason vocabulary — provider billing halt (HALT-01).
const (
    // ConditionBillingHalt — ...
    ConditionBillingHalt = "BillingHalt"
    // ReasonCreditBalanceTooLow — ...
    ReasonCreditBalanceTooLow = "CreditBalanceTooLow"
    // AnnotationBillingResumedAt — ...
    AnnotationBillingResumedAt = "tideproject.k8s/billing-resumed-at"
)
```

**New block to add (append after line 239):**
```go
// Phase 14 condition + reason vocabulary — operator budget cap blocked (BUDGET-02).
// A Project budget cap exhaustion blocks all new dispatch project-wide until the
// operator raises the cap or applies the bypass annotation. BudgetBlocked is set
// on the Project status by the TaskReconciler dispatch gate when IsCapExceeded
// returns true after RollUpUsage increments CostSpentCents. Distinct from
// BillingHalt (provider billing auth failure) — both may be true simultaneously.
const (
    // ConditionBudgetBlocked — operator's budget cap has been reached; new dispatch
    // is halted project-wide until the cap is raised (Spec.Budget.AbsoluteCapCents)
    // or the bypass annotation is applied. Phase 14 BUDGET-02.
    ConditionBudgetBlocked = "BudgetBlocked"

    // ReasonBudgetCapReached — the project's absolute cost cap (or rolling cap) was
    // exceeded; set by TaskReconciler dispatch gate via setBudgetBlockedIfNeeded.
    ReasonBudgetCapReached = "BudgetCapReached"
)
```

**Note on existing "BudgetBlocked" string literal in task_controller.go:351:** The existing code at line 351 already uses the string `"BudgetBlocked"` as a condition type on Tasks (without a constant). This new constant `ConditionBudgetBlocked = "BudgetBlocked"` formalizes it. Replace the string literal on line 351 with the constant.

---

### `charts/tide/values.yaml` (modify — add pricing.overrides stanza)

**Analog:** `charts/tide/values.yaml` lines 115–121 (`rateLimits.defaults` block — same pattern: new top-level key, documented, flows via flag)

**Existing pattern to follow** (lines 115–121):
```yaml
# Default rate-limit parameters for provider credential Secrets (D-D3).
# Applied when no Secret annotation or Project.Spec.Providers entry overrides them.
# Passed to the controller via --rate-limit-default-rpm and --rate-limit-default-burst flags.
rateLimits:
  defaults:
    requestsPerMinute: 60
    tokensPerMinute: 100000
    burst: 10
```

**New stanza to add (after the rateLimits block):**
```yaml
# Pricing overrides (D-02): per-model price corrections merged OVER the compiled
# priceTable at controller startup. Operators use this to correct price drift without
# a TIDE release. Keys are exact model ID strings; values are cents per million tokens.
# Passed to the controller via --pricing-overrides-json flag (JSON-encoded map).
#
# Example (all fields optional — omitted fields inherit compiled defaults):
#   pricing:
#     overrides:
#       claude-some-future-model:
#         inputCentsPerMTok: 300
#         outputCentsPerMTok: 1500
#         cacheReadCentsPerMTok: 30
#         cacheWriteCentsPerMTok: 375
pricing:
  overrides: {}
```

**Flag wiring in main.go** (mirrors `--rate-limit-default-rpm` pattern, lines 161–163):
```go
// In var declarations:
var pricingOverridesJSON string

// In flag.* block:
flag.StringVar(&pricingOverridesJSON, "pricing-overrides-json", "{}",
    "JSON-encoded map of model-ID to price overrides (pricing.overrides in values.yaml)")
```

---

### `hack/check-pricing-drift.sh` (new file)

**Analog:** `hack/scripts/verify-license.sh` (shell script in hack/, executed locally + from CI)

**Script header pattern** (from any hack/scripts/*.sh):
```bash
#!/usr/bin/env bash
# check-pricing-drift.sh — fetch Anthropic pricing docs and diff against
# the compiled priceTable in internal/subagent/anthropic/pricing.go.
# D-03: run weekly via .github/workflows/pricing-drift.yaml or locally.
# Usage: ./hack/check-pricing-drift.sh [--open-issue]
set -euo pipefail
```

**Core logic shape (D-03):**
```bash
# 1. Fetch the pricing page (retry-hardened, as seen in nightly-integration.yml):
curl -fsSL --retry 5 --retry-delay 3 --retry-all-errors --connect-timeout 30 \
    -o /tmp/anthropic-pricing.md \
    "https://platform.anthropic.com/docs/en/pricing.md" || {
    echo "ERROR: failed to fetch pricing page" >&2
    exit 1
}

# 2. Extract known model IDs and prices from the fetched page
#    (grep + awk; no external tools beyond standard POSIX utils)

# 3. Compare against compiled table entries (grep from pricing.go)
COMPILED=$(grep -E '"claude-[a-z0-9-]+":\s*\{' internal/subagent/anthropic/pricing.go)

# 4. Diff; emit issue body if drift detected
```

---

### `.github/workflows/pricing-drift.yaml` (new file)

**Analog:** `.github/workflows/nightly-integration.yml` (scheduled workflow, `actions/checkout@v4`, `curl` usage pattern)

**Workflow skeleton** (lines 17–34 of nightly-integration.yml as template):
```yaml
# TIDE pricing drift detection workflow (D-03).
# Fetches the Anthropic pricing docs page weekly, diffs against the compiled
# priceTable in internal/subagent/anthropic/pricing.go, and opens/updates a
# deduped labeled GitHub issue on drift. No auto-PR — a human reviews billing
# math changes.

name: pricing-drift
on:
  schedule:
    - cron: '0 9 * * 1'  # Monday 09:00 UTC (weekly)
  workflow_dispatch:

permissions: {}

jobs:
  check-pricing-drift:
    name: Pricing table drift check (D-03)
    runs-on: ubuntu-latest
    timeout-minutes: 5
    permissions:
      contents: read
      issues: write   # needed to open/update the drift issue
    steps:
      - name: Clone the code
        uses: actions/checkout@v4
        with:
          persist-credentials: false

      - name: Check pricing drift
        id: drift
        run: |
          ./hack/check-pricing-drift.sh
        # Script exits 0 if no drift, exits 1 if drift detected (captured in id: drift)
        continue-on-error: true

      - name: Open or update drift issue
        if: steps.drift.outcome == 'failure'
        uses: actions/github-script@v7
        with:
          script: |
            // Deduped issue open/update via labels (D-03 "deduped labeled issue")
            const label = 'pricing-drift';
            ...
```

---

## Shared Patterns

### Condition stamp (all controller files)
**Source:** `internal/controller/billing_halt.go` lines 125–134
**Apply to:** `budget_blocked.go` (setBudgetBlockedIfNeeded)
```go
patch := client.MergeFrom(project.DeepCopy())
meta.SetStatusCondition(&project.Status.Conditions, metav1.Condition{
    Type:               tideprojectv1alpha1.ConditionBudgetBlocked,
    Status:             metav1.ConditionTrue,
    Reason:             tideprojectv1alpha1.ReasonBudgetCapReached,
    Message:            "...",
    LastTransitionTime: metav1.Now(),
})
return c.Status().Patch(ctx, project, patch)
```

### Dispatch park (30s requeue, no phase change)
**Source:** `internal/controller/task_controller.go` lines 341–344
**Apply to:** All five dispatch sites for `checkBudgetBlocked`
```go
return taskGateResult{shouldHalt: true, result: ctrl.Result{RequeueAfter: 30 * time.Second}}, nil
```

### sync.Map in-process store
**Source:** `internal/budget/bucket.go` lines 52–98
**Apply to:** `internal/budget/reservation.go` (ReservationStore)
```go
type ReservationStore struct {
    m sync.Map // taskUID → int64 cents
}
// LoadOrStore for concurrent-safe lazy init; nolint:forcetypeassert on type assertion
```

### Fake client + WithStatusSubresource test setup
**Source:** `internal/controller/billing_halt_test.go` lines 124–137
**Apply to:** `internal/controller/budget_blocked_test.go`
```go
c := fake.NewClientBuilder().WithScheme(s).
    WithObjects(project).
    WithStatusSubresource(project).
    Build()
```

### Envtest Eventually condition check
**Source:** `internal/controller/billing_halt_regression_test.go` lines 705–712
**Apply to:** `internal/controller/budget_blocked_regression_test.go`
```go
Eventually(func(g Gomega) {
    var p tideprojectv1alpha1.Project
    g.Expect(mgrClient.Get(ctx, types.NamespacedName{...}, &p)).To(Succeed())
    c := meta.FindStatusCondition(p.Status.Conditions, tideprojectv1alpha1.ConditionBudgetBlocked)
    g.Expect(c).NotTo(BeNil())
    g.Expect(c.Status).To(Equal(metav1.ConditionTrue))
}, 5*time.Second, 100*time.Millisecond).Should(Succeed())
```

### maps.Clone for safe override merge
**Source:** `internal/budget/cap.go` lines 99–105 (ConsumeBypass pattern + `maps.Copy`)
**Apply to:** `internal/subagent/anthropic/pricing.go` New() function
```go
effective := maps.Clone(priceTable)
for k, v := range opts.PricingOverrides {
    effective[k] = v
}
```

### Non-fatal patch error logging
**Source:** `internal/controller/project_controller.go` lines 1167–1169
**Apply to:** setBudgetBlockedIfNeeded call sites (dispatch gates)
```go
if err := setBudgetBlockedIfNeeded(ctx, r.Client, project, r.ReservationStore.TotalReserved()); err != nil {
    logf.FromContext(ctx).Error(err, "setBudgetBlockedIfNeeded failed (non-fatal)")
}
```

### curl retry in CI/scripts
**Source:** `.github/workflows/nightly-integration.yml` lines 59–63
**Apply to:** `hack/check-pricing-drift.sh` and `.github/workflows/pricing-drift.yaml`
```bash
curl -fsSL \
    --retry 5 --retry-delay 3 --retry-all-errors --retry-connrefused \
    --connect-timeout 30 \
    -o ./output https://...
```

---

## No Analog Found

All files have close analogs. No entries in this section.

---

## Metadata

**Analog search scope:** `internal/budget/`, `internal/controller/billing_halt*.go`, `internal/subagent/anthropic/`, `api/v1alpha1/shared_types.go`, `charts/tide/values.yaml`, `.github/workflows/`, `hack/`
**Files scanned:** 17 source files read directly
**Pattern extraction date:** 2026-06-11
