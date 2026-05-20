# TIDE codebase audit — merged report

**Date:** 2026-05-20
**Sources merged:**
- `docs/audit-report-2026-05-20.md` (production-wiring focus)
- `docs/codebase_audit_report.md` (subsystem deep-dives)
- `docs/audit-report-2026-05-20-opus-4-7.md` (discipline/patterns; Claude Opus 4.7)

**Merge method.** All findings from each source were re-verified against the
current tree with independent `Explore` subagents reporting file:line evidence
before inclusion. Claims that failed verification appear in §6 (Refuted /
corrected). Best practices and subsystem documentation use the most precise
source available; findings are priority-ordered (P1 ship-blockers → P4
improvements).

---

## 1. Executive summary

TIDE shows architectural discipline well above the median Kubernetes operator
project — invariants are encoded as runnable checks (custom analyzers + shell
guards + in-source tests), the Reconcile pattern is uniform across six
reconcilers, and secret isolation is a structural property of the dispatch
design rather than a runtime convention.

The discipline does not extend evenly to **production wiring**. Four real
defects sit between the implemented mechanism and what actually executes when
the manager starts: one reconciler is wired without its dispatcher, planner
Jobs run a skeletal pod spec that doesn't honor the same execution contract as
task Jobs, default task deadlines diverge from token validity, and parent
resolution falls back to "first Project in the namespace." None of these are
mechanism failures — the mechanisms exist and work. They are integration
failures, and each is a small, localized fix.

Beyond P1, the housekeeping work is light: one dead analyzer, two CI parity
gaps, and a documentation-only budget feature.

---

## 2. Verified best practices

### 2.1 Architectural invariants encoded as runnable checks
Three independent enforcement layers protect different invariants:

- **Custom Go static analyzers** registered in `cmd/tide-lint`, run via
  `make tide-lint`:
  - `tools/analyzers/crosspool/analyzer.go:1` — rejects `select` statements
    that wait on both planner and executor pool channels (POOL-03 / Pitfall 6).
  - `tools/analyzers/providerfirewall/analyzer.go:1` — rejects LLM SDK
    imports from orchestrator-side packages (`pkg/controller/*`,
    `pkg/dispatch/*`, `pkg/dag/*`, `internal/controller/*`,
    `internal/webhook/*`, `internal/dispatch/*`).
  - `tools/analyzers/metriccardinality/doc.go:1` — rejects the literal
    `"task"` label in any `prometheus.New*Vec` call (OBS-02 / Pitfall 17).
- **Shell `verify-*` Makefile targets** for invariants grep can express
  cheaply: `verify-dag-imports`, `verify-no-aggregates`,
  `verify-no-sqlite-dep`, `verify-no-rbac-wildcards`,
  `verify-rbac-marker-discipline`, `verify-no-blocking`. Each appears as a
  named step in `.github/workflows/ci.yaml:51-66`.
- **In-source guard tests** for things a regex won't catch:
  `internal/controller/rbac_guard_test.go:33,62,89` asserts the wildcard-RBAC
  regex catches violations and stays silent on clean files.

### 2.2 Secret isolation as a structural property
- **Credential proxy** (`internal/credproxy/server.go`) is a per-Task sidecar.
  It mints a self-signed TLS cert at startup, listens on `127.0.0.1:8443`,
  swaps the per-Task HMAC bearer token for the real `ANTHROPIC_API_KEY`, and
  forwards to `api.anthropic.com`. The controller never sees the API key.
  Token layout (`internal/credproxy/token.go:13-27`): 16-byte nonce + 8-byte
  big-endian Unix expiry + 32-byte HMAC-SHA256 MAC (56 bytes total). The
  per-Task `taskUID` is bound into the MAC signature but **not** stored inside
  the token bytes, so a stolen token can't be replayed against a different
  Task pod (the verifier reads `TIDE_TASK_UID` from its own pod env).
