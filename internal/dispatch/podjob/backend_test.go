package podjob

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

	tidev1alpha1 "github.com/jsquirrelz/tide/api/v1alpha1"
	pkgdispatch "github.com/jsquirrelz/tide/pkg/dispatch"
)

// testScheme builds a runtime.Scheme with the types needed by PodJobBackend tests.
func testScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	if err := tidev1alpha1.AddToScheme(s); err != nil {
		t.Fatalf("AddToScheme tidev1alpha1: %v", err)
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

func (f *fakeEnvReader) ReadOut(_ context.Context, _ string) (pkgdispatch.EnvelopeOut, error) {
	return f.out, f.err
}

// testTask constructs a minimal Task for backend tests.
func testTask(ns, name string, uid types.UID) *tidev1alpha1.Task {
	return &tidev1alpha1.Task{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
			UID:       uid,
		},
		Spec: tidev1alpha1.TaskSpec{
			PlanRef:             "plan-alpha",
			FilesTouched:        []string{"foo.go"},
			DeclaredOutputPaths: []string{"out.json"},
			Caps: &tidev1alpha1.Caps{
				WallClockSeconds: 60,
			},
		},
	}
}

// testProject constructs a minimal Project for backend tests.
func testProject(ns, name string, uid types.UID) *tidev1alpha1.Project {
	return &tidev1alpha1.Project{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
			UID:       uid,
		},
		Spec: tidev1alpha1.ProjectSpec{
			TargetRepo:        "https://github.com/example/repo",
			ProviderSecretRef: "provider-secret",
		},
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
		for i := 0; i < 20; i++ {
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
	project := testProject("default", "project-beta", "project-uid-beta")

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
		for i := 0; i < 20; i++ {
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
	project := testProject("default", "project-gamma", "project-uid-gamma")

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
		for i := 0; i < 20; i++ {
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
	project := testProject("default", "project-delta", "project-uid-delta")

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
		for i := 0; i < 20; i++ {
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
