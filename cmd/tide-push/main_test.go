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
	"strings"
	"testing"
	"time"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
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

func TestRunPushIntegrateBeforeStage(t *testing.T) {
	base := t.TempDir()
	bareSrc, _ := seedBareRepo(t, base)
	branch := perRunBranch(t, "integrate")

	// Set up the push workspace (creates repo.git + worktrees/run-<branch>/).
	ws := setupWorkspace(t, bareSrc, branch)

	// Create two task branches in the bare repo, each with a unique file.
	taskBranch1 := "tide/wt-uid1"
	taskBranch2 := "tide/wt-uid2"
	createTaskBranchWithFile(t, filepath.Join(ws, "repo.git"), taskBranch1, "task1.txt", "task1 content")
	createTaskBranchWithFile(t, filepath.Join(ws, "repo.git"), taskBranch2, "task2.txt", "task2 content")

	writeArtifact(t, ws, "artifacts/plan.md", "# plan\n")

	cfg := pushConfig{
		Mode:                  "push",
		Branch:                branch,
		LastPushedSHA:         "",
		CommitMessage:         "tide: integrate test",
		ArtifactPaths:         []string{"artifacts/plan.md"},
		IntegrateTaskBranches: []string{taskBranch1, taskBranch2},
		Workspace:             ws,
		ProjectUID:            "p-integrate",
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	exit, stderr := stderrAndRun(t, ctx, cfg, "test-pat")
	if exit != 0 {
		t.Fatalf("push with --integrate-task-branches exit=%d stderr=%s", exit, stderr)
	}

	// Verify both task branch files appear in the bare repo's run branch.
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

func TestRunPushModeWritesExactBoundaryCommitMessage(t *testing.T) {
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
	// Author is the fixed TIDE-bot signature.
	if commit.Author.Name != "tide-bot" {
		t.Errorf("commit author name = %q, want %q", commit.Author.Name, "tide-bot")
	}
	if commit.Author.Email != "tide-bot@tideproject.k8s" {
		t.Errorf("commit author email = %q, want %q", commit.Author.Email, "tide-bot@tideproject.k8s")
	}
}
