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

// import_jobspec_test.go — unit tests for BuildImportJob (Phase 28). These
// exercise the REAL Job spec (the envtest path always set ImportImage="" and
// took the dev-skip, so the production Job was never asserted). Each test pins
// one of the three dispatch-path defects CR-01/02/03 + WR-04 so they cannot
// regress.
package controller_test

import (
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	tideprojectv1alpha2 "github.com/jsquirrelz/tide/api/v1alpha2"
	"github.com/jsquirrelz/tide/internal/controller"
)

// newImportTestProject builds a Project fixture for the import-Job tests.
func newImportTestProject() *tideprojectv1alpha2.Project {
	return &tideprojectv1alpha2.Project{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "proj-import",
			Namespace: "ns-import",
			UID:       "project-uid-import",
		},
	}
}

func newImportTestOpts() controller.ImportJobOptions {
	return controller.ImportJobOptions{
		ImportImage:   "ghcr.io/jsquirrelz/tide-import:v0.1.0-dev",
		SharedPVCName: "tide-projects",
		OldSubPath:    "old-project-uid/workspace",
		NewSubPath:    "new-project-uid/workspace",
		RekeyCMName:   "tide-import-rekey-project-uid-import",
	}
}

// TestBuildImportJob_ArgsAgainstEntrypoint asserts CR-01: the container runs the
// binary via the image ENTRYPOINT (Command nil, no sh/-c/cat) and passes
// --rekey-file=/rekey/rekey.json through Args. The distroless base has no shell,
// so any Command referencing sh/cat would crashloop the Job.
func TestBuildImportJob_ArgsAgainstEntrypoint(t *testing.T) {
	job := controller.BuildImportJob(newImportTestProject(), newImportTestOpts(), newTestScheme())

	containers := job.Spec.Template.Spec.Containers
	if len(containers) != 1 {
		t.Fatalf("expected exactly 1 container, got %d", len(containers))
	}
	c := containers[0]

	if c.Command != nil {
		t.Errorf("expected Command nil (use ENTRYPOINT), got %v", c.Command)
	}

	// No shell or coreutils may appear anywhere in Command or Args.
	for _, tok := range append(append([]string{}, c.Command...), c.Args...) {
		if tok == "sh" || tok == "-c" || tok == "cat" || strings.Contains(tok, "cat ") {
			t.Errorf("found shell/coreutils token %q in container exec; distroless base has no shell", tok)
		}
	}

	var sawRekeyFlag bool
	for _, a := range c.Args {
		if a == "--rekey-file=/rekey/rekey.json" {
			sawRekeyFlag = true
		}
	}
	if !sawRekeyFlag {
		t.Errorf("expected Args to contain --rekey-file=/rekey/rekey.json, got %v", c.Args)
	}
}

// TestBuildImportJob_RekeyMount asserts CR-03: the rekey ConfigMap volume mounts
// at /rekey, so the file the --rekey-file flag references (/rekey/rekey.json)
// exists once the CM key is "rekey.json". The CM key/data is asserted by the
// controller test; here we pin the mount path the Args depend on.
func TestBuildImportJob_RekeyMount(t *testing.T) {
	job := controller.BuildImportJob(newImportTestProject(), newImportTestOpts(), newTestScheme())
	c := job.Spec.Template.Spec.Containers[0]

	var rekeyMount *corev1.VolumeMount
	for i := range c.VolumeMounts {
		if c.VolumeMounts[i].MountPath == "/rekey" {
			rekeyMount = &c.VolumeMounts[i]
		}
	}
	if rekeyMount == nil {
		t.Fatalf("expected a VolumeMount at /rekey, got mounts %v", c.VolumeMounts)
	}

	// The /rekey mount must resolve to a ConfigMap volume naming the rekey CM.
	var sawRekeyCMVolume bool
	for _, v := range job.Spec.Template.Spec.Volumes {
		if v.Name == rekeyMount.Name {
			if v.ConfigMap == nil {
				t.Fatalf("rekey volume %q is not backed by a ConfigMap", v.Name)
			}
			if v.ConfigMap.Name != "tide-import-rekey-project-uid-import" {
				t.Errorf("rekey ConfigMap name = %q, want tide-import-rekey-project-uid-import", v.ConfigMap.Name)
			}
			sawRekeyCMVolume = true
		}
	}
	if !sawRekeyCMVolume {
		t.Errorf("no ConfigMap volume found for rekey mount %q", rekeyMount.Name)
	}
}

