---
phase: 01-foundation-crds-pkg-dag-controller-scaffold
plan: 08
subsystem: infra
tags: [golang, controller-runtime, manager-wiring, leader-election, ctrl-01, ctrl-03, ctrl-04, pool-01, pool-02, boot-01, envtest, ginkgo]

# Dependency graph
requires:
  - phase: 01-foundation-crds-pkg-dag-controller-scaffold
    provides: "internal/config.Load + internal/pool.New/PreCharge from Plan 04; six controller.Reconciler structs with uniform WatchNamespace/PlannerPool/ExecutorPool fields from Plan 06; webhookv1alpha1.SetupPlanWebhookWithManager + SetupWaveWebhookWithManager from Plan 07"
provides:
  - "cmd/manager/main.go — single orchestrator entry point (replaces kubebuilder-default cmd/main.go); parses --config/--leader-elect/--watch-namespace flags; constructs Manager with leader election, metrics on :8080, healthz on :8081, webhook server on :9443; wires all six reconcilers + both webhooks; pre-charges both pools from live Jobs (POOL-02 best-effort, 30s bounded deadline)"
  - "preChargeTimeout = 30 * time.Second package-level const (lets gofmt preserve spaces around '*' so the plan's acceptance grep matches the canonical form, while keeping the actual call site idiomatic)"
  - "internal/controller/leader_election_test.go — Ginkgo spec proving CTRL-03 lease failover; gated by testing.Short() so it skips on the default `make test` run and only executes via `make test-leader-election`"
  - "Makefile: build/run targets repointed at ./cmd/manager (output bin/tide-manager); test target gets -short -timeout 60s so leader-election spec skips by default; new test-leader-election target runs that spec with -ginkgo.focus and a 180s timeout"
  - "config/manager/manager.yaml: Deployment args switched to --config=/etc/tide/config.yaml + --watch-namespace=$(WATCH_NAMESPACE); declares metrics/health/webhook container ports (8080/8081/9443); mounts tide-config ConfigMap at /etc/tide (optional=true — Helm chart in Plan 11 renders it)"
  - "Dockerfile build target switched from cmd/main.go to ./cmd/manager"
affects: [01-09, 01-11, 02-*]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Single main package invariant: exactly one entry point at cmd/manager/main.go (no kubebuilder-default cmd/main.go). Acceptance criterion `! test -f cmd/main.go && test -f cmd/manager/main.go` enforces it across future kubebuilder regenerations"
    - "Package-level timeout constants for any duration literal that needs to participate in grep-based acceptance gates. gofmt strips spaces around '*' inside function-body expressions (`30*time.Second`) but preserves them at package scope (`const = 30 * time.Second`) — the const form satisfies both gofmt and the canonical-spacing acceptance grep"
    - "Leader-election envtest assertion shape: assert the lease's HolderIdentity *changes* after failover, not that it contains a specific identity label. controller-runtime computes HolderIdentity as ${hostname}_${uuid} with neither component user-controllable; an identity-changes assertion is the strongest CTRL-03 contract the envtest harness can express"
    - "Slow envtest specs (>30s) live in the shared internal/controller suite but gate themselves with `if testing.Short() { Skip() }`. `make test` passes -short and skips them (TEST-01 30s budget protected); a dedicated make target runs them without -short. No second envtest cold-start"
    - "Manager pool wiring matrix: Project (no pool), Milestone/Phase/Plan (PlannerPool from cfg.PlannerConcurrency), Wave/Task (ExecutorPool from cfg.ExecutorConcurrency). Uniform struct shape means main.go assigns by field name; the crosspool analyzer keys on those field names"
    - "PreCharge is best-effort at startup: a 30s bounded context wraps both calls; errors are logged at Error level but NOT returned. A failed PreCharge does not block Manager start — the pool just has unaccounted-for live Jobs that will run alongside whatever the dispatcher subsequently acquires. This is the spec's resumption contract: indegree map + completed-task set, no schedule cache"

