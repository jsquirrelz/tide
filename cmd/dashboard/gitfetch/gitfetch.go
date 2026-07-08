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

// Package gitfetch is the dashboard's cross-namespace-safe read path for
// planning artifacts (DASH-01, D-01). The manager cannot mount per-Project
// PVCs, so the artifact viewer reads the `.tide/` tree at the tip of a run
// branch directly from the git remote.
//
// The package is deliberately Kubernetes-free: it takes a repo URL, a branch,
// and pass-through credentials. Secret resolution stays in the API handler
// (plan 37-07), which keeps this package pure git + caching and fully testable
// against local fixture repos over the file:// transport.
//
// go-git's shallow-clone sharp edges are designed around, not patched around
// (RESEARCH Pattern 3): a fresh shallow clone per tip SHA, SingleBranch,
// no tag following, NoCheckout, and in-memory storage discarded after tree
// extraction. See GoGitFetcher.Fetch for the fresh-clone-per-SHA rationale.
package gitfetch

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	gitclient "github.com/go-git/go-git/v5/plumbing/transport/http"
	"github.com/go-git/go-git/v5/storage/memory"
)

// tideRoot is the single subtree the dashboard reads. Everything outside it is
// project source and is never returned.
const tideRoot = ".tide"

// File is a single planning artifact blob read from the remote. Path is
// repo-relative (e.g. ".tide/planning/milestone/m1/MILESTONE.md"); Name is its
// base name.
type File struct {
	Name    string
	Path    string
	Content []byte
}

// Auth carries per-request git credentials. It flows through call frames only
// (T-37-03-01): it is never stored in the Store's LRU and never logged. An
// empty Password means anonymous access (in-cluster http:// remotes).
type Auth struct {
	Username string
	Password string
}

// Fetcher is the seam between the caching Store and the concrete git read
// mechanism. GoGitFetcher is the v1 implementation; the interface exists so an
// exec-`git` fallback can be swapped in without touching the Store (RESEARCH
// Pattern 3 "wrap the fetcher in an interface").
type Fetcher interface {
	// Tip returns the branch tip SHA via ls-remote — cheap, no clone.
	Tip(ctx context.Context, repoURL, branch string, auth *Auth) (string, error)
	// Fetch shallow-clones the branch tip and returns its SHA plus every blob
	// under .tide/. A repo with no .tide/ directory returns (sha, nil, nil):
	// absence is data, not an error.
	Fetch(ctx context.Context, repoURL, branch string, auth *Auth) (sha string, files []File, err error)
}

// GoGitFetcher reads artifacts using go-git shallow clones into in-memory
// storage. The zero value is ready to use.
type GoGitFetcher struct{}

var _ Fetcher = GoGitFetcher{}

// basicAuth mirrors pkg/git/clone.go: HTTPS+PAT via BasicAuth with Username
// defaulting to the "x-access-token" GitHub convention (GitLab/Gitea accept any
// non-empty username when the password is a PAT). A nil Auth or an empty
// Password yields anonymous access — go-git receives a nil AuthMethod so no
// auth header is emitted to public/in-cluster http:// remotes (RESEARCH
// Pitfall 1, mirrored from pkg/git/clone.go's pat == "" guard).
func basicAuth(auth *Auth) *gitclient.BasicAuth {
	if auth == nil || auth.Password == "" {
		return nil
	}
	username := auth.Username
	if username == "" {
		username = "x-access-token"
	}
	return &gitclient.BasicAuth{Username: username, Password: auth.Password}
}

// Tip returns the tip SHA of refs/heads/<branch> via the remote List
// (ls-remote) API — no objects are transferred. Used by the Store to decide
// whether a cached tree is still current before paying for a clone.
func (GoGitFetcher) Tip(ctx context.Context, repoURL, branch string, auth *Auth) (string, error) {
	remote := gogit.NewRemote(memory.NewStorage(), &config.RemoteConfig{
		Name: "origin",
		URLs: []string{repoURL},
	})
	opts := &gogit.ListOptions{}
	if a := basicAuth(auth); a != nil {
		opts.Auth = a
	}
	refs, err := remote.ListContext(ctx, opts)
	if err != nil {
		return "", fmt.Errorf("gitfetch ls-remote %s: %w", repoURL, err)
	}
	target := plumbing.NewBranchReferenceName(branch)
	for _, ref := range refs {
		if ref.Name() == target {
			return ref.Hash().String(), nil
		}
	}
	return "", fmt.Errorf("gitfetch ls-remote %s: branch %q not found", repoURL, branch)
}

// Fetch shallow-clones the branch tip into in-memory storage and extracts every
// blob under .tide/.
//
// Fresh-clone-per-SHA rule: the returned in-memory clone is garbage after this
// call — it is never retained and never fetched into again. go-git's shallow
// clones cannot be safely deepened: a subsequent pull/fetch into a Depth:1
// clone surfaces "object not found" failures (go-git#305, src-d/go-git#900).
// A new tip SHA therefore always gets a brand-new shallow clone; the Store
// keys its cache on the SHA so a stale clone is never reused.
func (GoGitFetcher) Fetch(ctx context.Context, repoURL, branch string, auth *Auth) (string, []File, error) {
	opts := &gogit.CloneOptions{
		URL:           repoURL,
		ReferenceName: plumbing.NewBranchReferenceName(branch),
		SingleBranch:  true,
		Depth:         1,
		Tags:          gogit.NoTags,
		NoCheckout:    true,
	}
	if a := basicAuth(auth); a != nil {
		opts.Auth = a
	}

	repo, err := gogit.CloneContext(ctx, memory.NewStorage(), nil, opts)
	if err != nil {
		return "", nil, fmt.Errorf("gitfetch clone %s: %w", repoURL, err)
	}
	head, err := repo.Head()
	if err != nil {
		return "", nil, fmt.Errorf("gitfetch head %s: %w", repoURL, err)
	}
	sha := head.Hash().String()

	commit, err := repo.CommitObject(head.Hash())
	if err != nil {
		return "", nil, fmt.Errorf("gitfetch commit %s: %w", repoURL, err)
	}
	tree, err := commit.Tree()
	if err != nil {
		return "", nil, fmt.Errorf("gitfetch tree %s: %w", repoURL, err)
	}

	tideTree, err := tree.Tree(tideRoot)
	if err != nil {
		// Absence of .tide/ is data, not an error (D-01): a run branch that has
		// not yet accrued planning artifacts is a valid, empty result.
		if errors.Is(err, object.ErrDirectoryNotFound) {
			return sha, nil, nil
		}
		return "", nil, fmt.Errorf("gitfetch tide-subtree %s: %w", repoURL, err)
	}

	var files []File
	walkErr := tideTree.Files().ForEach(func(f *object.File) error {
		content, err := readBlob(f)
		if err != nil {
			return err
		}
		full := path.Join(tideRoot, f.Name)
		files = append(files, File{
			Name:    path.Base(full),
			Path:    full,
			Content: content,
		})
		return nil
	})
	if walkErr != nil {
		return "", nil, fmt.Errorf("gitfetch walk %s: %w", repoURL, walkErr)
	}
	return sha, files, nil
}

// readBlob reads a tree file's full contents. f.Reader streams the blob without
// forcing a UTF-8 conversion (unlike object.File.Contents), so binary artifacts
// survive byte-for-byte.
func readBlob(f *object.File) ([]byte, error) {
	r, err := f.Reader()
	if err != nil {
		return nil, err
	}
	defer func() { _ = r.Close() }()
	return io.ReadAll(r)
}
