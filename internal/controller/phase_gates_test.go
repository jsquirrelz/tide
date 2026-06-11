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
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	tideprojectv1alpha1 "github.com/jsquirrelz/tide/api/v1alpha1"
	"github.com/jsquirrelz/tide/internal/gates"
	pkgdispatch "github.com/jsquirrelz/tide/pkg/dispatch"
)

// gate-flow tests for PhaseReconciler (Plan 04-05 Task 1).
var _ = Describe("PhaseReconciler — gate-policy hook (Plan 04-05 Task 1)", Label("envtest", "phase4", "gates"), func() {
	ctx := context.Background()

	makeProjectAndMilestone := func(projectName, msName string, g tideprojectv1alpha1.Gates) {
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
	}

	cleanup := func(projectName, msName, phaseName string) {
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

	driveToJobCompletion := func(phaseName string, r *PhaseReconciler, envReader *mapEnvReader) {
		waitForCacheSync(phaseName, "default", &tideprojectv1alpha1.Phase{})
		Expect(reconcileWithRetry(r.Reconcile, types.NamespacedName{Name: phaseName, Namespace: "default"}, 5)).To(Succeed())
		var got tideprojectv1alpha1.Phase
		Eventually(func() error {
			return mgrClient.Get(ctx, types.NamespacedName{Name: phaseName, Namespace: "default"}, &got)
		}, 5*time.Second, 50*time.Millisecond).Should(Succeed())
		jobName := fmt.Sprintf("tide-phase-%s-1", got.UID)
		envReader.SetOut(string(got.UID), pkgdispatch.EnvelopeOut{
			TaskUID:  string(got.UID),
			ExitCode: 0,
		})
		Expect(makeFakeJobTerminal(ctx, mgrClient, jobName, "default", true)).To(Succeed())
		Expect(reconcileWithRetry(r.Reconcile, types.NamespacedName{Name: phaseName, Namespace: "default"}, 3)).To(Succeed())
	}

	Describe("Test 5a — gates.phase=approve: AwaitingApproval", func() {
		const projectName, msName, phaseName = "gate-proj-ph1", "gate-ms-ph1", "gate-phase-1"

		BeforeEach(func() {
			makeProjectAndMilestone(projectName, msName, tideprojectv1alpha1.Gates{Phase: gates.PolicyApprove})
		})
		AfterEach(func() { cleanup(projectName, msName, phaseName) })

		It("patches Status.Phase=AwaitingApproval with WaveOrLevelPaused True", func() {
			phase := &tideprojectv1alpha1.Phase{
				ObjectMeta: metav1.ObjectMeta{Name: phaseName, Namespace: "default"},
				Spec:       tideprojectv1alpha1.PhaseSpec{MilestoneRef: msName},
			}
			Expect(k8sClient.Create(ctx, phase)).To(Succeed())

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
			driveToJobCompletion(phaseName, r, envReader)

			Eventually(func(g Gomega) {
				var after tideprojectv1alpha1.Phase
				g.Expect(mgrClient.Get(ctx, types.NamespacedName{Name: phaseName, Namespace: "default"}, &after)).To(Succeed())
				g.Expect(after.Status.Phase).To(Equal("AwaitingApproval"))
				c := meta.FindStatusCondition(after.Status.Conditions, tideprojectv1alpha1.ConditionWaveOrLevelPaused)
				g.Expect(c).NotTo(BeNil())
				g.Expect(c.Reason).To(Equal(tideprojectv1alpha1.ReasonAwaitingApproval))
			}, 5*time.Second, 100*time.Millisecond).Should(Succeed())
		})
	})

	Describe("Test 5b — approve-phase annotation: Running+ApprovedByUser then Succeeded (D-04)", func() {
		const projectName, msName, phaseName = "gate-proj-ph2", "gate-ms-ph2", "gate-phase-2"

		BeforeEach(func() {
			makeProjectAndMilestone(projectName, msName, tideprojectv1alpha1.Gates{Phase: gates.PolicyApprove})
		})
		AfterEach(func() { cleanup(projectName, msName, phaseName) })

		It("consumes annotation, transitions Running+ApprovedByUser, then Succeeded via ChildCount gate (leaf)", func() {
			phase := &tideprojectv1alpha1.Phase{
				ObjectMeta: metav1.ObjectMeta{Name: phaseName, Namespace: "default"},
				Spec:       tideprojectv1alpha1.PhaseSpec{MilestoneRef: msName},
			}
			Expect(k8sClient.Create(ctx, phase)).To(Succeed())

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
			driveToJobCompletion(phaseName, r, envReader)

			Eventually(func() string {
				var after tideprojectv1alpha1.Phase
				if err := mgrClient.Get(ctx, types.NamespacedName{Name: phaseName, Namespace: "default"}, &after); err != nil {
					return ""
				}
				return after.Status.Phase
			}, 5*time.Second, 100*time.Millisecond).Should(Equal("AwaitingApproval"))

			var current tideprojectv1alpha1.Phase
			Expect(mgrClient.Get(ctx, types.NamespacedName{Name: phaseName, Namespace: "default"}, &current)).To(Succeed())
			patch := client.MergeFrom(current.DeepCopy())
			if current.Annotations == nil {
				current.Annotations = map[string]string{}
			}
			current.Annotations[gates.AnnotationApprovePrefix+"phase"] = "true"
			Expect(k8sClient.Patch(ctx, &current, patch)).To(Succeed())

			Expect(reconcileWithRetry(r.Reconcile, types.NamespacedName{Name: phaseName, Namespace: "default"}, 5)).To(Succeed())

			// The D-04 two-step: first Running+ApprovedByUser (or already Succeeded for leaf),
			// then Succeeded via ChildCount-gated succession. The key assertion: annotation consumed.
			Eventually(func(g Gomega) {
				var after tideprojectv1alpha1.Phase
				g.Expect(mgrClient.Get(ctx, types.NamespacedName{Name: phaseName, Namespace: "default"}, &after)).To(Succeed())
				// Must NOT still be AwaitingApproval (annotation must have been consumed by
				// the new AwaitingApproval branch in reconcilePlannerDispatch — D-01 fix).
				g.Expect(after.Status.Phase).NotTo(Equal("AwaitingApproval"),
					"Phase must leave AwaitingApproval after approve annotation is applied (D-01 parity fix)")
				_, has := after.Annotations[gates.AnnotationApprovePrefix+"phase"]
				g.Expect(has).To(BeFalse(), "approve-phase annotation should be consumed")
			}, 5*time.Second, 100*time.Millisecond).Should(Succeed())

			// Terminal: Succeeded via ChildCount-gated succession (leaf fixture, ChildCount=0).
			Expect(reconcileWithRetry(r.Reconcile, types.NamespacedName{Name: phaseName, Namespace: "default"}, 3)).To(Succeed())
			Eventually(func(g Gomega) {
				var after tideprojectv1alpha1.Phase
				g.Expect(mgrClient.Get(ctx, types.NamespacedName{Name: phaseName, Namespace: "default"}, &after)).To(Succeed())
				g.Expect(after.Status.Phase).To(Equal("Succeeded"))
			}, 5*time.Second, 100*time.Millisecond).Should(Succeed())
		})
	})

	// Phase oscillation regression spec — finding-2 root cause (Phase 12 D-01).
	// A Phase parked at AwaitingApproval must stay parked when no approve annotation
	// is present: 3 consecutive reconciles leave Status.Phase==AwaitingApproval and
	// create zero planner Jobs. The early-return in reconcilePlannerDispatch stops
	// the AwaitingApproval↔Running oscillation from the run-1 finding-2 symptom.
	Describe("Phase oscillation regression (run-1 finding 2) — AwaitingApproval early-return", func() {
		const projectName, msName, phaseName = "gate-proj-ph-osc", "gate-ms-ph-osc", "gate-phase-osc"

		BeforeEach(func() {
			makeProjectAndMilestone(projectName, msName, tideprojectv1alpha1.Gates{Phase: gates.PolicyApprove})
		})
		AfterEach(func() { cleanup(projectName, msName, phaseName) })

		It("Phase stays AwaitingApproval through 3 reconciles; zero planner Jobs created (D-01)", func() {
			phase := &tideprojectv1alpha1.Phase{
				ObjectMeta: metav1.ObjectMeta{Name: phaseName, Namespace: "default"},
				Spec:       tideprojectv1alpha1.PhaseSpec{MilestoneRef: msName},
			}
			Expect(k8sClient.Create(ctx, phase)).To(Succeed())

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
			driveToJobCompletion(phaseName, r, envReader)

			// First: assert parked at AwaitingApproval (gate hook fired).
			Eventually(func() string {
				var after tideprojectv1alpha1.Phase
				if err := mgrClient.Get(ctx, types.NamespacedName{Name: phaseName, Namespace: "default"}, &after); err != nil {
					return ""
				}
				return after.Status.Phase
			}, 5*time.Second, 100*time.Millisecond).Should(Equal("AwaitingApproval"))

			// Reconcile 3 more times WITHOUT adding the approve annotation.
			// The early-return branch must hold: Status.Phase stays AwaitingApproval,
			// zero NEW planner Jobs are created.
			var jobsBefore batchv1.JobList
			_ = k8sClient.List(ctx, &jobsBefore, client.InNamespace("default"))
			jobCountBefore := len(jobsBefore.Items)

			for range 3 {
				_, _ = r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: phaseName, Namespace: "default"}})
			}

			Eventually(func(g Gomega) {
				var after tideprojectv1alpha1.Phase
				g.Expect(mgrClient.Get(ctx, types.NamespacedName{Name: phaseName, Namespace: "default"}, &after)).To(Succeed())
				g.Expect(after.Status.Phase).To(Equal("AwaitingApproval"),
					"Phase must NOT leave AwaitingApproval without an approve annotation (oscillation fix D-01)")
			}, 2*time.Second, 100*time.Millisecond).Should(Succeed())

			var jobsAfter batchv1.JobList
			_ = k8sClient.List(ctx, &jobsAfter, client.InNamespace("default"))
			Expect(len(jobsAfter.Items)).To(Equal(jobCountBefore),
				"no new planner Jobs should be created while Phase is AwaitingApproval")
		})
	})

	Describe("Test 5c — reject annotation on Project: parks Phase with RejectedByUser condition (D-05)", func() {
		const projectName, msName, phaseName = "gate-proj-ph3", "gate-ms-ph3", "gate-phase-3"

		BeforeEach(func() {
			makeProjectAndMilestone(projectName, msName, tideprojectv1alpha1.Gates{Phase: gates.PolicyAuto})
			var proj tideprojectv1alpha1.Project
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: projectName, Namespace: "default"}, &proj)).To(Succeed())
			patch := client.MergeFrom(proj.DeepCopy())
			if proj.Annotations == nil {
				proj.Annotations = map[string]string{}
			}
			proj.Annotations[gates.AnnotationReject] = "phase halt"
			Expect(k8sClient.Patch(ctx, &proj, patch)).To(Succeed())
			Eventually(func() string {
				var p tideprojectv1alpha1.Project
				if err := mgrClient.Get(ctx, types.NamespacedName{Name: projectName, Namespace: "default"}, &p); err != nil {
					return ""
				}
				return p.Annotations[gates.AnnotationReject]
			}, 5*time.Second, 50*time.Millisecond).Should(Equal("phase halt"))
		})
		AfterEach(func() { cleanup(projectName, msName, phaseName) })

		It("Phase is parked with ConditionWaveOrLevelPaused/RejectedByUser (NOT Failed), then recovers after annotation clear", func() {
			phase := &tideprojectv1alpha1.Phase{
				ObjectMeta: metav1.ObjectMeta{Name: phaseName, Namespace: "default"},
				Spec:       tideprojectv1alpha1.PhaseSpec{MilestoneRef: msName},
			}
			Expect(k8sClient.Create(ctx, phase)).To(Succeed())
			waitForCacheSync(phaseName, "default", &tideprojectv1alpha1.Phase{})

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
			// D-05 dispatch-entry hold fires before Job creation — drive reconcile directly.
			Expect(reconcileWithRetry(r.Reconcile, types.NamespacedName{Name: phaseName, Namespace: "default"}, 3)).To(Succeed())

			// D-05: reject parks — Status.Phase must NOT be "Failed".
			Eventually(func(g Gomega) {
				var after tideprojectv1alpha1.Phase
				g.Expect(mgrClient.Get(ctx, types.NamespacedName{Name: phaseName, Namespace: "default"}, &after)).To(Succeed())
				g.Expect(after.Status.Phase).NotTo(Equal("Failed"),
					"D-05: reject must park the Phase, not fail-mark it")
				c := meta.FindStatusCondition(after.Status.Conditions, tideprojectv1alpha1.ConditionWaveOrLevelPaused)
				g.Expect(c).NotTo(BeNil(), "ConditionWaveOrLevelPaused must be set when parked")
				g.Expect(c.Status).To(Equal(metav1.ConditionTrue))
				g.Expect(c.Reason).To(Equal(tideprojectv1alpha1.ReasonRejectedByUser))
				g.Expect(c.Message).To(ContainSubstring("phase halt"))
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

			// After annotation clear, re-driving must let the Phase proceed.
			driveToJobCompletion(phaseName, r, envReader)
			Eventually(func(g Gomega) {
				var after tideprojectv1alpha1.Phase
				g.Expect(mgrClient.Get(ctx, types.NamespacedName{Name: phaseName, Namespace: "default"}, &after)).To(Succeed())
				g.Expect(after.Status.Phase).NotTo(Equal("Failed"),
					"D-05: Phase must not be Failed after reject annotation cleared")
			}, 5*time.Second, 100*time.Millisecond).Should(Succeed())
		})
	})

	// D-05 dispatch-hold: a Pending Phase under a rejected Project must not create a planner Job.
	Describe("Test 5c-hold — reject annotation on Project: halts new dispatch for Pending Phase", func() {
		const projectName, msName, phaseName = "gate-proj-ph3h", "gate-ms-ph3h", "gate-phase-3h"

		BeforeEach(func() {
			makeProjectAndMilestone(projectName, msName, tideprojectv1alpha1.Gates{Phase: gates.PolicyAuto})
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
		AfterEach(func() { cleanup(projectName, msName, phaseName) })

		It("Pending Phase creates no planner Job while Project carries reject annotation", func() {
			phase := &tideprojectv1alpha1.Phase{
				ObjectMeta: metav1.ObjectMeta{Name: phaseName, Namespace: "default"},
				Spec:       tideprojectv1alpha1.PhaseSpec{MilestoneRef: msName},
			}
			Expect(k8sClient.Create(ctx, phase)).To(Succeed())
			waitForCacheSync(phaseName, "default", &tideprojectv1alpha1.Phase{})

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

			var jobsBefore batchv1.JobList
			_ = k8sClient.List(ctx, &jobsBefore, client.InNamespace("default"))
			jobCountBefore := len(jobsBefore.Items)

			Expect(reconcileWithRetry(r.Reconcile, types.NamespacedName{Name: phaseName, Namespace: "default"}, 3)).To(Succeed())

			var jobsAfter batchv1.JobList
			_ = k8sClient.List(ctx, &jobsAfter, client.InNamespace("default"))
			Expect(len(jobsAfter.Items)).To(Equal(jobCountBefore),
				"no planner Job must be created while Project carries reject annotation")
		})
	})

	Describe("Test 5d — gates.phase=auto (default): Succeeded immediately", func() {
		const projectName, msName, phaseName = "gate-proj-ph4", "gate-ms-ph4", "gate-phase-4"

		BeforeEach(func() {
			// Empty Gates → default for phase is "auto" per gates.EvaluatePolicy.
			makeProjectAndMilestone(projectName, msName, tideprojectv1alpha1.Gates{})
		})
		AfterEach(func() { cleanup(projectName, msName, phaseName) })

		It("Phase reaches Succeeded immediately when phase gate is auto/empty", func() {
			phase := &tideprojectv1alpha1.Phase{
				ObjectMeta: metav1.ObjectMeta{Name: phaseName, Namespace: "default"},
				Spec:       tideprojectv1alpha1.PhaseSpec{MilestoneRef: msName},
			}
			Expect(k8sClient.Create(ctx, phase)).To(Succeed())

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
			driveToJobCompletion(phaseName, r, envReader)

			Eventually(func(g Gomega) {
				var after tideprojectv1alpha1.Phase
				g.Expect(mgrClient.Get(ctx, types.NamespacedName{Name: phaseName, Namespace: "default"}, &after)).To(Succeed())
				g.Expect(after.Status.Phase).To(Equal("Succeeded"))
			}, 5*time.Second, 100*time.Millisecond).Should(Succeed())
		})
	})

	// GATE-04 regression spec (Plan 12-03 Task 2) — run-1 finding-1: five phase
	// planners fired ~1s after the milestone parked at AwaitingApproval, spending
	// ~$0.64/planner before the operator had reviewed anything. This spec proves
	// the D-02 descent hold: zero planner Jobs while the parent Milestone is parked.
	Describe("GATE-04 — dispatch hold while parent Milestone awaiting approval", Label("gate04"), func() {
		const projectName = "gate04-proj-ph"
		const msName = "gate04-ms-ph"
		const phaseName = "gate04-phase"

		AfterEach(func() { cleanup(projectName, msName, phaseName) })

		It("Phase dispatch held: zero planner Jobs while parent Milestone AwaitingApproval; Job created after approval", func() {
			makeProjectAndMilestone(projectName, msName, tideprojectv1alpha1.Gates{Phase: gates.PolicyAuto})

			// Manually park the Milestone at AwaitingApproval (simulating the gate hook
			// in milestone_controller.go). The Phase under test must see this status.
			var ms tideprojectv1alpha1.Milestone
			Expect(mgrClient.Get(ctx, types.NamespacedName{Name: msName, Namespace: "default"}, &ms)).To(Succeed())
			msPatch := client.MergeFrom(ms.DeepCopy())
			ms.Status.Phase = "AwaitingApproval"
			Expect(mgrClient.Status().Patch(ctx, &ms, msPatch)).To(Succeed())
			Eventually(func() string {
				var got tideprojectv1alpha1.Milestone
				if err := mgrClient.Get(ctx, types.NamespacedName{Name: msName, Namespace: "default"}, &got); err != nil {
					return ""
				}
				return got.Status.Phase
			}, 5*time.Second, 50*time.Millisecond).Should(Equal("AwaitingApproval"))

			// Create the Phase with MilestoneRef pointing at the parked Milestone.
			phase := &tideprojectv1alpha1.Phase{
				ObjectMeta: metav1.ObjectMeta{Name: phaseName, Namespace: "default"},
				Spec:       tideprojectv1alpha1.PhaseSpec{MilestoneRef: msName},
			}
			Expect(k8sClient.Create(ctx, phase)).To(Succeed())
			waitForCacheSync(phaseName, "default", &tideprojectv1alpha1.Phase{})

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

			// Drive 3 consecutive reconciles while parent is parked.
			// The D-02 hold must prevent ANY planner Job creation.
			var jobsBefore batchv1.JobList
			_ = k8sClient.List(ctx, &jobsBefore, client.InNamespace("default"))
			jobCountBefore := len(jobsBefore.Items)

			for range 3 {
				_, _ = r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: phaseName, Namespace: "default"}})
			}

			// Assert: Phase stays at "" (held — not AwaitingApproval; Pitfall 5 guard).
			Eventually(func(g Gomega) {
				var after tideprojectv1alpha1.Phase
				g.Expect(mgrClient.Get(ctx, types.NamespacedName{Name: phaseName, Namespace: "default"}, &after)).To(Succeed())
				g.Expect(after.Status.Phase).To(Equal(""),
					"held child must stay at Status.Phase='' (not AwaitingApproval) while parent is parked (Pitfall 5)")
			}, 2*time.Second, 100*time.Millisecond).Should(Succeed())

			// Assert: no new planner Jobs created while parent parked.
			var jobsAfter batchv1.JobList
			_ = k8sClient.List(ctx, &jobsAfter, client.InNamespace("default"))
			Expect(len(jobsAfter.Items)).To(Equal(jobCountBefore),
				"no planner Jobs should be created while parent Milestone is AwaitingApproval (GATE-04 / run-1 finding-1)")

			// Now approve the Milestone (simulate: patch Status.Phase=Running).
			Expect(mgrClient.Get(ctx, types.NamespacedName{Name: msName, Namespace: "default"}, &ms)).To(Succeed())
			approvePatch := client.MergeFrom(ms.DeepCopy())
			ms.Status.Phase = "Running"
			Expect(mgrClient.Status().Patch(ctx, &ms, approvePatch)).To(Succeed())
			Eventually(func() string {
				var got tideprojectv1alpha1.Milestone
				if err := mgrClient.Get(ctx, types.NamespacedName{Name: msName, Namespace: "default"}, &got); err != nil {
					return ""
				}
				return got.Status.Phase
			}, 5*time.Second, 50*time.Millisecond).Should(Equal("Running"))

			// Drive the Phase reconciler — hold is now released.
			Expect(reconcileWithRetry(r.Reconcile, types.NamespacedName{Name: phaseName, Namespace: "default"}, 3)).To(Succeed())

			// Assert: planner Job now exists (dispatch unblocked).
			var phaseAfterApproval tideprojectv1alpha1.Phase
			Expect(mgrClient.Get(ctx, types.NamespacedName{Name: phaseName, Namespace: "default"}, &phaseAfterApproval)).To(Succeed())
			jobName := fmt.Sprintf("tide-phase-%s-1", phaseAfterApproval.UID)
			Eventually(func() error {
				var job batchv1.Job
				return mgrClient.Get(ctx, types.NamespacedName{Name: jobName, Namespace: "default"}, &job)
			}, 5*time.Second, 100*time.Millisecond).Should(Succeed(),
				"planner Job must be created after parent Milestone is approved (D-02 hold released)")
		})
	})
})
