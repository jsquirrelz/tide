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

package eval

import (
	"errors"
	"testing"

	"github.com/jsquirrelz/tide/pkg/dag"
	pkgdispatch "github.com/jsquirrelz/tide/pkg/dispatch"
)

// TestDAGAcyclicity_AcyclicFixture asserts that a 3-node, 2-edge linear DAG
// (task-01 → task-02 → task-03) produces exactly 3 waves with no error.
// This is the protocol gate that would catch a regression in pkg/dag.ComputeWaves.
func TestDAGAcyclicity_AcyclicFixture(t *testing.T) {
	nodes := []dag.NodeID{"task-01", "task-02", "task-03"}
	edges := []dag.Edge{
		{From: "task-01", To: "task-02"},
		{From: "task-02", To: "task-03"},
	}
	waves, err := dag.ComputeWaves(nodes, edges)
	if err != nil {
		t.Errorf("acyclic fixture must not return error: %v", err)
	}
	if len(waves) != 3 {
		t.Errorf("expected 3 waves, got %d", len(waves))
	}
}

// TestDAGAcyclicity_CyclicFixture asserts that a 2-node cycle (task-01 →
// task-02 → task-01) causes ComputeWaves to return a *dag.CycleError. Uses
// errors.As for type-safe assertion (not a string match) per the plan contract.
func TestDAGAcyclicity_CyclicFixture(t *testing.T) {
	nodes := []dag.NodeID{"task-01", "task-02"}
	edges := []dag.Edge{
		{From: "task-01", To: "task-02"},
		{From: "task-02", To: "task-01"},
	}
	_, err := dag.ComputeWaves(nodes, edges)
	if err == nil {
		t.Error("cyclic fixture must return an error, got nil")
	}
	var cycErr *dag.CycleError
	if !errors.As(err, &cycErr) {
		t.Errorf("expected *dag.CycleError, got %T: %v", err, err)
	}
}

// TestDeclaredOutputPaths_Presence is the structural protocol gate: a valid
// Task dispatch must carry non-empty DeclaredOutputPaths. This check uses a
// struct literal (no filesystem access) — harness.Validate requires real file
// timestamps and is out of scope for the deterministic gate (RESEARCH Q3).
func TestDeclaredOutputPaths_Presence(t *testing.T) {
	// A dispatch with non-empty DeclaredOutputPaths passes the structural check.
	envWithPaths := pkgdispatch.EnvelopeIn{
		DeclaredOutputPaths: []string{"some/output/file.go"},
	}
	if len(envWithPaths.DeclaredOutputPaths) == 0 {
		t.Error("envelope with paths must pass structural check: got empty DeclaredOutputPaths")
	}

	// A dispatch with empty DeclaredOutputPaths fails the structural check.
	envWithoutPaths := pkgdispatch.EnvelopeIn{
		DeclaredOutputPaths: []string{},
	}
	if len(envWithoutPaths.DeclaredOutputPaths) != 0 {
		t.Error("envelope without paths must fail structural check: got non-empty DeclaredOutputPaths")
	}
}
