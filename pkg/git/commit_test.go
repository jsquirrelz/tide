package git

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
)

func TestCommitWritesNewCommit(t *testing.T) {
	base := t.TempDir()
	bareSrc, _ := seedBareRepo(t, base)

	cloneDir := filepath.Join(t.TempDir(), "clone")
	repo, err := gogit.PlainClone(cloneDir, false, &gogit.CloneOptions{
		URL: "file://" + bareSrc,
	})
	if err != nil {
		t.Fatalf("PlainClone: %v", err)
	}
	wt, err := repo.Worktree()
	if err != nil {
		t.Fatalf("Worktree: %v", err)
	}

	if err := os.WriteFile(filepath.Join(cloneDir, "newfile.md"), []byte("hello\n"), 0o644); err != nil {
		t.Fatalf("write newfile: %v", err)
	}
	if err := AddPath(wt, "newfile.md"); err != nil {
		t.Fatalf("AddPath: %v", err)
	}

	author := object.Signature{
		Name:  "TIDE bot",
		Email: "tide@local",
		When:  time.Now(),
	}
	hash, err := Commit(wt, "test: new commit", author)
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if hash.IsZero() {
		t.Fatal("Commit returned zero hash")
	}

	// Verify the commit is reachable via repo.Log.
	logIter, err := repo.Log(&gogit.LogOptions{})
	if err != nil {
		t.Fatalf("repo.Log: %v", err)
	}
	defer logIter.Close()
	first, err := logIter.Next()
	if err != nil {
		t.Fatalf("logIter.Next: %v", err)
	}
	if first.Hash != hash {
		t.Errorf("log HEAD = %s; want %s", first.Hash, hash)
	}
	if first.Author.Name != "TIDE bot" || first.Author.Email != "tide@local" {
		t.Errorf("author = %s <%s>; want TIDE bot <tide@local>",
			first.Author.Name, first.Author.Email)
	}
	if first.Message != "test: new commit" {
		t.Errorf("message = %q; want %q", first.Message, "test: new commit")
	}
}

func TestAddPathStagesSingleFile(t *testing.T) {
	base := t.TempDir()
	bareSrc, _ := seedBareRepo(t, base)

	cloneDir := filepath.Join(t.TempDir(), "clone")
	repo, err := gogit.PlainClone(cloneDir, false, &gogit.CloneOptions{
		URL: "file://" + bareSrc,
	})
	if err != nil {
		t.Fatalf("PlainClone: %v", err)
	}
	wt, err := repo.Worktree()
	if err != nil {
		t.Fatalf("Worktree: %v", err)
	}

	// Create a file in a subdir to exercise the path-relative-to-worktree
	// behavior cmd/tide-push will rely on (D-B2 / W11).
	subdir := filepath.Join(cloneDir, "artifacts")
	if err := os.MkdirAll(subdir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	rel := filepath.Join("artifacts", "PLAN.md")
	if err := os.WriteFile(filepath.Join(cloneDir, rel), []byte("# plan\n"), 0o644); err != nil {
		t.Fatalf("write PLAN.md: %v", err)
	}

	if err := AddPath(wt, rel); err != nil {
		t.Fatalf("AddPath: %v", err)
	}

	status, err := wt.Status()
	if err != nil {
		t.Fatalf("wt.Status: %v", err)
	}
	entry, ok := status[rel]
	if !ok {
		t.Fatalf("path %q not in status: %+v", rel, status)
	}
	if entry.Staging != gogit.Added {
		t.Errorf("Staging for %q = %v; want %v (Added)", rel, entry.Staging, gogit.Added)
	}
}

func TestCommitNilWorktreeReturnsError(t *testing.T) {
	_, err := Commit(nil, "msg", object.Signature{Name: "x", Email: "x@x", When: time.Now()})
	if err == nil {
		t.Fatal("Commit(nil worktree) returned nil error")
	}
}

func TestAddPathNilWorktreeReturnsError(t *testing.T) {
	if err := AddPath(nil, "foo.txt"); err == nil {
		t.Fatal("AddPath(nil worktree) returned nil error")
	}
}
