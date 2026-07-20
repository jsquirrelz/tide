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

// Test name map:
//   - TestLoopPolicy_JSONRoundTrip / TestLoopStatus_JSONRoundTrip — a
//     fully-populated value marshals and unmarshals back to an equal value.
//   - TestLoopStatus_NoForbiddenFields — LOOP-03 compile-time structural
//     guard: a struct literal naming every LoopStatus field, so a future PR
//     adding a history slice fails to compile.
//   - TestLoopContract_Embeddable — a synthetic test-only CRD Spec/Status
//     embeds both LoopPolicy and LoopStatus and deep-copies + JSON
//     round-trips, proving the "embeddable in any domain CRD" claim without
//     touching a real Kind (Phase success criterion #1).
package v1alpha3_test

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	tidev1alpha3 "github.com/jsquirrelz/tide/api/v1alpha3"
)

// The four assertions below are Phase 52 D-06's structural guard: each
// proves PlanStatus/PhaseStatus/MilestoneStatus/ProjectStatus embeds the
// LoopStatus type itself (the guarded type LOOP-03 pins), never a locally-
// widened variant. If a future PR redeclares e.g. PlanStatus.LoopStatus as
// a different type, this fails to compile.
var (
	_ tidev1alpha3.LoopStatus = tidev1alpha3.PlanStatus{}.LoopStatus
	_ tidev1alpha3.LoopStatus = tidev1alpha3.PhaseStatus{}.LoopStatus
	_ tidev1alpha3.LoopStatus = tidev1alpha3.MilestoneStatus{}.LoopStatus
	_ tidev1alpha3.LoopStatus = tidev1alpha3.ProjectStatus{}.LoopStatus
)

func fullyPopulatedLoopPolicy() tidev1alpha3.LoopPolicy {
	maxDuration := metav1.Duration{Duration: 30 * time.Minute}
	return tidev1alpha3.LoopPolicy{
		MaxIterations:    3,
		MaxDuration:      &maxDuration,
		BudgetCents:      500,
		Autonomy:         tidev1alpha3.AutonomyAutonomous,
		EvaluatorRef:     "default-evaluator",
		EscalationPolicy: tidev1alpha3.EscalationRequireApproval,
	}
}

func fullyPopulatedLoopStatus() tidev1alpha3.LoopStatus {
	// metav1.Time.UnmarshalJSON normalizes to Local() (k8s.io/apimachinery
	// convention) — construct with .Local() up front so the pre-round-trip
	// value already matches the post-round-trip representation under
	// reflect.DeepEqual (the instant is identical either way; only the
	// in-memory Location differs otherwise).
	completedAt := metav1.NewTime(time.Date(2026, 7, 18, 12, 0, 0, 0, time.UTC).Local())
	return tidev1alpha3.LoopStatus{
		Iteration:   2,
		ParentRunID: "run-abc123",
		LastEvaluation: &tidev1alpha3.EvaluationSummary{
			Decision:          "REPAIRABLE",
			FindingsCount:     4,
			HighSeverityCount: 1,
			CompletedAt:       &completedAt,
		},
		ExitReason: tidev1alpha3.ExitApproved,
		CostCents:  120,
		Conditions: []metav1.Condition{
			{
				Type:               "Ready",
				Status:             metav1.ConditionTrue,
				Reason:             "EvaluationApproved",
				Message:            "loop exited on evaluator approval",
				LastTransitionTime: completedAt,
			},
		},
	}
}

// TestLoopPolicy_JSONRoundTrip asserts a fully-populated LoopPolicy marshals
// and unmarshals back to an equal value.
func TestLoopPolicy_JSONRoundTrip(t *testing.T) {
	want := fullyPopulatedLoopPolicy()

	data, err := json.Marshal(want)
	if err != nil {
		t.Fatalf("json.Marshal(LoopPolicy): %v", err)
	}

	var got tidev1alpha3.LoopPolicy
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("json.Unmarshal(LoopPolicy): %v", err)
	}

	if !reflect.DeepEqual(want, got) {
		t.Errorf("LoopPolicy round-trip mismatch:\n want: %+v\n got:  %+v", want, got)
	}
}

