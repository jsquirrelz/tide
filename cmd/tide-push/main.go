// Command tide-push is the per-Project push Job binary (Phase 3 D-B1 / D-B5).
//
// Operating modes:
//
//	--mode=clone   — initial repo clone into <workspace>/repo.git. Used once
//	                 at Project creation by the ProjectReconciler's clone
//	                 Job (see internal/controller/push_helpers.go
//	                 buildCloneJob). Reads GIT_PAT for private repos;
//	                 public repos work without one.
//
//	--mode=push    — level-boundary push (Phase 3 D-B2). Stages
//	                 --artifact-paths into the per-run worktree at
//	                 <workspace>/worktrees/run-<branch>, creates a single
//	                 commit with --commit-message and the fixed TIDE-bot
//	                 author signature (W11), computes a unified-diff of
//	                 the new commit, scans the diff with internal/gitleaks
//	                 (D-B3), and on clean diff pushes with HTTPS+PAT and
//	                 --force-with-lease against --last-pushed-sha (D-B6).
//
// Exit-code map (Phase 03-RESEARCH Q5 RESOLVED — the reconciler in
// plan 03-08 maps each `reason` to a distinct Project.Status.phase):
//
//	0  — success (push or clone succeeded)
//	1  — generic git failure
//	2  — invariant violation (envelope.reason=invalid-branch for D-B6
//	     main-guard, missing-creds for absent GIT_PAT, or bad args)
//	10 — gitleaks finding (envelope.reason=leak-detected)
//	11 — lease rejection (envelope.reason=lease-rejected)
//	12 — auth failure (envelope.reason=auth-failed)
//	13 — network/timeout (envelope.reason=network-timeout)
//
// Credentials: GIT_PAT comes from the K8s Secret named in
// project.Spec.Git.CredsSecretRef, wired as envFrom on the push Job pod
// (D-B1 trust-boundary isolation — only the push Job pod sees the PAT;
// the controller pod never does). The PAT is read from env, passed
// directly into pkg/git's BasicAuth, and is NEVER logged.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/transport"

	"github.com/jsquirrelz/tide/internal/gitleaks"
	pkggit "github.com/jsquirrelz/tide/pkg/git"
)

// pushConfig is the parsed CLI configuration shared by clone- and
// push-mode. The struct is passed by value into run() so tests can drive
// it without setting os.Args.
type pushConfig struct {
	Mode          string   // "clone" | "push"
	RepoURL       string   // clone-mode: remote URL
	Branch        string   // push-mode: per-run branch (D-B6)
	LastPushedSHA string   // push-mode: lease anchor (empty on first push)
	CommitMessage string   // push-mode: W11 boundary commit message
	ArtifactPaths []string // push-mode: workspace-relative paths to stage
	LeaksConfig   string   // push-mode: optional gitleaks override TOML path
	Workspace     string   // root, default "/workspace"
	ProjectUID    string   // push-mode: keys the envelope output path
}

// pushResult is the small JSON envelope written to
// <workspace>/envelopes/push/<project-uid>.json on push-mode runs.
// Mirrors Phase 2 D-A2 envelope-on-PVC contract; the ProjectReconciler
// (plan 03-08) reads this to patch Status.git.lastPushedSHA + map
// `reason` to a Status.phase.
type pushResult struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
	ProjectUID string `json:"projectUID"`
	Branch     string `json:"branch"`
	HeadSHA    string `json:"headSHA"`
	ExitCode   int    `json:"exitCode"`
	Reason     string `json:"reason"`
}

const (
	envelopeAPIVersion = "tideproject.k8s/v1alpha1"
	envelopeKind       = "PushResult"

	exitSuccess     = 0
	exitGenericFail = 1
	exitInvariant   = 2
	exitLeakBlocked = 10
	exitLeaseFailed = 11
	exitAuthFailed  = 12
	exitNetwork     = 13
)

// tideBotSignature returns the fixed TIDE-bot author signature used for
// every boundary commit (W11 — name+email are stable across runs; only
// the timestamp varies).
func tideBotSignature() object.Signature {
	return object.Signature{
		Name:  "tide-bot",
		Email: "tide-bot@tideproject.k8s",
		When:  time.Now(),
	}
}

