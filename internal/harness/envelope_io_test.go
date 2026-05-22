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

package harness

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	pkgdispatch "github.com/jsquirrelz/tide/pkg/dispatch"
)

// TestReadEnvelopeIn_RoundTrip writes a fixture envelope JSON to a tmpdir and
// reads it back, asserting that all fields survive the round-trip.
func TestReadEnvelopeIn_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	inPath := filepath.Join(dir, "in.json")

	fixture := pkgdispatch.EnvelopeIn{
		APIVersion:          pkgdispatch.APIVersionV1Alpha1,
		Kind:                pkgdispatch.KindTaskEnvelopeIn,
		TaskUID:             "abc-123",
		Role:                "executor",
		Level:               "task",
		Prompt:              "do the thing",
		FilesTouched:        []string{"src/main.go"},
		DeclaredOutputPaths: []string{"artifacts/M-001/P-001/L-001"},
		Caps: pkgdispatch.Caps{
			WallClockSeconds: 60,
			Iterations:       10,
			InputTokens:      1000,
			OutputTokens:     1000,
		},
	}

	if err := WriteEnvelopeIn(inPath, fixture); err != nil {
		t.Fatalf("WriteEnvelopeIn: %v", err)
	}

	got, err := ReadEnvelopeIn(inPath)
	if err != nil {
		t.Fatalf("ReadEnvelopeIn: %v", err)
	}

	if got.TaskUID != fixture.TaskUID {
		t.Errorf("TaskUID: got %q, want %q", got.TaskUID, fixture.TaskUID)
	}
	if got.Role != fixture.Role {
		t.Errorf("Role: got %q, want %q", got.Role, fixture.Role)
	}
	if got.Level != fixture.Level {
		t.Errorf("Level: got %q, want %q", got.Level, fixture.Level)
	}
	if got.Caps.WallClockSeconds != fixture.Caps.WallClockSeconds {
		t.Errorf("Caps.WallClockSeconds: got %d, want %d",
			got.Caps.WallClockSeconds, fixture.Caps.WallClockSeconds)
	}
}

// TestReadEnvelopeIn_RejectsUnknownAPIVersion verifies that an envelope with
// an unrecognized apiVersion returns an *UnknownAPIVersionError.
func TestReadEnvelopeIn_RejectsUnknownAPIVersion(t *testing.T) {
	dir := t.TempDir()
	inPath := filepath.Join(dir, "in.json")

	// Write a JSON fixture with a bad apiVersion.
	badJSON := `{"apiVersion":"tideproject.k8s/v999","kind":"TaskEnvelopeIn","taskUID":"x"}`
	if err := os.WriteFile(inPath, []byte(badJSON), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	_, err := ReadEnvelopeIn(inPath)
	if err == nil {
		t.Fatal("expected error for unknown apiVersion, got nil")
	}
	var uvErr *pkgdispatch.UnknownAPIVersionError
	if !errors.As(err, &uvErr) {
		t.Errorf("expected *UnknownAPIVersionError, got %T: %v", err, err)
	}
}

// TestWriteEnvelopeOut_CreatesAncestorDirs verifies that WriteEnvelopeOut
// creates missing ancestor directories via os.MkdirAll (D-G2 lazy mkdir).
func TestWriteEnvelopeOut_CreatesAncestorDirs(t *testing.T) {
	base := t.TempDir()
	outPath := filepath.Join(base, "a", "b", "c", "out.json")

	out := pkgdispatch.EnvelopeOut{
		APIVersion: pkgdispatch.APIVersionV1Alpha1,
		Kind:       pkgdispatch.KindTaskEnvelopeOut,
		TaskUID:    "abc-123",
		Result:     "success",
	}

	if err := WriteEnvelopeOut(outPath, out); err != nil {
		t.Fatalf("WriteEnvelopeOut: %v", err)
	}

	if _, err := os.Stat(filepath.Join(base, "a", "b", "c")); os.IsNotExist(err) {
		t.Error("ancestor directory a/b/c was not created")
	}
	if _, err := os.Stat(outPath); os.IsNotExist(err) {
		t.Error("output file was not created")
	}
}
