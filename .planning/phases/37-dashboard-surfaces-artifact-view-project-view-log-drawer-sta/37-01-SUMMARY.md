---
phase: 37-dashboard-surfaces-artifact-view-project-view-log-drawer-sta
plan: 01
subsystem: ui
tags: [sse, eventsource, react, hooks, pod-logs, dashboard, go, client-go]

# Dependency graph
requires:
  - phase: 04-dashboard
    provides: useSSEStream/useTaskLog hooks, PodLogStreamer, logs_sse.go SSE backend
provides:
  - Named terminal-event support (pod-gone/error/idle-timeout) on the task-log SSE hook
  - Terminal state machine in useTaskLog with permanent reconnect suppression + manual reconnect()
  - PodLogStreamer four-state rendering — every display state renders explicit copy (no silent-empty viewport)
  - resolvePodName widened to serve terminated-but-present (Succeeded/Failed) pods
affects: [37-10, log-drawer, dashboard-verify]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Opt-in named terminal SSE listeners routed through a ref-held callback (mirrors onMessage idiom); reconnect suppression owned by useSSEStream via a terminalRef flag"
    - "TaskLogState terminal states (pod-gone/stream-error/idle-closed) authoritative over transport connection state"
    - "Locked-copy constants + per-state data-testid for exhaustive placeholder coverage"

key-files:
  created: []
  modified:
    - dashboard/web/src/lib/sse.ts
    - dashboard/web/src/lib/sse.test.ts
    - dashboard/web/src/components/PodLogStreamer.tsx
    - dashboard/web/src/components/PodLogStreamer.test.tsx
    - cmd/dashboard/api/logs_sse.go
    - cmd/dashboard/api/logs_sse_test.go

key-decisions:
  - "The 'error' terminal listener guards on `instanceof MessageEvent` so a real-browser transport error (routed to addEventListener('error') as a plain Event) is not misclassified as a permanent stream-error — that path stays owned by onerror."
  - "Reconnect suppression lives in useSSEStream (terminalRef), not just useTaskLog, because the server-side close fires es.onerror after the terminal frame and would otherwise re-arm the backoff loop."
  - "useTaskLog derives its terminal state from an onTerminal callback into its own useState so a terminal frame overrides the raw transport state; reconnect() clears it and resets the ring buffer."

patterns-established:
  - "Terminal-frame handling: cancel pending timer + close socket once + record terminalEvent + suppress onerror reconnect."
  - "Every log-drawer display state renders explicit copy — enforced by a parameterized all-states-render-content test."

requirements-completed: [DASH-04]

coverage:
  - id: D1
    description: "Named terminal SSE events (pod-gone/error/idle-timeout) reach useTaskLog; pod-gone permanently suppresses reconnect; stream-error is manual-reconnect-only; existing project-event + backoff behavior regression-guarded"
    requirement: "DASH-04"
    verification:
      - kind: unit
        ref: "dashboard/web/src/lib/sse.test.ts#useTaskLog terminal events (Plan 37-01 Task 1)"
        status: pass
    human_judgment: false
  - id: D2
    description: "PodLogStreamer renders explicit copy for all seven display states; pod-gone is message-only (no retry), stream-error carries a manual Reconnect affordance"
    requirement: "DASH-04"
    verification:
      - kind: unit
        ref: "dashboard/web/src/components/PodLogStreamer.test.tsx#PodLogStreamer (Plan 37-01 Task 2) — terminal state rendering"
        status: pass
    human_judgment: false
  - id: D3
    description: "resolvePodName serves terminated-but-present (Succeeded/Failed) pods so a finished pod streams retained logs, then EOFs into an honest pod-gone frame; fully-GC'd pods still get an immediate pod-gone"
    requirement: "DASH-04"
    verification:
      - kind: unit
        ref: "cmd/dashboard/api/logs_sse_test.go#TestLogsResolvePodNameServesTerminatedPods"
        status: pass
    human_judgment: false
  - id: D4
    description: "Live D-15 repro (running task streams; GC'd task shows pod-gone; finished-but-present task streams retained logs) against a real deployment"
    verification: []
    human_judgment: true
    rationale: "Deferred to plan 37-10's human-verify checkpoint per the plan's <verification> note — automated coverage proves the state machine; only a live deploy proves the end-to-end repro."

# Metrics
duration: 12min
completed: 2026-07-08
status: complete
---

# Phase 37 Plan 01: Log-Drawer Four-State Model (DASH-04) Summary

**The silently-empty log drawer is fixed end-to-end: named terminal SSE frames now reach the hook, pod-gone permanently stops the reconnect loop, stream-error carries a manual Reconnect, and every drawer state renders explicit copy — plus finished-but-present pods stream their retained logs instead of being misreported as gone.**

## Performance

- **Duration:** ~12 min
- **Started:** 2026-07-08T08:44:21Z (phase execution start)
- **Completed:** 2026-07-08T08:57:21Z
- **Tasks:** 3
- **Files modified:** 6 (+ REQUIREMENTS.md metadata)

