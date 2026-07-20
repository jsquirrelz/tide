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

// task_verify_dispatch_test.go — Plan 51-06 Task 1 (verifierInFlightCount,
// plain-Go, non-vacuous under `-run 'VerifierInFlight'`) + Task 2 (the
// FORWARD half of the verifier sub-state-machine: executor exit-0 ->
// Verifying -> dispatch an independent verifier Job, envtest, genuinely
// executed under `--ginkgo.focus 'VerifierDispatch'` per the systemic
// vacuous -run finding documented in 51-01-SUMMARY.md / 51-03-SUMMARY.md —
// go test's plain `-run` flag only matches the package's sole top-level
// Ginkgo entry point (TestControllers), never the Describe/It text below).
package controller

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	tideprojectv1alpha3 "github.com/jsquirrelz/tide/api/v1alpha3"
	"github.com/jsquirrelz/tide/internal/budget"
	"github.com/jsquirrelz/tide/internal/dispatch/podjob"
	pkgdispatch "github.com/jsquirrelz/tide/pkg/dispatch"
)

// ---------- verifierInFlightCount tests (ESC-04 concurrency cap — plain Go, fake client) ----------

// makeVerifierJob creates a batchv1.Job with the tideproject.k8s/role=verifier
// + tideproject.k8s/project labels. terminal=true sets a Complete condition
// so isJobTerminal returns true. Mirrors dispatch_helpers_test.go's
// makePlannerJob shape for the verifier tier (D-10: a distinct, never
// overloaded pool). Carries a minimal valid PodSpec so it also admits
// against a REAL API server (envtest), not just the fake client the unit
// test below uses — TestVerifierInFlightCount's fake client skips this
// validation, but the Ginkgo cap-hit spec creates these via k8sClient.
func makeVerifierJob(name, ns, projectName string, terminal bool) *batchv1.Job {
	j := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
			Labels: map[string]string{
				"tideproject.k8s/role":    "verifier",
				"tideproject.k8s/project": projectName,
			},
		},
		Spec: batchv1.JobSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					RestartPolicy: corev1.RestartPolicyNever,
					Containers:    []corev1.Container{{Name: "main", Image: "busybox"}},
				},
			},
		},
	}
	if terminal {
		j.Status.Conditions = []batchv1.JobCondition{
			{Type: batchv1.JobComplete, Status: corev1.ConditionTrue},
		}
	}
	return j
}

// TestVerifierInFlightCount exercises the key behaviors of verifierInFlightCount:
// non-terminal counting, terminal exclusion, project-scoping, and the
// excludeJobName self-exclusion guard (Pitfall 7). Uses the fake client (like
// dispatch_helpers_test.go's TestPlannerInFlightCount) — no shared Ginkgo
// suite entry point, so `-run VerifierInFlight` genuinely executes this test.
func TestVerifierInFlightCount(t *testing.T) {
	cases := []struct {
		name        string
		jobs        []*batchv1.Job
		projectName string
		excludeName string
		wantCount   int
	}{
		{
			name: "two non-terminal jobs returns 2",
			jobs: []*batchv1.Job{
				makeVerifierJob("v1", "default", "proj-a", false),
				makeVerifierJob("v2", "default", "proj-a", false),
			},
			projectName: "proj-a",
			wantCount:   2,
		},
		{
			name: "one non-terminal and one terminal returns 1",
			jobs: []*batchv1.Job{
				makeVerifierJob("v1", "default", "proj-a", false),
				makeVerifierJob("v2", "default", "proj-a", true),
			},
			projectName: "proj-a",
			wantCount:   1,
		},
		{
			name:        "zero jobs returns 0",
			jobs:        nil,
			projectName: "proj-a",
			wantCount:   0,
		},
		{
			name: "project-scoped: only counts jobs for the given project",
			jobs: []*batchv1.Job{
				makeVerifierJob("v-a1", "default", "proj-a", false),
				makeVerifierJob("v-b1", "default", "proj-b", false),
			},
			projectName: "proj-a",
			wantCount:   1,
		},
		{
			name: "excludeJobName self-exclusion: the named Job is never counted",
			jobs: []*batchv1.Job{
				makeVerifierJob("v1", "default", "proj-a", false),
				makeVerifierJob("v2-self", "default", "proj-a", false),
			},
			projectName: "proj-a",
			excludeName: "v2-self",
			wantCount:   1,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var objs []client.Object
			for _, j := range tc.jobs {
				objs = append(objs, j)
			}
			c := newFakeClientForController(t, objs...)
			count, err := verifierInFlightCount(context.Background(), c, "default", tc.projectName, tc.excludeName)
			if err != nil {
				t.Fatalf("verifierInFlightCount: %v", err)
			}
			if count != tc.wantCount {
				t.Errorf("verifierInFlightCount = %d, want %d", count, tc.wantCount)
			}
		})
	}
}

