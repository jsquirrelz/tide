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

package controller_test

import (
	"strings"
	"testing"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"

	tideprojectv1alpha3 "github.com/jsquirrelz/tide/api/v1alpha3"
	"github.com/jsquirrelz/tide/internal/controller"
)

func newTestScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	_ = clientgoscheme.AddToScheme(s)
	_ = tideprojectv1alpha3.AddToScheme(s)
	return s
}

// TestBuildReporterJob_ServiceAccount asserts the Job uses the tide-reporter SA.
func TestBuildReporterJob_ServiceAccount(t *testing.T) {
	project := &tideprojectv1alpha3.Project{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "proj",
			Namespace: "ns-a",
			UID:       "project-uid-1",
		},
	}
	parent := &tideprojectv1alpha3.Milestone{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ms-1",
			Namespace: "ns-a",
			UID:       "parent-uid-1",
		},
	}
	scheme := newTestScheme()
	opts := controller.ReporterOptions{ReporterImage: "ghcr.io/jsquirrelz/tide-reporter:v0.1.0-dev"}
	job := controller.BuildReporterJob(parent, project, "tide-projects", "task-uid-1", "Milestone", opts, scheme)

	if job.Spec.Template.Spec.ServiceAccountName != "tide-reporter" {
		t.Errorf("expected ServiceAccountName=tide-reporter, got %q", job.Spec.Template.Spec.ServiceAccountName)
	}
}

// TestBuildReporterJob_SubPath asserts the PVC is mounted at /workspace with the correct subPath.
func TestBuildReporterJob_SubPath(t *testing.T) {
	project := &tideprojectv1alpha3.Project{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "proj",
			Namespace: "ns-a",
			UID:       "project-uid-2",
		},
	}
	parent := &tideprojectv1alpha3.Milestone{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ms-1",
			Namespace: "ns-a",
			UID:       "parent-uid-2",
		},
	}
	scheme := newTestScheme()
	opts := controller.ReporterOptions{ReporterImage: "ghcr.io/jsquirrelz/tide-reporter:v0.1.0-dev"}
	job := controller.BuildReporterJob(parent, project, "tide-projects", "task-uid-2", "Milestone", opts, scheme)

	containers := job.Spec.Template.Spec.Containers
	if len(containers) == 0 {
		t.Fatal("expected at least one container")
	}
	found := false
	for _, vm := range containers[0].VolumeMounts {
		if vm.MountPath == "/workspace" {
			found = true
			wantSubPath := "project-uid-2/workspace"
			if vm.SubPath != wantSubPath {
				t.Errorf("expected SubPath=%q, got %q", wantSubPath, vm.SubPath)
			}
		}
	}
	if !found {
		t.Error("no volumeMount at /workspace found")
	}
}

// TestBuildReporterJob_Args asserts all required flags are present in container args.
func TestBuildReporterJob_Args(t *testing.T) {
	project := &tideprojectv1alpha3.Project{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "proj",
			Namespace: "ns-b",
			UID:       "project-uid-3",
		},
	}
	parent := &tideprojectv1alpha3.Milestone{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ms-2",
			Namespace: "ns-b",
			UID:       "parent-uid-3",
		},
	}
	scheme := newTestScheme()
	opts := controller.ReporterOptions{ReporterImage: "ghcr.io/jsquirrelz/tide-reporter:v0.1.0-dev"}
	job := controller.BuildReporterJob(parent, project, "tide-projects", "task-uid-3", "Milestone", opts, scheme)

	args := job.Spec.Template.Spec.Containers[0].Args
	wantArgs := map[string]bool{
		"--workspace=/workspace":      false,
		"--project-uid=project-uid-3": false,
		"--task-uid=task-uid-3":       false,
		"--parent-name=ms-2":          false,
		"--parent-namespace=ns-b":     false,
		"--parent-kind=Milestone":     false,
	}
	for _, a := range args {
		wantArgs[a] = true
	}
	for arg, found := range wantArgs {
		if !found {
			t.Errorf("expected arg %q not present in %v", arg, args)
		}
	}
}

