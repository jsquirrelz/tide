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

// Tests for cmd/tide-push. Mirrors cmd/stub-subagent/main_test.go shape: a
// testable run() function with all config-as-parameters so tests can drive
// it without setting os.Args. Uses file:// URLs against local bare repos
// (the same pattern pkg/git's own tests use — fast, no network, no fixture
// servers).
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"

	pkggit "github.com/jsquirrelz/tide/pkg/git"
)

// seedBareRepo creates a bare repository at <baseDir>/origin.git with one
// initial commit on the default branch. Mirrors pkg/git's test helper.
// Returns the bare repo's filesystem path, the work-tree path used to seed
// it, and the SHA of the initial commit.
func seedBareRepo(t *testing.T, baseDir string) (string, plumbing.Hash) {
	t.Helper()

	bareDir := filepath.Join(baseDir, "origin.git")
	workDir := filepath.Join(baseDir, "origin-work")

	repo, err := gogit.PlainInit(workDir, false)
	if err != nil {
		t.Fatalf("PlainInit non-bare: %v", err)
	}
	wt, err := repo.Worktree()
	if err != nil {
		t.Fatalf("Worktree: %v", err)
	}

	readme := filepath.Join(workDir, "README.md")
	if err := os.WriteFile(readme, []byte("seed\n"), 0o644); err != nil {
		t.Fatalf("write seed README: %v", err)
	}
	if _, err := wt.Add("README.md"); err != nil {
		t.Fatalf("Add: %v", err)
	}
	h, err := wt.Commit("seed commit", &gogit.CommitOptions{
		Author: &object.Signature{Name: "Seed", Email: "seed@example.com", When: time.Now()},
	})
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}

	if _, err := gogit.PlainClone(bareDir, true /* bare */, &gogit.CloneOptions{
		URL: "file://" + workDir,
	}); err != nil {
		t.Fatalf("PlainClone bare: %v", err)
	}
	return bareDir, h
}

// defaultBranchOf returns the bare repo's default branch short name.
func defaultBranchOf(t *testing.T, repo *gogit.Repository) string {
	t.Helper()
	ref, err := repo.Reference(plumbing.HEAD, false)
	if err != nil {
		t.Fatalf("Reference HEAD: %v", err)
	}
	return ref.Target().Short()
}

