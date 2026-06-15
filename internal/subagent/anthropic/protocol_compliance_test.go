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

package anthropic

import (
	"os"
	"path/filepath"
	"testing"
)

// TestReadChildCRDs_ValidTask asserts that a well-formed Task child-CRD JSON
// file (Kind in childKindAllowlist, non-empty name, non-empty spec) is parsed
// by readChildCRDs without error and returned as a single spec.
//
// ChildCRDSpec JSON format: {"kind":"Task","name":"...","spec":{...}} —
// top-level "name" field, NOT nested under "metadata" (ChildCRDSpec struct, not K8s Object).
func TestReadChildCRDs_ValidTask(t *testing.T) {
	dir := t.TempDir()
	// Valid Task: kind "Task" (in allowlist) + non-empty name + non-empty spec.
	content := []byte(`{"kind":"Task","name":"task-01","spec":{"prompt":"do the thing"}}`)
	if err := os.WriteFile(filepath.Join(dir, "task-01.json"), content, 0600); err != nil {
		t.Fatalf("write fixture file: %v", err)
	}

	specs, err := readChildCRDs(dir, "")
	if err != nil {
		t.Fatalf("valid fixture: unexpected error: %v", err)
	}
	if len(specs) != 1 {
		t.Errorf("expected 1 spec, got %d", len(specs))
	}
}

// TestReadChildCRDs_BadKind asserts that a child-CRD JSON file with a Kind
// not in childKindAllowlist (e.g. "Forbidden") causes readChildCRDs to return
// an error. This exercises the allowlist rejection path (T-18-02-01 mitigation).
func TestReadChildCRDs_BadKind(t *testing.T) {
	dir := t.TempDir()
	// "Forbidden" is not in childKindAllowlist (Milestone/Phase/Plan/Task/Wave only).
	content := []byte(`{"kind":"Forbidden","name":"bad-01","spec":{}}`)
	if err := os.WriteFile(filepath.Join(dir, "bad.json"), content, 0600); err != nil {
		t.Fatalf("write fixture file: %v", err)
	}

	_, err := readChildCRDs(dir, "")
	if err == nil {
		t.Error("expected error for forbidden kind, got nil")
	}
}

// TestReadChildCRDs_MissingName asserts that a child-CRD JSON file with an
// empty name causes readChildCRDs to return an error. An empty Name is rejected
// because the controller uses it as metadata.name for the materialized child CRD.
func TestReadChildCRDs_MissingName(t *testing.T) {
	dir := t.TempDir()
	// Empty name — must be rejected by the name-required check.
	content := []byte(`{"kind":"Task","name":"","spec":{}}`)
	if err := os.WriteFile(filepath.Join(dir, "noname.json"), content, 0600); err != nil {
		t.Fatalf("write fixture file: %v", err)
	}

	_, err := readChildCRDs(dir, "")
	if err == nil {
		t.Error("expected error for empty Name, got nil")
	}
}
