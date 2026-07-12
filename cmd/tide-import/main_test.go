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

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/runtime"

	pkgdispatch "github.com/jsquirrelz/tide/pkg/dispatch"
)

// writeEnvelopeOut writes an EnvelopeOut JSON file at
// <workspace>/envelopes/<uid>/out.json, creating intermediate directories.
func writeEnvelopeOut(t *testing.T, workspace, uid string, env pkgdispatch.EnvelopeOut) {
	t.Helper()
	dir := filepath.Join(workspace, "envelopes", uid)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll %q: %v", dir, err)
	}
	data, err := json.Marshal(env)
	if err != nil {
		t.Fatalf("Marshal EnvelopeOut: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "out.json"), data, 0o644); err != nil {
		t.Fatalf("WriteFile out.json: %v", err)
	}
}

// writeInJSON writes an EnvelopeIn-style placeholder at
// <workspace>/envelopes/<uid>/in.json.
func writeInJSON(t *testing.T, workspace, uid string) {
	t.Helper()
	dir := filepath.Join(workspace, "envelopes", uid)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll %q: %v", dir, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "in.json"), []byte(`{"apiVersion":"dispatch.tideproject.k8s/v1alpha1","kind":"TaskEnvelopeIn"}`), 0o644); err != nil {
		t.Fatalf("WriteFile in.json: %v", err)
	}
}

// writeChildJSON writes a child CRD JSON file at
// <workspace>/envelopes/<uid>/children/<name>.json.
func writeChildJSON(t *testing.T, workspace, uid, name string, data []byte) {
	t.Helper()
	dir := filepath.Join(workspace, "envelopes", uid, "children")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll children dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
		t.Fatalf("WriteFile %q: %v", name, err)
	}
}

// makeRekeyTable builds a JSON rekey table (stdin input for run).
func makeRekeyTable(t *testing.T, entries []rekeyEntry) []byte {
	t.Helper()
	data, err := json.Marshal(entries)
	if err != nil {
		t.Fatalf("Marshal rekey table: %v", err)
	}
	return data
}

// planSpecRaw returns a JSON RawMessage for a PlanSpec with phaseRef.
func planSpecRaw(t *testing.T, phaseRef string) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(map[string]any{
		"phaseRef": phaseRef,
	})
	if err != nil {
		t.Fatalf("Marshal planSpec: %v", err)
	}
	return json.RawMessage(raw)
}

// planSpecRawWithExtras returns a JSON RawMessage for a PlanSpec with extra fields
// (objective, wave, filesTouched) that should be stripped by schema conversion.
func planSpecRawWithExtras(t *testing.T, phaseRef string) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(map[string]any{
		"phaseRef":     phaseRef,
		"objective":    "some planning objective",
		"wave":         2,
		"filesTouched": []string{"cmd/foo/main.go"},
	})
	if err != nil {
		t.Fatalf("Marshal planSpec with extras: %v", err)
	}
	return json.RawMessage(raw)
}

