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
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	batchv1 "k8s.io/api/batch/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	tideprojectv1alpha2 "github.com/jsquirrelz/tide/api/v1alpha2"
	"github.com/jsquirrelz/tide/internal/gates"
	pkgdispatch "github.com/jsquirrelz/tide/pkg/dispatch"
)

var _ = Describe("PhaseReconciler — planner dispatch", Label("envtest", "phase3"), func() {
	const projectName = "test-proj-ph"
	const milestoneName = "test-ms-ph"
	const phaseName = "test-phase-1"
	ctx := context.Background()

	BeforeEach(func() {
		proj := &tideprojectv1alpha2.Project{
			ObjectMeta: metav1.ObjectMeta{Name: projectName, Namespace: "default"},
			Spec: tideprojectv1alpha2.ProjectSpec{SchemaRevision: "v1alpha2",
				TargetRepo: "https://github.com/example/test.git",
				Subagent: tideprojectv1alpha2.SubagentConfig{
					Model: "claude-sonnet-4-6",
				},
				Git: &tideprojectv1alpha2.GitConfig{
					RepoURL:        "https://github.com/example/test.git",
					CredsSecretRef: "test-creds",
				},
			},
		}
		Expect(k8sClient.Create(ctx, proj)).To(Succeed())
		waitForCacheSync(projectName, "default", &tideprojectv1alpha2.Project{})
		ms := &tideprojectv1alpha2.Milestone{
			ObjectMeta: metav1.ObjectMeta{Name: milestoneName, Namespace: "default"},
			Spec:       tideprojectv1alpha2.MilestoneSpec{ProjectRef: projectName},
		}
		Expect(k8sClient.Create(ctx, ms)).To(Succeed())
		waitForCacheSync(milestoneName, "default", &tideprojectv1alpha2.Milestone{})
	})

	AfterEach(func() {
		ph := &tideprojectv1alpha2.Phase{}
		if err := k8sClient.Get(ctx, types.NamespacedName{Name: phaseName, Namespace: "default"}, ph); err == nil {
			ph.Finalizers = nil
			_ = k8sClient.Update(ctx, ph)
			_ = k8sClient.Delete(ctx, ph)
		}
		ms := &tideprojectv1alpha2.Milestone{}
		if err := k8sClient.Get(ctx, types.NamespacedName{Name: milestoneName, Namespace: "default"}, ms); err == nil {
			ms.Finalizers = nil
			_ = k8sClient.Update(ctx, ms)
			_ = k8sClient.Delete(ctx, ms)
		}
		proj := &tideprojectv1alpha2.Project{}
		if err := k8sClient.Get(ctx, types.NamespacedName{Name: projectName, Namespace: "default"}, proj); err == nil {
			proj.Finalizers = nil
			_ = k8sClient.Update(ctx, proj)
			_ = k8sClient.Delete(ctx, proj)
		}
		var jobs batchv1.JobList
		_ = k8sClient.List(ctx, &jobs)
		for i := range jobs.Items {
			j := jobs.Items[i]
			_ = k8sClient.Delete(ctx, &j)
		}
	})

	// Test 5 (plan 09-08 Defect B): Phase does NOT Succeed while its reporter has
	// not yet materialized the expected child Plans (observed < expected).
	// This is the exact race that caused Project=Complete while Plans=Running in the
	// 09-07 live acceptance: the phase_controller had NO guard and fell straight through
	// to patchPhaseSucceeded. With the ChildCount gate, the Phase must requeue (not
	// Succeed) until observed >= expected AND all Plans are Succeeded.
	It("Test 5 (09-08 Defect B): does NOT Succeed while expected Plans not yet materialized; Succeeds once they do", func() {
		const autoProjectName = "test-proj-ph-auto5"
		const autoMilestoneName = "test-ms-ph-auto5"
		const autoPhaseName = "test-phase-auto5"
		autoProj := &tideprojectv1alpha2.Project{
			ObjectMeta: metav1.ObjectMeta{Name: autoProjectName, Namespace: "default"},
			Spec: tideprojectv1alpha2.ProjectSpec{SchemaRevision: "v1alpha2",
				TargetRepo: "https://github.com/example/test.git",
				Subagent:   tideprojectv1alpha2.SubagentConfig{Model: "claude-opus-4-7"},
				Git: &tideprojectv1alpha2.GitConfig{
					RepoURL:        "https://github.com/example/test.git",
					CredsSecretRef: "test-creds",
				},
				// No gates → auto-pass so Phase can proceed without an approval gate stop.
			},
		}
		Expect(k8sClient.Create(ctx, autoProj)).To(Succeed())
		waitForCacheSync(autoProjectName, "default", &tideprojectv1alpha2.Project{})
		autoMs := &tideprojectv1alpha2.Milestone{
			ObjectMeta: metav1.ObjectMeta{Name: autoMilestoneName, Namespace: "default"},
			Spec:       tideprojectv1alpha2.MilestoneSpec{ProjectRef: autoProjectName},
		}
		Expect(k8sClient.Create(ctx, autoMs)).To(Succeed())
		waitForCacheSync(autoMilestoneName, "default", &tideprojectv1alpha2.Milestone{})
		DeferCleanup(func() {
			cleanObj := func(obj client.Object) {
				if err := k8sClient.Get(ctx, types.NamespacedName{Name: obj.GetName(), Namespace: "default"}, obj); err == nil {
					obj.SetFinalizers(nil)
					_ = k8sClient.Update(ctx, obj)
					_ = k8sClient.Delete(ctx, obj)
				}
			}
			cleanObj(&tideprojectv1alpha2.Plan{ObjectMeta: metav1.ObjectMeta{Name: "child-plan-ph5-pending", Namespace: "default"}})
			cleanObj(&tideprojectv1alpha2.Phase{ObjectMeta: metav1.ObjectMeta{Name: autoPhaseName, Namespace: "default"}})
			cleanObj(&tideprojectv1alpha2.Milestone{ObjectMeta: metav1.ObjectMeta{Name: autoMilestoneName, Namespace: "default"}})
			cleanObj(&tideprojectv1alpha2.Project{ObjectMeta: metav1.ObjectMeta{Name: autoProjectName, Namespace: "default"}})
		})

		autoPhase := &tideprojectv1alpha2.Phase{
			ObjectMeta: metav1.ObjectMeta{Name: autoPhaseName, Namespace: "default"},
			Spec:       tideprojectv1alpha2.PhaseSpec{MilestoneRef: autoMilestoneName},
		}
		Expect(k8sClient.Create(ctx, autoPhase)).To(Succeed())
		Eventually(func() error {
			return mgrClient.Get(ctx, types.NamespacedName{Name: autoPhaseName, Namespace: "default"}, &tideprojectv1alpha2.Phase{})
		}, "5s", "100ms").Should(Succeed())

		envReader := newMapEnvReader()
		r := &PhaseReconciler{
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

		Expect(reconcileWithRetry(r.Reconcile, types.NamespacedName{Name: autoPhaseName, Namespace: "default"}, 5)).To(Succeed())

		var got tideprojectv1alpha2.Phase
		Eventually(func() error {
			return mgrClient.Get(ctx, types.NamespacedName{Name: autoPhaseName, Namespace: "default"}, &got)
		}, "5s", "100ms").Should(Succeed())

		// Set ChildCount=1: the planner authored 1 Plan child. This simulates the tiny
		// status the PodStatusEnvelopeReader would read from the termination message.
		envReader.SetOut(string(got.UID), pkgdispatch.EnvelopeOut{
			TaskUID:    string(got.UID),
			ExitCode:   0,
			ChildCount: 1,
		})

		jobName := fmt.Sprintf("tide-phase-%s-1", got.UID)
		Expect(makeFakeJobTerminal(ctx, mgrClient, jobName, "default", true)).To(Succeed())

		// Gate assertion 1: with expected=1 and 0 Plans materialized yet, the Phase
		// must requeue — NOT Succeed. This is the premature-succession race fix.
		Eventually(func(g Gomega) {
			res, rerr := r.Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{Name: autoPhaseName, Namespace: "default"}})
			g.Expect(rerr).NotTo(HaveOccurred())
			g.Expect(res.RequeueAfter).To(BeNumerically(">", 0),
				"Phase must requeue while observed Plans < expected (premature-succession guard)")
		}, 5*time.Second, 100*time.Millisecond).Should(Succeed())

		var afterNoPlan tideprojectv1alpha2.Phase
		Expect(mgrClient.Get(ctx, types.NamespacedName{Name: autoPhaseName, Namespace: "default"}, &afterNoPlan)).To(Succeed())
		Expect(afterNoPlan.Status.Phase).NotTo(Equal("Succeeded"),
			"Phase must not Succeed when no Plans materialized yet (Defect B regression guard)")

		// Simulate reporter materializing the child Plan (still Pending).
		tru := true
		childPlan := &tideprojectv1alpha2.Plan{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "child-plan-ph5-pending",
				Namespace: "default",
				OwnerReferences: []metav1.OwnerReference{{
					APIVersion:         "tideproject.k8s/v1alpha1",
					Kind:               "Phase",
					Name:               got.GetName(),
					UID:                got.GetUID(),
					Controller:         &tru,
					BlockOwnerDeletion: &tru,
				}},
			},
			Spec: tideprojectv1alpha2.PlanSpec{PhaseRef: autoPhaseName},
		}
		Expect(k8sClient.Create(ctx, childPlan)).To(Succeed())
		waitForCacheSync("child-plan-ph5-pending", "default", &tideprojectv1alpha2.Plan{})

		// Gate assertion 2: observed=1 >= expected=1, but Plan is not yet Succeeded →
		// Phase must still requeue (BoundaryDetected returns false).
		Eventually(func(g Gomega) {
			res, rerr := r.Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{Name: autoPhaseName, Namespace: "default"}})
			g.Expect(rerr).NotTo(HaveOccurred())
			g.Expect(res.RequeueAfter).To(BeNumerically(">", 0),
				"Phase must requeue while child Plans exist but are not yet Succeeded")
		}, 5*time.Second, 100*time.Millisecond).Should(Succeed())

		// Gate assertion 3: patch the child Plan to Succeeded → Phase now Succeeds.
		var latestPlan tideprojectv1alpha2.Plan
		Expect(mgrClient.Get(ctx, types.NamespacedName{Name: "child-plan-ph5-pending", Namespace: "default"}, &latestPlan)).To(Succeed())
		planPatch := client.MergeFrom(latestPlan.DeepCopy())
		latestPlan.Status.Phase = "Succeeded"
		Expect(mgrClient.Status().Patch(ctx, &latestPlan, planPatch)).To(Succeed())

		Expect(reconcileWithRetry(r.Reconcile, types.NamespacedName{Name: autoPhaseName, Namespace: "default"}, 3)).To(Succeed())

		Eventually(func(g Gomega) {
			var final tideprojectv1alpha2.Phase
			g.Expect(mgrClient.Get(ctx, types.NamespacedName{Name: autoPhaseName, Namespace: "default"}, &final)).To(Succeed())
			g.Expect(final.Status.Phase).To(Equal("Succeeded"))
		}, 5*time.Second, 100*time.Millisecond).Should(Succeed())
	})

	It("Test 4: dispatches planner Job tide-phase-<uid>-1 and patches Status.Phase=Running", func() {
		phase := &tideprojectv1alpha2.Phase{
			ObjectMeta: metav1.ObjectMeta{Name: phaseName, Namespace: "default"},
			Spec:       tideprojectv1alpha2.PhaseSpec{MilestoneRef: milestoneName},
		}
		Expect(k8sClient.Create(ctx, phase)).To(Succeed())
		Eventually(func() error {
			return mgrClient.Get(ctx, types.NamespacedName{Name: phaseName, Namespace: "default"}, &tideprojectv1alpha2.Phase{})
		}, "5s", "100ms").Should(Succeed())

		r := &PhaseReconciler{
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

		Expect(reconcileWithRetry(r.Reconcile, types.NamespacedName{Name: phaseName, Namespace: "default"}, 5)).To(Succeed())

		Eventually(func(g Gomega) {
			var got tideprojectv1alpha2.Phase
			g.Expect(mgrClient.Get(ctx, types.NamespacedName{Name: phaseName, Namespace: "default"}, &got)).To(Succeed())
			expectedJobName := fmt.Sprintf("tide-phase-%s-1", got.UID)
			var job batchv1.Job
			g.Expect(mgrClient.Get(ctx, types.NamespacedName{Name: expectedJobName, Namespace: "default"}, &job)).To(Succeed())
			g.Expect(got.Status.Phase).To(Equal("Running"))
		}, 5*time.Second, 100*time.Millisecond).Should(Succeed())
	})
})