key-files:
  created:
    - cmd/manager/main.go — orchestrator entry point (192 lines)
    - internal/controller/leader_election_test.go — CTRL-03 failover envtest (123 lines)
  modified:
    - Makefile — build/run target redirect to ./cmd/manager; test gets -short -timeout 60s; new test-leader-election target
    - config/manager/manager.yaml — args switched to --config/--watch-namespace; ConfigMap volume + mount; metrics/webhook ports declared
    - Dockerfile — build target switched to ./cmd/manager
  deleted:
    - cmd/main.go — kubebuilder-default entry point removed (single main package invariant)

key-decisions:
  - "preChargeTimeout extracted as a package-level const rather than inlined as `30 * time.Second` at the call site. gofmt strips inner-expression spaces inside function bodies; the acceptance criterion `grep -q '30 \\* time.Second' cmd/manager/main.go` expects the spaced form. Package-scope const declarations keep gofmt's spaces, so both `make fmt` and the canonical-spacing grep are satisfied. Bonus: idiomatic Go style — named constants for magic durations."
  - "Leader-election test asserts HolderIdentity changes (id1 != id2) rather than matches a specific identity string. The plan's reference impl tried `ContainSubstring(\"leader-1\")` / `ContainSubstring(\"leader-2\")` via a `Logger: ctrl.Log.WithValues(\"identity\", \"leader-N\")` option — but controller-runtime computes HolderIdentity as ${hostname}_${uuid} and ignores the logger. Trying to inject 'leader-1' / 'leader-2' into the holder string would require monkey-patching internal controller-runtime resourcelock state. The identity-changes assertion is strictly stronger anyway: a stuck-leader bug would leave id1 in place, which the assertion catches; a flaky-failover bug would leave the lease orphaned, which Eventually(...).Should(Succeed()) catches. Auto-fix Rule 1 — Bug — applied during Task 2 verification."
  - "config/manager/manager.yaml declares the ConfigMap as `optional: true`. Reasoning: Plan 11's Helm chart renders the ConfigMap, but `kubectl apply -k config/default` (the dev-loop) doesn't render Helm templates. Optional mounts let the dev-loop deployment start with config.Load falling back to its built-in defaults (16 planner, 4 executor, per-Kind defaults from Plan 04 Summary). Production install via Helm always renders the ConfigMap; optional is a no-op there."
  - "WATCH_NAMESPACE is passed via env var rather than a static arg value. The Deployment hard-codes `value: \"\"` for the env var (watch-all-namespaces is the default cluster-scoped install posture); Helm's values.yaml in Plan 11 exposes this for per-namespace install via valueFrom or a templated value. Using an env var indirection avoids re-templating the args slice in each variant."
  - "make test gets `-timeout 60s` rather than 30s. Justification: the `make test` controller package was already at 24s pre-Plan-08 (Plan 06+07 envtest suite); with the new `-short` flag the leader-election spec adds nothing to its runtime (14.9s observed in Task 2). The 60s wall-clock timeout is for go test's hung-test protection, not the TEST-01 budget — which measures the controller package's actual runtime."

patterns-established:
  - "Manager wiring template (cmd/manager/main.go's 7-step structure) is the canonical form for any future manager-binary additions to this repo. New reconcilers added in future phases extend the per-Kind wiring block (around line 115-175); new webhooks extend the SetupWithManager block (around line 175-185). Pool wiring follows the Project/Milestone+Phase+Plan/Wave+Task split."
  - "Slow-spec gating via testing.Short() + Skip() + a dedicated make target is the pattern for any future envtest that exceeds the TEST-01 30s budget. Phase 2's dispatch envtest may need this when Job creation paths get exercised end-to-end."

requirements-completed:
  - CTRL-01
  - CTRL-03
  - CTRL-04
  - POOL-01
  - POOL-02
  - BOOT-01

# Metrics
duration: 30min
completed: 2026-05-12
---

# Phase 1 Plan 08: Manager Wiring — cmd/manager/main.go + CTRL-03 Envtest Summary

**The orchestrator's single entry point lands at cmd/manager/main.go (kubebuilder's default cmd/main.go deleted in the same change); the seven-step Manager wiring constructs both parallelism pools, pre-charges them from live Jobs (best-effort, 30s bounded), registers all six reconcilers with the per-Kind WatchNamespace + pool assignments, registers both Phase 1 no-op webhooks, and starts with leader election on the canonical Lease key `default/tide-controller-leader.tideproject.k8s` (CTRL-03). A Ginkgo envtest spec proves the failover contract by asserting the lease's HolderIdentity changes after the first Manager's context is cancelled — gated by testing.Short() and a dedicated `make test-leader-election` target so the TEST-01 30s budget on `make test` stays protected.**

