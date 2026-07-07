# Phase 35 — Deferred / Out-of-Scope Items

Discovered during execution; NOT fixed (scope boundary — not caused by this phase's changes).

## `go build ./...` fails on `cmd/tide-demo-init` in a fresh worktree
- **Found during:** Plan 35-01 broad build check.
- **Error:** `cmd/tide-demo-init/main.go:112:12: pattern all:fixture: no matching files found`.
- **Root cause:** `cmd/tide-demo-init/fixture/` is a **gitignored, build-time-populated** directory (SOT at `examples/tide-demo-fixture/`), required by the `//go:embed all:fixture` directive. It is absent in any fresh checkout until the build context populates it.
- **Evidence it's pre-existing:** identical failure on base `main` (624e770); package untouched by any Phase 35 commit.
- **Disposition:** out of scope for Phase 35 (git-base-ref). In-scope packages (`./api/...`, `./internal/...`, `./pkg/...`, `./cmd/tide-push/...`) build clean. No action.
