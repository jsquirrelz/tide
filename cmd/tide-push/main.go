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
//	                 commit with --commit-message and the env-sourced TIDE
//	                 agent author signature (W11 / D-05 — TIDE_AGENT_NAME /
//	                 TIDE_AGENT_EMAIL, compiled default TIDE Agent
//	                 <tide-agent@tideproject.k8s>), computes a unified-diff of
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
//	     main-guard, missing-creds for absent GIT_PAT, or bad args). Clone mode
//	     also rides this code for an unresolvable --base-ref
//	     (envelope.reason=baseref-unresolvable, Phase 35 D-05): the controller
//	     classifies on the reason, not the exit code — exit 14 belongs to
//	     Phase 34's integration-incomplete and must not be reused here.
//	10 — gitleaks finding (envelope.reason=leak-detected)
//	11 — lease rejection (envelope.reason=lease-rejected)
//	12 — auth failure (envelope.reason=auth-failed)
//	13 — network/timeout (envelope.reason=network-timeout)
//	14 — integration-completeness miss (Phase 34 D-06/INTEG-03; envelope.reason=
//	     integration-incomplete, envelope.missingBranches/missingTotal set) — the
//	     in-Job verify gate (`git merge-base --is-ancestor` per expected branch,
//	     run after integrate and before push) found at least one expected task
//	     branch not reachable from the run branch. Nothing is pushed.
//	15 — merge conflict (Phase 34 D-09/D-10; envelope.reason=merge-conflict,
//	     envelope.conflictBranch set) — a genuine content conflict (not a
//	     transient failure) was hit integrating a task branch; distinct from
//	     exit 1's undifferentiated "integration-failed" so the controller can
//	     classify conflict (park immediately) vs transient (bounded retry).
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
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"syscall"
	"time"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"golang.org/x/sys/unix"

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
	// RunBranch is the clone-mode per-run branch for EnsureRunBranch + run
	// worktree (D-B6/B5); if non-empty, EnsureRunBranch + provision run
	// worktree after clone.
	RunBranch string
	// BaseRef is the clone-mode ref the run branch is created from (Phase 35
	// BASE-01): a branch, tag, full 40-hex SHA, or refs/-qualified ref. Empty
	// means the remote default branch (HEAD). Passed to EnsureRunBranch, which
	// resolves it; an unresolvable value becomes the exit-2 baseref-unresolvable
	// clone envelope.
	BaseRef               string
	IntegrateTaskBranches []string // push-mode: task branch names to merge before staging artifacts (D-04)
	// IntegrationOnly marks a per-wave integration Job (D-02/D-04): merge +
	// verify task branches into the LOCAL run branch and exit success without
	// committing or pushing — the remote push belongs to boundary pushes.
	// Boundary pushes carry the cumulative branch set too (D-03/D-07), so the
	// no-push exit keys on this explicit flag, never on absent artifact paths.
	IntegrationOnly bool
	// StageEnvelopes is the raw --stage-envelopes flag value: a CSV of
	// `<uid>:<destPrefix>` pairs (DASH-02). It is parsed and validated inside
	// runPush (parseStageEnvelopes) so a malformed value fails loudly with the
	// typed envelope reason "artifact-stage-failed" before any git operation —
	// the same testable-through-run() convention the other push invariants use.
	StageEnvelopes string
}

// EnvelopeStage maps one on-PVC envelope directory (UID-keyed, the same key the
// dispatch/reporter path writes under envelopes/<uid>/) to the human-readable
// destination prefix it lands at on the run branch: .tide/planning/<DestPrefix>/
// (DASH-02, D-02). DestPrefix is `<kind>/<name>` with kind ∈
// {project, milestone, phase, plan}.
type EnvelopeStage struct {
	UID        string
	DestPrefix string
}

// destPrefixPattern constrains a --stage-envelopes destPrefix to a clean,
// slash-nested relative path built from DNS-1123-style segments. It rejects a
// leading dot/slash/dash and a trailing slash/dot, so bare "..", absolute "/abs",
// and dotfile prefixes never match. Interior "." and "/" are allowed (real
// prefixes look like "milestone/m1"), so nested traversal such as
// "milestone/../../etc" still matches the pattern — the containment check in
// parseStageEnvelopes is the second, load-bearing gate against escape (T-37-02-01).
var destPrefixPattern = regexp.MustCompile(`^[a-z0-9]([a-zA-Z0-9._/-]*[a-zA-Z0-9])?$`)

// parseStageEnvelopes parses the --stage-envelopes CSV (`<uid>:<destPrefix>`
// pairs) into []EnvelopeStage with fail-closed validation (DASH-02, D-03).
// Empty (or whitespace-only) input returns (nil, nil). Each token splits on its
// FIRST colon into a non-empty UID and a non-empty destPrefix; destPrefix must
// contain no backslash, must match destPrefixPattern, and — after
// filepath.Clean — must still resolve strictly under .tide/planning/ (traversal
// containment, T-37-02-01). Any violation returns an error; the caller maps it
// to reason "artifact-stage-failed" + nonzero exit before touching git.
func parseStageEnvelopes(s string) ([]EnvelopeStage, error) {
	if strings.TrimSpace(s) == "" {
		return nil, nil
	}
	base := filepath.Join(".tide", "planning")
	tokens := strings.Split(s, ",")
	out := make([]EnvelopeStage, 0, len(tokens))
	for _, tok := range tokens {
		tok = strings.TrimSpace(tok)
		if tok == "" {
			continue
		}
		rawUID, rawDest, found := strings.Cut(tok, ":")
		if !found {
			return nil, fmt.Errorf("stage-envelopes token %q missing ':' separator (want <uid>:<destPrefix>)", tok)
		}
		uid := strings.TrimSpace(rawUID)
		destPrefix := strings.TrimSpace(rawDest)
		if uid == "" {
			return nil, fmt.Errorf("stage-envelopes token %q has an empty UID", tok)
		}
		if destPrefix == "" {
			return nil, fmt.Errorf("stage-envelopes token %q has an empty destPrefix", tok)
		}
		if strings.ContainsRune(destPrefix, '\\') {
			return nil, fmt.Errorf("stage-envelopes destPrefix %q contains a backslash", destPrefix)
		}
		if !destPrefixPattern.MatchString(destPrefix) {
			return nil, fmt.Errorf("stage-envelopes destPrefix %q does not match %s", destPrefix, destPrefixPattern.String())
		}
		cleaned := filepath.Clean(filepath.Join(base, destPrefix))
		if cleaned != base && !strings.HasPrefix(cleaned, base+string(filepath.Separator)) {
			return nil, fmt.Errorf("stage-envelopes destPrefix %q escapes %s/", destPrefix, base)
		}
		out = append(out, EnvelopeStage{UID: uid, DestPrefix: destPrefix})
	}
	return out, nil
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

	// Phase 34 D-09/D-12: extended detail fields for the integration-miss
	// gate. JSON tags MUST match internal/controller/project_controller.go's
	// pushResultEnvelope struct exactly (cross-binary contract).
	// MissingBranches is truncated to the first missingBranchesLimit (sorted)
	// with MissingTotal carrying the full count (termination-log 4096-byte
	// cap). ConflictBranch names the task branch that hit a genuine merge
	// conflict (set only when Reason == "merge-conflict").
	MissingBranches []string `json:"missingBranches,omitempty"`
	MissingTotal    int      `json:"missingTotal,omitempty"`
	ConflictBranch  string   `json:"conflictBranch,omitempty"`

	// Phase 35 BASE-01/BASE-02: clone-mode fields (Kind == envelopeKindClone).
	// One struct serves both modes (RESEARCH Open Q2). BaseSHA is the resolved
	// commit the run branch was created from (success; empty on failure and on
	// the no-run-branch legacy path). BaseRef echoes the ref as given so the
	// controller can name it in the BaseRefUnresolvable condition. JSON keys are
	// the plan 35-02 objective contract plan 35-03 parses.
	BaseSHA string `json:"baseSHA,omitempty"`
	BaseRef string `json:"baseRef,omitempty"`
}

