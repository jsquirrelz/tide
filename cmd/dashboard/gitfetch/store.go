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

package gitfetch

import (
	"context"
	"fmt"
	"time"

	lru "github.com/hashicorp/golang-lru/v2"
)

// DefaultMaxEntries is the recommended LRU bound. Planning artifact trees are
// KBs–100s of KBs; 32 tip-branch trees is a comfortable working set for a
// dashboard serving a handful of concurrently-viewed projects (CONTEXT
// discretion). Callers may size up for larger deployments.
const DefaultMaxEntries = 32

// cacheKey identifies one immutable artifact snapshot. It carries the tip SHA,
// so a new tip is a distinct key and never collides with a stale tree.
//
// Credential-handling rule (T-37-03-01, R-02): neither the key nor the cached
// value carries an Auth field. Credentials flow through the Tip/Fetch call
// frames only and are never retained in the cache.
type cacheKey struct {
	RepoURL string
	Branch  string
	SHA     string
}

// cacheValue is the rederivable snapshot stored per key. fetchedAt is retained
// for observability/eviction reasoning only — it holds no credentials.
type cacheValue struct {
	files     []File
	fetchedAt time.Time
}

// Store is a bounded, in-process artifact cache over a Fetcher. It is
// rederivable by construction — nothing here is persisted to CRD status or
// ConfigMaps (RESEARCH: "The artifact LRU is in-process memory — rederivable").
//
// Lookup flow: a cheap ls-remote Tip resolves the current SHA, the (repo,
// branch, sha) key is checked against the LRU, and only a miss pays for a
// shallow Fetch. Because the key includes the tip SHA, an advanced branch is
// always a miss — the fresh-clone-per-SHA invariant (see GoGitFetcher.Fetch)
// holds end to end.
type Store struct {
	fetcher Fetcher
	cache   *lru.Cache[cacheKey, cacheValue]
}

// NewStore builds a Store over f with an LRU bound of maxEntries. maxEntries
// must be > 0; see DefaultMaxEntries for the recommended value.
func NewStore(f Fetcher, maxEntries int) (*Store, error) {
	if maxEntries <= 0 {
		return nil, fmt.Errorf("gitfetch: maxEntries must be > 0, got %d", maxEntries)
	}
	cache, err := lru.New[cacheKey, cacheValue](maxEntries)
	if err != nil {
		return nil, fmt.Errorf("gitfetch: new lru: %w", err)
	}
	return &Store{fetcher: f, cache: cache}, nil
}

// Artifacts returns the .tide/ tree at the current tip of branch, serving from
// the LRU on a hit and shallow-fetching on a miss. auth is passed through to
// Tip/Fetch call frames only and is never cached (T-37-03-01).
//
// This is the exact call plan 37-07's artifacts handler makes; the handler
// resolves auth from the per-project creds Secret via a typed clientset, so the
// gitfetch package stays Kubernetes-free.
func (s *Store) Artifacts(ctx context.Context, repoURL, branch string, auth *Auth) (string, []File, error) {
	sha, err := s.fetcher.Tip(ctx, repoURL, branch, auth)
	if err != nil {
		return "", nil, err
	}
	key := cacheKey{RepoURL: repoURL, Branch: branch, SHA: sha}
	if v, ok := s.cache.Get(key); ok {
		return sha, v.files, nil
	}
	fetchedSHA, files, err := s.fetcher.Fetch(ctx, repoURL, branch, auth)
	if err != nil {
		return "", nil, err
	}
	// The fetched SHA is authoritative for what we actually read; key the cache
	// on it so a tip that advanced between Tip and Fetch is stored under its own
	// key rather than masquerading as the older ls-remote SHA.
	key.SHA = fetchedSHA
	s.cache.Add(key, cacheValue{files: files, fetchedAt: time.Now()})
	return fetchedSHA, files, nil
}
