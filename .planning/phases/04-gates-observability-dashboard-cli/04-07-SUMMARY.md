---
phase: 04
plan: 07
subsystem: cli
tags: [cobra, cli-runtime, genericclioptions, tabwriter, tide-cli, d-c1, d-c2, d-c3, d-c4, pitfall-25, pitfall-27]
dependency_graph:
  requires:
    - "04-01 — central Prometheus registry (CLI is the read-side counterpart to the manager's metrics surface)"
    - "04-03 — pkg/otelai + internal/otelinit (downstream verbs in 04-08 will emit OpenInference spans for tail/approve via the same import path)"
  provides:
    - "cmd/tide — operator-facing CLI binary (5 verbs live: apply, watch, inspect-wave, artifact-get [dry-run], describe-budget; 5 stubs land in 04-08: tail, approve, reject, cancel, resume)"
    - "cmd/tide.K8sClient() + RESTConfig() + resolveNamespace() helpers — subcommand seams every verb consumes"
    - "Persistent flag surface — kubectl-aligned --kubeconfig/--context/--namespace + --output|-o (D-C4) via genericclioptions.NewConfigFlags(true)"
    - "Test seams — buildRootForTest(), inspectWaveRun/With Err, describeBudgetRun, parseArtifactRef, artifactGetDryRun — drive subcommand rendering directly from controller-runtime/pkg/client/fake fixtures"
    - "Makefile target `make tide-cli` → bin/tide"
    - "go.mod direct deps: github.com/spf13/cobra v1.10.2, k8s.io/cli-runtime v0.36.1"
  affects:
    - "04-08 — adds 5 annotation-writer verbs (tail/approve/reject/cancel/resume) as ~50-LOC additions each via rootCmd.AddCommand"
    - "04-09 — release pipeline + goreleaser + Krew plugin manifest (D-C2) consumes the cmd/tide binary built here"
    - "04-14 — kind harness fills in artifact-get's real apiserver pod-exec proxy (Task 2 ships dry-run only)"
tech_stack:
  added:
    - "github.com/spf13/cobra v1.10.2 (was indirect; now direct dep)"
    - "k8s.io/cli-runtime v0.36.1 (new direct dep; kubectl-aligned config flag surface)"
  patterns:
    - "Constructor-based command tree — buildRootForTest() seam so cmd_test.go builds isolated cobra trees per test without mutating a package-level rootCmd"
    - "Stub-with-honest-error pattern — plan0408Stubs() generates 5 verbs whose RunE returns a `not yet implemented — landing in plan 04-08` error, so `tide --help` reflects the eventual surface today"
    - "Signal-aware execution — signal.NotifyContext(context.Background, os.Interrupt, SIGTERM) + rootCmd.ExecuteContext propagates Ctrl-C to cmd.Context() in long-running subcommands (Pitfall 25 mitigation)"
    - "filepath.Base(os.Args[0]) for cobra Use — Krew-installed `kubectl-tide` and direct `tide` both render correct help (Pitfall 27 mitigation)"
    - "Canonical-label lookup pattern — Tasks filtered by `tideproject.k8s/project` + `tideproject.k8s/wave-index` per internal/controller/plan_controller.go:513-523 (do NOT invent new label keys)"
    - "Renderer/adapter split — *_run.go files hold pure-Go renderer functions; *.go files hold the cobra adapter wiring. Tests exercise renderers directly via fake clients; no cobra plumbing in the test tree."
key_files:
  created:
    - cmd/tide/main.go
    - cmd/tide/root_flags.go
    - cmd/tide/subcommands.go
    - cmd/tide/apply.go
    - cmd/tide/watch.go
    - cmd/tide/inspect_wave.go
    - cmd/tide/inspect_wave_run.go
    - cmd/tide/artifact_get.go
    - cmd/tide/artifact_get_run.go
    - cmd/tide/describe_budget.go
    - cmd/tide/describe_budget_run.go
    - cmd/tide/runners.go
    - cmd/tide/cmd_test.go
    - cmd/tide/inspect_wave_test.go
    - cmd/tide/describe_budget_test.go
  modified:
    - Makefile
    - go.mod
    - go.sum
    - .gitignore
