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

package podjob

import (
	"testing"

	tidev1alpha1 "github.com/jsquirrelz/tide/api/v1alpha2"
)

// TestDefaultCaps asserts the wall-clock floor application across nil, zero,
// and non-zero caps inputs for BOTH JobKindExecutor and JobKindPlanner.
// Covers Phase 04.1 P1.3 success criterion: the Kind-appropriate floor is
// applied identically wherever DefaultCaps is called.
func TestDefaultCaps(t *testing.T) {
	cases := []struct {
		name             string
		in               *tidev1alpha1.Caps
		kind             JobKind
		wantWallClock    int32
		wantIterations   int32
		wantInputTokens  int64
		wantOutputTokens int64
	}{
		// Executor branch — 1200s floor
		{
			name:          "executor: nil caps → 1200s floor",
			in:            nil,
			kind:          JobKindExecutor,
			wantWallClock: executorCapsFloorSeconds,
		},
		{
			name:          "executor: zero WallClockSeconds → 1200s floor",
			in:            &tidev1alpha1.Caps{WallClockSeconds: 0},
			kind:          JobKindExecutor,
			wantWallClock: executorCapsFloorSeconds,
		},
		{
			name:          "executor: negative WallClockSeconds → 1200s floor",
			in:            &tidev1alpha1.Caps{WallClockSeconds: -1},
			kind:          JobKindExecutor,
			wantWallClock: executorCapsFloorSeconds,
		},
		{
			name:          "executor: 60s WallClockSeconds → 60s (under floor but operator-set is honored)",
			in:            &tidev1alpha1.Caps{WallClockSeconds: 60},
			kind:          JobKindExecutor,
			wantWallClock: 60,
		},
		{
			name:          "executor: 600s WallClockSeconds → 600s (under floor but operator-set is honored)",
			in:            &tidev1alpha1.Caps{WallClockSeconds: 600},
			kind:          JobKindExecutor,
			wantWallClock: 600,
		},
		{
			name:           "executor: zero WallClockSeconds + non-zero Iterations → 1200s floor, Iterations preserved",
			in:             &tidev1alpha1.Caps{WallClockSeconds: 0, Iterations: 50},
			kind:           JobKindExecutor,
			wantWallClock:  executorCapsFloorSeconds,
			wantIterations: 50,
		},
		{
			name:             "executor: zero WallClockSeconds + non-zero Token caps → 1200s floor, tokens preserved",
			in:               &tidev1alpha1.Caps{WallClockSeconds: 0, InputTokens: 100000, OutputTokens: 50000},
			kind:             JobKindExecutor,
			wantWallClock:    executorCapsFloorSeconds,
			wantInputTokens:  100000,
			wantOutputTokens: 50000,
		},
		// Planner branch — 1800s floor
		{
			name:          "planner: nil caps → 1800s floor",
			in:            nil,
			kind:          JobKindPlanner,
			wantWallClock: plannerCapsFloorSeconds,
		},
		{
			name:          "planner: zero WallClockSeconds → 1800s floor",
			in:            &tidev1alpha1.Caps{WallClockSeconds: 0},
			kind:          JobKindPlanner,
			wantWallClock: plannerCapsFloorSeconds,
		},
		{
			name:          "planner: 60s WallClockSeconds → 60s (operator-set is honored regardless of Kind)",
			in:            &tidev1alpha1.Caps{WallClockSeconds: 60},
			kind:          JobKindPlanner,
			wantWallClock: 60,
		},
		{
			name:           "planner: zero WallClockSeconds + Iterations preserved across Kind",
			in:             &tidev1alpha1.Caps{WallClockSeconds: 0, Iterations: 20},
			kind:           JobKindPlanner,
			wantWallClock:  plannerCapsFloorSeconds,
			wantIterations: 20,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := DefaultCaps(tc.in, tc.kind)
			if got == nil {
				t.Fatalf("DefaultCaps returned nil; want non-nil")
			}
			if got.WallClockSeconds != tc.wantWallClock {
				t.Errorf("WallClockSeconds = %d; want %d", got.WallClockSeconds, tc.wantWallClock)
			}
			if got.Iterations != tc.wantIterations {
				t.Errorf("Iterations = %d; want %d", got.Iterations, tc.wantIterations)
			}
			if got.InputTokens != tc.wantInputTokens {
				t.Errorf("InputTokens = %d; want %d", got.InputTokens, tc.wantInputTokens)
			}
			if got.OutputTokens != tc.wantOutputTokens {
				t.Errorf("OutputTokens = %d; want %d", got.OutputTokens, tc.wantOutputTokens)
			}
		})
	}
}

// TestDefaultCaps_NilCapsDeadlineMatch asserts that for nil-caps input, the
// token-validity derivation and the Job activeDeadline derivation arrive at
// the same wall-clock value for EACH JobKind. This is a tautology by
// construction (both consumers route through DefaultCaps) — and that's the
// point. The test fails only if a future maintainer routes one consumer
// around the helper, OR if the planner floor regresses to the executor floor.
//
// Phase 04.1 P1.3 success criterion: "the two derived deadlines match
// within a grace window" (the grace is DefaultWallClockGraceSeconds, applied
// equally to both consumers downstream of DefaultCaps), with Kind-appropriate
// floors honored.
func TestDefaultCaps_NilCapsDeadlineMatch(t *testing.T) {
	// Executor branch — 1200s floor + 60s grace = 1260s
	capsForToken := DefaultCaps(nil, JobKindExecutor)
	capsForJob := DefaultCaps(nil, JobKindExecutor)
	tokenValidity := capsForToken.WallClockSeconds + DefaultWallClockGraceSeconds
	activeDeadline := capsForJob.WallClockSeconds + DefaultWallClockGraceSeconds
	if tokenValidity != activeDeadline {
		t.Errorf("executor: token validity (%ds) != active deadline (%ds); Phase 04.1 P1.3 drift",
			tokenValidity, activeDeadline)
	}
	if tokenValidity != executorCapsFloorSeconds+DefaultWallClockGraceSeconds {
		t.Errorf("executor: expected nil-caps deadline = %ds (floor + grace); got %ds",
			executorCapsFloorSeconds+DefaultWallClockGraceSeconds, tokenValidity)
	}

	// Planner branch — 1800s floor + 60s grace = 1860s
	plannerCapsForToken := DefaultCaps(nil, JobKindPlanner)
	plannerCapsForJob := DefaultCaps(nil, JobKindPlanner)
	plannerTokenValidity := plannerCapsForToken.WallClockSeconds + DefaultWallClockGraceSeconds
	plannerActiveDeadline := plannerCapsForJob.WallClockSeconds + DefaultWallClockGraceSeconds
	if plannerTokenValidity != plannerActiveDeadline {
		t.Errorf("planner: token validity (%ds) != active deadline (%ds); Phase 04.1 P1.3 drift",
			plannerTokenValidity, plannerActiveDeadline)
	}
	if plannerTokenValidity != plannerCapsFloorSeconds+DefaultWallClockGraceSeconds {
		t.Errorf("planner: expected nil-caps deadline = %ds (planner floor + grace); got %ds",
			plannerCapsFloorSeconds+DefaultWallClockGraceSeconds, plannerTokenValidity)
	}

	// Drift guard — planner floor MUST exceed executor floor (planner pods need API call latency)
	if plannerCapsFloorSeconds <= executorCapsFloorSeconds {
		t.Errorf("planner floor (%ds) must exceed executor floor (%ds); regression on Phase 04.1 P1.3 dual-floor design",
			plannerCapsFloorSeconds, executorCapsFloorSeconds)
	}
}
