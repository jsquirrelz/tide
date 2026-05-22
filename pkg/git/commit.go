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
	"fmt"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
)

// AddPath stages a single file at relpath (relative to the worktree root)
// for the next commit. Thin wrapper around go-git's Worktree.Add — exists
// to give cmd/tide-push (plan 03-06) a stable seam for D-B2 / W11 staging
// of planner-emitted Markdown artifacts before the boundary commit.
//
// D-B4 (CONTEXT.md): the worktree is the per-Task working tree whose
// index is independent of any sibling Task's index. Per-call staging is
// safe under wave parallelism.
//
// D-B2 (CONTEXT.md): the push Job authors a single commit per level
// boundary; AddPath is called once per artifact file that flows into
// that commit (MILESTONE.md / phase brief / PLAN.md / per-Task diffs).
func AddPath(wt *gogit.Worktree, relpath string) error {
	if wt == nil {
		return fmt.Errorf("git add %s: nil worktree", relpath)
	}
	if _, err := wt.Add(relpath); err != nil {
		return fmt.Errorf("git add %s: %w", relpath, err)
	}
	return nil
}

// Commit creates a commit on wt's current branch with msg and the given
// author signature. Returns the new commit's hash.
//
// D-B2 (CONTEXT.md): the boundary commit at every level (plan / phase /
// milestone / project) is authored by the push Job via this function;
// the structured commit message is the caller's responsibility (per W11).
//
// The author signature is supplied per-call rather than baked into this
// package because the TIDE-bot identity (name + email) is a runtime
// configuration concern owned by the caller (cmd/tide-push reads it from
// Helm values / env). Keeping it caller-supplied also lets tests use a
// deterministic synthetic identity.
func Commit(wt *gogit.Worktree, msg string, author object.Signature) (plumbing.Hash, error) {
	if wt == nil {
		return plumbing.ZeroHash, fmt.Errorf("git commit: nil worktree")
	}
	h, err := wt.Commit(msg, &gogit.CommitOptions{
		Author: &author,
	})
	if err != nil {
		return plumbing.ZeroHash, fmt.Errorf("git commit %q: %w", msg, err)
	}
	return h, nil
}
