# Deferred Items — Phase 42

Out-of-scope discoveries logged during plan execution. Not fixed (per SCOPE BOUNDARY rule) — only issues directly caused by the executing plan's own changes are auto-fixed.

## 42-03: `go build ./...` fails on `cmd/tide-demo-init` — pre-existing, unrelated to this plan

**Found during:** Task 1 verification (`go build ./...`)

**Symptom:** `cmd/tide-demo-init/main.go:112:12: pattern all:fixture: no matching files found`

**Cause:** `cmd/tide-demo-init/fixture/` is a gitignored directory materialized at build time via `make demo-fixture` (`go generate ./cmd/tide-demo-init/...`), per `.gitignore` line 52-56 and `Makefile:77-78`. This worktree never ran that target, so the directory backing the `//go:embed all:fixture` directive doesn't exist. Confirmed pre-existing and unrelated to plan 42-03 (last commit touching `cmd/tide-demo-init/main.go` is `25fce55`, unrelated to Phase 42; the directory was absent before this plan's edits).

**Scope:** `cmd/tide-demo-init` is not in this plan's `files_modified` list (`api/v1alpha3/*_types.go`, `config/crd/bases/*.yaml`). Out of scope per executor SCOPE BOUNDARY rule.

**Verification performed instead:** `go build ./api/... ./internal/controller/...` (the packages touched/consuming the new fields) exits 0. `go build $(go list -e ./... | grep -v /cmd/tide-demo-init)` also exits 0 across the rest of the repo (the only other non-zero list entries are test-only packages with build constraints excluding non-test files, expected). Repo compiles cleanly outside this pre-existing environmental gap.

**Recommended fix (not applied — out of scope):** run `make demo-fixture` before `go build ./...` in this worktree, or note the precondition in onboarding docs if not already covered.
