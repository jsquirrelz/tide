---
phase: quick-260611-439
plan: 01
subsystem: dispatch
tags: [podjob, caps, activeDeadlineSeconds, dogfood-run-1]
requires: []
provides:
  - "plannerCapsFloorSeconds=1800 / executorCapsFloorSeconds=1200 in internal/dispatch/podjob/caps.go"
affects:
  - "nil-caps Job activeDeadlineSeconds (jobspec.go) and token-mint validity (task/phase/plan controllers) via DefaultCaps"
tech-stack:
  added: []
  patterns: []
key-files:
  created: []
  modified:
    - internal/dispatch/podjob/caps.go
    - internal/dispatch/podjob/caps_test.go
decisions:
  - "Floor values 1800/1200 chosen from dogfood-run-1 live evidence (~11+ min Opus planner sessions); drift guard (planner > executor) preserved by construction"
metrics:
  duration: "~4 min"
  completed: 2026-06-11
---

# Quick 260611-439 Plan 01: PodJob Caps Floor Bump Summary

Raised nil-caps wall-clock floors 480→1200 (executor) and 600→1800 (planner) with dogfood-run-1 evidence recorded in bump-history comments, fixing the activeDeadlineSeconds mid-session pod kills that lost 8 plans and 10+ tasks.

## What Was Done

- `executorCapsFloorSeconds` 480 → 1200; `plannerCapsFloorSeconds` 600 → 1800 (caps.go).
- Added a bump-history paragraph to each constant in the established style (mirroring the 300→480 wave1-executor-failure paragraph), citing dogfood run 1 (2026-06-11): deadline kills → exitCode -1 → partial envelope flush → EnvelopeReadFailed → CR Failed; 8 plans + 10+ tasks lost.
- Updated the JobKind inline comments to the new floors and rephrased the historical paragraph's "below the 600s planner floor" to reference `plannerCapsFloorSeconds` by name (history otherwise intact).
- caps_test.go: refreshed stale literals only — test-case names ("→ 480s floor" → "→ 1200s floor", "→ 600s floor" → "→ 1800s floor"), section comments, deadline-match comments (540→1260, 660→1860), and appended "(under floor but operator-set is honored)" to the executor 600s operator-set case name.
- Did NOT touch: `DefaultTTLSecondsAfterFinished = 600` (TTL, not a floor), explicit `WallClockSeconds: 600` test fixtures in dispatch_helpers_test.go / planner_test.go, or the drift guard.

## Verification

- `go test ./internal/dispatch/podjob/...` — ok, exit 0 (floor tests, deadline-match, drift guard green against new constants)
- `go test ./internal/controller/...` — ok, exit 0 (token-mint validity derives from DefaultCaps; no hardcoded expectations)
- `grep -rnE 'FloorSeconds int32 = (480|600)'` on caps.go — no matches
- `grep -rlE '(activeDeadline|validity|floor).*(\b540\b|\b660\b)'` across internal/ (excluding caps files) — no matches
- `grep -n 'int32 = 1800'` and `'int32 = 1200'` each return exactly 1 line in caps.go
- `gofmt -l internal/dispatch/podjob/` — clean

## Deviations from Plan

None - plan executed exactly as written.

## Commits

| Commit  | Description                                                  |
| ------- | ------------------------------------------------------------ |
| 47a9aa9 | fix(quick-260611-439): raise podjob wall-clock cap floors after dogfood run 1 |

## Self-Check: PASSED

- internal/dispatch/podjob/caps.go — FOUND (constants 1800/1200 confirmed by grep)
- internal/dispatch/podjob/caps_test.go — FOUND (tests green)
- Commit 47a9aa9 — FOUND on main
