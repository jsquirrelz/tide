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
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestValidate_AcceptsWritesUnderDeclared verifies that files written inside
// a declared output path are not flagged as violations.
func TestValidate_AcceptsWritesUnderDeclared(t *testing.T) {
	root := t.TempDir()
	declaredDir := filepath.Join(root, "artifacts", "M-001", "P-001", "L-001")
	if err := os.MkdirAll(declaredDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	runStart := time.Now()
	time.Sleep(5 * time.Millisecond) // ensure mtime is after runStart

	outFile := filepath.Join(declaredDir, "result.txt")
	if err := os.WriteFile(outFile, []byte("ok"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	violations, err := Validate(root, runStart, []string{"artifacts/M-001/P-001/L-001"})
	if err != nil {
		t.Fatalf("Validate error: %v", err)
	}
	if len(violations) != 0 {
		t.Errorf("expected no violations, got: %v", violations)
	}
}

// TestValidate_RejectsWritesOutsideDeclared verifies that a file written
// outside the declared output path is returned in the violations list.
func TestValidate_RejectsWritesOutsideDeclared(t *testing.T) {
	root := t.TempDir()
	declaredDir := filepath.Join(root, "artifacts", "M-001", "P-001", "L-001")
	escapeDir := filepath.Join(root, "escape")
	if err := os.MkdirAll(declaredDir, 0o755); err != nil {
		t.Fatalf("mkdir declared: %v", err)
	}
	if err := os.MkdirAll(escapeDir, 0o755); err != nil {
		t.Fatalf("mkdir escape: %v", err)
	}

	runStart := time.Now()
	time.Sleep(5 * time.Millisecond)

	inScope := filepath.Join(declaredDir, "result.txt")
	outScope := filepath.Join(escapeDir, "leak.txt")
	if err := os.WriteFile(inScope, []byte("ok"), 0o644); err != nil {
		t.Fatalf("WriteFile in-scope: %v", err)
	}
	if err := os.WriteFile(outScope, []byte("leaked"), 0o644); err != nil {
		t.Fatalf("WriteFile out-scope: %v", err)
	}

	violations, err := Validate(root, runStart, []string{"artifacts/M-001/P-001/L-001"})
	if err != nil {
		t.Fatalf("Validate error: %v", err)
	}
	if len(violations) != 1 {
		t.Fatalf("expected 1 violation, got %d: %v", len(violations), violations)
	}
	// The resolved path of outScope should be in the violations list.
	resolvedOutScope, _ := filepath.EvalSymlinks(outScope)
	if violations[0] != resolvedOutScope {
		t.Errorf("violation path mismatch: got %q, want %q", violations[0], resolvedOutScope)
	}
}

// TestValidate_SkipsFilesModifiedBeforeRunStart verifies that pre-existing
// files (mtime before runStart) are not flagged.
func TestValidate_SkipsFilesModifiedBeforeRunStart(t *testing.T) {
	root := t.TempDir()
	declaredDir := filepath.Join(root, "artifacts", "M-001", "P-001", "L-001")
	otherDir := filepath.Join(root, "escape")
	if err := os.MkdirAll(declaredDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.MkdirAll(otherDir, 0o755); err != nil {
		t.Fatalf("mkdir other: %v", err)
	}

	// Write a pre-existing file in a non-declared dir BEFORE runStart.
	preExisting := filepath.Join(otherDir, "old.txt")
	if err := os.WriteFile(preExisting, []byte("old"), 0o644); err != nil {
		t.Fatalf("WriteFile pre-existing: %v", err)
	}
	// Force the mtime back to be clearly before runStart.
	oldTime := time.Now().Add(-10 * time.Second)
	if err := os.Chtimes(preExisting, oldTime, oldTime); err != nil {
		t.Fatalf("Chtimes: %v", err)
	}

	runStart := time.Now()
	time.Sleep(5 * time.Millisecond)

	// Write a new in-scope file AFTER runStart.
	inScope := filepath.Join(declaredDir, "new.txt")
	if err := os.WriteFile(inScope, []byte("new"), 0o644); err != nil {
		t.Fatalf("WriteFile in-scope: %v", err)
	}

	violations, err := Validate(root, runStart, []string{"artifacts/M-001/P-001/L-001"})
	if err != nil {
		t.Fatalf("Validate error: %v", err)
	}
	// The pre-existing file must NOT appear in violations (mtime before runStart).
	if len(violations) != 0 {
		t.Errorf("expected no violations, got: %v", violations)
	}
}

// TestValidate_AcceptsWritesWhenWorkspaceRootIsSymlink is the CR-05
// regression test: when the workspace root is under a symlink (e.g. macOS
// /tmp → /private/tmp, or any K8s tmpfs mount), declared paths and walk
// targets must resolve through the same prefix so files in scope are NOT
// flagged as violations.
//
// Before the fix, declared paths that didn't pre-exist were stored as the
// un-resolved abs form while walk targets were resolved via EvalSymlinks —
// the asymmetric Rel() comparison returned a "../..." path and the file
// was wrongly added to violations.
func TestValidate_AcceptsWritesWhenWorkspaceRootIsSymlink(t *testing.T) {
	// Build a workspace root that is a symlink. We allocate a real directory
	// elsewhere and point a symlink at it. The Validate call uses the symlink
	// path, which mirrors the production case where /tmp is a symlink to
	// /private/tmp on macOS and tmpfs-backed mounts in K8s pods.
	realRoot := t.TempDir()
	parent := t.TempDir()
	symlinkRoot := filepath.Join(parent, "ws")
	if err := os.Symlink(realRoot, symlinkRoot); err != nil {
		t.Skipf("Symlink creation not supported on this OS: %v", err)
	}

	// Declared path does NOT pre-exist — this is the path that historically
	// triggered the false-positive. The harness/runtime would mkdir + write
	// under it; here we simulate the same shape.
	declaredRel := "artifacts/M-001/P-001/L-001"
	declaredFull := filepath.Join(symlinkRoot, declaredRel)
	if err := os.MkdirAll(declaredFull, 0o755); err != nil {
		t.Fatalf("mkdir declared: %v", err)
	}

	runStart := time.Now()
	time.Sleep(5 * time.Millisecond)

	outFile := filepath.Join(declaredFull, "result.txt")
	if err := os.WriteFile(outFile, []byte("ok"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	violations, err := Validate(symlinkRoot, runStart, []string{declaredRel})
	if err != nil {
		t.Fatalf("Validate error: %v", err)
	}
	if len(violations) != 0 {
		t.Errorf("expected no violations when workspace root is a symlink, got: %v", violations)
	}
}

// TestValidate_RejectsSymlinkToOutOfScope is the load-bearing Pitfall-7 test.
// A symlink inside the declared scope that resolves to a target outside the
// declared scope must be flagged as a violation.
func TestValidate_RejectsSymlinkToOutOfScope(t *testing.T) {
	root := t.TempDir()
	declaredDir := filepath.Join(root, "artifacts", "M-001", "P-001", "L-001")
	escapeDir := filepath.Join(root, "escape")
	if err := os.MkdirAll(declaredDir, 0o755); err != nil {
		t.Fatalf("mkdir declared: %v", err)
	}
	if err := os.MkdirAll(escapeDir, 0o755); err != nil {
		t.Fatalf("mkdir escape: %v", err)
	}

	// Create the actual target file in the escape dir.
	targetFile := filepath.Join(escapeDir, "target.txt")
	if err := os.WriteFile(targetFile, []byte("secret"), 0o644); err != nil {
		t.Fatalf("WriteFile target: %v", err)
	}

	runStart := time.Now()
	time.Sleep(5 * time.Millisecond)

	// Create a symlink INSIDE the declared scope that points to the escape dir.
	symlinkPath := filepath.Join(declaredDir, "link.txt")
	if err := os.Symlink(targetFile, symlinkPath); err != nil {
		t.Fatalf("Symlink: %v", err)
	}

	// Chtimes on the symlink itself is tricky; ensure the target's mtime is
	// after runStart (it was written before runStart above, so adjust).
	now := time.Now()
	if err := os.Chtimes(targetFile, now, now); err != nil {
		t.Fatalf("Chtimes target: %v", err)
	}

	violations, err := Validate(root, runStart, []string{"artifacts/M-001/P-001/L-001"})
	if err != nil {
		t.Fatalf("Validate error: %v", err)
	}
	// The resolved target (in escape dir) must appear as a violation.
	if len(violations) == 0 {
		t.Fatal("expected symlink-to-out-of-scope violation, got none")
	}
	resolvedTarget, _ := filepath.EvalSymlinks(targetFile)
	found := false
	for _, v := range violations {
		if v == resolvedTarget {
			found = true
		}
	}
	if !found {
		t.Errorf("expected resolved target %q in violations, got: %v", resolvedTarget, violations)
	}
}
