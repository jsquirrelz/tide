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

package harness

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/go-git/go-git/v5/plumbing"

	pkgdispatch "github.com/jsquirrelz/tide/pkg/dispatch"
	pkggit "github.com/jsquirrelz/tide/pkg/git"
)

// CommitWorktree stages all changes in worktreeDir and commits them under the
// TIDE agent identity. Returns the HEAD SHA after the commit, an isEmpty flag
// (true when there are no changes to commit), and any error.
//
// Identity comes from pkggit.AgentIdentity — read from TIDE_AGENT_NAME /
// TIDE_AGENT_EMAIL env vars, falling back to the compiled default
// "TIDE Agent <tide-agent@tideproject.k8s>". This is the same source the
// integrate merge commit and the tide-push boundary commit use, so the
// default lives in exactly one place (D-04 / SIGN-01).
//
// Empty-diff policy (D-03 / SC-2): if git status --porcelain reports nothing,
// CommitWorktree returns (ZeroHash, true, nil) — no empty commit is created.
// The caller is expected to translate isEmpty=true into an explicit task
// failure (ExitCode=1, Result="empty-diff").
func CommitWorktree(worktreeDir, taskUID string) (plumbing.Hash, bool, error) {
	// Step 1: check for changes.
	statusOut, err := exec.Command("git", "-C", worktreeDir, "status", "--porcelain").Output()
	if err != nil {
		return plumbing.ZeroHash, false, fmt.Errorf("CommitWorktree: git status: %w", err)
	}
	if len(strings.TrimSpace(string(statusOut))) == 0 {
		return plumbing.ZeroHash, true, nil
	}

	// Step 2: stage all changes.
	if addOut, addErr := exec.Command("git", "-C", worktreeDir, "add", "-A").CombinedOutput(); addErr != nil {
		return plumbing.ZeroHash, false, fmt.Errorf("CommitWorktree: git add -A: %w: %s", addErr, addOut)
	}

	// Step 3: commit with the TIDE agent identity.
	agentName, agentEmail := pkggit.AgentIdentity()
	msg := fmt.Sprintf("tide: task %s authored", taskUID)
	// Note: -c flags must precede -C and the subcommand.
	commitArgs := []string{
		"-c", "user.name=" + agentName,
		"-c", "user.email=" + agentEmail,
		"-C", worktreeDir,
		"commit", "-m", msg,
	}
	if commitOut, commitErr := exec.Command("git", commitArgs...).CombinedOutput(); commitErr != nil {
		return plumbing.ZeroHash, false, fmt.Errorf("CommitWorktree: git commit: %w: %s", commitErr, commitOut)
	}

	// Step 4: read HEAD SHA.
	headOut, err := exec.Command("git", "-C", worktreeDir, "rev-parse", "HEAD").Output()
	if err != nil {
		return plumbing.ZeroHash, false, fmt.Errorf("CommitWorktree: rev-parse HEAD: %w", err)
	}
	return plumbing.NewHash(strings.TrimSpace(string(headOut))), false, nil
}

// ChangedFileManifest returns the bounded git `--name-status` manifest for
// worktreeDir's HEAD commit (the commit [CommitWorktree] just created), plus
// the pre-truncation total count. At most max entries are returned; the
// caller passes the returned total into [pkgdispatch.RunEvidence.ChangedFileTotal]
// so callers can observe truncation even when ChangedFiles itself was cut.
//
// Rename lines ("R100  old  new") use the new path — the manifest reports
// current file identity, not history. Only path + status letter are
// returned, never a diff (T-50-01: the manifest must stay small by
// construction, never carry content).
func ChangedFileManifest(worktreeDir string, max int) ([]pkgdispatch.ChangedFile, int, error) {
	out, err := exec.Command("git", "-C", worktreeDir, "show", "--name-status", "--format=").Output()
	if err != nil {
		return nil, 0, fmt.Errorf("ChangedFileManifest: git show --name-status: %w", err)
	}

	var all []pkgdispatch.ChangedFile
	for _, line := range strings.Split(strings.TrimRight(string(out), "\n"), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		// Rename/copy lines are "R100\told\tnew" (or M/A/D "M\tpath") — the
		// last field is always the current path.
		all = append(all, pkgdispatch.ChangedFile{
			Status: fields[0],
			Path:   fields[len(fields)-1],
		})
	}

	total := len(all)
	if max > 0 && len(all) > max {
		all = all[:max]
	}
	return all, total, nil
}
