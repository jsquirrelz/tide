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

package dag

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

// alphaThroughThetaFixture is the canonical regression fixture pinned to the
// README spec's worked example. Any refactor of ComputeWaves must keep this
// exact (nodes, edges) → waves mapping (DAG-04).
//
// README spec edges (see README.md "Wave computation"):
//
//	α→δ, β→δ, γ→η, ζ→η, δ→ε, η→θ
//
// Expected wave structure:
//
//	W1: {α,β,γ,ζ}   — no upstream dependencies
//	W2: {δ,η}       — δ depends on α,β; η depends on γ,ζ
//	W3: {ε,θ}       — ε depends on δ; θ depends on η
func alphaThroughThetaFixture() (nodes []NodeID, edges []Edge, expected [][]NodeID) {
	nodes = []NodeID{"alpha", "beta", "gamma", "delta", "epsilon", "zeta", "eta", "theta"}
	edges = []Edge{
		{From: "alpha", To: "delta"},
		{From: "beta", To: "delta"},
		{From: "gamma", To: "eta"},
		{From: "zeta", To: "eta"},
		{From: "delta", To: "epsilon"},
		{From: "eta", To: "theta"},
	}
	expected = [][]NodeID{
		{"alpha", "beta", "gamma", "zeta"},
		{"delta", "eta"},
		{"epsilon", "theta"},
	}
	return
}

// TestComputeWaves runs the full table of regression cases as subtests so
// `go test -run TestComputeWaves/<Name>` selects an individual case.
// Each subtest is also exposed as a top-level TestComputeWaves_<Name>
// function below for `-run TestComputeWaves_<Name>` callers.
func TestComputeWaves(t *testing.T) {
	alphaNodes, alphaEdges, alphaWaves := alphaThroughThetaFixture()

	type tc struct {
		name      string
		nodes     []NodeID
		edges     []Edge
		want      [][]NodeID
		wantCycle []NodeID // non-nil → expect *CycleError with this InvolvedNodes
		wantErr   string   // substring; non-empty and wantCycle nil → expect plain error
	}
	cases := []tc{
		{
			name:  "AlphaThroughTheta",
			nodes: alphaNodes,
			edges: alphaEdges,
			want:  alphaWaves,
		},
		{
			name:  "EmptyGraph",
			nodes: []NodeID{},
			edges: []Edge{},
			want:  nil,
		},
		{
			name:  "SingleNode",
			nodes: []NodeID{"alpha"},
			edges: []Edge{},
			want:  [][]NodeID{{"alpha"}},
		},
		{
			name:  "FullyParallel",
			nodes: []NodeID{"a", "b", "c"},
			edges: []Edge{},
			want:  [][]NodeID{{"a", "b", "c"}},
		},
		{
			name:  "LinearChain",
			nodes: []NodeID{"a", "b", "c"},
			edges: []Edge{{From: "a", To: "b"}, {From: "b", To: "c"}},
			want:  [][]NodeID{{"a"}, {"b"}, {"c"}},
		},
		{
			name:      "CycleSimple",
			nodes:     []NodeID{"a", "b"},
			edges:     []Edge{{From: "a", To: "b"}, {From: "b", To: "a"}},
			wantCycle: []NodeID{"a", "b"},
		},
		{
			name:      "CycleWithIslands",
			nodes:     []NodeID{"a", "b", "c", "d"},
			edges:     []Edge{{From: "a", To: "b"}, {From: "c", To: "d"}, {From: "d", To: "c"}},
			wantCycle: []NodeID{"c", "d"},
		},
		{
			name:    "DependsOnNonexistent",
			nodes:   []NodeID{"a"},
			edges:   []Edge{{From: "a", To: "b"}},
			wantErr: "unknown node",
		},
		{
			name:  "DuplicateEdges",
			nodes: []NodeID{"a", "b"},
			edges: []Edge{{From: "a", To: "b"}, {From: "a", To: "b"}},
			want:  [][]NodeID{{"a"}, {"b"}},
		},
	}

	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			assertComputeWavesCase(t, c.nodes, c.edges, c.want, c.wantCycle, c.wantErr)
		})
	}
}

