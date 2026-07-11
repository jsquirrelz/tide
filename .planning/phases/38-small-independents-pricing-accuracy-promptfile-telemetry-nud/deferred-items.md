# Phase 38 — Deferred Items

## Pre-existing: `go build ./...` / `go list ./...` fail on cmd/tide-demo-init embed

- **Found during:** 38-02 Task 1 verification (2026-07-11)
- **Symptom:** `cmd/tide-demo-init/main.go:112:12: pattern all:fixture: no matching files found` — `go list ./...` exits 1 with empty stdout, so any `$(go list ./... | grep -v ...)` recipe (e.g. `make test`'s package expansion) breaks when the `fixture/` scaffold has not been positioned.
- **Verified pre-existing:** reproduces identically in the main checkout at the same commit; not introduced by Phase 38 work.
- **Why deferred:** out of 38-02 scope (scope boundary — unrelated package). The package doc says the fixture is "positioned at build time"; either the positioning step is undocumented in the Makefile path or a build-tag/ignore guard is missing on the package.
