---
phase: 42-trace-context-foundation-planner-level-span-emission
plan: 03
subsystem: api
tags: [controller-runtime, crd, otel, kubebuilder, controller-gen, helm]

# Dependency graph
requires:
  - phase: 42 (plans 01/02, parallel wave 1)
    provides: pkg/otelai trace-context helpers and attribute conventions (independent surface, not consumed here)
provides:
  - Four durable, envReadOK-independent span-emission idempotency marker fields, one per planner level (Milestone/Phase/Plan/Project), on the v1alpha3 CRD status types
  - Regenerated config/crd/bases/*.yaml and charts/tide-crds/templates/*-crd.yaml exposing the new status properties
affects: [42-04, 42-05]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Dedicated *SpanEmittedUID marker per level, gated independent of the existing envReadOK-gated *RolledUpUID markers — prevents re-emitting degraded spans on every reconcile (Pitfall 2)"

key-files:
  created: []
  modified:
    - api/v1alpha3/milestone_types.go
    - api/v1alpha3/phase_types.go
    - api/v1alpha3/plan_types.go
    - api/v1alpha3/project_types.go
    - config/crd/bases/tideproject.k8s_milestones.yaml
    - config/crd/bases/tideproject.k8s_phases.yaml
    - config/crd/bases/tideproject.k8s_plans.yaml
    - config/crd/bases/tideproject.k8s_projects.yaml
    - charts/tide-crds/templates/milestone-crd.yaml
    - charts/tide-crds/templates/phase-crd.yaml
    - charts/tide-crds/templates/plan-crd.yaml
    - charts/tide-crds/templates/project-crd.yaml

key-decisions:
  - "PlannerSpanEmittedUID lives directly on ProjectStatus, NOT nested under BudgetStatus like PlannerRolledUpUID — span emission is telemetry bookkeeping, not cost bookkeeping (research Open Question 2, resolved in this plan)"
  - "New markers are plain optional string scalars, structurally independent of the envReadOK-gated *RolledUpUID markers — envReadOK-independence is a field-level guarantee, not a convention future code could violate"

patterns-established:
  - "Pattern 2 (from 42-RESEARCH.md): dedicated durable status marker per planner level for exactly-once side-effect gating, mirrored exactly from the existing *RolledUpUID budget-rollup marker shape"

requirements-completed: [ATTR-01, ATTR-02]

# Metrics
duration: ~20min
completed: 2026-07-15
---

# Phase 42 Plan 03: Planner-Level Span-Emission Idempotency Markers Summary

**Four additive `*SpanEmittedUID` scalar fields (Milestone/Phase/Plan/Project) added to v1alpha3 CRD status types and regenerated across manifests + Helm chart, giving ATTR-01/02 span emission a durable, envReadOK-independent exactly-once marker at every planner level.**

## Performance

- **Duration:** ~20 min
- **Completed:** 2026-07-15T16:48:55-04:00
- **Tasks:** 1
- **Files modified:** 12 (4 Go types files, 4 generated CRD YAMLs, 4 generated Helm chart CRD templates) + 1 new deferred-items.md

## Accomplishments
- Added `MilestoneSpanEmittedUID`, `PhaseSpanEmittedUID`, `PlanSpanEmittedUID`, `PlannerSpanEmittedUID` — one durable optional string marker per planner level, each placed immediately after its level's `*RolledUpUID` sibling (Project's directly on `ProjectStatus`, not Budget-nested)
- Regenerated `config/crd/bases/*.yaml` via `make manifests` (idempotent — verified a second run produces no further diff) and `make generate` (deepcopy unchanged, as expected for plain string scalars)
- The repo's `chart-reproducibility` pre-commit hook regenerated the matching `charts/tide-crds/templates/*-crd.yaml` Helm mirrors automatically; staged and committed together with the CRD source changes so the commit is chart-reproducible
- Verified zero `Trace`/`TraceStatus`/`SpanID` fields were introduced anywhere — Phase 43's `.status.trace` surface (PROP-02) stays untouched, per the plan's explicit boundary

## Task Commits

Each task was committed atomically:

1. **Task 1: Add four span-emission marker fields and regenerate manifests** - `9bd8520` (feat)

**Plan metadata:** (this SUMMARY commit, made by the orchestrator's shared-file merge — worktree mode does not commit STATE.md/ROADMAP.md)

## Files Created/Modified
- `api/v1alpha3/milestone_types.go` - `MilestoneSpanEmittedUID` field on `MilestoneStatus`
- `api/v1alpha3/phase_types.go` - `PhaseSpanEmittedUID` field on `PhaseStatus`
- `api/v1alpha3/plan_types.go` - `PlanSpanEmittedUID` field on `PlanStatus`
- `api/v1alpha3/project_types.go` - `PlannerSpanEmittedUID` field directly on `ProjectStatus` (after `BoundaryPush`, not inside `BudgetStatus`)
- `config/crd/bases/tideproject.k8s_{milestones,phases,plans,projects}.yaml` - controller-gen-regenerated status schema additions
- `charts/tide-crds/templates/{milestone,phase,plan,project}-crd.yaml` - helmify-regenerated mirrors of the above (chart-reproducibility hook)
- `.planning/phases/42-trace-context-foundation-planner-level-span-emission/deferred-items.md` (new) - out-of-scope pre-existing build gap log

## Decisions Made
- **Project marker placement:** `PlannerSpanEmittedUID` sits directly on `ProjectStatus`, deliberately breaking from `PlannerRolledUpUID`'s `BudgetStatus`-nested placement — per the plan's objective, budget rollup is cost bookkeeping and span emission is telemetry bookkeeping with no budget relationship, so all four markers are now structurally consistent (direct-on-Status) even though the pre-existing rollup marker isn't.
- **Doc-comment convention:** each field's comment states the gating mechanism (one-span-per-Job-attempt, envReadOK-independent), names the specific sibling marker it deliberately does NOT reuse, cites Pitfall 2, and cites "Phase 42 D-02/D-04" provenance — mirroring the house style set by the `*RolledUpUID` fields exactly.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] Staged chart-reproducibility hook's regenerated Helm CRD templates**
- **Found during:** Task 1 (commit attempt)
- **Issue:** The repo's pre-commit hook (`chart reproducibility (make helm + diff)`) runs `make helm` and diffs `charts/` against a fresh regeneration; it failed the first commit attempt because the new CRD fields hadn't yet propagated to `charts/tide-crds/templates/*-crd.yaml` (helmify mirrors of `config/crd/bases/*.yaml`). The hook itself regenerated those four chart files as a side effect of running the check.
- **Fix:** Staged the hook-regenerated `charts/tide-crds/templates/{milestone,phase,plan,project}-crd.yaml` files alongside the CRD source changes and re-committed. No manual edits — these are 100% generated output, consistent with the plan's "never hand-edit the CRD YAML" instruction extended to its Helm mirror.
- **Files modified:** `charts/tide-crds/templates/milestone-crd.yaml`, `charts/tide-crds/templates/phase-crd.yaml`, `charts/tide-crds/templates/plan-crd.yaml`, `charts/tide-crds/templates/project-crd.yaml`
- **Verification:** Second commit attempt passed the `chart reproducibility` hook cleanly.
- **Committed in:** `9bd8520` (Task 1 commit)

---

**Total deviations:** 1 auto-fixed (1 blocking — pre-commit hook chain requirement not explicit in the plan's `files_modified` list)
**Impact on plan:** Necessary for the commit to land at all under this repo's chart-reproducibility gate; purely generated output, zero hand edits, zero scope creep beyond what the plan's own manifest-regeneration mandate implies.

## Issues Encountered

`go build ./...` (as literally specified in the plan's `<verify><automated>` command) fails with `cmd/tide-demo-init/main.go:112:12: pattern all:fixture: no matching files found`. Root-caused as pre-existing and out of scope: `cmd/tide-demo-init/fixture/` is a gitignored directory materialized at build time via `make demo-fixture` (`go generate ./cmd/tide-demo-init/...`, per `.gitignore:52-56` and `Makefile:77-78`); this worktree never ran that target, and `cmd/tide-demo-init` is not in this plan's `files_modified` scope. Verified instead: `go build ./api/... ./internal/controller/...` (the packages consuming the new fields) exits 0; `go build $(go list -e ./... | grep -v /cmd/tide-demo-init)` also exits 0 across the rest of the repo (remaining `go list -e` misses are test-only packages excluded by build constraints, expected). Logged to `.planning/phases/42-trace-context-foundation-planner-level-span-emission/deferred-items.md` per the executor's scope-boundary rule rather than fixed. `gofmt -l` and `go vet ./api/... ./internal/controller/...` both clean.

## User Setup Required

None - no external service configuration required.

## Next Phase Readiness

The exactly-once storage mechanism for ATTR-01/02 span emission now exists at all four planner levels (`MilestoneSpanEmittedUID`, `PhaseSpanEmittedUID`, `PlanSpanEmittedUID`, `PlannerSpanEmittedUID`), unblocking plans 42-04/42-05's completion-handler wiring that will gate span synthesis on these markers. `envReadOK`-independence is structural (a dedicated field per level, not a shared/conventionally-gated one). Zero Phase-43 scope pulled forward — no `Trace`/`TraceStatus`/`SpanID` field exists anywhere in `api/v1alpha3/`.

Known gap for a future GSD cycle (not this plan's scope): this worktree needs `make demo-fixture` run before `go build ./...` (bare) will succeed cleanly — see deferred-items.md.

---
*Phase: 42-trace-context-foundation-planner-level-span-emission*
*Completed: 2026-07-15*

## Self-Check: PASSED

All 12 created/modified files verified present on disk (4 Go types files, 4 generated CRD YAMLs, 4 generated Helm chart CRD templates, deferred-items.md). Both commit hashes (`9bd8520`, `141a560`) verified present in `git log --oneline --all`.