const (
	envelopeAPIVersion = "tideproject.k8s/v1alpha1"
	envelopeKind       = "PushResult"
	// envelopeKindClone is the Kind clone-mode envelopes carry (Phase 35 D-05).
	// The controller parses by reason/fields, not Kind, but a distinct Kind
	// keeps clone provenance legible.
	envelopeKindClone = "CloneResult"

	exitSuccess     = 0
	exitGenericFail = 1
	exitInvariant   = 2
	exitLeakBlocked = 10
	exitLeaseFailed = 11
	exitAuthFailed  = 12
	exitNetwork     = 13

	// Phase 34 D-06/D-09: new terminal outcomes for the integration-miss gate.
	exitIntegrationMiss = 14
	exitMergeConflict   = 15

	// missingBranchesLimit bounds the envelope's MissingBranches list (D-12):
	// the termination-log surface is capped at 4096 bytes, so only the first
	// N (sorted) missing branches are carried; MissingTotal always carries
	// the full count.
	missingBranchesLimit = 10
)

// agentSignature returns the TIDE agent author signature used for every
// boundary commit. Identity comes from pkggit.AgentIdentity() (TIDE_AGENT_NAME /
// TIDE_AGENT_EMAIL env, compiled default "TIDE Agent <tide-agent@tideproject.k8s>",
// D-04 / D-05). The W11 stability contract holds: name+email are stable across
// runs because they come from install/Project config, not per-run state — only
// the timestamp (When) varies from commit to commit.
func agentSignature() object.Signature {
	name, email := pkggit.AgentIdentity()
	return object.Signature{
		Name:  name,
		Email: email,
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
	runBranch := fs.String("run-branch", "",
		"clone-mode: per-run branch for EnsureRunBranch + run worktree provision (B5/D-B6)")
	baseRef := fs.String("base-ref", "",
		"clone-mode: branch, tag, full 40-hex SHA, or refs/-qualified ref the run branch "+
			"is created from; empty means remote HEAD (BASE-01)")
	integrateTaskBranches := fs.String("integrate-task-branches", "",
		"push-mode: CSV of task branch names to merge before staging (D-04)")
	integrationOnly := fs.Bool("integration-only", false,
		"push-mode: per-wave integration Job — merge+verify locally, no commit/push (D-02)")
	stageEnvelopes := fs.String("stage-envelopes", "",
		"push-mode: CSV of <uid>:<destPrefix> pairs. Copies each envelope's "+
			"planning *.md + children/*.json from envelopes/<uid>/ (under the Job "+
			"workspace root) into .tide/planning/<destPrefix>/ on the run branch "+
			"(DASH-02, human-readable paths per D-02). out.json/in.json are never "+
			"staged (D-04); failures are loud (D-03).")

	if err := fs.Parse(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "tide-push: flag parse: %v\n", err)
		os.Exit(exitInvariant)
	}

	cfg := pushConfig{
		Mode:                  *mode,
		RepoURL:               *repoURL,
		Branch:                *branch,
		LastPushedSHA:         *lastPushedSHA,
		CommitMessage:         *commitMessage,
		ArtifactPaths:         splitCSV(*artifactPaths),
		LeaksConfig:           *leaksConfig,
		Workspace:             *workspace,
		ProjectUID:            *projectUID,
		RunBranch:             *runBranch,
		BaseRef:               *baseRef,
		IntegrateTaskBranches: splitCSV(*integrateTaskBranches),
		IntegrationOnly:       *integrationOnly,
		StageEnvelopes:        *stageEnvelopes,
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

// sharedFSGroup is the gid every TIDE dispatch/push pod shares via the pod
// SecurityContext fsGroup (pinned to 1000 in internal/controller/push_helpers.go
// and internal/dispatch/podjob/jobspec.go). The clone Job runs as uid 65532 but
// is a member of this group; the executor (claude-subagent) runs as uid 1000
// whose primary group is also this gid. Sharing the bare repo across those two
// uids requires repo.git to be group-owned by this gid and group-writable.
const sharedFSGroup = 1000

// makeWorkspaceGroupShared makes the per-run workspace tree at root writable by
// any pod sharing sharedFSGroup. The clone Job (uid 65532) creates several
// subtrees under /workspace that the executor (claude-subagent, uid 1000) must
// then write into: the bare repo at repo.git (per-task worktree branch refs,
// objects, worktree admin files) AND the worktrees/ directory (the executor's
// `git worktree add` creates worktrees/<taskUID>/ under it). go-git's clone and
// `git worktree add` create these 0755 owned by uid 65532, so the executor hits
// "Permission denied" — first on the branch ref under repo.git, then on the
// leading directories under worktrees/. Group-sharing the whole workspace once
// (rather than per-subtree) is the root fix. We add group rwX, set the setgid
// bit on directories so cross-uid-created entries inherit the shared group, and
// chgrp the tree to sharedFSGroup. Best-effort like the envelopes group-share
// (internal/harness mkdirSharedAll, cascade B): per-entry chmod/chown failures
// across uid boundaries are tolerated (the owning clone Job, a member of the
// fsGroup, succeeds at runtime; non-member test/CI environments simply no-op;
// the root mount point is often root-owned 0777 and is skipped harmlessly).
func makeWorkspaceGroupShared(root string) error {
	return filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // tolerate unreadable/transient entries; keep walking siblings
		}
		mode := info.Mode().Perm() | 0o060 // group read+write
		if info.IsDir() {
			mode |= 0o010 | os.ModeSetgid // group traverse + inherit group on new entries
		}
		// chgrp BEFORE chmod: a successful chown strips the setuid/setgid bits
		// on Linux, so the chmod must run last to leave setgid intact.
		_ = os.Chown(p, -1, sharedFSGroup) // best-effort chgrp to the shared fsGroup
		_ = os.Chmod(p, mode)              // best-effort across uid boundaries
		return nil
	})
}