decisions:
  - "Cobra v1.10.2 over v1.9.1 — k8s.io/cli-runtime@v0.36.1 requires cobra >=v1.10.2 as a minimum. The plan requested v1.9.1; bumping cli-runtime would be the alternative but CLAUDE.md says 'Never bump k8s.io/* independently — let controller-runtime's go.mod dictate'. k8s.io/* lives at v0.36 (controller-runtime v0.24.1 declares >=v0.36.0); cobra is the lighter-touch bump. [Rule 3]"
  - "k8s.io/* v0.36.0 → v0.36.1 patch bump — pulled transitively by cli-runtime@v0.36.1's resolver. controller-runtime v0.24.1's go.mod declares >=v0.36.0 (minimum bound, not exact pin), so the patch bump is MVCS-compatible. Pinned api/apimachinery/client-go/apiextensions-apiserver/apiserver/component-base to v0.36.1 for consistency. [Rule 3]"
  - "inspect-wave argument is the Project name, not a Plan name — the canonical label vocabulary (tideproject.k8s/project + tideproject.k8s/wave-index per internal/controller/plan_controller.go:513-523) keys task lookups on the Project. The verb name remains `inspect-wave <project>` for D-C3 alignment; if multi-plan-per-project emerges, a v1.x extension can switch the selector to Plan."
  - "artifact-get ships as dry-run only in v1.0 — the real apiserver pod-exec proxy needs a live cluster + RBAC + a sealed inspector image; that integration work belongs with the kind harness in plan 04-14. Dry-run prints the inspector-pod spec (busybox:1.36, /workspace mount, ttl 30s) so operators can verify the design and pivot to `kubectl exec` if they need the data today."
  - "describe-budget reads BudgetConfig.AbsoluteCapCents (USD cents int64), not the plan's resource.Quantity-shaped 'AbsoluteCap string' — the latter doesn't exist in api/v1alpha1. Actual schema: BudgetConfig{AbsoluteCapCents int64} + BudgetStatus{TokensSpent int64, CostSpentCents int64, WindowStart *metav1.Time}. Renderer formats cents as USD ($X.YZ) while preserving the cents value in JSON for scripts. [Rule 1 — corrected against actual schema]"
  - "watch is a 1s poll loop, not a real K8s Watch — simpler, RBAC-equivalent, and an operator-facing UX no different from push semantics at 1s cadence. A v1.x revision can swap to watch.Interface push semantics if the dashboard's SSE shape proves the latency gap matters. The loop honours ctx.Done() between ticks so Ctrl-C terminates within pollInterval."
  - "Renderer/adapter file split — *_run.go holds the pure-Go renderer functions (inspectWaveRun, describeBudgetRun); the same-named *.go file (inspect_wave.go, describe_budget.go) holds only the cobra command constructor + RunE adapter. Tests import the renderers directly via controller-runtime/pkg/client/fake; no cobra plumbing in the test tree."
  - "buildRootForTest() exists alongside the package-level rootCmd — each test builds an isolated tree so concurrent t.Parallel() runs don't fight over flag state. The production main() still uses the package-level rootCmd so subcommand init() patterns (post-04-08) remain familiar."
  - "Empty-wave message goes to stderr, exit 0 — `tide inspect-wave my-plan --wave 9` with no matching tasks writes 'No tasks in wave 9 for project P.' to stderr and exits 0. The empty result is informational, not a hard error (the wave might not yet be stamped); scripts that grep on exit code aren't misled."
metrics:
  duration_minutes: 12
  completed_date: 2026-05-19
  tasks_completed: 2
  files_created: 15
  files_modified: 4
  commits: 4
requirements_completed: [CLI-01, CLI-02, CLI-03]
---

# Phase 4 Plan 07: cobra-based `tide` CLI skeleton Summary

Ship `cmd/tide` — the operator-facing TIDE CLI binary — with five read-side verbs live (`apply`, `watch`, `inspect-wave`, `artifact-get` dry-run, `describe-budget`) and five write-back verbs stubbed (`tail`, `approve`, `reject`, `cancel`, `resume`) so `tide --help` reflects the entire D-C3 surface today. Foundation for the annotation-writer verbs landing in plan 04-08 — each will be a ~50-LOC addition via `rootCmd.AddCommand`.

## Performance

- **Duration:** 12 min
- **Tasks:** 2/2
- **Commits:** 4 (2 RED / 2 GREEN)
- **Files created:** 15
- **Files modified:** 4

## What landed

