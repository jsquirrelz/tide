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
	"fmt"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	tideprojectv1alpha1 "github.com/jsquirrelz/tide/api/v1alpha2"
	"github.com/jsquirrelz/tide/internal/gates"
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
			SubagentImage:  testSubagentImage, // dead since Phase 13; HelmProviderDefaults.Image is the default tier
			CredproxyImage: testCredproxyImage,
			SigningKey:     testSigningKey,
			HelmProviderDefaults: ProviderDefaults{
				Image: testSubagentImage,
			},
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

	// NOTE (Phase 09 plan 09-06, REQ-09-01): the former "Test 2: materializes
	// Phase children" and "Test 3: rejects bad child Kind" specs were removed.
	// Under the Option-C reader-Job architecture the Manager no longer
	// materializes children inline in handleJobCompletion — that work moved to
	// the in-namespace tide-reporter Job. Their coverage now lives in:
	//   - internal/reporter/materialize_test.go
	//       TestMaterializeChildCRDsHappyPath        (child create + ownerRef)
	//       TestMaterializeChildCRDsRejectsUnknownKind (Kind allowlist / bad Kind)
	//   - test/integration/kind/reporter_pod_test.go  (Manager spawns reporter
	//       Job → child Milestone/Phase appears, Layer B)
	// The Manager-level invariant that still belongs here — "do not Succeed
	// while a child Phase is pending" (debug #9) — is retained below, with the
	// child Phase created directly (simulating what the reporter Job does)
	// instead of relying on the removed inline materialization.

	// Test 5 (plan 09-08 Defect C): planner-level Usage is rolled up to
	// Project.Status.Budget.CostSpentCents after planner Job completes with
	// non-zero EstimatedCostCents. Guards that the "budget = {}" bug does not
	// recur (budget was never populated because the four planner controllers
	// discarded the EnvelopeOut returned by ReadOut).
	It("Test 5 (09-08 Defect C): rolls up planner Usage to Project.Status.Budget on Job completion", func() {
		const budgetProjectName = "test-proj-ms-budget5"
		const budgetMilestoneName = "test-ms-budget5"
		budgetProj := &tideprojectv1alpha1.Project{
			ObjectMeta: metav1.ObjectMeta{Name: budgetProjectName, Namespace: "default"},
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
		Expect(k8sClient.Create(ctx, budgetProj)).To(Succeed())
		waitForCacheSync(budgetProjectName, "default", &tideprojectv1alpha1.Project{})
		DeferCleanup(func() {
			ms := &tideprojectv1alpha1.Milestone{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: budgetMilestoneName, Namespace: "default"}, ms); err == nil {
				ms.Finalizers = nil
				_ = k8sClient.Update(ctx, ms)
				_ = k8sClient.Delete(ctx, ms)
			}
			p := &tideprojectv1alpha1.Project{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: budgetProjectName, Namespace: "default"}, p); err == nil {
				p.Finalizers = nil
				_ = k8sClient.Update(ctx, p)
				_ = k8sClient.Delete(ctx, p)
			}
		})

		ms := &tideprojectv1alpha1.Milestone{
			ObjectMeta: metav1.ObjectMeta{Name: budgetMilestoneName, Namespace: "default"},
			Spec:       tideprojectv1alpha1.MilestoneSpec{ProjectRef: budgetProjectName},
		}
		Expect(k8sClient.Create(ctx, ms)).To(Succeed())
		Eventually(func() error {
			return mgrClient.Get(ctx, types.NamespacedName{Name: budgetMilestoneName, Namespace: "default"}, &tideprojectv1alpha1.Milestone{})
		}, "5s", "100ms").Should(Succeed())

		envReader := newMapEnvReader()
		r := &MilestoneReconciler{
			Client:         mgrClient,
			Scheme:         k8sClient.Scheme(),
			Dispatcher:     &stubDispatcher{},
			PlannerPool:    newPlannerPoolForTest(),
			EnvReader:      envReader,
			SubagentImage:  testSubagentImage, // dead since Phase 13; HelmProviderDefaults.Image is the default tier
			CredproxyImage: testCredproxyImage,
			SigningKey:     testSigningKey,
			HelmProviderDefaults: ProviderDefaults{
				Image: testSubagentImage,
			},
		}

		Expect(reconcileWithRetry(r.Reconcile, types.NamespacedName{Name: budgetMilestoneName, Namespace: "default"}, 5)).To(Succeed())

		var got tideprojectv1alpha1.Milestone
		Eventually(func() error {
			return mgrClient.Get(ctx, types.NamespacedName{Name: budgetMilestoneName, Namespace: "default"}, &got)
		}, "5s", "100ms").Should(Succeed())

		// Set ChildCount=0 (leaf milestone) and a non-zero EstimatedCostCents.
		const plannerCostCents = int64(7)
		envReader.SetOut(string(got.UID), pkgdispatch.EnvelopeOut{
			TaskUID:    string(got.UID),
			ExitCode:   0,
			ChildCount: 0, // leaf
			Usage: pkgdispatch.Usage{
				InputTokens:        1000,
				OutputTokens:       200,
				EstimatedCostCents: plannerCostCents,
			},
		})

		jobName := fmt.Sprintf("tide-milestone-%s-1", got.UID)
		Expect(makeFakeJobTerminal(ctx, mgrClient, jobName, "default", true)).To(Succeed())

		// Reconcile: ChildCount=0 (leaf) → patchMilestoneSucceeded and rollup.
		Expect(reconcileWithRetry(r.Reconcile, types.NamespacedName{Name: budgetMilestoneName, Namespace: "default"}, 3)).To(Succeed())

		// Assert Project.Status.Budget.CostSpentCents >= plannerCostCents.
		Eventually(func(g Gomega) {
			var proj tideprojectv1alpha1.Project
			g.Expect(mgrClient.Get(ctx, types.NamespacedName{Name: budgetProjectName, Namespace: "default"}, &proj)).To(Succeed())
			g.Expect(proj.Status.Budget.CostSpentCents).To(BeNumerically(">=", plannerCostCents),
				"Project.Status.Budget.CostSpentCents must reflect planner spend (Defect C regression guard)")
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

		envReader := newMapEnvReader()
		r := &MilestoneReconciler{
			Client:         mgrClient,
			Scheme:         k8sClient.Scheme(),
			Dispatcher:     &stubDispatcher{},
			PlannerPool:    newPlannerPoolForTest(),
			EnvReader:      envReader,
			SubagentImage:  testSubagentImage, // dead since Phase 13; HelmProviderDefaults.Image is the default tier
			CredproxyImage: testCredproxyImage,
			SigningKey:     testSigningKey,
			HelmProviderDefaults: ProviderDefaults{
				Image: testSubagentImage,
			},
		}

		Expect(reconcileWithRetry(r.Reconcile, types.NamespacedName{Name: autoMilestoneName, Namespace: "default"}, 5)).To(Succeed())

		var got tideprojectv1alpha1.Milestone
		Eventually(func() error {
			return mgrClient.Get(ctx, types.NamespacedName{Name: autoMilestoneName, Namespace: "default"}, &got)
		}, "5s", "100ms").Should(Succeed())

		// Manager reads only the tiny status from the completed planner Job
		// (no ChildCRDs — materialization moved to the reporter Job, REQ-09-01).
		// Plan 09-08: set ChildCount=1 so the uniform ChildCount gate expects 1
		// child Phase; without it the gate would treat the milestone as a leaf and
		// Succeed immediately without waiting for the reporter to materialize the child.
		envReader.SetOut(string(got.UID), pkgdispatch.EnvelopeOut{
			TaskUID:    string(got.UID),
			ExitCode:   0,
			ChildCount: 1,
		})

		jobName := fmt.Sprintf("tide-milestone-%s-1", got.UID)
		Expect(makeFakeJobTerminal(ctx, mgrClient, jobName, "default", true)).To(Succeed())

		// Simulate the reporter Job: create the child Phase as a controller-owned
		// child of the Milestone, still PENDING (Status.Phase unset). Under Option C
		// the in-namespace tide-reporter materializes this from out.json; the Manager
		// only observes it via the Owns(&Phase{}) watch.
		tru := true
		childPhase := &tideprojectv1alpha1.Phase{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "child-phase-pending",
				Namespace: "default",
				OwnerReferences: []metav1.OwnerReference{{
					APIVersion:         "tideproject.k8s/v1alpha1",
					Kind:               "Milestone",
					Name:               got.GetName(),
					UID:                got.GetUID(),
					Controller:         &tru,
					BlockOwnerDeletion: &tru,
				}},
			},
			Spec: tideprojectv1alpha1.PhaseSpec{MilestoneRef: autoMilestoneName},
		}
		Expect(k8sClient.Create(ctx, childPhase)).To(Succeed())
		waitForCacheSync("child-phase-pending", "default", &tideprojectv1alpha1.Phase{})

		// With a materialized-but-pending child Phase, handleJobCompletion must
		// requeue rather than patch the Milestone Succeeded (debug #9 guard).
		Eventually(func(g Gomega) {
			res, rerr := r.Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{Name: autoMilestoneName, Namespace: "default"}})
			g.Expect(rerr).NotTo(HaveOccurred())
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

})

