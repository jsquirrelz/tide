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

package podjob

import (
	"context"
	"encoding/json"
	"errors"
	"maps"
	"os"
	"path/filepath"
	"testing"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	tidev1alpha3 "github.com/jsquirrelz/tide/api/v1alpha3"
	"github.com/jsquirrelz/tide/internal/owner"
	pkgdispatch "github.com/jsquirrelz/tide/pkg/dispatch"
	pkggit "github.com/jsquirrelz/tide/pkg/git"
)

// testScheme builds a runtime.Scheme with the types needed by PodJobBackend tests.
func testScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	if err := tidev1alpha3.AddToScheme(s); err != nil {
		t.Fatalf("AddToScheme tidev1alpha3: %v", err)
	}
	if err := batchv1.AddToScheme(s); err != nil {
		t.Fatalf("AddToScheme batchv1: %v", err)
	}
	if err := corev1.AddToScheme(s); err != nil {
		t.Fatalf("AddToScheme corev1: %v", err)
	}
	return s
}

// fakeEnvReader is a test double for EnvelopeReader that returns a pre-canned result.
type fakeEnvReader struct {
	out pkgdispatch.EnvelopeOut
	err error
}

func (f *fakeEnvReader) ReadOut(_ context.Context, _, _ string) (pkgdispatch.EnvelopeOut, error) {
	return f.out, f.err
}

// testTask constructs a minimal Task for backend tests.
// Phase 04.1 P1.4: the tideproject.k8s/project label is required so
// PodJobBackend.resolveProject can use the label fast-path (the prior
// projectList.Items[0] fallback was removed). In production, PlanReconciler
// stamps this label; in tests we set it at construction time.
func testTask(ns, name string, uid types.UID) *tidev1alpha3.Task {
	return &tidev1alpha3.Task{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
			UID:       uid,
			Labels: map[string]string{
				"tideproject.k8s/project": "project-alpha",
			},
		},
		Spec: tidev1alpha3.TaskSpec{
			PlanRef:             "plan-alpha",
			FilesTouched:        []string{"foo.go"},
			DeclaredOutputPaths: []string{"out.json"},
			Caps: &tidev1alpha3.Caps{
				WallClockSeconds: 60,
			},
		},
	}
}

// testProject constructs a minimal Project for backend tests.
func testProject(ns, name string, uid types.UID) *tidev1alpha3.Project {
	return &tidev1alpha3.Project{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
			UID:       uid,
		},
		Spec: tidev1alpha3.ProjectSpec{SchemaRevision: "v1alpha3",
			TargetRepo:        "https://github.com/example/repo",
			ProviderSecretRef: "provider-secret",
		},
	}
}

func TestFilesystemEnvelopeReaderReadsProjectScopedWorkspacePath(t *testing.T) {
	root := t.TempDir()
	projectUID := "project-uid-alpha"
	taskUID := "task-uid-alpha"
	want := pkgdispatch.EnvelopeOut{
		APIVersion: pkgdispatch.APIVersionV1Alpha1,
		Kind:       pkgdispatch.KindTaskEnvelopeOut,
		TaskUID:    taskUID,
		ExitCode:   0,
		Result:     "success",
	}
	path := filepath.Join(root, projectUID, "workspace", "envelopes", taskUID, "out.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll envelope dir: %v", err)
	}
	data, err := json.Marshal(want)
	if err != nil {
		t.Fatalf("marshal EnvelopeOut: %v", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write EnvelopeOut: %v", err)
	}

	reader := &FilesystemEnvelopeReader{WorkspaceRoot: root}
	got, err := reader.ReadOut(context.Background(), projectUID, taskUID)
	if err != nil {
		t.Fatalf("ReadOut() error = %v", err)
	}
	if got.TaskUID != want.TaskUID || got.Result != want.Result || got.ExitCode != want.ExitCode {
		t.Fatalf("ReadOut() = %+v, want %+v", got, want)
	}
}

