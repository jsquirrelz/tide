# Phase 38 — Deferred Items

## Pre-existing: `go build ./...` / `go list ./...` fail on cmd/tide-demo-init embed

- **Found during:** 38-02 Task 1 verification (2026-07-11)
- **Symptom:** `cmd/tide-demo-init/main.go:112:12: pattern all:fixture: no matching files found` — `go list ./...` exits 1 with empty stdout, so any `$(go list ./... | grep -v ...)` recipe (e.g. `make test`'s package expansion) breaks when the `fixture/` scaffold has not been positioned.
- **Verified pre-existing:** reproduces identically in the main checkout at the same commit; not introduced by Phase 38 work.
- **Why deferred:** out of 38-02 scope (scope boundary — unrelated package). The package doc says the fixture is "positioned at build time"; either the positioning step is undocumented in the Makefile path or a build-tag/ignore guard is missing on the package.

## Pre-existing: `make lint` fails on 4 modernize issues in cmd/dashboard/main_test.go

- **Found during:** 38-07 final lint verification (2026-07-11)
- **Symptom:** golangci-lint `modernize` flags 4 `ptr(x)` → `new(x)` simplifications in `cmd/dashboard/main_test.go` (lines 41, 44, 45 and the `ptr` helper at 73); `make lint` exits 1.
- **Verified pre-existing:** introduced by wave-1 commit d57209a (plan 38-05 TELEM-03 test scaffolding); `cmd/dashboard/main_test.go` is untouched by plan 38-07 (`git log` shows d57209a as the sole author of the flagged lines).
- **Why deferred:** out of 38-07 scope (scope boundary — dashboard files belong to plan 38-05's surface; editing them from a parallel worktree risks merge conflicts). Fix is mechanical: replace the `ptr` helper calls with `new("true")`-style expressions or run `make lint-fix` on that file.
