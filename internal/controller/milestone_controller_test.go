/*
Copyright 2026 TIDE Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	tideprojectv1alpha1 "github.com/jsquirrelz/tide/api/v1alpha1"
	"github.com/jsquirrelz/tide/internal/pool"
	pkgdispatch "github.com/jsquirrelz/tide/pkg/dispatch"
)

// newPlannerPoolForTest constructs a planner pool with capacity 16 for tests.
func newPlannerPoolForTest() *pool.Pool {
	return pool.New(16, "planner")
}

// reconcileWithRetry drives a Reconcile call N times, retrying on 409 Conflict.
type reconcilerFunc func(context.Context, reconcile.Request) (ctrl.Result, error)

func reconcileWithRetry(r reconcilerFunc, name types.NamespacedName, n int) error {
	for range n {
		for range 5 {
			_, err := r(context.Background(), reconcile.Request{NamespacedName: name})
			if err == nil {
				break
			}
			if strings.Contains(err.Error(), "the object has been modified") || strings.Contains(err.Error(), "Conflict") {
				continue
			}
			return err
		}
	}
	return nil
}

// makeFakeJobTerminal patches a Job to a terminal state (Complete or Failed)
// for envtest. envtest doesn't run real Jobs, so we set status conditions
// directly. status.startTime is required for finished jobs.
func makeFakeJobTerminal(ctx context.Context, c client.Client, name, namespace string, succeeded bool) error {
	var job batchv1.Job
	if err := c.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, &job); err != nil {
		return err
	}
	now := metav1.Now()
	job.Status.StartTime = &now
	job.Status.CompletionTime = &now
	job.Status.Conditions = []batchv1.JobCondition{}
	if succeeded {
		job.Status.Succeeded = 1
		job.Status.Conditions = append(job.Status.Conditions,
			batchv1.JobCondition{
				Type:               batchv1.JobSuccessCriteriaMet,
				Status:             corev1.ConditionTrue,
				LastTransitionTime: now,
			},
			batchv1.JobCondition{
				Type:               batchv1.JobComplete,
				Status:             corev1.ConditionTrue,
				LastTransitionTime: now,
			})
	} else {
		job.Status.Failed = 1
		job.Status.Conditions = append(job.Status.Conditions, batchv1.JobCondition{
			Type:               batchv1.JobFailed,
			Status:             corev1.ConditionTrue,
			LastTransitionTime: now,
		})
	}
	return c.Status().Update(ctx, &job)
}

var _ = Describe("MilestoneReconciler — planner dispatch + child materialization", Label("envtest", "phase3"), func() {
	const projectName = "test-proj-ms"
	const milestoneName = "test-ms-1"
	ctx := context.Background()

	BeforeEach(func() {
		// Create the parent Project so resolveProject succeeds.
		proj := &tideprojectv1alpha1.Project{
			ObjectMeta: metav1.ObjectMeta{Name: projectName, Namespace: "default"},
			Spec: tideprojectv1alpha1.ProjectSpec{
				TargetRepo: "https://github.com/example/test.git",
				Subagent: tideprojectv1alpha1.SubagentConfig{
					Model: "claude-opus-4-7",
				},
				Git: &tideprojectv1alpha1.GitConfig{
					RepoURL:        "https://github.com/example/test.git",
					CredsSecretRef: "test-creds",
				},
			},
		}
		Expect(k8sClient.Create(ctx, proj)).To(Succeed())
		waitForCacheSync(projectName, "default", &tideprojectv1alpha1.Project{})
	})

	AfterEach(func() {
		// Cleanup Milestone (best-effort).
		ms := &tideprojectv1alpha1.Milestone{}
		if err := k8sClient.Get(ctx, types.NamespacedName{Name: milestoneName, Namespace: "default"}, ms); err == nil {
			ms.Finalizers = nil
			_ = k8sClient.Update(ctx, ms)
			_ = k8sClient.Delete(ctx, ms)
		}
		proj := &tideprojectv1alpha1.Project{}
		if err := k8sClient.Get(ctx, types.NamespacedName{Name: projectName, Namespace: "default"}, proj); err == nil {
			proj.Finalizers = nil
			_ = k8sClient.Update(ctx, proj)
			_ = k8sClient.Delete(ctx, proj)
		}
		// Cleanup any child Phases.
		var phases tideprojectv1alpha1.PhaseList
		_ = k8sClient.List(ctx, &phases, client.InNamespace("default"))
		for i := range phases.Items {
			p := phases.Items[i]
			p.Finalizers = nil
			_ = k8sClient.Update(ctx, &p)
			_ = k8sClient.Delete(ctx, &p)
		}
		// Cleanup Jobs.
		var jobs batchv1.JobList
		_ = k8sClient.List(ctx, &jobs, client.InNamespace("default"))
		for i := range jobs.Items {
			j := jobs.Items[i]
			_ = k8sClient.Delete(ctx, &j)
		}
	})

	It("Test 1: dispatches planner Job and patches Status.Phase=Running on first reconcile", func() {
		// Create the Milestone.
		ms := &tideprojectv1alpha1.Milestone{
			ObjectMeta: metav1.ObjectMeta{Name: milestoneName, Namespace: "default"},
			Spec:       tideprojectv1alpha1.MilestoneSpec{ProjectRef: projectName},
		}
		Expect(k8sClient.Create(ctx, ms)).To(Succeed())
		Eventually(func() error {
			return mgrClient.Get(ctx, types.NamespacedName{Name: milestoneName, Namespace: "default"}, &tideprojectv1alpha1.Milestone{})
		}, "5s", "100ms").Should(Succeed())

		r := &MilestoneReconciler{
			Client:         mgrClient,
			Scheme:         k8sClient.Scheme(),
			Dispatcher:     &stubDispatcher{},
			PlannerPool:    newPlannerPoolForTest(),
			EnvReader:      newMapEnvReader(),
			SubagentImage:  testSubagentImage,
			CredproxyImage: testCredproxyImage,
			SigningKey:     testSigningKey,
		}

		// Reconcile a few times — first for finalizer ensure, then for owner ref, then for dispatch.
		Expect(reconcileWithRetry(r.Reconcile, types.NamespacedName{Name: milestoneName, Namespace: "default"}, 5)).To(Succeed())

		// Verify Job exists with the deterministic name.
		Eventually(func(g Gomega) {
			var got tideprojectv1alpha1.Milestone
			g.Expect(mgrClient.Get(ctx, types.NamespacedName{Name: milestoneName, Namespace: "default"}, &got)).To(Succeed())
			expectedJobName := fmt.Sprintf("tide-milestone-%s-1", got.UID)
			var job batchv1.Job
			g.Expect(mgrClient.Get(ctx, types.NamespacedName{Name: expectedJobName, Namespace: "default"}, &job)).To(Succeed())
			g.Expect(got.Status.Phase).To(Equal("Running"))
		}, 5*time.Second, 100*time.Millisecond).Should(Succeed())
	})

	It("Test 2: on Job completion materializes Phase children from EnvelopeOut.ChildCRDs", func() {
		// Create Milestone.
		ms := &tideprojectv1alpha1.Milestone{
			ObjectMeta: metav1.ObjectMeta{Name: milestoneName, Namespace: "default"},
			Spec:       tideprojectv1alpha1.MilestoneSpec{ProjectRef: projectName},
		}
		Expect(k8sClient.Create(ctx, ms)).To(Succeed())
		Eventually(func() error {
			return mgrClient.Get(ctx, types.NamespacedName{Name: milestoneName, Namespace: "default"}, &tideprojectv1alpha1.Milestone{})
		}, "5s", "100ms").Should(Succeed())

		// Pre-populate envelope reader with a phase ChildCRD.
		phaseSpec := tideprojectv1alpha1.PhaseSpec{MilestoneRef: milestoneName}
		rawSpec, err := json.Marshal(phaseSpec)
		Expect(err).NotTo(HaveOccurred())

		envReader := newMapEnvReader()
		r := &MilestoneReconciler{
			Client:         mgrClient,
			Scheme:         k8sClient.Scheme(),
			Dispatcher:     &stubDispatcher{},
			PlannerPool:    newPlannerPoolForTest(),
			EnvReader:      envReader,
			SubagentImage:  testSubagentImage,
			CredproxyImage: testCredproxyImage,
			SigningKey:     testSigningKey,
		}

		// Drive initial reconciles to create the Job.
		Expect(reconcileWithRetry(r.Reconcile, types.NamespacedName{Name: milestoneName, Namespace: "default"}, 5)).To(Succeed())

		// Fetch Milestone UID for envelope setup. Wait for cache to sync.
		var got tideprojectv1alpha1.Milestone
		Eventually(func() error {
			return mgrClient.Get(ctx, types.NamespacedName{Name: milestoneName, Namespace: "default"}, &got)
		}, "5s", "100ms").Should(Succeed())

		envReader.SetOut(string(got.UID), pkgdispatch.EnvelopeOut{
			APIVersion: pkgdispatch.APIVersionV1Alpha1,
			Kind:       pkgdispatch.KindTaskEnvelopeOut,
			TaskUID:    string(got.UID),
			ExitCode:   0,
			ChildCRDs: []pkgdispatch.ChildCRDSpec{
				{Kind: "Phase", Name: "child-phase-a", Spec: runtime.RawExtension{Raw: rawSpec}},
			},
		})

		// Patch the Job to Succeeded.
		jobName := fmt.Sprintf("tide-milestone-%s-1", got.UID)
		Expect(makeFakeJobTerminal(ctx, mgrClient, jobName, "default", true)).To(Succeed())

		// Reconcile to trigger handleJobCompletion.
		Expect(reconcileWithRetry(r.Reconcile, types.NamespacedName{Name: milestoneName, Namespace: "default"}, 3)).To(Succeed())

		// Verify child Phase created.
		Eventually(func(g Gomega) {
			var phase tideprojectv1alpha1.Phase
			g.Expect(mgrClient.Get(ctx, types.NamespacedName{Name: "child-phase-a", Namespace: "default"}, &phase)).To(Succeed())
			// Owner ref points at Milestone.
			refs := phase.GetOwnerReferences()
			var foundOwner bool
			for _, ref := range refs {
				if ref.Kind == "Milestone" && ref.UID == got.UID {
					foundOwner = true
				}
			}
			g.Expect(foundOwner).To(BeTrue(), "Phase should have Milestone owner ref")
		}, 5*time.Second, 100*time.Millisecond).Should(Succeed())
	})

	It("Test 4 (debug #9): does NOT Succeed while a materialized child Phase is still pending; Succeeds once it Succeeds", func() {
		// The premature-succession bug lives on the AUTO milestone-gate path
		// (the medium sample uses gates.milestone=auto). The BeforeEach Project
		// has no gates → Approve, which parks at AwaitingApproval before the
		// boundary check; create a dedicated auto-gated Project for this case.
		const autoProjectName = "test-proj-ms-auto9"
		const autoMilestoneName = "test-ms-auto9"
		autoProj := &tideprojectv1alpha1.Project{
			ObjectMeta: metav1.ObjectMeta{Name: autoProjectName, Namespace: "default"},
			Spec: tideprojectv1alpha1.ProjectSpec{
				TargetRepo: "https://github.com/example/test.git",
				Subagent:   tideprojectv1alpha1.SubagentConfig{Model: "claude-opus-4-7"},
				Git: &tideprojectv1alpha1.GitConfig{
					RepoURL:        "https://github.com/example/test.git",
					CredsSecretRef: "test-creds",
				},
				Gates: tideprojectv1alpha1.Gates{Milestone: tideprojectv1alpha1.GatePolicy("auto")},
			},
		}
		Expect(k8sClient.Create(ctx, autoProj)).To(Succeed())
		waitForCacheSync(autoProjectName, "default", &tideprojectv1alpha1.Project{})
		DeferCleanup(func() {
			p := &tideprojectv1alpha1.Project{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: autoProjectName, Namespace: "default"}, p); err == nil {
				p.Finalizers = nil
				_ = k8sClient.Update(ctx, p)
				_ = k8sClient.Delete(ctx, p)
			}
			m := &tideprojectv1alpha1.Milestone{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: autoMilestoneName, Namespace: "default"}, m); err == nil {
				m.Finalizers = nil
				_ = k8sClient.Update(ctx, m)
				_ = k8sClient.Delete(ctx, m)
			}
		})

		ms := &tideprojectv1alpha1.Milestone{
			ObjectMeta: metav1.ObjectMeta{Name: autoMilestoneName, Namespace: "default"},
			Spec:       tideprojectv1alpha1.MilestoneSpec{ProjectRef: autoProjectName},
		}
		Expect(k8sClient.Create(ctx, ms)).To(Succeed())
		Eventually(func() error {
			return mgrClient.Get(ctx, types.NamespacedName{Name: autoMilestoneName, Namespace: "default"}, &tideprojectv1alpha1.Milestone{})
		}, "5s", "100ms").Should(Succeed())

		phaseSpec := tideprojectv1alpha1.PhaseSpec{MilestoneRef: autoMilestoneName}
		rawSpec, err := json.Marshal(phaseSpec)
		Expect(err).NotTo(HaveOccurred())

		envReader := newMapEnvReader()
		r := &MilestoneReconciler{
			Client:         mgrClient,
			Scheme:         k8sClient.Scheme(),
			Dispatcher:     &stubDispatcher{},
			PlannerPool:    newPlannerPoolForTest(),
			EnvReader:      envReader,
			SubagentImage:  testSubagentImage,
			CredproxyImage: testCredproxyImage,
			SigningKey:     testSigningKey,
		}

		Expect(reconcileWithRetry(r.Reconcile, types.NamespacedName{Name: autoMilestoneName, Namespace: "default"}, 5)).To(Succeed())

		var got tideprojectv1alpha1.Milestone
		Eventually(func() error {
			return mgrClient.Get(ctx, types.NamespacedName{Name: autoMilestoneName, Namespace: "default"}, &got)
		}, "5s", "100ms").Should(Succeed())

		envReader.SetOut(string(got.UID), pkgdispatch.EnvelopeOut{
			TaskUID:  string(got.UID),
			ExitCode: 0,
			ChildCRDs: []pkgdispatch.ChildCRDSpec{
				{Kind: "Phase", Name: "child-phase-pending", Spec: runtime.RawExtension{Raw: rawSpec}},
			},
		})

		jobName := fmt.Sprintf("tide-milestone-%s-1", got.UID)
		Expect(makeFakeJobTerminal(ctx, mgrClient, jobName, "default", true)).To(Succeed())

		// handleJobCompletion materializes the child Phase. Because the child
		// Phase is NOT yet Succeeded, the Milestone must requeue, NOT Succeed.
		// Drive reconciles (each re-runs handleJobCompletion) until the child
		// Phase materializes; the materializing reconcile also exercises the new
		// requeue guard.
		Eventually(func(g Gomega) {
			res, rerr := r.Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{Name: autoMilestoneName, Namespace: "default"}})
			g.Expect(rerr).NotTo(HaveOccurred())
			var phase tideprojectv1alpha1.Phase
			g.Expect(mgrClient.Get(ctx, types.NamespacedName{Name: "child-phase-pending", Namespace: "default"}, &phase)).To(Succeed())
			// With a materialized-but-pending child Phase, the reconcile must
			// requeue rather than patch the Milestone Succeeded.
			g.Expect(res.RequeueAfter).To(BeNumerically(">", 0), "Milestone should requeue while child Phase pending")
		}, 5*time.Second, 100*time.Millisecond).Should(Succeed())

		// Milestone has NOT reached Succeeded while the child Phase is pending.
		var after tideprojectv1alpha1.Milestone
		Expect(mgrClient.Get(ctx, types.NamespacedName{Name: autoMilestoneName, Namespace: "default"}, &after)).To(Succeed())
		Expect(after.Status.Phase).NotTo(Equal("Succeeded"), "Milestone must not Succeed while child Phase pending")

		// Patch the child Phase to Succeeded, then reconcile: Milestone Succeeds.
		var child tideprojectv1alpha1.Phase
		Expect(mgrClient.Get(ctx, types.NamespacedName{Name: "child-phase-pending", Namespace: "default"}, &child)).To(Succeed())
		patch := client.MergeFrom(child.DeepCopy())
		child.Status.Phase = "Succeeded"
		Expect(mgrClient.Status().Patch(ctx, &child, patch)).To(Succeed())

		Expect(reconcileWithRetry(r.Reconcile, types.NamespacedName{Name: autoMilestoneName, Namespace: "default"}, 3)).To(Succeed())

		Eventually(func(g Gomega) {
			var after tideprojectv1alpha1.Milestone
			g.Expect(mgrClient.Get(ctx, types.NamespacedName{Name: autoMilestoneName, Namespace: "default"}, &after)).To(Succeed())
			g.Expect(after.Status.Phase).To(Equal("Succeeded"))
		}, 5*time.Second, 100*time.Millisecond).Should(Succeed())
	})

	It("Test 3: rejects bad child Kind and patches Status.Phase=Failed", func() {
		ms := &tideprojectv1alpha1.Milestone{
			ObjectMeta: metav1.ObjectMeta{Name: milestoneName, Namespace: "default"},
			Spec:       tideprojectv1alpha1.MilestoneSpec{ProjectRef: projectName},
		}
		Expect(k8sClient.Create(ctx, ms)).To(Succeed())
		Eventually(func() error {
			return mgrClient.Get(ctx, types.NamespacedName{Name: milestoneName, Namespace: "default"}, &tideprojectv1alpha1.Milestone{})
		}, "5s", "100ms").Should(Succeed())

		envReader := newMapEnvReader()
		r := &MilestoneReconciler{
			Client:         mgrClient,
			Scheme:         k8sClient.Scheme(),
			Dispatcher:     &stubDispatcher{},
			PlannerPool:    newPlannerPoolForTest(),
			EnvReader:      envReader,
			SubagentImage:  testSubagentImage,
			CredproxyImage: testCredproxyImage,
			SigningKey:     testSigningKey,
		}

		Expect(reconcileWithRetry(r.Reconcile, types.NamespacedName{Name: milestoneName, Namespace: "default"}, 5)).To(Succeed())

		var got tideprojectv1alpha1.Milestone
		Expect(mgrClient.Get(ctx, types.NamespacedName{Name: milestoneName, Namespace: "default"}, &got)).To(Succeed())

		// Bad Kind: "Pod" — not in allowlist.
		envReader.SetOut(string(got.UID), pkgdispatch.EnvelopeOut{
			TaskUID:  string(got.UID),
			ExitCode: 0,
			ChildCRDs: []pkgdispatch.ChildCRDSpec{
				{Kind: "Pod", Name: "evil", Spec: runtime.RawExtension{Raw: []byte(`{}`)}},
			},
		})

		jobName := fmt.Sprintf("tide-milestone-%s-1", got.UID)
		Expect(makeFakeJobTerminal(ctx, mgrClient, jobName, "default", true)).To(Succeed())

		Expect(reconcileWithRetry(r.Reconcile, types.NamespacedName{Name: milestoneName, Namespace: "default"}, 3)).To(Succeed())

		Eventually(func(g Gomega) {
			var msAfter tideprojectv1alpha1.Milestone
			g.Expect(mgrClient.Get(ctx, types.NamespacedName{Name: milestoneName, Namespace: "default"}, &msAfter)).To(Succeed())
			g.Expect(msAfter.Status.Phase).To(Equal("Failed"))
		}, 5*time.Second, 100*time.Millisecond).Should(Succeed())
	})
})
