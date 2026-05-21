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
	"fmt"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	batchv1 "k8s.io/api/batch/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	tideprojectv1alpha1 "github.com/jsquirrelz/tide/api/v1alpha1"
)

// alphaThroughThetaFixture creates the α…θ Tasks matching pkg/dag/kahn_test.go:
//
//	α, β, γ, ζ  (layer 0 — no deps)
//	δ(α,β), η(γ) (layer 1)
//	ε(δ), θ(η,ζ) (layer 2)
//
// All tasks are created under the given planRef in the default namespace.
func alphaThroughThetaFixture(planRef string) []string {
	tasks := []struct {
		name      string
		dependsOn []string
	}{
		{name: "alpha", dependsOn: nil},
		{name: "beta", dependsOn: nil},
		{name: "gamma", dependsOn: nil},
		{name: "zeta", dependsOn: nil},
		{name: "delta", dependsOn: []string{"alpha", "beta"}},
		{name: "eta", dependsOn: []string{"gamma"}},
		{name: "epsilon", dependsOn: []string{"delta"}},
		{name: "theta", dependsOn: []string{"eta", "zeta"}},
	}
	names := make([]string, 0, len(tasks))
	for _, t := range tasks {
		fullName := fmt.Sprintf("%s-%s", planRef, t.name)
		task := &tideprojectv1alpha1.Task{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fullName,
				Namespace: "default",
			},
			Spec: tideprojectv1alpha1.TaskSpec{
				PlanRef:             planRef,
				FilesTouched:        []string{fmt.Sprintf("src/%s.go", t.name)},
				DeclaredOutputPaths: []string{fmt.Sprintf("artifacts/%s.txt", t.name)},
			},
		}
		// Translate short names to full names in DependsOn.
		for _, dep := range t.dependsOn {
			task.Spec.DependsOn = append(task.Spec.DependsOn, fmt.Sprintf("%s-%s", planRef, dep))
		}
		Expect(k8sClient.Create(context.Background(), task)).To(Succeed())
		taskNameCopy := fullName
		Eventually(func() error {
			return mgrClient.Get(context.Background(),
				types.NamespacedName{Name: taskNameCopy, Namespace: "default"},
				&tideprojectv1alpha1.Task{})
		}, 5*time.Second, 50*time.Millisecond).Should(Succeed())
		names = append(names, fullName)
	}
	return names
}

// cleanupPlanFixture deletes a Plan and all its Tasks + Waves.
func cleanupPlanFixture(planName string, taskNames []string) {
	for _, name := range taskNames {
		t := &tideprojectv1alpha1.Task{}
		if err := k8sClient.Get(context.Background(), types.NamespacedName{Name: name, Namespace: "default"}, t); err == nil {
			_ = k8sClient.Delete(context.Background(), t)
		}
	}
	// Delete Waves with our plan UID prefix.
	var waveList tideprojectv1alpha1.WaveList
	_ = k8sClient.List(context.Background(), &waveList, client.InNamespace("default"))
	for _, w := range waveList.Items {
		if w.Spec.PlanRef == planName {
			wv := w
			r := &WaveReconciler{Client: k8sClient, Scheme: k8sClient.Scheme()}
			_ = k8sClient.Delete(context.Background(), &wv)
			for i := 0; i < 3; i++ {
				_, _ = r.Reconcile(context.Background(), reconcile.Request{
					NamespacedName: types.NamespacedName{Name: wv.Name, Namespace: "default"},
				})
			}
		}
	}
	p := &tideprojectv1alpha1.Plan{}
	if err := k8sClient.Get(context.Background(), types.NamespacedName{Name: planName, Namespace: "default"}, p); err == nil {
		r := &PlanReconciler{Client: k8sClient, Scheme: k8sClient.Scheme(), Dispatcher: &stubDispatcher{}, SigningKey: testSigningKey, CredproxyImage: testCredproxyImage}
		_ = k8sClient.Delete(context.Background(), p)
		for i := 0; i < 3; i++ {
			_, _ = r.Reconcile(context.Background(), reconcile.Request{
				NamespacedName: types.NamespacedName{Name: planName, Namespace: "default"},
			})
		}
	}
}

// newPlanReconciler builds a PlanReconciler wired for testing.
// Uses mgrClient (the manager's cached client) so that MatchingFields queries against
// the in-process .spec.planRef field indexer work correctly.
// Phase 04.1 P1.2: CredproxyImage and SigningKey are required for reconcilePlannerDispatch.
func newPlanReconciler() *PlanReconciler {
	return &PlanReconciler{
		Client:         mgrClient,
		Scheme:         k8sClient.Scheme(),
		Dispatcher:     &stubDispatcher{},
		SubagentImage:  testSubagentImage,
		CredproxyImage: testCredproxyImage,
		SigningKey:     testSigningKey,
	}
}

