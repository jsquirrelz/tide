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

// Phase 4 Plan 01 Task 1: constant tests for D-W1 (PhasePushLeakBlocked)
// and D-G2 (ConditionWaveOrLevelPaused + 4 Reason constants).
//
// These tests are table-driven and reference the symbols by name (compile-time
// check) plus assert their exact string value. The string values are part of
// the CRD condition vocabulary — downstream reconcilers (plans 04-05/04-06)
// match on these literals when setting Status.Conditions / Status.Phase, so a
// silent rename here would break the gate-policy seam (D-G2/D-G3/D-G4).
package v1alpha1_test

import (
	"strings"
	"testing"

	tideprojectv1alpha1 "github.com/jsquirrelz/tide/api/v1alpha1"
)

// TestPhase4Constants_Values asserts every Phase 4 constant's exact value.
// Subtests give per-constant failure isolation so a single break doesn't mask
// the others.
func TestPhase4Constants_Values(t *testing.T) {
	cases := []struct {
		name string
		got  string
		want string
	}{
		// D-W1: new Phase constant — distinct from PhasePushLeaseFailed so the
		// project_controller switch arm can fire tide_secret_leak_blocked_total
		// on exit-10 (gitleaks) without conflating exit-11 (lease).
		{"PhasePushLeakBlocked", tideprojectv1alpha1.PhasePushLeakBlocked, "PushLeakBlocked"},

		// D-G2: gate-policy condition — set by every level reconciler when
		// gate=approve OR gate=pause OR PauseBetweenWaves triggers at boundary.
		{"ConditionWaveOrLevelPaused", tideprojectv1alpha1.ConditionWaveOrLevelPaused, "WaveOrLevelPaused"},

		// D-G2/G3/G4 Reasons — paired with ConditionWaveOrLevelPaused on
		// Status.Conditions.Reason. The CLI (plan 04-07) and dashboard
		// (plan 04-12) read these literal strings.
		{"ReasonAwaitingApproval", tideprojectv1alpha1.ReasonAwaitingApproval, "AwaitingApproval"},
		{"ReasonPausedAtBoundary", tideprojectv1alpha1.ReasonPausedAtBoundary, "PausedAtBoundary"},
		{"ReasonRejectedByUser", tideprojectv1alpha1.ReasonRejectedByUser, "RejectedByUser"},
		{"ReasonResumedByUser", tideprojectv1alpha1.ReasonResumedByUser, "ResumedByUser"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.got != tc.want {
				t.Errorf("%s = %q, want %q", tc.name, tc.got, tc.want)
			}
		})
	}
}

// TestPhase4Constants_DistinctFromExisting asserts that the new W-1 phase is
// not aliased to PhasePushLeaseFailed — they MUST be distinct strings because
// the project_controller fires different counters on each.
func TestPhase4Constants_DistinctFromExisting(t *testing.T) {
	if tideprojectv1alpha1.PhasePushLeakBlocked == tideprojectv1alpha1.PhasePushLeaseFailed {
		t.Errorf("D-W1 violation: PhasePushLeakBlocked %q must differ from PhasePushLeaseFailed %q",
			tideprojectv1alpha1.PhasePushLeakBlocked, tideprojectv1alpha1.PhasePushLeaseFailed)
	}
	if !strings.HasPrefix(tideprojectv1alpha1.PhasePushLeakBlocked, "PushLeak") {
		t.Errorf("PhasePushLeakBlocked %q should start with PushLeak (leak-class phase, distinct from lease-class)",
			tideprojectv1alpha1.PhasePushLeakBlocked)
	}
}