// assertComputeWavesCase is shared between the table-driven TestComputeWaves
// and the individually-named TestComputeWaves_* functions so a single
// failure produces only one diagnostic surface to debug.
func assertComputeWavesCase(t *testing.T, nodes []NodeID, edges []Edge, want [][]NodeID, wantCycle []NodeID, wantErr string) {
	t.Helper()
	got, err := ComputeWaves(nodes, edges)

	if wantCycle != nil {
		var ce *CycleError
		if !errors.As(err, &ce) {
			t.Fatalf("ComputeWaves: expected *CycleError, got %v (type %T)", err, err)
		}
		if !reflect.DeepEqual(ce.InvolvedNodes, wantCycle) {
			t.Fatalf("CycleError.InvolvedNodes = %v, want %v", ce.InvolvedNodes, wantCycle)
		}
		// Error() string must mention each involved node (DAG-02).
		for _, n := range wantCycle {
			if !strings.Contains(ce.Error(), n) {
				t.Fatalf("CycleError.Error() = %q, expected to contain node %q", ce.Error(), n)
			}
		}
		if got != nil {
			t.Fatalf("ComputeWaves returned non-nil waves alongside CycleError: %v", got)
		}
		return
	}

	if wantErr != "" {
		if err == nil {
			t.Fatalf("ComputeWaves: expected error containing %q, got nil", wantErr)
		}
		var ce *CycleError
		if errors.As(err, &ce) {
			t.Fatalf("ComputeWaves: expected plain error, got *CycleError: %v", err)
		}
		if !strings.Contains(err.Error(), wantErr) {
			t.Fatalf("ComputeWaves error = %q, expected to contain %q", err.Error(), wantErr)
		}
		return
	}

	if err != nil {
		t.Fatalf("ComputeWaves: unexpected error: %v", err)
	}
	if !equalWaves(got, want) {
		t.Fatalf("ComputeWaves waves mismatch\n got:  %v\n want: %v", got, want)
	}
}

// equalWaves compares two wave sequences. A nil and a zero-length result
// are treated as equivalent (the empty-graph case).
func equalWaves(a, b [][]NodeID) bool {
	if len(a) == 0 && len(b) == 0 {
		return true
	}
	return reflect.DeepEqual(a, b)
}

// Below: each subtest from the table re-exposed as a top-level test function
// so `go test -run TestComputeWaves_<Name>` selects it directly. The plan's
// acceptance criteria use this naming pattern.

func TestComputeWaves_AlphaThroughTheta(t *testing.T) {
	nodes, edges, want := alphaThroughThetaFixture()
	assertComputeWavesCase(t, nodes, edges, want, nil, "")
}

func TestComputeWaves_EmptyGraph(t *testing.T) {
	assertComputeWavesCase(t, []NodeID{}, []Edge{}, nil, nil, "")
}

func TestComputeWaves_SingleNode(t *testing.T) {
	assertComputeWavesCase(t, []NodeID{"alpha"}, []Edge{}, [][]NodeID{{"alpha"}}, nil, "")
}

func TestComputeWaves_FullyParallel(t *testing.T) {
	assertComputeWavesCase(t,
		[]NodeID{"a", "b", "c"}, []Edge{},
		[][]NodeID{{"a", "b", "c"}}, nil, "")
}

func TestComputeWaves_LinearChain(t *testing.T) {
	assertComputeWavesCase(t,
		[]NodeID{"a", "b", "c"},
		[]Edge{{From: "a", To: "b"}, {From: "b", To: "c"}},
		[][]NodeID{{"a"}, {"b"}, {"c"}}, nil, "")
}

func TestComputeWaves_CycleSimple(t *testing.T) {
	assertComputeWavesCase(t,
		[]NodeID{"a", "b"},
		[]Edge{{From: "a", To: "b"}, {From: "b", To: "a"}},
		nil, []NodeID{"a", "b"}, "")
}

func TestComputeWaves_CycleWithIslands(t *testing.T) {
	assertComputeWavesCase(t,
		[]NodeID{"a", "b", "c", "d"},
		[]Edge{{From: "a", To: "b"}, {From: "c", To: "d"}, {From: "d", To: "c"}},
		nil, []NodeID{"c", "d"}, "")
}

func TestComputeWaves_DependsOnNonexistent(t *testing.T) {
	assertComputeWavesCase(t,
		[]NodeID{"a"},
		[]Edge{{From: "a", To: "b"}},
		nil, nil, "unknown node")
}

func TestComputeWaves_DuplicateEdges(t *testing.T) {
	assertComputeWavesCase(t,
		[]NodeID{"a", "b"},
		[]Edge{{From: "a", To: "b"}, {From: "a", To: "b"}},
		[][]NodeID{{"a"}, {"b"}}, nil, "")
}

// TestComputeWaves_Determinism asserts that within-wave node ordering is
// stable across repeated invocations against the α…θ fixture. Map iteration
// order in Go is intentionally randomised, so without the explicit
// sort.Strings calls in ComputeWaves the output order would drift between
// runs. 100 iterations is comfortably more than the period at which the
// runtime varies map seeds.
func TestComputeWaves_Determinism(t *testing.T) {
	nodes, edges, want := alphaThroughThetaFixture()

	first, err := ComputeWaves(nodes, edges)
	if err != nil {
		t.Fatalf("ComputeWaves: %v", err)
	}
	if !reflect.DeepEqual(first, want) {
		t.Fatalf("first run waves mismatch\n got:  %v\n want: %v", first, want)
	}

	for i := 0; i < 100; i++ {
		got, err := ComputeWaves(nodes, edges)
		if err != nil {
			t.Fatalf("iteration %d: ComputeWaves: %v", i, err)
		}
		if !reflect.DeepEqual(got, first) {
			t.Fatalf("iteration %d: nondeterministic output\n got:   %v\n first: %v", i, got, first)
		}
	}
}