func main() {
	fs := flag.NewFlagSet("tide-push", flag.ExitOnError)
	mode := fs.String("mode", "", "operating mode: clone | push")
	repoURL := fs.String("repo-url", "", "clone-mode: remote URL")
	branch := fs.String("branch", "", "push-mode: per-run branch (D-B6 format)")
	lastPushedSHA := fs.String("last-pushed-sha", "", "push-mode: lease anchor (empty on first push)")
	commitMessage := fs.String("commit-message", "", "push-mode: W11 boundary commit message")
	artifactPaths := fs.String("artifact-paths", "", "push-mode: CSV list of workspace-relative paths to stage")
	leaksConfig := fs.String("leaks-config", "", "push-mode: optional gitleaks override TOML path")
	workspace := fs.String("workspace", "/workspace", "workspace root")
	projectUID := fs.String("project-uid", "", "push-mode: keys envelope output path")

	if err := fs.Parse(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "tide-push: flag parse: %v\n", err)
		os.Exit(exitInvariant)
	}

	cfg := pushConfig{
		Mode:          *mode,
		RepoURL:       *repoURL,
		Branch:        *branch,
		LastPushedSHA: *lastPushedSHA,
		CommitMessage: *commitMessage,
		ArtifactPaths: splitCSV(*artifactPaths),
		LeaksConfig:   *leaksConfig,
		Workspace:     *workspace,
		ProjectUID:    *projectUID,
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer cancel()

	os.Exit(run(ctx, cfg, os.Stdout, os.Stderr))
}

