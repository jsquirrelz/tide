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
// shape (milestone/phase all route through spawnReporterIfNeeded with the
// identical gate+stamp idiom) — one representative spec pins both. Project's
// inline arm, Plan's inline arm, and Task's trace-only path are structurally
// distinct code; project and task each get their own spec.
//
// Phase 52 live-proof DEFECT-B (the plan inline arm's own spec, at the bottom
// of this file): the plan level is the ONE level whose planner re-dispatches
// (52-09's findings-seeded re-plan), and its reporter Job name was
// attempt-blind ("tide-reporter-<planUID>"). Attempt 2's spawn found attempt
// 1's completed reporter Job by name (still inside its 300s TTL — a stub
// planner attempt takes ~20s), skipped the Create as T-09-13 idempotency,
// stamped the durable marker, and the re-planned attempt's children were
// NEVER materialized: the plan-check loop dead-stalled in Running with zero
// errors. The fix suffixes re-plan attempts ("tide-reporter-<planUID>-<n>",
// n>1); attempt 1 keeps the historical unsuffixed name.
//
// CR-01 re-fix (Phase 47 gap-closure #2): the first-generation marker above
// closed only the reporter-Job-GC window and left a SECOND window open. The
// marker is stamped in TWO key spaces — the live planner Job's UID (when a Job
// is present on the reconcile) and the deterministic level name (when
// completedJob is nil). A level that stays Running past its planner Job's 600s
// TTL-GC first stamps the UID, then re-enters handleJobCompletion with
// completedJob == nil and recomputes spawnKey to the NAME; a UID can never
// equal that name, so the old equality gate reopened and spawned a duplicate
// reporter (the first was already gone at its own 300s TTL). The two
// "planner-Job TTL-GC re-entry" specs at the bottom of this file pin that
// transition shut: the FIRST completion carries a live planner Job (marker =
// UID), the SECOND carries completedJob == nil (spawnKey = name). The
// reporter-Job-GC specs above intentionally keep BOTH completions on the
// nil-Job path, so the marker there is a name in both — they never exercise
// the UID→name transition, hence these dedicated specs. Task is exempt: its
// handler early-returns on completedJob == nil, so it never reaches the
// nil-Job key-space recompute.
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

		// Negative control: a genuinely NEW attempt still spawns, proving the
		// gate is per-attempt on the live-Job path (case a) and not a permanent
		// one-shot latch. After the CR-01 re-fix, the nil-Job short-circuit
		// (case b) treats ANY non-empty marker as "already spawned" — so a fresh
		// spawn now requires a live planner Job (non-nil completedJob) whose UID
		// differs from the stored marker (a mismatched marker on the nil-Job path
		// no longer re-opens the gate; that was the CR-01 bug). Diverge the marker
		// first so the new live-Job attempt is unambiguously distinct.
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

		distinctStart := time.Now().Add(-5 * time.Minute)
		distinctEnd := time.Now()
		distinctJob := succeededPlannerJob("rsi-ms-distinct-attempt", distinctStart, distinctEnd)
		_, err = r.handleJobCompletion(ctx, ms, distinctJob)
		Expect(err).NotTo(HaveOccurred())

		Eventually(func() error {
			return mgrClient.Get(ctx, types.NamespacedName{Name: reporterJobName, Namespace: "default"}, &batchv1.Job{})
		}, "5s", "100ms").Should(Succeed(), "a distinct live-Job attempt must spawn a fresh reporter Job")

		Eventually(func(g Gomega) {
			var fresh tideprojectv1alpha3.Milestone
			g.Expect(mgrClient.Get(ctx, types.NamespacedName{Name: rsiMSName, Namespace: "default"}, &fresh)).To(Succeed())
			g.Expect(fresh.Status.MilestoneReporterSpawnedUID).To(Equal(string(distinctJob.UID)),
				"marker must advance to the distinct attempt's live-Job UID after the fresh spawn")
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

// ─── Milestone level — planner-Job TTL-GC re-entry (CR-01 re-fix regression pin) ──
//
// Unlike the reporter-Job-GC milestone spec above (which drives BOTH completions
// with a nil completedJob, stamping the marker to the deterministic name), this
// spec drives the FIRST completion with a LIVE planner Job so the marker is
// stamped in the UID key space, then re-enters with completedJob == nil (the
// planner Job's own 600s TTL-GC) so spawnKey is recomputed to the name. That
// UID(marker) vs name(spawnKey) mismatch is the exact window the first-generation
// marker left open; the alreadySpawned predicate now honors the non-empty marker
// directly on the nil-Job path. Milestone stands in for phase (same shared-helper
// gate+stamp idiom).
var _ = Describe("ReporterSpawnIdempotency — Milestone level (planner-Job TTL-GC re-entry)", Label("envtest", "heavy"), func() {
	ctx := context.Background()

	const (
		rsiMSGCProjName = "reporter-spawn-idem-ms-gc-proj"
		rsiMSGCName     = "reporter-spawn-idem-ms-gc"
	)

	var envReader *mapEnvReader

	BeforeEach(func() {
		childRollupProject(ctx, rsiMSGCProjName)
		envReader = newMapEnvReader()
	})

	AfterEach(func() {
		ms := &tideprojectv1alpha3.Milestone{}
		if err := k8sClient.Get(ctx, types.NamespacedName{Name: rsiMSGCName, Namespace: "default"}, ms); err == nil {
			ms.Finalizers = nil
			_ = k8sClient.Update(ctx, ms)
			_ = k8sClient.Delete(ctx, ms)
		}
		cleanupChildRollupProject(ctx, rsiMSGCProjName)

		var jobs batchv1.JobList
		_ = k8sClient.List(ctx, &jobs, client.InNamespace("default"))
		for i := range jobs.Items {
			j := jobs.Items[i]
			if strings.HasPrefix(j.Name, "tide-reporter-") || strings.HasPrefix(j.Name, "tide-milestone-") {
				_ = k8sClient.Delete(ctx, &j, client.PropagationPolicy(metav1.DeletePropagationBackground))
			}
		}
	})

	It("honors the durable marker when the planner Job is TTL-GC'd (completedJob UID→nil-name), spawning no duplicate reporter", func() {
		ms := &tideprojectv1alpha3.Milestone{
			ObjectMeta: metav1.ObjectMeta{Name: rsiMSGCName, Namespace: "default"},
			Spec:       tideprojectv1alpha3.MilestoneSpec{ProjectRef: rsiMSGCProjName},
		}
		Expect(k8sClient.Create(ctx, ms)).To(Succeed())
		waitForCacheSync(rsiMSGCName, "default", &tideprojectv1alpha3.Milestone{})
		Expect(mgrClient.Get(ctx, types.NamespacedName{Name: rsiMSGCName, Namespace: "default"}, ms)).To(Succeed())

		statusPatch := client.MergeFrom(ms.DeepCopy())
		ms.Status.Phase = "Running"
		Expect(k8sClient.Status().Patch(ctx, ms, statusPatch)).To(Succeed())
		Expect(mgrClient.Get(ctx, types.NamespacedName{Name: rsiMSGCName, Namespace: "default"}, ms)).To(Succeed())

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

		// First completion with the planner Job PRESENT (non-nil completedJob):
		// spawnKey is the live Job's UID, so the durable marker is stamped in the
		// UID key space — NOT the deterministic milestoneJobName.
		start := time.Now().Add(-5 * time.Minute)
		end := time.Now()
		plannerJob := succeededPlannerJob("rsi-ms-gc-planner", start, end)
		_, err := r.handleJobCompletion(ctx, ms, plannerJob)
		Expect(err).NotTo(HaveOccurred())

		Eventually(func() error {
			return mgrClient.Get(ctx, types.NamespacedName{Name: reporterJobName, Namespace: "default"}, &batchv1.Job{})
		}, "5s", "100ms").Should(Succeed(), "reporter Job must be spawned on first completion")

		Eventually(func(g Gomega) {
			var fresh tideprojectv1alpha3.Milestone
			g.Expect(mgrClient.Get(ctx, types.NamespacedName{Name: rsiMSGCName, Namespace: "default"}, &fresh)).To(Succeed())
			g.Expect(fresh.Status.MilestoneReporterSpawnedUID).To(Equal(string(plannerJob.UID)),
				"marker must be stamped to the live planner Job's UID (not the deterministic name) on first completion")
		}, "5s", "100ms").Should(Succeed())

		// Reporter's own 300s TTL-GC: delete the reporter Job and wait to NotFound
		// so the shared helper's inner name-based Get cannot mask the gate.
		Expect(k8sClient.Delete(ctx, &batchv1.Job{
			ObjectMeta: metav1.ObjectMeta{Name: reporterJobName, Namespace: "default"},
		}, client.PropagationPolicy(metav1.DeletePropagationBackground))).To(Succeed())
		Eventually(func() bool {
			return apierrors.IsNotFound(mgrClient.Get(ctx, types.NamespacedName{Name: reporterJobName, Namespace: "default"}, &batchv1.Job{}))
		}, "5s", "100ms").Should(BeTrue(), "reporter Job must be gone (300s reporter TTL-GC simulated)")

		Expect(mgrClient.Get(ctx, types.NamespacedName{Name: rsiMSGCName, Namespace: "default"}, ms)).To(Succeed())

		// Planner Job's 600s TTL-GC: the next reconcile can no longer find the
		// planner Job, so completedJob == nil while the milestone is still Running.
		// Pre-fix, spawnKey recomputes to milestoneJobName (a NAME) and
		// marker(UID) != spawnKey(name) reopened the gate → a second reporter.
		_, err = r.handleJobCompletion(ctx, ms, nil)
		Expect(err).NotTo(HaveOccurred())

		Consistently(func() error {
			return mgrClient.Get(ctx, types.NamespacedName{Name: reporterJobName, Namespace: "default"}, &batchv1.Job{})
		}, "3s", "200ms").Should(MatchError(ContainSubstring("not found")),
			"the nil-Job re-entry must NOT recompute a name key and re-create the reporter")

		var afterSecond tideprojectv1alpha3.Milestone
		Expect(mgrClient.Get(ctx, types.NamespacedName{Name: rsiMSGCName, Namespace: "default"}, &afterSecond)).To(Succeed())
		Expect(afterSecond.Status.MilestoneReporterSpawnedUID).To(Equal(string(plannerJob.UID)),
			"marker must still hold the original planner-Job UID after the skipped nil-Job re-drive")
	})
})

// ─── Project level — planner-Job TTL-GC re-entry (CR-01 re-fix regression pin) ──
//
// The project inline-arm analogue of the milestone spec above: first completion
// carries a live planner Job (marker = UID), then the nil-Job re-entry (planner
// Job 600s TTL-GC) recomputes spawnKey to plannerJobName. Pre-fix, marker(UID) !=
// spawnKey(name) reopened the inline gate and re-Created the reporter.
var _ = Describe("ReporterSpawnIdempotency — Project level (planner-Job TTL-GC re-entry)", Label("envtest", "heavy"), func() {
	ctx := context.Background()

	const rsiProjGCName = "reporter-spawn-idem-project-gc"

	BeforeEach(func() {
		ensurePVC(ctx, qqhPVCName, "default")
	})

	AfterEach(func() {
		qqhCleanupProject(ctx, rsiProjGCName)
		var jobs batchv1.JobList
		_ = k8sClient.List(ctx, &jobs, client.InNamespace("default"))
		for i := range jobs.Items {
			j := jobs.Items[i]
			if strings.HasPrefix(j.Name, "tide-project-") || strings.HasPrefix(j.Name, "tide-reporter-") {
				_ = k8sClient.Delete(ctx, &j, client.PropagationPolicy(metav1.DeletePropagationBackground))
			}
		}
	})

	It("honors the durable marker when the project planner Job is TTL-GC'd (completedJob UID→nil-name), spawning no duplicate reporter", func() {
		proj := qqhCreateProject(ctx, rsiProjGCName)

		envReader := newMapEnvReader()
		r := qqhBuildReconciler(envReader)

		envReader.SetOut(string(proj.UID), pkgdispatch.EnvelopeOut{
			TaskUID:    string(proj.UID),
			ExitCode:   0,
			ChildCount: 0,
			Usage: pkgdispatch.Usage{
				InputTokens:        700,
				OutputTokens:       120,
				EstimatedCostCents: 64,
			},
		})

		reporterJobName := fmt.Sprintf("tide-reporter-%s", proj.UID)

		// First completion with the planner Job PRESENT: marker stamped in the UID
		// key space.
		start := time.Now().Add(-5 * time.Minute)
		end := time.Now()
		plannerJob := succeededPlannerJob("rsi-project-gc-planner", start, end)
		_, err := r.handleProjectJobCompletion(ctx, proj, plannerJob)
		Expect(err).NotTo(HaveOccurred())

		Eventually(func() error {
			return mgrClient.Get(ctx, types.NamespacedName{Name: reporterJobName, Namespace: "default"}, &batchv1.Job{})
		}, "5s", "100ms").Should(Succeed(), "reporter Job must be spawned on first completion")

		Eventually(func(g Gomega) {
			var fresh tideprojectv1alpha3.Project
			g.Expect(mgrClient.Get(ctx, types.NamespacedName{Name: rsiProjGCName, Namespace: "default"}, &fresh)).To(Succeed())
			g.Expect(fresh.Status.ProjectReporterSpawnedUID).To(Equal(string(plannerJob.UID)),
				"marker must be stamped to the live planner Job's UID (not the deterministic name) on first completion")
		}, "5s", "100ms").Should(Succeed())

		// Reporter's own 300s TTL-GC.
		Expect(k8sClient.Delete(ctx, &batchv1.Job{
			ObjectMeta: metav1.ObjectMeta{Name: reporterJobName, Namespace: "default"},
		}, client.PropagationPolicy(metav1.DeletePropagationBackground))).To(Succeed())
		Eventually(func() bool {
			return apierrors.IsNotFound(mgrClient.Get(ctx, types.NamespacedName{Name: reporterJobName, Namespace: "default"}, &batchv1.Job{}))
		}, "5s", "100ms").Should(BeTrue(), "reporter Job must be gone (300s reporter TTL-GC simulated)")

		Expect(mgrClient.Get(ctx, types.NamespacedName{Name: rsiProjGCName, Namespace: "default"}, proj)).To(Succeed())

		// Planner Job's 600s TTL-GC: nil-Job re-entry recomputes spawnKey to the
		// name — the durable marker (UID) must still short-circuit the inline arm.
		_, err = r.handleProjectJobCompletion(ctx, proj, nil)
		Expect(err).NotTo(HaveOccurred())

		Consistently(func() error {
			return mgrClient.Get(ctx, types.NamespacedName{Name: reporterJobName, Namespace: "default"}, &batchv1.Job{})
		}, "3s", "200ms").Should(MatchError(ContainSubstring("not found")),
			"the nil-Job re-entry must NOT recompute a name key and re-create the reporter")

		var afterSecond tideprojectv1alpha3.Project
		Expect(mgrClient.Get(ctx, types.NamespacedName{Name: rsiProjGCName, Namespace: "default"}, &afterSecond)).To(Succeed())
		Expect(afterSecond.Status.ProjectReporterSpawnedUID).To(Equal(string(plannerJob.UID)),
			"marker must still hold the original planner-Job UID after the skipped nil-Job re-drive")
	})
})

// ─── Plan level (inline arm — re-plan attempt reporter collision, DEFECT-B) ──

var _ = Describe("ReporterSpawnIdempotency — Plan level (re-plan attempt reporter collision)", Label("envtest", "heavy"), func() {
	ctx := context.Background()

	const (
		rsiPlanProjName = "rsi-plan-proj"
		rsiPlanMSName   = "rsi-plan-ms"
		rsiPlanPhName   = "rsi-plan-ph"
		rsiPlanName     = "rsi-plan"
	)

	var envReader *mapEnvReader

	BeforeEach(func() {
		childRollupProject(ctx, rsiPlanProjName)
		ms := &tideprojectv1alpha3.Milestone{
			ObjectMeta: metav1.ObjectMeta{Name: rsiPlanMSName, Namespace: "default"},
			Spec:       tideprojectv1alpha3.MilestoneSpec{ProjectRef: rsiPlanProjName},
		}
		Expect(k8sClient.Create(ctx, ms)).To(Succeed())
		waitForCacheSync(rsiPlanMSName, "default", &tideprojectv1alpha3.Milestone{})
		ph := &tideprojectv1alpha3.Phase{
			ObjectMeta: metav1.ObjectMeta{Name: rsiPlanPhName, Namespace: "default"},
			Spec:       tideprojectv1alpha3.PhaseSpec{MilestoneRef: rsiPlanMSName},
		}
		Expect(k8sClient.Create(ctx, ph)).To(Succeed())
		waitForCacheSync(rsiPlanPhName, "default", &tideprojectv1alpha3.Phase{})
		envReader = newMapEnvReader()
	})

	AfterEach(func() {
		plan := &tideprojectv1alpha3.Plan{}
		if err := k8sClient.Get(ctx, types.NamespacedName{Name: rsiPlanName, Namespace: "default"}, plan); err == nil {
			plan.Finalizers = nil
			_ = k8sClient.Update(ctx, plan)
			_ = k8sClient.Delete(ctx, plan)
		}
		ph := &tideprojectv1alpha3.Phase{}
		if err := k8sClient.Get(ctx, types.NamespacedName{Name: rsiPlanPhName, Namespace: "default"}, ph); err == nil {
			ph.Finalizers = nil
			_ = k8sClient.Update(ctx, ph)
			_ = k8sClient.Delete(ctx, ph)
		}
		ms := &tideprojectv1alpha3.Milestone{}
		if err := k8sClient.Get(ctx, types.NamespacedName{Name: rsiPlanMSName, Namespace: "default"}, ms); err == nil {
			ms.Finalizers = nil
			_ = k8sClient.Update(ctx, ms)
			_ = k8sClient.Delete(ctx, ms)
		}
		cleanupChildRollupProject(ctx, rsiPlanProjName)

		var jobs batchv1.JobList
		_ = k8sClient.List(ctx, &jobs, client.InNamespace("default"))
		for i := range jobs.Items {
			j := jobs.Items[i]
			if strings.HasPrefix(j.Name, "tide-reporter-") || strings.HasPrefix(j.Name, "tide-plan-") {
				_ = k8sClient.Delete(ctx, &j, client.PropagationPolicy(metav1.DeletePropagationBackground))
			}
		}
	})

	It("spawns a fresh attempt-suffixed reporter Job for a re-planned attempt while attempt 1's reporter Job still lives (52 D-04; live-proof DEFECT-B)", func() {
		plan := &tideprojectv1alpha3.Plan{
			ObjectMeta: metav1.ObjectMeta{Name: rsiPlanName, Namespace: "default"},
			Spec:       tideprojectv1alpha3.PlanSpec{PhaseRef: rsiPlanPhName},
		}
		Expect(k8sClient.Create(ctx, plan)).To(Succeed())
		waitForCacheSync(rsiPlanName, "default", &tideprojectv1alpha3.Plan{})
		Expect(mgrClient.Get(ctx, types.NamespacedName{Name: rsiPlanName, Namespace: "default"}, plan)).To(Succeed())

		statusPatch := client.MergeFrom(plan.DeepCopy())
		plan.Status.Phase = "Running"
		Expect(k8sClient.Status().Patch(ctx, plan, statusPatch)).To(Succeed())
		Expect(mgrClient.Get(ctx, types.NamespacedName{Name: rsiPlanName, Namespace: "default"}, plan)).To(Succeed())

		envReader.SetOut(string(plan.UID), pkgdispatch.EnvelopeOut{
			TaskUID:    string(plan.UID),
			ExitCode:   0,
			ChildCount: 0,
		})

		r := &PlanReconciler{
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

		// Attempt 1 (Iteration 0): a live planner Job completes → the reporter
		// spawns under the historical unsuffixed name (backcompat pin — every
		// non-re-planned level keeps this exact name).
		start := time.Now().Add(-5 * time.Minute)
		job1 := succeededPlannerJob("rsi-plan-attempt-1", start, time.Now())
		_, err := r.handlePlannerJobCompletion(ctx, plan, job1)
		Expect(err).NotTo(HaveOccurred())

		unsuffixedName := fmt.Sprintf("tide-reporter-%s", plan.UID)
		Eventually(func() error {
			return mgrClient.Get(ctx, types.NamespacedName{Name: unsuffixedName, Namespace: "default"}, &batchv1.Job{})
		}, "5s", "100ms").Should(Succeed(), "attempt 1's reporter Job must keep the unsuffixed name")

		Eventually(func(g Gomega) {
			var fresh tideprojectv1alpha3.Plan
			g.Expect(mgrClient.Get(ctx, types.NamespacedName{Name: rsiPlanName, Namespace: "default"}, &fresh)).To(Succeed())
			g.Expect(fresh.Status.PlanReporterSpawnedUID).To(Equal(string(job1.UID)),
				"marker must hold attempt 1's live-Job UID")
		}, "5s", "100ms").Should(Succeed())

		// Simulate 52-09 dispatchPlanRepair's quality bump: a REPAIRABLE verdict
		// deleted the rejected children and set LoopStatus.Iteration=1, so the
		// next planner completion is attempt 2. Attempt 1's reporter Job is
		// deliberately NOT deleted — the live-proof race: a stub planner attempt
		// takes ~20s, far inside the reporter's 300s TTL.
		Expect(mgrClient.Get(ctx, types.NamespacedName{Name: rsiPlanName, Namespace: "default"}, plan)).To(Succeed())
		iterPatch := client.MergeFrom(plan.DeepCopy())
		plan.Status.LoopStatus.Iteration = 1
		Expect(k8sClient.Status().Patch(ctx, plan, iterPatch)).To(Succeed())
		Eventually(func(g Gomega) {
			g.Expect(mgrClient.Get(ctx, types.NamespacedName{Name: rsiPlanName, Namespace: "default"}, plan)).To(Succeed())
			g.Expect(plan.Status.LoopStatus.Iteration).To(BeEquivalentTo(1))
		}, "5s", "100ms").Should(Succeed(), "cache must observe the repair's Iteration bump before re-driving completion")

		job2 := succeededPlannerJob("rsi-plan-attempt-2", start, time.Now())
		_, err = r.handlePlannerJobCompletion(ctx, plan, job2)
		Expect(err).NotTo(HaveOccurred())

		// DEFECT-B pin: attempt 2's reporter must be a DISTINCT Job. The
		// unsuffixed name is occupied by attempt 1's completed reporter, so a
		// name-only gate silently skips the spawn, stamps the marker, and the
		// re-planned attempt's children are never materialized (the loop
		// dead-stalls in Running with zero errors — observed live, tide-lv4).
		suffixedName := fmt.Sprintf("tide-reporter-%s-2", plan.UID)
		Eventually(func() error {
			return mgrClient.Get(ctx, types.NamespacedName{Name: suffixedName, Namespace: "default"}, &batchv1.Job{})
		}, "5s", "100ms").Should(Succeed(),
			"attempt 2's reporter Job must spawn under the attempt-suffixed name while attempt 1's Job still exists")

		Eventually(func(g Gomega) {
			var fresh tideprojectv1alpha3.Plan
			g.Expect(mgrClient.Get(ctx, types.NamespacedName{Name: rsiPlanName, Namespace: "default"}, &fresh)).To(Succeed())
			g.Expect(fresh.Status.PlanReporterSpawnedUID).To(Equal(string(job2.UID)),
				"marker must advance to attempt 2's live-Job UID")
		}, "5s", "100ms").Should(Succeed())
	})
})
