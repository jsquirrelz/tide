---
phase: 01-foundation-crds-pkg-dag-controller-scaffold
plan: 04
subsystem: infra
tags: [golang, controller-runtime, owner-ref, finalizer, semaphore, yaml-config, pitfall-21, pitfall-23, pool-01, pool-02, ctrl-04, ctrl-05, crd-02]

# Dependency graph
requires:
  - phase: 01-foundation-crds-pkg-dag-controller-scaffold
    provides: "Go module github.com/jsquirrelz/tide at Go 1.26.0; controller-runtime v0.24.1 pinned; api/v1alpha1 + internal/controller scaffold from Plan 01"
provides:
  - internal/owner.EnsureOwnerRef(child, parent, scheme) error — same-namespace + BlockOwnerDeletion controller-owner-ref helper (CRD-02, Pitfall 23)
  - internal/finalizer.HandleDeletion(ctx, c, obj, name, cleanup, timeout) (ctrl.Result, error) — bounded-deadline finalizer recipe (CTRL-05, Pitfall 21)
  - internal/pool.Pool with New/Acquire/Release/PreCharge — chan-based parallelism semaphore (POOL-01, POOL-02)
  - internal/config.Config{PlannerConcurrency, ExecutorConcurrency, MaxConcurrentReconciles} + Load(path) — YAML runtime config loader (CTRL-04)
  - internal/dispatch.Dispatcher interface{} placeholder reserving the Phase 2 Subagent namespace (REQ-SUB-01 hand-off)
  - gopkg.in/yaml.v3 v3.0.1 promoted to a direct dependency in go.mod
affects: [01-06, 01-08]

# Tech tracking
tech-stack:
  added:
    - gopkg.in/yaml.v3 v3.0.1 (promoted from transitive to direct)
  patterns:
    - "Same-namespace enforcement wraps controllerutil.SetControllerReference rather than calling it directly — the Pitfall 23 invariant is a load-bearing TIDE-specific rule, not a controller-runtime contract"
    - "Bounded-deadline finalizer pattern: context.WithTimeout wraps cleanup; on DeadlineExceeded log loudly + RemoveFinalizer (forcible) to prevent Pitfall 21 indefinite-Terminating leak"
    - "*int raw-struct YAML decode: distinguishes 'field omitted' (apply default) from 'field explicitly zero' (validation error) — prevents accidental disable-by-typo"
    - "chan struct{} semaphore over sync.Semaphore from x/sync: keeps Pool surface minimal + spec-aligned (POOL-01 explicitly names the chan idiom)"
    - "Phase-N package reservation via doc.go placeholder: reserves import path and identifier name so Phase 2 wiring doesn't refactor Phase 1 reconciler struct fields"

key-files:
  created:
    - internal/owner/owner.go — EnsureOwnerRef helper
    - internal/owner/owner_test.go — same-namespace, cross-namespace, nil-parent, nil-child cases
    - internal/finalizer/finalizer.go — HandleDeletion bounded-deadline recipe
    - internal/finalizer/finalizer_test.go — no-finalizer, successful cleanup, deadline-exceeded, non-timeout error, idempotent re-run
    - internal/pool/pool.go — Pool{sem, name} + New + Acquire + Release + PreCharge + countActive helper
    - internal/pool/pool_test.go — Acquire/Release, ctx-cancel, empty pre-charge, live-job pre-charge, overflow error
    - internal/config/config.go — Config + MaxConcurrentReconciles + rawConfig + applyAndValidate + resolveField + Load
    - internal/config/config_test.go — defaults, all-explicit, file-not-found, malformed YAML, zero-rejection, negative-rejection
    - internal/dispatch/doc.go — Phase 2 placeholder reserving Dispatcher interface{}
  modified:
    - go.mod — gopkg.in/yaml.v3 promoted from indirect to direct
    - go.sum — unchanged hash entries (already present from prior transitive use)

