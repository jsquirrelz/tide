/*
Copyright 2026 TIDE Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

// Plan QQH-01: RED-first envtest proving that a terminal planner Job which
// still exists (not yet TTL-GC'd) causes reconcileProjectPlannerDispatch to
// spawn the tide-reporter-<uid> Job and roll up planner Usage into
// Project.Status.Budget.CostSpentCents.
//
// Root cause: the current Step 1b idempotency guard ("Job exists → return nil")
// fires BEFORE the Step 2 terminal-state check on the Running branch, making
// handleProjectJobCompletion unreachable when the Job is still present.
// The fix (Task 2) mirrors milestone_controller.go:reconcilePlannerDispatch
// which checks terminal state (Step 2) BEFORE the idempotency guard (Step 2b).
package controller

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	batchv1 "k8s.io/api/batch/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	tideprojectv1alpha2 "github.com/jsquirrelz/tide/api/v1alpha2"
	pkgdispatch "github.com/jsquirrelz/tide/pkg/dispatch"
)

const (
	// qqhPVCName is the shared PVC for QQH-01 specs; created once in the first
	// BeforeEach that needs it via ensurePVC (idempotent).
	qqhPVCName     = "tide-projects-qqh-completion"
	qqhReporterImg = "ghcr.io/jsquirrelz/tide-reporter:test"
)

// qqhBuildReconciler constructs a fully-wired ProjectReconciler for QQH-01 specs.
// Do NOT reuse newTestProjectReconciler — it omits SigningKey, EnvReader,
// ReporterImage, and PlannerPool, which are all required for dispatch and completion.
func qqhBuildReconciler(envReader *mapEnvReader) *ProjectReconciler {
	return &ProjectReconciler{
		Client:         mgrClient,
		Scheme:         k8sClient.Scheme(),
		Dispatcher:     &stubDispatcher{},
		PlannerPool:    newPlannerPoolForTest(),
		EnvReader:      envReader,
		SigningKey:     testSigningKey,
		CredproxyImage: testCredproxyImage,
		ReporterImage:  qqhReporterImg,
		SharedPVCName:  qqhPVCName,
		HelmProviderDefaults: ProviderDefaults{
			Image: testSubagentImage,
		},
	}
}

// qqhCreateProject creates a minimal Project with the given name and waits for
// the cache to reflect it. Returns the hydrated project (with UID populated).
func qqhCreateProject(ctx context.Context, name string) *tideprojectv1alpha2.Project {
	proj := &tideprojectv1alpha2.Project{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "default",
		},
		Spec: tideprojectv1alpha2.ProjectSpec{
			SchemaRevision: "v1alpha2",
			TargetRepo:     "https://github.com/example/test.git",
			OutcomePrompt:  "Build a test project",
			Subagent:       tideprojectv1alpha2.SubagentConfig{Model: "claude-opus-4-7"},
		},
	}
	Expect(k8sClient.Create(ctx, proj)).To(Succeed())
	waitForCacheSync(name, "default", &tideprojectv1alpha2.Project{})
	return proj
}

// qqhCleanupProject removes finalizers and deletes the named Project (best-effort).
func qqhCleanupProject(ctx context.Context, name string) {
	p := &tideprojectv1alpha2.Project{}
	if err := k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: "default"}, p); err == nil {
		p.Finalizers = nil
		_ = k8sClient.Update(ctx, p)
		_ = k8sClient.Delete(ctx, p)
	}
}

var _ = Describe("ProjectReconciler — planner Job completion while Job still exists (QQH-01)", Label("envtest"), func() {
	ctx := context.Background()

	// primary spec — unique names to avoid cross-spec state leakage.
	Describe("primary: terminal planner Job still present", func() {
		const primProjName = "qqh-proj-primary"

		BeforeEach(func() {
			ensurePVC(ctx, qqhPVCName, "default")
		})

		AfterEach(func() {
			qqhCleanupProject(ctx, primProjName)
			// Delete only Jobs whose name starts with "tide-project-" or "tide-reporter-"
			// to avoid deleting Jobs belonging to other concurrent specs.
			var jobs batchv1.JobList
			_ = k8sClient.List(ctx, &jobs, client.InNamespace("default"))
			for i := range jobs.Items {
				j := &jobs.Items[i]
				if len(j.Name) > 13 && (j.Name[:13] == "tide-project-" || len(j.Name) > 13 && j.Name[:13] == "tide-reporter") {
					_ = k8sClient.Delete(ctx, j, client.PropagationPolicy(metav1.DeletePropagationBackground))
				}
			}
		})

		It("reporter Job spawns + budget rolls up on terminal planner Job that still exists", func() {
			proj := qqhCreateProject(ctx, primProjName)

			envReader := newMapEnvReader()
			r := qqhBuildReconciler(envReader)

			// Phase 1 — first reconcile with Phase not Running so the planner Job is
			// created and Phase is patched to Running in-memory and on the API server.
			// (Step 2 only fires on PhaseRunning; absent-job + non-Running → dispatch.)
			_, err := r.reconcileProjectPlannerDispatch(ctx, proj)
			Expect(err).NotTo(HaveOccurred())
			// reconcileProjectPlannerDispatch patches proj.Status.Phase in-place.
			Expect(proj.Status.Phase).To(Equal(tideprojectv1alpha2.PhaseRunning),
				"dispatch must have patched Phase=Running in-memory")

			plannerJobName := fmt.Sprintf("tide-project-%s-1", proj.UID)

			// Sanity: planner Job was created by the dispatch.
			Eventually(func() error {
				return mgrClient.Get(ctx, types.NamespacedName{Name: plannerJobName, Namespace: "default"}, &batchv1.Job{})
			}, "5s", "100ms").Should(Succeed(), "planner Job must exist after initial dispatch")

			// Phase 2 — seed the planner cost and make the Job terminal while it
			// still exists (not GC'd). This is the exact state the bug manifests in:
			// Step 1b ("job exists → return nil") fires before the terminal check.
			const plannerCostCents = int64(36)
			envReader.SetOut(string(proj.UID), pkgdispatch.EnvelopeOut{
				TaskUID:    string(proj.UID),
				ExitCode:   0,
				ChildCount: 0, // leaf project — no children to wait for
				Usage: pkgdispatch.Usage{
					InputTokens:        1000,
					OutputTokens:       200,
					EstimatedCostCents: plannerCostCents,
				},
			})

			// makeFakeJobTerminal patches the Job to Complete state WITHOUT deleting it.
			Expect(makeFakeJobTerminal(ctx, mgrClient, plannerJobName, "default", true)).To(Succeed())

			// Wait for the cache to reflect the terminal Job status before reconciling.
			// The reconciler reads the Job through the cache-backed mgrClient; if the
			// cache hasn't caught up, isJobTerminal returns false and the fix won't fire.
			Eventually(func() bool {
				var j batchv1.Job
				if err := mgrClient.Get(ctx, types.NamespacedName{Name: plannerJobName, Namespace: "default"}, &j); err != nil {
					return false
				}
				return isJobTerminal(&j)
			}, "5s", "100ms").Should(BeTrue(), "cache must reflect terminal Job status before second reconcile")

			// Phase 3 — reconcile again with Phase=Running and Job present+terminal.
			// On current (buggy) code: Step 1b fires → return nil → no reporter, no budget.
			// After fix: Step 2 fires first → isJobTerminal → handleProjectJobCompletion.
			Expect(mgrClient.Get(ctx, types.NamespacedName{Name: primProjName, Namespace: "default"}, proj)).To(Succeed())
			_, err = r.reconcileProjectPlannerDispatch(ctx, proj)
			Expect(err).NotTo(HaveOccurred())

			reporterJobName := fmt.Sprintf("tide-reporter-%s", proj.UID)

			// (a) Reporter Job must be spawned.
			Eventually(func() error {
				return mgrClient.Get(ctx, types.NamespacedName{Name: reporterJobName, Namespace: "default"}, &batchv1.Job{})
			}, 5*time.Second, 100*time.Millisecond).Should(Succeed(),
				"tide-reporter-<uid> Job must be created on planner Job completion while Job still exists")

			// (b) Project.Status.Budget.CostSpentCents must reflect the planner spend.
			Eventually(func(g Gomega) {
				var refreshedProj tideprojectv1alpha2.Project
				g.Expect(mgrClient.Get(ctx, types.NamespacedName{Name: primProjName, Namespace: "default"}, &refreshedProj)).To(Succeed())
				g.Expect(refreshedProj.Status.Budget.CostSpentCents).To(
					BeNumerically(">=", plannerCostCents),
					"Project.Status.Budget.CostSpentCents must reflect planner spend (no 10-min TTL stall)")
			}, 5*time.Second, 100*time.Millisecond).Should(Succeed())
		})
	})

	// control spec — separate project name to avoid cross-spec leakage.
	Describe("control: still-Running planner Job leaves reporter absent and budget 0", func() {
		const ctrlProjName = "qqh-proj-control"

		BeforeEach(func() {
			ensurePVC(ctx, qqhPVCName, "default")
		})

		AfterEach(func() {
			qqhCleanupProject(ctx, ctrlProjName)
		})

		It("no reporter Job, budget stays 0 when planner Job is non-terminal", func() {
			proj := qqhCreateProject(ctx, ctrlProjName)

			envReader := newMapEnvReader()
			r := qqhBuildReconciler(envReader)

			// Dispatch first (Phase not Running → creates planner Job → patches Phase=Running
			// in-memory via reconcileProjectPlannerDispatch).
			_, err := r.reconcileProjectPlannerDispatch(ctx, proj)
			Expect(err).NotTo(HaveOccurred())
			// In-memory check: the dispatch patched proj.Status.Phase directly.
			Expect(proj.Status.Phase).To(Equal(tideprojectv1alpha2.PhaseRunning),
				"dispatch must patch Phase=Running in-memory")

			plannerJobName := fmt.Sprintf("tide-project-%s-1", proj.UID)
			Eventually(func() error {
				return mgrClient.Get(ctx, types.NamespacedName{Name: plannerJobName, Namespace: "default"}, &batchv1.Job{})
			}, "5s", "100ms").Should(Succeed(), "planner Job must exist after dispatch")

			const plannerCostCents = int64(36)
			envReader.SetOut(string(proj.UID), pkgdispatch.EnvelopeOut{
				TaskUID:    string(proj.UID),
				ExitCode:   0,
				ChildCount: 0,
				Usage: pkgdispatch.Usage{
					InputTokens:        1000,
					OutputTokens:       200,
					EstimatedCostCents: plannerCostCents,
				},
			})

			// Do NOT make the Job terminal — it is still Running.
			// Re-reconcile with Phase=Running and a non-terminal Job.
			Expect(mgrClient.Get(ctx, types.NamespacedName{Name: ctrlProjName, Namespace: "default"}, proj)).To(Succeed())
			_, err = r.reconcileProjectPlannerDispatch(ctx, proj)
			Expect(err).NotTo(HaveOccurred())

			reporterJobName := fmt.Sprintf("tide-reporter-%s", proj.UID)

			// Reporter must NOT have been spawned.
			Consistently(func() error {
				return mgrClient.Get(ctx, types.NamespacedName{Name: reporterJobName, Namespace: "default"}, &batchv1.Job{})
			}, 1*time.Second, 100*time.Millisecond).Should(MatchError(ContainSubstring("not found")),
				"control: tide-reporter-<uid> must NOT be created while planner Job is still Running")

			// Budget must remain 0.
			var refreshedProj tideprojectv1alpha2.Project
			Expect(mgrClient.Get(ctx, types.NamespacedName{Name: ctrlProjName, Namespace: "default"}, &refreshedProj)).To(Succeed())
			Expect(refreshedProj.Status.Budget.CostSpentCents).To(BeZero(),
				"control: budget must remain 0 while planner Job is non-terminal")
		})
	})
})
