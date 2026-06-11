---
phase: quick-260611-439
plan: 01
type: execute
wave: 1
depends_on: []
files_modified:
  - internal/dispatch/podjob/caps.go
  - internal/dispatch/podjob/caps_test.go
autonomous: true
requirements: [QUICK-260611-439]

must_haves:
  truths:
    - "A nil-caps planner Job gets activeDeadlineSeconds 1860 (1800 floor + 60 grace), enough for ~11+ min Opus plan-planner sessions"
    - "A nil-caps executor Job gets activeDeadlineSeconds 1260 (1200 floor + 60 grace)"
    - "Drift guard still holds: planner floor (1800) > executor floor (1200)"
    - "Token-mint validity tracks the new floors automatically (no other file hardcodes the old values)"
  artifacts:
    - path: "internal/dispatch/podjob/caps.go"
      provides: "plannerCapsFloorSeconds=1800, executorCapsFloorSeconds=1200, with bump rationale comments"
      contains: "plannerCapsFloorSeconds int32 = 1800"
    - path: "internal/dispatch/podjob/caps_test.go"
      provides: "Floor tests green against new constants; stale 480s/600s literals in names/comments updated"
  key_links:
    - from: "internal/controller/task_controller.go"
      to: "internal/dispatch/podjob/caps.go"
      via: "DefaultCaps(caps, JobKindExecutor) for token-mint validity"
      pattern: "DefaultCaps"
    - from: "internal/dispatch/podjob/jobspec.go"
      to: "internal/dispatch/podjob/caps.go"
      via: "DefaultCaps for activeDeadlineSeconds derivation"
      pattern: "DefaultCaps"
---

<objective>
Raise the wall-clock cap floors in internal/dispatch/podjob/caps.go: plannerCapsFloorSeconds 600→1800, executorCapsFloorSeconds 480→1200.

Purpose: Dogfood run 1 (2026-06-11, verified live) showed heavyweight Opus plan-planner sessions (~11+ min) and Sonnet executor tasks exceeding the current floors. The Job's activeDeadlineSeconds (floor + 60s grace) kills the pod mid-session → exitCode -1, partial envelope flush, EnvelopeReadFailed → CR marked Failed. 8 plans + 10+ tasks were lost this way in one run. This is the same failure mode the existing caps.go comment documents for the prior 300→480 executor bump (wave1-executor-failure debug session).

Output: Two updated constants with bump-rationale comments in the established style, plus refreshed literal references in caps_test.go. Single commit to main (user-authorized).
</objective>

<execution_context>
@$HOME/.claude/get-shit-done/workflows/execute-plan.md
@$HOME/.claude/get-shit-done/templates/summary.md
</execution_context>

<context>
@internal/dispatch/podjob/caps.go
@internal/dispatch/podjob/caps_test.go
</context>

<interfaces>
<!-- Current state of the two constants being changed (caps.go lines 35-63): -->

From internal/dispatch/podjob/caps.go:
```go
const (
	JobKindExecutor JobKind = "executor" // Phase 2 task dispatch — 480s floor
	JobKindPlanner  JobKind = "planner"  // Phase 3 planner dispatch — 600s floor
)

const executorCapsFloorSeconds int32 = 480
const plannerCapsFloorSeconds int32 = 600
```

<!-- caps_test.go assertions already reference the constants by name
     (executorCapsFloorSeconds / plannerCapsFloorSeconds), so they track the
     bump automatically. Only literal mentions in test-case NAMES and comments
     are stale ("→ 480s floor", "→ 600s floor", "480s floor + 60s grace = 540s",
     "600s floor + 60s grace = 660s"). -->
</interfaces>

<tasks>

<task type="auto">
  <name>Task 1: Bump both wall-clock floors, document the dogfood-run-1 evidence, refresh stale literals</name>
  <files>internal/dispatch/podjob/caps.go, internal/dispatch/podjob/caps_test.go</files>
  <action>
In internal/dispatch/podjob/caps.go:

