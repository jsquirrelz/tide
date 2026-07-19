/*
Copyright 2026 TIDE Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Plan 51-05 Task 2 — CoOccurring holds tests. Pins the uniform dispatch-hold
// order (Billing → Failure → Verify → Budget → Import) across the four
// checkDispatchHolds callers (Milestone/Phase/Plan directly, Task via
// gateChecks' Phase 51 D-09 delegation) and proves the two folded W-2 todos:
//
//   - task-dispatch-gate-order-divergence: Task's gateChecks used to check
//     Import SECOND (before Billing/Failure/Budget); it now delegates to
//     checkDispatchHolds and checks Import LAST, same as the planner tier.
//   - project-dispatch-missing-failurehalt-gate: the Project planner chain
//     used to have no checkFailureHalt/checkVerifyHalt at all and would keep
//     spending under a conservative halt; it now holds.
//
// Also proves T-51-05e (the task-only BUDGET-03 reservation-headroom hold
// survives the gateChecks→checkDispatchHolds migration) and ESC-03 (a
// VerifyHalt is a distinct halt class — it never stamps FailureHalt, never
// touches Project.Status.Phase, and never touches a sibling Task's phase).
//
// Plain Go tests (not Ginkgo) so `-run 'CoOccurring'` genuinely executes
// against internal/controller's shared TestControllers Ginkgo entry point
// instead of vacuously matching zero specs (51-01-SUMMARY.md finding).
package controller

import (
	"context"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	tideprojectv1alpha3 "github.com/jsquirrelz/tide/api/v1alpha3"
	"github.com/jsquirrelz/tide/internal/budget"
	"github.com/jsquirrelz/tide/internal/owner"
)

// ---------- checkDispatchHolds order pinning (shared by all four tiers) ----------

func projectWithBudget(name string, capCents int64) *tideprojectv1alpha3.Project {
	return &tideprojectv1alpha3.Project{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
		Spec: tideprojectv1alpha3.ProjectSpec{
			SchemaRevision: "v1alpha3",
			TargetRepo:     "https://example.com/repo.git",
			Budget:         tideprojectv1alpha3.BudgetConfig{AbsoluteCapCents: capCents},
		},
	}
}

// TestCheckDispatchHolds_BillingBeforeImport pins Billing(30s) ahead of
// Import(5s) — the pre-existing planner-tier invariant, unchanged by Phase 51.
func TestCheckDispatchHolds_BillingBeforeImport(t *testing.T) {
	project := projectWithBudget("proj-billing-import", 0)
	project.Spec.ImportSource = &tideprojectv1alpha3.ImportSourceRef{SeedManifestConfigMap: "seed-cm", SalvagedPVCSubPath: "seed-pvc/workspace"}
	meta.SetStatusCondition(&project.Status.Conditions, metav1.Condition{
		Type: tideprojectv1alpha3.ConditionBillingHalt, Status: metav1.ConditionTrue,
		Reason: "test", LastTransitionTime: metav1.Now(),
	})

	held, result := checkDispatchHolds(context.Background(), project, "milestone", "ms-1")
	if !held {
		t.Fatal("expected held=true")
	}
	if result.RequeueAfter != 30*time.Second {
		t.Errorf("RequeueAfter = %s; want 30s (Billing must be checked before Import)", result.RequeueAfter)
	}
}

// TestCheckDispatchHolds_VerifyBeforeImport pins the newly-inserted VerifyHalt
// hold ahead of Import(5s) — proving it landed in the "early" group, not
// accidentally appended after Import.
func TestCheckDispatchHolds_VerifyBeforeImport(t *testing.T) {
	project := projectWithBudget("proj-verify-import", 0)
	project.Spec.ImportSource = &tideprojectv1alpha3.ImportSourceRef{SeedManifestConfigMap: "seed-cm", SalvagedPVCSubPath: "seed-pvc/workspace"}
	meta.SetStatusCondition(&project.Status.Conditions, metav1.Condition{
		Type: tideprojectv1alpha3.ConditionVerifyHalt, Status: metav1.ConditionTrue,
		Reason: tideprojectv1alpha3.ReasonVerifyExhausted, LastTransitionTime: metav1.Now(),
	})

	held, result := checkDispatchHolds(context.Background(), project, "plan", "plan-1")
	if !held {
		t.Fatal("expected held=true")
	}
	if result.RequeueAfter != 30*time.Second {
		t.Errorf("RequeueAfter = %s; want 30s (VerifyHalt must be checked before Import)", result.RequeueAfter)
	}
}

// TestCheckDispatchHolds_FailureBeforeImport mirrors the Billing case for
// FailureHalt (conservative profile).
func TestCheckDispatchHolds_FailureBeforeImport(t *testing.T) {
	project := projectWithBudget("proj-failure-import", 0)
	project.Spec.ImportSource = &tideprojectv1alpha3.ImportSourceRef{SeedManifestConfigMap: "seed-cm", SalvagedPVCSubPath: "seed-pvc/workspace"}
	meta.SetStatusCondition(&project.Status.Conditions, metav1.Condition{
		Type: tideprojectv1alpha3.ConditionFailureHalt, Status: metav1.ConditionTrue,
		Reason: tideprojectv1alpha3.ReasonTaskFailedHalt, LastTransitionTime: metav1.Now(),
	})

	held, result := checkDispatchHolds(context.Background(), project, "phase", "phase-1")
	if !held {
		t.Fatal("expected held=true")
	}
	if result.RequeueAfter != 30*time.Second {
		t.Errorf("RequeueAfter = %s; want 30s (FailureHalt must be checked before Import)", result.RequeueAfter)
	}
}

// TestCheckDispatchHolds_ImportAlone_FiresWithShortInterval proves Import
// still parks (5s) when nothing else holds — the baseline behavior.
func TestCheckDispatchHolds_ImportAlone_FiresWithShortInterval(t *testing.T) {
	project := projectWithBudget("proj-import-alone", 0)
	project.Spec.ImportSource = &tideprojectv1alpha3.ImportSourceRef{SeedManifestConfigMap: "seed-cm", SalvagedPVCSubPath: "seed-pvc/workspace"}

	held, result := checkDispatchHolds(context.Background(), project, "milestone", "ms-2")
	if !held {
		t.Fatal("expected held=true (import pending, no ImportComplete condition)")
	}
	if result.RequeueAfter != 5*time.Second {
		t.Errorf("RequeueAfter = %s; want 5s", result.RequeueAfter)
	}
}

// ---------- gateChecks (Task tier, delegated) co-occurring proof ----------

// TestCoOccurringHolds_GateChecks_BillingWinsOverImport is the
// concrete regression closing
// .planning/todos/pending/2026-07-12-task-dispatch-gate-order-divergence.md:
// before Phase 51 D-09, Task's inline chain checked Import SECOND, so this
// exact co-occurring state would have parked on the 5s Import hold instead of
// the 30s Billing hold. Post-migration, Task must produce the SAME hold as
// the planner tier under the identical Project state.
func TestCoOccurringHolds_GateChecks_BillingWinsOverImport(t *testing.T) {
	project := projectWithBudget("proj-task-co-occur", 0)
	project.Spec.ImportSource = &tideprojectv1alpha3.ImportSourceRef{SeedManifestConfigMap: "seed-cm", SalvagedPVCSubPath: "seed-pvc/workspace"}
	meta.SetStatusCondition(&project.Status.Conditions, metav1.Condition{
		Type: tideprojectv1alpha3.ConditionBillingHalt, Status: metav1.ConditionTrue,
		Reason: "test", LastTransitionTime: metav1.Now(),
	})

	task := &tideprojectv1alpha3.Task{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "task-co-occur",
			Namespace: "default",
			UID:       "uid-co-occur",
			Labels:    map[string]string{owner.LabelProject: project.Name},
		},
	}

	s := fakeSchemeWithAll(t)
	fakeClient := fake.NewClientBuilder().WithScheme(s).
		WithObjects(project, task).
		WithStatusSubresource(&tideprojectv1alpha3.Task{}, &tideprojectv1alpha3.Project{}).
		Build()
	r := &TaskReconciler{
		Client: fakeClient,
		Scheme: s,
		Deps: TaskReconcilerDeps{
			Budget:       budget.NewStore(),
			Defaults:     budget.Limits{RequestsPerMinute: 120, BurstSize: 10},
			Reservations: budget.NewReservationStore(),
		},
	}

	gate, err := r.gateChecks(context.Background(), task)
	if err != nil {
		t.Fatalf("gateChecks returned error: %v", err)
	}
	if !gate.shouldHalt {
		t.Fatal("shouldHalt = false; want true (Billing halt active)")
	}
	if gate.result.RequeueAfter != 30*time.Second {
		t.Errorf("RequeueAfter = %s; want 30s — Task must hold on Billing before reaching Import "+
			"(pre-migration this returned 5s, Import-wins)", gate.result.RequeueAfter)
	}
}

// TestGateChecks_HeadroomExhausted_StillParksAfterMigration proves T-51-05e:
// the task-only BUDGET-03 reservation-headroom hold (no planner-tier
// counterpart in checkDispatchHolds) survives the gateChecks migration.
func TestGateChecks_HeadroomExhausted_StillParksAfterMigration(t *testing.T) {
	project := projectWithBudget("proj-headroom", 100)
	project.Status.Budget.CostSpentCents = 90

	task := &tideprojectv1alpha3.Task{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "task-headroom",
			Namespace: "default",
			UID:       "uid-headroom",
			Labels:    map[string]string{owner.LabelProject: project.Name},
		},
	}

	s := fakeSchemeWithAll(t)
	fakeClient := fake.NewClientBuilder().WithScheme(s).
		WithObjects(project, task).
		WithStatusSubresource(&tideprojectv1alpha3.Task{}, &tideprojectv1alpha3.Project{}).
		Build()
	r := &TaskReconciler{
		Client: fakeClient,
		Scheme: s,
		Deps: TaskReconcilerDeps{
			Budget:               budget.NewStore(),
			Defaults:             budget.Limits{RequestsPerMinute: 120, BurstSize: 10},
			Reservations:         budget.NewReservationStore(),
			ReserveEstimateCents: 20, // 90 spent + 0 reserved + 20 estimate >= 100 cap
		},
	}

	gate, err := r.gateChecks(context.Background(), task)
	if err != nil {
		t.Fatalf("gateChecks returned error: %v", err)
	}
	if !gate.shouldHalt {
		t.Fatal("shouldHalt = false; want true (reservation headroom exhausted)")
	}
	if gate.result.RequeueAfter != 30*time.Second {
		t.Errorf("RequeueAfter = %s; want 30s", gate.result.RequeueAfter)
	}
}

// TestCoOccurringHolds_GateChecks_LegacyBudgetPhase_StillParks proves the task-only
// pre-Phase-14 BudgetExceeded phase fallback (no checkDispatchHolds
// counterpart) also survives the migration.
func TestCoOccurringHolds_GateChecks_LegacyBudgetPhase_StillParks(t *testing.T) {
	project := projectWithBudget("proj-legacy-phase", 0)
	project.Status.Phase = tideprojectv1alpha3.PhaseBudgetExceeded

	task := &tideprojectv1alpha3.Task{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "task-legacy-phase",
			Namespace: "default",
			UID:       "uid-legacy-phase",
			Labels:    map[string]string{owner.LabelProject: project.Name},
		},
	}

	s := fakeSchemeWithAll(t)
	fakeClient := fake.NewClientBuilder().WithScheme(s).
		WithObjects(project, task).
		WithStatusSubresource(&tideprojectv1alpha3.Task{}, &tideprojectv1alpha3.Project{}).
		Build()
	r := &TaskReconciler{
		Client: fakeClient,
		Scheme: s,
		Deps: TaskReconcilerDeps{
			Budget:       budget.NewStore(),
			Defaults:     budget.Limits{RequestsPerMinute: 120, BurstSize: 10},
			Reservations: budget.NewReservationStore(),
		},
	}

	gate, err := r.gateChecks(context.Background(), task)
	if err != nil {
		t.Fatalf("gateChecks returned error: %v", err)
	}
	if !gate.shouldHalt {
		t.Fatal("shouldHalt = false; want true (legacy BudgetExceeded phase)")
	}
}

// ---------- Project planner chain (folds project-dispatch-missing-failurehalt-gate) ----------

// TestCoOccurringHolds_ProjectPlannerDispatch_ConservativeFailureHalt_NowHolds closes
// .planning/todos/pending/2026-07-12-project-dispatch-missing-failurehalt-gate.md:
// before Phase 51 D-09 the Project planner chain had no checkFailureHalt at
// all and would dispatch (spend) under a conservative-profile halt.
func TestCoOccurringHolds_ProjectPlannerDispatch_ConservativeFailureHalt_NowHolds(t *testing.T) {
	project := &tideprojectv1alpha3.Project{
		ObjectMeta: metav1.ObjectMeta{Name: "proj-cfh", Namespace: "default", UID: "uid-cfh"},
		Spec: tideprojectv1alpha3.ProjectSpec{
			SchemaRevision: "v1alpha3",
			TargetRepo:     "https://example.com/repo.git",
			FailureProfile: tideprojectv1alpha3.FailureProfileConservative,
		},
	}
	meta.SetStatusCondition(&project.Status.Conditions, metav1.Condition{
		Type: tideprojectv1alpha3.ConditionFailureHalt, Status: metav1.ConditionTrue,
		Reason: tideprojectv1alpha3.ReasonTaskFailedHalt, LastTransitionTime: metav1.Now(),
	})

	s := fakeSchemeWithAll(t)
	fakeClient := fake.NewClientBuilder().WithScheme(s).
		WithObjects(project).
		WithStatusSubresource(&tideprojectv1alpha3.Project{}).
		Build()
	r := &ProjectReconciler{
		Client: fakeClient,
		Scheme: s,
		Deps: PlannerReconcilerDeps{
			Dispatcher: &stubDispatcher{},
			SigningKey: []byte("tide-test-signing-key-32-bytes!!"),
		},
	}

	result, err := r.reconcileProjectPlannerDispatch(context.Background(), project)
	if err != nil {
		t.Fatalf("reconcileProjectPlannerDispatch returned error: %v", err)
	}
	if result.RequeueAfter != 30*time.Second {
		t.Errorf("RequeueAfter = %s; want 30s — the Project planner chain must hold under a "+
			"conservative FailureHalt (pre-Phase-51-D-09 it had no checkFailureHalt gate at all)",
			result.RequeueAfter)
	}
}

// TestReconcileProjectPlannerDispatch_VerifyHalt_Holds proves ESC-02: the
// Project planner chain also holds under VerifyHalt.
func TestReconcileProjectPlannerDispatch_VerifyHalt_Holds(t *testing.T) {
	project := &tideprojectv1alpha3.Project{
		ObjectMeta: metav1.ObjectMeta{Name: "proj-vh", Namespace: "default", UID: "uid-vh"},
		Spec: tideprojectv1alpha3.ProjectSpec{
			SchemaRevision: "v1alpha3",
			TargetRepo:     "https://example.com/repo.git",
		},
	}
	meta.SetStatusCondition(&project.Status.Conditions, metav1.Condition{
		Type: tideprojectv1alpha3.ConditionVerifyHalt, Status: metav1.ConditionTrue,
		Reason: tideprojectv1alpha3.ReasonVerifyExhausted, LastTransitionTime: metav1.Now(),
	})

	s := fakeSchemeWithAll(t)
	fakeClient := fake.NewClientBuilder().WithScheme(s).
		WithObjects(project).
		WithStatusSubresource(&tideprojectv1alpha3.Project{}).
		Build()
	r := &ProjectReconciler{
		Client: fakeClient,
		Scheme: s,
		Deps: PlannerReconcilerDeps{
			Dispatcher: &stubDispatcher{},
			SigningKey: []byte("tide-test-signing-key-32-bytes!!"),
		},
	}

	result, err := r.reconcileProjectPlannerDispatch(context.Background(), project)
	if err != nil {
		t.Fatalf("reconcileProjectPlannerDispatch returned error: %v", err)
	}
	if result.RequeueAfter != 30*time.Second {
		t.Errorf("RequeueAfter = %s; want 30s — the Project planner chain must hold under VerifyHalt "+
			"(ESC-02: VerifyHalt gates BOTH tiers)", result.RequeueAfter)
	}
}

// ---------- ESC-03: VerifyHalt is a distinct halt class ----------

// TestVerifyHalt_DistinctClass_LeavesFailureAndSiblingPhaseUntouched proves
// ESC-03: stamping VerifyHalt never reinterprets Failed wave semantics.
// setVerifyHaltIfNeeded only ever patches the Project's Conditions — it never
// stamps ConditionFailureHalt, never touches Project.Status.Phase (which
// drives conservative-profile propagation), and never touches any Task
// object (which drives wave-sibling continuation via Task.Status.Phase).
func TestVerifyHalt_DistinctClass_LeavesFailureAndSiblingPhaseUntouched(t *testing.T) {
	s := fakeSchemeWithAll(t)
	project := &tideprojectv1alpha3.Project{
		ObjectMeta: metav1.ObjectMeta{Name: "proj-esc03", Namespace: "default"},
		Spec: tideprojectv1alpha3.ProjectSpec{
			SchemaRevision: "v1alpha3",
			TargetRepo:     "https://example.com/repo.git",
			FailureProfile: tideprojectv1alpha3.FailureProfileStrict,
		},
	}
	sibling := &tideprojectv1alpha3.Task{
		ObjectMeta: metav1.ObjectMeta{Name: "sibling-task", Namespace: "default"},
		Status:     tideprojectv1alpha3.TaskStatus{Phase: tideprojectv1alpha3.LevelPhasePending},
	}
	c := fake.NewClientBuilder().WithScheme(s).
		WithObjects(project, sibling).
		WithStatusSubresource(&tideprojectv1alpha3.Project{}, &tideprojectv1alpha3.Task{}).
		Build()

	if err := setVerifyHaltIfNeeded(context.Background(), c, project, time.Time{}); err != nil {
		t.Fatalf("setVerifyHaltIfNeeded: %v", err)
	}

	var gotProject tideprojectv1alpha3.Project
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: "default", Name: "proj-esc03"}, &gotProject); err != nil {
		t.Fatalf("get project: %v", err)
	}
	if !checkVerifyHalt(&gotProject) {
		t.Fatal("expected VerifyHalt=True to be stamped")
	}
	if checkFailureHalt(&gotProject) {
		t.Error("VerifyHalt must never imply/stamp FailureHalt — distinct halt class (ESC-03)")
	}
	if gotProject.Status.Phase != "" {
		t.Errorf("Project.Status.Phase = %q; want untouched (conservative-profile propagation reads Phase, not VerifyHalt)", gotProject.Status.Phase)
	}

	var gotSibling tideprojectv1alpha3.Task
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: "default", Name: "sibling-task"}, &gotSibling); err != nil {
		t.Fatalf("get sibling task: %v", err)
	}
	if gotSibling.Status.Phase != tideprojectv1alpha3.LevelPhasePending {
		t.Errorf("wave-sibling Task.Status.Phase = %q; want untouched %q — a VerifyHalt stamp must never reach a sibling Task",
			gotSibling.Status.Phase, tideprojectv1alpha3.LevelPhasePending)
	}
}