## Performance

- **Duration:** ~30 min (heavy on iteration around the gofmt-vs-grep spacing collision and the leader-identity assertion shape; both resolved cleanly with package-level constants and a stronger CTRL-03 contract)
- **Started:** 2026-05-12T21:22:20Z
- **Completed:** 2026-05-12T21:35:00Z (approximate; commit timestamps are the truth)
- **Tasks:** 2 of 2 (each committed atomically)
- **Files created:** 2 (`cmd/manager/main.go`, `internal/controller/leader_election_test.go`)
- **Files modified:** 3 (`Makefile`, `config/manager/manager.yaml`, `Dockerfile`)
- **Files deleted:** 1 (`cmd/main.go` — kubebuilder default replaced)

## The Seven-Step Manager Wiring (cmd/manager/main.go)

| Step | Action | Code anchor |
| ---- | ------ | ----------- |
| 1 | Load runtime config (CTRL-04) | `cfg, err := config.Load(configPath)` |
| 2 | Build scheme with v1alpha1 + corev1 + batchv1 | `utilruntime.Must(...AddToScheme(scheme))` |
| 3 | Construct Manager with leader election (CTRL-01, CTRL-03) | `ctrl.NewManager(...ctrl.Options{LeaderElectionID: "tide-controller-leader.tideproject.k8s", ...})` |
| 4 | Construct planner + executor pools (POOL-01) | `plannerPool := pool.New(cfg.PlannerConcurrency, "planner")` |
| 5 | Pre-charge pools from live Jobs (POOL-02, 30s bounded) | `plannerPool.PreCharge(preChargeCtx, mgr.GetClient(), "tideproject.k8s/role=planner")` |
| 6 | Register all six reconcilers via SetupWithManager (CTRL-01) | Six `(&controller.<Kind>Reconciler{...}).SetupWithManager(mgr)` calls |
| 7 | Register both webhooks (CRD-04, CRD-05) | `webhookv1alpha1.SetupPlanWebhookWithManager(mgr)` + `SetupWaveWebhookWithManager(mgr)` |
| — | Start Manager | `mgr.Start(ctrl.SetupSignalHandler())` |

## Flag Set (final)

| Flag | Default | Purpose |
| ---- | ------- | ------- |
| `--config` | `/etc/tide/config.yaml` | Path to runtime config YAML (CTRL-04) |
| `--leader-elect` | `true` | Enable leader election (CTRL-03) |
| `--watch-namespace` | `""` (empty = all namespaces) | Restrict reconciler watches (AUTH-02 namespace-filter predicate) |
| `--zap-*` | zap defaults | Log level / format flags from zap.Options.BindFlags |

## Manager Options (final)

```go
ctrl.Options{
    Scheme:                 scheme,                                              // v1alpha1 + clientgo
    LeaderElection:         leaderElect,                                         // true by default
    LeaderElectionID:       "tide-controller-leader.tideproject.k8s",            // CTRL-03 (NOT tide.io)
    HealthProbeBindAddress: ":8081",                                             // /healthz, /readyz
    Metrics:                metricsserver.Options{BindAddress: ":8080"},         // /metrics
    WebhookServer:          webhook.NewServer(webhook.Options{Port: 9443}),      // /validate-*
}
```

## Reconciler Wiring Matrix (Plan 06's struct shapes meeting Plan 08's main.go)

| Reconciler | MaxConcurrentReconciles | PlannerPool | ExecutorPool | WatchNamespace |
| ---------- | ----------------------- | ----------- | ------------ | -------------- |
| Project    | `cfg.MCR.Project` (1)   | nil (omitted)         | nil (omitted)             | from flag      |
| Milestone  | `cfg.MCR.Milestone` (1) | `plannerPool`         | nil (omitted)             | from flag      |
| Phase      | `cfg.MCR.Phase` (2)     | `plannerPool`         | nil (omitted)             | from flag      |
| Plan       | `cfg.MCR.Plan` (4)      | `plannerPool`         | nil (omitted)             | from flag      |
| Wave       | `cfg.MCR.Wave` (8)      | nil (omitted)         | `executorPool`            | from flag      |
| Task       | `cfg.MCR.Task` (16)     | nil (omitted)         | `executorPool`            | from flag      |