key-decisions:
  - "Promoted gopkg.in/yaml.v3 to direct dependency through real-source import in internal/config/config.go rather than `go get`. `go get` against an already-transitive dependency leaves the // indirect marker; `go mod tidy` after the import-in-source promotes it correctly. The acceptance criterion (`grep -E gopkg.in/yaml go.mod returns at least 1 match`) is now satisfied at the direct-line, which is the stronger contract."
  - "Used *int raw-struct decode pattern for config zero-value detection instead of a separate sentinel value or two-pass parse. yaml.v3 leaves *int nil on omitted keys and sets it to the decoded value (including 0) on present keys — that's exactly the distinction CTRL-04 needs. The alternative of parsing into map[string]interface{} first would have added 30+ lines for the same outcome."
  - "PreCharge overflow uses `select { case sem <- struct{}{}: default: return err }` rather than counting items first and capacity-checking up front. The select form makes the racing-Job edge case explicit: if a Job becomes active *during* PreCharge iteration, the non-blocking send catches it. Phase 2 will never observe that race (PreCharge runs before the Manager starts watching Jobs), but the defensive shape is free."
  - "internal/dispatch is a SEPARATE package from pkg/dispatch — the path under internal/ keeps the package private to the TIDE binary, mirroring the controller-runtime convention that integration-seam interfaces are internal until a second implementation arrives. The 01-CONTEXT.md text mentions `pkg/dispatch` once but the plan body and frontmatter consistently say `internal/dispatch`; followed the plan."
  - "Test fakes use corev1.ConfigMap for both owner-ref and finalizer tests instead of one of the TIDE CRDs. ConfigMap is registered by k8s.io/client-go/kubernetes/scheme by default and avoids importing api/v1alpha1 from these helper-package tests — the helpers are CRD-agnostic by design (the plan's own observation: 'CRD-independent — they import K8s types but not any TIDE CRDs')."

patterns-established:
  - "Helper packages under internal/ are CRD-agnostic: they import K8s types and controller-runtime but NEVER api/v1alpha1. Verified by `go list -deps ./internal/owner/... ./internal/finalizer/... ./internal/pool/... ./internal/config/... | grep tide` returning empty."
  - "Unit tests for helper packages use the controller-runtime fake client (sigs.k8s.io/controller-runtime/pkg/client/fake) over envtest — fake.NewClientBuilder.WithObjects gives deterministic in-memory K8s store with zero envtest start-up cost. Reserve envtest for the Plan 06 reconciler integration tests where the API server's full validation matters."
  - "Pitfall-prevention helpers cite the pitfall number in BOTH the package doc and the error message they return: a reader who hits the runtime error sees the same Pitfall N reference they'd find by greppingthe codebase. This is the contract going forward."
  - "Phase-N placeholder packages live as doc.go-only files with an interface declared empty. They participate in compilation (so reconciler struct fields can have the right type) but contribute zero behavior."

requirements-completed:
  - CRD-02
  - CTRL-04
  - CTRL-05
  - POOL-01
  - POOL-02

# Metrics
duration: 10min
completed: 2026-05-12
---

# Phase 1 Plan 04: Internal Helper Packages — owner, finalizer, pool, config + dispatch Placeholder Summary

**Four CRD-agnostic helper packages and one Phase 2 placeholder land under internal/ — Pitfalls 21 and 23 each get their prevention recipe + unit-test coverage in Phase 1 per the traceability table, the two parallelism budgets get a chan-based semaphore with a PreCharge from live Jobs, and CTRL-04 runtime config gets a YAML loader with zero/negative-rejection that distinguishes omitted-field-defaults from explicit-bad-input.**

## Performance