// runClone performs the initial bare clone of cfg.RepoURL into
// <workspace>/repo.git. Every exit path writes a clone-result envelope
// (Kind CloneResult) to /dev/termination-log + the PVC (Phase 35 D-05): a
// success envelope carries the resolved base SHA (D-11), an unresolvable
// --base-ref writes reason baseref-unresolvable at exit 2 (D-05), and any other
// failure writes reason clone-failed at its existing exit code.
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
		writeCloneEnvelope(cfg, "", classifyGitError(err), "clone-failed")
		fmt.Fprintf(stderr, "tide-push: clone failed: %v\n", redactPAT(err.Error(), pat))
		return classifyGitError(err)
	}

	// Mark the bare repo as group-shared so every subsequent git write — by the
	// executor (uid 1000) committing in its worktree, and by the integration/push
	// Job (uid 65532) merging — creates group-writable objects and refs. Both run
	// with primary gid 1000 (RunAsGroup), so core.sharedRepository=group lets them
	// write into each other's object/ref dirs. Set immediately after Clone, before
	// EnsureRunBranch + worktree-add, so those writes are group-shared too. git's
	// umask alone (0755 objects) would otherwise block the cross-uid merge with
	// "insufficient permission for adding an object to repository database".
	shareCmd := exec.Command("git", "-C", destDir, "config", "core.sharedRepository", "group")
	if out, err := shareCmd.CombinedOutput(); err != nil {
		writeCloneEnvelope(cfg, "", exitGenericFail, "clone-failed")
		fmt.Fprintf(stderr, "tide-push: set core.sharedRepository: %v: %s\n", err, string(out))
		return exitGenericFail
	}

	// B5: if --run-branch is set, create the run branch ref in the bare repo
	// via EnsureRunBranch (resolving cfg.BaseRef) and provision the run worktree
	// via `git worktree add`. The linked worktree shares the object store with
	// destDir so task branches pushed to destDir are visible for `git merge` in
	// the integration step.
	//
	// runPush opens the run worktree with PlainOpenWithOptions(EnableDotGitCommonDir:true)
	// so it correctly resolves HEAD through the commondir mechanism of linked worktrees.
	//
	// baseSHA is the resolved base commit the run branch was created from
	// (D-11); it rides the success envelope back to the controller for the
	// status.git.baseSHA stamp. On the no-run-branch legacy path it stays empty.
	var baseSHA string
	if cfg.RunBranch != "" {
		h, err := pkggit.EnsureRunBranch(destDir, cfg.RunBranch, cfg.BaseRef)
		if err != nil {
			// BASE-02/D-05: an unresolvable baseRef fails fast, classified on the
			// envelope reason (exit 2 — the controller keys on reason, not code).
			if errors.Is(err, pkggit.ErrBaseRefUnresolvable) {
				writeCloneEnvelope(cfg, "", exitInvariant, "baseref-unresolvable")
				fmt.Fprintf(stderr, "tide-push: %v\n", redactPAT(err.Error(), pat))
				return exitInvariant
			}
			writeCloneEnvelope(cfg, "", exitGenericFail, "clone-failed")
			fmt.Fprintf(stderr, "tide-push: EnsureRunBranch: %v\n", redactPAT(err.Error(), pat))
			return exitGenericFail
		}
		baseSHA = h.String()

		runWorktreeDir := filepath.Join(cfg.Workspace, "worktrees", "run-"+cfg.RunBranch)
		out, err := exec.Command("git", "-C", destDir, "worktree", "add", runWorktreeDir, cfg.RunBranch).CombinedOutput()
		if err != nil {
			// Idempotent: if worktree already exists, log and continue.
			if !strings.Contains(string(out), "already") {
				writeCloneEnvelope(cfg, "", exitGenericFail, "clone-failed")
				fmt.Fprintf(stderr, "tide-push: provision run worktree: %v: %s\n", err, redactPAT(string(out), pat))
				return exitGenericFail
			}
		}

		// Group-share the whole per-run workspace so the executor
		// (claude-subagent, uid 1000) can write the subtrees this clone Job
		// created as uid 65532: the per-task worktree branch ref/objects/admin
		// under repo.git, AND the worktrees/<taskUID>/ directory its
		// `git worktree add` creates under worktrees/. Without this the executor
		// fails first on "cannot lock ref 'refs/heads/tide/wt-<uid>'" and then on
		// "could not create leading directories of '/workspace/worktrees/<uid>/.git'".
		if err := makeWorkspaceGroupShared(cfg.Workspace); err != nil {
			writeCloneEnvelope(cfg, "", exitGenericFail, "clone-failed")
			fmt.Fprintf(stderr, "tide-push: share workspace group perms: %v\n", err)
			return exitGenericFail
		}
	}

	// Success on every clone path (with or without a run branch): the envelope
	// carries the resolved baseSHA (empty on the legacy no-run-branch path).
	writeCloneEnvelope(cfg, baseSHA, exitSuccess, "")
	return exitSuccess
}

