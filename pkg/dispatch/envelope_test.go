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
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/runtime"
)

// assertRoundTripIn is a shared helper called by both the table-driven tests
// and top-level TestEnvelopeIn_* functions so a single failure produces one
// diagnostic surface (mirrors assertComputeWavesCase in pkg/dag/kahn_test.go).
func assertRoundTripIn(t *testing.T, in EnvelopeIn) {
	t.Helper()
	data, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("json.Marshal(EnvelopeIn): %v", err)
	}
	var got EnvelopeIn
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("json.Unmarshal(EnvelopeIn): %v", err)
	}
	// Compare field by field (time.Time zero value comparison).
	if in.APIVersion != got.APIVersion {
		t.Errorf("APIVersion: got %q, want %q", got.APIVersion, in.APIVersion)
	}
	if in.Kind != got.Kind {
		t.Errorf("Kind: got %q, want %q", got.Kind, in.Kind)
	}
	if in.TaskUID != got.TaskUID {
		t.Errorf("TaskUID: got %q, want %q", got.TaskUID, in.TaskUID)
	}
	if in.Role != got.Role {
		t.Errorf("Role: got %q, want %q", got.Role, in.Role)
	}
	if in.Level != got.Level {
		t.Errorf("Level: got %q, want %q", got.Level, in.Level)
	}
	if in.Prompt != got.Prompt {
		t.Errorf("Prompt: got %q, want %q", got.Prompt, in.Prompt)
	}
	if !stringSlicesEqual(in.FilesTouched, got.FilesTouched) {
		t.Errorf("FilesTouched: got %v, want %v", got.FilesTouched, in.FilesTouched)
	}
	if !stringSlicesEqual(in.DependsOn, got.DependsOn) {
		t.Errorf("DependsOn: got %v, want %v", got.DependsOn, in.DependsOn)
	}
	if !stringSlicesEqual(in.DeclaredOutputPaths, got.DeclaredOutputPaths) {
		t.Errorf("DeclaredOutputPaths: got %v, want %v", got.DeclaredOutputPaths, in.DeclaredOutputPaths)
	}
	if in.Caps != got.Caps {
		t.Errorf("Caps: got %+v, want %+v", got.Caps, in.Caps)
	}
	if in.ProxyEndpoint != got.ProxyEndpoint {
		t.Errorf("ProxyEndpoint: got %q, want %q", got.ProxyEndpoint, in.ProxyEndpoint)
	}
	if in.SignedToken != got.SignedToken {
		t.Errorf("SignedToken: got %q, want %q", got.SignedToken, in.SignedToken)
	}
	// Provider value comparison (D-C3 — value type, not pointer).
	if in.Provider.Vendor != got.Provider.Vendor {
		t.Errorf("Provider.Vendor: got %q, want %q", got.Provider.Vendor, in.Provider.Vendor)
	}
	if in.Provider.Model != got.Provider.Model {
		t.Errorf("Provider.Model: got %q, want %q", got.Provider.Model, in.Provider.Model)
	}
	if len(in.Provider.Params) != len(got.Provider.Params) {
		t.Errorf("Provider.Params length: got %d, want %d", len(got.Provider.Params), len(in.Provider.Params))
	}
	for k, v := range in.Provider.Params {
		if got.Provider.Params[k] != v {
			t.Errorf("Provider.Params[%q]: got %q, want %q", k, got.Provider.Params[k], v)
		}
	}
	// Dev pointer comparison.
	if in.Dev == nil && got.Dev != nil {
		t.Errorf("Dev: got %+v, want nil", got.Dev)
	}
	if in.Dev != nil && got.Dev == nil {
		t.Errorf("Dev: got nil, want %+v", in.Dev)
	}
	if in.Dev != nil && got.Dev != nil && *in.Dev != *got.Dev {
		t.Errorf("Dev: got %+v, want %+v", *got.Dev, *in.Dev)
	}
}

