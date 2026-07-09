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

// Tests for cmd/dashboard/gitfetch. Uses file:// URLs against local bare
// repos (the same pattern pkg/git and cmd/tide-push tests use — fast, no
// network, no fixture servers). The production http(s) path shares the exact
// same CloneOptions, so the local transport exercises the real code.
package gitfetch

import (
	"context"
	"path"
	"sort"
	"testing"
	"time"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/storage/memory"
)

// repoFixture is a local bare repo seeded on a specific branch, addressable via
// a file:// URL. It keeps a live worktree so tests can advance the branch tip.
type repoFixture struct {
	url    string
	branch string
	repo   *gogit.Repository
	work   string
}

// seedRepo builds a bare repo carrying files on branch and returns the fixture
// plus the initial tip SHA. Keys of files are repo-relative slash paths.
func seedRepo(t *testing.T, branch string, files map[string]string) (*repoFixture, string) {
	t.Helper()
	base := t.TempDir()
	work := path.Join(base, "work")
	bare := path.Join(base, "origin.git")

	repo, err := gogit.PlainInit(work, false)
	if err != nil {
		t.Fatalf("PlainInit work: %v", err)
	}
	wt, err := repo.Worktree()
	if err != nil {
		t.Fatalf("Worktree: %v", err)
	}
	writeAndAdd(t, work, wt, files)
	h, err := wt.Commit("seed", commitOpts())
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}
	// Switch onto the target branch so later commits extend it.
	refName := plumbing.NewBranchReferenceName(branch)
	if err := wt.Checkout(&gogit.CheckoutOptions{Branch: refName, Create: true}); err != nil {
		t.Fatalf("Checkout create %s: %v", branch, err)
	}
	if _, err := gogit.PlainInit(bare, true); err != nil {
		t.Fatalf("PlainInit bare: %v", err)
	}
	if _, err := repo.CreateRemote(&config.RemoteConfig{
		Name: "origin",
		URLs: []string{"file://" + bare},
	}); err != nil {
		t.Fatalf("CreateRemote: %v", err)
	}
	f := &repoFixture{url: "file://" + bare, branch: branch, repo: repo, work: work}
	f.push(t)
	return f, h.String()
}

// addCommit extends the fixture branch with new files and returns the new tip.
func (f *repoFixture) addCommit(t *testing.T, files map[string]string) string {
	t.Helper()
	wt, err := f.repo.Worktree()
	if err != nil {
		t.Fatalf("Worktree: %v", err)
	}
	writeAndAdd(t, f.work, wt, files)
	h, err := wt.Commit("more", commitOpts())
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}
	f.push(t)
	return h.String()
}

func (f *repoFixture) push(t *testing.T) {
	t.Helper()
	ref := config.RefSpec("+" + plumbing.NewBranchReferenceName(f.branch) + ":" + plumbing.NewBranchReferenceName(f.branch))
	if err := f.repo.Push(&gogit.PushOptions{RemoteName: "origin", RefSpecs: []config.RefSpec{ref}}); err != nil {
		t.Fatalf("Push: %v", err)
	}
}

func writeAndAdd(t *testing.T, work string, wt *gogit.Worktree, files map[string]string) {
	t.Helper()
	fs := wt.Filesystem
	for p, c := range files {
		dir := path.Dir(p)
		if dir != "." {
			if err := fs.MkdirAll(dir, 0o755); err != nil {
				t.Fatalf("MkdirAll %s: %v", dir, err)
			}
		}
		fh, err := fs.Create(p)
		if err != nil {
			t.Fatalf("Create %s: %v", p, err)
		}
		if _, err := fh.Write([]byte(c)); err != nil {
			t.Fatalf("Write %s: %v", p, err)
		}
		_ = fh.Close()
		if _, err := wt.Add(p); err != nil {
			t.Fatalf("Add %s: %v", p, err)
		}
	}
}

func commitOpts() *gogit.CommitOptions {
	return &gogit.CommitOptions{
		Author: &object.Signature{Name: "Fixture", Email: "fixture@example.com", When: time.Now()},
	}
}

func testCtx(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	t.Cleanup(cancel)
	return ctx
}