// TestBuildReporterJob_TraceparentArg asserts BuildReporterJob carries
// --traceparent=<value> in the container Args (not Env — Pitfall 3) when
// ReporterOptions.TraceParent is set, and omits it entirely when empty.
func TestBuildReporterJob_TraceparentArg(t *testing.T) {
	const traceParent = "00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01"

	project := &tideprojectv1alpha3.Project{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "proj",
			Namespace: "ns-c",
			UID:       "project-uid-4",
		},
	}
	parent := &tideprojectv1alpha3.Milestone{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ms-3",
			Namespace: "ns-c",
			UID:       "parent-uid-4",
		},
	}
	scheme := newTestScheme()

	t.Run("present when set", func(t *testing.T) {
		opts := controller.ReporterOptions{
			ReporterImage: "ghcr.io/jsquirrelz/tide-reporter:v0.1.0-dev",
			TraceParent:   traceParent,
		}
		job := controller.BuildReporterJob(parent, project, "tide-projects", "task-uid-4", "Milestone", opts, scheme)
		args := job.Spec.Template.Spec.Containers[0].Args
		want := "--traceparent=" + traceParent
		var found bool
		for _, a := range args {
			if a == want {
				found = true
			}
		}
		if !found {
			t.Errorf("expected arg %q not present in %v", want, args)
		}
	})

	t.Run("absent when empty", func(t *testing.T) {
		opts := controller.ReporterOptions{ReporterImage: "ghcr.io/jsquirrelz/tide-reporter:v0.1.0-dev"}
		job := controller.BuildReporterJob(parent, project, "tide-projects", "task-uid-4", "Milestone", opts, scheme)
		args := job.Spec.Template.Spec.Containers[0].Args
		for _, a := range args {
			if strings.HasPrefix(a, "--traceparent") {
				t.Errorf("did not expect a --traceparent arg when TraceParent is empty, got %v", args)
			}
		}
	})
}

// TestBuildReporterJob_SkipMessageSpansArg asserts BuildReporterJob carries
// the bareword --skip-message-spans Arg (Args-only transport, D-04) when
// ReporterOptions.SkipMessageSpans is true, in BOTH Job shapes (D-05's
// "composes uniformly with both Job shapes"), and omits it entirely when
// false (D-03 default-safe: absent means synthesize).
func TestBuildReporterJob_SkipMessageSpansArg(t *testing.T) {
	project := &tideprojectv1alpha3.Project{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "proj",
			Namespace: "ns-c",
			UID:       "project-uid-4",
		},
	}
	parent := &tideprojectv1alpha3.Milestone{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ms-3",
			Namespace: "ns-c",
			UID:       "parent-uid-4",
		},
	}
	scheme := newTestScheme()

	t.Run("present when true (materialization shape)", func(t *testing.T) {
		opts := controller.ReporterOptions{
			ReporterImage:    "ghcr.io/jsquirrelz/tide-reporter:v0.1.0-dev",
			SkipMessageSpans: true,
		}
		job := controller.BuildReporterJob(parent, project, "tide-projects", "task-uid-4", "Milestone", opts, scheme)
		args := job.Spec.Template.Spec.Containers[0].Args
		var found bool
		for _, a := range args {
			if a == "--skip-message-spans" {
				found = true
			}
		}
		if !found {
			t.Errorf("expected arg %q not present in %v", "--skip-message-spans", args)
		}
	})

	t.Run("present when true (trace-only shape)", func(t *testing.T) {
		opts := controller.ReporterOptions{
			ReporterImage:    "ghcr.io/jsquirrelz/tide-reporter:v0.1.0-dev",
			TraceOnly:        true,
			TraceOnlyJobKey:  "job-uid-skip-1",
			SkipMessageSpans: true,
		}
		job := controller.BuildReporterJob(parent, project, "tide-projects", "task-uid-4", "Milestone", opts, scheme)
		args := job.Spec.Template.Spec.Containers[0].Args
		var found bool
		for _, a := range args {
			if a == "--skip-message-spans" {
				found = true
			}
		}
		if !found {
			t.Errorf("expected arg %q not present in %v", "--skip-message-spans", args)
		}
	})

	t.Run("absent when false", func(t *testing.T) {
		opts := controller.ReporterOptions{ReporterImage: "ghcr.io/jsquirrelz/tide-reporter:v0.1.0-dev"}
		job := controller.BuildReporterJob(parent, project, "tide-projects", "task-uid-4", "Milestone", opts, scheme)
		args := job.Spec.Template.Spec.Containers[0].Args
		for _, a := range args {
			if a == "--skip-message-spans" || strings.HasPrefix(a, "--skip-message-spans") {
				t.Errorf("did not expect a --skip-message-spans arg when SkipMessageSpans is false, got %v", args)
			}
		}
	})
}