func TestPodStatusEnvelopeReaderReadsSubagentTerminationMessage(t *testing.T) {
	taskUID := "task-uid-alpha"
	want := pkgdispatch.EnvelopeOut{
		APIVersion: pkgdispatch.APIVersionV1Alpha1,
		Kind:       pkgdispatch.KindTaskEnvelopeOut,
		TaskUID:    taskUID,
		ExitCode:   0,
		Result:     "success",
	}
	data, err := json.Marshal(want)
	if err != nil {
		t.Fatalf("marshal EnvelopeOut: %v", err)
	}

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "task-pod",
			Namespace: "task-ns",
			Labels: map[string]string{
				"tideproject.k8s/task-uid": taskUID,
			},
		},
		Status: corev1.PodStatus{
			ContainerStatuses: []corev1.ContainerStatus{
				{
					Name: ContainerNameSubagent,
					State: corev1.ContainerState{
						Terminated: &corev1.ContainerStateTerminated{
							Message: string(data),
						},
					},
				},
			},
		},
	}
	reader := &PodStatusEnvelopeReader{
		Client: fake.NewClientBuilder().
			WithScheme(testScheme(t)).
			WithObjects(pod).
			Build(),
	}

	got, err := reader.ReadOut(context.Background(), "project-uid-alpha", taskUID)
	if err != nil {
		t.Fatalf("ReadOut() error = %v", err)
	}
	if got.TaskUID != want.TaskUID || got.Result != want.Result || got.ExitCode != want.ExitCode {
		t.Fatalf("ReadOut() = %#v, want %#v", got, want)
	}
}

// terminatedPod builds a pod with a terminated subagent container carrying
// message, labeled with taskUID plus any extra labels — the fixture shape
// every PodStatusEnvelopeReader test consumes.
func terminatedPod(name, taskUID, message string, extraLabels map[string]string) *corev1.Pod {
	labels := map[string]string{"tideproject.k8s/task-uid": taskUID}
	maps.Copy(labels, extraLabels)
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "task-ns", Labels: labels},
		Status: corev1.PodStatus{
			ContainerStatuses: []corev1.ContainerStatus{
				{
					Name: ContainerNameSubagent,
					State: corev1.ContainerState{
						Terminated: &corev1.ContainerStateTerminated{Message: message},
					},
				},
			},
		},
	}
}

// A contract-bearing Task terminates with BOTH an executor pod and a verifier
// pod carrying the same task-uid label. ReadOut serves executor/planner
// completion and must never return the verifier's message — before the
// role filter this was cache-order roulette (found live 2026-07-19).
func TestPodStatusEnvelopeReader_ReadOut_SkipsVerifierPods(t *testing.T) {
	taskUID := "task-uid-mixed"
	executorOut, err := json.Marshal(pkgdispatch.EnvelopeOut{
		APIVersion: pkgdispatch.APIVersionV1Alpha1,
		Kind:       pkgdispatch.KindTaskEnvelopeOut,
		TaskUID:    taskUID,
		Result:     "executor-result",
	})
	if err != nil {
		t.Fatalf("marshal executor EnvelopeOut: %v", err)
	}
	verifierStub, err := json.Marshal(pkgdispatch.TerminationStub{GateDecision: "REPAIRABLE"})
	if err != nil {
		t.Fatalf("marshal verifier TerminationStub: %v", err)
	}

	reader := &PodStatusEnvelopeReader{
		Client: fake.NewClientBuilder().
			WithScheme(testScheme(t)).
			WithObjects(
				// Verifier pod first so an unfiltered implementation that takes
				// the first match returns the wrong message deterministically.
				terminatedPod("a-verifier-pod", taskUID, string(verifierStub),
					map[string]string{"tideproject.k8s/role": "verifier"}),
				terminatedPod("b-executor-pod", taskUID, string(executorOut),
					map[string]string{"tideproject.k8s/role": "executor"}),
			).
			Build(),
	}

	got, err := reader.ReadOut(context.Background(), "project-uid", taskUID)
	if err != nil {
		t.Fatalf("ReadOut() error = %v", err)
	}
	if got.Result != "executor-result" {
		t.Fatalf("ReadOut() returned %q; must skip the verifier pod and return the executor envelope", got.Result)
	}
}