// runPush stages cfg.ArtifactPaths into the per-run worktree, commits
// with the W11 message + TIDE-bot signature, scans the resulting commit
// diff via gitleaks (D-B3, W10), and on clean diff pushes with
// --force-with-lease (D-B6).
func runPush(ctx context.Context, cfg pushConfig, stderr io.Writer) int {
	if exit, failed := validatePushInvariants(cfg, stderr); failed {
		return exit
	}

	// Phase 34 D-02 (belt-and-braces) / D-06: kernel flock held across
	// integrate -> verify -> push. The control-plane single-flight gate
	// (D-02, internal/controller/git_writer.go gitWriterInFlightCount) is the
	// PRIMARY serializer; this lock is defense in depth against two Jobs
	// somehow racing on the same shared-PVC integration worktree. Lockfile
	// EXISTENCE carries no meaning — it is never deleted; the kernel releases
	// the lock automatically on process exit, so a crashed Job can never
	// wedge a successor (no lockfile-existence protocol — a locked
	// constraint). Placed inside repo.git so no checkout ever cleans it.
	bareRepoPath := filepath.Join(cfg.Workspace, "repo.git")
	lockFile, exit, failed := acquireIntegrationLock(cfg, bareRepoPath, stderr)
	if failed {
		return exit
	}
	defer func() { _ = lockFile.Close() }()

	// DASH-02: parse --stage-envelopes up front, before any git operation, so a
	// malformed map (traversal, empty UID/destPrefix, bad pattern) fails loudly
	// with the typed reason "artifact-stage-failed" and never reaches integrate,
	// clone-open, or push (D-03, T-37-02-01). Validation is fail-closed and pure.
	stageEnvs, err := parseStageEnvelopes(cfg.StageEnvelopes)
	if err != nil {
		writePushEnvelope(cfg, "", exitInvariant, "artifact-stage-failed", nil, 0, "")
		fmt.Fprintf(stderr, "tide-push: %v\n", err)
		return exitInvariant
	}

	if exit, done := runIntegrationPhase(cfg, bareRepoPath, stderr); done {
		return exit
	}

	// Open the per-run worktree. The orchestrator's prior clone-mode Job
	// populated <workspace>/repo.git and provisioned a linked worktree at
	// <workspace>/worktrees/run-<branch>/ via `git worktree add`.
	// EnableDotGitCommonDir:true is required so go-git resolves the branch ref
	// via the commondir mechanism used by linked git worktrees (without it,
	// repo.Head() returns "reference not found" because the branch ref lives
	// in the parent bare repo, not in the per-worktree .git directory).
	worktreeDir := filepath.Join(cfg.Workspace, "worktrees", "run-"+cfg.Branch)
	repo, err := gogit.PlainOpenWithOptions(worktreeDir, &gogit.PlainOpenOptions{
		EnableDotGitCommonDir: true,
	})
	if err != nil {
		writePushEnvelope(cfg, "", exitGenericFail, "no-worktree", nil, 0, "")
		fmt.Fprintf(stderr, "tide-push: PlainOpen %s failed: %v\n", worktreeDir, err)
		return exitGenericFail
	}

	pat, exit, failed := resolveGitAuth(cfg, repo, stderr)
	if failed {
		return exit
	}

	wt, err := repo.Worktree()
	if err != nil {
		writePushEnvelope(cfg, "", exitGenericFail, "no-worktree", nil, 0, "")
		fmt.Fprintf(stderr, "tide-push: Worktree() failed: %v\n", err)
		return exitGenericFail
	}

	// Record the pre-commit HEAD so we can compute the new commit's diff
	// against it after Commit() lands (W10 — unified-diff vs parent for
	// gitleaks.ScanDiff).
	headRef, err := repo.Head()
	if err != nil {
		writePushEnvelope(cfg, "", exitGenericFail, "no-head", nil, 0, "")
		fmt.Fprintf(stderr, "tide-push: Head() failed: %v\n", err)
		return exitGenericFail
	}
	oldHash := headRef.Hash()

	if exit, failed := stageArtifacts(cfg, wt, worktreeDir, stderr); failed {
		return exit
	}

	// DASH-02: stage mapped envelope planning artifacts beside the ArtifactPaths
	// staging (D-01/D-02). For each <uid>:<destPrefix>, stageEnvelopeArtifacts
	// globs the level's planning *.md and children/*.json under envelopes/<uid>/
	// (the same PVC key the dispatch/reporter path writes) and copies them into
	// .tide/planning/<destPrefix>/ in the run worktree. out.json/in.json are never
	// staged (D-04); a missing envelope dir or any copy/stage error is a loud
	// failure (reason artifact-stage-failed, D-03). The staged files ride the same
	// commit / gitleaks scan / force-with-lease push below; byte-identical
	// restaging leaves the tree clean, which the clean-tree skip turns into an
	// idempotent no-op push.
	if code := stageEnvelopeArtifacts(cfg, stageEnvs, worktreeDir, wt, stderr); code != exitSuccess {
		return code
	}

	newHash, scanBase, exit, failed := commitOrReuseHead(cfg, repo, wt, worktreeDir, oldHash, stderr)
	if failed {
		return exit
	}

	// W10: compute the unified diff of the pushed commit against scanBase via
	// newCommit.Patch(oldCommit).String(). The Patch.String() output uses
	// `+ ` / `- ` line prefixes that gitleaks rules can match. When
	// newHash == scanBase (clean tree, no remote anchor) the diff is empty and
	// the scan is a no-op — the push proceeds.
	diff, err := computeUnifiedDiff(repo, scanBase, newHash)
	if err != nil {
		writePushEnvelope(cfg, "", exitGenericFail, "diff-failed", nil, 0, "")
		fmt.Fprintf(stderr, "tide-push: compute diff %s..%s: %v\n", scanBase, newHash, err)
		return exitGenericFail
	}

	// D-B3 / W10: scan the diff for secret-shaped patterns. A finding
	// short-circuits the push so the secret never leaves the cluster.
	found, _, scanErr := gitleaks.ScanDiff(diff)
	if scanErr != nil {
		writePushEnvelope(cfg, "", exitGenericFail, "gitleaks-error", nil, 0, "")
		fmt.Fprintf(stderr, "tide-push: gitleaks scan error: %v\n", scanErr)
		return exitGenericFail
	}
	if found {
		writePushEnvelope(cfg, newHash.String(), exitLeakBlocked, "leak-detected", nil, 0, "")
		// IMPORTANT: do NOT log the diff or finding contents — they
		// carry the matched secret value. Log only the count via the
		// envelope reason field. The reconciler in plan 03-08 fires
		// the tide_secret_leak_blocked_total counter on observation.
		fmt.Fprintf(stderr, "tide-push: gitleaks: at least one finding; refusing push\n")
		return exitLeakBlocked
	}

	// D-B6: push with --force-with-lease against cfg.LastPushedSHA.
	// First push (lease="") omits the lease via pkggit.Push contract. The
	// push is idempotent: re-pushing an already-present run branch HEAD is a
	// no-op fast-forward, so a retried boundary-push Job converges safely.
	if err := pkggit.Push(ctx, repo, cfg.Branch, cfg.LastPushedSHA, pat); err != nil {
		// DASH-02: an idempotent restage leaves the tree clean and HEAD already on
		// the remote at the leased SHA, so the force-with-lease push is a no-op that
		// go-git reports as NoErrAlreadyUpToDate. Treat it as success for cumulative
		// envelope maps — the remote already carries this HEAD. Mirrors pkg/git
		// Fetch, which swallows the same sentinel.
		if errors.Is(err, gogit.NoErrAlreadyUpToDate) {
			fmt.Fprintf(stderr, "tide-push: remote already up-to-date for %s — nothing to push\n", cfg.Branch)
			writePushEnvelope(cfg, newHash.String(), exitSuccess, "", nil, 0, "")
			return exitSuccess
		}
		exit, reason := classifyPushError(err)
		writePushEnvelope(cfg, newHash.String(), exit, reason, nil, 0, "")
		fmt.Fprintf(stderr, "tide-push: push failed (reason=%s): %v\n", reason, redactPAT(err.Error(), pat))
		return exit
	}

	writePushEnvelope(cfg, newHash.String(), exitSuccess, "", nil, 0, "")
	return exitSuccess
}

