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
	"strings"
	"testing"
)

// TestTerminalReason_EnvelopeOut_RoundTrip builds an EnvelopeOut carrying a
// non-zero TerminalReason, LoopRunID, AttemptID, and a fully-populated
// RunEvidence, marshals it, unmarshals into a fresh struct, and asserts every
// new field round-trips without data loss (EXEC-01/EXEC-02/EXEC-03).
func TestTerminalReason_EnvelopeOut_RoundTrip(t *testing.T) {
	out := fullyPopulatedEnvelopeOut()
	out.TerminalReason = TerminalReasonCapExceeded
	out.LoopRunID = "uid-alpha-0001"
	out.AttemptID = "uid-alpha-0001-2"
	out.RunEvidence = &RunEvidence{
		SpecID:            "spec-001",
		LockingCommit:     "deadbeefcafefeed",
		Commands:          []string{"go test ./...", "go vet ./..."},
		EvaluatorVersions: []string{"gate-v1"},
		ChangedFiles: []ChangedFile{
			{Path: "pkg/foo/foo.go", Status: "M"},
			{Path: "pkg/foo/foo_test.go", Status: "A"},
		},
		ChangedFileTotal: 2,
		Model:            "claude-sonnet-4-6",
		PromptVersion:    "v1",
		RuntimeVersion:   "claude-code/2.1.178",
	}

	data, err := json.Marshal(out)
	if err != nil {
		t.Fatalf("json.Marshal(EnvelopeOut): %v", err)
	}
	var got EnvelopeOut
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("json.Unmarshal(EnvelopeOut): %v", err)
	}

	if got.TerminalReason != out.TerminalReason {
		t.Errorf("TerminalReason: got %q, want %q", got.TerminalReason, out.TerminalReason)
	}
	if got.LoopRunID != out.LoopRunID {
		t.Errorf("LoopRunID: got %q, want %q", got.LoopRunID, out.LoopRunID)
	}
	if got.AttemptID != out.AttemptID {
		t.Errorf("AttemptID: got %q, want %q", got.AttemptID, out.AttemptID)
	}
	if got.RunEvidence == nil {
		t.Fatal("RunEvidence: got nil, want non-nil")
	}
	want := *out.RunEvidence
	gotEv := *got.RunEvidence
	if gotEv.SpecID != want.SpecID {
		t.Errorf("RunEvidence.SpecID: got %q, want %q", gotEv.SpecID, want.SpecID)
	}
	if gotEv.LockingCommit != want.LockingCommit {
		t.Errorf("RunEvidence.LockingCommit: got %q, want %q", gotEv.LockingCommit, want.LockingCommit)
	}
	if !stringSlicesEqual(gotEv.Commands, want.Commands) {
		t.Errorf("RunEvidence.Commands: got %v, want %v", gotEv.Commands, want.Commands)
	}
	if !stringSlicesEqual(gotEv.EvaluatorVersions, want.EvaluatorVersions) {
		t.Errorf("RunEvidence.EvaluatorVersions: got %v, want %v", gotEv.EvaluatorVersions, want.EvaluatorVersions)
	}
	if len(gotEv.ChangedFiles) != len(want.ChangedFiles) {
		t.Fatalf("RunEvidence.ChangedFiles length: got %d, want %d", len(gotEv.ChangedFiles), len(want.ChangedFiles))
	}
	for i := range want.ChangedFiles {
		if gotEv.ChangedFiles[i] != want.ChangedFiles[i] {
			t.Errorf("RunEvidence.ChangedFiles[%d]: got %+v, want %+v", i, gotEv.ChangedFiles[i], want.ChangedFiles[i])
		}
	}
	if gotEv.ChangedFileTotal != want.ChangedFileTotal {
		t.Errorf("RunEvidence.ChangedFileTotal: got %d, want %d", gotEv.ChangedFileTotal, want.ChangedFileTotal)
	}
	if gotEv.Model != want.Model {
		t.Errorf("RunEvidence.Model: got %q, want %q", gotEv.Model, want.Model)
	}
	if gotEv.PromptVersion != want.PromptVersion {
		t.Errorf("RunEvidence.PromptVersion: got %q, want %q", gotEv.PromptVersion, want.PromptVersion)
	}
	if gotEv.RuntimeVersion != want.RuntimeVersion {
		t.Errorf("RunEvidence.RuntimeVersion: got %q, want %q", gotEv.RuntimeVersion, want.RuntimeVersion)
	}
}

// TestTerminalReason_ZeroValueIsInvalid is the D-02 fail-closed regression
// table: the zero value and any unrecognized string are invalid; exactly the
// 5 defined consts are valid.
func TestTerminalReason_ZeroValueIsInvalid(t *testing.T) {
	cases := []struct {
		name string
		tr   TerminalReason
		want bool
	}{
		{"ZeroValue", TerminalReason(""), false},
		{"Completed", TerminalReasonCompleted, true},
		{"CapExceeded", TerminalReasonCapExceeded, true},
		{"Blocked", TerminalReasonBlocked, true},
		{"ToolFailure", TerminalReasonToolFailure, true},
		{"InvalidOutput", TerminalReasonInvalidOutput, true},
		{"UppercaseCompleted", TerminalReason("COMPLETED"), false},
		{"Done", TerminalReason("done"), false},
		{"Approved", TerminalReason("approved"), false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.tr.Valid(); got != c.want {
				t.Errorf("TerminalReason(%q).Valid() = %v, want %v", string(c.tr), got, c.want)
			}
		})
	}
}

// TestRunEvidence_BoundedTruncatesPathological builds a pathological
// RunEvidence (500 changed files with 4KB paths, 50 commands of 10KB each)
// and asserts Bounded() truncates every field to its bounds const while
// preserving ChangedFileTotal as the pre-truncation count (T-50-01).
func TestRunEvidence_BoundedTruncatesPathological(t *testing.T) {
	changedFiles := make([]ChangedFile, 500)
	for i := range changedFiles {
		changedFiles[i] = ChangedFile{
			Path:   strings.Repeat("p", 4096),
			Status: "M",
		}
	}
	commands := make([]string, 50)
	for i := range commands {
		commands[i] = strings.Repeat("c", 10*1024)
	}
	e := RunEvidence{
		ChangedFiles: changedFiles,
		Commands:     commands,
	}

	got := e.Bounded()

	if len(got.ChangedFiles) > MaxRunEvidenceChangedFiles {
		t.Errorf("ChangedFiles length = %d, want <= %d", len(got.ChangedFiles), MaxRunEvidenceChangedFiles)
	}
	for i, f := range got.ChangedFiles {
		if len(f.Path) > MaxRunEvidencePathBytes {
			t.Errorf("ChangedFiles[%d].Path length = %d, want <= %d", i, len(f.Path), MaxRunEvidencePathBytes)
		}
	}
	if got.ChangedFileTotal != 500 {
		t.Errorf("ChangedFileTotal = %d, want 500 (pre-truncation count)", got.ChangedFileTotal)
	}
	if len(got.Commands) > MaxRunEvidenceCommands {
		t.Errorf("Commands length = %d, want <= %d", len(got.Commands), MaxRunEvidenceCommands)
	}
	for i, c := range got.Commands {
		if len(c) > MaxRunEvidenceCommandBytes {
			t.Errorf("Commands[%d] length = %d, want <= %d", i, len(c), MaxRunEvidenceCommandBytes)
		}
	}

	data, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("json.Marshal(RunEvidence): %v", err)
	}
	if len(data) >= 64*1024 {
		t.Errorf("json.Marshal(Bounded RunEvidence) size = %d bytes, want < 64KB", len(data))
	}
}
