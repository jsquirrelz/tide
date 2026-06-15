---
phase: 19-template-reorder-token-minimization
plan: "01"
subsystem: prompt-templates
tags: [templates, token-minimization, prompt-engineering, eval-harness]
dependency_graph:
  requires: [phase-18-eval-harness]
  provides: [milestone-planner-reordered, project-planner-reordered]
  affects: [internal/eval/testdata/goldie, internal/eval/testdata/ratchets]
tech_stack:
  added: []
  patterns: [three-file-atomic-commit, d03-stable-prefix-order, d06-why-annotations, d07-sharedcontext-slot]
key_files:
  created: []
  modified:
    - internal/subagent/common/templates/milestone_planner.tmpl
    - internal/subagent/common/templates/project_planner.tmpl
    - internal/eval/testdata/goldie/milestone_planner.golden
    - internal/eval/testdata/goldie/project_planner.golden
    - internal/eval/testdata/ratchets/milestone_planner.txt
    - internal/eval/testdata/ratchets/project_planner.txt
decisions:
  - "D-06 annotation form confirmed: {{/* WHY: ... */ -}} (trim-right only) on own line before annotated line — zero rendered bytes, preceding newline preserved"
  - "D-07 slot confirmed: bare {{/* SharedContext slot — populated in Phase 20 (CACHE-02/03) */}} (no trim markers) — comment line's own newline becomes the blank-line section separator"
  - "D-01/D-02 confirmed: dispatch metadata block removed, inline {{.TaskUID}} path examples replaced with abstract instructions, both TaskUID occurrences in volatile suffix are correct (label + path-mapping)"
  - "Paradigm paragraph compressed to 1-line spec-read core (resolved open question from 19-RESEARCH.md)"
  - "project_planner.tmpl terminator variant 'Original prompt (the project outcome):' preserved verbatim per plan requirement"
metrics:
  duration_seconds: 579
  completed_date: "2026-06-15"
  tasks_completed: 2
  files_modified: 6
  commits: 6
---

# Phase 19 Plan 01: Milestone and Project Planner Reorder + Trim Summary

Reordered `milestone_planner.tmpl` and `project_planner.tmpl` into canonical D-03 stable-prefix-first order (role → fixed instructions → SharedContext slot → volatile suffix → prompt), added WHY annotations, dropped dispatch metadata block, and compressed the paradigm paragraph — lowering milestone ratchet from 2214 → 1862 bytes (352 fewer, 15.9% reduction) and project ratchet from 2474 → 2193 bytes (281 fewer, 11.4% reduction).

## What Was Built

Both planner templates now follow the D-03 canonical section order required for Phase 20 prefix-caching readiness:

1. **Role preamble** — unchanged, template-specific one-liner
2. **Fixed instructions** — paradigm (compressed to 1 line) + Your job + HOW-TO-EMIT (UID-free abstract paths)
3. **SharedContext slot** — `{{/* SharedContext slot — populated in Phase 20 (CACHE-02/03) */}}` zero-token marker
4. **Volatile suffix** — `TaskUID: {{.TaskUID}}` + one-line task-dir path mapping (2 UID occurrences: label + path line)
5. **Prompt** — `Original prompt:` / `Original prompt (the project outcome):` + `{{.Prompt}}`

### Template-specific differences preserved
- `project_planner.tmpl` terminator variant: "Original prompt (the project outcome):" (not normalized)
- `project_planner.tmpl` HOW-TO-EMIT: produces exactly ONE Milestone child CRD
- `milestone_planner.tmpl` HOW-TO-EMIT: produces ONE JSON file per Phase

## Commits