// validatePushInvariants checks the D-B6 never-targets-main guard, the
// --branch presence invariant, and the W11 --commit-message presence
// invariant. On any violation it writes the envelope + stderr message and
// returns (exitCode, true); otherwise (0, false).
func validatePushInvariants(cfg pushConfig, stderr io.Writer) (int, bool) {
	// Invariant 1: D-B6 never-targets-main guard. The push Job is the
	// policy enforcement point (per Phase 3 PATTERNS.md note).
	if cfg.Branch == "main" || cfg.Branch == "master" {
		writePushEnvelope(cfg, "", exitInvariant, "invalid-branch", nil, 0, "")
		fmt.Fprintf(stderr, "tide-push: branch must not be %s (D-B6)\n", cfg.Branch)
		return exitInvariant, true
	}
	if cfg.Branch == "" {
		writePushEnvelope(cfg, "", exitInvariant, "invalid-branch", nil, 0, "")
		fmt.Fprintf(stderr, "tide-push: push mode requires --branch\n")
		return exitInvariant, true
	}

	// Invariant 3: commit message non-empty (W11 — orchestrator-supplied
	// boundary message). Empty == programmer error in the calling
	// reconciler. Checked before PlainOpen (cheap pre-condition).
	//
	// Integration-only wave Jobs are EXEMPT: they never create the boundary
	// staging commit this message is for — the merges carry their own
	// generated messages (pkg/git IntegrateTaskBranches) and the Job exits
	// after the verify gate, before staging/push. triggerWaveIntegrationJob
	// dispatches with an empty message by design; enforcing W11 here killed
	// every wave-integration Job at startup (PR #3 run 7: exit 2 in 5ms,
	// envelope-unreadable — the stderr line became the termination message
	// via FallbackToLogsOnError and failed JSON parsing), so the Phase 34
	// integration gate never ran end-to-end.
	if cfg.CommitMessage == "" && !cfg.IntegrationOnly {
		writePushEnvelope(cfg, "", exitInvariant, "missing-commit-message", nil, 0, "")
		fmt.Fprintf(stderr, "tide-push: push mode requires --commit-message (W11)\n")
		return exitInvariant, true
	}
	return 0, false
}

// acquireIntegrationLock opens (creating if needed) and flocks
// <bareRepoPath>/tide-integrate.lock. On failure it writes the envelope +
// stderr message and returns (nil, exitCode, true); otherwise the caller
// owns the returned *os.File and must close it.
func acquireIntegrationLock(cfg pushConfig, bareRepoPath string, stderr io.Writer) (*os.File, int, bool) {
	lockPath := filepath.Join(bareRepoPath, "tide-integrate.lock")
	lockFile, lockErr := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o664)
	if lockErr != nil {
		writePushEnvelope(cfg, "", exitGenericFail, "lock-open-failed", nil, 0, "")
		fmt.Fprintf(stderr, "tide-push: open lockfile %s: %v\n", lockPath, lockErr)
		return nil, exitGenericFail, true
	}
	if err := unix.Flock(int(lockFile.Fd()), unix.LOCK_EX); err != nil {
		writePushEnvelope(cfg, "", exitGenericFail, "lock-failed", nil, 0, "")
		fmt.Fprintf(stderr, "tide-push: flock %s: %v\n", lockPath, err)
		_ = lockFile.Close()
		return nil, exitGenericFail, true
	}
	return lockFile, 0, false
}

// runIntegrationPhase runs the D-04 task-branch merge, the D-06/INTEG-03
// verify gate, and the per-wave integration-only early-success exit. The
// returned bool is true whenever runPush should return immediately with the
// returned exit code — for a success outcome (integration-only mode) as
// well as any failure outcome.
func runIntegrationPhase(cfg pushConfig, bareRepoPath string, stderr io.Writer) (int, bool) {
	// D-04: if --integrate-task-branches is set, merge each task branch into
	// the run branch BEFORE staging planner artifacts. This ensures the
	// boundary commit captures both executor-authored code and planner
	// artifacts in one unified push.
	if len(cfg.IntegrateTaskBranches) > 0 {
		if err := pkggit.IntegrateTaskBranches(bareRepoPath, cfg.Branch, cfg.IntegrateTaskBranches); err != nil {
			// D-09: classify a genuine content conflict distinctly from a
			// transient failure so the controller can park immediately
			// (conflict) instead of burning the bounded-retry budget
			// (transient). pkg/git already left the integration worktree
			// clean (merge --abort) before returning this error.
			var mce *pkggit.MergeConflictError
			if errors.As(err, &mce) {
				writePushEnvelope(cfg, "", exitMergeConflict, "merge-conflict", nil, 0, mce.TaskBranch)
				fmt.Fprintf(stderr, "tide-push: merge conflict integrating %s into %s\n", mce.TaskBranch, mce.RunBranch)
				return exitMergeConflict, true
			}
			writePushEnvelope(cfg, "", exitGenericFail, "integration-failed", nil, 0, "")
			fmt.Fprintf(stderr, "tide-push: integrate task branches: %v\n", err)
			return exitGenericFail, true
		}
	}

	// D-06/INTEG-03 verify gate: recompute completeness from git — never
	// trust controller bookkeeping (IntegratedThroughWave is progress
	// tracking, not a completeness verdict). For every branch this Job was
	// told to integrate, confirm it is NOW an ancestor of the run branch in
	// the bare repo (the same repo the push reads). Runs unconditionally
	// (D-05: no unverified push class) — an empty cfg.IntegrateTaskBranches
	// vacuously passes, and a branch already integrated by an earlier
	// wave/Job passes naturally (its tip is already an ancestor; no
	// empty-diff special case needed, verified git semantics).
	verify := verifyIntegrationComplete(bareRepoPath, cfg.Branch, cfg.IntegrateTaskBranches)
	if verify.infraErr != nil {
		writePushEnvelope(cfg, "", exitGenericFail, "integration-failed", nil, 0, "")
		fmt.Fprintf(stderr, "tide-push: verify integration completeness: %v\n", verify.infraErr)
		return exitGenericFail, true
	}
	if len(verify.branches) > 0 {
		writePushEnvelope(cfg, "", exitIntegrationMiss, "integration-incomplete",
			verify.branches, verify.total, "")
		fmt.Fprintf(stderr, "tide-push: integration-incomplete: %d branch(es) not reachable from %s\n",
			verify.total, cfg.Branch)
		return exitIntegrationMiss, true
	}

	if cfg.IntegrationOnly {
		// Per-wave integration-only mode (explicit --integration-only, set by
		// triggerWaveIntegrationJob and nothing else): task branches were
		// integrated (and verified complete above). The merge has already
		// advanced the LOCAL run branch — the only thing the next wave's
		// worktrees need, since they fork from it on the shared PVC. Nothing
		// is committed or pushed; the remote push belongs to boundary pushes.
		// A success envelope is written so wave-Job outcomes are visible to
		// the controller/metrics (Pitfall 3). Boundary pushes also carry a
		// non-empty --integrate-task-branches (the D-03/D-07 cumulative set)
		// with no artifacts, so this exit MUST key on the explicit flag —
		// keying on "branches set, no artifacts" would swallow every
		// post-task boundary push and no work would ever reach the remote.
		fmt.Fprintf(stderr,
			"tide-push: integration-only run — merged %d task branch(es) into %s locally; skipping commit/push\n",
			len(cfg.IntegrateTaskBranches), cfg.Branch)
		writePushEnvelope(cfg, "", exitSuccess, "", nil, 0, "")
		return exitSuccess, true
	}

	return 0, false
}