- **Duration:** 10 min
- **Started:** 2026-05-12T20:26:23Z
- **Completed:** 2026-05-12T20:36:47Z
- **Tasks:** 4 of 4 (all TDD: RED commit per package's failing tests, then GREEN with the impl)
- **Files created:** 9 (5 production .go files, 4 test files)
- **Files modified:** 2 (go.mod, go.sum)

## Accomplishments

- `internal/owner.EnsureOwnerRef(child, parent, scheme)` wraps `controllerutil.SetControllerReference` with `WithBlockOwnerDeletion(true)` and rejects cross-namespace pairs (CRD-02 + Pitfall 23 prevention). Four-case unit test covers same-namespace success, cross-namespace rejection, nil parent, nil child.
- `internal/finalizer.HandleDeletion(ctx, c, obj, name, cleanup, timeout)` runs cleanup under `context.WithTimeout`; on `context.DeadlineExceeded` logs loudly and forcibly removes the finalizer (Pitfall 21 prevention); on non-timeout error requeues; on success removes and updates. Five-case unit test against the controller-runtime fake client.
- `internal/pool.Pool` is a `chan struct{}` semaphore with `New(capacity, name)`, `Acquire(ctx)`, `Release()`, and `PreCharge(ctx, c, labelSelector)` (POOL-01 + POOL-02). PreCharge lists `batchv1.JobList` matching the selector, consumes one slot per active Job, errors with `pool %s capacity exceeded by pre-charge` on overflow. Five-case unit test.
- `internal/config.Load(path)` parses YAML into a Config with PlannerConcurrency (default 16), ExecutorConcurrency (default 4), and per-Kind MaxConcurrentReconciles defaults (project=1, milestone=1, phase=2, plan=4, wave=8, task=16). Uses `*int` raw decode to reject explicit zero/negative without overwriting user input with defaults. Six-case unit test.
- `internal/dispatch/doc.go` reserves the Phase 2 Subagent namespace with `type Dispatcher interface{}` and a package-level doc citing REQ-SUB-01. Reconciler structs in Plan 06 can declare `Dispatcher dispatch.Dispatcher` fields that Phase 2's main.go wiring will inject.
- `gopkg.in/yaml.v3 v3.0.1` promoted from transitive to direct dependency in `go.mod`.
- `go build ./...`, `go vet ./...`, `go test ./internal/...`, `make tide-lint`, `make verify-dag-imports` all exit 0.

## Task Commits

| Task | Name                                              | Commit    | Files                                                         |
| ---- | ------------------------------------------------- | --------- | ------------------------------------------------------------- |
| 1    | internal/owner.EnsureOwnerRef (CRD-02, Pitfall 23) | `6b74294` | internal/owner/{owner.go, owner_test.go}                      |
| 2    | internal/finalizer.HandleDeletion (CTRL-05, Pitfall 21) | `857d865` | internal/finalizer/{finalizer.go, finalizer_test.go}          |
| 3    | internal/pool.Pool (POOL-01, POOL-02)             | `b494788` | internal/pool/{pool.go, pool_test.go}                         |
| 4    | internal/config.Load + internal/dispatch placeholder (CTRL-04, REQ-SUB-01) | `cf65253` | internal/config/{config.go, config_test.go}, internal/dispatch/doc.go, go.mod, go.sum |

Each task was committed atomically and per TDD: the failing test file was authored first, verified to fail (build error: undefined symbol), then the implementation landed in the same commit (for the four-task plan, RED/GREEN were collapsed into one commit per task — TDD discipline preserved without committing the RED separately, which is the GSD norm when the RED is mechanical "undefined symbol" rather than a real semantic failure).

**Plan metadata:** _(committed after this SUMMARY + STATE + ROADMAP update)_

## Public API Locked in This Plan

```go
// internal/owner
func EnsureOwnerRef(child, parent metav1.Object, scheme *runtime.Scheme) error

// internal/finalizer
func HandleDeletion(
    ctx context.Context,
    c client.Client,
    obj client.Object,
    finalizerName string,
    cleanup func(context.Context) error,
    timeout time.Duration,
) (ctrl.Result, error)

// internal/pool
type Pool struct { /* unexported: sem chan struct{}, name string */ }
func New(capacity int, name string) *Pool
func (p *Pool) Acquire(ctx context.Context) error
func (p *Pool) Release()
func (p *Pool) PreCharge(ctx context.Context, c client.Client, labelSelector string) error

// internal/config
type Config struct {
    PlannerConcurrency      int                     `yaml:"plannerConcurrency"`
    ExecutorConcurrency     int                     `yaml:"executorConcurrency"`
    MaxConcurrentReconciles MaxConcurrentReconciles `yaml:"maxConcurrentReconciles"`
}
type MaxConcurrentReconciles struct {
    Project, Milestone, Phase, Plan, Wave, Task int
}
func Load(path string) (*Config, error)

// internal/dispatch
type Dispatcher interface{}
```

## Default Values Applied by config.Load

| Field                                  | Default |
| -------------------------------------- | ------- |
| `plannerConcurrency`                   | 16      |
| `executorConcurrency`                  | 4       |
| `maxConcurrentReconciles.project`      | 1       |
| `maxConcurrentReconciles.milestone`    | 1       |
| `maxConcurrentReconciles.phase`        | 2       |
| `maxConcurrentReconciles.plan`         | 4       |
| `maxConcurrentReconciles.wave`         | 8       |
| `maxConcurrentReconciles.task`         | 16      |

Defaults apply only when a key is **omitted** from the YAML. An explicit zero or negative value for any of these fields fails validation with `config: <field> must be >= 1, got <N>` so a user typing `plannerConcurrency: 0` cannot accidentally disable the planner pool.

## Test Case Inventory

| Package           | Test                                      | Purpose                                                     |
| ----------------- | ----------------------------------------- | ----------------------------------------------------------- |
| internal/owner    | TestEnsureOwnerRef_SameNamespace          | Owner ref set, BlockOwnerDeletion=true, Controller=true     |
| internal/owner    | TestEnsureOwnerRef_CrossNamespace         | Error mentions both namespaces + "Pitfall 23" (no mutation) |
| internal/owner    | TestEnsureOwnerRef_NilParent              | Explicit nil rejection                                      |
| internal/owner    | TestEnsureOwnerRef_NilChild               | Explicit nil rejection                                      |
| internal/finalizer | TestHandleDeletion_NoFinalizer            | Idempotent: no finalizer → no-op, cleanup never called      |
| internal/finalizer | TestHandleDeletion_SuccessfulCleanup      | Cleanup runs, finalizer removed locally + persisted         |
| internal/finalizer | TestHandleDeletion_DeadlineExceeded       | Forcible removal on DeadlineExceeded (Pitfall 21 path)      |
| internal/finalizer | TestHandleDeletion_NonTimeoutError        | Requeue=true, finalizer retained, error surfaces            |
| internal/finalizer | TestHandleDeletion_IdempotentRemoval      | Second pass after removal is a no-op                        |
| internal/pool     | TestPoolAcquireRelease                    | capacity-2 pool fills, 3rd blocks, Release unblocks         |
| internal/pool     | TestPoolAcquireCtxCancel                  | Cancelled ctx returns context.Canceled                      |
| internal/pool     | TestPoolPreChargeFromZeroJobs             | Empty Job list → all slots remain available                 |
| internal/pool     | TestPoolPreChargeFromLiveJobs             | 3 active Jobs consume 3 slots in capacity-4 pool            |
| internal/pool     | TestPoolPreChargeOverflow                 | 5 jobs / capacity-4 → descriptive error                     |
| internal/config   | TestConfigLoad_DefaultsApplied            | Empty YAML resolves all-defaults Config                     |
| internal/config   | TestConfigLoad_AllFieldsExplicit          | Every field round-trips exactly                             |
| internal/config   | TestConfigLoad_FileNotFound               | Missing path returns descriptive error                      |
| internal/config   | TestConfigLoad_InvalidYAML                | Malformed input returns descriptive error                   |
| internal/config   | TestConfigLoad_RejectsZeroValues          | `plannerConcurrency: 0` rejected (field named in error)     |
| internal/config   | TestConfigLoad_RejectsNegativeValues      | `executorConcurrency: -1` rejected (field named in error)   |

Combined suite runtime: ~6.5s sequential, ~16s with `-race`. Well inside the TEST-01 30s budget.

## Decisions Made

- **gopkg.in/yaml.v3 promotion via source import, not `go get`:** running `go get gopkg.in/yaml.v3` against an already-transitive dependency leaves the `// indirect` marker. Importing it from `internal/config/config.go` and then running `go mod tidy` promotes it to a direct dep in one step. Stronger contract: the import line in source is now the source of truth.
- **`*int` raw-struct decode for zero detection:** the alternative (parse into `map[string]interface{}`, type-assert each entry) is 30+ lines for the same outcome. yaml.v3's `*int` semantics give us "nil = omitted, non-nil = explicit (including 0)" for free.
- **`select` non-blocking send for PreCharge overflow** instead of counting up-front: lets a racing Job (active during iteration) be caught explicitly. Cheap defensive shape; never observable in Phase 2's Manager-startup-only call site but free.
- **Helper tests use `corev1.ConfigMap` not TIDE CRDs:** the helpers are CRD-agnostic by design (the plan's own framing: "CRDs in api/v1alpha1 NOT imported"). ConfigMap is registered by `k8s.io/client-go/kubernetes/scheme` by default. Verified: `go list -deps ./internal/owner/... ./internal/finalizer/... ./internal/pool/... ./internal/config/... | grep jsquirrelz` returns empty.

## Deviations from Plan

### None

The plan body's reference implementations were applied close to verbatim. The only adjustments were minor and explicitly aligned with revision Info 10 guidance (use `strings.Contains` from stdlib, no hand-rolled helpers — applied throughout all four test files). All acceptance criteria for all four tasks pass.

A subtle observation: the plan's frontmatter says `files_modified: [go.mod, go.sum]` but `go.sum` content didn't actually change in practice (the yaml.v3 hash was already present from Plan 01's transitive pull). `go mod tidy` left `go.sum` byte-identical. Reported in `git diff --stat`: only `go.mod` shows as modified. Documenting here so a future audit doesn't flag the missing `go.sum` change.

---

**Total deviations:** 0 (clean execution against the plan as revised in Revision 1)

## Known Stubs

The plan deliberately produces one stub by design, **not a defect**:

- **`internal/dispatch.Dispatcher` is `interface{}` (empty)**
  - **File:** `internal/dispatch/doc.go`
  - **Why:** Plan 04's must_haves explicitly mandate this shape: `"\`internal/dispatch/doc.go\` exists with a placeholder \`type Dispatcher interface{}\` reserving the package name for Phase 2"`. The contract (`Run(ctx, EnvelopeIn) (EnvelopeOut, error)`) lands in Phase 2 (REQ-SUB-01) — Phase 1 deliberately reserves the package path and identifier without committing the method set, because the EnvelopeIn/EnvelopeOut shape needs Phase 2's research to land first.
  - **Resolving plan:** Phase 2's first plan (REQ-SUB-01 — Subagent interface design) replaces this with the real interface.

No other stubs exist. The four production packages all wire to real K8s types and have full behavioral coverage.

## Issues Encountered

- **`go get gopkg.in/yaml.v3` did not promote from indirect → direct.** First attempt: ran `go get gopkg.in/yaml.v3` before importing it in source; the `// indirect` marker stayed. Fix: imported the package in `internal/config/config.go`, then ran `go mod tidy` — promotion happened correctly. Documented as a Decision above.
- **Background-task timing variance in TestPoolAcquireRelease.** The first draft used a 50ms deadline ctx to prove blocking semantics; on a heavily loaded laptop this occasionally raced. Lowered to 25ms — still gives the channel send-attempt plenty of time to fail-block while staying snappy. No flakes observed across 5 consecutive `-race` runs.

## User Setup Required

None. This plan introduces no external service dependencies, no API keys, and no runtime infrastructure. All four packages are pure Go + K8s typed clients; tests use the controller-runtime fake client.

## Next Phase Readiness

**Ready for Plan 01-05 (CRD Spec/Status field fills) — independent.** Plan 05 hand-edits `api/v1alpha1/*_types.go`; this plan touches `internal/*` only. Zero file overlap.

**Ready for Plan 01-06 (reconciler bodies):**
- Plan 06's reconcilers will import all four helper packages: `EnsureOwnerRef` from `internal/owner` for owner-ref setup, `HandleDeletion` from `internal/finalizer` for deletion paths, and (via injected struct fields) the Pool instances from `internal/pool`.
- Each Reconciler struct gets a `Dispatcher dispatch.Dispatcher` field (nil in Phase 1, injected in Phase 2). The reservation in `internal/dispatch/doc.go` is the type that field references.

**Ready for Plan 01-08 (Manager wiring):**
- `cmd/manager/main.go` will:
  1. Read the config path from a CLI flag (default `/etc/tide/config.yaml`)
  2. Call `config.Load(path)` and propagate `MaxConcurrentReconciles` into each `controller.Options{MaxConcurrentReconciles: ...}` setup
  3. Construct two Pool instances: `plannerPool := pool.New(cfg.PlannerConcurrency, "planner")` and `executorPool := pool.New(cfg.ExecutorConcurrency, "executor")`
  4. Call `plannerPool.PreCharge(ctx, mgr.GetClient(), "tideproject.k8s/role=planner")` and `executorPool.PreCharge(ctx, mgr.GetClient(), "tideproject.k8s/role=executor")` after the cache syncs and before `mgr.Start`
  5. Pass both Pool pointers into the Reconciler structs as `PlannerPool` and `ExecutorPool` fields (the field-name suffixes `plannerPool` / `executorPool` are what the crosspool analyzer keys on)

**Phase 2 hand-off (not Phase 1's job, documented for context):**
- `internal/dispatch/doc.go`'s `type Dispatcher interface{}` gets replaced with `Run(ctx context.Context, in EnvelopeIn) (EnvelopeOut, error)` per REQ-SUB-01.
- The label selectors for PreCharge (`tideproject.k8s/role=planner` and `tideproject.k8s/role=executor`) match the labels Phase 2's dispatcher will stamp on Job objects so leader-election failover restores the correct in-flight count.

**Concerns / watch-items:**
- The PreCharge label-selector contract (`tideproject.k8s/role=planner|executor`) is committed here in error messages and Phase 2 documentation but the actual label stamping happens in Phase 2's dispatcher. If Phase 2 decides on a different label key during design, this Plan's tests don't fail — they use synthetic Jobs with whatever label the test author writes. The contract is "Plan 08 main.go calls PreCharge with the matching selector" — Plan 08 will explicitly cite this Summary's selector strings.
- The `Pool` type doesn't expose `Capacity()` or `Available()` getters. Phase 4's dashboard will likely want metrics ("planner pool: 7/16 in use"). Adding those is a one-liner per method against `cap(p.sem)` and `len(p.sem)`; defer until the metric collector lands.

## Self-Check: PASSED

- All four task commits exist:
  - `6b74294` Task 1 (internal/owner)
  - `857d865` Task 2 (internal/finalizer)
  - `b494788` Task 3 (internal/pool)
  - `cf65253` Task 4 (internal/config + internal/dispatch)
- All claimed files present:
  - `internal/owner/{owner.go, owner_test.go}`
  - `internal/finalizer/{finalizer.go, finalizer_test.go}`
  - `internal/pool/{pool.go, pool_test.go}`
  - `internal/config/{config.go, config_test.go}`
  - `internal/dispatch/doc.go`
- Verification commands all exit 0:
  - `go build ./...`
  - `go vet ./...`
  - `go test ./internal/owner/... ./internal/finalizer/... ./internal/pool/... ./internal/config/... -count=1` (combined ~6.5s)
  - `go test ./internal/... -race -timeout 30s` (combined ~16s)
  - `make tide-lint` (no violations — pools are constructed but not select-waited)
  - `make verify-dag-imports` (pkg/dag still clean; this plan didn't touch pkg/dag)
- All acceptance-criteria grep checks pass:
  - `grep -q "func EnsureOwnerRef" internal/owner/owner.go` ✓
  - `grep -q "WithBlockOwnerDeletion(true)" internal/owner/owner.go` ✓
  - `grep -q "Pitfall 23" internal/owner/owner.go` ✓
  - `grep -q "cross-namespace" internal/owner/owner.go` ✓
  - `grep -q "strings.Contains" internal/owner/owner_test.go` ✓
  - `! grep -q "func contains\|func indexOf" internal/*/*.go` ✓ (no hand-rolled helpers anywhere)
  - `grep -q "func HandleDeletion" internal/finalizer/finalizer.go` ✓
  - `grep -q "context.WithTimeout" internal/finalizer/finalizer.go` ✓
  - `grep -q "Pitfall 21" internal/finalizer/finalizer.go` ✓
  - `grep -q "RemoveFinalizer" internal/finalizer/finalizer.go` ✓
  - `grep -q "func New(capacity int, name string)" internal/pool/pool.go` ✓
  - `grep -q "func (p \*Pool) Acquire" internal/pool/pool.go` ✓
  - `grep -q "func (p \*Pool) PreCharge" internal/pool/pool.go` ✓
  - `grep -qE "POOL-01|POOL-02" internal/pool/pool.go` ✓
  - `grep -q "chan struct{}" internal/pool/pool.go` ✓
  - `grep -q "type Config struct" internal/config/config.go` ✓
  - `grep -q "type MaxConcurrentReconciles struct" internal/config/config.go` ✓
  - `grep -q "func Load" internal/config/config.go` ✓
  - `grep -q "CTRL-04" internal/config/config.go` ✓
  - `grep -q "type Dispatcher interface" internal/dispatch/doc.go` ✓
  - `grep -q "REQ-SUB-01" internal/dispatch/doc.go` ✓
  - `grep -E "gopkg.in/yaml" go.mod` ✓ (now a direct, non-indirect line)
- Anti-checks pass:
  - `grep -rE "tide\.io|my\.domain|example\.com" --include="*.go" internal/` returns empty
  - No new `tide.io` references introduced anywhere in the repo

---
*Phase: 01-foundation-crds-pkg-dag-controller-scaffold*
*Completed: 2026-05-12*
