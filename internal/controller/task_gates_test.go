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
)

// gate-flow tests for TaskReconciler (Plan 04-05 Task 1).
//
// The Task gate hook fires BEFORE Job dispatch in the 6-step reconcileDispatch
// body (Tasks have no children, so the gate seam is at the pre-dispatch point).
// Per the plan: gates.task=auto is the default; explicit "approve" pauses
// before dispatching the executor Job.
var _ = Describe("TaskReconciler — gate-policy hook (Plan 04-05 Task 1)", Label("envtest", "phase4", "gates"), func() {
	ctx := context.Background()

	makeProjectWithGates := func(name string, g tideprojectv1alpha3.Gates) *tideprojectv1alpha3.Project {
		p := &tideprojectv1alpha3.Project{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
			Spec: tideprojectv1alpha3.ProjectSpec{SchemaRevision: "v1alpha3",
				TargetRepo: "https://github.com/example/tide.git",
				Gates:      g,
			},
		}
		Expect(k8sClient.Create(ctx, p)).To(Succeed())
		waitForCacheSync(name, "default", &tideprojectv1alpha3.Project{})
		return p
	}

	Describe("Test 7a — gates.task=approve: AwaitingApproval, no Job created", func() {
		const projectName, planRef, taskName = "gate-proj-tk1", "gate-plan-tk1", "gate-task-1"

		BeforeEach(func() {
			makeProjectWithGates(projectName, tideprojectv1alpha3.Gates{Task: gates.PolicyApprove})
			makeTask(taskName, planRef, nil, projectName)
		})
		AfterEach(func() {
			cleanupTask(taskName)
			cleanupProject(projectName)
			var jobs batchv1.JobList
			_ = k8sClient.List(ctx, &jobs, client.InNamespace("default"))
			for i := range jobs.Items {
				j := jobs.Items[i]
				_ = k8sClient.Delete(ctx, &j)
			}
		})

		It("Task is parked AwaitingApproval and no Job is created", func() {
			r := newTaskReconciler(newMapEnvReader())
			name := types.NamespacedName{Name: taskName, Namespace: "default"}

			_, err := reconcileN(r, name, 5)
			Expect(err).NotTo(HaveOccurred())

			Eventually(func(g Gomega) {
				var t tideprojectv1alpha3.Task
				g.Expect(mgrClient.Get(ctx, name, &t)).To(Succeed())
				g.Expect(t.Status.Phase).To(Equal("AwaitingApproval"))
				c := meta.FindStatusCondition(t.Status.Conditions, tideprojectv1alpha3.ConditionWaveOrLevelPaused)
				g.Expect(c).NotTo(BeNil())
				g.Expect(c.Reason).To(Equal(tideprojectv1alpha3.ReasonAwaitingApproval))
			}, 5*time.Second, 100*time.Millisecond).Should(Succeed())

			// No Job should have been created for this task while paused.
			var jobs batchv1.JobList
			Expect(k8sClient.List(ctx, &jobs, client.InNamespace("default"))).To(Succeed())
			for _, j := range jobs.Items {
				Expect(j.Labels["tideproject.k8s/task-uid"]).NotTo(Equal(string(getTaskUID(taskName))))
			}
		})
	})

	Describe("Test 7b — reject annotation on Project parks Task with RejectedByUser condition (D-05)", func() {
		const projectName, planRef, taskName = "gate-proj-tk2", "gate-plan-tk2", "gate-task-2"

		BeforeEach(func() {
			makeProjectWithGates(projectName, tideprojectv1alpha3.Gates{Task: gates.PolicyAuto})
			// Stamp reject annotation on the Project.
			var proj tideprojectv1alpha3.Project
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: projectName, Namespace: "default"}, &proj)).To(Succeed())
			patch := client.MergeFrom(proj.DeepCopy())
			if proj.Annotations == nil {
				proj.Annotations = map[string]string{}
			}
			proj.Annotations[gates.AnnotationReject] = "task halt"
			Expect(k8sClient.Patch(ctx, &proj, patch)).To(Succeed())
			Eventually(func() string {
				var p tideprojectv1alpha3.Project
				if err := mgrClient.Get(ctx, types.NamespacedName{Name: projectName, Namespace: "default"}, &p); err != nil {
					return ""
				}
				return p.Annotations[gates.AnnotationReject]
			}, 5*time.Second, 50*time.Millisecond).Should(Equal("task halt"))
			makeTask(taskName, planRef, nil, projectName)
		})
		AfterEach(func() {
			cleanupTask(taskName)
			cleanupProject(projectName)
		})

		It("Task is parked with ConditionWaveOrLevelPaused/RejectedByUser (NOT Failed), then recovers after annotation clear", func() {
			r := newTaskReconciler(newMapEnvReader())
			name := types.NamespacedName{Name: taskName, Namespace: "default"}

			_, err := reconcileN(r, name, 5)
			Expect(err).NotTo(HaveOccurred())

			// D-05: reject parks — Status.Phase must NOT be "Failed".
			Eventually(func(g Gomega) {
				var t tideprojectv1alpha3.Task
				g.Expect(mgrClient.Get(ctx, name, &t)).To(Succeed())
				g.Expect(t.Status.Phase).NotTo(Equal("Failed"),
					"D-05: reject must park the Task, not fail-mark it")
				c := meta.FindStatusCondition(t.Status.Conditions, tideprojectv1alpha3.ConditionWaveOrLevelPaused)
				g.Expect(c).NotTo(BeNil(), "ConditionWaveOrLevelPaused must be set when parked")
				g.Expect(c.Status).To(Equal(metav1.ConditionTrue))
				g.Expect(c.Reason).To(Equal(tideprojectv1alpha3.ReasonRejectedByUser))
				g.Expect(c.Message).To(ContainSubstring("task halt"))
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

			// After annotation clear, re-driving must let the Task proceed (no longer halted).
			_, err = reconcileN(r, name, 3)
			Expect(err).NotTo(HaveOccurred())
			Eventually(func(g Gomega) {
				var t tideprojectv1alpha3.Task
				g.Expect(mgrClient.Get(ctx, name, &t)).To(Succeed())
				g.Expect(t.Status.Phase).NotTo(Equal("Failed"),
					"D-05: Task must not be Failed after reject annotation cleared")
			}, 5*time.Second, 100*time.Millisecond).Should(Succeed())
		})
	})

	Describe("Test 7c — gates.task=auto: dispatches Job normally", func() {
		const projectName, planRef, taskName = "gate-proj-tk3", "gate-plan-tk3", "gate-task-3"

		BeforeEach(func() {
			makeProjectWithGates(projectName, tideprojectv1alpha3.Gates{Task: gates.PolicyAuto})
			makeTask(taskName, planRef, nil, projectName)
		})
		AfterEach(func() {
			cleanupTask(taskName)
			cleanupProject(projectName)
			var jobs batchv1.JobList
			_ = k8sClient.List(ctx, &jobs, client.InNamespace("default"))
			for i := range jobs.Items {
				j := jobs.Items[i]
				_ = k8sClient.Delete(ctx, &j)
			}
		})

		It("Task transitions to Running (today's behavior preserved)", func() {
			r := newTaskReconciler(newMapEnvReader())
			name := types.NamespacedName{Name: taskName, Namespace: "default"}

			_, err := reconcileN(r, name, 5)
			Expect(err).NotTo(HaveOccurred())

			Eventually(func(g Gomega) {
				var t tideprojectv1alpha3.Task
				g.Expect(mgrClient.Get(ctx, name, &t)).To(Succeed())
				g.Expect(t.Status.Phase).To(Equal("Running"))
			}, 5*time.Second, 100*time.Millisecond).Should(Succeed())
		})
	})
})

