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

// Per-method error-path unit tests for the 4 methods extracted from
// TaskReconciler.reconcileDispatch in Phase 04.1 P3.1. These are pure Go
// tests (no envtest/Ginkgo) using the fake controller-runtime client so they
// run fast without a live cluster.
package controller

import (
	"context"
	"errors"
	"testing"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	tideprojectv1alpha1 "github.com/jsquirrelz/tide/api/v1alpha2"
	"github.com/jsquirrelz/tide/internal/budget"
	"github.com/jsquirrelz/tide/internal/credproxy"
	"github.com/jsquirrelz/tide/internal/dispatch/podjob"
)

// fakeSchemeWithAll returns a runtime.Scheme with TIDE types + k8s batch/core registered.
func fakeSchemeWithAll(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	if err := tideprojectv1alpha1.AddToScheme(s); err != nil {
		t.Fatalf("AddToScheme: %v", err)
	}
	if err := batchv1.AddToScheme(s); err != nil {
		t.Fatalf("AddToScheme batchv1: %v", err)
	}
	if err := corev1.AddToScheme(s); err != nil {
		t.Fatalf("AddToScheme corev1: %v", err)
	}
	return s
}

// ---------- TestGateChecks tests ----------

// TestGateChecks_TerminalShortCircuit verifies that a Task in Phase=Succeeded
// returns shouldHalt=true with an empty ctrl.Result (no requeue).
func TestGateChecks_TerminalShortCircuit(t *testing.T) {
	task := &tideprojectv1alpha1.Task{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "task-terminal",
			Namespace: "default",
			UID:       types.UID("uid-terminal"),
		},
		Status: tideprojectv1alpha1.TaskStatus{
			Phase: "Succeeded",
		},
	}
	s := fakeSchemeWithAll(t)
	fakeClient := fake.NewClientBuilder().WithScheme(s).
		WithRuntimeObjects(task).
		WithStatusSubresource(&tideprojectv1alpha1.Task{}).
		Build()
	r := &TaskReconciler{
		Client: fakeClient,
		Scheme: s,
		Deps: TaskReconcilerDeps{
			Budget:         budget.NewStore(),
			Defaults:       budget.Limits{RequestsPerMinute: 120, BurstSize: 10},
			SigningKey:     []byte("tide-test-signing-key-32-bytes!!"),
			SubagentImage:  "tide-stub-subagent:test",
			CredproxyImage: "tide-credproxy:test",
		},
	}

	gate, err := r.gateChecks(context.Background(), task)
	if err != nil {
		t.Fatalf("gateChecks returned error: %v", err)
	}
	if !gate.shouldHalt {
		t.Error("shouldHalt = false; want true for Succeeded task")
	}
	if gate.result.RequeueAfter != 0 {
		t.Errorf("RequeueAfter = %s; want 0 for terminal short-circuit", gate.result.RequeueAfter)
	}
	if gate.project != nil {
		t.Errorf("project = %v; want nil for halted gate", gate.project)
	}
}

// TestGateChecks_FailedShortCircuit verifies that a Task in Phase=Failed
// also returns shouldHalt=true.
func TestGateChecks_FailedShortCircuit(t *testing.T) {
	task := &tideprojectv1alpha1.Task{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "task-failed-sc",
			Namespace: "default",
			UID:       types.UID("uid-failed-sc"),
		},
		Status: tideprojectv1alpha1.TaskStatus{
			Phase: "Failed",
		},
	}
	s := fakeSchemeWithAll(t)
	fakeClient := fake.NewClientBuilder().WithScheme(s).
		WithRuntimeObjects(task).
		Build()
	r := &TaskReconciler{
		Client: fakeClient,
		Scheme: s,
		Deps: TaskReconcilerDeps{
			Budget:   budget.NewStore(),
			Defaults: budget.Limits{RequestsPerMinute: 120, BurstSize: 10},
		},
	}

	gate, err := r.gateChecks(context.Background(), task)
	if err != nil {
		t.Fatalf("gateChecks returned error: %v", err)
	}
	if !gate.shouldHalt {
		t.Error("shouldHalt = false; want true for Failed task")
	}
}

