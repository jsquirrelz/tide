# Deferred Items — Phase 41 Plan 03

Out-of-scope discoveries logged per executor SCOPE BOUNDARY rule. Not fixed as part of this plan.

## `cmd/tide-demo-init` missing embedded `fixture/` directory

`go build ./...` fails with:

```
cmd/tide-demo-init/main.go:112:12: pattern all:fixture: no matching files found
```

`cmd/tide-demo-init/main.go` declares `//go:embed all:fixture` but no `fixture/` directory
exists at `cmd/tide-demo-init/` in this checkout. Confirmed unrelated to Plan 41-03 (no files
in `cmd/tide-demo-init/` were touched by this plan; the directory listing shows only
`main.go`, `main_test.go`, `README.md` — no `.gitignore` entry excludes it either). Pre-existing
environmental gap, not introduced by this plan. Plan verification instead uses the scoped
commands `go build ./... && go vet ./internal/controller/...` (Task 1/2) and
`go test ./internal/controller/... ./cmd/manager/... -count=1` (Task 3), consistent with the
plan's own `<verify>` blocks — both pass clean on the touched packages.

## Update (Plan 41-06)

Still present, still unrelated — confirmed again during 41-06's Task 3 acceptance-criteria run
(`go build ./...` fails identically at `cmd/tide-demo-init/main.go:112:12`; `.gitignore:56`
confirms `cmd/tide-demo-init/fixture/` is intentionally untracked and materialized by
`go:generate` from `examples/tide-demo-fixture/`). 41-06 verified with
`go build ./... 2>&1 | grep -v tide-demo-init` plus explicit scoped builds of every touched
package (`internal/controller`, `cmd/manager`, `test/integration/envtest`), all clean.

## Update (Plan 41-07)

Still present, still unrelated — confirmed again during 41-07's Task 1/2 acceptance-criteria run
(`go build ./...` / `go list ./...` fail identically at `cmd/tide-demo-init/main.go:112:12`; a
failing embed pattern anywhere in the module aborts `go list ./...` entirely, returning zero
packages, which is why a plain `go vet ./...` also reports nothing useful). 41-07 verified with
explicit scoped builds/vets (`go build ./internal/controller/... ./api/...`,
`go vet ./internal/... ./api/... ./cmd/manager/... ./cmd/dashboard/... ./pkg/...`) plus the full
`go test ./internal/controller/... -count=1` Ginkgo suite (204/204 specs green, envtest via
`KUBEBUILDER_ASSETS`) — all clean.

## `internal/controller/dispatch_helpers.go:551,559,566` logcheck findings (pre-existing, Plan 41-05)

`golangci-lint run ./internal/controller/...` reports 3 `logcheck` findings ("Key positional
arguments are expected to be inlined constant strings... Please replace level provided with
string value") inside `checkDispatchHolds` (added by Plan 41-05, commit `96dd23b`). Confirmed via
`git show 9217126...:internal/controller/dispatch_helpers.go` that this exact code (and finding)
predates any Plan 41-06 commit — 41-06 never touches lines 480-570 of this file (its own edit to
`dispatch_helpers.go` is the `PlannerReconcilerDeps` struct added near the top of the file, Task
1). Out of scope for 41-06's test-fixture-sweep task; not fixed here.
