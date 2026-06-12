---
phase: 15-paper-cuts
plan: "06"
subsystem: dashboard-api
tags: [sse, waves, aggregate, informer, backend]
one-liner: "waves.snapshot SSE event derived from label-selector queries over Tasks, delivered on subscribe and on Task change over the existing project-events channel"
dependency-graph:
  requires: []
  provides: [waves.snapshot-sse-event, computeRunningWaves-helper, WithCacheReader-option]
  affects: [cmd/dashboard/api, cmd/dashboard/router.go]
tech-stack:
  added: []
  patterns:
    - "computeRunningWaves: label-selector List + in-memory group-by (planRef, wave-index)"
    - "isRunningPhase predicate: {Running, Dispatching} membership"
    - "WithCacheReader functional option on EventsHandler (nil-safe)"
key-files:
  created:
    - cmd/dashboard/api/waves.go
    - cmd/dashboard/api/waves_test.go
  modified:
    - cmd/dashboard/api/informer_bridge.go
    - cmd/dashboard/api/informer_bridge_test.go
    - cmd/dashboard/api/events_sse.go
    - cmd/dashboard/api/events_sse_test.go
    - cmd/dashboard/router.go
decisions:
  - "labelProject/labelWaveIndex consts defined in waves.go (package api) separate from cmd/tide/inspect_wave_run.go to avoid import cycle — cmd/tide is a main package"
  - "marshalWavesSnapshot inlined as json.Marshal(snap) rather than a named helper — single call site, no abstraction needed"
  - "WithCacheReader accepts client.Reader (not client.Client) — the read-only interface is sufficient and matches the informer_bridge pattern"
  - "T-15-17 accepted: no debounce on re-derivation per event; cache-backed List is O(tasks-in-namespace) in-memory, no apiserver round-trip"
metrics:
  duration: "~15 minutes"
  completed: "2026-06-12"
  tasks_completed: 2
  files_created: 2
  files_modified: 5
requirements: [CUTS-06]
---

# Phase 15 Plan 06: waves.snapshot Backend Summary

waves.snapshot SSE event derived from label-selector queries over Tasks, delivered on subscribe and on Task change over the existing project-events channel — server half of CUTS-06 (UI-SPEC C3/C5).

## What Was Built

### Task 1: computeRunningWaves aggregate helper + payload types

`cmd/dashboard/api/waves.go` exports the locked UI-SPEC C3/C5 payload types and the `computeRunningWaves(ctx, cli, ns, projectName)` helper:

- `RunningWaveTask{Name, Status}` / `RunningWave{PlanName, WaveIndex, Tasks}` / `WavesSnapshot{Waves}` — JSON field names are exact matches to the UI-SPEC C3 wire contract
- Label-selector query on `tideproject.k8s/project` (cache-backed `client.Reader`); groups by `(Spec.PlanRef, tideproject.k8s/wave-index)`
- `isRunningPhase` predicate: `{Running, Dispatching}`; wave included iff `anyRunning`
- Sort: plan name asc, wave index numeric asc, task name asc within wave
- `WavesSnapshot.Waves` is always a non-nil `[]RunningWave` so zero result serializes as `[]` not `null`
- Label constants (`labelProject`, `labelWaveIndex`) mirror `cmd/tide/inspect_wave_run.go` vocabulary

`cmd/dashboard/api/waves_test.go` covers all four behaviors with fake-client fixtures.

### Task 2: Wire emission + snapshot-on-subscribe

**informer_bridge.go** — after each `task.{create,update,delete}` event publish, derives and publishes a `waves.snapshot` event to the same project hub key. Marshal failures are logged at V(1) and skipped (never panic). T-15-17 disposition documented in a code comment.

**events_sse.go** — adds `cacheReader client.Reader` field + `WithCacheReader(r)` functional option (nil-safe: no behavior change when absent, preserving all existing constructors). In `ServeHTTP`, immediately after `Hub.Subscribe` and before the event loop, when `cacheReader != nil`: derives the aggregate and writes an `event: waves.snapshot\ndata: ...\n\n` frame directly to the response, without a hub sequence ID (it is a synthetic out-of-band frame, not replay-eligible).

**router.go** — wires `WithCacheReader(deps.Client)` at the `NewEventsHandler` construction site alongside the existing `WithClient(deps.Client)`.

## Invariants Verified

- `go test ./cmd/dashboard/... -count=1` exits 0
- `make verify-no-aggregates` exits 0 — no aggregate fields added to CRD status
- `TestZeroMutationRoutes` passes — no new REST route registered
- `gofmt -l cmd/dashboard` prints nothing

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] `contains` function redeclaration**
- **Found during:** Task 1 (RED phase, compile error)
- **Issue:** `waves_test.go` declared a `contains()` helper that collided with the existing `contains()` in `tasks_test.go` (same package)
- **Fix:** Renamed to `containsStr()` in `waves_test.go`
- **Files modified:** `cmd/dashboard/api/waves_test.go`
- **Commit:** 99ecbaa

No other deviations — plan executed exactly as written.

## Known Stubs

None. The aggregate is fully wired: label-selector queries live, snapshot delivered on subscribe and on every Task change.

## Threat Flags

No new threat surfaces beyond those in the plan's threat model. T-15-16 (cross-project leakage) is mitigated by the `client.MatchingLabels{labelProject: projectName}` filter, asserted by Test 4. T-15-19 (waves in CRD status) mitigated and gate green.

## Self-Check

| Artifact | Status |
|----------|--------|
| `cmd/dashboard/api/waves.go` | FOUND |
| `cmd/dashboard/api/waves_test.go` | FOUND |
| `cmd/dashboard/api/informer_bridge.go` (waves.snapshot) | FOUND |
| `cmd/dashboard/api/events_sse.go` (WithCacheReader) | FOUND |
| `cmd/dashboard/router.go` (WithCacheReader wired) | FOUND |
| Commit 99ecbaa (test RED) | FOUND |
| Commit 954110f (feat GREEN) | FOUND |
| Commit 576db1d (feat wiring) | FOUND |
| `go test ./cmd/dashboard/...` | PASS |
| `make verify-no-aggregates` | PASS |
| `TestZeroMutationRoutes` | PASS |
| `gofmt` clean | PASS |

## Self-Check: PASSED