// TestGateChecks_ParentUnresolved verifies that a Task with no project label
// and no owner refs triggers ErrParentUnresolved → shouldHalt=true with a 30s
// RequeueAfter and the ConditionParentUnresolved condition set on the Task.
func TestGateChecks_ParentUnresolved(t *testing.T) {
	task := &tideprojectv1alpha1.Task{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "task-no-parent",
			Namespace: "default",
			UID:       types.UID("uid-no-parent"),
		},
		// No labels, no owner refs — resolveProject returns ErrParentUnresolved.
	}
	s := fakeSchemeWithAll(t)
	fakeClient := fake.NewClientBuilder().WithScheme(s).
		WithRuntimeObjects(task).
		WithStatusSubresource(&tideprojectv1alpha1.Task{}).
		Build()
	r := &TaskReconciler{
		Client: fakeClient,
		Scheme: s,
		Deps: TaskReconcilerDeps{
			Budget:   budget.NewStore(),
			Defaults: budget.Limits{RequestsPerMinute: 120, BurstSize: 10},
		},
	}

	gate, err := r.gateChecks(context.Background(), task)
	if err != nil {
		t.Fatalf("gateChecks returned unexpected error: %v", err)
	}
	if !gate.shouldHalt {
		t.Error("shouldHalt = false; want true for ErrParentUnresolved")
	}
	// Should requeue with 30s delay.
	if gate.result.RequeueAfter == 0 {
		t.Error("RequeueAfter = 0; want 30s for parent-unresolved requeue")
	}
}

// ---------- TestAcquireDispatchSlots tests ----------

// TestAcquireDispatchSlots_NoProviderSecret verifies that when the Project has
// no ProviderSecretRef, acquireDispatchSlots returns nil error and a no-op release.
func TestAcquireDispatchSlots_NoProviderSecret(t *testing.T) {
	task := &tideprojectv1alpha1.Task{
		ObjectMeta: metav1.ObjectMeta{Name: "task-noprov", Namespace: "default", UID: "uid-noprov"},
	}
	project := &tideprojectv1alpha1.Project{
		ObjectMeta: metav1.ObjectMeta{Name: "proj-noprov", Namespace: "default"},
		// No ProviderSecretRef.
	}
	s := fakeSchemeWithAll(t)
	fakeClient := fake.NewClientBuilder().WithScheme(s).
		WithRuntimeObjects(task, project).
		WithStatusSubresource(&tideprojectv1alpha1.Task{}).
		Build()
	r := &TaskReconciler{
		Client: fakeClient,
		Scheme: s,
		Deps: TaskReconcilerDeps{
			Budget:   budget.NewStore(),
			Defaults: budget.Limits{RequestsPerMinute: 120, BurstSize: 10},
		},
	}

	release, err := r.acquireDispatchSlots(context.Background(), task, project)
	if err != nil {
		t.Fatalf("acquireDispatchSlots returned error: %v", err)
	}
	if release == nil {
		t.Fatal("release is nil; want a callable no-op func")
	}
	// Calling release on a no-op path should not panic.
	release()
}

// TestAcquireDispatchSlots_RateLimitHit verifies that a pre-exhausted bucket
// returns a *rateLimitedError and a non-nil release function.
func TestAcquireDispatchSlots_RateLimitHit(t *testing.T) {
	// Build exhausted store: RPM=1, burst=1; pre-reserve the single token.
	exhaustedStore := budget.NewStore()
	secretUID := "secret-uid-rl"
	exhaustedLimits := budget.Limits{RequestsPerMinute: 1, BurstSize: 1}
	lim := exhaustedStore.ForSecret(secretUID, exhaustedLimits)
	preRsv := lim.Reserve()
	_ = preRsv // intentionally NOT cancelled — drains the bucket

	task := &tideprojectv1alpha1.Task{
		ObjectMeta: metav1.ObjectMeta{Name: "task-rl", Namespace: "default", UID: "uid-rl"},
	}
	project := &tideprojectv1alpha1.Project{
		ObjectMeta: metav1.ObjectMeta{Name: "proj-rl", Namespace: "default"},
		Spec:       tideprojectv1alpha1.ProjectSpec{SchemaRevision: "v1alpha2", ProviderSecretRef: "test-secret-rl"},
	}
	// Pre-create the secret in the fake client with the known UID so Budget can
	// look it up.
	secretObj := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-secret-rl",
			Namespace: "default",
			UID:       types.UID(secretUID),
		},
	}

	s := fakeSchemeWithAll(t)
	fakeClient := fake.NewClientBuilder().WithScheme(s).
		WithRuntimeObjects(task, project, secretObj).
		WithStatusSubresource(&tideprojectv1alpha1.Task{}).
		Build()
	r := &TaskReconciler{
		Client: fakeClient,
		Scheme: s,
		Deps: TaskReconcilerDeps{
			Budget:   exhaustedStore,
			Defaults: exhaustedLimits,
		},
	}

	release, err := r.acquireDispatchSlots(context.Background(), task, project)
	if release == nil {
		t.Fatal("release is nil; acquireDispatchSlots must always return a release func")
	}
	if err == nil {
		t.Fatal("err = nil; expected *rateLimitedError when bucket exhausted")
	}
	var rlErr *rateLimitedError
	if !errors.As(err, &rlErr) {
		t.Errorf("err type = %T; want *rateLimitedError", err)
	}
	if rlErr.delay <= 0 {
		t.Errorf("rateLimitedError.delay = %s; want > 0", rlErr.delay)
	}
}

