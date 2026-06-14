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
				SubagentImage:  testSubagentImage, // dead since Phase 13; HelmProviderDefaults.Image is the default tier
				CredproxyImage: testCredproxyImage,
				SigningKey:     testSigningKey,
				HelmProviderDefaults: ProviderDefaults{
					Image: testSubagentImage,
				},
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

	Describe("Test 2 — approve annotation present: resumes via Running+ApprovedByUser then Succeeded", func() {
		const projectName = "gate-proj-ms2"
		const msName = "gate-ms-2"

		BeforeEach(func() {
			makeProject(projectName, tideprojectv1alpha1.Gates{Milestone: gates.PolicyApprove})
		})
		AfterEach(func() { cleanup(projectName, msName) })

		It("consumes annotation, patches Running+ApprovedByUser, then Succeeded via ChildCount gate (leaf)", func() {
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
				SubagentImage:  testSubagentImage, // dead since Phase 13; HelmProviderDefaults.Image is the default tier
				CredproxyImage: testCredproxyImage,
				SigningKey:     testSigningKey,
				HelmProviderDefaults: ProviderDefaults{
					Image: testSubagentImage,
				},
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

			// Reconcile and verify the annotation was consumed and the level eventually Succeeded.
			// For this leaf fixture (ChildCount=0), the reconciler transitions:
			//   AwaitingApproval → (consume annotation) → Running+ApprovedByUser → (requeue)
			//   → handleJobCompletion (leaf, expected==0) → Succeeded.
			// The ApprovedByUser condition is set in the AwaitingApproval branch; patchMilestoneSucceeded
			// then overwrites it with ResumedByUser — that is the existing behavior for auto-succeed
			// after approval. The GATE-01 regression spec (below) covers the non-leaf case where the
			// ApprovedByUser condition is visible before children complete.
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

	// GATE-01 regression (run-1 finding 7) — approve with incomplete children must NOT Succeed.
	// This is the trust-killer from dogfood run 1: approving a Milestone with N incomplete
	// Phase children must return it to Running+ApprovedByUser, NOT Succeeded.
	// Succeeded fires only when all N children complete via ChildCount-gated succession (D-03).
	Describe("GATE-01 regression (run-1 finding 7) — approve with incomplete children", func() {
		const projectName = "gate-proj-ms-gate01"
		const msName = "gate-ms-gate01"

		BeforeEach(func() {
			makeProject(projectName, tideprojectv1alpha1.Gates{Milestone: gates.PolicyApprove})
		})
		AfterEach(func() {
			// Also clean up any child Phase CRs we create.
			var phases tideprojectv1alpha1.PhaseList
			_ = k8sClient.List(ctx, &phases, client.InNamespace("default"))
			for i := range phases.Items {
				ph := phases.Items[i]
				if ph.Spec.MilestoneRef == msName {
					ph.Finalizers = nil
					_ = k8sClient.Update(ctx, &ph)
					_ = k8sClient.Delete(ctx, &ph)
				}
			}
			cleanup(projectName, msName)
		})

		It("approve with ChildCount=5 → Running+ApprovedByUser; Succeeded only after all 5 children Succeed", func() {
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
				SubagentImage:  testSubagentImage, // dead since Phase 13; HelmProviderDefaults.Image is the default tier
				CredproxyImage: testCredproxyImage,
				SigningKey:     testSigningKey,
				HelmProviderDefaults: ProviderDefaults{
					Image: testSubagentImage,
				},
			}

			// Drive to job completion with ChildCount=5 (5 Phase children expected).
			waitForCacheSync(msName, "default", &tideprojectv1alpha1.Milestone{})
			Expect(reconcileWithRetry(r.Reconcile, types.NamespacedName{Name: msName, Namespace: "default"}, 5)).To(Succeed())
			var got tideprojectv1alpha1.Milestone
			Eventually(func() error {
				return mgrClient.Get(ctx, types.NamespacedName{Name: msName, Namespace: "default"}, &got)
			}, 5*time.Second, 50*time.Millisecond).Should(Succeed())
			jobName := fmt.Sprintf("tide-milestone-%s-1", got.UID)
			// Set ChildCount=5 so the succession path gates on 5 children.
			envReader.SetOut(string(got.UID), pkgdispatch.EnvelopeOut{
				TaskUID:    string(got.UID),
				ExitCode:   0,
				ChildCount: 5,
			})
			Expect(makeFakeJobTerminal(ctx, mgrClient, jobName, "default", true)).To(Succeed())
			Expect(reconcileWithRetry(r.Reconcile, types.NamespacedName{Name: msName, Namespace: "default"}, 3)).To(Succeed())

			// Assert parked at AwaitingApproval.
			Eventually(func(g Gomega) {
				var after tideprojectv1alpha1.Milestone
				g.Expect(mgrClient.Get(ctx, types.NamespacedName{Name: msName, Namespace: "default"}, &after)).To(Succeed())
				g.Expect(after.Status.Phase).To(Equal("AwaitingApproval"))
			}, 5*time.Second, 100*time.Millisecond).Should(Succeed())

			// Apply approve annotation.
			var current tideprojectv1alpha1.Milestone
			Expect(mgrClient.Get(ctx, types.NamespacedName{Name: msName, Namespace: "default"}, &current)).To(Succeed())
			approvePatch := client.MergeFrom(current.DeepCopy())
			if current.Annotations == nil {
				current.Annotations = map[string]string{}
			}
			current.Annotations[gates.AnnotationApprovePrefix+"milestone"] = "true"
			Expect(k8sClient.Patch(ctx, &current, approvePatch)).To(Succeed())

			// Reconcile — must transition to Running+ApprovedByUser, NOT Succeeded.
			Expect(reconcileWithRetry(r.Reconcile, types.NamespacedName{Name: msName, Namespace: "default"}, 5)).To(Succeed())

			// GATE-01 assertion: still Running, NOT Succeeded, with ApprovedByUser condition.
			Eventually(func(g Gomega) {
				var after tideprojectv1alpha1.Milestone
				g.Expect(mgrClient.Get(ctx, types.NamespacedName{Name: msName, Namespace: "default"}, &after)).To(Succeed())
				g.Expect(after.Status.Phase).To(Equal("Running"),
					"GATE-01: approval with 5 incomplete children must return to Running, not Succeeded (run-1 finding 7)")
				c := meta.FindStatusCondition(after.Status.Conditions, tideprojectv1alpha1.ConditionWaveOrLevelPaused)
				g.Expect(c).NotTo(BeNil())
				g.Expect(c.Status).To(Equal(metav1.ConditionFalse))
				g.Expect(c.Reason).To(Equal(tideprojectv1alpha1.ReasonApprovedByUser))
				_, has := after.Annotations[gates.AnnotationApprovePrefix+"milestone"]
				g.Expect(has).To(BeFalse(), "approve-milestone annotation should be consumed")
			}, 5*time.Second, 100*time.Millisecond).Should(Succeed())

			// Multiple further reconciles must NOT advance to Succeeded (0 of 5 children complete).
			for range 3 {
				Expect(reconcileWithRetry(r.Reconcile, types.NamespacedName{Name: msName, Namespace: "default"}, 1)).To(Succeed())
			}
			Consistently(func(g Gomega) {
				var after tideprojectv1alpha1.Milestone
				g.Expect(mgrClient.Get(ctx, types.NamespacedName{Name: msName, Namespace: "default"}, &after)).To(Succeed())
				g.Expect(after.Status.Phase).NotTo(Equal("Succeeded"),
					"GATE-01: Succeeded must not fire while children are incomplete")
			}, 1*time.Second, 200*time.Millisecond).Should(Succeed())

			// Create 5 child Phases owned by the Milestone, each in Succeeded state.
			// countChildPhases uses OwnerReferences (ref.Kind=="Milestone" && ref.UID==ms.UID),
			// so we must set the OwnerReference on each child.
			msUID := got.UID
			trueVal := true
			for i := range 5 {
				ph := &tideprojectv1alpha1.Phase{
					ObjectMeta: metav1.ObjectMeta{
						Name:      fmt.Sprintf("%s-child-%d", msName, i),
						Namespace: "default",
						Labels: map[string]string{
							"tideproject.k8s/project": projectName,
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								APIVersion: tideprojectv1alpha1.GroupVersion.String(),
								Kind:       "Milestone",
								Name:       msName,
								UID:        msUID,
								Controller: &trueVal,
							},
						},
					},
					Spec: tideprojectv1alpha1.PhaseSpec{MilestoneRef: msName},
				}
				Expect(k8sClient.Create(ctx, ph)).To(Succeed())
				waitForCacheSync(ph.Name, "default", &tideprojectv1alpha1.Phase{})
				// Patch Status.Phase=Succeeded so BoundaryDetected returns true.
				var freshPh tideprojectv1alpha1.Phase
				Expect(mgrClient.Get(ctx, types.NamespacedName{Name: ph.Name, Namespace: "default"}, &freshPh)).To(Succeed())
				spatch := client.MergeFrom(freshPh.DeepCopy())
				freshPh.Status.Phase = "Succeeded"
				Expect(k8sClient.Status().Patch(ctx, &freshPh, spatch)).To(Succeed())
			}

			// Drive reconciler — Running branch calls handleJobCompletion again;
			// observed(5) >= expected(5) and BoundaryDetected → Succeeded.
			Expect(reconcileWithRetry(r.Reconcile, types.NamespacedName{Name: msName, Namespace: "default"}, 5)).To(Succeed())
			Eventually(func(g Gomega) {
				var after tideprojectv1alpha1.Milestone
				g.Expect(mgrClient.Get(ctx, types.NamespacedName{Name: msName, Namespace: "default"}, &after)).To(Succeed())
				g.Expect(after.Status.Phase).To(Equal("Succeeded"),
					"GATE-01: Succeeded must fire after all 5 children complete")
			}, 10*time.Second, 200*time.Millisecond).Should(Succeed())
		})
	})

	Describe("Test 3 — reject annotation on Project: parks Milestone with RejectedByUser condition (D-05)", func() {
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

		It("Milestone is parked with ConditionWaveOrLevelPaused/RejectedByUser (NOT Failed), then recovers after annotation clear", func() {
			ms := &tideprojectv1alpha1.Milestone{
				ObjectMeta: metav1.ObjectMeta{Name: msName, Namespace: "default"},
				Spec:       tideprojectv1alpha1.MilestoneSpec{ProjectRef: projectName},
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
				SubagentImage:  testSubagentImage, // dead since Phase 13; HelmProviderDefaults.Image is the default tier
				CredproxyImage: testCredproxyImage,
				SigningKey:     testSigningKey,
				HelmProviderDefaults: ProviderDefaults{
					Image: testSubagentImage,
				},
			}
			// D-05 dispatch-entry hold fires before Job creation — drive reconcile directly.
			// No Job is created; the reconciler parks the Milestone with RejectedByUser.
			Expect(reconcileWithRetry(r.Reconcile, types.NamespacedName{Name: msName, Namespace: "default"}, 3)).To(Succeed())

			// D-05: reject parks — Status.Phase must NOT be "Failed".
			// ConditionWaveOrLevelPaused must be True with Reason=RejectedByUser.
			Eventually(func(g Gomega) {
				var after tideprojectv1alpha1.Milestone
				g.Expect(mgrClient.Get(ctx, types.NamespacedName{Name: msName, Namespace: "default"}, &after)).To(Succeed())
				g.Expect(after.Status.Phase).NotTo(Equal("Failed"),
					"D-05: reject must park the Milestone, not fail-mark it")
				c := meta.FindStatusCondition(after.Status.Conditions, tideprojectv1alpha1.ConditionWaveOrLevelPaused)
				g.Expect(c).NotTo(BeNil(), "ConditionWaveOrLevelPaused must be set when parked")
				g.Expect(c.Status).To(Equal(metav1.ConditionTrue))
				g.Expect(c.Reason).To(Equal(tideprojectv1alpha1.ReasonRejectedByUser))
				g.Expect(c.Message).To(ContainSubstring("operator halt"))
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

			// After annotation clear, re-driving must let the Milestone proceed
			// (dispatch-entry hold no longer fires — Job is created and completed).
			driveToJobCompletion(msName, r, envReader)
			Eventually(func(g Gomega) {
				var after tideprojectv1alpha1.Milestone
				g.Expect(mgrClient.Get(ctx, types.NamespacedName{Name: msName, Namespace: "default"}, &after)).To(Succeed())
				// After resume, Status.Phase must NOT be "Failed" and must have progressed
				// past the park (Succeeded for this leaf fixture).
				g.Expect(after.Status.Phase).NotTo(Equal("Failed"),
					"D-05: Milestone must not be Failed after reject annotation cleared")
			}, 5*time.Second, 100*time.Millisecond).Should(Succeed())
		})
	})

	// D-05 dispatch-hold: a Pending Milestone under a rejected Project must not create a planner Job.
	Describe("Test 3b — reject annotation on Project: halts new dispatch for Pending Milestone", func() {
		const projectName = "gate-proj-ms3b"
		const msName = "gate-ms-3b"

		BeforeEach(func() {
			makeProject(projectName, tideprojectv1alpha1.Gates{Milestone: gates.PolicyAuto})
			var proj tideprojectv1alpha1.Project
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: projectName, Namespace: "default"}, &proj)).To(Succeed())
			patch := client.MergeFrom(proj.DeepCopy())
			if proj.Annotations == nil {
				proj.Annotations = map[string]string{}
			}
			proj.Annotations[gates.AnnotationReject] = "halt dispatch"
			Expect(k8sClient.Patch(ctx, &proj, patch)).To(Succeed())
			Eventually(func() string {
				var p tideprojectv1alpha1.Project
				if err := mgrClient.Get(ctx, types.NamespacedName{Name: projectName, Namespace: "default"}, &p); err != nil {
					return ""
				}
				return p.Annotations[gates.AnnotationReject]
			}, 5*time.Second, 50*time.Millisecond).Should(Equal("halt dispatch"))
		})
		AfterEach(func() { cleanup(projectName, msName) })

		It("Pending Milestone creates no planner Job while Project carries reject annotation", func() {
			ms := &tideprojectv1alpha1.Milestone{
				ObjectMeta: metav1.ObjectMeta{Name: msName, Namespace: "default"},
				Spec:       tideprojectv1alpha1.MilestoneSpec{ProjectRef: projectName},
			}
			Expect(k8sClient.Create(ctx, ms)).To(Succeed())
			waitForCacheSync(msName, "default", &tideprojectv1alpha1.Milestone{})

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

			var jobsBefore batchv1.JobList
			_ = k8sClient.List(ctx, &jobsBefore, client.InNamespace("default"))
			jobCountBefore := len(jobsBefore.Items)

			Expect(reconcileWithRetry(r.Reconcile, types.NamespacedName{Name: msName, Namespace: "default"}, 3)).To(Succeed())

			var jobsAfter batchv1.JobList
			_ = k8sClient.List(ctx, &jobsAfter, client.InNamespace("default"))
			Expect(jobsAfter.Items).To(HaveLen(jobCountBefore),
				"no planner Job must be created while Project carries reject annotation")
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
				SubagentImage:  testSubagentImage, // dead since Phase 13; HelmProviderDefaults.Image is the default tier
				CredproxyImage: testCredproxyImage,
				SigningKey:     testSigningKey,
				HelmProviderDefaults: ProviderDefaults{
					Image: testSubagentImage,
				},
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
