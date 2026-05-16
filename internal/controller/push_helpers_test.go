/*
Copyright 2026 TIDE Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package controller

import (
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"

	tideprojectv1alpha1 "github.com/jsquirrelz/tide/api/v1alpha1"
)

// fixtureProject returns a hand-constructed Project for buildPushJob /
// buildCloneJob testing. Pure unit-level fixture — no envtest, no client,
// no controller-runtime machinery; the helpers are pure functions that
// only read fields off the *Project struct.
func fixtureProject() *tideprojectv1alpha1.Project {
	return &tideprojectv1alpha1.Project{
		TypeMeta: metav1.TypeMeta{
			APIVersion: tideprojectv1alpha1.GroupVersion.String(),
			Kind:       "Project",
		},
		ObjectMeta: metav1.ObjectMeta{
			UID:       types.UID("proj-uid-abc"),
			Namespace: "test-ns",
			Name:      "demo-project",
		},
		Spec: tideprojectv1alpha1.ProjectSpec{
			TargetRepo: "https://github.com/example/demo.git",
			Git: &tideprojectv1alpha1.GitConfig{
				RepoURL:        "https://github.com/example/demo.git",
				CredsSecretRef: "demo-git-creds",
			},
		},
	}
}

// schemeForTest returns a runtime.Scheme with the Project type registered.
// EnsureOwnerRef needs a scheme to translate the parent's GVK.
func schemeForTest(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	if err := tideprojectv1alpha1.AddToScheme(s); err != nil {
		t.Fatalf("AddToScheme: %v", err)
	}
	return s
}

// ---------- Test 1: buildPushJob name (D-B5 deterministic) ----------

func TestBuildPushJobName(t *testing.T) {
	project := fixtureProject()
	scheme := schemeForTest(t)
	opts := PushOptions{
		TidePushImage: "ghcr.io/jsquirrelz/tide-push:test",
		Branch:        "tide/run-demo-1747200000",
	}
	job := buildPushJob(project, "tide-projects", opts, scheme)
	want := "tide-push-proj-uid-abc"
	if job.ObjectMeta.Name != want {
		t.Errorf("Job.Name = %q, want %q (D-B5 deterministic)", job.ObjectMeta.Name, want)
	}
	if job.ObjectMeta.Namespace != "test-ns" {
		t.Errorf("Job.Namespace = %q, want %q", job.ObjectMeta.Namespace, "test-ns")
	}
}

// ---------- Test 2: buildPushJob ServiceAccountName ----------

func TestBuildPushJobServiceAccount(t *testing.T) {
	project := fixtureProject()
	scheme := schemeForTest(t)
	job := buildPushJob(project, "tide-projects", PushOptions{}, scheme)
	got := job.Spec.Template.Spec.ServiceAccountName
	if got != "tide-push" {
		t.Errorf("Job.Spec.Template.Spec.ServiceAccountName = %q, want %q (dedicated SA, D-B1 least-privilege)", got, "tide-push")
	}
}

// ---------- Test 3: buildPushJob envFrom git creds Secret ----------

func TestBuildPushJobEnvFromCredsSecret(t *testing.T) {
	project := fixtureProject()
	scheme := schemeForTest(t)
	job := buildPushJob(project, "tide-projects", PushOptions{}, scheme)
	containers := job.Spec.Template.Spec.Containers
	if len(containers) != 1 {
		t.Fatalf("Containers length = %d, want 1", len(containers))
	}
	envFrom := containers[0].EnvFrom
	if len(envFrom) == 0 {
		t.Fatal("Container has no envFrom entries (D-B1 requires git creds Secret envFrom)")
	}
	found := false
	for _, ef := range envFrom {
		if ef.SecretRef != nil && ef.SecretRef.LocalObjectReference.Name == project.Spec.Git.CredsSecretRef {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Container envFrom does not contain SecretRef to %q (project.Spec.Git.CredsSecretRef)", project.Spec.Git.CredsSecretRef)
	}
}

// ---------- Test 4: buildPushJob volume + mount ----------

func TestBuildPushJobVolumeMount(t *testing.T) {
	project := fixtureProject()
	scheme := schemeForTest(t)
	job := buildPushJob(project, "tide-projects-pvc", PushOptions{}, scheme)

	// Volume should be backed by PVC tide-projects-pvc.
	vols := job.Spec.Template.Spec.Volumes
	if len(vols) == 0 {
		t.Fatal("No volumes on Job pod spec")
	}
	var wsVol *corev1.Volume
	for i := range vols {
		if vols[i].Name == "project-workspace" {
			wsVol = &vols[i]
			break
		}
	}
	if wsVol == nil {
		t.Fatal("No project-workspace volume found")
	}
	if wsVol.PersistentVolumeClaim == nil {
		t.Fatal("project-workspace volume is not backed by a PVC")
	}
	if wsVol.PersistentVolumeClaim.ClaimName != "tide-projects-pvc" {
		t.Errorf("PVC name = %q, want %q", wsVol.PersistentVolumeClaim.ClaimName, "tide-projects-pvc")
	}

	// Mount /workspace SubPath <project.UID>/workspace.
	mounts := job.Spec.Template.Spec.Containers[0].VolumeMounts
	if len(mounts) == 0 {
		t.Fatal("No volume mounts on push container")
	}
	var wsMount *corev1.VolumeMount
	for i := range mounts {
		if mounts[i].Name == "project-workspace" {
			wsMount = &mounts[i]
			break
		}
	}
	if wsMount == nil {
		t.Fatal("No project-workspace mount on push container")
	}
	if wsMount.MountPath != "/workspace" {
		t.Errorf("MountPath = %q, want %q", wsMount.MountPath, "/workspace")
	}
	wantSubPath := "proj-uid-abc/workspace"
	if wsMount.SubPath != wantSubPath {
		t.Errorf("SubPath = %q, want %q (per-Project PVC isolation)", wsMount.SubPath, wantSubPath)
	}
}

// ---------- Test 5: buildPushJob args ----------

func TestBuildPushJobArgs(t *testing.T) {
	project := fixtureProject()
	scheme := schemeForTest(t)
	opts := PushOptions{
		TidePushImage: "ghcr.io/jsquirrelz/tide-push:test",
		Branch:        "tide/run-demo-1747200000",
		LastPushedSHA: "deadbeef1234",
	}
	job := buildPushJob(project, "tide-projects", opts, scheme)
	args := job.Spec.Template.Spec.Containers[0].Args
	joined := strings.Join(args, " ")

	wants := []string{
		"--mode=push",
		"--branch=tide/run-demo-1747200000",
		"--last-pushed-sha=deadbeef1234",
		"--project-uid=proj-uid-abc",
	}
	for _, w := range wants {
		found := false
		for _, a := range args {
			if a == w {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Args missing %q (got: %s)", w, joined)
		}
	}
}

// ---------- Test 6: buildPushJob owner reference ----------

func TestBuildPushJobOwnerReference(t *testing.T) {
	project := fixtureProject()
	scheme := schemeForTest(t)
	job := buildPushJob(project, "tide-projects", PushOptions{}, scheme)
	refs := job.OwnerReferences
	if len(refs) == 0 {
		t.Fatal("Job has no OwnerReferences (expected one pointing at Project)")
	}
	var projRef *metav1.OwnerReference
	for i := range refs {
		if refs[i].Kind == "Project" {
			projRef = &refs[i]
			break
		}
	}
	if projRef == nil {
		t.Fatal("No OwnerReference of Kind=Project")
	}
	if projRef.UID != project.UID {
		t.Errorf("OwnerReference.UID = %q, want %q", projRef.UID, project.UID)
	}
	if projRef.Controller == nil || !*projRef.Controller {
		t.Error("OwnerReference.Controller is not true")
	}
	if projRef.BlockOwnerDeletion == nil || !*projRef.BlockOwnerDeletion {
		t.Error("OwnerReference.BlockOwnerDeletion is not true")
	}
}

// ---------- Test 7: buildCloneJob name ----------

func TestBuildCloneJobName(t *testing.T) {
	project := fixtureProject()
	scheme := schemeForTest(t)
	opts := CloneOptions{TidePushImage: "ghcr.io/jsquirrelz/tide-push:test"}
	job := buildCloneJob(project, "tide-projects", opts, scheme)
	want := "tide-clone-proj-uid-abc"
	if job.ObjectMeta.Name != want {
		t.Errorf("Job.Name = %q, want %q", job.ObjectMeta.Name, want)
	}
}

// ---------- buildCommitMessage tests (D-B2 / W11) ----------

// TestBuildCommitMessage_AllFourShapes asserts all four D-B2 boundary
// commit message strings are produced verbatim with the locked-in
// "+ executed" suffix only on the Plan boundary.
func TestBuildCommitMessage_AllFourShapes(t *testing.T) {
	tests := []struct {
		name     string
		boundary string
		argName  string
		want     string
	}{
		{
			name:     "Plan boundary — only one with '+ executed' suffix",
			boundary: "plan",
			argName:  "03-foo",
			want:     "tide: plan 03-foo authored + executed",
		},
		{
			name:     "Phase boundary",
			boundary: "phase",
			argName:  "02-bar",
			want:     "tide: phase 02-bar authored",
		},
		{
			name:     "Milestone boundary",
			boundary: "milestone",
			argName:  "M-001",
			want:     "tide: milestone M-001 authored",
		},
		{
			name:     "Project boundary — no name suffix",
			boundary: "project",
			argName:  "",
			want:     "tide: project complete",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := buildCommitMessage(tt.boundary, tt.argName)
			if err != nil {
				t.Fatalf("buildCommitMessage(%q, %q): %v", tt.boundary, tt.argName, err)
			}
			if got != tt.want {
				t.Errorf("buildCommitMessage(%q, %q) = %q, want %q", tt.boundary, tt.argName, got, tt.want)
			}
		})
	}
}

// TestBuildCommitMessage_RejectsUnknownBoundary asserts unknown boundary
// names error out (e.g., "wave" — Tasks ship in their parent Plan's commit).
func TestBuildCommitMessage_RejectsUnknownBoundary(t *testing.T) {
	_, err := buildCommitMessage("wave", "w1")
	if err == nil {
		t.Fatal("buildCommitMessage accepted unknown boundary; expected error")
	}
}

// TestBuildCommitMessage_RejectsEmptyNameWhenRequired asserts the Plan,
// Phase, and Milestone boundaries reject empty names. Only "project"
// allows an empty name.
func TestBuildCommitMessage_RejectsEmptyNameWhenRequired(t *testing.T) {
	for _, boundary := range []string{"plan", "phase", "milestone"} {
		_, err := buildCommitMessage(boundary, "")
		if err == nil {
			t.Errorf("buildCommitMessage(%q, \"\") accepted empty name; expected error", boundary)
		}
	}
}

// TestBuildPushJobWithArtifacts asserts opts.ArtifactPaths is CSV-joined
// into a single --artifact-paths=<csv> arg (NOT repeated flags).
func TestBuildPushJobWithArtifacts(t *testing.T) {
	project := fixtureProject()
	scheme := schemeForTest(t)
	opts := PushOptions{
		TidePushImage: "ghcr.io/jsquirrelz/tide-push:test",
		Branch:        "tide/run-demo-1747200000",
		CommitMessage: "tide: plan 03-foo authored + executed",
		ArtifactPaths: []string{"artifacts/M-001/P-003/L-005/PLAN.md", "artifacts/M-001/P-003/L-005/SUMMARY.md"},
	}
	job := buildPushJob(project, "tide-projects", opts, scheme)
	args := job.Spec.Template.Spec.Containers[0].Args
	joined := strings.Join(args, " ")

	wants := []string{
		"--commit-message=tide: plan 03-foo authored + executed",
		"--artifact-paths=artifacts/M-001/P-003/L-005/PLAN.md,artifacts/M-001/P-003/L-005/SUMMARY.md",
	}
	for _, w := range wants {
		found := false
		for _, a := range args {
			if a == w {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Args missing %q (got: %s)", w, joined)
		}
	}
}

// ---------- Test 8: buildCloneJob args ----------

func TestBuildCloneJobArgs(t *testing.T) {
	project := fixtureProject()
	scheme := schemeForTest(t)
	opts := CloneOptions{TidePushImage: "ghcr.io/jsquirrelz/tide-push:test"}
	job := buildCloneJob(project, "tide-projects", opts, scheme)
	args := job.Spec.Template.Spec.Containers[0].Args
	joined := strings.Join(args, " ")
	wants := []string{
		"--mode=clone",
		"--repo-url=https://github.com/example/demo.git",
	}
	for _, w := range wants {
		found := false
		for _, a := range args {
			if a == w {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Clone args missing %q (got: %s)", w, joined)
		}
	}
}
