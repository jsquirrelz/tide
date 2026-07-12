/*
Copyright 2026 TIDE Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

// Phase 31 Plan 03 envtest — ADOPT-02 (accrual) and ADOPT-04 (exactly-once across TTL-GC).
//
// Covers the three child-level planner budget rollup sites:
//   - MilestoneRolledUpUID gating budget.RollUpUsage in milestone_controller.go
//   - PhaseRolledUpUID gating budget.RollUpUsage in phase_controller.go
//   - PlanRolledUpUID gating budget.RollUpUsage in plan_controller.go (D-03a new)
//
// ADOPT-02: after the first planner Job completion, Project.Status.Budget.CostSpentCents
// and TokensSpent increase by the stubbed Usage, and the level-specific marker is set.
//
// ADOPT-04: after a second post-TTL-GC completion call (isFirstCompletion=true again,
// reporter Job gone), CostSpentCents is unchanged — the durable marker is the sole guard.
//
// Test approach: set ReporterImage="" so spawnReporterIfNeeded returns (true, nil) —
// isFirstCompletion=true on every call without requiring a PVC. This is the correct
// simulation of the post-TTL-GC condition: the reporter Job is absent so isFirstCompletion
// flips back to true, and the level marker is the SOLE guard against double-count.
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

	tideprojectv1alpha3 "github.com/jsquirrelz/tide/api/v1alpha3"
	pkgdispatch "github.com/jsquirrelz/tide/pkg/dispatch"
)

// childRollupProject creates a minimal auto-gated Project, waits for cache, and returns it.
func childRollupProject(ctx context.Context, name string) *tideprojectv1alpha3.Project {
	proj := &tideprojectv1alpha3.Project{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
		Spec: tideprojectv1alpha3.ProjectSpec{
			SchemaRevision: "v1alpha3",
			TargetRepo:     "https://github.com/example/child-rollup.git",
			Subagent:       tideprojectv1alpha3.SubagentConfig{Model: "claude-sonnet-4-6"},
			Gates:          tideprojectv1alpha3.Gates{Milestone: tideprojectv1alpha3.GatePolicy("auto")},
		},
	}
	Expect(k8sClient.Create(ctx, proj)).To(Succeed())
	waitForCacheSync(name, "default", &tideprojectv1alpha3.Project{})
	return proj
}

// cleanupChildRollupProject deletes the project (best-effort, removes finalizers first).
func cleanupChildRollupProject(ctx context.Context, name string) {
	p := &tideprojectv1alpha3.Project{}
	if err := k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: "default"}, p); err == nil {
		p.Finalizers = nil
		_ = k8sClient.Update(ctx, p)
		_ = k8sClient.Delete(ctx, p)
	}
}

// refetchProject reloads a Project from the manager cache.
func refetchProject(ctx context.Context, name string) *tideprojectv1alpha3.Project {
	p := &tideprojectv1alpha3.Project{}
	Expect(mgrClient.Get(ctx, types.NamespacedName{Name: name, Namespace: "default"}, p)).To(Succeed())
	return p
}

// ─── Milestone level ─────────────────────────────────────────────────────────

var _ = Describe("ChildRollupIdempotency — Milestone level (ADOPT-02 + ADOPT-04)", Label("envtest", "heavy"), func() {
	ctx := context.Background()

	const (
		crMSProjName = "child-rollup-ms-proj"
		crMSName     = "child-rollup-ms"
	)

	var envReader *mapEnvReader

	BeforeEach(func() {
		childRollupProject(ctx, crMSProjName)
		envReader = newMapEnvReader()
	})

	AfterEach(func() {
		ms := &tideprojectv1alpha3.Milestone{}
		if err := k8sClient.Get(ctx, types.NamespacedName{Name: crMSName, Namespace: "default"}, ms); err == nil {
			ms.Finalizers = nil
			_ = k8sClient.Update(ctx, ms)
			_ = k8sClient.Delete(ctx, ms)
		}
		cleanupChildRollupProject(ctx, crMSProjName)
	})

	It("ADOPT-02+04: milestone rollup accrues on first call and is idempotent on second (TTL-GC simulation)", func() {
		// Create Milestone.
		ms := &tideprojectv1alpha3.Milestone{
			ObjectMeta: metav1.ObjectMeta{Name: crMSName, Namespace: "default"},
			Spec:       tideprojectv1alpha3.MilestoneSpec{ProjectRef: crMSProjName},
		}
		Expect(k8sClient.Create(ctx, ms)).To(Succeed())
		waitForCacheSync(crMSName, "default", &tideprojectv1alpha3.Milestone{})
		Expect(mgrClient.Get(ctx, types.NamespacedName{Name: crMSName, Namespace: "default"}, ms)).To(Succeed())

		// Set Status.Phase=Running so the reconcile path enters handleJobCompletion.
		statusPatch := client.MergeFrom(ms.DeepCopy())
		ms.Status.Phase = "Running"
		Expect(k8sClient.Status().Patch(ctx, ms, statusPatch)).To(Succeed())
		Expect(mgrClient.Get(ctx, types.NamespacedName{Name: crMSName, Namespace: "default"}, ms)).To(Succeed())

		const costCents = int64(37)
		const inputTokens = int64(1500)
		envReader.SetOut(string(ms.UID), pkgdispatch.EnvelopeOut{
			TaskUID:    string(ms.UID),
			ExitCode:   0,
			ChildCount: 0,
			Usage: pkgdispatch.Usage{
				InputTokens:        inputTokens,
				OutputTokens:       300,
				EstimatedCostCents: costCents,
			},
		})

		r := &MilestoneReconciler{
			Client: mgrClient,
			Scheme: k8sClient.Scheme(),
			Deps: PlannerReconcilerDeps{
				Dispatcher:     &stubDispatcher{},
				EnvReader:      envReader,
				CredproxyImage: testCredproxyImage,
				SigningKey:     testSigningKey,
				HelmProviderDefaults: ProviderDefaults{
					Image: testSubagentImage,
				},
			},
			PlannerPool: newPlannerPoolForTest(),
			// ReporterImage deliberately empty: spawnReporterIfNeeded returns
			// (true, nil) → isFirstCompletion=true on every call without a PVC.
		}

		// First call: ADOPT-02 — accrual.
		_, err := r.handleJobCompletion(ctx, ms, nil)
		Expect(err).NotTo(HaveOccurred())

		expectedJobName := fmt.Sprintf("tide-milestone-%s-1", ms.UID)

		// ADOPT-02: Project budget increased and milestone marker is set.
		Eventually(func(g Gomega) {
			proj := refetchProject(ctx, crMSProjName)
			g.Expect(proj.Status.Budget.CostSpentCents).To(
				BeNumerically("==", costCents),
				"ADOPT-02: CostSpentCents must reflect milestone planner spend after first rollup")
			g.Expect(proj.Status.Budget.TokensSpent).To(
				BeNumerically(">=", inputTokens),
				"ADOPT-02: TokensSpent must include milestone planner input tokens")
		}, 5*time.Second, 100*time.Millisecond).Should(Succeed())

		Eventually(func(g Gomega) {
			var fresh tideprojectv1alpha3.Milestone
			g.Expect(mgrClient.Get(ctx, types.NamespacedName{Name: crMSName, Namespace: "default"}, &fresh)).To(Succeed())
			g.Expect(fresh.Status.MilestoneRolledUpUID).To(
				Equal(expectedJobName),
				"ADOPT-02: MilestoneRolledUpUID must be set to the planner Job name")
		}, 5*time.Second, 100*time.Millisecond).Should(Succeed())

		// Capture cost and reload ms with the marker already set.
		proj := refetchProject(ctx, crMSProjName)
		costBefore := proj.Status.Budget.CostSpentCents
		Expect(mgrClient.Get(ctx, types.NamespacedName{Name: crMSName, Namespace: "default"}, ms)).To(Succeed())

		// Second call: ADOPT-04 — marker prevents double-count.
		// isFirstCompletion=true (ReporterImage="") but MilestoneRolledUpUID==jobName → skipped.
		_, err = r.handleJobCompletion(ctx, ms, nil)
		Expect(err).NotTo(HaveOccurred())

		Consistently(func(g Gomega) {
			proj := refetchProject(ctx, crMSProjName)
			g.Expect(proj.Status.Budget.CostSpentCents).To(
				BeNumerically("==", costBefore),
				"ADOPT-04: CostSpentCents must NOT double-count after second post-TTL-GC completion (milestone)")
		}, 2*time.Second, 200*time.Millisecond).Should(Succeed())
	})
})

// ─── Phase level ─────────────────────────────────────────────────────────────

var _ = Describe("ChildRollupIdempotency — Phase level (ADOPT-02 + ADOPT-04)", Label("envtest", "heavy"), func() {
	ctx := context.Background()

	const (
		crPHProjName = "child-rollup-ph-proj"
		crPHMSName   = "child-rollup-ph-ms"
		crPHName     = "child-rollup-ph"
	)

	var envReader *mapEnvReader

	BeforeEach(func() {
		childRollupProject(ctx, crPHProjName)
		ms := &tideprojectv1alpha3.Milestone{
			ObjectMeta: metav1.ObjectMeta{Name: crPHMSName, Namespace: "default"},
			Spec:       tideprojectv1alpha3.MilestoneSpec{ProjectRef: crPHProjName},
		}
		Expect(k8sClient.Create(ctx, ms)).To(Succeed())
		waitForCacheSync(crPHMSName, "default", &tideprojectv1alpha3.Milestone{})
		envReader = newMapEnvReader()
	})

	AfterEach(func() {
		ph := &tideprojectv1alpha3.Phase{}
		if err := k8sClient.Get(ctx, types.NamespacedName{Name: crPHName, Namespace: "default"}, ph); err == nil {
			ph.Finalizers = nil
			_ = k8sClient.Update(ctx, ph)
			_ = k8sClient.Delete(ctx, ph)
		}
		ms := &tideprojectv1alpha3.Milestone{}
		if err := k8sClient.Get(ctx, types.NamespacedName{Name: crPHMSName, Namespace: "default"}, ms); err == nil {
			ms.Finalizers = nil
			_ = k8sClient.Update(ctx, ms)
			_ = k8sClient.Delete(ctx, ms)
		}
		cleanupChildRollupProject(ctx, crPHProjName)
	})

	It("ADOPT-02+04: phase rollup accrues on first call and is idempotent on second (TTL-GC simulation)", func() {
		ph := &tideprojectv1alpha3.Phase{
			ObjectMeta: metav1.ObjectMeta{Name: crPHName, Namespace: "default"},
			Spec:       tideprojectv1alpha3.PhaseSpec{MilestoneRef: crPHMSName},
		}
		Expect(k8sClient.Create(ctx, ph)).To(Succeed())
		waitForCacheSync(crPHName, "default", &tideprojectv1alpha3.Phase{})
		Expect(mgrClient.Get(ctx, types.NamespacedName{Name: crPHName, Namespace: "default"}, ph)).To(Succeed())

		statusPatch := client.MergeFrom(ph.DeepCopy())
		ph.Status.Phase = "Running"
		Expect(k8sClient.Status().Patch(ctx, ph, statusPatch)).To(Succeed())
		Expect(mgrClient.Get(ctx, types.NamespacedName{Name: crPHName, Namespace: "default"}, ph)).To(Succeed())

		const costCents = int64(53)
		const inputTokens = int64(2200)
		envReader.SetOut(string(ph.UID), pkgdispatch.EnvelopeOut{
			TaskUID:    string(ph.UID),
			ExitCode:   0,
			ChildCount: 0,
			Usage: pkgdispatch.Usage{
				InputTokens:        inputTokens,
				OutputTokens:       400,
				EstimatedCostCents: costCents,
			},
		})

		r := &PhaseReconciler{
			Client: mgrClient,
			Scheme: k8sClient.Scheme(),
			Deps: PlannerReconcilerDeps{
				Dispatcher:     &stubDispatcher{},
				EnvReader:      envReader,
				CredproxyImage: testCredproxyImage,
				SigningKey:     testSigningKey,
				HelmProviderDefaults: ProviderDefaults{
					Image: testSubagentImage,
				},
			},
			PlannerPool: newPlannerPoolForTest(),
			// ReporterImage empty: isFirstCompletion=true without PVC.
		}

		// Note: PhaseReconciler.resolveProject resolves the project via the Phase's
		// MilestoneRef → Milestone.Spec.ProjectRef chain. The milestone (crPHMSName)
		// points to crPHProjName via its Spec.ProjectRef, so rollup lands on that project.

		// First call: ADOPT-02.
		_, err := r.handleJobCompletion(ctx, ph, nil)
		Expect(err).NotTo(HaveOccurred())

		expectedJobName := fmt.Sprintf("tide-phase-%s-1", ph.UID)

		Eventually(func(g Gomega) {
			proj := refetchProject(ctx, crPHProjName)
			g.Expect(proj.Status.Budget.CostSpentCents).To(
				BeNumerically("==", costCents),
				"ADOPT-02: CostSpentCents must reflect phase planner spend after first rollup")
			g.Expect(proj.Status.Budget.TokensSpent).To(
				BeNumerically(">=", inputTokens),
				"ADOPT-02: TokensSpent must include phase planner input tokens")
		}, 5*time.Second, 100*time.Millisecond).Should(Succeed())

		Eventually(func(g Gomega) {
			var fresh tideprojectv1alpha3.Phase
			g.Expect(mgrClient.Get(ctx, types.NamespacedName{Name: crPHName, Namespace: "default"}, &fresh)).To(Succeed())
			g.Expect(fresh.Status.PhaseRolledUpUID).To(
				Equal(expectedJobName),
				"ADOPT-02: PhaseRolledUpUID must be set to the planner Job name")
		}, 5*time.Second, 100*time.Millisecond).Should(Succeed())

		proj := refetchProject(ctx, crPHProjName)
		costBefore := proj.Status.Budget.CostSpentCents
		Expect(mgrClient.Get(ctx, types.NamespacedName{Name: crPHName, Namespace: "default"}, ph)).To(Succeed())

		// Second call: ADOPT-04.
		_, err = r.handleJobCompletion(ctx, ph, nil)
		Expect(err).NotTo(HaveOccurred())

		Consistently(func(g Gomega) {
			proj := refetchProject(ctx, crPHProjName)
			g.Expect(proj.Status.Budget.CostSpentCents).To(
				BeNumerically("==", costBefore),
				"ADOPT-04: CostSpentCents must NOT double-count after second post-TTL-GC completion (phase)")
		}, 2*time.Second, 200*time.Millisecond).Should(Succeed())
	})
})

// ─── Plan level (D-03a — no prior marker) ────────────────────────────────────

var _ = Describe("ChildRollupIdempotency — Plan level ADOPT-02+04 (D-03a new marker)", Label("envtest", "heavy"), func() {
	ctx := context.Background()

	const (
		crPlanProjName = "child-rollup-plan-proj"
		crPlanMSName   = "child-rollup-plan-ms"
		crPlanPhName   = "child-rollup-plan-ph"
		crPlanName     = "child-rollup-plan"
	)

	var envReader *mapEnvReader

	BeforeEach(func() {
		childRollupProject(ctx, crPlanProjName)
		ms := &tideprojectv1alpha3.Milestone{
			ObjectMeta: metav1.ObjectMeta{Name: crPlanMSName, Namespace: "default"},
			Spec:       tideprojectv1alpha3.MilestoneSpec{ProjectRef: crPlanProjName},
		}
		Expect(k8sClient.Create(ctx, ms)).To(Succeed())
		waitForCacheSync(crPlanMSName, "default", &tideprojectv1alpha3.Milestone{})
		ph := &tideprojectv1alpha3.Phase{
			ObjectMeta: metav1.ObjectMeta{Name: crPlanPhName, Namespace: "default"},
			Spec:       tideprojectv1alpha3.PhaseSpec{MilestoneRef: crPlanMSName},
		}
		Expect(k8sClient.Create(ctx, ph)).To(Succeed())
		waitForCacheSync(crPlanPhName, "default", &tideprojectv1alpha3.Phase{})
		envReader = newMapEnvReader()
	})

	AfterEach(func() {
		plan := &tideprojectv1alpha3.Plan{}
		if err := k8sClient.Get(ctx, types.NamespacedName{Name: crPlanName, Namespace: "default"}, plan); err == nil {
			plan.Finalizers = nil
			_ = k8sClient.Update(ctx, plan)
			_ = k8sClient.Delete(ctx, plan)
		}
		ph := &tideprojectv1alpha3.Phase{}
		if err := k8sClient.Get(ctx, types.NamespacedName{Name: crPlanPhName, Namespace: "default"}, ph); err == nil {
			ph.Finalizers = nil
			_ = k8sClient.Update(ctx, ph)
			_ = k8sClient.Delete(ctx, ph)
		}
		ms := &tideprojectv1alpha3.Milestone{}
		if err := k8sClient.Get(ctx, types.NamespacedName{Name: crPlanMSName, Namespace: "default"}, ms); err == nil {
			ms.Finalizers = nil
			_ = k8sClient.Update(ctx, ms)
			_ = k8sClient.Delete(ctx, ms)
		}
		cleanupChildRollupProject(ctx, crPlanProjName)
	})

	It("ADOPT-02+04 (D-03a): plan rollup accrues on first call and is idempotent on second (TTL-GC simulation)", func() {
		plan := &tideprojectv1alpha3.Plan{
			ObjectMeta: metav1.ObjectMeta{Name: crPlanName, Namespace: "default"},
			Spec:       tideprojectv1alpha3.PlanSpec{PhaseRef: crPlanPhName},
		}
		Expect(k8sClient.Create(ctx, plan)).To(Succeed())
		waitForCacheSync(crPlanName, "default", &tideprojectv1alpha3.Plan{})
		Expect(mgrClient.Get(ctx, types.NamespacedName{Name: crPlanName, Namespace: "default"}, plan)).To(Succeed())

		statusPatch := client.MergeFrom(plan.DeepCopy())
		plan.Status.Phase = "Running"
		Expect(k8sClient.Status().Patch(ctx, plan, statusPatch)).To(Succeed())
		Expect(mgrClient.Get(ctx, types.NamespacedName{Name: crPlanName, Namespace: "default"}, plan)).To(Succeed())

		const costCents = int64(71)
		const inputTokens = int64(3100)
		envReader.SetOut(string(plan.UID), pkgdispatch.EnvelopeOut{
			TaskUID:    string(plan.UID),
			ExitCode:   0,
			ChildCount: 0,
			Usage: pkgdispatch.Usage{
				InputTokens:        inputTokens,
				OutputTokens:       600,
				EstimatedCostCents: costCents,
			},
		})

		r := &PlanReconciler{
			Client: mgrClient,
			Scheme: k8sClient.Scheme(),
			Deps: PlannerReconcilerDeps{
				Dispatcher:     &stubDispatcher{},
				EnvReader:      envReader,
				CredproxyImage: testCredproxyImage,
				SigningKey:     testSigningKey,
				HelmProviderDefaults: ProviderDefaults{
					Image: testSubagentImage,
				},
			},
			PlannerPool: newPlannerPoolForTest(),
			// ReporterImage empty: isFirstCompletion=true without PVC.
		}

		// Note: PlanReconciler.resolveProjectForPlan resolves the project via
		// Plan.Spec.PhaseRef → Phase.Spec.MilestoneRef → Milestone.Spec.ProjectRef.
		// With the parent chain crPlanName→crPlanPhName→crPlanMSName→crPlanProjName
		// the rollup lands on crPlanProjName.

		// First call: ADOPT-02.
		_, err := r.handlePlannerJobCompletion(ctx, plan, nil)
		Expect(err).NotTo(HaveOccurred())

		expectedJobName := fmt.Sprintf("tide-plan-%s-1", plan.UID)

		// ADOPT-02: Project budget increased, plan marker set.
		Eventually(func(g Gomega) {
			proj := refetchProject(ctx, crPlanProjName)
			g.Expect(proj.Status.Budget.CostSpentCents).To(
				BeNumerically("==", costCents),
				"ADOPT-02: CostSpentCents must reflect plan planner spend after first rollup (D-03a)")
			g.Expect(proj.Status.Budget.TokensSpent).To(
				BeNumerically(">=", inputTokens),
				"ADOPT-02: TokensSpent must include plan planner input tokens")
		}, 5*time.Second, 100*time.Millisecond).Should(Succeed())

		Eventually(func(g Gomega) {
			var fresh tideprojectv1alpha3.Plan
			g.Expect(mgrClient.Get(ctx, types.NamespacedName{Name: crPlanName, Namespace: "default"}, &fresh)).To(Succeed())
			g.Expect(fresh.Status.PlanRolledUpUID).To(
				Equal(expectedJobName),
				"ADOPT-02: PlanRolledUpUID must be set to the planner Job name (D-03a new marker)")
		}, 5*time.Second, 100*time.Millisecond).Should(Succeed())

		proj := refetchProject(ctx, crPlanProjName)
		costBefore := proj.Status.Budget.CostSpentCents

		// Reload plan so it has the marker set in memory.
		Expect(mgrClient.Get(ctx, types.NamespacedName{Name: crPlanName, Namespace: "default"}, plan)).To(Succeed())

		// Second call: ADOPT-04.
		// isFirstCompletion=true (ReporterImage=""), but PlanRolledUpUID==jobName → skipped.
		_, err = r.handlePlannerJobCompletion(ctx, plan, nil)
		Expect(err).NotTo(HaveOccurred())

		Consistently(func(g Gomega) {
			proj := refetchProject(ctx, crPlanProjName)
			g.Expect(proj.Status.Budget.CostSpentCents).To(
				BeNumerically("==", costBefore),
				"ADOPT-04: CostSpentCents must NOT double-count after second post-TTL-GC completion (plan, D-03a)")
		}, 2*time.Second, 200*time.Millisecond).Should(Succeed())
	})
})