// TestRunHappyCopy verifies (a): a full old-UID tree is copied to the new-UID
// path, out.json.TaskUID is rewritten to newUID, and the report JSON is emitted.
func TestRunHappyCopy(t *testing.T) {
	oldWS := t.TempDir()
	newWS := t.TempDir()
	oldUID := "uid-old-happy"
	newUID := "uid-new-happy"

	env := pkgdispatch.EnvelopeOut{
		APIVersion: pkgdispatch.APIVersionV1Alpha1,
		Kind:       pkgdispatch.KindTaskEnvelopeOut,
		TaskUID:    oldUID,
		ExitCode:   0,
		ChildCount: 1,
		ChildCRDs: []pkgdispatch.ChildCRDSpec{
			{Kind: "Plan", Name: "plan-01", Spec: runtime.RawExtension{Raw: planSpecRaw(t, "phase-01")}},
		},
	}
	writeEnvelopeOut(t, oldWS, oldUID, env)
	writeInJSON(t, oldWS, oldUID)
	writeChildJSON(t, oldWS, oldUID, "plan-01.json", []byte(`{"kind":"Plan","name":"plan-01","spec":{"phaseRef":"phase-01"}}`))

	table := makeRekeyTable(t, []rekeyEntry{{FQName: "milestone-01/plan-01", OldUID: oldUID, NewUID: newUID}})

	cfg := importConfig{OldWorkspace: oldWS, NewWorkspace: newWS}
	var stdout, stderr bytes.Buffer
	code := run(context.Background(), cfg, bytes.NewReader(table), &stdout, &stderr)
	if code != exitSuccess {
		t.Fatalf("run exit=%d, stderr=%q stdout=%q", code, stderr.String(), stdout.String())
	}

	// Verify new out.json has newUID.
	outPath := filepath.Join(newWS, "envelopes", newUID, "out.json")
	outData, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read new out.json: %v", err)
	}
	var got pkgdispatch.EnvelopeOut
	if err := json.Unmarshal(outData, &got); err != nil {
		t.Fatalf("unmarshal new out.json: %v", err)
	}
	if got.TaskUID != newUID {
		t.Errorf("out.json.taskUID = %q, want %q", got.TaskUID, newUID)
	}

	// Verify in.json was copied.
	if _, err := os.Stat(filepath.Join(newWS, "envelopes", newUID, "in.json")); err != nil {
		t.Errorf("in.json not copied to new-UID path: %v", err)
	}

	// Verify child was copied.
	if _, err := os.Stat(filepath.Join(newWS, "envelopes", newUID, "children", "plan-01.json")); err != nil {
		t.Errorf("child plan-01.json not copied: %v", err)
	}

	// Verify report JSON on stdout contains copied > 0.
	var report importReport
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("unmarshal stdout report: %v", err)
	}
	if report.Copied == 0 {
		t.Errorf("report.copied = 0, want > 0")
	}
}

// TestRunEnvelopesBaseSetgid (GAP-7) verifies the destination envelopes base dir
// is created with the setgid bit and group-write (2775), unconditionally — even
// when the rekey table is empty. The tide-import Job runs as uid 65532 and owns
// /workspace/envelopes; if it left the dir at a plain 0755 the uid-1000 init Job
// could not chmod it (EPERM, non-owner → Project InitFailed) and uid-1000
// planner/executor pods could not create their own envelope subdirs after a
// resume-from-import.
func TestRunEnvelopesBaseSetgid(t *testing.T) {
	oldWS := t.TempDir()
	newWS := t.TempDir()

	// Empty rekey table: the base-dir setup must still run.
	table := makeRekeyTable(t, []rekeyEntry{})

	cfg := importConfig{OldWorkspace: oldWS, NewWorkspace: newWS}
	var stdout, stderr bytes.Buffer
	code := run(context.Background(), cfg, bytes.NewReader(table), &stdout, &stderr)
	if code != exitSuccess {
		t.Fatalf("run exit=%d, stderr=%q", code, stderr.String())
	}

	info, err := os.Stat(filepath.Join(newWS, "envelopes"))
	if err != nil {
		t.Fatalf("stat envelopes base dir: %v", err)
	}
	if info.Mode()&os.ModeSetgid == 0 {
		t.Errorf("envelopes base dir mode = %v, want setgid bit set (2775)", info.Mode())
	}
	if perm := info.Mode().Perm(); perm != 0o775 {
		t.Errorf("envelopes base dir perm = %o, want 0775 (group-writable)", perm)
	}
}

