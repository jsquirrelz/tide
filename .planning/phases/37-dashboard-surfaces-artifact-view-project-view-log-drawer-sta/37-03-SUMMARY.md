---
phase: 37-dashboard-surfaces-artifact-view-project-view-log-drawer-sta
plan: 03
subsystem: api
tags: [go-git, golang-lru, dashboard, artifacts, shallow-clone, lru-cache]

# Dependency graph
requires:
  - phase: 37-02
    provides: ".tide/ planning tree materialized on the run branch via tide-push envelope staging (the exact content this read path fetches)"
provides:
  - "cmd/dashboard/gitfetch package: Fetcher interface seam + GoGitFetcher shallow-clone implementation"
  - "Store: bounded LRU over the Fetcher, keyed on (repoURL, branch, tip SHA)"
  - "K8s-free dashboard read path for .tide/ artifact trees (DASH-01 / D-01)"
  - "github.com/hashicorp/golang-lru/v2 promoted to a direct dependency"
affects: [37-07, 37-05, 37-08]

# Tech tracking
tech-stack:
  added: [github.com/hashicorp/golang-lru/v2 v2.0.7 (promoted indirect -> direct)]
  patterns:
    - "Fresh shallow clone per tip SHA into in-memory storer, discarded after tree extraction (go-git#305/src-d#900 avoidance)"
    - "Interface seam (Fetcher) so an exec-git fallback is swappable without touching the cache"
    - "Credential pass-through: Auth flows through call frames only, never stored in cache key/value"
    - "ls-remote Tip-check gates the clone; LRU keyed on SHA makes an advanced branch always a miss"

key-files:
  created:
    - cmd/dashboard/gitfetch/gitfetch.go
    - cmd/dashboard/gitfetch/gitfetch_test.go
    - cmd/dashboard/gitfetch/store.go
    - cmd/dashboard/gitfetch/store_test.go
  modified:
    - go.mod

key-decisions:
  - "Key the LRU on the SHA actually returned by Fetch (not the earlier ls-remote SHA) so a tip that advances between Tip and Fetch is stored under its own key, never masquerading as stale content."
  - "Read blobs via object.File.Reader()+io.ReadAll rather than File.Contents() to preserve byte fidelity (Contents forces a UTF-8 string round-trip)."
  - "Anonymous access collapses a typed-nil *BasicAuth to a genuine nil interface (guard `if a := basicAuth(auth); a != nil`) to avoid go-git emitting an empty auth header — mirrors pkg/git/clone.go's pat=='' guard."

patterns-established:
  - "gitfetch stays Kubernetes-free: Secret resolution is the caller's job (plan 37-07 handler), keeping this package unit-testable against file:// fixtures."
  - "TDD RED->GREEN per task with fixture bare repos built in-test over the local git transport."

requirements-completed: [DASH-01]

coverage:
  - id: D1
    description: "GoGitFetcher shallow-clones a run branch tip and extracts the .tide/ tree with byte fidelity, excluding non-.tide files; missing .tide/ is empty-not-error; Tip tracks the branch tip via ls-remote."
    requirement: DASH-01
    verification:
      - kind: unit
        ref: "cmd/dashboard/gitfetch/gitfetch_test.go#TestGoGitFetcherFetchExtractsTideTree"
        status: pass
      - kind: unit
        ref: "cmd/dashboard/gitfetch/gitfetch_test.go#TestGoGitFetcherTipTracksBranchTip"
        status: pass
      - kind: unit
        ref: "cmd/dashboard/gitfetch/gitfetch_test.go#TestGoGitFetcherFetchNonexistentBranchErrors"
        status: pass
      - kind: unit
        ref: "cmd/dashboard/gitfetch/gitfetch_test.go#TestGoGitFetcherFetchNoTideDirIsEmptyNotError"
        status: pass
    human_judgment: false
  - id: D2
    description: "No credential retention or leakage: a failed auth'd fetch never embeds the PAT in its error string; LRU key/value carry no Auth field (T-37-03-01)."
    requirement: DASH-01
    verification:
      - kind: unit
        ref: "cmd/dashboard/gitfetch/gitfetch_test.go#TestGoGitFetcherErrorNeverLeaksPAT"
        status: pass
      - kind: other
        ref: "grep -c 'Auth' cmd/dashboard/gitfetch/store.go — only copyright/doc-comment/func-param, no struct field"
        status: pass
    human_judgment: false
  - id: D3
    description: "Bounded, rederivable LRU Store: single Fetch per SHA on cache hit, refetch on new SHA, LRU eviction at the bound, Tip-error non-pollution, and race-safe concurrent access."
    requirement: DASH-01
    verification:
      - kind: unit
        ref: "cmd/dashboard/gitfetch/store_test.go#TestStoreServesSecondCallFromCache"
        status: pass
      - kind: unit
        ref: "cmd/dashboard/gitfetch/store_test.go#TestStoreEvictsAtBound"
        status: pass
      - kind: unit
        ref: "cmd/dashboard/gitfetch/store_test.go#TestStoreTipErrorDoesNotPolluteCache"
        status: pass
      - kind: unit
        ref: "cmd/dashboard/gitfetch/store_test.go#TestStoreConcurrentArtifacts (go test -race)"
        status: pass
    human_judgment: false
  - id: D4
    description: "golang-lru/v2 promoted from indirect to a direct require in go.mod."
    requirement: DASH-01
    verification:
      - kind: other
        ref: "grep -n 'golang-lru' go.mod — line 10, direct require block, no // indirect"
        status: pass
      - kind: other
        ref: "go build ./... — clean"
        status: pass
    human_judgment: false