// resolveGitAuth reads GIT_PAT (Invariant 2 / D-B1) and determines whether
// the origin remote requires authentication (HTTPS or SSH remotes do;
// anonymous in-cluster http:// remotes do not). On a required-but-missing
// PAT it writes the envelope + stderr message and returns ("", exitCode,
// true); otherwise it returns (pat, 0, false).
func resolveGitAuth(cfg pushConfig, repo *gogit.Repository, stderr io.Writer) (string, int, bool) {
	// Read GIT_PAT once into a local variable; never log it.
	pat := os.Getenv("GIT_PAT")

	// Determine whether the push remote requires authentication by reading the
	// origin remote URL from the open repo config. This is the standard go-git
	// pattern for accessing remote config from an open repository (resolved
	// Open Question #2 from 08-RESEARCH.md).
	requirePAT := true // safe default: require PAT unless we can prove otherwise
	if repoConfig, cfgErr := repo.Config(); cfgErr != nil {
		// Config() failure means the worktree may be corrupted. Fall back to
		// the safe default (requirePAT=true = existing behavior preserved).
		fmt.Fprintf(stderr, "tide-push: repo.Config() failed (worktree may be corrupted): %v\n", cfgErr)
	} else if originRemote, ok := repoConfig.Remotes["origin"]; ok && len(originRemote.URLs) > 0 {
		remoteURL := originRemote.URLs[0]
		// Require PAT only for HTTPS or SSH (git@) remotes. Anonymous http://
		// in-cluster remotes (demo git-http-server) do not require auth.
		requirePAT = strings.HasPrefix(remoteURL, "https://") || strings.HasPrefix(remoteURL, "git@")
	}
	// If origin remote is missing or has no URLs: requirePAT stays true (safe default).

	if requirePAT && pat == "" {
		writePushEnvelope(cfg, "", exitInvariant, "missing-creds", nil, 0, "")
		fmt.Fprintf(stderr, "tide-push: GIT_PAT env is empty (required for https:// and git@ remotes)\n")
		return "", exitInvariant, true
	}
	return pat, 0, false
}

// stageArtifacts copies each cfg.ArtifactPaths entry into the worktree (D-B2
// / W11) and stages it via pkggit.AddPath. On any failure it writes the
// envelope + stderr message and returns (exitCode, true).
func stageArtifacts(cfg pushConfig, wt *gogit.Worktree, worktreeDir string, stderr io.Writer) (int, bool) {
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
			writePushEnvelope(cfg, "", exitGenericFail, "artifact-copy-failed", nil, 0, "")
			fmt.Fprintf(stderr, "tide-push: copy artifact %s -> worktree: %v\n", rel, err)
			return exitGenericFail, true
		}
		if err := pkggit.AddPath(wt, rel); err != nil {
			writePushEnvelope(cfg, "", exitGenericFail, "stage-failed", nil, 0, "")
			fmt.Fprintf(stderr, "tide-push: stage %s: %v\n", rel, err)
			return exitGenericFail, true
		}
	}
	return 0, false
}

// commitOrReuseHead determines whether the worktree has anything to commit
// after staging. On a clean tree (nothing staged, or staged content was
// byte-identical) it reuses the existing HEAD as newHash — attempting an
// empty commit would fail — and computes scanBase from cfg.LastPushedSHA
// when available (mirrors commit 8e57348's merge-only boundary handling).
// Otherwise it creates the W11 boundary commit. On any failure it writes
// the envelope + stderr message and returns exitCode/true.
func commitOrReuseHead(
	cfg pushConfig, repo *gogit.Repository, wt *gogit.Worktree, worktreeDir string,
	oldHash plumbing.Hash, stderr io.Writer,
) (plumbing.Hash, plumbing.Hash, int, bool) {
	// Determine whether staging produced anything to commit. After the artifact
	// copy+stage loop, the worktree may be clean for two reasons: (1) no
	// --artifact-paths were supplied at all (a level boundary push whose only
	// job is to land the already-integrated run branch on the remote — the
	// phase/milestone/project boundaries never stage planner artifacts), or
	// (2) the staged artifacts were byte-identical to what is already committed.
	// In BOTH cases attempting pkggit.Commit fails with "cannot create empty
	// commit: clean working tree" and the push never fires — the medium-DoD
	// boundary-push defect. This mirrors commit 8e57348 ("per-wave integration
	// job is merge-only — no empty boundary commit") at the project/milestone/
	// phase boundary: skip the empty commit, but STILL push the run branch HEAD
	// to the remote so the merged work actually leaves the cluster.
	clean, err := worktreeClean(worktreeDir)
	if err != nil {
		writePushEnvelope(cfg, "", exitGenericFail, "status-failed", nil, 0, "")
		fmt.Fprintf(stderr, "tide-push: git status %s: %v\n", worktreeDir, err)
		return plumbing.ZeroHash, plumbing.ZeroHash, exitGenericFail, true
	}

	// newHash is the commit that will be pushed. On a clean tree it is the
	// current HEAD (the run branch tip the integration merge already advanced);
	// otherwise it is the new boundary commit.
	newHash := oldHash
	// scanBase is the parent the gitleaks diff is computed against. On a fresh
	// boundary commit it is the pre-commit HEAD (oldHash). On a clean-tree push
	// it is the last SHA already on the remote (cfg.LastPushedSHA) so only the
	// newly-arriving content is scanned; if no remote anchor exists yet (first
	// push of this run branch), scanBase falls back to oldHash, yielding an
	// empty diff (HEAD vs HEAD) — the merged task commits were authored and
	// committed in-cluster, never carrying secrets the executor introduced.
	scanBase := oldHash

	if clean {
		fmt.Fprintf(stderr,
			"tide-push: clean working tree — nothing to commit; pushing already-integrated run branch %s\n", cfg.Branch)
		if cfg.LastPushedSHA != "" {
			if anchor := plumbing.NewHash(cfg.LastPushedSHA); !anchor.IsZero() {
				if _, cerr := repo.CommitObject(anchor); cerr == nil {
					scanBase = anchor
				}
			}
		}
	} else {
		// Commit. pkggit.Commit returns the new hash (W11 — single boundary
		// commit per push). Author is the fixed TIDE-bot signature.
		h, cErr := pkggit.Commit(wt, cfg.CommitMessage, agentSignature())
		if cErr != nil {
			writePushEnvelope(cfg, "", exitGenericFail, "commit-failed", nil, 0, "")
			fmt.Fprintf(stderr, "tide-push: commit failed: %v\n", cErr)
			return plumbing.ZeroHash, plumbing.ZeroHash, exitGenericFail, true
		}
		newHash = h
	}
	return newHash, scanBase, 0, false
}

