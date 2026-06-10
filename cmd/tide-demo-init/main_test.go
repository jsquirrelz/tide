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
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	gogit "github.com/go-git/go-git/v5"
)

// TestBootstrapDirRequired covers the invariant path (--bootstrap-dir
// empty). The function MUST return an *invariantError so main()'s
// errors.As discriminator maps it to exit code 2 (per cmd/tide-push
// convention). Tests assert against the sentinel via errors.As rather
// than string-matching the message — that keeps the human-readable
// stderr text free to evolve.
func TestBootstrapDirRequired(t *testing.T) {
	err := bootstrap(context.Background(), "")
	if err == nil {
		t.Fatal("bootstrap(\"\") returned nil error; want invariantError")
	}
	var inv *invariantError
	if !errors.As(err, &inv) {
		t.Fatalf("bootstrap(\"\") error = %v (%T); want *invariantError", err, err)
	}
}

// TestBootstrapRefusesExistingTarget covers the second invariant path
// (target dir already exists). The function MUST refuse to overwrite an
// existing path, returning an *invariantError. Distinct from the
// empty-dir path so the exit-code discriminator distinguishes "you
// forgot the flag" from "you re-ran without cleaning up the PVC".
func TestBootstrapRefusesExistingTarget(t *testing.T) {
	tempdir := t.TempDir()
	existing := filepath.Join(tempdir, "demo-remote.git")
	if err := os.MkdirAll(existing, 0o755); err != nil {
		t.Fatalf("MkdirAll setup: %v", err)
	}

	err := bootstrap(context.Background(), existing)
	if err == nil {
		t.Fatalf("bootstrap(%q) returned nil error; want invariantError for existing target", existing)
	}
	var inv *invariantError
	if !errors.As(err, &inv) {
		t.Fatalf("bootstrap(%q) error = %v (%T); want *invariantError", existing, err, err)
	}
}

// TestBootstrapHappyPath covers the success path. After bootstrap returns
// nil, the target directory MUST be a bare git repo (HEAD file present,
// no .git subdir) carrying a commit reachable from HEAD whose tree
// contains the embedded fixture files (verified via clone-and-inspect).
//
// The clone-back step is the strongest end-to-end check: it exercises
// the same code path the medium-sample controller's clone Job will run
// against this bare repo. If the bare repo were missing commits or
// refs, the clone would fail or produce an empty tree.
func TestBootstrapHappyPath(t *testing.T) {
	tempdir := t.TempDir()
	bareDir := filepath.Join(tempdir, "demo-remote.git")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := bootstrap(ctx, bareDir); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}

	// 1. Bare repo shape: HEAD present, .git absent.
	if _, err := os.Stat(filepath.Join(bareDir, "HEAD")); err != nil {
		t.Errorf("expected HEAD file in bare dir %q: %v", bareDir, err)
	}
	if _, err := os.Stat(filepath.Join(bareDir, ".git")); err == nil {
		t.Errorf("bare clone should not have a .git subdir at %q", bareDir)
	}

	// 2. Open the bare repo + check it has commits via PlainOpen.
	bareRepo, err := gogit.PlainOpen(bareDir)
	if err != nil {
		t.Fatalf("PlainOpen bare repo %q: %v", bareDir, err)
	}
	headRef, err := bareRepo.Head()
	if err != nil {
		t.Fatalf("bareRepo.Head: %v (no commits pushed?)", err)
	}
	if headRef.Hash().IsZero() {
		t.Fatal("bareRepo HEAD hash is zero; want a real commit")
	}
	// Confirm the commit object resolves (catches missing-object cases).
	commit, err := bareRepo.CommitObject(headRef.Hash())
	if err != nil {
		t.Fatalf("CommitObject %s: %v", headRef.Hash(), err)
	}
	if commit.Author.Name != authorName {
		t.Errorf("commit author name = %q, want %q", commit.Author.Name, authorName)
	}
	if commit.Author.Email != authorEmail {
		t.Errorf("commit author email = %q, want %q", commit.Author.Email, authorEmail)
	}
	if !strings.Contains(commit.Message, "Phase 5 D-B3") {
		t.Errorf("commit message = %q, want substring %q", commit.Message, "Phase 5 D-B3")
	}

	// 3. Clone the bare repo into a fresh tempdir and inspect the
	//    working tree. This is the strongest assertion — it proves the
	//    bare repo is clone-able (the operation the medium-sample
	//    controller's clone Job performs) AND that the embedded fixture
	//    content actually flowed through the seeding commit into the
	//    cloned working tree.
	cloneDir := filepath.Join(tempdir, "clone")
	cloneCtx, cloneCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cloneCancel()
	if _, err := gogit.PlainCloneContext(cloneCtx, cloneDir, false /* not bare */, &gogit.CloneOptions{
		URL: "file://" + bareDir,
	}); err != nil {
		t.Fatalf("PlainClone file://%s: %v", bareDir, err)
	}

	// The embedded fixture lives at examples/tide-demo-fixture/ in the
	// repo SOT, copied into cmd/tide-demo-init/fixture/ via go:generate
	// (local builds) or Dockerfile COPY (image builds). After unpack,
	// the cloned working tree should carry main.go, main_test.go,
	// go.mod, and README.md at its root.
	for _, want := range []string{"main.go", "main_test.go", "go.mod", "README.md"} {
		path := filepath.Join(cloneDir, want)
		info, err := os.Stat(path)
		if err != nil {
			t.Errorf("expected fixture file %q in clone: %v", want, err)
			continue
		}
		if info.Size() == 0 {
			t.Errorf("fixture file %q in clone is empty", want)
		}
	}

	// 4. The main.go from the fixture SOT carries the Greeting()
	//    function. Verify it round-tripped through embed → bare push →
	//    clone without bytewise drift.
	data, err := os.ReadFile(filepath.Join(cloneDir, "main.go"))
	if err != nil {
		t.Fatalf("read clone main.go: %v", err)
	}
	if !strings.Contains(string(data), "func Greeting") {
		t.Errorf("cloned main.go missing func Greeting; embedded fixture drift?\ngot:\n%s", string(data))
	}
}