// TestNoClobber verifies (b): a pre-existing dst file is not overwritten and
// the skipped count increments.
func TestNoClobber(t *testing.T) {
	oldWS := t.TempDir()
	newWS := t.TempDir()
	oldUID := "uid-old-noclobber"
	newUID := "uid-new-noclobber"

	env := pkgdispatch.EnvelopeOut{
		APIVersion: pkgdispatch.APIVersionV1Alpha1,
		Kind:       pkgdispatch.KindTaskEnvelopeOut,
		TaskUID:    oldUID,
		ExitCode:   0,
		ChildCount: 0,
	}
	writeEnvelopeOut(t, oldWS, oldUID, env)

	// Pre-write out.json at the dst with different content.
	dstDir := filepath.Join(newWS, "envelopes", newUID)
	if err := os.MkdirAll(dstDir, 0o755); err != nil {
		t.Fatalf("MkdirAll dst dir: %v", err)
	}
	preExistingContent := []byte(`{"apiVersion":"dispatch.tideproject.k8s/v1alpha1","kind":"TaskEnvelopeOut","taskUID":"pre-existing"}`)
	if err := os.WriteFile(filepath.Join(dstDir, "out.json"), preExistingContent, 0o644); err != nil {
		t.Fatalf("WriteFile pre-existing: %v", err)
	}

	table := makeRekeyTable(t, []rekeyEntry{{FQName: "ms/phase/plan", OldUID: oldUID, NewUID: newUID}})
	cfg := importConfig{OldWorkspace: oldWS, NewWorkspace: newWS}
	var stdout, stderr bytes.Buffer
	code := run(context.Background(), cfg, bytes.NewReader(table), &stdout, &stderr)
	if code != exitSuccess {
		t.Fatalf("run exit=%d, stderr=%q", code, stderr.String())
	}

	// Pre-existing out.json should not have been overwritten.
	data, err := os.ReadFile(filepath.Join(dstDir, "out.json"))
	if err != nil {
		t.Fatalf("read dst out.json: %v", err)
	}
	if !bytes.Equal(data, preExistingContent) {
		t.Errorf("out.json was overwritten (no-clobber violated): got %q, want %q", string(data), string(preExistingContent))
	}

	// Report should show skipped > 0.
	var report importReport
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("unmarshal stdout report: %v", err)
	}
	if report.Skipped == 0 {
		t.Errorf("report.skipped = 0, want > 0 (file was pre-existing)")
	}
}

// TestAtomicTaskUIDRewrite verifies (c): out.json with a stale TaskUID is
// rewritten to newUID; no .tmp file is left behind.
func TestAtomicTaskUIDRewrite(t *testing.T) {
	oldWS := t.TempDir()
	newWS := t.TempDir()
	oldUID := "uid-old-atomic"
	newUID := "uid-new-atomic"

	env := pkgdispatch.EnvelopeOut{
		APIVersion: pkgdispatch.APIVersionV1Alpha1,
		Kind:       pkgdispatch.KindTaskEnvelopeOut,
		TaskUID:    oldUID,
		ExitCode:   0,
		ChildCount: 0,
	}
	writeEnvelopeOut(t, oldWS, oldUID, env)

	table := makeRekeyTable(t, []rekeyEntry{{FQName: "milestone/plan-atomic", OldUID: oldUID, NewUID: newUID}})
	cfg := importConfig{OldWorkspace: oldWS, NewWorkspace: newWS}
	var stdout, stderr bytes.Buffer
	code := run(context.Background(), cfg, bytes.NewReader(table), &stdout, &stderr)
	if code != exitSuccess {
		t.Fatalf("run exit=%d, stderr=%q stdout=%q", code, stderr.String(), stdout.String())
	}

	outPath := filepath.Join(newWS, "envelopes", newUID, "out.json")
	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read new out.json: %v", err)
	}
	var got pkgdispatch.EnvelopeOut
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.TaskUID != newUID {
		t.Errorf("TaskUID = %q, want %q", got.TaskUID, newUID)
	}

	// No .tmp file should remain.
	tmpPath := outPath + ".tmp"
	if _, err := os.Stat(tmpPath); err == nil {
		t.Errorf("leftover .tmp file found at %q", tmpPath)
	}
}