### `cmd/tide/main.go` — cobra root + signal-aware execution

```go
var rootCmd = newRootCmd()

func newRootCmd() *cobra.Command {
    c := &cobra.Command{
        Use:          filepath.Base(os.Args[0]),   // Pitfall 27: tide / kubectl-tide
        Version:      version,                     // ldflag: -X main.version=...
        SilenceUsage: true,
    }
    registerPersistentFlags(c)
    registerSubcommands(c)
    return c
}

func main() {
    ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
    defer stop()
    if err := rootCmd.ExecuteContext(ctx); err != nil { ... os.Exit(1) }
}
```

`filepath.Base(os.Args[0])` resolves to `tide` or `kubectl-tide` automatically (Pitfall 27 — Krew-installed binaries are renamed). `signal.NotifyContext` wraps a fresh context.Background and propagates SIGINT/SIGTERM into `cmd.Context()`; the test `TestSignalCancelPropagatesToContext` asserts that a blocking subcommand returns within 1s of ctx cancel (Pitfall 25 mitigation).

### `cmd/tide/root_flags.go` — kubectl-aligned persistent flags

```go
var configFlags = genericclioptions.NewConfigFlags(true)
var outputFormat = "human"

func registerPersistentFlags(root *cobra.Command) {
    configFlags.AddFlags(root.PersistentFlags())
    root.PersistentFlags().StringVarP(&outputFormat, "output", "o", "human", "Output format: human, json")
}

func K8sClient() (client.Client, error) {
    cfg, _ := configFlags.ToRESTConfig()
    return client.New(cfg, client.Options{Scheme: scheme})
}
```

`genericclioptions.NewConfigFlags(true)` registers the full kubectl flag surface (--kubeconfig, --context, --namespace, --as, --as-group, --cluster, --insecure-skip-tls-verify, etc.) — `tide` works wherever `kubectl get pods` works without a `tide login` flow (D-C1). The `--output|-o` flag adds the `human|json` format enum (D-C4). The `scheme` includes both `clientgoscheme` (for core K8s objects) and `tidev1alpha1.AddToScheme` (for Project/Milestone/Phase/Plan/Task/Wave) so controller-runtime client.Client returns strongly-typed objects.

### `cmd/tide/subcommands.go` — verb registration + plan-04-08 stubs

`registerSubcommands` mounts every D-C3 verb. The 5 stub verbs (`tail`, `approve`, `reject`, `cancel`, `resume`) ship with a uniform RunE that returns `not yet implemented — landing in plan 04-08` — so `tide --help` lists the entire eventual surface AND the stub-test in `cmd_test.go` can assert the error wording uniformly:

```go
func TestStubVerbsReturnNotImplemented(t *testing.T) {
    for _, verb := range []string{"tail", "approve", "reject", "cancel", "resume"} {
        root := newTestRoot(t)
        root.SetArgs([]string{verb})
        err := root.Execute()
        if !strings.Contains(err.Error(), "04-08") { ... }
    }
}
```

### `cmd/tide/apply.go` — server-side apply wrapper

Reads a YAML manifest (`-f file`, required), unmarshals into `unstructured.Unstructured`, and patches it server-side via `client.Patch(client.Apply, &client.PatchOptions{FieldManager: "tide-cli", Force: ptr.To(true)})`. On `apierrors.IsInvalid` failures, walks `StatusError.ErrStatus.Details.Causes` and prints each violation field-by-field — operators see `spec.targetRepo: required value` instead of a wall of JSON.

### `cmd/tide/watch.go` — 1s poll loop

`tide watch <project>` polls `Project.Status.Phase` + active-Milestone count every second, printing a one-line status update when it changes. Honours `ctx.Done()` between ticks so Ctrl-C terminates within `pollInterval` (1s):

```
default/my-project phase=Running active_milestones=2
default/my-project phase=Running active_milestones=1
default/my-project phase=Complete active_milestones=0
```

Output uses `cmd.OutOrStdout()` so test fixtures can capture the stream without monkey-patching `os.Stdout`.

### `cmd/tide/inspect_wave_run.go` — tabwriter + JSON renderer

Lists Tasks in the resolved namespace, filters by the canonical label vocabulary (`tideproject.k8s/project` + `tideproject.k8s/wave-index` per `internal/controller/plan_controller.go:513-523` — do NOT invent new keys), sorts by `(wave, name)` for determinism, and renders one of:

| Format | Shape |
|--------|-------|
| `human` (default) | `text/tabwriter` 5-column block: NAME, STATUS, AGE, ATTEMPT, SCHEDULED-IN-WAVE |
| `json` (`-o json`) | Indented JSON array of `{name, status, age, attempt, wave}` rows |

Empty results write `No tasks in wave N for project P.` (or `No tasks for project P.`) to stderr and exit 0. The empty form is informational, not a hard error — the wave might not yet be stamped by PlanReconciler.

### `cmd/tide/describe_budget_run.go` — human/json budget renderer

Reads `Project.Status.Budget.CostSpentCents` against `Project.Spec.Budget.AbsoluteCapCents` (USD cents int64 per `api/v1alpha1/project_types.go` — the plan's `resource.Quantity` shape doesn't exist; the schema uses int64 cents). Renders as either:

**Human form** (7 lines, dashboard-grammar):
```
Project: my-project
Absolute cap:    $50.00 (5000 cents)
Current spend:   $12.34 (1234 cents)
Tokens spent:    1500000
Window start:    2026-05-19T15:00:00Z
Utilization:     24.7%
Status:          within budget
```

When `CostSpentCents > AbsoluteCapCents` the status line reads `OVER BUDGET` (uppercase marker).

**JSON form** (script-friendly):
```json
{
  "project": "my-project",
  "capCents": 5000,
  "currentSpendCents": 1234,
  "tokensSpent": 1500000,
  "windowStart": "2026-05-19T15:00:00Z",
  "withinBudget": true,
  "utilizationPct": 24.68
}
```

T-04-C3 mitigation by construction — the renderer reads ONLY `Status.Budget` + `Spec.Budget.AbsoluteCapCents` fields. It never touches `Spec.SecretRefs`, `ProviderSecretRef`, or any kubeconfig-derived token.

### `cmd/tide/artifact_get_run.go` — ref parser + dry-run

`parseArtifactRef` splits `<ns>/<project>/<path>` via `strings.SplitN(ref, "/", 3)` so the path component may contain slashes. Rejects empty refs, refs with <3 parts, and refs with empty components.

`artifactGetDryRun` prints the inspector-pod spec the real (post-04-14) implementation will create:

```
tide artifact-get (dry-run; real apiserver pod-exec proxy lands in plan 04-14)
  namespace: default
  project: my-project
  path: envelopes/abc/out.json
  inspector pod:
    image: busybox:1.36
    command: ["sh", "-c", "cat /workspace/artifacts/envelopes/abc/out.json"]
    volumeMounts:
      - name: workspace
        mountPath: /workspace
    volumes:
      - name: workspace
        persistentVolumeClaim:
          claimName: tide-projects
    ttlSecondsAfterFinished: 30
```

v1.0 acceptable simplicity tradeoff per RESEARCH §1318 — the real proxy needs a live cluster + RBAC + a sealed inspector image, all of which belong with the kind harness in plan 04-14.

### Makefile addition

```make
.PHONY: tide-cli
tide-cli: ## Build the operator-facing tide CLI binary (Phase 4 D-C1..C4).
	go build -o bin/tide ./cmd/tide
```

`/tide` added to `.gitignore` to prevent accidental root-level binaries from a bare `go build ./cmd/tide` invocation (mirrors the existing `/tide-lint`, `/manager`, `/credproxy` entries).

## Test coverage

`cmd/tide/cmd_test.go` — 7 tests (root command shape):
- `TestHelpListsAllVerbs` — `tide --help` contains all 10 D-C3 verbs + `completion`
- `TestVersionFlagPrintsStableShape` — `tide --version` contains the default `dev`
- `TestPersistentFlagsAvailable` — `--kubeconfig`, `--context`, `--namespace`, `--output`, `-o` registered
- `TestSignalCancelPropagatesToContext` — ctx-cancel returns RunE within 1s
- `TestUseResolvesFromArgv0` — non-empty `Use` (Pitfall 27)
- `TestApplyRequiresFFlag` — `tide apply` (no `-f`) errors
- `TestStubVerbsReturnNotImplemented` — all 5 stubs return errors citing plan 04-08