// splitCSV trims and returns non-empty comma-separated tokens.
func splitCSV(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// run is the testable in-process entry point. It dispatches on cfg.Mode
// and returns the process exit code.
//
// Tests pass cfg directly; the binary's main() constructs cfg from
// os.Args. Stdout is reserved for structured push-result-ish output;
// stderr carries log lines (and NEVER the PAT — Test 1 + Test 2 capture
// stderr to assert).
func run(ctx context.Context, cfg pushConfig, _ io.Writer, stderr io.Writer) int {
	switch cfg.Mode {
	case "clone":
		return runClone(ctx, cfg, stderr)
	case "push":
		return runPush(ctx, cfg, stderr)
	default:
		fmt.Fprintf(stderr, "tide-push: unknown mode %q (want clone|push)\n", cfg.Mode)
		return exitInvariant
	}
}

// runClone performs the initial bare clone of cfg.RepoURL into
// <workspace>/repo.git. No envelope is written — clone-mode is only ever
// the Project's one-time setup.
func runClone(ctx context.Context, cfg pushConfig, stderr io.Writer) int {
	if cfg.RepoURL == "" {
		fmt.Fprintf(stderr, "tide-push: clone mode requires --repo-url\n")
		return exitInvariant
	}
	if cfg.Workspace == "" {
		fmt.Fprintf(stderr, "tide-push: clone mode requires --workspace\n")
		return exitInvariant
	}

	destDir := filepath.Join(cfg.Workspace, "repo.git")
	pat := os.Getenv("GIT_PAT") // empty PAT is fine for public repos
	if _, err := pkggit.Clone(ctx, cfg.RepoURL, destDir, pat); err != nil {
		fmt.Fprintf(stderr, "tide-push: clone failed: %v\n", redactPAT(err.Error(), pat))
		return classifyGitError(err)
	}
	return exitSuccess
}

// runPush stages cfg.ArtifactPaths into the per-run worktree, commits
// with the W11 message + TIDE-bot signature, scans the resulting commit
// diff via gitleaks (D-B3, W10), and on clean diff pushes with
// --force-with-lease (D-B6).
func runPush(ctx context.Context, cfg pushConfig, stderr io.Writer) int {
	// Invariant 1: D-B6 never-targets-main guard. The push Job is the
	// policy enforcement point (per Phase 3 PATTERNS.md note).
	if cfg.Branch == "main" || cfg.Branch == "master" {
		writePushEnvelope(cfg, "", exitInvariant, "invalid-branch")
		fmt.Fprintf(stderr, "tide-push: branch must not be %s (D-B6)\n", cfg.Branch)
		return exitInvariant
	}
	if cfg.Branch == "" {
		writePushEnvelope(cfg, "", exitInvariant, "invalid-branch")
		fmt.Fprintf(stderr, "tide-push: push mode requires --branch\n")
		return exitInvariant
	}

	// Invariant 2: GIT_PAT present (D-B1 — only the push Job pod sees
	// the PAT). Read once into a local variable; never log it.
	pat := os.Getenv("GIT_PAT")
	if pat == "" {
		writePushEnvelope(cfg, "", exitInvariant, "missing-creds")
		fmt.Fprintf(stderr, "tide-push: GIT_PAT env is empty\n")
		return exitInvariant
	}

	// Invariant 3: commit message non-empty (W11 — orchestrator-supplied
	// boundary message). Empty == programmer error in the calling
	// reconciler.
	if cfg.CommitMessage == "" {
		writePushEnvelope(cfg, "", exitInvariant, "missing-commit-message")
		fmt.Fprintf(stderr, "tide-push: push mode requires --commit-message (W11)\n")
		return exitInvariant
	}

	// Open the per-run worktree. The orchestrator's prior clone-mode Job
	// populated <workspace>/repo.git; subsequent push Jobs find a working
	// tree at <workspace>/worktrees/run-<branch>/ already provisioned by
	// the harness (Phase 3 D-B4 — per-Task worktrees). For the push
	// boundary the orchestrator stages all per-Task artifacts into a
	// single per-run worktree.
	worktreeDir := filepath.Join(cfg.Workspace, "worktrees", "run-"+cfg.Branch)
	repo, err := gogit.PlainOpen(worktreeDir)
	if err != nil {
		writePushEnvelope(cfg, "", exitGenericFail, "no-worktree")
		fmt.Fprintf(stderr, "tide-push: PlainOpen %s failed: %v\n", worktreeDir, err)
		return exitGenericFail
	}
	wt, err := repo.Worktree()
	if err != nil {
		writePushEnvelope(cfg, "", exitGenericFail, "no-worktree")
		fmt.Fprintf(stderr, "tide-push: Worktree() failed: %v\n", err)
		return exitGenericFail
	}

	// Record the pre-commit HEAD so we can compute the new commit's diff
	// against it after Commit() lands (W10 — unified-diff vs parent for
	// gitleaks.ScanDiff).
	headRef, err := repo.Head()
	if err != nil {
		writePushEnvelope(cfg, "", exitGenericFail, "no-head")
		fmt.Fprintf(stderr, "tide-push: Head() failed: %v\n", err)
		return exitGenericFail
	}
	oldHash := headRef.Hash()

	// Stage each artifact path (D-B2 / W11). pkggit.AddPath wraps
	// wt.Add; missing files surface as errors so the caller (reconciler)
	// can correct the artifact-path list.
	for _, rel := range cfg.ArtifactPaths {
		// Resolve relative-to-workspace path against the worktree. The
		// push Job receives workspace-relative artifact paths
		// (e.g. "artifacts/M-001/P-003/L-005/PLAN.md") and must copy
		// them into the worktree before staging. We use os.Link as
		// the cheap default; fall back to a byte-copy if hardlink
		// fails (cross-filesystem, etc).
		src := filepath.Join(cfg.Workspace, rel)
		dst := filepath.Join(worktreeDir, rel)
		if err := copyIntoWorktree(src, dst); err != nil {
			writePushEnvelope(cfg, "", exitGenericFail, "artifact-copy-failed")
			fmt.Fprintf(stderr, "tide-push: copy artifact %s -> worktree: %v\n", rel, err)
			return exitGenericFail
		}
		if err := pkggit.AddPath(wt, rel); err != nil {
			writePushEnvelope(cfg, "", exitGenericFail, "stage-failed")
			fmt.Fprintf(stderr, "tide-push: stage %s: %v\n", rel, err)
			return exitGenericFail
		}
	}

	// Commit. pkggit.Commit returns the new hash (W11 — single boundary
	// commit per push). Author is the fixed TIDE-bot signature.
	newHash, err := pkggit.Commit(wt, cfg.CommitMessage, tideBotSignature())
	if err != nil {
		writePushEnvelope(cfg, "", exitGenericFail, "commit-failed")
		fmt.Fprintf(stderr, "tide-push: commit failed: %v\n", err)
		return exitGenericFail
	}

	// W10: compute the unified diff of the new commit against its parent
	// via newCommit.Patch(oldCommit).String(). The Patch.String() output
	// uses `+ ` / `- ` line prefixes that gitleaks rules can match.
	diff, err := computeUnifiedDiff(repo, oldHash, newHash)
	if err != nil {
		writePushEnvelope(cfg, "", exitGenericFail, "diff-failed")
		fmt.Fprintf(stderr, "tide-push: compute diff %s..%s: %v\n", oldHash, newHash, err)
		return exitGenericFail
	}

	// D-B3 / W10: scan the diff for secret-shaped patterns. A finding
	// short-circuits the push so the secret never leaves the cluster.
	found, _, scanErr := gitleaks.ScanDiff(diff)
	if scanErr != nil {
		writePushEnvelope(cfg, "", exitGenericFail, "gitleaks-error")
		fmt.Fprintf(stderr, "tide-push: gitleaks scan error: %v\n", scanErr)
		return exitGenericFail
	}
	if found {
		writePushEnvelope(cfg, newHash.String(), exitLeakBlocked, "leak-detected")
		// IMPORTANT: do NOT log the diff or finding contents — they
		// carry the matched secret value. Log only the count via the
		// envelope reason field. The reconciler in plan 03-08 fires
		// the tide_secret_leak_blocked_total counter on observation.
		fmt.Fprintf(stderr, "tide-push: gitleaks: at least one finding; refusing push\n")
		return exitLeakBlocked
	}

	// D-B6: push with --force-with-lease against cfg.LastPushedSHA.
	// First push (lease="") omits the lease via pkggit.Push contract.
	if err := pkggit.Push(ctx, repo, cfg.Branch, cfg.LastPushedSHA, pat); err != nil {
		exit, reason := classifyPushError(err)
		writePushEnvelope(cfg, newHash.String(), exit, reason)
		fmt.Fprintf(stderr, "tide-push: push failed (reason=%s): %v\n", reason, redactPAT(err.Error(), pat))
		return exit
	}

	writePushEnvelope(cfg, newHash.String(), exitSuccess, "")
	return exitSuccess
}

// computeUnifiedDiff resolves both commit hashes and returns the unified
// diff text of newHash vs oldHash, via the W10-pinned
// newCommit.Patch(oldCommit).String() API of go-git/v5.
//
// On the first push of a fresh branch (no prior commits), oldHash is
// already the initial commit (the bare repo's seed). The Patch call
// works against any two commits in the same repo's object database.
func computeUnifiedDiff(repo *gogit.Repository, oldHash, newHash plumbing.Hash) (string, error) {
	oldCommit, err := repo.CommitObject(oldHash)
	if err != nil {
		return "", fmt.Errorf("CommitObject old=%s: %w", oldHash, err)
	}
	newCommit, err := repo.CommitObject(newHash)
	if err != nil {
		return "", fmt.Errorf("CommitObject new=%s: %w", newHash, err)
	}
	// W10 pin: Patch returns a *object.Patch whose String() emits unified
	// diff text with `+ ` / `- ` line prefixes that gitleaks rules can
	// match. This is the structural compatibility fix vs the raw
	// tree.Diff path documented in 03-RESEARCH §W10.
	patch, err := newCommit.Patch(oldCommit)
	if err != nil {
		return "", fmt.Errorf("Patch %s..%s: %w", oldHash, newHash, err)
	}
	return patch.String(), nil
}

// copyIntoWorktree copies src to dst, creating parent dirs as needed.
// Uses io.Copy rather than os.Link because the source and destination
// may live on different filesystems within the PVC SubPath mount.
func copyIntoWorktree(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open src %s: %w", src, err)
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", filepath.Dir(dst), err)
	}
	out, err := os.Create(dst)
	if err != nil {
		return fmt.Errorf("create dst %s: %w", dst, err)
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		return fmt.Errorf("copy %s -> %s: %w", src, dst, err)
	}
	return nil
}

