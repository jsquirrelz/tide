---
phase: 19-template-reorder-token-minimization
plan: "04"
subsystem: eval-harness
tags: [eval, prompt-templates, regression-guard, test]
dependency_graph:
  requires: ["19-01", "19-02", "19-03"]
  provides: ["PROMPT-05-guard", "phase-19-make-test-gate"]
  affects: ["internal/eval/render_test.go"]
tech_stack:
  added: []
  patterns: ["go:embed template source read via os.ReadFile", "subtests over templateCases slice"]
key_files:
  created: []
  modified:
    - internal/eval/render_test.go
decisions:
  - "Read raw .tmpl source via os.ReadFile with relative path (../subagent/common/templates/) rather than exposing unexported templateFS — avoids package-level API change, test still exercises shipped templates transitively via the existing LoadPromptTemplate path"
  - "Assert both .Params reference AND {{range}} action — catches map-range regardless of which map field a future author uses"
metrics:
  duration: "~4 minutes"
  completed: "2026-06-15"
  tasks_completed: 1
  tasks_total: 2
  files_modified: 2
---

# Phase 19 Plan 04: PROMPT-05 Guard + Phase Gate Summary

**One-liner:** PROMPT-05 regression guard (`TestNoMapInterpolation`) added to CI; all five reordered+trimmed templates pass the full-tier `make test` gate (MAKE_EXIT=0, zero FAIL lines).

## Tasks

### Task 1: Add PROMPT-05 regression guard + run full-tier make test phase gate

**Status:** COMPLETE  
**Commit:** `3aeb272`

Added `TestNoMapInterpolation` to `internal/eval/render_test.go`. The test walks all five `templateCases` entries, reads each `.tmpl` source file via `os.ReadFile`, and asserts:

1. No `.Params` reference — `ProviderSpec.Params` is the only `map[string]string` field on `EnvelopeIn`; map-typed interpolation in a template produces key-order nondeterminism in rendered bytes.
2. No `{{range` action — a range over a map produces nondeterministic output; a range over a slice would be safe but also not present today.

All five subtests pass today (PROMPT-05 confirmed no-op). A future template edit that introduces map-range interpolation will fail CI, signaling that stable-key-order serialization is required per PROMPT-05 scope.

Full-tier `make test` result: MAKE_EXIT=0, zero `--- FAIL` / `^FAIL` lines.

### Acceptance Criteria Verification

| Criterion | Result |
|-----------|--------|
| `go test ./internal/eval/ -run TestNoMapInterpolation` exits 0 | PASS (all 5 subtests) |
| `grep -c "func TestNoMapInterpolation" render_test.go` returns 1 | 1 |
| `make test` MAKE_EXIT=0 | 0 |
| Zero FAIL lines in `make test` | ZERO |
| Zero `.Params`/`{{range` in templates | ZERO HITS |
| STABLE-PREFIX INVARIANT (zero TaskUID before slot marker) | PASS all 5 templates |
| Zero `.Provider`/`.Level`/`.Role` interpolation in templates | ZERO HITS |
| Ratchets below baseline (project<2474, milestone<2214, phase<2271, plan<4281, task<1961) | project=2193, milestone=1862, phase=1974, plan=3985, task=1566 — ALL BELOW |

### Task 2: Human review of annotated reorder/trim diffs (D-05) + make eval token confirmation

**Status:** AWAITING CHECKPOINT — not auto-approved (blocking human-verify gate)

## Deviations from Plan

**1. [Rule 1 - Minor] go fmt whitespace fix in test/e2e/kind_setup_test.go**
- **Found during:** `make test` (which runs `go fmt ./...`)
- **Issue:** String concatenation formatting: `"--set", "subagent.defaults.image=" + kindE2EStubSubagentImage` → `"--set", "subagent.defaults.image="+kindE2EStubSubagentImage`
- **Fix:** Included in the same commit since `go fmt` produced it as part of the task's gate run
- **Files modified:** `test/e2e/kind_setup_test.go`
- **Commit:** `3aeb272`

No other deviations. Plan executed as written for Task 1.

## Known Stubs

None — `TestNoMapInterpolation` reads and asserts on real template source; no mock data.

## Threat Flags

None — this plan modifies only test code (`render_test.go`) and includes a `go fmt` whitespace fix. No new network endpoints, auth paths, file access patterns, or schema changes.

## Self-Check

- [x] `internal/eval/render_test.go` exists and contains `func TestNoMapInterpolation`
- [x] Commit `3aeb272` exists in git log
- [x] `make test` MAKE_EXIT=0 confirmed by reading echoed exit status
- [x] Zero FAIL lines confirmed by grep

## Self-Check: PASSED
