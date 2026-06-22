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
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	dag "github.com/jsquirrelz/tide/pkg/dag"
	dispatch "github.com/jsquirrelz/tide/pkg/dispatch"
)

// makeValidEnvelope returns valid out.json bytes for the given taskUID and child count.
func makeValidEnvelope(taskUID string, childCount int) []byte {
	children := make([]dispatch.ChildCRDSpec, childCount)
	for i := range children {
		children[i] = dispatch.ChildCRDSpec{
			Kind: "Milestone",
			Name: "ms-" + string(rune('0'+i)),
		}
	}
	env := dispatch.EnvelopeOut{
		APIVersion: dispatch.APIVersionV1Alpha1,
		Kind:       dispatch.KindTaskEnvelopeOut,
		TaskUID:    taskUID,
		ExitCode:   0,
		ChildCRDs:  children,
		ChildCount: childCount,
	}
	data, err := json.Marshal(env)
	if err != nil {
		panic("makeValidEnvelope: " + err.Error())
	}
	return data
}

// makeBundleDir sets up a bundle directory for testing.
// The bundleDir must have been created by the caller (t.TempDir()).
// Returns the directory path.
func makeBundleDir(t *testing.T, manifest BundleManifest, envelopes map[string][]byte) string {
	t.Helper()
	dir := t.TempDir()

	// Write seed-manifest.json.
	manifestData, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, BundleFileSeedManifest), manifestData, 0o644); err != nil {
		t.Fatal(err)
	}

	// Write pvc-envelopes.tgz (nested tgz with envelopes/<uid>/out.json).
	innerFiles := make(map[string][]byte)
	for uid, data := range envelopes {
		innerFiles["envelopes/"+uid+"/out.json"] = data
	}
	innerTgzData, err := WritePVCEnvelopesTgz(innerFiles)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, BundleFilePVCEnvelopes), innerTgzData, 0o644); err != nil {
		t.Fatal(err)
	}

	// Write required bundle files.
	for _, name := range []string{BundleFileProject, BundleFileMilestones, BundleFilePhases, BundleFilePlans, BundleFileSeedOutline} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("---"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	return dir
}

// TestValidateBundle_AllAdopt tests that a valid bundle returns all adopt verdicts.
func TestValidateBundle(t *testing.T) {
	t.Run("all adopt", func(t *testing.T) {
		uid := "uid-ms-01"
		envBytes := makeValidEnvelope(uid, 2)
		sha := computeEnvelopeSHA256(envBytes)

		manifest := BundleManifest{
			Milestones: []BundleEntry{
				{
					Name:   "ms-01",
					FQName: MilestoneFQName("ms-01"),
					OldUID: uid,
					SHA256: sha,
				},
			},
			Phases: []BundleEntry{},
			Plans:  []BundleEntry{},
		}

		dir := makeBundleDir(t, manifest, map[string][]byte{uid: envBytes})

		result, err := ValidateBundle(dir)
		if err != nil {
			t.Fatalf("ValidateBundle: %v", err)
		}
		if result.CycleRejected {
			t.Error("expected no cycle rejection")
		}
		if len(result.Rows) != 1 {
			t.Fatalf("expected 1 row, got %d", len(result.Rows))
		}
		if result.Rows[0].Verdict != "adopt" {
			t.Errorf("expected verdict=adopt, got %q (reason: %s)", result.Rows[0].Verdict, result.Rows[0].Reason)
		}
	})
}

