---
phase: 01-foundation-crds-pkg-dag-controller-scaffold
plan: 05
subsystem: infra
tags: [crd, kubebuilder, controller-gen, cel-validation, persist-01, persist-02, pitfall-4, status-conditions, makefile, github-actions, ci]

# Dependency graph
requires:
  - phase: 01-foundation-crds-pkg-dag-controller-scaffold
    provides: "Six kubebuilder-scaffolded api/v1alpha1/*_types.go skeletons; Makefile generate+manifests targets; controller-runtime v0.24.1 + ginkgo v2.28.3; .github/workflows/ci.yaml from Plan 03"
provides:
  - api/v1alpha1/shared_types.go — ConditionPending/Ready/Reconciling/Failed + ReasonInitialized/AwaitingDispatch/FinalizerTimedOut/SubagentDispatchFailed constants
  - Six hand-edited *_types.go files filling Spec/Status fields per CONTEXT.md D-A3/D-B1/D-B2/D-F1/D-F2
  - Wave.Spec pinned to exactly two fields (planRef + waveIndex) — D-B2 structurally enforced
  - Task.Spec.DependsOn []string (D-F1) + Task.Spec.FilesTouched with kubebuilder MinItems=1 (D-F2)
  - CEL +kubebuilder:validation:XValidation on ProjectSpec.targetRepo (CRD-03)
  - CEL +kubebuilder:validation:Minimum=0 on WaveSpec.WaveIndex (CRD-03)
  - Regenerated zz_generated.deepcopy.go via `make generate`
  - Regenerated config/crd/bases/tideproject.k8s_*.yaml (six files) via `make manifests`
  - Makefile verify-no-aggregates target (PERSIST-02 / Pitfall 4 grep gate)
  - Makefile verify-no-sqlite-dep target (PERSIST-01 grep gate)
  - api/v1alpha1/aggregates_guard_test.go — programmatic PERSIST-02 regex proof (revision Warning 4)
  - .github/workflows/ci.yaml runs both PERSIST gates as PR-blocking steps
  - Scoped controller-gen paths (./api/... + ./internal/controller/... + ./internal/webhook/...) so codegen no longer descends into tools/analyzers/*/testdata fixtures
affects: [01-06, 01-07, 01-10, 02-*, 04-*]

# Tech tracking
tech-stack:
  added: []  # no new module deps; all changes are markers, struct fields, Makefile targets
  patterns:
    - "Spec/Status separation with the Conditions array consistently +listType=map +listMapKey=type on every Status"
    - "Shared condition vocabulary (4 condition types, 4 reasons) declared once in shared_types.go and used uniformly across all six Kinds"
    - "CEL XValidation rule on Spec (struct-level) for cross-field invariants that can't be expressed as field-level markers; field-level MinLength/MinItems/Minimum for everything else"
    - "Wave.Spec exactly-two-fields invariant is structurally enforced — `make verify-no-aggregates` greps for forbidden tokens, and the `awk '/type WaveSpec struct/,/^}/' | grep -c json:` count is a documented invariant on acceptance criteria"
    - "PERSIST gates use grep regex over real source files (PERSIST-02) and go.mod (PERSIST-01); regex tokens cannot appear in comments without false-positive — comment writers must paraphrase the contract instead of echoing the denylist tokens"
    - "Programmatic guard test (aggregates_guard_test.go) writes temp fixtures and exercises the same regex — replaces manual mutate-and-revert recipes per revision Warning 4"
    - "controller-gen paths are scoped to the packages that actually carry kubebuilder markers — api/ + internal/controller/ + internal/webhook/ — never ./... at the module root because tools/analyzers/*/testdata fixtures are unresolvable to controller-gen's module resolver"

key-files:
  created:
    - api/v1alpha1/shared_types.go
    - api/v1alpha1/aggregates_guard_test.go
  modified:
    - api/v1alpha1/project_types.go
    - api/v1alpha1/milestone_types.go
    - api/v1alpha1/phase_types.go
    - api/v1alpha1/plan_types.go
    - api/v1alpha1/task_types.go
    - api/v1alpha1/wave_types.go
    - api/v1alpha1/zz_generated.deepcopy.go
    - config/crd/bases/tideproject.k8s_projects.yaml
    - config/crd/bases/tideproject.k8s_milestones.yaml
    - config/crd/bases/tideproject.k8s_phases.yaml
    - config/crd/bases/tideproject.k8s_plans.yaml
    - config/crd/bases/tideproject.k8s_tasks.yaml
    - config/crd/bases/tideproject.k8s_waves.yaml
    - Makefile
    - .github/workflows/ci.yaml

