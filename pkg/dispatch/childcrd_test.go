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
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/runtime"
)

// TestChildCRDSpec_RoundTrip asserts that a fully-populated ChildCRDSpec
// (Kind + Name + Spec runtime.RawExtension) round-trips through
// json.Marshal+json.Unmarshal with the raw spec bytes preserved exactly.
// Field tags MUST be `kind`, `name`, `spec` (D-A1).
func TestChildCRDSpec_RoundTrip(t *testing.T) {
	rawSpec := []byte(`{"projectRef":"p1","milestoneRef":"m1"}`)
	in := ChildCRDSpec{
		Kind: "Phase",
		Name: "phase-foo",
		Spec: runtime.RawExtension{Raw: rawSpec},
	}
	data, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("json.Marshal(ChildCRDSpec): %v", err)
	}
	if !strings.Contains(string(data), `"kind":"Phase"`) {
		t.Errorf(`serialized JSON missing "kind":"Phase"; got: %s`, string(data))
	}
	if !strings.Contains(string(data), `"name":"phase-foo"`) {
		t.Errorf(`serialized JSON missing "name":"phase-foo"; got: %s`, string(data))
	}
	if !strings.Contains(string(data), `"spec"`) {
		t.Errorf(`serialized JSON missing "spec" key; got: %s`, string(data))
	}

	var got ChildCRDSpec
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("json.Unmarshal(ChildCRDSpec): %v", err)
	}
	if got.Kind != in.Kind {
		t.Errorf("Kind: got %q, want %q", got.Kind, in.Kind)
	}
	if got.Name != in.Name {
		t.Errorf("Name: got %q, want %q", got.Name, in.Name)
	}
	// runtime.RawExtension round-trip: compare raw bytes via bytes.Equal.
	// json.Unmarshal of a RawExtension populates .Raw with the JSON-encoded
	// representation of the sub-document, preserving the original bytes.
	if !bytes.Equal(bytes.TrimSpace(got.Spec.Raw), bytes.TrimSpace(in.Spec.Raw)) {
		t.Errorf("Spec.Raw: got %s, want %s", string(got.Spec.Raw), string(in.Spec.Raw))
	}
}

// TestChildCRDSpec_RoundTrip_EmptyKind asserts that the Kind field is required
// (no omitempty); an empty-kind ChildCRDSpec still serializes the "kind" key
// with an empty string. Consumer-side allowlist validation (T-308 mitigation
// deferred to plan 03-08) is the gate, not the JSON tag.
func TestChildCRDSpec_RoundTrip_EmptyKind(t *testing.T) {
	in := ChildCRDSpec{
		Kind: "",
		Name: "orphan",
		Spec: runtime.RawExtension{Raw: []byte(`{}`)},
	}
	data, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	if !strings.Contains(string(data), `"kind":""`) {
		t.Errorf(`serialized JSON missing "kind":"" (required field); got: %s`, string(data))
	}
}

// TestChildCRDSpec_RoundTrip_NestedJSON asserts that a non-trivial nested JSON
// payload in Spec.Raw survives a marshal/unmarshal cycle without re-encoding
// loss. This is the load-bearing property: the orchestrator decodes Spec.Raw
// into the appropriate typed Spec at materialization time (plan 03-08).
func TestChildCRDSpec_RoundTrip_NestedJSON(t *testing.T) {
	nested := []byte(`{"planRef":"plan-001","dependsOn":["alpha","beta"],"filesTouched":["pkg/foo/foo.go"]}`)
	in := ChildCRDSpec{
		Kind: "Task",
		Name: "task-gamma",
		Spec: runtime.RawExtension{Raw: nested},
	}
	data, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	var got ChildCRDSpec
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	// The unmarshaled Raw should re-decode to the same logical JSON structure.
	var origMap, gotMap map[string]any
	if err := json.Unmarshal(in.Spec.Raw, &origMap); err != nil {
		t.Fatalf("json.Unmarshal(in.Spec.Raw): %v", err)
	}
	if err := json.Unmarshal(got.Spec.Raw, &gotMap); err != nil {
		t.Fatalf("json.Unmarshal(got.Spec.Raw): %v", err)
	}
	origJSON, _ := json.Marshal(origMap)
	gotJSON, _ := json.Marshal(gotMap)
	if !bytes.Equal(origJSON, gotJSON) {
		t.Errorf("nested JSON differs after round-trip:\n  orig: %s\n  got:  %s", origJSON, gotJSON)
	}
}