- **Upstream route allowlist** (`internal/credproxy/server.go:41-46`):
  only `POST /v1/messages` and `POST /v1/messages/count_tokens` are
  forwarded. Other paths return 403. Defense in depth against a compromised
  subagent.
- **Push boundary** is its own pod (`cmd/tide-push/main.go`). The repository
  credential never enters the controller manager.

### 2.3 Six-step Reconcile pattern, applied uniformly
All six reconcilers (`Project`, `Milestone`, `Phase`, `Plan`, `Wave`, `Task`)
follow: Fetch → DeletionTimestamp → Finalizer → OwnerRef → Dispatcher / business
seam → Status update. Divergences are documented: `ProjectReconciler` skips
step 4 (top-level CRD); `Plan` / `Wave` / `Task` log-and-continue on missing
parent so dispatch doesn't stall.

### 2.4 Bounded-deadline finalizer
`internal/finalizer/finalizer.go:56-68` runs the cleanup callback under a
5-minute `context.WithTimeout`. On `context.DeadlineExceeded` it
**forcibly removes the finalizer** and updates the resource — a hung external
API can't trap an object in `Terminating` forever (Pitfall 21).

### 2.5 Test infrastructure
- 121 `_test.go` files vs 133 non-test, non-generated source files (≈ 0.91:1).
- Eight `test*` Make targets stratified by cost: `test` (envtest, < 30s),
  `test-int-fast` (Layer A envtest, ~90s), `test-int` (Layer A + Layer B kind,
  30 min outer / 20 min inner), `test-e2e` and `test-e2e-kind` (kind, 15 min),
  `test-e2e-live` (live Anthropic; 15-min budget; triple-gated by
  `//go:build live_e2e`, BeforeSuite env-var check, and a $1.00 hard cap via
  `Project.Spec.budget.absoluteCapCents=100`).
- Tests inject a `stubDispatcher` (`internal/controller/task_controller_test.go:48`)
  with a `var _ dispatch.Dispatcher = (*stubDispatcher)(nil)` compile-time
  assertion (line 54).

### 2.6 Single, well-leveraged field indexer
`mgr.GetFieldIndexer().IndexField(...)` registers exactly one index —
`Task.spec.planRef` at `internal/controller/task_controller.go:931` (constant
`taskPlanRefIndexKey` at line 62). It serves five call sites:
`TaskReconciler.listSiblingTasks` (line 635),
`PlanReconciler.reconcilePlannerDispatch` (line 189) and
`.reconcileWaveMaterialization` (line 481),
`WaveReconciler.reconcileObservational` (line 150),
`TaskReconciler.siblingsToTaskMapper` (line 820). No full-namespace `List`s on
the hot path.

### 2.7 Manager startup is properly fail-fast
`cmd/manager/main.go` does six things before `mgr.Start()` (line 448):
- **Fail-fast:** config load (212), signing-key length check (274),
  HMAC self-test round-trip (285), OTel boot (192), both webhooks (419–425).
- **Best-effort with logging:** planner / executor pool `PreCharge` (262, 265),
  budget `PreCharge` runnable (437–445).
- **OTel shutdown** is bounded at 5s under `context.Background()` because
  `signalCtx` is already cancelled when the defer fires (lines 196–206).

### 2.8 Forbidden patterns clean
Across `cmd/`, `internal/`, `pkg/`, `api/` (excluding tests, generated,
vendored, `.claude/`):

| pattern | hits |
|---|---|
| `TODO` / `FIXME` / `XXX` / `HACK` | 0 |
| bare `panic(` | 0 |
| `time.Sleep(` | 0 |
| `<-time.After(` | 1 — `cmd/stub-subagent/main.go:263` (intentional test-mode handler; not a reconciler) |