# Metrics
duration: ~20min
completed: 2026-07-08
status: complete
---

# Phase 37 Plan 03: Dashboard git read path (gitfetch) Summary

**A K8s-free `cmd/dashboard/gitfetch` package — a go-git shallow-clone Fetcher behind an interface seam plus a bounded golang-lru Store — that reads the `.tide/` tree at a run-branch tip with byte fidelity and zero credential retention.**

## Performance

- **Duration:** ~20 min
- **Completed:** 2026-07-08
- **Tasks:** 2 (both TDD)
- **Files modified:** 5 (4 created, 1 modified)

## Accomplishments
- `Fetcher` interface + `GoGitFetcher`: shallow clone (`Depth:1`, `SingleBranch`, `NoTags`, `NoCheckout`) into an in-memory storer, walk only the `.tide` subtree, return `[]File` with repo-relative `Path` and byte-identical `Content`; `Tip` via `ListContext` (ls-remote).
- Fresh-clone-per-SHA invariant documented and enforced by construction — no pull/fetch into a shallow clone (go-git#305 / src-d/go-git#900 avoidance).
- `Store`: bounded LRU keyed on `(repoURL, branch, sha)` with Tip-check → LRU Get → fetch-on-miss; credentials pass through call frames only.
- `golang-lru/v2` promoted from indirect to direct require via `go mod tidy`.

## Task Commits

Each task followed the TDD RED → GREEN cycle:

1. **Task 1: Fetcher interface + go-git shallow-clone impl**
   - RED: `87e7d3f` (test)
   - GREEN: `74b6ba6` (feat)
2. **Task 2: LRU Store + golang-lru promotion**
   - RED: `123448f` (test)
   - GREEN: `89e3125` (feat)

## Files Created/Modified
- `cmd/dashboard/gitfetch/gitfetch.go` - `File`/`Auth` types, `Fetcher` interface seam, `GoGitFetcher` shallow-clone `Fetch` + ls-remote `Tip`, `basicAuth` mirroring pkg/git/clone.go.
- `cmd/dashboard/gitfetch/gitfetch_test.go` - Fixture bare-repo helpers over the local transport; tree extraction, Tip tracking, missing-branch error, empty-.tide-as-data, PAT non-leak.
- `cmd/dashboard/gitfetch/store.go` - `Store`, `NewStore` (validates bound), `Artifacts`; `cacheKey`/`cacheValue` carry no Auth; `DefaultMaxEntries=32`.
- `cmd/dashboard/gitfetch/store_test.go` - Fake Fetcher with call counting; hit/miss/eviction/error-propagation + `-race` concurrency.
- `go.mod` - `github.com/hashicorp/golang-lru/v2` moved from indirect to direct.

## Decisions Made
- Cache is keyed on the SHA returned by `Fetch` (authoritative for what was actually read), not the earlier ls-remote SHA — closes a Tip/Fetch race window.
- `File.Reader()` + `io.ReadAll` instead of `File.Contents()` to keep binary/byte fidelity.
- Typed-nil guard on `basicAuth` so anonymous access sends no auth header (mirrors the repo's pat=="" precedent).

## Deviations from Plan
None - plan executed exactly as written. All threat-register mitigations (T-37-03-01 credential non-retention/non-leak, T-37-03-02 DoS-bounded shallow options + LRU cap) are implemented and test-proven; no package-manager install occurred (golang-lru was already resolved — promotion only, satisfying T-37-SC).

## Issues Encountered
- The plan's acceptance grep expected `grep -c 'NoTags' gitfetch.go` == 1; the initial package doc comment also mentioned `NoTags`, yielding 2. Reworded the doc comment to "no tag following" so the sole `NoTags` token is the load-bearing `gogit.NoTags` in code. Behavior unchanged.

## User Setup Required
None - no external service configuration required.

## Next Phase Readiness
- `Store.Artifacts(ctx, repoURL, branch, auth)` is the exact seam plan 37-07's artifacts handler consumes; that plan resolves creds from the per-project Secret via a typed clientset and wires the Store into the endpoint.
- 37-05/37-08 will render the served `.tide/` content in the UI (markdown XSS-safety is their responsibility per T-37-03-03).

## Self-Check: PASSED

Created files verified present; task commits verified in `git log`. See section below.

---
*Phase: 37-dashboard-surfaces-artifact-view-project-view-log-drawer-sta*
*Completed: 2026-07-08*