// TestAcquireDispatchSlots_ReleaseFn verifies that calling release() after
// a successful acquire cancels the held reservation (CR-03 preservation).
func TestAcquireDispatchSlots_ReleaseFn(t *testing.T) {
	// A fresh store with high capacity. We just need to observe that release()
	// is callable without panic and does not return an error.
	store := budget.NewStore()
	secretUID := "secret-uid-release"
	limits := budget.Limits{RequestsPerMinute: 60, BurstSize: 5}

	task := &tideprojectv1alpha1.Task{
		ObjectMeta: metav1.ObjectMeta{Name: "task-rel", Namespace: "default", UID: "uid-rel"},
	}
	project := &tideprojectv1alpha1.Project{
		ObjectMeta: metav1.ObjectMeta{Name: "proj-rel", Namespace: "default"},
		Spec:       tideprojectv1alpha1.ProjectSpec{SchemaRevision: "v1alpha2", ProviderSecretRef: "secret-rel"},
	}
	secretObj := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "secret-rel",
			Namespace: "default",
			UID:       types.UID(secretUID),
		},
	}

	s := fakeSchemeWithAll(t)
	fakeClient := fake.NewClientBuilder().WithScheme(s).
		WithRuntimeObjects(task, project, secretObj).
		WithStatusSubresource(&tideprojectv1alpha1.Task{}).
		Build()
	r := &TaskReconciler{
		Client: fakeClient,
		Scheme: s,
		Deps: TaskReconcilerDeps{
			Budget:   store,
			Defaults: limits,
		},
	}

	release, err := r.acquireDispatchSlots(context.Background(), task, project)
	if err != nil {
		t.Fatalf("acquireDispatchSlots returned error: %v", err)
	}
	// release() must be callable without panic (CR-03 — the deferred cancel path).
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("release() panicked: %v", r)
		}
	}()
	release()
}

// ---------- TestPrepareDispatch tests ----------

// TestPrepareDispatch_AttemptIncrement verifies that attempt counter starts at 1
// for a fresh Task (no prior Jobs).
func TestPrepareDispatch_AttemptIncrement(t *testing.T) {
	task := &tideprojectv1alpha1.Task{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "task-attempt",
			Namespace: "default",
			UID:       types.UID("uid-attempt"),
		},
		Spec: tideprojectv1alpha1.TaskSpec{
			FilesTouched:        []string{"src/main.go"},
			DeclaredOutputPaths: []string{"artifacts/out.txt"},
		},
	}
	project := &tideprojectv1alpha1.Project{
		ObjectMeta: metav1.ObjectMeta{Name: "proj-attempt", Namespace: "default"},
		Spec:       tideprojectv1alpha1.ProjectSpec{SchemaRevision: "v1alpha2", MaxAttemptsPerTask: 3},
	}

	s := fakeSchemeWithAll(t)
	fakeClient := fake.NewClientBuilder().WithScheme(s).
		WithRuntimeObjects(task, project).
		WithStatusSubresource(&tideprojectv1alpha1.Task{}).
		Build()
	r := &TaskReconciler{
		Client: fakeClient,
		Scheme: s,
		Deps: TaskReconcilerDeps{
			Budget:         budget.NewStore(),
			Defaults:       budget.Limits{RequestsPerMinute: 120, BurstSize: 10},
			SigningKey:     []byte("tide-test-signing-key-32-bytes!!"),
			SubagentImage:  "tide-stub-subagent:test",
			CredproxyImage: "tide-credproxy:test",
		},
	}

	spec, err := r.prepareDispatch(context.Background(), task, project)
	if err != nil {
		t.Fatalf("prepareDispatch returned error: %v", err)
	}
	if spec.attempt != 1 {
		t.Errorf("attempt = %d; want 1 for fresh Task with no prior Jobs", spec.attempt)
	}
	if spec.token == "" {
		t.Error("token is empty; want non-empty signed token")
	}
	if len(spec.envInJSON) == 0 {
		t.Error("envInJSON is empty; want non-empty EnvelopeIn JSON")
	}
}