### 2.9 Anthropic SDK boundary tighter than the design document requires
- **Zero** Anthropic SDK imports in `cmd/`, `internal/`, or `pkg/` Go source.
- Only doc-comment references appear in `pkg/dispatch/doc.go` and
  `pkg/dag/doc.go` (explaining what is *forbidden*).
- The Anthropic client is invoked through `internal/subagent/anthropic/` and
  surfaced behind the `Subagent` interface in `pkg/dispatch/subagent.go:17`.

### 2.10 Strict lint baseline
`.golangci.yml` enables 19 linters with `default: none`: `copyloopvar, dupl,
errcheck, ginkgolinter, goconst, gocyclo, govet, ineffassign, lll, modernize,
misspell, nakedret, prealloc, revive, staticcheck, unconvert, unparam, unused,
logcheck` (last via the k8s `logcheck` module). Path-scoped relaxations: `lll`
under `api/*`; `dupl` + `lll` under `internal/*`. Formatters: `gofmt` +
`goimports`.

### 2.11 Spec → code fidelity (stack choices honored)
- Dashboard streaming is SSE (`Content-Type: text/event-stream` at
  `cmd/dashboard/api/events_sse.go:180` and `logs_sse.go:154`); zero WebSocket
  imports anywhere.
- No `argoproj` / `tektoncd` references in source.
- `charts/tide/values.yaml:269` defaults `prometheus.serviceMonitor.enabled:
  false` — matches the named anti-pattern.
- `go.mod` has no SQL driver (no `sqlite`, `pgx`, `lib/pq`, `mysql`).

### 2.12 Webhook validation
`internal/webhook/v1alpha1/plan_webhook.go`:
- Builds a DAG from child Tasks (nodes = `Task.Name`, edges =
  `(DependsOn[i], Task.Name)`), runs `dag.ComputeWaves(...)`, rejects
  admission on cycle with a `CycleDetected` warning event.
- Computes exact-equality intersection of `filesTouched` across tasks lacking
  a declared `depends_on` edge; warns or rejects per strict/warn mode.
- **Informer-cache gap handling** (`plan_webhook.go:135-145`): when the
  child-Task list returns zero due to admission ordering (Plan admitted before
  its Tasks have synced), returns an `admission.Warnings` rather than an
  error — preserves `kubectl apply -k` ergonomics while letting the
  reconcile-time path validate later.

---

## 3. Subsystem deep-dives

### 3.1 Push-boundary leak protection
`cmd/tide-push/main.go:321` runs `gitleaks.ScanDiff(diff)` against the unified
diff between the new commit and the previous head. The embedded gitleaks
v8.30.1 default ruleset (~150 secret-shape signatures) is used. On a positive
hit, `tide-push` does **not** call `git push`; it writes a push-result
envelope with reason `"leak-detected"` and exits with code 10
(`exitLeakBlocked = 10`, line 100). The controller reads the envelope, sets
`Project.Status.Phase = PhasePushLeakBlocked`
(`api/v1alpha1/project_types.go`) and increments
`tide_secret_leak_blocked_total` (the condition `Reason` field — distinct
from the phase — carries the string `"LeakDetected"`). The repository PAT
never leaves the `tide-push` Job pod.

### 3.2 Budget enforcement
`internal/budget/cap.go:18-23` enforces `AbsoluteCapCents` at dispatch time;
`TaskReconciler` checks `budget.IsCapExceeded` before launching any worker
Job. Operators can bypass via `tideproject.k8s/bypass-budget=true` (one-shot,
consumed) or `tideproject.k8s/bypass-budget-until=<RFC3339>` (TTL-based).

`BudgetConfig.RollingWindowCapCents` is **documentation-only** at present.
`internal/budget/tally.go:42-44` preserves `WindowStart` after first call but
implements no boundary-crossing reset; the API type itself says
"As of Phase 2 only `AbsoluteCapCents` is enforced … this field is
documentation-only and should not be relied on for cost control" (per the
WR-02 comment in `api/v1alpha1/project_types.go`). The intended cumulative
behavior over time is `CostSpentCents` accumulating indefinitely until
`AbsoluteCapCents` halts dispatch. See P4.1.

