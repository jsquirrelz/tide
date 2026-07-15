---
id: 260715-4jd
slug: fix-kind-suite-namespace-terminating-rec
type: quick
status: complete
completed: 2026-07-15
commits:
  - a470cda  # fix(260715-4jd): swap fire-and-forget deleteNamespace to blocking deleteNamespaceAndWait in 5 multi-spec kind tests
files_modified:
  - test/integration/kind/push_lease_test.go
  - test/integration/kind/credproxy_test.go
  - test/integration/kind/wave_test.go
  - test/integration/kind/integration_miss_test.go
  - test/integration/kind/medium_http_test.go
---

# Quick Task 260715-4jd — Summary

## What changed

Swapped the fire-and-forget `deleteNamespace()` teardown call for the blocking
`deleteNamespaceAndWait()` at all 6 call sites across the 5 multi-spec kind
test files that reuse a shared namespace constant across their specs:

| File | Call site(s) | Block type |
|---|---|---|
| `push_lease_test.go:63` | `deleteNamespaceAndWait(pushLeaseNS)` | `AfterEach` |
| `credproxy_test.go:64` | `deleteNamespaceAndWait(credproxyNS)` | `AfterEach` |
| `wave_test.go:92-93` | `deleteNamespaceAndWait(testNS)` + `deleteNamespaceAndWait(fixtureNS)` | `AfterEach` |
| `integration_miss_test.go:151` | `deleteNamespaceAndWait(integrationMissNamespace)` | `AfterAll` |
| `medium_http_test.go:293` | `deleteNamespaceAndWait(mediumHTTPNamespace)` | `AfterAll` |

Root cause: `deleteNamespace()` issues `kubectl delete --timeout=30s` and
returns as soon as that call exits — even if the namespace is still
`Terminating`. `push_lease_test.go`'s four specs share the namespace constant
`push-lease-test`; its `AfterEach` returned mid-termination, so the next
spec's `BeforeEach` recreated the namespace while it was still tearing down —
namespace-create was rejected, the `tide-projects` PVC never appeared, and
`pvcPrewarmPod`'s 60s Bound wait timed out (observed phase `""` = NotFound
throughout). Verified on HEAD `c09937f`; a focused rerun on a fresh cluster
was 4/4 green, confirming non-determinism rather than a deterministic bug.
The same race class exists in every multi-spec file with a shared namespace
constant — `deleteNamespaceAndWait` (already used by
`import_resume_test.go` for exactly this reason) polls `k8sClient.Get` until
`NotFound` (3-min budget, 5s interval) before returning.

Also updated the two prose comments that named the old helper
(`integration_miss_test.go:165`, `medium_http_test.go:467`) so they stay
truthful — both now say `deleteNamespaceAndWait` (bare name, no parens, per
the plan's count-gate note).

## Explicitly NOT done

- `suite_test.go` untouched — both `deleteNamespace` and `deleteNamespaceAndWait`
  helpers stay exactly as they were; this was a call-site swap only.
- Single-spec kind test files left as fire-and-forget `deleteNamespace` — their
  namespaces are distinct per spec, so no successor-collision race exists.
- No kind cluster run performed in this task; verification was static
  (compile + grep counts). The orchestrator runs the full Layer B suite
  separately after commit to confirm the race is closed.

## Deviations from Plan

None — plan executed exactly as written. All 6 call sites and both comment
sites matched the plan's cited line numbers and text exactly on this HEAD.

## Verification

- `go vet ./test/integration/kind/` → exit 0 (package compiles).
- Scoped grep across the 5 modified files: `grep -cv 'deleteNamespaceAndWait('` on
  all `deleteNamespace(` matches → `0` (no bare `deleteNamespace(` calls remain).
- `deleteNamespaceAndWait(` count across the 5 files → `6` (1+1+2+1+1, matches plan).
- `git diff --stat test/integration/kind/suite_test.go` → empty (helper file untouched).
- `git diff --diff-filter=D --name-only HEAD~1 HEAD` → empty (no accidental deletions).
- `git status --short` after commit → clean (no stray untracked files).

## Self-Check

- FOUND: test/integration/kind/push_lease_test.go (deleteNamespaceAndWait present)
- FOUND: test/integration/kind/credproxy_test.go (deleteNamespaceAndWait present)
- FOUND: test/integration/kind/wave_test.go (deleteNamespaceAndWait present, both sites)
- FOUND: test/integration/kind/integration_miss_test.go (deleteNamespaceAndWait present)
- FOUND: test/integration/kind/medium_http_test.go (deleteNamespaceAndWait present)
- FOUND: commit a470cda in `git log --oneline --all`

**Self-Check: PASSED**
