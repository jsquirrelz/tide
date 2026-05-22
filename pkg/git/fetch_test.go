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
	"os"
	"path/filepath"
	"testing"
	"time"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
)

// TestFetchAlreadyUpToDate covers the no-op path: clone from a server,
// fetch immediately, expect nil (NoErrAlreadyUpToDate must be swallowed).
func TestFetchAlreadyUpToDate(t *testing.T) {
	base := t.TempDir()
	bareSrc, _ := seedBareRepo(t, base)

	destDir := filepath.Join(t.TempDir(), "clone.git")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	repo, err := Clone(ctx, "file://"+bareSrc, destDir, "")
	if err != nil {
		t.Fatalf("Clone: %v", err)
	}

	if err := Fetch(ctx, repo, ""); err != nil {
		t.Errorf("Fetch (already up-to-date): unexpected error: %v", err)
	}
}

// TestFetchPullsNewCommits covers the active path: after cloning, the
// upstream gains a new commit; Fetch retrieves it and the new HEAD is
// visible via repo.References.
func TestFetchPullsNewCommits(t *testing.T) {
	base := t.TempDir()
	bareSrc, originalHead := seedBareRepo(t, base)

	destDir := filepath.Join(t.TempDir(), "clone.git")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	repo, err := Clone(ctx, "file://"+bareSrc, destDir, "")
	if err != nil {
		t.Fatalf("Clone: %v", err)
	}
	branch := defaultBranchOf(t, repo)

	// Push a new commit into the bare source via a sidecar working copy
	// PlainCloned from the bare repo. This simulates an "external" push.
	sidecarDir := filepath.Join(t.TempDir(), "sidecar")
	sidecar, err := gogit.PlainClone(sidecarDir, false, &gogit.CloneOptions{
		URL:           "file://" + bareSrc,
		ReferenceName: plumbing.NewBranchReferenceName(branch),
		SingleBranch:  true,
	})
	if err != nil {
		t.Fatalf("sidecar PlainClone: %v", err)
	}
	sidecarWT, err := sidecar.Worktree()
	if err != nil {
		t.Fatalf("sidecar Worktree: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sidecarDir, "newfile.txt"), []byte("hello\n"), 0o644); err != nil {
		t.Fatalf("write newfile: %v", err)
	}
	if _, err := sidecarWT.Add("newfile.txt"); err != nil {
		t.Fatalf("sidecar Add: %v", err)
	}
	newCommit, err := sidecarWT.Commit("external commit", &gogit.CommitOptions{
		Author: &object.Signature{
			Name:  "External",
			Email: "ext@example.com",
			When:  time.Now(),
		},
	})
	if err != nil {
		t.Fatalf("sidecar Commit: %v", err)
	}
	if err := sidecar.PushContext(ctx, &gogit.PushOptions{}); err != nil {
		t.Fatalf("sidecar Push: %v", err)
	}
	if newCommit == originalHead {
		t.Fatal("sidecar commit equals original head — fixture broken")
	}

	// Now Fetch in our cloned repo and verify the new commit is reachable
	// via the remote-tracking ref.
	if err := Fetch(ctx, repo, ""); err != nil {
		t.Fatalf("Fetch: %v", err)
	}

	remoteRef, err := repo.Reference(plumbing.NewRemoteReferenceName("origin", branch), false)
	if err != nil {
		t.Fatalf("Reference origin/%s: %v", branch, err)
	}
	if remoteRef.Hash() != newCommit {
		t.Errorf("after Fetch, origin/%s = %s; want %s", branch, remoteRef.Hash(), newCommit)
	}
}