---

## 4. Findings — priority ordered

### P1 — Production wiring defects

#### P1.1 — `ProjectReconciler` runs without its Dispatcher in production
`internal/controller/project_controller.go:140` declares the `Dispatcher`
field, and the Reconcile body at line 198 gates real lifecycle work on
`r.Dispatcher != nil`. `cmd/manager/main.go:319-328` constructs
`ProjectReconciler` **without** assigning `Dispatcher`, while
Milestone/Phase/Plan/Task all receive it (lines 339, 359, 374, 408). The
`// CR-01 fix:` comments at 336–359 show this exact bug was already fixed for
the other four reconcilers; ProjectReconciler was overlooked. Fix: assign
`Dispatcher: dispatcher` at line 326-ish and add a manager-construction test
that fails if a required dependency is omitted.

#### P1.2 — Planner Jobs use a skeletal pod template
`internal/controller/planner_job_helpers.go` is 38 lines total — a literal
PodTemplateSpec with a single container, no PVC mount, no credproxy sidecar,
no signed-token env, no `EnvelopeIn` writer. Its own comment admits "This
skeletal template lets the up-stack reconcilers dispatch Jobs in envtest
without pulling in the full Phase 2 podjob.Build infrastructure." The
Milestone / Phase / Plan reconcilers serialize an envelope and then discard
it at the Job boundary. Fix: factor a planner variant of
`internal/dispatch/podjob.Build` that shares the task-dispatch contract
(PVC mount, credproxy sidecar, signed token, bounded deadline, termination
output).

#### P1.3 — Default task caps cause a 300s ↔ 60s deadline mismatch
`internal/controller/task_controller.go:68` defines
`const defaultWallClockFloorSeconds int32 = 300`, applied to token validity
when `task.Spec.Caps` is nil or `WallClockSeconds <= 0` (line 380-385).
`internal/dispatch/podjob/jobspec.go:108-113` derives `activeDeadlineSeconds`
directly from caps without applying that floor; when caps are nil/zero, the
Job's `activeDeadlineSeconds` defaults to 60 seconds. Result: the token is
valid for ~360s but the Job is killed at 60s. Fix: extract a shared
`defaultCaps(caps)` helper called from both token mint and Job spec build,
plus a nil-caps test asserting the two derived deadlines match (with grace).

#### P1.4 — "First Project in namespace" fallback
`internal/controller/task_controller.go:624-629` (`resolveProject`) and
`internal/controller/plan_controller.go:798-802` (`resolveProjectName`) both
`List` Projects in the namespace and return `projectList.Items[0]` with
no tie-breaker. In a namespace hosting multiple Projects, status / budget /
dispatch / ownership all attach to whichever Project sorts first.
Fix: require a label, an explicit `Spec.ProjectRef`, or walk the owner chain;
on miss, set a `ParentUnresolved` condition and requeue rather than guessing.

### P2 — CI / tooling hygiene

#### P2.1 — `tools/analyzers/dagimports/` is dead code
The analyzer doc claims it rejects forbidden imports under `pkg/dag/`, but
`cmd/tide-lint/main.go` only registers `crosspool`, `providerfirewall`, and
`metriccardinality`. The load-bearing DAG-05 gate is the shell-based
`verify-dag-imports` Makefile target. Fix: either delete the analyzer or wire
it into `cmd/tide-lint`. (Note: `codebase_audit_report.md` §4 incorrectly
lists `dagimports` as an active CI-rejecting linter.)

#### P2.2 — Two `verify-*` Make targets aren't named in `ci.yaml`
`verify-dispatch-imports` and `verify-import-firewall` exist as Make targets
but don't appear as named steps in `.github/workflows/ci.yaml`. The latter
is functionally covered because the providerfirewall analyzer runs via
`make tide-lint` (line 69). Fix: a one-line note in the Makefile target
description, or named CI steps for parity with the other `verify-*` gates.