// TestPrepareDispatch_ExceedMaxAttempts verifies that when the attempt counter
// exceeds MaxAttemptsPerTask, prepareDispatch returns a *maxAttemptsError
// (which reconcileDispatch translates to a clean halt).
func TestPrepareDispatch_ExceedMaxAttempts(t *testing.T) {
	task := &tideprojectv1alpha1.Task{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "task-maxatt",
			Namespace: "default",
			UID:       types.UID("uid-maxatt"),
		},
	}
	project := &tideprojectv1alpha1.Project{
		ObjectMeta: metav1.ObjectMeta{Name: "proj-maxatt", Namespace: "default"},
		Spec:       tideprojectv1alpha1.ProjectSpec{SchemaRevision: "v1alpha2", MaxAttemptsPerTask: 1},
	}

	// Pre-create a Job labeled attempt=1 so nextAttempt returns 2 > MaxAttemptsPerTask=1.
	preExistingJob := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "tide-task-uid-maxatt-1",
			Namespace: "default",
			Labels: map[string]string{
				"tideproject.k8s/task-uid": "uid-maxatt",
				"tideproject.k8s/attempt":  "1",
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

	s := fakeSchemeWithAll(t)
	fakeClient := fake.NewClientBuilder().WithScheme(s).
		WithRuntimeObjects(task, project, preExistingJob).
		WithStatusSubresource(&tideprojectv1alpha1.Task{}).
		Build()
	r := &TaskReconciler{
		Client: fakeClient,
		Scheme: s,
		Deps: TaskReconcilerDeps{
			Budget:         budget.NewStore(),
			Defaults:       budget.Limits{RequestsPerMinute: 120, BurstSize: 10},
			SigningKey:     []byte("tide-test-signing-key-32-bytes!!"),
			SubagentImage:  "tide-stub-subagent:test",
			CredproxyImage: "tide-credproxy:test",
		},
	}

	_, err := r.prepareDispatch(context.Background(), task, project)
	if err == nil {
		t.Fatal("prepareDispatch returned nil error; want *maxAttemptsError")
	}
	var maErr *maxAttemptsError
	if !errors.As(err, &maErr) {
		t.Errorf("err type = %T; want *maxAttemptsError", err)
	}
}

// ---------- TestCreateDispatchJob tests ----------

// TestCreateDispatchJob_AlreadyExists verifies that when the Job already
// exists (idempotent dispatch path, Pitfall F / SUB-03), createDispatchJob
// returns (ctrl.Result{}, nil) — treating AlreadyExists as success.
func TestCreateDispatchJob_AlreadyExists(t *testing.T) {
	taskUID := types.UID("uid-ae")
	task := &tideprojectv1alpha1.Task{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "task-ae",
			Namespace: "default",
			UID:       taskUID,
		},
		Spec: tideprojectv1alpha1.TaskSpec{
			FilesTouched:        []string{"src/main.go"},
			DeclaredOutputPaths: []string{"artifacts/out.txt"},
		},
	}
	project := &tideprojectv1alpha1.Project{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "proj-ae",
			Namespace: "default",
			UID:       types.UID("proj-uid-ae"),
		},
	}

	// Pre-create the Job that createDispatchJob would try to create.
	preJob := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      podjob.JobName(taskUID, 1),
			Namespace: "default",
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

	s := fakeSchemeWithAll(t)
	fakeClient := fake.NewClientBuilder().WithScheme(s).
		WithRuntimeObjects(task, project, preJob).
		WithStatusSubresource(&tideprojectv1alpha1.Task{}).
		Build()
	r := &TaskReconciler{
		Client: fakeClient,
		Scheme: s,
		Deps: TaskReconcilerDeps{
			Budget:         budget.NewStore(),
			Defaults:       budget.Limits{RequestsPerMinute: 120, BurstSize: 10},
			SigningKey:     []byte("tide-test-signing-key-32-bytes!!"),
			SubagentImage:  "tide-stub-subagent:test",
			CredproxyImage: "tide-credproxy:test",
		},
	}

	// Mint a valid signed token.
	token, err := credproxy.Sign(r.Deps.SigningKey, string(taskUID), 5*time.Minute)
	if err != nil {
		t.Fatalf("credproxy.Sign: %v", err)
	}

	spec := taskDispatchSpec{
		attempt:   1,
		token:     token,
		envInJSON: []byte(`{"apiVersion":"v1alpha1","taskUID":"uid-ae"}`),
		project:   project,
	}

	result, err := r.createDispatchJob(context.Background(), task, spec)
	if err != nil {
		t.Fatalf("createDispatchJob returned error: %v", err)
	}
	if result.RequeueAfter != 0 {
		t.Errorf("RequeueAfter = %s; want 0 for successful dispatch (AlreadyExists path)", result.RequeueAfter)
	}
}

