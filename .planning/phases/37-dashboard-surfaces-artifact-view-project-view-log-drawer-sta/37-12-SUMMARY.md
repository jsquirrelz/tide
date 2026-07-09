---
phase: 37-dashboard-surfaces-artifact-view-project-view-log-drawer-sta
plan: 12
subsystem: dashboard
tags: [dashboard, log-drawer, sse, reconnect, gap-closure, DASH-04]
gap_closure: true
requires:
  - "useTaskLog.reconnect() (dashboard/web/src/lib/sse.ts) — doReconnect clears the pending backoff timer + resets attemptRef"
provides:
  - "manual Reconnect affordance in the log drawer reconnecting state (Gap 37-G2)"
  - "rebuilt embedded SPA dist carrying the reconnecting-state Reconnect button"
affects:
  - dashboard/web/src/components/PodLogStreamer.tsx
  - cmd/dashboard/embed/dist
tech-stack:
  added: []
  patterns:
    - "reconnecting-state Reconnect reuses the stream-error button styling (secondary variant, not accent) per UI-SPEC Color rule"
key-files:
  created: []
  modified:
    - dashboard/web/src/components/PodLogStreamer.tsx
    - dashboard/web/src/components/PodLogStreamer.test.tsx
    - cmd/dashboard/embed/dist/index.html
    - cmd/dashboard/embed/dist/assets/index-CDj1PDD4.js
decisions:
  - "No sse.ts change: reconnect() → doReconnect already cancels the pending timer + resets backoff, so force-resubscribe from the reconnecting state is already safe"
  - "Distinct data-testid (pod-log-reconnecting-reconnect) so the reconnecting button does not collide with the stream-error button's pod-log-reconnect testid"
metrics:
  duration: ~10m
  completed: 2026-07-09
  tasks: 2
  files: 4
status: complete
---

# Phase 37 Plan 12: Reconnecting-State Manual Reconnect Button Summary

Gap 37-G2 closure — the log drawer's reconnecting state now renders a manual Reconnect button (secondary variant) alongside the retained spinner + auto-backoff, wired to `useTaskLog.reconnect()`, so an operator can force an immediate re-subscribe without waiting out the exponential backoff.

## What Was Built

**Task 1 — Reconnecting-state manual Reconnect button (TDD)**
- Extended the `state === "reconnecting"` branch in `PodLogStreamer.tsx` to render a secondary-variant Reconnect button (`data-testid="pod-log-reconnecting-reconnect"`) below the existing Loader2 spinner + "Reconnecting to log stream…" copy.
- Button `onClick={() => reconnect()}` — `reconnect` was already destructured from `useTaskLog` at line 95; `doReconnect` (sse.ts:364-388) cancels the pending backoff timer and resets `attemptRef` before reopening, giving force-resubscribe semantics. No `sse.ts` change.
- Reused the exact stream-error Reconnect button styling (border, px-2 py-1, text-primary, hover:surface-raised, 12px, mt-2) so the two affordances read identically — secondary, NOT accent, per UI-SPEC Color rule.
- pod-gone (D-13 message-only) and stream-error (D-14) branches untouched.
- Added 5 tests to `PodLogStreamer.test.tsx` in a new "Gap 37-G2" describe block: button present in reconnecting state, button invokes `reconnect()` once, spinner retained, pod-gone buttonless regression, stream-error regression.

**Task 2 — Rebuild embedded SPA dist**
- Ran `make dashboard-frontend` (npm ci + build + full frontend suite + <500KB bundle gate), which regenerated `cmd/dashboard/embed/dist`.
- The old hashed asset `index-DgctWc3k.js` was replaced by `index-CDj1PDD4.js`; `index.html` updated to the new hash. Verified the new bundle contains `pod-log-reconnecting-reconnect` (grep count 1) — the change is embedded, not just in source.
- No hand-edits under `cmd/dashboard/embed/dist` — build output only.

## Verification

| Command | Result |
|---------|--------|
| `npx vitest run src/components/PodLogStreamer.test.tsx` | PASS — 21/21 tests (was 19 before; RED confirmed 2 new tests failing pre-implementation, GREEN after) |
| `grep -c 'pod-log-reconnecting-reconnect' PodLogStreamer.tsx` | 1 |
| `make dashboard-frontend` | PASS — 271/271 frontend tests across 32 files; dist copied into embed |
| `make verify-dashboard-freshness` | EXIT 0 — "PASS: cmd/dashboard/embed/dist/ matches a fresh SPA build"; "PASS: embedded bundle contains telemetry marker (panel-cache-efficiency)" |
| new bundle carries testid | `grep -c` on `cmd/dashboard/embed/dist/assets/index-CDj1PDD4.js` = 1 |

## Deviations from Plan

None — plan executed exactly as written. No `sse.ts` change was needed (as the plan predicted). No package installs.

Environment note: the dev host manages Node via asdf 0.19.0; `node`/`npx` were run with `/Users/justinsearles/.asdf/installs/nodejs/22.22.3/bin` on PATH. No `.tool-versions` file was created or left behind in the repo.

## Commits

- `5949f18` feat(37-12): add manual Reconnect button to log drawer reconnecting state
- `11d1335` chore(37-12): rebuild embedded SPA dist with reconnecting-state Reconnect button

## Self-Check: PASSED
- `dashboard/web/src/components/PodLogStreamer.tsx` — modified, committed in 5949f18
- `dashboard/web/src/components/PodLogStreamer.test.tsx` — modified, committed in 5949f18
- `cmd/dashboard/embed/dist` — rebuilt, committed in 11d1335
- Commits `5949f18` and `11d1335` present in git log
