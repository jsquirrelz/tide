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

package git

import (
	"errors"
	"fmt"
	"strings"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
)

// ErrBaseRefUnresolvable is the sentinel wrapped by every unresolvable-baseRef
// error from EnsureRunBranch/resolveBaseRef. Callers (cmd/tide-push runClone)
// classify it via errors.Is to emit the exit-2 baseref-unresolvable envelope
// the ProjectReconciler keys its BaseRefUnresolvable condition on (Phase 35
// D-05/D-06). It never carries the ref text itself — the wrapping fmt.Errorf
// does, via %q.
var ErrBaseRefUnresolvable = errors.New("baseref unresolvable")

// EnsureRunBranch ensures refs/heads/<branch> exists in the bare repo at
// bareRepoPath, creating it when absent and returning the hash the run branch
// points at. It is idempotent: an existing run branch is left untouched (and
// its tip returned) so a Job retry or a second reconcile does not reset run
// history — the existence check runs FIRST, before any baseRef resolution, so
// a retry carrying a now-unresolvable baseRef against an existing run branch
// still succeeds (Phase 35 Pitfall 6, which is what makes D-09/D-10 free).
//
// baseRef selects the commit the run branch is created from (Phase 35 BASE-01):
//   - "" (the default) — the bare clone's HEAD, the remote default-branch tip
//     (unchanged pre-Phase-35 behavior).
//   - otherwise — resolveBaseRef's explicit chain (D-01/D-02/D-03): a
//     refs/-qualified ref verbatim, an existing branch (local or
//     refs/remotes/origin/*), a tag (annotated tags peel to the commit), or a
//     full 40-hex commit SHA reachable from a fetched branch or tag. An
//     unresolvable value returns an error wrapping ErrBaseRefUnresolvable.
//
// This is also the D-B6 "create the run branch" step (Phase 10 Option B): the
// ProjectReconciler derives the tide/run-<project>-<unix> name but no code
// created the ref, so the executor's worktree-add against it failed with
// "couldn't find remote ref". Creating the ref here — before any executor
// worktree is added — closes that gap.
//
// Ref creation is a pure go-git storer operation: no working tree and no git
// CLI are required, so this is safe to call from any pod with PVC access
// (the clone Job, per D-B7).
func EnsureRunBranch(bareRepoPath, branch, baseRef string) (plumbing.Hash, error) {
	if bareRepoPath == "" {
		return plumbing.ZeroHash, fmt.Errorf("git ensure-run-branch: empty bareRepoPath")
	}
	if branch == "" {
		return plumbing.ZeroHash, fmt.Errorf("git ensure-run-branch: empty branch")
	}

	repo, err := gogit.PlainOpen(bareRepoPath)
	if err != nil {
		return plumbing.ZeroHash, fmt.Errorf("git ensure-run-branch: open %s: %w", bareRepoPath, err)
	}

	// Idempotent early-return — MUST stay ahead of any baseRef resolution
	// (Pitfall 6): a retried clone Job whose run branch already exists returns
	// the existing tip without re-resolving.
	refName := plumbing.NewBranchReferenceName(branch)
	if existing, err := repo.Reference(refName, false); err == nil {
		return existing.Hash(), nil
	}

	// Determine the commit the run branch is created from.
	var target plumbing.Hash
	if baseRef == "" {
		head, err := repo.Head()
		if err != nil {
			return plumbing.ZeroHash, fmt.Errorf("git ensure-run-branch: resolve HEAD of %s: %w", bareRepoPath, err)
		}
		target = head.Hash()
	} else {
		h, err := resolveBaseRef(repo, baseRef)
		if err != nil {
			return plumbing.ZeroHash, err
		}
		target = h
	}

	if err := repo.Storer.SetReference(plumbing.NewHashReference(refName, target)); err != nil {
		return plumbing.ZeroHash, fmt.Errorf("git ensure-run-branch: create %s @ %s: %w", branch, target, err)
	}
	return target, nil
}

// resolveBaseRef implements the D-01/D-02/D-03 resolution chain against a
// production-shaped bare clone. It deliberately does NOT use
// repo.ResolveRevision: that accepts HEAD, short SHAs, and ~/^ suffixes —
// exactly the forms the contract rejects.
//
// Order:
//  1. refs/-qualified (D-02): resolved verbatim, before the chain. When a
//     refs/heads/<name> form misses (non-default branches are not stored under
//     refs/heads/* in a non-mirror clone), fall back to
//     refs/remotes/origin/<name> so the disambiguation escape hatch works.
//  2. branch: refs/heads/<ref> then refs/remotes/origin/<ref> — the second is
//     where ALL non-default branches live in TIDE's bare clone (Pitfall 1).
//  3. tag: refs/tags/<ref>; annotated tags peel to the commit (D-11).
//  4. SHA: an exact 40-hex value whose object exists locally.
//
// After any successful non-tag arm the resolved hash is confirmed to name a
// commit (a refs/-verbatim ref could name a blob/tree).
func resolveBaseRef(repo *gogit.Repository, ref string) (plumbing.Hash, error) {
	lookup := func(name plumbing.ReferenceName) (plumbing.Hash, bool) {
		r, err := repo.Reference(name, true)
		if err != nil {
			return plumbing.ZeroHash, false
		}
		return r.Hash(), true
	}

	var h plumbing.Hash
	var found bool
	switch {
	case strings.HasPrefix(ref, "refs/"): // D-02 verbatim, before the chain
		h, found = lookup(plumbing.ReferenceName(ref))
		if !found && strings.HasPrefix(ref, "refs/heads/") {
			// Non-default branches live under refs/remotes/origin/* in a
			// non-mirror bare clone (default fetch refspec) — map the qualified
			// branch form there so the escape hatch resolves them too.
			h, found = lookup(plumbing.NewRemoteReferenceName("origin", strings.TrimPrefix(ref, "refs/heads/")))
		}
	default:
		h, found = lookup(plumbing.NewBranchReferenceName(ref))
		if !found {
			h, found = lookup(plumbing.NewRemoteReferenceName("origin", ref))
		}
		if !found {
			h, found = lookup(plumbing.NewTagReferenceName(ref))
		}
		if !found && plumbing.IsHash(ref) {
			h, found = plumbing.NewHash(ref), true
		}
	}

	if !found {
		return plumbing.ZeroHash, fmt.Errorf(
			"unable to resolve %q to a commit SHA: baseRef must be an existing branch, "+
				"tag, or full 40-hex commit SHA reachable from a branch or tag: %w",
			ref, ErrBaseRefUnresolvable)
	}

	// Peel annotated tags (D-11: stamp the peeled commit). TagObject succeeds
	// iff the hash names a tag object; a lightweight tag / branch / SHA misses
	// here and falls through to the commit check.
	if tag, err := repo.TagObject(h); err == nil {
		c, cerr := tag.Commit()
		if cerr != nil {
			return plumbing.ZeroHash, fmt.Errorf("unable to resolve %q to a commit SHA: peel annotated tag: %w", ref, cerr)
		}
		return c.Hash, nil
	}

	// Verify the hash names a commit reachable in the local object DB (D-03):
	// a full clone fetches all heads + tags, so object presence ≈ reachability.
	if _, err := repo.CommitObject(h); err != nil {
		return plumbing.ZeroHash, fmt.Errorf(
			"unable to resolve %q to a commit SHA: object %s is not a commit reachable from a fetched branch or tag: %w",
			ref, h, ErrBaseRefUnresolvable)
	}
	return h, nil
}
