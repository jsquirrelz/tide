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
	"context"
	"path/filepath"
	"testing"
	"time"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
)

// TestRemoteBranchTipFindsExistingBranch covers the present case: after
// cloning a seeded bare repo, RemoteBranchTip must return the branch's tip
// hash with found=true.
func TestRemoteBranchTipFindsExistingBranch(t *testing.T) {
	base := t.TempDir()
	bareSrc, head := seedBareRepo(t, base)

	destDir := filepath.Join(t.TempDir(), "clone.git")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	repo, err := Clone(ctx, "file://"+bareSrc, destDir, "")
	if err != nil {
		t.Fatalf("Clone: %v", err)
	}
	branch := defaultBranchOf(t, repo)

	hash, found, err := RemoteBranchTip(ctx, repo, branch, "")
	if err != nil {
		t.Fatalf("RemoteBranchTip: unexpected error: %v", err)
	}
	if !found {
		t.Fatal("RemoteBranchTip: found=false, want true")
	}
	if hash != head {
		t.Errorf("RemoteBranchTip hash = %s, want %s", hash, head)
	}
}

// TestRemoteBranchTipReturnsNotFoundForMissingBranch covers the absent
// case: a branch name the remote never had must return found=false with a
// nil error (not an error condition — first-push semantics rely on this
// distinction).
func TestRemoteBranchTipReturnsNotFoundForMissingBranch(t *testing.T) {
	base := t.TempDir()
	bareSrc, _ := seedBareRepo(t, base)

	destDir := filepath.Join(t.TempDir(), "clone.git")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	repo, err := Clone(ctx, "file://"+bareSrc, destDir, "")
	if err != nil {
		t.Fatalf("Clone: %v", err)
	}

	hash, found, err := RemoteBranchTip(ctx, repo, "tide/run-does-not-exist-1", "")
	if err != nil {
		t.Fatalf("RemoteBranchTip: unexpected error: %v", err)
	}
	if found {
		t.Error("RemoteBranchTip: found=true for a nonexistent branch")
	}
	if hash != plumbing.ZeroHash {
		t.Errorf("RemoteBranchTip hash = %s, want ZeroHash", hash)
	}
}

// TestRemoteBranchTipRejectsInvalidInput mirrors Push's own guard style:
// nil repo and empty branch are caller errors, not remote-state answers.
func TestRemoteBranchTipRejectsInvalidInput(t *testing.T) {
	base := t.TempDir()
	bareSrc, _ := seedBareRepo(t, base)
	destDir := filepath.Join(t.TempDir(), "clone.git")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	repo, err := Clone(ctx, "file://"+bareSrc, destDir, "")
	if err != nil {
		t.Fatalf("Clone: %v", err)
	}
	branch := defaultBranchOf(t, repo)

	tests := []struct {
		name   string
		repo   *gogit.Repository
		branch string
	}{
		{"nil repo", nil, branch},
		{"empty branch", repo, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, _, err := RemoteBranchTip(ctx, tt.repo, tt.branch, ""); err == nil {
				t.Errorf("RemoteBranchTip(%s): want error, got nil", tt.name)
			}
		})
	}
}
