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

package controller

import (
	"context"
	"slices"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	batchv1 "k8s.io/api/batch/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	tideprojectv1alpha1 "github.com/jsquirrelz/tide/api/v1alpha1"
)

// fileTouchGateReconciler constructs a PlanReconciler wired for the file-touch
// gate tests. DefaultFileTouchMode defaults to "warn" — tests that need strict
// mode drive it via Project.Spec.PlanAdmission.FileTouchMode resolved through
// the tideproject.k8s/project label on the Plan.
func fileTouchGateReconciler() *PlanReconciler {
	return &PlanReconciler{
		Client:               mgrClient,
		Scheme:               k8sClient.Scheme(),
		Dispatcher:           &stubDispatcher{},
		PlannerPool:          newPlannerPoolForTest(),
		EnvReader:            newMapEnvReader(),
		SubagentImage:        testSubagentImage,
		CredproxyImage:       testCredproxyImage,
		SigningKey:           testSigningKey,
		DefaultFileTouchMode: "warn",
		HelmProviderDefaults: ProviderDefaults{
			Image: testSubagentImage,
		},
	}
}

// makeFileTouchProject creates just the Project CRD for file-touch gate tests.
// The plan uses the `tideproject.k8s/project` label to short-circuit the owner-ref
// chain walk so the reconciler gate finds the project even without a full
// Phase/Milestone/Project chain. The Plan's spec.PhaseRef is set to a non-existent
// phase name so the reconciler skips the owner-ref update step (which would fire
// the admission webhook on every reconcile cycle and block strict-mode Plans that
// already have overlapping Tasks in the indexer cache).
func makeFileTouchProject(ctx context.Context, projectName, ftMode string) {
	proj := &tideprojectv1alpha1.Project{
		ObjectMeta: metav1.ObjectMeta{Name: projectName, Namespace: "default"},
		Spec: tideprojectv1alpha1.ProjectSpec{
			TargetRepo: "https://github.com/example/ft-test.git",
			Subagent:   tideprojectv1alpha1.SubagentConfig{Model: "claude-opus-4-7"},
			PlanAdmission: tideprojectv1alpha1.PlanAdmissionConfig{
				FileTouchMode: ftMode,
			},
		},
	}
	Expect(k8sClient.Create(ctx, proj)).To(Succeed())
	waitForCacheSync(projectName, "default", &tideprojectv1alpha1.Project{})
}

// cleanupFileTouchFixture removes all objects created for a file-touch gate test.
func cleanupFileTouchFixture(ctx context.Context, projectName, planName string, taskNames []string) {
	for _, tn := range taskNames {
		t := &tideprojectv1alpha1.Task{}
		if err := k8sClient.Get(ctx, types.NamespacedName{Name: tn, Namespace: "default"}, t); err == nil {
			t.Finalizers = nil
			_ = k8sClient.Update(ctx, t)
			_ = k8sClient.Delete(ctx, t)
		}
	}
	p := &tideprojectv1alpha1.Plan{}
	if err := k8sClient.Get(ctx, types.NamespacedName{Name: planName, Namespace: "default"}, p); err == nil {
		p.Finalizers = nil
		_ = k8sClient.Update(ctx, p)
		_ = k8sClient.Delete(ctx, p)
	}
	proj := &tideprojectv1alpha1.Project{}
	if err := k8sClient.Get(ctx, types.NamespacedName{Name: projectName, Namespace: "default"}, proj); err == nil {
		proj.Finalizers = nil
		_ = k8sClient.Update(ctx, proj)
		_ = k8sClient.Delete(ctx, proj)
	}
}

// mkFileTouchTask creates a Task CRD in the cluster for file-touch gate tests.
func mkFileTouchTask(ctx context.Context, name, planRef string, dependsOn, filesTouched []string) *tideprojectv1alpha1.Task {
	t := &tideprojectv1alpha1.Task{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
		Spec: tideprojectv1alpha1.TaskSpec{
			PlanRef:             planRef,
			DependsOn:           dependsOn,
			FilesTouched:        filesTouched,
			DeclaredOutputPaths: filesTouched,
			PromptPath:          "envelopes/test/children/" + name + ".json",
		},
	}
	Expect(k8sClient.Create(ctx, t)).To(Succeed())
	return t
}