// TestBuildReporterJob_SessionIDArg asserts BuildReporterJob carries
// --session-id=<value> in the container Args (not Env — 46 D-05) when
// ReporterOptions.SessionID is set, in BOTH Job shapes, and omits it
// entirely when empty.
func TestBuildReporterJob_SessionIDArg(t *testing.T) {
	project := &tideprojectv1alpha3.Project{
		ObjectMeta: metav1.ObjectMeta{Name: "proj", Namespace: "ns-p", UID: "project-uid-18"},
	}
	parent := &tideprojectv1alpha3.Milestone{
		ObjectMeta: metav1.ObjectMeta{Name: "ms-16", Namespace: "ns-p", UID: "parent-uid-18"},
	}
	scheme := newTestScheme()

	t.Run("present when set (materialization shape)", func(t *testing.T) {
		opts := controller.ReporterOptions{
			ReporterImage: "ghcr.io/jsquirrelz/tide-reporter:v0.1.0-dev",
			SessionID:     "project-uid-18",
		}
		job := controller.BuildReporterJob(parent, project, "tide-projects", "task-uid-18", "Milestone", opts, scheme)
		args := job.Spec.Template.Spec.Containers[0].Args
		want := "--session-id=project-uid-18"
		var found bool
		for _, a := range args {
			if a == want {
				found = true
			}
		}
		if !found {
			t.Errorf("expected arg %q not present in %v", want, args)
		}
	})

	t.Run("present when set (trace-only shape)", func(t *testing.T) {
		opts := controller.ReporterOptions{
			ReporterImage:   "ghcr.io/jsquirrelz/tide-reporter:v0.1.0-dev",
			TraceOnly:       true,
			TraceOnlyJobKey: "job-uid-session-1",
			SessionID:       "project-uid-18",
		}
		job := controller.BuildReporterJob(parent, project, "tide-projects", "task-uid-18", "Milestone", opts, scheme)
		args := job.Spec.Template.Spec.Containers[0].Args
		want := "--session-id=project-uid-18"
		var found bool
		for _, a := range args {
			if a == want {
				found = true
			}
		}
		if !found {
			t.Errorf("expected arg %q not present in %v", want, args)
		}
	})

	t.Run("absent when empty", func(t *testing.T) {
		opts := controller.ReporterOptions{ReporterImage: "ghcr.io/jsquirrelz/tide-reporter:v0.1.0-dev"}
		job := controller.BuildReporterJob(parent, project, "tide-projects", "task-uid-18", "Milestone", opts, scheme)
		args := job.Spec.Template.Spec.Containers[0].Args
		for _, a := range args {
			if strings.HasPrefix(a, "--session-id") {
				t.Errorf("did not expect a --session-id arg when SessionID is empty, got %v", args)
			}
		}
	})
}

// TestBuildReporterJob_MetadataArg asserts BuildReporterJob carries
// --metadata=<value> in the container Args, pre-JSON-encoded and passed
// through verbatim (this file never marshals it), in BOTH Job shapes, and
// omits it entirely when empty.
func TestBuildReporterJob_MetadataArg(t *testing.T) {
	const metadataJSON = `{"level":"task"}`

	project := &tideprojectv1alpha3.Project{
		ObjectMeta: metav1.ObjectMeta{Name: "proj", Namespace: "ns-q", UID: "project-uid-19"},
	}
	parent := &tideprojectv1alpha3.Milestone{
		ObjectMeta: metav1.ObjectMeta{Name: "ms-17", Namespace: "ns-q", UID: "parent-uid-19"},
	}
	scheme := newTestScheme()

	t.Run("present when set (materialization shape)", func(t *testing.T) {
		opts := controller.ReporterOptions{
			ReporterImage: "ghcr.io/jsquirrelz/tide-reporter:v0.1.0-dev",
			MetadataJSON:  metadataJSON,
		}
		job := controller.BuildReporterJob(parent, project, "tide-projects", "task-uid-19", "Milestone", opts, scheme)
		args := job.Spec.Template.Spec.Containers[0].Args
		want := "--metadata=" + metadataJSON
		var found bool
		for _, a := range args {
			if a == want {
				found = true
			}
		}
		if !found {
			t.Errorf("expected arg %q not present in %v", want, args)
		}
	})

	t.Run("present when set (trace-only shape)", func(t *testing.T) {
		opts := controller.ReporterOptions{
			ReporterImage:   "ghcr.io/jsquirrelz/tide-reporter:v0.1.0-dev",
			TraceOnly:       true,
			TraceOnlyJobKey: "job-uid-metadata-1",
			MetadataJSON:    metadataJSON,
		}
		job := controller.BuildReporterJob(parent, project, "tide-projects", "task-uid-19", "Milestone", opts, scheme)
		args := job.Spec.Template.Spec.Containers[0].Args
		want := "--metadata=" + metadataJSON
		var found bool
		for _, a := range args {
			if a == want {
				found = true
			}
		}
		if !found {
			t.Errorf("expected arg %q not present in %v", want, args)
		}
	})

	t.Run("absent when empty", func(t *testing.T) {
		opts := controller.ReporterOptions{ReporterImage: "ghcr.io/jsquirrelz/tide-reporter:v0.1.0-dev"}
		job := controller.BuildReporterJob(parent, project, "tide-projects", "task-uid-19", "Milestone", opts, scheme)
		args := job.Spec.Template.Spec.Containers[0].Args
		for _, a := range args {
			if strings.HasPrefix(a, "--metadata") {
				t.Errorf("did not expect a --metadata arg when MetadataJSON is empty, got %v", args)
			}
		}
	})
}

