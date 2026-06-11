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

	tideprojectv1alpha1 "github.com/jsquirrelz/tide/api/v1alpha1"
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

	makeProjectChain := func(projectName, msName, phaseName string, g tideprojectv1alpha1.Gates) {
		proj := &tideprojectv1alpha1.Project{
			ObjectMeta: metav1.ObjectMeta{Name: projectName, Namespace: "default"},
			Spec: tideprojectv1alpha1.ProjectSpec{
				TargetRepo: "https://github.com/example/test.git",
				Subagent:   tideprojectv1alpha1.SubagentConfig{Model: "claude-opus-4-7"},
				Git: &tideprojectv1alpha1.GitConfig{
					RepoURL:        "https://github.com/example/test.git",
					CredsSecretRef: "test-creds",
				},
				Gates: g,
			},
		}
		Expect(k8sClient.Create(ctx, proj)).To(Succeed())
		waitForCacheSync(projectName, "default", &tideprojectv1alpha1.Project{})
		ms := &tideprojectv1alpha1.Milestone{
			ObjectMeta: metav1.ObjectMeta{Name: msName, Namespace: "default"},
			Spec:       tideprojectv1alpha1.MilestoneSpec{ProjectRef: projectName},
		}
		Expect(k8sClient.Create(ctx, ms)).To(Succeed())
		waitForCacheSync(msName, "default", &tideprojectv1alpha1.Milestone{})
		ph := &tideprojectv1alpha1.Phase{
			ObjectMeta: metav1.ObjectMeta{Name: phaseName, Namespace: "default"},
			Spec:       tideprojectv1alpha1.PhaseSpec{MilestoneRef: msName},
		}
		Expect(k8sClient.Create(ctx, ph)).To(Succeed())
		waitForCacheSync(phaseName, "default", &tideprojectv1alpha1.Phase{})
	}

	cleanup := func(projectName, msName, phaseName, planName string) {
		pl := &tideprojectv1alpha1.Plan{}
		if err := k8sClient.Get(ctx, types.NamespacedName{Name: planName, Namespace: "default"}, pl); err == nil {
			pl.Finalizers = nil
			_ = k8sClient.Update(ctx, pl)
			_ = k8sClient.Delete(ctx, pl)
		}
		ph := &tideprojectv1alpha1.Phase{}
		if err := k8sClient.Get(ctx, types.NamespacedName{Name: phaseName, Namespace: "default"}, ph); err == nil {
			ph.Finalizers = nil
			_ = k8sClient.Update(ctx, ph)
			_ = k8sClient.Delete(ctx, ph)
		}
		ms := &tideprojectv1alpha1.Milestone{}
		if err := k8sClient.Get(ctx, types.NamespacedName{Name: msName, Namespace: "default"}, ms); err == nil {
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
		var jobs batchv1.JobList
		_ = k8sClient.List(ctx, &jobs, client.InNamespace("default"))
		for i := range jobs.Items {
			j := jobs.Items[i]
			_ = k8sClient.Delete(ctx, &j)
		}
	}

	driveToJobCompletion := func(planName string, r *PlanReconciler, envReader *mapEnvReader) {
		waitForCacheSync(planName, "default", &tideprojectv1alpha1.Plan{})
		Expect(reconcileWithRetry(r.Reconcile, types.NamespacedName{Name: planName, Namespace: "default"}, 5)).To(Succeed())
		var got tideprojectv1alpha1.Plan
		Eventually(func() error {
			return mgrClient.Get(ctx, types.NamespacedName{Name: planName, Namespace: "default"}, &got)
		}, 5*time.Second, 50*time.Millisecond).Should(Succeed())
		jobName := fmt.Sprintf("tide-plan-%s-1", got.UID)
		envReader.SetOut(string(got.UID), pkgdispatch.EnvelopeOut{
			TaskUID:  string(got.UID),
			ExitCode: 0,
		})
		Expect(makeFakeJobTerminal(ctx, mgrClient, jobName, "default", true)).To(Succeed())
		Expect(reconcileWithRetry(r.Reconcile, types.NamespacedName{Name: planName, Namespace: "default"}, 3)).To(Succeed())
	}

	Describe("Test 6a — gates.plan=approve: AwaitingApproval", func() {
		const projectName, msName, phaseName, planName = "gate-proj-pl1", "gate-ms-pl1", "gate-phase-pl1", "gate-plan-1"

		BeforeEach(func() {
			makeProjectChain(projectName, msName, phaseName, tideprojectv1alpha1.Gates{Plan: gates.PolicyApprove})
		})
		AfterEach(func() { cleanup(projectName, msName, phaseName, planName) })

		It("Plan patches Status.Phase=AwaitingApproval", func() {
			plan := &tideprojectv1alpha1.Plan{
				ObjectMeta: metav1.ObjectMeta{Name: planName, Namespace: "default"},
				Spec:       tideprojectv1alpha1.PlanSpec{PhaseRef: phaseName},
			}
			Expect(k8sClient.Create(ctx, plan)).To(Succeed())

			envReader := newMapEnvReader()
			r := &PlanReconciler{
				Client:         mgrClient,
				Scheme:         k8sClient.Scheme(),
				Dispatcher:     &stubDispatcher{},
				PlannerPool:    newPlannerPoolForTest(),
				EnvReader:      envReader,
				SubagentImage:  testSubagentImage,
				CredproxyImage: testCredproxyImage,
				SigningKey:     testSigningKey,
			}
			driveToJobCompletion(planName, r, envReader)

			Eventually(func(g Gomega) {
				var after tideprojectv1alpha1.Plan
				g.Expect(mgrClient.Get(ctx, types.NamespacedName{Name: planName, Namespace: "default"}, &after)).To(Succeed())
				g.Expect(after.Status.Phase).To(Equal("AwaitingApproval"))
				c := meta.FindStatusCondition(after.Status.Conditions, tideprojectv1alpha1.ConditionWaveOrLevelPaused)
				g.Expect(c).NotTo(BeNil())
				g.Expect(c.Reason).To(Equal(tideprojectv1alpha1.ReasonAwaitingApproval))
			}, 5*time.Second, 100*time.Millisecond).Should(Succeed())
		})
	})

	Describe("Test 6b — approve-plan annotation: resumes and clears Running phase", func() {
		const projectName, msName, phaseName, planName = "gate-proj-pl2", "gate-ms-pl2", "gate-phase-pl2", "gate-plan-2"

		BeforeEach(func() {
			makeProjectChain(projectName, msName, phaseName, tideprojectv1alpha1.Gates{Plan: gates.PolicyApprove})
		})
		AfterEach(func() { cleanup(projectName, msName, phaseName, planName) })

		It("consumes annotation and the Plan exits the AwaitingApproval state", func() {
			plan := &tideprojectv1alpha1.Plan{
				ObjectMeta: metav1.ObjectMeta{Name: planName, Namespace: "default"},
				Spec:       tideprojectv1alpha1.PlanSpec{PhaseRef: phaseName},
			}
			Expect(k8sClient.Create(ctx, plan)).To(Succeed())

			envReader := newMapEnvReader()
			r := &PlanReconciler{
				Client:         mgrClient,
				Scheme:         k8sClient.Scheme(),
				Dispatcher:     &stubDispatcher{},
				PlannerPool:    newPlannerPoolForTest(),
				EnvReader:      envReader,
				SubagentImage:  testSubagentImage,
				CredproxyImage: testCredproxyImage,
				SigningKey:     testSigningKey,
			}
			driveToJobCompletion(planName, r, envReader)

			Eventually(func() string {
				var after tideprojectv1alpha1.Plan
				if err := mgrClient.Get(ctx, types.NamespacedName{Name: planName, Namespace: "default"}, &after); err != nil {
					return ""
				}
				return after.Status.Phase
			}, 5*time.Second, 100*time.Millisecond).Should(Equal("AwaitingApproval"))

			var current tideprojectv1alpha1.Plan
			Expect(mgrClient.Get(ctx, types.NamespacedName{Name: planName, Namespace: "default"}, &current)).To(Succeed())
			patch := client.MergeFrom(current.DeepCopy())
			if current.Annotations == nil {
				current.Annotations = map[string]string{}
			}
			current.Annotations[gates.AnnotationApprovePrefix+"plan"] = "true"
			Expect(k8sClient.Patch(ctx, &current, patch)).To(Succeed())

			Expect(reconcileWithRetry(r.Reconcile, types.NamespacedName{Name: planName, Namespace: "default"}, 5)).To(Succeed())

			Eventually(func(g Gomega) {
				var after tideprojectv1alpha1.Plan
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
			makeProjectChain(projectName, msName, phaseName, tideprojectv1alpha1.Gates{Plan: gates.PolicyAuto})
			var proj tideprojectv1alpha1.Project
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: projectName, Namespace: "default"}, &proj)).To(Succeed())
			patch := client.MergeFrom(proj.DeepCopy())
			if proj.Annotations == nil {
				proj.Annotations = map[string]string{}
			}
			proj.Annotations[gates.AnnotationReject] = "plan halt"
			Expect(k8sClient.Patch(ctx, &proj, patch)).To(Succeed())
			Eventually(func() string {
				var p tideprojectv1alpha1.Project
				if err := mgrClient.Get(ctx, types.NamespacedName{Name: projectName, Namespace: "default"}, &p); err != nil {
					return ""
				}
				return p.Annotations[gates.AnnotationReject]
			}, 5*time.Second, 50*time.Millisecond).Should(Equal("plan halt"))
		})
		AfterEach(func() { cleanup(projectName, msName, phaseName, planName) })

		It("Plan is parked with ConditionWaveOrLevelPaused/RejectedByUser (NOT Failed), then recovers after annotation clear", func() {
			plan := &tideprojectv1alpha1.Plan{
				ObjectMeta: metav1.ObjectMeta{Name: planName, Namespace: "default"},
				Spec:       tideprojectv1alpha1.PlanSpec{PhaseRef: phaseName},
			}
			Expect(k8sClient.Create(ctx, plan)).To(Succeed())
			waitForCacheSync(planName, "default", &tideprojectv1alpha1.Plan{})

			envReader := newMapEnvReader()
			r := &PlanReconciler{
				Client:         mgrClient,
				Scheme:         k8sClient.Scheme(),
				Dispatcher:     &stubDispatcher{},
				PlannerPool:    newPlannerPoolForTest(),
				EnvReader:      envReader,
				SubagentImage:  testSubagentImage,
				CredproxyImage: testCredproxyImage,
				SigningKey:     testSigningKey,
			}
			// D-05 dispatch-entry hold fires before Job creation — drive reconcile directly.
			Expect(reconcileWithRetry(r.Reconcile, types.NamespacedName{Name: planName, Namespace: "default"}, 3)).To(Succeed())

			// D-05: reject parks — Status.Phase must NOT be "Failed".
			Eventually(func(g Gomega) {
				var after tideprojectv1alpha1.Plan
				g.Expect(mgrClient.Get(ctx, types.NamespacedName{Name: planName, Namespace: "default"}, &after)).To(Succeed())
				g.Expect(after.Status.Phase).NotTo(Equal("Failed"),
					"D-05: reject must park the Plan, not fail-mark it")
				c := meta.FindStatusCondition(after.Status.Conditions, tideprojectv1alpha1.ConditionWaveOrLevelPaused)
				g.Expect(c).NotTo(BeNil(), "ConditionWaveOrLevelPaused must be set when parked")
				g.Expect(c.Status).To(Equal(metav1.ConditionTrue))
				g.Expect(c.Reason).To(Equal(tideprojectv1alpha1.ReasonRejectedByUser))
				g.Expect(c.Message).To(ContainSubstring("plan halt"))
			}, 5*time.Second, 100*time.Millisecond).Should(Succeed())

			// D-05 recovery: clear the reject annotation (simulating tide resume).
			var current tideprojectv1alpha1.Project
			Expect(mgrClient.Get(ctx, types.NamespacedName{Name: projectName, Namespace: "default"}, &current)).To(Succeed())
			newAnno := gates.ConsumeReject(&current)
			annoPatch := client.MergeFrom(current.DeepCopy())
			current.SetAnnotations(newAnno)
			Expect(k8sClient.Patch(ctx, &current, annoPatch)).To(Succeed())
			Eventually(func() string {
				var p tideprojectv1alpha1.Project
				if err := mgrClient.Get(ctx, types.NamespacedName{Name: projectName, Namespace: "default"}, &p); err != nil {
					return "err"
				}
				return p.Annotations[gates.AnnotationReject]
			}, 5*time.Second, 50*time.Millisecond).Should(BeEmpty())

			// After annotation clear, re-driving must let the Plan proceed.
			driveToJobCompletion(planName, r, envReader)
			Eventually(func(g Gomega) {
				var after tideprojectv1alpha1.Plan
				g.Expect(mgrClient.Get(ctx, types.NamespacedName{Name: planName, Namespace: "default"}, &after)).To(Succeed())
				g.Expect(after.Status.Phase).NotTo(Equal("Failed"),
					"D-05: Plan must not be Failed after reject annotation cleared")
			}, 5*time.Second, 100*time.Millisecond).Should(Succeed())
		})
	})
})
