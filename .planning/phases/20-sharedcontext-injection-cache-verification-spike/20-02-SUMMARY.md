---
phase: 20-sharedcontext-injection-cache-verification-spike
plan: "02"
subsystem: templates
tags: [prompt-templates, shared-context, goldie, ratchet, cache, CACHE-03]

requires:
  - phase: 20-sharedcontext-injection-cache-verification-spike
    plan: "01"
    provides: EnvelopeIn.SharedContext field (sharedContext,omitempty) on pkg/dispatch/envelope.go

provides:
  - "{{if .SharedContext}}...{{end}} guard wired into all four planner templates (D-07 slot filled)"
  - "Non-empty SharedContext golden fixture proving CACHE-03 stable-prefix ordering"
  - "go test ./internal/eval/ green: empty-fixture ratchets unchanged at 1862/1974/3985/2193"

affects:
  - phase 20-03 (controller stamp — BuildPlannerEnvelope will set SharedContext which now renders)
  - phase 20-05 (make eval gate — non-empty golden proves ordering; token floor confirmed in that plan)

tech-stack:
  added: []
  patterns:
    - "SharedContext guard form: {{if .SharedContext}}\\n{{.SharedContext}}\\n{{end}} (no trim markers) preserves surrounding whitespace so empty renders are byte-identical to pre-edit golden"
    - "Non-empty golden fixture with goldenAssertWithSharedContext helper + ordering assertion"

key-files:
  created:
    - internal/eval/testdata/goldie/milestone_planner_with_shared_context.golden
    - internal/eval/testdata/goldie/project_planner_with_shared_context.golden
    - internal/eval/testdata/goldie/phase_planner_with_shared_context.golden
    - internal/eval/testdata/goldie/plan_planner_with_shared_context.golden
  modified:
    - internal/subagent/common/templates/milestone_planner.tmpl
    - internal/subagent/common/templates/project_planner.tmpl
    - internal/subagent/common/templates/phase_planner.tmpl
    - internal/subagent/common/templates/plan_planner.tmpl
    - internal/eval/render_test.go

key-decisions:
  - "Trim marker deviation: PATTERNS.md specified {{- if .SharedContext}}...{{end -}} but empirical test showed the leading/trailing trim consumed surrounding newlines, making empty renders shorter than the existing golden. Used untrimmed {{if}}...{{end}} form which is byte-identical when SharedContext is empty."
  - "Non-empty golden covers all four planner templates via goldenAssertWithSharedContext; ratchets are not added for the non-empty fixture (ratchetAssert only covers production dispatch shape which leaves SharedContext empty)"
  - "Ordering assertion is in the test itself (not a ratchet): strings.Index(blob) < strings.Index(TaskUID:) proves stable-prefix placement"

patterns-established:
  - "SharedContext guard: {{if .SharedContext}}\\n{{.SharedContext}}\\n{{end}} (no trim) — zero-byte when empty, adds blob + two newlines when non-empty"
  - "goldenAssertWithSharedContext: reusable helper that sets fixture SharedContext and asserts ordering before goldie comparison"

requirements-completed: [CACHE-03]

duration: 10min
completed: "2026-06-15"
---

# Phase 20 Plan 02: SharedContext Template Injection Summary

**{{.SharedContext}} guard wired into all four planner templates with zero-byte empty render; non-empty golden fixture proves stable-prefix ordering (CACHE-03)**

## Performance

- **Duration:** ~10 min
- **Started:** 2026-06-15T23:30:00Z
- **Completed:** 2026-06-15T23:38:56Z
- **Tasks:** 2 of 2
- **Files modified:** 9

## Accomplishments

- Replaced the D-07 `{{/* SharedContext slot */}}` comment marker in all four planner templates (milestone/project/phase/plan) with a `{{if .SharedContext}}...{{end}}` guard that renders zero extra bytes when empty
- Confirmed wave-0 ratchet integrity: `go test ./internal/eval/` exits 0 with all ratchets unchanged (milestone=1862, project=2193, phase=1974, plan=3985, task=1566)
- Added `goldenAssertWithSharedContext` helper + four `TestGoldenRender_*WithSharedContext` tests covering all planner templates with a 397-byte deterministic wave-scoped fixture blob
- Ordering assertion built into each test: SharedContext blob index < TaskUID: index proves CACHE-03 stable-prefix placement

## Task Commits

1. **Task 1: Wave-0 ratchet-integrity check** - `83bb412` (feat)
2. **Task 2: Non-empty SharedContext fixture, golden + ratchet re-baseline** - `683e8b1` (feat)

**Plan metadata:** (this commit)

## Files Created/Modified

