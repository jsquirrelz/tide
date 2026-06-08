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
	"strings"
	"testing"
)

// writeChild writes a child-CRD JSON file into childrenDir for a test.
func writeChild(t *testing.T, childrenDir, name, body string) {
	t.Helper()
	if err := os.MkdirAll(childrenDir, 0o755); err != nil {
		t.Fatalf("mkdir children: %v", err)
	}
	if err := os.WriteFile(filepath.Join(childrenDir, name), []byte(body), 0o644); err != nil {
		t.Fatalf("write child %q: %v", name, err)
	}
}

// Test: defect #5 happy path — fixture files in the children dir populate
// ChildCRDs in deterministic filename order, with kind/name preserved and the
// raw spec carried through.
func TestReadChildCRDs_PopulatesFromFixtures(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "children")
	// Written out of order to prove sort-by-filename determinism.
	writeChild(t, dir, "phase-02.json", `{"kind":"Phase","name":"phase-02-b","spec":{"milestoneRef":"m1"}}`)
	writeChild(t, dir, "phase-01.json", `{"kind":"Phase","name":"phase-01-a","spec":{"milestoneRef":"m1"}}`)
	// A non-JSON file must be ignored, not parsed.
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("ignore me"), 0o644); err != nil {
		t.Fatalf("write notes: %v", err)
	}

	got, err := readChildCRDs(dir, "envelopes/test-uid/children")
	if err != nil {
		t.Fatalf("readChildCRDs: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2 (.txt ignored)", len(got))
	}
	if got[0].Name != "phase-01-a" || got[1].Name != "phase-02-b" {
		t.Errorf("order = [%q, %q], want [phase-01-a, phase-02-b]", got[0].Name, got[1].Name)
	}
	if got[0].Kind != "Phase" {
		t.Errorf("kind = %q, want Phase", got[0].Kind)
	}
	if !strings.Contains(string(got[0].Spec.Raw), "milestoneRef") {
		t.Errorf("spec.Raw missing milestoneRef: %s", got[0].Spec.Raw)
	}
	// Defect #10b: each child carries its workspace-relative origin path so the
	// controller can wire Task.Spec.PromptPath.
	if got[0].SourcePath != "envelopes/test-uid/children/phase-01.json" {
		t.Errorf("SourcePath[0] = %q, want envelopes/test-uid/children/phase-01.json", got[0].SourcePath)
	}
	if got[1].SourcePath != "envelopes/test-uid/children/phase-02.json" {
		t.Errorf("SourcePath[1] = %q, want envelopes/test-uid/children/phase-02.json", got[1].SourcePath)
	}
}

// Test: a missing children dir is zero children, not an error (a no-op planner
// or executor-shaped run must not fail on a clean exit).
func TestReadChildCRDs_MissingDirIsEmpty(t *testing.T) {
	got, err := readChildCRDs(filepath.Join(t.TempDir(), "nonexistent"), "envelopes/test-uid/children")
	if err != nil {
		t.Fatalf("readChildCRDs on missing dir: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("len = %d, want 0", len(got))
	}
}

// Test: Kind allowlist — a file declaring a non-TIDE Kind poisons the batch.
func TestReadChildCRDs_RejectsDisallowedKind(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "children")
	writeChild(t, dir, "evil.json", `{"kind":"Secret","name":"steal","spec":{}}`)

	_, err := readChildCRDs(dir, "envelopes/test-uid/children")
	if err == nil {
		t.Fatal("expected error for disallowed kind Secret, got nil")
	}
	if !strings.Contains(err.Error(), "disallowed kind") {
		t.Errorf("error = %v, want disallowed-kind message", err)
	}
}

// Test: an empty-name child is rejected (the controller uses name as
// metadata.name).
func TestReadChildCRDs_RejectsEmptyName(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "children")
	writeChild(t, dir, "noname.json", `{"kind":"Phase","name":"","spec":{}}`)

	_, err := readChildCRDs(dir, "envelopes/test-uid/children")
	if err == nil {
		t.Fatal("expected error for empty name, got nil")
	}
	if !strings.Contains(err.Error(), "empty name") {
		t.Errorf("error = %v, want empty-name message", err)
	}
}

// Test: traversal defense — a symlink entry in the children dir is rejected
// even if it points at a valid child file outside the dir.
func TestReadChildCRDs_RejectsSymlink(t *testing.T) {
	base := t.TempDir()
	dir := filepath.Join(base, "children")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// A legit-looking target outside the children dir.
	outside := filepath.Join(base, "outside.json")
	if err := os.WriteFile(outside, []byte(`{"kind":"Phase","name":"p","spec":{}}`), 0o644); err != nil {
		t.Fatalf("write outside: %v", err)
	}
	link := filepath.Join(dir, "link.json")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlink unsupported on this platform: %v", err)
	}

	_, err := readChildCRDs(dir, "envelopes/test-uid/children")
	if err == nil {
		t.Fatal("expected error for symlink entry, got nil")
	}
	if !strings.Contains(err.Error(), "symlink") {
		t.Errorf("error = %v, want symlink-rejection message", err)
	}
}

// Test: malformed JSON in a child file surfaces a parse error (poisoned batch).
func TestReadChildCRDs_RejectsMalformedJSON(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "children")
	writeChild(t, dir, "bad.json", `{not json`)

	_, err := readChildCRDs(dir, "envelopes/test-uid/children")
	if err == nil {
		t.Fatal("expected parse error for malformed JSON, got nil")
	}
	if !strings.Contains(err.Error(), "parse child file") {
		t.Errorf("error = %v, want parse-error message", err)
	}
}
