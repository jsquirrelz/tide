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

package v1alpha3

import (
	"fmt"
	"strings"

	tideprojectv1alpha3 "github.com/jsquirrelz/tide/api/v1alpha3"
)

// FileTouchMismatchPair records a pair of Tasks that share an EXACT file path
// without a declared dependsOn edge between them. (Pitfall G: same-directory
// siblings — e.g. foo.go + foo_test.go — do NOT generate derived edges because
// they share only a directory prefix, not an exact path.)
//
// Exported so PlanReconciler can call ComputeFileTouchMismatches and use the
// result without importing private types from this package.
type FileTouchMismatchPair struct {
	TaskA      string
	TaskB      string
	SharedPath string // the EXACT path shared between TaskA.filesTouched and TaskB.filesTouched
}

// ComputeFileTouchMismatches returns pairs of Tasks (a, b) where their
// filesTouched sets overlap on EXACT path equality AND no declared within-plan
// dependsOn edge exists in either direction.
//
// Exported so the PlanReconciler dispatch gate (D-05) can call it after Tasks
// materialize — the webhook remains the early-admission layer; the reconciler
// is the authoritative seat that sees reporter-flow Tasks.
//
// Cross-scope deps are ignored for the mismatch check (only within-plan edge
// presence clears the mismatch flag; a cross-scope dep on the same task is a
// separate concern handled by the global assembler in Phase 24).
//
// Algorithm (EXACT-equality only — Pitfall G defense):
//  1. Build a name → declared-dependsOn set for O(1) edge lookup.
//  2. For each pair (a, b) with a.Name < b.Name (lexicographic — avoids duplicates):
//     - Compute exact intersection of a.FilesTouched ∩ b.FilesTouched.
//     - If empty → skip (no overlap).
//     - If b.Name in a.DependsOn OR a.Name in b.DependsOn → declared edge; skip.
//     - Else → append one FileTouchMismatchPair per shared path.
//  3. Return the list.
//
// Complexity: O(N² × P) where N = task count, P = average filesTouched length.
// Acceptable for v1 Plans bounded to ≤20 Tasks per RESEARCH.md.
func ComputeFileTouchMismatches(tasks []tideprojectv1alpha3.Task) []FileTouchMismatchPair {
	// Build name → dependsOn set for fast lookup.
	dependsOnSet := make(map[string]map[string]struct{}, len(tasks))
	for i := range tasks {
		t := &tasks[i]
		deps := make(map[string]struct{}, len(t.Spec.DependsOn))
		for _, d := range t.Spec.DependsOn {
			deps[d] = struct{}{}
		}
		dependsOnSet[t.Name] = deps
	}

	var mismatches []FileTouchMismatchPair

	for i := range tasks {
		for j := i + 1; j < len(tasks); j++ {
			a := &tasks[i]
			b := &tasks[j]

			// Canonical ordering: ensure a.Name < b.Name to avoid duplicate pairs.
			if a.Name > b.Name {
				a, b = b, a
			}

			// Compute EXACT intersection of filesTouched.
			// Pitfall G: "pkg/x/y.go" and "pkg/x/y_test.go" are different strings —
			// they do NOT intersect. Only identical path strings match.
			bFiles := make(map[string]struct{}, len(b.Spec.FilesTouched))
			for _, f := range b.Spec.FilesTouched {
				bFiles[f] = struct{}{}
			}

			var shared []string
			for _, f := range a.Spec.FilesTouched {
				if _, ok := bFiles[f]; ok {
					shared = append(shared, f)
				}
			}

			if len(shared) == 0 {
				continue
			}

			// Check for declared dependsOn edge in either direction.
			if _, depAtoB := dependsOnSet[b.Name][a.Name]; depAtoB {
				continue // b depends on a — declared; no mismatch
			}
			if _, depBtoA := dependsOnSet[a.Name][b.Name]; depBtoA {
				continue // a depends on b — declared; no mismatch
			}

			// Undeclared overlap: record one entry per shared path.
			for _, p := range shared {
				mismatches = append(mismatches, FileTouchMismatchPair{
					TaskA:      a.Name,
					TaskB:      b.Name,
					SharedPath: p,
				})
			}
		}
	}

	return mismatches
}

// SummariseMismatches returns a compact human-readable string of all mismatches
// for use in error messages and K8s Events.
//
// Exported so PlanReconciler can build the condition Message that names both
// tasks and the shared path (T-15-07 mitigation).
func SummariseMismatches(mismatches []FileTouchMismatchPair) string {
	parts := make([]string, 0, len(mismatches))
	for _, m := range mismatches {
		parts = append(parts, fmt.Sprintf("(%s,%s)@%q", m.TaskA, m.TaskB, m.SharedPath))
	}
	return strings.Join(parts, "; ")
}
