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
	"fmt"
	"testing"

	corev1 "k8s.io/api/core/v1"

	tidev1alpha3 "github.com/jsquirrelz/tide/api/v1alpha3"
)

// verifierScratchVolumeName aliases jobspec.go's VolumeVerifierScratch const
// (Phase 48 D-08) for readability in these assertions.
const verifierScratchVolumeName = VolumeVerifierScratch

// testGitCredsSecretName is a git-write/push credential secret name used only
// to prove its absence from the built Job — never a real secret.
const testGitCredsSecretName = "git-push-creds-secret"

// TestBuildJobSpec_Verifier_WorkspaceMountReadOnly verifies D-08: with
// ReadOnly:true, the subagent's PRIMARY /workspace mount is ReadOnly. Scoped
// by MountPath (not just Name) because Phase 51 TASK-04 adds a SECOND
// VolumeMount of the same VolumeProjectWorkspace volume (the RW
// envelopes/<uid>/ subPath mount at /workspace/envelopes/<uid>) — see
// TestBuildJobSpec_Verifier_RWEnvelopesSubPathMount in jobspec_test.go.
func TestBuildJobSpec_Verifier_WorkspaceMountReadOnly(t *testing.T) {
	opts := buildTestOptions()
	opts.ReadOnly = true
	job := BuildJobSpec(opts)
	containers := job.Spec.Template.Spec.Containers
	if len(containers) == 0 {
		t.Fatal("no containers in pod spec")
	}
	subagent := containers[0]
	var found bool
	for _, vm := range subagent.VolumeMounts {
		if vm.Name == VolumeProjectWorkspace && vm.MountPath == "/workspace" {
			found = true
			if !vm.ReadOnly {
				t.Errorf("subagent %q volumeMount ReadOnly = false; want true when opts.ReadOnly", VolumeProjectWorkspace)
			}
		}
	}
	if !found {
		t.Fatalf("subagent container missing primary volumeMount %q at /workspace", VolumeProjectWorkspace)
	}
}

// TestBuildJobSpec_Verifier_ScratchEmptyDirMounted verifies D-08: with
// ReadOnly:true, a verifier-scratch emptyDir volume exists and is mounted
// read-write at /scratch on the subagent container.
func TestBuildJobSpec_Verifier_ScratchEmptyDirMounted(t *testing.T) {
	opts := buildTestOptions()
	opts.ReadOnly = true
	job := BuildJobSpec(opts)
	spec := job.Spec.Template.Spec

	var scratchVolume *corev1.Volume
	for i := range spec.Volumes {
		if spec.Volumes[i].Name == verifierScratchVolumeName {
			scratchVolume = &spec.Volumes[i]
		}
	}
	if scratchVolume == nil {
		t.Fatalf("pod spec missing %q emptyDir volume when opts.ReadOnly", verifierScratchVolumeName)
	}
	if scratchVolume.EmptyDir == nil {
		t.Errorf("volume %q is not an emptyDir", verifierScratchVolumeName)
	}

	if len(spec.Containers) == 0 {
		t.Fatal("no containers in pod spec")
	}
	subagent := spec.Containers[0]
	var mount *corev1.VolumeMount
	for i := range subagent.VolumeMounts {
		if subagent.VolumeMounts[i].Name == verifierScratchVolumeName {
			mount = &subagent.VolumeMounts[i]
		}
	}
	if mount == nil {
		t.Fatalf("subagent container missing volumeMount %q", verifierScratchVolumeName)
	}
	if mount.MountPath != "/scratch" {
		t.Errorf("volumeMount %q MountPath = %q; want /scratch", verifierScratchVolumeName, mount.MountPath)
	}
	if mount.ReadOnly {
		t.Errorf("volumeMount %q ReadOnly = true; want false (read-write scratch for incidental writes)", verifierScratchVolumeName)
	}

	// WR-02: TMPDIR/HOME must redirect to the sole writable path so
	// httpx/anthropic/langchain don't hit EROFS under ReadOnlyRootFilesystem.
	var tmpdir, home string
	var foundTMPDIR, foundHOME bool
	for _, e := range subagent.Env {
		switch e.Name {
		case "TMPDIR":
			tmpdir, foundTMPDIR = e.Value, true
		case "HOME":
			home, foundHOME = e.Value, true
		}
	}
	if !foundTMPDIR || tmpdir != "/scratch" {
		t.Errorf("subagent env TMPDIR = %q (found=%v); want \"/scratch\" when opts.ReadOnly", tmpdir, foundTMPDIR)
	}
	if !foundHOME || home != "/scratch" {
		t.Errorf("subagent env HOME = %q (found=%v); want \"/scratch\" when opts.ReadOnly", home, foundHOME)
	}
}

