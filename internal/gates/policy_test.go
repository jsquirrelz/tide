/*
Copyright 2026 TIDE Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

// Package gates unit tests for policy.go: EvaluatePolicy + DefaultGates +
// PolicyAuto/PolicyApprove/PolicyPause constants. Pure-func tests modeled on
// internal/budget/cap_test.go (table-driven, no envtest, no fake client).
package gates

import (
	"testing"

	tideprojectv1alpha1 "github.com/jsquirrelz/tide/api/v1alpha1"
)

// Test 1-2 + 3-4-5: EvaluatePolicy covers default + explicit + unknown level.
func TestEvaluatePolicy(t *testing.T) {
	cases := []struct {
		name  string
		gates tideprojectv1alpha1.Gates
		level string
		want  tideprojectv1alpha1.GatePolicy
	}{
		// Test 1: milestone default = approve.
		{"empty/milestone -> approve", tideprojectv1alpha1.Gates{}, "milestone", PolicyApprove},
		// Test 2: phase/plan/task defaults = auto.
		{"empty/phase -> auto", tideprojectv1alpha1.Gates{}, "phase", PolicyAuto},
		{"empty/plan -> auto", tideprojectv1alpha1.Gates{}, "plan", PolicyAuto},
		{"empty/task -> auto", tideprojectv1alpha1.Gates{}, "task", PolicyAuto},
		// Test 3: explicit milestone override wins.
		{"explicit/milestone=pause", tideprojectv1alpha1.Gates{Milestone: PolicyPause}, "milestone", PolicyPause},
		{"explicit/milestone=auto", tideprojectv1alpha1.Gates{Milestone: PolicyAuto}, "milestone", PolicyAuto},
		// Test 4: explicit phase override wins.
		{"explicit/phase=approve", tideprojectv1alpha1.Gates{Phase: PolicyApprove}, "phase", PolicyApprove},
		{"explicit/phase=pause", tideprojectv1alpha1.Gates{Phase: PolicyPause}, "phase", PolicyPause},
		// Explicit overrides at all four levels.
		{"explicit/plan=approve", tideprojectv1alpha1.Gates{Plan: PolicyApprove}, "plan", PolicyApprove},
		{"explicit/plan=pause", tideprojectv1alpha1.Gates{Plan: PolicyPause}, "plan", PolicyPause},
		{"explicit/task=approve", tideprojectv1alpha1.Gates{Task: PolicyApprove}, "task", PolicyApprove},
		{"explicit/task=pause", tideprojectv1alpha1.Gates{Task: PolicyPause}, "task", PolicyPause},
		// Test 5: unknown level falls back to PolicyAuto (safe default; never
		// happens in production but must not panic).
		{"unknown level -> auto", tideprojectv1alpha1.Gates{}, "bogus", PolicyAuto},
		{"unknown level even with all gates set -> auto",
			tideprojectv1alpha1.Gates{Milestone: PolicyPause, Phase: PolicyPause, Plan: PolicyPause, Task: PolicyPause},
			"wave", PolicyAuto},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := EvaluatePolicy(tc.gates, tc.level)
			if got != tc.want {
				t.Errorf("EvaluatePolicy(%+v, %q) = %q; want %q", tc.gates, tc.level, got, tc.want)
			}
		})
	}
}

// Test 6: DefaultGates returns the locked defaults and round-trips through
// EvaluatePolicy.
func TestDefaultGatesAndRoundTrip(t *testing.T) {
	g := DefaultGates()
	if g.Milestone != PolicyApprove {
		t.Errorf("DefaultGates().Milestone = %q; want %q", g.Milestone, PolicyApprove)
	}
	if g.Phase != PolicyAuto {
		t.Errorf("DefaultGates().Phase = %q; want %q", g.Phase, PolicyAuto)
	}
	if g.Plan != PolicyAuto {
		t.Errorf("DefaultGates().Plan = %q; want %q", g.Plan, PolicyAuto)
	}
	if g.Task != PolicyAuto {
		t.Errorf("DefaultGates().Task = %q; want %q", g.Task, PolicyAuto)
	}
	if g.PauseBetweenWaves != false {
		t.Errorf("DefaultGates().PauseBetweenWaves = %v; want false", g.PauseBetweenWaves)
	}

	// Round-trip: EvaluatePolicy(DefaultGates(), <level>) matches the per-level defaults.
	roundTrip := []struct {
		level string
		want  tideprojectv1alpha1.GatePolicy
	}{
		{"milestone", PolicyApprove},
		{"phase", PolicyAuto},
		{"plan", PolicyAuto},
		{"task", PolicyAuto},
	}
	for _, rt := range roundTrip {
		got := EvaluatePolicy(g, rt.level)
		if got != rt.want {
			t.Errorf("EvaluatePolicy(DefaultGates(), %q) = %q; want %q", rt.level, got, rt.want)
		}
	}
}

// Test 7: exported PolicyAuto/Approve/Pause constants carry the expected
// string values (the CEL-validated enum on the CRD field).
func TestPolicyConstants(t *testing.T) {
	cases := []struct {
		got  tideprojectv1alpha1.GatePolicy
		want string
	}{
		{PolicyAuto, "auto"},
		{PolicyApprove, "approve"},
		{PolicyPause, "pause"},
	}
	for _, tc := range cases {
		if string(tc.got) != tc.want {
			t.Errorf("constant value = %q; want %q", string(tc.got), tc.want)
		}
	}
}