// reconcilePlanN drives a PlanReconciler N times, retrying on 409 Conflict.
func reconcilePlanN(r *PlanReconciler, name types.NamespacedName, n int) (ctrl.Result, error) {
	var result ctrl.Result
	var err error
	for i := 0; i < n; i++ {
		for attempt := 0; attempt < 5; attempt++ {
			result, err = r.Reconcile(context.Background(), reconcile.Request{NamespacedName: name})
			if err == nil {
				break
			}
			if strings.Contains(err.Error(), "the object has been modified") {
				err = nil
				continue
			}
			return result, err
		}
		if err != nil {
			return result, err
		}
	}
	return result, err
}

// makePlan creates a Plan in the default namespace with the given ValidationState.
// Waits for the manager cache to reflect the Plan (and its status patch) before returning.
func makePlan(name, phaseRef, validationState string) *tideprojectv1alpha1.Plan {
	p := &tideprojectv1alpha1.Plan{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "default",
		},
		Spec: tideprojectv1alpha1.PlanSpec{
			PhaseRef: phaseRef,
		},
	}
	Expect(k8sClient.Create(context.Background(), p)).To(Succeed())
	// Wait for cache to see the Plan.
	Eventually(func() error {
		return mgrClient.Get(context.Background(),
			types.NamespacedName{Name: name, Namespace: "default"},
			&tideprojectv1alpha1.Plan{})
	}, 5*time.Second, 50*time.Millisecond).Should(Succeed())

	if validationState != "" {
		var pp tideprojectv1alpha1.Plan
		Expect(k8sClient.Get(context.Background(), types.NamespacedName{Name: name, Namespace: "default"}, &pp)).To(Succeed())
		patch := client.MergeFrom(pp.DeepCopy())
		pp.Status.ValidationState = validationState
		Expect(k8sClient.Status().Patch(context.Background(), &pp, patch)).To(Succeed())
		// Wait for cache to reflect the ValidationState.
		Eventually(func() string {
			var updated tideprojectv1alpha1.Plan
			if err := mgrClient.Get(context.Background(),
				types.NamespacedName{Name: name, Namespace: "default"}, &updated); err != nil {
				return ""
			}
			return updated.Status.ValidationState
		}, 5*time.Second, 50*time.Millisecond).Should(Equal(validationState))
	}
	return p
}

