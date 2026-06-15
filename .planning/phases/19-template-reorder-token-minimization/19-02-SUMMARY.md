---
phase: 19-template-reorder-token-minimization
plan: "02"
subsystem: testing
tags: [go-templates, eval-harness, prompt-engineering, token-optimization, goldie, ratchet]

# Dependency graph
requires:
  - phase: 19-template-reorder-token-minimization
    provides: plan 19-01 completed (milestone_planner + project_planner reordered; Phase 18 eval harness in place)
provides:
  - phase_planner.tmpl reordered to D-03 stable-prefix-first order with SharedContext slot and volatile suffix
  - plan_planner.tmpl reordered to D-03 stable-prefix-first order; FILE-TOUCH RULE + REQUIRED fields + JSON-escaping directives preserved verbatim
  - Regenerated goldens for phase_planner and plan_planner
  - Lowered ratchets: phase_planner 2271→1974, plan_planner 4281→3985
affects: [19-03, 19-04, 20-cache-prefix-population]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "D-03 stable-prefix-first template order: role preamble → fixed instructions → SharedContext slot → volatile suffix → {{.Prompt}}"
    - "D-06 annotation: {{/* WHY: ... */ -}} (trim-right only) before load-bearing lines — zero rendered bytes, durable safety record"
    - "D-07 slot reservation: bare {{/* SharedContext slot — populated in Phase 20 (CACHE-02/03) */}} produces one blank-line separator"
    - "Three-file atomic commit discipline: .tmpl + .golden + .txt in same commit for every byte change"

key-files:
  created: []
  modified:
    - internal/subagent/common/templates/phase_planner.tmpl
    - internal/subagent/common/templates/plan_planner.tmpl
    - internal/eval/testdata/goldie/phase_planner.golden
    - internal/eval/testdata/goldie/plan_planner.golden
    - internal/eval/testdata/ratchets/phase_planner.txt
    - internal/eval/testdata/ratchets/plan_planner.txt

key-decisions:
  - "D-01 implemented: volatile suffix carries TaskUID label line + task-dir path mapping (2 occurrences correct); stable prefix is UID-free"
  - "D-02 implemented: Level/Role/Provider.Vendor/Provider.Model printed lines dropped entirely from both templates"
  - "D-04 conservative trim: kept README.md + MILESTONE.md/PHASE-BRIEF.md spec-read directives verbatim; only TIDE acronym expansion paragraph compressed"
  - "D-06 annotation: 3 WHY annotations in phase_planner, 5 WHY annotations in plan_planner (heightened density for T-19-03 mitigations)"
  - "D-07 slot: bare {{/* SharedContext slot — populated in Phase 20 (CACHE-02/03) */}} (no trim markers) between fixed instructions and volatile suffix"
  - "plan_planner machine contract blocks (FILE-TOUCH RULE, four REQUIRED fields, two JSON-escaping IMPORTANT blocks) preserved verbatim per T-19-03"

patterns-established:
  - "Annotate-before-trim: WHY annotations committed in Commit A establish the safety record that gates Commit C+ trims"
  - "Stable-prefix invariant: awk check exits 0 ⇔ zero TaskUID before SharedContext slot marker"

requirements-completed: [PROMPT-01, PROMPT-02, PROMPT-03, PROMPT-04]

# Metrics
duration: 35min
completed: 2026-06-15
---

# Phase 19 Plan 02: Template Reorder + Trim (phase_planner + plan_planner) Summary

**phase_planner and plan_planner reordered to D-03 stable-prefix-first with volatile suffix, dropping 297 and 296 bytes respectively from v1.0.1 baselines, with FILE-TOUCH RULE and JSON-escaping machine contract preserved verbatim**

## Performance

- **Duration:** ~35 min
- **Started:** 2026-06-15T19:53:00Z
- **Completed:** 2026-06-15T20:28:23Z
- **Tasks:** 2 (each with 3 sub-commits: A/B/C)
- **Files modified:** 6

## Accomplishments

- `phase_planner.tmpl` reordered: role preamble → fixed instructions → SharedContext slot → volatile suffix; ratchet 2271→1974 (-297 bytes, 13.1% reduction)
- `plan_planner.tmpl` reordered: same D-03 order; FILE-TOUCH RULE, four REQUIRED spec fields (planRef/filesTouched/declaredOutputPaths/prompt), and both JSON-escaping IMPORTANT blocks preserved verbatim; ratchet 4281→3985 (-296 bytes, 6.9% reduction)
- Zero volatile tokens (TaskUID/Provider.*/Level/Role) in stable prefix of either template; TaskUID appears twice in volatile suffix only (label line + path mapping, per D-01)
- Phase 18 eval harness remained green at every commit boundary (6 commits total)

