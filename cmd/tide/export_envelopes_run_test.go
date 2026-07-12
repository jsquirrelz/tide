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

package main

import (
	"context"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	tidev1alpha3 "github.com/jsquirrelz/tide/api/v1alpha3"
	pkgbundle "github.com/jsquirrelz/tide/pkg/bundle"
)

// TestBuildExportInspectorPodSpec_SubPath (GAP-13) pins that the export inspector
// pod mounts the per-project PVC at SubPath "<UID>/workspace" — matching the init
// Job, import Job, loader pod, and reporter. With only "<UID>" the envelopes/ tree
// sat at /workspace/workspace/envelopes, so `tar -C /workspace envelopes/` found
// nothing and the pod exited 1 ("failed before streaming"), failing the round-trip.
func TestBuildExportInspectorPodSpec_SubPath(t *testing.T) {
	const uid = "80e443c9-3c44-4f68-8775-5105200ddb5c"
	pod := buildExportInspectorPodSpec("tide-export-abcd1234", "proj-ns", uid, "tide-projects")

	mounts := pod.Spec.Containers[0].VolumeMounts
	if len(mounts) != 1 {
		t.Fatalf("inspector container has %d volume mounts, want 1", len(mounts))
	}
	want := uid + "/workspace"
	if mounts[0].SubPath != want {
		t.Errorf("inspector mount SubPath = %q, want %q (per-project PVC layout is <UID>/workspace/envelopes)", mounts[0].SubPath, want)
	}
	if !mounts[0].ReadOnly {
		t.Errorf("inspector mount must be ReadOnly (T-29-02-01 confinement)")
	}
	if mp := mounts[0].MountPath; mp != "/workspace" {
		t.Errorf("inspector MountPath = %q, want /workspace (tar runs -C /workspace)", mp)
	}
}

// TestAssembleBundleFiles_ProjectTypeMeta (GAP-16) pins that the exported
// project.yaml carries apiVersion + kind. The controller-runtime typed client
// strips TypeMeta on Get, so without an explicit re-stamp the round-trip
// `kubectl apply project.yaml` fails validation ("apiVersion not set, kind not
// set").
func TestAssembleBundleFiles_ProjectTypeMeta(t *testing.T) {
	proj := &tidev1alpha3.Project{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "p1",
			Namespace: "ns1",
			UID:       types.UID("u-1"),
		},
	}
	c := fake.NewClientBuilder().WithScheme(testScheme(t)).WithObjects(proj).Build()

	files, err := assembleBundleFiles(context.Background(), c, "ns1", "p1", "u-1", &pkgbundle.BundleManifest{}, map[string][]byte{})
	if err != nil {
		t.Fatalf("assembleBundleFiles: %v", err)
	}
	py := string(files[pkgbundle.BundleFileProject])
	if !strings.Contains(py, "apiVersion: tideproject.k8s/v1alpha3") {
		t.Errorf("project.yaml missing apiVersion; got:\n%s", py)
	}
	if !strings.Contains(py, "kind: Project") {
		t.Errorf("project.yaml missing kind; got:\n%s", py)
	}
	// GAP-17: the exported project.yaml must be namespace-portable — no baked-in
	// origin namespace, else `kubectl apply -n <other-ns>` fails with a mismatch.
	if strings.Contains(py, "namespace: ns1") {
		t.Errorf("project.yaml must not bind to the origin namespace; got:\n%s", py)
	}
}