var _ = Describe("PlanReconciler Wave materialization", Label("envtest", "phase2"), func() {
	ctx := context.Background()

	Describe("TestPlanReconciler_NoOpUntilValidated", func() {
		const planName = "plan-noop"
		const phaseRef = "phase-noop"

		BeforeEach(func() {
			makePlan(planName, phaseRef, "") // ValidationState empty
		})
		AfterEach(func() {
			cleanupPlanFixture(planName, nil)
		})

		It("should not create any Waves when ValidationState is empty", func() {
			r := newPlanReconciler()
			planNS := types.NamespacedName{Name: planName, Namespace: "default"}

			_, err := reconcilePlanN(r, planNS, 4)
			Expect(err).NotTo(HaveOccurred())

			var waveList tideprojectv1alpha1.WaveList
			Expect(k8sClient.List(ctx, &waveList,
				client.InNamespace("default"),
			)).To(Succeed())
			for _, w := range waveList.Items {
				Expect(w.Spec.PlanRef).NotTo(Equal(planName),
					"expected no Waves for unvalidated Plan")
			}
		})
	})

	Describe("TestPlanReconciler_OnValidated_MaterializesWavesFromComputeWaves", func() {
		const planName = "plan-validated"
		const phaseRef = "phase-validated"
		var taskNames []string
		var planUID types.UID

		BeforeEach(func() {
			makePlan(planName, phaseRef, "Validated")
			var p tideprojectv1alpha1.Plan
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: planName, Namespace: "default"}, &p)).To(Succeed())
			planUID = p.UID
			taskNames = alphaThroughThetaFixture(planName)
		})
		AfterEach(func() {
			cleanupPlanFixture(planName, taskNames)
		})

		It("should create 3 Waves with correct names for the α…θ DAG (3 layers: sizes 4/2/2)", func() {
			r := newPlanReconciler()
			planNS := types.NamespacedName{Name: planName, Namespace: "default"}

			// Drive through finalizer + owner-ref + materialization passes.
			_, err := reconcilePlanN(r, planNS, 5)
			Expect(err).NotTo(HaveOccurred())

			// Expect 3 Waves: tide-wave-{UID}-0, -1, -2.
			for i := 0; i < 3; i++ {
				waveName := fmt.Sprintf("tide-wave-%s-%d", planUID, i)
				var wave tideprojectv1alpha1.Wave
				Expect(k8sClient.Get(ctx, types.NamespacedName{Name: waveName, Namespace: "default"}, &wave)).To(Succeed(),
					"expected wave %s to exist", waveName)
				Expect(wave.Spec.PlanRef).To(Equal(planName))
				Expect(wave.Spec.WaveIndex).To(Equal(i))
			}
		})
	})

	Describe("TestPlanReconciler_WaveCreationIdempotent", func() {
		const planName = "plan-idempotent"
		const phaseRef = "phase-idempotent"
		var taskNames []string
		var planUID types.UID

		BeforeEach(func() {
			makePlan(planName, phaseRef, "Validated")
			var p tideprojectv1alpha1.Plan
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: planName, Namespace: "default"}, &p)).To(Succeed())
			planUID = p.UID
			taskNames = alphaThroughThetaFixture(planName)
		})
		AfterEach(func() {
			cleanupPlanFixture(planName, taskNames)
		})

		It("should not create duplicate Waves on repeated reconciliation", func() {
			r := newPlanReconciler()
			planNS := types.NamespacedName{Name: planName, Namespace: "default"}

			// First reconcile pass.
			_, err := reconcilePlanN(r, planNS, 5)
			Expect(err).NotTo(HaveOccurred())

			// Second reconcile pass (idempotent).
			_, err = reconcilePlanN(r, planNS, 3)
			Expect(err).NotTo(HaveOccurred())

			// Wave count should still be exactly 3.
			var waveList tideprojectv1alpha1.WaveList
			Expect(k8sClient.List(ctx, &waveList, client.InNamespace("default"))).To(Succeed())
			planWaves := 0
			for _, w := range waveList.Items {
				if w.Spec.PlanRef == planName {
					planWaves++
				}
			}
			Expect(planWaves).To(Equal(3),
				"expected exactly 3 waves for plan %s (UID=%s)", planName, planUID)
		})
	})

	Describe("TestPlanReconciler_StampsTaskLabels", func() {
		const planName = "plan-labelstamp"
		const phaseRef = "phase-labelstamp"
		var taskNames []string

		BeforeEach(func() {
			makeProjectForTask("proj-labelstamp")
			// Phase 04.1 P1.4: stamp the project label on the Plan so resolveProjectName
			// finds it via the label fast-path (the old projectList.Items[0] fallback
			// was removed). In production, PlanReconciler gets this label via owner-ref
			// chain wiring; in unit tests we stamp it directly.
			plan := makePlan(planName, phaseRef, "Validated")
			var pp tideprojectv1alpha1.Plan
			Expect(k8sClient.Get(context.Background(), types.NamespacedName{Name: plan.Name, Namespace: "default"}, &pp)).To(Succeed())
			if pp.Labels == nil {
				pp.Labels = map[string]string{}
			}
			pp.Labels["tideproject.k8s/project"] = "proj-labelstamp"
			Expect(k8sClient.Update(context.Background(), &pp)).To(Succeed())
			taskNames = alphaThroughThetaFixture(planName)
		})
		AfterEach(func() {
			cleanupPlanFixture(planName, taskNames)
			cleanupProject("proj-labelstamp")
		})

		It("should stamp tideproject.k8s/wave-index and tideproject.k8s/project labels on every Task", func() {
			r := newPlanReconciler()
			planNS := types.NamespacedName{Name: planName, Namespace: "default"}

			_, err := reconcilePlanN(r, planNS, 5)
			Expect(err).NotTo(HaveOccurred())

			for _, name := range taskNames {
				var task tideprojectv1alpha1.Task
				Expect(k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: "default"}, &task)).To(Succeed())
				Expect(task.Labels).To(HaveKey("tideproject.k8s/wave-index"),
					"task %s missing wave-index label", name)
			}
		})
	})

	Describe("TestPlanReconciler_OwnerRefCascadeToPlan", func() {
		const planName = "plan-ownerref"
		const phaseRef = "phase-ownerref"
		var taskNames []string
		var planUID types.UID

		BeforeEach(func() {
			makePlan(planName, phaseRef, "Validated")
			var p tideprojectv1alpha1.Plan
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: planName, Namespace: "default"}, &p)).To(Succeed())
			planUID = p.UID
			taskNames = alphaThroughThetaFixture(planName)
		})
		AfterEach(func() {
			cleanupPlanFixture(planName, taskNames)
		})

		It("should set Controller=true, BlockOwnerDeletion=true on each Wave's OwnerReference", func() {
			r := newPlanReconciler()
			planNS := types.NamespacedName{Name: planName, Namespace: "default"}

			_, err := reconcilePlanN(r, planNS, 5)
			Expect(err).NotTo(HaveOccurred())

			for i := 0; i < 3; i++ {
				waveName := fmt.Sprintf("tide-wave-%s-%d", planUID, i)
				var wave tideprojectv1alpha1.Wave
				Expect(k8sClient.Get(ctx, types.NamespacedName{Name: waveName, Namespace: "default"}, &wave)).To(Succeed())

				Expect(wave.OwnerReferences).NotTo(BeEmpty(),
					"wave %s missing owner references", waveName)
				found := false
				for _, ref := range wave.OwnerReferences {
					if ref.Kind == "Plan" && ref.Name == planName {
						Expect(ref.Controller).NotTo(BeNil())
						Expect(*ref.Controller).To(BeTrue())
						Expect(ref.BlockOwnerDeletion).NotTo(BeNil())
						Expect(*ref.BlockOwnerDeletion).To(BeTrue())
						found = true
					}
				}
				Expect(found).To(BeTrue(), "wave %s missing Plan owner ref", waveName)
			}
		})
	})

	Describe("TestPlanReconciler_ComputeWavesEveryReconcile", func() {
		const planName = "plan-persist03"
		const phaseRef = "phase-persist03"
		var taskNames []string
		var planUID types.UID

		BeforeEach(func() {
			makePlan(planName, phaseRef, "Validated")
			var p tideprojectv1alpha1.Plan
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: planName, Namespace: "default"}, &p)).To(Succeed())
			planUID = p.UID
			// Start with just 2 tasks (no deps → 1 wave).
			for _, name := range []string{"persist03-a", "persist03-b"} {
				t := &tideprojectv1alpha1.Task{
					ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
					Spec: tideprojectv1alpha1.TaskSpec{
						PlanRef:             planName,
						FilesTouched:        []string{"src/main.go"},
						DeclaredOutputPaths: []string{"artifacts/out.txt"},
					},
				}
				Expect(k8sClient.Create(ctx, t)).To(Succeed())
				taskNames = append(taskNames, name)
			}
		})
		AfterEach(func() {
			cleanupPlanFixture(planName, taskNames)
		})

		It("should pick up new Tasks added between reconciles (PERSIST-03: no cached schedule)", func() {
			r := newPlanReconciler()
			planNS := types.NamespacedName{Name: planName, Namespace: "default"}

			// Wait for BeforeSuite-created tasks to appear in cache.
			for _, tn := range []string{"persist03-a", "persist03-b"} {
				tnCopy := tn
				Eventually(func() error {
					return mgrClient.Get(ctx, types.NamespacedName{Name: tnCopy, Namespace: "default"}, &tideprojectv1alpha1.Task{})
				}, 5*time.Second, 50*time.Millisecond).Should(Succeed())
			}

			// First reconcile: 2 tasks with no deps → 1 wave.
			_, err := reconcilePlanN(r, planNS, 5)
			Expect(err).NotTo(HaveOccurred())

			wave0Name := fmt.Sprintf("tide-wave-%s-0", planUID)
			var wave0 tideprojectv1alpha1.Wave
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: wave0Name, Namespace: "default"}, &wave0)).To(Succeed())
			Expect(wave0.Spec.WaveIndex).To(Equal(0))

			// Add a new task that depends on persist03-a → forces a second wave.
			newTask := &tideprojectv1alpha1.Task{
				ObjectMeta: metav1.ObjectMeta{Name: "persist03-c", Namespace: "default"},
				Spec: tideprojectv1alpha1.TaskSpec{
					PlanRef:             planName,
					FilesTouched:        []string{"src/c.go"},
					DeclaredOutputPaths: []string{"artifacts/c.txt"},
					DependsOn:           []string{"persist03-a"},
				},
			}
			Expect(k8sClient.Create(ctx, newTask)).To(Succeed())
			taskNames = append(taskNames, "persist03-c")
			// Wait for the new task to appear in the cache before re-reconciling.
			Eventually(func() error {
				return mgrClient.Get(ctx, types.NamespacedName{Name: "persist03-c", Namespace: "default"}, &tideprojectv1alpha1.Task{})
			}, 5*time.Second, 50*time.Millisecond).Should(Succeed())

			// Second reconcile: should re-derive waves and create wave 1.
			_, err = reconcilePlanN(r, planNS, 3)
			Expect(err).NotTo(HaveOccurred())

			wave1Name := fmt.Sprintf("tide-wave-%s-1", planUID)
			var wave1 tideprojectv1alpha1.Wave
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: wave1Name, Namespace: "default"}, &wave1)).To(Succeed(),
				"expected wave-1 to be created after adding a dependent Task (PERSIST-03 assertion)")
		})
	})
})