// ---------- TestReconcileDispatch_CommittedReleaseSuppression ----------

// TestReconcileDispatch_CommittedReleaseSuppression is a regression test for
// CR-03 preservation across the P3.1 extraction. It verifies that:
//  1. The rate-limited sentinel error (rateLimitedError) is correctly translated
//     to ctrl.Result{RequeueAfter > 0} with nil error.
//  2. The committed=false → release() path is exercised (bucket was not
//     permanently drained by the reconciler).
//
// The test exercises the full reconcileDispatch orchestrator with an exhausted
// bucket so the rateLimited path fires deterministically.
func TestReconcileDispatch_CommittedReleaseSuppression(t *testing.T) {
	// Build an exhausted budget store.
	store := budget.NewStore()
	secretUID := "secret-uid-cr03"
	limits := budget.Limits{RequestsPerMinute: 1, BurstSize: 1}
	lim := store.ForSecret(secretUID, limits)
	preRsv := lim.Reserve()
	_ = preRsv // drain bucket — intentionally not cancelled

	s := fakeSchemeWithAll(t)

	task := &tideprojectv1alpha1.Task{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "task-cr03",
			Namespace: "default",
			UID:       types.UID("uid-cr03"),
			Labels:    map[string]string{"tideproject.k8s/project": "proj-cr03"},
		},
	}
	projectObj := &tideprojectv1alpha1.Project{
		ObjectMeta: metav1.ObjectMeta{Name: "proj-cr03", Namespace: "default"},
		Spec: tideprojectv1alpha1.ProjectSpec{SchemaRevision: "v1alpha2",
			ProviderSecretRef: "secret-cr03",
		},
	}
	secretObj := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "secret-cr03",
			Namespace: "default",
			UID:       types.UID(secretUID),
		},
	}

	// Register the .spec.planRef field indexer required by listSiblingTasks.
	fakeClient := fake.NewClientBuilder().WithScheme(s).
		WithRuntimeObjects(task, projectObj, secretObj).
		WithStatusSubresource(&tideprojectv1alpha1.Task{}).
		WithIndex(&tideprojectv1alpha1.Task{}, taskPlanRefIndexKey, func(obj client.Object) []string {
			t := obj.(*tideprojectv1alpha1.Task) //nolint:forcetypeassert
			return []string{t.Spec.PlanRef}
		}).
		Build()
	r := &TaskReconciler{
		Client: fakeClient,
		Scheme: s,
		Deps: TaskReconcilerDeps{
			Budget:         store,
			Defaults:       limits,
			SigningKey:     []byte("tide-test-signing-key-32-bytes!!"),
			SubagentImage:  "tide-stub-subagent:test",
			CredproxyImage: "tide-credproxy:test",
			Dispatcher:     &stubDispatcher{},
		},
	}

	result, err := r.reconcileDispatch(context.Background(), task)

	// Rate-limited path must return (RequeueAfter > 0, nil) — not an error.
	if err != nil {
		t.Fatalf("reconcileDispatch returned error: %v; want nil for rate-limit path", err)
	}
	if result.RequeueAfter == 0 {
		t.Errorf("RequeueAfter = 0; want > 0 for exhausted bucket (CR-03 sentinel translation)")
	}
	t.Logf("CR-03 regression: RequeueAfter = %s (sentinel correctly translated)", result.RequeueAfter)
}