// ---------- verifier dispatch envtest (Plan 06 Task 2, FORWARD half) ----------

// newVerifyDispatchTaskReconciler mirrors task_controller_test.go's
// newTaskReconciler but adds the Phase 51 fields dispatchVerifier needs
// (VerifierImage, a fresh ReservationStore + a positive ReserveEstimateCents
// so the D-05/Pitfall-6 no-leak assertions are meaningful).
func newVerifyDispatchTaskReconciler(envReader podjob.EnvelopeReader) *TaskReconciler {
	return &TaskReconciler{
		Client: mgrClient,
		Scheme: k8sClient.Scheme(),
		Deps: TaskReconcilerDeps{
			Dispatcher:     &stubDispatcher{},
			Budget:         testBudgetStore,
			Defaults:       testBudgetDefaults,
			SigningKey:     testSigningKey,
			CredproxyImage: testCredproxyImage,
			EnvReader:      envReader,
			HelmProviderDefaults: ProviderDefaults{
				Image: testSubagentImage,
			},
			VerifierImage:        "tide-langgraph-verifier:test",
			Reservations:         budget.NewReservationStore(),
			ReserveEstimateCents: 500,
		},
	}
}

// makeVerifyTask creates a Task carrying the given VerificationSpec (in
// addition to every other required TaskSpec field) and the
// tideproject.k8s/project label makeTask stamps for the resolveProject
// fast-path. Waits for cache sync before returning.
func makeVerifyTask(name, planRef, projectName string, v tideprojectv1alpha3.VerificationSpec) *tideprojectv1alpha3.Task {
	t := &tideprojectv1alpha3.Task{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "default",
			Labels:    map[string]string{"tideproject.k8s/project": projectName},
		},
		Spec: tideprojectv1alpha3.TaskSpec{
			PlanRef:             planRef,
			FilesTouched:        []string{"src/main.go"},
			DeclaredOutputPaths: []string{"artifacts/out.txt"},
			PromptPath:          "envelopes/test/children/" + name + ".json",
			Verification:        v,
		},
	}
	Expect(k8sClient.Create(context.Background(), t)).To(Succeed())
	waitForCacheSync(name, "default", &tideprojectv1alpha3.Task{})
	return t
}

