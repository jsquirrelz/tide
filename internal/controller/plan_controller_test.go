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
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	tideprojectv1alpha3 "github.com/jsquirrelz/tide/api/v1alpha3"
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
		task := &tideprojectv1alpha3.Task{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fullName,
				Namespace: "default",
			},
			Spec: tideprojectv1alpha3.TaskSpec{
				PlanRef:             planRef,
				FilesTouched:        []string{fmt.Sprintf("src/%s.go", t.name)},
				DeclaredOutputPaths: []string{fmt.Sprintf("artifacts/%s.txt", t.name)},
				PromptPath:          fmt.Sprintf("envelopes/test/children/%s.json", t.name),
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
				&tideprojectv1alpha3.Task{})
		}, 5*time.Second, 50*time.Millisecond).Should(Succeed())
		names = append(names, fullName)
	}
	return names
}

// cleanupPlanFixture deletes a Plan and all its Tasks + Waves.
func cleanupPlanFixture(planName string, taskNames []string) {
	for _, name := range taskNames {
		t := &tideprojectv1alpha3.Task{}
		if err := k8sClient.Get(context.Background(), types.NamespacedName{Name: name, Namespace: "default"}, t); err == nil {
			_ = k8sClient.Delete(context.Background(), t)
		}
	}
	// Delete Waves with our plan UID prefix. In v1alpha3 Waves are global-scope
	// (no Spec.PlanRef); the per-plan stub names them tide-wave-<plan.UID>-<i>,
	// so filter on that name prefix instead.
	var planForUID tideprojectv1alpha3.Plan
	wavePrefix := ""
	if err := k8sClient.Get(context.Background(),
		types.NamespacedName{Name: planName, Namespace: "default"}, &planForUID); err == nil {
		wavePrefix = fmt.Sprintf("tide-wave-%s-", planForUID.UID)
	}
	var waveList tideprojectv1alpha3.WaveList
	_ = k8sClient.List(context.Background(), &waveList, client.InNamespace("default"))
	for _, w := range waveList.Items {
		if wavePrefix != "" && strings.HasPrefix(w.Name, wavePrefix) {
			wv := w
			r := &WaveReconciler{Client: k8sClient, Scheme: k8sClient.Scheme()}
			_ = k8sClient.Delete(context.Background(), &wv)
			for range 3 {
				_, _ = r.Reconcile(context.Background(), reconcile.Request{
					NamespacedName: types.NamespacedName{Name: wv.Name, Namespace: "default"},
				})
			}
		}
	}
	p := &tideprojectv1alpha3.Plan{}
	if err := k8sClient.Get(context.Background(), types.NamespacedName{Name: planName, Namespace: "default"}, p); err == nil {
		r := &PlanReconciler{Client: k8sClient, Scheme: k8sClient.Scheme(), Dispatcher: &stubDispatcher{}, SigningKey: testSigningKey, CredproxyImage: testCredproxyImage}
		_ = k8sClient.Delete(context.Background(), p)
		for range 3 {
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
		CredproxyImage: testCredproxyImage,
		SigningKey:     testSigningKey,
		HelmProviderDefaults: ProviderDefaults{
			Image: testSubagentImage,
		},
	}
}

// makePlan creates a Plan in the default namespace with the given ValidationState.
// Waits for the manager cache to reflect the Plan (and its status patch) before returning.
func makePlan(name, phaseRef, validationState string) *tideprojectv1alpha3.Plan {
	p := &tideprojectv1alpha3.Plan{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "default",
		},
		Spec: tideprojectv1alpha3.PlanSpec{
			PhaseRef: phaseRef,
		},
	}
	Expect(k8sClient.Create(context.Background(), p)).To(Succeed())
	// Wait for cache to see the Plan.
	Eventually(func() error {
		return mgrClient.Get(context.Background(),
			types.NamespacedName{Name: name, Namespace: "default"},
			&tideprojectv1alpha3.Plan{})
	}, 5*time.Second, 50*time.Millisecond).Should(Succeed())

	if validationState != "" {
		var pp tideprojectv1alpha3.Plan
		Expect(k8sClient.Get(context.Background(), types.NamespacedName{Name: name, Namespace: "default"}, &pp)).To(Succeed())
		patch := client.MergeFrom(pp.DeepCopy())
		pp.Status.ValidationState = validationState
		Expect(k8sClient.Status().Patch(context.Background(), &pp, patch)).To(Succeed())
		// Wait for cache to reflect the ValidationState.
		Eventually(func() string {
			var updated tideprojectv1alpha3.Plan
			if err := mgrClient.Get(context.Background(),
				types.NamespacedName{Name: name, Namespace: "default"}, &updated); err != nil {
				return ""
			}
			return updated.Status.ValidationState
		}, 5*time.Second, 50*time.Millisecond).Should(Equal(validationState))
	}
	return p
}