// TestBuildJobSpec_Verifier_ReadOnlyRootFilesystem verifies D-08: with
// ReadOnly:true, the subagent container's SecurityContext.ReadOnlyRootFilesystem
// flips to true.
func TestBuildJobSpec_Verifier_ReadOnlyRootFilesystem(t *testing.T) {
	opts := buildTestOptions()
	opts.ReadOnly = true
	job := BuildJobSpec(opts)
	containers := job.Spec.Template.Spec.Containers
	if len(containers) == 0 {
		t.Fatal("no containers in pod spec")
	}
	subagent := containers[0]
	if subagent.SecurityContext == nil || subagent.SecurityContext.ReadOnlyRootFilesystem == nil {
		t.Fatal("subagent SecurityContext.ReadOnlyRootFilesystem is nil; want true when opts.ReadOnly")
	}
	if !*subagent.SecurityContext.ReadOnlyRootFilesystem {
		t.Error("subagent SecurityContext.ReadOnlyRootFilesystem = false; want true when opts.ReadOnly")
	}
}

// TestBuildJobSpec_Verifier_NoGitCredsInAnyContainer is a D-08 regression test:
// git-write/push credentials (Project.Spec.Git.CredsSecretRef) are already
// isolated to the separate tide-push Job (push_helpers.go) and MUST NOT appear
// in any BuildJobSpec container's Env or EnvFrom, regardless of ReadOnly.
func TestBuildJobSpec_Verifier_NoGitCredsInAnyContainer(t *testing.T) {
	for _, readOnly := range []bool{true, false} {
		t.Run(fmt.Sprintf("ReadOnly=%v", readOnly), func(t *testing.T) {
			opts := buildTestOptions()
			project := *opts.Project
			project.Spec.Git = &tidev1alpha3.GitConfig{
				RepoURL:        "https://github.com/example/repo",
				CredsSecretRef: testGitCredsSecretName,
			}
			opts.Project = &project
			opts.ReadOnly = readOnly

			job := BuildJobSpec(opts)
			spec := job.Spec.Template.Spec
			allContainers := make([]corev1.Container, 0, len(spec.InitContainers)+len(spec.Containers))
			allContainers = append(allContainers, spec.InitContainers...)
			allContainers = append(allContainers, spec.Containers...)
			for _, c := range allContainers {
				for _, e := range c.Env {
					if e.Value == testGitCredsSecretName {
						t.Errorf("container %q Env contains git creds secret name %q", c.Name, testGitCredsSecretName)
					}
				}
				for _, ef := range c.EnvFrom {
					if ef.SecretRef != nil && ef.SecretRef.Name == testGitCredsSecretName {
						t.Errorf("container %q EnvFrom references git creds secret %q", c.Name, testGitCredsSecretName)
					}
				}
			}
		})
	}
}

// TestBuildJobSpec_Verifier_DefaultUnchanged verifies D-08's non-regression
// requirement: ReadOnly:false (and the zero-value legacy shape) produces
// byte-identical behavior to pre-D-08 BuildJobSpec — RW workspace mount, no
// scratch volume, ReadOnlyRootFilesystem false.
func TestBuildJobSpec_Verifier_DefaultUnchanged(t *testing.T) {
	cases := map[string]BuildOptions{
		"explicit-false": func() BuildOptions {
			o := buildTestOptions()
			o.ReadOnly = false
			return o
		}(),
		"zero-value-legacy": buildTestOptions(), // ReadOnly unset == false
	}
	for name, opts := range cases {
		t.Run(name, func(t *testing.T) {
			job := BuildJobSpec(opts)
			spec := job.Spec.Template.Spec
			if len(spec.Containers) == 0 {
				t.Fatal("no containers in pod spec")
			}
			subagent := spec.Containers[0]

			for _, vm := range subagent.VolumeMounts {
				if vm.Name == VolumeProjectWorkspace && vm.ReadOnly {
					t.Errorf("%s: subagent workspace mount ReadOnly = true; want false (default RW)", name)
				}
				if vm.Name == verifierScratchVolumeName {
					t.Errorf("%s: unexpected %q mount present when ReadOnly=false", name, verifierScratchVolumeName)
				}
			}
			for _, v := range spec.Volumes {
				if v.Name == verifierScratchVolumeName {
					t.Errorf("%s: unexpected %q volume present when ReadOnly=false", name, verifierScratchVolumeName)
				}
			}
			for _, e := range subagent.Env {
				if e.Name == "TMPDIR" || e.Name == "HOME" {
					t.Errorf("%s: unexpected env var %q present when ReadOnly=false", name, e.Name)
				}
			}
			if subagent.SecurityContext == nil || subagent.SecurityContext.ReadOnlyRootFilesystem == nil {
				t.Fatalf("%s: subagent SecurityContext.ReadOnlyRootFilesystem is nil", name)
			}
			if *subagent.SecurityContext.ReadOnlyRootFilesystem {
				t.Errorf("%s: subagent SecurityContext.ReadOnlyRootFilesystem = true; want false (default)", name)
			}
		})
	}
}