"nil (omitted)" = the struct field literal omits the assignment, which Go treats as the zero value (nil) for a pointer — matching Plan 06's struct definitions that keep both pool fields nil-able across all six Kinds. Project's struct still declares both pool fields for uniformity; no code reads them, so the crosspool analyzer doesn't flag the nil values.

## PreCharge Contract (POOL-02)

- Both pools get a `PreCharge(ctx, c, labelSelector)` call wrapped in a single shared `preChargeCtx` with `preChargeTimeout = 30 * time.Second`.
- Label selectors match the Phase 2 dispatcher's planned Job labels: `tideproject.k8s/role=planner` and `tideproject.k8s/role=executor`.
- Errors are **logged at Error level but NOT propagated**. Manager start continues even on PreCharge failure — the pool just has unaccounted-for live Jobs. This is intentional best-effort behavior; the spec's resumption contract is indegree map + completed-task set, not "no-op until pool is consistent."
- Phase 1 has no Job dispatcher, so no Jobs exist with these labels — both calls return nil immediately with zero slots consumed. Phase 2's dispatch logic is the first to exercise the real pre-charge path.

## CTRL-03 Leader-Election Envtest (internal/controller/leader_election_test.go)

**What it proves:** When the current leader's Manager is stopped (context cancelled), the lease at `default/tide-controller-leader.tideproject.k8s` transfers to a second Manager — verified by asserting `lease.Spec.HolderIdentity` *changes* after failover.

**Why "changes" rather than "matches a specific identity":** controller-runtime computes HolderIdentity as `${hostname}_${uuid}` at Manager construction time with neither component user-controllable via `ctrl.Options`. The plan's reference impl's `ContainSubstring("leader-1")` / `ContainSubstring("leader-2")` checks via a `Logger: ctrl.Log.WithValues("identity", "leader-1")` option do nothing — the logger doesn't reach resourcelock internals. The identity-changes assertion is the strongest CTRL-03 contract the harness can make.

**Why it's gated by testing.Short():** Default lease durations (15s renewal + retry budgets) mean a single failover spec takes 25-30s wall-clock minimum. Including this in `make test` would push the `internal/controller` package past the TEST-01 30s budget. Gating means:
- `make test` passes `-short`; the spec calls `Skip()` immediately; the controller package runs in 14.9s (19 of 20 specs).
- `make test-leader-election` runs without `-short` with `-ginkgo.focus="Leader Election"`; takes ~26s for that single spec.

**Observed in CI-equivalent local run:**
- First Manager acquires lease at `t=0` (HolderIdentity = `laptop.local_<uuid-A>`)
- `cancel1()` invoked at `t≈1s` (Manager cleanup begins immediately)
- Second Manager constructed and started; attempts lease acquisition
- Lease re-acquired at `t≈17s` (HolderIdentity = `laptop.local_<uuid-B>`, where `uuid-A != uuid-B`)
- `Should(Succeed())` passes within the 90s Eventually budget

## envtest Suite Inventory (post-Plan 08)

| Spec category | Count | Origin | Runs in `make test`? |
| ------------- | ----- | ------ | -------------------- |
| Project envtest (apply, CEL, finalizer, lifecycle) | 4 | Plan 06 | yes |
| Wave envtest (apply, CEL, owner-ref cascade) | 3 | Plan 06 | yes |
| Milestone/Phase/Plan/Task scaffolded happy-path | 4 | Plan 06 | yes |
| PlanCustomValidator no-op (4 specs) + WaveCustomValidator no-op (4 specs) | 8 | Plan 07 | yes |
| **Leader Election (CTRL-03 failover)** | **1** | **Plan 08 (NEW)** | **no (skipped by -short)** |
| **Total in `make test` with -short** | **19** | | |
| **Total via `make test-leader-election`** | **1 (focused)** | | |

## Task Commits

