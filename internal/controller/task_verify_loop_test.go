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

// task_verify_loop_test.go — Plan 51-07: the BACKWARD half of the verifier
// sub-state-machine Plan 06 opened. Consumes a terminal verifier Job's
// EnvelopeOut.Verdict (ClassifyVerdict fail-closed, D-06 controller-side
// dominance re-check), drives the three-tier decision (APPROVED ->
// Succeeded, REPAIRABLE -> repairOrHalt, BLOCKED -> haltVerify), enforces
// TASK-06 anti-gaming structurally, and proves LoopStatus re-derives after a
// simulated controller restart.
//
// Per the systemic finding documented in 51-01/51-03/51-05/51-06-SUMMARY.md:
// internal/controller's sole top-level Ginkgo entry point is TestControllers
// — a plain `go test -run 'VerifyLoop|AntiGaming|InfraRetry|Resume'` matches
// ZERO Describe/It text below and exits 0 vacuously. The Ginkgo specs in this
// file are genuinely verified via
// `go test ./internal/controller/... -v -args --ginkgo.focus '<pattern>'`
// (see 51-07-SUMMARY.md). The plain testing.T functions below (no shared
// Ginkgo suite dependency) DO genuinely execute under a plain `-run` filter.
package controller

import (
	"context"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	batchv1 "k8s.io/api/batch/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	tideprojectv1alpha3 "github.com/jsquirrelz/tide/api/v1alpha3"
	"github.com/jsquirrelz/tide/internal/dispatch/podjob"
	pkgdispatch "github.com/jsquirrelz/tide/pkg/dispatch"
)

// ---------- pure-function unit tests (genuinely executed under -run) ----------

// TestVerifyLoop_HasDeterministicFailure proves the D-06 controller-side
// dominance re-check: ONLY a Finding carrying BOTH
// Severity=="blocker" AND Dimension=="gate-command" dominates — every other
// shape (wrong dimension, wrong severity, no findings, nil verdict) does not.
func TestVerifyLoop_HasDeterministicFailure(t *testing.T) {
	cases := []struct {
		name string
		gd   *pkgdispatch.GateDecision
		want bool
	}{
		{"nil verdict", nil, false},
		{"approved, no findings", &pkgdispatch.GateDecision{Verdict: pkgdispatch.VerdictApproved}, false},
		{"approved, unrelated finding", &pkgdispatch.GateDecision{
			Verdict:  pkgdispatch.VerdictApproved,
			Findings: []pkgdispatch.Finding{{Dimension: "style", Severity: "advisory"}},
		}, false},
		{"approved, blocker but wrong dimension", &pkgdispatch.GateDecision{
			Verdict:  pkgdispatch.VerdictApproved,
			Findings: []pkgdispatch.Finding{{Dimension: "correctness", Severity: "blocker"}},
		}, false},
		{"approved, gate-command but not blocker severity", &pkgdispatch.GateDecision{
			Verdict:  pkgdispatch.VerdictApproved,
			Findings: []pkgdispatch.Finding{{Dimension: "gate-command", Severity: "advisory"}},
		}, false},
		{"approved, gate-command blocker dominates (D-06)", &pkgdispatch.GateDecision{
			Verdict:  pkgdispatch.VerdictApproved,
			Findings: []pkgdispatch.Finding{{Dimension: "gate-command", Severity: "blocker"}},
		}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := hasDeterministicFailure(tc.gd); got != tc.want {
				t.Errorf("hasDeterministicFailure(%+v) = %v, want %v", tc.gd, got, tc.want)
			}
		})
	}
}

// TestAntiGaming_IntersectsProtected proves TASK-06's structural anti-gaming
// intersection with BOTH a true-positive (an evaluator/fixture/verifier-image
// edit) and a true-negative (an ordinary source+test repair) — Pitfall 5's
// own scope guard against flagging every *_test.go edit as gaming.
func TestAntiGaming_IntersectsProtected(t *testing.T) {
	cases := []struct {
		name    string
		changed []pkgdispatch.ChangedFile
		want    bool
	}{
		{"empty changed files", nil, false},
		{"ordinary source + test file (true negative)", []pkgdispatch.ChangedFile{
			{Path: "internal/foo/bar.go", Status: "M"},
			{Path: "internal/foo/bar_test.go", Status: "M"},
		}, false},
		{"evaluator package edit (true positive)", []pkgdispatch.ChangedFile{
			{Path: "internal/eval/scorer.go", Status: "M"},
		}, true},
		{"verifier image edit (true positive)", []pkgdispatch.ChangedFile{
			{Path: "cmd/tide-langgraph-verifier/verifier/tools.py", Status: "M"},
		}, true},
		{"task_verifier.tmpl edit (true positive)", []pkgdispatch.ChangedFile{
			{Path: "internal/subagent/common/templates/task_verifier.tmpl", Status: "M"},
		}, true},
		{"mixed: one ordinary + one protected still flags", []pkgdispatch.ChangedFile{
			{Path: "internal/foo/bar.go", Status: "M"},
			{Path: "evals/thresholds.json", Status: "M"},
		}, true},
	}
	protected := protectedPathsFor(nil)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := intersectsProtected(tc.changed, protected); got != tc.want {
				t.Errorf("intersectsProtected(%v) = %v, want %v", tc.changed, got, tc.want)
			}
		})
	}
}