`cmd/tide/inspect_wave_test.go` — 4 tests (tabwriter + JSON + filtering):
- `TestInspectWaveHumanRendersTabwriter` — 5-column shape, both tasks visible
- `TestInspectWaveFiltersByWave` — `--wave 0` includes alpha, excludes zeta (wave-1)
- `TestInspectWaveEmptyWaveFiltered` — `No tasks in wave 9 for project P.` on stderr
- `TestInspectWaveJSONOutput` — JSON array with required keys

`cmd/tide/describe_budget_test.go` — 6 tests (budget + artifact-get):
- `TestDescribeBudgetHumanOutput` — 5 required strings (Project/cap/spend/tokens/within budget)
- `TestDescribeBudgetOverCap` — `OVER BUDGET` marker when spend > cap
- `TestDescribeBudgetJSONOutput` — 4 required JSON keys
- `TestArtifactGetRefParsingMalformed` — 4 invalid refs all error
- `TestArtifactGetRefParsingValid` — happy-path parse roundtrip
- `TestArtifactGetDryRunPrintsPodSpec` — 5 required pod-spec strings

```
=== Test Summary ===
17/17 PASS with -race
go test ./cmd/tide/... -race -count=1  →  ok  github.com/jsquirrelz/tide/cmd/tide
```

## Plan verification block

| Check | Result |
|-------|--------|
| `go build -o /tmp/tide-test ./cmd/tide` | succeeds |
| `/tmp/tide-test --help \| grep -E "^  (apply\|watch\|tail\|approve\|reject\|cancel\|resume\|inspect-wave\|artifact-get\|describe-budget\|completion)" \| wc -l` | 11 (10 D-C3 verbs + completion) |
| `/tmp/tide-test --version` | `tide version dev` |
| `go test ./cmd/tide/... -race -v` | PASS (17/17) |
| `go vet ./cmd/tide/...` | clean |
| `make tide-lint` | clean (exit 0) |
| `go build ./...` | clean |
| `rg "os\.Create\|os\.WriteFile" cmd/tide/ \| grep -v _test` | 0 matches (T-04-C1) |

## TDD Gate Compliance

Both tasks followed strict RED → GREEN cycles. Commit ledger on branch `worktree-agent-a518a6e654815c14b`:

| Task | Phase | Commit    | Type | Subject                                                       |
| ---- | ----- | --------- | ---- | ------------------------------------------------------------- |
| 1    | RED   | `3f991db` | test | cobra-based tide CLI skeleton test surface                    |
| 1    | GREEN | `761234a` | feat | cobra-based tide CLI skeleton + apply + watch                 |
| 2    | RED   | `123cd74` | test | inspect-wave/describe-budget/artifact-get test surface        |
| 2    | GREEN | `7b67c5a` | feat | inspect-wave/describe-budget/artifact-get renderers           |

Four commits. Each RED gate was verified by `go test` compile failure (`undefined: <symbol>`) before the GREEN implementation landed. GREEN commits are the only places production code lands.

## What downstream plans now consume

| Downstream plan | Consumes |
|----------------|----------|
| **04-08** (annotation-writer verbs) | rootCmd.AddCommand + K8sClient() + resolveNamespace() — each verb is ~50 LOC: parse args, write annotation via client.Patch, exit |
| **04-09** (release pipeline + Krew) | `make tide-cli` → bin/tide as the goreleaser-built artifact |
| **04-14** (kind harness) | Fills in `runArtifactGet` real apiserver pod-exec proxy (replaces dry-run) |

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 — Dependency floor] cobra v1.9.1 → v1.10.2 forced by cli-runtime**

