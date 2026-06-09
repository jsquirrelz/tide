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

package git

import (
	"testing"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
)

// TestEnsureRunBranch_CreatesAtHead verifies the missing D-B6 step (Phase 10
// Option B): the run branch ref is created at the bare repo's default-branch
// tip when absent, so a subsequent executor worktree-add can check it out
// instead of failing with "couldn't find remote ref".
func TestEnsureRunBranch_CreatesAtHead(t *testing.T) {
	base := t.TempDir()
	bareDir, head := seedBareRepo(t, base)

	const runBranch = "tide/run-proj-123"
	if err := EnsureRunBranch(bareDir, runBranch); err != nil {
		t.Fatalf("EnsureRunBranch: %v", err)
	}

	repo, err := gogit.PlainOpen(bareDir)
	if err != nil {
		t.Fatalf("PlainOpen: %v", err)
	}
	ref, err := repo.Reference(plumbing.NewBranchReferenceName(runBranch), false)
	if err != nil {
		t.Fatalf("run branch ref not found after EnsureRunBranch: %v", err)
	}
	if ref.Hash() != head {
		t.Errorf("run branch points at %s; want default HEAD %s", ref.Hash(), head)
	}
}

// TestEnsureRunBranch_Idempotent verifies a second call is a no-op success and
// does not move the ref (a re-reconcile / Job retry must not reset run history).
func TestEnsureRunBranch_Idempotent(t *testing.T) {
	base := t.TempDir()
	bareDir, _ := seedBareRepo(t, base)

	const runBranch = "tide/run-proj-123"
	if err := EnsureRunBranch(bareDir, runBranch); err != nil {
		t.Fatalf("EnsureRunBranch (first): %v", err)
	}
	repo, _ := gogit.PlainOpen(bareDir)
	first, _ := repo.Reference(plumbing.NewBranchReferenceName(runBranch), false)

	if err := EnsureRunBranch(bareDir, runBranch); err != nil {
		t.Fatalf("EnsureRunBranch (second): %v", err)
	}
	second, _ := repo.Reference(plumbing.NewBranchReferenceName(runBranch), false)
	if first.Hash() != second.Hash() {
		t.Errorf("ref moved on idempotent re-call: %s -> %s", first.Hash(), second.Hash())
	}
}

// TestEnsureRunBranch_EmptyBranchRejected guards the programmer-error case.
func TestEnsureRunBranch_EmptyBranchRejected(t *testing.T) {
	base := t.TempDir()
	bareDir, _ := seedBareRepo(t, base)
	if err := EnsureRunBranch(bareDir, ""); err == nil {
		t.Fatal("expected error for empty branch, got nil")
	}
}