// TestAntiGaming_PersistedManifestRoundTrip proves BL-01's persistence seam:
// the EXECUTOR's bounded RunEvidence projects onto the api-local
// RunEvidenceSummary (runEvidenceSummaryFrom), and the repairOrHalt
// belt-and-suspenders re-detects a protected-path edit through that persisted
// manifest (intersectsProtectedRefs) — never the verifier's nil RunEvidence.
func TestAntiGaming_PersistedManifestRoundTrip(t *testing.T) {
	protected := protectedPathsFor(nil)

	t.Run("nil executor evidence projects to nil", func(t *testing.T) {
		if got := runEvidenceSummaryFrom(nil); got != nil {
			t.Errorf("runEvidenceSummaryFrom(nil) = %v, want nil", got)
		}
	})

	t.Run("protected path survives the round-trip and is re-detected", func(t *testing.T) {
		ev := &pkgdispatch.RunEvidence{
			ChangedFiles: []pkgdispatch.ChangedFile{{Path: "internal/eval/scorer.go", Status: "M"}},
			Commands:     []string{"make test-verify"},
		}
		summary := runEvidenceSummaryFrom(ev)
		if summary == nil || len(summary.ChangedFiles) != 1 || summary.ChangedFiles[0].Path != "internal/eval/scorer.go" {
			t.Fatalf("round-trip lost the manifest: %+v", summary)
		}
		if len(summary.Commands) != 1 {
			t.Errorf("Commands lost in round-trip: %+v", summary.Commands)
		}
		if !intersectsProtectedRefs(summary.ChangedFiles, protected) {
			t.Error("intersectsProtectedRefs failed to re-detect a protected-path edit through the persisted manifest")
		}
	})

	t.Run("ordinary paths are not flagged through the persisted manifest", func(t *testing.T) {
		ev := &pkgdispatch.RunEvidence{ChangedFiles: []pkgdispatch.ChangedFile{
			{Path: "internal/foo/bar.go", Status: "M"},
			{Path: "internal/foo/bar_test.go", Status: "M"},
		}}
		summary := runEvidenceSummaryFrom(ev)
		if intersectsProtectedRefs(summary.ChangedFiles, protected) {
			t.Error("intersectsProtectedRefs wrongly flagged ordinary source+test paths as gaming")
		}
	})
}

// TestVerifyLoop_ApplyLoopStatus proves LOOP-03's current-iteration-only
// contract: Iteration mirrors Status.Attempt, LastEvaluation is populated
// from a real Verdict (nil-safe on a degraded envelope), and ExitReason
// stays empty for a mid-loop (still-active) call and is set exactly to the
// caller-supplied value for a terminal call.
func TestVerifyLoop_ApplyLoopStatus(t *testing.T) {
	t.Run("populates LastEvaluation and Iteration from a real verdict, ExitReason empty mid-loop", func(t *testing.T) {
		task := &tideprojectv1alpha3.Task{Status: tideprojectv1alpha3.TaskStatus{Attempt: 2}}
		out := pkgdispatch.EnvelopeOut{
			LoopRunID:   "task-uid",
			CompletedAt: time.Now(),
			Verdict: &pkgdispatch.GateDecision{
				Verdict: pkgdispatch.VerdictRepairable,
				Findings: []pkgdispatch.Finding{
					{Severity: "blocker"},
					{Severity: "advisory"},
				},
			},
		}
		applyLoopStatus(task, out, "")
		if task.Status.LoopStatus.Iteration != 2 {
			t.Errorf("Iteration = %d, want 2", task.Status.LoopStatus.Iteration)
		}
		if task.Status.LoopStatus.ExitReason != "" {
			t.Errorf("ExitReason = %q, want empty (loop still active)", task.Status.LoopStatus.ExitReason)
		}
		if task.Status.LoopStatus.LastEvaluation == nil {
			t.Fatal("LastEvaluation = nil, want populated")
		}
		if task.Status.LoopStatus.LastEvaluation.Decision != "REPAIRABLE" {
			t.Errorf("Decision = %q, want REPAIRABLE", task.Status.LoopStatus.LastEvaluation.Decision)
		}
		if task.Status.LoopStatus.LastEvaluation.FindingsCount != 2 {
			t.Errorf("FindingsCount = %d, want 2", task.Status.LoopStatus.LastEvaluation.FindingsCount)
		}
		if task.Status.LoopStatus.LastEvaluation.HighSeverityCount != 1 {
			t.Errorf("HighSeverityCount = %d, want 1", task.Status.LoopStatus.LastEvaluation.HighSeverityCount)
		}
	})

	t.Run("sets ExitReason on a terminal outcome and stays nil-safe on a nil Verdict", func(t *testing.T) {
		task := &tideprojectv1alpha3.Task{Status: tideprojectv1alpha3.TaskStatus{Attempt: 3}}
		applyLoopStatus(task, pkgdispatch.EnvelopeOut{}, tideprojectv1alpha3.ExitIterationsExhausted)
		if task.Status.LoopStatus.ExitReason != tideprojectv1alpha3.ExitIterationsExhausted {
			t.Errorf("ExitReason = %q, want %q", task.Status.LoopStatus.ExitReason, tideprojectv1alpha3.ExitIterationsExhausted)
		}
		if task.Status.LoopStatus.LastEvaluation != nil {
			t.Errorf("LastEvaluation = %+v, want nil for a verdict-less envelope", task.Status.LoopStatus.LastEvaluation)
		}
	})
}

// ---------- envtest fixtures (verified via --ginkgo.focus) ----------