// TestPathTraversal verifies (d): a rekey entry whose oldUID contains "../"
// causes run to return 2 and creates nothing in new-workspace.
func TestPathTraversal(t *testing.T) {
	oldWS := t.TempDir()
	newWS := t.TempDir()

	// Attempt path traversal via oldUID.
	table := makeRekeyTable(t, []rekeyEntry{
		{FQName: "milestone/bad", OldUID: "../etc", NewUID: "uid-new-trav"},
	})
	cfg := importConfig{OldWorkspace: oldWS, NewWorkspace: newWS}
	var stdout, stderr bytes.Buffer
	code := run(context.Background(), cfg, bytes.NewReader(table), &stdout, &stderr)
	if code != exitInvariant {
		t.Errorf("path traversal: exit=%d, want %d (exitInvariant); stderr=%q", code, exitInvariant, stderr.String())
	}

	// The only thing in new-workspace must be the empty envelopes base dir that
	// run() always creates up front (GAP-7 setgid setup); the traversal entry is
	// rejected before any copy, so nothing escaped and no rekeyed subdir exists.
	entries, err := os.ReadDir(newWS)
	if err != nil {
		t.Fatalf("ReadDir newWS: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != "envelopes" {
		names := make([]string, len(entries))
		for i, e := range entries {
			names[i] = e.Name()
		}
		t.Fatalf("new-workspace entries = %v, want only the empty [envelopes] base dir (no traversal escape)", names)
	}
	envEntries, err := os.ReadDir(filepath.Join(newWS, "envelopes"))
	if err != nil {
		t.Fatalf("ReadDir newWS/envelopes: %v", err)
	}
	if len(envEntries) != 0 {
		t.Errorf("envelopes dir has %d entries after traversal rejection, want 0 (entry was not rejected before copy)", len(envEntries))
	}
}

// TestPathTraversalNewUID verifies path traversal detection via newUID.
func TestPathTraversalNewUID(t *testing.T) {
	oldWS := t.TempDir()
	newWS := t.TempDir()

	// Attempt path traversal via newUID.
	table := makeRekeyTable(t, []rekeyEntry{
		{FQName: "milestone/bad", OldUID: "uid-old-ok", NewUID: "../etc/passwd"},
	})
	cfg := importConfig{OldWorkspace: oldWS, NewWorkspace: newWS}
	var stdout, stderr bytes.Buffer
	code := run(context.Background(), cfg, bytes.NewReader(table), &stdout, &stderr)
	if code != exitInvariant {
		t.Errorf("path traversal newUID: exit=%d, want %d", code, exitInvariant)
	}
}

// TestFQNameNoAliasing verifies (e): two entries reusing the same short name
// but under different parent chains produce distinct new-UID directories.
func TestFQNameNoAliasing(t *testing.T) {
	oldWS := t.TempDir()
	newWS := t.TempDir()
	oldUID1 := "uid-old-alias1"
	newUID1 := "uid-new-alias1"
	oldUID2 := "uid-old-alias2"
	newUID2 := "uid-new-alias2"

	// Both use the short name "plan-01" but under different milestone parents.
	env1 := pkgdispatch.EnvelopeOut{
		APIVersion: pkgdispatch.APIVersionV1Alpha1,
		Kind:       pkgdispatch.KindTaskEnvelopeOut,
		TaskUID:    oldUID1,
		ExitCode:   0,
		ChildCount: 0,
	}
	env2 := pkgdispatch.EnvelopeOut{
		APIVersion: pkgdispatch.APIVersionV1Alpha1,
		Kind:       pkgdispatch.KindTaskEnvelopeOut,
		TaskUID:    oldUID2,
		ExitCode:   0,
		ChildCount: 0,
	}
	writeEnvelopeOut(t, oldWS, oldUID1, env1)
	writeEnvelopeOut(t, oldWS, oldUID2, env2)

	table := makeRekeyTable(t, []rekeyEntry{
		{FQName: "milestone-01/plan-01", OldUID: oldUID1, NewUID: newUID1},
		{FQName: "milestone-02/plan-01", OldUID: oldUID2, NewUID: newUID2},
	})
	cfg := importConfig{OldWorkspace: oldWS, NewWorkspace: newWS}
	var stdout, stderr bytes.Buffer
	code := run(context.Background(), cfg, bytes.NewReader(table), &stdout, &stderr)
	if code != exitSuccess {
		t.Fatalf("run exit=%d, stderr=%q", code, stderr.String())
	}

	// Verify two distinct new-UID directories were created.
	for _, newUID := range []string{newUID1, newUID2} {
		outPath := filepath.Join(newWS, "envelopes", newUID, "out.json")
		if _, err := os.Stat(outPath); err != nil {
			t.Errorf("new-UID dir %q not created: %v", newUID, err)
		}
	}
}

// TestConversionNoOp verifies (f): a Plan child with extra objective/wave/filesTouched
// round-trips to clean v1alpha3 PlanSpec (only phaseRef/dependsOn survive).
func TestConversionNoOp(t *testing.T) {
	oldWS := t.TempDir()
	newWS := t.TempDir()
	oldUID := "uid-old-conv"
	newUID := "uid-new-conv"

	rawWithExtras := planSpecRawWithExtras(t, "phase-02")
	env := pkgdispatch.EnvelopeOut{
		APIVersion: pkgdispatch.APIVersionV1Alpha1,
		Kind:       pkgdispatch.KindTaskEnvelopeOut,
		TaskUID:    oldUID,
		ExitCode:   0,
		ChildCount: 1,
		ChildCRDs: []pkgdispatch.ChildCRDSpec{
			{Kind: "Plan", Name: "plan-conv", Spec: runtime.RawExtension{Raw: rawWithExtras}},
		},
	}
	writeEnvelopeOut(t, oldWS, oldUID, env)

	table := makeRekeyTable(t, []rekeyEntry{{FQName: "milestone-01/phase-02/plan-conv", OldUID: oldUID, NewUID: newUID}})
	cfg := importConfig{OldWorkspace: oldWS, NewWorkspace: newWS}
	var stdout, stderr bytes.Buffer
	code := run(context.Background(), cfg, bytes.NewReader(table), &stdout, &stderr)
	if code != exitSuccess {
		t.Fatalf("run exit=%d, stderr=%q stdout=%q", code, stderr.String(), stdout.String())
	}

	outPath := filepath.Join(newWS, "envelopes", newUID, "out.json")
	outData, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read converted out.json: %v", err)
	}
	var got pkgdispatch.EnvelopeOut
	if err := json.Unmarshal(outData, &got); err != nil {
		t.Fatalf("unmarshal converted out.json: %v", err)
	}
	if len(got.ChildCRDs) != 1 {
		t.Fatalf("expected 1 child CRD, got %d", len(got.ChildCRDs))
	}
	// The converted spec should not contain objective/wave/filesTouched.
	childSpec := got.ChildCRDs[0].Spec.Raw
	if strings.Contains(string(childSpec), "objective") {
		t.Errorf("converted Plan spec still contains 'objective': %s", string(childSpec))
	}
	if strings.Contains(string(childSpec), `"wave"`) {
		t.Errorf("converted Plan spec still contains 'wave': %s", string(childSpec))
	}
	if strings.Contains(string(childSpec), "filesTouched") {
		t.Errorf("converted Plan spec still contains 'filesTouched': %s", string(childSpec))
	}
	// phaseRef must survive.
	if !strings.Contains(string(childSpec), "phase-02") {
		t.Errorf("converted Plan spec lost phaseRef value 'phase-02': %s", string(childSpec))
	}
}

