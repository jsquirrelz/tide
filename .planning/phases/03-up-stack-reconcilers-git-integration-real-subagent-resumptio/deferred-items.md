# Phase 03 — Deferred Items

Out-of-scope discoveries surfaced during execution. NOT auto-fixed; logged here
for future plans to pick up.

## Plan 03-01 (2026-05-15)

### envtest binaries missing in fresh worktrees

- **Found during:** Task 2 verification — `go test ./internal/controller/...`
  fails BeforeSuite with `open ../../bin/k8s: no such file or directory` and
  `fork/exec /usr/local/kubebuilder/bin/etcd: no such file or directory`.
- **Scope:** Pre-existing. Reproduces on the base commit `a9027d9` with my
  uncommitted edits stashed. Not caused by the plan 03-01 schema bump.
- **Cause:** The worktree spawned for parallel execution does not run
  `make envtest` / `setup-envtest` as part of its bootstrap. The
  `internal/controller` and `test/integration/envtest` suites both require
  the kube-apiserver + etcd binaries under `bin/k8s/...`.
- **Defer to:** Phase 03 worktree-bootstrap hardening (or whatever plan
  surfaces this gap when downstream Phase 03 plans need controller envtest
  coverage to land). The fix is `make envtest` in the worktree spawn
  recipe (or making `setup-envtest` a `go test` prerequisite via
  `TestMain`).
- **`test/integration/kind` failure** is also pre-existing — the worktree
  has no kind cluster context. Out of scope for `pkg/dispatch`-only plan
  03-01.
