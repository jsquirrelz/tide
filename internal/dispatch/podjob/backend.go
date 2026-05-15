package podjob

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"

	tidev1alpha1 "github.com/jsquirrelz/tide/api/v1alpha1"
	"github.com/jsquirrelz/tide/internal/owner"
	pkgdispatch "github.com/jsquirrelz/tide/pkg/dispatch"
)

// EnvelopeReader is the interface for reading an EnvelopeOut from the per-Project PVC.
// Injected into PodJobBackend for testability — production wiring uses
// FilesystemEnvelopeReader.
type EnvelopeReader interface {
	// ReadOut reads /workspaces/{projectUID}/workspace/envelopes/{taskUID}/out.json.
	// Returns a wrapped error if the file does not exist or cannot be parsed.
	ReadOut(ctx context.Context, projectUID, taskUID string) (pkgdispatch.EnvelopeOut, error)
}

// FilesystemEnvelopeReader reads the EnvelopeOut JSON from the local filesystem.
// Used when the Manager pod has the shared tide-projects PVC mounted at WorkspaceRoot.
//
// Manager mounts the PVC at /workspaces (no subPath); Task pods mount the PVC with
// subPath {project-uid}/workspace at /workspace. The Manager reads:
// /workspaces/{project-uid}/workspace/envelopes/{taskUID}/out.json
type FilesystemEnvelopeReader struct {
	// WorkspaceRoot is the local path where the PVC is mounted (typically "/workspaces"
	// for the Manager pod, or "/workspace" for a test environment).
	WorkspaceRoot string
}

// ReadOut reads the EnvelopeOut from
// WorkspaceRoot/{projectUID}/workspace/envelopes/{taskUID}/out.json.
func (r *FilesystemEnvelopeReader) ReadOut(_ context.Context, projectUID, taskUID string) (pkgdispatch.EnvelopeOut, error) {
	path := filepath.Join(r.WorkspaceRoot, projectUID, "workspace", "envelopes", taskUID, "out.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return pkgdispatch.EnvelopeOut{}, fmt.Errorf("read envelope out %q: %w", path, err)
	}
	var out pkgdispatch.EnvelopeOut
	if err := json.Unmarshal(data, &out); err != nil {
		return pkgdispatch.EnvelopeOut{}, fmt.Errorf("unmarshal envelope out %q: %w", path, err)
	}
	return out, nil
}

// PodStatusEnvelopeReader reads EnvelopeOut JSON from the completed subagent
// container's termination message. This avoids assuming the manager can mount
// the namespace-local PVC used by a Task Job.
type PodStatusEnvelopeReader struct {
	Client   client.Reader
	Fallback EnvelopeReader
}

func (r *PodStatusEnvelopeReader) ReadOut(ctx context.Context, projectUID, taskUID string) (pkgdispatch.EnvelopeOut, error) {
	var pods corev1.PodList
	if r.Client != nil {
		if err := r.Client.List(ctx, &pods, client.MatchingLabels{"tideproject.k8s/task-uid": taskUID}); err != nil {
			return pkgdispatch.EnvelopeOut{}, fmt.Errorf("list pods for task %s: %w", taskUID, err)
		}
		for _, pod := range pods.Items {
			for _, status := range pod.Status.ContainerStatuses {
				if status.Name != ContainerNameSubagent || status.State.Terminated == nil {
					continue
				}
				msg := strings.TrimSpace(status.State.Terminated.Message)
				if msg == "" {
					continue
				}
				var out pkgdispatch.EnvelopeOut
				if err := json.Unmarshal([]byte(msg), &out); err != nil {
					return pkgdispatch.EnvelopeOut{}, fmt.Errorf("unmarshal termination envelope for pod %s/%s: %w", pod.Namespace, pod.Name, err)
				}
				return out, nil
			}
		}
	}
	if r.Fallback != nil {
		return r.Fallback.ReadOut(ctx, projectUID, taskUID)
	}
	return pkgdispatch.EnvelopeOut{}, fmt.Errorf("no termination envelope found for task %s", taskUID)
}

// PodJobBackend satisfies internal/dispatch.Dispatcher. It creates one K8s Job per
// Task dispatch attempt, monitors the Job to terminal state, and reads the result
// EnvelopeOut from the per-Project PVC via EnvReader.
//
// Phase 2 executor path note: The TaskReconciler (Plan 09) does NOT call Run —
// it calls BuildJobSpec + client.Create directly, then receives Owns-watch events on
// Job terminal state. Run is exposed for:
//  1. Unit/integration test fixtures that need a single call to drive a Job end-to-end.
//  2. Phase 3+ planner-dispatch callers running in a goroutine outside Reconcile.
//
// Calling Run from inside Reconcile() is forbidden (Pitfall 1 — blocks the
// reconciler goroutine on long-running I/O).
type PodJobBackend struct {
	Client         client.Client
	Scheme         *runtime.Scheme
	SubagentImage  string
	CredproxyImage string
	SigningKey     []byte
	EnvReader      EnvelopeReader
	// PVCName is the name of the chart-provisioned shared PVC (default "tide-projects").
	PVCName string
}

