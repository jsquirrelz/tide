---
phase: 19-template-reorder-token-minimization
plan: "03"
subsystem: prompt-templates
tags: [template-reorder, token-minimization, eval-harness, task-executor]
dependency_graph:
  requires: []
  provides:
    - task_executor.tmpl reordered to D-03 stable-prefix-first structure
    - SharedContext slot marker (Phase 20 insertion point)
    - Lowered ratchet baseline (1566 bytes, down from 1961)
  affects:
    - internal/eval/testdata/goldie/task_executor.golden
    - internal/eval/testdata/ratchets/task_executor.txt
tech_stack:
  added: []
  patterns:
    - "{{/* WHY */ -}} annotation: trim-right-only comment before section headers"
    - "D-07 bare {{/* SharedContext slot */}} marker: zero-token Phase 20 insertion point"
    - "Three-file atomic commit: .tmpl + .golden + .txt in same commit"
key_files:
  created: []
  modified:
    - internal/subagent/common/templates/task_executor.tmpl
    - internal/eval/testdata/goldie/task_executor.golden
    - internal/eval/testdata/ratchets/task_executor.txt
decisions:
  - "D-06 annotations placed before non-indented section headers (Your job: and Emit...) to avoid indentation loss from {{/* */ -}} trim-right eating leading whitespace on indented lines"
  - "SharedContext slot uses bare {{/* */}} (no trim markers) producing a blank-line section separator between stable prefix and volatile suffix"
  - "TIDE paradigm compressed from 4-line expansion to 1 terse sentence; spec-read directive preserved verbatim then also compressed from 3 lines to 1"
  - "Your job bullets tightened: removed 'per-Task' qualifier on in.json bullet, dropped push-Job orchestrator-internals from commit bullet; semantic contract preserved"
metrics:
  duration_minutes: 10
  completed_date: "2026-06-15"
  tasks_completed: 1
  files_modified: 3
---

# Phase 19 Plan 03: task_executor.tmpl Reorder + Annotate + Trim Summary

Reordered `task_executor.tmpl` to canonical D-03 stable-prefix-first structure, consolidating all 6 scattered `{{.TaskUID}}` occurrences into a single volatile-suffix filesystem-layout block; annotated load-bearing lines; conservatively trimmed 395 bytes (20.1%) from the v1.0.1 baseline of 1961 to 1566.

## Tasks Completed

| Task | Name | Commits | Files |
|------|------|---------|-------|
| 1A | Annotate load-bearing lines (Commit A) | d1a59c3 | task_executor.tmpl |
| 1B | Reorder to D-03 structure (Commit B) | adacf32 | task_executor.tmpl + golden + ratchet |
| 1C | Trim TIDE paradigm paragraph (Commit C) | 616563a | task_executor.tmpl + golden + ratchet |
| 1D | Compress spec-read directive (Commit D) | 9caa409 | task_executor.tmpl + golden + ratchet |
| 1E | Tighten Your-job bullets (Commit E) | a7994a5 | task_executor.tmpl + golden + ratchet |

## Final State

```
task_executor.tmpl (35 lines, ratchet 1566 bytes):

  [STABLE PREFIX]
  Role preamble
  TIDE one-liner
  spec-read directive
  {{/* WHY */}} Your job: (4 annotated bullets, UID-free)
  {{/* WHY */}} Emit EnvelopeOut (4 fields, load-bearing)

  {{/* SharedContext slot — populated in Phase 20 (CACHE-02/03) */}}

  [VOLATILE SUFFIX]
  TaskUID: {{.TaskUID}}
  Filesystem layout: 5 path lines (all {{.TaskUID}} occurrences)
  Original prompt: {{.Prompt}}
```

## Ratchet Progression