## Accomplishments
- Root-cause fix for DASH-04's three stacked causes: (1) `sse.ts` now registers `addEventListener` for the backend's named terminal frames (`pod-gone`/`error`/`idle-timeout`); (2) the previously-missing `reconnecting` placeholder renders; (3) terminal frames permanently break the EventSource auto-reconnect loop instead of hammering a GC'd pod forever.
- `useTaskLog` gained `"pod-gone" | "stream-error"` states and a `reconnect()` function; pod-gone suppresses reconnect permanently (D-13), stream-error is manual-reconnect-only (D-14), idle-timeout maps to the existing idle-closed copy.
- `PodLogStreamer` renders all seven display states with locked verbatim copy; a parameterized test proves no state falls through to an empty viewport.
- Backend `resolvePodName` widened to `Succeeded`/`Failed` so a terminated-but-present pod serves retained logs, then EOFs into an honest pod-gone frame; truly-GC'd pods still get an immediate pod-gone.

## Task Commits

Each task was committed atomically (TDD: test → feat):

1. **Task 1: sse.ts named terminal events + terminal state machine** - `0eb8f00` (test) → `216cfb2` (feat)
2. **Task 2: PodLogStreamer four-state rendering** - `359f619` (test) → `f8b873a` (feat)
3. **Task 3: resolvePodName serves terminated-but-present pods** - `deef580` (feat)

**Plan metadata:** committed with this SUMMARY + REQUIREMENTS.md (DASH-04 marked complete).

## Files Created/Modified
- `dashboard/web/src/lib/sse.ts` - Opt-in `terminalEventTypes`/`onTerminal` on useSSEStream; terminal handler closes the socket once and suppresses reconnect via `terminalRef`; `onerror` respects the flag; returns `terminalEvent` + `reconnect()`. useTaskLog grows `pod-gone`/`stream-error`, maps terminal frames to drawer states, exposes `reconnect()`.
- `dashboard/web/src/lib/sse.test.ts` - Four terminal-event tests (pod-gone suppression proven by flat constructor-call count, error→stream-error + manual reconnect, idle-timeout→idle-closed, transport-error backoff regression guard).
- `dashboard/web/src/components/PodLogStreamer.tsx` - `COPY_RECONNECTING/POD_GONE/STREAM_ERROR_*` constants; reconnecting (Loader2 + muted), pod-gone (muted, message only), stream-error (AlertTriangle in `--color-destructive` + body + Reconnect button wired to `reconnect()`).
- `dashboard/web/src/components/PodLogStreamer.test.tsx` - Terminal-state render tests + parameterized all-states-non-empty guard; mock updated for new states + `reconnect`.
- `cmd/dashboard/api/logs_sse.go` - `resolvePodName` phase switch includes `PodSucceeded`/`PodFailed`; doc comments updated to cite the DASH-04 widened contract.
- `cmd/dashboard/api/logs_sse_test.go` - `TestLogsResolvePodNameServesTerminatedPods` table test (Running/Pending/Succeeded/Failed resolve; Unknown does not).

## Decisions Made
- **Terminal-error disambiguation:** the `error` listener only treats `MessageEvent`s (frames with data) as terminal; plain transport-error Events (which real browsers also dispatch to `addEventListener('error')`) are ignored so a transient network drop still flows through the automatic-backoff `reconnecting` path rather than being pinned to a permanent stream-error.
- **Suppression owned by useSSEStream:** because the backend closes the connection right after a terminal frame (which fires `es.onerror`), the reconnect-suppression flag had to live in the hook that owns `scheduleReconnect`, not only in useTaskLog.
- **reconnect() clears the ring buffer:** on manual reopen the pod re-streams retained logs, so clearing lines avoids duplicated history.

## Deviations from Plan

None - plan executed exactly as written. (One minor within-scope robustness choice: the `instanceof MessageEvent` guard on the `error` listener, documented above under Decisions, hardens the state machine against the real-browser transport-error/named-error name collision the fake test harness doesn't exercise.)

## Issues Encountered
- Dashboard `node_modules` was not present in the worktree; ran `npm ci` (292 packages) before the vitest suites. asdf required `ASDF_NODEJS_VERSION=22.22.3` to resolve `npx`/`node`.

## User Setup Required
None - no external service configuration required.

## Next Phase Readiness
- DASH-04 automated coverage complete; the state machine and backend widening are proven by unit tests.
- Live D-15 repro (running task, GC'd task, finished-but-present task) remains for plan 37-10's human-verify checkpoint against a real deployment.
- Sibling plans (37-02..37-09) proceed independently; no shared surface with this plan beyond the log endpoint contract, which is unchanged in shape.

## Self-Check: PASSED

All 6 modified source files present on disk; all 5 task commits (2 TDD test→feat pairs + 1 feat) resolve in git history. Full suites green: web 212/212 vitest + `tsc -b` clean; `go test ./cmd/dashboard/api/` ok.

---
*Phase: 37-dashboard-surfaces-artifact-view-project-view-log-drawer-sta*
*Completed: 2026-07-08*
