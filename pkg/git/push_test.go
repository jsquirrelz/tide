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
	"strings"
	"testing"
	"time"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
)

// cloneWithOrigin clones bareSrc into a non-bare working copy and returns
// the repo + worktree. The clone keeps "origin" pointing at the bare repo
// via file:// URL (this is what go-git's PlainClone wires by default), so
// repo.PushContext with RemoteName="origin" pushes back into the bare.
func cloneWithOrigin(t *testing.T, bareSrc, cloneDir string) (*gogit.Repository, *gogit.Worktree) {
	t.Helper()
	repo, err := gogit.PlainClone(cloneDir, false, &gogit.CloneOptions{
		URL: "file://" + bareSrc,
	})
	if err != nil {
		t.Fatalf("PlainClone %s: %v", cloneDir, err)
	}
	wt, err := repo.Worktree()
	if err != nil {
		t.Fatalf("Worktree: %v", err)
	}
	return repo, wt
}

func mkSigner(name string) object.Signature {
	return object.Signature{
		Name:  name,
		Email: strings.ToLower(name) + "@example.com",
		When:  time.Now(),
	}
}

// addCommit writes a file in the working dir, stages it, and commits.
// Returns the new commit hash.
func addCommit(t *testing.T, repo *gogit.Repository, wt *gogit.Worktree, dir, filename, content, msg, authorName string) plumbing.Hash {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, filename), []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", filename, err)
	}
	if _, err := wt.Add(filename); err != nil {
		t.Fatalf("wt.Add %s: %v", filename, err)
	}
	h, err := wt.Commit(msg, &gogit.CommitOptions{Author: ptrSig(mkSigner(authorName))})
	if err != nil {
		t.Fatalf("wt.Commit %q: %v", msg, err)
	}
	return h
}

func ptrSig(s object.Signature) *object.Signature { return &s }