// mkFileTouchPlan creates a Plan for file-touch gate tests. The Plan is labeled
// with the project name for reconciler resolution, and uses a non-existent phase
// name to prevent the reconciler's owner-ref update from triggering the webhook
// with overlapping Tasks in view (which would cause a strict-mode rejection).
func mkFileTouchPlan(planName, projectName string) *tideprojectv1alpha1.Plan {
	return &tideprojectv1alpha1.Plan{
		ObjectMeta: metav1.ObjectMeta{
			Name:      planName,
			Namespace: "default",
			Labels: map[string]string{
				// Label fast-path: resolveProjectForPlan uses this to skip the chain
				// walk, so the reconciler gate finds Project.Spec.FileTouchMode=strict
				// even without a real Phase/Milestone chain.
				"tideproject.k8s/project": projectName,
			},
		},
		// Use a non-existent phase name: the reconciler's step-4 gets NotFound,
		// skips the r.Update(plan) owner-ref call, and never fires the admission
		// webhook with overlapping Tasks in view.
		Spec: tideprojectv1alpha1.PlanSpec{PhaseRef: "ft-phase-stub"},
	}
}

// PlanReconciler — file-touch dispatch gate (D-05, D-06) [CUTS-07]
//
// These tests assert the reconciler gate's behavior:
//   - Test 1: strict + shared file + no edge → park (ValidationState=FileTouchMismatch, zero Jobs)
//   - Test 2: adding dependsOn lifts the park on the next reconcile
//   - Test 3: non-strict mode does not park even with overlap
//   - Test 4: parked plan never has Status.Phase=Failed
var _ = Describe("PlanReconciler — file-touch dispatch gate (D-05, D-06)", Label("envtest", "phase15", "file-touch"), func() {
	ctx := context.Background()

	Describe("Test 1 — strict + sibling overlap + no edge → ValidationState=FileTouchMismatch, zero Jobs (run-1 symptom regression)", func() {
		const (
			projectName = "ft-proj-1"
			planName    = "ft-plan-1"
			taskA       = "ft-task-a1"
			taskB       = "ft-task-b1"
		)

		BeforeEach(func() {
			makeFileTouchProject(ctx, projectName, "strict")
		})
		AfterEach(func() {
			cleanupFileTouchFixture(ctx, projectName, planName, []string{taskA, taskB})
		})

		It("parks the Plan with FileTouchMismatch and dispatches zero Jobs", func() {
			// Create a Plan and stamp it as Validated (the gate only runs after Validated).
			plan := mkFileTouchPlan(planName, projectName)
			Expect(k8sClient.Create(ctx, plan)).To(Succeed())
			waitForCacheSync(planName, "default", &tideprojectv1alpha1.Plan{})

			// Stamp ValidationState=Validated so the gate runs (status subresource —
			// no admission webhook fires on status patches).
			patchValidated := client.MergeFrom(plan.DeepCopy())
			plan.Status.ValidationState = "Validated"
			Expect(k8sClient.Status().Patch(ctx, plan, patchValidated)).To(Succeed())

			// Create two sibling Tasks sharing the same file with no dependsOn.
			mkFileTouchTask(ctx, taskA, planName, nil, []string{"pkg/shared/common.go"})
			mkFileTouchTask(ctx, taskB, planName, nil, []string{"pkg/shared/common.go"})

			// Wait for tasks to appear in the manager's field-indexer cache.
			Eventually(func() int {
				var tl tideprojectv1alpha1.TaskList
				_ = mgrClient.List(ctx, &tl, client.InNamespace("default"),
					client.MatchingFields{taskPlanRefIndexKey: planName})
				return len(tl.Items)
			}, "15s", "200ms").Should(BeNumerically(">=", 2))

			// Drive reconcile via the gate reconciler.
			r := fileTouchGateReconciler()
			Expect(reconcileWithRetry(r.Reconcile, types.NamespacedName{Name: planName, Namespace: "default"}, 3)).To(Succeed())

			// Assert: ValidationState=FileTouchMismatch.
			Eventually(func(g Gomega) {
				var after tideprojectv1alpha1.Plan
				g.Expect(mgrClient.Get(ctx, types.NamespacedName{Name: planName, Namespace: "default"}, &after)).To(Succeed())
				g.Expect(after.Status.ValidationState).To(Equal("FileTouchMismatch"),
					"strict + shared file + no edge must park with ValidationState=FileTouchMismatch")

				// Condition names both task names and the shared path.
				cond := meta.FindStatusCondition(after.Status.Conditions, tideprojectv1alpha1.ConditionWaveOrLevelPaused)
				g.Expect(cond).NotTo(BeNil(), "WaveOrLevelPaused condition must be set")
				g.Expect(cond.Status).To(Equal(metav1.ConditionTrue))
				g.Expect(cond.Reason).To(Equal("FileTouchMismatch"))
				g.Expect(cond.Message).To(ContainSubstring(taskA))
				g.Expect(cond.Message).To(ContainSubstring(taskB))
				g.Expect(cond.Message).To(ContainSubstring("pkg/shared/common.go"))
			}, "15s", "200ms").Should(Succeed())

			// Assert: ZERO Jobs were dispatched whose name contains this plan's UID.
			// We scope to jobs whose name prefix matches tide-plan-<uid> to avoid
			// false positives from Jobs created by other concurrent test specs.
			var afterPlan tideprojectv1alpha1.Plan
			Expect(mgrClient.Get(ctx, types.NamespacedName{Name: planName, Namespace: "default"}, &afterPlan)).To(Succeed())
			planUID := string(afterPlan.UID)

			var jobs batchv1.JobList
			Expect(k8sClient.List(ctx, &jobs, client.InNamespace("default"))).To(Succeed())
			for _, j := range jobs.Items {
				// Only check Jobs that could have been dispatched for this Plan.
				if len(planUID) > 0 {
					isForThisPlan := false
					for _, ref := range j.OwnerReferences {
						if ref.Kind == "Plan" && string(ref.UID) == planUID {
							isForThisPlan = true
						}
					}
					Expect(isForThisPlan).To(BeFalse(),
						"no Job should be dispatched while Plan is parked at FileTouchMismatch; found job %s", j.Name)
				}
			}

			// Assert: Status.Phase is NOT Failed (park-not-fail doctrine).
			var afterFinal tideprojectv1alpha1.Plan
			Expect(mgrClient.Get(ctx, types.NamespacedName{Name: planName, Namespace: "default"}, &afterFinal)).To(Succeed())
			Expect(afterFinal.Status.Phase).NotTo(Equal("Failed"),
				"the file-touch gate must park-not-fail; Phase=Failed must never be set by this gate")
		})
	})

	Describe("Test 2 — adding dependsOn lifts the park (D-06)", func() {
		const (
			projectName = "ft-proj-2"
			planName    = "ft-plan-2"
			taskA       = "ft-task-a2"
			taskB       = "ft-task-b2"
		)

		BeforeEach(func() {
			makeFileTouchProject(ctx, projectName, "strict")
		})
		AfterEach(func() {
			cleanupFileTouchFixture(ctx, projectName, planName, []string{taskA, taskB})
		})

		It("clears FileTouchMismatch park when the dependsOn edge is added", func() {
			plan := mkFileTouchPlan(planName, projectName)
			Expect(k8sClient.Create(ctx, plan)).To(Succeed())
			waitForCacheSync(planName, "default", &tideprojectv1alpha1.Plan{})

			// Stamp Validated.
			patchValidated := client.MergeFrom(plan.DeepCopy())
			plan.Status.ValidationState = "Validated"
			Expect(k8sClient.Status().Patch(ctx, plan, patchValidated)).To(Succeed())

			// Both tasks share the same file, no edge → gate parks.
			tA := mkFileTouchTask(ctx, taskA, planName, nil, []string{"pkg/shared/lib.go"})
			_ = mkFileTouchTask(ctx, taskB, planName, nil, []string{"pkg/shared/lib.go"})

			Eventually(func() int {
				var tl tideprojectv1alpha1.TaskList
				_ = mgrClient.List(ctx, &tl, client.InNamespace("default"),
					client.MatchingFields{taskPlanRefIndexKey: planName})
				return len(tl.Items)
			}, "15s", "200ms").Should(BeNumerically(">=", 2))

			r := fileTouchGateReconciler()
			Expect(reconcileWithRetry(r.Reconcile, types.NamespacedName{Name: planName, Namespace: "default"}, 3)).To(Succeed())

			// First: confirm parked.
			Eventually(func(g Gomega) {
				var after tideprojectv1alpha1.Plan
				g.Expect(mgrClient.Get(ctx, types.NamespacedName{Name: planName, Namespace: "default"}, &after)).To(Succeed())
				g.Expect(after.Status.ValidationState).To(Equal("FileTouchMismatch"))
			}, "10s", "100ms").Should(Succeed())

			// Fix the overlap: add dependsOn edge on taskB → taskA.
			Eventually(func() error {
				fresh := &tideprojectv1alpha1.Task{}
				if err := mgrClient.Get(ctx, types.NamespacedName{Name: taskB, Namespace: "default"}, fresh); err != nil {
					return err
				}
				patch := client.MergeFrom(fresh.DeepCopy())
				fresh.Spec.DependsOn = []string{tA.Name}
				return k8sClient.Patch(ctx, fresh, patch)
			}, "10s", "200ms").Should(Succeed())

			// Wait for cache to reflect the updated Task.
			Eventually(func() bool {
				var freshB tideprojectv1alpha1.Task
				if err := mgrClient.Get(ctx, types.NamespacedName{Name: taskB, Namespace: "default"}, &freshB); err != nil {
					return false
				}
				return slices.Contains(freshB.Spec.DependsOn, tA.Name)
			}, "10s", "200ms").Should(BeTrue(), "cache should reflect dependsOn edge")

			// Reconcile again — park should lift.
			Expect(reconcileWithRetry(r.Reconcile, types.NamespacedName{Name: planName, Namespace: "default"}, 3)).To(Succeed())

			// Assert: ValidationState cleared back to Validated (park lifted).
			Eventually(func(g Gomega) {
				var after tideprojectv1alpha1.Plan
				g.Expect(mgrClient.Get(ctx, types.NamespacedName{Name: planName, Namespace: "default"}, &after)).To(Succeed())
				g.Expect(after.Status.ValidationState).To(Equal("Validated"),
					"adding dependsOn edge must lift the FileTouchMismatch park (ValidationState returns to Validated)")
				// Paused condition should be cleared (Status=False).
				cond := meta.FindStatusCondition(after.Status.Conditions, tideprojectv1alpha1.ConditionWaveOrLevelPaused)
				if cond != nil {
					g.Expect(cond.Status).To(Equal(metav1.ConditionFalse),
						"WaveOrLevelPaused condition must be cleared (Status=False) after park lifts")
				}
			}, "15s", "200ms").Should(Succeed())
		})
	})

	Describe("Test 3 — non-strict mode: overlap does NOT park", func() {
		const (
			projectName = "ft-proj-3"
			planName    = "ft-plan-3"
			taskA       = "ft-task-a3"
			taskB       = "ft-task-b3"
		)

		BeforeEach(func() {
			makeFileTouchProject(ctx, projectName, "warn") // warn mode — no park
		})
		AfterEach(func() {
			cleanupFileTouchFixture(ctx, projectName, planName, []string{taskA, taskB})
		})

		It("does not park when mode is warn even with overlapping filesTouched", func() {
			plan := mkFileTouchPlan(planName, projectName)
			Expect(k8sClient.Create(ctx, plan)).To(Succeed())
			waitForCacheSync(planName, "default", &tideprojectv1alpha1.Plan{})

			// Stamp Validated.
			patchValidated := client.MergeFrom(plan.DeepCopy())
			plan.Status.ValidationState = "Validated"
			Expect(k8sClient.Status().Patch(ctx, plan, patchValidated)).To(Succeed())

			// Both tasks share the same file — in warn mode, no park.
			mkFileTouchTask(ctx, taskA, planName, nil, []string{"pkg/shared/util.go"})
			mkFileTouchTask(ctx, taskB, planName, nil, []string{"pkg/shared/util.go"})

			Eventually(func() int {
				var tl tideprojectv1alpha1.TaskList
				_ = mgrClient.List(ctx, &tl, client.InNamespace("default"),
					client.MatchingFields{taskPlanRefIndexKey: planName})
				return len(tl.Items)
			}, "15s", "200ms").Should(BeNumerically(">=", 2))

			r := fileTouchGateReconciler()
			Expect(reconcileWithRetry(r.Reconcile, types.NamespacedName{Name: planName, Namespace: "default"}, 3)).To(Succeed())

			// Assert: ValidationState is NOT FileTouchMismatch (should remain Validated or unset).
			Consistently(func(g Gomega) {
				var after tideprojectv1alpha1.Plan
				g.Expect(mgrClient.Get(ctx, types.NamespacedName{Name: planName, Namespace: "default"}, &after)).To(Succeed())
				g.Expect(after.Status.ValidationState).NotTo(Equal("FileTouchMismatch"),
					"warn mode must NOT park on file-touch overlap")
			}, "2s", "200ms").Should(Succeed())
		})
	})

	Describe("Test 4 — park is not Failed (D-05 park-not-fail doctrine)", func() {
		const (
			projectName = "ft-proj-4"
			planName    = "ft-plan-4"
			taskA       = "ft-task-a4"
			taskB       = "ft-task-b4"
		)

		BeforeEach(func() {
			makeFileTouchProject(ctx, projectName, "strict")
		})
		AfterEach(func() {
			cleanupFileTouchFixture(ctx, projectName, planName, []string{taskA, taskB})
		})

		It("never sets Status.Phase=Failed when parking for FileTouchMismatch", func() {
			plan := mkFileTouchPlan(planName, projectName)
			Expect(k8sClient.Create(ctx, plan)).To(Succeed())
			waitForCacheSync(planName, "default", &tideprojectv1alpha1.Plan{})

			patchValidated := client.MergeFrom(plan.DeepCopy())
			plan.Status.ValidationState = "Validated"
			Expect(k8sClient.Status().Patch(ctx, plan, patchValidated)).To(Succeed())

			mkFileTouchTask(ctx, taskA, planName, nil, []string{"pkg/shared/model.go"})
			mkFileTouchTask(ctx, taskB, planName, nil, []string{"pkg/shared/model.go"})

			Eventually(func() int {
				var tl tideprojectv1alpha1.TaskList
				_ = mgrClient.List(ctx, &tl, client.InNamespace("default"),
					client.MatchingFields{taskPlanRefIndexKey: planName})
				return len(tl.Items)
			}, "15s", "200ms").Should(BeNumerically(">=", 2))

			r := fileTouchGateReconciler()
			Expect(reconcileWithRetry(r.Reconcile, types.NamespacedName{Name: planName, Namespace: "default"}, 3)).To(Succeed())

			// Poll for the parked state.
			Eventually(func(g Gomega) {
				var after tideprojectv1alpha1.Plan
				g.Expect(mgrClient.Get(ctx, types.NamespacedName{Name: planName, Namespace: "default"}, &after)).To(Succeed())
				g.Expect(after.Status.ValidationState).To(Equal("FileTouchMismatch"))
			}, "10s", "100ms").Should(Succeed())

			// Assert that Status.Phase is NEVER Failed.
			var after tideprojectv1alpha1.Plan
			Expect(mgrClient.Get(ctx, types.NamespacedName{Name: planName, Namespace: "default"}, &after)).To(Succeed())
			Expect(after.Status.Phase).NotTo(Equal("Failed"),
				"file-touch gate must park-not-fail (D-05 doctrine): Status.Phase=Failed must never be set by this gate")
		})
	})
})