var _ = Describe("MilestoneReconciler — D-03 project-label backfill (CUTS-01)", Label("envtest"), func() {
	ctx := context.Background()

	It("backfills tideproject.k8s/project on a Milestone that was created without the label, and is idempotent on second reconcile", func() {
		const projName = "backfill-proj-ms"
		const msName = "backfill-ms-01"

		// Create parent Project (no special labels needed — the project name is enough).
		proj := &tideprojectv1alpha1.Project{
			ObjectMeta: metav1.ObjectMeta{Name: projName, Namespace: "default"},
			Spec: tideprojectv1alpha1.ProjectSpec{
				TargetRepo: "https://github.com/example/test.git",
				Subagent:   tideprojectv1alpha1.SubagentConfig{Model: "claude-opus-4-7"},
			},
		}
		Expect(k8sClient.Create(ctx, proj)).To(Succeed())
		waitForCacheSync(projName, "default", &tideprojectv1alpha1.Project{})

		// Create Milestone WITHOUT the tideproject.k8s/project label (simulating a
		// pre-Phase-15 / run-1 CR created by the reporter before D-01 was in place).
		ms := &tideprojectv1alpha1.Milestone{
			ObjectMeta: metav1.ObjectMeta{
				Name:      msName,
				Namespace: "default",
				// Labels intentionally absent — this is the pre-Phase-15 shape.
			},
			Spec: tideprojectv1alpha1.MilestoneSpec{ProjectRef: projName},
		}
		Expect(k8sClient.Create(ctx, ms)).To(Succeed())
		waitForCacheSync(msName, "default", &tideprojectv1alpha1.Milestone{})

		r := &MilestoneReconciler{
			Client: mgrClient,
			Scheme: k8sClient.Scheme(),
			// No Dispatcher — drives steps 1-5 without planner dispatch.
		}

		// First reconcile: finalizer, owner-ref, then backfill.
		Expect(reconcileWithRetry(r.Reconcile, types.NamespacedName{Name: msName, Namespace: "default"}, 5)).To(Succeed())

		// Assert the project label was backfilled.
		var after tideprojectv1alpha1.Milestone
		Expect(mgrClient.Get(ctx, types.NamespacedName{Name: msName, Namespace: "default"}, &after)).To(Succeed())
		Expect(after.Labels["tideproject.k8s/project"]).To(Equal(projName),
			"backfill must stamp tideproject.k8s/project from the OwnerRef chain")

		// Idempotency: record ResourceVersion, reconcile again, verify unchanged.
		rvBefore := after.ResourceVersion
		Expect(reconcileWithRetry(r.Reconcile, types.NamespacedName{Name: msName, Namespace: "default"}, 2)).To(Succeed())
		var after2 tideprojectv1alpha1.Milestone
		Expect(mgrClient.Get(ctx, types.NamespacedName{Name: msName, Namespace: "default"}, &after2)).To(Succeed())
		Expect(after2.ResourceVersion).To(Equal(rvBefore),
			"second reconcile must not patch the object (idempotent backfill)")

		// Cleanup.
		after2.Finalizers = nil
		_ = k8sClient.Update(ctx, &after2)
		_ = k8sClient.Delete(ctx, &after2)
		proj2 := &tideprojectv1alpha1.Project{}
		if err := k8sClient.Get(ctx, types.NamespacedName{Name: projName, Namespace: "default"}, proj2); err == nil {
			proj2.Finalizers = nil
			_ = k8sClient.Update(ctx, proj2)
			_ = k8sClient.Delete(ctx, proj2)
		}
	})
})