// assertRoundTripOut is the equivalent helper for EnvelopeOut.
func assertRoundTripOut(t *testing.T, out EnvelopeOut) {
	t.Helper()
	data, err := json.Marshal(out)
	if err != nil {
		t.Fatalf("json.Marshal(EnvelopeOut): %v", err)
	}
	var got EnvelopeOut
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("json.Unmarshal(EnvelopeOut): %v", err)
	}
	if out.APIVersion != got.APIVersion {
		t.Errorf("APIVersion: got %q, want %q", got.APIVersion, out.APIVersion)
	}
	if out.Kind != got.Kind {
		t.Errorf("Kind: got %q, want %q", got.Kind, out.Kind)
	}
	if out.TaskUID != got.TaskUID {
		t.Errorf("TaskUID: got %q, want %q", got.TaskUID, out.TaskUID)
	}
	if out.ExitCode != got.ExitCode {
		t.Errorf("ExitCode: got %d, want %d", got.ExitCode, out.ExitCode)
	}
	if out.Result != got.Result {
		t.Errorf("Result: got %q, want %q", got.Result, out.Result)
	}
	if out.Reason != got.Reason {
		t.Errorf("Reason: got %q, want %q", got.Reason, out.Reason)
	}
	if out.Usage != got.Usage {
		t.Errorf("Usage: got %+v, want %+v", got.Usage, out.Usage)
	}
	if !stringSlicesEqual(out.Artifacts, got.Artifacts) {
		t.Errorf("Artifacts: got %v, want %v", got.Artifacts, out.Artifacts)
	}
	// time.Time round-trip: compare UnixNano to avoid monotonic-clock divergence.
	if out.CompletedAt.UnixNano() != got.CompletedAt.UnixNano() {
		t.Errorf("CompletedAt: got %v, want %v", got.CompletedAt, out.CompletedAt)
	}
	// ChildCRDs slice comparison (D-A1).
	if len(out.ChildCRDs) != len(got.ChildCRDs) {
		t.Errorf("ChildCRDs length: got %d, want %d", len(got.ChildCRDs), len(out.ChildCRDs))
	}
	for i := range out.ChildCRDs {
		if i >= len(got.ChildCRDs) {
			break
		}
		if out.ChildCRDs[i].Kind != got.ChildCRDs[i].Kind {
			t.Errorf("ChildCRDs[%d].Kind: got %q, want %q", i, got.ChildCRDs[i].Kind, out.ChildCRDs[i].Kind)
		}
		if out.ChildCRDs[i].Name != got.ChildCRDs[i].Name {
			t.Errorf("ChildCRDs[%d].Name: got %q, want %q", i, got.ChildCRDs[i].Name, out.ChildCRDs[i].Name)
		}
		if !bytes.Equal(bytes.TrimSpace(out.ChildCRDs[i].Spec.Raw), bytes.TrimSpace(got.ChildCRDs[i].Spec.Raw)) {
			t.Errorf("ChildCRDs[%d].Spec.Raw: got %s, want %s", i,
				string(got.ChildCRDs[i].Spec.Raw), string(out.ChildCRDs[i].Spec.Raw))
		}
	}
	// Git pointer comparison.
	if out.Git == nil && got.Git != nil {
		t.Errorf("Git: got %+v, want nil", got.Git)
	}
	if out.Git != nil && got.Git == nil {
		t.Errorf("Git: got nil, want %+v", out.Git)
	}
	if out.Git != nil && got.Git != nil && out.Git.HeadSHA != got.Git.HeadSHA {
		t.Errorf("Git.HeadSHA: got %q, want %q", got.Git.HeadSHA, out.Git.HeadSHA)
	}
}

