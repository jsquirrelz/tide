# Deferred Items — Phase 42

Out-of-scope discoveries logged during plan execution, per executor scope-boundary rule (do not fix; document only).

## 42-02: `go build ./...` fails on unrelated `cmd/tide-demo-init`

**Found during:** Plan 42-02, Task 1 verification (`go build ./...` repo-wide compile check).

**Issue:** `cmd/tide-demo-init/main.go:112` has `//go:embed all:fixture`, but the worktree has no `cmd/tide-demo-init/fixture/` directory content (`git ls-files cmd/tide-demo-init/fixture` returns nothing), so the embed pattern matches zero files and the package fails to compile with `pattern all:fixture: no matching files found`.

**Scope:** Zero commits in this plan touch `cmd/tide-demo-init/`. `git diff --stat` for this plan's commits shows only `pkg/otelai/tracecontext.go` and `pkg/otelai/tracecontext_test.go` — this is a pre-existing worktree/checkout state issue, not introduced by 42-02.

**Verification performed instead:** `go build ./pkg/otelai/...` (this plan's actual package) passes clean; `go vet ./pkg/otelai/...` passes clean; `go test ./pkg/otelai/...` passes clean (7/7).

**Action:** Not fixed (out of scope per Rule 1-3 scope boundary). Flagging for whoever verifies Phase 42 or a later plan that touches `cmd/tide-demo-init/`.

## 42-03: `go build ./...` fails on `cmd/tide-demo-init` — pre-existing, unrelated to this plan

**Found during:** Task 1 verification (`go build ./...`)

**Symptom:** `cmd/tide-demo-init/main.go:112:12: pattern all:fixture: no matching files found`

**Cause:** `cmd/tide-demo-init/fixture/` is a gitignored directory materialized at build time via `make demo-fixture` (`go generate ./cmd/tide-demo-init/...`), per `.gitignore` line 52-56 and `Makefile:77-78`. This worktree never ran that target, so the directory backing the `//go:embed all:fixture` directive doesn't exist. Confirmed pre-existing and unrelated to plan 42-03 (last commit touching `cmd/tide-demo-init/main.go` is `25fce55`, unrelated to Phase 42; the directory was absent before this plan's edits).

**Scope:** `cmd/tide-demo-init` is not in this plan's `files_modified` list (`api/v1alpha3/*_types.go`, `config/crd/bases/*.yaml`). Out of scope per executor SCOPE BOUNDARY rule.

**Verification performed instead:** `go build ./api/... ./internal/controller/...` (the packages touched/consuming the new fields) exits 0. `go build $(go list -e ./... | grep -v /cmd/tide-demo-init)` also exits 0 across the rest of the repo (the only other non-zero list entries are test-only packages with build constraints excluding non-test files, expected). Repo compiles cleanly outside this pre-existing environmental gap.

**Recommended fix (not applied — out of scope):** run `make demo-fixture` before `go build ./...` in this worktree, or note the precondition in onboarding docs if not already covered.
