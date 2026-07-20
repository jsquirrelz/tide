---
phase: 52-per-level-looppolicy-parameterization
plan: 03
subsystem: subagent-prompts
tags: [go-templates, text-template, prompt-engineering, verifier, loop-contract]

# Dependency graph
requires:
  - phase: 51-the-task-loop
    provides: task_verifier.tmpl (the structural skeleton mirrored section-for-section), VerifyContext, ClassifyVerdict, LoadPromptTemplate(role, level) convention
provides:
  - plan_verifier.tmpl — goal-backward rubric prompt (4 named ESC-01 dimensions) for the plan-check loop
  - phase_verifier.tmpl / milestone_verifier.tmpl / project_verifier.tmpl — observable-outcome level-verify prompts for maxIterations:0 levels
  - plan_planner.tmpl D-04 re-plan findings block ({{if .RepairFindings}})
  - pkg/dispatch.RepairFinding + EnvelopeIn.RepairFindings (the Go-side render-data field the findings block needs to stay production-safe)
  - PromptTemplateVersion bumped v4 -> v5
affects: [52-07, 52-08, 52-09]  # plan-check loop, level-verify hook, gate-policy resolver — the plans that dispatch these new templates and populate PlanGoal/Children/LevelGoal at runtime

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Per-level verifier templates land behind the existing LoadPromptTemplate(role, level) filename convention with zero loader code changes — <level>_verifier.tmpl"
    - "New template fields referencing data not yet on EnvelopeIn (PlanGoal/Children/LevelGoal) are exercised in tests via test-only structs that embed EnvelopeIn, since no production dispatch site exists yet for these four templates"
    - "A template field added to an EXISTING template with an EXISTING production caller (plan_planner.tmpl) must exist as a real Go field on EnvelopeIn (even if always zero-valued today) — Go's text/template hard-errors on missing struct fields, so a bare-EnvelopeIn caller would crash at render time otherwise"
    - "Trim-marker discipline ({{- if .Field}}...{{end}}) around a new conditional block preserves byte-identical output when the field is empty — verified against the frozen internal/eval golden file + byte ratchet, not just reasoned about"

key-files:
  created:
    - internal/subagent/common/templates/plan_verifier.tmpl
    - internal/subagent/common/templates/phase_verifier.tmpl
    - internal/subagent/common/templates/milestone_verifier.tmpl
    - internal/subagent/common/templates/project_verifier.tmpl
  modified:
    - internal/subagent/common/templates/plan_planner.tmpl
    - internal/subagent/common/prompt_templates.go
    - internal/subagent/common/prompt_templates_test.go
    - pkg/dispatch/envelope.go
    - internal/eval/render_test.go

key-decisions:
  - "Added pkg/dispatch.RepairFinding + EnvelopeIn.RepairFindings (Rule 1/3 auto-fix, not in the plan's files_modified) because plan_planner.tmpl already has a real, unconditional production caller (internal/subagent/anthropic/subagent.go Run()) that executes the template against a bare EnvelopeIn value for every planner dispatch; referencing an EnvelopeIn field that doesn't exist would hard-error every real plan-level planner dispatch, and internal/eval/render_test.go's TestGoldenRender_PlanPlanner/TestByteRatchet_PlanPlanner already exercise this exact path under go test ./..."
  - "The four new *_verifier.tmpl templates (plan/phase/milestone/project) intentionally do NOT get PlanGoal/Children/LevelGoal fields added to EnvelopeIn — they have zero existing production callers (grep-confirmed), so per the plan's explicit instruction ('Plans 52-07/52-08/52-09 supply these exact structs'), tests exercise them with test-only structs embedding EnvelopeIn instead"
  - "Narrowed internal/eval's TestNoMapInterpolation guard to exclude {{range .RepairFindings}} specifically (a []RepairFinding slice range, not a map range) rather than deleting or weakening the guard generally — it still catches any other {{range}} action, including a hypothetical future map range"

requirements-completed: [ESC-01]

# Metrics
duration: ~28min
completed: 2026-07-20
---

# Phase 52 Plan 03: Per-Level Verifier Prompt Templates Summary

**Four new `<level>_verifier.tmpl` files (goal-backward rubric for plan-check, observable-outcome framing for phase/milestone/project) plus a backward-compatible D-04 re-plan findings block on `plan_planner.tmpl`, all behind the unchanged `LoadPromptTemplate(role, level)` loader — `PromptTemplateVersion` v4 → v5.**

## Performance

- **Duration:** ~28 min
- **Tasks:** 2
- **Files modified:** 9 (4 created, 5 modified)

## Accomplishments