// stringSlicesEqual compares two string slices treating nil and empty as equal.
func stringSlicesEqual(a, b []string) bool {
	if len(a) == 0 && len(b) == 0 {
		return true
	}
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// fullyPopulatedEnvelopeIn returns an EnvelopeIn with every field set, for
// round-trip fixtures.
func fullyPopulatedEnvelopeIn() EnvelopeIn {
	return EnvelopeIn{
		APIVersion:          APIVersionV1Alpha1,
		Kind:                KindTaskEnvelopeIn,
		TaskUID:             "uid-alpha-0001",
		Role:                "executor",
		Level:               "task",
		Prompt:              "implement the feature",
		FilesTouched:        []string{"pkg/foo/foo.go", "pkg/foo/foo_test.go"},
		DependsOn:           []string{"beta", "gamma"},
		DeclaredOutputPaths: []string{"/workspace/artifacts/P-001/L-001/"},
		Caps: Caps{
			WallClockSeconds: 300,
			Iterations:       50,
			InputTokens:      200000,
			OutputTokens:     8000,
		},
		ProxyEndpoint: "https://127.0.0.1:8443",
		SignedToken:   "hmac-token-base64==",
		Provider: ProviderSpec{
			Vendor: "anthropic",
			Model:  "claude-sonnet-4-6",
			Params: map[string]string{"thinking-budget": "4096"},
		},
		Dev: &Dev{
			TestMode: "success",
		},
	}
}

// fullyPopulatedEnvelopeOut returns an EnvelopeOut with every field set.
func fullyPopulatedEnvelopeOut() EnvelopeOut {
	return EnvelopeOut{
		APIVersion: APIVersionV1Alpha1,
		Kind:       KindTaskEnvelopeOut,
		TaskUID:    "uid-alpha-0001",
		ExitCode:   0,
		Result:     "task completed",
		Reason:     "",
		Usage: Usage{
			InputTokens:         12345,
			OutputTokens:        678,
			EstimatedCostCents:  3,
			Iterations:          5,
			CacheReadTokens:     100,
			CacheCreationTokens: 50,
		},
		Artifacts:   []string{"/workspace/artifacts/P-001/L-001/result.json"},
		CompletedAt: time.Date(2026, 5, 12, 10, 0, 0, 0, time.UTC),
		ChildCRDs: []ChildCRDSpec{
			{
				Kind: "Phase",
				Name: "phase-foo",
				Spec: runtime.RawExtension{Raw: []byte(`{"projectRef":"p1"}`)},
			},
		},
		Git: &GitOutput{HeadSHA: "abc123def456"},
	}
}

// TestEnvelopeIn_RoundTrip builds a fully-populated EnvelopeIn (including
// non-nil Dev), encodes to JSON, decodes into a fresh struct, and asserts
// every field round-trips without data loss.
func TestEnvelopeIn_RoundTrip(t *testing.T) {
	assertRoundTripIn(t, fullyPopulatedEnvelopeIn())
}

// TestEnvelopeIn_RoundTrip_OmitsDevWhenNil asserts that the serialized JSON
// does NOT contain the key "dev" when the Dev field is nil (omitempty contract
// for D-F1 — production envelopes must not be polluted with "dev: null").
func TestEnvelopeIn_RoundTrip_OmitsDevWhenNil(t *testing.T) {
	in := fullyPopulatedEnvelopeIn()
	in.Dev = nil

	data, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	if strings.Contains(string(data), `"dev"`) {
		t.Errorf("serialized JSON contains \"dev\" key but Dev was nil; got: %s", string(data))
	}
}

// TestEnvelopeIn_RoundTrip_OmitsDependsOnWhenNil asserts that the serialized
// JSON does NOT contain "dependsOn" when the slice is nil (omitempty contract).
func TestEnvelopeIn_RoundTrip_OmitsDependsOnWhenNil(t *testing.T) {
	in := fullyPopulatedEnvelopeIn()
	in.DependsOn = nil

	data, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	if strings.Contains(string(data), `"dependsOn"`) {
		t.Errorf("serialized JSON contains \"dependsOn\" key but slice was nil; got: %s", string(data))
	}
}

// TestEnvelopeOut_RoundTrip mirrors TestEnvelopeIn_RoundTrip for the output
// envelope.
func TestEnvelopeOut_RoundTrip(t *testing.T) {
	assertRoundTripOut(t, fullyPopulatedEnvelopeOut())
}

// TestValidateAPIVersionKind_RejectsUnknownAPIVersion asserts that an
// unrecognized apiVersion yields *UnknownAPIVersionError via errors.As.
func TestValidateAPIVersionKind_RejectsUnknownAPIVersion(t *testing.T) {
	err := ValidateAPIVersionKind("tideproject.k8s/v2", KindTaskEnvelopeIn, KindTaskEnvelopeIn)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var target *UnknownAPIVersionError
	if !errors.As(err, &target) {
		t.Fatalf("expected *UnknownAPIVersionError, got %T: %v", err, err)
	}
	if target.APIVersion != "tideproject.k8s/v2" {
		t.Errorf("UnknownAPIVersionError.APIVersion = %q, want %q", target.APIVersion, "tideproject.k8s/v2")
	}
}

// TestValidateAPIVersionKind_RejectsUnknownKind asserts that a recognized
// apiVersion but unrecognized kind yields *UnknownKindError via errors.As.
func TestValidateAPIVersionKind_RejectsUnknownKind(t *testing.T) {
	err := ValidateAPIVersionKind(APIVersionV1Alpha1, "Bogus", KindTaskEnvelopeIn)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var target *UnknownKindError
	if !errors.As(err, &target) {
		t.Fatalf("expected *UnknownKindError, got %T: %v", err, err)
	}
	if target.Kind != "Bogus" {
		t.Errorf("UnknownKindError.Kind = %q, want %q", target.Kind, "Bogus")
	}
}

// TestValidateAPIVersionKind_AcceptsValid asserts that a valid apiVersion and
// matching kind returns nil.
func TestValidateAPIVersionKind_AcceptsValid(t *testing.T) {
	err := ValidateAPIVersionKind(APIVersionV1Alpha1, KindTaskEnvelopeIn, KindTaskEnvelopeIn)
	if err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
}

// TestValidateAPIVersionKind_AcceptsOut asserts that v1alpha1 + TaskEnvelopeOut
// passes validation when expectedKind is KindTaskEnvelopeOut.
func TestValidateAPIVersionKind_AcceptsOut(t *testing.T) {
	err := ValidateAPIVersionKind(APIVersionV1Alpha1, KindTaskEnvelopeOut, KindTaskEnvelopeOut)
	if err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
}

// TestEnvelopeIn_Constants asserts that the exported constants carry the
// expected literal values per D-A3.
func TestEnvelopeIn_Constants(t *testing.T) {
	if APIVersionV1Alpha1 != "tideproject.k8s/v1alpha1" {
		t.Errorf("APIVersionV1Alpha1 = %q, want %q", APIVersionV1Alpha1, "tideproject.k8s/v1alpha1")
	}
	if KindTaskEnvelopeIn != "TaskEnvelopeIn" {
		t.Errorf("KindTaskEnvelopeIn = %q, want %q", KindTaskEnvelopeIn, "TaskEnvelopeIn")
	}
	if KindTaskEnvelopeOut != "TaskEnvelopeOut" {
		t.Errorf("KindTaskEnvelopeOut = %q, want %q", KindTaskEnvelopeOut, "TaskEnvelopeOut")
	}
}

// TestEnvelopeIn_SubtestTable is the table-driven companion to the top-level
// TestEnvelopeIn_* functions above (dual-shape per PATTERNS.md §envelope_test.go).
// `go test -run TestEnvelopeIn_SubtestTable/<Name>` selects an individual case.
func TestEnvelopeIn_SubtestTable(t *testing.T) {
	type tc struct {
		name string
		in   EnvelopeIn
	}
	cases := []tc{
		{
			name: "FullyPopulated",
			in:   fullyPopulatedEnvelopeIn(),
		},
		{
			name: "NilDependsOn",
			in: func() EnvelopeIn {
				e := fullyPopulatedEnvelopeIn()
				e.DependsOn = nil
				return e
			}(),
		},
		{
			name: "NilDev",
			in: func() EnvelopeIn {
				e := fullyPopulatedEnvelopeIn()
				e.Dev = nil
				return e
			}(),
		},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			assertRoundTripIn(t, c.in)
		})
	}
}