// waitForJobTerminalInCache blocks until the RECONCILER's cached client
// (mgrClient) observes jobName as terminal (isJobTerminal). checkVerifyingState/
// checkRunningState read via r.Client (mgrClient, cached) while
// completeVerifierJob/completeExecutorJob write via k8sClient (direct) — an
// informer-cache-sync race identical to 51-06-SUMMARY.md's own documented
// cap-hit-test fix ("dispatchVerifier's cap check reads via the reconciler's
// cached client... without an explicit Eventually-based cache-sync wait, the
// check races the informer cache").
func waitForJobTerminalInCache(ctx context.Context, jobName string) {
	EventuallyWithOffset(1, func() bool {
		var job batchv1.Job
		if err := mgrClient.Get(ctx, types.NamespacedName{Name: jobName, Namespace: "default"}, &job); err != nil {
			return false
		}
		return isJobTerminal(&job)
	}, 5*time.Second, 50*time.Millisecond).Should(BeTrue(), "timed out waiting for the reconciler cache to observe terminal Job %s", jobName)
}

// completeVerifierJob patches the deterministic verifier Job for attempt to
// terminal (JobComplete=True) and waits for the reconciler's cache to
// observe it, so the next Reconcile call reaches handleVerifierCompletion
// via checkVerifyingState. The Job must already exist (dispatched by a prior
// reconcile via dispatchVerifier).
func completeVerifierJob(ctx context.Context, task *tideprojectv1alpha3.Task, attempt int) {
	jobName := podjob.VerifierJobName(task.UID, attempt)
	var job batchv1.Job
	ExpectWithOffset(1, k8sClient.Get(ctx, types.NamespacedName{Name: jobName, Namespace: "default"}, &job)).To(Succeed())
	jobPatch := client.MergeFrom(job.DeepCopy())
	completeJobStatus(&job)
	ExpectWithOffset(1, k8sClient.Status().Patch(ctx, &job, jobPatch)).To(Succeed())
	waitForJobTerminalInCache(ctx, jobName)
}

// driveExecutorCompletion dispatches a contract-bearing Task's executor,
// stages the given EXECUTOR EnvelopeOut (execOut — where a real executor's
// RunEvidence.ChangedFiles lands, the data the read-only verifier never
// writes), completes the executor Job, and reconciles once so
// handleJobCompletion runs. It does NOT assert the resulting phase: the caller
// decides whether the completion transitioned to Verifying (normal) or
// short-circuited to a VerifyHalted anti-gaming escalation (BL-01). execOut's
// TaskUID is stamped internally to match the created Task. Returns the
// reconciler, refreshed task, and attempt number.
func driveExecutorCompletion(ctx context.Context, envReader *mapEnvReader, taskName, planRef, projName string, v tideprojectv1alpha3.VerificationSpec, execOut pkgdispatch.EnvelopeOut) (*TaskReconciler, *tideprojectv1alpha3.Task, int) {
	task := makeVerifyTask(taskName, planRef, projName, v)
	r := newVerifyDispatchTaskReconciler(envReader)
	name := types.NamespacedName{Name: taskName, Namespace: "default"}

	ExpectWithOffset(1, reconcileWithRetry(r.Reconcile, name, 4)).To(Succeed())
	ExpectWithOffset(1, k8sClient.Get(ctx, name, task)).To(Succeed())

	execOut.TaskUID = string(task.UID)
	envReader.SetOut(string(task.UID), execOut)
	attempt := completeExecutorJob(ctx, task)
	waitForJobTerminalInCache(ctx, podjob.JobName(task.UID, attempt))

	_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: name})
	ExpectWithOffset(1, err).NotTo(HaveOccurred())
	ExpectWithOffset(1, k8sClient.Get(ctx, name, task)).To(Succeed())

	return r, task, attempt
}

// driveToVerifying dispatches a contract-bearing Task's executor, completes
// it (exit 0, clean run evidence), and reconciles once more so the Task
// transitions to Verifying and its independent verifier Job is dispatched
// (Plan 06's forward half). Returns the reconciler, the refreshed task, and
// the attempt number the verifier Job was dispatched under.
func driveToVerifying(ctx context.Context, envReader *mapEnvReader, taskName, planRef, projName string, v tideprojectv1alpha3.VerificationSpec) (*TaskReconciler, *tideprojectv1alpha3.Task, int) {
	r, task, attempt := driveExecutorCompletion(ctx, envReader, taskName, planRef, projName, v, pkgdispatch.EnvelopeOut{
		ExitCode:    0,
		Result:      "success",
		CompletedAt: time.Now(),
	})
	ExpectWithOffset(1, task.Status.Phase).To(Equal(tideprojectv1alpha3.LevelPhaseVerifying))
	return r, task, attempt
}

