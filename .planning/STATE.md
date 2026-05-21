---
gsd_state_version: 1.0
milestone: v1.0
milestone_name: bump. Closes Phase 02.1's BLOCKED runtime gate captured in 02.1-04-VERIFICATION.md.
status: executing
stopped_at: Phase 4 UI-SPEC approved
last_updated: "2026-05-21T04:48:51.108Z"
last_activity: 2026-05-21 -- Completed quick task 260521-gmm: Phase 03 cascade 11 pvcPrewarmPod helper (closes WaitForFirstConsumer deadlock for Pod-less fixtures)
progress:
  total_phases: 8
  completed_phases: 6
  total_plans: 82
  completed_plans: 79
  percent: 96
---

# Project State

## Project Reference

See: .planning/PROJECT.md (updated 2026-05-12)

**Core value:** The five-level paradigm (Milestone → Phase → Plan → Task → Wave) runs as a real K8s orchestrator that can drive its own next milestone end-to-end.
**Current focus:** Phase 04.1 — pre-v1-audit-fixes-cross-phase-uat-closeout

## Current Position

Phase: 04.1 (pre-v1-audit-fixes-cross-phase-uat-closeout) — EXECUTING
Plan: 1 of 15
Status: Executing Phase 04.1
Last activity: 2026-05-21 -- Completed quick task 260521-gmm: Phase 03 cascade 11 pvcPrewarmPod helper (closes WaitForFirstConsumer deadlock for Pod-less fixtures)

Progress: [██████████] 100% (Phase 02.2 scope)

## Performance Metrics

**Velocity:**

- Total plans completed: 13
- Average duration: —
- Total execution time: 0 hours

**By Phase:**

| Phase | Plans | Total | Avg/Plan |
|-------|-------|-------|----------|
| 02 | 13 | - | - |

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
| Phase 02 P09 | multi-session | 3 tasks | 23 files |

## Accumulated Context

### Roadmap Evolution

- Phase 02.1 inserted after Phase 2: Layer B kind integration tests: ship test files in Phase 2, debug/fix in 2.1 to make make test-int green on dev laptop (URGENT)
- Phase 02.2 inserted after Phase 02: Layer B kind test timing fixes — closes Phase 02.1 BLOCKED runtime gate (kindTestTimeout vs helm timeout mismatch; AfterSuite zombie-container cleanup; make test-int wall-time scope; optional cert-manager bump) (URGENT)
- Phase 02.2 COMPLETED 2026-05-14: Closed Phase 02.1's BLOCKED runtime gate. 12 tactical iterations, 11 cascades closed. Five original fixes landed (kindTestTimeout 4m→7m; cert-manager v1.16.2→v1.20.2; helm install --replace; cleanupKindCluster() zombie-container fallback; CI YAML DUR-check drop). Seven additional harness/production-wiring fixes surfaced and closed across cascades 2–11: --metrics-bind-address flag, --webhook-cert-path flag, Makefile 300s→600s→1800s budget, credproxy/caps/output/failure fixture helpers, Dispatcher field nil production wiring, Job activeDeadlineSeconds + Layer A Eventually budgets + Makefile timeout, PVC namespace-scoping architectural pivot (PodStatusEnvelopeReader), Secret namespace-scoping (ensureSigningKeySecret). End-to-end runtime verification PASSED: 7/7 Layer B specs PASS (clean + rerun), 18/18 Layer A PASS; inner wall 355.20s; chain_status: empirically_closed. Captured in 02.2-12-VERIFICATION.md.
- Phase 04.1 inserted after Phase 4: Pre-v1 audit fixes + cross-phase UAT closeout (URGENT)

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
- [Phase 02.2]: kindTestTimeout = 7m (NOT 6m floor): RESEARCH §"Pattern 1" budget arithmetic — 50s pre-helm setup + 300s helm --timeout 5m + 5s waitForControllerReady + 60s+ variance margin. 7m gives 2m margin above helm's 5m, survives slow CI runners. 6m floor leaves only 60s margin and re-introduces shadow-kill risk.
- [Phase 02.2]: AfterSuite zombie cleanup pattern = kind delete → docker ps -aq --filter label=io.x-k8s.kind.cluster=<name> → docker rm -f -v fallback. Best-effort (GinkgoWriter.Printf warnings, NOT Fail()). KEEP_KIND_CLUSTER=true short-circuit MUST precede the cleanup helper (T-02.2-02 mitigation). Plain exec.Command() (not exec.CommandContext) because outer ctx is cancelled by AfterSuite entry (Pitfall 7).
- [Phase 02.2]: Test wall-time budget scope = go test only (not full make test-int chain). Makefile's existing inner `timeout 300s go test` is the source of truth; CI's outer DUR > 300 check measured the wrong span (includes ~880s cold-cache test-int-kind-prep). Dropped the CI outer check; rely on `timeout 300s` exit 124 propagating through `time make test-int`. Final budget: INTEGRATION_TIMEOUT=1800s outer, KIND_GO_TEST_TIMEOUT=20m inner go test, budget raised from 300s to 600s to 1800s across cascades 4/9C.
- [Phase 02.2]: cert-manager v1.20.2 bump verified non-breaking for TIDE — chart uses cert-manager.io/v1 Issuer + Certificate (stable since 1.x); both Certificate templates already specify issuerRef.kind: Issuer explicitly (v1.20 issuerRef-defaults revert is non-impacting per RESEARCH Pattern 4 + Pitfall 4).
- [Phase 02.2]: helm install --replace flag added to applyController() helm args for KEEP_KIND_CLUSTER=true rerun idempotency (Q1 micro-fix from RESEARCH §"Open Questions Surfaced"). Resolves "cannot re-use a name that is still in use" failure mode when rerun encounters an existing tide release in tide-system.
- [Phase 02.2]: PVC namespace-scoping architectural pivot (cascade-10) — manager-side PVC mount removed; EnvelopeReader is now PodStatusEnvelopeReader which reads EnvelopeOut from the subagent container's terminationMessagePath via Pod.status.containerStatuses[].state.terminated.message. Manager no longer requires cross-namespace PVC visibility. Pod-side PVC remains namespace-local provisioned by ensureProjectsPVC(ns) per test namespace; testdata/three-task-wave.yaml declares it inline for kubectl apply -f self-containedness.
- [Phase 02.2]: Secret namespace-scoping (cascade-11) — ensureSigningKeySecret(ns) mirrors tide-system/tide-signing-key into target namespace via kubectl get secret -o jsonpath + base64-identical data. controllerSigningKeyData() centralizes the read; createNamespace(ns) + applyController() both call it. CRDs-only mode degrades to GinkgoWriter warning (not Fail()).
- [Phase 02.2]: 02.2-12-VERIFICATION.md records chain_status: empirically_closed — 7/7 Layer B PASS (clean + rerun), 18/18 Layer A PASS, inner wall 355.20s, pvc_not_found_event_count=0, signing_key_not_found_event_count=0, deadline_exceeded_count=0.

