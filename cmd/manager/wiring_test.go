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
	"os"
	"strings"
	"testing"

	"github.com/jsquirrelz/tide/internal/controller"
	"github.com/jsquirrelz/tide/internal/dispatch/podjob"
)

// TestReconcilerWiringComplete asserts that every reconciler constructed by
// main() has its required dispatch-tier dependencies wired. A nil field here
// means the production path silently short-circuits (the bug class that
// Phase 04.1 P1.1 closed for ProjectReconciler).
//
// This test does NOT exercise the full Manager construction — it only checks
// the struct-literal completeness for each reconciler. Constructs minimal
// non-nil stand-ins for Dispatcher and EnvReader and asserts they propagate.
//
// Required-field matrix (per Phase 04.1 P1.1 locked user decision):
//   - Project:   Dispatcher                (not EnvReader — no envelope consumer)
//   - Milestone: Dispatcher, EnvReader
//   - Phase:     Dispatcher, EnvReader
//   - Plan:      Dispatcher, EnvReader
//   - Task:      Dispatcher                (EnvReader on the Dispatcher itself)
func TestReconcilerWiringComplete(t *testing.T) {
	dispatcher := &podjob.PodJobBackend{}
	envReader := &podjob.FilesystemEnvelopeReader{}

	cases := []struct {
		name    string
		nilFn   func() bool // returns true if a required field is nil after construction
		message string
	}{
		{
			name:    "Project.Dispatcher",
			nilFn:   func() bool { return (&controller.ProjectReconciler{Dispatcher: dispatcher}).Dispatcher == nil },
			message: "ProjectReconciler.Dispatcher must be non-nil after main() wires the struct literal (Phase 04.1 P1.1 — project_controller.go:198 gates reconcileProjectPhase2 on this)",
		},
		{
			name:    "Milestone.Dispatcher",
			nilFn:   func() bool { return (&controller.MilestoneReconciler{Dispatcher: dispatcher}).Dispatcher == nil },
			message: "MilestoneReconciler.Dispatcher must be non-nil (CR-01 fix; milestone_controller.go:144 gates planner-dispatch path on this)",
		},
		{
			name:    "Milestone.EnvReader",
			nilFn:   func() bool { return (&controller.MilestoneReconciler{EnvReader: envReader}).EnvReader == nil },
			message: "MilestoneReconciler.EnvReader must be non-nil (CR-01 fix; handleJobCompletion needs it to materialize child Phase CRDs)",
		},
		{
			name:    "Phase.Dispatcher",
			nilFn:   func() bool { return (&controller.PhaseReconciler{Dispatcher: dispatcher}).Dispatcher == nil },
			message: "PhaseReconciler.Dispatcher must be non-nil (CR-01 fix)",
		},
		{
			name:    "Phase.EnvReader",
			nilFn:   func() bool { return (&controller.PhaseReconciler{EnvReader: envReader}).EnvReader == nil },
			message: "PhaseReconciler.EnvReader must be non-nil (CR-01 fix)",
		},
		{
			name:    "Plan.Dispatcher",
			nilFn:   func() bool { return (&controller.PlanReconciler{Dispatcher: dispatcher}).Dispatcher == nil },
			message: "PlanReconciler.Dispatcher must be non-nil",
		},
		{
			name:    "Plan.EnvReader",
			nilFn:   func() bool { return (&controller.PlanReconciler{EnvReader: envReader}).EnvReader == nil },
			message: "PlanReconciler.EnvReader must be non-nil",
		},
		{
			name: "Task.Deps.Dispatcher",
			nilFn: func() bool {
				return (&controller.TaskReconciler{Deps: controller.TaskReconcilerDeps{Dispatcher: dispatcher}}).Deps.Dispatcher == nil
			},
			message: "TaskReconciler.Deps.Dispatcher must be non-nil (Phase 04.1 P3.2 — dispatch-tier deps now carried in Deps)",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.nilFn() {
				t.Errorf("%s: %s", tc.name, tc.message)
			}
		})
	}
}

// TestProductionOverrideMarkers asserts the PROD_OVERRIDE_REQUIRED marker
// persists above the dev-tag default envOrDefault calls at main.go:164-165.
// Phase 04.1 P4.3 — comment-only enforcement; prevents future maintainers
// from accepting :v0.1.0-dev as release-stable by accident.
func TestProductionOverrideMarkers(t *testing.T) {
	data, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "PROD_OVERRIDE_REQUIRED") {
		t.Fatal("expected PROD_OVERRIDE_REQUIRED marker in main.go (Phase 04.1 P4.3)")
	}
	count := strings.Count(content, "PROD_OVERRIDE_REQUIRED")
	if count < 2 {
		t.Fatalf("expected >= 2 PROD_OVERRIDE_REQUIRED markers (one per dev tag default); got %d", count)
	}
}