// Run satisfies internal/dispatch.Dispatcher.
//
// Run is NOT for the Phase 2 executor path (TaskReconciler handles that via Owns-watch).
// Run is for test fixtures and Phase 3 planner-dispatch callers.
//
// Run:
//  1. Fetches the Task + owning Project to build the full BuildOptions.
//  2. Calls BuildJobSpec to construct the Job spec.
//  3. Calls client.Create (treats AlreadyExists as success — Pitfall F idempotency).
//  4. Sets owner reference (Task → Job) via internal/owner.EnsureOwnerRef.
//  5. Polls the Job status until terminal state (Succeeded or Failed).
//  6. Reads EnvelopeOut via b.EnvReader.ReadOut.
//  7. Returns EnvelopeOut.
func (b *PodJobBackend) Run(ctx context.Context, in pkgdispatch.EnvelopeIn) (pkgdispatch.EnvelopeOut, error) {
	// 1. Resolve Task. We use label-selector to find the Task by UID.
	//    For Phase 2 / test usage, callers pre-populate the fake client with the Task object;
	//    in production the Task is always the reconciler's own object.
	var taskList tidev1alpha1.TaskList
	if err := b.Client.List(ctx, &taskList); err != nil {
		return pkgdispatch.EnvelopeOut{}, fmt.Errorf("list tasks: %w", err)
	}
	var task *tidev1alpha1.Task
	for i := range taskList.Items {
		if string(taskList.Items[i].UID) == in.TaskUID {
			task = &taskList.Items[i]
			break
		}
	}
	if task == nil {
		return pkgdispatch.EnvelopeOut{}, fmt.Errorf("task with UID %q not found", in.TaskUID)
	}

	// 2. Resolve Project.
	var projectList tidev1alpha1.ProjectList
	if err := b.Client.List(ctx, &projectList, client.InNamespace(task.Namespace)); err != nil {
		return pkgdispatch.EnvelopeOut{}, fmt.Errorf("list projects: %w", err)
	}
	var project *tidev1alpha1.Project
	if len(projectList.Items) > 0 {
		project = &projectList.Items[0]
	}
	if project == nil {
		return pkgdispatch.EnvelopeOut{}, fmt.Errorf("no project found in namespace %q", task.Namespace)
	}

	// 3. Determine attempt number. For Run callers (test fixtures and Phase 3 planners),
	//    we use attempt=1 unless the Task.Status.Attempt is already set.
	attempt := task.Status.Attempt
	if attempt == 0 {
		attempt = 1
	}

	// 4. Encode EnvelopeIn for the envelope-writer init container.
	envInJSON, err := json.Marshal(in)
	if err != nil {
		return pkgdispatch.EnvelopeOut{}, fmt.Errorf("marshal envelope in: %w", err)
	}

	pvcName := b.PVCName
	if pvcName == "" {
		pvcName = "tide-projects"
	}

	opts := BuildOptions{
		Task:           task,
		Project:        project,
		Attempt:        attempt,
		SignedToken:    string(b.SigningKey), // simplified for Run; TaskReconciler uses HMAC in Plan 09
		EnvelopeInJSON: envInJSON,
		SubagentImage:  b.SubagentImage,
		CredproxyImage: b.CredproxyImage,
		SecretUID:      string(project.UID),
		PVCName:        pvcName,
		ProjectUID:     string(project.UID),
	}

	// 5. Build the Job spec.
	job := BuildJobSpec(opts)

	// 6. Set owner reference (Task → Job) — cascade on Task deletion.
	if err := owner.EnsureOwnerRef(job, task, b.Scheme); err != nil {
		return pkgdispatch.EnvelopeOut{}, fmt.Errorf("ensure owner ref: %w", err)
	}

	// 7. Create the Job. AlreadyExists is treated as success (Pitfall F — SUB-03 idempotency).
	if err := b.Client.Create(ctx, job); err != nil {
		if !apierrors.IsAlreadyExists(err) {
			return pkgdispatch.EnvelopeOut{}, fmt.Errorf("create job: %w", err)
		}
		// AlreadyExists: the Job was already created (watch-lag race). Proceed to watch.
	}

	// 8. Poll Job to terminal state.
	// For Phase 2 test fixtures: the goroutine in tests updates Job status to Succeeded.
	// For Phase 3 production callers: the goroutine runs a planner in the background;
	// polling every 2s is acceptable since this is NOT in the reconcile hot path.
	jobName := client.ObjectKey{Namespace: task.Namespace, Name: JobName(task.UID, attempt)}
	pollErr := wait.PollUntilContextTimeout(ctx, 2*time.Second, 5*time.Minute, true,
		func(ctx context.Context) (done bool, err error) {
			var j batchv1.Job
			if err := b.Client.Get(ctx, jobName, &j); err != nil {
				if apierrors.IsNotFound(err) {
					return false, nil // not yet visible in the cache
				}
				return false, fmt.Errorf("get job: %w", err)
			}
			return isJobTerminal(&j), nil
		},
	)
	if pollErr != nil {
		return pkgdispatch.EnvelopeOut{}, fmt.Errorf("wait for job terminal state: %w", pollErr)
	}

	// 9. Read EnvelopeOut from PVC via the injected reader.
	out, err := b.EnvReader.ReadOut(ctx, string(project.UID), in.TaskUID)
	if err != nil {
		return pkgdispatch.EnvelopeOut{}, err
	}
	return out, nil
}

// isJobTerminal returns true if the Job has a Complete or Failed condition with Status=True.
func isJobTerminal(job *batchv1.Job) bool {
	for _, c := range job.Status.Conditions {
		if c.Status == corev1.ConditionTrue {
			if c.Type == batchv1.JobComplete || c.Type == batchv1.JobFailed {
				return true
			}
		}
	}
	return false
}
