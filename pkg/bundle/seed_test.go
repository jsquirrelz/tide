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

package bundle

import (
	"bytes"
	"encoding/json"
	"testing"
)

// seedEntry mirrors internal/controller.seedEntry exactly (same json tags).
// Used only in tests to verify BundleEntry is a byte-identical superset.
type seedEntry struct {
	Name         string   `json:"name"`
	FQName       string   `json:"fqName"`
	OldUID       string   `json:"oldUID"`
	DependsOn    []string `json:"dependsOn,omitempty"`
	Status       string   `json:"status,omitempty"`
	PhaseRef     string   `json:"phaseRef,omitempty"`
	MilestoneRef string   `json:"milestoneRef,omitempty"`
	ProjectRef   string   `json:"projectRef,omitempty"`
}

type seedManifest struct {
	Milestones []seedEntry `json:"milestones"`
	Phases     []seedEntry `json:"phases"`
	Plans      []seedEntry `json:"plans"`
}

// TestSeedTagCompatibility verifies BundleManifest marshals to JSON whose three
// array keys + per-entry field tags are byte-identical to seedManifest/seedEntry;
// ImportController's json.Unmarshal of the same bytes would succeed and the extra
// sha256 field is silently dropped.
func TestSeedTagCompatibility(t *testing.T) {
	bm := BundleManifest{
		Milestones: []BundleEntry{
			{
				Name:       "ms-01",
				FQName:     "ms-01",
				OldUID:     "uid-ms-01",
				DependsOn:  []string{},
				Status:     "Succeeded",
				ProjectRef: "my-project",
				SHA256:     "abc123",
			},
		},
		Phases: []BundleEntry{
			{
				Name:         "phase-01",
				FQName:       "ms-01/phase-01",
				OldUID:       "uid-phase-01",
				MilestoneRef: "ms-01",
				Status:       "Succeeded",
				SHA256:       "def456",
			},
		},
		Plans: []BundleEntry{
			{
				Name:     "plan-01",
				FQName:   "ms-01/phase-01/plan-01",
				OldUID:   "uid-plan-01",
				PhaseRef: "phase-01",
				Status:   "Succeeded",
				SHA256:   "ghi789",
			},
		},
	}

	data, err := json.Marshal(bm)
	if err != nil {
		t.Fatalf("marshal BundleManifest: %v", err)
	}

	// Unmarshal into the restricted seedManifest (ImportController's view).
	var sm seedManifest
	if err := json.Unmarshal(data, &sm); err != nil {
		t.Fatalf("unmarshal into seedManifest: %v", err)
	}

	if len(sm.Milestones) != 1 {
		t.Fatalf("expected 1 milestone, got %d", len(sm.Milestones))
	}
	if sm.Milestones[0].Name != "ms-01" {
		t.Errorf("milestone name: got %q, want %q", sm.Milestones[0].Name, "ms-01")
	}
	if sm.Milestones[0].FQName != "ms-01" {
		t.Errorf("milestone fqName: got %q, want %q", sm.Milestones[0].FQName, "ms-01")
	}
	if sm.Milestones[0].OldUID != "uid-ms-01" {
		t.Errorf("milestone oldUID: got %q, want %q", sm.Milestones[0].OldUID, "uid-ms-01")
	}
	if sm.Milestones[0].ProjectRef != "my-project" {
		t.Errorf("milestone projectRef: got %q, want %q", sm.Milestones[0].ProjectRef, "my-project")
	}
	if sm.Milestones[0].Status != "Succeeded" {
		t.Errorf("milestone status: got %q, want %q", sm.Milestones[0].Status, "Succeeded")
	}

	if len(sm.Phases) != 1 {
		t.Fatalf("expected 1 phase, got %d", len(sm.Phases))
	}
	if sm.Phases[0].MilestoneRef != "ms-01" {
		t.Errorf("phase milestoneRef: got %q, want %q", sm.Phases[0].MilestoneRef, "ms-01")
	}

	if len(sm.Plans) != 1 {
		t.Fatalf("expected 1 plan, got %d", len(sm.Plans))
	}
	if sm.Plans[0].PhaseRef != "phase-01" {
		t.Errorf("plan phaseRef: got %q, want %q", sm.Plans[0].PhaseRef, "phase-01")
	}

	// Verify sha256 is NOT present in the seedManifest (unknown field dropped).
	// Re-marshal the seedManifest and check the sha256 field does not appear.
	seedData, err := json.Marshal(sm)
	if err != nil {
		t.Fatalf("marshal seedManifest: %v", err)
	}
	if bytes.Contains(seedData, []byte("sha256")) {
		t.Error("sha256 field leaked into seedManifest round-trip")
	}
}