key-decisions:
  - "Scoped controller-gen paths (Rule 3 - Blocking auto-fix). `make generate` and `make manifests` previously used `paths=\"./...\"` which walks the entire module tree. Plan 02 introduced an analysistest violation fixture at tools/analyzers/dagimports/testdata/src/violation/pkg/dag/dag.go that imports k8s.io/apimachinery/pkg/runtime — intentionally, to prove the DAG-05 import-firewall analyzer catches it — but controller-gen uses standard module resolution (not analysistest's GOPATH resolver) and fails to find the import. Scoped paths to ./api/... (for generate) and ./api/... + ./internal/controller/... + ./internal/webhook/... (for manifests, which also needs the RBAC + webhook markers in those internal packages). This is the minimal additive fix — it doesn't change generated output and doesn't require touching the testdata fixtures. Documented inline in the Makefile."
  - "Rewrote a doc comment in plan_types.go to avoid the literal tokens Schedule/IndegreeMap (Rule 1 - Bug auto-fix). The PlanStatus block originally read \"PERSIST-02 enforced: NO Schedule, NO Waves []slice, NO IndegreeMap\" — those exact tokens are what verify-no-aggregates greps for, so the gate false-positived on the comment itself. Comment now describes the contract in paraphrase (\"no aggregate wave list, no cached dag, no indegree map — see make verify-no-aggregates for the enforced field-name denylist\") which keeps the contract self-documenting in code while letting the gate stay strict."
  - "Programmatic guard test mirrors the Makefile regex inline (revision Warning 4). aggregates_guard_test.go declares the regex as a package const (aggregatesPattern) AND the Makefile target reproduces the same regex in shell. If they drift, the integration sub-test (TestMakeVerifyNoAggregatesPassesOnRealTypes) catches it because it shells out to the actual Makefile target. A future cleanup could go-generate or shell-quote one form from the other, but two independent statements of the same pattern is acceptable for v1 (the integration test is the cross-check)."
  - "Did NOT add Schedule, Waves []slice, IndegreeMap, or CachedDag fields anywhere — and never will. The Wave.Spec exactly-two-fields invariant (planRef + waveIndex per D-B2) means any future PR that tries to add scheduling-relevant data to Wave.Spec is structurally rejected by the PR review checklist; verify-no-aggregates is the automated belt-and-suspenders."
  - "Status condition vocabulary committed at exactly 4 condition types (Pending, Ready, Reconciling, Failed) per CONTEXT.md Claude's Discretion. Phase 2's reconcilers can add condition-specific Reason constants but the core 4 condition types are the uniform surface across all six Kinds."

patterns-established:
  - "Hand-editing kubebuilder scaffold types: write the file out wholesale (Write, not Edit) — the scaffolded `// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!` comment block is the explicit license to replace the whole struct shape, and the canonical Spec/Status form is small enough to inline in the plan body (revision Warning 6)"
  - "CEL XValidation marker on Spec: `// +kubebuilder:validation:XValidation:rule=\"<expr>\",message=\"<text>\"` placed directly above the struct declaration. The rule's CEL string uses SINGLE quotes for string literals inside the double-quoted marker (e.g., self.targetRepo.startsWith('http'))"
  - "controller-gen path scoping: when in doubt, scope to the exact directory tree that carries kubebuilder markers. paths=\"./...\" is the kubebuilder scaffolder default but breaks when ANY other directory under the module contains unresolvable Go code (testdata fixtures, vendored test helpers, etc.)"
  - "PR-gate-aware code comments: when a Makefile target's regex enforces a denylist, the codebase's own documentation must paraphrase the denylist tokens rather than echo them — otherwise the gate fires on its own documentation"

requirements-completed:
  - CRD-01
  - CRD-02
  - CRD-03
  - PERSIST-01
  - PERSIST-02

# Metrics
duration: 7min
completed: 2026-05-12
---

# Phase 1 Plan 05: Six CRD Spec/Status fills + CEL markers + PERSIST gates Summary