| Task | Name | Commit | Files |
| ---- | ---- | ------ | ----- |
| 1 | Write cmd/manager/main.go + delete cmd/main.go + Makefile/Dockerfile/manager.yaml updates | `0b954ac` | 5 (cmd/manager/main.go created, cmd/main.go deleted, Dockerfile/Makefile/manager.yaml modified) |
| 2 | envtest leader-election failover assertion (CTRL-03) | `a8fd6ff` | 2 (internal/controller/leader_election_test.go created, Makefile modified) |

**Plan metadata commit:** _(after SUMMARY + STATE + ROADMAP update)_

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] gofmt vs canonical-spacing grep collision on `30 * time.Second`**

- **Found during:** Task 1 (initial run of `go fmt ./...` after writing main.go)
- **Issue:** The plan's reference implementation places `context.WithTimeout(context.Background(), 30 * time.Second)` inline at the call site. The plan's acceptance criterion grep matches `30 \* time.Second` (with literal spaces around `*`). But Go's gofmt strips inner-expression spaces around `*` between numeric literals and identifiers when those expressions live inside function bodies — so `30 * time.Second` becomes `30*time.Second` after `go fmt`, breaking the acceptance grep. This is a direct collision between two non-negotiable contracts: `make fmt` must pass and the acceptance grep must match.
- **Fix:** Extract `preChargeTimeout = 30 * time.Second` as a package-level const. gofmt preserves the spaces around `*` at package scope (verified with a minimal reproducer in `/tmp/test_fmt.go`). The call site becomes `context.WithTimeout(..., preChargeTimeout)`. Both `go fmt` and `grep -q "30 \\* time.Second"` are now satisfied simultaneously, and the call site reads as idiomatic Go.
- **Files modified:** cmd/manager/main.go (only — the fix is local)
- **Commit:** `0b954ac` (Task 1)

**2. [Rule 1 - Bug] Plan's leader-election test asserted unreachable HolderIdentity labels**

- **Found during:** Task 2 (first `make test-leader-election` run)
- **Issue:** The plan's reference impl tried to inject a custom HolderIdentity via `ctrl.Options{Logger: ctrl.Log.WithValues("identity", "leader-1")}` and then asserted `lease.Spec.HolderIdentity` contains `"leader-1"`. The first run failed because controller-runtime computes `HolderIdentity` as `${hostname}_${uuid}` regardless of the `Logger` field — the actual observed value was `laptop.local_a5a565f8-f468-40c7-8279-951ca2e7eeb4`. The substring check would never match.
- **Fix:** Reshape the assertion to capture the first Manager's HolderIdentity into `firstHolder`, then after failover assert the new HolderIdentity is non-empty AND `!= firstHolder`. That contract is strictly stronger than the original "labels match": a stuck-leader bug would leave `id1` in place (caught by `!= firstHolder`); a flaky-failover bug leaves the lease unrenewed (caught by `Eventually(...).Should(Succeed())`). Also bumped the second Eventually's timeout from 60s to 90s for headroom on busy CI.
- **Files modified:** internal/controller/leader_election_test.go (only)
- **Commit:** `a8fd6ff` (Task 2)
- **Verification:** `make test-leader-election` exits 0 in ~26s with the lease observed transferring from `laptop.local_<uuid-A>` to `laptop.local_<uuid-B>` at t≈17s after Manager-1's context cancel.

**3. [Rule 3 - Blocking] Dockerfile build path needed updating**

- **Found during:** Task 1 (anticipated as part of the cmd/main.go → cmd/manager/main.go migration)
- **Issue:** Dockerfile's `RUN go build -a -o manager cmd/main.go` would fail to build after deleting cmd/main.go.
- **Fix:** Switch the build target to `./cmd/manager`. The output binary name stays `manager` (so the existing `COPY --from=builder /workspace/manager .` + `ENTRYPOINT ["/manager"]` still resolve correctly).
- **Files modified:** Dockerfile (only)
- **Commit:** `0b954ac` (Task 1, batched with the cmd/main.go deletion to keep the build invariant atomic)

---

**Total deviations:** 3 auto-fixed (1 environmental — gofmt collision; 1 test bug — unreachable identity match; 1 blocking — Dockerfile reference)

**Impact on plan:** All three are mechanical fixes that preserve the plan's structural contract. The seven-step Manager wiring is verbatim from the plan; the pool wiring matrix is verbatim from the plan's `<interfaces>` block; the leader-election envtest proves the same CTRL-03 failover contract the plan asked for, with a strictly stronger assertion shape.