| Commit | Event | Ratchet |
|--------|-------|---------|
| baseline (v1.0.1) | before Phase 19 | 1961 |
| d1a59c3 (A) | annotation — zero bytes | 1961 |
| adacf32 (B) | reorder: drop metadata block, abstract bullets | 1945 |
| 616563a (C) | trim TIDE paradigm 4-line → 1-line | 1763 |
| 9caa409 (D) | compress spec-read directive 3-line → 1-line | 1711 |
| a7994a5 (E) | tighten Your-job bullets | 1566 |

Total reduction: **395 bytes (20.1%)**.

## Acceptance Criteria Verification

| Criterion | Result |
|-----------|--------|
| `awk` stable-prefix invariant (zero {{.TaskUID}} before marker) | PASS |
| Volatile tokens before marker: 0 | PASS |
| {{.TaskUID}} after marker: 5 (label + 4 path lines) | PASS |
| .Provider/.Level/.Role anywhere: 0 | PASS |
| SharedContext slot marker present: 1 | PASS |
| Filesystem path lines (in.json/out.json/events.jsonl): 4 | PASS |
| no git credentials directive: 1 | PASS |
| WHY annotations: 2 | PASS |
| Ratchet < 1961: 1566 | PASS |
| `go test ./internal/eval/` green at every commit boundary | PASS |

## Deviations from Plan

### Non-issues (implementation decisions, not deviations)

**1. Annotation placement changed from inline-bullet to section-header**

The plan suggested annotating individual bullet lines like `DeclaredOutputPaths` constraint directly. This was attempted but failed: `{{/* WHY */ -}}` trim-right eats `\n  ` (newline + leading spaces) from indented continuation lines, stripping the indentation and corrupting rendered output.

Resolution: Placed both WHY annotations before non-indented section headers (`Your job:` and `Emit your output as an EnvelopeOut with:`) — each comment covers all bullets in the section. This satisfies the `>= 2` WHY annotation criterion while preserving rendered output byte-for-byte (trim-right eats only the comment line's own `\n`).

This is per D-06: "place the comment on its own line BEFORE the annotated line, using trim-right only." The section-header placement is consistent with the PATTERNS doc example (which uses `Write ONLY...` — a non-indented line).

**2. SharedContext slot produces double blank line before TaskUID**

The bare `{{/* SharedContext slot */}}` (no trim markers) on its own line + an explicit blank line in source before `TaskUID:` produces two blank lines in rendered output. This matches the PATTERNS.md documented behavior ("the comment line's own `\n` becomes the blank-line section separator; the explicit `\n` after the comment in source creates the second blank line"). Semantically correct; golden captures this.

**3. GOLDIE_UPDATE env var instead of `-update` flag**

The goldie `-update` flag is registered via Go's `flag` package and cannot be passed as a top-level `go test -update` argument (it gets parsed as a go tool flag for unrecognized programs). Used `GOLDIE_UPDATE=true go test ...` (goldie's documented fallback). Functionally identical.

None of the above changed plan semantics or outcomes.

## Decisions Made

- D-06 annotation mechanics: trim-right-only before section headers avoids indentation corruption on indented continuation lines
- SharedContext slot is bare `{{/* */}}` (no trim markers), produces expected blank-line separator per PATTERNS/RESEARCH verification
- Conservative trim scope: paradigm (4→1 line), spec-read (3→1 line), Your-job bullets (4→4 lines, tighter wording); EnvelopeOut and filesystem layout blocks untouched

## Self-Check: PASSED

| Check | Result |
|-------|--------|
| task_executor.tmpl exists | FOUND |
| task_executor.golden exists | FOUND |
| task_executor.txt exists | FOUND |
| 19-03-SUMMARY.md exists | FOUND |
| Commit d1a59c3 (A: annotate) | FOUND |
| Commit adacf32 (B: reorder) | FOUND |
| Commit 616563a (C: paradigm trim) | FOUND |
| Commit 9caa409 (D: spec-read trim) | FOUND |
| Commit a7994a5 (E: Your-job trim) | FOUND |
| `go test ./internal/eval/` | PASS |