1. Change `executorCapsFloorSeconds` from 480 to 1200 and `plannerCapsFloorSeconds` from 600 to 1800.
2. Extend each constant's doc comment in the established style (mirror the existing "Raised 300→480 after the wave1-executor-failure debug session" paragraph). Record for this bump: raised 480→1200 (executor) / 600→1800 (planner) after dogfood run 1 (2026-06-11) — heavyweight Opus plan-planner sessions ran ~11+ min and real Sonnet executor tasks exceeded the old floors; the Job's activeDeadlineSeconds (floor + 60s grace) killed the pods → exitCode -1, partial envelope flush, EnvelopeReadFailed → CR marked Failed; 8 plans and 10+ tasks were lost this way in one run. Note that token-mint validity tracks the same floors via DefaultCaps (task_controller.go mint; phase/plan controllers' credproxy.Sign use plannerCaps.WallClockSeconds + grace), so no other change is needed. The plannerCapsFloorSeconds constant currently has only a one-paragraph comment — add the bump-history paragraph there too.
3. Update the inline JobKind const comments ("— 480s floor" / "— 600s floor") to the new values. Also fix the now-stale "while staying below the 600s planner floor" mention inside the executor comment's existing 300→480 paragraph (rephrase to reference the planner floor by name, or update the number — keep the historical paragraph otherwise intact).

In internal/dispatch/podjob/caps_test.go:

4. Assertions use the constant names so values track automatically — verify no assertion hardcodes 480/600/540/660 as a floor expectation. Update only the stale literal mentions: test-case name strings ("→ 480s floor" → "→ 1200s floor", "→ 600s floor" → "→ 1800s floor"), the section comments ("// Executor branch — 480s floor", "// Planner branch — 600s floor"), and the deadline-match comments ("// Executor branch — 480s floor + 60s grace = 540s" → 1200/1260, "// Planner branch — 600s floor + 60s grace = 660s" → 1800/1860). Leave the "executor: 600s WallClockSeconds → 600s" operator-set case as-is — 600 is now under the floor but operator-set values are honored regardless, so the case still passes; optionally append "(under floor but operator-set is honored)" to its name for clarity, matching the existing 60s case's phrasing.

Keep the diff minimal: two constants + comments + test-name/comment literals only. The drift guard (planner > executor) is satisfied by 1800 > 1200 — do not touch the guard.

Do NOT touch: internal/dispatch/podjob/jobspec.go `DefaultTTLSecondsAfterFinished = 600` (a TTL, not a wall-clock floor); `WallClockSeconds: 600` in internal/controller/dispatch_helpers_test.go and cmd/stub-subagent/planner_test.go (explicit operator-set caps that bypass the floor by design).
  </action>
  <verify>
    <automated>go test ./internal/dispatch/podjob/... && go test ./internal/controller/... && ! grep -rnE 'FloorSeconds int32 = (480|600)' internal/dispatch/podjob/caps.go && ! grep -rlE '(activeDeadline|validity|floor).*(\b540\b|\b660\b)' --include='*.go' internal/ | grep -v caps</automated>
  </verify>
  <done>
    - plannerCapsFloorSeconds = 1800 and executorCapsFloorSeconds = 1200 in caps.go, each with a bump-history comment paragraph citing dogfood run 1 evidence
    - go test ./internal/dispatch/podjob/... passes (floor tests, deadline-match test, drift guard all green)
    - go test ./internal/controller/... passes (token-mint validity derives from DefaultCaps, no hardcoded expectations)
    - No remaining file hardcodes 480/600/540/660 as wall-clock floor or derived-deadline expectations (grep confirms; TTL and explicit-caps test fixtures excluded)
    - Single commit to main containing only caps.go + caps_test.go changes
  </done>
</task>

</tasks>

<verification>
- `go test ./internal/dispatch/podjob/...` exit 0
- `go test ./internal/controller/...` exit 0
- `grep -n 'int32 = 1800' internal/dispatch/podjob/caps.go` and `grep -n 'int32 = 1200' internal/dispatch/podjob/caps.go` each return 1 line
- `git log --oneline -1 -- internal/dispatch/podjob/caps.go` shows the bump commit on main
</verification>

<success_criteria>
- Nil-caps planner Jobs derive activeDeadlineSeconds 1860 and token validity 1860; nil-caps executor Jobs derive 1260 — verified by TestDefaultCaps and TestDefaultCaps_NilCapsDeadlineMatch passing against the new constants
- Drift guard holds (1800 > 1200)
- Comment history in caps.go records both bumps (300→480 wave1-executor-failure; 480→1200 / 600→1800 dogfood run 1) in matching style
- Diff is minimal: caps.go + caps_test.go only
</success_criteria>

<output>
Create `.planning/quick/260611-439-podjob-caps-floor-bump/SUMMARY.md` when done
</output>
