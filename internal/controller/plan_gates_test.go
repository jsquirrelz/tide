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
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	tideprojectv1alpha3 "github.com/jsquirrelz/tide/api/v1alpha3"
	"github.com/jsquirrelz/tide/internal/gates"
	pkgdispatch "github.com/jsquirrelz/tide/pkg/dispatch"
)

// gate-flow tests for PlanReconciler (Plan 04-05 Task 1).
//
// Plan gates fire on planner-Job completion (where the planner has authored
// child Tasks). The gate hook lands inside handlePlannerJobCompletion BEFORE
// the final "clear Running" patch that lets the Wave path take over.
var _ = Describe("PlanReconciler — gate-policy hook (Plan 04-05 Task 1)", Label("envtest", "phase4", "gates"), func() {
	ctx := context.Background()

	makeProjectChain := func(projectName, msName, phaseName string, g tideprojectv1alpha3.Gates) {
		proj := &tideprojectv1alpha3.Project{
			ObjectMeta: metav1.ObjectMeta{Name: projectName, Namespace: "default"},
			Spec: tideprojectv1alpha3.ProjectSpec{SchemaRevision: "v1alpha3",
				TargetRepo: "https://github.com/example/test.git",
				Subagent:   tideprojectv1alpha3.SubagentConfig{Model: "claude-opus-4-7"},
				Git: &tideprojectv1alpha3.GitConfig{
					RepoURL:        "https://github.com/example/test.git",
					CredsSecretRef: "test-creds",
				},
				Gates: g,
			},
		}
		Expect(k8sClient.Create(ctx, proj)).To(Succeed())
		waitForCacheSync(projectName, "default", &tideprojectv1alpha3.Project{})
		ms := &tideprojectv1alpha3.Milestone{
			ObjectMeta: metav1.ObjectMeta{Name: msName, Namespace: "default"},
			Spec:       tideprojectv1alpha3.MilestoneSpec{ProjectRef: projectName},
		}
		Expect(k8sClient.Create(ctx, ms)).To(Succeed())
		waitForCacheSync(msName, "default", &tideprojectv1alpha3.Milestone{})
		ph := &tideprojectv1alpha3.Phase{
			ObjectMeta: metav1.ObjectMeta{Name: phaseName, Namespace: "default"},
			Spec:       tideprojectv1alpha3.PhaseSpec{MilestoneRef: msName},
		}
		Expect(k8sClient.Create(ctx, ph)).To(Succeed())
		waitForCacheSync(phaseName, "default", &tideprojectv1alpha3.Phase{})
	}

	cleanup := func(projectName, msName, phaseName, planName string) {
		pl := &tideprojectv1alpha3.Plan{}
		if err := k8sClient.Get(ctx, types.NamespacedName{Name: planName, Namespace: "default"}, pl); err == nil {
			pl.Finalizers = nil
			_ = k8sClient.Update(ctx, pl)
			_ = k8sClient.Delete(ctx, pl)
		}
		ph := &tideprojectv1alpha3.Phase{}
		if err := k8sClient.Get(ctx, types.NamespacedName{Name: phaseName, Namespace: "default"}, ph); err == nil {
			ph.Finalizers = nil
			_ = k8sClient.Update(ctx, ph)
			_ = k8sClient.Delete(ctx, ph)
		}
		ms := &tideprojectv1alpha3.Milestone{}
		if err := k8sClient.Get(ctx, types.NamespacedName{Name: msName, Namespace: "default"}, ms); err == nil {
			ms.Finalizers = nil
			_ = k8sClient.Update(ctx, ms)
			_ = k8sClient.Delete(ctx, ms)
		}
		proj := &tideprojectv1alpha3.Project{}
		if err := k8sClient.Get(ctx, types.NamespacedName{Name: projectName, Namespace: "default"}, proj); err == nil {
			proj.Finalizers = nil
			_ = k8sClient.Update(ctx, proj)
			_ = k8sClient.Delete(ctx, proj)
		}
		var jobs batchv1.JobList
		_ = k8sClient.List(ctx, &jobs, client.InNamespace("default"))
		for i := range jobs.Items {
			j := jobs.Items[i]
			_ = k8sClient.Delete(ctx, &j)
		}
	}

	driveToJobCompletion := func(planName string, r *PlanReconciler, envReader *mapEnvReader, childCount int) {
		waitForCacheSync(planName, "default", &tideprojectv1alpha3.Plan{})
		Expect(reconcileWithRetry(r.Reconcile, types.NamespacedName{Name: planName, Namespace: "default"}, 5)).To(Succeed())
		var got tideprojectv1alpha3.Plan
		Eventually(func() error {
			return mgrClient.Get(ctx, types.NamespacedName{Name: planName, Namespace: "default"}, &got)
		}, 5*time.Second, 50*time.Millisecond).Should(Succeed())
		jobName := fmt.Sprintf("tide-plan-%s-1", got.UID)
		envReader.SetOut(string(got.UID), pkgdispatch.EnvelopeOut{
			TaskUID:    string(got.UID),
			ExitCode:   0,
			ChildCount: childCount,
		})
		Expect(makeFakeJobTerminal(ctx, mgrClient, jobName, "default", true)).To(Succeed())
		Expect(reconcileWithRetry(r.Reconcile, types.NamespacedName{Name: planName, Namespace: "default"}, 3)).To(Succeed())
	}

	Describe("Test 6a — gates.plan=approve: AwaitingApproval", func() {
		const projectName, msName, phaseName, planName = "gate-proj-pl1", "gate-ms-pl1", "gate-phase-pl1", "gate-plan-1"

		BeforeEach(func() {
			makeProjectChain(projectName, msName, phaseName, tideprojectv1alpha3.Gates{Plan: gates.PolicyApprove})
		})
		AfterEach(func() { cleanup(projectName, msName, phaseName, planName) })

		It("Plan patches Status.Phase=AwaitingApproval", func() {
			plan := &tideprojectv1alpha3.Plan{
				ObjectMeta: metav1.ObjectMeta{Name: planName, Namespace: "default"},
				Spec:       tideprojectv1alpha3.PlanSpec{PhaseRef: phaseName},
			}
			Expect(k8sClient.Create(ctx, plan)).To(Succeed())

			envReader := newMapEnvReader()
			r := &PlanReconciler{
				Client:         mgrClient,
				Scheme:         k8sClient.Scheme(),
				Dispatcher:     &stubDispatcher{},
				PlannerPool:    newPlannerPoolForTest(),
				EnvReader:      envReader,
				CredproxyImage: testCredproxyImage,
				SigningKey:     testSigningKey,
				HelmProviderDefaults: ProviderDefaults{
					Image: testSubagentImage,
				},
			}
			driveToJobCompletion(planName, r, envReader, 0)

			Eventually(func(g Gomega) {
				var after tideprojectv1alpha3.Plan
				g.Expect(mgrClient.Get(ctx, types.NamespacedName{Name: planName, Namespace: "default"}, &after)).To(Succeed())
				g.Expect(after.Status.Phase).To(Equal("AwaitingApproval"))
				c := meta.FindStatusCondition(after.Status.Conditions, tideprojectv1alpha3.ConditionWaveOrLevelPaused)
				g.Expect(c).NotTo(BeNil())
				g.Expect(c.Reason).To(Equal(tideprojectv1alpha3.ReasonAwaitingApproval))
			}, 5*time.Second, 100*time.Millisecond).Should(Succeed())
		})
	})

	Describe("Test 6b — approve-plan annotation: resumes and clears Running phase", func() {
		const projectName, msName, phaseName, planName = "gate-proj-pl2", "gate-ms-pl2", "gate-phase-pl2", "gate-plan-2"

		BeforeEach(func() {
			makeProjectChain(projectName, msName, phaseName, tideprojectv1alpha3.Gates{Plan: gates.PolicyApprove})
		})
		AfterEach(func() { cleanup(projectName, msName, phaseName, planName) })

		It("consumes annotation and the Plan exits the AwaitingApproval state", func() {
			plan := &tideprojectv1alpha3.Plan{
				ObjectMeta: metav1.ObjectMeta{Name: planName, Namespace: "default"},
				Spec:       tideprojectv1alpha3.PlanSpec{PhaseRef: phaseName},
			}
			Expect(k8sClient.Create(ctx, plan)).To(Succeed())

			envReader := newMapEnvReader()
			r := &PlanReconciler{
				Client:         mgrClient,
				Scheme:         k8sClient.Scheme(),
				Dispatcher:     &stubDispatcher{},
				PlannerPool:    newPlannerPoolForTest(),
				EnvReader:      envReader,
				CredproxyImage: testCredproxyImage,
				SigningKey:     testSigningKey,
				HelmProviderDefaults: ProviderDefaults{
					Image: testSubagentImage,
				},
			}
			driveToJobCompletion(planName, r, envReader, 0)

			Eventually(func() string {
				var after tideprojectv1alpha3.Plan
				if err := mgrClient.Get(ctx, types.NamespacedName{Name: planName, Namespace: "default"}, &after); err != nil {
					return ""
				}
				return after.Status.Phase
			}, 5*time.Second, 100*time.Millisecond).Should(Equal("AwaitingApproval"))

			var current tideprojectv1alpha3.Plan
			Expect(mgrClient.Get(ctx, types.NamespacedName{Name: planName, Namespace: "default"}, &current)).To(Succeed())
			patch := client.MergeFrom(current.DeepCopy())
			if current.Annotations == nil {
				current.Annotations = map[string]string{}
			}
			current.Annotations[gates.AnnotationApprovePrefix+"plan"] = "true"
			Expect(k8sClient.Patch(ctx, &current, patch)).To(Succeed())

			Expect(reconcileWithRetry(r.Reconcile, types.NamespacedName{Name: planName, Namespace: "default"}, 5)).To(Succeed())

			Eventually(func(g Gomega) {
				var after tideprojectv1alpha3.Plan
				g.Expect(mgrClient.Get(ctx, types.NamespacedName{Name: planName, Namespace: "default"}, &after)).To(Succeed())
				// Plan should exit AwaitingApproval — either to the Wave-materialization path
				// (Phase cleared to "") or whatever subsequent state. The key contract: not AwaitingApproval.
				g.Expect(after.Status.Phase).NotTo(Equal("AwaitingApproval"))
				_, has := after.Annotations[gates.AnnotationApprovePrefix+"plan"]
				g.Expect(has).To(BeFalse(), "approve-plan annotation should be consumed")
			}, 5*time.Second, 100*time.Millisecond).Should(Succeed())
		})
	})

	Describe("Test 6c — reject annotation on Project: parks Plan with RejectedByUser condition (D-05)", func() {
		const projectName, msName, phaseName, planName = "gate-proj-pl3", "gate-ms-pl3", "gate-phase-pl3", "gate-plan-3"

		BeforeEach(func() {
			makeProjectChain(projectName, msName, phaseName, tideprojectv1alpha3.Gates{Plan: gates.PolicyAuto})
			var proj tideprojectv1alpha3.Project
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: projectName, Namespace: "default"}, &proj)).To(Succeed())
			patch := client.MergeFrom(proj.DeepCopy())
			if proj.Annotations == nil {
				proj.Annotations = map[string]string{}
			}
			proj.Annotations[gates.AnnotationReject] = "plan halt"
			Expect(k8sClient.Patch(ctx, &proj, patch)).To(Succeed())
			Eventually(func() string {
				var p tideprojectv1alpha3.Project
				if err := mgrClient.Get(ctx, types.NamespacedName{Name: projectName, Namespace: "default"}, &p); err != nil {
					return ""
				}
				return p.Annotations[gates.AnnotationReject]
			}, 5*time.Second, 50*time.Millisecond).Should(Equal("plan halt"))
		})
		AfterEach(func() { cleanup(projectName, msName, phaseName, planName) })

		It("Plan is parked with ConditionWaveOrLevelPaused/RejectedByUser (NOT Failed), then recovers after annotation clear", func() {
			plan := &tideprojectv1alpha3.Plan{
				ObjectMeta: metav1.ObjectMeta{Name: planName, Namespace: "default"},
				Spec:       tideprojectv1alpha3.PlanSpec{PhaseRef: phaseName},
			}
			Expect(k8sClient.Create(ctx, plan)).To(Succeed())
			waitForCacheSync(planName, "default", &tideprojectv1alpha3.Plan{})

			envReader := newMapEnvReader()
			r := &PlanReconciler{
				Client:         mgrClient,
				Scheme:         k8sClient.Scheme(),
				Dispatcher:     &stubDispatcher{},
				PlannerPool:    newPlannerPoolForTest(),
				EnvReader:      envReader,
				CredproxyImage: testCredproxyImage,
				SigningKey:     testSigningKey,
				HelmProviderDefaults: ProviderDefaults{
					Image: testSubagentImage,
				},
			}
			// D-05 dispatch-entry hold fires before Job creation — drive reconcile directly.
			Expect(reconcileWithRetry(r.Reconcile, types.NamespacedName{Name: planName, Namespace: "default"}, 3)).To(Succeed())

			// D-05: reject parks — Status.Phase must NOT be "Failed".
			Eventually(func(g Gomega) {
				var after tideprojectv1alpha3.Plan
				g.Expect(mgrClient.Get(ctx, types.NamespacedName{Name: planName, Namespace: "default"}, &after)).To(Succeed())
				g.Expect(after.Status.Phase).NotTo(Equal("Failed"),
					"D-05: reject must park the Plan, not fail-mark it")
				c := meta.FindStatusCondition(after.Status.Conditions, tideprojectv1alpha3.ConditionWaveOrLevelPaused)
				g.Expect(c).NotTo(BeNil(), "ConditionWaveOrLevelPaused must be set when parked")
				g.Expect(c.Status).To(Equal(metav1.ConditionTrue))
				g.Expect(c.Reason).To(Equal(tideprojectv1alpha3.ReasonRejectedByUser))
				g.Expect(c.Message).To(ContainSubstring("plan halt"))
			}, 5*time.Second, 100*time.Millisecond).Should(Succeed())

			// D-05 recovery: clear the reject annotation (simulating tide resume).
			var current tideprojectv1alpha3.Project
			Expect(mgrClient.Get(ctx, types.NamespacedName{Name: projectName, Namespace: "default"}, &current)).To(Succeed())
			newAnno := gates.ConsumeReject(&current)
			annoPatch := client.MergeFrom(current.DeepCopy())
			current.SetAnnotations(newAnno)
			Expect(k8sClient.Patch(ctx, &current, annoPatch)).To(Succeed())
			Eventually(func() string {
				var p tideprojectv1alpha3.Project
				if err := mgrClient.Get(ctx, types.NamespacedName{Name: projectName, Namespace: "default"}, &p); err != nil {
					return "err"
				}
				return p.Annotations[gates.AnnotationReject]
			}, 5*time.Second, 50*time.Millisecond).Should(BeEmpty())

			// After annotation clear, re-driving must let the Plan proceed.
			driveToJobCompletion(planName, r, envReader, 0)
			Eventually(func(g Gomega) {
				var after tideprojectv1alpha3.Plan
				g.Expect(mgrClient.Get(ctx, types.NamespacedName{Name: planName, Namespace: "default"}, &after)).To(Succeed())
				g.Expect(after.Status.Phase).NotTo(Equal("Failed"),
					"D-05: Plan must not be Failed after reject annotation cleared")
			}, 5*time.Second, 100*time.Millisecond).Should(Succeed())
		})
	})

	// Test 6d — GATE-01/GATE-04 regression: gates.plan=approve with ChildCount>0
	// parks before executor dispatch (CR-01).
	//
	// CR-01 defect: the gate hook fires AFTER the ChildCount requeue in
	// handlePlannerJobCompletion, so when ChildCount>0 the requeue fires first
	// and patchPlanAwaitingApproval never runs — executor Tasks dispatch unreviewed.
	// Additionally reconcilePlannerDispatch lacks an AwaitingApproval early-return,
	// so a Plan that somehow parks is immediately stomped back to Running on the
	// next reconcile (CR-02 within the same flow).
	//
	// RED assertions:
	//   PRIMARY: Status.Phase == "AwaitingApproval" after ChildCount>0 planner completion
	//   SECONDARY: zero executor Jobs and zero Wave CRs while parked (GATE-04)
	Describe("Test 6d — GATE-01/GATE-04 regression: gates.plan=approve with ChildCount>0 parks before executor dispatch (CR-01)", func() {
		const projectName, msName, phaseName, planName = "gate-proj-pl4", "gate-ms-pl4", "gate-phase-pl4", "gate-plan-4"
		const task1Name, task2Name = "gate-plan4-task-1", "gate-plan4-task-2"

		var r *PlanReconciler
		var envReader *mapEnvReader

		BeforeEach(func() {
			// gates.task=auto intentionally — the hold under test is the PARENT Plan hold
			// (checkParentApproval kind=Plan), not the task-level gate.
			makeProjectChain(projectName, msName, phaseName, tideprojectv1alpha3.Gates{Plan: gates.PolicyApprove})
		})
		AfterEach(func() {
			cleanupTask(task1Name)
			cleanupTask(task2Name)
			cleanup(projectName, msName, phaseName, planName)
			// Clean up any Wave CRs created in the test namespace.
			var waves tideprojectv1alpha3.WaveList
			_ = k8sClient.List(ctx, &waves, client.InNamespace("default"))
			for i := range waves.Items {
				w := waves.Items[i]
				_ = k8sClient.Delete(ctx, &w)
			}
		})

		It("parks at AwaitingApproval (not stomped to Running), holds Task dispatch, lifts on approval", func() {
			plan := &tideprojectv1alpha3.Plan{
				ObjectMeta: metav1.ObjectMeta{Name: planName, Namespace: "default"},
				Spec:       tideprojectv1alpha3.PlanSpec{PhaseRef: phaseName},
			}
			Expect(k8sClient.Create(ctx, plan)).To(Succeed())

			envReader = newMapEnvReader()
			r = &PlanReconciler{
				Client:         mgrClient,
				Scheme:         k8sClient.Scheme(),
				Dispatcher:     &stubDispatcher{},
				PlannerPool:    newPlannerPoolForTest(),
				EnvReader:      envReader,
				CredproxyImage: testCredproxyImage,
				SigningKey:     testSigningKey,
				HelmProviderDefaults: ProviderDefaults{
					Image: testSubagentImage,
				},
			}

			// Step 1: drive planner completion with ChildCount=2.
			// PRIMARY RED ASSERTION: today the ChildCount requeue (observed 0 < 2)
			// fires BEFORE the gate hook, so the Plan never parks — it requeues instead.
			driveToJobCompletion(planName, r, envReader, 2)

			planNN := types.NamespacedName{Name: planName, Namespace: "default"}

			// Step 2: assert parked at AwaitingApproval.
			Eventually(func(g Gomega) {
				var after tideprojectv1alpha3.Plan
				g.Expect(mgrClient.Get(ctx, planNN, &after)).To(Succeed())
				g.Expect(after.Status.Phase).To(Equal("AwaitingApproval"),
					"PRIMARY: plan with ChildCount>0 must park at AwaitingApproval before the ChildCount requeue")
				c := meta.FindStatusCondition(after.Status.Conditions, tideprojectv1alpha3.ConditionWaveOrLevelPaused)
				g.Expect(c).NotTo(BeNil())
				g.Expect(c.Status).To(Equal(metav1.ConditionTrue))
				g.Expect(c.Reason).To(Equal(tideprojectv1alpha3.ReasonAwaitingApproval))
			}, 5*time.Second, 100*time.Millisecond).Should(Succeed())

			// Step 3: simulate reporter materializing 2 Tasks.
			makeTask(task1Name, planName, nil, projectName)
			makeTask(task2Name, planName, nil, projectName)
			waitForCacheSync(task1Name, "default", &tideprojectv1alpha3.Task{})
			waitForCacheSync(task2Name, "default", &tideprojectv1alpha3.Task{})

			// Step 4: drive Plan reconciler 3 more times — a parked Plan must not
			// take the tasks-exist early-exit to the wave path (CR-02 parity).
			Expect(reconcileWithRetry(r.Reconcile, planNN, 3)).To(Succeed())
			var afterPark tideprojectv1alpha3.Plan
			Expect(mgrClient.Get(ctx, planNN, &afterPark)).To(Succeed())
			Expect(afterPark.Status.Phase).To(Equal("AwaitingApproval"),
				"parked Plan with Tasks must not be stomped back to Running via the tasks-exist exit")

			// Step 5: drive TaskReconciler on both tasks — zero executor Jobs while parked.
			// The checkParentApproval(kind=Plan) hold at task_controller.go:326 must
			// engage now that the Plan actually reaches AwaitingApproval.
			taskReconciler := newTaskReconciler(envReader)
			task1NN := types.NamespacedName{Name: task1Name, Namespace: "default"}
			task2NN := types.NamespacedName{Name: task2Name, Namespace: "default"}

			err := reconcileWithRetry(taskReconciler.Reconcile, task1NN, 3)
			Expect(err).NotTo(HaveOccurred())
			err = reconcileWithRetry(taskReconciler.Reconcile, task2NN, 3)
			Expect(err).NotTo(HaveOccurred())

			// SECOND RED ASSERTION: Task reconciler holds (Status.Phase stays "")
			// and zero executor Jobs exist while the plan is parked.
			Eventually(func(g Gomega) {
				var t1 tideprojectv1alpha3.Task
				g.Expect(mgrClient.Get(ctx, task1NN, &t1)).To(Succeed())
				g.Expect(t1.Status.Phase).To(Equal(""),
					"Task must stay at Phase=\"\" (held) while parent Plan is parked (Pitfall 5)")

				var t2 tideprojectv1alpha3.Task
				g.Expect(mgrClient.Get(ctx, task2NN, &t2)).To(Succeed())
				g.Expect(t2.Status.Phase).To(Equal(""),
					"Task must stay at Phase=\"\" (held) while parent Plan is parked (Pitfall 5)")

				// Zero executor Jobs while parked.
				uid1 := string(getTaskUID(task1Name))
				uid2 := string(getTaskUID(task2Name))
				var jobs batchv1.JobList
				g.Expect(k8sClient.List(ctx, &jobs, client.InNamespace("default"))).To(Succeed())
				for _, j := range jobs.Items {
					execUID := j.Labels["tideproject.k8s/task-uid"]
					g.Expect(execUID).NotTo(Equal(uid1),
						"GATE-04: no executor Job for task-1 while Plan is parked")
					g.Expect(execUID).NotTo(Equal(uid2),
						"GATE-04: no executor Job for task-2 while Plan is parked")
				}

				// Zero Wave CRs created by the wave path while parked. v1alpha3
				// Waves carry no Spec.PlanRef; this Plan's Waves are identified by
				// the per-plan stub name prefix tide-wave-<plan.UID>-.
				var parkedPlan tideprojectv1alpha3.Plan
				g.Expect(mgrClient.Get(ctx, planNN, &parkedPlan)).To(Succeed())
				wavePrefix := fmt.Sprintf("tide-wave-%s-", parkedPlan.UID)
				var waves tideprojectv1alpha3.WaveList
				g.Expect(k8sClient.List(ctx, &waves, client.InNamespace("default"))).To(Succeed())
				for _, w := range waves.Items {
					g.Expect(strings.HasPrefix(w.Name, wavePrefix)).To(BeFalse(),
						"GATE-04: no Wave CRs must be created while Plan is parked")
				}
			}, 5*time.Second, 100*time.Millisecond).Should(Succeed())

			// Step 6: approve — annotate and reconcile.
			var current tideprojectv1alpha3.Plan
			Expect(mgrClient.Get(ctx, planNN, &current)).To(Succeed())
			patch := client.MergeFrom(current.DeepCopy())
			if current.Annotations == nil {
				current.Annotations = map[string]string{}
			}
			current.Annotations[gates.AnnotationApprovePrefix+"plan"] = "true"
			Expect(k8sClient.Patch(ctx, &current, patch)).To(Succeed())

			Expect(reconcileWithRetry(r.Reconcile, planNN, 5)).To(Succeed())

			Eventually(func(g Gomega) {
				var after tideprojectv1alpha3.Plan
				g.Expect(mgrClient.Get(ctx, planNN, &after)).To(Succeed())
				g.Expect(after.Status.Phase).To(Equal("Running"),
					"after approval Plan must return to Running")
				_, has := after.Annotations[gates.AnnotationApprovePrefix+"plan"]
				g.Expect(has).To(BeFalse(), "approve annotation must be consumed")
				c := meta.FindStatusCondition(after.Status.Conditions, tideprojectv1alpha3.ConditionWaveOrLevelPaused)
				g.Expect(c).NotTo(BeNil())
				g.Expect(c.Status).To(Equal(metav1.ConditionFalse))
				g.Expect(c.Reason).To(Equal(tideprojectv1alpha3.ReasonApprovedByUser))
			}, 5*time.Second, 100*time.Millisecond).Should(Succeed())

			// Step 7: after approval the Task hold lifts and an executor Job is dispatched.
			err = reconcileWithRetry(taskReconciler.Reconcile, task1NN, 5)
			Expect(err).NotTo(HaveOccurred())
			Eventually(func(g Gomega) {
				uid1 := string(getTaskUID(task1Name))
				var jobs batchv1.JobList
				g.Expect(k8sClient.List(ctx, &jobs, client.InNamespace("default"))).To(Succeed())
				found := false
				for _, j := range jobs.Items {
					if j.Labels["tideproject.k8s/task-uid"] == uid1 {
						found = true
						break
					}
				}
				g.Expect(found).To(BeTrue(),
					"after Plan approval an executor Job must be created for task-1 (hold lifted)")
			}, 5*time.Second, 100*time.Millisecond).Should(Succeed())
		})
	})

	// Test 6e — CR-02 regression: parked leaf Plan stays parked (no Running stomp).
	//
	// CR-02 defect: reconcilePlannerDispatch lacks the AwaitingApproval early-return
	// that milestone/phase controllers have. So a parked leaf Plan (ChildCount==0)
	// falls through to the dispatch body on the NEXT reconcile and is stomped back
	// to Running by the PlannerDispatched status patch, with no annotation consumed.
	Describe("Test 6e — CR-02 regression: parked leaf Plan stays parked (no Running stomp)", func() {
		const projectName, msName, phaseName, planName = "gate-proj-pl5", "gate-ms-pl5", "gate-phase-pl5", "gate-plan-5"

		BeforeEach(func() {
			makeProjectChain(projectName, msName, phaseName, tideprojectv1alpha3.Gates{Plan: gates.PolicyApprove})
		})
		AfterEach(func() { cleanup(projectName, msName, phaseName, planName) })

		It("parked Plan remains AwaitingApproval across repeated reconciles with no annotation", func() {
			plan := &tideprojectv1alpha3.Plan{
				ObjectMeta: metav1.ObjectMeta{Name: planName, Namespace: "default"},
				Spec:       tideprojectv1alpha3.PlanSpec{PhaseRef: phaseName},
			}
			Expect(k8sClient.Create(ctx, plan)).To(Succeed())

			envReader := newMapEnvReader()
			r := &PlanReconciler{
				Client:         mgrClient,
				Scheme:         k8sClient.Scheme(),
				Dispatcher:     &stubDispatcher{},
				PlannerPool:    newPlannerPoolForTest(),
				EnvReader:      envReader,
				CredproxyImage: testCredproxyImage,
				SigningKey:     testSigningKey,
				HelmProviderDefaults: ProviderDefaults{
					Image: testSubagentImage,
				},
			}

			// Drive to completion with childCount=0 (leaf plan) — should park at AwaitingApproval.
			// This step passes today (same as Test 6a).
			driveToJobCompletion(planName, r, envReader, 0)

			planNN := types.NamespacedName{Name: planName, Namespace: "default"}

			Eventually(func() string {
				var after tideprojectv1alpha3.Plan
				if err := mgrClient.Get(ctx, planNN, &after); err != nil {
					return ""
				}
				return after.Status.Phase
			}, 5*time.Second, 100*time.Millisecond).Should(Equal("AwaitingApproval"))

			// RED assertion: re-reconciling 3 times without an approve annotation
			// must not stomp the Plan back to Running. Today the first re-reconcile
			// falls through to the dispatch body (no AwaitingApproval early-return)
			// and patches Phase="Running" via the PlannerDispatched status patch.
			for i := range 3 {
				Expect(reconcileWithRetry(r.Reconcile, planNN, 1)).To(Succeed())
				var after tideprojectv1alpha3.Plan
				Expect(mgrClient.Get(ctx, planNN, &after)).To(Succeed())
				Expect(after.Status.Phase).To(Equal("AwaitingApproval"),
					fmt.Sprintf("CR-02: Plan must remain AwaitingApproval after reconcile %d with no annotation", i+1))
			}
		})
	})
})