var _ = Describe("Task loop: verifier verdict consumption (Phase 51 Plan 07, VerifyLoop)", Label("envtest", "phase51", "verify-loop"), func() {
	ctx := context.Background()

	It("APPROVED with no dominating finding marks the Task Succeeded", func() {
		const projName, planRef, taskName = "vl-proj-approved", "vl-plan-approved", "vl-task-approved"
		makeProjectForTask(projName)
		defer cleanupProject(projName)

		envReader := newMapEnvReader()
		_, task, attempt := driveToVerifying(ctx, envReader, taskName, planRef, projName, tideprojectv1alpha3.VerificationSpec{
			Phase: "Locked", Version: 1, GateCommand: "make test-verify", MaxIterations: 3, OnExhaustion: "requireApproval",
		})
		defer cleanupTask(taskName)

		envReader.SetOut(string(task.UID), pkgdispatch.EnvelopeOut{
			TaskUID: string(task.UID), ExitCode: 0, Result: "verified", CompletedAt: time.Now(),
			Verdict: &pkgdispatch.GateDecision{Verdict: pkgdispatch.VerdictApproved, Summary: "clean pass"},
		})
		completeVerifierJob(ctx, task, attempt)

		r := newVerifyDispatchTaskReconciler(envReader)
		name := types.NamespacedName{Name: taskName, Namespace: "default"}
		_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: name})
		Expect(err).NotTo(HaveOccurred())

		Expect(k8sClient.Get(ctx, name, task)).To(Succeed())
		Expect(task.Status.Phase).To(Equal(tideprojectv1alpha3.LevelPhaseSucceeded))
		Expect(task.Status.LoopStatus.ExitReason).To(Equal(tideprojectv1alpha3.ExitApproved))
		Expect(task.Status.LoopStatus.LastEvaluation).NotTo(BeNil())
		Expect(task.Status.LoopStatus.LastEvaluation.Decision).To(Equal("APPROVED"))
	})

	It("an APPROVED verdict can NEVER pass over a red gate-command finding (D-06 dominance)", func() {
		const projName, planRef, taskName = "vl-proj-dominance", "vl-plan-dominance", "vl-task-dominance"
		makeProjectForTask(projName)
		defer cleanupProject(projName)

		envReader := newMapEnvReader()
		_, task, attempt := driveToVerifying(ctx, envReader, taskName, planRef, projName, tideprojectv1alpha3.VerificationSpec{
			Phase: "Locked", Version: 1, GateCommand: "make test-verify", MaxIterations: 3, OnExhaustion: "requireApproval",
		})
		defer cleanupTask(taskName)

		envReader.SetOut(string(task.UID), pkgdispatch.EnvelopeOut{
			TaskUID: string(task.UID), ExitCode: 0, Result: "verified", CompletedAt: time.Now(),
			Verdict: &pkgdispatch.GateDecision{
				Verdict:  pkgdispatch.VerdictApproved,
				Summary:  "LLM said approved despite a failing gate command",
				Findings: []pkgdispatch.Finding{{Dimension: "gate-command", Severity: "blocker", Evidence: "make test-verify exited 1"}},
			},
			RunEvidence: &pkgdispatch.RunEvidence{ChangedFiles: []pkgdispatch.ChangedFile{{Path: "internal/foo/bar.go", Status: "M"}}},
		})
		completeVerifierJob(ctx, task, attempt)

		r := newVerifyDispatchTaskReconciler(envReader)
		name := types.NamespacedName{Name: taskName, Namespace: "default"}
		_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: name})
		Expect(err).NotTo(HaveOccurred())

		Expect(k8sClient.Get(ctx, name, task)).To(Succeed())
		Expect(task.Status.Phase).NotTo(Equal(tideprojectv1alpha3.LevelPhaseSucceeded),
			"D-06: an APPROVED verdict must never pass over a red gate-command finding")
		Expect(task.Status.Phase).To(Equal(tideprojectv1alpha3.LevelPhaseRunning),
			"the dominance re-check routes an APPROVED-over-red-gate verdict through repairOrHalt, minting a fresh attempt")
		Expect(task.Status.Attempt).To(Equal(attempt + 1))
	})

	It("a verifier envelope that cannot be read halts fail-closed (never APPROVED)", func() {
		const projName, planRef, taskName = "vl-proj-unreadable", "vl-plan-unreadable", "vl-task-unreadable"
		makeProjectForTask(projName)
		defer cleanupProject(projName)

		envReader := newMapEnvReader()
		_, task, attempt := driveToVerifying(ctx, envReader, taskName, planRef, projName, tideprojectv1alpha3.VerificationSpec{
			Phase: "Locked", Version: 1, GateCommand: "make test-verify", MaxIterations: 3, OnExhaustion: "requireApproval",
		})
		defer cleanupTask(taskName)

		envReader.SetErr(string(task.UID), context.DeadlineExceeded)
		completeVerifierJob(ctx, task, attempt)

		r := newVerifyDispatchTaskReconciler(envReader)
		name := types.NamespacedName{Name: taskName, Namespace: "default"}
		_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: name})
		Expect(err).NotTo(HaveOccurred())

		Expect(k8sClient.Get(ctx, name, task)).To(Succeed())
		Expect(task.Status.Phase).To(Equal(tideprojectv1alpha3.LevelPhaseVerifyHalted),
			"an unreadable verifier envelope must never resolve to Succeeded")

		var project tideprojectv1alpha3.Project
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: projName, Namespace: "default"}, &project)).To(Succeed())
		Expect(meta.IsStatusConditionTrue(project.Status.Conditions, tideprojectv1alpha3.ConditionVerifyHalt)).To(BeTrue(),
			"ESC-02/ESC-03: an unreadable verifier envelope must stamp ConditionVerifyHalt project-wide")
	})

	It("a BLOCKED verdict halts the Task and stamps ConditionVerifyHalt", func() {
		const projName, planRef, taskName = "vl-proj-blocked", "vl-plan-blocked", "vl-task-blocked"
		makeProjectForTask(projName)
		defer cleanupProject(projName)

		envReader := newMapEnvReader()
		_, task, attempt := driveToVerifying(ctx, envReader, taskName, planRef, projName, tideprojectv1alpha3.VerificationSpec{
			Phase: "Locked", Version: 1, GateCommand: "make test-verify", MaxIterations: 3, OnExhaustion: "requireApproval",
		})
		defer cleanupTask(taskName)

		envReader.SetOut(string(task.UID), pkgdispatch.EnvelopeOut{
			TaskUID: string(task.UID), ExitCode: 0, Result: "verified", CompletedAt: time.Now(),
			Verdict: &pkgdispatch.GateDecision{Verdict: pkgdispatch.VerdictBlocked, Summary: "fundamentally wrong approach"},
		})
		completeVerifierJob(ctx, task, attempt)

		r := newVerifyDispatchTaskReconciler(envReader)
		name := types.NamespacedName{Name: taskName, Namespace: "default"}
		_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: name})
		Expect(err).NotTo(HaveOccurred())

		Expect(k8sClient.Get(ctx, name, task)).To(Succeed())
		Expect(task.Status.Phase).To(Equal(tideprojectv1alpha3.LevelPhaseVerifyHalted))
		Expect(task.Status.LoopStatus.ExitReason).To(Equal(tideprojectv1alpha3.ExitEscalated))

		var project tideprojectv1alpha3.Project
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: projName, Namespace: "default"}, &project)).To(Succeed())
		Expect(meta.IsStatusConditionTrue(project.Status.Conditions, tideprojectv1alpha3.ConditionVerifyHalt)).To(BeTrue())
	})

	It("a REPAIRABLE verdict mints a fresh attempt seeded with the locked spec + a staged evidence packet (TASK-02)", func() {
		const projName, planRef, taskName = "vl-proj-repair", "vl-plan-repair", "vl-task-repair"
		makeProjectForTask(projName)
		defer cleanupProject(projName)

		envReader := newMapEnvReader()
		_, task, attempt := driveToVerifying(ctx, envReader, taskName, planRef, projName, tideprojectv1alpha3.VerificationSpec{
			Phase: "Locked", Version: 1, GateCommand: "make test-verify", MaxIterations: 3, OnExhaustion: "requireApproval",
		})
		defer cleanupTask(taskName)

		envReader.SetOut(string(task.UID), pkgdispatch.EnvelopeOut{
			TaskUID: string(task.UID), ExitCode: 0, Result: "verified", CompletedAt: time.Now(),
			Verdict: &pkgdispatch.GateDecision{
				Verdict:  pkgdispatch.VerdictRepairable,
				Summary:  "one test still fails",
				Findings: []pkgdispatch.Finding{{Dimension: "correctness", Severity: "advisory", Evidence: "TestFoo failed"}},
			},
			RunEvidence: &pkgdispatch.RunEvidence{ChangedFiles: []pkgdispatch.ChangedFile{{Path: "internal/foo/bar.go", Status: "M"}}},
		})
		completeVerifierJob(ctx, task, attempt)

		r := newVerifyDispatchTaskReconciler(envReader)
		name := types.NamespacedName{Name: taskName, Namespace: "default"}
		_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: name})
		Expect(err).NotTo(HaveOccurred())

		Expect(k8sClient.Get(ctx, name, task)).To(Succeed())
		Expect(task.Status.Phase).To(Equal(tideprojectv1alpha3.LevelPhaseRunning))
		Expect(task.Status.Attempt).To(Equal(attempt + 1))
		Expect(task.Status.LoopStatus.ExitReason).To(BeEmpty(), "the loop is still active — ExitReason must stay empty")
		Expect(task.Status.LoopStatus.Iteration).To(Equal(int32(attempt)),
			"LastEvaluation/Iteration must summarize the attempt that was JUST verified, never the fresh attempt about to dispatch")

		freshJobName := podjob.JobName(task.UID, attempt+1)
		var freshJob batchv1.Job
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: freshJobName, Namespace: "default"}, &freshJob)).To(Succeed())
		envIn := decodeEnvelopeIn(&freshJob)
		Expect(envIn.PromptPath).To(Equal(task.Spec.PromptPath), "the fresh attempt must be seeded with the ORIGINAL locked spec, unchanged")
		Expect(envIn.Verify).NotTo(BeNil())
		Expect(envIn.Verify.EvidencePacketPath).NotTo(BeEmpty(), "TASK-02: a repair attempt must carry a staged evidence packet reference")
		Expect(envIn.Verify.GateCommand).To(BeEmpty(), "an executor envelope's VerifyContext carries ONLY EvidencePacketPath")
	})

	It("REPAIRABLE at MaxIterations halts instead of repairing further (TASK-05 onExhaustion)", func() {
		const projName, planRef, taskName = "vl-proj-exhausted", "vl-plan-exhausted", "vl-task-exhausted"
		makeProjectForTask(projName)
		defer cleanupProject(projName)

		envReader := newMapEnvReader()
		_, task, attempt := driveToVerifying(ctx, envReader, taskName, planRef, projName, tideprojectv1alpha3.VerificationSpec{
			Phase: "Locked", Version: 1, GateCommand: "make test-verify", MaxIterations: 1, OnExhaustion: "requireApproval",
		})
		defer cleanupTask(taskName)

		envReader.SetOut(string(task.UID), pkgdispatch.EnvelopeOut{
			TaskUID: string(task.UID), ExitCode: 0, Result: "verified", CompletedAt: time.Now(),
			Verdict: &pkgdispatch.GateDecision{
				Verdict:  pkgdispatch.VerdictRepairable,
				Findings: []pkgdispatch.Finding{{Dimension: "correctness", Severity: "advisory"}},
			},
		})
		completeVerifierJob(ctx, task, attempt)

		r := newVerifyDispatchTaskReconciler(envReader)
		name := types.NamespacedName{Name: taskName, Namespace: "default"}
		_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: name})
		Expect(err).NotTo(HaveOccurred())

		Expect(k8sClient.Get(ctx, name, task)).To(Succeed())
		Expect(task.Status.Phase).To(Equal(tideprojectv1alpha3.LevelPhaseVerifyHalted))
		Expect(task.Status.Attempt).To(Equal(attempt), "an exhausted loop must never mint a further attempt")
		Expect(task.Status.LoopStatus.ExitReason).To(Equal(tideprojectv1alpha3.ExitIterationsExhausted))

		freshJobName := podjob.JobName(task.UID, attempt+1)
		var freshJob batchv1.Job
		err = k8sClient.Get(ctx, types.NamespacedName{Name: freshJobName, Namespace: "default"}, &freshJob)
		Expect(apierrors.IsNotFound(err)).To(BeTrue(), "no further executor Job may dispatch once MaxIterations is exhausted")
	})
})

