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

// Plan 14-03 Task 3 — BudgetBlocked dispatch-gate regression tests.
//
// BUDGET-02 / BUDGET-03: run-1 regression — cap $100, wide wave; tasks were
// dispatched past the $100 cap (~$40 overshoot) and the Project was silently
// left without any BudgetBlocked condition (watch-predicate gap: RollUpUsage
// patches Status but does not increment metadata.generation, so the
// ProjectReconciler's GenerationChangedPredicate never re-enqueued it, and
// handleBudgetGate never fired).
//
// Fix (RESEARCH Option A): detection lives in the TaskReconciler — at the
// dispatch gate and immediately after RollUpUsage, not in the ProjectReconciler.
package controller

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	batchv1 "k8s.io/api/batch/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	tideprojectv1alpha1 "github.com/jsquirrelz/tide/api/v1alpha2"
	"github.com/jsquirrelz/tide/internal/budget"
	"github.com/jsquirrelz/tide/internal/dispatch/podjob"
)

// stampBudgetSpend patches project.Status.Budget.CostSpentCents to simulate
// task completion rolling up spend past the cap. Used across run-1 regression
// scenarios.
func stampBudgetSpend(ctx context.Context, projectName string, spentCents int64) {
	var proj tideprojectv1alpha1.Project
	Expect(k8sClient.Get(ctx, types.NamespacedName{Name: projectName, Namespace: "default"}, &proj)).To(Succeed())
	sp := client.MergeFrom(proj.DeepCopy())
	proj.Status.Budget.CostSpentCents = spentCents
	Expect(k8sClient.Status().Patch(ctx, &proj, sp)).To(Succeed())
	// Wait for the manager cache to reflect the spend — reconciler reads via mgrClient.
	Eventually(func() bool {
		var p tideprojectv1alpha1.Project
		if err := mgrClient.Get(ctx, types.NamespacedName{Name: projectName, Namespace: "default"}, &p); err != nil {
			return false
		}
		return p.Status.Budget.CostSpentCents == spentCents
	}, 5*time.Second, 50*time.Millisecond).Should(BeTrue(),
		"budget spend must be visible in cache before reconcile")
}

// makeBudgetProject creates a Project with a given AbsoluteCapCents budget.
func makeBudgetProject(ctx context.Context, name string, capCents int64) *tideprojectv1alpha1.Project {
	p := &tideprojectv1alpha1.Project{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
		Spec: tideprojectv1alpha1.ProjectSpec{
			TargetRepo: "https://github.com/example/tide.git",
			Budget: tideprojectv1alpha1.BudgetConfig{
				AbsoluteCapCents: capCents,
			},
		},
	}
	Expect(k8sClient.Create(ctx, p)).To(Succeed())
	waitForCacheSync(name, "default", &tideprojectv1alpha1.Project{})
	return p
}

// newBBTaskReconciler builds a TaskReconciler for BudgetBlocked regression specs.
// Wires Dispatcher, SigningKey, and the new Phase 14 Reservations fields.
func newBBTaskReconciler(envReader podjob.EnvelopeReader, reserveEstimate int64) *TaskReconciler {
	return &TaskReconciler{
		Client: mgrClient,
		Scheme: k8sClient.Scheme(),
		Deps: TaskReconcilerDeps{
			Dispatcher:           &stubDispatcher{},
			Budget:               testBudgetStore,
			Defaults:             testBudgetDefaults,
			SigningKey:           testSigningKey,
			CredproxyImage:       testCredproxyImage,
			EnvReader:            envReader,
			HelmProviderDefaults: ProviderDefaults{Image: testSubagentImage},
			Reservations:         budget.NewReservationStore(),
			ReserveEstimateCents: reserveEstimate,
		},
	}
}

// ---- Scenario 1a: run-1 regression — cap trips → BudgetBlocked=True on Project ----