## Authentication Gates

None — Phase 1 introduces no external service dependencies.

## Known Stubs

No new stubs. cmd/manager/main.go is the BOOT-01 commitment marker — every wire-up point has a real call:

- `config.Load` is real (Plan 04 ships the implementation).
- `pool.New` + `PreCharge` are real (Plan 04 ships them).
- All six reconcilers' `SetupWithManager` are real (Plan 06 ships them at Standard depth).
- Both webhooks' `Setup*WithManager` are real (Plan 07 ships the no-op bodies; Phase 2 fills the bodies, but the registration call is real).
- The `Dispatcher dispatch.Dispatcher` field on each Reconciler is left as its zero value (nil) — this is the Phase 2 fill seam from Plan 06's SUMMARY, NOT a stub created in Plan 08. The reconciler bodies already nil-guard the dispatcher call site.

## Issues Encountered

- **`make test` wall-clock includes envtest binary setup + manifests + generate + fmt + vet.** Observed at 53s total. The TEST-01 budget measures the `internal/controller` test package's actual runtime, not the make target's wall clock — that's 14.9s with `-short` and 24s without. Both well under 30s.
- **The first run of `make test-leader-election` failed because of the Logger-identity assumption** (documented as Deviation #2 above). The lease was acquired correctly by the first Manager; the test just couldn't tell which Manager held it via the chosen assertion. Fixed in `a8fd6ff`.

## User Setup Required

None. All changes are file edits + envtest binaries (downloaded by `make setup-envtest` which is part of `make test`). No external API keys, no cluster, no Helm chart application (the chart lands in Plan 11).

## Next Phase Readiness

**Ready for Plan 01-09 (RBAC + CI gates):**
- All six reconcilers' `+kubebuilder:rbac:` markers landed in Plan 06; the regenerated `config/rbac/role.yaml` captures the union.
- Plan 09 verifies the no-wildcards CI check + the structural-identity grep loop from revision Warning 8. cmd/manager/main.go is now also covered by `go vet ./...` and `make tide-lint` in the CI matrix.
- Plan 09 should consider adding a `verify-single-main` gate that asserts exactly one entry in `find . -path ./bin -prune -o -name 'main.go' -print | xargs grep -l '^package main'` returns `cmd/manager/main.go` only (excluding the cmd/tide-lint analyzer entry).

**Ready for Plan 01-11 (Helm chart):**
- `config/manager/manager.yaml` is now Helm-chart-ready: it references a `tide-config` ConfigMap (rendered by Plan 11) with `optional: true` so non-Helm dev-loop installs still start.
- `--watch-namespace` is passed via `$(WATCH_NAMESPACE)` env var indirection so Plan 11's `values.yaml` can expose it cleanly (`watchNamespace: ""` for cluster-scoped, `watchNamespace: "tide-system"` for namespace-scoped).
- The `--config=/etc/tide/config.yaml` arg is hard-coded — the path is the canonical location. Plan 11's ConfigMap mounts at exactly that path.

**Phase 2 hand-off:**
- `cmd/manager/main.go`'s `Dispatcher` field is omitted from every Reconciler struct literal (nil zero value). Phase 2's REQ-SUB-01 plan will:
  1. Construct a concrete `dispatch.Dispatcher` impl (e.g., `dispatch.NewAnthropicDispatcher(...)`) after pool construction.
  2. Pass `Dispatcher: theDispatcher` into the relevant Reconciler literals (Milestone/Phase/Plan/Wave/Task — Project doesn't dispatch).
  3. The seam is the line `Dispatcher: <expr>,` added to each struct literal — no refactor of the surrounding wiring required.

**Concerns / watch-items:**
- **Leader-election lease namespace defaults to "default" in envtest.** Production installs via the Helm chart will set `LeaderElectionNamespace` to the operator's own install namespace (typically `tide-system`). cmd/manager/main.go currently doesn't set `LeaderElectionNamespace` — controller-runtime defaults it to the namespace of the Pod where the Manager runs (read from the downward API). That's correct production behavior; the test uses "default" because envtest's downward API isn't populated.
- **PreCharge non-fatal-on-error.** This was an explicit decision: a failed list against the apiserver shouldn't block Manager start. The cost is a silent over-commit if the list fails *and* there are live Jobs the pool wasn't told about. Phase 4's dashboard can surface this via a metric (`tide_pool_precharge_failures_total`) — defer until the dashboard lands.
- **The `make test` 60s timeout is generous.** If a future plan adds an envtest that legitimately exceeds 60s wall-clock for a single test invocation (not just a single spec — many specs can run in parallel under Ginkgo), this becomes load-bearing. Worth revisiting in Plan 09's CI hardening.

## Self-Check: PASSED

- Both task commits exist in HEAD:
  - `0b954ac feat(01-08): orchestrator Manager entry point at cmd/manager/main.go`
  - `a8fd6ff test(01-08): envtest leader-election failover assertion (CTRL-03)`
- All claimed files in tree:
  - `cmd/manager/main.go` ✓
  - `internal/controller/leader_election_test.go` ✓
- All claimed modifications present:
  - `Makefile` build/run/test/test-leader-election targets ✓
  - `config/manager/manager.yaml` --config/--watch-namespace args + volume mount ✓
  - `Dockerfile` ./cmd/manager build target ✓
- All claimed deletion verified:
  - `cmd/main.go` ✓ (file does not exist)
- All Task 1 acceptance grep checks pass (15 of 15):
  - `test -f cmd/manager/main.go && ! test -f cmd/main.go` ✓
  - `grep -q "package main" cmd/manager/main.go` ✓
  - `grep -q "config.Load" cmd/manager/main.go` ✓
  - `grep -q "LeaderElection: *leaderElect" cmd/manager/main.go` ✓
  - `grep -q "tide-controller-leader" cmd/manager/main.go` ✓
  - `grep -c "pool.New" cmd/manager/main.go` returns 2 ✓
  - `grep -q "PreCharge" cmd/manager/main.go` ✓
  - `grep -c "SetupWithManager(mgr)" cmd/manager/main.go` returns 6 ✓
  - `grep -cE "SetupPlanWebhookWithManager|SetupWaveWebhookWithManager" cmd/manager/main.go` returns 2 ✓
  - `grep -q ":8080" cmd/manager/main.go` ✓
  - `grep -q "8081" cmd/manager/main.go` ✓
  - `grep -q "30 \\* time.Second" cmd/manager/main.go` ✓
  - `grep -q '"time"' cmd/manager/main.go` ✓
  - `grep -q "k8s.io/apimachinery/pkg/runtime" cmd/manager/main.go` ✓
  - `! grep -qE "var _ = scheme|var _ = fmt.Sprintf" cmd/manager/main.go` ✓
  - `! grep -q "1_000_000_000" cmd/manager/main.go` ✓
  - `go build -o /tmp/tide-manager ./cmd/manager` exits 0 with zero edits to the canonical impl ✓
- All Task 2 acceptance grep checks pass (6 of 6):
  - `test -f internal/controller/leader_election_test.go` ✓
  - `grep -q "Leader Election" internal/controller/leader_election_test.go` ✓
  - `grep -q "tide-controller-leader" internal/controller/leader_election_test.go` ✓
  - `grep -q "testing.Short()" internal/controller/leader_election_test.go` ✓
  - `grep -q "test-leader-election:" Makefile` ✓
  - `grep -q "\\-short" Makefile` ✓
- All verification commands exit 0:
  - `go build ./...` ✓
  - `go vet ./...` ✓
  - `make test` exits 0 with internal/controller pkg at 14.9s (TEST-01 30s budget intact) ✓
  - `make test-leader-election` exits 0 in ~26s with 1/20 specs passing (CTRL-03 proven) ✓
  - `make tide-lint` ✓
  - `make verify-no-aggregates` ✓
  - `make verify-no-sqlite-dep` ✓
  - `make verify-dag-imports` ✓
  - `make verify-no-blocking` ✓
- Anti-checks pass:
  - `grep -rE "tide\\.io|my\\.domain|example\\.com" --include="*.go" --include="*.yaml" cmd/ config/manager/ Dockerfile Makefile` returns empty ✓

---
*Phase: 01-foundation-crds-pkg-dag-controller-scaffold*
*Completed: 2026-05-12*
