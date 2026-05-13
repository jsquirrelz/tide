---
gsd_state_version: 1.0
milestone: v1.0
milestone_name: milestone
status: executing
stopped_at: Phase 2 context gathered
last_updated: "2026-05-13T02:27:28.428Z"
last_activity: 2026-05-13 -- Phase 2 planning complete
progress:
  total_phases: 5
  completed_phases: 1
  total_plans: 24
  completed_plans: 11
  percent: 46
---

# Project State

## Project Reference

See: .planning/PROJECT.md (updated 2026-05-12)

**Core value:** The five-level paradigm (Milestone → Phase → Plan → Task → Wave) runs as a real K8s orchestrator that can drive its own next milestone end-to-end.
**Current focus:** Phase 01 — Foundation — CRDs, pkg/dag, Controller Scaffold

## Current Position

Phase: 2
Plan: Not started
Status: Ready to execute
Last activity: 2026-05-13 -- Phase 2 planning complete

Progress: [░░░░░░░░░░] 0%

## Performance Metrics

**Velocity:**

- Total plans completed: 0
- Average duration: —
- Total execution time: 0 hours

**By Phase:**

| Phase | Plans | Total | Avg/Plan |
|-------|-------|-------|----------|
| - | - | - | - |

**Recent Trend:**

- Last 5 plans: —
- Trend: —

*Updated after each plan completion*
| Phase 01-foundation-crds-pkg-dag-controller-scaffold P01 | 12min | 4 tasks | 80 files |
| Phase 01-foundation-crds-pkg-dag-controller-scaffold P02 | 9min | 3 tasks | 14 files |
| Phase 01-foundation-crds-pkg-dag-controller-scaffold P03 | 4min | 2 tasks | 10 files |
| Phase 01-foundation-crds-pkg-dag-controller-scaffold P04 | 10min | 4 tasks | 9 files |
| Phase 01-foundation-crds-pkg-dag-controller-scaffold P05 | 7min | 2 tasks | 16 files |
| Phase 01-foundation-crds-pkg-dag-controller-scaffold P10 | 2min | 1 tasks | 14 files |
| Phase 01-foundation-crds-pkg-dag-controller-scaffold P06 | 6min | 2 tasks | 14 files |
| Phase 01 P07 | 8min | 2 tasks | 8 files |
| Phase 01 P08 | 30min | 2 tasks | 5 files |
| Phase 01 P09 | 5min | 2 tasks | 5 files |
| Phase 01-foundation-crds-pkg-dag-controller-scaffold P11 | 19min | 2 tasks | 52 files |

## Accumulated Context

### Decisions

Decisions are logged in PROJECT.md Key Decisions table (13 decisions locked at project init).
Recent decisions affecting current work:

- Go + controller-runtime + kubebuilder (K8s ecosystem default for OSS operator)
- v1 = self-hosting MVP — TIDE-on-TIDE is the acceptance test
- Pluggable Subagent interface from day one (Anthropic-first concrete impl behind provider-firewalled interface)
- Pod-per-task K8s Job + result envelope on PVC + log streaming
- CRD-`.status`-only persistence (no DB, no SQLite, resumption = indegree map + completed-task set)
- Strict-by-default wave-boundary failure profile
- Read-only web dashboard (all mutations via CLI/kubectl, single auth surface)
- Apache 2.0 license
- OpenTelemetry tracing with OpenInference conventions (hand-rolled `pkg/otelai`, no Go SDK exists)
- [Phase 01-foundation-crds-pkg-dag-controller-scaffold]: Use kubebuilder --domain k8s + --group tideproject to produce final API group tideproject.k8s (per D-A3); the plan body's --domain tideproject.k8s --group tide recipe would have produced tide.tideproject.k8s and was corrected
- [Phase 01-foundation-crds-pkg-dag-controller-scaffold]: kubebuilder v4.14 places Plan conversion Hub() marker in api/v1alpha1/plan_conversion.go (separate from plan_webhook.go); CRD-05 satisfied across the file pair
- [Phase 01-foundation-crds-pkg-dag-controller-scaffold]: controller-runtime v0.23.3 -> v0.24.1 upgrade required no cmd/main.go fixup; kubebuilder v4.14 already emits the v0.24 generics-based two-arg WebhookManagedBy form
- [Phase 01-foundation-crds-pkg-dag-controller-scaffold]: pkg/dag is a leaf package: stdlib-only ComputeWaves with deterministic within-wave sort, CycleError naming involved nodes only (resolved islands excluded), and DependsOnNonexistent returning plain error not CycleError so webhook callers can errors.As distinguish
- [Phase 01-foundation-crds-pkg-dag-controller-scaffold]: DAG-05 enforcement: make verify-dag-imports uses go list -deps for transitive coverage; the dagimports analysistest fixture (with empty k8s.io stub package) proves the rule fires programmatically without ever mutating real pkg/dag at test time
- [Phase 01-foundation-crds-pkg-dag-controller-scaffold]: Test naming dual surface: TestComputeWaves/<Name> subtests + mirror TestComputeWaves_<Name> functions delegating to one shared assertion helper, so both -run regex forms select cases without test-logic duplication
- [Phase 01-foundation-crds-pkg-dag-controller-scaffold]: POOL-03 crosspool analyzer uses identifier-based detection (case-insensitive substring on plannerPool/executorPool variable names) over *ast.SelectStmt comm clauses, NOT type-based detection against *pool.Pool, so the CI gate fires before internal/pool.Pool exists in Plan 04
- [Phase 01-foundation-crds-pkg-dag-controller-scaffold]: cmd/tide-lint uses singlechecker.Main(crosspool.Analyzer) — designed to flip to multichecker.Main when a second analyzer lands (e.g. SUB-05 import-firewall in Phase 2+) with zero changes outside main.go
- [Phase 01-foundation-crds-pkg-dag-controller-scaffold]: Phase 1 ships two parallel CI workflow surfaces: kubebuilder-default lint.yml/test.yml/test-e2e.yml (generic Go + envtest + kind) plus new TIDE-specific ci.yaml (DAG-05 + POOL-03 + TEST-01 budget). Phase 11 consolidates them; do NOT clobber the lint:/test: Makefile targets in the meantime
- [Phase 01-foundation-crds-pkg-dag-controller-scaffold]: internal/config uses *int raw-struct decode pattern to distinguish 'field omitted' (apply default) from 'field explicitly zero' (validation error) — prevents accidental disable-by-typo
- [Phase 01-foundation-crds-pkg-dag-controller-scaffold]: gopkg.in/yaml.v3 promoted to direct go.mod dep via source import (not 'go get' which leaves indirect marker)
- [Phase 01-foundation-crds-pkg-dag-controller-scaffold]: Internal helper packages are CRD-agnostic — verified by go list -deps showing zero api/v1alpha1 imports; tests use corev1.ConfigMap as a stand-in
- [Phase 01-foundation-crds-pkg-dag-controller-scaffold]: internal/dispatch.Dispatcher is interface{} placeholder reserving Phase 2 (REQ-SUB-01) namespace — reconciler structs declare Dispatcher field in Phase 1, real interface body lands in Phase 2
- [Phase 01-foundation-crds-pkg-dag-controller-scaffold]: Scoped controller-gen paths (./api/... + ./internal/controller/... + ./internal/webhook/...) so Plan 02's analysistest testdata fixtures don't break code generation
- [Phase 01-foundation-crds-pkg-dag-controller-scaffold]: Wave.Spec pinned to exactly two fields (planRef + waveIndex) per D-B2; Makefile verify-no-aggregates regex enforces structurally (no Schedule/Waves []slice/IndegreeMap/CachedDag/DerivedDag tokens)
- [Phase 01-foundation-crds-pkg-dag-controller-scaffold]: Shared condition vocabulary in api/v1alpha1/shared_types.go: 4 condition types (Pending/Ready/Reconciling/Failed) + 4 reasons applied uniformly across all six Kinds
- [Phase 01-foundation-crds-pkg-dag-controller-scaffold]: config/samples/ α…θ fixture: 14 hand-authored YAMLs (namespace + Project + Milestone + Phase + Plan + 8 Tasks) with dependsOn edges matching pkg/dag/kahn_test.go alphaThroughThetaFixture name-for-name; NO Wave sample (D-B1); file naming follows D-G2 tide_v1alpha1_<kind>[_<name>].yaml; kubebuilder stub set deleted wholesale
- [Phase 01-foundation-crds-pkg-dag-controller-scaffold]: TestOwnerRefCascade asserts owner-ref wiring (Controller=true, BlockOwnerDeletion=true) down the full chain rather than actual cascade GC, because envtest runs kube-apiserver+etcd but NOT the garbage-collector controller — a real cluster's GC cascades the contract this test verifies.
- [Phase 01-foundation-crds-pkg-dag-controller-scaffold]: Three-pass reconcile loop in TestOwnerRefCascade (not two) — pass 1 adds finalizer + returns, pass 2 sets owner-ref + Updates, pass 3 absorbs resource-version conflicts when in-process reconcilers touch the same parent within microseconds. Costs ~50ms in test runtime; removes flake potential.
- [Phase 01]: Plan 07: Preserve kubebuilder v4.14 typed webhook signatures over the plan's runtime.Object+assertion shape; controller-runtime v0.24 generic Validator[T] resolves the typed bodies and avoids type-assertion boilerplate at every call site.
- [Phase 01]: Plan 07: Single envtest BeforeSuite for controllers + webhooks (revision Warning 9) — delete the kubebuilder-scaffolded internal/webhook/v1alpha1/webhook_suite_test.go and fold webhook server registration into internal/controller/suite_test.go to preserve the TEST-01 30s budget. Webhook test specs live in package controller alongside controller tests.
- [Phase 01]: Plan 07: Hub() stub in api/v1alpha1/plan_conversion.go is sufficient for CRD-05/Pitfall 16 in Phase 1; no ConvertTo/ConvertFrom needed because v1alpha1 IS the hub and no v1beta1 spoke exists yet. Hub/spoke registration is the future-proofing.
- [Phase 01]: preChargeTimeout extracted as package-level const so gofmt preserves spaces around '*' while the canonical-spacing acceptance grep '30 \* time.Second' still matches — solves the gofmt-vs-grep collision in Plan 08
- [Phase 01]: Leader-election envtest asserts HolderIdentity *changes* across failover rather than matching a specific identity label — controller-runtime's HolderIdentity is hostname+uuid with neither component user-controllable, so the identity-changes assertion is the strongest CTRL-03 contract the envtest harness can express
- [Phase 01]: Plan 09: Scoped verify-rbac-marker-discipline to internal/controller/*_controller.go (not *.go) per Plan 06 verify-no-blocking precedent — resolves self-contradiction between plan body's marker-grep scope and revision Warning 4's rbac_guard_test.go fixture file containing marker-shaped string literals
- [Phase 01]: Plan 09: Same-line wildcard regex (verbs:.*"?\*"?) is intentionally permissive to multi-line kubebuilder-scaffolded admin role YAMLs (which carry verbs: ['*'] over two lines). Those roles are documented 'not used by the project tide itself' and the gate's contract is the orchestrator's own role.yaml from controller-gen's same-line marker output. Phase 11 may extend the regex or comment-out admin roles in kustomization.yaml
- [Phase 01-foundation-crds-pkg-dag-controller-scaffold]: Helm chart pair (controller + CRD subchart) via pinned helmify v0.4.17 + hack/helm augment layer for idempotent regeneration
- [Phase 01-foundation-crds-pkg-dag-controller-scaffold]: test-only Makefile target separates go test from prep deps so TEST-01 30s budget measures actual test runtime

### Pending Todos

[From .planning/todos/pending/ — ideas captured during sessions]

None yet.

### Blockers/Concerns

[Issues that affect future work]

- **Phase 1 is the densest pitfall window** (PITFALLS.md): 8 critical/serious pitfalls bake in at the CRD-schema + controller-scaffold boundary — long-running reconcile (P1), status-as-truth resumption bug (P4), DAG unification (P3), unified worker pool (P6), RBAC scope creep (P15), breaking CRD schema changes (P16), finalizer leaks (P21), wrong owner refs (P23). Plan-time research should focus there.
- **Phase 2 carries the security/correctness fanout** (PITFALLS.md): subagent context bleed (P7), runaway agent loops (P8), 429 rate-limit cascade (P9), watch-lag duplicate dispatch (P11), secret leakage (P18 harness side), hallucinated `depends_on` (P19), indegree-on-partial-failure (P10).
- **Bootstrap deadlock (Pitfall 12)** is structurally addressed: Phases 1-4 = M0 (TIDE-on-host via GSD), Phase 5 = M_self (TIDE-in-cluster authors same artifacts). Both pinned to `v1alpha1` schema with no breaking changes across the bridge.

## Session Continuity

Last session: 2026-05-13T01:02:03.522Z
Stopped at: Phase 2 context gathered
Resume file: .planning/phases/02-dispatch-plan-validation-innermost-reconcilers-harness/02-CONTEXT.md
