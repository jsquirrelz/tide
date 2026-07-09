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
	"errors"
	"sync"
	"testing"
)

// fakeFetcher is a deterministic Fetcher driven by an in-memory tip map. It
// counts Tip and per-(repo,branch,sha) Fetch calls so tests can prove
// cache hits, eviction, and error propagation without touching git.
type fakeFetcher struct {
	mu         sync.Mutex
	tips       map[string]string // repoURL -> current tip sha
	tipErr     error
	fetchErr   error
	files      []File
	tipCalls   int
	fetchCalls map[string]int // "repo|branch|sha" -> count
}

func newFake() *fakeFetcher {
	return &fakeFetcher{
		tips:       map[string]string{},
		fetchCalls: map[string]int{},
		files:      []File{{Name: "MILESTONE.md", Path: ".tide/planning/m/MILESTONE.md", Content: []byte("m\n")}},
	}
}

func (f *fakeFetcher) Tip(_ context.Context, repoURL, _ string, _ *Auth) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.tipCalls++
	if f.tipErr != nil {
		return "", f.tipErr
	}
	return f.tips[repoURL], nil
}

func (f *fakeFetcher) Fetch(_ context.Context, repoURL, branch string, _ *Auth) (string, []File, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fetchErr != nil {
		return "", nil, f.fetchErr
	}
	sha := f.tips[repoURL]
	f.fetchCalls[repoURL+"|"+branch+"|"+sha]++
	return sha, f.files, nil
}

func (f *fakeFetcher) fetchCount(repoURL, branch, sha string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.fetchCalls[repoURL+"|"+branch+"|"+sha]
}

func (f *fakeFetcher) totalFetches() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, c := range f.fetchCalls {
		n += c
	}
	return n
}

func bg() context.Context { return context.Background() }

// Test 1: two Artifacts calls at the same tip SHA Fetch exactly once (second
// served from the LRU); Tip is called both times.
func TestStoreServesSecondCallFromCache(t *testing.T) {
	fake := newFake()
	fake.tips["repoA"] = "sha1"
	s, err := NewStore(fake, 8)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	sha1, files1, err := s.Artifacts(bg(), "repoA", "main", nil)
	if err != nil {
		t.Fatalf("Artifacts 1: %v", err)
	}
	sha2, files2, err := s.Artifacts(bg(), "repoA", "main", nil)
	if err != nil {
		t.Fatalf("Artifacts 2: %v", err)
	}

	if sha1 != "sha1" || sha2 != "sha1" {
		t.Errorf("sha = %q,%q want sha1", sha1, sha2)
	}
	if n := fake.fetchCount("repoA", "main", "sha1"); n != 1 {
		t.Errorf("Fetch called %d times, want 1", n)
	}
	if fake.tipCalls != 2 {
		t.Errorf("Tip called %d times, want 2", fake.tipCalls)
	}
	if len(files1) != 1 || len(files2) != 1 || string(files2[0].Content) != "m\n" {
		t.Errorf("files mismatch: %v %v", files1, files2)
	}
}

// Test 2: advancing the tip SHA triggers a fresh Fetch; the old entry is not
// corrupted (its content still fetched exactly once).
func TestStoreRefetchesOnNewSHA(t *testing.T) {
	fake := newFake()
	fake.tips["repoA"] = "sha1"
	s, err := NewStore(fake, 8)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	if _, _, err := s.Artifacts(bg(), "repoA", "main", nil); err != nil {
		t.Fatalf("Artifacts sha1: %v", err)
	}
	fake.tips["repoA"] = "sha2"
	sha, _, err := s.Artifacts(bg(), "repoA", "main", nil)
	if err != nil {
		t.Fatalf("Artifacts sha2: %v", err)
	}
	if sha != "sha2" {
		t.Errorf("sha = %q, want sha2", sha)
	}
	if n := fake.fetchCount("repoA", "main", "sha1"); n != 1 {
		t.Errorf("sha1 fetched %d times, want 1", n)
	}
	if n := fake.fetchCount("repoA", "main", "sha2"); n != 1 {
		t.Errorf("sha2 fetched %d times, want 1", n)
	}
}

// Test 3: with maxEntries=2 and three distinct keys, the least-recently-used
// entry is evicted (re-requesting the first key re-Fetches).
func TestStoreEvictsAtBound(t *testing.T) {
	fake := newFake()
	fake.tips["r1"] = "s1"
	fake.tips["r2"] = "s2"
	fake.tips["r3"] = "s3"
	s, err := NewStore(fake, 2)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	for _, r := range []string{"r1", "r2", "r3"} {
		if _, _, err := s.Artifacts(bg(), r, "main", nil); err != nil {
			t.Fatalf("Artifacts %s: %v", r, err)
		}
	}
	// r1 is LRU after r2,r3 inserted → evicted. Re-request re-Fetches.
	if _, _, err := s.Artifacts(bg(), "r1", "main", nil); err != nil {
		t.Fatalf("Artifacts r1 again: %v", err)
	}
	if n := fake.fetchCount("r1", "main", "s1"); n != 2 {
		t.Errorf("r1 fetched %d times, want 2 (evicted then refetched)", n)
	}
	if n := fake.fetchCount("r3", "main", "s3"); n != 1 {
		t.Errorf("r3 fetched %d times, want 1 (still cached)", n)
	}
}

// Test 4: a Tip error propagates without inserting anything into the cache.
func TestStoreTipErrorDoesNotPolluteCache(t *testing.T) {
	fake := newFake()
	fake.tipErr = errors.New("ls-remote boom")
	s, err := NewStore(fake, 8)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	sha, files, err := s.Artifacts(bg(), "repoA", "main", nil)
	if err == nil {
		t.Fatalf("Artifacts: err = nil, sha=%q files=%d", sha, len(files))
	}
	if fake.totalFetches() != 0 {
		t.Errorf("Fetch called despite Tip error: %d", fake.totalFetches())
	}
}

// NewStore rejects a non-positive bound.
func TestNewStoreRejectsBadBound(t *testing.T) {
	for _, n := range []int{0, -1} {
		if _, err := NewStore(newFake(), n); err == nil {
			t.Errorf("NewStore(_, %d) = nil error, want rejection", n)
		}
	}
}

// Concurrency: many parallel Artifacts calls at the same tip must be race-safe
// (run under `go test -race`) and return consistent content.
func TestStoreConcurrentArtifacts(t *testing.T) {
	fake := newFake()
	fake.tips["repoA"] = "sha1"
	s, err := NewStore(fake, 8)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	const n = 32
	var wg sync.WaitGroup
	errs := make([]error, n)
	wg.Add(n)
	for i := range n {
		go func(i int) {
			defer wg.Done()
			sha, files, err := s.Artifacts(bg(), "repoA", "main", nil)
			if err != nil {
				errs[i] = err
				return
			}
			if sha != "sha1" || len(files) != 1 {
				errs[i] = errors.New("inconsistent result")
			}
		}(i)
	}
	wg.Wait()
	for i, e := range errs {
		if e != nil {
			t.Errorf("goroutine %d: %v", i, e)
		}
	}
}