#### P2.3 — `test.yml` and `test-e2e.yml` run `go mod tidy` without diff check
Both workflows (`.github/workflows/test.yml`, `test-e2e.yml`) invoke
`go mod tidy` then proceed; neither follows with `git diff --exit-code go.mod
go.sum`. Module drift can be silently absorbed into the run. Fix: add the
diff check, or remove the `tidy` calls and rely on a single dedicated
"module hygiene" step in `ci.yaml`.

#### P2.4 — Live-E2E workflow exists in docs only
`make test-e2e-live` is fully implemented and gate-protected, but no
`.github/workflows/live-e2e.yml` is committed — `docs/live-e2e.md` provides a
template for operators to wire it themselves. Fix: if the intent is "nightly
Anthropic-billed run for this repo," commit the workflow with
`secrets.ANTHROPIC_API_KEY` and a `schedule:` trigger; if the intent is
"self-host when you fork," call that out at the top of `docs/live-e2e.md`.

### P3 — Code shape

#### P3.1 — Reconciler size hotspots
| file | total | longest method | size |
|---|---|---|---|
| `internal/controller/task_controller.go` | 976 | `reconcileDispatch` (line 194) — 12-step dispatch body | 268 lines |
| `internal/controller/plan_controller.go` | 837 | `maybePauseForWaveApprove` (line 569) | 129 lines |
| `internal/controller/project_controller.go` | 760 | `reconcilePhase3Lifecycle` (line 363) | 200 lines |

Extracting the budget gate, indegree compute, gate-policy hook, and Job-create
steps from `reconcileDispatch` into named methods would make the dispatch
body readable without changing the reconcile shape.

#### P3.2 — `TaskReconciler` carries 10+ injected fields
`PlannerPool, ExecutorPool, Dispatcher, Budget, Defaults, SigningKey,
SubagentImage, CredproxyImage, EnvReader, WatchNamespace` plus embedded
`client.Client, Scheme, MaxConcurrentReconciles`. If more land, consolidate
into a `TaskReconcilerDeps` carrier (mirrors `HelmProviderDefaults`).

### P4 — Improvements

#### P4.1 — Implement budget rolling-window reset
`RollingWindowCapCents` is documentation-only. Plan-shaped suggestion: in
`ProjectReconciler`, compare `now.Sub(Status.Budget.WindowStart)` against the
configured `RollingWindowCapCents` window duration; on boundary crossing,
reset `Status.Budget.CostSpentCents` and `Status.Budget.TokensSpent` and
advance `WindowStart`. Add a tally test that walks the clock across a window
edge.

#### P4.2 — Make the cred-proxy upstream allowlist configurable
The current allowlist (`internal/credproxy/server.go:41-46`) hardcodes
`POST /v1/messages` and `POST /v1/messages/count_tokens`. As LLM features
expand (prompt caching, search tools, files API), every new route requires a
proxy rebuild + redeploy. Suggestion: drive the allowlist from a
`Task`-level annotation or a Project-level `Spec.Providers[*].AllowedRoutes`
field, keeping hardcoded defaults as the safe baseline.

#### P4.3 — Production-image defaults pin `:v0.1.0-dev` tags in source
`cmd/manager/main.go:164-165` defaults `TIDE_PUSH_IMAGE` and
`CLAUDE_SUBAGENT_IMAGE` to `:v0.1.0-dev` tags. Fine as Helm-overridden
fallbacks; add a `// PROD_OVERRIDE_REQUIRED` comment so future maintainers
don't accept the dev tag as a release-stable placeholder.

#### P4.4 — Logging convention sweep
Spot-check reconciler log strings against the k8s logging guidelines
(`logcheck` linter is enabled but enforces only a subset). Pass: terminal
period removal, active/past voice, capitalized first word, balanced key-value
pairs.