// terminationMessagePath is the K8s default container termination-message
// path. Plan 04-06 W-1: tide-push writes the push-result envelope here so
// the ProjectReconciler can read it from the Pod's
// Status.ContainerStatuses[0].State.Terminated.Message without mounting
// the PVC. K8s caps the file at 4096 bytes; the JSON envelope is well
// under 1 KB so no truncation risk.
const terminationMessagePath = "/dev/termination-log"

// writePushEnvelope writes the push-result JSON to
// <workspace>/envelopes/push/<project-uid>.json AND to
// /dev/termination-log (terminationMessagePath). Best-effort on both
// writes: write failures are logged but do not change the exit code (the
// caller has already decided what to return).
func writePushEnvelope(cfg pushConfig, headSHA string, exit int, reason string) {
	pr := pushResult{
		APIVersion: envelopeAPIVersion,
		Kind:       envelopeKind,
		ProjectUID: cfg.ProjectUID,
		Branch:     cfg.Branch,
		HeadSHA:    headSHA,
		ExitCode:   exit,
		Reason:     reason,
	}
	data, err := json.Marshal(pr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "tide-push: marshal envelope: %v\n", err)
		return
	}

	// W-1 surface: write to /dev/termination-log (K8s default
	// terminationMessagePath) so the ProjectReconciler can read
	// Reason without mounting the PVC. Best-effort — the file may
	// not be writable in non-K8s test environments.
	if err := os.WriteFile(terminationMessagePath, data, 0o644); err != nil {
		// Not all environments have /dev/termination-log writable
		// (host machine tests, etc.) — log at low signal.
		fmt.Fprintf(os.Stderr, "tide-push: write terminationMessage (best-effort): %v\n", err)
	}

	// Also write to PVC envelope path (Phase 3 D-A2 contract) for
	// downstream consumers that mount the PVC. Skip if ProjectUID
	// is unset (no PVC path possible).
	if cfg.ProjectUID == "" {
		return
	}
	envDir := filepath.Join(cfg.Workspace, "envelopes", "push")
	if err := os.MkdirAll(envDir, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "tide-push: mkdir envelope dir %s: %v\n", envDir, err)
		return
	}
	envPath := filepath.Join(envDir, cfg.ProjectUID+".json")
	if err := os.WriteFile(envPath, data, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "tide-push: write envelope %s: %v\n", envPath, err)
	}
}