// The verifier image writes the tiny TerminationStub (D-05a) to its
// termination message — gateDecision enum string, never a full EnvelopeOut.
// ReadVerifierOut must select the verifier-role pod and graft that stub
// verdict into EnvelopeOut.Verdict; before this existed every live verify
// fail-closed as VerifierVerdictMissing (found live 2026-07-19).
func TestPodStatusEnvelopeReader_ReadVerifierOut_GraftsStubVerdict(t *testing.T) {
	taskUID := "task-uid-verify"
	executorOut, err := json.Marshal(pkgdispatch.EnvelopeOut{
		APIVersion: pkgdispatch.APIVersionV1Alpha1,
		Kind:       pkgdispatch.KindTaskEnvelopeOut,
		TaskUID:    taskUID,
		Result:     "executor-result",
	})
	if err != nil {
		t.Fatalf("marshal executor EnvelopeOut: %v", err)
	}
	verifierStub, err := json.Marshal(pkgdispatch.TerminationStub{
		ExitCode:          0,
		GateDecision:      "REPAIRABLE",
		FindingsCount:     2,
		HighSeverityCount: 1,
	})
	if err != nil {
		t.Fatalf("marshal verifier TerminationStub: %v", err)
	}

	reader := &PodStatusEnvelopeReader{
		Client: fake.NewClientBuilder().
			WithScheme(testScheme(t)).
			WithObjects(
				terminatedPod("executor-pod", taskUID, string(executorOut),
					map[string]string{"tideproject.k8s/role": "executor"}),
				terminatedPod("verifier-pod", taskUID, string(verifierStub),
					map[string]string{"tideproject.k8s/role": "verifier"}),
			).
			Build(),
	}

	got, err := reader.ReadVerifierOut(context.Background(), "project-uid", taskUID)
	if err != nil {
		t.Fatalf("ReadVerifierOut() error = %v", err)
	}
	if got.Verdict == nil {
		t.Fatalf("ReadVerifierOut() Verdict = nil; stub gateDecision must be grafted")
	}
	if got.Verdict.Verdict != pkgdispatch.VerdictRepairable {
		t.Fatalf("ReadVerifierOut() Verdict = %q, want REPAIRABLE", got.Verdict.Verdict)
	}
}

// Repair loops leave one terminated verifier pod per attempt sharing the
// task-uid label; ReadVerifierOut must return the HIGHEST attempt's verdict,
// not whichever the cache lists first.
func TestPodStatusEnvelopeReader_ReadVerifierOut_PicksHighestAttempt(t *testing.T) {
	taskUID := "task-uid-attempts"
	stub1, err := json.Marshal(pkgdispatch.TerminationStub{GateDecision: "REPAIRABLE"})
	if err != nil {
		t.Fatalf("marshal attempt-1 stub: %v", err)
	}
	stub2, err := json.Marshal(pkgdispatch.TerminationStub{GateDecision: "APPROVED"})
	if err != nil {
		t.Fatalf("marshal attempt-2 stub: %v", err)
	}

	reader := &PodStatusEnvelopeReader{
		Client: fake.NewClientBuilder().
			WithScheme(testScheme(t)).
			WithObjects(
				terminatedPod("verifier-attempt-2", taskUID, string(stub2),
					map[string]string{"tideproject.k8s/role": "verifier", owner.LabelAttempt: "2"}),
				terminatedPod("verifier-attempt-1", taskUID, string(stub1),
					map[string]string{"tideproject.k8s/role": "verifier", owner.LabelAttempt: "1"}),
			).
			Build(),
	}

	got, err := reader.ReadVerifierOut(context.Background(), "project-uid", taskUID)
	if err != nil {
		t.Fatalf("ReadVerifierOut() error = %v", err)
	}
	if got.Verdict == nil || got.Verdict.Verdict != pkgdispatch.VerdictApproved {
		t.Fatalf("ReadVerifierOut() = %#v, want the attempt-2 APPROVED verdict", got.Verdict)
	}
}