// TestKindAllowlistReject verifies (g): a child with Kind "Secret" or "Wave"
// causes run to return 2.
func TestKindAllowlistReject(t *testing.T) {
	cases := []struct {
		name string
		kind string
	}{
		{"Secret", "Secret"},
		{"Wave", "Wave"},
		{"Pod", "Pod"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			oldWS := t.TempDir()
			newWS := t.TempDir()
			oldUID := "uid-old-kind-" + tc.kind
			newUID := "uid-new-kind-" + tc.kind

			env := pkgdispatch.EnvelopeOut{
				APIVersion: pkgdispatch.APIVersionV1Alpha1,
				Kind:       pkgdispatch.KindTaskEnvelopeOut,
				TaskUID:    oldUID,
				ExitCode:   0,
				ChildCount: 1,
				ChildCRDs: []pkgdispatch.ChildCRDSpec{
					{Kind: tc.kind, Name: "evil", Spec: runtime.RawExtension{Raw: json.RawMessage(`{}`)}},
				},
			}
			writeEnvelopeOut(t, oldWS, oldUID, env)

			table := makeRekeyTable(t, []rekeyEntry{{FQName: "ms/evil", OldUID: oldUID, NewUID: newUID}})
			cfg := importConfig{OldWorkspace: oldWS, NewWorkspace: newWS}
			var stdout, stderr bytes.Buffer
			code := run(context.Background(), cfg, bytes.NewReader(table), &stdout, &stderr)
			if code != exitInvariant {
				t.Errorf("Kind=%q: exit=%d, want %d; stderr=%q", tc.kind, code, exitInvariant, stderr.String())
			}
		})
	}
}

