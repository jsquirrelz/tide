---
phase: 43-task-level-parity-trace-context-propagation
plan: 01
subsystem: api
tags: [opentelemetry, otelai, crd, kubebuilder, controller-gen, helm]

# Dependency graph
requires:
  - phase: 42-trace-context-foundation-planner-level-span-emission
    provides: pkg/otelai/tracecontext.go primitives (TraceIDFromUID, FormatTraceparent, ExtractRemoteParent) and the four planner-level SpanEmittedUID idempotency markers this plan's fields sit alongside
provides:
  - "MilestoneTraceSpanID / PhaseTraceSpanID / PlanTraceSpanID / ProjectTraceSpanID / TaskTraceSpanID — flat status fields on all five CRDs carrying each level's own synthesized OTel SpanID hex"
  - "TaskSpanEmittedUID — Task's net-new idempotency marker, mirroring the four existing planner-level {Level}SpanEmittedUID fields"
  - "Regenerated config/crd/bases/*.yaml and charts/tide-crds/templates/*.yaml exposing all six new JSON properties"
affects: [43-02, 46-dashboard-deep-link]

# Tech tracking
tech-stack:
  added: []
  patterns: ["flat {Level}TraceSpanID string field on *Status structs, house-style-matched to the existing {Level}SpanEmittedUID markers rather than a nested Status.Trace struct"]

key-files:
  created: []
  modified:
    - api/v1alpha3/milestone_types.go
    - api/v1alpha3/phase_types.go
    - api/v1alpha3/plan_types.go
    - api/v1alpha3/project_types.go
    - api/v1alpha3/task_types.go
    - config/crd/bases/tideproject.k8s_milestones.yaml
    - config/crd/bases/tideproject.k8s_phases.yaml
    - config/crd/bases/tideproject.k8s_plans.yaml
    - config/crd/bases/tideproject.k8s_projects.yaml
    - config/crd/bases/tideproject.k8s_tasks.yaml
    - charts/tide-crds/templates/milestone-crd.yaml
    - charts/tide-crds/templates/phase-crd.yaml
    - charts/tide-crds/templates/plan-crd.yaml
    - charts/tide-crds/templates/project-crd.yaml
    - charts/tide-crds/templates/task-crd.yaml

key-decisions:
  - "Flat {Level}TraceSpanID string chosen over a nested Status.Trace{SpanID string} struct — CONTEXT.md's explicit Claude's-Discretion grant, resolved for house-style consistency with the existing {Level}SpanEmittedUID flat fields. PROP-02's literal '.status.trace' wording is satisfied by intent (durable, small, re-readable carrier), not by literal nesting."
  - "Fields store ONLY the SpanID hex (trace.SpanID.String(), 16 hex chars), never the TraceID — always re-derivable from Project.UID via otelai.TraceIDFromUID, avoiding redundant storage."
  - "TaskSpanEmittedUID kept structurally separate from TaskTraceSpanID (D-04): one answers 'did I already emit for this Job UID', the other 'what is my span's identity' — no folding."

patterns-established:
  - "make helm must run alongside make manifests generate whenever config/crd/bases/*.yaml changes — the chart-reproducibility pre-commit hook enforces charts/tide-crds/templates/*.yaml stays byte-identical to a fresh helmify regen."

requirements-completed: [PROP-02]

# Metrics
duration: 25min
completed: 2026-07-16
---

# Phase 43 Plan 01: Durable Per-Level TraceSpanID Carrier Fields Summary