- `plan_verifier.tmpl`: goal-backward rubric naming all four ESC-01 dimensions (goal alignment, file-touch plausibility, dependency correctness, verification derivability), ranging `.Children` for a bounded child-Task summary, verdict semantics + coverage-not-conservatism directive mirrored from `task_verifier.tmpl`.
- `phase_verifier.tmpl` / `milestone_verifier.tmpl` / `project_verifier.tmpl`: observable-outcome verification ("the checked-out run branch is your reachable filesystem") for the `maxIterations:0` levels, explicitly stating that exhaustion escalates rather than iterates at these levels.
- `plan_planner.tmpl` gained a `{{if .RepairFindings}}` D-04 findings block ("address EVERY finding") — proven byte-identical to pre-change output when empty via the pre-existing frozen `internal/eval` golden file + byte ratchet for `plan_planner` (both pass unchanged, empty-SharedContext and with-SharedContext variants).
- `PromptTemplateVersion` bumped `v4` → `v5` once, covering all five template edits in this plan (the package's own maintenance rule).
- Zero changes to `LoadPromptTemplate`'s function body or `task_verifier.tmpl` — confirmed via `git diff --stat` across both commits.

## Task Commits

Each task was committed atomically:

1. **Task 1: plan_verifier.tmpl (goal-backward rubric) + plan_planner.tmpl findings block** - `f806e194` (feat)
2. **Task 2: phase/milestone/project_verifier.tmpl + version bump + render tests** - `6ffd83a1` (feat)

_No separate plan-metadata commit — SUMMARY.md is the metadata commit in worktree mode (orchestrator merges centrally)._

## Files Created/Modified

- `internal/subagent/common/templates/plan_verifier.tmpl` - new: goal-backward plan-check rubric prompt
- `internal/subagent/common/templates/plan_planner.tmpl` - added the D-04 `{{if .RepairFindings}}` re-plan evidence block
- `internal/subagent/common/templates/phase_verifier.tmpl` - new: observable-outcome level-verify prompt
- `internal/subagent/common/templates/milestone_verifier.tmpl` - new: observable-outcome level-verify prompt
- `internal/subagent/common/templates/project_verifier.tmpl` - new: observable-outcome level-verify prompt
- `internal/subagent/common/prompt_templates.go` - `PromptTemplateVersion` v4→v5, doc comment lists all 10 templates
- `internal/subagent/common/prompt_templates_test.go` - `TestLoadPromptTemplate_LevelVerifiers`, `TestLoadPromptTemplate_PlanVerifier_FourDimensions`, `TestPlanPlannerTemplate_RepairFindingsBlock`, `TestPlanPlannerTemplate_RepairFindingsAbsentUnchanged`, plus test-only `childFixture`/`planVerifierFixture`/`levelVerifierFixture` structs
- `pkg/dispatch/envelope.go` - added `RepairFinding` type + `EnvelopeIn.RepairFindings` field (deviation, see below)
- `internal/eval/render_test.go` - narrowed `TestNoMapInterpolation`'s range guard to exclude the new safe slice range (deviation, see below)

## Decisions Made

- Kept the render-data contract for the four new verifier templates (`.PlanGoal`, `.Children`, `.LevelGoal`) as test-only structs rather than adding them to `pkg/dispatch.EnvelopeIn`, per the plan's explicit "Plans 52-07/52-08/52-09 supply these exact structs" — there is no existing production dispatch site for `role="verifier"` at any level except `task`, so nothing breaks by deferring the real wiring.
- Added `pkg/dispatch.RepairFinding` + `EnvelopeIn.RepairFindings` now (not deferred) because `plan_planner.tmpl` is a different case: it already has a real, unconditional production caller today (`internal/subagent/anthropic/subagent.go`'s `Run()`, and `internal/eval/render_test.go`'s golden/ratchet tests, which run under plain `go test ./...`). Without this field, referencing `.RepairFindings` in the template would hard-error every real dispatch and every existing golden/ratchet test.
- Placed `RepairFindings` as a new top-level `EnvelopeIn` field (mirroring `SharedContext`), not nested under `.Verify`, because `Verify` is nil for planner dispatches per its own doc comment ("Populated when Role=='verifier' ... OR executor repair") — nesting there would just move the same nil-field problem one level down.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1/3 - Bug / Blocking] Added `pkg/dispatch.RepairFinding` + `EnvelopeIn.RepairFindings`**
- **Found during:** Task 1 (plan_planner.tmpl findings block)
- **Issue:** `plan_planner.tmpl` gaining `{{if .RepairFindings}}` / `{{range .RepairFindings}}` would reference a field that does not exist on `pkg/dispatch.EnvelopeIn`. Go's `text/template` hard-errors on missing struct fields (no forgiving default for structs, unlike maps). The real production dispatch path (`internal/subagent/anthropic/subagent.go:254`, `tmpl.Execute(&promptBuf, in)` with `in pkgdispatch.EnvelopeIn`) executes this exact template for every planner dispatch at every level, including `role="planner", level="plan"`. `internal/eval/render_test.go`'s `TestGoldenRender_PlanPlanner` / `TestByteRatchet_PlanPlanner` / `TestGoldenRender_PlanPlannerWithSharedContext` also render this template against a bare `EnvelopeIn` under plain `go test ./...` (no build tag, no live credentials required). Shipping the template change without the field would crash the one real production dispatch path this template serves, and would fail `go test ./internal/eval/...` immediately.
- **Fix:** Added `RepairFinding` (`Severity`/`Confidence`/`Summary`, all `omitempty`) and `EnvelopeIn.RepairFindings []RepairFinding` (`omitempty`), doc-commented as populated by a later plan-check-loop plan, nil/empty on every dispatch today.
- **Files modified:** `pkg/dispatch/envelope.go`
- **Verification:** `go test ./pkg/dispatch/... ./internal/eval/... ./internal/subagent/... -count=1` all green; `TestGoldenRender_PlanPlanner`/`TestByteRatchet_PlanPlanner`/`TestGoldenRender_PlanPlannerWithSharedContext` pass with zero golden/ratchet file changes (byte-identical, empirically confirmed, not just reasoned).
- **Committed in:** `f806e194` (Task 1 commit)

