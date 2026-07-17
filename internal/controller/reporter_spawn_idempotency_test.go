/*
Copyright 2026 TIDE Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// reporter_spawn_idempotency_test.go — Phase 47 Plan 10 (CR-01 / gap #2).
//
// Defect: the reporter-spawn gate at every spawn site (dispatch_helpers.go's
// shared spawnReporterIfNeeded, the project inline arm, and
// spawnTaskTraceReporterIfNeeded) was Get→IsNotFound→Create — name-only. The
// reporter Job's TTLSecondsAfterFinished=300 (reporter_jobspec.go) garbage-
// collects the Job, and once it's gone the name-only gate re-opens: a
// sustained-reconcile parent re-enters the gate and re-Creates a second
// reporter with freshly-recomputed ReporterOptions. The live proof measured
// only 115/386 LLM spans carrying full enrichment because of exactly this
// window. Plan 47-07 closed the gap with a durable per-attempt ".status"
// marker (MilestoneReporterSpawnedUID / PhaseReporterSpawnedUID /
// PlanReporterSpawnedUID / ProjectReporterSpawnedUID /
// TaskTraceReporterSpawnedUID) keyed on the completed planner/dispatch Job's
// UID (or its deterministic name when no Job object is observed). This file
// is the proof: a reporter Job deleted after its first spawn — simulating
// the 300s TTL-GC — must NOT be re-created when the parent's completion path
// re-runs for the SAME attempt, while a genuinely NEW attempt key still
// spawns.
//
// Simulation rule: envtest has no TTL controller. An explicit reporter-Job
// Delete (background propagation, waited to NotFound) IS the TTL-GC end
// state — the exact gate-reopening condition the defect exploits (mirrors
// child_rollup_idempotency_test.go's and project_planner_completion_test.go's
// BYPASS-03 established convention for this class of proof).
//
// Scope (deliberate, per plan): milestone stands in for the shared-helper
// shape (milestone/phase/plan all route through spawnReporterIfNeeded with
// the identical gate+stamp idiom) — one representative spec pins all three.
// Project's inline arm and Task's trace-only path are structurally distinct
// code and each get their own spec.
package controller

import (
	"context"
	"fmt"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	batchv1 "k8s.io/api/batch/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	tideprojectv1alpha3 "github.com/jsquirrelz/tide/api/v1alpha3"
	pkgdispatch "github.com/jsquirrelz/tide/pkg/dispatch"
)

// ─── Milestone level (shared-helper shape: representative of milestone/phase/plan) ──

var _ = Describe("ReporterSpawnIdempotency — Milestone level (shared-helper shape)", Label("envtest", "heavy"), func() {
	ctx := context.Background()

	const (
		rsiMSProjName = "reporter-spawn-idem-ms-proj"
		rsiMSName     = "reporter-spawn-idem-ms"
	)

	var envReader *mapEnvReader

	BeforeEach(func() {
		childRollupProject(ctx, rsiMSProjName)
		envReader = newMapEnvReader()
	})

	AfterEach(func() {
		ms := &tideprojectv1alpha3.Milestone{}
		if err := k8sClient.Get(ctx, types.NamespacedName{Name: rsiMSName, Namespace: "default"}, ms); err == nil {
			ms.Finalizers = nil
			_ = k8sClient.Update(ctx, ms)
			_ = k8sClient.Delete(ctx, ms)
		}
		cleanupChildRollupProject(ctx, rsiMSProjName)

		var jobs batchv1.JobList
		_ = k8sClient.List(ctx, &jobs, client.InNamespace("default"))
		for i := range jobs.Items {
			j := jobs.Items[i]
			if strings.HasPrefix(j.Name, "tide-reporter-") || strings.HasPrefix(j.Name, "tide-milestone-") {
				_ = k8sClient.Delete(ctx, &j, client.PropagationPolicy(metav1.DeletePropagationBackground))
			}
		}
	})

	It("does not re-create a TTL-GC'd reporter Job for the same attempt, but a distinct attempt key still spawns", func() {
		ms := &tideprojectv1alpha3.Milestone{
			ObjectMeta: metav1.ObjectMeta{Name: rsiMSName, Namespace: "default"},
			Spec:       tideprojectv1alpha3.MilestoneSpec{ProjectRef: rsiMSProjName},
		}
		Expect(k8sClient.Create(ctx, ms)).To(Succeed())
		waitForCacheSync(rsiMSName, "default", &tideprojectv1alpha3.Milestone{})
		Expect(mgrClient.Get(ctx, types.NamespacedName{Name: rsiMSName, Namespace: "default"}, ms)).To(Succeed())

		statusPatch := client.MergeFrom(ms.DeepCopy())
		ms.Status.Phase = "Running"
		Expect(k8sClient.Status().Patch(ctx, ms, statusPatch)).To(Succeed())
		Expect(mgrClient.Get(ctx, types.NamespacedName{Name: rsiMSName, Namespace: "default"}, ms)).To(Succeed())

		envReader.SetOut(string(ms.UID), pkgdispatch.EnvelopeOut{
			TaskUID:    string(ms.UID),
			ExitCode:   0,
			ChildCount: 0,
			Usage: pkgdispatch.Usage{
				InputTokens:        900,
				OutputTokens:       150,
				EstimatedCostCents: 21,
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
				ReporterImage:  testReporterImage,
				HelmProviderDefaults: ProviderDefaults{
					Image: testSubagentImage,
				},
			},
			PlannerPool: newPlannerPoolForTest(),
		}

		reporterJobName := fmt.Sprintf("tide-reporter-%s", ms.UID)
		milestoneJobName := fmt.Sprintf("tide-milestone-%s-1", ms.UID)

		// First completion (nil completedJob — deterministic-name fallback path,
		// mirrors child_rollup_idempotency_test.go's drive convention): spawns
		// the reporter Job and stamps the durable marker to milestoneJobName.
		_, err := r.handleJobCompletion(ctx, ms, nil)
		Expect(err).NotTo(HaveOccurred())

		Eventually(func() error {
			return mgrClient.Get(ctx, types.NamespacedName{Name: reporterJobName, Namespace: "default"}, &batchv1.Job{})
		}, "5s", "100ms").Should(Succeed(), "reporter Job must be spawned on first completion")

		Eventually(func(g Gomega) {
			var fresh tideprojectv1alpha3.Milestone
			g.Expect(mgrClient.Get(ctx, types.NamespacedName{Name: rsiMSName, Namespace: "default"}, &fresh)).To(Succeed())
			g.Expect(fresh.Status.MilestoneReporterSpawnedUID).To(Equal(milestoneJobName),
				"MilestoneReporterSpawnedUID must be stamped to the planner Job name after first spawn")
		}, "5s", "100ms").Should(Succeed())

		// CR-01 simulation: envtest has no TTL controller, so an explicit
		// Delete (background propagation, waited to NotFound) IS the 300s
		// TTL-GC end state — the exact gate-reopening condition under test.
		Expect(k8sClient.Delete(ctx, &batchv1.Job{
			ObjectMeta: metav1.ObjectMeta{Name: reporterJobName, Namespace: "default"},
		}, client.PropagationPolicy(metav1.DeletePropagationBackground))).To(Succeed())
		Eventually(func() bool {
			return apierrors.IsNotFound(mgrClient.Get(ctx, types.NamespacedName{Name: reporterJobName, Namespace: "default"}, &batchv1.Job{}))
		}, "5s", "100ms").Should(BeTrue(), "reporter Job must be gone (TTL-GC simulated) before re-driving completion")

		Expect(mgrClient.Get(ctx, types.NamespacedName{Name: rsiMSName, Namespace: "default"}, ms)).To(Succeed())

		// Re-drive the SAME completion (nil completedJob → same spawnKey =
		// milestoneJobName): the durable marker gate must skip the spawn
		// entirely — the exact CR-01 window this suite pins shut.
		_, err = r.handleJobCompletion(ctx, ms, nil)
		Expect(err).NotTo(HaveOccurred())

		Consistently(func() error {
			return mgrClient.Get(ctx, types.NamespacedName{Name: reporterJobName, Namespace: "default"}, &batchv1.Job{})
		}, "3s", "200ms").Should(MatchError(ContainSubstring("not found")),
			"a TTL-GC'd reporter Job must NOT be re-created for an already-observed spawn key")

		var afterSecond tideprojectv1alpha3.Milestone
		Expect(mgrClient.Get(ctx, types.NamespacedName{Name: rsiMSName, Namespace: "default"}, &afterSecond)).To(Succeed())
		Expect(afterSecond.Status.MilestoneReporterSpawnedUID).To(Equal(milestoneJobName),
			"marker must still hold the original spawn key after the skipped re-drive")

		// Negative control: diverge the marker to simulate a DISTINCT attempt
		// key (the plan's own "resetting the marker" allowance for this
		// proof) — a mismatched marker must still spawn a fresh reporter,
		// proving the gate is per-attempt equality, not a permanent one-shot
		// latch once any reporter has ever been observed.
		Expect(mgrClient.Get(ctx, types.NamespacedName{Name: rsiMSName, Namespace: "default"}, ms)).To(Succeed())
		markerResetPatch := client.MergeFrom(ms.DeepCopy())
		ms.Status.MilestoneReporterSpawnedUID = "stale-attempt-key"
		Expect(k8sClient.Status().Patch(ctx, ms, markerResetPatch)).To(Succeed())

		// The reconciler reads through mgrClient's informer cache, which syncs
		// the direct k8sClient write asynchronously — wait for the cache to
		// observe the reset before re-driving, otherwise the reconciler can
		// still see the OLD (already-matching) marker and skip the spawn,
		// producing a false negative on a slower/loaded suite run.
		Eventually(func(g Gomega) {
			g.Expect(mgrClient.Get(ctx, types.NamespacedName{Name: rsiMSName, Namespace: "default"}, ms)).To(Succeed())
			g.Expect(ms.Status.MilestoneReporterSpawnedUID).To(Equal("stale-attempt-key"))
		}, "5s", "100ms").Should(Succeed(), "cache must observe the marker reset before re-driving completion")

		_, err = r.handleJobCompletion(ctx, ms, nil)
		Expect(err).NotTo(HaveOccurred())

		Eventually(func() error {
			return mgrClient.Get(ctx, types.NamespacedName{Name: reporterJobName, Namespace: "default"}, &batchv1.Job{})
		}, "5s", "100ms").Should(Succeed(), "a distinct (mismatched) attempt key must spawn a fresh reporter Job")

		Eventually(func(g Gomega) {
			var fresh tideprojectv1alpha3.Milestone
			g.Expect(mgrClient.Get(ctx, types.NamespacedName{Name: rsiMSName, Namespace: "default"}, &fresh)).To(Succeed())
			g.Expect(fresh.Status.MilestoneReporterSpawnedUID).To(Equal(milestoneJobName),
				"marker must advance back to the current spawn key after the fresh spawn")
		}, "5s", "100ms").Should(Succeed())
	})
})

// ─── Project level (inline arm) ──────────────────────────────────────────────

var _ = Describe("ReporterSpawnIdempotency — Project level (inline arm)", Label("envtest", "heavy"), func() {
	ctx := context.Background()

	const rsiProjName = "reporter-spawn-idem-project"

	BeforeEach(func() {
		ensurePVC(ctx, qqhPVCName, "default")
	})

	AfterEach(func() {
		qqhCleanupProject(ctx, rsiProjName)
		var jobs batchv1.JobList
		_ = k8sClient.List(ctx, &jobs, client.InNamespace("default"))
		for i := range jobs.Items {
			j := jobs.Items[i]
			if strings.HasPrefix(j.Name, "tide-project-") || strings.HasPrefix(j.Name, "tide-reporter-") {
				_ = k8sClient.Delete(ctx, &j, client.PropagationPolicy(metav1.DeletePropagationBackground))
			}
		}
	})

	It("does not re-create a TTL-GC'd reporter Job for the same project planner completion", func() {
		proj := qqhCreateProject(ctx, rsiProjName)

		envReader := newMapEnvReader()
		r := qqhBuildReconciler(envReader)

		const plannerCostCents = int64(64)
		envReader.SetOut(string(proj.UID), pkgdispatch.EnvelopeOut{
			TaskUID:    string(proj.UID),
			ExitCode:   0,
			ChildCount: 0,
			Usage: pkgdispatch.Usage{
				InputTokens:        700,
				OutputTokens:       120,
				EstimatedCostCents: plannerCostCents,
			},
		})

		reporterJobName := fmt.Sprintf("tide-reporter-%s", proj.UID)
		plannerJobName := fmt.Sprintf("tide-project-%s-1", proj.UID)

		// First completion (nil completedJob — the TTL-GC'd/never-observed
		// planner Job fallback path, mirrors BYPASS-05): spawns the reporter
		// and stamps the durable marker to plannerJobName.
		_, err := r.handleProjectJobCompletion(ctx, proj, nil)
		Expect(err).NotTo(HaveOccurred())

		Eventually(func() error {
			return mgrClient.Get(ctx, types.NamespacedName{Name: reporterJobName, Namespace: "default"}, &batchv1.Job{})
		}, "5s", "100ms").Should(Succeed(), "reporter Job must be spawned on first completion")

		Eventually(func(g Gomega) {
			var fresh tideprojectv1alpha3.Project
			g.Expect(mgrClient.Get(ctx, types.NamespacedName{Name: rsiProjName, Namespace: "default"}, &fresh)).To(Succeed())
			g.Expect(fresh.Status.ProjectReporterSpawnedUID).To(Equal(plannerJobName),
				"ProjectReporterSpawnedUID must be stamped to the planner Job name after first spawn")
		}, "5s", "100ms").Should(Succeed())

		// CR-01 simulation: explicit Delete IS the TTL-GC end state in envtest.
		Expect(k8sClient.Delete(ctx, &batchv1.Job{
			ObjectMeta: metav1.ObjectMeta{Name: reporterJobName, Namespace: "default"},
		}, client.PropagationPolicy(metav1.DeletePropagationBackground))).To(Succeed())
		Eventually(func() bool {
			return apierrors.IsNotFound(mgrClient.Get(ctx, types.NamespacedName{Name: reporterJobName, Namespace: "default"}, &batchv1.Job{}))
		}, "5s", "100ms").Should(BeTrue(), "reporter Job must be gone (TTL-GC simulated) before re-driving completion")

		Expect(mgrClient.Get(ctx, types.NamespacedName{Name: rsiProjName, Namespace: "default"}, proj)).To(Succeed())

		// Re-drive the SAME completion (nil completedJob → same spawnKey =
		// plannerJobName): the durable marker gate must skip the whole inline
		// spawn arm entirely.
		_, err = r.handleProjectJobCompletion(ctx, proj, nil)
		Expect(err).NotTo(HaveOccurred())

		Consistently(func() error {
			return mgrClient.Get(ctx, types.NamespacedName{Name: reporterJobName, Namespace: "default"}, &batchv1.Job{})
		}, "3s", "200ms").Should(MatchError(ContainSubstring("not found")),
			"a TTL-GC'd reporter Job must NOT be re-created for an already-observed project completion")

		var afterSecond tideprojectv1alpha3.Project
		Expect(mgrClient.Get(ctx, types.NamespacedName{Name: rsiProjName, Namespace: "default"}, &afterSecond)).To(Succeed())
		Expect(afterSecond.Status.ProjectReporterSpawnedUID).To(Equal(plannerJobName),
			"marker must still hold the original spawn key after the skipped re-drive")
	})
})

// ─── Task level (trace-only path) ────────────────────────────────────────────

var _ = Describe("ReporterSpawnIdempotency — Task level (trace-only path)", Label("envtest", "heavy"), func() {
	ctx := context.Background()

	const (
		rsiTaskProjName = "reporter-spawn-idem-task-proj"
		rsiTaskMSName   = "reporter-spawn-idem-task-ms"
		rsiTaskPhName   = "reporter-spawn-idem-task-ph"
		rsiTaskPlanName = "reporter-spawn-idem-task-plan"
		rsiTaskName     = "reporter-spawn-idem-task"

		rsiTaskOTLPEndpoint = "otel-collector.tide-system.svc:4317"
	)

	var envReader *mapEnvReader

	BeforeEach(func() {
		spanEmissionProject(ctx, rsiTaskProjName, "claude-test-model")
		ms := &tideprojectv1alpha3.Milestone{
			ObjectMeta: metav1.ObjectMeta{Name: rsiTaskMSName, Namespace: "default"},
			Spec:       tideprojectv1alpha3.MilestoneSpec{ProjectRef: rsiTaskProjName},
		}
		Expect(k8sClient.Create(ctx, ms)).To(Succeed())
		waitForCacheSync(rsiTaskMSName, "default", &tideprojectv1alpha3.Milestone{})
		ph := &tideprojectv1alpha3.Phase{
			ObjectMeta: metav1.ObjectMeta{Name: rsiTaskPhName, Namespace: "default"},
			Spec:       tideprojectv1alpha3.PhaseSpec{MilestoneRef: rsiTaskMSName},
		}
		Expect(k8sClient.Create(ctx, ph)).To(Succeed())
		waitForCacheSync(rsiTaskPhName, "default", &tideprojectv1alpha3.Phase{})
		plan := &tideprojectv1alpha3.Plan{
			ObjectMeta: metav1.ObjectMeta{Name: rsiTaskPlanName, Namespace: "default"},
			Spec:       tideprojectv1alpha3.PlanSpec{PhaseRef: rsiTaskPhName},
		}
		Expect(k8sClient.Create(ctx, plan)).To(Succeed())
		waitForCacheSync(rsiTaskPlanName, "default", &tideprojectv1alpha3.Plan{})
		envReader = newMapEnvReader()
	})

	AfterEach(func() {
		task := &tideprojectv1alpha3.Task{}
		if err := k8sClient.Get(ctx, types.NamespacedName{Name: rsiTaskName, Namespace: "default"}, task); err == nil {
			task.Finalizers = nil
			_ = k8sClient.Update(ctx, task)
			_ = k8sClient.Delete(ctx, task)
		}
		plan := &tideprojectv1alpha3.Plan{}
		if err := k8sClient.Get(ctx, types.NamespacedName{Name: rsiTaskPlanName, Namespace: "default"}, plan); err == nil {
			plan.Finalizers = nil
			_ = k8sClient.Update(ctx, plan)
			_ = k8sClient.Delete(ctx, plan)
		}
		ph := &tideprojectv1alpha3.Phase{}
		if err := k8sClient.Get(ctx, types.NamespacedName{Name: rsiTaskPhName, Namespace: "default"}, ph); err == nil {
			ph.Finalizers = nil
			_ = k8sClient.Update(ctx, ph)
			_ = k8sClient.Delete(ctx, ph)
		}
		ms := &tideprojectv1alpha3.Milestone{}
		if err := k8sClient.Get(ctx, types.NamespacedName{Name: rsiTaskMSName, Namespace: "default"}, ms); err == nil {
			ms.Finalizers = nil
			_ = k8sClient.Update(ctx, ms)
			_ = k8sClient.Delete(ctx, ms)
		}
		cleanupSpanEmissionProject(ctx, rsiTaskProjName)

		var jobs batchv1.JobList
		_ = k8sClient.List(ctx, &jobs, client.InNamespace("default"))
		for i := range jobs.Items {
			j := jobs.Items[i]
			_ = k8sClient.Delete(ctx, &j)
		}
	})

	It("does not re-create a TTL-GC'd trace-only reporter Job for the same completed-Job attempt", func() {
		task := &tideprojectv1alpha3.Task{
			ObjectMeta: metav1.ObjectMeta{Name: rsiTaskName, Namespace: "default"},
			Spec: tideprojectv1alpha3.TaskSpec{
				PlanRef:             rsiTaskPlanName,
				FilesTouched:        []string{"src/main.go"},
				PromptPath:          "envelopes/test/children/" + rsiTaskName + ".json",
				DeclaredOutputPaths: []string{"artifacts/out.txt"},
			},
		}
		Expect(k8sClient.Create(ctx, task)).To(Succeed())
		waitForCacheSync(rsiTaskName, "default", &tideprojectv1alpha3.Task{})
		Expect(mgrClient.Get(ctx, types.NamespacedName{Name: rsiTaskName, Namespace: "default"}, task)).To(Succeed())

		statusPatch := client.MergeFrom(task.DeepCopy())
		task.Status.Phase = "Running"
		Expect(k8sClient.Status().Patch(ctx, task, statusPatch)).To(Succeed())
		Expect(mgrClient.Get(ctx, types.NamespacedName{Name: rsiTaskName, Namespace: "default"}, task)).To(Succeed())

		var proj tideprojectv1alpha3.Project
		Expect(mgrClient.Get(ctx, types.NamespacedName{Name: rsiTaskProjName, Namespace: "default"}, &proj)).To(Succeed())

		start := time.Now().Add(-5 * time.Minute)
		end := time.Now()
		// completedJob is never created in the cluster — handleJobCompletion only
		// reads Status fields off the object it is handed directly (mirrors
		// span_emission_test.go's succeededPlannerJob convention). Task's spawn
		// key is always completedJob.UID (no nil-fallback branch, unlike the
		// four planner-tier levels), so re-driving with this SAME object is what
		// exercises the "same attempt" gate.
		completedJob := succeededPlannerJob("rsi-task-job", start, end)

		envReader.SetOut(string(task.UID), pkgdispatch.EnvelopeOut{
			TaskUID:     string(task.UID),
			Result:      "success",
			CompletedAt: end,
		})

		r := &TaskReconciler{
			Client: mgrClient,
			Scheme: k8sClient.Scheme(),
			Deps: TaskReconcilerDeps{
				Dispatcher:     &stubDispatcher{},
				EnvReader:      envReader,
				CredproxyImage: testCredproxyImage,
				SigningKey:     testSigningKey,
				HelmProviderDefaults: ProviderDefaults{
					Image: testSubagentImage,
				},
				ReporterImage: testReporterImage,
				OTLPEndpoint:  rsiTaskOTLPEndpoint,
			},
		}

		wantJobName := "tide-reporter-trace-" + string(completedJob.UID)

		// First completion: spawns the trace-only reporter and stamps the
		// durable marker to the completed dispatch Job's UID.
		_, err := r.handleJobCompletion(ctx, task, &proj, completedJob)
		Expect(err).NotTo(HaveOccurred())

		Eventually(func() error {
			return mgrClient.Get(ctx, types.NamespacedName{Name: wantJobName, Namespace: "default"}, &batchv1.Job{})
		}, "5s", "100ms").Should(Succeed(), "trace-only reporter Job must be spawned on first completion")

		Eventually(func(g Gomega) {
			var fresh tideprojectv1alpha3.Task
			g.Expect(mgrClient.Get(ctx, types.NamespacedName{Name: rsiTaskName, Namespace: "default"}, &fresh)).To(Succeed())
			g.Expect(fresh.Status.TaskTraceReporterSpawnedUID).To(Equal(string(completedJob.UID)),
				"TaskTraceReporterSpawnedUID must be stamped to the completed Job's UID after first spawn")
		}, "5s", "100ms").Should(Succeed())

		// CR-01 simulation: explicit Delete IS the TTL-GC end state.
		Expect(k8sClient.Delete(ctx, &batchv1.Job{
			ObjectMeta: metav1.ObjectMeta{Name: wantJobName, Namespace: "default"},
		}, client.PropagationPolicy(metav1.DeletePropagationBackground))).To(Succeed())
		Eventually(func() bool {
			return apierrors.IsNotFound(mgrClient.Get(ctx, types.NamespacedName{Name: wantJobName, Namespace: "default"}, &batchv1.Job{}))
		}, "5s", "100ms").Should(BeTrue(), "trace-only reporter Job must be gone (TTL-GC simulated) before re-driving completion")

		Expect(mgrClient.Get(ctx, types.NamespacedName{Name: rsiTaskName, Namespace: "default"}, task)).To(Succeed())

		// Re-drive with the SAME completedJob (same UID → same spawn key): the
		// durable marker gate must skip the spawn entirely — the exact CR-01
		// window this suite pins shut.
		_, err = r.handleJobCompletion(ctx, task, &proj, completedJob)
		Expect(err).NotTo(HaveOccurred())

		Consistently(func() error {
			return mgrClient.Get(ctx, types.NamespacedName{Name: wantJobName, Namespace: "default"}, &batchv1.Job{})
		}, "3s", "200ms").Should(MatchError(ContainSubstring("not found")),
			"a TTL-GC'd trace-only reporter Job must NOT be re-created for the same completed-Job attempt")

		var afterSecond tideprojectv1alpha3.Task
		Expect(mgrClient.Get(ctx, types.NamespacedName{Name: rsiTaskName, Namespace: "default"}, &afterSecond)).To(Succeed())
		Expect(afterSecond.Status.TaskTraceReporterSpawnedUID).To(Equal(string(completedJob.UID)),
			"marker must still hold the original spawn key after the skipped re-drive")
	})
})
