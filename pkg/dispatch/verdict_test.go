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

package dispatch

import (
	"encoding/json"
	"os"
	"testing"
)

// TestGateDecision_GoldenFixtureRoundTrip reads the canonical shared golden
// fixture (D-02 — the single source of truth Plan 03's Python test also
// reads) and asserts it unmarshals to the expected GateDecision shape. This
// deliberately does NOT re-marshal-and-byte-compare: Go and Python struct/
// dict serialization key order is not guaranteed to match (RESEARCH
// Anti-Pattern) — asserting against the canonical decoded values is the
// stable check.
func TestGateDecision_GoldenFixtureRoundTrip(t *testing.T) {
	golden, err := os.ReadFile("testdata/gate_decision_golden.json")
	if err != nil {
		t.Fatalf("read golden fixture: %v", err)
	}
	var decoded GateDecision
	if err := json.Unmarshal(golden, &decoded); err != nil {
		t.Fatalf("unmarshal golden fixture: %v", err)
	}
	if decoded.Verdict != VerdictRepairable {
		t.Errorf("Verdict = %q, want %q", decoded.Verdict, VerdictRepairable)
	}
	if decoded.Summary == "" {
		t.Error("Summary is empty, want non-empty")
	}
	if len(decoded.Findings) < 1 {
		t.Fatalf("Findings length = %d, want >= 1", len(decoded.Findings))
	}
	f := decoded.Findings[0]
	if f.Dimension == "" {
		t.Error("Findings[0].Dimension is empty, want non-empty")
	}
	if f.Severity == "" {
		t.Error("Findings[0].Severity is empty, want non-empty")
	}
	if f.Confidence == "" {
		t.Error("Findings[0].Confidence is empty, want non-empty")
	}
	if f.Evidence == "" {
		t.Error("Findings[0].Evidence is empty, want non-empty")
	}
	if f.SuggestedFix == "" {
		t.Error("Findings[0].SuggestedFix is empty, want non-empty")
	}
}

// TestClassifyVerdict_FailsClosed is the D-04 regression table: the three
// named fail shapes (empty JSON, missing verdict field, malformed JSON) all
// classify to VerdictBlocked, and a well-formed APPROVED document is the
// positive control that proves the classifier isn't just always returning
// BLOCKED.
func TestClassifyVerdict_FailsClosed(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want Verdict
	}{
		{"EmptyJSON", ``, VerdictBlocked},
		{"MissingVerdictField", `{"summary":"looks fine","findings":[]}`, VerdictBlocked},
		{"Malformed", `{not valid json`, VerdictBlocked},
		{"ValidApproved", `{"verdict":"APPROVED","summary":"ok","findings":[]}`, VerdictApproved},
		{"ValidRepairable", `{"verdict":"REPAIRABLE","summary":"needs a fix","findings":[]}`, VerdictRepairable},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ClassifyVerdict([]byte(c.raw)); got != c.want {
				t.Errorf("ClassifyVerdict(%q) = %q, want %q", c.raw, got, c.want)
			}
		})
	}
}

// TestClassifyVerdict_UnrecognizedVerdictField asserts that a well-formed
// JSON document whose verdict field holds a value outside the terminal set
// (typo, stale vocabulary, empty string) classifies to BLOCKED via the
// switch statement's default branch, not the empty/malformed early returns.
func TestClassifyVerdict_UnrecognizedVerdictField(t *testing.T) {
	got := ClassifyVerdict([]byte(`{"verdict":"REJECTED","summary":"stale vocabulary"}`))
	if got != VerdictBlocked {
		t.Errorf("ClassifyVerdict(unrecognized verdict) = %q, want %q", got, VerdictBlocked)
	}
}