### Pending Todos

[From .planning/todos/pending/ — ideas captured during sessions]

None yet.

### Blockers/Concerns

[Issues that affect future work]

- **Phase 1 is the densest pitfall window** (PITFALLS.md): 8 critical/serious pitfalls bake in at the CRD-schema + controller-scaffold boundary — long-running reconcile (P1), status-as-truth resumption bug (P4), DAG unification (P3), unified worker pool (P6), RBAC scope creep (P15), breaking CRD schema changes (P16), finalizer leaks (P21), wrong owner refs (P23). Plan-time research should focus there.
- **Phase 2 carries the security/correctness fanout** (PITFALLS.md): subagent context bleed (P7), runaway agent loops (P8), 429 rate-limit cascade (P9), watch-lag duplicate dispatch (P11), secret leakage (P18 harness side), hallucinated `depends_on` (P19), indegree-on-partial-failure (P10).
- **Bootstrap deadlock (Pitfall 12)** is structurally addressed: Phases 1-4 = M0 (TIDE-on-host via GSD), Phase 5 = M_self (TIDE-in-cluster authors same artifacts). Both pinned to `v1alpha1` schema with no breaking changes across the bridge.

### Quick Tasks Completed

| # | Description | Date | Commit | Directory |
|---|-------------|------|--------|-----------|
| 260521-ccz | Phase 03 cascade 9: apply createNamespace(pushLeaseNS) recipe + drop SKIP gate | 2026-05-21 | 5e1db67, bc3ed29 | [260521-ccz-push-lease-cascade-9-recipe](./quick/260521-ccz-push-lease-cascade-9-recipe/) |
| 260521-eoz | Phase 03 cascade 10: Pillar 4 List filter (refutes duplicate-dispatch framing) | 2026-05-21 | aa65c8e | [260521-eoz-phase-03-cascade-10-filter-pillar-4-list](./quick/260521-eoz-phase-03-cascade-10-filter-pillar-4-list/) |
| 260521-f8x | Phase 03 cascade 7: gate Plan-planner dispatch on resolveProjectForPlan != nil | 2026-05-21 | 88356ad, 6212147 | [260521-f8x-phase-03-cascade-7-gate-plan-planner-dis](./quick/260521-f8x-phase-03-cascade-7-gate-plan-planner-dis/) |
| 260521-gmm | Phase 03 cascade 11: pvcPrewarmPod helper to bind WaitForFirstConsumer PVCs | 2026-05-21 | e8083a5 | [260521-gmm-phase-03-cascade-11-pvcprewarmpod-helper](./quick/260521-gmm-phase-03-cascade-11-pvcprewarmpod-helper/) |

## Session Continuity

Last session: 2026-05-17T12:59:01.629Z
Stopped at: Phase 4 UI-SPEC approved
Resume file: .planning/phases/04-gates-observability-dashboard-cli/04-UI-SPEC.md
