package git

import (
	"context"
	"fmt"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	gitclient "github.com/go-git/go-git/v5/plumbing/transport/http"
)

// Push pushes the local ref refs/heads/<branch> to origin/refs/heads/<branch>
// using HTTPS+PAT auth, with --force-with-lease semantics conditioned on
// lastPushedSHA.
//
// D-B6 (CONTEXT.md): structural mitigation for Pitfall 13
// (TIDE-orchestrated artifacts overwrite manual work). When lastPushedSHA
// is non-empty, --force-with-lease is set against
// refs/heads/<branch>:lastPushedSHA — the remote refuses the push if its
// current ref no longer matches that hash (someone else pushed in the
// interim). When lastPushedSHA is empty (first push to a fresh per-run
// branch), the lease is omitted entirely: per RESEARCH §"Pitfall 2",
// passing plumbing.NewHash("") would yield plumbing.ZeroHash which is
// "ref must not exist" — almost always desirable on first push, but
// callers may have created the ref via an earlier failed push attempt,
// so omitting the lease lets the natural push semantics surface the
// conflict explicitly.
//
// pkg/git does NOT enforce a "never push to main" policy here — that's a
// caller policy (D-B6 locks the per-run-branch name at
// cmd/tide-push's level, per plan 03-06). Keeping pkg/git generic lets
// future internal/git/{host}/ adapters wrap the same primitive.
//
// Authentication is HTTPS+PAT via the Username "x-access-token"
// convention. Cancellation propagates through ctx.
func Push(ctx context.Context, repo *gogit.Repository, branch, lastPushedSHA, pat string) error {
	if repo == nil {
		return fmt.Errorf("git push: nil repo")
	}
	if branch == "" {
		return fmt.Errorf("git push: empty branch")
	}

	refSpec := config.RefSpec(fmt.Sprintf("refs/heads/%s:refs/heads/%s", branch, branch))
	opts := &gogit.PushOptions{
		RemoteName: "origin",
		RefSpecs:   []config.RefSpec{refSpec},
		Auth: &gitclient.BasicAuth{
			Username: "x-access-token",
			Password: pat,
		},
	}
	if lastPushedSHA != "" {
		opts.ForceWithLease = &gogit.ForceWithLease{
			RefName: plumbing.NewBranchReferenceName(branch),
			Hash:    plumbing.NewHash(lastPushedSHA),
		}
	}
	if err := repo.PushContext(ctx, opts); err != nil {
		return fmt.Errorf("git push %s (lease=%q): %w", branch, lastPushedSHA, err)
	}
	return nil
}