// TestBuildReporterJob_TagsArg asserts BuildReporterJob carries
// --tags=<comma-joined> in the container Args when ReporterOptions.Tags is
// non-empty, in BOTH Job shapes, and omits it entirely when the slice is
// empty (zero-valued).
func TestBuildReporterJob_TagsArg(t *testing.T) {
	project := &tideprojectv1alpha3.Project{
		ObjectMeta: metav1.ObjectMeta{Name: "proj", Namespace: "ns-r", UID: "project-uid-20"},
	}
	parent := &tideprojectv1alpha3.Milestone{
		ObjectMeta: metav1.ObjectMeta{Name: "ms-18", Namespace: "ns-r", UID: "parent-uid-20"},
	}
	scheme := newTestScheme()

	t.Run("present when set (materialization shape)", func(t *testing.T) {
		opts := controller.ReporterOptions{
			ReporterImage: "ghcr.io/jsquirrelz/tide-reporter:v0.1.0-dev",
			Tags:          []string{"task", "strict"},
		}
		job := controller.BuildReporterJob(parent, project, "tide-projects", "task-uid-20", "Milestone", opts, scheme)
		args := job.Spec.Template.Spec.Containers[0].Args
		want := "--tags=task,strict"
		var found bool
		for _, a := range args {
			if a == want {
				found = true
			}
		}
		if !found {
			t.Errorf("expected arg %q not present in %v", want, args)
		}
	})

	t.Run("present when set (trace-only shape)", func(t *testing.T) {
		opts := controller.ReporterOptions{
			ReporterImage:   "ghcr.io/jsquirrelz/tide-reporter:v0.1.0-dev",
			TraceOnly:       true,
			TraceOnlyJobKey: "job-uid-tags-1",
			Tags:            []string{"task", "strict"},
		}
		job := controller.BuildReporterJob(parent, project, "tide-projects", "task-uid-20", "Milestone", opts, scheme)
		args := job.Spec.Template.Spec.Containers[0].Args
		want := "--tags=task,strict"
		var found bool
		for _, a := range args {
			if a == want {
				found = true
			}
		}
		if !found {
			t.Errorf("expected arg %q not present in %v", want, args)
		}
	})

	t.Run("absent when empty", func(t *testing.T) {
		opts := controller.ReporterOptions{ReporterImage: "ghcr.io/jsquirrelz/tide-reporter:v0.1.0-dev"}
		job := controller.BuildReporterJob(parent, project, "tide-projects", "task-uid-20", "Milestone", opts, scheme)
		args := job.Spec.Template.Spec.Containers[0].Args
		for _, a := range args {
			if strings.HasPrefix(a, "--tags") {
				t.Errorf("did not expect a --tags arg when Tags is empty, got %v", args)
			}
		}
	})
}

// TestBuildReporterJob_OwnerRef asserts owner ref is set to the parent (not necessarily the project).
func TestBuildReporterJob_OwnerRef(t *testing.T) {
	project := &tideprojectv1alpha3.Project{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "proj",
			Namespace: "ns-c",
			UID:       "project-uid-4",
		},
	}
	parent := &tideprojectv1alpha3.Milestone{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ms-3",
			Namespace: "ns-c",
			UID:       "parent-uid-4",
		},
	}
	scheme := newTestScheme()
	opts := controller.ReporterOptions{ReporterImage: "ghcr.io/jsquirrelz/tide-reporter:v0.1.0-dev"}
	job := controller.BuildReporterJob(parent, project, "tide-projects", "task-uid-4", "Milestone", opts, scheme)

	if len(job.OwnerReferences) == 0 {
		t.Fatal("expected owner references to be set")
	}
	ownerFound := false
	for _, ref := range job.OwnerReferences {
		if ref.UID == "parent-uid-4" {
			ownerFound = true
			if ref.Controller == nil || !*ref.Controller {
				t.Error("expected Controller=true on owner ref")
			}
		}
	}
	if !ownerFound {
		t.Errorf("expected owner ref with UID=parent-uid-4, refs=%v", job.OwnerReferences)
	}
}