**Six new additive CRD status fields (five `{Level}TraceSpanID` carriers + Task's `TaskSpanEmittedUID` idempotency marker) landed across all five CRD Go types and regenerated manifests/chart, giving every downstream Phase 43 plan a real field to read parent span IDs from and write its own into.**

## Performance

- **Duration:** ~25 min
- **Started:** 2026-07-16T16:00:00Z (approx, after HEAD assertion + context read)
- **Completed:** 2026-07-16T16:24:34Z
- **Tasks:** 2 completed
- **Files modified:** 15 (5 `*_types.go` + 5 `config/crd/bases/*.yaml` + 5 `charts/tide-crds/templates/*.yaml`)

## Accomplishments
- Added `MilestoneTraceSpanID`, `PhaseTraceSpanID`, `PlanTraceSpanID`, `ProjectTraceSpanID` to the four existing planner-level `*Status` structs, each placed immediately after that struct's `{Level}SpanEmittedUID` sibling with a matching doc-comment style and `+optional` marker.
- Added Task's two net-new fields (`TaskSpanEmittedUID`, `TaskTraceSpanID`) to `TaskStatus`, which previously had neither field family.
- Regenerated `config/crd/bases/*.yaml` via `make manifests generate` and `charts/tide-crds/templates/*.yaml` via `make helm`, exposing all six new JSON properties (`milestoneTraceSpanID`, `phaseTraceSpanID`, `planTraceSpanID`, `projectTraceSpanID`, `taskSpanEmittedUID`, `taskTraceSpanID`) in both the raw CRD manifests and the Helm-templated subchart.
- Verified regen idempotency (`make manifests generate` produces zero diff against the committed tree) and confirmed the diff is purely additive across all touched files (0 removed lines).

## Task Commits

Each task was committed atomically:

1. **Task 1: Add span-ID carrier fields to all five CRD Status structs** - `6b4fc23` (feat)
2. **Task 2: Regenerate CRD manifests and deepcopy, verify schema exposure** - `e616105` (feat)

**Plan metadata:** SUMMARY.md commit follows this file (docs: complete plan)

## Files Created/Modified
- `api/v1alpha3/milestone_types.go` - `MilestoneTraceSpanID string` added to `MilestoneStatus`
- `api/v1alpha3/phase_types.go` - `PhaseTraceSpanID string` added to `PhaseStatus`
- `api/v1alpha3/plan_types.go` - `PlanTraceSpanID string` added to `PlanStatus`
- `api/v1alpha3/project_types.go` - `ProjectTraceSpanID string` added to `ProjectStatus`, directly (not Budget-nested), mirroring `PlannerSpanEmittedUID`'s placement
- `api/v1alpha3/task_types.go` - both `TaskSpanEmittedUID string` and `TaskTraceSpanID string` added to `TaskStatus` (net-new field family)
- `config/crd/bases/tideproject.k8s_{milestones,phases,plans,projects,tasks}.yaml` - regenerated via `make manifests generate`, exposing the new properties under each CRD's status schema
- `charts/tide-crds/templates/{milestone,phase,plan,project,task}-crd.yaml` - regenerated via `make helm` to keep the helmify-derived subchart byte-identical to a fresh regen (chart-reproducibility pre-commit gate)

## Decisions Made

**Flat field shape (per CONTEXT.md's explicit discretion grant):** Chose `{Level}TraceSpanID string` matching the existing `{Level}SpanEmittedUID` house style, over ARCHITECTURE.md's suggested nested `Status.Trace *TraceStatus{SpanID string}`. **Note for the verifier:** PROP-02's literal `.status.trace` wording is satisfied by intent (a durable, small, re-readable per-level carrier), not by literal path nesting — this is the CONTEXT.md-sanctioned resolution, not a deviation.

**TraceID never stored:** Every new field stores only the SpanID hex; the shared TraceID is always re-derived from `Project.UID` via `otelai.TraceIDFromUID`, avoiding a second copy of the same value on every CRD.

**TaskSpanEmittedUID kept separate from TaskTraceSpanID (D-04):** copied the exact idempotency-marker doc-comment convention from the four existing controllers rather than reusing/folding into the new span-ID field — they answer different questions (already-emitted-for-this-Job-UID vs. span identity).

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] `make helm` regen required alongside `make manifests generate`**
- **Found during:** Task 2 (Regenerate CRD manifests)
- **Issue:** The plan's Task 2 action only specified `make manifests generate` + committing `config/crd/bases/*.yaml`. The repo's `chart-reproducibility` pre-commit hook (`verify-chart-reproducible` Make target) failed the first commit attempt with exit code 2, reporting `charts/tide-crds/templates/{milestone,phase,plan,project,task}-crd.yaml` drifted from a fresh `make helm` regeneration — the helmify-derived CRD subchart wasn't in sync with the newly regenerated `config/crd/bases/`.
- **Fix:** Ran `make helm` (the target the hook's own failure message names), which re-synced `charts/tide-crds/templates/*.yaml` via helmify. Diffed clean (0 removed lines, additive-only, matching the six new JSON properties). Staged alongside the CRD base YAMLs in the same Task 2 commit.
- **Files modified:** `charts/tide-crds/templates/{milestone,phase,plan,project,task}-crd.yaml`
- **Verification:** Re-ran the commit; `chart reproducibility (make helm + diff)` hook reported `Passed`. Re-ran `make manifests generate && git diff --exit-code config/crd/bases/` post-commit — `REGEN-CLEAN`, confirming idempotency.
- **Committed in:** `e616105` (part of Task 2 commit)

---

**Total deviations:** 1 auto-fixed (1 blocking — pre-commit hook enforcement)
**Impact on plan:** Necessary for the commit to land at all; purely mechanical regen-sync, no scope creep, no hand-editing of generated files.

## Issues Encountered

`go build ./...` fails on `cmd/tide-demo-init/main.go:112` (`pattern all:fixture: no matching files found` — a `//go:embed all:fixture` directive whose `fixture/` directory is absent in this worktree checkout). Verified this is pre-existing and unrelated to this plan: reproduces identically at the base commit `b8729ceefa17454bef3beb514a7812894b1b3603` (before any Task 1/2 changes). `go build ./api/... ./internal/... ./cmd/...` (excluding `tide-demo-init`) and `go vet ./api/...` both pass clean. Not a ship-blocker for this plan's scope.

## User Setup Required

None - no external service configuration required.

## Next Phase Readiness

- All five CRDs now carry the durable per-level span-ID field and Task carries its own idempotency marker — plan 43-02 (or whichever plan wires `synthesizePlannerSpan`'s parenting retrofit and Task's net-new span-emission block) can compile parent-read and self-write code against these exact field names (`MilestoneTraceSpanID`, `PhaseTraceSpanID`, `PlanTraceSpanID`, `ProjectTraceSpanID`, `TaskSpanEmittedUID`, `TaskTraceSpanID`) with no further schema changes needed.
- No blockers. The `TraceParent`/`traceparent` env/Args injection work (PROP-01, `internal/dispatch/podjob/jobspec.go`, `internal/controller/reporter_jobspec.go`, `cmd/tide-reporter/main.go`) and the `synthesizePlannerSpan` parenting retrofit (TRACE-02) described in 43-CONTEXT.md/43-PATTERNS.md are out of this plan's scope (43-01 was fields-only) and remain for the sibling/next plan.

---
*Phase: 43-task-level-parity-trace-context-propagation*
*Completed: 2026-07-16*