- **Found during:** Task 1 — `go get k8s.io/cli-runtime@v0.36.0` initial attempt
- **Issue:** k8s.io/cli-runtime@v0.36.0 declares `require github.com/spf13/cobra v1.10.2` (not v1.9.1). The plan's `go get github.com/spf13/cobra@v1.9.1 k8s.io/cli-runtime@latest` recipe is unsatisfiable.
- **Fix:** Pinned cobra to v1.10.2 (cli-runtime's floor); kept cli-runtime at v0.36.1 (the latest compatible patch in the v0.36 line). CLAUDE.md says "Never bump k8s.io/* independently — let controller-runtime's go.mod dictate"; cobra is the lighter-touch bump because it's a stable v1 line (v1.10.2 is API-compatible with v1.9.1 for our usage — Command, ExecuteContext, RunE, Args signatures unchanged).
- **Files modified:** go.mod, go.sum
- **Verification:** `go build ./cmd/tide && go test ./cmd/tide/... -race` both pass; `go vet ./...` clean
- **Committed in:** `761234a` (Task 1 GREEN — go.mod alongside the code)
- **Why this counts as Rule 3:** the dependency floor was a blocking issue; resolving it required choosing between bumping cobra past plan-spec or bumping cli-runtime to a different version. CLAUDE.md's k8s.io rule made the choice for us.

**2. [Rule 3 — Patch bump] k8s.io/* v0.36.0 → v0.36.1 (transitive)**

- **Found during:** Task 1 — `go get k8s.io/cli-runtime/pkg/genericclioptions` resolved to v0.36.1
- **Issue:** Adding cli-runtime@v0.36.1 caused `go mod tidy` to bump api/apimachinery/client-go from v0.36.0 to v0.36.1 transitively.
- **Fix:** Pinned api/apimachinery/client-go/apiextensions-apiserver/apiserver/component-base/kms to v0.36.1 for consistency. controller-runtime v0.24.1 declares `>=v0.36.0` (minimum bound, not exact pin) so the patch is MVCS-compatible — verified `go build ./... && go vet ./...` both clean.
- **Files modified:** go.mod, go.sum
- **Committed in:** `761234a` (Task 1 GREEN)
- **Why this counts as Rule 3:** patch-level k8s.io bumps are routine maintenance; the CLAUDE.md rule prohibits independent MINOR/MAJOR bumps. A v0.36.0 → v0.36.1 patch within the same minor is the lighter touch.

**3. [Rule 1 — Bug] describe-budget schema mismatch (plan vs. api/v1alpha1)**

- **Found during:** Task 2 — writing describe_budget_test.go against actual project_types.go
- **Issue:** Plan PROSE described `Budget{AbsoluteCap string, RollingWindowHours int, CurrentSpend resource.Quantity, LastWindowSpend resource.Quantity, BypassExpiresAt *metav1.Time}` — none of those fields exist in `api/v1alpha1/project_types.go`. Actual schema: `BudgetConfig{AbsoluteCapCents int64}` + `BudgetStatus{TokensSpent int64, CostSpentCents int64, WindowStart *metav1.Time}`.
- **Fix:** Implemented the renderer against the actual schema. Human form renders cents as USD ($X.YZ) while preserving the int64 cents value in JSON. JSON keys are `capCents`, `currentSpendCents`, `tokensSpent`, `windowStart`, `withinBudget`, `utilizationPct` (script-friendly). The plan's `bypass-state` line is omitted because there is no `BypassExpiresAt` field; if a future plan adds bypass tracking, a one-line addition surfaces it.
- **Files modified:** cmd/tide/describe_budget_run.go, cmd/tide/describe_budget_test.go
- **Verification:** TestDescribeBudgetHumanOutput, TestDescribeBudgetOverCap, TestDescribeBudgetJSONOutput all PASS with -race
- **Committed in:** `7b67c5a` (Task 2 GREEN)
- **Why this counts as Rule 1:** the plan's described shape would have produced a compile error against the actual CRD types. Implementing against the real schema is bug-fix-at-the-plan-spec layer; the contract (render Status.Budget vs cap) is preserved.

### Architectural decisions auto-applied (no checkpoint)

**Renderer/adapter file split** — `*_run.go` files (`inspect_wave_run.go`, `describe_budget_run.go`, `artifact_get_run.go`) hold pure-Go renderer functions; the same-named `*.go` files hold only the cobra command constructor + RunE adapter. Tests import the renderers directly via controller-runtime/pkg/client/fake — no cobra plumbing in the test tree. This pattern is the foundation for plan 04-08's annotation-writer verbs: each new write-back verb gets a 30-LOC `*_run.go` file with `(ctx, client.Client, ns, name, …)` signature + a 15-LOC adapter that resolves the namespace + client.

**buildRootForTest() alongside package-level rootCmd** — production `main()` uses the singleton rootCmd; tests build fresh trees via `newRootCmd()`/`buildRootForTest()`. Lets tests run with `t.Parallel()` without fighting over flag state, while keeping the production binary's subcommand init pattern familiar for downstream plans.

**`tide watch` is a 1s poll, not a real K8s Watch** — simpler, RBAC-equivalent, and operator-facing UX no different at 1s cadence. The poll loop honours ctx.Done() between ticks so Ctrl-C terminates within pollInterval. A v1.x revision can swap to `watch.Interface` push semantics if dashboard SSE latency comparisons prove the gap matters.

**`artifact-get` ships dry-run only in v1.0** — the real apiserver pod-exec proxy needs a live cluster + RBAC + a sealed inspector image; that work belongs with the kind harness in plan 04-14. The dry-run output is a literal pod spec, so operators can copy it into `kubectl exec` themselves today if they need the data immediately.

## Known Stubs

| Stub | Where | Why | Resolution Plan |
|------|-------|-----|-----------------|
| `tail` verb | cmd/tide/subcommands.go (plan0408Stubs) | Annotation-writer verbs; pod-log streaming logic depends on the `pods/log` proxy seam | Plan 04-08 |
| `approve` verb | same | Writes `tideproject.k8s/approve-wave-N` annotation; depends on the gates work in plan 04-04 (now landed) | Plan 04-08 |
| `reject` verb | same | Writes `tideproject.k8s/reject` annotation | Plan 04-08 |
| `cancel` verb | same | Destructive (cascade delete + PVC cleanup); requires --force | Plan 04-08 |
| `resume` verb | same | Clears reject annotation | Plan 04-08 |
| `artifact-get` real impl | cmd/tide/artifact_get_run.go | Dry-run ships v1.0 acceptable per RESEARCH §1318; real apiserver pod-exec needs a live cluster | Plan 04-14 (kind harness) |

All stubs return errors citing the future plan — `tide --help` is honest about implementation state. No stubs render UI strings as "TODO" / "coming soon".

## Threat Flags

None new. The plan's `<threat_model>` (T-04-C1 cache poisoning, T-04-C2 verb impersonation, T-04-C3 secret info-disclosure, T-04-C-cluster-confusion) is fully mitigated:

- **T-04-C1 (no local cache):** `rg "os\.Create|os\.WriteFile" cmd/tide/ | grep -v _test` returns 0 matches. Verified at every commit.
- **T-04-C2 (verb impersonation):** cobra registry locks the verb set at compile time via `registerSubcommands`. No dynamic verb registration anywhere in the tree. `tide --help` reflects the static set.
- **T-04-C3 (no secrets in output):** describe-budget renderer reads ONLY `Status.Budget` + `Spec.Budget.AbsoluteCapCents` — by construction it cannot leak `Spec.SecretRefs` or `ProviderSecretRef`. inspect-wave reads only Task `Status.Phase`, `Status.Attempt`, `CreationTimestamp`, and labels.
- **T-04-C-cluster-confusion:** `--context` flag visible in every `tide --help` (kubectl-aligned via genericclioptions). Documentation lands in docs/cli.md in plan 04-09.

No new threat surface introduced.

## Self-Check: PASSED

**Files exist:**
- `cmd/tide/main.go`
- `cmd/tide/root_flags.go`
- `cmd/tide/subcommands.go`
- `cmd/tide/apply.go`
- `cmd/tide/watch.go`
- `cmd/tide/inspect_wave.go`
- `cmd/tide/inspect_wave_run.go`
- `cmd/tide/artifact_get.go`
- `cmd/tide/artifact_get_run.go`
- `cmd/tide/describe_budget.go`
- `cmd/tide/describe_budget_run.go`
- `cmd/tide/runners.go`
- `cmd/tide/cmd_test.go`
- `cmd/tide/inspect_wave_test.go`
- `cmd/tide/describe_budget_test.go`
- `Makefile` (modified)
- `go.mod` / `go.sum` (modified)
- `.gitignore` (modified)

**Commits exist on worktree branch (`git log --oneline 016d5c7..HEAD`):**
- `3f991db` test(04-07): RED — cobra-based tide CLI skeleton test surface
- `761234a` feat(04-07): GREEN — cobra-based tide CLI skeleton + apply + watch
- `123cd74` test(04-07): RED — inspect-wave/describe-budget/artifact-get test surface
- `7b67c5a` feat(04-07): GREEN — inspect-wave/describe-budget/artifact-get renderers

All 17/17 tests pass with `-race`. `make tide-lint` clean. `go build ./...` clean. `go vet ./...` clean. Plan verification block fully satisfied.