// No verifier pod at all (e.g. termination message lost to node pressure) is
// a read error — the caller's fail-closed VerifierEnvelopeUnreadable path,
// never a silent empty envelope.
func TestPodStatusEnvelopeReader_ReadVerifierOut_NoVerifierPodErrors(t *testing.T) {
	reader := &PodStatusEnvelopeReader{
		Client: fake.NewClientBuilder().WithScheme(testScheme(t)).Build(),
	}

	_, err := reader.ReadVerifierOut(context.Background(), "project-uid", "task-uid-none")
	if err == nil {
		t.Fatalf("ReadVerifierOut() with no verifier pod must error (fail-closed)")
	}
}

func TestPodJobBackend_Run_CreatesJob(t *testing.T) {
	s := testScheme(t)
	task := testTask("default", "task-alpha", "uid-alpha")
	project := testProject("default", "project-alpha", "project-uid-alpha")

	fakeClient := fake.NewClientBuilder().
		WithScheme(s).
		WithObjects(task, project).
		WithStatusSubresource(task).
		Build()

	canned := pkgdispatch.EnvelopeOut{
		APIVersion: pkgdispatch.APIVersionV1Alpha1,
		Kind:       pkgdispatch.KindTaskEnvelopeOut,
		TaskUID:    "uid-alpha",
		ExitCode:   0,
		Result:     "success",
	}
	backend := &PodJobBackend{
		Client:         fakeClient,
		Scheme:         s,
		SubagentImage:  "test-subagent:latest",
		CredproxyImage: "test-credproxy:latest",
		SigningKey:     []byte("test-signing-key"),
		EnvReader:      &fakeEnvReader{out: canned},
		PVCName:        "tide-projects",
	}

	in := pkgdispatch.EnvelopeIn{
		APIVersion: pkgdispatch.APIVersionV1Alpha1,
		Kind:       pkgdispatch.KindTaskEnvelopeIn,
		TaskUID:    "uid-alpha",
	}

	// Run with a timeout context; we'll push Job to terminal state via a goroutine.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Simulate Job completion by updating status after a short delay.
	go func() {
		time.Sleep(100 * time.Millisecond)
		var jobList batchv1.JobList
		for range 20 {
			if err := fakeClient.List(ctx, &jobList, client.InNamespace("default")); err == nil && len(jobList.Items) > 0 {
				break
			}
			time.Sleep(50 * time.Millisecond)
		}
		if len(jobList.Items) == 0 {
			return
		}
		job := jobList.Items[0].DeepCopy()
		job.Status.Conditions = []batchv1.JobCondition{
			{
				Type:   batchv1.JobComplete,
				Status: corev1.ConditionTrue,
			},
		}
		_ = fakeClient.Status().Update(ctx, job)
	}()

	out, err := backend.Run(ctx, in)
	if err != nil {
		t.Fatalf("Run() returned error: %v", err)
	}
	if out.TaskUID != "uid-alpha" {
		t.Errorf("out.TaskUID = %q; want %q", out.TaskUID, "uid-alpha")
	}

	// Verify the Job was created with the deterministic name.
	wantJobName := JobName(task.UID, 1)
	var job batchv1.Job
	if err := fakeClient.Get(ctx, client.ObjectKey{Namespace: "default", Name: wantJobName}, &job); err != nil {
		t.Errorf("Job %q not found after Run: %v", wantJobName, err)
	}
}