// completeExecutorJob patches task to Running and marks the deterministic
// executor Job (JobComplete=True) so the next Reconcile call reaches
// handleJobCompletion via checkRunningState. reconcileWithRetry's own
// dispatch pass already created the real executor Job (via BuildJobSpec, the
// SAME path production dispatch uses) — this only Gets it and completes it,
// mirroring task_controller_test.go's own completion-simulation pattern but
// idempotent against the Job dispatch already having happened. Returns the
// attempt number the Job was dispatched under.
func completeExecutorJob(ctx context.Context, task *tideprojectv1alpha3.Task) int {
	patch := client.MergeFrom(task.DeepCopy())
	task.Status.Phase = tideprojectv1alpha3.LevelPhaseRunning
	now := metav1.Now()
	task.Status.StartedAt = &now
	ExpectWithOffset(1, k8sClient.Status().Patch(ctx, task, patch)).To(Succeed())

	attempt := task.Status.Attempt
	if attempt == 0 {
		attempt = 1
	}
	jobName := podjob.JobName(task.UID, attempt)
	var job batchv1.Job
	if err := k8sClient.Get(ctx, types.NamespacedName{Name: jobName, Namespace: "default"}, &job); apierrors.IsNotFound(err) {
		job = batchv1.Job{
			ObjectMeta: metav1.ObjectMeta{
				Name:      jobName,
				Namespace: "default",
				Labels: map[string]string{
					"tideproject.k8s/task-uid": string(task.UID),
					"tideproject.k8s/attempt":  fmt.Sprintf("%d", attempt),
				},
			},
			Spec: batchv1.JobSpec{
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						RestartPolicy: corev1.RestartPolicyNever,
						Containers:    []corev1.Container{{Name: "main", Image: "busybox"}},
					},
				},
			},
		}
		ExpectWithOffset(1, k8sClient.Create(ctx, &job)).To(Succeed())
	} else {
		ExpectWithOffset(1, err).NotTo(HaveOccurred())
	}
	jobPatch := client.MergeFrom(job.DeepCopy())
	completeJobStatus(&job)
	ExpectWithOffset(1, k8sClient.Status().Patch(ctx, &job, jobPatch)).To(Succeed())
	return attempt
}

// completeJobStatus stamps a VALID terminal (Complete) status onto job. k8s
// >=1.34 Job validation rejects a Complete=True condition unless the Job also
// carries a SuccessCriteriaMet=True condition and both startTime and
// completionTime — so patching only {Complete=True} (the pre-1.34 shortcut)
// now fails apiserver validation with "cannot set Complete=True condition
// without the SuccessCriteriaMet=true condition". isJobTerminal still reads
// this as terminal (it keys on JobComplete=True).
func completeJobStatus(job *batchv1.Job) {
	now := metav1.Now()
	job.Status.StartTime = &now
	job.Status.CompletionTime = &now
	job.Status.Conditions = []batchv1.JobCondition{
		{Type: batchv1.JobSuccessCriteriaMet, Status: corev1.ConditionTrue},
		{Type: batchv1.JobComplete, Status: corev1.ConditionTrue},
	}
}

// decodeEnvelopeIn extracts and decodes the EnvelopeIn a Job's
// envelope-writer init container was built to write (the base64-encoded
// ENVELOPE_IN_B64 env var — BuildJobSpec's own wire format).
func decodeEnvelopeIn(job *batchv1.Job) pkgdispatch.EnvelopeIn {
	var envInB64 string
	for _, c := range job.Spec.Template.Spec.InitContainers {
		if c.Name != podjob.ContainerNameEnvelopeWriter {
			continue
		}
		for _, e := range c.Env {
			if e.Name == "ENVELOPE_IN_B64" {
				envInB64 = e.Value
			}
		}
	}
	ExpectWithOffset(1, envInB64).NotTo(BeEmpty(), "envelope-writer init container must carry ENVELOPE_IN_B64")
	raw, err := base64.StdEncoding.DecodeString(envInB64)
	ExpectWithOffset(1, err).NotTo(HaveOccurred())
	var envIn pkgdispatch.EnvelopeIn
	ExpectWithOffset(1, json.Unmarshal(raw, &envIn)).To(Succeed())
	return envIn
}

