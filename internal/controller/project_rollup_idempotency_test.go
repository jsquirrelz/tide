/*
Copyright 2026 TIDE Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

// Phase 34 Plan 02 envtest — PREFLIGHT-02 (project-level rollup exactly-once).
//
// Covers the project-level planner budget rollup site in project_controller.go:
//   - PlannerRolledUpUID gating budget.RollUpUsage in handleProjectJobCompletion
//
// PREFLIGHT-02 accrual (Test 1): after the first planner Job completion,
// Project.Status.Budget.CostSpentCents increases by the stubbed Usage, and
// the PlannerRolledUpUID marker is set to tide-project-<uid>-1.
//
// PREFLIGHT-02 idempotency across TTL-GC (Test 2): after a second post-TTL-GC
// completion call (isFirstCompletion=true again, reporter Job absent), CostSpentCents
// is UNCHANGED — the durable marker is the sole guard against double-count.
//
// Test approach: set ReporterImage="" so spawnReporterIfNeeded returns (true, nil) —
// isFirstCompletion=true on every call without requiring a PVC. This is the correct
// simulation of the post-TTL-GC condition: the reporter Job is absent so isFirstCompletion
// flips back to true, and the PlannerRolledUpUID marker is the SOLE guard against double-count.
//
// This mirrors the milestone/phase/plan level tests in child_rollup_idempotency_test.go
// and the BYPASS-03 test in project_planner_completion_test.go, but is distinct:
// it validates the new RetryOnConflict + MergeFromWithOptimisticLock marker pattern
// added in Phase 34 Plan 02 at the project level.
//
// Phase 38 DEBT-01 (v1.0.6 audit W1): this spec family is the envtest coverage for
// the hardened project-level PlannerRolledUpUID stamp. The spec text names the
// marker so the RESEARCH validation map's -ginkgo.focus='PlannerRolledUpUID'
// selects it.
package controller

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	tideprojectv1alpha2 "github.com/jsquirrelz/tide/api/v1alpha2"
	pkgdispatch "github.com/jsquirrelz/tide/pkg/dispatch"
)

var _ = Describe("ProjectRollupIdempotency — project-level PlannerRolledUpUID stamp (PREFLIGHT-02 / DEBT-01 W1)", Label("envtest"), func() {
	ctx := context.Background()

	const (
		prProjName = "project-rollup-idempotency-proj"
	)

	var envReader *mapEnvReader

	BeforeEach(func() {
		// Create a minimal auto-gated Project.
		proj := &tideprojectv1alpha2.Project{
			ObjectMeta: metav1.ObjectMeta{Name: prProjName, Namespace: "default"},
			Spec: tideprojectv1alpha2.ProjectSpec{
				SchemaRevision: "v1alpha2",
				TargetRepo:     "https://github.com/example/project-rollup.git",
				Subagent:       tideprojectv1alpha2.SubagentConfig{Model: "claude-sonnet-4-6"},
				Gates:          tideprojectv1alpha2.Gates{Milestone: tideprojectv1alpha2.GatePolicy("auto")},
			},
		}
		Expect(k8sClient.Create(ctx, proj)).To(Succeed())
		waitForCacheSync(prProjName, "default", &tideprojectv1alpha2.Project{})
		envReader = newMapEnvReader()
	})

	AfterEach(func() {
		p := &tideprojectv1alpha2.Project{}
		if err := k8sClient.Get(ctx, types.NamespacedName{Name: prProjName, Namespace: "default"}, p); err == nil {
			p.Finalizers = nil
			_ = k8sClient.Update(ctx, p)
			_ = k8sClient.Delete(ctx, p)
		}
	})

	It("PREFLIGHT-02: project rollup accrues on first call and is idempotent on second (TTL-GC simulation)", func() {
		// Load the project with its server-assigned UID.
		proj := &tideprojectv1alpha2.Project{}
		Expect(mgrClient.Get(ctx, types.NamespacedName{Name: prProjName, Namespace: "default"}, proj)).To(Succeed())

		// Set Status.Phase=Running so handleProjectJobCompletion's rollup branch fires.
		statusPatch := client.MergeFrom(proj.DeepCopy())
		proj.Status.Phase = "Running"
		Expect(k8sClient.Status().Patch(ctx, proj, statusPatch)).To(Succeed())
		Expect(mgrClient.Get(ctx, types.NamespacedName{Name: prProjName, Namespace: "default"}, proj)).To(Succeed())

		const costCents = int64(89)
		const inputTokens = int64(4200)

		// Seed the envelope for this project UID.
		envReader.SetOut(string(proj.UID), pkgdispatch.EnvelopeOut{
			TaskUID:    string(proj.UID),
			ExitCode:   0,
			ChildCount: 0,
			Usage: pkgdispatch.Usage{
				InputTokens:        inputTokens,
				OutputTokens:       500,
				EstimatedCostCents: costCents,
			},
		})

		// Construct a ProjectReconciler with ReporterImage="" so spawnReporterIfNeeded
		// returns (true, nil) — isFirstCompletion=true on every call without a PVC.
		r := &ProjectReconciler{
			Client:         mgrClient,
			Scheme:         k8sClient.Scheme(),
			Dispatcher:     &stubDispatcher{},
			PlannerPool:    newPlannerPoolForTest(),
			EnvReader:      envReader,
			SigningKey:     testSigningKey,
			CredproxyImage: testCredproxyImage,
			// ReporterImage deliberately empty: spawnReporterIfNeeded returns
			// (true, nil) → isFirstCompletion=true on every call without a PVC.
			HelmProviderDefaults: ProviderDefaults{
				Image: testSubagentImage,
			},
		}

		// First call: PREFLIGHT-02 accrual.
		_, err := r.handleProjectJobCompletion(ctx, proj, nil)
		Expect(err).NotTo(HaveOccurred())

		expectedJobName := fmt.Sprintf("tide-project-%s-1", proj.UID)

		// PREFLIGHT-02 Test 1: CostSpentCents must reflect project planner spend,
		// and PlannerRolledUpUID must be set to the planner Job name.
		Eventually(func(g Gomega) {
			var fresh tideprojectv1alpha2.Project
			g.Expect(mgrClient.Get(ctx, types.NamespacedName{Name: prProjName, Namespace: "default"}, &fresh)).To(Succeed())
			g.Expect(fresh.Status.Budget.CostSpentCents).To(
				BeNumerically("==", costCents),
				"PREFLIGHT-02: CostSpentCents must reflect project planner spend after first rollup")
			g.Expect(fresh.Status.Budget.TokensSpent).To(
				BeNumerically(">=", inputTokens),
				"PREFLIGHT-02: TokensSpent must include project planner input tokens")
			g.Expect(fresh.Status.Budget.PlannerRolledUpUID).To(
				Equal(expectedJobName),
				"PREFLIGHT-02: PlannerRolledUpUID must be set to tide-project-<uid>-1 after first rollup")
		}, 5*time.Second, 100*time.Millisecond).Should(Succeed())

		// Capture cost baseline and reload project with the marker already set in cache.
		var projWithMarker tideprojectv1alpha2.Project
		Expect(mgrClient.Get(ctx, types.NamespacedName{Name: prProjName, Namespace: "default"}, &projWithMarker)).To(Succeed())
		costBefore := projWithMarker.Status.Budget.CostSpentCents

		// Second call: PREFLIGHT-02 idempotency across TTL-GC.
		// isFirstCompletion=true (ReporterImage="") but PlannerRolledUpUID==plannerJobName →
		// the RetryOnConflict block short-circuits and no second rollup fires.
		_, err = r.handleProjectJobCompletion(ctx, &projWithMarker, nil)
		Expect(err).NotTo(HaveOccurred())

		// PREFLIGHT-02 Test 2: CostSpentCents must be UNCHANGED on the second post-TTL-GC
		// reconcile — the durable marker is the sole guard and no second rollup fires.
		Consistently(func(g Gomega) {
			var fresh tideprojectv1alpha2.Project
			g.Expect(mgrClient.Get(ctx, types.NamespacedName{Name: prProjName, Namespace: "default"}, &fresh)).To(Succeed())
			g.Expect(fresh.Status.Budget.CostSpentCents).To(
				BeNumerically("==", costBefore),
				"PREFLIGHT-02: CostSpentCents must NOT double-count after second post-TTL-GC completion (project)")
		}, 2*time.Second, 200*time.Millisecond).Should(Succeed())
	})
})
