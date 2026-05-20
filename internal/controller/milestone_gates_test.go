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

// gate-flow tests for MilestoneReconciler (Plan 04-05 Task 1).
//
// Each test creates a parent Project with a specific Gates configuration,
// then drives the reconcile through to handleJobCompletion (where the gate
// hook lives) by faking the planner Job into a Succeeded terminal state.
var _ = Describe("MilestoneReconciler — gate-policy hook (Plan 04-05 Task 1)", Label("envtest", "phase4", "gates"), func() {
	ctx := context.Background()

	// shared cleanup helper
	cleanup := func(projectName, msName string) {
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

	makeProject := func(name string, g tideprojectv1alpha1.Gates) {
		proj := &tideprojectv1alpha1.Project{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
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
		waitForCacheSync(name, "default", &tideprojectv1alpha1.Project{})
	}

	driveToJobCompletion := func(msName string, r *MilestoneReconciler, envReader *mapEnvReader) string {
		waitForCacheSync(msName, "default", &tideprojectv1alpha1.Milestone{})
		Expect(reconcileWithRetry(r.Reconcile, types.NamespacedName{Name: msName, Namespace: "default"}, 5)).To(Succeed())
		var got tideprojectv1alpha1.Milestone
		Eventually(func() error {
			return mgrClient.Get(ctx, types.NamespacedName{Name: msName, Namespace: "default"}, &got)
		}, 5*time.Second, 50*time.Millisecond).Should(Succeed())
		jobName := fmt.Sprintf("tide-milestone-%s-1", got.UID)
		envReader.SetOut(string(got.UID), pkgdispatch.EnvelopeOut{
			TaskUID:  string(got.UID),
			ExitCode: 0,
		})
		Expect(makeFakeJobTerminal(ctx, mgrClient, jobName, "default", true)).To(Succeed())
		Expect(reconcileWithRetry(r.Reconcile, types.NamespacedName{Name: msName, Namespace: "default"}, 3)).To(Succeed())
		return string(got.UID)
	}

	Describe("Test 1 — gates.milestone=approve, no annotation: AwaitingApproval", func() {
		const projectName = "gate-proj-ms1"
		const msName = "gate-ms-1"

		BeforeEach(func() {
			makeProject(projectName, tideprojectv1alpha1.Gates{Milestone: gates.PolicyApprove})
		})
		AfterEach(func() { cleanup(projectName, msName) })

		It("patches Status.Phase=AwaitingApproval + Condition WaveOrLevelPaused=True", func() {
			ms := &tideprojectv1alpha1.Milestone{
				ObjectMeta: metav1.ObjectMeta{Name: msName, Namespace: "default"},
				Spec:       tideprojectv1alpha1.MilestoneSpec{ProjectRef: projectName},
			}
			Expect(k8sClient.Create(ctx, ms)).To(Succeed())

			envReader := newMapEnvReader()
			r := &MilestoneReconciler{
				Client:         mgrClient,
				Scheme:         k8sClient.Scheme(),
				Dispatcher:     &stubDispatcher{},
				PlannerPool:    newPlannerPoolForTest(),
				EnvReader:      envReader,
				SubagentImage:  testSubagentImage,
				CredproxyImage: testCredproxyImage,
				SigningKey:      testSigningKey,
			}
			driveToJobCompletion(msName, r, envReader)

			Eventually(func(g Gomega) {
				var after tideprojectv1alpha1.Milestone
				g.Expect(mgrClient.Get(ctx, types.NamespacedName{Name: msName, Namespace: "default"}, &after)).To(Succeed())
				g.Expect(after.Status.Phase).To(Equal("AwaitingApproval"))
				c := meta.FindStatusCondition(after.Status.Conditions, tideprojectv1alpha1.ConditionWaveOrLevelPaused)
				g.Expect(c).NotTo(BeNil())
				g.Expect(c.Status).To(Equal(metav1.ConditionTrue))
				g.Expect(c.Reason).To(Equal(tideprojectv1alpha1.ReasonAwaitingApproval))
			}, 5*time.Second, 100*time.Millisecond).Should(Succeed())
		})
	})

	Describe("Test 2 — approve annotation present: resumes to Succeeded", func() {
		const projectName = "gate-proj-ms2"
		const msName = "gate-ms-2"

		BeforeEach(func() {
			makeProject(projectName, tideprojectv1alpha1.Gates{Milestone: gates.PolicyApprove})
		})
		AfterEach(func() { cleanup(projectName, msName) })

		It("consumes annotation and patches Status.Phase=Succeeded", func() {
			ms := &tideprojectv1alpha1.Milestone{
				ObjectMeta: metav1.ObjectMeta{Name: msName, Namespace: "default"},
				Spec:       tideprojectv1alpha1.MilestoneSpec{ProjectRef: projectName},
			}
			Expect(k8sClient.Create(ctx, ms)).To(Succeed())

			envReader := newMapEnvReader()
			r := &MilestoneReconciler{
				Client:         mgrClient,
				Scheme:         k8sClient.Scheme(),
				Dispatcher:     &stubDispatcher{},
				PlannerPool:    newPlannerPoolForTest(),
				EnvReader:      envReader,
				SubagentImage:  testSubagentImage,
				CredproxyImage: testCredproxyImage,
				SigningKey:      testSigningKey,
			}
			driveToJobCompletion(msName, r, envReader)

			// First gate trip should have parked us at AwaitingApproval.
			Eventually(func(g Gomega) string {
				var after tideprojectv1alpha1.Milestone
				if err := mgrClient.Get(ctx, types.NamespacedName{Name: msName, Namespace: "default"}, &after); err != nil {
					return ""
				}
				return after.Status.Phase
			}, 5*time.Second, 100*time.Millisecond).Should(Equal("AwaitingApproval"))

			// Apply approve annotation.
			var current tideprojectv1alpha1.Milestone
			Expect(mgrClient.Get(ctx, types.NamespacedName{Name: msName, Namespace: "default"}, &current)).To(Succeed())
			patch := client.MergeFrom(current.DeepCopy())
			if current.Annotations == nil {
				current.Annotations = map[string]string{}
			}
			current.Annotations[gates.AnnotationApprovePrefix+"milestone"] = "true"
			Expect(k8sClient.Patch(ctx, &current, patch)).To(Succeed())

			// Reconcile and verify the annotation was consumed and the level Succeeded.
			Expect(reconcileWithRetry(r.Reconcile, types.NamespacedName{Name: msName, Namespace: "default"}, 5)).To(Succeed())

			Eventually(func(g Gomega) {
				var after tideprojectv1alpha1.Milestone
				g.Expect(mgrClient.Get(ctx, types.NamespacedName{Name: msName, Namespace: "default"}, &after)).To(Succeed())
				g.Expect(after.Status.Phase).To(Equal("Succeeded"))
				_, has := after.Annotations[gates.AnnotationApprovePrefix+"milestone"]
				g.Expect(has).To(BeFalse(), "approve-milestone annotation should be consumed")
			}, 5*time.Second, 100*time.Millisecond).Should(Succeed())
		})
	})

	Describe("Test 3 — reject annotation on Project: short-circuits to Failed/RejectedByUser", func() {
		const projectName = "gate-proj-ms3"
		const msName = "gate-ms-3"

		BeforeEach(func() {
			makeProject(projectName, tideprojectv1alpha1.Gates{Milestone: gates.PolicyAuto})
			// Stamp reject annotation on the Project.
			var proj tideprojectv1alpha1.Project
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: projectName, Namespace: "default"}, &proj)).To(Succeed())
			patch := client.MergeFrom(proj.DeepCopy())
			if proj.Annotations == nil {
				proj.Annotations = map[string]string{}
			}
			proj.Annotations[gates.AnnotationReject] = "operator halt"
			Expect(k8sClient.Patch(ctx, &proj, patch)).To(Succeed())
			Eventually(func() string {
				var p tideprojectv1alpha1.Project
				if err := mgrClient.Get(ctx, types.NamespacedName{Name: projectName, Namespace: "default"}, &p); err != nil {
					return ""
				}
				return p.Annotations[gates.AnnotationReject]
			}, 5*time.Second, 50*time.Millisecond).Should(Equal("operator halt"))
		})
		AfterEach(func() { cleanup(projectName, msName) })

		It("Milestone reaches Status.Phase=Failed with Reason=RejectedByUser and the reject reason in Message", func() {
			ms := &tideprojectv1alpha1.Milestone{
				ObjectMeta: metav1.ObjectMeta{Name: msName, Namespace: "default"},
				Spec:       tideprojectv1alpha1.MilestoneSpec{ProjectRef: projectName},
			}
			Expect(k8sClient.Create(ctx, ms)).To(Succeed())

			envReader := newMapEnvReader()
			r := &MilestoneReconciler{
				Client:         mgrClient,
				Scheme:         k8sClient.Scheme(),
				Dispatcher:     &stubDispatcher{},
				PlannerPool:    newPlannerPoolForTest(),
				EnvReader:      envReader,
				SubagentImage:  testSubagentImage,
				CredproxyImage: testCredproxyImage,
				SigningKey:      testSigningKey,
			}
			driveToJobCompletion(msName, r, envReader)

			Eventually(func(g Gomega) {
				var after tideprojectv1alpha1.Milestone
				g.Expect(mgrClient.Get(ctx, types.NamespacedName{Name: msName, Namespace: "default"}, &after)).To(Succeed())
				g.Expect(after.Status.Phase).To(Equal("Failed"))
				c := meta.FindStatusCondition(after.Status.Conditions, tideprojectv1alpha1.ConditionFailed)
				g.Expect(c).NotTo(BeNil())
				g.Expect(c.Reason).To(Equal(tideprojectv1alpha1.ReasonRejectedByUser))
				g.Expect(c.Message).To(ContainSubstring("operator halt"))
			}, 5*time.Second, 100*time.Millisecond).Should(Succeed())
		})
	})

	Describe("Test 4 — gates.milestone=auto: no-op gate, Succeeded immediately", func() {
		const projectName = "gate-proj-ms4"
		const msName = "gate-ms-4"

		BeforeEach(func() {
			makeProject(projectName, tideprojectv1alpha1.Gates{Milestone: gates.PolicyAuto})
		})
		AfterEach(func() { cleanup(projectName, msName) })

		It("patches Status.Phase=Succeeded immediately", func() {
			ms := &tideprojectv1alpha1.Milestone{
				ObjectMeta: metav1.ObjectMeta{Name: msName, Namespace: "default"},
				Spec:       tideprojectv1alpha1.MilestoneSpec{ProjectRef: projectName},
			}
			Expect(k8sClient.Create(ctx, ms)).To(Succeed())

			envReader := newMapEnvReader()
			r := &MilestoneReconciler{
				Client:         mgrClient,
				Scheme:         k8sClient.Scheme(),
				Dispatcher:     &stubDispatcher{},
				PlannerPool:    newPlannerPoolForTest(),
				EnvReader:      envReader,
				SubagentImage:  testSubagentImage,
				CredproxyImage: testCredproxyImage,
				SigningKey:      testSigningKey,
			}
			driveToJobCompletion(msName, r, envReader)

			Eventually(func(g Gomega) {
				var after tideprojectv1alpha1.Milestone
				g.Expect(mgrClient.Get(ctx, types.NamespacedName{Name: msName, Namespace: "default"}, &after)).To(Succeed())
				g.Expect(after.Status.Phase).To(Equal("Succeeded"))
			}, 5*time.Second, 100*time.Millisecond).Should(Succeed())
		})
	})
})
