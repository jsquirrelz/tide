# Deferred Items — Phase 42

Out-of-scope discoveries logged during plan execution, per executor scope-boundary rule (do not fix; document only).

## 42-02: `go build ./...` fails on unrelated `cmd/tide-demo-init`

**Found during:** Plan 42-02, Task 1 verification (`go build ./...` repo-wide compile check).

**Issue:** `cmd/tide-demo-init/main.go:112` has `//go:embed all:fixture`, but the worktree has no `cmd/tide-demo-init/fixture/` directory content (`git ls-files cmd/tide-demo-init/fixture` returns nothing), so the embed pattern matches zero files and the package fails to compile with `pattern all:fixture: no matching files found`.

**Scope:** Zero commits in this plan touch `cmd/tide-demo-init/`. `git diff --stat` for this plan's commits shows only `pkg/otelai/tracecontext.go` and `pkg/otelai/tracecontext_test.go` — this is a pre-existing worktree/checkout state issue, not introduced by 42-02.

**Verification performed instead:** `go build ./pkg/otelai/...` (this plan's actual package) passes clean; `go vet ./pkg/otelai/...` passes clean; `go test ./pkg/otelai/...` passes clean (7/7).

**Action:** Not fixed (out of scope per Rule 1-3 scope boundary). Flagging for whoever verifies Phase 42 or a later plan that touches `cmd/tide-demo-init/`.
