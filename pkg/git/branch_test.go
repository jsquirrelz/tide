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
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
)

// TestEnsureRunBranch_CreatesAtHead verifies the missing D-B6 step (Phase 10
// Option B): the run branch ref is created at the bare repo's default-branch
// tip when absent, so a subsequent executor worktree-add can check it out
// instead of failing with "couldn't find remote ref". With no baseRef the
// behavior is unchanged (HEAD), now returning HEAD's hash.
func TestEnsureRunBranch_CreatesAtHead(t *testing.T) {
	base := t.TempDir()
	bareDir, head := seedBareRepo(t, base)

	const runBranch = "tide/run-proj-123"
	got, err := EnsureRunBranch(bareDir, runBranch, "")
	if err != nil {
		t.Fatalf("EnsureRunBranch: %v", err)
	}
	if got != head {
		t.Errorf("returned hash = %s; want default HEAD %s", got, head)
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
// does not move the ref (a re-reconcile / Job retry must not reset run history),
// and that the returned hash is the existing tip.
func TestEnsureRunBranch_Idempotent(t *testing.T) {
	base := t.TempDir()
	bareDir, _ := seedBareRepo(t, base)

	const runBranch = "tide/run-proj-123"
	first, err := EnsureRunBranch(bareDir, runBranch, "")
	if err != nil {
		t.Fatalf("EnsureRunBranch (first): %v", err)
	}

	second, err := EnsureRunBranch(bareDir, runBranch, "")
	if err != nil {
		t.Fatalf("EnsureRunBranch (second): %v", err)
	}
	if first != second {
		t.Errorf("ref moved on idempotent re-call: %s -> %s", first, second)
	}
}

// TestEnsureRunBranch_EmptyBranchRejected guards the programmer-error case.
func TestEnsureRunBranch_EmptyBranchRejected(t *testing.T) {
	base := t.TempDir()
	bareDir, _ := seedBareRepo(t, base)
	if _, err := EnsureRunBranch(bareDir, "", ""); err == nil {
		t.Fatal("expected error for empty branch, got nil")
	}
}

// richRepo carries a production-shaped bare clone (non-default branches live
// only under refs/remotes/origin/*, exactly as pkggit.Clone leaves them) plus
// the SHAs the resolution tests assert against.
type richRepo struct {
	bareDir      string
	defaultHead  plumbing.Hash // default-branch tip (also the annotated-tag target)
	featureHead  plumbing.Hash // tip of the non-default branch feature/hotfix
	lightweightC plumbing.Hash // commit the lightweight tag v1.0.0-lw points at
}

// seedResolvableClone builds a source repo with a non-default branch
// (feature/hotfix, tip != default tip), an annotated tag (v1.0.0 on the
// default tip) and a lightweight tag (v1.0.0-lw on the first commit), then
// clones it via pkggit.Clone. The clone is the load-bearing part: go-git's
// non-mirror bare clone stores non-default branches only under
// refs/remotes/origin/* (Pitfall 1), so resolving feature/hotfix exercises the
// remote-fallback arm — resolving against the seeded source directly would
// hide the bug.
func seedResolvableClone(t *testing.T) richRepo {
	t.Helper()
	base := t.TempDir()
	workDir := filepath.Join(base, "src-work")

	repo, err := gogit.PlainInit(workDir, false)
	if err != nil {
		t.Fatalf("PlainInit: %v", err)
	}
	wt, err := repo.Worktree()
	if err != nil {
		t.Fatalf("Worktree: %v", err)
	}

	mk := func(fname, content, msg string) plumbing.Hash {
		t.Helper()
		if err := os.WriteFile(filepath.Join(workDir, fname), []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", fname, err)
		}
		if _, err := wt.Add(fname); err != nil {
			t.Fatalf("Add %s: %v", fname, err)
		}
		h, err := wt.Commit(msg, &gogit.CommitOptions{
			Author: &object.Signature{Name: "Seed", Email: "seed@example.com", When: time.Now()},
		})
		if err != nil {
			t.Fatalf("Commit %s: %v", msg, err)
		}
		return h
	}

	c1 := mk("a.txt", "1\n", "commit 1")
	defaultHead := mk("a.txt", "1\n2\n", "commit 2")

	// Capture the default branch name (go-git default is "master") so we can
	// return the worktree to it before cloning — the source HEAD determines the
	// clone's default branch.
	headSym, err := repo.Reference(plumbing.HEAD, false)
	if err != nil {
		t.Fatalf("Reference HEAD: %v", err)
	}
	defaultBranch := headSym.Target()

	// Annotated tag on the default tip → peels to defaultHead (D-11).
	if _, err := repo.CreateTag("v1.0.0", defaultHead, &gogit.CreateTagOptions{
		Tagger:  &object.Signature{Name: "Seed", Email: "seed@example.com", When: time.Now()},
		Message: "release 1.0.0",
	}); err != nil {
		t.Fatalf("CreateTag annotated: %v", err)
	}
	// Lightweight tag on the first commit (distinct from the annotated target).
	if _, err := repo.CreateTag("v1.0.0-lw", c1, nil); err != nil {
		t.Fatalf("CreateTag lightweight: %v", err)
	}

	// Non-default branch off the default tip with its own distinct commit.
	if err := wt.Checkout(&gogit.CheckoutOptions{
		Branch: plumbing.NewBranchReferenceName("feature/hotfix"),
		Create: true,
	}); err != nil {
		t.Fatalf("Checkout feature/hotfix: %v", err)
	}
	featureHead := mk("b.txt", "feature\n", "feature commit")

	// Return to the default branch so the clone's HEAD is the default branch,
	// not feature/hotfix.
	if err := wt.Checkout(&gogit.CheckoutOptions{Branch: defaultBranch}); err != nil {
		t.Fatalf("Checkout back to default: %v", err)
	}

	bareDir := filepath.Join(base, "clone.git")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if _, err := Clone(ctx, "file://"+workDir, bareDir, ""); err != nil {
		t.Fatalf("pkggit.Clone: %v", err)
	}

	return richRepo{
		bareDir:      bareDir,
		defaultHead:  defaultHead,
		featureHead:  featureHead,
		lightweightC: c1,
	}
}

// runBranchTip opens the bare repo and returns the run branch's tip hash.
func runBranchTip(t *testing.T, bareDir, runBranch string) plumbing.Hash {
	t.Helper()
	repo, err := gogit.PlainOpen(bareDir)
	if err != nil {
		t.Fatalf("PlainOpen: %v", err)
	}
	ref, err := repo.Reference(plumbing.NewBranchReferenceName(runBranch), false)
	if err != nil {
		t.Fatalf("run branch %q ref not found: %v", runBranch, err)
	}
	return ref.Hash()
}

// TestEnsureRunBranch_NonDefaultBranch is the load-bearing Pitfall 1 case: a
// non-default branch name resolves via refs/remotes/origin/* in a
// production-shaped clone. Removing the remote-fallback lookup fails this test.
func TestEnsureRunBranch_NonDefaultBranch(t *testing.T) {
	rr := seedResolvableClone(t)
	const runBranch = "tide/run-feature"
	got, err := EnsureRunBranch(rr.bareDir, runBranch, "feature/hotfix")
	if err != nil {
		t.Fatalf("EnsureRunBranch feature/hotfix: %v", err)
	}
	if got != rr.featureHead {
		t.Errorf("returned hash = %s; want feature tip %s", got, rr.featureHead)
	}
	if tip := runBranchTip(t, rr.bareDir, runBranch); tip != rr.featureHead {
		t.Errorf("run branch tip = %s; want feature tip %s", tip, rr.featureHead)
	}
}

// TestEnsureRunBranch_AnnotatedTagPeeled verifies D-11: an annotated tag stamps
// the PEELED commit hash, not the tag object hash.
func TestEnsureRunBranch_AnnotatedTagPeeled(t *testing.T) {
	rr := seedResolvableClone(t)
	const runBranch = "tide/run-annotated"
	got, err := EnsureRunBranch(rr.bareDir, runBranch, "v1.0.0")
	if err != nil {
		t.Fatalf("EnsureRunBranch v1.0.0: %v", err)
	}
	if got != rr.defaultHead {
		t.Errorf("annotated tag resolved to %s; want peeled commit %s", got, rr.defaultHead)
	}
}

// TestEnsureRunBranch_LightweightTag verifies a lightweight tag resolves to its
// commit hash directly.
func TestEnsureRunBranch_LightweightTag(t *testing.T) {
	rr := seedResolvableClone(t)
	const runBranch = "tide/run-lw"
	got, err := EnsureRunBranch(rr.bareDir, runBranch, "v1.0.0-lw")
	if err != nil {
		t.Fatalf("EnsureRunBranch v1.0.0-lw: %v", err)
	}
	if got != rr.lightweightC {
		t.Errorf("lightweight tag resolved to %s; want %s", got, rr.lightweightC)
	}
}

// TestEnsureRunBranch_FullSHA verifies a full 40-hex SHA of an existing commit
// resolves.
func TestEnsureRunBranch_FullSHA(t *testing.T) {
	rr := seedResolvableClone(t)
	const runBranch = "tide/run-sha"
	got, err := EnsureRunBranch(rr.bareDir, runBranch, rr.defaultHead.String())
	if err != nil {
		t.Fatalf("EnsureRunBranch full SHA: %v", err)
	}
	if got != rr.defaultHead {
		t.Errorf("full SHA resolved to %s; want %s", got, rr.defaultHead)
	}
}

// TestEnsureRunBranch_NonexistentSHA verifies a syntactically valid 40-hex SHA
// of a nonexistent object is unresolvable with the reachability wording (D-03).
func TestEnsureRunBranch_NonexistentSHA(t *testing.T) {
	rr := seedResolvableClone(t)
	const missing = "0123456789abcdef0123456789abcdef01234567"
	_, err := EnsureRunBranch(rr.bareDir, "tide/run-missing-sha", missing)
	if err == nil {
		t.Fatal("expected error for nonexistent SHA, got nil")
	}
	if !errors.Is(err, ErrBaseRefUnresolvable) {
		t.Errorf("error does not wrap ErrBaseRefUnresolvable: %v", err)
	}
	if !strings.Contains(err.Error(), "reachable") {
		t.Errorf("nonexistent-SHA error should mention reachability; got %v", err)
	}
}

// TestEnsureRunBranch_RefsQualifiedTag verifies a refs/-qualified tag resolves
// verbatim (D-02) and still peels annotated tags.
func TestEnsureRunBranch_RefsQualifiedTag(t *testing.T) {
	rr := seedResolvableClone(t)
	got, err := EnsureRunBranch(rr.bareDir, "tide/run-refs-tag", "refs/tags/v1.0.0")
	if err != nil {
		t.Fatalf("EnsureRunBranch refs/tags/v1.0.0: %v", err)
	}
	if got != rr.defaultHead {
		t.Errorf("refs/tags/v1.0.0 resolved to %s; want %s", got, rr.defaultHead)
	}
}

// TestEnsureRunBranch_RefsQualifiedBranchRemoteFallback verifies the D-02
// escape-hatch nuance: refs/heads/<non-default> falls back to
// refs/remotes/origin/<non-default> in a production clone.
func TestEnsureRunBranch_RefsQualifiedBranchRemoteFallback(t *testing.T) {
	rr := seedResolvableClone(t)
	got, err := EnsureRunBranch(rr.bareDir, "tide/run-refs-branch", "refs/heads/feature/hotfix")
	if err != nil {
		t.Fatalf("EnsureRunBranch refs/heads/feature/hotfix: %v", err)
	}
	if got != rr.featureHead {
		t.Errorf("refs/heads/feature/hotfix resolved to %s; want feature tip %s", got, rr.featureHead)
	}
}

// TestEnsureRunBranch_RejectsDisallowedForms verifies D-01: HEAD, short SHAs,
// and ~/^ suffixes are rejected with a typed error naming the ref.
func TestEnsureRunBranch_RejectsDisallowedForms(t *testing.T) {
	rr := seedResolvableClone(t)
	shortSHA := rr.defaultHead.String()[:7]
	for i, ref := range []string{"HEAD", shortSHA, "master~1", "master^"} {
		ref := ref
		t.Run(ref, func(t *testing.T) {
			runBranch := "tide/run-reject-" + string(rune('a'+i))
			_, err := EnsureRunBranch(rr.bareDir, runBranch, ref)
			if err == nil {
				t.Fatalf("expected error for %q, got nil", ref)
			}
			if !errors.Is(err, ErrBaseRefUnresolvable) {
				t.Errorf("error for %q does not wrap ErrBaseRefUnresolvable: %v", ref, err)
			}
			if !strings.Contains(err.Error(), "unable to resolve") {
				t.Errorf("error for %q missing 'unable to resolve': %v", ref, err)
			}
			if !strings.Contains(err.Error(), ref) {
				t.Errorf("error for %q does not name the ref: %v", ref, err)
			}
		})
	}
}

// TestEnsureRunBranch_IdempotentIgnoresBaseRef verifies Pitfall 6 ordering: when
// the run branch already exists, a garbage baseRef is never resolved — the call
// returns the existing tip with no error.
func TestEnsureRunBranch_IdempotentIgnoresBaseRef(t *testing.T) {
	rr := seedResolvableClone(t)
	const runBranch = "tide/run-idem-baseref"

	first, err := EnsureRunBranch(rr.bareDir, runBranch, "feature/hotfix")
	if err != nil {
		t.Fatalf("EnsureRunBranch (first): %v", err)
	}

	// Second call with an unresolvable ref must NOT error and must return the
	// existing tip — resolution is skipped on the idempotent early-return path.
	second, err := EnsureRunBranch(rr.bareDir, runBranch, "totally-bogus-ref-xyz")
	if err != nil {
		t.Fatalf("idempotent call with garbage baseRef errored: %v", err)
	}
	if first != second {
		t.Errorf("idempotent call moved the ref: %s -> %s", first, second)
	}
}
