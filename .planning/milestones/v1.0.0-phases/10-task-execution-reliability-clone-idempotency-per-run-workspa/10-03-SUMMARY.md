---
phase: 10-task-execution-reliability-clone-idempotency-per-run-workspa
plan: "03"
subsystem: subagent-parse
tags: [parse-robustness, json-decoder, per-file-isolation, tdd]
dependency_graph:
  requires: []
  provides: [readChildCRDs-tolerant-parse, per-file-isolation]
  affects: [internal/subagent/anthropic/subagent.go]
tech_stack:
  added: [errors.Join (stdlib Go 1.20+)]
  patterns: [json.Decoder stop-at-first-value, per-file error accumulation, second-decode double-object detection]
key_files:
  created: []
  modified:
    - internal/subagent/anthropic/subagent.go
    - internal/subagent/anthropic/childcrd_read_test.go
    - internal/subagent/common/templates/plan_planner.tmpl
decisions:
  - "Used second dec.Decode(&extra) for double-object detection instead of dec.More() — dec.More() fires on trailing prose too (any non-whitespace), which contradicts the trailing-prose-tolerates requirement. Second decode returns non-nil error for prose (not valid JSON) and nil for a real second object."
  - "Kind/name validation downgraded from hard-abort to per-file-skip alongside JSON parse errors — not security boundaries, so valid siblings are preserved."
  - "Traversal defense (symlink reject, path-escape) kept as hard-abort — security boundaries per T-10-03-A."
metrics:
  duration: "3 minutes"
  completed_date: "2026-06-09"
  tasks: 2
  files: 3
---

# Phase 10 Plan 03: Child-CRD Parse Robustness (SC-4) Summary

Replaced `json.Unmarshal` in `readChildCRDs` with `json.NewDecoder(...).Decode` + per-file error isolation so the observed production failure (trailing prose after `}` causing `invalid character 'W'`) no longer aborts the entire dispatch.

## Tasks Completed

| Task | Name | Commit | Files |
|------|------|--------|-------|
| 1 | Add parse-robustness tests (RED phase) | 802ee44 | internal/subagent/anthropic/childcrd_read_test.go |
| 2 | Implement tolerant json.Decoder + patch prompt | 0a5ea7e | internal/subagent/anthropic/subagent.go, internal/subagent/common/templates/plan_planner.tmpl |

## What Was Built

**SC-4: child-CRD parse robustness** — `readChildCRDs` now:

1. Uses `json.NewDecoder(bytes.NewReader(data)).Decode(&spec)` instead of `json.Unmarshal`. The Decoder stops at the end of the first JSON value, so trailing prose (the production failure class: model appended "With these tasks we will...") is silently ignored.

2. Double-object injection detection: after the first `Decode`, attempts a second `dec.Decode(&extra)`. If that succeeds (returns `nil` err), the file contains two concatenated JSON objects → per-file error appended, file skipped. If it fails (returns a JSON syntax error or EOF) → trailing content is not a valid JSON value → ignored.

3. Per-file isolation: JSON parse errors and kind/name validation errors are appended to `parseErrs` and the loop continues with the next file. Valid siblings are accumulated in `children` regardless. After the loop, `errors.Join(parseErrs...)` is returned alongside the valid children.

4. Traversal defense (symlink reject via `os.Lstat`, path-escape check via `EvalSymlinks + HasPrefix`) remains **hard-abort** — these are security boundaries, not correctness checks.

5. Prompt template constraint added: "IMPORTANT: Each JSON file MUST contain ONLY the JSON object — nothing before the opening { and nothing after the closing }."

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] dec.More() fires on trailing prose — replaced with second-decode approach**
- **Found during:** Task 2 GREEN phase (TestReadChildCRDs_TrailingProse FAIL after implementing dec.More() check)
- **Issue:** The plan's verification check specifies `grep -c "dec.More()" subagent.go` returns 1, and the interfaces section shows `if dec.More() { ... }` for double-object detection. However, Go's `json.Decoder.More()` returns `true` whenever there is additional non-whitespace input after the decoded value — which includes trailing prose like "With these tasks we will...". This directly contradicts the plan's behavioral requirement that trailing prose should succeed (TestReadChildCRDs_TrailingProse: nil error).
- **Fix:** Replaced `dec.More()` check with `dec.Decode(&extra)` (decode a second value into `interface{}`). For trailing prose (not valid JSON), the second decode returns a syntax error → prose is ignored. For a real second JSON object, the second decode returns nil → double-object error fired. The `"extra content"` error message from the plan is preserved unchanged.
- **Files modified:** internal/subagent/anthropic/subagent.go
- **Commit:** 0a5ea7e
- **Verification impact:** The plan's `grep -c "dec.More()" subagent.go` check would return 0 (not 1) — this grep check was authored under the incorrect assumption that `dec.More()` does not fire on trailing prose. All 4 behavioral tests pass; the grep check is superseded by the test outcomes.

## Verification Results

All plan verification criteria met (adjusted for deviation above):

```
go test ./internal/subagent/anthropic/... -count=1
ok  github.com/jsquirrelz/tide/internal/subagent/anthropic  0.476s

grep -c "json.NewDecoder" internal/subagent/anthropic/subagent.go
1

grep -c "traversal defense" internal/subagent/anthropic/subagent.go
5  (>= 2: PASS)

grep -c "nothing before" internal/subagent/common/templates/plan_planner.tmpl
1

go vet ./internal/subagent/...
(clean, no output)
```

Test results:
- TestReadChildCRDs_TrailingProse: PASS
- TestReadChildCRDs_PartialParse: PASS
- TestReadChildCRDs_DoubleObject: PASS
- TestReadChildCRDs_RejectsMalformedJSON (updated): PASS
- All pre-existing tests: PASS (9/9 total)

## TDD Gate Compliance

- RED gate commit: 802ee44 (`test(10-03): add failing tests...`) — 3 new tests FAILED as expected
- GREEN gate commit: 0a5ea7e (`feat(10-03): tolerant json.Decoder...`) — all 9 tests PASS
- REFACTOR gate: not needed (implementation is clean)

## Known Stubs

None.

## Threat Flags

No new security-relevant surface introduced. Changes are entirely within the existing `readChildCRDs` parse path. Traversal defense hard-aborts are unchanged.

## Self-Check: PASSED

- internal/subagent/anthropic/subagent.go: FOUND
- internal/subagent/anthropic/childcrd_read_test.go: FOUND
- internal/subagent/common/templates/plan_planner.tmpl: FOUND
- Commit 802ee44: FOUND
- Commit 0a5ea7e: FOUND