// Phase 13 HALT-01 / D-05: BillingHalt dispatch-entry hold at the Task level.
//
// The hold is the third dispatch-entry guard after CheckRejected + checkParentApproval.
// Park semantics (D-05): log at V(1), requeue 30s, Status.Phase unchanged, no per-Task
// condition written (avoids status flapping — the operator-visible signal is the single
// Project BillingHalt condition).
var _ = Describe("TaskReconciler — BillingHalt dispatch-entry hold (Phase 13 HALT-01)", Label("envtest", "phase13", "billing-halt"), func() {
	ctx := context.Background()

	Describe("Test 13a — BillingHalt=True on Project parks Task, no Job created", func() {
		const projectName, planRef, taskName = "billing-halt-proj-1", "billing-halt-plan-1", "billing-halt-task-1"

		BeforeEach(func() {
			// Create project
			p := &tideprojectv1alpha3.Project{
				ObjectMeta: metav1.ObjectMeta{Name: projectName, Namespace: "default"},
				Spec: tideprojectv1alpha3.ProjectSpec{SchemaRevision: "v1alpha3",
					TargetRepo: "https://github.com/example/tide.git",
				},
			}
			Expect(k8sClient.Create(ctx, p)).To(Succeed())
			waitForCacheSync(projectName, "default", &tideprojectv1alpha3.Project{})

			// Stamp BillingHalt=True condition on the Project.
			var proj tideprojectv1alpha3.Project
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: projectName, Namespace: "default"}, &proj)).To(Succeed())
			statusPatch := client.MergeFrom(proj.DeepCopy())
			meta.SetStatusCondition(&proj.Status.Conditions, metav1.Condition{
				Type:               tideprojectv1alpha3.ConditionBillingHalt,
				Status:             metav1.ConditionTrue,
				Reason:             tideprojectv1alpha3.ReasonCreditBalanceTooLow,
				Message:            "Test: billing halt stamped",
				LastTransitionTime: metav1.Now(),
			})
			Expect(k8sClient.Status().Patch(ctx, &proj, statusPatch)).To(Succeed())
			Eventually(func() bool {
				var p tideprojectv1alpha3.Project
				if err := mgrClient.Get(ctx, types.NamespacedName{Name: projectName, Namespace: "default"}, &p); err != nil {
					return false
				}
				c := meta.FindStatusCondition(p.Status.Conditions, tideprojectv1alpha3.ConditionBillingHalt)
				return c != nil && c.Status == metav1.ConditionTrue
			}, 5*time.Second, 50*time.Millisecond).Should(BeTrue(), "BillingHalt condition should be visible in cache")

			makeTask(taskName, planRef, nil, projectName)
		})
		AfterEach(func() {
			cleanupTask(taskName)
			cleanupProject(projectName)
			var jobs batchv1.JobList
			_ = k8sClient.List(ctx, &jobs, client.InNamespace("default"))
			for i := range jobs.Items {
				j := jobs.Items[i]
				if j.Labels["tideproject.k8s/project"] == projectName {
					_ = k8sClient.Delete(ctx, &j)
				}
			}
		})

		It("Task stays parked (Status.Phase unchanged, no Job), 30s requeue on BillingHalt", func() {
			r := newTaskReconciler(newMapEnvReader())
			name := types.NamespacedName{Name: taskName, Namespace: "default"}

			result, err := reconcileN(r, name, 5)
			Expect(err).NotTo(HaveOccurred())

			// Park semantics: requeue with 30s interval (billing recovery is operator-paced).
			Expect(result.RequeueAfter).To(Equal(30*time.Second),
				"BillingHalt hold must park with 30s requeue, not fail or no-requeue")

			// Status.Phase must NOT be changed to "Failed" — park-not-fail (D-05, T-13-15).
			var t tideprojectv1alpha3.Task
			Expect(mgrClient.Get(ctx, name, &t)).To(Succeed())
			Expect(t.Status.Phase).NotTo(Equal("Failed"),
				"D-05/T-13-15: BillingHalt must park the Task (not fail) — wave-boundary failure semantics preserved")

			// No per-Task BillingHalt condition written (avoids status flapping per dogfood finding).
			billingCond := meta.FindStatusCondition(t.Status.Conditions, tideprojectv1alpha3.ConditionBillingHalt)
			Expect(billingCond).To(BeNil(),
				"HALT-01 park: no per-Task BillingHalt condition should be written (operator signal is on Project only)")

			// No Job should be created for this task while halted (T-13-12: bypass mitigation).
			taskUID := t.UID
			var jobs batchv1.JobList
			Expect(k8sClient.List(ctx, &jobs, client.InNamespace("default"))).To(Succeed())
			for _, j := range jobs.Items {
				Expect(j.Labels["tideproject.k8s/task-uid"]).NotTo(Equal(string(taskUID)),
					"BillingHalt hold must prevent Job dispatch entirely")
			}
		})
	})

	Describe("Test 13b — BillingHalt cleared: dispatch resumes (no Job while halted, Job after clear)", func() {
		const projectName, planRef, taskName = "billing-halt-proj-2", "billing-halt-plan-2", "billing-halt-task-2"

		BeforeEach(func() {
			p := &tideprojectv1alpha3.Project{
				ObjectMeta: metav1.ObjectMeta{Name: projectName, Namespace: "default"},
				Spec: tideprojectv1alpha3.ProjectSpec{SchemaRevision: "v1alpha3",
					TargetRepo: "https://github.com/example/tide.git",
				},
			}
			Expect(k8sClient.Create(ctx, p)).To(Succeed())
			waitForCacheSync(projectName, "default", &tideprojectv1alpha3.Project{})

			// Stamp BillingHalt=True on the Project.
			var proj tideprojectv1alpha3.Project
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: projectName, Namespace: "default"}, &proj)).To(Succeed())
			statusPatch := client.MergeFrom(proj.DeepCopy())
			meta.SetStatusCondition(&proj.Status.Conditions, metav1.Condition{
				Type:               tideprojectv1alpha3.ConditionBillingHalt,
				Status:             metav1.ConditionTrue,
				Reason:             tideprojectv1alpha3.ReasonCreditBalanceTooLow,
				Message:            "Test: billing halt stamped",
				LastTransitionTime: metav1.Now(),
			})
			Expect(k8sClient.Status().Patch(ctx, &proj, statusPatch)).To(Succeed())
			Eventually(func() bool {
				var p tideprojectv1alpha3.Project
				if err := mgrClient.Get(ctx, types.NamespacedName{Name: projectName, Namespace: "default"}, &p); err != nil {
					return false
				}
				c := meta.FindStatusCondition(p.Status.Conditions, tideprojectv1alpha3.ConditionBillingHalt)
				return c != nil && c.Status == metav1.ConditionTrue
			}, 5*time.Second, 50*time.Millisecond).Should(BeTrue())

			makeTask(taskName, planRef, nil, projectName)
		})
		AfterEach(func() {
			cleanupTask(taskName)
			cleanupProject(projectName)
			var jobs batchv1.JobList
			_ = k8sClient.List(ctx, &jobs, client.InNamespace("default"))
			for i := range jobs.Items {
				j := jobs.Items[i]
				if j.Labels["tideproject.k8s/project"] == projectName {
					_ = k8sClient.Delete(ctx, &j)
				}
			}
		})

		It("Task parked while BillingHalt present, dispatches Job after condition cleared", func() {
			r := newTaskReconciler(newMapEnvReader())
			name := types.NamespacedName{Name: taskName, Namespace: "default"}

			// While halted: reconcile must park with 30s requeue.
			result, err := reconcileN(r, name, 3)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).To(Equal(30 * time.Second))

			// Clear the BillingHalt condition (mirrors what `tide resume` does via
			// meta.RemoveStatusCondition + status update — D-06).
			var current tideprojectv1alpha3.Project
			Expect(mgrClient.Get(ctx, types.NamespacedName{Name: projectName, Namespace: "default"}, &current)).To(Succeed())
			clearPatch := client.MergeFrom(current.DeepCopy())
			meta.RemoveStatusCondition(&current.Status.Conditions, tideprojectv1alpha3.ConditionBillingHalt)
			Expect(k8sClient.Status().Patch(ctx, &current, clearPatch)).To(Succeed())
			Eventually(func() bool {
				var p tideprojectv1alpha3.Project
				if err := mgrClient.Get(ctx, types.NamespacedName{Name: projectName, Namespace: "default"}, &p); err != nil {
					return false
				}
				c := meta.FindStatusCondition(p.Status.Conditions, tideprojectv1alpha3.ConditionBillingHalt)
				return c == nil
			}, 5*time.Second, 50*time.Millisecond).Should(BeTrue(), "BillingHalt condition should be removed from cache")

			// After clear, re-reconcile should let dispatch proceed (no longer halted).
			_, err = reconcileN(r, name, 3)
			Expect(err).NotTo(HaveOccurred())

			// Task must not be Failed after halt cleared.
			var t tideprojectv1alpha3.Task
			Expect(mgrClient.Get(ctx, name, &t)).To(Succeed())
			Expect(t.Status.Phase).NotTo(Equal("Failed"),
				"D-05: Task must not be Failed after BillingHalt cleared")

			// Task should have progressed to Running (dispatch resumed).
			Eventually(func(g Gomega) {
				var t tideprojectv1alpha3.Task
				g.Expect(mgrClient.Get(ctx, name, &t)).To(Succeed())
				g.Expect(t.Status.Phase).To(Equal("Running"),
					"Dispatch must resume after BillingHalt condition cleared")
			}, 5*time.Second, 100*time.Millisecond).Should(Succeed())
		})
	})
})

// getTaskUID is a small helper for the assertion in Test 7a; returns "" on miss.
func getTaskUID(name string) types.UID {
	var t tideprojectv1alpha3.Task
	if err := mgrClient.Get(context.Background(), types.NamespacedName{Name: name, Namespace: "default"}, &t); err != nil {
		return ""
	}
	return t.UID
}