// TestCompletenessRejectExitCode verifies (h): out.json with exitCode=1 is
// marked incomplete and NOT copied to the new-UID path.
func TestCompletenessRejectExitCode(t *testing.T) {
	oldWS := t.TempDir()
	newWS := t.TempDir()
	oldUID := "uid-old-exit1"
	newUID := "uid-new-exit1"

	env := pkgdispatch.EnvelopeOut{
		APIVersion: pkgdispatch.APIVersionV1Alpha1,
		Kind:       pkgdispatch.KindTaskEnvelopeOut,
		TaskUID:    oldUID,
		ExitCode:   1, // non-zero: envelope is incomplete
		ChildCount: 0,
	}
	writeEnvelopeOut(t, oldWS, oldUID, env)

	table := makeRekeyTable(t, []rekeyEntry{{FQName: "ms/plan-fail", OldUID: oldUID, NewUID: newUID}})
	cfg := importConfig{OldWorkspace: oldWS, NewWorkspace: newWS}
	var stdout, stderr bytes.Buffer
	code := run(context.Background(), cfg, bytes.NewReader(table), &stdout, &stderr)
	// Exit 0 is OK — incompleteness is not a fatal error (it's an expected condition
	// for plan-level envelopes that budget-halted); the envelope is simply marked incomplete.
	if code != exitSuccess {
		t.Fatalf("run exit=%d, stderr=%q", code, stderr.String())
	}

	// The new-UID directory should NOT have been created (incomplete envelope skipped).
	newEnvDir := filepath.Join(newWS, "envelopes", newUID)
	if _, err := os.Stat(newEnvDir); err == nil {
		t.Errorf("new-UID envelope dir %q was created for an incomplete envelope (exitCode=1)", newEnvDir)
	}

	// Verify incomplete count in report.
	var report importReport
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("unmarshal report: %v", err)
	}
	if report.Incomplete == 0 {
		t.Errorf("report.incomplete = 0, want > 0 (envelope had exitCode=1)")
	}
}

// TestCompletenessRejectChildCountMismatch verifies (i): out.json with
// childCount=3 but only 2 ChildCRDs is marked incomplete, not copied.
func TestCompletenessRejectChildCountMismatch(t *testing.T) {
	oldWS := t.TempDir()
	newWS := t.TempDir()
	oldUID := "uid-old-mismatch"
	newUID := "uid-new-mismatch"

	env := pkgdispatch.EnvelopeOut{
		APIVersion: pkgdispatch.APIVersionV1Alpha1,
		Kind:       pkgdispatch.KindTaskEnvelopeOut,
		TaskUID:    oldUID,
		ExitCode:   0,
		ChildCount: 3, // declared 3
		ChildCRDs: []pkgdispatch.ChildCRDSpec{
			{Kind: "Plan", Name: "plan-a", Spec: runtime.RawExtension{Raw: planSpecRaw(t, "phase-01")}},
			{Kind: "Plan", Name: "plan-b", Spec: runtime.RawExtension{Raw: planSpecRaw(t, "phase-01")}},
			// only 2 present, declared 3 → mismatch
		},
	}
	writeEnvelopeOut(t, oldWS, oldUID, env)

	table := makeRekeyTable(t, []rekeyEntry{{FQName: "ms/plan-mismatch", OldUID: oldUID, NewUID: newUID}})
	cfg := importConfig{OldWorkspace: oldWS, NewWorkspace: newWS}
	var stdout, stderr bytes.Buffer
	code := run(context.Background(), cfg, bytes.NewReader(table), &stdout, &stderr)
	if code != exitSuccess {
		t.Fatalf("run exit=%d, stderr=%q", code, stderr.String())
	}

	// The new-UID directory should NOT have been created.
	newEnvDir := filepath.Join(newWS, "envelopes", newUID)
	if _, err := os.Stat(newEnvDir); err == nil {
		t.Errorf("new-UID envelope dir %q was created for a mismatch envelope", newEnvDir)
	}

	var report importReport
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("unmarshal report: %v", err)
	}
	if report.Incomplete == 0 {
		t.Errorf("report.incomplete = 0, want > 0 (ChildCount mismatch)")
	}
}