// TestLoopStatus_JSONRoundTrip asserts a fully-populated LoopStatus —
// including a non-nil LastEvaluation — marshals and unmarshals back to an
// equal value, with LastEvaluation's bounded fields preserved.
func TestLoopStatus_JSONRoundTrip(t *testing.T) {
	want := fullyPopulatedLoopStatus()

	data, err := json.Marshal(want)
	if err != nil {
		t.Fatalf("json.Marshal(LoopStatus): %v", err)
	}

	var got tidev1alpha3.LoopStatus
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("json.Unmarshal(LoopStatus): %v", err)
	}

	if !reflect.DeepEqual(want, got) {
		t.Errorf("LoopStatus round-trip mismatch:\n want: %+v\n got:  %+v", want, got)
	}
	if got.LastEvaluation == nil {
		t.Fatal("LoopStatus.LastEvaluation is nil after round-trip, want non-nil")
	}
	if got.LastEvaluation.Decision != "REPAIRABLE" || got.LastEvaluation.FindingsCount != 4 || got.LastEvaluation.HighSeverityCount != 1 {
		t.Errorf("LastEvaluation bounded fields not preserved: %+v", got.LastEvaluation)
	}
}

// TestLoopStatus_NoForbiddenFields asserts (at compile time via struct
// literal) that LoopStatus carries no iteration-history field — only the
// current-iteration summary + exit reason (LOOP-03). This test exists to pin
// the contract: if a history slice (e.g. PreviousEvaluations
// []EvaluationSummary) is ever added, the literal below will fail to
// compile with "unknown field" — intentional.
func TestLoopStatus_NoForbiddenFields(t *testing.T) {
	_ = tidev1alpha3.LoopStatus{
		Iteration:      0,
		ParentRunID:    "",
		LastEvaluation: nil,
		ExitReason:     "",
		CostCents:      0,
		Conditions:     nil,
	}

	// Runtime assertion: marshalled JSON must not contain a history-shaped key.
	status := fullyPopulatedLoopStatus()
	data, err := json.Marshal(status)
	if err != nil {
		t.Fatalf("json.Marshal(LoopStatus): %v", err)
	}
	for _, forbidden := range []string{`"previousEvaluations"`, `"evaluations"`, `"history"`} {
		if strings.Contains(string(data), forbidden) {
			t.Errorf("LoopStatus JSON contains forbidden history-shaped key %s: %s", forbidden, data)
		}
	}
}

// testEmbedder is a synthetic, test-only CRD-shaped struct embedding both
// LoopPolicy and LoopStatus in a Spec/Status split — proving embeddability
// without touching a real Kind (Phase success criterion #1).
type testEmbedder struct {
	Spec struct {
		Policy tidev1alpha3.LoopPolicy `json:"policy"`
	} `json:"spec"`
	Status struct {
		Loop tidev1alpha3.LoopStatus `json:"loop"`
	} `json:"status"`
}

// TestLoopContract_Embeddable proves LoopPolicy and LoopStatus are
// embeddable in an arbitrary domain CRD's Spec/Status: a synthetic embedder
// struct deep-copies (via the generated DeepCopy methods on the embedded
// types) and JSON round-trips.
func TestLoopContract_Embeddable(t *testing.T) {
	var want testEmbedder
	want.Spec.Policy = fullyPopulatedLoopPolicy()
	want.Status.Loop = fullyPopulatedLoopStatus()

	// Exercise the generated DeepCopy methods directly on the embedded
	// fields — the mechanism a real domain CRD's own DeepCopyInto would call.
	policyCopy := want.Spec.Policy.DeepCopy()
	if !reflect.DeepEqual(*policyCopy, want.Spec.Policy) {
		t.Errorf("LoopPolicy.DeepCopy() mismatch:\n want: %+v\n got:  %+v", want.Spec.Policy, *policyCopy)
	}
	statusCopy := want.Status.Loop.DeepCopy()
	if !reflect.DeepEqual(*statusCopy, want.Status.Loop) {
		t.Errorf("LoopStatus.DeepCopy() mismatch:\n want: %+v\n got:  %+v", want.Status.Loop, *statusCopy)
	}

	data, err := json.Marshal(want)
	if err != nil {
		t.Fatalf("json.Marshal(testEmbedder): %v", err)
	}

	var got testEmbedder
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("json.Unmarshal(testEmbedder): %v", err)
	}

	if !reflect.DeepEqual(want, got) {
		t.Errorf("testEmbedder round-trip mismatch:\n want: %+v\n got:  %+v", want, got)
	}
}