// TestEnvelopeOut_SubtestTable is the table-driven companion for EnvelopeOut.
func TestEnvelopeOut_SubtestTable(t *testing.T) {
	type tc struct {
		name string
		out  EnvelopeOut
	}
	cases := []tc{
		{
			name: "FullyPopulated",
			out:  fullyPopulatedEnvelopeOut(),
		},
		{
			name: "NonZeroExitCode",
			out: func() EnvelopeOut {
				e := fullyPopulatedEnvelopeOut()
				e.ExitCode = 1
				e.Reason = "forced-failure"
				return e
			}(),
		},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			assertRoundTripOut(t, c.out)
		})
	}
}

// --- Phase 3 envelope-schema-bump tests (plan 03-01 Task 2) ---

// TestEnvelopeIn_PlannerLevel_RoundTrip is plan-03-01 Task 2 Test 1: an
// EnvelopeIn with Role="planner", Level="milestone", Provider populated
// round-trips through json and ValidateAPIVersionKind still passes for the
// canonical apiVersion+kind pair (preserves Phase 2 D-A3 envelope rejection
// contract).
func TestEnvelopeIn_PlannerLevel_RoundTrip(t *testing.T) {
	in := fullyPopulatedEnvelopeIn()
	in.Role = "planner"
	in.Level = "milestone"
	in.Provider = ProviderSpec{
		Vendor: "anthropic",
		Model:  "claude-opus-4-7",
		Params: map[string]string{"thinking-budget": "16384"},
	}
	assertRoundTripIn(t, in)

	if err := ValidateAPIVersionKind(in.APIVersion, in.Kind, KindTaskEnvelopeIn); err != nil {
		t.Errorf("ValidateAPIVersionKind: unexpected error %v", err)
	}
}

// TestEnvelopeIn_UnknownAPIVersion_StillRejected is plan-03-01 Task 2 Test 2:
// the Phase 2 D-A3 schema-rejection contract is preserved across the Phase 3
// schema bump — an unknown apiVersion (tideproject.k8s/v9999) is still
// rejected by ValidateAPIVersionKind even though the body carries Phase 3
// fields.
func TestEnvelopeIn_UnknownAPIVersion_StillRejected(t *testing.T) {
	err := ValidateAPIVersionKind("tideproject.k8s/v9999", KindTaskEnvelopeIn, KindTaskEnvelopeIn)
	if err == nil {
		t.Fatal("expected error for unknown apiVersion, got nil")
	}
	var target *UnknownAPIVersionError
	if !errors.As(err, &target) {
		t.Fatalf("expected *UnknownAPIVersionError, got %T: %v", err, err)
	}
	if target.APIVersion != "tideproject.k8s/v9999" {
		t.Errorf("UnknownAPIVersionError.APIVersion = %q, want %q",
			target.APIVersion, "tideproject.k8s/v9999")
	}
}