// TestBuildReporterJob_RoleLabel asserts the Job and pod template carry tideproject.k8s/role=reporter.
func TestBuildReporterJob_RoleLabel(t *testing.T) {
	project := &tideprojectv1alpha3.Project{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "proj",
			Namespace: "ns-d",
			UID:       "project-uid-5",
		},
	}
	parent := &tideprojectv1alpha3.Milestone{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ms-4",
			Namespace: "ns-d",
			UID:       "parent-uid-5",
		},
	}
	scheme := newTestScheme()
	opts := controller.ReporterOptions{ReporterImage: "ghcr.io/jsquirrelz/tide-reporter:v0.1.0-dev"}
	job := controller.BuildReporterJob(parent, project, "tide-projects", "task-uid-5", "Milestone", opts, scheme)

	const roleKey = "tideproject.k8s/role"
	if job.Labels[roleKey] != "reporter" {
		t.Errorf("expected job label %s=reporter, got %q", roleKey, job.Labels[roleKey])
	}
	if job.Spec.Template.Labels[roleKey] != "reporter" {
		t.Errorf("expected pod template label %s=reporter, got %q", roleKey, job.Spec.Template.Labels[roleKey])
	}
}

// TestBuildReporterJob_SecurityContext asserts RunAsNonRoot + drop ALL caps.
func TestBuildReporterJob_SecurityContext(t *testing.T) {
	project := &tideprojectv1alpha3.Project{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "proj",
			Namespace: "ns-e",
			UID:       "project-uid-6",
		},
	}
	parent := &tideprojectv1alpha3.Milestone{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ms-5",
			Namespace: "ns-e",
			UID:       "parent-uid-6",
		},
	}
	scheme := newTestScheme()
	opts := controller.ReporterOptions{ReporterImage: "ghcr.io/jsquirrelz/tide-reporter:v0.1.0-dev"}
	job := controller.BuildReporterJob(parent, project, "tide-projects", "task-uid-6", "Milestone", opts, scheme)

	sc := job.Spec.Template.Spec.Containers[0].SecurityContext
	if sc == nil {
		t.Fatal("expected SecurityContext to be set")
	}
	if sc.RunAsNonRoot == nil || !*sc.RunAsNonRoot {
		t.Error("expected RunAsNonRoot=true")
	}
	if sc.Capabilities == nil {
		t.Fatal("expected Capabilities to be set")
	}
	found := false
	for _, cap := range sc.Capabilities.Drop {
		if cap == "ALL" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected Capabilities.Drop to include ALL, got %v", sc.Capabilities.Drop)
	}
}

// TestBuildReporterJob_NoGitCredsEnvFrom asserts no git-creds EnvFrom is present.
func TestBuildReporterJob_NoGitCredsEnvFrom(t *testing.T) {
	gitCfg := &tideprojectv1alpha3.GitConfig{
		CredsSecretRef: "my-git-creds",
	}
	project := &tideprojectv1alpha3.Project{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "proj",
			Namespace: "ns-f",
			UID:       "project-uid-7",
		},
		Spec: tideprojectv1alpha3.ProjectSpec{SchemaRevision: "v1alpha3",
			Git: gitCfg,
		},
	}
	parent := &tideprojectv1alpha3.Milestone{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ms-6",
			Namespace: "ns-f",
			UID:       "parent-uid-7",
		},
	}
	scheme := newTestScheme()
	opts := controller.ReporterOptions{ReporterImage: "ghcr.io/jsquirrelz/tide-reporter:v0.1.0-dev"}
	job := controller.BuildReporterJob(parent, project, "tide-projects", "task-uid-7", "Milestone", opts, scheme)

	for _, c := range job.Spec.Template.Spec.Containers {
		if len(c.EnvFrom) > 0 {
			t.Errorf("expected no EnvFrom (git creds) on reporter container, got %v", c.EnvFrom)
		}
	}
}

// TestBuildReporterJob_Namespace asserts the Job is in the project namespace.
func TestBuildReporterJob_Namespace(t *testing.T) {
	project := &tideprojectv1alpha3.Project{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "proj",
			Namespace: "tenant-ns",
			UID:       "project-uid-8",
		},
	}
	parent := &tideprojectv1alpha3.Milestone{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ms-7",
			Namespace: "tenant-ns",
			UID:       "parent-uid-8",
		},
	}
	scheme := newTestScheme()
	opts := controller.ReporterOptions{ReporterImage: "ghcr.io/jsquirrelz/tide-reporter:v0.1.0-dev"}
	job := controller.BuildReporterJob(parent, project, "tide-projects", "task-uid-8", "Milestone", opts, scheme)

	if job.Namespace != "tenant-ns" {
		t.Errorf("expected job namespace=tenant-ns, got %q", job.Namespace)
	}
}

