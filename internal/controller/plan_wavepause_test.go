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
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	tideprojectv1alpha2 "github.com/jsquirrelz/tide/api/v1alpha2"
	"github.com/jsquirrelz/tide/internal/gates"
)

// PauseBetweenWaves tests for PlanReconciler (Plan 04-05 Task 2).
//
// Fixture: a 2-wave DAG: wave 0 = {alpha, beta}, wave 1 = {gamma DependsOn alpha}.
// When `Project.Spec.Gates.PauseBetweenWaves == true` and wave 0 has all
// tasks Succeeded, PlanReconciler must surface a Condition WaveOrLevelPaused
// True (Reason=PausedAtBoundary) until annotation approve-wave-1 arrives.
var _ = Describe("PlanReconciler — PauseBetweenWaves (Plan 04-05 Task 2)", Label("envtest", "phase4", "wavepause"), func() {
	ctx := context.Background()

	// makeProjectWithPause creates a Project (with PauseBetweenWaves), the
	// Milestone+Phase chain so resolveProjectForPlan succeeds, then a Plan with
	// a 2-wave Task DAG. Returns the Plan name.
	makeProjectWithPause := func(projectName, msName, phaseName, planName string, pause bool) string {
		proj := &tideprojectv1alpha2.Project{
			ObjectMeta: metav1.ObjectMeta{Name: projectName, Namespace: "default"},
			Spec: tideprojectv1alpha2.ProjectSpec{SchemaRevision: "v1alpha2",
				TargetRepo: "https://github.com/example/tide.git",
				Gates:      tideprojectv1alpha2.Gates{PauseBetweenWaves: pause},
			},
		}
		Expect(k8sClient.Create(ctx, proj)).To(Succeed())
		waitForCacheSync(projectName, "default", &tideprojectv1alpha2.Project{})
		ms := &tideprojectv1alpha2.Milestone{
			ObjectMeta: metav1.ObjectMeta{Name: msName, Namespace: "default"},
			Spec:       tideprojectv1alpha2.MilestoneSpec{ProjectRef: projectName},
		}
		Expect(k8sClient.Create(ctx, ms)).To(Succeed())
		waitForCacheSync(msName, "default", &tideprojectv1alpha2.Milestone{})
		ph := &tideprojectv1alpha2.Phase{
			ObjectMeta: metav1.ObjectMeta{Name: phaseName, Namespace: "default"},
			Spec:       tideprojectv1alpha2.PhaseSpec{MilestoneRef: msName},
		}
		Expect(k8sClient.Create(ctx, ph)).To(Succeed())
		waitForCacheSync(phaseName, "default", &tideprojectv1alpha2.Phase{})
		makePlan(planName, phaseName, "Validated")
		// 2-wave DAG: alpha (w0), beta (w0), gamma DependsOn alpha (w1).
		makeTask(planName+"-alpha", planName, nil, projectName)
		makeTask(planName+"-beta", planName, nil, projectName)
		makeTask(planName+"-gamma", planName, []string{planName + "-alpha"}, projectName)
		return planName
	}

	cleanupWavepauseFixture := func(projectName, msName, phaseName, planName string) {
		for _, suffix := range []string{"-alpha", "-beta", "-gamma"} {
			cleanupTask(planName + suffix)
		}
		cleanupPlanFixture(planName, nil)
		ph := &tideprojectv1alpha2.Phase{}
		if err := k8sClient.Get(ctx, types.NamespacedName{Name: phaseName, Namespace: "default"}, ph); err == nil {
			ph.Finalizers = nil
			_ = k8sClient.Update(ctx, ph)
			_ = k8sClient.Delete(ctx, ph)
		}
		ms := &tideprojectv1alpha2.Milestone{}
		if err := k8sClient.Get(ctx, types.NamespacedName{Name: msName, Namespace: "default"}, ms); err == nil {
			ms.Finalizers = nil
			_ = k8sClient.Update(ctx, ms)
			_ = k8sClient.Delete(ctx, ms)
		}
		cleanupProject(projectName)
	}

	Describe("Test 1 — wave 0 Succeeded, gates.pauseBetweenWaves=true: Plan condition WaveOrLevelPaused True", func() {
		const projectName = "wpause-proj-1"
		const msName = "wpause-ms-1"
		const phaseName = "wpause-ph-1"
		const planName = "wpause-plan-1"

		BeforeEach(func() {
			makeProjectWithPause(projectName, msName, phaseName, planName, true)
		})
		AfterEach(func() {
			cleanupWavepauseFixture(projectName, msName, phaseName, planName)
		})

		It("Plan carries Condition WaveOrLevelPaused True with Reason=PausedAtBoundary and Message referencing wave 1", func() {
			r := newPlanReconciler()
			planNS := types.NamespacedName{Name: planName, Namespace: "default"}

			// Drive Plan reconcile: materializes Waves + stamps Task labels.
			_, err := reconcilePlanN(r, planNS, 5)
			Expect(err).NotTo(HaveOccurred())

			// Mark wave-0 tasks Succeeded (alpha + beta).
			markTaskSucceeded(planName + "-alpha")
			markTaskSucceeded(planName + "-beta")

			// Reconcile again — should detect the wave boundary and pause.
			_, err = reconcilePlanN(r, planNS, 3)
			Expect(err).NotTo(HaveOccurred())

			Eventually(func(g Gomega) {
				var after tideprojectv1alpha2.Plan
				g.Expect(mgrClient.Get(ctx, planNS, &after)).To(Succeed())
				c := meta.FindStatusCondition(after.Status.Conditions, tideprojectv1alpha2.ConditionWaveOrLevelPaused)
				g.Expect(c).NotTo(BeNil())
				g.Expect(c.Status).To(Equal(metav1.ConditionTrue))
				g.Expect(c.Reason).To(Equal(tideprojectv1alpha2.ReasonPausedAtBoundary))
				g.Expect(c.Message).To(ContainSubstring("wave 1"))
			}, 5*time.Second, 100*time.Millisecond).Should(Succeed())
		})
	})

	Describe("Test 2 — approve-wave-1 annotation: consume, clear condition, allow dispatch", func() {
		const projectName = "wpause-proj-2"
		const msName = "wpause-ms-2"
		const phaseName = "wpause-ph-2"
		const planName = "wpause-plan-2"

		BeforeEach(func() {
			makeProjectWithPause(projectName, msName, phaseName, planName, true)
		})
		AfterEach(func() {
			cleanupWavepauseFixture(projectName, msName, phaseName, planName)
		})

		It("ConsumeWaveApprove removes the annotation and the condition flips False", func() {
			r := newPlanReconciler()
			planNS := types.NamespacedName{Name: planName, Namespace: "default"}

			_, err := reconcilePlanN(r, planNS, 5)
			Expect(err).NotTo(HaveOccurred())

			markTaskSucceeded(planName + "-alpha")
			markTaskSucceeded(planName + "-beta")
			_, err = reconcilePlanN(r, planNS, 3)
			Expect(err).NotTo(HaveOccurred())

			// Sanity: pause condition is set.
			Eventually(func() bool {
				var p tideprojectv1alpha2.Plan
				if err := mgrClient.Get(ctx, planNS, &p); err != nil {
					return false
				}
				c := meta.FindStatusCondition(p.Status.Conditions, tideprojectv1alpha2.ConditionWaveOrLevelPaused)
				return c != nil && c.Status == metav1.ConditionTrue
			}, 5*time.Second, 100*time.Millisecond).Should(BeTrue())

			// Apply approve-wave-1 annotation.
			var current tideprojectv1alpha2.Plan
			Expect(mgrClient.Get(ctx, planNS, &current)).To(Succeed())
			patch := client.MergeFrom(current.DeepCopy())
			if current.Annotations == nil {
				current.Annotations = map[string]string{}
			}
			current.Annotations[gates.AnnotationApproveWavePrefix+"1"] = "true"
			Expect(k8sClient.Patch(ctx, &current, patch)).To(Succeed())

			_, err = reconcilePlanN(r, planNS, 5)
			Expect(err).NotTo(HaveOccurred())

			Eventually(func(g Gomega) {
				var after tideprojectv1alpha2.Plan
				g.Expect(mgrClient.Get(ctx, planNS, &after)).To(Succeed())
				c := meta.FindStatusCondition(after.Status.Conditions, tideprojectv1alpha2.ConditionWaveOrLevelPaused)
				g.Expect(c).NotTo(BeNil())
				g.Expect(c.Status).To(Equal(metav1.ConditionFalse))
				_, has := after.Annotations[gates.AnnotationApproveWavePrefix+"1"]
				g.Expect(has).To(BeFalse(), "approve-wave-1 annotation should be consumed")
			}, 5*time.Second, 100*time.Millisecond).Should(Succeed())
		})
	})

	Describe("Test 3 — PauseBetweenWaves=false: no pause condition set", func() {
		const projectName = "wpause-proj-3"
		const msName = "wpause-ms-3"
		const phaseName = "wpause-ph-3"
		const planName = "wpause-plan-3"

		BeforeEach(func() {
			makeProjectWithPause(projectName, msName, phaseName, planName, false)
		})
		AfterEach(func() {
			cleanupWavepauseFixture(projectName, msName, phaseName, planName)
		})

		It("No WaveOrLevelPaused condition is set (today's behavior preserved)", func() {
			r := newPlanReconciler()
			planNS := types.NamespacedName{Name: planName, Namespace: "default"}

			_, err := reconcilePlanN(r, planNS, 5)
			Expect(err).NotTo(HaveOccurred())

			markTaskSucceeded(planName + "-alpha")
			markTaskSucceeded(planName + "-beta")

			_, err = reconcilePlanN(r, planNS, 3)
			Expect(err).NotTo(HaveOccurred())

			Consistently(func(g Gomega) {
				var after tideprojectv1alpha2.Plan
				g.Expect(mgrClient.Get(ctx, planNS, &after)).To(Succeed())
				c := meta.FindStatusCondition(after.Status.Conditions, tideprojectv1alpha2.ConditionWaveOrLevelPaused)
				if c == nil {
					return // never set — happy path
				}
				// If present, must be False (e.g., ResumedByUser); never True with PausedAtBoundary.
				g.Expect(c.Status).NotTo(Equal(metav1.ConditionTrue))
			}, 2*time.Second, 250*time.Millisecond).Should(Succeed())
		})
	})

	Describe("Test 4 — AnnotationChangedPredicate wired into Wave + Plan SetupWithManager (grep contract)", func() {
		It("Wave + Plan + Milestone + Phase + Task controllers carry AnnotationChangedPredicate in source", func() {
			// This is a documentation-level grep contract: the production source
			// files MUST contain the predicate. Inline source-grep test surfaces
			// the contract as a unit-level assertion alongside the runtime tests.
			for _, file := range []string{
				"milestone_controller.go",
				"phase_controller.go",
				"plan_controller.go",
				"task_controller.go",
				"wave_controller.go",
			} {
				data, err := readSourceFile(file)
				Expect(err).NotTo(HaveOccurred(), "reading %s", file)
				Expect(strings.Contains(data, "AnnotationChangedPredicate")).To(BeTrue(),
					"%s missing AnnotationChangedPredicate", file)
			}
		})
	})
})