// Run-1 symptom: cap hit at $100, ~$40 overshoot, silent Project.
// Scenario: cap=$100 ($10000 cents); CostSpentCents patched to $100.01 (10001).
// After the reconcile, the Project MUST carry BudgetBlocked=True (was silently absent in run-1).
var _ = Describe("BudgetBlocked run-1 regression (a): cap trips → Project carries BudgetBlocked",
	Label("envtest", "phase14", "budget-blocked", "regression"), func() {
		ctx := context.Background()
		const (
			projName   = "bb-run1-a-proj"
			planRef    = "bb-run1-a-plan"
			taskName   = "bb-run1-a-task"
			capCents   = int64(10000)
			spentCents = int64(10001)
		)

		var reconciler *TaskReconciler
		BeforeEach(func() {
			reconciler = newBBTaskReconciler(newMapEnvReader(), 0)
			makeBudgetProject(ctx, projName, capCents)
			makeTask(taskName, planRef, nil, projName)
			stampBudgetSpend(ctx, projName, spentCents)
		})
		AfterEach(func() {
			cleanupTask(taskName)
			cleanupProject(projName)
		})

		It("Project carries BudgetBlocked=True with Reason=BudgetCapReached after cap-crossing reconcile", func() {
			name := types.NamespacedName{Name: taskName, Namespace: "default"}
			_, err := reconcileN(reconciler, name, 3)
			Expect(err).NotTo(HaveOccurred())

			Eventually(func(g Gomega) {
				var p tideprojectv1alpha1.Project
				g.Expect(mgrClient.Get(ctx, types.NamespacedName{Name: projName, Namespace: "default"}, &p)).To(Succeed())
				c := meta.FindStatusCondition(p.Status.Conditions, tideprojectv1alpha1.ConditionBudgetBlocked)
				g.Expect(c).NotTo(BeNil(),
					"run-1 regression: Project must carry BudgetBlocked condition (was silently absent in run-1)")
				g.Expect(c.Status).To(Equal(metav1.ConditionTrue))
				g.Expect(c.Reason).To(Equal(tideprojectv1alpha1.ReasonBudgetCapReached))
			}, 5*time.Second, 100*time.Millisecond).Should(Succeed())
		})
	})

// ---- Scenario 1b: run-1 regression — no Job created; Task NOT Failed ----

var _ = Describe("BudgetBlocked run-1 regression (b): no Job created; Task NOT Failed",
	Label("envtest", "phase14", "budget-blocked", "regression"), func() {
		ctx := context.Background()
		const (
			projName   = "bb-run1-b-proj"
			planRef    = "bb-run1-b-plan"
			taskName   = "bb-run1-b-task"
			capCents   = int64(10000)
			spentCents = int64(10001)
		)

		var reconciler *TaskReconciler
		BeforeEach(func() {
			reconciler = newBBTaskReconciler(newMapEnvReader(), 0)
			makeBudgetProject(ctx, projName, capCents)
			makeTask(taskName, planRef, nil, projName)
			stampBudgetSpend(ctx, projName, spentCents)
		})
		AfterEach(func() {
			cleanupTask(taskName)
			cleanupProject(projName)
		})

		It("no executor Job created while BudgetBlocked; task is NOT Failed; hold returns 30s requeue", func() {
			name := types.NamespacedName{Name: taskName, Namespace: "default"}
			result, err := reconcileN(reconciler, name, 3)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).To(Equal(30*time.Second),
				"budget-blocked hold must return 30s park requeue (not empty Result)")

			var t tideprojectv1alpha1.Task
			Expect(mgrClient.Get(ctx, name, &t)).To(Succeed())
			Expect(t.Status.Phase).NotTo(Equal("Failed"),
				"BUDGET-02: BudgetBlocked must park the Task, not fail it")

			// Consistently: no Job created while cap is breached (~3s window).
			taskUID := t.UID
			Consistently(func() bool {
				var jobs batchv1.JobList
				if err := k8sClient.List(ctx, &jobs, client.InNamespace("default")); err != nil {
					return false
				}
				for _, j := range jobs.Items {
					if j.Labels["tideproject.k8s/task-uid"] == string(taskUID) {
						return true // unwanted Job found
					}
				}
				return false
			}, 3*time.Second, 200*time.Millisecond).Should(BeFalse(),
				"no executor Job must be created while BudgetBlocked=True (consistent over 3s)")
		})
	})