// TestBuildReporterJob_DeterministicName asserts two calls with the same parent produce the same Job name.
func TestBuildReporterJob_DeterministicName(t *testing.T) {
	project := &tideprojectv1alpha3.Project{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "proj",
			Namespace: "ns-g",
			UID:       "project-uid-9",
		},
	}
	parent := &tideprojectv1alpha3.Milestone{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ms-8",
			Namespace: "ns-g",
			UID:       "parent-uid-9",
		},
	}
	scheme := newTestScheme()
	opts := controller.ReporterOptions{ReporterImage: "ghcr.io/jsquirrelz/tide-reporter:v0.1.0-dev"}
	job1 := controller.BuildReporterJob(parent, project, "tide-projects", "task-uid-9", "Milestone", opts, scheme)
	job2 := controller.BuildReporterJob(parent, project, "tide-projects", "task-uid-9", "Milestone", opts, scheme)

	if job1.Name != job2.Name {
		t.Errorf("expected deterministic name, got %q and %q", job1.Name, job2.Name)
	}
	if job1.Name == "" {
		t.Error("expected non-empty job name")
	}
}

// TestBuildReporterJob_BackoffAndTTL asserts BackoffLimit=2 and TTLSecondsAfterFinished=300.
func TestBuildReporterJob_BackoffAndTTL(t *testing.T) {
	project := &tideprojectv1alpha3.Project{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "proj",
			Namespace: "ns-h",
			UID:       "project-uid-10",
		},
	}
	parent := &tideprojectv1alpha3.Milestone{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ms-9",
			Namespace: "ns-h",
			UID:       "parent-uid-10",
		},
	}
	scheme := newTestScheme()
	opts := controller.ReporterOptions{ReporterImage: "ghcr.io/jsquirrelz/tide-reporter:v0.1.0-dev"}
	job := controller.BuildReporterJob(parent, project, "tide-projects", "task-uid-10", "Milestone", opts, scheme)

	if job.Spec.BackoffLimit == nil || *job.Spec.BackoffLimit != 2 {
		t.Errorf("expected BackoffLimit=2, got %v", job.Spec.BackoffLimit)
	}
	if job.Spec.TTLSecondsAfterFinished == nil || *job.Spec.TTLSecondsAfterFinished != 300 {
		t.Errorf("expected TTLSecondsAfterFinished=300, got %v", job.Spec.TTLSecondsAfterFinished)
	}
	if job.Spec.Template.Spec.RestartPolicy != corev1.RestartPolicyNever {
		t.Errorf("expected RestartPolicy=Never, got %v", job.Spec.Template.Spec.RestartPolicy)
	}
}

// TestBuildReporterJob_Image asserts the container uses opts.ReporterImage.
func TestBuildReporterJob_Image(t *testing.T) {
	project := &tideprojectv1alpha3.Project{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "proj",
			Namespace: "ns-i",
			UID:       "project-uid-11",
		},
	}
	parent := &tideprojectv1alpha3.Milestone{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ms-10",
			Namespace: "ns-i",
			UID:       "parent-uid-11",
		},
	}
	scheme := newTestScheme()
	image := "ghcr.io/jsquirrelz/tide-reporter:v1.2.3"
	opts := controller.ReporterOptions{ReporterImage: image}
	job := controller.BuildReporterJob(parent, project, "tide-projects", "task-uid-11", "Milestone", opts, scheme)

	got := job.Spec.Template.Spec.Containers[0].Image
	if got != image {
		t.Errorf("expected image=%q, got %q", image, got)
	}
}

// TestBuildReporterJob_ProjectAsParent asserts Project-as-parent works (project is its own parent).
func TestBuildReporterJob_ProjectAsParent(t *testing.T) {
	project := &tideprojectv1alpha3.Project{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "self-proj",
			Namespace: "ns-j",
			UID:       "project-uid-12",
		},
	}
	scheme := newTestScheme()
	opts := controller.ReporterOptions{ReporterImage: "ghcr.io/jsquirrelz/tide-reporter:v0.1.0-dev"}
	// Project IS the parent (project-level planner case)
	job := controller.BuildReporterJob(project, project, "tide-projects", "task-uid-12", "Project", opts, scheme)

	if job.Namespace != "ns-j" {
		t.Errorf("expected job namespace=ns-j, got %q", job.Namespace)
	}
	// Should have owner ref pointing at the project
	ownerFound := false
	for _, ref := range job.OwnerReferences {
		if ref.UID == "project-uid-12" {
			ownerFound = true
		}
	}
	if !ownerFound {
		t.Errorf("expected owner ref with UID=project-uid-12, refs=%v", job.OwnerReferences)
	}
	// args should include --parent-kind=Project
	args := job.Spec.Template.Spec.Containers[0].Args
	found := false
	for _, a := range args {
		if a == "--parent-kind=Project" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected --parent-kind=Project in args, got %v", args)
	}
}

