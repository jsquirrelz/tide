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
	"context"
	"fmt"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	gitclient "github.com/go-git/go-git/v5/plumbing/transport/http"
)

// RemoteBranchTip resolves the origin remote and lists its refs via an
// authenticated ListContext call, using the same x-access-token BasicAuth
// shape as Push (auth is tolerated on file-path transports — the whole
// existing test suite relies on that). It returns the hash of
// refs/heads/<branch> when the ref is present in the listing, or
// (plumbing.ZeroHash, false, nil) when the branch does not exist on the
// remote (a normal outcome, not an error — first-push semantics rely on
// distinguishing "not found" from "read failed").
//
// This is the read-side primitive callers use to refresh a possibly-stale
// --force-with-lease anchor against the remote's actual state (Phase 47
// gap-closure, boundary-push stale-lease defect). pkg/git enforces no
// refresh policy here — that is caller policy, matching Push's own policy
// note above.
func RemoteBranchTip(ctx context.Context, repo *gogit.Repository, branch, pat string) (plumbing.Hash, bool, error) {
	if repo == nil {
		return plumbing.ZeroHash, false, fmt.Errorf("git remote branch tip: nil repo")
	}
	if branch == "" {
		return plumbing.ZeroHash, false, fmt.Errorf("git remote branch tip: empty branch")
	}

	remote, err := repo.Remote("origin")
	if err != nil {
		return plumbing.ZeroHash, false, fmt.Errorf("git remote branch tip: origin remote: %w", err)
	}

	refs, err := remote.ListContext(ctx, &gogit.ListOptions{
		Auth: &gitclient.BasicAuth{
			Username: "x-access-token",
			Password: pat,
		},
	})
	if err != nil {
		return plumbing.ZeroHash, false, fmt.Errorf("git remote branch tip: list %s: %w", branch, err)
	}

	target := plumbing.NewBranchReferenceName(branch)
	for _, ref := range refs {
		if ref.Name() == target {
			return ref.Hash(), true, nil
		}
	}
	return plumbing.ZeroHash, false, nil
}