// ---- Scenario 2: reservation bound — second task parks when headroom exhausted ----

// cap=10000, spent=9000, estimate=600.
// First task dispatches: 9000+0+600 < 10000 (headroom). Reserve(600).
// Second task parks: 9000+600+600 = 10200 >= 10000 (no headroom).
// Total committed never exceeds cap + one estimate, vs run-1's ~$40 wave-wide overshoot.
var _ = Describe("BudgetBlocked reservation bound: second task parks when headroom exhausted",
	Label("envtest", "phase14", "budget-blocked", "regression"), func() {
		ctx := context.Background()
		const (
			projName   = "bb-rsv-proj"
			planRef    = "bb-rsv-plan"
			taskA      = "bb-rsv-task-a"
			taskB      = "bb-rsv-task-b"
			capCents   = int64(10000)
			spentCents = int64(9000)
			estimate   = int64(600)
		)

		var reconciler *TaskReconciler
		BeforeEach(func() {
			reconciler = newBBTaskReconciler(newMapEnvReader(), estimate)
			makeBudgetProject(ctx, projName, capCents)
			makeTask(taskA, planRef, nil, projName)
			makeTask(taskB, planRef, nil, projName)
			stampBudgetSpend(ctx, projName, spentCents)
		})
		AfterEach(func() {
			cleanupTask(taskA)
			cleanupTask(taskB)
			cleanupProject(projName)
			cleanupHaltTestJobs(projName)
		})

		It("first task dispatches (headroom OK); second task parks (headroom exhausted after reserve)", func() {
			nameA := types.NamespacedName{Name: taskA, Namespace: "default"}
			nameB := types.NamespacedName{Name: taskB, Namespace: "default"}

			// Task A: spent(9000) + reserved(0) + estimate(600) = 9600 < cap(10000) → dispatch.
			pumpTaskToRunning(reconciler, projName, taskA)
			var ta tideprojectv1alpha1.Task
			Expect(mgrClient.Get(ctx, nameA, &ta)).To(Succeed())
			Expect(ta.Status.Phase).To(Equal("Running"),
				"BUDGET-03: first task must dispatch when headroom is available")

			// Task B: spent(9000) + reserved(600) + estimate(600) = 10200 >= cap(10000) → park.
			result, err := reconcileN(reconciler, nameB, 3)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).To(Equal(30*time.Second),
				"BUDGET-03: second task must park at headroom gate (30s requeue)")

			var tb tideprojectv1alpha1.Task
			Expect(mgrClient.Get(ctx, nameB, &tb)).To(Succeed())
			Expect(tb.Status.Phase).NotTo(Equal("Running"),
				"BUDGET-03: second task must not dispatch — headroom gate parks it")
			Expect(tb.Status.Phase).NotTo(Equal("Failed"),
				"BUDGET-03: headroom park must not fail the task")
		})
	})

// ---- Scenario 3: cap-raise recovery — BudgetBlocked clears; dispatch resumes ----