// classifyPushError maps a pkggit.Push error to (exit-code, reason)
// per the Phase 03-RESEARCH Q5 RESOLVED exit-code map. We inspect the
// error string conservatively because go-git's error types are not
// stable across versions (and the lease/auth/network distinctions are
// not surfaced as discrete sentinel errors in v5.19.0).
func classifyPushError(err error) (int, string) {
	if err == nil {
		return exitSuccess, ""
	}
	// Auth failures surface as transport.ErrAuthenticationRequired or
	// transport.ErrAuthorizationFailed (both wrapped by Push).
	if errors.Is(err, transport.ErrAuthenticationRequired) ||
		errors.Is(err, transport.ErrAuthorizationFailed) {
		return exitAuthFailed, "auth-failed"
	}
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "stale info") ||
		strings.Contains(msg, "non-fast-forward") ||
		strings.Contains(msg, "force-with-lease"):
		return exitLeaseFailed, "lease-rejected"
	case strings.Contains(msg, "authentication") || strings.Contains(msg, "authorization") ||
		strings.Contains(msg, "401") || strings.Contains(msg, "403"):
		return exitAuthFailed, "auth-failed"
	case strings.Contains(msg, "timeout") || strings.Contains(msg, "dial tcp") ||
		strings.Contains(msg, "no such host"):
		return exitNetwork, "network-timeout"
	}
	return exitGenericFail, "push-failed"
}

// classifyGitError is the clone-side analog for runClone — auth and
// network errors are surfaced via the same exit codes so the
// reconciler can react identically.
func classifyGitError(err error) int {
	if err == nil {
		return exitSuccess
	}
	if errors.Is(err, transport.ErrAuthenticationRequired) ||
		errors.Is(err, transport.ErrAuthorizationFailed) {
		return exitAuthFailed
	}
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "authentication") || strings.Contains(msg, "authorization") ||
		strings.Contains(msg, "401") || strings.Contains(msg, "403"):
		return exitAuthFailed
	case strings.Contains(msg, "timeout") || strings.Contains(msg, "dial tcp") ||
		strings.Contains(msg, "no such host"):
		return exitNetwork
	}
	return exitGenericFail
}

// redactPAT removes any occurrences of pat from msg. Defense in depth:
// pkggit's error wrappers shouldn't include the PAT, but if a future
// go-git version inlines the auth URL into error text the PAT must not
// flow into our stderr.
//
// WR-09 fix: also strip the URL-encoded forms of the PAT. go-git may log
// auth URLs with the PAT percent-encoded (e.g. `%2B` for `+`), so the
// raw-substring redaction alone leaks via that path. We redact ALL of:
//   - the raw PAT
//   - url.QueryEscape(pat)  — percent-encoding for application/x-www-form-urlencoded
//   - url.PathEscape(pat)   — percent-encoding for URL paths (treats / specially)
//
// Duplicate-redact is safe because ReplaceAll is idempotent on its own
// output: once a substring becomes `<redacted>` it no longer matches the
// next variant.
func redactPAT(msg, pat string) string {
	if pat == "" {
		return msg
	}
	msg = strings.ReplaceAll(msg, pat, "<redacted>")
	if enc := url.QueryEscape(pat); enc != pat {
		msg = strings.ReplaceAll(msg, enc, "<redacted>")
	}
	if enc := url.PathEscape(pat); enc != pat {
		msg = strings.ReplaceAll(msg, enc, "<redacted>")
	}
	return msg
}