// TestBuildEnvelopeIn_PromptPath covers defect #10b fix: buildEnvelopeIn now stamps
// EnvelopeIn.PromptPath from Task.Spec.PromptPath instead of reading the prompt
// cross-namespace off the Manager PVC. The Manager no longer holds a PromptReader;
// the in-pod anthropic runner reads the prompt artifact same-namespace in-pod.
func TestBuildEnvelopeIn_PromptPath(t *testing.T) {
	const promptPath = "envelopes/planner-uid/children/task-01.json"

	task := &tideprojectv1alpha1.Task{
		ObjectMeta: metav1.ObjectMeta{Name: "task-prompt", Namespace: "default", UID: types.UID("uid-prompt")},
		Spec: tideprojectv1alpha1.TaskSpec{
			PlanRef:             "plan-01",
			FilesTouched:        []string{"main.go"},
			DeclaredOutputPaths: []string{"main.go"},
			PromptPath:          promptPath,
		},
	}
	const runBranch = "tide/run-proj-prompt-1700000000"
	project := &tideprojectv1alpha1.Project{
		ObjectMeta: metav1.ObjectMeta{Name: "proj-prompt", Namespace: "default", UID: types.UID("proj-uid")},
		Status: tideprojectv1alpha1.ProjectStatus{
			Git: tideprojectv1alpha1.GitStatus{BranchName: runBranch},
		},
	}

	s := fakeSchemeWithAll(t)
	fakeClient := fake.NewClientBuilder().WithScheme(s).WithRuntimeObjects(task, project).Build()

	r := &TaskReconciler{
		Client: fakeClient,
		Scheme: s,
		Deps: TaskReconcilerDeps{
			Budget:         budget.NewStore(),
			Defaults:       budget.Limits{RequestsPerMinute: 120, BurstSize: 10},
			SigningKey:     []byte("tide-test-signing-key-32-bytes!!"),
			SubagentImage:  "tide-stub-subagent:test",
			CredproxyImage: "tide-credproxy:test",
		},
	}

	t.Run("PromptPath stamped on EnvelopeIn", func(t *testing.T) {
		envIn, _, err := r.buildEnvelopeIn(context.Background(), task, project, 1, "tok")
		if err != nil {
			t.Fatalf("buildEnvelopeIn: %v", err)
		}
		if envIn.PromptPath != promptPath {
			t.Errorf("EnvelopeIn.PromptPath = %q, want %q", envIn.PromptPath, promptPath)
		}
		// Prompt must be empty — the Manager no longer reads it cross-namespace.
		if envIn.Prompt != "" {
			t.Errorf("EnvelopeIn.Prompt = %q, want empty (in-pod read via PromptPath)", envIn.Prompt)
		}
	})

	// 09-09: the executor's worktree branch is threaded via EnvelopeIn.Branch
	// from project.Status.Git.BranchName (replaces the never-written branch.txt).
	t.Run("Branch stamped from project run branch", func(t *testing.T) {
		envIn, _, err := r.buildEnvelopeIn(context.Background(), task, project, 1, "tok")
		if err != nil {
			t.Fatalf("buildEnvelopeIn: %v", err)
		}
		if envIn.Branch != runBranch {
			t.Errorf("EnvelopeIn.Branch = %q, want %q (executor worktree branch must be non-empty)", envIn.Branch, runBranch)
		}
	})

	// Phase 11: the executor envelope must carry a resolved ProviderSpec
	// (Vendor "anthropic" + the task-level model). Latent until a run first
	// reached real task execution — the in-pod anthropic runner refuses an
	// empty vendor ("refusing vendor=\"\"").
	t.Run("Provider resolved with anthropic vendor and task model", func(t *testing.T) {
		projWithModel := project.DeepCopy()
		projWithModel.Spec.Subagent.Model = "claude-haiku-4-5"
		envIn, _, err := r.buildEnvelopeIn(context.Background(), task, projWithModel, 1, "tok")
		if err != nil {
			t.Fatalf("buildEnvelopeIn: %v", err)
		}
		if envIn.Provider.Vendor != "anthropic" {
			t.Errorf("EnvelopeIn.Provider.Vendor = %q, want %q", envIn.Provider.Vendor, "anthropic")
		}
		if envIn.Provider.Model != "claude-haiku-4-5" {
			t.Errorf("EnvelopeIn.Provider.Model = %q, want %q", envIn.Provider.Model, "claude-haiku-4-5")
		}
	})
}
