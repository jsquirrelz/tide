# Phase 41: Refactoring Review — Non-Breaking Cleanup - Context

**Gathered:** 2026-07-11 (auto mode — decisions auto-selected from the seed's own recommendations; log in 41-DISCUSSION-LOG.md)
**Status:** Ready for planning

<domain>
## Phase Boundary

The 12-item operator-shared refactoring review (seed: `.planning/todos/pending/2026-07-09-phase-41-refactoring-review.md`) lands as non-breaking cleanup: quick wins (typed Status.Phase constants, meta.IsStatusConditionTrue, dead code/fields, mojibake, test-helper unification) then structural extractions (shared dispatch-holds gate chain, PlannerDeps carrier, condition-polarity normalization, status-helper extraction, magic-literal centralization, log-style decision). Item 3 (stale scheme comment + duplicate AddToScheme) was verified already fixed by Phase 40's crank — **11 live items**. No CRD schema changes, no API version changes, no new capabilities.

</domain>

<decisions>
## Implementation Decisions

### Seed staleness (Phase 40 landed after the seed was written)
- **D-01: Every seed file:line is a hint, not an anchor.** The seed was verified against pre-Phase-40 source; Phase 40 renamed `api/v1alpha2`→`api/v1alpha3`, moved webhooks, and swept comments. The researcher MUST re-verify each item against current HEAD (re-locate by symbol/grep) before planning. Orchestrator spot-verification 2026-07-11: item 3 DONE (cmd/manager/main.go:308-309 now registers clientgo + v1alpha3 once each — drop from scope); item 5 mojibake STILL PRESENT (13 lines dispatch_helpers.go, 9 lines subagent.go); item 1 still live (Project constants now at api/v1alpha3/project_types.go:464+, other kinds still raw literals); item 4 dead code still present (gateDispatch/ensureJob now at task_controller.go:1434/1452).
- **D-02: All seed references to `api/v1alpha2` now mean `api/v1alpha3`.** The seed predates the crank.

### Item 1 — typed Status.Phase constants
- **D-03: Constants live in `api/v1alpha3` per-kind types files, following the existing Project pattern** (`project_types.go:464+`). Field type stays `string` — NO `+kubebuilder:validation:Enum` this phase (that is a CRD schema change; this phase is strictly non-breaking). Enum validation is a future-phase candidate.

### Item 9 — ConditionParentUnresolved polarity
- **D-04: `True == parent unresolved`** (matches the type name and the Task controller's existing usage). Fix `surfaceParentRefUnresolved` in milestone/phase controllers; clear to `False/ParentResolved` once the parent appears. Sweep every consumer (`rg ConditionParentUnresolved` incl. cmd/dashboard) and update tests in the same commit — this is an observable status-semantics change, document it in the item's commit body.

### Item 12 — log-style policy
- **D-05: Amend AGENTS.md, do not churn the code.** All 47 `logger.Info/Error` sites are internally-consistent lowercase-initial; log text is load-bearing (phase_gates_test.go greps, CLAUDE.md runtime-gate verification protocols grep exact strings like `"creating job"` / `"dispatch held"`). Codify lowercase-initial in AGENTS.md's logging section as a quick win; zero log-message edits.

### Phase-40 code-review findings (40-REVIEW.md: 0 Critical / 6 Warning / 10 Info)
- **D-06: No silent scope expansion.** The 6 WR findings route via `/gsd:code-review 40 --fix` or an explicit user fold decision — they are NOT Phase 41 items. Exception: where a seed item's files literally overlap a REVIEW finding (item 5 mojibake ≡ REVIEW Info comment-drift in dispatch_helpers.go), the executor naturally resolves both in one edit.

### Sequencing & plan shape
- **D-07: Adopt the seed's sequencing with item 3 removed:** quick wins 2 → 5 → 6 → 4 → 1 (each independently shippable), then structural 7 → 8 → 10, with 9 + 11 as focused correctness fixes and 12 landing as the AGENTS.md-amendment quick win. Item 7 (dispatch-holds gate chain) migrates ONE controller per plan/commit — gate ordering is semantically load-bearing (slot leaks, park-before-acquire); preserve exact order + requeue values (5s/30s).
- **D-08: Requirements minted at plan time** (REFAC-xx pattern, mirroring Phase 40's CRANK-xx), one per item or per coherent item group.
- **D-09: Honor the seed's "Do NOT refactor yet" list** — except its `api/v1alpha1` entry, which Phase 40 resolved (packages deleted). Generated files, the `//nolint:gocyclo` flat state machines, `ctrl.Result{Requeue: true}` sites, webhook-test placement, and `charts/tide/values.yaml` all stay untouched.

### Claude's Discretion
- Exact plan/wave grouping of the 11 items (respecting D-07 sequencing).
- Whether item 6's generic `reconcileN` uses an interface param or generics — either is fine, match the test suite's existing idiom.
- Item 11's constant placement details (label keys → `internal/owner` next to `LabelProject` per the seed; PVC name plumbed from the reconciler field).

### Folded Todos
- `.planning/todos/pending/2026-07-09-phase-41-refactoring-review.md` — the phase seed itself (12 items, file:line-verified 2026-07-09). Folded as the phase's defining scope; `resolves_phase: 41` added so phase close auto-closes it.

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### The seed (defines all 11 items)
- `.planning/todos/pending/2026-07-09-phase-41-refactoring-review.md` — the full 12-item review with per-item Files/Problem/Shape/Risk/Verify; item 3 already done by Phase 40; §"Do NOT refactor yet" and §"Suggested sequencing" are binding (D-07/D-09)

### Phase 40 outputs this phase builds on
- `.planning/phases/40-deprecate-v1alpha1-api/40-CONTEXT.md` — the crank's locked decisions (D-04 guard generalization, D-05 owner-ref single-accept, D-08 envelope group) — do not undo any of them
- `.planning/phases/40-deprecate-v1alpha1-api/40-REVIEW.md` — 6 WR + 10 Info advisory findings; companion work per D-06, not phase scope

### Conventions the items must respect
- `AGENTS.md` — logging-section amendment target for item 12 (D-05); also the generated-files exclusion list the seed §Scope honors
- `CLAUDE.md` §"Verify Before Claiming" — the exact-string log greps that make D-05 choose doc-amendment over code churn

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- `api/v1alpha3/project_types.go:464+` — the Phase-constant pattern item 1 extends to the other five kinds
- `internal/controller/task_controller.go:90-122` `TaskReconcilerDeps` — the deps-carrier pattern item 8 extends to the four planner reconcilers
- `internal/controller/dispatch_helpers.go` — the established home for shared dispatch logic (items 7, 8); note it gained `levelOverrideKey` in Phase 40
- `internal/owner` (`LabelProject`) — the home for label-key constants (item 11)
- `cmd/manager/wiring_test.go` + `wave_dispatcher_wiring_test.go` — existing wiring locks to extend for item 8

### Established Patterns
- Nil-safe halt wrappers (`checkBillingHalt` etc.) — keep the wrappers, replace loop bodies with `meta.IsStatusConditionTrue` (item 2)
- Boundary-push/spawnReporter-style leaf helpers — the extraction shape for item 10 (leaf status-mutation primitives, NOT a generic level-reconciler)
- The single shared envtest BeforeSuite (TEST-01 budget) — why webhook tests stay in `internal/controller/` (seed do-not list)

### Integration Points
- Items 7/8/10 touch all four planner controllers + task controller — the same files Phase 40 just swept; expect clean v1alpha3 imports, re-locate all line numbers
- Item 11's PVC-name plumb connects the `--workspaces-pvc-name` flag → reconciler fields → Job builders (a latent config bug, not just style)

</code_context>

<specifics>
## Specific Ideas

- The seed's per-item **Verify** lines are the acceptance criteria — carry them into plans verbatim (updated for post-40 paths).
- Item 7's helper signature sketch from the seed: `checkDispatchHolds(ctx, c, project, level, objName) (held bool, result ctrl.Result)` covering project-scoped holds only; level-specific holds stay at call sites.
- Item 12's caution list: `rg -l 'dispatch held|creating job' internal test .planning` — the greps that must never break.

</specifics>

<deferred>
## Deferred Ideas

- `+kubebuilder:validation:Enum` on Status.Phase fields — CRD schema change; rides a future version crank (v1alpha4).
- The 6 × 40-REVIEW.md WR findings — `/gsd:code-review 40 --fix` or explicit user fold (D-06).
- `/gsd:secure-phase 40` — security enforcement gate flagged at Phase 40 close; still outstanding.
- controller-runtime bump folding the 14 deprecated `ctrl.Result{Requeue: true}` sites (seed do-not list).

### Reviewed Todos (not folded)
- `2026-07-03-dashboard-log-stream-drawer-empty.md` (score 0.9) — delivered by Phase 37 (log-drawer states); needs close-out audit, not re-execution
- `2026-07-03-dashboard-planning-dag-artifact-view.md` (score 0.9) — delivered by Phase 37 (artifact view); needs close-out audit
- `2026-07-03-git-baseref-run-branch.md` (score 0.6) — delivered by Phase 35 (baseRef); needs close-out audit
- `2026-07-03-signed-commits-verified-badge.md` (score 0.6) — GPG portion descoped 2026-07-03; bot-identity portion delivered by Phase 36; needs close-out audit
- `2026-07-03-wave-parallel-integration-miss.md` (score 0.6) — delivered by Phase 34 (integration-miss gate); needs close-out audit
- `2026-07-09-phase-40-v1alpha-removal-semantic-rename.md` (score 0.6) — superseded by the imported Phase 40 plan, which is now executed and verified; moved to completed
- `cache-f1-direct-sdk-cross-pod-caching.md` (score 0.6) — deferred milestone-level work (CACHE-F1), unrelated to refactoring
- `2026-07-03-project-level-subagent-override-slot.md` (score 0.2) — delivered by Phase 40 (D-02 levels rename); needs close-out audit

The five "delivered by 34–38/40" todos above predate those phases' execution and were never closed — recommend `/gsd:audit-uat` or a manual close-out sweep rather than folding into any phase.

</deferred>

---

*Phase: 41-refactoring-review-non-breaking-cleanup-12-items*
*Context gathered: 2026-07-11*