// TestBuildReporterJob_EmptyImageStillBuilds asserts a zero ReporterImage still produces a valid Job
// (caller-side skip is done by the spawn site, not the builder).
func TestBuildReporterJob_EmptyImageStillBuilds(t *testing.T) {
	project := &tideprojectv1alpha3.Project{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "proj",
			Namespace: "ns-k",
			UID:       "project-uid-13",
		},
	}
	parent := &tideprojectv1alpha3.Milestone{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ms-11",
			Namespace: "ns-k",
			UID:       "parent-uid-13",
		},
	}
	scheme := newTestScheme()
	opts := controller.ReporterOptions{ReporterImage: ""}
	// Must not panic; caller decides whether to skip based on empty image.
	var job *batchv1.Job
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("BuildReporterJob panicked with empty image: %v", r)
		}
	}()
	job = controller.BuildReporterJob(parent, project, "tide-projects", "task-uid-13", "Milestone", opts, scheme)
	if job == nil {
		t.Error("expected non-nil Job even when image is empty")
	}
}

// TestBuildReporterJob_OTLPEndpointEnv asserts that setting opts.OTLPEndpoint
// stamps exactly two container Env entries: OTEL_EXPORTER_OTLP_ENDPOINT (the
// forwarded manager value) and the hardcoded OTEL_BSP_MAX_EXPORT_BATCH_SIZE=6
// literal (T-44-04 mitigation; RESEARCH Size-Boundary Model).
func TestBuildReporterJob_OTLPEndpointEnv(t *testing.T) {
	project := &tideprojectv1alpha3.Project{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "proj",
			Namespace: "ns-l",
			UID:       "project-uid-14",
		},
	}
	parent := &tideprojectv1alpha3.Milestone{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ms-12",
			Namespace: "ns-l",
			UID:       "parent-uid-14",
		},
	}
	scheme := newTestScheme()
	opts := controller.ReporterOptions{
		ReporterImage: "ghcr.io/jsquirrelz/tide-reporter:v0.1.0-dev",
		OTLPEndpoint:  "collector:4317",
	}
	job := controller.BuildReporterJob(parent, project, "tide-projects", "task-uid-14", "Milestone", opts, scheme)

	env := job.Spec.Template.Spec.Containers[0].Env
	want := map[string]string{
		"OTEL_EXPORTER_OTLP_ENDPOINT":    "collector:4317",
		"OTEL_BSP_MAX_EXPORT_BATCH_SIZE": "6",
	}
	if len(env) != len(want) {
		t.Fatalf("expected exactly %d Env entries, got %d: %v", len(want), len(env), env)
	}
	for _, e := range env {
		wantVal, ok := want[e.Name]
		if !ok {
			t.Errorf("unexpected Env entry %q", e.Name)
			continue
		}
		if e.Value != wantVal {
			t.Errorf("expected Env %s=%q, got %q", e.Name, wantVal, e.Value)
		}
	}
}

// TestBuildReporterJob_NoOTLPEndpointNoEnv asserts that when OTLPEndpoint is
// empty, the container Env is empty — byte-identical posture to today (the
// reporter's otelinit falls back to its no-op TracerProvider).
func TestBuildReporterJob_NoOTLPEndpointNoEnv(t *testing.T) {
	project := &tideprojectv1alpha3.Project{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "proj",
			Namespace: "ns-m",
			UID:       "project-uid-15",
		},
	}
	parent := &tideprojectv1alpha3.Milestone{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ms-13",
			Namespace: "ns-m",
			UID:       "parent-uid-15",
		},
	}
	scheme := newTestScheme()
	opts := controller.ReporterOptions{ReporterImage: "ghcr.io/jsquirrelz/tide-reporter:v0.1.0-dev"}
	job := controller.BuildReporterJob(parent, project, "tide-projects", "task-uid-15", "Milestone", opts, scheme)

	env := job.Spec.Template.Spec.Containers[0].Env
	if len(env) != 0 {
		t.Errorf("expected zero Env entries when OTLPEndpoint is empty, got %v", env)
	}
}

