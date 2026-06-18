# Project Research Summary

**Project:** TIDE v1.0.3 — Planning Resumption & Cost Resilience
**Domain:** Kubernetes CRD operator — envelope-resumption, budget-halt correctness, plan-import
**Researched:** 2026-06-18
**Confidence:** HIGH

## Executive Summary

TIDE v1.0.3 addresses a concrete failure observed in dogfood run #2: the project spent ~$90 authoring 59 planning envelopes (3 milestones, 15 phases, 39 plans, 1 project), then budget-halted with zero execution. Re-running from scratch would re-pay the full planning cost. The milestone has two separable workstreams: (1) standalone correctness bugs on the existing budget-bypass path that must be fixed first, independently of any import work, and (2) a plan-import mechanism that bridges the UID-churn gap so a fresh `kubectl apply` can resume from salvaged envelopes. All prior-art workflow orchestrators (Argo, Temporal, Prefect, Dagster, Bazel) converge on the same principle: key on stable, input-derived identifiers rather than runtime IDs; validate before skipping; maintain DAG ordering through the skip.

The most critical correctness bugs are already identified and localized. R-01 (`bypass-clear sets Phase=Pending` should be `Running`; sentinel is `Status.Git.BranchName != ""`) and R-04 (budget rollup double-count when reporter Job TTL-GC races — needs a durable `PlannerRolledUpUID` status field, not reporter-Job-existence) are standalone fixes that do not touch the import path and must be resolved before any import work begins. R-07 (clone Job re-dispatches on resume because TTL-GC'd Job is the only guard — needs `Status.Git.CloneComplete` flag) rounds out the Phase 1 correctness work. The already-landed project-controller ordering fix (`2a5e0dc`) also lacks regression coverage and must be addressed in this phase.

The plan-import feature presents an unresolved design tension that is THE key decision for plan-phase discussion. STACK.md and FEATURES.md recommend Approach A (name-based / stable-key envelope directory: `envelopes-by-name/{level}/{crdName}/out.json`, written at planner completion, checked before dispatch). ARCHITECTURE.md recommends Approach B (UID-rewrite import step: one-shot `ImportController` + `tide-import` Job copies old-UID envelope trees to new-UID paths before the normal reconcile runs) as architecturally safer because it leaves all five reconcilers, the reporter Job, and the `FilesystemEnvelopeReader` path contract unchanged. Both approaches are valid; the roadmapper must not pick a winner — this must be surfaced as an explicit checkpoint before Phase 2 implementation begins.

## Key Findings

### Recommended Stack

No new go.mod entries are required for v1.0.3. All implementation uses existing stdlib (`path/filepath`, `encoding/json`, `os`, `crypto/sha256`) and existing controller-runtime patterns. The `AlreadyExists`-is-success idempotency model already used throughout the dispatcher (`internal/dispatch/podjob/names.go:SUB-03`) extends naturally to both the planner-skip and import-copy operations. The one new binary (`cmd/tide-import/main.go` under Approach B) uses only stdlib I/O. The `github.com/cespare/xxhash/v2 v2.3.0` is already an indirect dep and available at zero cost if a non-crypto hash is wanted, but is not needed for either approach.

**Core technologies — all existing, zero new additions:**
- `path/filepath` (stdlib): stable-key path construction — already used in `FilesystemEnvelopeReader`
- `encoding/json` (stdlib): envelope marshal/unmarshal and import validation — already used throughout
- `crypto/sha256` (stdlib): optional write-side integrity checksum — already in `internal/credproxy/token.go`
- `controller-runtime v0.24.1` (locked): reconcile loop, condition patching — existing `AlreadyExists`-is-success pattern extends to both planner-skip and import
- `os.Rename` (stdlib): atomic write for UID-rewrite step (Approach B) — prevents partial-write corruption on Job kill

### Expected Features

Research from all prior-art systems converges on a two-tier feature set: fix the broken bypass path first (table stakes, no import required), then add import as the headline differentiator.

**Must have — Phase 1 correctness (independent of import):**
- R-01 fix: bypass-clear targets `Running` not `Pending`; guarded by `Status.Git.BranchName != ""`
- R-04 fix: durable `Status.Budget.PlannerRolledUpUID` replaces reporter-Job-existence guard for rollup idempotency
- R-07 fix: `Status.Git.CloneComplete` durable flag gates clone-Job dispatch (prevents re-clone on resume)
- Cap-raise ergonomics (TS-R6): both cap values evaluated simultaneously before re-halting
- Regression envtest for `2a5e0dc` ordering fix (TS-R7)

**Must have — Phase 2 import core:**
- Stable envelope identity: either name-based directory (Approach A) or UID-rewrite import step (Approach B) — decision gates all other import work
- Envelope completeness validation: `len(ChildCRDs) == ChildCount` before any planner skip (R-03)
- Explicit cycle detection before creating any child CRDs: `dag.ComputeWaves` must run — webhook bypassed by `client.Create` (R-12)
- v1alpha1→v1alpha2 schema conversion for salvaged `ChildCRDSpec.Spec.Raw` bytes (R-06)
- Wave CRs must NOT be imported — always re-derived by `deriveGlobalWaves` (Anti-Pattern 2)
- Budget rollup suppressed for imported envelopes (R-13)
- Import atomicity per-Milestone: partial Milestone import rejected (R-05)
- `ImportComplete` condition as first-step guard for at-least-once idempotency (R-09)

**Should have — Phase 3 operator tooling:**
- `tide import-envelopes` or `tide import seed` CLI verb + `SeedManifestConfigMap` generation
- Dry-run mode: report accepted/rejected envelopes before writing
- `tide export-envelopes`: export PVC envelopes to local directory for portability across cluster teardowns

**Defer to v1.0.4+:**
- Per-level "Resumed from envelope" dashboard badge (D-R3): non-blocking observability feature

**Anti-features (do not build):**
- Cache the wave schedule in `.status` — direct spec invariant violation
- Accept UID-keyed salvaged envelope directly on fresh apply — UID churn guaranteed
- Skip cycle detection on import — cycles are bugs, not runtime conditions
- In-memory envelope cache across reconciles — violates CRD-status-only persistence

### Architecture Approach

The five existing planner dispatch sites (`reconcileProjectPlannerDispatch` and four level-specific `reconcilePlannerDispatch` methods) all follow the same child-existence idempotency guard pattern. Any import approach must satisfy this guard naturally. The `ImportComplete` condition on `Project.Status` (Approach B) or the `tideproject.k8s/envelope-adopted-from` annotation (Approach A) serves as the observable import signal. Wave CRs are never imported — `deriveGlobalWaves` derives them fresh from the imported Task graph on first reconcile.

**Major components — Approach B (UID-rewrite, ARCHITECTURE.md recommendation):**

1. `api/v1alpha2/import_types.go` (NEW) — `ImportSourceRef` + `ImportSeedManifest` JSON schema (name → savedUID + status per CR)
2. `internal/controller/import_controller.go` (NEW) — state machine: `Pending → CreatingCRs → CopyingEnvelopes → Complete / Failed`; materializes CRs from seed, dispatches `tide-import` Job, sets `ConditionImportComplete`
3. `cmd/tide-import/main.go` (NEW binary) — in-pod: `cp -n` old-UID → new-UID + rename-atomic `out.json` TaskUID patch
4. `ImportComplete` guard at all five dispatch sites (5x one-liner after Step 1 terminal short-circuit)
5. `cmd/tide/import_seed.go` (NEW subcommand) — reads salvage tgz, emits `SeedManifestConfigMap` YAML

**Major components — Approach A (stable-key, STACK.md / FEATURES.md recommendation):**

1. `pkg/dispatch/envelope_paths.go` (NEW) — `StableKeyEnvelopePath(workspaceRoot, projectUID, level, crdName)` constructor
2. `ReadByName` / `WriteStableKey` on `EnvelopeReader` interface — stable-key read/write alongside existing UID-keyed path
3. `adoptEnvelopeAndMaterialize` method on each planner reconciler (5x) — stamps annotation, materializes children, skips Job dispatch
4. `api/v1alpha2/shared_types.go` addition — `AnnotationEnvelopeAdoptedFrom` constant
5. `tide import-envelopes` CLI subcommand — reads salvage tgz, extracts `in.json: dispatch.parentName`, writes to stable-key paths

### Critical Pitfalls

1. **R-01: Bypass-clear sets Phase=Pending, triggering re-init loop** — Fix `project_controller.go:1257` to `PhaseRunning`; guard with `Status.Git.BranchName != ""` sentinel; also gate init-Job dispatch on `BranchName == ""` to make a second init-Job structurally impossible. Directly observed failure.

2. **R-04: TTL-GC race on reporter Job causes budget double-count** — Replace reporter-Job-existence guard at `project_controller.go:1156-1175` with durable `Status.Budget.PlannerRolledUpUID` field. Import path must suppress rollup unconditionally for salvaged envelopes (prior run already counted the cost). Directly observed failure class.

3. **R-05: Partial-plan import corrupts global Execution DAG** — `depgraph.go:28` conservative design (unresolved ref = no edge) means missing Tasks silently produce zero indegree and dispatch immediately. Import atomicity must be per-Milestone; every `Task.Spec.DependsOn` entry must resolve before any CRs are created.

4. **R-06: v1alpha1 salvage envelopes carry v1alpha1 `ChildCRDSpec.Spec.Raw`** — `MaterializeChildCRDs` decodes into v1alpha2 structs; `Wave.Spec.ProjectRef` (Phase 23) is zero-valued from v1alpha1 bytes, producing orphan Waves that never dispatch. Import must run v1alpha1→v1alpha2 conversion on each `Spec.Raw`.

5. **R-12: Cycle detection bypassed for imported plan trees** — `client.Create` bypasses the admission webhook. Import must explicitly call `dag.ComputeWaves` on the full task set before creating any child CRDs. A cyclic salvage must produce `ImportFailed / CyclicPlanDetected` condition.

6. **R-02: UID-churn aliasing** — Import must validate `out.ChildCRDs[*].Name` against expected children of the current object at its level. Salvaged envelopes are untrusted foreign data; same threat model as T-308 `ChildCRDSpec` allowlist.

## Implications for Roadmap

The research strongly supports a three-phase milestone structure. Phase 1 is standalone correctness bugs — no design decisions required, all fixes are localized. Phase 2 is the import feature core, preceded by an explicit Approach A vs B decision checkpoint. Phase 3 is operator tooling gated on Phase 2 working end-to-end.

### Phase 1: Budget-Bypass Correctness

**Rationale:** All three bugs (R-01, R-04, R-07) are independently observable, localized to `project_controller.go`, and produce immediate user-visible failures (re-init loop, budget double-count, clone re-dispatch). They are prerequisites to import work — import on top of a broken bypass path compounds failures. These fixes have zero design ambiguity; no research checkpoint needed.

**Delivers:** A budget-halt/resume cycle that correctly resumes at `Running`, never re-initializes an already-cloned workspace, and does not double-count planning cost. Plus regression coverage for the `2a5e0dc` ordering fix.

**Addresses:** TS-R4, TS-R5, TS-R6, TS-R7, R-01, R-04, R-07

**Avoids:** R-01 re-init loop, R-04 double-count, R-07 re-clone, R-08 cap-raise re-halt

**Research flag:** None needed. All fixes are mechanical, code-located, with zero design ambiguity.

### Phase 2: Plan-Import Design Decision + Core Implementation

**Rationale:** The Approach A vs B decision is the foundation for all import work and must be resolved before any implementation begins. Both approaches are architecturally valid; the choice has downstream implications for the number of files changed, the operator workflow, and long-term maintenance of the envelope path contract. A plan-phase research step is needed to present both approaches with their tradeoffs, surface the salvage fixture constraint (only UID-keyed paths exist in the tgz), and checkpoint with the operator before any code is written.

Key data point for the decision: the `salvage-20260618/pvc-envelopes.tgz` contains only `envelopes/<oldUID>/` paths — no `envelopes-by-name/` paths were ever written (the original TIDE run predates Approach A). Approach A therefore requires a migration step that is functionally equivalent to what Approach B does explicitly. This narrows the practical gap between the two for the immediate dogfood use case.

**Delivers:** Working envelope-import mechanism that bridges UID-churn, validated against the `salvage-20260618` fixture. Envelopes adopted from salvage skip planner dispatch; cycle detection runs; v1alpha1 schema is converted; Wave CRs are re-derived fresh; budget rollup suppressed for imported envelopes.

**Addresses:** TS-R1, TS-R2, TS-R3, R-02, R-03, R-05, R-06, R-09, R-10, R-11, R-12, R-13

**Avoids:** AF-R1 (wave schedule caching), AF-R3 (skip cycle detection), Anti-Pattern 2 (importing Wave CRs), Anti-Pattern 3 (non-atomic UID rewrite)

**Research flag:** Needs `/gsd:plan-phase --research-phase` to present Approach A vs B tradeoffs and checkpoint. Do not pick a winner during roadmap.

### Phase 3: Operator Tooling and End-to-End Validation

**Rationale:** CLI commands are gated on Phase 2 completion — they wrap the validated import mechanism. The end-to-end kind integration test using `salvage-20260618` is the acceptance gate for the entire milestone.

**Delivers:** `tide import-envelopes` / `tide import seed` CLI verb for the dogfood operator workflow; dry-run mode; `tide export-envelopes` for portable salvage across cluster teardowns; kind integration test `IMPORT-E2E-01` asserting all Milestones reach `Succeeded` without planner re-dispatch.

**Addresses:** D-R1, D-R2, D-R4

**Avoids:** Partial-import footgun (dry-run surfaces rejections before write)

**Research flag:** None needed. CLI wrappers over validated core logic. Standard `cobra` subcommand patterns already used in TIDE CLI.

### Phase Ordering Rationale

- Phase 1 before Phase 2: bypass bugs are independent and must be fixed first; a broken bypass running alongside import code makes integration failures ambiguous.
- Explicit design checkpoint between Phase 2 plan-phase and implementation: Approach A vs B is a one-way door; resolve before writing code.
- Phase 3 after Phase 2: operator tooling wraps the core; writing CLI commands before the core works produces rework.
- Wave CRs are never in scope for Phase 2/3 import — always re-derived. This must be stated explicitly in each plan that touches the import path.
- v1alpha1→v1alpha2 schema conversion belongs in Phase 2 alongside the import materializer, not deferred to Phase 3 — it is a correctness gate.

### Research Flags

Phases needing deeper research during planning:
- **Phase 2 (Plan-Import):** Approach A vs B design decision must be resolved before implementation plans are written. The research files provide full analysis for both; the plan-phase should surface the salvage fixture constraint (only UID-keyed paths exist in the tgz) and checkpoint with the operator.

Phases with standard patterns (skip research-phase):
- **Phase 1 (Bypass Correctness):** All fixes are code-located, fully diagnosed. No new patterns needed.
- **Phase 3 (Operator Tooling):** CLI wrappers over validated core. Standard cobra subcommand pattern already used in TIDE CLI.

## Confidence Assessment

| Area | Confidence | Notes |
|------|------------|-------|
| Stack | HIGH | Zero new dependencies; all findings derived from live codebase + go.mod. No inference required. |
| Features | HIGH | P1 correctness bugs are observed failures with code-located root causes. Import features have solid prior art from Argo/Temporal/Prefect/Dagster/Bazel. |
| Architecture | HIGH | Both approaches derived from direct code reading of all five integration sites + the salvage fixture. Tradeoffs clearly documented. |
| Pitfalls | HIGH | R-01 and R-04 are directly observed; R-02/R-03/R-05/R-06/R-12 derived from code inspection; all have code-location citations. |

**Overall confidence:** HIGH

### Gaps to Address

- **Approach A vs B decision:** This is the only true gap. Both approaches are fully documented; the gap is an operator decision, not a research gap. Must be resolved at plan-phase before implementation begins.
- **`tide export-envelopes` PVC access mechanism:** kubectl-cp wrapper vs. controller-spawned Job — operational preference decision for Phase 3 plan-phase.
- **Envelope schema version constant:** If v1.0.3 adds any field to `EnvelopeOut`, `APIVersionV1Alpha1` must be bumped or new fields must be `omitempty`. Flag for Phase 2 schema design review — not a research gap.

## Sources

### Primary (HIGH confidence — live codebase)

- `internal/controller/project_controller.go` (lines 339, 969-1102, 1156-1182, 1257, 1300-1321) — bypass-clear bug, rollup guard, dispatch sites
- `internal/dispatch/podjob/backend.go` (lines 92-141) — `FilesystemEnvelopeReader.ReadOut`, UID-keyed path contract
- `pkg/dispatch/envelope.go` (lines 21-24, 400-409) — `EnvelopeOut`, `ValidateAPIVersionKind`, `APIVersionV1Alpha1` constant
- `internal/controller/depgraph.go` (lines 17-30) — conservative empty-return on unresolved scope (R-05 root)
- `internal/controller/dispatch_helpers.go` (lines 17-36) — `MaterializeChildCRDs`, T-308 Kind allowlist
- `internal/controller/milestone_controller.go` (lines 239-493, 507-717) — canonical dispatch site + idempotency guard at line 304
- `api/v1alpha2/shared_types.go` (lines 216, 253) — annotation constant pattern for `AnnotationBillingResumedAt`
- `examples/projects/dogfood/salvage-20260618/pvc-envelopes/` — 59 envelopes, UID-keyed paths only, `in.json` carries `dispatch.parentName`
- `go.mod` — zero new `require` entries needed

### Primary (HIGH confidence — official docs)

- Argo Workflows memoization docs + issues #12936 and #2320 — skip-without-DAG-ordering bug, retry vs resubmit distinction
- Temporal workflow execution docs — event-history replay, durable execution model, budget-halt signal pattern
- Prefect task caching docs — INPUTS cache policy, result persistence requirement
- Dagster asset versioning docs — code_version + data_version staleness model
- Bazel remote caching docs + ACM Queue article — action key construction, CAS + AC separation

### Secondary (MEDIUM confidence — community consensus)

- Augment Code async AI workflows guide — per-step snapshots, structured handoff files
- Addy Osmani long-running agents blog — budget circuit breakers, artifact-based session continuity
- Zylos AI checkpointing research — user expectation of "no double billing" on resume
- Astronomer Airflow rerunning DAGs guide — "skip succeeded, re-run failed" reprocessing modes
- Opsmeter budget exceeded response playbook — soft limits with human-in-the-loop escalation

---
*Research completed: 2026-06-18*
*Ready for roadmap: yes*