// verifyResult carries the D-06/INTEG-03 verify gate's outcome: either an
// infrastructure error (infraErr set — e.g. a corrupted bare repo; treated as
// a generic/transient failure, NOT a completeness miss) or the list of
// expected branches NOT reachable from the run branch (branches, sorted;
// total is the untruncated count — writePushEnvelope applies the D-12
// truncation separately).
type verifyResult struct {
	branches []string
	total    int
	infraErr error
}

// verifyIntegrationComplete runs `git merge-base --is-ancestor <branch>
// <runBranch>` (RESEARCH Pattern 4, verified git 2.54 semantics: exit 0 =
// ancestor — including an empty-diff branch whose tip is already an
// ancestor, no special case needed; exit 1 = not an ancestor; any other exit
// = infrastructure error) for every branch in expectedBranches against
// bareRepoPath — the bare repo is the source of truth the push reads, not
// the integration worktree. An empty expectedBranches list vacuously passes.
func verifyIntegrationComplete(bareRepoPath, runBranch string, expectedBranches []string) verifyResult {
	var missing []string
	for _, br := range expectedBranches {
		cmd := exec.Command("git", "-C", bareRepoPath, "merge-base", "--is-ancestor", br, runBranch)
		if err := cmd.Run(); err != nil {
			var ee *exec.ExitError
			if errors.As(err, &ee) && ee.ExitCode() == 1 {
				missing = append(missing, br)
				continue
			}
			return verifyResult{infraErr: fmt.Errorf("merge-base --is-ancestor %s %s: %w", br, runBranch, err)}
		}
	}
	sort.Strings(missing)
	return verifyResult{branches: missing, total: len(missing)}
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
		return "", fmt.Errorf("patch %s..%s: %w", oldHash, newHash, err)
	}
	return patch.String(), nil
}

// worktreeClean reports whether the run worktree has no staged or unstaged
// changes (`git status --porcelain` is empty). A clean tree at the boundary
// push means the per-wave integration merge already advanced the run branch
// and there is nothing new to commit — the boundary push must then skip the
// (otherwise-empty) commit and push the existing HEAD. We shell out to git
// rather than go-git's wt.Status(): go-git's status is materially slower on
// large worktrees and, for a linked worktree opened with EnableDotGitCommonDir,
// the porcelain command is the same primitive internal/harness.CommitWorktree
// already relies on (D-03), keeping the empty-diff policy consistent across the
// executor commit path and the boundary push path.
func worktreeClean(worktreeDir string) (bool, error) {
	out, err := exec.Command("git", "-C", worktreeDir, "status", "--porcelain").Output()
	if err != nil {
		return false, fmt.Errorf("git status --porcelain: %w", err)
	}
	return len(strings.TrimSpace(string(out))) == 0, nil
}