// TestBuildReporterJob_TraceOnly asserts the trace-only Job shape: a distinct
// name keyed on the completed dispatch Job's UID (never colliding with the
// materialization reporter's "tide-reporter-<parentUID>" name), minimal Args
// with no parent-CR flags, and everything else (SA/PVC subPath/SecurityContext/
// role label) identical to the materialization shape.
func TestBuildReporterJob_TraceOnly(t *testing.T) {
	project := &tideprojectv1alpha3.Project{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "proj",
			Namespace: "ns-n",
			UID:       "project-uid-16",
		},
	}
	parent := &tideprojectv1alpha3.Milestone{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ms-14",
			Namespace: "ns-n",
			UID:       "parent-uid-16",
		},
	}
	scheme := newTestScheme()
	opts := controller.ReporterOptions{
		ReporterImage:   "ghcr.io/jsquirrelz/tide-reporter:v0.1.0-dev",
		TraceOnly:       true,
		TraceOnlyJobKey: "job-uid-123",
	}
	job := controller.BuildReporterJob(parent, project, "tide-projects", "task-uid-9", "Milestone", opts, scheme)

	wantName := "tide-reporter-trace-job-uid-123"
	if job.Name != wantName {
		t.Errorf("expected Job name=%q, got %q", wantName, job.Name)
	}

	args := job.Spec.Template.Spec.Containers[0].Args
	wantPresent := []string{"--trace-only", "--workspace=/workspace", "--task-uid=task-uid-9"}
	for _, want := range wantPresent {
		found := false
		for _, a := range args {
			if a == want {
				found = true
			}
		}
		if !found {
			t.Errorf("expected arg %q not present in %v", want, args)
		}
	}
	wantAbsentPrefixes := []string{"--parent-name", "--parent-namespace", "--parent-kind"}
	for _, prefix := range wantAbsentPrefixes {
		for _, a := range args {
			if strings.HasPrefix(a, prefix) {
				t.Errorf("did not expect arg with prefix %q in trace-only Args, got %v", prefix, args)
			}
		}
	}

	// Shared shape assertions — identical to the materialization Job.
	if job.Spec.Template.Spec.ServiceAccountName != "tide-reporter" {
		t.Errorf("expected ServiceAccountName=tide-reporter, got %q", job.Spec.Template.Spec.ServiceAccountName)
	}
	const roleKey = "tideproject.k8s/role"
	if job.Labels[roleKey] != "reporter" {
		t.Errorf("expected job label %s=reporter, got %q", roleKey, job.Labels[roleKey])
	}
	sc := job.Spec.Template.Spec.Containers[0].SecurityContext
	if sc == nil || sc.RunAsNonRoot == nil || !*sc.RunAsNonRoot {
		t.Error("expected trace-only shape to keep hardened SecurityContext (RunAsNonRoot=true)")
	}
	foundSubPath := false
	for _, vm := range job.Spec.Template.Spec.Containers[0].VolumeMounts {
		if vm.MountPath == "/workspace" {
			foundSubPath = true
			wantSubPath := "project-uid-16/workspace"
			if vm.SubPath != wantSubPath {
				t.Errorf("expected SubPath=%q, got %q", wantSubPath, vm.SubPath)
			}
		}
	}
	if !foundSubPath {
		t.Error("no volumeMount at /workspace found in trace-only shape")
	}
}

// TestBuildReporterJob_TraceOnlyFalseUnchanged is the regression guard: with
// TraceOnly left at its zero value (false), the materialization name/args
// shape is byte-identical to today's (pre-Phase-44) behavior.
func TestBuildReporterJob_TraceOnlyFalseUnchanged(t *testing.T) {
	project := &tideprojectv1alpha3.Project{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "proj",
			Namespace: "ns-o",
			UID:       "project-uid-17",
		},
	}
	parent := &tideprojectv1alpha3.Milestone{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ms-15",
			Namespace: "ns-o",
			UID:       "parent-uid-17",
		},
	}
	scheme := newTestScheme()
	opts := controller.ReporterOptions{ReporterImage: "ghcr.io/jsquirrelz/tide-reporter:v0.1.0-dev"}
	job := controller.BuildReporterJob(parent, project, "tide-projects", "task-uid-17", "Milestone", opts, scheme)

	wantName := "tide-reporter-parent-uid-17"
	if job.Name != wantName {
		t.Errorf("expected Job name=%q, got %q", wantName, job.Name)
	}
	args := job.Spec.Template.Spec.Containers[0].Args
	wantArgs := map[string]bool{
		"--workspace=/workspace":       false,
		"--project-uid=project-uid-17": false,
		"--task-uid=task-uid-17":       false,
		"--parent-name=ms-15":          false,
		"--parent-namespace=ns-o":      false,
		"--parent-kind=Milestone":      false,
	}
	for _, a := range args {
		wantArgs[a] = true
	}
	for arg, found := range wantArgs {
		if !found {
			t.Errorf("expected arg %q not present in %v", arg, args)
		}
	}
}