// TestBuildImportJob_PVCSubPathMounts asserts the two PVC subPath mounts: old
// read-only, new read-write — both off the shared PVC, never the root (IMPORT-05
// containment). Pins the mount contract the binary reads/writes against.
func TestBuildImportJob_PVCSubPathMounts(t *testing.T) {
	job := controller.BuildImportJob(newImportTestProject(), newImportTestOpts(), newTestScheme())
	c := job.Spec.Template.Spec.Containers[0]

	var oldMount, newMount *corev1.VolumeMount
	for i := range c.VolumeMounts {
		switch c.VolumeMounts[i].MountPath {
		case "/old-workspace":
			oldMount = &c.VolumeMounts[i]
		case "/new-workspace":
			newMount = &c.VolumeMounts[i]
		}
	}
	if oldMount == nil || newMount == nil {
		t.Fatalf("expected /old-workspace and /new-workspace mounts, got %v", c.VolumeMounts)
	}
	if !oldMount.ReadOnly {
		t.Errorf("/old-workspace must be ReadOnly (source envelopes are never written)")
	}
	if newMount.ReadOnly {
		t.Errorf("/new-workspace must be writable (destination for rekey'd envelopes)")
	}
	if oldMount.SubPath != "old-project-uid/workspace" {
		t.Errorf("/old-workspace SubPath = %q, want old-project-uid/workspace", oldMount.SubPath)
	}
	if newMount.SubPath != "new-project-uid/workspace" {
		t.Errorf("/new-workspace SubPath = %q, want new-project-uid/workspace", newMount.SubPath)
	}
	if oldMount.Name == "" || oldMount.Name != newMount.Name {
		t.Errorf("both subPath mounts must reference the same PVC volume (containment); got %q and %q",
			oldMount.Name, newMount.Name)
	}
}

// TestBuildImportJob_FSGroup asserts GAP-6: the pod sets PodSecurityContext
// {FSGroup:1000, RunAsUser:65532, RunAsGroup:1000} so kubelet chowns the RW
// new-workspace PVC mount at startup and the nonroot tide-import binary can
// MkdirAll the new-UID envelope tree (without it: "mkdir ...: permission denied").
func TestBuildImportJob_FSGroup(t *testing.T) {
	job := controller.BuildImportJob(newImportTestProject(), newImportTestOpts(), newTestScheme())
	sc := job.Spec.Template.Spec.SecurityContext
	if sc == nil {
		t.Fatal("BuildImportJob: pod spec has no SecurityContext (expected FSGroup=1000)")
	}
	if sc.FSGroup == nil || *sc.FSGroup != 1000 {
		t.Errorf("BuildImportJob: SecurityContext.FSGroup = %v, want 1000 (kubelet chowns the RW PVC mount)", sc.FSGroup)
	}
	if sc.RunAsGroup == nil || *sc.RunAsGroup != 1000 {
		t.Errorf("BuildImportJob: SecurityContext.RunAsGroup = %v, want 1000 (primary gid matching FSGroup)", sc.RunAsGroup)
	}
	if sc.RunAsUser == nil || *sc.RunAsUser != 65532 {
		t.Errorf("BuildImportJob: SecurityContext.RunAsUser = %v, want 65532 (distroless nonroot; required alongside RunAsGroup)", sc.RunAsUser)
	}
}

// TestBuildImportJob_ActiveDeadline asserts WR-04: a bounded ActiveDeadlineSeconds
// is set so a hung copy cannot pin the RW PVC mount indefinitely.
func TestBuildImportJob_ActiveDeadline(t *testing.T) {
	job := controller.BuildImportJob(newImportTestProject(), newImportTestOpts(), newTestScheme())
	if job.Spec.ActiveDeadlineSeconds == nil {
		t.Fatalf("expected ActiveDeadlineSeconds to be set (WR-04)")
	}
	if *job.Spec.ActiveDeadlineSeconds <= 0 {
		t.Errorf("ActiveDeadlineSeconds = %d, want a positive bound", *job.Spec.ActiveDeadlineSeconds)
	}
}