var _ = Describe("Task loop: anti-gaming structural enforcement (Phase 51 Plan 07, AntiGaming)", Label("envtest", "phase51", "anti-gaming"), func() {
	ctx := context.Background()

	// BL-01: the anti-gaming escalation fires at EXECUTOR completion, before a
	// verifier is ever dispatched — WHERE the executor's own
	// RunEvidence.ChangedFiles is present (the read-only verifier never writes
	// it). This is the currently-untested-and-dangerous case: a *successful*
	// gaming attempt (one whose weakened gate would return APPROVED) is caught
	// too, because escalation happens BEFORE the verifier can bless it.
	It("an executor attempt editing a protected evaluator/fixture path escalates BEFORE the verifier runs (true positive, APPROVED-path coverage)", func() {
		const projName, planRef, taskName = "ag-proj-positive", "ag-plan-positive", "ag-task-positive"
		makeProjectForTask(projName)
		defer cleanupProject(projName)

		envReader := newMapEnvReader()
		_, task, attempt := driveExecutorCompletion(ctx, envReader, taskName, planRef, projName,
			tideprojectv1alpha3.VerificationSpec{
				Phase: "Locked", Version: 1, GateCommand: "make test-verify", MaxIterations: 3, OnExhaustion: "requireApproval",
			},
			pkgdispatch.EnvelopeOut{
				ExitCode: 0, Result: "success", CompletedAt: time.Now(),
				RunEvidence: &pkgdispatch.RunEvidence{ChangedFiles: []pkgdispatch.ChangedFile{
					{Path: "internal/eval/scorer.go", Status: "M"},
				}},
			})
		defer cleanupTask(taskName)

		Expect(task.Status.Phase).To(Equal(tideprojectv1alpha3.LevelPhaseVerifyHalted),
			"an evaluator-path edit must escalate at executor completion, never reach Verifying/Succeeded")
		Expect(task.Status.Attempt).To(Equal(attempt), "an anti-gaming escalation must never mint a fresh attempt")
		Expect(task.Status.LoopStatus.ExitReason).To(Equal(tideprojectv1alpha3.ExitEscalated))

		// No verifier Job was ever dispatched — the escalation fired first, so
		// no verifier could return APPROVED and bless the gaming attempt.
		verifierJobName := podjob.VerifierJobName(task.UID, attempt)
		var vJob batchv1.Job
		err := k8sClient.Get(ctx, types.NamespacedName{Name: verifierJobName, Namespace: "default"}, &vJob)
		Expect(apierrors.IsNotFound(err)).To(BeTrue(), "a gaming attempt must never dispatch a verifier that could bless it")

		found := false
		for _, c := range task.Status.Conditions {
			if c.Type == tideprojectv1alpha3.ConditionFailed && c.Reason == "AntiGamingDetected" {
				found = true
			}
		}
		Expect(found).To(BeTrue(), "the condition Reason must be grep-distinguishable as AntiGamingDetected")
	})

	It("an ordinary executor code+test change (non-protected paths) is NOT flagged as gaming; it proceeds to verify (true negative)", func() {
		const projName, planRef, taskName = "ag-proj-negative", "ag-plan-negative", "ag-task-negative"
		makeProjectForTask(projName)
		defer cleanupProject(projName)

		envReader := newMapEnvReader()
		_, task, _ := driveExecutorCompletion(ctx, envReader, taskName, planRef, projName,
			tideprojectv1alpha3.VerificationSpec{
				Phase: "Locked", Version: 1, GateCommand: "make test-verify", MaxIterations: 3, OnExhaustion: "requireApproval",
			},
			pkgdispatch.EnvelopeOut{
				ExitCode: 0, Result: "success", CompletedAt: time.Now(),
				RunEvidence: &pkgdispatch.RunEvidence{ChangedFiles: []pkgdispatch.ChangedFile{
					{Path: "internal/foo/bar.go", Status: "M"},
					{Path: "internal/foo/bar_test.go", Status: "M"},
				}},
			})
		defer cleanupTask(taskName)

		Expect(task.Status.Phase).To(Equal(tideprojectv1alpha3.LevelPhaseVerifying),
			"an ordinary source+test change must proceed to verification, not escalate")

		found := false
		for _, c := range task.Status.Conditions {
			if c.Reason == "AntiGamingDetected" {
				found = true
			}
		}
		Expect(found).To(BeFalse(), "an ordinary change must never carry the AntiGamingDetected reason")

		// BL-01/LO-02: the executor's manifest is persisted for the evidence
		// packet + the repairOrHalt belt-and-suspenders (never the verifier's
		// nil RunEvidence).
		Expect(task.Status.LastAttemptEvidence).NotTo(BeNil())
		Expect(task.Status.LastAttemptEvidence.ChangedFiles).To(HaveLen(2))
	})
})