// stageEnvelopeArtifacts copies each mapped envelope's planning *.md and
// children/*.json into .tide/planning/<destPrefix>/ under worktreeDir and stages
// them (DASH-02). It returns exitSuccess when every mapped envelope staged
// cleanly, or a nonzero exit code after writing the push envelope with reason
// "artifact-stage-failed" on the first failure (missing dir, no *.md, or a
// copy/stage error) — the loud-failure convention (D-03). Only the two allowed
// globs are read, so out.json/in.json are excluded by construction (D-04).
func stageEnvelopeArtifacts(
	cfg pushConfig,
	stageEnvs []EnvelopeStage,
	worktreeDir string,
	wt *gogit.Worktree,
	stderr io.Writer,
) int {
	for _, es := range stageEnvs {
		srcDir := filepath.Join(cfg.Workspace, "envelopes", es.UID)
		info, statErr := os.Stat(srcDir)
		if statErr != nil {
			if os.IsNotExist(statErr) {
				// A level can reach Succeeded via child roll-up (succession) without
				// its OWN planner Job having run — that level legitimately produced no
				// envelope on the PVC, so there is nothing to stage for it. Skip it with
				// a loud warning rather than failing the whole boundary push; the run
				// branch still pushes and the levels that DO have envelopes still stage.
				// A dir that EXISTS but is empty/corrupt remains a hard failure below (a
				// genuinely incomplete envelope is a real bug, not a missing artifact).
				fmt.Fprintf(stderr,
					"tide-push: stage-envelopes: envelope dir %s absent — level %s has no staged artifact (roll-up completion?); skipping\n",
					srcDir, es.DestPrefix)
				continue
			}
			writePushEnvelope(cfg, "", exitGenericFail, "artifact-stage-failed", nil, 0, "")
			fmt.Fprintf(stderr, "tide-push: stage-envelopes: stat envelope dir %s: %v\n", srcDir, statErr)
			return exitGenericFail
		}
		if !info.IsDir() {
			writePushEnvelope(cfg, "", exitGenericFail, "artifact-stage-failed", nil, 0, "")
			fmt.Fprintf(stderr, "tide-push: stage-envelopes: envelope path %s exists but is not a directory\n", srcDir)
			return exitGenericFail
		}

		mdMatches, gerr := filepath.Glob(filepath.Join(srcDir, "*.md"))
		if gerr != nil {
			writePushEnvelope(cfg, "", exitGenericFail, "artifact-stage-failed", nil, 0, "")
			fmt.Fprintf(stderr, "tide-push: stage-envelopes: glob *.md in %s: %v\n", srcDir, gerr)
			return exitGenericFail
		}
		if len(mdMatches) == 0 {
			// A planner-completed level always emits at least one planning *.md;
			// an empty set means the envelope is incomplete — fail loudly (D-03).
			writePushEnvelope(cfg, "", exitGenericFail, "artifact-stage-failed", nil, 0, "")
			fmt.Fprintf(stderr,
				"tide-push: stage-envelopes: no *.md under %s (a planner-completed level must have at least one)\n",
				srcDir)
			return exitGenericFail
		}
		childMatches, gerr := filepath.Glob(filepath.Join(srcDir, "children", "*.json"))
		if gerr != nil {
			writePushEnvelope(cfg, "", exitGenericFail, "artifact-stage-failed", nil, 0, "")
			fmt.Fprintf(stderr, "tide-push: stage-envelopes: glob children/*.json in %s: %v\n", srcDir, gerr)
			return exitGenericFail
		}

		// rel is the worktree-relative destination; the children/ subdirectory is
		// preserved so plan/task JSON lands under .tide/planning/<destPrefix>/children/.
		rels := make([]struct{ src, rel string }, 0, len(mdMatches)+len(childMatches))
		for _, m := range mdMatches {
			rels = append(rels, struct{ src, rel string }{
				m, filepath.Join(".tide", "planning", es.DestPrefix, filepath.Base(m)),
			})
		}
		for _, m := range childMatches {
			rels = append(rels, struct{ src, rel string }{
				m, filepath.Join(".tide", "planning", es.DestPrefix, "children", filepath.Base(m)),
			})
		}

		for _, r := range rels {
			dst := filepath.Join(worktreeDir, r.rel)
			if err := copyIntoWorktree(r.src, dst); err != nil {
				writePushEnvelope(cfg, "", exitGenericFail, "artifact-stage-failed", nil, 0, "")
				fmt.Fprintf(stderr, "tide-push: stage-envelopes: copy %s -> %s: %v\n", r.src, r.rel, err)
				return exitGenericFail
			}
			if err := pkggit.AddPath(wt, r.rel); err != nil {
				writePushEnvelope(cfg, "", exitGenericFail, "artifact-stage-failed", nil, 0, "")
				fmt.Fprintf(stderr, "tide-push: stage-envelopes: stage %s: %v\n", r.rel, err)
				return exitGenericFail
			}
		}
	}
	return exitSuccess
}

// copyIntoWorktree copies src to dst, creating parent dirs as needed.
// Uses io.Copy rather than os.Link because the source and destination
// may live on different filesystems within the PVC SubPath mount.
func copyIntoWorktree(src, dst string) (err error) {
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open src %s: %w", src, err)
	}
	defer func() { _ = in.Close() }() // read-only handle; close error is not actionable
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", filepath.Dir(dst), err)
	}
	out, err := os.Create(dst)
	if err != nil {
		return fmt.Errorf("create dst %s: %w", dst, err)
	}
	defer func() {
		// A failed close on the destination can mean a truncated/unflushed
		// copy; surface it if the copy itself succeeded.
		if cerr := out.Close(); cerr != nil && err == nil {
			err = fmt.Errorf("close dst %s: %w", dst, cerr)
		}
	}()
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
//
// missingBranches/missingTotal/conflictBranch are the Phase 34 D-09/D-12
// detail fields; pass (nil, 0, "") when they do not apply to this outcome.
// missingBranches is truncated to missingBranchesLimit here (sorted order is
// the caller's responsibility — the verify gate already collects them in
// scan order over the sorted --integrate-task-branches CSV).
func writePushEnvelope(
	cfg pushConfig, headSHA string, exit int, reason string,
	missingBranches []string, missingTotal int, conflictBranch string,
) {
	truncated := missingBranches
	if len(truncated) > missingBranchesLimit {
		truncated = truncated[:missingBranchesLimit]
	}
	pr := pushResult{
		APIVersion:      envelopeAPIVersion,
		Kind:            envelopeKind,
		ProjectUID:      cfg.ProjectUID,
		Branch:          cfg.Branch,
		HeadSHA:         headSHA,
		ExitCode:        exit,
		Reason:          reason,
		MissingBranches: truncated,
		MissingTotal:    missingTotal,
		ConflictBranch:  conflictBranch,
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

// writeCloneEnvelope writes the clone-result JSON (Kind CloneResult) to
// /dev/termination-log (terminationMessagePath) AND, when cfg.ProjectUID is
// non-empty, to <workspace>/envelopes/clone/<project-uid>.json. Best-effort on
// both writes: failures are logged but do not change the exit code (the caller
// has already decided what to return).
//
// The clone path is deliberately distinct from writePushEnvelope's
// envelopes/push/ so a later boundary push never overwrites clone provenance.
// baseSHA is the resolved base commit on success (empty on failure and on the
// no-run-branch legacy path); reason is empty on success and
// baseref-unresolvable / clone-failed on the failure paths (Phase 35 D-05/D-11).
func writeCloneEnvelope(cfg pushConfig, baseSHA string, exit int, reason string) {
	pr := pushResult{
		APIVersion: envelopeAPIVersion,
		Kind:       envelopeKindClone,
		ProjectUID: cfg.ProjectUID,
		ExitCode:   exit,
		Reason:     reason,
		BaseSHA:    baseSHA,
		BaseRef:    cfg.BaseRef,
	}
	data, err := json.Marshal(pr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "tide-push: marshal clone envelope: %v\n", err)
		return
	}

	// W-1 surface: /dev/termination-log so the ProjectReconciler can read the
	// reason without mounting the PVC. Best-effort (not writable off-cluster).
	if err := os.WriteFile(terminationMessagePath, data, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "tide-push: write terminationMessage (best-effort): %v\n", err)
	}

	// PVC copy for consumers that mount the PVC. Skip when ProjectUID is unset.
	if cfg.ProjectUID == "" {
		return
	}
	envDir := filepath.Join(cfg.Workspace, "envelopes", "clone")
	if err := os.MkdirAll(envDir, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "tide-push: mkdir clone envelope dir %s: %v\n", envDir, err)
		return
	}
	envPath := filepath.Join(envDir, cfg.ProjectUID+".json")
	if err := os.WriteFile(envPath, data, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "tide-push: write clone envelope %s: %v\n", envPath, err)
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