---

## 5. Recommended fix order

1. **P1.1** wire `ProjectReconciler.Dispatcher` (5-line PR + manager
   construction test).
2. **P1.4** remove first-Project-in-namespace fallback (2 files; replace with
   a fail-closed condition).
3. **P1.3** unify caps defaulting between token mint and Job spec build (one
   shared helper + nil-caps test).
4. **P1.2** factor a planner variant of `podjob.Build` and rewire the
   up-stack reconcilers (largest fix; bounded but real engineering).
5. **P2.1** delete or wire `dagimports`.
6. **P2.2, P2.3, P2.4** CI parity / hygiene (small, parallelizable).
7. **P4.1** budget rolling-window reset (needed before any production multi-day run).

P3 and the remaining P4 items are not ship-blockers.

---

## 6. Refuted / corrected claims from source audits

From `audit-report-2026-05-20.md`:
- **Q2 — "Subagent image selection is fragmented and `CLAUDE_SUBAGENT_IMAGE`
  doesn't reach all paths."** REFUTED. The Helm template sets both
  `--subagent-image` (flag → dispatcher) and `CLAUDE_SUBAGENT_IMAGE` (env →
  provider defaults). Both flow end-to-end. The pattern is layered, not
  fragmented. (`cmd/manager/main.go:166, 309, 335, 356, 375` ;
  `charts/tide/templates/deployment.yaml:30, 49-50`.)
- **Q6 — "CI workflow duplication."** PARTIAL. The real defect is the
  unguarded `go mod tidy` in `test.yml` and `test-e2e.yml` (now P2.3); the
  "all workflows duplicate" framing overstates the overlap.

From `codebase_audit_report.md`:
- **§4 — `dagimports` listed as a CI-rejecting linter.** REFUTED. The
  analyzer source exists in `tools/analyzers/dagimports/` but is not
  registered in `cmd/tide-lint/main.go`. The DAG-05 gate is the shell-based
  `verify-dag-imports` Makefile target. See P2.1.
- **§3.5 — "Project phase is set to `LeakDetected`."** CORRECTED. The
  `Project.Status.Phase` is actually `PhasePushLeakBlocked`
  (`api/v1alpha1/project_types.go`); the string `"LeakDetected"` appears as
  the **Condition Reason**, not the phase. Counter
  (`tide_secret_leak_blocked_total`) increment is real and confirmed.

Already-removed claims from `audit-report-2026-05-20-opus-4-7.md` (the draft
that produced this report's §2 / §3 content):
- `.claude/worktrees/` orphan copies — git-ignored, not a code-quality issue.
- `bin/k8s/1.34.0-*` committed binaries — `git ls-files bin/k8s/` returns 0.
- "20+ linters" — actual count is 19.

---

## 7. Methodology

Three audits were already on disk for 2026-05-20, each taking a different
angle: production-wiring bugs, subsystem deep-dives, and discipline patterns.
The merge process:

1. **Cross-checked overlapping claims** between the three reports. Where they
   agreed, kept the most precisely cited version.
2. **Re-verified every unverified claim** via independent `Explore`
   subagents that received only the claim, never the source audit's wording,
   so verdicts couldn't anchor on the original framing. Verdicts:
   VERIFIED / REFUTED / PARTIAL, each with file:line evidence.
3. **Dropped refuted claims** (audit-1 Q2, audit-2 §4 `dagimports`).
4. **Corrected partial / inaccurate claims** with the actual code state
   (audit-1 Q6 narrowed; audit-2 §3.5 phase-vs-reason).
5. **Priority-ordered the surviving findings** by impact (P1 ship-blockers →
   P4 improvements) rather than by source audit, so a reader can act on the
   merged list without consulting the originals.

The original three reports remain in `docs/` unchanged; this report
supersedes them for consumption but does not delete them.