## Task Commits

Each task was committed in three-file atomic sub-commits (A/B/C):

**Task 1: phase_planner.tmpl**
1. Commit A (annotation): `07ce3f7` (refactor: annotate load-bearing lines)
2. Commit B (reorder): `f4aa263` (feat: reorder to D-03, three-file atomic: tmpl+golden+ratchet)
3. Commit C (trim): `a058095` (refactor: trim paradigm preamble, three-file atomic)

**Task 2: plan_planner.tmpl**
4. Commit A (annotation): `00d07da` (refactor: annotate load-bearing lines with heightened density)
5. Commit B (reorder): `ee1bf72` (feat: reorder to D-03, three-file atomic: tmpl+golden+ratchet)
6. Commit C (trim): `4b8e1c5` (refactor: trim paradigm preamble, three-file atomic)

## Files Created/Modified

- `internal/subagent/common/templates/phase_planner.tmpl` — reordered, annotated, trimmed; D-03 order with D-07 slot
- `internal/subagent/common/templates/plan_planner.tmpl` — reordered, annotated, trimmed; machine contract blocks preserved
- `internal/eval/testdata/goldie/phase_planner.golden` — regenerated (reflects D-03 structure without metadata block)
- `internal/eval/testdata/goldie/plan_planner.golden` — regenerated (reflects D-03 structure without metadata block)
- `internal/eval/testdata/ratchets/phase_planner.txt` — lowered from 2271 to 1974
- `internal/eval/testdata/ratchets/plan_planner.txt` — lowered from 4281 to 3985

## Decisions Made

- Compressed TIDE acronym paragraph from "TIDE (Topologically-Indexed Dependency Execution) runs hierarchical agentic coding work as a Milestone → Phase → Plan → Task → Wave DAG, dispatched as Kubernetes subagent Jobs" to "TIDE is a Kubernetes-native hierarchical agentic work orchestrator" — redundant with README.md spec the model reads
- Added "the spec is load-bearing" emphasis to maintain imperative tone in the compressed preamble
- Chose not to trim anything from the HOW-TO-EMIT block, four REQUIRED fields, FILE-TOUCH RULE, or JSON-escaping IMPORTANT blocks (T-19-03: these ARE the machine contract; heightened conservatism applied)
- Annotation-first discipline: WHY comments committed in Commit A before any content removal in Commit C

## Deviations from Plan

None — plan executed exactly as written. The safe sequencing (A→B→C) was followed for both templates; three-file atomic commits at every byte-changing step; eval harness green at every boundary.

## Issues Encountered

None. The `-update` flag for goldie requires the form `go test ./internal/eval/ -run TestGoldenRender_X -update` (flag after package path, not before), which worked correctly once the package path argument was positioned correctly.

## Known Stubs

None. Both templates render fully with all instructions intact.

## Next Phase Readiness

- `phase_planner.tmpl` and `plan_planner.tmpl` are in canonical D-03 order with SharedContext slot markers ready for Phase 20 (CACHE-02/03) population
- Both ratchets are lower than v1.0.1 baselines (phase: 1974 < 2271, plan: 3985 < 4281)
- Protocol tests (TestDAGAcyclicity_*, TestDeclaredOutputPaths_Presence) remain green
- Phase 19 plans 19-03 (milestone_planner already done in 19-01; task_executor remains) and 19-04 (human review checkpoint) can proceed

---
*Phase: 19-template-reorder-token-minimization*
*Completed: 2026-06-15*

## Self-Check: PASSED

Files exist:
- `internal/subagent/common/templates/phase_planner.tmpl` — FOUND
- `internal/subagent/common/templates/plan_planner.tmpl` — FOUND
- `internal/eval/testdata/goldie/phase_planner.golden` — FOUND
- `internal/eval/testdata/goldie/plan_planner.golden` — FOUND
- `internal/eval/testdata/ratchets/phase_planner.txt` — FOUND (1974 < 2271)
- `internal/eval/testdata/ratchets/plan_planner.txt` — FOUND (3985 < 4281)

Commits exist (git log confirms):
- `07ce3f7` — FOUND
- `f4aa263` — FOUND
- `a058095` — FOUND
- `00d07da` — FOUND
- `ee1bf72` — FOUND
- `4b8e1c5` — FOUND
