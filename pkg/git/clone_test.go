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

// seedBareRepo creates a bare repository at <baseDir>/origin.git with two
// commits on the default "master" branch and returns the bare repo's
// filesystem path and the SHA of the second (HEAD) commit. Test helper
// only — the bare repo is reachable via "file://<path>" by go-git.
//
// The package uses go-git's default initial-branch name ("master") rather
// than overriding to "main" to keep the helper free of init-time options
// that vary across go-git versions. Tests that need a specific branch
// name use the returned default.
func seedBareRepo(t *testing.T, baseDir string) (string, plumbing.Hash) {
	t.Helper()

	bareDir := filepath.Join(baseDir, "origin.git")
	workDir := filepath.Join(baseDir, "origin-work")

	// 1. Create a non-bare repo with two commits in workDir.
	repo, err := gogit.PlainInit(workDir, false)
	if err != nil {
		t.Fatalf("PlainInit non-bare: %v", err)
	}

	wt, err := repo.Worktree()
	if err != nil {
		t.Fatalf("Worktree: %v", err)
	}

	mkCommit := func(filename, content, msg string) plumbing.Hash {
		t.Helper()
		path := filepath.Join(workDir, filename)
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", filename, err)
		}
		if _, err := wt.Add(filename); err != nil {
			t.Fatalf("Add %s: %v", filename, err)
		}
		h, err := wt.Commit(msg, &gogit.CommitOptions{
			Author: &object.Signature{
				Name:  "Test Author",
				Email: "test@example.com",
				When:  time.Now(),
			},
		})
		if err != nil {
			t.Fatalf("Commit %s: %v", msg, err)
		}
		return h
	}

	_ = mkCommit("README.md", "first\n", "first commit")
	head := mkCommit("README.md", "first\nsecond\n", "second commit")

	// 2. Bare-clone workDir into bareDir via PlainClone with file:// URL —
	//    no auth, no network, just a local filesystem clone.
	if _, err := gogit.PlainClone(bareDir, true /* bare */, &gogit.CloneOptions{
		URL: "file://" + workDir,
	}); err != nil {
		t.Fatalf("PlainClone bare: %v", err)
	}

	return bareDir, head
}

// defaultBranchOf returns the short branch name that the bare repo's HEAD
// symbolic ref points at. Used by tests to avoid hardcoding "master" vs
// "main" across go-git versions or future init-defaults changes.
func defaultBranchOf(t *testing.T, repo *gogit.Repository) string {
	t.Helper()
	ref, err := repo.Reference(plumbing.HEAD, false)
	if err != nil {
		t.Fatalf("Reference HEAD: %v", err)
	}
	// HEAD is a symbolic ref to refs/heads/<branch>.
	target := ref.Target()
	return target.Short()
}

// TestCloneSucceeds covers the happy path: clone a file://-backed bare
// repo and verify HEAD matches the source's second commit.
func TestCloneSucceeds(t *testing.T) {
	base := t.TempDir()
	bareSrc, srcHead := seedBareRepo(t, base)

	destDir := filepath.Join(t.TempDir(), "dest.git")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	repo, err := Clone(ctx, "file://"+bareSrc, destDir, "any-pat-ignored-for-file-url")
	if err != nil {
		t.Fatalf("Clone: %v", err)
	}
	if repo == nil {
		t.Fatal("Clone returned nil repo without error")
	}

	// Verify destDir contains a bare-repo layout (HEAD file at root, no
	// .git subdir).
	if _, err := os.Stat(filepath.Join(destDir, "HEAD")); err != nil {
		t.Errorf("expected HEAD file in bare dest %q: %v", destDir, err)
	}
	if _, err := os.Stat(filepath.Join(destDir, ".git")); err == nil {
		t.Errorf("bare clone should not have a .git subdir at %q", destDir)
	}

	// Verify HEAD points at srcHead (the second commit).
	branch := defaultBranchOf(t, repo)
	headRef, err := repo.Reference(plumbing.NewBranchReferenceName(branch), false)
	if err != nil {
		t.Fatalf("Reference %s: %v", branch, err)
	}
	if headRef.Hash() != srcHead {
		t.Errorf("clone HEAD = %s, want %s", headRef.Hash(), srcHead)
	}
}

// TestCloneFailsOnUnreachableURL exercises the error path: a context with
// a short deadline against an unused port should surface a non-nil error
// well within the test's outer timeout.
func TestCloneFailsOnUnreachableURL(t *testing.T) {
	destDir := filepath.Join(t.TempDir(), "dest.git")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// 127.0.0.1:1 is the canonical "nothing listening" port for tests.
	// The clone should fail with a transport / dial error or a context
	// timeout — either way, a non-nil error.
	_, err := Clone(ctx, "http://127.0.0.1:1/repo.git", destDir, "test-pat")
	if err == nil {
		t.Fatal("Clone against unreachable URL returned nil error")
	}
}