- `internal/subagent/common/templates/milestone_planner.tmpl` - D-07 slot replaced with `{{if .SharedContext}}...{{end}}` guard
- `internal/subagent/common/templates/project_planner.tmpl` - same
- `internal/subagent/common/templates/phase_planner.tmpl` - same
- `internal/subagent/common/templates/plan_planner.tmpl` - same
- `internal/eval/render_test.go` - goldenAssertWithSharedContext helper + ordering assertion + 4 new test functions
- `internal/eval/testdata/goldie/milestone_planner_with_shared_context.golden` - 2261 bytes (non-empty fixture)
- `internal/eval/testdata/goldie/project_planner_with_shared_context.golden` - 2592 bytes (non-empty fixture)
- `internal/eval/testdata/goldie/phase_planner_with_shared_context.golden` - 2373 bytes (non-empty fixture)
- `internal/eval/testdata/goldie/plan_planner_with_shared_context.golden` - 4384 bytes (non-empty fixture)

## Non-empty Fixture Byte Counts (re-baselined in Task 2)

| Template | Empty ratchet (unchanged) | Non-empty golden bytes | Delta |
|---|---|---|---|
| milestone_planner | 1862 | 2261 | +399 |
| project_planner | 2193 | 2592 | +399 |
| phase_planner | 1974 | 2373 | +399 |
| plan_planner | 3985 | 4384 | +399 |

The +399 byte delta = 397-byte fixture blob + 2 newlines introduced by the guard form.

Note: these non-empty goldens prove CACHE-03 ordering. The live >=1,024-token floor confirmation is Plan 05's `make eval` run, not a unit test.

## Decisions Made

**Trim marker deviation (auto-fixed — Rule 1 Bug):** PATTERNS.md specified `{{- if .SharedContext}}...{{end -}}` (with leading and trailing trim markers). Empirical test showed this form stripped the newline before the `if` (via `{{-`) and the newline after `{{end}}` (via `-}}`), making the empty-SharedContext render lose the blank line between "machine contract." and "TaskUID:". This changed the golden bytes from the expected values. Fix: use `{{if .SharedContext}}...{{end}}` (no trim markers) which preserves all surrounding whitespace, keeping the empty render byte-identical to the pre-edit golden. Verified by `go test ./internal/eval/` passing with all ratchets unchanged.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] PATTERNS.md trim markers cause golden byte drift on empty render**
- **Found during:** Task 1 (Wave-0 ratchet-integrity check)
- **Issue:** `{{- if .SharedContext}}...{{end -}}` trims surrounding newlines even when SharedContext is empty, making the rendered output shorter than the existing golden (removed blank line between "machine contract." and "TaskUID:")
- **Fix:** Used untrimmed `{{if .SharedContext}}...{{end}}` form which leaves surrounding whitespace intact; empty render is byte-identical to pre-edit golden
- **Files modified:** All four planner .tmpl files
- **Verification:** `go test ./internal/eval/` exits 0; all TestByteRatchet_* tests pass with ratchets at 1862/1974/3985/2193
- **Committed in:** 83bb412

---

**Total deviations:** 1 auto-fixed (Rule 1 — bug in plan's trim-marker specification)
**Impact on plan:** Fix is narrowly scoped to template syntax; all acceptance criteria met. Zero-byte empty render confirmed empirically.

## Issues Encountered

None beyond the trim-marker deviation documented above.

## Known Stubs

None — the guard is fully functional; non-empty SharedContext renders correctly. The live >=1,024-token floor confirmation is deferred to Plan 05 by design (not a stub).

## Threat Flags

None — no new network endpoints, auth paths, file access patterns, or schema changes. SharedContext renders as plain template TEXT with no shell/exec path (T-20-02-01 accepted). task_executor.tmpl confirmed free of `{{.SharedContext}}` interpolation (T-20-02-02 mitigated; grep returns 0).

## Self-Check

Files exist:
- milestone_planner.tmpl: modified ✓
- project_planner.tmpl: modified ✓
- phase_planner.tmpl: modified ✓
- plan_planner.tmpl: modified ✓
- milestone_planner_with_shared_context.golden: created ✓
- project_planner_with_shared_context.golden: created ✓
- phase_planner_with_shared_context.golden: created ✓
- plan_planner_with_shared_context.golden: created ✓
- render_test.go: modified ✓

Commits exist:
- 83bb412: feat(20-02): wire {{.SharedContext}} guard into all four planner templates ✓
- 683e8b1: feat(20-02): add non-empty SharedContext golden fixture, prove CACHE-03 ordering ✓

## Self-Check: PASSED

## Next Phase Readiness

- All four planner templates now interpolate `SharedContext` in the D-07 slot
- Empty-fixture gate is green (ratchets unchanged at 1862/1974/3985/2193)
- Plan 20-03 (controller stamp) can now set `BuildPlannerEnvelope`'s SharedContext and it will render
- Plan 20-05 (`make eval` token floor gate) is the live >=1,024-token confirmation

---
*Phase: 20-sharedcontext-injection-cache-verification-spike*
*Completed: 2026-06-15*