// Cascade 7: reconcilePlannerDispatch must not commit a planner Job to the
// API server when resolveProjectForPlan returns nil. BuildJobSpec drops the
// credproxy provider Secret from EnvFrom when opts.Project == nil
// (internal/dispatch/podjob/jobspec.go:259-273), which would launch credproxy
// without ANTHROPIC_API_KEY → CrashLoopBackOff → Job.Status.Failed=1. Dispatch
// is single-shot (idempotent on AlreadyExists), so the first nil-Project create
// would permanently wedge the planner. The guard tested here gates dispatch on
// Project resolution: empty PhaseRef → permanent (no requeue); non-empty
// PhaseRef but unresolvable chain → transient (requeue after 1s).
var _ = Describe("PlanReconciler — D-03 project-label backfill (CUTS-01)", Label("envtest"), func() {
	ctx := context.Background()

	It("backfills tideproject.k8s/project on a Plan that was created without the label via Plan→Phase→Milestone→Project chain, and is idempotent on second reconcile", func() {
		const projName = "backfill-proj-pl"
		const msName = "backfill-ms-pl-01"
		const phName = "backfill-phase-pl-01"
		const planName = "backfill-plan-pl-01"

		// Create Project.
		proj := &tideprojectv1alpha3.Project{
			ObjectMeta: metav1.ObjectMeta{Name: projName, Namespace: "default"},
			Spec: tideprojectv1alpha3.ProjectSpec{SchemaRevision: "v1alpha3",
				TargetRepo: "https://github.com/example/test.git",
				Subagent:   tideprojectv1alpha3.SubagentConfig{Model: "claude-opus-4-7"},
			},
		}
		Expect(k8sClient.Create(ctx, proj)).To(Succeed())
		waitForCacheSync(projName, "default", &tideprojectv1alpha3.Project{})

		// Create Milestone (with ProjectRef so the chain is traversable).
		ms := &tideprojectv1alpha3.Milestone{
			ObjectMeta: metav1.ObjectMeta{Name: msName, Namespace: "default"},
			Spec:       tideprojectv1alpha3.MilestoneSpec{ProjectRef: projName},
		}
		Expect(k8sClient.Create(ctx, ms)).To(Succeed())
		waitForCacheSync(msName, "default", &tideprojectv1alpha3.Milestone{})

		// Create Phase (with MilestoneRef so Plan→Phase→Milestone→Project is traversable).
		ph := &tideprojectv1alpha3.Phase{
			ObjectMeta: metav1.ObjectMeta{Name: phName, Namespace: "default"},
			Spec:       tideprojectv1alpha3.PhaseSpec{MilestoneRef: msName},
		}
		Expect(k8sClient.Create(ctx, ph)).To(Succeed())
		waitForCacheSync(phName, "default", &tideprojectv1alpha3.Phase{})

		// Create Plan WITHOUT the tideproject.k8s/project label (pre-v1.0.1 shape).
		pl := &tideprojectv1alpha3.Plan{
			ObjectMeta: metav1.ObjectMeta{
				Name:      planName,
				Namespace: "default",
				// Labels intentionally absent.
			},
			Spec: tideprojectv1alpha3.PlanSpec{PhaseRef: phName},
		}
		Expect(k8sClient.Create(ctx, pl)).To(Succeed())
		waitForCacheSync(planName, "default", &tideprojectv1alpha3.Plan{})

		r := &PlanReconciler{
			Client: mgrClient,
			Scheme: k8sClient.Scheme(),
			// No Dispatcher — drives steps 1-5 only.
		}

		// First reconcile: finalizer, owner-ref, then backfill.
		Expect(reconcileWithRetry(r.Reconcile, types.NamespacedName{Name: planName, Namespace: "default"}, 5)).To(Succeed())

		// Assert the project label was backfilled.
		var after tideprojectv1alpha3.Plan
		Expect(mgrClient.Get(ctx, types.NamespacedName{Name: planName, Namespace: "default"}, &after)).To(Succeed())
		Expect(after.Labels["tideproject.k8s/project"]).To(Equal(projName),
			"backfill must stamp tideproject.k8s/project via Plan→Phase→Milestone→Project chain")

		// Idempotency: record ResourceVersion, reconcile again, verify unchanged.
		rvBefore := after.ResourceVersion
		Expect(reconcileWithRetry(r.Reconcile, types.NamespacedName{Name: planName, Namespace: "default"}, 2)).To(Succeed())
		var after2 tideprojectv1alpha3.Plan
		Expect(mgrClient.Get(ctx, types.NamespacedName{Name: planName, Namespace: "default"}, &after2)).To(Succeed())
		Expect(after2.ResourceVersion).To(Equal(rvBefore),
			"second reconcile must not patch the object (idempotent backfill)")

		// Cleanup.
		after2.Finalizers = nil
		_ = k8sClient.Update(ctx, &after2)
		_ = k8sClient.Delete(ctx, &after2)
		ph2 := &tideprojectv1alpha3.Phase{}
		if err := k8sClient.Get(ctx, types.NamespacedName{Name: phName, Namespace: "default"}, ph2); err == nil {
			ph2.Finalizers = nil
			_ = k8sClient.Update(ctx, ph2)
			_ = k8sClient.Delete(ctx, ph2)
		}
		ms2 := &tideprojectv1alpha3.Milestone{}
		if err := k8sClient.Get(ctx, types.NamespacedName{Name: msName, Namespace: "default"}, ms2); err == nil {
			ms2.Finalizers = nil
			_ = k8sClient.Update(ctx, ms2)
			_ = k8sClient.Delete(ctx, ms2)
		}
		proj2 := &tideprojectv1alpha3.Project{}
		if err := k8sClient.Get(ctx, types.NamespacedName{Name: projName, Namespace: "default"}, proj2); err == nil {
			proj2.Finalizers = nil
			_ = k8sClient.Update(ctx, proj2)
			_ = k8sClient.Delete(ctx, proj2)
		}
	})
})

