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

// Plan 23-03 Task 1 — SCHEMA-03 old-object fail-closed guard tests, generalized
// under Phase 40 Plan 40-03 (D-04) to the version-agnostic guard shape.
//
// TestOldShapeRejection verifies that the ProjectReconciler schema-revision guard
// rejects any Project whose SchemaRevision does not equal expectedSchemaRevision
// ("v1alpha3") — an empty or wrong-value discriminator signals an object authored
// under a prior schema revision that slipped into etcd before the CRD upgrade.
// The guard must set a RequiresReinstall condition and return reconcile.TerminalError
// so the reconciler never runs and never requeue-storms.
//
// These are pure Go tests (no envtest/Ginkgo) using the fake controller-runtime
// client so they run fast without a live cluster. No cross-version scheme
// registration is needed — the "old shape" is simulated by constructing a
// current-type Project with an empty or stale SchemaRevision string directly.
package controller

import (
	"context"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	tidev1alpha3 "github.com/jsquirrelz/tide/api/v1alpha3"
)

// v2GuardScheme returns a runtime.Scheme with the current TIDE API version
// registered for the guard tests.
func v2GuardScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	if err := tidev1alpha3.AddToScheme(s); err != nil {
		t.Fatalf("AddToScheme v1alpha3: %v", err)
	}
	return s
}

// TestOldShapeRejection verifies that a Project with an absent SchemaRevision
// triggers the fail-closed guard: RequiresReinstall condition set, blocked=true
// returned, no dispatch.
func TestOldShapeRejection(t *testing.T) {
	ctx := context.Background()
	s := v2GuardScheme(t)

	// Construct a Project with SchemaRevision empty (simulates an object
	// authored under a prior schema revision that slipped through — the
	// Required CEL rule blocks new applies but an etcd-stranded object can
	// still reach the reconciler).
	proj := &tidev1alpha3.Project{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "old-project",
			Namespace: "default",
		},
		Spec: tidev1alpha3.ProjectSpec{
			SchemaRevision: "", // absent = prior-schema-revision signal
			TargetRepo:     "https://github.com/example/repo.git",
		},
	}

	fc := fake.NewClientBuilder().
		WithScheme(s).
		WithObjects(proj).
		WithStatusSubresource(proj).
		Build()

	r := &ProjectReconciler{
		Client: fc,
		Scheme: s,
	}

	blocked, err := r.checkSchemaRevisionGuard(ctx, proj)
	if err == nil {
		t.Fatal("checkSchemaRevisionGuard: expected TerminalError for old-shape Project, got nil")
	}
	if !blocked {
		t.Fatal("checkSchemaRevisionGuard: expected blocked=true for old-shape Project")
	}

	// Fetch the updated project status and verify the RequiresReinstall condition.
	updated := &tidev1alpha3.Project{}
	if getErr := fc.Get(ctx, client.ObjectKey{Name: "old-project", Namespace: "default"}, updated); getErr != nil {
		t.Fatalf("Get updated project: %v", getErr)
	}

	var found bool
	for _, c := range updated.Status.Conditions {
		if c.Type == tidev1alpha3.ConditionReady &&
			c.Reason == tidev1alpha3.ReasonRequiresReinstall &&
			c.Status == metav1.ConditionFalse {
			found = true
			if !strings.Contains(c.Message, "v1alpha3") {
				t.Errorf("RequiresReinstall message = %q; want it to mention v1alpha3", c.Message)
			}
			if !strings.Contains(c.Message, "docs/migration/v1alpha2-to-v1alpha3.md") {
				t.Errorf("RequiresReinstall message = %q; want it to reference the migration doc path", c.Message)
			}
			break
		}
	}
	if !found {
		t.Errorf("expected RequiresReinstall condition (Ready=False) on Project status; conditions = %v",
			updated.Status.Conditions)
	}
}

// TestOldShapeRejection_WrongRevisionBlocks verifies that a Project carrying a
// stale (non-current) SchemaRevision value also trips the guard — not just an
// empty string.
func TestOldShapeRejection_WrongRevisionBlocks(t *testing.T) {
	ctx := context.Background()
	s := v2GuardScheme(t)

	proj := &tidev1alpha3.Project{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stale-project",
			Namespace: "default",
		},
		Spec: tidev1alpha3.ProjectSpec{
			SchemaRevision: "v1alpha2", // wrong-value case — one crank behind
			TargetRepo:     "https://github.com/example/repo.git",
		},
	}

	fc := fake.NewClientBuilder().
		WithScheme(s).
		WithObjects(proj).
		WithStatusSubresource(proj).
		Build()

	r := &ProjectReconciler{
		Client: fc,
		Scheme: s,
	}

	blocked, err := r.checkSchemaRevisionGuard(ctx, proj)
	if err == nil {
		t.Fatal("checkSchemaRevisionGuard: expected TerminalError for stale-revision Project, got nil")
	}
	if !blocked {
		t.Fatal("checkSchemaRevisionGuard: expected blocked=true for stale-revision Project")
	}
}

// TestOldShapeRejection_CurrentShapePasses verifies that a Project with
// SchemaRevision="v1alpha3" passes the schema guard without setting
// the RequiresReinstall condition.
func TestOldShapeRejection_CurrentShapePasses(t *testing.T) {
	ctx := context.Background()
	s := v2GuardScheme(t)

	// Create a fully-valid v1alpha3 Project with SchemaRevision set.
	proj := &tidev1alpha3.Project{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "new-project",
			Namespace: "default",
		},
		Spec: tidev1alpha3.ProjectSpec{
			SchemaRevision: "v1alpha3",
			TargetRepo:     "https://github.com/example/repo.git",
		},
	}

	fc := fake.NewClientBuilder().
		WithScheme(s).
		WithObjects(proj).
		WithStatusSubresource(proj).
		Build()

	r := &ProjectReconciler{
		Client: fc,
		Scheme: s,
	}

	blocked, err := r.checkSchemaRevisionGuard(ctx, proj)
	if blocked {
		t.Errorf("checkSchemaRevisionGuard: expected blocked=false for v1alpha3-shaped Project; err=%v", err)
	}
	// No RequiresReinstall condition should be set.
	updated := &tidev1alpha3.Project{}
	if getErr := fc.Get(ctx, client.ObjectKey{Name: "new-project", Namespace: "default"}, updated); getErr != nil {
		// Project status not updated (guard passed) — that's fine.
		return
	}
	for _, c := range updated.Status.Conditions {
		if c.Reason == tidev1alpha3.ReasonRequiresReinstall {
			t.Errorf("v1alpha3-shaped Project got RequiresReinstall condition; should have passed the guard")
			return
		}
	}
}