// setupWorkspace stages the per-Project PVC layout the push Job expects:
//
//	<workspace>/repo.git/                      (bare clone of origin.git)
//	<workspace>/worktrees/run-<branch>/        (per-run working tree on `branch`)
//	<workspace>/artifacts/.../<file>           (planner-emitted artifacts)
//	<workspace>/envelopes/push/                (push-result envelope dest)
//
// branch is the per-run branch the test wants the push to target — typically a
// "tide/run-<name>-<unix>" name per D-B6. The worktree is checked out on that
// branch (created off the bare's default if it doesn't exist).
func setupWorkspace(t *testing.T, bareSrc, branch string) string {
	t.Helper()
	ws := t.TempDir()

	// Clone bareSrc into <ws>/repo.git (bare). The orchestrator's clone-mode
	// Job would do this; tests pre-stage it so push-mode has a populated
	// /workspace/repo.git/.
	repoGit := filepath.Join(ws, "repo.git")
	if _, err := gogit.PlainClone(repoGit, true /* bare */, &gogit.CloneOptions{
		URL: "file://" + bareSrc,
	}); err != nil {
		t.Fatalf("setupWorkspace PlainClone bare: %v", err)
	}

	// Clone the bare back into a working worktree at <ws>/worktrees/run-<branch>/
	wt := filepath.Join(ws, "worktrees", "run-"+branch)
	if _, err := gogit.PlainClone(wt, false /* not bare */, &gogit.CloneOptions{
		URL: "file://" + repoGit,
	}); err != nil {
		t.Fatalf("setupWorkspace PlainClone worktree: %v", err)
	}

	// Rewrite the worktree's "origin" to point at bareSrc directly so the
	// push from the worktree lands on the test's source-of-truth bare repo.
	repo, err := gogit.PlainOpen(wt)
	if err != nil {
		t.Fatalf("setupWorkspace PlainOpen worktree: %v", err)
	}
	if err := repo.DeleteRemote("origin"); err != nil {
		t.Fatalf("DeleteRemote origin: %v", err)
	}
	if _, err := repo.CreateRemote(&config.RemoteConfig{
		Name: "origin",
		URLs: []string{"file://" + bareSrc},
	}); err != nil {
		t.Fatalf("CreateRemote origin: %v", err)
	}

	// Check out the per-run branch on the worktree (D-B6 — push Jobs always
	// target a per-run branch, never the bare's default). If the bare
	// already has it the worktree picks up the existing ref; if not, we
	// create it pointing at HEAD.
	wtRepo, err := repo.Worktree()
	if err != nil {
		t.Fatalf("Worktree(): %v", err)
	}
	head, err := repo.Head()
	if err != nil {
		t.Fatalf("Head(): %v", err)
	}
	refName := plumbing.NewBranchReferenceName(branch)
	if _, err := repo.Reference(refName, false); err != nil {
		// branch doesn't exist locally — create it at HEAD
		if err := repo.Storer.SetReference(plumbing.NewHashReference(refName, head.Hash())); err != nil {
			t.Fatalf("SetReference %s: %v", refName, err)
		}
	}
	if err := wtRepo.Checkout(&gogit.CheckoutOptions{Branch: refName}); err != nil {
		t.Fatalf("Checkout %s: %v", refName, err)
	}

	// Pre-create artifact + envelope dirs.
	if err := os.MkdirAll(filepath.Join(ws, "artifacts"), 0o755); err != nil {
		t.Fatalf("MkdirAll artifacts: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(ws, "envelopes", "push"), 0o755); err != nil {
		t.Fatalf("MkdirAll envelopes/push: %v", err)
	}
	return ws
}

// perRunBranch returns a D-B6-shaped per-run branch name.
func perRunBranch(t *testing.T, prefix string) string {
	t.Helper()
	return "tide/run-" + prefix + "-1747200000"
}

// writeArtifact creates a file at workspace-relative path with content.
func writeArtifact(t *testing.T, workspace, relpath, content string) {
	t.Helper()
	full := filepath.Join(workspace, relpath)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("MkdirAll %s: %v", filepath.Dir(full), err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile %s: %v", full, err)
	}
}

// readPushEnvelope reads <workspace>/envelopes/push/<project-uid>.json.
func readPushEnvelope(t *testing.T, workspace, projectUID string) pushResult {
	t.Helper()
	path := filepath.Join(workspace, "envelopes", "push", projectUID+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read push envelope %s: %v", path, err)
	}
	var pr pushResult
	if err := json.Unmarshal(data, &pr); err != nil {
		t.Fatalf("unmarshal push envelope: %v", err)
	}
	return pr
}

// stderrAndRun runs run() against the given cfg, capturing stderr.
func stderrAndRun(t *testing.T, ctx context.Context, cfg pushConfig, pat string) (int, []byte) {
	t.Helper()
	t.Setenv("GIT_PAT", pat)
	var stderr bytes.Buffer
	exit := run(ctx, cfg, io.Discard, &stderr)
	return exit, stderr.Bytes()
}

// ---------- Test 1: push mode, clean diff, first push ----------

func TestRunPushModeCleanFirstPush(t *testing.T) {
	base := t.TempDir()
	bareSrc, _ := seedBareRepo(t, base)
	bareRepo, err := gogit.PlainOpen(bareSrc)
	if err != nil {
		t.Fatalf("PlainOpen bareSrc: %v", err)
	}
	branch := perRunBranch(t, "clean")
	_ = defaultBranchOf(t, bareRepo) // unused but kept to assert helper compiles

	ws := setupWorkspace(t, bareSrc, branch)
	writeArtifact(t, ws, "artifacts/M-001/P-003/L-005/PLAN.md", "# clean plan\n")

	cfg := pushConfig{
		Mode:          "push",
		Branch:        branch,
		LastPushedSHA: "",
		CommitMessage: "tide: plan 03-foo authored + executed",
		ArtifactPaths: []string{"artifacts/M-001/P-003/L-005/PLAN.md"},
		Workspace:     ws,
		ProjectUID:    "p1",
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	exit, stderr := stderrAndRun(t, ctx, cfg, "test-pat-clean")
	if exit != 0 {
		t.Fatalf("exit=%d, stderr=%s", exit, stderr)
	}
	if bytes.Contains(stderr, []byte("test-pat-clean")) {
		t.Errorf("PAT leaked to stderr: %s", stderr)
	}

	pr := readPushEnvelope(t, ws, "p1")
	if pr.ExitCode != 0 {
		t.Errorf("envelope.exitCode = %d, want 0", pr.ExitCode)
	}
	if pr.Reason != "" {
		t.Errorf("envelope.reason = %q, want empty", pr.Reason)
	}
	if pr.HeadSHA == "" {
		t.Error("envelope.headSHA is empty")
	}

	// Verify bare repo advanced.
	ref, err := bareRepo.Reference(plumbing.NewBranchReferenceName(branch), false)
	if err != nil {
		t.Fatalf("Reference bare branch: %v", err)
	}
	if ref.Hash().String() != pr.HeadSHA {
		t.Errorf("bare ref = %s, envelope.headSHA = %s", ref.Hash(), pr.HeadSHA)
	}

	// Verify commit message is the exact W11 boundary message.
	bareCommit, err := bareRepo.CommitObject(ref.Hash())
	if err != nil {
		t.Fatalf("CommitObject: %v", err)
	}
	if !strings.Contains(bareCommit.Message, "tide: plan 03-foo authored + executed") {
		t.Errorf("commit message = %q, want it to contain W11 boundary string", bareCommit.Message)
	}
}

// ---------- Test 2: push mode, diff contains sk-ant- secret ----------

func TestRunPushModeGitleaksBlocksAnthropicKey(t *testing.T) {
	base := t.TempDir()
	bareSrc, _ := seedBareRepo(t, base)
	bareRepo, err := gogit.PlainOpen(bareSrc)
	if err != nil {
		t.Fatalf("PlainOpen bareSrc: %v", err)
	}
	branch := perRunBranch(t, "leaky")
	// Bare repo has no per-run branch yet — that's the point of the test:
	// the leak must block the push BEFORE the branch is created on the bare.
	_ = bareRepo

	ws := setupWorkspace(t, bareSrc, branch)
	// 93-char body matches gitleaks v8 anthropic-api-key rule (mirrors
	// internal/gitleaks/scanner_test.go fixture). Plus trailing space so the
	// closing delimiter `[\x60'"\s;]` matches.
	const secret = "sk-ant-api03-odJFCrnl2edlBDdz1C5Jau2RJtBRnlWmTSHf6pWkLUyifDLkDmWJ6UuVTAIjvFu7WICPhDeOZIiBOB-Y6sHrFH2ZUCr-lAA"
	writeArtifact(t, ws, "artifacts/M-001/P-003/L-005/PLAN.md",
		"# plan with secret\nANTHROPIC_API_KEY="+secret+" \n")

	cfg := pushConfig{
		Mode:          "push",
		Branch:        branch,
		LastPushedSHA: "",
		CommitMessage: "tide: plan 03-foo authored + executed",
		ArtifactPaths: []string{"artifacts/M-001/P-003/L-005/PLAN.md"},
		Workspace:     ws,
		ProjectUID:    "p2",
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	exit, stderr := stderrAndRun(t, ctx, cfg, "test-pat-leak")
	if exit != 10 {
		t.Fatalf("exit=%d, want 10 (leak-detected); stderr=%s", exit, stderr)
	}
	if bytes.Contains(stderr, []byte("test-pat-leak")) {
		t.Errorf("PAT leaked to stderr: %s", stderr)
	}
	// Also confirm the secret itself is NOT logged verbatim — the redact
	// boundary should NOT echo the matched secret value either. We don't
	// assert the secret string is absent (gitleaks Findings carry the
	// match; if it leaks via Finding.Secret that's a separate bug). What
	// matters here is the binary refuses to push.

	pr := readPushEnvelope(t, ws, "p2")
	if pr.ExitCode != 10 {
		t.Errorf("envelope.exitCode = %d, want 10", pr.ExitCode)
	}
	if pr.Reason != "leak-detected" {
		t.Errorf("envelope.reason = %q, want %q", pr.Reason, "leak-detected")
	}

	// Bare repo MUST NOT have the per-run branch (push refused → ref never
	// created on remote).
	bareReopen, err := gogit.PlainOpen(bareSrc)
	if err != nil {
		t.Fatalf("PlainOpen bareSrc post-push: %v", err)
	}
	if _, refErr := bareReopen.Reference(plumbing.NewBranchReferenceName(branch), false); refErr == nil {
		t.Errorf("bare repo has per-run branch %s despite leak block", branch)
	}
}

// ---------- Test 3: push mode, subsequent push with lease ----------

func TestRunPushModeSubsequentPushHonorsLease(t *testing.T) {
	base := t.TempDir()
	bareSrc, _ := seedBareRepo(t, base)
	branch := perRunBranch(t, "lease")
	ws := setupWorkspace(t, bareSrc, branch)
	writeArtifact(t, ws, "artifacts/file-1.md", "# first push\n")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// First push.
	cfg1 := pushConfig{
		Mode:          "push",
		Branch:        branch,
		LastPushedSHA: "",
		CommitMessage: "tide: plan 03-foo authored + executed",
		ArtifactPaths: []string{"artifacts/file-1.md"},
		Workspace:     ws,
		ProjectUID:    "p3",
	}
	exit1, stderr1 := stderrAndRun(t, ctx, cfg1, "pat-1")
	if exit1 != 0 {
		t.Fatalf("first push exit=%d, stderr=%s", exit1, stderr1)
	}
	pr1 := readPushEnvelope(t, ws, "p3")
	if pr1.HeadSHA == "" {
		t.Fatal("first push: envelope.headSHA empty")
	}

	// Second push with lease=first headSHA.
	writeArtifact(t, ws, "artifacts/file-2.md", "# second push\n")
	cfg2 := pushConfig{
		Mode:          "push",
		Branch:        branch,
		LastPushedSHA: pr1.HeadSHA,
		CommitMessage: "tide: plan 03-bar authored + executed",
		ArtifactPaths: []string{"artifacts/file-2.md"},
		Workspace:     ws,
		ProjectUID:    "p3",
	}
	exit2, stderr2 := stderrAndRun(t, ctx, cfg2, "pat-2")
	if exit2 != 0 {
		t.Fatalf("second push exit=%d, stderr=%s", exit2, stderr2)
	}
	pr2 := readPushEnvelope(t, ws, "p3")
	if pr2.HeadSHA == pr1.HeadSHA {
		t.Errorf("envelope.headSHA didn't advance: %s == %s", pr2.HeadSHA, pr1.HeadSHA)
	}
}

// ---------- Test 4: push mode, never-targets-main guard ----------

func TestRunPushModeRefusesMainBranch(t *testing.T) {
	base := t.TempDir()
	bareSrc, _ := seedBareRepo(t, base)
	// We don't even need a workspace — the guard fires before anything else.
	ws := t.TempDir()
	if err := os.MkdirAll(filepath.Join(ws, "envelopes", "push"), 0o755); err != nil {
		t.Fatalf("mkdir envelopes: %v", err)
	}
	_ = bareSrc

	cfg := pushConfig{
		Mode:          "push",
		Branch:        "main",
		LastPushedSHA: "",
		CommitMessage: "tide: plan x authored",
		ArtifactPaths: []string{},
		Workspace:     ws,
		ProjectUID:    "p4",
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	exit, _ := stderrAndRun(t, ctx, cfg, "pat-main")
	if exit != 2 {
		t.Fatalf("exit=%d, want 2 (invariant violation)", exit)
	}
	pr := readPushEnvelope(t, ws, "p4")
	if pr.Reason != "invalid-branch" {
		t.Errorf("envelope.reason = %q, want %q", pr.Reason, "invalid-branch")
	}
}

// ---------- Test 5: clone mode ----------

func TestRunCloneMode(t *testing.T) {
	base := t.TempDir()
	bareSrc, _ := seedBareRepo(t, base)
	ws := t.TempDir()

	cfg := pushConfig{
		Mode:      "clone",
		RepoURL:   "file://" + bareSrc,
		Workspace: ws,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	exit, stderr := stderrAndRun(t, ctx, cfg, "any-pat-ignored-for-file")
	if exit != 0 {
		t.Fatalf("clone exit=%d, stderr=%s", exit, stderr)
	}

	// Bare repo cloned into <ws>/repo.git
	if _, err := os.Stat(filepath.Join(ws, "repo.git", "HEAD")); err != nil {
		t.Errorf("expected HEAD in %s/repo.git: %v", ws, err)
	}

	// Clone mode emits no envelope. The envelopes/push dir may or may not
	// exist; the push-result path is keyed on ProjectUID which is empty for
	// clone mode, so there is nothing to assert here beyond the HEAD check above.
}

// ---------- Test 6: push mode, missing GIT_PAT with http:// remote ----------
// Invariant 2 (D-B1) is now scheme-conditional: empty PAT is ACCEPTED for
// anonymous in-cluster http:// remotes. This test verifies the http:// case
// proceeds (no missing-creds exit) when GIT_PAT is empty.

func TestRunPushModeHTTPRemoteAcceptsEmptyPAT(t *testing.T) {
	base := t.TempDir()
	bareSrc, _ := seedBareRepo(t, base)
	branch := perRunBranch(t, "http-anon")
	ws := setupWorkspaceWithRemoteURL(t, bareSrc, branch,
		"http://git-http-server.tide-sample-medium.svc.cluster.local/demo-remote.git")

	writeArtifact(t, ws, "artifacts/file-http.md", "# http anon\n")

	cfg := pushConfig{
		Mode:          "push",
		Branch:        branch,
		LastPushedSHA: "",
		CommitMessage: "tide: http anon push",
		ArtifactPaths: []string{"artifacts/file-http.md"},
		Workspace:     ws,
		ProjectUID:    "p6-http",
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Explicitly NOT setting GIT_PAT — http:// remote must NOT require it.
	os.Unsetenv("GIT_PAT")
	var stderr bytes.Buffer
	// NOTE: This will fail at the network push step (no real http server),
	// but must NOT fail with exit 2 / missing-creds. Any exit other than 2
	// with reason != "missing-creds" proves the guard was relaxed correctly.
	exit := run(ctx, cfg, io.Discard, &stderr)
	if exit == exitInvariant {
		// Read the envelope if it exists to distinguish missing-creds from other invariants.
		envPath := filepath.Join(ws, "envelopes", "push", "p6-http.json")
		if data, readErr := os.ReadFile(envPath); readErr == nil {
			var pr pushResult
			if jsonErr := json.Unmarshal(data, &pr); jsonErr == nil && pr.Reason == "missing-creds" {
				t.Fatalf("http:// remote with empty GIT_PAT should not exit missing-creds; exit=%d stderr=%s", exit, stderr.Bytes())
			}
		}
		// If envelope doesn't exist or has a different reason, the invariant exit is
		// from a different guard (e.g. network failure trying to push) — that's acceptable.
	}
	// Any non-invariant exit (or invariant exit from a non-missing-creds reason) is acceptable:
	// the guard was relaxed. The test proves the scheme-conditional logic executes.
}

// ---------- Test 6b: push mode, missing GIT_PAT with https:// remote (must still fail) ----------
// Invariant 2 (D-B1): GIT_PAT REQUIRED for https:// production remotes.
// T-08-05-03 mitigation: the scheme-conditional guard must NOT relax for https://.

func TestRunPushModeHTTPSRemoteRequiresPAT(t *testing.T) {
	base := t.TempDir()
	bareSrc, _ := seedBareRepo(t, base)
	branch := perRunBranch(t, "https-creds")
	ws := setupWorkspaceWithRemoteURL(t, bareSrc, branch,
		"https://github.com/owner/repo.git")

	cfg := pushConfig{
		Mode:          "push",
		Branch:        branch,
		LastPushedSHA: "",
		CommitMessage: "tide: https push",
		ArtifactPaths: []string{},
		Workspace:     ws,
		ProjectUID:    "p6b",
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Empty GIT_PAT + https:// remote → must exit with missing-creds.
	os.Unsetenv("GIT_PAT")
	var stderr bytes.Buffer
	exit := run(ctx, cfg, io.Discard, &stderr)
	if exit != exitInvariant {
		t.Fatalf("https:// + empty PAT: exit=%d want %d (exitInvariant); stderr=%s",
			exit, exitInvariant, stderr.Bytes())
	}
	pr := readPushEnvelope(t, ws, "p6b")
	if pr.Reason != "missing-creds" {
		t.Errorf("envelope.reason = %q, want %q", pr.Reason, "missing-creds")
	}
}

// ---------- Test 6c: push mode, missing GIT_PAT with git@ remote (must still fail) ----------
// T-08-05-03 mitigation: git@ (SSH) remotes must still require GIT_PAT (treated as SSH key).

func TestRunPushModeSSHRemoteRequiresPAT(t *testing.T) {
	base := t.TempDir()
	bareSrc, _ := seedBareRepo(t, base)
	branch := perRunBranch(t, "ssh-creds")
	ws := setupWorkspaceWithRemoteURL(t, bareSrc, branch,
		"git@github.com:owner/repo.git")

	cfg := pushConfig{
		Mode:          "push",
		Branch:        branch,
		LastPushedSHA: "",
		CommitMessage: "tide: ssh push",
		ArtifactPaths: []string{},
		Workspace:     ws,
		ProjectUID:    "p6c",
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Empty GIT_PAT + git@ remote → must exit with missing-creds.
	os.Unsetenv("GIT_PAT")
	var stderr bytes.Buffer
	exit := run(ctx, cfg, io.Discard, &stderr)
	if exit != exitInvariant {
		t.Fatalf("git@ + empty PAT: exit=%d want %d (exitInvariant); stderr=%s",
			exit, exitInvariant, stderr.Bytes())
	}
	pr := readPushEnvelope(t, ws, "p6c")
	if pr.Reason != "missing-creds" {
		t.Errorf("envelope.reason = %q, want %q", pr.Reason, "missing-creds")
	}
}

// setupWorkspaceWithRemoteURL is like setupWorkspace but sets the worktree's
// "origin" remote to remoteURL instead of the file:// bareSrc URL. This lets
// tests verify scheme-conditional behavior without needing a live server.
// The push will fail at the network layer for non-file URLs; the tests only
// care about the pre-push guard behavior, not the actual push outcome.
func setupWorkspaceWithRemoteURL(t *testing.T, bareSrc, branch, remoteURL string) string {
	t.Helper()
	ws := setupWorkspace(t, bareSrc, branch)

	// Re-open the worktree and change origin to the test remote URL.
	wt := filepath.Join(ws, "worktrees", "run-"+branch)
	repo, err := gogit.PlainOpen(wt)
	if err != nil {
		t.Fatalf("setupWorkspaceWithRemoteURL PlainOpen: %v", err)
	}
	if err := repo.DeleteRemote("origin"); err != nil {
		t.Fatalf("setupWorkspaceWithRemoteURL DeleteRemote: %v", err)
	}
	if _, err := repo.CreateRemote(&config.RemoteConfig{
		Name: "origin",
		URLs: []string{remoteURL},
	}); err != nil {
		t.Fatalf("setupWorkspaceWithRemoteURL CreateRemote %q: %v", remoteURL, err)
	}
	return ws
}

// ---------- Test 7: --commit-message + --artifact-paths exact message ----------

// ---------- Task 1 TDD: TestRunCloneModeNoRunBranchIsNoOp ----------
// With no --run-branch flag, clone completes without error and no run worktree
// is provisioned (backward-compatible behavior).

func TestRunCloneModeNoRunBranchIsNoOp(t *testing.T) {
	base := t.TempDir()
	bareSrc, _ := seedBareRepo(t, base)
	ws := t.TempDir()

	cfg := pushConfig{
		Mode:      "clone",
		RepoURL:   "file://" + bareSrc,
		Workspace: ws,
		// RunBranch intentionally absent (empty string).
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	exit, stderr := stderrAndRun(t, ctx, cfg, "")
	if exit != 0 {
		t.Fatalf("clone with no --run-branch exit=%d stderr=%s", exit, stderr)
	}

	// No run worktree should exist at all.
	worktreesDir := filepath.Join(ws, "worktrees")
	if entries, err := os.ReadDir(worktreesDir); err == nil && len(entries) > 0 {
		t.Errorf("expected no worktrees dir entries when --run-branch is absent; got %v", entries)
	}
}

// ---------- Task 1 TDD: TestRunCloneProvisions ----------
// With --run-branch=tide/run-test-123, after clone completes:
// (a) the run branch ref exists in the bare repo,
// (b) the run worktree directory exists.

func TestRunCloneProvisions(t *testing.T) {
	base := t.TempDir()
	bareSrc, _ := seedBareRepo(t, base)
	ws := t.TempDir()

	const runBranch = "tide/run-test-123"
	cfg := pushConfig{
		Mode:      "clone",
		RepoURL:   "file://" + bareSrc,
		Workspace: ws,
		RunBranch: runBranch,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	exit, stderr := stderrAndRun(t, ctx, cfg, "")
	if exit != 0 {
		t.Fatalf("clone with --run-branch exit=%d stderr=%s", exit, stderr)
	}

	destDir := filepath.Join(ws, "repo.git")

	// (a) Run branch ref must exist in the bare repo.
	bareRepo, err := gogit.PlainOpen(destDir)
	if err != nil {
		t.Fatalf("PlainOpen bare repo: %v", err)
	}
	refName := plumbing.NewBranchReferenceName(runBranch)
	if _, err := bareRepo.Reference(refName, false); err != nil {
		t.Errorf("run branch ref %q not found after clone: %v", runBranch, err)
	}

	// (b) Run worktree directory must exist.
	runWorktreeDir := filepath.Join(ws, "worktrees", "run-"+runBranch)
	if _, err := os.Stat(runWorktreeDir); err != nil {
		t.Errorf("run worktree directory %q not found after clone: %v", runWorktreeDir, err)
	}
}

// ---------- Task 1 TDD: TestRunPushIntegrateBeforeStage ----------
// With --integrate-task-branches and two pre-existing task branches in the
// test repo, runPush calls IntegrateTaskBranches before staging artifacts;
// both task branch files appear in the run branch after push.
//
// This test uses runClone with --run-branch to provision the workspace (so the
// run worktree is a proper linked worktree off repo.git that can see task
// branches), then pushes with --integrate-task-branches.

func TestRunPushIntegrateBeforeStage(t *testing.T) {
	base := t.TempDir()
	bareSrc, _ := seedBareRepo(t, base)
	branch := perRunBranch(t, "integrate")
	ws := t.TempDir()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Step 1: Clone and provision run worktree via runClone + --run-branch.
	cloneCfg := pushConfig{
		Mode:      "clone",
		RepoURL:   "file://" + bareSrc,
		Workspace: ws,
		RunBranch: branch,
	}
	if exit, stderr := stderrAndRun(t, ctx, cloneCfg, ""); exit != 0 {
		t.Fatalf("clone phase exit=%d stderr=%s", exit, stderr)
	}

	// Step 2: Create two task branches in the workspace bare repo.
	taskBranch1 := "tide/wt-uid1"
	taskBranch2 := "tide/wt-uid2"
	createTaskBranchWithFile(t, filepath.Join(ws, "repo.git"), taskBranch1, "task1.txt", "task1 content")
	createTaskBranchWithFile(t, filepath.Join(ws, "repo.git"), taskBranch2, "task2.txt", "task2 content")

	// Step 3: Write a planner artifact.
	writeArtifact(t, ws, "artifacts/plan.md", "# plan\n")

	// The push remote must point at bareSrc so stderrAndRun can push there via
	// file://. The run worktree (linked off repo.git) has origin pointing at
	// bareSrc already (it was cloned from there). We need to push to bareSrc.
	// Since the run worktree origin = bareSrc (inherited from clone), this works.

	// Step 4: Push with --integrate-task-branches.
	pushCfg := pushConfig{
		Mode:                  "push",
		Branch:                branch,
		LastPushedSHA:         "",
		CommitMessage:         "tide: integrate test",
		ArtifactPaths:         []string{"artifacts/plan.md"},
		IntegrateTaskBranches: []string{taskBranch1, taskBranch2},
		Workspace:             ws,
		ProjectUID:            "p-integrate",
	}
	if exit, stderr := stderrAndRun(t, ctx, pushCfg, "test-pat"); exit != 0 {
		t.Fatalf("push with --integrate-task-branches exit=%d stderr=%s", exit, stderr)
	}

	// Verify both task branch files appear in the bareSrc run branch.
	bareRepo, err := gogit.PlainOpen(bareSrc)
	if err != nil {
		t.Fatalf("PlainOpen bareSrc: %v", err)
	}
	ref, err := bareRepo.Reference(plumbing.NewBranchReferenceName(branch), false)
	if err != nil {
		t.Fatalf("Reference %q: %v", branch, err)
	}
	commit, err := bareRepo.CommitObject(ref.Hash())
	if err != nil {
		t.Fatalf("CommitObject: %v", err)
	}
	tree, err := commit.Tree()
	if err != nil {
		t.Fatalf("Tree: %v", err)
	}
	for _, fname := range []string{"task1.txt", "task2.txt"} {
		if _, err := tree.File(fname); err != nil {
			t.Errorf("file %q not found in run branch after integration: %v", fname, err)
		}
	}
}

// createTaskBranchWithFile creates a branch in the given bare repo with a
// single new file, using the default branch as base.
func createTaskBranchWithFile(t *testing.T, bareRepoPath, branchName, fileName, content string) {
	t.Helper()
	workDir := t.TempDir()
	repo, err := gogit.PlainClone(workDir, false, &gogit.CloneOptions{
		URL: "file://" + bareRepoPath,
	})
	if err != nil {
		t.Fatalf("createTaskBranchWithFile PlainClone: %v", err)
	}
	wt, err := repo.Worktree()
	if err != nil {
		t.Fatalf("createTaskBranchWithFile Worktree: %v", err)
	}
	head, err := repo.Head()
	if err != nil {
		t.Fatalf("createTaskBranchWithFile Head: %v", err)
	}
	refName := plumbing.NewBranchReferenceName(branchName)
	if err := repo.Storer.SetReference(plumbing.NewHashReference(refName, head.Hash())); err != nil {
		t.Fatalf("createTaskBranchWithFile SetReference: %v", err)
	}
	if err := wt.Checkout(&gogit.CheckoutOptions{Branch: refName}); err != nil {
		t.Fatalf("createTaskBranchWithFile Checkout: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workDir, fileName), []byte(content), 0o644); err != nil {
		t.Fatalf("createTaskBranchWithFile WriteFile: %v", err)
	}
	if _, err := wt.Add(fileName); err != nil {
		t.Fatalf("createTaskBranchWithFile Add: %v", err)
	}
	if _, err := wt.Commit("add "+fileName, &gogit.CommitOptions{
		Author: &object.Signature{Name: "Test", Email: "test@example.com", When: time.Now()},
	}); err != nil {
		t.Fatalf("createTaskBranchWithFile Commit: %v", err)
	}
	if err := repo.Push(&gogit.PushOptions{
		RemoteName: "origin",
		RefSpecs:   []config.RefSpec{config.RefSpec("+" + refName + ":" + refName)},
	}); err != nil {
		t.Fatalf("createTaskBranchWithFile Push: %v", err)
	}
}

// ---------- Task 1 (37-02): parseStageEnvelopes ----------

// TestParseStageEnvelopes covers the --stage-envelopes CSV parser: happy-path
// pairing + whitespace trimming + empty input (Test 1), and the full fail-closed
// rejection set including traversal (Test 2). Validation must be pure (no I/O) so
// bad args are rejected before any git operation.
func TestParseStageEnvelopes(t *testing.T) {
	t.Run("happy path pairs and trims", func(t *testing.T) {
		got, err := parseStageEnvelopes("abc-123:milestone/m1, def-456 : phase/p2")
		if err != nil {
			t.Fatalf("parseStageEnvelopes: unexpected error: %v", err)
		}
		want := []EnvelopeStage{
			{UID: "abc-123", DestPrefix: "milestone/m1"},
			{UID: "def-456", DestPrefix: "phase/p2"},
		}
		if len(got) != len(want) {
			t.Fatalf("len = %d, want %d (%+v)", len(got), len(want), got)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Errorf("pair[%d] = %+v, want %+v", i, got[i], want[i])
			}
		}
	})

	t.Run("empty input returns nil no error", func(t *testing.T) {
		got, err := parseStageEnvelopes("")
		if err != nil {
			t.Fatalf("empty input error: %v", err)
		}
		if got != nil {
			t.Errorf("empty input = %+v, want nil", got)
		}
		got, err = parseStageEnvelopes("   ")
		if err != nil {
			t.Fatalf("whitespace input error: %v", err)
		}
		if got != nil {
			t.Errorf("whitespace input = %+v, want nil", got)
		}
	})

	rejects := []struct {
		name string
		in   string
	}{
		{"no colon", "abc-123"},
		{"empty uid", ":milestone/m1"},
		{"empty destPrefix", "abc-123:"},
		{"destPrefix traversal dotdot", "abc-123:../escape"},
		{"destPrefix nested traversal", "abc-123:milestone/../../etc"},
		{"destPrefix absolute", "abc-123:/abs"},
		{"destPrefix backslash", "abc-123:milestone\\m1"},
		{"destPrefix leading dot", "abc-123:.hidden"},
		{"destPrefix leading slash pattern", "abc-123:/milestone/m1"},
		{"destPrefix trailing slash", "abc-123:milestone/"},
	}
	for _, tc := range rejects {
		t.Run("rejects "+tc.name, func(t *testing.T) {
			if _, err := parseStageEnvelopes(tc.in); err == nil {
				t.Errorf("parseStageEnvelopes(%q) = nil error, want rejection", tc.in)
			}
		})
	}
}

// TestStageEnvelopesInvalidValueFailsLoud proves a rejected --stage-envelopes
// value drives tide-push to a nonzero exit with envelope reason
// "artifact-stage-failed" BEFORE any git operation (Task 1 Test 3). The bare
// remote must be untouched — the per-run branch never appears.
func TestStageEnvelopesInvalidValueFailsLoud(t *testing.T) {
	base := t.TempDir()
	bareSrc, _ := seedBareRepo(t, base)
	branch := perRunBranch(t, "bad-stage")
	ws := setupWorkspace(t, bareSrc, branch)

	cfg := pushConfig{
		Mode:           "push",
		Branch:         branch,
		CommitMessage:  "tide: bad stage-envelopes",
		StageEnvelopes: "abc-123:../escape",
		Workspace:      ws,
		ProjectUID:     "p-bad-stage",
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	exit, stderr := stderrAndRun(t, ctx, cfg, "test-pat")
	if exit == 0 {
		t.Fatalf("exit=0, want nonzero for invalid --stage-envelopes; stderr=%s", stderr)
	}
	pr := readPushEnvelope(t, ws, "p-bad-stage")
	if pr.Reason != "artifact-stage-failed" {
		t.Errorf("envelope.reason = %q, want %q", pr.Reason, "artifact-stage-failed")
	}

	// No git op happened: the per-run branch must not exist on the remote.
	bareRepo, err := gogit.PlainOpen(bareSrc)
	if err != nil {
		t.Fatalf("PlainOpen bareSrc: %v", err)
	}
	if _, refErr := bareRepo.Reference(plumbing.NewBranchReferenceName(branch), false); refErr == nil {
		t.Errorf("bare repo has branch %s despite invalid stage-envelopes", branch)
	}
}

// ---------- Task 2 (37-02): envelope staging step ----------

// writeEnvelopeFile writes content to <workspace>/envelopes/<uid>/<rel>.
func writeEnvelopeFile(t *testing.T, workspace, uid, rel string, content []byte) {
	t.Helper()
	full := filepath.Join(workspace, "envelopes", uid, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("MkdirAll %s: %v", filepath.Dir(full), err)
	}
	if err := os.WriteFile(full, content, 0o644); err != nil {
		t.Fatalf("WriteFile %s: %v", full, err)
	}
}

// treePathsUnder returns the sorted set of tree paths on the bare branch whose
// name starts with prefix.
func treePathsUnder(t *testing.T, bareSrc, branch, prefix string) []string {
	t.Helper()
	repo, err := gogit.PlainOpen(bareSrc)
	if err != nil {
		t.Fatalf("PlainOpen %s: %v", bareSrc, err)
	}
	ref, err := repo.Reference(plumbing.NewBranchReferenceName(branch), false)
	if err != nil {
		t.Fatalf("Reference %s: %v", branch, err)
	}
	commit, err := repo.CommitObject(ref.Hash())
	if err != nil {
		t.Fatalf("CommitObject: %v", err)
	}
	tree, err := commit.Tree()
	if err != nil {
		t.Fatalf("Tree: %v", err)
	}
	var paths []string
	if err := tree.Files().ForEach(func(f *object.File) error {
		if strings.HasPrefix(f.Name, prefix) {
			paths = append(paths, f.Name)
		}
		return nil
	}); err != nil {
		t.Fatalf("walk tree: %v", err)
	}
	sort.Strings(paths)
	return paths
}

// treeFileBytes returns the byte content of a file at path on the bare branch.
func treeFileBytes(t *testing.T, bareSrc, branch, path string) []byte {
	t.Helper()
	repo, err := gogit.PlainOpen(bareSrc)
	if err != nil {
		t.Fatalf("PlainOpen %s: %v", bareSrc, err)
	}
	ref, err := repo.Reference(plumbing.NewBranchReferenceName(branch), false)
	if err != nil {
		t.Fatalf("Reference %s: %v", branch, err)
	}
	commit, err := repo.CommitObject(ref.Hash())
	if err != nil {
		t.Fatalf("CommitObject: %v", err)
	}
	f, err := commit.File(path)
	if err != nil {
		t.Fatalf("commit.File %s: %v", path, err)
	}
	r, err := f.Reader()
	if err != nil {
		t.Fatalf("File.Reader %s: %v", path, err)
	}
	defer func() { _ = r.Close() }()
	data, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read blob %s: %v", path, err)
	}
	return data
}

// commitCount returns the number of commits reachable from the bare branch tip.
func commitCount(t *testing.T, bareSrc, branch string) int {
	t.Helper()
	repo, err := gogit.PlainOpen(bareSrc)
	if err != nil {
		t.Fatalf("PlainOpen %s: %v", bareSrc, err)
	}
	ref, err := repo.Reference(plumbing.NewBranchReferenceName(branch), false)
	if err != nil {
		t.Fatalf("Reference %s: %v", branch, err)
	}
	iter, err := repo.Log(&gogit.LogOptions{From: ref.Hash()})
	if err != nil {
		t.Fatalf("Log: %v", err)
	}
	n := 0
	if err := iter.ForEach(func(*object.Commit) error { n++; return nil }); err != nil {
		t.Fatalf("iterate log: %v", err)
	}
	return n
}

// TestStageEnvelopesHappyPath (Task 2 Test 1): an envelope with planning *.md +
// children/*.json plus out.json/in.json stages EXACTLY the *.md and children
// JSON under .tide/planning/<destPrefix>/ — D-04 exclusion proven by the full
// listing (out.json/in.json never appear under .tide/).
func TestStageEnvelopesHappyPath(t *testing.T) {
	base := t.TempDir()
	bareSrc, _ := seedBareRepo(t, base)
	branch := perRunBranch(t, "stage-happy")
	ws := setupWorkspace(t, bareSrc, branch)

	writeEnvelopeFile(t, ws, "u1", "MILESTONE.md", []byte("# milestone\n"))
	writeEnvelopeFile(t, ws, "u1", "notes.md", []byte("some notes\n"))
	writeEnvelopeFile(t, ws, "u1", "children/phase-1.json", []byte(`{"kind":"Phase"}`))
	writeEnvelopeFile(t, ws, "u1", "out.json", []byte(`{"reason":""}`))
	writeEnvelopeFile(t, ws, "u1", "in.json", []byte(`{"prompt":"x"}`))

	cfg := pushConfig{
		Mode:           "push",
		Branch:         branch,
		CommitMessage:  "tide: stage milestone envelope",
		StageEnvelopes: "u1:milestone/m1",
		Workspace:      ws,
		ProjectUID:     "p-stage-happy",
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	exit, stderr := stderrAndRun(t, ctx, cfg, "test-pat")
	if exit != 0 {
		t.Fatalf("exit=%d, stderr=%s", exit, stderr)
	}

	got := treePathsUnder(t, bareSrc, branch, ".tide/")
	want := []string{
		".tide/planning/milestone/m1/MILESTONE.md",
		".tide/planning/milestone/m1/children/phase-1.json",
		".tide/planning/milestone/m1/notes.md",
	}
	sort.Strings(want)
	if len(got) != len(want) {
		t.Fatalf("under .tide/ got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf(".tide path[%d] = %q, want %q (full got=%v)", i, got[i], want[i], got)
		}
	}
}

// TestStageEnvelopesByteFidelity (Task 2 Test 2): staged bytes are identical to
// source, including a *.md larger than 1 MiB — nothing trims or size-caps (D-03).
func TestStageEnvelopesByteFidelity(t *testing.T) {
	base := t.TempDir()
	bareSrc, _ := seedBareRepo(t, base)
	branch := perRunBranch(t, "stage-bytes")
	ws := setupWorkspace(t, bareSrc, branch)

	// >1 MiB deterministic body.
	big := bytes.Repeat([]byte("tidewater-0123456789\n"), (1<<20)/21+64)
	if len(big) <= 1<<20 {
		t.Fatalf("fixture not >1 MiB: %d", len(big))
	}
	writeEnvelopeFile(t, ws, "big", "PLAN.md", big)
	childJSON := []byte(`{"kind":"Task","name":"t-1","big":true}`)
	writeEnvelopeFile(t, ws, "big", "children/task-1.json", childJSON)

	cfg := pushConfig{
		Mode:           "push",
		Branch:         branch,
		CommitMessage:  "tide: stage big envelope",
		StageEnvelopes: "big:plan/big-plan",
		Workspace:      ws,
		ProjectUID:     "p-stage-bytes",
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if exit, stderr := stderrAndRun(t, ctx, cfg, "test-pat"); exit != 0 {
		t.Fatalf("exit=%d, stderr=%s", exit, stderr)
	}

	gotMD := treeFileBytes(t, bareSrc, branch, ".tide/planning/plan/big-plan/PLAN.md")
	if !bytes.Equal(gotMD, big) {
		t.Errorf("PLAN.md bytes differ: got %d bytes, want %d bytes", len(gotMD), len(big))
	}
	gotJSON := treeFileBytes(t, bareSrc, branch, ".tide/planning/plan/big-plan/children/task-1.json")
	if !bytes.Equal(gotJSON, childJSON) {
		t.Errorf("child json bytes differ: got %q want %q", gotJSON, childJSON)
	}
}

// TestStageEnvelopesMissingDirFailsLoud (Task 2 Test 3): a mapped envelope whose
// directory does not exist exits nonzero with reason artifact-stage-failed and
// pushes nothing (the per-run branch never appears on the remote).
func TestStageEnvelopesMissingDirFailsLoud(t *testing.T) {
	base := t.TempDir()
	bareSrc, _ := seedBareRepo(t, base)
	branch := perRunBranch(t, "stage-missing")
	ws := setupWorkspace(t, bareSrc, branch)

	cfg := pushConfig{
		Mode:           "push",
		Branch:         branch,
		CommitMessage:  "tide: stage missing envelope",
		StageEnvelopes: "does-not-exist:phase/p9",
		Workspace:      ws,
		ProjectUID:     "p-stage-missing",
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	exit, stderr := stderrAndRun(t, ctx, cfg, "test-pat")
	if exit == 0 {
		t.Fatalf("exit=0, want nonzero for missing envelope dir; stderr=%s", stderr)
	}
	pr := readPushEnvelope(t, ws, "p-stage-missing")
	if pr.Reason != "artifact-stage-failed" {
		t.Errorf("envelope.reason = %q, want %q", pr.Reason, "artifact-stage-failed")
	}
	bareRepo, err := gogit.PlainOpen(bareSrc)
	if err != nil {
		t.Fatalf("PlainOpen bareSrc: %v", err)
	}
	if _, refErr := bareRepo.Reference(plumbing.NewBranchReferenceName(branch), false); refErr == nil {
		t.Errorf("bare repo has branch %s despite missing envelope dir", branch)
	}
}

// TestStageEnvelopesIdempotentRestage (Task 2 Test 4): pushing the same
// cumulative map twice succeeds both times; the second push takes the clean-tree
// path and adds no second commit (remote commit count unchanged).
func TestStageEnvelopesIdempotentRestage(t *testing.T) {
	base := t.TempDir()
	bareSrc, _ := seedBareRepo(t, base)
	branch := perRunBranch(t, "stage-idem")
	ws := setupWorkspace(t, bareSrc, branch)

	writeEnvelopeFile(t, ws, "u2", "PHASE.md", []byte("# phase\n"))
	writeEnvelopeFile(t, ws, "u2", "children/plan-1.json", []byte(`{"kind":"Plan"}`))

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	cfg := pushConfig{
		Mode:           "push",
		Branch:         branch,
		CommitMessage:  "tide: stage phase envelope",
		StageEnvelopes: "u2:phase/p1",
		Workspace:      ws,
		ProjectUID:     "p-stage-idem",
	}

	if exit, stderr := stderrAndRun(t, ctx, cfg, "test-pat"); exit != 0 {
		t.Fatalf("first push exit=%d, stderr=%s", exit, stderr)
	}
	firstCount := commitCount(t, bareSrc, branch)
	pr1 := readPushEnvelope(t, ws, "p-stage-idem")

	// Second push: byte-identical cumulative map. lastPushedSHA anchors the lease.
	cfg.LastPushedSHA = pr1.HeadSHA
	exit2, stderr2 := stderrAndRun(t, ctx, cfg, "test-pat")
	if exit2 != 0 {
		t.Fatalf("second push exit=%d, stderr=%s", exit2, stderr2)
	}
	if !bytes.Contains(stderr2, []byte("clean working tree")) {
		t.Errorf("second push did not take the clean-tree path; stderr=%s", stderr2)
	}
	secondCount := commitCount(t, bareSrc, branch)
	if secondCount != firstCount {
		t.Errorf("commit count changed after idempotent restage: first=%d second=%d", firstCount, secondCount)
	}
}

func TestRunPushModeWritesExactBoundaryCommitMessage(t *testing.T) {
	// Ensure the agent env is unset so the compiled default identity applies.
	t.Setenv("TIDE_AGENT_NAME", "")
	t.Setenv("TIDE_AGENT_EMAIL", "")

	base := t.TempDir()
	bareSrc, _ := seedBareRepo(t, base)
	bareRepo, err := gogit.PlainOpen(bareSrc)
	if err != nil {
		t.Fatalf("PlainOpen: %v", err)
	}
	branch := perRunBranch(t, "w11")

	ws := setupWorkspace(t, bareSrc, branch)
	writeArtifact(t, ws, "artifacts/M-001/P-003/L-005/PLAN.md", "# plan body\n")
	writeArtifact(t, ws, "artifacts/M-001/P-003/L-005/SUMMARY.md", "# summary body\n")

	const msg = "tide: plan 03-foo authored + executed"
	cfg := pushConfig{
		Mode:          "push",
		Branch:        branch,
		LastPushedSHA: "",
		CommitMessage: msg,
		ArtifactPaths: []string{
			"artifacts/M-001/P-003/L-005/PLAN.md",
			"artifacts/M-001/P-003/L-005/SUMMARY.md",
		},
		Workspace:  ws,
		ProjectUID: "p7",
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	exit, stderr := stderrAndRun(t, ctx, cfg, "pat-w11")
	if exit != 0 {
		t.Fatalf("exit=%d, stderr=%s", exit, stderr)
	}
	pr := readPushEnvelope(t, ws, "p7")
	if pr.HeadSHA == "" {
		t.Fatal("envelope.headSHA empty")
	}

	// Remote branch must show the exact W11 message string.
	ref, err := bareRepo.Reference(plumbing.NewBranchReferenceName(branch), false)
	if err != nil {
		t.Fatalf("Reference: %v", err)
	}
	commit, err := bareRepo.CommitObject(ref.Hash())
	if err != nil {
		t.Fatalf("CommitObject: %v", err)
	}
	if !strings.Contains(commit.Message, msg) {
		t.Errorf("remote commit message = %q, must contain %q", commit.Message, msg)
	}
	// Author is the env-sourced TIDE agent signature; with the agent env vars
	// unset it resolves to the compiled default. go-git copies Author to
	// Committer when Committer is unset, so both must match (Pitfall 8).
	if commit.Author.Name != "TIDE Agent" {
		t.Errorf("commit author name = %q, want %q", commit.Author.Name, "TIDE Agent")
	}
	if commit.Author.Email != "tide-agent@tideproject.k8s" {
		t.Errorf("commit author email = %q, want %q", commit.Author.Email, "tide-agent@tideproject.k8s")
	}
	if commit.Committer.Name != commit.Author.Name || commit.Committer.Email != commit.Author.Email {
		t.Errorf("committer %q <%s> must equal author %q <%s>",
			commit.Committer.Name, commit.Committer.Email, commit.Author.Name, commit.Author.Email)
	}
}

// TestMakeWorkspaceGroupShared verifies the permission-bit logic that lets the
// executor (a different uid sharing the PVC fsGroup) write into the workspace
// subtrees the clone Job created: every directory becomes group-writable +
// setgid, and every file becomes group-writable. The chgrp to sharedFSGroup is
// best-effort and environment-dependent (the test runner is rarely a member of
// gid 1000), so the group ownership itself is not asserted here.
func TestMakeWorkspaceGroupShared(t *testing.T) {
	root := t.TempDir()
	// A nested tree mimicking repo.git/refs/heads + worktrees/ + a file.
	sub := filepath.Join(root, "repo.git", "refs", "heads")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatalf("mkdir tree: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "worktrees"), 0o755); err != nil {
		t.Fatalf("mkdir worktrees: %v", err)
	}
	f := filepath.Join(sub, "main")
	if err := os.WriteFile(f, []byte("ref"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	if err := makeWorkspaceGroupShared(root); err != nil {
		t.Fatalf("makeWorkspaceGroupShared: %v", err)
	}

	// Directories: group write (0o020), group exec (0o010), and setgid.
	for _, d := range []string{root, filepath.Join(root, "repo.git"), filepath.Join(root, "worktrees"), sub} {
		fi, err := os.Stat(d)
		if err != nil {
			t.Fatalf("stat %s: %v", d, err)
		}
		m := fi.Mode()
		if m.Perm()&0o020 == 0 {
			t.Errorf("dir %s not group-writable: %v", d, m.Perm())
		}
		if m.Perm()&0o010 == 0 {
			t.Errorf("dir %s not group-traversable: %v", d, m.Perm())
		}
		// setgid (group inheritance for new entries) is the defense-in-depth
		// layer; the critical bit for unblocking the executor is group-write +
		// the chgrp. BSD chmod (macOS dev boxes) silently drops S_ISGID when the
		// caller is not a member of the file's group, so only assert it on Linux
		// — the cluster runtime where the clone Job (uid 65532) is a member of
		// the fsGroup and setgid sticks.
		if runtime.GOOS == "linux" && m&os.ModeSetgid == 0 {
			t.Errorf("dir %s missing setgid: %v", d, m)
		}
	}

	// File: group write.
	fi, err := os.Stat(f)
	if err != nil {
		t.Fatalf("stat file: %v", err)
	}
	if fi.Mode().Perm()&0o020 == 0 {
		t.Errorf("file %s not group-writable: %v", f, fi.Mode().Perm())
	}
}

// TestRunPushIntegrationOnlyNoArtifacts covers the per-wave integration job:
// --integrate-task-branches is set but there are NO planner artifacts. The merge
// must advance the LOCAL run branch and the run MUST exit 0 — it must NOT fall
// through to the boundary commit, which would fail with "cannot create empty
// commit: clean working tree" (the medium DoD per-wave integration failure).
func TestRunPushIntegrationOnlyNoArtifacts(t *testing.T) {
	base := t.TempDir()
	bareSrc, _ := seedBareRepo(t, base)
	branch := perRunBranch(t, "integ-only")
	ws := t.TempDir()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	cloneCfg := pushConfig{Mode: "clone", RepoURL: "file://" + bareSrc, Workspace: ws, RunBranch: branch}
	if exit, stderr := stderrAndRun(t, ctx, cloneCfg, ""); exit != 0 {
		t.Fatalf("clone phase exit=%d stderr=%s", exit, stderr)
	}

	taskBranch := "tide/wt-uidA"
	createTaskBranchWithFile(t, filepath.Join(ws, "repo.git"), taskBranch, "taskA.txt", "task A content")

	// Per-wave integration Job: --integration-only with the branch set.
	pushCfg := pushConfig{
		Mode:                  "push",
		Branch:                branch,
		CommitMessage:         "tide: integrate wave 1",
		ArtifactPaths:         nil,
		IntegrateTaskBranches: []string{taskBranch},
		IntegrationOnly:       true,
		Workspace:             ws,
		ProjectUID:            "p-integ-only",
	}
	exit, stderr := stderrAndRun(t, ctx, pushCfg, "test-pat")
	if exit != 0 {
		t.Fatalf("integration-only push exit=%d (want 0); stderr=%s", exit, stderr)
	}

	// The merge must have advanced the LOCAL run branch (workspace repo.git),
	// making the task file reachable from the run branch tip.
	localRepo, err := gogit.PlainOpen(filepath.Join(ws, "repo.git"))
	if err != nil {
		t.Fatalf("PlainOpen local repo.git: %v", err)
	}
	ref, err := localRepo.Reference(plumbing.NewBranchReferenceName(branch), false)
	if err != nil {
		t.Fatalf("local run branch ref %q: %v", branch, err)
	}
	commit, err := localRepo.CommitObject(ref.Hash())
	if err != nil {
		t.Fatalf("CommitObject: %v", err)
	}
	if _, err := commit.File("taskA.txt"); err != nil {
		t.Errorf("taskA.txt not reachable from local run branch after integration-only merge: %v", err)
	}
}

// TestRunPushBoundaryCleanTreePushesIntegratedBranch is the third medium-DoD
// defect: a level-boundary push (phase/milestone/project) carries NO planner
// artifacts and NO --integrate-task-branches — the per-wave integration job has
// already merged every task branch into the run branch, leaving a clean working
// tree. The boundary push must NOT attempt an empty commit (which fails with
// "cannot create empty commit: clean working tree" and never reaches the push);
// it must skip the commit and STILL push the already-integrated run branch to
// the remote so the merged work leaves the cluster. Mirrors commit 8e57348's
// "no empty commit" handling at the boundary-push path.
func TestRunPushBoundaryCleanTreePushesIntegratedBranch(t *testing.T) {
	base := t.TempDir()
	bareSrc, _ := seedBareRepo(t, base)
	branch := perRunBranch(t, "boundary-clean")
	ws := t.TempDir()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Step 1: clone + provision the run worktree (origin resolves to bareSrc).
	cloneCfg := pushConfig{Mode: "clone", RepoURL: "file://" + bareSrc, Workspace: ws, RunBranch: branch}
	if exit, stderr := stderrAndRun(t, ctx, cloneCfg, ""); exit != 0 {
		t.Fatalf("clone phase exit=%d stderr=%s", exit, stderr)
	}

	// Step 2: a task branch with a new file, then a per-wave integration push
	// (integrate set, NO artifacts) that merges it into the run branch LOCALLY
	// and does not push — exactly the wave-internal contract.
	taskBranch := "tide/wt-uidB"
	createTaskBranchWithFile(t, filepath.Join(ws, "repo.git"), taskBranch, "taskB.txt", "task B content")
	waveCfg := pushConfig{
		Mode:                  "push",
		Branch:                branch,
		CommitMessage:         "tide: integrate wave 1",
		IntegrateTaskBranches: []string{taskBranch},
		IntegrationOnly:       true,
		Workspace:             ws,
		ProjectUID:            "p-boundary-clean",
	}
	if exit, stderr := stderrAndRun(t, ctx, waveCfg, "test-pat"); exit != 0 {
		t.Fatalf("wave integration push exit=%d stderr=%s", exit, stderr)
	}

	// Precondition: the wave integration must NOT have pushed — the remote run
	// branch should not exist yet (push happens only at the boundary).
	bareRepo, err := gogit.PlainOpen(bareSrc)
	if err != nil {
		t.Fatalf("PlainOpen bareSrc: %v", err)
	}
	if _, err := bareRepo.Reference(plumbing.NewBranchReferenceName(branch), false); err == nil {
		t.Fatalf("run branch %q unexpectedly present on remote after integration-only push", branch)
	}

	// Step 3: the level boundary push — NO artifacts, NO integrate. The working
	// tree is clean (merge already advanced HEAD). This is the path that
	// previously failed "cannot create empty commit: clean working tree".
	boundaryCfg := pushConfig{
		Mode:          "push",
		Branch:        branch,
		CommitMessage: "tide: phase phase-01-implement-formattednow authored",
		Workspace:     ws,
		ProjectUID:    "p-boundary-clean",
	}
	exit, stderr := stderrAndRun(t, ctx, boundaryCfg, "test-pat")
	if exit != 0 {
		t.Fatalf("boundary push on clean tree exit=%d (want 0); stderr=%s", exit, stderr)
	}
	if bytes.Contains(stderr, []byte("cannot create empty commit")) {
		t.Errorf("boundary push attempted an empty commit: %s", stderr)
	}
	// (d) SC2 gap check: a "nothing to commit" skip message must be emitted so the
	// operator can distinguish a successful clean-tree push from a silent no-op.
	if !bytes.Contains(stderr, []byte("clean working tree")) {
		t.Errorf("boundary push on clean tree did not emit 'clean working tree' skip message; stderr=%s", stderr)
	}

	// The merged run branch must now exist on the remote and carry the task file.
	bareRepo, err = gogit.PlainOpen(bareSrc)
	if err != nil {
		t.Fatalf("re-open bareSrc: %v", err)
	}
	ref, err := bareRepo.Reference(plumbing.NewBranchReferenceName(branch), false)
	if err != nil {
		t.Fatalf("run branch %q not pushed to remote by boundary push: %v", branch, err)
	}
	commit, err := bareRepo.CommitObject(ref.Hash())
	if err != nil {
		t.Fatalf("CommitObject: %v", err)
	}
	if _, err := commit.File("taskB.txt"); err != nil {
		t.Errorf("taskB.txt not present on remote run branch after boundary push: %v", err)
	}

	// The push envelope must report success with a non-empty HEAD SHA.
	pr := readPushEnvelope(t, ws, "p-boundary-clean")
	if pr.ExitCode != 0 || pr.Reason != "" {
		t.Errorf("push envelope exitCode=%d reason=%q, want 0/empty", pr.ExitCode, pr.Reason)
	}
	if pr.HeadSHA == "" {
		t.Error("push envelope HeadSHA empty on clean-tree boundary push")
	}
	if pr.HeadSHA != ref.Hash().String() {
		t.Errorf("envelope HeadSHA=%s but remote ref=%s", pr.HeadSHA, ref.Hash())
	}
}

// TestRunPushBoundaryWithBranchesStillPushes pins the boundary-push contract
// under the D-03/D-07 cumulative branch set: every controller-dispatched
// boundary push now carries --integrate-task-branches (the cumulative
// Succeeded set) and never --artifact-paths. Such a Job must integrate,
// verify, and STILL push the run branch to the remote — only an explicit
// --integration-only Job (the per-wave integration case) may exit without
// pushing. Regression: the early exit previously keyed on "branches set, no
// artifacts", which swallowed every post-task boundary push.
func TestRunPushBoundaryWithBranchesStillPushes(t *testing.T) {
	base := t.TempDir()
	bareSrc, _ := seedBareRepo(t, base)
	branch := perRunBranch(t, "boundary-branches")
	ws := t.TempDir()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	cloneCfg := pushConfig{Mode: "clone", RepoURL: "file://" + bareSrc, Workspace: ws, RunBranch: branch}
	if exit, stderr := stderrAndRun(t, ctx, cloneCfg, ""); exit != 0 {
		t.Fatalf("clone phase exit=%d stderr=%s", exit, stderr)
	}

	taskBranch := "tide/wt-uidC"
	createTaskBranchWithFile(t, filepath.Join(ws, "repo.git"), taskBranch, "taskC.txt", "task C content")

	// The controller's boundary-push shape: cumulative branches, no artifacts,
	// NOT integration-only.
	boundaryCfg := pushConfig{
		Mode:                  "push",
		Branch:                branch,
		CommitMessage:         "tide: milestone milestone-01 authored + executed",
		IntegrateTaskBranches: []string{taskBranch},
		Workspace:             ws,
		ProjectUID:            "p-boundary-branches",
	}
	exit, stderr := stderrAndRun(t, ctx, boundaryCfg, "test-pat")
	if exit != 0 {
		t.Fatalf("boundary push with branches exit=%d (want 0); stderr=%s", exit, stderr)
	}

	// The run branch must exist on the remote and carry the merged task file.
	bareRepo, err := gogit.PlainOpen(bareSrc)
	if err != nil {
		t.Fatalf("PlainOpen bareSrc: %v", err)
	}
	ref, err := bareRepo.Reference(plumbing.NewBranchReferenceName(branch), false)
	if err != nil {
		t.Fatalf("run branch %q not pushed to remote by boundary push carrying branches: %v", branch, err)
	}
	commit, err := bareRepo.CommitObject(ref.Hash())
	if err != nil {
		t.Fatalf("CommitObject: %v", err)
	}
	if _, err := commit.File("taskC.txt"); err != nil {
		t.Errorf("taskC.txt not present on remote run branch: %v", err)
	}

	// Envelope must be a real push success: non-empty HeadSHA matching the
	// remote tip (an empty HeadSHA would wipe the D-B6 lease fence upstream).
	pr := readPushEnvelope(t, ws, "p-boundary-branches")
	if pr.ExitCode != 0 || pr.Reason != "" {
		t.Errorf("push envelope exitCode=%d reason=%q, want 0/empty", pr.ExitCode, pr.Reason)
	}
	if pr.HeadSHA != ref.Hash().String() {
		t.Errorf("envelope HeadSHA=%q, want remote tip %s", pr.HeadSHA, ref.Hash())
	}
}

// ---------- Phase 34 (D-06/INTEG-03): verifyIntegrationComplete unit tests ----------
//
// verifyIntegrationComplete is the pure predicate the D-06 verify gate is
// built on. Testing it directly (rather than only through the full run(cfg)
// integrate->verify->push flow) is deliberate: within a single Job execution
// under the shared flock, a *successful* merge of every branch in
// cfg.IntegrateTaskBranches makes each one an ancestor of the run branch by
// git's own merge semantics — there is no way to reproduce a genuine post-
// merge "miss" through the public run(cfg) entrypoint without fault-
// injecting the merge step itself. verifyIntegrationComplete is exactly the
// seam designed to catch that class of bug/race, so it is unit-tested here
// directly against a real bare repo (no mocks).

// verifyFixtureRepo builds a bare repo with a run branch and a task branch
// that is NOT merged into it, returning (bareDir, runBranch, taskBranch).
func verifyFixtureRepo(t *testing.T) (bareDir, runBranch, taskBranch string) {
	t.Helper()
	base := t.TempDir()
	bareDir, _ = seedBareRepo(t, base)
	runBranch = "tide/run-verify-1747200000"

	repo, err := gogit.PlainOpen(bareDir)
	if err != nil {
		t.Fatalf("PlainOpen bareDir: %v", err)
	}
	head, err := repo.Head()
	if err != nil {
		t.Fatalf("Head: %v", err)
	}
	// Run branch ref at the seed commit.
	runRef := plumbing.NewBranchReferenceName(runBranch)
	if err := repo.Storer.SetReference(plumbing.NewHashReference(runRef, head.Hash())); err != nil {
		t.Fatalf("SetReference run branch: %v", err)
	}

	// Task branch: a clone that adds a commit, pushed back — never merged
	// into runBranch.
	taskBranch = "tide/wt-unmerged"
	createTaskBranchWithFile(t, bareDir, taskBranch, "unmerged.txt", "never merged\n")
	return bareDir, runBranch, taskBranch
}

func TestVerifyIntegrationCompleteDetectsMiss(t *testing.T) {
	bareDir, runBranch, taskBranch := verifyFixtureRepo(t)

	result := verifyIntegrationComplete(bareDir, runBranch, []string{taskBranch})
	if result.infraErr != nil {
		t.Fatalf("unexpected infra error: %v", result.infraErr)
	}
	if len(result.branches) != 1 || result.branches[0] != taskBranch {
		t.Errorf("branches = %v, want exactly [%s]", result.branches, taskBranch)
	}
	if result.total != 1 {
		t.Errorf("total = %d, want 1", result.total)
	}
}

func TestVerifyIntegrationCompletePassesWhenMerged(t *testing.T) {
	bareDir, runBranch, taskBranch := verifyFixtureRepo(t)

	if err := pkggitIntegrateForTest(bareDir, runBranch, []string{taskBranch}); err != nil {
		t.Fatalf("integrate: %v", err)
	}

	result := verifyIntegrationComplete(bareDir, runBranch, []string{taskBranch})
	if result.infraErr != nil {
		t.Fatalf("unexpected infra error: %v", result.infraErr)
	}
	if len(result.branches) != 0 {
		t.Errorf("branches = %v, want none (already merged)", result.branches)
	}
}

func TestVerifyIntegrationCompleteEmptyDiffPassesWithoutSpecialCase(t *testing.T) {
	base := t.TempDir()
	bareDir, _ := seedBareRepo(t, base)
	runBranch := "tide/run-verify-emptydiff"

	repo, err := gogit.PlainOpen(bareDir)
	if err != nil {
		t.Fatalf("PlainOpen: %v", err)
	}
	head, err := repo.Head()
	if err != nil {
		t.Fatalf("Head: %v", err)
	}
	runRef := plumbing.NewBranchReferenceName(runBranch)
	if err := repo.Storer.SetReference(plumbing.NewHashReference(runRef, head.Hash())); err != nil {
		t.Fatalf("SetReference: %v", err)
	}
	// A "task branch" whose tip IS the run branch's current tip (no unique
	// commits — the empty-diff task case, INTEG-03 clarification).
	emptyDiffBranch := "tide/wt-emptydiff"
	branchRef := plumbing.NewBranchReferenceName(emptyDiffBranch)
	if err := repo.Storer.SetReference(plumbing.NewHashReference(branchRef, head.Hash())); err != nil {
		t.Fatalf("SetReference empty-diff branch: %v", err)
	}

	result := verifyIntegrationComplete(bareDir, runBranch, []string{emptyDiffBranch})
	if result.infraErr != nil {
		t.Fatalf("unexpected infra error: %v", result.infraErr)
	}
	if len(result.branches) != 0 {
		t.Errorf("empty-diff branch flagged as missing (should pass naturally, no special case): %v", result.branches)
	}
}

func TestVerifyIntegrationCompleteEmptyExpectedListVacuouslyPasses(t *testing.T) {
	bareDir, runBranch, _ := verifyFixtureRepo(t)
	result := verifyIntegrationComplete(bareDir, runBranch, nil)
	if result.infraErr != nil {
		t.Fatalf("unexpected infra error: %v", result.infraErr)
	}
	if len(result.branches) != 0 {
		t.Errorf("branches = %v, want none for an empty expected list", result.branches)
	}
}

// pkggitIntegrateForTest merges taskBranches into runBranch via the real
// pkg/git.IntegrateTaskBranches, mirroring what runPush does internally.
func pkggitIntegrateForTest(bareDir, runBranch string, taskBranches []string) error {
	return pkggit.IntegrateTaskBranches(bareDir, runBranch, taskBranches)
}

// ---------- Phase 34 (D-12): truncation ----------

func TestVerifyIntegrationCompleteTruncationAtEnvelopeWrite(t *testing.T) {
	base := t.TempDir()
	bareDir, _ := seedBareRepo(t, base)
	runBranch := "tide/run-verify-truncate"

	repo, err := gogit.PlainOpen(bareDir)
	if err != nil {
		t.Fatalf("PlainOpen: %v", err)
	}
	head, err := repo.Head()
	if err != nil {
		t.Fatalf("Head: %v", err)
	}
	runRef := plumbing.NewBranchReferenceName(runBranch)
	if err := repo.Storer.SetReference(plumbing.NewHashReference(runRef, head.Hash())); err != nil {
		t.Fatalf("SetReference: %v", err)
	}

	// 12 unmerged task branches — more than missingBranchesLimit (10).
	branches := make([]string, 0, 12)
	for i := range 12 {
		name := "tide/wt-trunc-" + string(rune('a'+i))
		createTaskBranchWithFile(t, bareDir, name, "trunc-"+string(rune('a'+i))+".txt", "content")
		branches = append(branches, name)
	}

	result := verifyIntegrationComplete(bareDir, runBranch, branches)
	if result.infraErr != nil {
		t.Fatalf("unexpected infra error: %v", result.infraErr)
	}
	if result.total != 12 {
		t.Fatalf("result.total = %d, want 12 (untruncated count)", result.total)
	}

	// writePushEnvelope applies the D-12 truncation; verify it end-to-end via
	// the actual envelope written to the PVC path.
	ws := t.TempDir()
	if err := os.MkdirAll(filepath.Join(ws, "envelopes", "push"), 0o755); err != nil {
		t.Fatalf("mkdir envelopes/push: %v", err)
	}
	cfg := pushConfig{Branch: runBranch, Workspace: ws, ProjectUID: "p-trunc"}
	writePushEnvelope(cfg, "", exitIntegrationMiss, "integration-incomplete", result.branches, result.total, "")

	pr := readPushEnvelope(t, ws, "p-trunc")
	if pr.ExitCode != exitIntegrationMiss {
		t.Errorf("envelope.ExitCode = %d, want %d", pr.ExitCode, exitIntegrationMiss)
	}
	if pr.Reason != "integration-incomplete" {
		t.Errorf("envelope.Reason = %q, want integration-incomplete", pr.Reason)
	}
	if len(pr.MissingBranches) != missingBranchesLimit {
		t.Errorf("envelope.MissingBranches len = %d, want %d (truncated)", len(pr.MissingBranches), missingBranchesLimit)
	}
	if pr.MissingTotal != 12 {
		t.Errorf("envelope.MissingTotal = %d, want 12 (untruncated)", pr.MissingTotal)
	}
}

// ---------- Phase 34 (D-02): flock ----------

func TestRunPushHoldsFlockAcrossIntegrateVerifyPush(t *testing.T) {
	base := t.TempDir()
	bareSrc, _ := seedBareRepo(t, base)
	branch := perRunBranch(t, "flock")

	ws := setupWorkspace(t, bareSrc, branch)
	writeArtifact(t, ws, "artifacts/plan.md", "# plan\n")

	cfg := pushConfig{
		Mode:          "push",
		Branch:        branch,
		CommitMessage: "tide: flock test",
		ArtifactPaths: []string{"artifacts/plan.md"},
		Workspace:     ws,
		ProjectUID:    "p-flock",
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	exit, stderr := stderrAndRun(t, ctx, cfg, "pat-flock")
	if exit != 0 {
		t.Fatalf("exit=%d stderr=%s", exit, stderr)
	}

	lockPath := filepath.Join(ws, "repo.git", "tide-integrate.lock")
	if _, err := os.Stat(lockPath); err != nil {
		t.Errorf("lockfile %s not created: %v", lockPath, err)
	}
}

// ---------- Phase 34 (D-09): conflict classification + wave-success envelope ----------

func TestRunPushIntegrateConflictReturnsMergeConflictEnvelope(t *testing.T) {
	base := t.TempDir()
	bareSrc, _ := seedBareRepo(t, base)
	branch := perRunBranch(t, "conflict")
	ws := t.TempDir()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	cloneCfg := pushConfig{Mode: "clone", RepoURL: "file://" + bareSrc, Workspace: ws, RunBranch: branch}
	if exit, stderr := stderrAndRun(t, ctx, cloneCfg, ""); exit != 0 {
		t.Fatalf("clone phase exit=%d stderr=%s", exit, stderr)
	}

	// Two branches that both modify the same file/line — a genuine conflict.
	branchA := "tide/wt-conflict-a"
	branchB := "tide/wt-conflict-b"
	createTaskBranchWithFile(t, filepath.Join(ws, "repo.git"), branchA, "conflict.txt", "side A\n")
	createTaskBranchWithFile(t, filepath.Join(ws, "repo.git"), branchB, "conflict.txt", "side B\n")

	pushCfg := pushConfig{
		Mode:                  "push",
		Branch:                branch,
		CommitMessage:         "tide: integrate conflict test",
		IntegrateTaskBranches: []string{branchA, branchB},
		Workspace:             ws,
		ProjectUID:            "p-conflict",
	}
	exit, stderr := stderrAndRun(t, ctx, pushCfg, "test-pat")
	if exit != exitMergeConflict {
		t.Fatalf("exit=%d, want exitMergeConflict=%d; stderr=%s", exit, exitMergeConflict, stderr)
	}

	pr := readPushEnvelope(t, ws, "p-conflict")
	if pr.Reason != "merge-conflict" {
		t.Errorf("envelope.Reason = %q, want merge-conflict", pr.Reason)
	}
	if pr.ConflictBranch != branchB {
		t.Errorf("envelope.ConflictBranch = %q, want %q", pr.ConflictBranch, branchB)
	}

	// Worktree must be left clean (no lingering MERGE_HEAD) — a follow-up Job
	// must be able to merge cleanly.
	integrationDir := filepath.Join(ws, "worktrees", "run-"+branch)
	if _, err := os.Stat(filepath.Join(integrationDir, ".git", "MERGE_HEAD")); err == nil {
		t.Errorf("MERGE_HEAD still present after conflict — worktree not cleaned up")
	}
}

func TestRunPushIntegrateTransientFailureUnchangedReason(t *testing.T) {
	base := t.TempDir()
	bareSrc, _ := seedBareRepo(t, base)
	branch := perRunBranch(t, "transient")
	ws := t.TempDir()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	cloneCfg := pushConfig{Mode: "clone", RepoURL: "file://" + bareSrc, Workspace: ws, RunBranch: branch}
	if exit, stderr := stderrAndRun(t, ctx, cloneCfg, ""); exit != 0 {
		t.Fatalf("clone phase exit=%d stderr=%s", exit, stderr)
	}

	pushCfg := pushConfig{
		Mode:                  "push",
		Branch:                branch,
		CommitMessage:         "tide: integrate transient test",
		IntegrateTaskBranches: []string{"tide/wt-does-not-exist"},
		Workspace:             ws,
		ProjectUID:            "p-transient",
	}
	exit, stderr := stderrAndRun(t, ctx, pushCfg, "test-pat")
	if exit != exitGenericFail {
		t.Fatalf("exit=%d, want exitGenericFail=%d; stderr=%s", exit, exitGenericFail, stderr)
	}
	pr := readPushEnvelope(t, ws, "p-transient")
	if pr.Reason != "integration-failed" {
		t.Errorf("envelope.Reason = %q, want integration-failed (unchanged, not misclassified as conflict/miss)", pr.Reason)
	}
}

func TestRunPushIntegrationOnlySuccessWritesEnvelope(t *testing.T) {
	// Pitfall 3: today the integration-only success path exits before ever
	// calling writePushEnvelope. Phase 34 adds a success envelope so wave-Job
	// outcomes are observable.
	base := t.TempDir()
	bareSrc, _ := seedBareRepo(t, base)
	branch := perRunBranch(t, "wave-envelope")
	ws := t.TempDir()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	cloneCfg := pushConfig{Mode: "clone", RepoURL: "file://" + bareSrc, Workspace: ws, RunBranch: branch}
	if exit, stderr := stderrAndRun(t, ctx, cloneCfg, ""); exit != 0 {
		t.Fatalf("clone phase exit=%d stderr=%s", exit, stderr)
	}
	taskBranch := "tide/wt-wave-envelope"
	createTaskBranchWithFile(t, filepath.Join(ws, "repo.git"), taskBranch, "wave.txt", "wave content")

	pushCfg := pushConfig{
		Mode:                  "push",
		Branch:                branch,
		CommitMessage:         "tide: integrate wave 1",
		IntegrateTaskBranches: []string{taskBranch},
		IntegrationOnly:       true,
		Workspace:             ws,
		ProjectUID:            "p-wave-envelope",
	}
	if exit, stderr := stderrAndRun(t, ctx, pushCfg, "test-pat"); exit != 0 {
		t.Fatalf("integration-only push exit=%d stderr=%s", exit, stderr)
	}

	pr := readPushEnvelope(t, ws, "p-wave-envelope")
	if pr.ExitCode != exitSuccess || pr.Reason != "" {
		t.Errorf("wave-success envelope = {exit:%d reason:%q}, want {0, \"\"}", pr.ExitCode, pr.Reason)
	}
}

func TestRunPushIntegrationOnlyEmptyCommitMessageSucceeds(t *testing.T) {
	// Cross-binary contract regression (PR #3 run 7): triggerWaveIntegrationJob
	// dispatches wave-integration Jobs with an EMPTY --commit-message —
	// integration-only mode never creates the boundary staging commit, so it
	// has no message to give. The W11 invariant rejected that as
	// missing-commit-message (exit 2 in milliseconds), killing every
	// wave-integration Job before the merge; the sibling test above always
	// passed a message, so the controller's real dispatch shape was untested.
	base := t.TempDir()
	bareSrc, _ := seedBareRepo(t, base)
	branch := perRunBranch(t, "wave-no-msg")
	ws := t.TempDir()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	cloneCfg := pushConfig{Mode: "clone", RepoURL: "file://" + bareSrc, Workspace: ws, RunBranch: branch}
	if exit, stderr := stderrAndRun(t, ctx, cloneCfg, ""); exit != 0 {
		t.Fatalf("clone phase exit=%d stderr=%s", exit, stderr)
	}
	taskBranch := "tide/wt-wave-no-msg"
	createTaskBranchWithFile(t, filepath.Join(ws, "repo.git"), taskBranch, "wave.txt", "wave content")

	// The controller's EXACT dispatch shape: no CommitMessage.
	pushCfg := pushConfig{
		Mode:                  "push",
		Branch:                branch,
		IntegrateTaskBranches: []string{taskBranch},
		IntegrationOnly:       true,
		Workspace:             ws,
		ProjectUID:            "p-wave-no-msg",
	}
	if exit, stderr := stderrAndRun(t, ctx, pushCfg, "test-pat"); exit != 0 {
		t.Fatalf("integration-only push with empty commit message exit=%d stderr=%s", exit, stderr)
	}

	pr := readPushEnvelope(t, ws, "p-wave-no-msg")
	if pr.ExitCode != exitSuccess || pr.Reason != "" {
		t.Errorf("wave-success envelope = {exit:%d reason:%q}, want {0, \"\"}", pr.ExitCode, pr.Reason)
	}
}

// ---------- Phase 35 (BASE-01/BASE-02): clone-mode envelope + --base-ref ----------

// readCloneEnvelope reads <workspace>/envelopes/clone/<project-uid>.json,
// returning the parsed struct and the raw bytes (for exact-key-spelling checks).
func readCloneEnvelope(t *testing.T, workspace, projectUID string) (pushResult, []byte) {
	t.Helper()
	path := filepath.Join(workspace, "envelopes", "clone", projectUID+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read clone envelope %s: %v", path, err)
	}
	var pr pushResult
	if err := json.Unmarshal(data, &pr); err != nil {
		t.Fatalf("unmarshal clone envelope: %v", err)
	}
	return pr, data
}

// seedSourceWithFeature builds a NON-bare source repo with a default branch and
// a non-default branch feature/hotfix (distinct tip). Cloning it via file://
// yields a production-shaped bare repo where feature/hotfix lives only under
// refs/remotes/origin/* — the layout runClone resolves --base-ref against.
func seedSourceWithFeature(t *testing.T) (workDir string, defaultHead, featureHead plumbing.Hash) {
	t.Helper()
	base := t.TempDir()
	workDir = filepath.Join(base, "src-work")

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

	defaultHead = mk("a.txt", "1\n", "commit 1")
	headSym, err := repo.Reference(plumbing.HEAD, false)
	if err != nil {
		t.Fatalf("Reference HEAD: %v", err)
	}
	defaultBranch := headSym.Target()

	if err := wt.Checkout(&gogit.CheckoutOptions{
		Branch: plumbing.NewBranchReferenceName("feature/hotfix"),
		Create: true,
	}); err != nil {
		t.Fatalf("Checkout feature/hotfix: %v", err)
	}
	featureHead = mk("b.txt", "feature\n", "feature commit")

	if err := wt.Checkout(&gogit.CheckoutOptions{Branch: defaultBranch}); err != nil {
		t.Fatalf("Checkout back to default: %v", err)
	}
	return workDir, defaultHead, featureHead
}

// TestRunCloneWritesSuccessEnvelopeDefaultHead covers D-11: clone with
// --run-branch and NO --base-ref writes a CloneResult envelope whose baseSHA is
// the source HEAD hash.
func TestRunCloneWritesSuccessEnvelopeDefaultHead(t *testing.T) {
	base := t.TempDir()
	bareSrc, srcHead := seedBareRepo(t, base)
	ws := t.TempDir()

	cfg := pushConfig{
		Mode:       "clone",
		RepoURL:    "file://" + bareSrc,
		Workspace:  ws,
		RunBranch:  "tide/run-default-1",
		ProjectUID: "c-default",
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	exit, stderr := stderrAndRun(t, ctx, cfg, "")
	if exit != 0 {
		t.Fatalf("clone exit=%d stderr=%s", exit, stderr)
	}

	pr, raw := readCloneEnvelope(t, ws, "c-default")
	if pr.Kind != "CloneResult" {
		t.Errorf("envelope.kind = %q, want CloneResult", pr.Kind)
	}
	if pr.ExitCode != 0 {
		t.Errorf("envelope.exitCode = %d, want 0", pr.ExitCode)
	}
	if pr.Reason != "" {
		t.Errorf("envelope.reason = %q, want empty", pr.Reason)
	}
	if pr.BaseSHA != srcHead.String() {
		t.Errorf("envelope.baseSHA = %q, want source HEAD %s", pr.BaseSHA, srcHead)
	}
	// Exact key spelling locked to the objective contract.
	for _, key := range []string{`"kind"`, `"reason"`, `"baseSHA"`, `"exitCode"`} {
		if !bytes.Contains(raw, []byte(key)) {
			t.Errorf("envelope JSON missing key %s: %s", key, raw)
		}
	}
}

// TestRunCloneWritesSuccessEnvelopeFeatureBranch covers the feature-branch
// success path: run branch tip AND envelope baseSHA equal the feature tip.
func TestRunCloneWritesSuccessEnvelopeFeatureBranch(t *testing.T) {
	workDir, _, featureHead := seedSourceWithFeature(t)
	ws := t.TempDir()

	cfg := pushConfig{
		Mode:       "clone",
		RepoURL:    "file://" + workDir,
		Workspace:  ws,
		RunBranch:  "tide/run-feature-1",
		BaseRef:    "feature/hotfix",
		ProjectUID: "c-feature",
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	exit, stderr := stderrAndRun(t, ctx, cfg, "")
	if exit != 0 {
		t.Fatalf("clone exit=%d stderr=%s", exit, stderr)
	}

	pr, raw := readCloneEnvelope(t, ws, "c-feature")
	if pr.BaseSHA != featureHead.String() {
		t.Errorf("envelope.baseSHA = %q, want feature tip %s", pr.BaseSHA, featureHead)
	}
	if pr.BaseRef != "feature/hotfix" {
		t.Errorf("envelope.baseRef = %q, want feature/hotfix", pr.BaseRef)
	}
	if !bytes.Contains(raw, []byte(`"baseRef"`)) {
		t.Errorf("envelope JSON missing baseRef key: %s", raw)
	}

	// Run branch tip in the cloned bare repo must equal the feature tip.
	bareRepo, err := gogit.PlainOpen(filepath.Join(ws, "repo.git"))
	if err != nil {
		t.Fatalf("PlainOpen repo.git: %v", err)
	}
	ref, err := bareRepo.Reference(plumbing.NewBranchReferenceName("tide/run-feature-1"), false)
	if err != nil {
		t.Fatalf("run branch ref: %v", err)
	}
	if ref.Hash() != featureHead {
		t.Errorf("run branch tip = %s, want feature tip %s", ref.Hash(), featureHead)
	}
}

// TestRunCloneWritesFailureEnvelopeUnresolvable covers BASE-02/D-05: an
// unresolvable --base-ref exits 2 with reason baseref-unresolvable and no
// baseSHA, and does not create the run branch.
func TestRunCloneWritesFailureEnvelopeUnresolvable(t *testing.T) {
	base := t.TempDir()
	bareSrc, _ := seedBareRepo(t, base)
	ws := t.TempDir()

	cfg := pushConfig{
		Mode:       "clone",
		RepoURL:    "file://" + bareSrc,
		Workspace:  ws,
		RunBranch:  "tide/run-bad-1",
		BaseRef:    "no-such-ref",
		ProjectUID: "c-bad",
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	exit, stderr := stderrAndRun(t, ctx, cfg, "")
	if exit != 2 {
		t.Fatalf("clone exit=%d, want 2 (exitInvariant); stderr=%s", exit, stderr)
	}

	pr, _ := readCloneEnvelope(t, ws, "c-bad")
	if pr.Reason != "baseref-unresolvable" {
		t.Errorf("envelope.reason = %q, want baseref-unresolvable", pr.Reason)
	}
	if pr.ExitCode != 2 {
		t.Errorf("envelope.exitCode = %d, want 2", pr.ExitCode)
	}
	if pr.BaseRef != "no-such-ref" {
		t.Errorf("envelope.baseRef = %q, want no-such-ref", pr.BaseRef)
	}
	if pr.BaseSHA != "" {
		t.Errorf("envelope.baseSHA = %q, want empty on failure", pr.BaseSHA)
	}

	// The run branch must NOT have been created in the bare repo.
	bareRepo, err := gogit.PlainOpen(filepath.Join(ws, "repo.git"))
	if err != nil {
		t.Fatalf("PlainOpen repo.git: %v", err)
	}
	if _, err := bareRepo.Reference(plumbing.NewBranchReferenceName("tide/run-bad-1"), false); err == nil {
		t.Error("run branch was created despite unresolvable baseRef")
	}
}

// TestRunCloneNoRunBranchWritesSuccessEnvelope covers the legacy no-run-branch
// path: clone still exits 0 and writes a CloneResult success envelope with an
// empty baseSHA (the controller stamps empty — documented, not special-cased).
func TestRunCloneNoRunBranchWritesSuccessEnvelope(t *testing.T) {
	base := t.TempDir()
	bareSrc, _ := seedBareRepo(t, base)
	ws := t.TempDir()

	cfg := pushConfig{
		Mode:       "clone",
		RepoURL:    "file://" + bareSrc,
		Workspace:  ws,
		ProjectUID: "c-norb",
		// RunBranch intentionally absent.
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	exit, stderr := stderrAndRun(t, ctx, cfg, "")
	if exit != 0 {
		t.Fatalf("clone exit=%d stderr=%s", exit, stderr)
	}

	pr, _ := readCloneEnvelope(t, ws, "c-norb")
	if pr.Kind != "CloneResult" {
		t.Errorf("envelope.kind = %q, want CloneResult", pr.Kind)
	}
	if pr.ExitCode != 0 || pr.Reason != "" {
		t.Errorf("envelope = {exit:%d reason:%q}, want {0, \"\"}", pr.ExitCode, pr.Reason)
	}
	if pr.BaseSHA != "" {
		t.Errorf("envelope.baseSHA = %q, want empty on the no-run-branch path", pr.BaseSHA)
	}
}

// TestRunCloneNoProjectUIDSkipsPVCEnvelope verifies the PVC envelope is skipped
// (no error) when --project-uid is unset.
func TestRunCloneNoProjectUIDSkipsPVCEnvelope(t *testing.T) {
	base := t.TempDir()
	bareSrc, _ := seedBareRepo(t, base)
	ws := t.TempDir()

	cfg := pushConfig{
		Mode:      "clone",
		RepoURL:   "file://" + bareSrc,
		Workspace: ws,
		RunBranch: "tide/run-nouid-1",
		// ProjectUID intentionally empty.
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	exit, stderr := stderrAndRun(t, ctx, cfg, "")
	if exit != 0 {
		t.Fatalf("clone exit=%d stderr=%s", exit, stderr)
	}
	// No clone envelope dir/file should exist when ProjectUID is empty.
	if _, err := os.Stat(filepath.Join(ws, "envelopes", "clone")); err == nil {
		t.Error("clone envelope dir created despite empty ProjectUID")
	}
}