// MilestoneReconciler — DEBT-02 (WR-10): reject short-circuit fires before
// the envelope read and reporter spawn.
//
// This Describe block contains the regression spec that asserts the fix for
// DEBT-02 on the milestone level: when a Project carries the reject annotation,
// MilestoneReconciler.handleJobCompletion must park the Milestone Rejected WITHOUT
// spawning a new tide-reporter Job (T-17-04). The milestone reject check was moved
// to immediately after projectUID derivation in Phase 12 (commit be82c7e); this
// spec is the regression guard ensuring it stays before both the envelope read and
// the reporter spawn.
var _ = Describe("MilestoneReconciler — DEBT-02 reject short-circuit before reporter spawn", Label("envtest"), func() {
	ctx := context.Background()

	It("parks Milestone Rejected and creates zero tide-reporter Jobs when Project carries the reject annotation", func() {
		const projName = "reject-proj-ms-d02"
		const msName = "reject-ms-d02"

		// Create Project with the reject annotation (simulates `tide reject`).
		proj := &tideprojectv1alpha1.Project{
			ObjectMeta: metav1.ObjectMeta{
				Name:      projName,
				Namespace: "default",
				Annotations: map[string]string{
					gates.AnnotationReject: "operator halt ms test",
				},
			},
			Spec: tideprojectv1alpha1.ProjectSpec{
				TargetRepo: "https://github.com/example/test.git",
				Subagent:   tideprojectv1alpha1.SubagentConfig{Model: "claude-opus-4-7"},
				Git: &tideprojectv1alpha1.GitConfig{
					RepoURL:        "https://github.com/example/test.git",
					CredsSecretRef: "test-creds",
				},
			},
		}
		Expect(k8sClient.Create(ctx, proj)).To(Succeed())
		waitForCacheSync(projName, "default", &tideprojectv1alpha1.Project{})

		// Wait until the reject annotation is visible via the manager's cached client.
		Eventually(func() string {
			var p tideprojectv1alpha1.Project
			if err := mgrClient.Get(ctx, types.NamespacedName{Name: projName, Namespace: "default"}, &p); err != nil {
				return ""
			}
			return p.Annotations[gates.AnnotationReject]
		}, 5*time.Second, 50*time.Millisecond).Should(Equal("operator halt ms test"))

		ms := &tideprojectv1alpha1.Milestone{
			ObjectMeta: metav1.ObjectMeta{Name: msName, Namespace: "default"},
			Spec:       tideprojectv1alpha1.MilestoneSpec{ProjectRef: projName},
		}
		Expect(k8sClient.Create(ctx, ms)).To(Succeed())
		waitForCacheSync(msName, "default", &tideprojectv1alpha1.Milestone{})

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
			HelmProviderDefaults: ProviderDefaults{
				Image: testSubagentImage,
			},
		}

		// Drive through to handleJobCompletion: dispatch, fake planner Job terminal,
		// then reconcile again. The dispatch-entry reject hold may fire first (parked
		// before planner Job is created) — handle both paths.
		Expect(reconcileWithRetry(r.Reconcile, types.NamespacedName{Name: msName, Namespace: "default"}, 5)).To(Succeed())

		var got tideprojectv1alpha1.Milestone
		Eventually(func() error {
			return mgrClient.Get(ctx, types.NamespacedName{Name: msName, Namespace: "default"}, &got)
		}, 5*time.Second, 50*time.Millisecond).Should(Succeed())

		jobName := fmt.Sprintf("tide-milestone-%s-1", got.UID)
		var plannerJob batchv1.Job
		if err := mgrClient.Get(ctx, types.NamespacedName{Name: jobName, Namespace: "default"}, &plannerJob); err == nil {
			// Planner Job exists — fake it complete to trigger handleJobCompletion.
			envReader.SetOut(string(got.UID), pkgdispatch.EnvelopeOut{TaskUID: string(got.UID), ExitCode: 0})
			Expect(makeFakeJobTerminal(ctx, mgrClient, jobName, "default", true)).To(Succeed())
			Expect(reconcileWithRetry(r.Reconcile, types.NamespacedName{Name: msName, Namespace: "default"}, 3)).To(Succeed())
		}

		// Load-bearing assertion (a): Milestone must be parked with RejectedByUser condition.
		Eventually(func(g Gomega) {
			var after tideprojectv1alpha1.Milestone
			g.Expect(mgrClient.Get(ctx, types.NamespacedName{Name: msName, Namespace: "default"}, &after)).To(Succeed())
			c := meta.FindStatusCondition(after.Status.Conditions, tideprojectv1alpha1.ConditionWaveOrLevelPaused)
			g.Expect(c).NotTo(BeNil(), "ConditionWaveOrLevelPaused must be set when parked Rejected")
			g.Expect(c.Reason).To(Equal(tideprojectv1alpha1.ReasonRejectedByUser),
				"Milestone must be parked with RejectedByUser reason (D-05)")
			g.Expect(c.Message).To(ContainSubstring("operator halt ms test"),
				"reject reason must be propagated to the condition message")
		}, 5*time.Second, 100*time.Millisecond).Should(Succeed())

		// Load-bearing assertion (b): NO tide-reporter-<milestone-uid> Job must exist
		// (assert NONE created, not that one was deleted — Pitfall 3 / T-17-04 / T-17-05).
		var got2 tideprojectv1alpha1.Milestone
		Expect(mgrClient.Get(ctx, types.NamespacedName{Name: msName, Namespace: "default"}, &got2)).To(Succeed())
		reporterJobName := fmt.Sprintf("tide-reporter-%s", got2.UID)
		var reporterJob batchv1.Job
		getErr := mgrClient.Get(ctx, types.NamespacedName{Name: reporterJobName, Namespace: "default"}, &reporterJob)
		Expect(getErr).To(HaveOccurred(),
			"no tide-reporter Job must be created when Project is rejected (T-17-04)")
		Expect(getErr.Error()).To(ContainSubstring("not found"),
			"reporter Job absence must be a not-found error, not some other error")

		// Cleanup.
		DeferCleanup(func() {
			msObj := &tideprojectv1alpha1.Milestone{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: msName, Namespace: "default"}, msObj); err == nil {
				msObj.Finalizers = nil
				_ = k8sClient.Update(ctx, msObj)
				_ = k8sClient.Delete(ctx, msObj)
			}
			projObj := &tideprojectv1alpha1.Project{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: projName, Namespace: "default"}, projObj); err == nil {
				projObj.Finalizers = nil
				_ = k8sClient.Update(ctx, projObj)
				_ = k8sClient.Delete(ctx, projObj)
			}
			var jobs batchv1.JobList
			_ = k8sClient.List(ctx, &jobs, client.InNamespace("default"))
			for i := range jobs.Items {
				j := jobs.Items[i]
				_ = k8sClient.Delete(ctx, &j)
			}
		})
	})
})