// TestBootstrapEnablesAnonymousReceivePack (debug #15) asserts the
// bootstrapped bare repo carries http.receivepack=true in its config.
// git-http-backend advertises and serves the receive-pack service ONLY
// when the served repo's own config has http.receivepack truthy — the
// nginx GIT_HTTP_RECEIVE_PACK fastcgi param is a no-op git-http-backend
// does not honor. Without this option the boundary push of the per-run
// branch to the in-cluster http:// remote is rejected ("authorization
// failed"). This is the seed-time half of the fix (entrypoint.sh self-heals
// the same option at startup as a backstop).
func TestBootstrapEnablesAnonymousReceivePack(t *testing.T) {
	tempdir := t.TempDir()
	bareDir := filepath.Join(tempdir, "demo-remote.git")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := bootstrap(ctx, bareDir); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}

	bareRepo, err := gogit.PlainOpen(bareDir)
	if err != nil {
		t.Fatalf("PlainOpen bare repo %q: %v", bareDir, err)
	}
	cfg, err := bareRepo.Config()
	if err != nil {
		t.Fatalf("bareRepo.Config: %v", err)
	}
	got := cfg.Raw.Section("http").Option("receivepack")
	if got != "true" {
		t.Errorf("bare repo http.receivepack = %q, want \"true\" (anonymous push would be refused by git-http-backend)", got)
	}

	// The raw on-disk config file must also carry it — Config() reflects the
	// stored config, but assert the persisted file directly so a future change
	// that only mutates the in-memory copy (without SetConfig) is caught.
	raw, err := os.ReadFile(filepath.Join(bareDir, "config"))
	if err != nil {
		t.Fatalf("read bare repo config file: %v", err)
	}
	if !strings.Contains(strings.ToLower(string(raw)), "receivepack = true") {
		t.Errorf("on-disk bare repo config missing 'receivepack = true':\n%s", string(raw))
	}
}