var _ = Describe("Task loop: verify-exhaustion is a distinct halt class (Phase 51 ESC-03, VerifyHaltClass)", Label("envtest", "phase51", "esc03"), func() {
	ctx := context.Background()

	// HI-01 end-to-end: drives the FULL haltVerify flow under a conservative
	// FailureProfile and proves ESC-03 — a verify-exhaustion halts as the
	// distinct LevelPhaseVerifyHalted terminal, stamps the project-wide
	// ConditionVerifyHalt, and NEVER trips the conservative ConditionFailureHalt
	// (the exact over-halt the old Phase=Failed verify-halt caused on the next
	// reconcile). The prior coverage was helper-only (co_occurring_holds_test);
	// this exercises the reconcile path that stamped the halt.
	It("halts VerifyHalted, stamps VerifyHalt, and never stamps conservative FailureHalt", func() {
		const projName, planRef, taskName, sibName = "esc3-proj", "esc3-plan", "esc3-task", "esc3-sibling"

		proj := &tideprojectv1alpha3.Project{
			ObjectMeta: metav1.ObjectMeta{Name: projName, Namespace: "default"},
			Spec: tideprojectv1alpha3.ProjectSpec{
				SchemaRevision: "v1alpha3",
				TargetRepo:     "https://github.com/example/tide.git",
				FailureProfile: tideprojectv1alpha3.FailureProfileConservative,
			},
		}
		Expect(k8sClient.Create(ctx, proj)).To(Succeed())
		waitForCacheSync(projName, "default", &tideprojectv1alpha3.Project{})
		defer cleanupProject(projName)

		// A wave sibling: ESC-03 requires a VerifyHalt to leave sibling phases untouched.
		sibling := makeVerifyTask(sibName, planRef, projName, tideprojectv1alpha3.VerificationSpec{})
		defer cleanupTask(sibName)

		envReader := newMapEnvReader()
		_, task, attempt := driveToVerifying(ctx, envReader, taskName, planRef, projName, tideprojectv1alpha3.VerificationSpec{
			Phase: "Locked", Version: 1, GateCommand: "make test-verify", MaxIterations: 1, OnExhaustion: "requireApproval",
		})
		defer cleanupTask(taskName)

		// REPAIRABLE at MaxIterations=1 → exhaustion → haltVerify.
		envReader.SetOut(string(task.UID), pkgdispatch.EnvelopeOut{
			TaskUID: string(task.UID), ExitCode: 0, Result: "verified", CompletedAt: time.Now(),
			Verdict: &pkgdispatch.GateDecision{
				Verdict:  pkgdispatch.VerdictRepairable,
				Findings: []pkgdispatch.Finding{{Dimension: "correctness", Severity: "advisory"}},
			},
		})
		completeVerifierJob(ctx, task, attempt)

		r := newVerifyDispatchTaskReconciler(envReader)
		name := types.NamespacedName{Name: taskName, Namespace: "default"}
		_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: name})
		Expect(err).NotTo(HaveOccurred())

		Expect(k8sClient.Get(ctx, name, task)).To(Succeed())
		Expect(task.Status.Phase).To(Equal(tideprojectv1alpha3.LevelPhaseVerifyHalted))

		var project tideprojectv1alpha3.Project
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: projName, Namespace: "default"}, &project)).To(Succeed())
		Expect(meta.IsStatusConditionTrue(project.Status.Conditions, tideprojectv1alpha3.ConditionVerifyHalt)).To(BeTrue(),
			"ESC-02: verify-exhaustion stamps the project-wide VerifyHalt")

		// Reconcile the halted Task AGAIN — under the OLD Phase=Failed behavior
		// THIS is the reconcile whose gateChecks Failed short-circuit stamped
		// the conservative ConditionFailureHalt. The VerifyHalted short-circuit
		// (Step 1a) must not.
		_, err = r.Reconcile(ctx, reconcile.Request{NamespacedName: name})
		Expect(err).NotTo(HaveOccurred())

		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: projName, Namespace: "default"}, &project)).To(Succeed())
		Expect(meta.IsStatusConditionTrue(project.Status.Conditions, tideprojectv1alpha3.ConditionFailureHalt)).To(BeFalse(),
			"ESC-03: a VerifyHalt must NEVER stamp the conservative FailureHalt — distinct halt class")

		// The wave sibling's own phase is untouched by the VerifyHalt.
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: sibName, Namespace: "default"}, sibling)).To(Succeed())
		Expect(sibling.Status.Phase).NotTo(Equal(tideprojectv1alpha3.LevelPhaseVerifyHalted))
		Expect(sibling.Status.Phase).NotTo(Equal(tideprojectv1alpha3.LevelPhaseFailed))
	})
})