**Six hand-edited api/v1alpha1/*_types.go files fill Spec/Status with the canonical schemas (D-B2 / D-F1 / D-F2), CEL XValidation rules land on Project + Task + Wave, the shared condition vocabulary moves into a single shared_types.go, and two new Makefile gates (verify-no-aggregates + verify-no-sqlite-dep) are PR-enforced — Phase 2 reconcilers now have the real type surface to consume.**

## Performance

- **Duration:** ~7 min
- **Started:** 2026-05-12T20:40:45Z
- **Completed:** 2026-05-12T20:48:12Z
- **Tasks:** 2 of 2
- **Files created:** 2 (shared_types.go, aggregates_guard_test.go)
- **Files modified:** 14 (six *_types.go, zz_generated.deepcopy.go, six CRD YAMLs, Makefile, ci.yaml)

## Final Spec/Status Field Inventory

### Project (`tideproject.k8s/v1alpha1 Project`)

| Field | Type | Required | Validation | Source |
| --- | --- | --- | --- | --- |
| `Spec.TargetRepo` | string | yes | `MinLength=1`; CEL XValidation: starts with `http` or `git@` | CRD-03 |
| `Spec.SecretRefs` | SecretRefs (AnthropicAPIKey, GitCredentials) | no | — | AUTH-01 reservation |
| `Spec.ModelSelection` | ModelSelection (Milestone, Phase, Plan, Task) | no | — | Phase 2 reservation |
| `Spec.Gates` | Gates (per-level GatePolicy enum + PauseBetweenWaves) | no | GatePolicy = `auto`\|`approve`\|`pause` | Phase 4 reservation |
| `Status.Phase` | string | optional | free-form state label | shared |
| `Status.Conditions` | []metav1.Condition | optional | +listType=map +listMapKey=type | shared |

### Milestone (`tideproject.k8s/v1alpha1 Milestone`)

| Field | Type | Required | Validation |
| --- | --- | --- | --- |
| `Spec.ProjectRef` | string | yes | MinLength=1 |
| `Spec.DependsOn` | []string | no | — |
| `Status.Phase` | string | optional | — |
| `Status.Conditions` | []metav1.Condition | optional | +listType=map +listMapKey=type |

### Phase (`tideproject.k8s/v1alpha1 Phase`)

| Field | Type | Required | Validation |
| --- | --- | --- | --- |
| `Spec.MilestoneRef` | string | yes | MinLength=1 |
| `Spec.DependsOn` | []string | no | — |
| `Status.Phase` | string | optional | — |
| `Status.Conditions` | []metav1.Condition | optional | +listType=map +listMapKey=type |

### Plan (`tideproject.k8s/v1alpha1 Plan` — also the conversion hub from Plan 01-01)

| Field | Type | Required | Validation |
| --- | --- | --- | --- |
| `Spec.PhaseRef` | string | yes | MinLength=1 |
| `Status.Phase` | string | optional | — |
| `Status.Conditions` | []metav1.Condition | optional | +listType=map +listMapKey=type |

PlanStatus is intentionally minimal — Phase 2 adds `ValidationState` + `CycleEdges` for cycle-detection feedback.

### Task (`tideproject.k8s/v1alpha1 Task` — D-F1 + D-F2)

| Field | Type | Required | Validation | Source |
| --- | --- | --- | --- | --- |
| `Spec.PlanRef` | string | yes | MinLength=1 | — |
| `Spec.DependsOn` | []string | no | — | D-F1 |
| `Spec.FilesTouched` | []string | **yes** | **MinItems=1** | D-F2 |
| `Spec.PromptRef` | string | no | — | — |
| `Status.Phase` | string | optional | — | shared |
| `Status.Conditions` | []metav1.Condition | optional | +listType=map +listMapKey=type | shared |
| `Status.Attempt` | int | optional | — | — |
| `Status.ExitCode` | *int | optional | — | — |
| `Status.CompletedAt` | *metav1.Time | optional | — | — |

### Wave (`tideproject.k8s/v1alpha1 Wave` — D-B1 + D-B2)

| Field | Type | Required | Validation | Source |
| --- | --- | --- | --- | --- |
| `Spec.PlanRef` | string | yes | MinLength=1 | D-B1 |
| `Spec.WaveIndex` | int | yes | **Minimum=0** | D-B1, D-B2 |
| `Status.Phase` | string | optional | — | shared |
| `Status.Conditions` | []metav1.Condition | optional | +listType=map +listMapKey=type | shared |
| `Status.TaskRefs` | []string | optional | — | D-B2 (observation only) |
| `Status.DispatchedAt` | *metav1.Time | optional | — | — |
| `Status.CompletedAt` | *metav1.Time | optional | — | — |

Wave.Spec carries **EXACTLY two fields** — `awk '/type WaveSpec struct/,/^}/' api/v1alpha1/wave_types.go | grep -c json:` returns 2. Any future PR adding a third field is rejected by the schema review checklist.

## CEL Validation Markers Landed

| Kind | Marker | Scope | Rule |
| --- | --- | --- | --- |
| Project | `+kubebuilder:validation:XValidation:rule=...,message=...` | ProjectSpec (struct) | `self.targetRepo.startsWith('http') || self.targetRepo.startsWith('git@')` |
| Task | `+kubebuilder:validation:MinItems=1` | TaskSpec.FilesTouched (field) | non-empty array |
| Wave | `+kubebuilder:validation:Minimum=0` | WaveSpec.WaveIndex (field) | non-negative integer |
| Project (Spec) | `+kubebuilder:validation:MinLength=1` | ProjectSpec.TargetRepo | non-empty string |
| Milestone (Spec) | `+kubebuilder:validation:MinLength=1` | MilestoneSpec.ProjectRef | non-empty string |
| Phase (Spec) | `+kubebuilder:validation:MinLength=1` | PhaseSpec.MilestoneRef | non-empty string |
| Plan (Spec) | `+kubebuilder:validation:MinLength=1` | PlanSpec.PhaseRef | non-empty string |
| Task (Spec) | `+kubebuilder:validation:MinLength=1` | TaskSpec.PlanRef | non-empty string |
| Wave (Spec) | `+kubebuilder:validation:MinLength=1` | WaveSpec.PlanRef | non-empty string |
| Project (Enum) | `+kubebuilder:validation:Enum=auto;approve;pause` | GatePolicy type | one of three values |

Verified the generated YAML at `config/crd/bases/tideproject.k8s_projects.yaml` contains the `x-kubernetes-validations:` block with the CEL rule; `tideproject.k8s_tasks.yaml` contains `minItems: 1`; `tideproject.k8s_waves.yaml` contains `minimum: 0`.

## Shared Status Condition Vocabulary

```go
package v1alpha1

const (
    ConditionPending     = "Pending"
    ConditionReady       = "Ready"
    ConditionReconciling = "Reconciling"
    ConditionFailed      = "Failed"

    ReasonInitialized            = "Initialized"
    ReasonAwaitingDispatch       = "AwaitingDispatch"
    ReasonFinalizerTimedOut      = "FinalizerTimedOut"
    ReasonSubagentDispatchFailed = "SubagentDispatchFailed"
)
```

Used uniformly across all six Kinds via `meta.SetStatusCondition` calls in Phase 2's reconciler bodies.

## Makefile Gates Added

### `verify-no-aggregates` (PERSIST-02 / Pitfall 4)

```makefile
.PHONY: verify-no-aggregates
verify-no-aggregates:
	@echo "verifying no aggregate schedule fields on api/v1alpha1 types (PERSIST-02)..."
	@MATCHES=$$(grep -nE 'Schedule|Waves *\[\]|IndegreeMap|CachedDag|DerivedDag' api/v1alpha1/*_types.go || true); \
	if [ -n "$$MATCHES" ]; then \
		echo "PERSIST-02 violation: aggregate schedule fields detected:"; \
		echo "$$MATCHES"; \
		exit 1; \
	fi
	@echo "OK: no aggregate schedule fields"
```

Greps real source for the forbidden field-name denylist. Exits 0 against current Phase 1 types.

### `verify-no-sqlite-dep` (PERSIST-01)

```makefile
.PHONY: verify-no-sqlite-dep
verify-no-sqlite-dep:
	@echo "verifying no DB driver deps in go.mod (PERSIST-01)..."
	@MATCHES=$$(grep -nE 'database/sql|github.com/mattn/go-sqlite3|gorm\.io|github.com/jackc/pgx' go.mod || true); \
	if [ -n "$$MATCHES" ]; then \
		echo "PERSIST-01 violation: forbidden DB drivers in go.mod:"; \
		echo "$$MATCHES"; \
		exit 1; \
	fi
	@echo "OK: no DB driver deps"
```

Greps go.mod for DB driver imports. Exits 0 against current Phase 1 dependency graph.

## aggregates_guard_test.go Contract (Revision Warning 4)

Replaces the originally-proposed manual "insert Schedule field → run gate → see exit 1 → revert" recipe with three programmatic assertions in `api/v1alpha1/aggregates_guard_test.go`:

| Test | What it asserts |
| --- | --- |
| `TestAggregatesGuardCatchesViolation` | Writes a temp file with `Schedule []string` and asserts the canonical regex flags it (proves the rule actually fires) |
| `TestAggregatesGuardSilentOnCleanFile` | Writes a clean `PlanStatus { Phase string }` and asserts the regex does NOT match (proves no false positives) |
| `TestMakeVerifyNoAggregatesPassesOnRealTypes` | Shells out to `make verify-no-aggregates` against the real Phase 1 codebase and asserts exit 0 (integration cross-check) |

All three run under `go test ./api/v1alpha1/... -run TestAggregatesGuard` in <1.3s. No mutation of real `*_types.go` files occurs.

## CI Workflow Steps Added

`.github/workflows/ci.yaml` gains two new steps, inserted directly after the DAG-05 step from Plan 03 and before the tide-lint step:

```yaml
- name: Verify pkg/dag imports (DAG-05)
  run: make verify-dag-imports

- name: Verify no aggregate schedule fields (PERSIST-02)
  run: make verify-no-aggregates

- name: Verify no DB drivers (PERSIST-01)
  run: make verify-no-sqlite-dep

- name: Run custom analyzers (POOL-03 / Pitfall 6)
  run: make tide-lint
```

The kubebuilder-generated lint.yml / test.yml / test-e2e.yml are untouched; this workflow is the TIDE-specific gate layer.

## Task Commits

| Task | Name | Commit | Files |
| --- | --- | --- | --- |
| 1 | shared_types.go + six *_types.go hand-edits + scoped controller-gen | `a2bf17e` | 15 files (6 *_types.go, shared_types.go, zz_generated.deepcopy.go, 6 CRD YAMLs, Makefile) |
| 2 | PERSIST Makefile gates + aggregates_guard_test.go + ci.yaml wiring | `e759f2d` | 4 files (Makefile, ci.yaml, aggregates_guard_test.go, plan_types.go comment paraphrase) |

**Plan metadata commit:** _(committed after SUMMARY/STATE/ROADMAP update)_

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] controller-gen `paths="./..."` cannot resolve testdata violation fixture**

- **Found during:** Task 1 (first run of `make generate`)
- **Issue:** Plan 02 introduced `tools/analyzers/dagimports/testdata/src/violation/pkg/dag/dag.go` which imports `_ "k8s.io/apimachinery/pkg/runtime"` — intentionally, with a `// want` directive proving the DAG-05 analyzer catches it. analysistest resolves this via its GOPATH-style fixture mechanism (with a stub package under `testdata/src/k8s.io/...`). But controller-gen uses standard Go module resolution and fails with `pkg/dag/dag.go:9:2: no required module provides package k8s.io/apimachinery/pkg/runtime`. This pre-dates Plan 05 (verified by checking out HEAD before my edits and running `make generate`) but blocks Plan 05's Task 1 from completing because `make generate` and `make manifests` are core acceptance criteria.
- **Fix:** Scoped the controller-gen invocation in `Makefile` from `paths="./..."` to:
  - `paths="./api/..."` for `generate` (DeepCopy methods only need to be generated for kubebuilder-tagged CRD types)
  - `paths="./api/..." paths="./internal/controller/..." paths="./internal/webhook/..."` for `manifests` (CRD types in api/, RBAC markers in internal/controller/, webhook markers in internal/webhook/)
- **Files modified:** `Makefile`
- **Verification:** `make generate` exits 0 and regenerates `zz_generated.deepcopy.go`; `make manifests` exits 0 and regenerates all six CRD YAML files; both are idempotent (no diff after re-run).
- **Committed in:** `a2bf17e` (Task 1)
- **Documented inline:** A comment in the Makefile explains the scope reasoning so a future contributor doesn't try to "fix" the paths back to `./...`.

**2. [Rule 1 - Bug] PlanStatus doc comment tripped the new `verify-no-aggregates` gate**

- **Found during:** Task 2 (first run of `make verify-no-aggregates`)
- **Issue:** The PlanStatus block I wrote in Task 1 contained the comment `// PERSIST-02 enforced: NO Schedule, NO Waves []slice, NO IndegreeMap.` — those exact tokens are the denylist the new gate greps for. The gate immediately fired on its own documentation: `api/v1alpha1/plan_types.go:31:// PERSIST-02 enforced: NO Schedule, NO Waves []slice, NO IndegreeMap.`
- **Fix:** Rewrote the comment to describe the same contract in paraphrase, without echoing the literal forbidden tokens. New comment: `PERSIST-02 / Pitfall 4: no aggregate wave list, no cached dag, no indegree map — see \`make verify-no-aggregates\` for the enforced field-name denylist.` The contract is still self-documenting in code and a reader knows where to find the authoritative regex (the Makefile target).
- **Files modified:** `api/v1alpha1/plan_types.go`
- **Verification:** `make verify-no-aggregates` exits 0 against the rewritten comment.
- **Committed in:** `e759f2d` (Task 2)
- **Pattern established:** When a Makefile gate enforces a token-based denylist, code comments must paraphrase the contract, not echo the denylist tokens. Documented in `patterns-established`.

---

**Total deviations:** 2 auto-fixed (1 Rule 3 - Blocking, 1 Rule 1 - Bug)

**Impact on plan:** Both auto-fixes are mechanical adjustments that don't change the plan's intent or output. The scoped controller-gen paths still cover every package carrying kubebuilder markers; the rewritten comment still documents the PERSIST-02 contract. No scope creep; no architectural change.

## Authentication Gates

None — this plan introduces no external service dependencies.

## Issues Encountered

- **`make generate` was already broken at HEAD before Plan 05 started.** Verified by reverting `api/v1alpha1/` to the pre-Plan-05 state and running `make generate`: same controller-gen error. The breakage was introduced by Plan 02's analysistest violation fixture but never surfaced because Plan 02's acceptance criteria didn't include re-running `make generate` after adding the testdata. Plan 05 surfaces and fixes it (Rule 3 - Blocking).
- **`git stash --keep-index` followed by `git checkout HEAD -- api/v1alpha1/` did NOT save uncommitted untracked files (shared_types.go) — they survived, but the tracked-file edits to the six *_types.go reverted to HEAD. `git stash pop` restored everything cleanly; no work lost.** A reminder that `git checkout HEAD -- <path>` is destructive to working-tree edits in the targeted path — use `git stash` for full safety, not `--keep-index`.

## User Setup Required

None — all changes are file edits, Makefile targets, and CI workflow steps. No external API keys, no cluster, no service accounts.

## Next Phase Readiness

**Ready for Plan 01-06 (reconciler bodies):**
- Reconcilers in `internal/controller/*_controller.go` can now consume real Spec/Status field types from `api/v1alpha1`. The `meta.SetStatusCondition` calls should use the constants from `shared_types.go` (`ConditionPending`, `ConditionReady`, etc.) for uniform vocabulary.
- `TaskSpec.FilesTouched` is the field Plan 07's Plan webhook will reconcile against `TaskSpec.DependsOn` (Phase 2's REQ-PLAN-02 file-touch ↔ dependsOn check).
- `WaveStatus.TaskRefs` is the field Plan 06's WaveReconciler writes after calling `pkg/dag.ComputeWaves`.

**Ready for Plan 01-07 (webhook bodies):**
- Plan webhook will validate `TaskSpec.DependsOn` references resolve within the same Plan; the data lives in `Task.Spec.DependsOn []string` now (D-F1).
- Wave webhook will reject any client-applied Wave (D-B1) — the schema is locked at two fields so the rejection logic just looks at "is this from the WaveReconciler".

**Ready for Plan 01-10 (sample CRDs):**
- Sample YAMLs can now be authored with realistic content. The α…θ worked example will fit naturally: 8 Task CRDs with `dependsOn` arrays matching the README spec.

**Phase 2+ hand-off:**
- The CEL XValidation rule on `ProjectSpec.targetRepo` is admission-time enforcement; Phase 2's validating admission webhook (`internal/webhook/v1alpha1/plan_webhook.go`) only handles graph-shape validation (cycle detection) that CEL cannot express.
- `Spec.SecretRefs`, `Spec.ModelSelection`, `Spec.Gates` on Project are field shapes only — Phase 2/3/4 consume them in dispatch, model selection, and human-gate wiring respectively.

**Concerns / watch-items:**
- The `verify-no-aggregates` regex is intentionally strict — it will false-positive on ANY code comment that contains the literal tokens (Schedule / Waves []slice / IndegreeMap / CachedDag / DerivedDag). Future contributors writing comments must paraphrase. Documented as `patterns-established`.
- The scoped controller-gen paths in the Makefile are now load-bearing — Plan 06 must not revert them to `./...` even when adding new internal packages that carry kubebuilder markers. If a future Plan adds markers in a new directory tree (e.g., `internal/dispatch/` for a future operator-managed CRD), it must add a corresponding `paths=` flag.

## Self-Check: PASSED

- All commits exist:
  - `a2bf17e` Task 1 (six types + shared_types + scoped controller-gen)
  - `e759f2d` Task 2 (PERSIST gates + aggregates_guard_test + ci.yaml)
- All claimed files present:
  - `api/v1alpha1/shared_types.go` (created)
  - `api/v1alpha1/aggregates_guard_test.go` (created)
  - `api/v1alpha1/project_types.go`, `milestone_types.go`, `phase_types.go`, `plan_types.go`, `task_types.go`, `wave_types.go` (modified)
  - `api/v1alpha1/zz_generated.deepcopy.go` (regenerated, modified)
  - `config/crd/bases/tideproject.k8s_{projects,milestones,phases,plans,tasks,waves}.yaml` (regenerated, 6 files)
  - `Makefile` (modified — `verify-no-aggregates` + `verify-no-sqlite-dep` targets + scoped controller-gen paths)
  - `.github/workflows/ci.yaml` (modified — two new steps after DAG-05)
- Verification commands all exit 0:
  - `go build ./...`
  - `go vet ./...`
  - `go test ./api/v1alpha1/... -count=1` (3 tests, ~1.3s)
  - `make generate` (idempotent; no diff after re-run)
  - `make manifests` (idempotent; no diff after re-run)
  - `make verify-no-aggregates`
  - `make verify-no-sqlite-dep`
  - `make verify-dag-imports` (still clean; no regression)
  - `make tide-lint` (still clean; no regression)
- All acceptance-criteria grep checks pass:
  - `test -f api/v1alpha1/shared_types.go` ✓
  - `grep -q "ConditionReady" api/v1alpha1/shared_types.go` ✓
  - `grep -q "ConditionReconciling" api/v1alpha1/shared_types.go` ✓
  - For each Kind: `grep -q "type <Kind>Spec struct"` and `grep -q "type <Kind>Status struct"` ✓
  - `grep -B 1 "type ProjectSpec" api/v1alpha1/project_types.go | grep -q "XValidation"` ✓
  - `grep -q "MinItems=1" api/v1alpha1/task_types.go` ✓
  - `grep -q "DependsOn \[\]string" api/v1alpha1/task_types.go` ✓
  - `grep -q "Minimum=0" api/v1alpha1/wave_types.go` ✓
  - `awk '/type WaveSpec struct/,/^}/' api/v1alpha1/wave_types.go | grep -c "json:"` returns **2** (D-B2 invariant) ✓
  - `grep -c "v1alpha1" api/v1alpha1/groupversion_info.go` returns ≥1 ✓
  - `ls config/crd/bases/ | grep -c "tideproject.k8s_"` returns **6** ✓
  - `grep -l "x-kubernetes-validations" config/crd/bases/*.yaml | wc -l` returns **≥1** ✓
  - `grep -q "verify-no-aggregates:" Makefile` ✓
  - `grep -q "verify-no-sqlite-dep:" Makefile` ✓
  - `grep -q "PERSIST-02" Makefile` ✓
  - `grep -q "PERSIST-01" Makefile` ✓
  - `grep -q "verify-no-aggregates" .github/workflows/ci.yaml` ✓
  - `grep -q "verify-no-sqlite-dep" .github/workflows/ci.yaml` ✓
  - `test -f api/v1alpha1/aggregates_guard_test.go` ✓
  - `go test ./api/v1alpha1/... -run TestAggregatesGuard` exits 0 ✓
- Anti-checks pass:
  - `grep -rE "tide\.io|my\.domain|example\.com" --include="*.go" --include="*.yaml" --include="Makefile" api/v1alpha1/ .github/` returns no matches (excluding legitimate `k8s.io/` imports)
  - No new `tide.io` references introduced anywhere

---
*Phase: 01-foundation-crds-pkg-dag-controller-scaffold*
*Completed: 2026-05-12*
