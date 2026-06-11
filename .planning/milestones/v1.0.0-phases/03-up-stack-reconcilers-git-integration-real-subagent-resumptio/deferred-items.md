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

## Plan 03-08 (2026-05-15)

### Pre-existing controller-suite breakage from plan 03-02 schema additions

- **Symptom:** 24 controller-suite tests fail at envtest BeforeEach with:
  ```
  Project.tideproject.k8s "<name>" is invalid:
  spec.git.repoURL: Invalid value: "":
  spec.git.repoURL in body should match '^https?://.+'
  ```
- **Root cause:** Plan 03-02 added `GitConfig.RepoURL` as a CRD-level
  `+kubebuilder:validation:Pattern=^https?://.+` AND made `repoURL`
  required whenever `GitConfig` is present. Existing test fixtures
  (`makeProjectForTask` in `task_controller_test.go` and several
  `ProjectReconciler` test BeforeEach blocks) create Projects with no
  `Git` field set in Go, but the CRD pattern validation fires anyway
  after the empty-struct round-trip through envtest's API server.
- **Scope:** This is NOT a deviation caused by plan 03-08's work.
  Plan 03-08 introduces *new* tests with correctly-formed Git config
  blocks (those pass). The 24 pre-existing failures were already
  broken on the wave-3 merge into main; confirmed by checking out the
  base commit (`e9c01737`) and running the suite — same 24 failures.
- **Fix shape (deferred):** A follow-up plan needs to update
  `makeProjectForTask` and the ProjectReconciler test BeforeEach
  blocks to set `Git: GitConfig{RepoURL: "https://...",
  CredsSecretRef: "..."}` (a 3-line change per fixture, ~10 fixtures).
- **Files affected:** `internal/controller/task_controller_test.go`
  (`makeProjectForTask` at line 156), `internal/controller/project_controller_test.go`
  (6+ test BeforeEach blocks).
- **Defer to:** Next Phase 3 plan that needs a clean controller suite
  (likely the closeout plan that gates phase completion on suite
  green). Plan 03-08 unblocked 4 of the previously-failing 28 specs
  by adding the new Phase 3 specs that already use the correct Git
  fixture shape, but the legacy fixture-set still needs the 1-line
  fix to clear the remaining 24.
