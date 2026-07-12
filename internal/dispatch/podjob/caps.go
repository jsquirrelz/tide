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
	tidev1alpha3 "github.com/jsquirrelz/tide/api/v1alpha3"
)

// JobKind discriminates planner from executor Jobs for caps-default purposes.
// Phase 04.1 P1.3 introduces this discriminator so that DefaultCaps can apply
// a Kind-specific wall-clock floor: executor pods need image pull + scheduler
// delay + init container; planner pods need that PLUS Anthropic API call
// latency (per RESEARCH.md §P1.3).
//
// Plan 04.1-05 (P1.2) extends BuildJobSpec's BuildOptions with a Kind field
// that propagates through to DefaultCaps; until that lands, the only consumer
// is the executor path (task_controller.go + jobspec.go for task Jobs), which
// always passes JobKindExecutor.
type JobKind string

const (
	JobKindExecutor JobKind = "executor" // Phase 2 task dispatch — 1200s floor
	JobKindPlanner  JobKind = "planner"  // Phase 3 planner dispatch — 1800s floor
)

// executorCapsFloorSeconds is the minimum wall-clock budget applied to
// executor Jobs (task dispatch) when caps is nil or caps.WallClockSeconds <= 0.
// Sized to outlive image pull + scheduler delay + init container startup on a
// cold cluster (Phase 2 WR-01; hoisted from internal/controller/task_controller.go
// by Phase 04.1 P1.3).
//
// Raised 300→480 after the wave1-executor-failure debug session: on the live
// medium run a caps-unset dependent executor task (write a test against the
// just-committed wave-0 function) ran its real-LLM tool loop past the 300s
// floor and was killed by the Job's activeDeadlineSeconds (=floor+grace=360s)
// with reason DeadlineExceeded, never writing out.json — surfacing downstream
// as EnvelopeReadFailed / exitCode=-1. The quick wave-0 task (~163s) fit under
// 360s, which is why the failure was intermittent and wave-dependent. 480s
// (+60s grace = 540s deadline) gave a real executor task ~50% headroom over
// the old cap while staying below plannerCapsFloorSeconds (the drift guard in
// caps_test.go requires planner > executor). Token-mint validity tracks this
// same floor via DefaultCaps in task_controller.go, so the two stay aligned.
//
// Raised 480→1200 after dogfood run 1 (2026-06-11, verified live): real
// Sonnet executor tasks ran their tool loops past the 480s floor and were
// killed by the Job's activeDeadlineSeconds (=floor+grace=540s) — exitCode -1,
// partial envelope flush, EnvelopeReadFailed → Task CR marked Failed. 10+
// tasks were lost this way in one run (alongside 8 plans on the planner
// floor). Token-mint validity tracks the same floor via DefaultCaps
// (task_controller.go mint), so no other change is needed.
const executorCapsFloorSeconds int32 = 1200

// plannerCapsFloorSeconds is the minimum wall-clock budget applied to planner
// Jobs (milestone/phase/plan dispatch) when caps is nil or
// caps.WallClockSeconds <= 0. Sized to cover executor floor + Anthropic API
// call latency on planner pods (RESEARCH.md §P1.3).
//
// Raised 600→1800 after dogfood run 1 (2026-06-11, verified live): heavyweight
// Opus plan-planner sessions ran ~11+ min, well past the 600s floor, and were
// killed by the Job's activeDeadlineSeconds (=floor+grace=660s) — exitCode -1,
// partial envelope flush, EnvelopeReadFailed → Plan CR marked Failed. 8 plans
// were lost this way in one run (alongside 10+ tasks on the executor floor).
// Token-mint validity tracks the same floor via DefaultCaps (phase/plan
// controllers' credproxy.Sign use plannerCaps.WallClockSeconds + grace), so
// no other change is needed.
const plannerCapsFloorSeconds int32 = 1800

// DefaultCaps returns a *Caps with the Kind-appropriate wall-clock floor
// applied. If caps is non-nil and WallClockSeconds > 0, returns caps unchanged
// (no allocation; operator-set values are always honored regardless of Kind).
// Otherwise returns a NEW *Caps with WallClockSeconds set to executorCapsFloorSeconds
// (kind=JobKindExecutor) or plannerCapsFloorSeconds (kind=JobKindPlanner) and
// any non-zero fields from the input preserved (Iterations, InputTokens,
// OutputTokens — operator-set caps on other dimensions are NOT clobbered).
//
// Used by:
//   - internal/controller/task_controller.go (token mint — credproxy.Sign validity, JobKindExecutor)
//   - internal/dispatch/podjob/jobspec.go    (activeDeadlineSeconds derivation, Kind from BuildOptions)
//   - internal/controller/milestone_controller.go / phase / plan (planner dispatch via Plan 04.1-05, JobKindPlanner)
//
// A nil-caps unit test (caps_test.go) asserts that both consumers' derived
// deadlines match within DefaultWallClockGraceSeconds for EACH Kind, which the
// structural routing makes a tautology — the test fails only if a future
// maintainer routes one consumer around this helper.
func DefaultCaps(caps *tidev1alpha3.Caps, kind JobKind) *tidev1alpha3.Caps {
	if caps != nil && caps.WallClockSeconds > 0 {
		return caps
	}
	floor := executorCapsFloorSeconds
	if kind == JobKindPlanner {
		floor = plannerCapsFloorSeconds
	}
	out := tidev1alpha3.Caps{
		WallClockSeconds: floor,
	}
	if caps != nil {
		if caps.Iterations > 0 {
			out.Iterations = caps.Iterations
		}
		if caps.InputTokens > 0 {
			out.InputTokens = caps.InputTokens
		}
		if caps.OutputTokens > 0 {
			out.OutputTokens = caps.OutputTokens
		}
	}
	return &out
}
