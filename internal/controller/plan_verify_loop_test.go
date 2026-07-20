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

// plan_verify_loop_test.go — Phase 52 Plan 09: the plan-check re-plan loop's
// pure decision math (D-04/D-05/D-06) — severityScore/replanStalled, the
// stall-detection primitives repairOrHaltPlan (plan_controller.go) consumes.
//
// Per the systemic finding documented in 51-01/51-03/51-05/51-06/52-07/
// 52-08-SUMMARY.md: internal/controller's sole top-level Ginkgo entry point
// is TestControllers — a plain `go test -run 'TestSeverityScore|
// TestReplanStalled'` matches ZERO Describe/It text and would exit 0
// vacuously if these were Ginkgo specs. They are NOT: the functions below
// are plain testing.T functions (no shared Ginkgo suite dependency) that DO
// genuinely execute under a plain `-run` filter, mirroring
// task_verify_loop_test.go's own file shape (the repo's documented home for
// pure-function loop-math tests).
package controller

import (
	"testing"

	tideprojectv1alpha3 "github.com/jsquirrelz/tide/api/v1alpha3"
)

// TestSeverityScore pins D-05's severity-weighted scoring formula: a
// high-severity finding dominates (weight 10) over the raw findings count
// (weight 1).
func TestSeverityScore(t *testing.T) {
	cases := []struct {
		name              string
		findingsCount     int32
		highSeverityCount int32
		want              int
	}{
		{"3 findings, 1 high-severity", 3, 1, 13},
		{"zero findings, zero high-severity", 0, 0, 0},
		{"findings only, no high-severity", 5, 0, 5},
		{"high-severity only", 0, 2, 20},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := severityScore(tc.findingsCount, tc.highSeverityCount); got != tc.want {
				t.Errorf("severityScore(findings=%d, high=%d) = %d, want %d", tc.findingsCount, tc.highSeverityCount, got, tc.want)
			}
		})
	}
}

// TestReplanStalled pins D-05's strictly-decreasing stall-detection
// requirement: a new score that does not STRICTLY improve on the previous
// iteration's evaluation halts the loop early, before a remaining iteration
// is consumed. No previous evaluation (the first-ever REPAIRABLE verdict,
// nothing yet to compare against) is never stalled.
func TestReplanStalled(t *testing.T) {
	prevScore13 := &tideprojectv1alpha3.EvaluationSummary{FindingsCount: 3, HighSeverityCount: 1} // severityScore = 13

	cases := []struct {
		name     string
		prev     *tideprojectv1alpha3.EvaluationSummary
		newScore int
		want     bool
	}{
		{"no previous evaluation (Iteration 0, first REPAIRABLE) — never stalled", nil, 13, false},
		{"same score as previous (13 vs 13) — not strictly decreasing, STALLED", prevScore13, 13, true},
		{"new score improves (13 vs 12) — proceed", prevScore13, 12, false},
		{"new score regresses (13 vs 14) — STALLED", prevScore13, 14, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := replanStalled(tc.prev, tc.newScore); got != tc.want {
				t.Errorf("replanStalled(prev=%+v, newScore=%d) = %v, want %v", tc.prev, tc.newScore, got, tc.want)
			}
		})
	}
}