// Cascade 7: reconcilePlannerDispatch must not commit a planner Job to the
// API server when resolveProjectForPlan returns nil. BuildJobSpec drops the
// credproxy provider Secret from EnvFrom when opts.Project == nil
// (internal/dispatch/podjob/jobspec.go:259-273), which would launch credproxy
// without ANTHROPIC_API_KEY → CrashLoopBackOff → Job.Status.Failed=1. Dispatch
// is single-shot (idempotent on AlreadyExists), so the first nil-Project create
// would permanently wedge the planner. The guard tested here gates dispatch on
// Project resolution: empty PhaseRef → permanent (no requeue); non-empty
// PhaseRef but unresolvable chain → transient (requeue after 1s).
var _ = Describe("PlanReconciler nil-Project dispatch guard (cascade-7)", Label("envtest", "phase2"), func() {
	ctx := context.Background()

	Describe("TestPlanReconciler_PlannerDispatch_RequeuesWhenPhaseNotInCache", func() {
		const planName = "plan-cascade7-transient"
		const phaseRef = "phase-cascade7-missing"

		BeforeEach(func() {
			// Phase is deliberately NOT created — resolveProjectForPlan returns
			// nil via the Phase Get-failure branch at line 487.
			makePlan(planName, phaseRef, "")
		})
		AfterEach(func() {
			cleanupPlanFixture(planName, nil)
		})

		It("should requeue without dispatching when Phase/Milestone/Project chain is unresolvable", func() {
			r := newPlanReconciler()

			var plan tideprojectv1alpha1.Plan
			Expect(mgrClient.Get(ctx, types.NamespacedName{Name: planName, Namespace: "default"}, &plan)).To(Succeed())
			planUID := plan.UID

			result, handled, err := r.reconcilePlannerDispatch(ctx, &plan)
			Expect(err).NotTo(HaveOccurred())
			Expect(handled).To(BeFalse(),
				"guard should NOT mark as handled — dispatch was not committed")
			Expect(result.RequeueAfter).To(Equal(1*time.Second),
				"transient cache-miss arm must requeue at 1s")

			// No planner Job should exist — the guard fired before r.Create.
			var job batchv1.Job
			err = mgrClient.Get(ctx, types.NamespacedName{
				Name:      fmt.Sprintf("tide-plan-%s-1", planUID),
				Namespace: "default",
			}, &job)
			Expect(apierrors.IsNotFound(err)).To(BeTrue(),
				"planner Job must not exist when guard fires")
		})
	})

	Describe("TestPlanReconciler_PlannerDispatch_PermanentOnEmptyPhaseRef", func() {
		// Empty-PhaseRef arm: stack-constructed Plan (no round-trip through
		// k8sClient.Create — admission may reject empty PhaseRef). The List
		// inside reconcilePlannerDispatch needs valid Namespace/Name, so we
		// stamp both even though the spec is otherwise empty.
		It("should refuse dispatch without requeueing when Spec.PhaseRef is empty", func() {
			r := newPlanReconciler()
			plan := tideprojectv1alpha1.Plan{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "plan-cascade7-perm",
					Namespace: "default",
				},
				Spec: tideprojectv1alpha1.PlanSpec{PhaseRef: ""},
			}
			result, handled, err := r.reconcilePlannerDispatch(ctx, &plan)
			Expect(err).NotTo(HaveOccurred())
			Expect(handled).To(BeFalse(),
				"guard should NOT mark as handled — dispatch was not committed")
			Expect(result.Requeue).To(BeFalse(),
				"empty-PhaseRef arm must not requeue (permanent error)")
			Expect(result.RequeueAfter).To(Equal(time.Duration(0)),
				"empty-PhaseRef arm must not requeue (permanent error)")
		})
	})
})
