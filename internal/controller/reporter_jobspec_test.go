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
	"testing"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"

	tideprojectv1alpha1 "github.com/jsquirrelz/tide/api/v1alpha1"
	"github.com/jsquirrelz/tide/internal/controller"
)

func newTestScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	_ = clientgoscheme.AddToScheme(s)
	_ = tideprojectv1alpha1.AddToScheme(s)
	return s
}

// TestBuildReporterJob_ServiceAccount asserts the Job uses the tide-reporter SA.
func TestBuildReporterJob_ServiceAccount(t *testing.T) {
	project := &tideprojectv1alpha1.Project{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "proj",
			Namespace: "ns-a",
			UID:       "project-uid-1",
		},
	}
	parent := &tideprojectv1alpha1.Milestone{
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
	project := &tideprojectv1alpha1.Project{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "proj",
			Namespace: "ns-a",
			UID:       "project-uid-2",
		},
	}
	parent := &tideprojectv1alpha1.Milestone{
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
	project := &tideprojectv1alpha1.Project{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "proj",
			Namespace: "ns-b",
			UID:       "project-uid-3",
		},
	}
	parent := &tideprojectv1alpha1.Milestone{
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

// TestBuildReporterJob_OwnerRef asserts owner ref is set to the parent (not necessarily the project).
func TestBuildReporterJob_OwnerRef(t *testing.T) {
	project := &tideprojectv1alpha1.Project{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "proj",
			Namespace: "ns-c",
			UID:       "project-uid-4",
		},
	}
	parent := &tideprojectv1alpha1.Milestone{
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
	project := &tideprojectv1alpha1.Project{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "proj",
			Namespace: "ns-d",
			UID:       "project-uid-5",
		},
	}
	parent := &tideprojectv1alpha1.Milestone{
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
	project := &tideprojectv1alpha1.Project{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "proj",
			Namespace: "ns-e",
			UID:       "project-uid-6",
		},
	}
	parent := &tideprojectv1alpha1.Milestone{
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
	gitCfg := &tideprojectv1alpha1.GitConfig{
		CredsSecretRef: "my-git-creds",
	}
	project := &tideprojectv1alpha1.Project{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "proj",
			Namespace: "ns-f",
			UID:       "project-uid-7",
		},
		Spec: tideprojectv1alpha1.ProjectSpec{
			Git: gitCfg,
		},
	}
	parent := &tideprojectv1alpha1.Milestone{
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
	project := &tideprojectv1alpha1.Project{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "proj",
			Namespace: "tenant-ns",
			UID:       "project-uid-8",
		},
	}
	parent := &tideprojectv1alpha1.Milestone{
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
	project := &tideprojectv1alpha1.Project{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "proj",
			Namespace: "ns-g",
			UID:       "project-uid-9",
		},
	}
	parent := &tideprojectv1alpha1.Milestone{
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
	project := &tideprojectv1alpha1.Project{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "proj",
			Namespace: "ns-h",
			UID:       "project-uid-10",
		},
	}
	parent := &tideprojectv1alpha1.Milestone{
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
	project := &tideprojectv1alpha1.Project{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "proj",
			Namespace: "ns-i",
			UID:       "project-uid-11",
		},
	}
	parent := &tideprojectv1alpha1.Milestone{
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
	project := &tideprojectv1alpha1.Project{
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
	project := &tideprojectv1alpha1.Project{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "proj",
			Namespace: "ns-k",
			UID:       "project-uid-13",
		},
	}
	parent := &tideprojectv1alpha1.Milestone{
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
