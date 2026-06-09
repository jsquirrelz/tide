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
)

// EnsureRunBranch ensures refs/heads/<branch> exists in the bare repo at
// bareRepoPath, creating it at the current HEAD (the default branch tip) when
// absent. It is idempotent: an existing run branch is left untouched so a Job
// retry or a second reconcile does not reset run history.
//
// This is the D-B6 "create the run branch" step that was missing before Phase
// 10 Option B: the ProjectReconciler derives the tide/run-<project>-<unix>
// name (project_controller.go) but no code created the ref, so the executor's
// worktree-add against it failed with "couldn't find remote ref". Creating the
// ref here — before any executor worktree is added — closes that gap.
//
// Ref creation is a pure go-git storer operation: no working tree and no git
// CLI are required, so this is safe to call from any pod with PVC access
// (the clone Job, per D-B7).
func EnsureRunBranch(bareRepoPath, branch string) error {
	if bareRepoPath == "" {
		return fmt.Errorf("git ensure-run-branch: empty bareRepoPath")
	}
	if branch == "" {
		return fmt.Errorf("git ensure-run-branch: empty branch")
	}

	repo, err := gogit.PlainOpen(bareRepoPath)
	if err != nil {
		return fmt.Errorf("git ensure-run-branch: open %s: %w", bareRepoPath, err)
	}

	refName := plumbing.NewBranchReferenceName(branch)
	if _, err := repo.Reference(refName, false); err == nil {
		return nil // already exists — idempotent no-op
	}

	head, err := repo.Head()
	if err != nil {
		return fmt.Errorf("git ensure-run-branch: resolve HEAD of %s: %w", bareRepoPath, err)
	}

	if err := repo.Storer.SetReference(plumbing.NewHashReference(refName, head.Hash())); err != nil {
		return fmt.Errorf("git ensure-run-branch: create %s @ %s: %w", branch, head.Hash(), err)
	}
	return nil
}