**2. [Rule 3 - Blocking] Narrowed `internal/eval`'s `TestNoMapInterpolation` range guard**
- **Found during:** Task 1 (plan_planner.tmpl findings block)
- **Issue:** `TestNoMapInterpolation` (PROMPT-05 regression guard) fails on ANY `{{range` substring found in the five templateCases templates, regardless of whether the ranged value is a map (its actual stated concern — key-order nondeterminism) or a slice (deterministic, safe). Adding the legitimate `{{range .RepairFindings}}` (over `[]RepairFinding`, not a map) to `plan_planner.tmpl` would trip this over-broad guard as a false positive.
- **Fix:** Narrowed the check to strip the one known-safe `{{range .RepairFindings}}` occurrence before testing for `{{range`, keeping the guard's actual purpose (catching a future accidental map-range) intact for everything else.
- **Files modified:** `internal/eval/render_test.go`
- **Verification:** `go test ./internal/eval/... -run TestNoMapInterpolation -v` passes for all five template cases including `plan_planner`.
- **Committed in:** `f806e194` (Task 1 commit)

---

**Total deviations:** 2 auto-fixed (1 Rule 1/3 bug/blocking, 1 Rule 3 blocking)
**Impact on plan:** Both fixes were required to keep the plan's own Task 1 change from breaking an existing production dispatch path and an existing green test suite. No scope creep beyond what Task 1's `plan_planner.tmpl` edit required to stay safe.

## Issues Encountered

- `52-PATTERNS.md`, referenced by the plan's `<context>` block and Task 1's `<read_first>` as the source of the "7-point section-for-section map" and "internal/subagent/common/prompt_templates.go (NEW)" filename-convention doc, does not exist anywhere under `.planning/phases/52-per-level-looppolicy-parameterization/`. Mirrored `task_verifier.tmpl`'s actual structure directly (as the `<read_first>` instruction also directs) instead of a PATTERNS.md map. No functional impact — all acceptance-criteria greps and tests pass.
- `internal/controller`'s envtest suite (`TestControllers`) could not run in this sandbox (`/usr/local/kubebuilder/bin/etcd: no such file or directory`, no `setup-envtest`-provisioned binaries pre-installed) when run standalone via `go test ./internal/controller/...`. Not a regression: this plan touches zero files under `internal/controller/`. Running the full `make test` target (which provisions `KUBEBUILDER_ASSETS` via `setup-envtest` first) succeeded and `internal/controller` passed green (74.9% coverage, 87.4s) — see verification below.

## User Setup Required

None - no external service configuration required.

## Next Phase Readiness

- The render-data contract for downstream plans is pinned and verified renderable: `plan_verifier.tmpl` against `.Verify`/`.PlanGoal`/`.Children` (`.Name`/`.DependsOn`/`.Files`/`.GateCommand`); `phase/milestone/project_verifier.tmpl` against `.Verify`/`.LevelGoal`; `plan_planner.tmpl` against `.RepairFindings` (`.Severity`/`.Confidence`/`.Summary`, now a real `pkg/dispatch.EnvelopeIn` field).
- Plans 52-07/52-08/52-09 (plan-check loop, level-verify hook, gate-policy resolver) can build their dispatch sites directly against these templates with zero further loader or template changes — `LoadPromptTemplate("verifier", "plan"|"phase"|"milestone"|"project")` all resolve today.
- Full verification run: `go build ./...` clean; `make test` (unit tier incl. envtest, 74 packages) all green; `./bin/golangci-lint run ./...` (v2.11.4) 0 issues; `git diff --stat internal/subagent/common/templates/task_verifier.tmpl` empty across both commits (D-09 untouched, confirmed).
- No blockers.

---
*Phase: 52-per-level-looppolicy-parameterization*
*Completed: 2026-07-20*