// PhaseReconciler — DEBT-02 (WR-10): reject short-circuit fires before reporter spawn
//
// This Describe block contains the regression spec that asserts the fix for DEBT-02:
// when a Project carries the reject annotation, PhaseReconciler.handleJobCompletion
// must park the Phase Rejected WITHOUT spawning a new tide-reporter Job. Prior to
// the fix, spawnReporterIfNeeded ran before gates.CheckRejected, so a rejected
// Project's completing planner Job still launched a reporter Job (T-17-04).
var _ = Describe("PhaseReconciler — DEBT-02 reject short-circuit before reporter spawn", Label("envtest"), func() {
	ctx := context.Background()

	It("parks Phase Rejected and creates zero tide-reporter Jobs when Project carries the reject annotation", func() {
		const projName = "reject-proj-ph-d02"
		const msName = "reject-ms-ph-d02"
		const phName = "reject-phase-d02"

		// Create Project with the reject annotation (simulates `tide reject`).
		proj := &tideprojectv1alpha2.Project{
			ObjectMeta: metav1.ObjectMeta{
				Name:      projName,
				Namespace: "default",
				Annotations: map[string]string{
					gates.AnnotationReject: "operator halt test",
				},
			},
			Spec: tideprojectv1alpha2.ProjectSpec{SchemaRevision: "v1alpha2",
				TargetRepo: "https://github.com/example/test.git",
				Subagent:   tideprojectv1alpha2.SubagentConfig{Model: "claude-opus-4-7"},
				Git: &tideprojectv1alpha2.GitConfig{
					RepoURL:        "https://github.com/example/test.git",
					CredsSecretRef: "test-creds",
				},
			},
		}
		Expect(k8sClient.Create(ctx, proj)).To(Succeed())
		waitForCacheSync(projName, "default", &tideprojectv1alpha2.Project{})

		// Wait until the reject annotation is visible via the manager's cached client.
		Eventually(func() string {
			var p tideprojectv1alpha2.Project
			if err := mgrClient.Get(ctx, types.NamespacedName{Name: projName, Namespace: "default"}, &p); err != nil {
				return ""
			}
			return p.Annotations[gates.AnnotationReject]
		}, 5*time.Second, 50*time.Millisecond).Should(Equal("operator halt test"))

		// Create Milestone and Phase hierarchy.
		ms := &tideprojectv1alpha2.Milestone{
			ObjectMeta: metav1.ObjectMeta{Name: msName, Namespace: "default"},
			Spec:       tideprojectv1alpha2.MilestoneSpec{ProjectRef: projName},
		}
		Expect(k8sClient.Create(ctx, ms)).To(Succeed())
		waitForCacheSync(msName, "default", &tideprojectv1alpha2.Milestone{})

		ph := &tideprojectv1alpha2.Phase{
			ObjectMeta: metav1.ObjectMeta{Name: phName, Namespace: "default"},
			Spec:       tideprojectv1alpha2.PhaseSpec{MilestoneRef: msName},
		}
		Expect(k8sClient.Create(ctx, ph)).To(Succeed())
		waitForCacheSync(phName, "default", &tideprojectv1alpha2.Phase{})

		envReader := newMapEnvReader()
		r := &PhaseReconciler{
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

		// Drive through to handleJobCompletion: first drive reconcile to dispatch the
		// planner Job, fake it terminal, then reconcile again to trigger handleJobCompletion.
		Expect(reconcileWithRetry(r.Reconcile, types.NamespacedName{Name: phName, Namespace: "default"}, 5)).To(Succeed())

		var got tideprojectv1alpha2.Phase
		Eventually(func() error {
			return mgrClient.Get(ctx, types.NamespacedName{Name: phName, Namespace: "default"}, &got)
		}, 5*time.Second, 50*time.Millisecond).Should(Succeed())

		// The planner Job may or may not be created (the dispatch-entry hold may fire
		// first on the Pending path since the reject annotation is already present).
		// Either way: drive handleJobCompletion directly by patching the Job terminal if
		// one was created, or call handleJobCompletion directly via reconcile if parked.
		jobName := fmt.Sprintf("tide-phase-%s-1", got.UID)
		var plannerJob batchv1.Job
		if err := mgrClient.Get(ctx, types.NamespacedName{Name: jobName, Namespace: "default"}, &plannerJob); err == nil {
			// Planner Job exists — fake it complete to enter handleJobCompletion.
			envReader.SetOut(string(got.UID), pkgdispatch.EnvelopeOut{TaskUID: string(got.UID), ExitCode: 0})
			Expect(makeFakeJobTerminal(ctx, mgrClient, jobName, "default", true)).To(Succeed())
			Expect(reconcileWithRetry(r.Reconcile, types.NamespacedName{Name: phName, Namespace: "default"}, 3)).To(Succeed())
		}

		// Load-bearing assertion (a): Phase must be parked Rejected (RejectedByUser condition).
		Eventually(func(g Gomega) {
			var after tideprojectv1alpha2.Phase
			g.Expect(mgrClient.Get(ctx, types.NamespacedName{Name: phName, Namespace: "default"}, &after)).To(Succeed())
			c := meta.FindStatusCondition(after.Status.Conditions, tideprojectv1alpha2.ConditionWaveOrLevelPaused)
			g.Expect(c).NotTo(BeNil(), "ConditionWaveOrLevelPaused must be set when parked Rejected")
			g.Expect(c.Reason).To(Equal(tideprojectv1alpha2.ReasonRejectedByUser),
				"Phase must be parked with RejectedByUser reason (D-05)")
			g.Expect(c.Message).To(ContainSubstring("operator halt test"),
				"reject reason must be propagated to the condition message")
		}, 5*time.Second, 100*time.Millisecond).Should(Succeed())

		// Load-bearing assertion (b): NO tide-reporter-<phase-uid> Job must exist for
		// this Phase. The reporter Job name is "tide-reporter-<phase-uid>" (see
		// dispatch_helpers.go). Assert by exact name — NOT by listing all Jobs — to
		// avoid false-positives from unrelated reporter Jobs from concurrent specs.
		// Pitfall 3: assert NONE created, never assert deletion.
		var got2 tideprojectv1alpha2.Phase
		Expect(mgrClient.Get(ctx, types.NamespacedName{Name: phName, Namespace: "default"}, &got2)).To(Succeed())
		reporterJobName := fmt.Sprintf("tide-reporter-%s", got2.UID)
		var reporterJob batchv1.Job
		getErr := mgrClient.Get(ctx, types.NamespacedName{Name: reporterJobName, Namespace: "default"}, &reporterJob)
		Expect(getErr).To(HaveOccurred(),
			"no tide-reporter Job must be created when Project is rejected (T-17-04)")
		Expect(getErr.Error()).To(ContainSubstring("not found"),
			"reporter Job absence must be a not-found error, not some other error")

		// Cleanup.
		DeferCleanup(func() {
			for _, name := range []struct{ n string }{
				{phName}, {msName}, {projName},
			} {
				phObj := &tideprojectv1alpha2.Phase{}
				if err := k8sClient.Get(ctx, types.NamespacedName{Name: name.n, Namespace: "default"}, phObj); err == nil {
					phObj.Finalizers = nil
					_ = k8sClient.Update(ctx, phObj)
					_ = k8sClient.Delete(ctx, phObj)
				}
				msObj := &tideprojectv1alpha2.Milestone{}
				if err := k8sClient.Get(ctx, types.NamespacedName{Name: name.n, Namespace: "default"}, msObj); err == nil {
					msObj.Finalizers = nil
					_ = k8sClient.Update(ctx, msObj)
					_ = k8sClient.Delete(ctx, msObj)
				}
				projObj := &tideprojectv1alpha2.Project{}
				if err := k8sClient.Get(ctx, types.NamespacedName{Name: name.n, Namespace: "default"}, projObj); err == nil {
					projObj.Finalizers = nil
					_ = k8sClient.Update(ctx, projObj)
					_ = k8sClient.Delete(ctx, projObj)
				}
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

// ---------------------------------------------------------------------------
// Phase 33 PLANFAIL-01 / PLANFAIL-03 — phase planner false-leaf guard
// ---------------------------------------------------------------------------

var _ = Describe("PhaseReconciler — PLANFAIL D4 false-leaf guard (Phase 33)", Label("envtest", "phase33"), func() {
	const planfailProjectName = "test-proj-ph-planfail"
	const planfailMilestoneName = "test-ms-ph-planfail"
	ctx := context.Background()

	BeforeEach(func() {
		proj := &tideprojectv1alpha2.Project{
			ObjectMeta: metav1.ObjectMeta{Name: planfailProjectName, Namespace: "default"},
			Spec: tideprojectv1alpha2.ProjectSpec{SchemaRevision: "v1alpha2",
				TargetRepo: "https://github.com/example/test.git",
				Subagent:   tideprojectv1alpha2.SubagentConfig{Model: "claude-sonnet-4-6"},
				Git: &tideprojectv1alpha2.GitConfig{
					RepoURL:        "https://github.com/example/test.git",
					CredsSecretRef: "test-creds",
				},
				// auto gate so planner Job completion flows directly to succession logic
				// without parking at AwaitingApproval — the guard fires inside if envReadOK {.
				Gates: tideprojectv1alpha2.Gates{
					Phase: tideprojectv1alpha2.GatePolicy("auto"),
				},
			},
		}
		Expect(k8sClient.Create(ctx, proj)).To(Succeed())
		waitForCacheSync(planfailProjectName, "default", &tideprojectv1alpha2.Project{})
		ms := &tideprojectv1alpha2.Milestone{
			ObjectMeta: metav1.ObjectMeta{Name: planfailMilestoneName, Namespace: "default"},
			Spec:       tideprojectv1alpha2.MilestoneSpec{ProjectRef: planfailProjectName},
		}
		Expect(k8sClient.Create(ctx, ms)).To(Succeed())
		waitForCacheSync(planfailMilestoneName, "default", &tideprojectv1alpha2.Milestone{})
	})

	AfterEach(func() {
		cleanObj := func(obj client.Object) {
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: obj.GetName(), Namespace: "default"}, obj); err == nil {
				obj.SetFinalizers(nil)
				_ = k8sClient.Update(ctx, obj)
				_ = k8sClient.Delete(ctx, obj)
			}
		}
		var phases tideprojectv1alpha2.PhaseList
		_ = k8sClient.List(ctx, &phases, client.InNamespace("default"),
			client.MatchingLabels{"tideproject.k8s/project": planfailProjectName})
		for i := range phases.Items {
			cleanObj(&phases.Items[i])
		}
		cleanObj(&tideprojectv1alpha2.Milestone{ObjectMeta: metav1.ObjectMeta{Name: planfailMilestoneName, Namespace: "default"}})
		cleanObj(&tideprojectv1alpha2.Project{ObjectMeta: metav1.ObjectMeta{Name: planfailProjectName, Namespace: "default"}})
		var jobs batchv1.JobList
		_ = k8sClient.List(ctx, &jobs, client.InNamespace("default"))
		for i := range jobs.Items {
			j := jobs.Items[i]
			_ = k8sClient.Delete(ctx, &j)
		}
	})

	// PLANFAIL-01: a phase planner that exits nonzero with zero children must be
	// marked Failed (not Succeeded). This is the D4 false-leaf guard.
	It("PLANFAIL-01: phase planner exitCode=1,childCount=0 → Status.Phase=Failed with ReasonPlannerFailed", func() {
		const phName = "planfail-01-phase"
		ph := &tideprojectv1alpha2.Phase{
			ObjectMeta: metav1.ObjectMeta{Name: phName, Namespace: "default"},
			Spec:       tideprojectv1alpha2.PhaseSpec{MilestoneRef: planfailMilestoneName},
		}
		Expect(k8sClient.Create(ctx, ph)).To(Succeed())
		Eventually(func() error {
			return mgrClient.Get(ctx, types.NamespacedName{Name: phName, Namespace: "default"}, &tideprojectv1alpha2.Phase{})
		}, "5s", "100ms").Should(Succeed())

		envReader := newMapEnvReader()
		r := &PhaseReconciler{
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

		// Drive reconcile to dispatch the planner Job.
		Expect(reconcileWithRetry(r.Reconcile, types.NamespacedName{Name: phName, Namespace: "default"}, 5)).To(Succeed())

		var got tideprojectv1alpha2.Phase
		Eventually(func() error {
			return mgrClient.Get(ctx, types.NamespacedName{Name: phName, Namespace: "default"}, &got)
		}, "5s", "100ms").Should(Succeed())

		// Simulate planner exiting nonzero with zero children (the false-leaf case).
		envReader.SetOut(string(got.UID), pkgdispatch.EnvelopeOut{
			TaskUID:    string(got.UID),
			ExitCode:   1,
			Reason:     "forced-failure",
			ChildCount: 0,
		})
		jobName := fmt.Sprintf("tide-phase-%s-1", got.UID)
		Expect(makeFakeJobTerminal(ctx, mgrClient, jobName, "default", true)).To(Succeed())

		// Reconcile — guard must fire and mark Phase Failed.
		Expect(reconcileWithRetry(r.Reconcile, types.NamespacedName{Name: phName, Namespace: "default"}, 3)).To(Succeed())

		Eventually(func(g Gomega) {
			var after tideprojectv1alpha2.Phase
			g.Expect(mgrClient.Get(ctx, types.NamespacedName{Name: phName, Namespace: "default"}, &after)).To(Succeed())
			g.Expect(after.Status.Phase).To(Equal("Failed"),
				"PLANFAIL-01: phase planner exitCode!=0,childCount==0 must mark Phase Failed")
			cond := meta.FindStatusCondition(after.Status.Conditions, tideprojectv1alpha2.ConditionFailed)
			g.Expect(cond).NotTo(BeNil(), "ConditionFailed must be set")
			g.Expect(cond.Status).To(Equal(metav1.ConditionTrue), "ConditionFailed must be True")
			g.Expect(cond.Reason).To(Equal(tideprojectv1alpha2.ReasonPlannerFailed),
				"ConditionFailed.Reason must be ReasonPlannerFailed")
		}, 5*time.Second, 100*time.Millisecond).Should(Succeed())
	})

	// PLANFAIL-03 (phase): a genuine leaf (exitCode==0, childCount==0) must still
	// Succeed — fail-check ordering before succeed-check is load-bearing.
	It("PLANFAIL-03: phase planner exitCode=0,childCount=0 (genuine leaf) → Status.Phase=Succeeded", func() {
		const phName = "planfail-03-phase"
		ph := &tideprojectv1alpha2.Phase{
			ObjectMeta: metav1.ObjectMeta{Name: phName, Namespace: "default"},
			Spec:       tideprojectv1alpha2.PhaseSpec{MilestoneRef: planfailMilestoneName},
		}
		Expect(k8sClient.Create(ctx, ph)).To(Succeed())
		Eventually(func() error {
			return mgrClient.Get(ctx, types.NamespacedName{Name: phName, Namespace: "default"}, &tideprojectv1alpha2.Phase{})
		}, "5s", "100ms").Should(Succeed())

		envReader := newMapEnvReader()
		r := &PhaseReconciler{
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

		// Drive reconcile to dispatch the planner Job.
		Expect(reconcileWithRetry(r.Reconcile, types.NamespacedName{Name: phName, Namespace: "default"}, 5)).To(Succeed())

		var got tideprojectv1alpha2.Phase
		Eventually(func() error {
			return mgrClient.Get(ctx, types.NamespacedName{Name: phName, Namespace: "default"}, &got)
		}, "5s", "100ms").Should(Succeed())

		// Genuine leaf: exitCode=0, childCount=0 — must NOT trigger the fail guard.
		envReader.SetOut(string(got.UID), pkgdispatch.EnvelopeOut{
			TaskUID:    string(got.UID),
			ExitCode:   0,
			ChildCount: 0,
		})
		jobName := fmt.Sprintf("tide-phase-%s-1", got.UID)
		Expect(makeFakeJobTerminal(ctx, mgrClient, jobName, "default", true)).To(Succeed())

		// Reconcile — genuine leaf must Succeed.
		Expect(reconcileWithRetry(r.Reconcile, types.NamespacedName{Name: phName, Namespace: "default"}, 3)).To(Succeed())

		Eventually(func(g Gomega) {
			var after tideprojectv1alpha2.Phase
			g.Expect(mgrClient.Get(ctx, types.NamespacedName{Name: phName, Namespace: "default"}, &after)).To(Succeed())
			g.Expect(after.Status.Phase).To(Equal("Succeeded"),
				"PLANFAIL-03: genuine leaf (exitCode==0,childCount==0) must still Succeed at phase level")
		}, 5*time.Second, 100*time.Millisecond).Should(Succeed())
	})
})

var _ = Describe("PhaseReconciler — D-03 project-label backfill (CUTS-01)", Label("envtest"), func() {
	ctx := context.Background()

	It("backfills tideproject.k8s/project on a Phase that was created without the label via Phase→Milestone→Project chain, and is idempotent on second reconcile", func() {
		const projName = "backfill-proj-ph"
		const msName = "backfill-ms-ph-01"
		const phName = "backfill-phase-01"

		// Create Project.
		proj := &tideprojectv1alpha2.Project{
			ObjectMeta: metav1.ObjectMeta{Name: projName, Namespace: "default"},
			Spec: tideprojectv1alpha2.ProjectSpec{SchemaRevision: "v1alpha2",
				TargetRepo: "https://github.com/example/test.git",
				Subagent:   tideprojectv1alpha2.SubagentConfig{Model: "claude-opus-4-7"},
			},
		}
		Expect(k8sClient.Create(ctx, proj)).To(Succeed())
		waitForCacheSync(projName, "default", &tideprojectv1alpha2.Project{})

		// Create Milestone (with projectRef so the chain is traversable).
		ms := &tideprojectv1alpha2.Milestone{
			ObjectMeta: metav1.ObjectMeta{Name: msName, Namespace: "default"},
			Spec:       tideprojectv1alpha2.MilestoneSpec{ProjectRef: projName},
		}
		Expect(k8sClient.Create(ctx, ms)).To(Succeed())
		waitForCacheSync(msName, "default", &tideprojectv1alpha2.Milestone{})

		// Create Phase WITHOUT the tideproject.k8s/project label (pre-Phase-15 shape).
		ph := &tideprojectv1alpha2.Phase{
			ObjectMeta: metav1.ObjectMeta{
				Name:      phName,
				Namespace: "default",
				// Labels intentionally absent.
			},
			Spec: tideprojectv1alpha2.PhaseSpec{MilestoneRef: msName},
		}
		Expect(k8sClient.Create(ctx, ph)).To(Succeed())
		waitForCacheSync(phName, "default", &tideprojectv1alpha2.Phase{})

		r := &PhaseReconciler{
			Client: mgrClient,
			Scheme: k8sClient.Scheme(),
			// No Dispatcher — drives steps 1-5 without planner dispatch.
		}

		// First reconcile: finalizer, owner-ref, then backfill.
		Expect(reconcileWithRetry(r.Reconcile, types.NamespacedName{Name: phName, Namespace: "default"}, 5)).To(Succeed())

		// Assert the project label was backfilled.
		var after tideprojectv1alpha2.Phase
		Expect(mgrClient.Get(ctx, types.NamespacedName{Name: phName, Namespace: "default"}, &after)).To(Succeed())
		Expect(after.Labels["tideproject.k8s/project"]).To(Equal(projName),
			"backfill must stamp tideproject.k8s/project via Phase→Milestone→Project chain")

		// Idempotency: record ResourceVersion, reconcile again, verify unchanged.
		rvBefore := after.ResourceVersion
		Expect(reconcileWithRetry(r.Reconcile, types.NamespacedName{Name: phName, Namespace: "default"}, 2)).To(Succeed())
		var after2 tideprojectv1alpha2.Phase
		Expect(mgrClient.Get(ctx, types.NamespacedName{Name: phName, Namespace: "default"}, &after2)).To(Succeed())
		Expect(after2.ResourceVersion).To(Equal(rvBefore),
			"second reconcile must not patch the object (idempotent backfill)")

		// Cleanup.
		after2.Finalizers = nil
		_ = k8sClient.Update(ctx, &after2)
		_ = k8sClient.Delete(ctx, &after2)
		ms2 := &tideprojectv1alpha2.Milestone{}
		if err := k8sClient.Get(ctx, types.NamespacedName{Name: msName, Namespace: "default"}, ms2); err == nil {
			ms2.Finalizers = nil
			_ = k8sClient.Update(ctx, ms2)
			_ = k8sClient.Delete(ctx, ms2)
		}
		proj2 := &tideprojectv1alpha2.Project{}
		if err := k8sClient.Get(ctx, types.NamespacedName{Name: projName, Namespace: "default"}, proj2); err == nil {
			proj2.Finalizers = nil
			_ = k8sClient.Update(ctx, proj2)
			_ = k8sClient.Delete(ctx, proj2)
		}
	})
})