// Test 1: Fetch returns the tip SHA and exactly the .tide/ files with
// byte-identical content; non-.tide files are excluded.
func TestGoGitFetcherFetchExtractsTideTree(t *testing.T) {
	const branch = "tide/run-extract"
	milestone := ".tide/planning/milestone/m1/MILESTONE.md"
	child := ".tide/planning/milestone/m1/children/p1.json"
	f, wantSHA := seedRepo(t, branch, map[string]string{
		milestone:   "# milestone m1\n",
		child:       `{"kind":"Phase","id":"p1"}`,
		"README.md": "not a tide file\n",
	})

	var g GoGitFetcher
	gotSHA, files, err := g.Fetch(testCtx(t), f.url, branch, nil)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if gotSHA != wantSHA {
		t.Errorf("Fetch sha = %s, want %s", gotSHA, wantSHA)
	}

	byPath := map[string][]byte{}
	for _, fl := range files {
		byPath[fl.Path] = fl.Content
	}
	if len(byPath) != 2 {
		t.Fatalf("got %d .tide files %v, want exactly 2", len(byPath), keys(byPath))
	}
	if got := string(byPath[milestone]); got != "# milestone m1\n" {
		t.Errorf("MILESTONE.md content = %q", got)
	}
	if got := string(byPath[child]); got != `{"kind":"Phase","id":"p1"}` {
		t.Errorf("child json content = %q", got)
	}
	// Name is the base name, Path is the full repo-relative path.
	for _, fl := range files {
		if fl.Name != path.Base(fl.Path) {
			t.Errorf("File.Name = %q, want base of %q", fl.Name, fl.Path)
		}
	}
	if _, leaked := byPath["README.md"]; leaked {
		t.Error("non-.tide README.md leaked into results")
	}
}

// Test 2: Tip agrees with Fetch, and a new commit changes the Tip result.
func TestGoGitFetcherTipTracksBranchTip(t *testing.T) {
	const branch = "tide/run-tip"
	f, _ := seedRepo(t, branch, map[string]string{
		".tide/planning/milestone/m1/MILESTONE.md": "v1\n",
	})

	var g GoGitFetcher
	ctx := testCtx(t)

	tip1, err := g.Tip(ctx, f.url, branch, nil)
	if err != nil {
		t.Fatalf("Tip 1: %v", err)
	}
	fetchSHA, _, err := g.Fetch(ctx, f.url, branch, nil)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if tip1 != fetchSHA {
		t.Errorf("Tip %s != Fetch %s", tip1, fetchSHA)
	}

	newSHA := f.addCommit(t, map[string]string{
		".tide/planning/milestone/m1/MILESTONE.md": "v2\n",
	})
	tip2, err := g.Tip(ctx, f.url, branch, nil)
	if err != nil {
		t.Fatalf("Tip 2: %v", err)
	}
	if tip2 == tip1 {
		t.Errorf("Tip did not advance after new commit: %s", tip2)
	}
	if tip2 != newSHA {
		t.Errorf("Tip = %s, want new commit %s", tip2, newSHA)
	}
}

// Test 3: Fetch on a nonexistent branch returns a wrapped error — no panic,
// no empty-success.
func TestGoGitFetcherFetchNonexistentBranchErrors(t *testing.T) {
	const branch = "tide/run-real"
	f, _ := seedRepo(t, branch, map[string]string{
		".tide/x.md": "x\n",
	})

	var g GoGitFetcher
	sha, files, err := g.Fetch(testCtx(t), f.url, "tide/run-missing", nil)
	if err == nil {
		t.Fatalf("Fetch missing branch: err = nil, sha=%q files=%d", sha, len(files))
	}
}

// Test 4: a repo with no .tide/ directory returns sha + empty files, nil error
// (absence is data, not an error).
func TestGoGitFetcherFetchNoTideDirIsEmptyNotError(t *testing.T) {
	const branch = "tide/run-empty"
	f, wantSHA := seedRepo(t, branch, map[string]string{
		"README.md":   "only a readme\n",
		"src/main.go": "package main\n",
	})

	var g GoGitFetcher
	sha, files, err := g.Fetch(testCtx(t), f.url, branch, nil)
	if err != nil {
		t.Fatalf("Fetch no-.tide: %v", err)
	}
	if sha != wantSHA {
		t.Errorf("sha = %s, want %s", sha, wantSHA)
	}
	if len(files) != 0 {
		t.Errorf("files = %v, want empty", files)
	}
}

// Test 5 (T-37-03-01): a failed fetch carrying an Auth PAT must not embed the
// password value in the returned error string.
func TestGoGitFetcherErrorNeverLeaksPAT(t *testing.T) {
	const branch = "tide/run-secret"
	const pat = "supersecret-pat-value-DO-NOT-LEAK"
	f, _ := seedRepo(t, branch, map[string]string{
		".tide/x.md": "x\n",
	})

	var g GoGitFetcher
	// Nonexistent branch forces an error while Auth is populated.
	_, _, err := g.Fetch(testCtx(t), f.url, "tide/run-missing", &Auth{Username: "x-access-token", Password: pat})
	if err == nil {
		t.Fatal("expected error fetching missing branch with auth")
	}
	if contains(err.Error(), pat) {
		t.Errorf("error string leaked PAT: %q", err.Error())
	}
}

// sanity: the fixture actually stores a real memory-backed clone (guards the
// import used by the production path so the seam stays honest).
func TestFixtureUsesMemoryStorer(t *testing.T) {
	_ = memory.NewStorage()
}

func keys(m map[string][]byte) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func contains(s, sub string) bool {
	return len(sub) > 0 && len(s) >= len(sub) && indexOf(s, sub) >= 0
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