| Commit | Hash | Description |
|--------|------|-------------|
| Task 1 Commit A | add9657 | Annotate milestone_planner.tmpl load-bearing lines |
| Task 1 Commit B | ff84def | Reorder milestone_planner.tmpl to D-03 order (ratchet 2214 → 1998) |
| Task 1 Commit C | e857130 | Compress paradigm paragraph (ratchet 1998 → 1862) |
| Task 2 Commit A | a0bcfd1 | Annotate project_planner.tmpl load-bearing lines |
| Task 2 Commit B | 42805ff | Reorder project_planner.tmpl to D-03 order (ratchet 2474 → 2329) |
| Task 2 Commit C | 472c577 | Compress paradigm paragraph (ratchet 2329 → 2193) |

## Ratchet Reductions

| Template | Baseline | After Reorder (commit B) | After Trim (commit C) | Total Reduction |
|----------|----------|--------------------------|----------------------|-----------------|
| milestone_planner | 2214 | 1998 (−216) | 1862 (−136) | −352 (15.9%) |
| project_planner | 2474 | 2329 (−145) | 2193 (−136) | −281 (11.4%) |

Both final ratchet values are strictly below their v1.0.1 baselines (PROMPT-04 success criterion).

## Acceptance Criteria Verification

### milestone_planner.tmpl
- `go test ./internal/eval/` exits 0: PASSED
- Stable-prefix invariant (awk check): PASSED (zero TaskUID before SharedContext marker)
- Volatile suffix carries UID: PASSED (2 occurrences — label + path-mapping line)
- Provider/Level/Role anywhere: PASSED (0 occurrences)
- SharedContext slot present: PASSED (1 occurrence)
- README.md directive preserved: PASSED (1 occurrence)
- Ratchet < 2214: PASSED (1862)
- WHY annotations: PASSED (5 annotations)

### project_planner.tmpl
- `go test ./internal/eval/` exits 0: PASSED
- Stable-prefix invariant (awk check): PASSED (zero TaskUID before SharedContext marker)
- Volatile suffix carries UID: PASSED (2 occurrences — label + path-mapping line)
- Provider/Level/Role anywhere: PASSED (0 occurrences)
- SharedContext slot present: PASSED (1 occurrence)
- Terminator variant preserved: PASSED ("Original prompt (the project outcome):" present)
- Ratchet < 2474: PASSED (2193)
- WHY annotations: PASSED (5 annotations)

## Deviations from Plan

None — plan executed exactly as written. Three-file atomic commit discipline followed throughout. go test ./internal/eval/ green at every commit boundary.

### Minor notes (not deviations)

**Acceptance criteria `grep -c "the project outcome" returns 1` — actual count is 2:** The plan's acceptance check expected count=1 but the original template already had "Read the project outcome" in the "Your job" bullet, giving count=2. Both occurrences are correct template content; the terminator variant IS preserved at line 48. The criterion should be interpreted as "returns >= 1" to confirm the terminator is present — it is.

**Golden regeneration via `-args -update`:** The RESEARCH.md command `go test -update ./internal/eval/ -run TestGoldenRender_<Name>` fails because the flag is parsed by the Go test runner as a package argument (no Go files in ./). The correct form is `go test ./internal/eval/ -run TestGoldenRender_<Name> -args -update` which passes `-update` to the test binary.

## Known Stubs

None — both templates are fully functional. The SharedContext slot is an explicitly documented zero-token placeholder (per D-07); it is intentionally empty and will be populated in Phase 20 (CACHE-02/03).

## Threat Flags

No new security-relevant surface introduced. This plan edits only `.tmpl` and `testdata/` files. T-19-01 (instruction integrity) mitigated by D-04 conservative trim + PROMPT-03 annotation pass (annotations committed before removals). T-19-02 (structural-gate blind spot) acknowledged as residual MEDIUM risk per plan's threat model.

## Self-Check: PASSED

All files exist, all commits found, both ratchets below baseline, eval package green.

| Check | Result |
|-------|--------|
| All 6 modified files exist | PASSED |
| SUMMARY.md created | PASSED |
| All 6 commits exist (add9657..472c577) | PASSED |
| milestone_planner ratchet 1862 < 2214 | PASSED |
| project_planner ratchet 2193 < 2474 | PASSED |
| go test ./internal/eval/ exits 0 | PASSED |