var _ = Describe("Task loop: verifier dispatch (Phase 51 Plan 06, VerifierDispatch)", Label("envtest", "phase51", "verifier-dispatch"), func() {
	ctx := context.Background()

	It("dispatches an independent verifier Job with the locked contract, langgraph vendor, and lockedSHA stamped", func() {
		const projName = "vd-proj-contract"
		const planRef = "vd-plan-contract"
		const taskName = "vd-task-contract"

		makeProjectForTask(projName)
		defer cleanupProject(projName)

		// Stamp a fake LastPushedSHA so TASK-01's LockedSHA propagation is
		// observable, and wait for the reconciler's cached client to see it.
		var proj tideprojectv1alpha3.Project
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: projName, Namespace: "default"}, &proj)).To(Succeed())
		gp := client.MergeFrom(proj.DeepCopy())
		const wantLockedSHA = "deadbeefcafefeed00000000000000000000000"
		proj.Status.Git.LastPushedSHA = wantLockedSHA
		Expect(k8sClient.Status().Patch(ctx, &proj, gp)).To(Succeed())
		Eventually(func() string {
			var p tideprojectv1alpha3.Project
			if err := mgrClient.Get(ctx, types.NamespacedName{Name: projName, Namespace: "default"}, &p); err != nil {
				return ""
			}
			return p.Status.Git.LastPushedSHA
		}, 5*time.Second, 50*time.Millisecond).Should(Equal(wantLockedSHA))

		task := makeVerifyTask(taskName, planRef, projName, tideprojectv1alpha3.VerificationSpec{
			Phase:             "Locked",
			Version:           1,
			GateCommand:       "make test-verify",
			Commands:          []string{"make lint"},
			RequiredArtifacts: []string{"pkg/foo/foo.go"},
			Evaluator:         "default-evaluator",
			MaxIterations:     3,
			OnExhaustion:      "requireApproval",
		})
		defer cleanupTask(taskName)

		envReader := newMapEnvReader()
		r := newVerifyDispatchTaskReconciler(envReader)
		name := types.NamespacedName{Name: taskName, Namespace: "default"}

		Expect(reconcileWithRetry(r.Reconcile, name, 4)).To(Succeed())
		Expect(k8sClient.Get(ctx, name, task)).To(Succeed())

		envReader.SetOut(string(task.UID), pkgdispatch.EnvelopeOut{
			TaskUID:     string(task.UID),
			ExitCode:    0,
			Result:      "success",
			CompletedAt: time.Now(),
			Usage:       pkgdispatch.Usage{InputTokens: 100, OutputTokens: 50, EstimatedCostCents: 10},
		})

		attempt := completeExecutorJob(ctx, task)

		_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: name})
		Expect(err).NotTo(HaveOccurred())

		Expect(k8sClient.Get(ctx, name, task)).To(Succeed())
		Expect(task.Status.Phase).To(Equal(tideprojectv1alpha3.LevelPhaseVerifying),
			"a contract-bearing Task must transition to Verifying, never Succeeded, on executor exit-0 (EXEC-04)")
		Expect(task.Status.LockedSHA).To(Equal(wantLockedSHA))

		verifierJobName := podjob.VerifierJobName("task", string(task.UID), attempt)
		var verifierJob batchv1.Job
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: verifierJobName, Namespace: "default"}, &verifierJob)).To(Succeed())
		Expect(verifierJob.Labels["tideproject.k8s/role"]).To(Equal("verifier"))
		Expect(verifierJob.Labels["tideproject.k8s/project"]).To(Equal(projName))
		Expect(verifierJob.Labels["tideproject.k8s/task-uid"]).To(Equal(string(task.UID)))

		envIn := decodeEnvelopeIn(&verifierJob)
		Expect(envIn.Role).To(Equal("verifier"))
		Expect(envIn.Provider.Vendor).To(Equal("langgraph"))
		Expect(envIn.Verify).NotTo(BeNil())
		Expect(envIn.Verify.GateCommand).To(Equal("make test-verify"))
		Expect(envIn.Verify.Commands).To(Equal([]string{"make test-verify", "make lint"}),
			"Commands must be the resolved ordered union [gateCommand]++spec.Commands")
		Expect(envIn.Verify.RequiredArtifacts).To(Equal([]string{"pkg/foo/foo.go"}))
		Expect(envIn.Verify.EvaluatorRef).To(Equal("default-evaluator"))
		Expect(envIn.Prompt).To(ContainSubstring("make test-verify"),
			"the prompt must be rendered controller-side via task_verifier.tmpl")
		// ME-02/EVAL-04: the "Original task instruction" section must carry the
		// executor's task-intent path (not the empty self-referential envIn.Prompt),
		// so the verifier judges the candidate against the task intent.
		Expect(task.Spec.PromptPath).NotTo(BeEmpty())
		Expect(envIn.Prompt).To(ContainSubstring(task.Spec.PromptPath),
			"the rendered verifier prompt must carry the executor's original task-instruction path")
	})

	It("preserves the legacy exit-0 -> Succeeded path for a Task with no verification contract (OQ2)", func() {
		const projName = "vd-proj-nocontract"
		const planRef = "vd-plan-nocontract"
		const taskName = "vd-task-nocontract"

		makeProjectForTask(projName)
		defer cleanupProject(projName)

		task := makeVerifyTask(taskName, planRef, projName, tideprojectv1alpha3.VerificationSpec{})
		defer cleanupTask(taskName)

		envReader := newMapEnvReader()
		r := newVerifyDispatchTaskReconciler(envReader)
		name := types.NamespacedName{Name: taskName, Namespace: "default"}

		Expect(reconcileWithRetry(r.Reconcile, name, 4)).To(Succeed())
		Expect(k8sClient.Get(ctx, name, task)).To(Succeed())

		envReader.SetOut(string(task.UID), pkgdispatch.EnvelopeOut{
			TaskUID:     string(task.UID),
			ExitCode:    0,
			Result:      "success",
			CompletedAt: time.Now(),
		})

		attempt := completeExecutorJob(ctx, task)

		_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: name})
		Expect(err).NotTo(HaveOccurred())

		Expect(k8sClient.Get(ctx, name, task)).To(Succeed())
		Expect(task.Status.Phase).To(Equal(tideprojectv1alpha3.LevelPhaseSucceeded),
			"a contract-less Task must keep the pre-Phase-51 exit-0 -> Succeeded path")

		verifierJobName := podjob.VerifierJobName("task", string(task.UID), attempt)
		var verifierJob batchv1.Job
		err = k8sClient.Get(ctx, types.NamespacedName{Name: verifierJobName, Namespace: "default"}, &verifierJob)
		Expect(apierrors.IsNotFound(err)).To(BeTrue(), "no verifier Job may be created for a contract-less Task")
	})

	It("leaves a contract-bearing Task parked in Verifying (no unschedulable Job) when VerifierImage is empty (LO-01)", func() {
		const projName = "vd-proj-noimage"
		const planRef = "vd-plan-noimage"
		const taskName = "vd-task-noimage"

		makeProjectForTask(projName)
		defer cleanupProject(projName)

		task := makeVerifyTask(taskName, planRef, projName, tideprojectv1alpha3.VerificationSpec{
			Phase: "Locked", Version: 1, GateCommand: "make test-verify",
		})
		defer cleanupTask(taskName)

		envReader := newMapEnvReader()
		r := newVerifyDispatchTaskReconciler(envReader)
		r.Deps.VerifierImage = "" // TIDE_VERIFIER_IMAGE unset (dev cluster / no chart)
		name := types.NamespacedName{Name: taskName, Namespace: "default"}

		Expect(reconcileWithRetry(r.Reconcile, name, 4)).To(Succeed())
		Expect(k8sClient.Get(ctx, name, task)).To(Succeed())

		envReader.SetOut(string(task.UID), pkgdispatch.EnvelopeOut{
			TaskUID: string(task.UID), ExitCode: 0, Result: "success", CompletedAt: time.Now(),
		})
		attempt := completeExecutorJob(ctx, task)
		waitForJobTerminalInCache(ctx, podjob.JobName(task.UID, attempt))

		_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: name})
		Expect(err).NotTo(HaveOccurred())

		Expect(k8sClient.Get(ctx, name, task)).To(Succeed())
		Expect(task.Status.Phase).To(Equal(tideprojectv1alpha3.LevelPhaseVerifying),
			"an empty VerifierImage must leave the Task benignly parked in Verifying, not Failed")

		verifierJobName := podjob.VerifierJobName("task", string(task.UID), attempt)
		var verifierJob batchv1.Job
		err = k8sClient.Get(ctx, types.NamespacedName{Name: verifierJobName, Namespace: "default"}, &verifierJob)
		Expect(apierrors.IsNotFound(err)).To(BeTrue(), "an empty VerifierImage must NOT create an unschedulable verifier Job")
	})

	It("defers verifier dispatch at the ESC-04 concurrency cap without leaking a reservation (Pitfall 6)", func() {
		const projName = "vd-proj-caphit"
		const planRef = "vd-plan-caphit"
		const taskName = "vd-task-caphit"

		makeProjectForTask(projName)
		defer cleanupProject(projName)

		task := makeVerifyTask(taskName, planRef, projName, tideprojectv1alpha3.VerificationSpec{
			Phase:       "Locked",
			Version:     1,
			GateCommand: "make test-verify",
		})
		defer cleanupTask(taskName)

		// Saturate the ESC-04 cap with defaultVerifierConcurrencyCap dummy
		// non-terminal verifier Jobs for this project — unrelated to
		// taskName, so verifierInFlightCount's self-exclusion never applies
		// to them.
		fillerJobs := make([]*batchv1.Job, 0, defaultVerifierConcurrencyCap)
		for i := range defaultVerifierConcurrencyCap {
			filler := makeVerifierJob(fmt.Sprintf("vd-verifier-filler-%d", i), "default", projName, false)
			Expect(k8sClient.Create(ctx, filler)).To(Succeed())
			fillerJobs = append(fillerJobs, filler)
		}
		defer func() {
			for _, j := range fillerJobs {
				_ = k8sClient.Delete(ctx, j)
			}
		}()
		// dispatchVerifier's cap check reads via r.Client (mgrClient, the
		// CACHED client) — wait for the informer cache to observe every
		// filler Job before triggering dispatch, or the cap check races the
		// cache and under-counts (flaky false-negative on the deferral).
		Eventually(func() int {
			var jobs batchv1.JobList
			if err := mgrClient.List(ctx, &jobs, client.InNamespace("default"),
				client.MatchingLabels{"tideproject.k8s/role": "verifier", "tideproject.k8s/project": projName}); err != nil {
				return -1
			}
			return len(jobs.Items)
		}, 5*time.Second, 50*time.Millisecond).Should(Equal(defaultVerifierConcurrencyCap))

		envReader := newMapEnvReader()
		r := newVerifyDispatchTaskReconciler(envReader)
		name := types.NamespacedName{Name: taskName, Namespace: "default"}

		Expect(reconcileWithRetry(r.Reconcile, name, 4)).To(Succeed())
		Expect(k8sClient.Get(ctx, name, task)).To(Succeed())

		envReader.SetOut(string(task.UID), pkgdispatch.EnvelopeOut{
			TaskUID:     string(task.UID),
			ExitCode:    0,
			Result:      "success",
			CompletedAt: time.Now(),
		})

		attempt := completeExecutorJob(ctx, task)

		result, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: name})
		Expect(err).NotTo(HaveOccurred())
		Expect(result.RequeueAfter).To(Equal(10*time.Second),
			"a cap-hit verifier dispatch must defer via requeue, never error or block")

		Expect(k8sClient.Get(ctx, name, task)).To(Succeed())
		Expect(task.Status.Phase).To(Equal(tideprojectv1alpha3.LevelPhaseVerifying))

		verifierJobName := podjob.VerifierJobName("task", string(task.UID), attempt)
		var verifierJob batchv1.Job
		err = k8sClient.Get(ctx, types.NamespacedName{Name: verifierJobName, Namespace: "default"}, &verifierJob)
		Expect(apierrors.IsNotFound(err)).To(BeTrue(), "no verifier Job may be created while the ESC-04 cap is saturated")

		Expect(r.Deps.Reservations.TotalReserved()).To(Equal(int64(0)),
			"the executor's own reservation must settle (not leak) when the verifier dispatch defers on the cap, per Pitfall 6")
	})
})