// PlanReconciler — DEBT-04 (CR-01): envelope-read error is non-fatal (Pitfall-1 parity)
//
// When the EnvReader returns a transient error in handlePlannerJobCompletion, the Plan
// must NOT be set to terminal Status.Phase="Failed". Instead the handler defers to the
// children-based succession fallback, exactly as milestone_controller.go and
// phase_controller.go do (Phase 12 Pitfall 1). This Describe block is the regression
// guard for DEBT-04.
var _ = Describe("PlanReconciler — DEBT-04 envelope-read error is non-fatal (Pitfall-1 parity)", Label("envtest"), func() {
	ctx := context.Background()

	It("does not set Status.Phase=Failed when EnvReader.ReadOut returns a transient error", func() {
		const projName = "debt04-proj-plan"
		const msName = "debt04-ms-plan"
		const phName = "debt04-phase-plan"
		const planName = "debt04-plan-01"

		// Create Project → Milestone → Phase → Plan chain so resolveProjectForPlan succeeds.
		proj := &tideprojectv1alpha3.Project{
			ObjectMeta: metav1.ObjectMeta{Name: projName, Namespace: "default"},
			Spec: tideprojectv1alpha3.ProjectSpec{SchemaRevision: "v1alpha3",
				TargetRepo: "https://github.com/example/test.git",
				Subagent:   tideprojectv1alpha3.SubagentConfig{Model: "claude-opus-4-7"},
			},
		}
		Expect(k8sClient.Create(ctx, proj)).To(Succeed())
		waitForCacheSync(projName, "default", &tideprojectv1alpha3.Project{})

		ms := &tideprojectv1alpha3.Milestone{
			ObjectMeta: metav1.ObjectMeta{Name: msName, Namespace: "default"},
			Spec:       tideprojectv1alpha3.MilestoneSpec{ProjectRef: projName},
		}
		Expect(k8sClient.Create(ctx, ms)).To(Succeed())
		waitForCacheSync(msName, "default", &tideprojectv1alpha3.Milestone{})

		ph := &tideprojectv1alpha3.Phase{
			ObjectMeta: metav1.ObjectMeta{Name: phName, Namespace: "default"},
			Spec:       tideprojectv1alpha3.PhaseSpec{MilestoneRef: msName},
		}
		Expect(k8sClient.Create(ctx, ph)).To(Succeed())
		waitForCacheSync(phName, "default", &tideprojectv1alpha3.Phase{})

		pl := &tideprojectv1alpha3.Plan{
			ObjectMeta: metav1.ObjectMeta{
				Name:      planName,
				Namespace: "default",
				Labels: map[string]string{
					"tideproject.k8s/project": projName,
				},
			},
			Spec: tideprojectv1alpha3.PlanSpec{PhaseRef: phName},
		}
		Expect(k8sClient.Create(ctx, pl)).To(Succeed())
		waitForCacheSync(planName, "default", &tideprojectv1alpha3.Plan{})

		// Fetch the Plan so we have its UID.
		var plan tideprojectv1alpha3.Plan
		Expect(mgrClient.Get(ctx, types.NamespacedName{Name: planName, Namespace: "default"}, &plan)).To(Succeed())

		// Wire an EnvReader that returns an error for this Plan's UID (simulates a
		// transient PVC/read failure — the condition that previously wedged the Plan).
		envReader := newMapEnvReader()
		envReader.SetErr(string(plan.UID), fmt.Errorf("simulated transient read error for DEBT-04"))

		r := &PlanReconciler{
			Client:    mgrClient,
			Scheme:    k8sClient.Scheme(),
			EnvReader: envReader,
			// No Dispatcher or ReporterImage — we call handlePlannerJobCompletion directly.
		}

		// Drive handlePlannerJobCompletion directly with a nil completedJob
		// (billing-halt path gates on out.ExitCode != 0, unreachable when read errors).
		_, err := r.handlePlannerJobCompletion(ctx, &plan, nil)

		// The load-bearing assertions:
		//
		// 1. The in-memory plan struct must NOT have Status.Phase="Failed" set
		//    (the buggy code mutates the struct before patching). This catches the
		//    regression even if the status subresource patch is delayed in cache.
		Expect(plan.Status.Phase).NotTo(Equal("Failed"),
			"DEBT-04: handlePlannerJobCompletion must not mutate plan.Status.Phase to Failed on a transient read error (in-memory check)")

		// 2. A transient read error must not return a hard reconcile error.
		Expect(err).NotTo(HaveOccurred(),
			"a transient envelope read error must not return a hard reconcile error (non-fatal)")

		// 3. Cross-check via the direct API-server client (k8sClient) to verify the
		//    ConditionFailed with Reason=EnvelopeReadFailed was NOT patched to etcd.
		//    Use Eventually so the watch stream propagates if the cache is stale.
		Consistently(func(g Gomega) {
			var fresh tideprojectv1alpha3.Plan
			g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: planName, Namespace: "default"}, &fresh)).To(Succeed())
			g.Expect(fresh.Status.Phase).NotTo(Equal("Failed"),
				"DEBT-04: a transient envelope-read error must NOT persist terminal Failed to etcd")
		}, 1*time.Second, 100*time.Millisecond).Should(Succeed())

		// Cleanup.
		DeferCleanup(func() {
			for _, obj := range []client.Object{
				&tideprojectv1alpha3.Plan{ObjectMeta: metav1.ObjectMeta{Name: planName, Namespace: "default"}},
				&tideprojectv1alpha3.Phase{ObjectMeta: metav1.ObjectMeta{Name: phName, Namespace: "default"}},
				&tideprojectv1alpha3.Milestone{ObjectMeta: metav1.ObjectMeta{Name: msName, Namespace: "default"}},
				&tideprojectv1alpha3.Project{ObjectMeta: metav1.ObjectMeta{Name: projName, Namespace: "default"}},
			} {
				_ = k8sClient.Get(ctx, types.NamespacedName{Name: obj.GetName(), Namespace: obj.GetNamespace()}, obj)
				obj.SetFinalizers(nil)
				_ = k8sClient.Update(ctx, obj)
				_ = k8sClient.Delete(ctx, obj)
			}
		})
	})
})

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

			var plan tideprojectv1alpha3.Plan
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
			plan := tideprojectv1alpha3.Plan{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "plan-cascade7-perm",
					Namespace: "default",
				},
				Spec: tideprojectv1alpha3.PlanSpec{PhaseRef: ""},
			}
			result, handled, err := r.reconcilePlannerDispatch(ctx, &plan)
			Expect(err).NotTo(HaveOccurred())
			Expect(handled).To(BeFalse(),
				"guard should NOT mark as handled — dispatch was not committed")
			Expect(result.Requeue).To(BeFalse(), //nolint:staticcheck // SA1019: asserting the controller does not set the legacy Requeue field
				"empty-PhaseRef arm must not requeue (permanent error)")
			Expect(result.RequeueAfter).To(Equal(time.Duration(0)),
				"empty-PhaseRef arm must not requeue (permanent error)")
		})
	})
})