// TestSHA256Determinism verifies computeEnvelopeSHA256 returns lowercase hex
// and is stable across repeated calls.
func TestSHA256(t *testing.T) {
	input := []byte(`{"apiVersion":"tideproject.k8s/v1alpha1","kind":"TaskEnvelopeOut"}`)

	sum1 := computeEnvelopeSHA256(input)
	sum2 := computeEnvelopeSHA256(input)

	if sum1 == "" {
		t.Fatal("computeEnvelopeSHA256 returned empty string")
	}
	if sum1 != sum2 {
		t.Errorf("sha256 not deterministic: %q vs %q", sum1, sum2)
	}
	// Must be lowercase hex (64 chars for sha256).
	if len(sum1) != 64 {
		t.Errorf("sha256 hex length: got %d, want 64", len(sum1))
	}
	for _, c := range sum1 {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			t.Errorf("sha256 hex contains non-lowercase-hex char: %c", c)
			break
		}
	}

	// Different input yields different sum.
	other := computeEnvelopeSHA256([]byte(`{}`))
	if other == sum1 {
		t.Error("different inputs yielded identical sha256")
	}
}

// TestStampChildCount verifies:
//   - legacy shape (childCount absent/0, len(childCRDs)>0) → repaired, warning written
//   - already-stamped shape → returned unchanged (no spurious rewrite)
//   - executor shape (no childCRDs) → returned unchanged
func TestStampChildCount(t *testing.T) {
	t.Run("stamps legacy shape", func(t *testing.T) {
		// Legacy: childCount absent (omitempty), 3 children present.
		raw := []byte(`{"apiVersion":"tideproject.k8s/v1alpha1","kind":"TaskEnvelopeOut","taskUID":"t1","exitCode":0,"childCRDs":[{"apiVersion":"x","kind":"Milestone","name":"ms-01","spec":{}},{"apiVersion":"x","kind":"Milestone","name":"ms-02","spec":{}},{"apiVersion":"x","kind":"Milestone","name":"ms-03","spec":{}}]}`)

		var warnBuf bytes.Buffer
		repaired, err := stampChildCount(raw, &warnBuf)
		if err != nil {
			t.Fatalf("stampChildCount: %v", err)
		}

		// Must have childCount=3.
		var out struct {
			ChildCount int `json:"childCount"`
		}
		if err := json.Unmarshal(repaired, &out); err != nil {
			t.Fatalf("unmarshal repaired: %v", err)
		}
		if out.ChildCount != 3 {
			t.Errorf("childCount: got %d, want 3", out.ChildCount)
		}

		// Warning must mention the envelope UID.
		if !bytes.Contains(warnBuf.Bytes(), []byte("t1")) {
			t.Errorf("warning missing envelope UID; got: %s", warnBuf.String())
		}
	})

	t.Run("leaves already-stamped unchanged", func(t *testing.T) {
		// childCount=2, 2 children — correct, no repair needed.
		raw := []byte(`{"apiVersion":"tideproject.k8s/v1alpha1","kind":"TaskEnvelopeOut","taskUID":"t2","exitCode":0,"childCount":2,"childCRDs":[{"apiVersion":"x","kind":"Milestone","name":"ms-01","spec":{}},{"apiVersion":"x","kind":"Milestone","name":"ms-02","spec":{}}]}`)

		var warnBuf bytes.Buffer
		result, err := stampChildCount(raw, &warnBuf)
		if err != nil {
			t.Fatalf("stampChildCount: %v", err)
		}

		if !bytes.Equal(result, raw) {
			t.Error("stampChildCount rewrote already-correct bytes")
		}
		if warnBuf.Len() > 0 {
			t.Errorf("unexpected warning for already-stamped envelope: %s", warnBuf.String())
		}
	})

	t.Run("leaves executor shape unchanged", func(t *testing.T) {
		// Executor: no childCRDs, childCount=0 — this is legitimate, not a legacy issue.
		raw := []byte(`{"apiVersion":"tideproject.k8s/v1alpha1","kind":"TaskEnvelopeOut","taskUID":"t3","exitCode":0}`)

		var warnBuf bytes.Buffer
		result, err := stampChildCount(raw, &warnBuf)
		if err != nil {
			t.Fatalf("stampChildCount: %v", err)
		}

		if !bytes.Equal(result, raw) {
			t.Error("stampChildCount rewrote executor (childCount=0, no children) bytes")
		}
		if warnBuf.Len() > 0 {
			t.Errorf("unexpected warning for executor envelope: %s", warnBuf.String())
		}
	})
}

// TestFQName verifies the three-component FQ name builders.
func TestFQName(t *testing.T) {
	tests := []struct {
		name   string
		call   func() string
		expect string
	}{
		{"milestone", func() string { return MilestoneFQName("ms-01") }, "ms-01"},
		{"phase", func() string { return PhaseFQName("ms-01", "phase-01") }, "ms-01/phase-01"},
		{"plan", func() string { return PlanFQName("ms-01", "phase-01", "plan-01") }, "ms-01/phase-01/plan-01"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.call()
			if got != tt.expect {
				t.Errorf("FQName: got %q, want %q", got, tt.expect)
			}
		})
	}
}
