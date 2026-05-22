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
	"errors"
	"fmt"

	gogit "github.com/go-git/go-git/v5"
	gitclient "github.com/go-git/go-git/v5/plumbing/transport/http"
)

// Fetch updates the local bare repo from origin using HTTPS+PAT auth.
//
// Per ART-03, Fetch is the lease-refresh entrypoint: after a
// PushLeaseFailed condition (D-B6), the controller / push Job calls Fetch
// to learn the remote's current head SHA, then reissues the push with the
// refreshed lease. Phase 3's orchestrator does not invoke Fetch directly
// (push-time --force-with-lease is the detection mechanism), but the
// symbol is shipped so the API surface is complete and Phase 4 / v1.x
// callers (a `tide refresh-remote` CLI verb, dashboard refresh action)
// land without an API break.
//
// go-git's NoErrAlreadyUpToDate sentinel is treated as success (no remote
// changes is the normal lease-refresh outcome).
func Fetch(ctx context.Context, repo *gogit.Repository, pat string) error {
	err := repo.FetchContext(ctx, &gogit.FetchOptions{
		Auth: &gitclient.BasicAuth{
			Username: "x-access-token",
			Password: pat,
		},
	})
	if err != nil && !errors.Is(err, gogit.NoErrAlreadyUpToDate) {
		return fmt.Errorf("git fetch: %w", err)
	}
	return nil
}
