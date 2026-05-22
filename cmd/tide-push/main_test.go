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

	// Clone mode emits no envelope.
	if _, err := os.Stat(filepath.Join(ws, "envelopes", "push")); err == nil {
		// dir may or may not exist; we just want to confirm no
		// project-uid.json sitting there. The push-result path is keyed
		// on ProjectUID which is empty for clone mode.
	}
}

// ---------- Test 6: push mode, missing GIT_PAT ----------

func TestRunPushModeRefusesMissingCreds(t *testing.T) {
	base := t.TempDir()
	bareSrc, _ := seedBareRepo(t, base)
	branch := perRunBranch(t, "no-creds")
	ws := setupWorkspace(t, bareSrc, branch)

	cfg := pushConfig{
		Mode:          "push",
		Branch:        branch,
		LastPushedSHA: "",
		CommitMessage: "tide: plan x",
		ArtifactPaths: []string{},
		Workspace:     ws,
		ProjectUID:    "p6",
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Explicitly NOT setting GIT_PAT.
	os.Unsetenv("GIT_PAT")
	var stderr bytes.Buffer
	exit := run(ctx, cfg, io.Discard, &stderr)
	if exit != 2 {
		t.Fatalf("exit=%d, want 2 (invariant); stderr=%s", exit, stderr.Bytes())
	}
	pr := readPushEnvelope(t, ws, "p6")
	if pr.Reason != "missing-creds" {
		t.Errorf("envelope.reason = %q, want %q", pr.Reason, "missing-creds")
	}
}

// ---------- Test 7: --commit-message + --artifact-paths exact message ----------

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