var _ = Describe("BudgetBlocked cap-raise recovery: condition clears and dispatch resumes",
	Label("envtest", "phase14", "budget-blocked", "regression"), func() {
		ctx := context.Background()
		const (
			projName   = "bb-caprise-proj"
			planRef    = "bb-caprise-plan"
			taskName   = "bb-caprise-task"
			capCents   = int64(10000)
			spentCents = int64(10001)
			raisedCap  = int64(20000)
		)

		var reconciler *TaskReconciler
		BeforeEach(func() {
			reconciler = newBBTaskReconciler(newMapEnvReader(), 0)
			makeBudgetProject(ctx, projName, capCents)
			makeTask(taskName, planRef, nil, projName)
			stampBudgetSpend(ctx, projName, spentCents)
		})
		AfterEach(func() {
			cleanupTask(taskName)
			cleanupProject(projName)
			cleanupHaltTestJobs(projName)
		})

		It("BudgetBlocked=True clears to False when cap raised; parked task dispatches", func() {
			name := types.NamespacedName{Name: taskName, Namespace: "default"}

			// Pre-condition: pump reconciler until BudgetBlocked=True on the Project.
			Eventually(func(g Gomega) {
				_, err := reconcileN(reconciler, name, 1)
				g.Expect(err).NotTo(HaveOccurred())
				var p tideprojectv1alpha1.Project
				g.Expect(mgrClient.Get(ctx, types.NamespacedName{Name: projName, Namespace: "default"}, &p)).To(Succeed())
				c := meta.FindStatusCondition(p.Status.Conditions, tideprojectv1alpha1.ConditionBudgetBlocked)
				g.Expect(c).NotTo(BeNil())
				g.Expect(c.Status).To(Equal(metav1.ConditionTrue))
			}, 5*time.Second, 100*time.Millisecond).Should(Succeed(),
				"pre-condition: BudgetBlocked=True must be visible before cap raise")

			// Operator raises the cap via Spec edit (standard recovery path per D-04).
			var proj tideprojectv1alpha1.Project
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: projName, Namespace: "default"}, &proj)).To(Succeed())
			projPatch := client.MergeFrom(proj.DeepCopy())
			proj.Spec.Budget.AbsoluteCapCents = raisedCap
			Expect(k8sClient.Patch(ctx, &proj, projPatch)).To(Succeed())

			// Wait for cache to reflect the spec change.
			Eventually(func() bool {
				var p tideprojectv1alpha1.Project
				if err := mgrClient.Get(ctx, types.NamespacedName{Name: projName, Namespace: "default"}, &p); err != nil {
					return false
				}
				return p.Spec.Budget.AbsoluteCapCents == raisedCap
			}, 5*time.Second, 50*time.Millisecond).Should(BeTrue(),
				"raised cap must be visible in cache")

			// Reconcile again — bidirectional setBudgetBlockedIfNeeded clears the condition
			// (cap no longer exceeded with the raised limit).
			Eventually(func(g Gomega) {
				_, err := reconcileN(reconciler, name, 1)
				g.Expect(err).NotTo(HaveOccurred())
				var p tideprojectv1alpha1.Project
				g.Expect(mgrClient.Get(ctx, types.NamespacedName{Name: projName, Namespace: "default"}, &p)).To(Succeed())
				c := meta.FindStatusCondition(p.Status.Conditions, tideprojectv1alpha1.ConditionBudgetBlocked)
				if c == nil {
					return // absent is also a cleared state
				}
				g.Expect(c.Status).To(Equal(metav1.ConditionFalse),
					"T-14-08 mitigated: cap raise must clear BudgetBlocked (Reason=BudgetCapCleared)")
				g.Expect(c.Reason).To(Equal(tideprojectv1alpha1.ReasonBudgetCapCleared))
			}, 5*time.Second, 100*time.Millisecond).Should(Succeed(),
				"BudgetBlocked must flip to False after cap raise (bidirectional clear path)")

			// Dispatch must now succeed.
			pumpTaskToRunning(reconciler, projName, taskName)
			var t tideprojectv1alpha1.Task
			Expect(mgrClient.Get(ctx, name, &t)).To(Succeed())
			Expect(t.Status.Phase).To(Equal("Running"),
				"cap-raise recovery: task must dispatch after BudgetBlocked cleared")
		})
	})