// TestPushFirstPushOmitsLease covers Test 2: a fresh clone pushes back to
// the bare origin with lastPushedSHA="" — no ForceWithLease, push must
// succeed, the bare repo's ref must advance.
func TestPushFirstPushOmitsLease(t *testing.T) {
	base := t.TempDir()
	bareSrc, originalHead := seedBareRepo(t, base)

	cloneDir := filepath.Join(t.TempDir(), "clone-a")
	repo, wt := cloneWithOrigin(t, bareSrc, cloneDir)

	bareRepo, err := gogit.PlainOpen(bareSrc)
	if err != nil {
		t.Fatalf("PlainOpen bareSrc: %v", err)
	}
	branch := defaultBranchOf(t, bareRepo)

	newHead := addCommit(t, repo, wt, cloneDir, "first.md", "first push\n", "first push commit", "A")
	if newHead == originalHead {
		t.Fatal("addCommit produced no new hash — fixture broken")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := Push(ctx, repo, branch, "" /* first push, no lease */, "ignored-pat-for-file"); err != nil {
		t.Fatalf("Push first: %v", err)
	}

	// Verify bareSrc's ref advanced to newHead.
	ref, err := bareRepo.Reference(plumbing.NewBranchReferenceName(branch), false)
	if err != nil {
		t.Fatalf("Reference bare branch: %v", err)
	}
	if ref.Hash() != newHead {
		t.Errorf("bare %s = %s; want %s (push didn't advance ref)", branch, ref.Hash(), newHead)
	}
}

// TestPushHonorsLeaseWhenRemoteUnchanged covers Test 3: after a first
// successful push, a subsequent push with the previous head as
// lastPushedSHA also succeeds (lease holds because no external party
// changed the remote ref).
func TestPushHonorsLeaseWhenRemoteUnchanged(t *testing.T) {
	base := t.TempDir()
	bareSrc, _ := seedBareRepo(t, base)

	cloneDir := filepath.Join(t.TempDir(), "clone-b")
	repo, wt := cloneWithOrigin(t, bareSrc, cloneDir)

	bareRepo, err := gogit.PlainOpen(bareSrc)
	if err != nil {
		t.Fatalf("PlainOpen bareSrc: %v", err)
	}
	branch := defaultBranchOf(t, bareRepo)

	firstNew := addCommit(t, repo, wt, cloneDir, "one.md", "one\n", "first", "A")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := Push(ctx, repo, branch, "", "pat"); err != nil {
		t.Fatalf("Push 1: %v", err)
	}

	secondNew := addCommit(t, repo, wt, cloneDir, "two.md", "two\n", "second", "A")
	if secondNew == firstNew {
		t.Fatal("second commit equals first — fixture broken")
	}
	if err := Push(ctx, repo, branch, firstNew.String(), "pat"); err != nil {
		t.Fatalf("Push 2 (lease=firstNew): %v", err)
	}

	ref, err := bareRepo.Reference(plumbing.NewBranchReferenceName(branch), false)
	if err != nil {
		t.Fatalf("Reference bare branch: %v", err)
	}
	if ref.Hash() != secondNew {
		t.Errorf("after second push, bare %s = %s; want %s", branch, ref.Hash(), secondNew)
	}
}

// TestPushRejectsStaleLease covers Test 4: after our first push, a
// sidecar clone bypasses our Push() and force-pushes a divergent commit
// to the bare origin. Our subsequent Push() with the now-stale
// lastPushedSHA must surface an error (lease rejection).
func TestPushRejectsStaleLease(t *testing.T) {
	base := t.TempDir()
	bareSrc, _ := seedBareRepo(t, base)

	mineDir := filepath.Join(t.TempDir(), "clone-mine")
	mineRepo, mineWT := cloneWithOrigin(t, bareSrc, mineDir)

	bareRepo, err := gogit.PlainOpen(bareSrc)
	if err != nil {
		t.Fatalf("PlainOpen bareSrc: %v", err)
	}
	branch := defaultBranchOf(t, bareRepo)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// 1. First push from mine: succeeds, no lease.
	mineFirst := addCommit(t, mineRepo, mineWT, mineDir, "mine-1.md", "mine\n", "mine 1", "Mine")
	if err := Push(ctx, mineRepo, branch, "", "pat"); err != nil {
		t.Fatalf("Push (mine, first): %v", err)
	}

	// 2. Sidecar party clones, makes its own commit, force-pushes
	//    directly via go-git PushContext (bypassing our Push wrapper).
	//    This simulates an out-of-band push that invalidates mine's
	//    lease.
	otherDir := filepath.Join(t.TempDir(), "clone-other")
	otherRepo, otherWT := cloneWithOrigin(t, bareSrc, otherDir)
	otherHead := addCommit(t, otherRepo, otherWT, otherDir, "other.md", "other\n", "other-side commit", "Other")
	if err := otherRepo.PushContext(ctx, &gogit.PushOptions{
		RemoteName: "origin",
		RefSpecs: []config.RefSpec{
			config.RefSpec("refs/heads/" + branch + ":refs/heads/" + branch),
		},
		Force: true,
	}); err != nil {
		t.Fatalf("sidecar push: %v", err)
	}

	// Verify the bare ref moved to otherHead — sanity check the
	// fixture.
	bareRef, err := bareRepo.Reference(plumbing.NewBranchReferenceName(branch), false)
	if err != nil {
		t.Fatalf("Reference bare: %v", err)
	}
	if bareRef.Hash() != otherHead {
		t.Fatalf("bare did not move to otherHead: got %s, want %s",
			bareRef.Hash(), otherHead)
	}

	// 3. Now mine adds another commit and attempts Push with the
	//    stale-but-still-valid-as-far-as-mine-knows lastPushedSHA.
	//    The lease should be REJECTED because the remote moved off
	//    mineFirst.
	_ = addCommit(t, mineRepo, mineWT, mineDir, "mine-2.md", "mine 2\n", "mine 2", "Mine")
	err = Push(ctx, mineRepo, branch, mineFirst.String(), "pat")
	if err == nil {
		t.Fatal("Push with stale lease unexpectedly succeeded")
	}
	// Error message should reference the branch + lease so callers
	// (cmd/tide-push, plan 03-06) can surface a structured failure.
	// Avoid asserting on the internal go-git error message, which is
	// version-fragile; just confirm the wrapper added our context.
	if !strings.Contains(err.Error(), "git push "+branch) {
		t.Errorf("error %q does not include branch context", err.Error())
	}
	if !strings.Contains(err.Error(), "lease=") {
		t.Errorf("error %q does not include lease context", err.Error())
	}
}

func TestPushValidatesArgs(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := Push(ctx, nil, "main", "", ""); err == nil {
		t.Error("Push(nil repo) returned nil error")
	}

	base := t.TempDir()
	bareSrc, _ := seedBareRepo(t, base)
	cloneDir := filepath.Join(t.TempDir(), "clone-validate")
	repo, _ := cloneWithOrigin(t, bareSrc, cloneDir)
	if err := Push(ctx, repo, "", "", ""); err == nil {
		t.Error("Push(empty branch) returned nil error")
	}
}