func TestPodJobBackend_Run_IdempotentOnAlreadyExists(t *testing.T) {
	s := testScheme(t)
	task := testTask("default", "task-beta", "uid-beta")
	// Phase 04.1 P1.4: project name must match the tideproject.k8s/project label
	// set in testTask (project-alpha) so the label fast-path resolves the project.
	project := testProject("default", "project-alpha", "project-uid-beta")

	// Pre-create the Job before Run is called — simulates AlreadyExists from watch lag.
	preExistingJob := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      JobName(task.UID, 1),
			Namespace: "default",
			Labels: map[string]string{
				"tideproject.k8s/task-uid": "uid-beta",
			},
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(s).
		WithObjects(task, project, preExistingJob).
		WithStatusSubresource(task).
		Build()

	canned := pkgdispatch.EnvelopeOut{
		APIVersion: pkgdispatch.APIVersionV1Alpha1,
		Kind:       pkgdispatch.KindTaskEnvelopeOut,
		TaskUID:    "uid-beta",
		ExitCode:   0,
	}
	backend := &PodJobBackend{
		Client:         fakeClient,
		Scheme:         s,
		SubagentImage:  "test-subagent:latest",
		CredproxyImage: "test-credproxy:latest",
		SigningKey:     []byte("test-signing-key"),
		EnvReader:      &fakeEnvReader{out: canned},
		PVCName:        "tide-projects",
	}

	in := pkgdispatch.EnvelopeIn{
		APIVersion: pkgdispatch.APIVersionV1Alpha1,
		Kind:       pkgdispatch.KindTaskEnvelopeIn,
		TaskUID:    "uid-beta",
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Push Job to Succeeded immediately via a goroutine.
	go func() {
		time.Sleep(100 * time.Millisecond)
		var jobList batchv1.JobList
		for range 20 {
			if err := fakeClient.List(ctx, &jobList, client.InNamespace("default")); err == nil && len(jobList.Items) > 0 {
				break
			}
			time.Sleep(50 * time.Millisecond)
		}
		if len(jobList.Items) == 0 {
			return
		}
		job := jobList.Items[0].DeepCopy()
		job.Status.Conditions = []batchv1.JobCondition{
			{Type: batchv1.JobComplete, Status: corev1.ConditionTrue},
		}
		_ = fakeClient.Status().Update(ctx, job)
	}()

	_, err := backend.Run(ctx, in)
	if err != nil {
		t.Fatalf("Run() returned error on AlreadyExists: %v", err)
	}

	// Verify only one Job exists (pre-created one, not a duplicate).
	var jobList batchv1.JobList
	if err := fakeClient.List(ctx, &jobList, client.InNamespace("default")); err != nil {
		t.Fatalf("List jobs: %v", err)
	}
	if len(jobList.Items) != 1 {
		t.Errorf("expected 1 Job (idempotent), got %d", len(jobList.Items))
	}
}

func TestPodJobBackend_Run_PropagatesEnvelopeReadError(t *testing.T) {
	s := testScheme(t)
	task := testTask("default", "task-gamma", "uid-gamma")
	// Phase 04.1 P1.4: project name must match the tideproject.k8s/project label
	// set in testTask (project-alpha) so the label fast-path resolves the project.
	project := testProject("default", "project-alpha", "project-uid-gamma")

	fakeClient := fake.NewClientBuilder().
		WithScheme(s).
		WithObjects(task, project).
		WithStatusSubresource(task).
		Build()

	readErr := errors.New("envelope read failed: file not found")
	backend := &PodJobBackend{
		Client:         fakeClient,
		Scheme:         s,
		SubagentImage:  "test-subagent:latest",
		CredproxyImage: "test-credproxy:latest",
		SigningKey:     []byte("test-signing-key"),
		EnvReader:      &fakeEnvReader{err: readErr},
		PVCName:        "tide-projects",
	}

	in := pkgdispatch.EnvelopeIn{
		APIVersion: pkgdispatch.APIVersionV1Alpha1,
		Kind:       pkgdispatch.KindTaskEnvelopeIn,
		TaskUID:    "uid-gamma",
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Push Job to Succeeded so Run advances past the watch loop.
	go func() {
		time.Sleep(100 * time.Millisecond)
		var jobList batchv1.JobList
		for range 20 {
			if err := fakeClient.List(ctx, &jobList, client.InNamespace("default")); err == nil && len(jobList.Items) > 0 {
				break
			}
			time.Sleep(50 * time.Millisecond)
		}
		if len(jobList.Items) == 0 {
			return
		}
		job := jobList.Items[0].DeepCopy()
		job.Status.Conditions = []batchv1.JobCondition{
			{Type: batchv1.JobComplete, Status: corev1.ConditionTrue},
		}
		_ = fakeClient.Status().Update(ctx, job)
	}()

	_, err := backend.Run(ctx, in)
	if err == nil {
		t.Fatal("Run() should have returned an error when EnvReader fails")
	}
	if !errors.Is(err, readErr) {
		t.Errorf("Run() error = %v; want to wrap %v", err, readErr)
	}
}

func TestPodJobBackend_Run_OwnerRefCascades_Task(t *testing.T) {
	s := testScheme(t)
	task := testTask("default", "task-delta", "uid-delta")
	// Phase 04.1 P1.4: project name must match the tideproject.k8s/project label
	// set in testTask (project-alpha) so the label fast-path resolves the project.
	project := testProject("default", "project-alpha", "project-uid-delta")

	fakeClient := fake.NewClientBuilder().
		WithScheme(s).
		WithObjects(task, project).
		WithStatusSubresource(task).
		Build()

	canned := pkgdispatch.EnvelopeOut{
		APIVersion: pkgdispatch.APIVersionV1Alpha1,
		Kind:       pkgdispatch.KindTaskEnvelopeOut,
		TaskUID:    "uid-delta",
		ExitCode:   0,
	}
	backend := &PodJobBackend{
		Client:         fakeClient,
		Scheme:         s,
		SubagentImage:  "test-subagent:latest",
		CredproxyImage: "test-credproxy:latest",
		SigningKey:     []byte("test-signing-key"),
		EnvReader:      &fakeEnvReader{out: canned},
		PVCName:        "tide-projects",
	}

	in := pkgdispatch.EnvelopeIn{
		APIVersion: pkgdispatch.APIVersionV1Alpha1,
		Kind:       pkgdispatch.KindTaskEnvelopeIn,
		TaskUID:    "uid-delta",
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Push Job to terminal state.
	go func() {
		time.Sleep(100 * time.Millisecond)
		var jobList batchv1.JobList
		for range 20 {
			if err := fakeClient.List(ctx, &jobList, client.InNamespace("default")); err == nil && len(jobList.Items) > 0 {
				break
			}
			time.Sleep(50 * time.Millisecond)
		}
		if len(jobList.Items) == 0 {
			return
		}
		job := jobList.Items[0].DeepCopy()
		job.Status.Conditions = []batchv1.JobCondition{
			{Type: batchv1.JobComplete, Status: corev1.ConditionTrue},
		}
		_ = fakeClient.Status().Update(ctx, job)
	}()

	_, err := backend.Run(ctx, in)
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}

	// Verify Job has owner reference to Task.
	wantJobName := JobName(task.UID, 1)
	var job batchv1.Job
	if err := fakeClient.Get(ctx, client.ObjectKey{Namespace: "default", Name: wantJobName}, &job); err != nil {
		t.Fatalf("Get job: %v", err)
	}

	var found bool
	for _, ref := range job.OwnerReferences {
		if ref.UID == task.UID {
			found = true
			if ref.Controller == nil || !*ref.Controller {
				t.Errorf("OwnerReference.Controller = false; want true")
			}
			if ref.BlockOwnerDeletion == nil || !*ref.BlockOwnerDeletion {
				t.Errorf("OwnerReference.BlockOwnerDeletion = false; want true")
			}
		}
	}
	if !found {
		t.Errorf("Job %q has no OwnerReference to Task UID %q", wantJobName, task.UID)
	}
}

// TestPodJobBackend_Run_AgentIdentityPrecedence covers the SIGN-01 (D-03) inline
// identity precedence mirror in Run: a Project's Spec.Git.AgentName/AgentEmail beats
// the backend field, which beats the compiled default. Asserted via the rendered
// subagent-container env on the created Job (the same chain resolveAgentIdentity walks
// controller-side; podjob mirrors it inline to avoid an import cycle).
func TestPodJobBackend_Run_AgentIdentityPrecedence(t *testing.T) {
	// runAndReadIdentityEnv drives Run to completion and returns the
	// TIDE_AGENT_NAME/TIDE_AGENT_EMAIL env values on the created Job's subagent.
	runAndReadIdentityEnv := func(t *testing.T, project *tidev1alpha3.Project, backendName, backendEmail string) (string, string) {
		t.Helper()
		s := testScheme(t)
		task := testTask("default", "task-id", "uid-id")

		fakeClient := fake.NewClientBuilder().
			WithScheme(s).
			WithObjects(task, project).
			WithStatusSubresource(task).
			Build()

		backend := &PodJobBackend{
			Client:         fakeClient,
			Scheme:         s,
			SubagentImage:  "test-subagent:latest",
			CredproxyImage: "test-credproxy:latest",
			SigningKey:     []byte("test-signing-key"),
			EnvReader:      &fakeEnvReader{out: pkgdispatch.EnvelopeOut{TaskUID: "uid-id", ExitCode: 0}},
			PVCName:        "tide-projects",
			AgentName:      backendName,
			AgentEmail:     backendEmail,
		}

		in := pkgdispatch.EnvelopeIn{
			APIVersion: pkgdispatch.APIVersionV1Alpha1,
			Kind:       pkgdispatch.KindTaskEnvelopeIn,
			TaskUID:    "uid-id",
		}

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		go func() {
			time.Sleep(100 * time.Millisecond)
			var jobList batchv1.JobList
			for range 20 {
				if err := fakeClient.List(ctx, &jobList, client.InNamespace("default")); err == nil && len(jobList.Items) > 0 {
					break
				}
				time.Sleep(50 * time.Millisecond)
			}
			if len(jobList.Items) == 0 {
				return
			}
			job := jobList.Items[0].DeepCopy()
			job.Status.Conditions = []batchv1.JobCondition{
				{Type: batchv1.JobComplete, Status: corev1.ConditionTrue},
			}
			_ = fakeClient.Status().Update(ctx, job)
		}()

		if _, err := backend.Run(ctx, in); err != nil {
			t.Fatalf("Run() error: %v", err)
		}

		var job batchv1.Job
		if err := fakeClient.Get(ctx, client.ObjectKey{Namespace: "default", Name: JobName(task.UID, 1)}, &job); err != nil {
			t.Fatalf("Get job: %v", err)
		}
		env := map[string]string{}
		for _, e := range job.Spec.Template.Spec.Containers[0].Env {
			env[e.Name] = e.Value
		}
		return env[pkggit.EnvAgentName], env[pkggit.EnvAgentEmail]
	}

	t.Run("backend field beats compiled default", func(t *testing.T) {
		project := testProject("default", "project-alpha", "project-uid-id")
		name, email := runAndReadIdentityEnv(t, project, "Backend Agent", "backend@example.com")
		if name != "Backend Agent" {
			t.Errorf("name = %q; want %q", name, "Backend Agent")
		}
		if email != "backend@example.com" {
			t.Errorf("email = %q; want %q", email, "backend@example.com")
		}
	})

	t.Run("project Spec.Git beats backend field", func(t *testing.T) {
		project := testProject("default", "project-alpha", "project-uid-id")
		project.Spec.Git = &tidev1alpha3.GitConfig{
			AgentName:  "Project Agent",
			AgentEmail: "project@example.com",
		}
		name, email := runAndReadIdentityEnv(t, project, "Backend Agent", "backend@example.com")
		if name != "Project Agent" {
			t.Errorf("name = %q; want %q", name, "Project Agent")
		}
		if email != "project@example.com" {
			t.Errorf("email = %q; want %q", email, "project@example.com")
		}
	})

	t.Run("compiled default when nothing set", func(t *testing.T) {
		project := testProject("default", "project-alpha", "project-uid-id")
		name, email := runAndReadIdentityEnv(t, project, "", "")
		if name != pkggit.DefaultAgentName {
			t.Errorf("name = %q; want %q", name, pkggit.DefaultAgentName)
		}
		if email != pkggit.DefaultAgentEmail {
			t.Errorf("email = %q; want %q", email, pkggit.DefaultAgentEmail)
		}
	})
}

// TestFilesystemEnvelopeReaderReadPrompt covers defect #10b: the prompt is read
// fresh from the children/<name>.json PVC artifact at a workspace-relative
// PromptPath, with path-traversal defense and an empty-prompt hard error.
func TestFilesystemEnvelopeReaderReadPrompt(t *testing.T) {
	root := t.TempDir()
	projectUID := "project-uid-beta"
	plannerUID := "planner-uid-beta"
	promptPath := filepath.Join("envelopes", plannerUID, "children", "task-01.json")

	full := filepath.Join(root, projectUID, "workspace", promptPath)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("MkdirAll children dir: %v", err)
	}
	body := `{"kind":"Task","name":"task-01-foo","spec":{"planRef":"plan-01","prompt":"implement FormattedNow"}}`
	if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
		t.Fatalf("write child: %v", err)
	}

	reader := &FilesystemEnvelopeReader{WorkspaceRoot: root}

	t.Run("happy path returns spec.prompt", func(t *testing.T) {
		got, err := reader.ReadPrompt(context.Background(), projectUID, promptPath)
		if err != nil {
			t.Fatalf("ReadPrompt() error = %v", err)
		}
		if got != "implement FormattedNow" {
			t.Fatalf("ReadPrompt() = %q, want %q", got, "implement FormattedNow")
		}
	})

	t.Run("absolute path rejected", func(t *testing.T) {
		if _, err := reader.ReadPrompt(context.Background(), projectUID, "/etc/passwd"); err == nil {
			t.Fatal("ReadPrompt() with absolute path: want error, got nil")
		}
	})

	t.Run("traversal escape rejected", func(t *testing.T) {
		if _, err := reader.ReadPrompt(context.Background(), projectUID, "../../../../etc/passwd"); err == nil {
			t.Fatal("ReadPrompt() with traversal: want error, got nil")
		}
	})

	t.Run("empty promptPath rejected", func(t *testing.T) {
		if _, err := reader.ReadPrompt(context.Background(), projectUID, ""); err == nil {
			t.Fatal("ReadPrompt() with empty path: want error, got nil")
		}
	})

	t.Run("empty spec.prompt is a hard error", func(t *testing.T) {
		emptyPath := filepath.Join("envelopes", plannerUID, "children", "task-empty.json")
		ef := filepath.Join(root, projectUID, "workspace", emptyPath)
		if err := os.WriteFile(ef, []byte(`{"kind":"Task","name":"t","spec":{"prompt":""}}`), 0o644); err != nil {
			t.Fatalf("write empty child: %v", err)
		}
		if _, err := reader.ReadPrompt(context.Background(), projectUID, emptyPath); err == nil {
			t.Fatal("ReadPrompt() with empty spec.prompt: want error, got nil")
		}
	})

	t.Run("missing file errors", func(t *testing.T) {
		miss := filepath.Join("envelopes", plannerUID, "children", "task-404.json")
		if _, err := reader.ReadPrompt(context.Background(), projectUID, miss); err == nil {
			t.Fatal("ReadPrompt() with missing file: want error, got nil")
		}
	})
}