var _ = Describe("Task loop: infra-retry stays distinct from quality-iteration (Phase 51 Plan 07, InfraRetry)", Label("envtest", "phase51", "infra-retry"), func() {
	ctx := context.Background()

	It("a non-terminal executor Job re-observe never touches Attempt or dispatches a repair", func() {
		const projName, planRef, taskName = "ir-proj-nonterminal", "ir-plan-nonterminal", "ir-task-nonterminal"
		makeProjectForTask(projName)
		defer cleanupProject(projName)

		task := makeVerifyTask(taskName, planRef, projName, tideprojectv1alpha3.VerificationSpec{
			Phase: "Locked", Version: 1, GateCommand: "make test-verify", MaxIterations: 3, OnExhaustion: "requireApproval",
		})
		defer cleanupTask(taskName)

		envReader := newMapEnvReader()
		r := newVerifyDispatchTaskReconciler(envReader)
		name := types.NamespacedName{Name: taskName, Namespace: "default"}
		Expect(reconcileWithRetry(r.Reconcile, name, 4)).To(Succeed())
		Expect(k8sClient.Get(ctx, name, task)).To(Succeed())
		Expect(task.Status.Phase).To(Equal(tideprojectv1alpha3.LevelPhaseRunning))
		beforeAttempt := task.Status.Attempt

		// The executor Job is dispatched but deliberately left non-terminal
		// (mirrors an in-flight infra retry: the K8s Job's own backoffLimit
		// may still be retrying failed pods under the SAME Job/attempt) —
		// checkRunningState's pre-existing Job re-read path is the ONLY
		// route reached here; it has no edge to repairOrHalt/
		// dispatchRepairAttempt, which are reachable exclusively from a
		// TERMINAL verifier Job via checkVerifyingState.
		_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: name})
		Expect(err).NotTo(HaveOccurred())

		Expect(k8sClient.Get(ctx, name, task)).To(Succeed())
		Expect(task.Status.Phase).To(Equal(tideprojectv1alpha3.LevelPhaseRunning),
			"a non-terminal Job re-observe (infra-retry path) must never mint a new attempt or dispatch a verifier")
		Expect(task.Status.Attempt).To(Equal(beforeAttempt))

		verifierJobName := podjob.VerifierJobName(task.UID, beforeAttempt)
		var verifierJob batchv1.Job
		err = k8sClient.Get(ctx, types.NamespacedName{Name: verifierJobName, Namespace: "default"}, &verifierJob)
		Expect(apierrors.IsNotFound(err)).To(BeTrue(), "no verifier Job may dispatch while the executor attempt is still non-terminal")
	})
})

