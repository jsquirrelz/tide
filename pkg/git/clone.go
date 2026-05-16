package git

import (
	"context"
	"fmt"

	gogit "github.com/go-git/go-git/v5"
	gitclient "github.com/go-git/go-git/v5/plumbing/transport/http"
)

// Clone performs a bare HTTPS+PAT clone of repoURL into destDir.
//
// destDir MUST NOT already exist (or MUST be an empty directory) — go-git's
// PlainCloneContext writes the bare repo layout (HEAD, refs/, objects/, ...)
// at destDir directly when bare=true. Callers (ProjectReconciler's
// tide-clone-{project-uid} Job, per CONTEXT.md D-B7) point destDir at
// /workspace/repo.git on the per-Project shared PVC so subsequent
// AddWorktree calls (D-B4) can create per-Task working trees alongside.
//
// Authentication is HTTPS+PAT via the &http.BasicAuth pattern with
// Username "x-access-token" (the GitHub convention; GitLab/Gitea accept any
// non-empty Username when Password is a PAT). Per ART-05, this is the
// default and only v1.0 auth path; SSH is deferred.
//
// Cancellation propagates through ctx — the calling Job's
// activeDeadlineSeconds is the outer wall-clock cap.
func Clone(ctx context.Context, repoURL, destDir, pat string) (*gogit.Repository, error) {
	repo, err := gogit.PlainCloneContext(ctx, destDir, true /* bare */, &gogit.CloneOptions{
		URL: repoURL,
		Auth: &gitclient.BasicAuth{
			Username: "x-access-token",
			Password: pat,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("git clone %s: %w", repoURL, err)
	}
	return repo, nil
}