// TestDryRun_ChecksumMismatch tests that a tampered out.json → re-plan: checksum mismatch.
func TestDryRun(t *testing.T) {
	t.Run("checksum mismatch", func(t *testing.T) {
		uid := "uid-ms-02"
		goodBytes := makeValidEnvelope(uid, 1)
		sha := computeEnvelopeSHA256(goodBytes)

		// Tamper: compute sha over good bytes but write different bytes.
		tamperedBytes := makeValidEnvelope(uid, 2)

		manifest := BundleManifest{
			Milestones: []BundleEntry{
				{
					Name:   "ms-02",
					FQName: MilestoneFQName("ms-02"),
					OldUID: uid,
					SHA256: sha, // sha from goodBytes, but envelope has tamperedBytes
				},
			},
			Phases: []BundleEntry{},
			Plans:  []BundleEntry{},
		}

		dir := makeBundleDir(t, manifest, map[string][]byte{uid: tamperedBytes})

		result, err := ValidateBundle(dir)
		if err != nil {
			t.Fatalf("ValidateBundle: %v", err)
		}
		if len(result.Rows) != 1 {
			t.Fatalf("expected 1 row, got %d", len(result.Rows))
		}
		if result.Rows[0].Verdict != "re-plan" {
			t.Errorf("expected verdict=re-plan, got %q", result.Rows[0].Verdict)
		}
		if result.Rows[0].Reason == "" {
			t.Error("expected non-empty reason for checksum mismatch")
		}
		if !containsAny(result.Rows[0].Reason, "checksum", "sha256") {
			t.Errorf("reason should mention checksum/sha256: %q", result.Rows[0].Reason)
		}
	})

	t.Run("schema mismatch", func(t *testing.T) {
		uid := "uid-ms-03"
		// Build an envelope with wrong apiVersion.
		env := dispatch.EnvelopeOut{
			APIVersion: "wrong.version/v1",
			Kind:       dispatch.KindTaskEnvelopeOut,
			TaskUID:    uid,
			ExitCode:   0,
			ChildCount: 0,
		}
		envBytes, _ := json.Marshal(env)
		sha := computeEnvelopeSHA256(envBytes)

		manifest := BundleManifest{
			Milestones: []BundleEntry{
				{
					Name:   "ms-03",
					FQName: MilestoneFQName("ms-03"),
					OldUID: uid,
					SHA256: sha,
				},
			},
			Phases: []BundleEntry{},
			Plans:  []BundleEntry{},
		}

		dir := makeBundleDir(t, manifest, map[string][]byte{uid: envBytes})

		result, err := ValidateBundle(dir)
		if err != nil {
			t.Fatalf("ValidateBundle: %v", err)
		}
		if result.Rows[0].Verdict != "re-plan" {
			t.Errorf("expected re-plan, got %q", result.Rows[0].Verdict)
		}
		if !containsAny(result.Rows[0].Reason, "schema", "apiVersion", "version") {
			t.Errorf("reason should mention schema: %q", result.Rows[0].Reason)
		}
	})

	t.Run("completeness failure", func(t *testing.T) {
		uid := "uid-ms-04"
		// Build an envelope where childCount != len(childCRDs) and neither is 0
		// (the non-legacy failure, D-16a stamps the legacy 0-shape).
		env := dispatch.EnvelopeOut{
			APIVersion: dispatch.APIVersionV1Alpha1,
			Kind:       dispatch.KindTaskEnvelopeOut,
			TaskUID:    uid,
			ExitCode:   0,
			ChildCRDs: []dispatch.ChildCRDSpec{
				{Kind: "Milestone", Name: "ms-a"},
				{Kind: "Milestone", Name: "ms-b"},
			},
			ChildCount: 5, // deliberately wrong, not the legacy 0
		}
		envBytes, _ := json.Marshal(env)
		sha := computeEnvelopeSHA256(envBytes)

		manifest := BundleManifest{
			Milestones: []BundleEntry{
				{
					Name:   "ms-04",
					FQName: MilestoneFQName("ms-04"),
					OldUID: uid,
					SHA256: sha,
				},
			},
			Phases: []BundleEntry{},
			Plans:  []BundleEntry{},
		}

		dir := makeBundleDir(t, manifest, map[string][]byte{uid: envBytes})

		result, err := ValidateBundle(dir)
		if err != nil {
			t.Fatalf("ValidateBundle: %v", err)
		}
		if result.Rows[0].Verdict != "re-plan" {
			t.Errorf("expected re-plan, got %q", result.Rows[0].Verdict)
		}
		if !containsAny(result.Rows[0].Reason, "completeness", "childCount") {
			t.Errorf("reason should mention completeness/childCount: %q", result.Rows[0].Reason)
		}
	})

	t.Run("missing envelope", func(t *testing.T) {
		// Seed entry references an OldUID that has no envelope in pvc-envelopes.tgz.
		manifest := BundleManifest{
			Milestones: []BundleEntry{
				{
					Name:   "ms-05",
					FQName: MilestoneFQName("ms-05"),
					OldUID: "uid-missing",
				},
			},
			Phases: []BundleEntry{},
			Plans:  []BundleEntry{},
		}

		// Write bundle with no envelope for "uid-missing".
		dir := makeBundleDir(t, manifest, map[string][]byte{})

		result, err := ValidateBundle(dir)
		if err != nil {
			t.Fatalf("ValidateBundle: %v", err)
		}
		if result.Rows[0].Verdict != "re-plan" {
			t.Errorf("expected re-plan for missing envelope, got %q", result.Rows[0].Verdict)
		}
	})
}

// TestDryRunCycle tests that a seed graph with a back-edge cycle is hard-rejected.
func TestDryRunCycle(t *testing.T) {
	uid1 := "uid-cycle-a"
	uid2 := "uid-cycle-b"
	fq1 := MilestoneFQName("cycle-a")
	fq2 := MilestoneFQName("cycle-b")

	env1 := makeValidEnvelope(uid1, 0)
	env2 := makeValidEnvelope(uid2, 0)
	sha1 := computeEnvelopeSHA256(env1)
	sha2 := computeEnvelopeSHA256(env2)

	// cycle-a dependsOn cycle-b AND cycle-b dependsOn cycle-a.
	manifest := BundleManifest{
		Milestones: []BundleEntry{
			{
				Name:      "cycle-a",
				FQName:    fq1,
				OldUID:    uid1,
				SHA256:    sha1,
				DependsOn: []string{fq2},
			},
			{
				Name:      "cycle-b",
				FQName:    fq2,
				OldUID:    uid2,
				SHA256:    sha2,
				DependsOn: []string{fq1},
			},
		},
		Phases: []BundleEntry{},
		Plans:  []BundleEntry{},
	}

	dir := makeBundleDir(t, manifest, map[string][]byte{
		uid1: env1,
		uid2: env2,
	})

	result, err := ValidateBundle(dir)
	if err != nil {
		t.Fatalf("ValidateBundle returned error: %v", err)
	}

	if !result.CycleRejected {
		t.Fatal("expected cycle rejection")
	}
	if result.CycleError == nil {
		t.Fatal("expected non-nil CycleError")
	}

	var cycleErr *dag.CycleError
	if !errors.As(result.CycleError, &cycleErr) {
		t.Fatalf("expected *dag.CycleError, got %T", result.CycleError)
	}

	// Both FQNames must appear in InvolvedNodes.
	found := map[string]bool{}
	for _, n := range cycleErr.InvolvedNodes {
		found[n] = true
	}
	if !found[fq1] {
		t.Errorf("InvolvedNodes missing %q: %v", fq1, cycleErr.InvolvedNodes)
	}
	if !found[fq2] {
		t.Errorf("InvolvedNodes missing %q: %v", fq2, cycleErr.InvolvedNodes)
	}
}

// containsAny returns true if s contains any of the substrings.
func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if len(s) >= len(sub) {
			for i := 0; i <= len(s)-len(sub); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
		}
	}
	return false
}