// controllerRekeyRow mirrors internal/controller.rekeyRow byte-for-byte (same
// JSON tags). The controller emits a JSON ARRAY of these into the rekey
// ConfigMap; the binary decodes into []rekeyEntry. This local copy lets the
// test prove the two shapes agree without crossing the package boundary.
type controllerRekeyRow struct {
	FQName string `json:"fqName"`
	OldUID string `json:"oldUID"`
	NewUID string `json:"newUID"`
}

// TestRekeyTableRoundTrip proves CR-02 cannot regress: the JSON ARRAY shape the
// controller marshals decodes cleanly into the binary's []rekeyEntry, with all
// three fields preserved. The pre-fix controller emitted a map[string]…, which
// would fail to unmarshal into a slice with
// "cannot unmarshal object into Go value of type []rekeyEntry".
func TestRekeyTableRoundTrip(t *testing.T) {
	controllerSide := []controllerRekeyRow{
		{FQName: "ms-02", OldUID: "old-ms", NewUID: "new-ms"},
		{FQName: "ms-02/ph-03", OldUID: "old-ph", NewUID: "new-ph"},
		{FQName: "ms-02/ph-03/plan-01", OldUID: "old-pl", NewUID: "new-pl"},
	}
	wire, err := json.Marshal(controllerSide)
	if err != nil {
		t.Fatalf("marshal controller-side rekey array: %v", err)
	}

	var binarySide []rekeyEntry
	if err := json.Unmarshal(wire, &binarySide); err != nil {
		t.Fatalf("binary failed to decode controller rekey array: %v", err)
	}
	if len(binarySide) != len(controllerSide) {
		t.Fatalf("decoded %d rows, want %d", len(binarySide), len(controllerSide))
	}
	for i, row := range binarySide {
		if row.FQName != controllerSide[i].FQName ||
			row.OldUID != controllerSide[i].OldUID ||
			row.NewUID != controllerSide[i].NewUID {
			t.Errorf("row %d = %+v, want %+v", i, row, controllerSide[i])
		}
	}
}

// TestRunReadsRekeyFile proves the CR-01 flag path: when cfg.RekeyFile is set,
// run() reads the table from that file (not stdin), mirroring the production Job
// which passes --rekey-file=/rekey/rekey.json because the distroless base has no
// shell to pipe stdin.
func TestRunReadsRekeyFile(t *testing.T) {
	oldWS := t.TempDir()
	newWS := t.TempDir()
	oldUID := "uid-old-rekeyfile"
	newUID := "uid-new-rekeyfile"

	env := pkgdispatch.EnvelopeOut{
		APIVersion: pkgdispatch.APIVersionV1Alpha1,
		Kind:       pkgdispatch.KindTaskEnvelopeOut,
		TaskUID:    oldUID,
		ExitCode:   0,
		ChildCount: 0,
	}
	writeEnvelopeOut(t, oldWS, oldUID, env)

	table := makeRekeyTable(t, []rekeyEntry{{FQName: "ms/plan-rekeyfile", OldUID: oldUID, NewUID: newUID}})
	rekeyPath := filepath.Join(t.TempDir(), "rekey.json")
	if err := os.WriteFile(rekeyPath, table, 0o644); err != nil {
		t.Fatalf("write rekey file: %v", err)
	}

	cfg := importConfig{OldWorkspace: oldWS, NewWorkspace: newWS, RekeyFile: rekeyPath}
	// stdin is deliberately empty: if run() read stdin instead of the file it
	// would fail to decode and return exitInvariant.
	var stdout, stderr bytes.Buffer
	code := run(context.Background(), cfg, bytes.NewReader(nil), &stdout, &stderr)
	if code != exitSuccess {
		t.Fatalf("run with --rekey-file exit=%d, stderr=%q", code, stderr.String())
	}

	// The new-UID envelope dir (with the rewritten out.json) must now exist.
	newOut := filepath.Join(newWS, "envelopes", newUID, "out.json")
	if _, err := os.Stat(newOut); err != nil {
		t.Errorf("expected rewritten out.json at %q: %v", newOut, err)
	}
}