var _ = Describe("Task loop: LoopStatus resume after a simulated controller restart (Phase 51 Plan 07, Resume)", Label("envtest", "phase51", "resume"), func() {
	ctx := context.Background()

	It("re-derives Attempt + LoopStatus from CRD status alone across a fresh reconciler instance", func() {
		const projName, planRef, taskName = "rs-proj-restart", "rs-plan-restart", "rs-task-restart"
		makeProjectForTask(projName)
		defer cleanupProject(projName)

		envReader := newMapEnvReader()
		_, task, attempt := driveToVerifying(ctx, envReader, taskName, planRef, projName, tideprojectv1alpha3.VerificationSpec{
			Phase: "Locked", Version: 1, GateCommand: "make test-verify", MaxIterations: 3, OnExhaustion: "requireApproval",
		})
		defer cleanupTask(taskName)

		// First cycle: REPAIRABLE -> mints attempt 2 (still on the ORIGINAL reconciler r1).
		envReader.SetOut(string(task.UID), pkgdispatch.EnvelopeOut{
			TaskUID: string(task.UID), ExitCode: 0, Result: "verified", CompletedAt: time.Now(),
			Verdict: &pkgdispatch.GateDecision{
				Verdict:  pkgdispatch.VerdictRepairable,
				Findings: []pkgdispatch.Finding{{Dimension: "correctness", Severity: "advisory"}},
			},
		})
		completeVerifierJob(ctx, task, attempt)
		r1 := newVerifyDispatchTaskReconciler(envReader)
		name := types.NamespacedName{Name: taskName, Namespace: "default"}
		_, err := r1.Reconcile(ctx, reconcile.Request{NamespacedName: name})
		Expect(err).NotTo(HaveOccurred())
		Expect(k8sClient.Get(ctx, name, task)).To(Succeed())
		Expect(task.Status.Attempt).To(Equal(attempt + 1))

		// Simulate a manager restart: a BRAND NEW reconciler + a BRAND NEW
		// in-memory ReservationStore (newVerifyDispatchTaskReconciler mints
		// its own budget.NewReservationStore() — nothing carries over from
		// r1). Only CRD-persisted state (Task.Status.Attempt, LoopStatus)
		// and re-derivable Job-label state (nextAttempt's List) survive.
		freshEnvReader := newMapEnvReader()
		r2 := newVerifyDispatchTaskReconciler(freshEnvReader)

		freshEnvReader.SetOut(string(task.UID), pkgdispatch.EnvelopeOut{
			TaskUID: string(task.UID), ExitCode: 0, Result: "success", CompletedAt: time.Now(),
		})
		attempt2 := completeExecutorJob(ctx, task)
		Expect(attempt2).To(Equal(attempt + 1))
		waitForJobTerminalInCache(ctx, podjob.JobName(task.UID, attempt2))

		_, err = r2.Reconcile(ctx, reconcile.Request{NamespacedName: name})
		Expect(err).NotTo(HaveOccurred())
		Expect(k8sClient.Get(ctx, name, task)).To(Succeed())
		Expect(task.Status.Phase).To(Equal(tideprojectv1alpha3.LevelPhaseVerifying),
			"the post-restart reconciler must correctly re-derive and dispatch the second verifier attempt")

		freshEnvReader.SetOut(string(task.UID), pkgdispatch.EnvelopeOut{
			TaskUID: string(task.UID), ExitCode: 0, Result: "verified", CompletedAt: time.Now(),
			Verdict: &pkgdispatch.GateDecision{Verdict: pkgdispatch.VerdictApproved, Summary: "clean on repair"},
		})
		completeVerifierJob(ctx, task, attempt2)

		_, err = r2.Reconcile(ctx, reconcile.Request{NamespacedName: name})
		Expect(err).NotTo(HaveOccurred())
		Expect(k8sClient.Get(ctx, name, task)).To(Succeed())
		Expect(task.Status.Phase).To(Equal(tideprojectv1alpha3.LevelPhaseSucceeded))
		Expect(task.Status.LoopStatus.Iteration).To(Equal(int32(attempt2)))
	})
})