// TestEnvelopeOut_ChildCRDs_RoundTrip is plan-03-01 Task 2 Test 3: an
// EnvelopeOut with a populated ChildCRDs slice (Phase materialization shape)
// round-trips through json; the raw spec bytes survive the round-trip
// per D-A1.
func TestEnvelopeOut_ChildCRDs_RoundTrip(t *testing.T) {
	out := fullyPopulatedEnvelopeOut()
	out.ChildCRDs = []ChildCRDSpec{
		{
			Kind: "Phase",
			Name: "phase-foo",
			Spec: runtime.RawExtension{Raw: []byte(`{}`)},
		},
	}
	assertRoundTripOut(t, out)
}

// TestEnvelopeOut_OmitsChildCRDsWhenEmpty asserts that the serialized JSON
// does NOT contain "childCRDs" when the slice is nil (omitempty contract —
// executor-level EnvelopeOut never materializes child CRDs and should not
// carry the field). Companion to plan-03-01 Task 2 Test 3.
func TestEnvelopeOut_OmitsChildCRDsWhenEmpty(t *testing.T) {
	out := fullyPopulatedEnvelopeOut()
	out.ChildCRDs = nil

	data, err := json.Marshal(out)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	if strings.Contains(string(data), `"childCRDs"`) {
		t.Errorf(`serialized JSON contains "childCRDs" key but slice was nil; got: %s`, string(data))
	}
}

// TestEnvelopeOut_Git_RoundTrip is plan-03-01 Task 2 Test 4: an EnvelopeOut
// with Git=&GitOutput{HeadSHA:...} round-trips; nil Git omits the "git" JSON
// field per the *GitOutput + omitempty contract.
func TestEnvelopeOut_Git_RoundTrip(t *testing.T) {
	out := fullyPopulatedEnvelopeOut()
	out.Git = &GitOutput{HeadSHA: "abc123"}
	assertRoundTripOut(t, out)

	// Nil Git → no "git" key in JSON.
	out.Git = nil
	data, err := json.Marshal(out)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	if strings.Contains(string(data), `"git"`) {
		t.Errorf(`serialized JSON contains "git" key but Git was nil; got: %s`, string(data))
	}
}

// TestUsage_CacheTokens is plan-03-01 Task 2 Test 5: Usage.CacheReadTokens
// and Usage.CacheCreationTokens round-trip via JSON. Field tags MUST be
// `cacheReadTokens` and `cacheCreationTokens` per D-C5 (maps to Anthropic
// stream-json cache_read_input_tokens / cache_creation_input_tokens).
func TestUsage_CacheTokens(t *testing.T) {
	u := Usage{
		InputTokens:         200,
		OutputTokens:        50,
		EstimatedCostCents:  1,
		Iterations:          1,
		CacheReadTokens:     100,
		CacheCreationTokens: 50,
	}
	data, err := json.Marshal(u)
	if err != nil {
		t.Fatalf("json.Marshal(Usage): %v", err)
	}
	if !strings.Contains(string(data), `"cacheReadTokens":100`) {
		t.Errorf(`serialized JSON missing "cacheReadTokens":100; got: %s`, string(data))
	}
	if !strings.Contains(string(data), `"cacheCreationTokens":50`) {
		t.Errorf(`serialized JSON missing "cacheCreationTokens":50; got: %s`, string(data))
	}
	var got Usage
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("json.Unmarshal(Usage): %v", err)
	}
	if got != u {
		t.Errorf("Usage round-trip mismatch: got %+v, want %+v", got, u)
	}
}

// TestEnvelopeIn_ProviderTag asserts that EnvelopeIn serializes the Provider
// field under the canonical "provider" JSON key (D-C3 — value type, not
// pointer; every dispatch carries a provider).
func TestEnvelopeIn_ProviderTag(t *testing.T) {
	in := fullyPopulatedEnvelopeIn()
	data, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	if !strings.Contains(string(data), `"provider"`) {
		t.Errorf(`serialized JSON missing "provider" key; got: %s`, string(data))
	}
	if !strings.Contains(string(data), `"vendor":"anthropic"`) {
		t.Errorf(`serialized JSON missing "vendor":"anthropic"; got: %s`, string(data))
	}
}
