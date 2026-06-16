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
	"slices"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	batchv1 "k8s.io/api/batch/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	tideprojectv1alpha1 "github.com/jsquirrelz/tide/api/v1alpha2"
	pkgdispatch "github.com/jsquirrelz/tide/pkg/dispatch"
)

// Plan 04-06 Task 2: W-2 mid-stack boundary push triggers.
//
// When Milestone/Phase/Plan reconcilers reach the post-children-Succeeded
// seam (after gate-policy approval), they invoke maybeTriggerBoundaryPush
// which creates a `tide-push-<project-uid>` Job carrying the level's D-B2
// commit message.
//
// Each test exercises one reconciler at the Succeeded-transition seam,
// faking the planner Job into Completed, then asserts the push Job exists
// with the correct commit-message arg.
var _ = Describe("Up-stack reconcilers — W-2 boundary push trigger (Plan 04-06 Task 2)", Label("envtest", "phase4", "boundarypush"), func() {
	ctx := context.Background()

	// pushArgForJob fetches the named tide-push-<uid> Job and returns the
	// container Args. Used to assert the commit-message arg shape.
	pushArgForJob := func(jobName string) []string {
		var job batchv1.Job
		if err := k8sClient.Get(ctx, types.NamespacedName{Name: jobName, Namespace: "default"}, &job); err != nil {
			return nil
		}
		if len(job.Spec.Template.Spec.Containers) == 0 {
			return nil
		}
		return job.Spec.Template.Spec.Containers[0].Args
	}

	// expectPushJobWithMessage Eventually-asserts a push Job exists for the
	// given Project UID and its container Args carry the expected
	// --commit-message=<text> arg.
	expectPushJobWithMessage := func(projectUID types.UID, expectedMessage string) {
		pushJobName := fmt.Sprintf("tide-push-%s", projectUID)
		Eventually(func(g Gomega) {
			args := pushArgForJob(pushJobName)
			g.Expect(args).NotTo(BeEmpty(), "expected push Job %s to exist", pushJobName)
			found := slices.Contains(args, "--commit-message="+expectedMessage)
			g.Expect(found).To(BeTrue(),
				"expected push Job args to contain --commit-message=%q; got: %s",
				expectedMessage, strings.Join(args, " "))
		}, 5*time.Second, 100*time.Millisecond).Should(Succeed())
	}

	cleanupBP := func(names ...string) {
		// Delete any push Jobs first so reused project UIDs don't collide.
		var jobs batchv1.JobList
		_ = k8sClient.List(ctx, &jobs, client.InNamespace("default"))
		for i := range jobs.Items {
			j := jobs.Items[i]
			_ = k8sClient.Delete(ctx, &j)
		}
		for _, n := range names {
			ms := &tideprojectv1alpha1.Milestone{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: n, Namespace: "default"}, ms); err == nil {
				ms.Finalizers = nil
				_ = k8sClient.Update(ctx, ms)
				_ = k8sClient.Delete(ctx, ms)
			}
			ph := &tideprojectv1alpha1.Phase{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: n, Namespace: "default"}, ph); err == nil {
				ph.Finalizers = nil
				_ = k8sClient.Update(ctx, ph)
				_ = k8sClient.Delete(ctx, ph)
			}
			pl := &tideprojectv1alpha1.Plan{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: n, Namespace: "default"}, pl); err == nil {
				pl.Finalizers = nil
				_ = k8sClient.Update(ctx, pl)
				_ = k8sClient.Delete(ctx, pl)
			}
			// CR-03 fix: also clean up child Tasks created to satisfy BoundaryDetected.
			tsk := &tideprojectv1alpha1.Task{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: n, Namespace: "default"}, tsk); err == nil {
				tsk.Finalizers = nil
				_ = k8sClient.Update(ctx, tsk)
				_ = k8sClient.Delete(ctx, tsk)
			}
			proj := &tideprojectv1alpha1.Project{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: n, Namespace: "default"}, proj); err == nil {
				proj.Finalizers = nil
				_ = k8sClient.Update(ctx, proj)
				_ = k8sClient.Delete(ctx, proj)
			}
		}
	}

	makeProjectForBP := func(name string, gates tideprojectv1alpha1.Gates) *tideprojectv1alpha1.Project {
		proj := &tideprojectv1alpha1.Project{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
			Spec: tideprojectv1alpha1.ProjectSpec{
				TargetRepo: "https://github.com/example/test.git",
				Git: &tideprojectv1alpha1.GitConfig{
					RepoURL:        "https://github.com/example/test.git",
					CredsSecretRef: "test-creds",
				},
				Gates: gates,
			},
		}
		Expect(k8sClient.Create(ctx, proj)).To(Succeed())
		waitForCacheSync(name, "default", &tideprojectv1alpha1.Project{})

		// Stamp the BranchName so push-job dispatch picks a non-empty branch.
		var got tideprojectv1alpha1.Project
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: "default"}, &got)).To(Succeed())
		patch := client.MergeFrom(got.DeepCopy())
		got.Status.Git.BranchName = "tide/run-" + name + "-1747200000"
		Expect(k8sClient.Status().Patch(ctx, &got, patch)).To(Succeed())
		return &got
	}

	// CR-03 fix: the boundary-push trigger is now gated by
	// gates.BoundaryDetected which requires the parent's children to be
	// Status.Phase=Succeeded *and* metav1.IsControlledBy(child, parent).
	// makeSucceededChild creates a controller-owned child CRD of the given
	// kind under the parent, patches its Status.Phase=Succeeded, and waits
	// for the cache to observe both the create and the status patch so
	// BoundaryDetected returns true on the next reconcile.
	makeSucceededChildPhase := func(name, msName string, msParent client.Object) {
		ph := &tideprojectv1alpha1.Phase{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
			Spec:       tideprojectv1alpha1.PhaseSpec{MilestoneRef: msName},
		}
		t := true
		ph.OwnerReferences = []metav1.OwnerReference{{
			APIVersion:         "tideproject.k8s/v1alpha1",
			Kind:               "Milestone",
			Name:               msParent.GetName(),
			UID:                msParent.GetUID(),
			Controller:         &t,
			BlockOwnerDeletion: &t,
		}}
		Expect(k8sClient.Create(ctx, ph)).To(Succeed())
		waitForCacheSync(name, "default", &tideprojectv1alpha1.Phase{})
		var got tideprojectv1alpha1.Phase
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: "default"}, &got)).To(Succeed())
		patch := client.MergeFrom(got.DeepCopy())
		got.Status.Phase = "Succeeded"
		Expect(k8sClient.Status().Patch(ctx, &got, patch)).To(Succeed())
		Eventually(func() string {
			var g tideprojectv1alpha1.Phase
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: "default"}, &g); err != nil {
				return ""
			}
			return g.Status.Phase
		}, 5*time.Second, 50*time.Millisecond).Should(Equal("Succeeded"))
	}

	makeSucceededChildPlan := func(name, phName string, phParent client.Object) {
		pl := &tideprojectv1alpha1.Plan{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
			Spec:       tideprojectv1alpha1.PlanSpec{PhaseRef: phName},
		}
		t := true
		pl.OwnerReferences = []metav1.OwnerReference{{
			APIVersion:         "tideproject.k8s/v1alpha1",
			Kind:               "Phase",
			Name:               phParent.GetName(),
			UID:                phParent.GetUID(),
			Controller:         &t,
			BlockOwnerDeletion: &t,
		}}
		Expect(k8sClient.Create(ctx, pl)).To(Succeed())
		waitForCacheSync(name, "default", &tideprojectv1alpha1.Plan{})
		var got tideprojectv1alpha1.Plan
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: "default"}, &got)).To(Succeed())
		patch := client.MergeFrom(got.DeepCopy())
		got.Status.Phase = "Succeeded"
		Expect(k8sClient.Status().Patch(ctx, &got, patch)).To(Succeed())
		Eventually(func() string {
			var g tideprojectv1alpha1.Plan
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: "default"}, &g); err != nil {
				return ""
			}
			return g.Status.Phase
		}, 5*time.Second, 50*time.Millisecond).Should(Equal("Succeeded"))
	}

	// makeSucceededChildTask is NOT defined: plan_controller does not gate
	// on BoundaryDetected (see fix-note in plan_controller.go), so Test 3
	// does not need to pre-create a Succeeded child Task.

	Describe("Test 1: Milestone boundary dispatches `tide: milestone <name> authored`", func() {
		const projectName = "bp-proj-ms"
		const msName = "bp-ms-1"
		AfterEach(func() {
			// CR-03 fix: child Phase added to satisfy BoundaryDetected.
			cleanupBP(projectName, msName, msName+"-child")
		})

		It("creates a tide-push-<project-uid> Job with the milestone D-B2 message", func() {
			proj := makeProjectForBP(projectName, tideprojectv1alpha1.Gates{Milestone: "auto"})

			// Create Milestone with parent ref to Project.
			ms := &tideprojectv1alpha1.Milestone{
				ObjectMeta: metav1.ObjectMeta{Name: msName, Namespace: "default"},
				Spec:       tideprojectv1alpha1.MilestoneSpec{ProjectRef: projectName},
			}
			Expect(k8sClient.Create(ctx, ms)).To(Succeed())
			waitForCacheSync(msName, "default", &tideprojectv1alpha1.Milestone{})

			envReader := newMapEnvReader()
			r := &MilestoneReconciler{
				Client:         k8sClient,
				Scheme:         k8sClient.Scheme(),
				Dispatcher:     &stubDispatcher{},
				PlannerPool:    newPlannerPoolForTest(),
				EnvReader:      envReader,
				SubagentImage:  testSubagentImage, // dead since Phase 13; HelmProviderDefaults.Image is the default tier
				CredproxyImage: testCredproxyImage,
				SigningKey:     testSigningKey,
				TidePushImage:  "ghcr.io/jsquirrelz/tide-push:test",
				HelmProviderDefaults: ProviderDefaults{
					Image: testSubagentImage,
				},
			}

			Expect(reconcileWithRetry(r.Reconcile, types.NamespacedName{Name: msName, Namespace: "default"}, 5)).To(Succeed())

			// Get Milestone UID + set envelope-out, mark planner Job Succeeded.
			var got tideprojectv1alpha1.Milestone
			Expect(mgrClient.Get(ctx, types.NamespacedName{Name: msName, Namespace: "default"}, &got)).To(Succeed())
			// Plan 09-08: ChildCount=1 so the ChildCount gate expects 1 child Phase
			// before allowing succession. Without it the gate treats the milestone as a
			// leaf and Succeeds immediately without triggering the boundary push.
			envReader.SetOut(string(got.UID), pkgdispatch.EnvelopeOut{
				TaskUID:    string(got.UID),
				ExitCode:   0,
				ChildCount: 1,
			})
			plannerJobName := fmt.Sprintf("tide-milestone-%s-1", got.UID)
			Expect(makeFakeJobTerminal(ctx, mgrClient, plannerJobName, "default", true)).To(Succeed())

			// CR-03 fix: create a Succeeded child Phase so gates.BoundaryDetected
			// returns true (the boundary push trigger is now gated on all-children-
			// Succeeded). Owner ref points at the Milestone we just fetched.
			makeSucceededChildPhase(msName+"-child", msName, &got)

			Expect(reconcileWithRetry(r.Reconcile, types.NamespacedName{Name: msName, Namespace: "default"}, 3)).To(Succeed())

			// Push Job named `tide-push-<project-uid>` should exist with
			// commit-message `tide: milestone bp-ms-1 authored`.
			expectPushJobWithMessage(proj.UID, "tide: milestone "+msName+" authored")
		})
	})

	Describe("Test 2: Phase boundary dispatches `tide: phase <name> authored`", func() {
		const projectName = "bp-proj-ph"
		const msName = "bp-ms-ph-parent"
		const phaseName = "bp-ph-1"
		AfterEach(func() {
			// CR-03 fix: child Plan added to satisfy BoundaryDetected.
			cleanupBP(projectName, msName, phaseName, phaseName+"-child")
		})

		It("creates a tide-push-<project-uid> Job with the phase D-B2 message", func() {
			proj := makeProjectForBP(projectName, tideprojectv1alpha1.Gates{Phase: "auto"})

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

			envReader := newMapEnvReader()
			r := &PhaseReconciler{
				Client:         k8sClient,
				Scheme:         k8sClient.Scheme(),
				Dispatcher:     &stubDispatcher{},
				PlannerPool:    newPlannerPoolForTest(),
				EnvReader:      envReader,
				SubagentImage:  testSubagentImage, // dead since Phase 13; HelmProviderDefaults.Image is the default tier
				CredproxyImage: testCredproxyImage,
				SigningKey:     testSigningKey,
				TidePushImage:  "ghcr.io/jsquirrelz/tide-push:test",
				HelmProviderDefaults: ProviderDefaults{
					Image: testSubagentImage,
				},
			}

			Expect(reconcileWithRetry(r.Reconcile, types.NamespacedName{Name: phaseName, Namespace: "default"}, 5)).To(Succeed())

			var got tideprojectv1alpha1.Phase
			Expect(mgrClient.Get(ctx, types.NamespacedName{Name: phaseName, Namespace: "default"}, &got)).To(Succeed())
			// Plan 09-08: ChildCount=1 so the ChildCount gate expects 1 child Plan.
			envReader.SetOut(string(got.UID), pkgdispatch.EnvelopeOut{
				TaskUID:    string(got.UID),
				ExitCode:   0,
				ChildCount: 1,
			})
			plannerJobName := fmt.Sprintf("tide-phase-%s-1", got.UID)
			Expect(makeFakeJobTerminal(ctx, mgrClient, plannerJobName, "default", true)).To(Succeed())

			// CR-03 fix: create a Succeeded child Plan so BoundaryDetected returns true.
			makeSucceededChildPlan(phaseName+"-child", phaseName, &got)

			Expect(reconcileWithRetry(r.Reconcile, types.NamespacedName{Name: phaseName, Namespace: "default"}, 3)).To(Succeed())

			expectPushJobWithMessage(proj.UID, "tide: phase "+phaseName+" authored")
		})
	})

	Describe("Test 3: Plan boundary dispatches `tide: plan <name> authored + executed`", func() {
		const projectName = "bp-proj-pl"
		const msName = "bp-ms-pl-parent"
		const phaseName = "bp-ph-pl-parent"
		const planName = "bp-pl-1"
		AfterEach(func() {
			cleanupBP(projectName, msName, phaseName, planName)
		})

		// CR-03 note: plan_controller does NOT gate the boundary push on
		// gates.BoundaryDetected (see fix-note in plan_controller.go) because
		// the structural reconcile flow makes handlePlannerJobCompletion
		// unreachable once children exist. This test therefore exercises the
		// original semantic — push fires on planner-Job-terminal, no child
		// Tasks pre-existing. The milestone + phase tests above DO pre-create
		// Succeeded children to exercise the new BoundaryDetected gate.
		It("creates a tide-push-<project-uid> Job with the plan D-B2 message (with '+ executed' suffix)", func() {
			proj := makeProjectForBP(projectName, tideprojectv1alpha1.Gates{Plan: "auto"})
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
				EnvReader:      envReader,
				PlannerPool:    newPlannerPoolForTest(),
				SubagentImage:  testSubagentImage, // dead since Phase 13; HelmProviderDefaults.Image is the default tier
				CredproxyImage: testCredproxyImage,
				SigningKey:     testSigningKey,
				TidePushImage:  "ghcr.io/jsquirrelz/tide-push:test",
				HelmProviderDefaults: ProviderDefaults{
					Image: testSubagentImage,
				},
			}

			Expect(reconcileWithRetry(r.Reconcile, types.NamespacedName{Name: planName, Namespace: "default"}, 5)).To(Succeed())

			var got tideprojectv1alpha1.Plan
			Expect(mgrClient.Get(ctx, types.NamespacedName{Name: planName, Namespace: "default"}, &got)).To(Succeed())
			envReader.SetOut(string(got.UID), pkgdispatch.EnvelopeOut{
				TaskUID:  string(got.UID),
				ExitCode: 0,
			})
			plannerJobName := fmt.Sprintf("tide-plan-%s-1", got.UID)
			Expect(makeFakeJobTerminal(ctx, mgrClient, plannerJobName, "default", true)).To(Succeed())

			Expect(reconcileWithRetry(r.Reconcile, types.NamespacedName{Name: planName, Namespace: "default"}, 3)).To(Succeed())

			expectPushJobWithMessage(proj.UID, "tide: plan "+planName+" authored + executed")
		})
	})

	Describe("Test 4: idempotent — AlreadyExists tolerated", func() {
		const projectName = "bp-proj-idem"
		const msName = "bp-ms-idem"
		AfterEach(func() {
			// CR-03 fix: child Phase added to satisfy BoundaryDetected.
			cleanupBP(projectName, msName, msName+"-child")
		})

		It("a second reconcile after the push Job already exists does not panic and does not duplicate", func() {
			proj := makeProjectForBP(projectName, tideprojectv1alpha1.Gates{Milestone: "auto"})

			ms := &tideprojectv1alpha1.Milestone{
				ObjectMeta: metav1.ObjectMeta{Name: msName, Namespace: "default"},
				Spec:       tideprojectv1alpha1.MilestoneSpec{ProjectRef: projectName},
			}
			Expect(k8sClient.Create(ctx, ms)).To(Succeed())
			waitForCacheSync(msName, "default", &tideprojectv1alpha1.Milestone{})

			envReader := newMapEnvReader()
			r := &MilestoneReconciler{
				Client:         k8sClient,
				Scheme:         k8sClient.Scheme(),
				Dispatcher:     &stubDispatcher{},
				PlannerPool:    newPlannerPoolForTest(),
				EnvReader:      envReader,
				SubagentImage:  testSubagentImage, // dead since Phase 13; HelmProviderDefaults.Image is the default tier
				CredproxyImage: testCredproxyImage,
				SigningKey:     testSigningKey,
				TidePushImage:  "ghcr.io/jsquirrelz/tide-push:test",
				HelmProviderDefaults: ProviderDefaults{
					Image: testSubagentImage,
				},
			}
			Expect(reconcileWithRetry(r.Reconcile, types.NamespacedName{Name: msName, Namespace: "default"}, 5)).To(Succeed())

			var got tideprojectv1alpha1.Milestone
			Expect(mgrClient.Get(ctx, types.NamespacedName{Name: msName, Namespace: "default"}, &got)).To(Succeed())
			// Plan 09-08: ChildCount=1 so the ChildCount gate expects 1 child Phase.
			envReader.SetOut(string(got.UID), pkgdispatch.EnvelopeOut{TaskUID: string(got.UID), ExitCode: 0, ChildCount: 1})
			Expect(makeFakeJobTerminal(ctx, mgrClient, fmt.Sprintf("tide-milestone-%s-1", got.UID), "default", true)).To(Succeed())

			// CR-03 fix: create a Succeeded child Phase so BoundaryDetected returns true.
			makeSucceededChildPhase(msName+"-child", msName, &got)

			// First reconcile pass — push Job created.
			Expect(reconcileWithRetry(r.Reconcile, types.NamespacedName{Name: msName, Namespace: "default"}, 3)).To(Succeed())
			expectPushJobWithMessage(proj.UID, "tide: milestone "+msName+" authored")

			// Second pass — must not error on AlreadyExists.
			Expect(reconcileWithRetry(r.Reconcile, types.NamespacedName{Name: msName, Namespace: "default"}, 3)).To(Succeed())
		})
	})

	Describe("Test 6: reject short-circuit skips push", func() {
		const projectName = "bp-proj-rej"
		const msName = "bp-ms-rej"
		AfterEach(func() {
			cleanupBP(projectName, msName)
		})

		It("Project has reject annotation: push Job is NOT created", func() {
			proj := makeProjectForBP(projectName, tideprojectv1alpha1.Gates{Milestone: "auto"})

			// Apply reject annotation on Project.
			var pp tideprojectv1alpha1.Project
			Expect(mgrClient.Get(ctx, types.NamespacedName{Name: projectName, Namespace: "default"}, &pp)).To(Succeed())
			patch := client.MergeFrom(pp.DeepCopy())
			if pp.Annotations == nil {
				pp.Annotations = map[string]string{}
			}
			pp.Annotations["tideproject.k8s/reject"] = "operator halted"
			Expect(k8sClient.Patch(ctx, &pp, patch)).To(Succeed())

			ms := &tideprojectv1alpha1.Milestone{
				ObjectMeta: metav1.ObjectMeta{Name: msName, Namespace: "default"},
				Spec:       tideprojectv1alpha1.MilestoneSpec{ProjectRef: projectName},
			}
			Expect(k8sClient.Create(ctx, ms)).To(Succeed())
			waitForCacheSync(msName, "default", &tideprojectv1alpha1.Milestone{})

			envReader := newMapEnvReader()
			r := &MilestoneReconciler{
				Client:         k8sClient,
				Scheme:         k8sClient.Scheme(),
				Dispatcher:     &stubDispatcher{},
				PlannerPool:    newPlannerPoolForTest(),
				EnvReader:      envReader,
				SubagentImage:  testSubagentImage, // dead since Phase 13; HelmProviderDefaults.Image is the default tier
				CredproxyImage: testCredproxyImage,
				SigningKey:     testSigningKey,
				TidePushImage:  "ghcr.io/jsquirrelz/tide-push:test",
				HelmProviderDefaults: ProviderDefaults{
					Image: testSubagentImage,
				},
			}
			// D-05 dispatch-entry hold fires before Job creation — the reconciler parks the
			// Milestone with RejectedByUser condition and no planner Job is created.
			Expect(reconcileWithRetry(r.Reconcile, types.NamespacedName{Name: msName, Namespace: "default"}, 5)).To(Succeed())

			// Push Job must NOT exist (Milestone is parked by dispatch-entry hold; boundary push never fires).
			pushJobName := fmt.Sprintf("tide-push-%s", proj.UID)
			Consistently(func() error {
				var j batchv1.Job
				err := k8sClient.Get(ctx, types.NamespacedName{Name: pushJobName, Namespace: "default"}, &j)
				if err == nil {
					return fmt.Errorf("push Job %s exists when reject should have short-circuited", pushJobName)
				}
				if !apierrors.IsNotFound(err) {
					return err
				}
				return nil
			}, 2*time.Second, 100*time.Millisecond).Should(Succeed())
		})
	})
})
