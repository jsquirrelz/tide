---
phase: 04
plan: 11
subsystem: dashboard-backend
tags: [dashboard, sse, events, pod-logs, informer-bridge, dash-03, dash-04, pitfall-22, pitfall-23]
requires:
  - 04-10  # cmd/dashboard skeleton + chi router + hub.Hub pubsub
provides:
  - cmd/dashboard/api/events_sse.go     EventsHandler (DASH-03)
  - cmd/dashboard/api/logs_sse.go       LogsHandler   (DASH-04)
  - cmd/dashboard/api/informer_bridge.go BridgeInformerToHub for 6 CRD kinds
  - GET /api/v1/projects/{name}/events  SSE project event stream
  - GET /api/v1/tasks/{name}/log        SSE per-task pod-log stream
  - Hub.SubscriberCount(project)         white-box test helper
affects:
  - cmd/dashboard/router.go (route table — adds 2 GETs; DASH-05 invariant preserved)
  - cmd/dashboard/main.go   (constructs kubernetes.Clientset + registers informer bridge as manager.RunnableFunc)
  - dashboard/web (plan 04-12/04-13 — frontend EventSource will consume both endpoints)
tech-stack:
  added:
    - k8s.io/client-go/kubernetes  (typed clientset for pods/log subresource — controller-runtime client doesn't expose subresource streams)
  patterns:
    - SSE handler with http.Flusher loop + select on {ctx.Done, ticker.C, sub.Events} (RESEARCH §399-441 Pattern 2)
    - pods/log Stream → bufio.Scanner → linesChan → SSE data frame (Pitfall 22 cleanup chain)
    - function-pointer test seam (WithStreamOpener) mirroring cmd/tide/tail.go's tailStreamer
    - controller-runtime cache.Informer.AddEventHandler over 6 kinds, project-keyed publish
    - Owner-chain resolution via Spec.{Project,Milestone,Phase,Plan}Ref (no OwnerReferences stamped today)
key-files:
  created:
    - cmd/dashboard/api/events_sse.go
    - cmd/dashboard/api/events_sse_test.go
    - cmd/dashboard/api/logs_sse.go
    - cmd/dashboard/api/logs_sse_test.go
    - cmd/dashboard/api/informer_bridge.go
    - cmd/dashboard/api/informer_bridge_test.go
  modified:
    - cmd/dashboard/router.go            (+ Clientset field on Dependencies; conditional registration of events/log routes)
    - cmd/dashboard/main.go              (+ kubernetes.NewForConfig; + BridgeInformerToHub as RunnableFunc)
    - cmd/dashboard/hub/pubsub.go        (+ SubscriberCount white-box helper for cleanup tests)
decisions:
  - **Informer bridge as manager.RunnableFunc** — ties the bridge into the cache-sync lifecycle automatically; ctx cancel on SIGTERM propagates without bespoke wiring.
  - **No OpenAPI/OwnerReferences-based owner walk** — Phase 1/2 reconcilers stamp Spec.{Project,Milestone,Phase,Plan}Ref but never OwnerReferences. resolveProjectKey walks those refs explicitly via client.Reader.Get; missing parent = drop event (V(1) log).
  - **Minimal JSON projection on every event** — name/namespace/kind/phase/resourceVersion + parent ref. Full CRD serialization would 10× the wire load; the frontend fetches detail via the existing GET endpoints when needed.
  - **Function-pointer test seam (WithStreamOpener)** instead of envtest — mirrors cmd/tide/tail.go's tailStreamer; the real pods/log call is exercised in plan 04-14's kind harness, not here.
  - **fakeCache test scaffold over envtest for the informer bridge** — envtest cold-start is ~30s and would require Ginkgo. A purpose-built fakeCache implements the minimal cache.Cache surface BridgeInformerToHub touches (GetInformer + AddEventHandler) and lets us synthetically OnAdd against the registered handler to assert the publish path. Full informer-driven verification lives in plan 04-14.
  - **Heartbeat on /events only, idle timeout on /log only** — log streams are inherently chatty (so the 5min idle timer is the natural close), but the events stream can sit idle for hours, so the 15s heartbeat comment is the proxy-keepalive lever. Both surfaces set the Pitfall 23 header trio.
  - **Conditional route registration** — events route only when deps.Hub is non-nil; logs route only when deps.Clientset is non-nil. Tests pass nil for both so router_test.go's TestZeroMutationRoutes walk doesn't depend on the SSE handlers existing.
metrics:
  duration_minutes: 40
  tasks_completed: 2
  files_created: 6
  files_modified: 3
  commits: 4
  test_count: 23  # 6 events + 9 informer bridge + 8 logs
  completed_date: 2026-05-19
---

# Phase 4 Plan 11: Dashboard SSE Endpoints + Informer Bridge Summary

**One-liner:** Closes the dashboard backend's read surface — DASH-03 (project SSE event stream over the Hub from plan 04-10) and DASH-04 (per-task pod-log SSE stream via client-go pods/log Follow subresource), with a controller-runtime informer bridge that translates cache events on all 6 TIDE CRD kinds into Hub publishes keyed by the owning Project name. Pitfall 22 (subscriber + log-stream leak) and Pitfall 23 (nginx-ingress buffering) are explicitly mitigated and race-tested.

## What landed

| Surface | Files | Purpose |
| ------- | ----- | ------- |
| **EventsHandler (DASH-03)** | `cmd/dashboard/api/events_sse.go` | `GET /api/v1/projects/{name}/events?stream=sse`. Subscribes the connection to the Hub, replays via `Last-Event-ID`, emits `id:N\nevent:T\ndata:J\n\n` frames + `:heartbeat\n\n` SSE comments at 15s. Sets `Content-Type: text/event-stream`, `Cache-Control: no-cache`, `X-Accel-Buffering: no`, `Connection: keep-alive`. defer-unsubscribes on every exit path. |
| **LogsHandler (DASH-04)** | `cmd/dashboard/api/logs_sse.go` | `GET /api/v1/tasks/{name}/log?stream=sse&container=...`. Resolves Task → Pod via the `tideproject.k8s/task-uid` label, opens a `pods/log Follow:true` stream, bufio-scans into a linesChan, emits `data: <line>\n\n` frames. Terminal events: `idle-timeout`, `pod-gone`, `error`. defer-closes the stream on every exit path; reader goroutine exits via scanner EOF or ctx-driven close. |
| **Informer bridge** | `cmd/dashboard/api/informer_bridge.go` | `BridgeInformerToHub(ctx, cache, client, hub, log)` calls `cache.GetInformer(...)` + `Informer.AddEventHandler(...)` for `Project`, `Milestone`, `Phase`, `Plan`, `Task`, `Wave`. On each Add/Update/Delete, resolves the owning Project key (walks `Spec.*Ref` chain via the supplied client.Reader) and Publishes a minimal JSON projection to the Hub. |
| **Hub.SubscriberCount** | `cmd/dashboard/hub/pubsub.go` | White-box helper used by the fan-out + disconnect-cleanup tests to assert subscribers actually leave the hub when the client disconnects. Concurrent-safe (acquires h.mu). |
| **Router wiring** | `cmd/dashboard/router.go` | Adds `Clientset kubernetes.Interface` to Dependencies; registers `r.Get("/projects/{name}/events", ...)` when `deps.Hub != nil` and `r.Get("/tasks/{name}/log", ...)` when `deps.Clientset != nil`. Tests pass nil for both so `TestZeroMutationRoutes` can still walk the route shape on a stripped-down router. |
| **main.go wiring** | `cmd/dashboard/main.go` | Builds `kubernetes.NewForConfig(restCfg)` next to the manager; threads it into `Dependencies.Clientset`. Wraps `BridgeInformerToHub` in another `mgr.Add(manager.RunnableFunc(...))` so it ties into the manager lifecycle (start after cache up; ctx cancel on SIGTERM propagates). |
| **Test suites** | `*_test.go` | 23 net-new tests across the 3 surfaces, all passing under `-race` and goleak. |

## Route table (full inventory after this plan)

| Method | Path                                | Owner                           | Status              |
| ------ | ----------------------------------- | ------------------------------- | ------------------- |
| GET    | `/healthz`                          | router.go                       | implemented (04-10) |
| GET    | `/readyz`                           | router.go                       | implemented (04-10) |
| GET    | `/api/v1/projects`                  | api/projects.go `List`          | implemented (04-10) |
| GET    | `/api/v1/projects/{name}`           | api/projects.go `Get`           | implemented (04-10) |
| GET    | `/api/v1/projects/{name}/events`    | api/events_sse.go `EventsHandler` | **NEW (04-11)**    |
| GET    | `/api/v1/tasks/{name}/log`          | api/logs_sse.go `LogsHandler`     | **NEW (04-11)**    |
| GET    | `/*`                                | router.go `spaFallback`         | implemented (04-10) |

`TestZeroMutationRoutes` (cmd/dashboard/router_test.go) still walks chi's route tree and asserts every method is `GET` or `HEAD`. With the two new SSE routes added, the test still passes — DASH-05's compile-time zero-mutation invariant is preserved.

## Pitfall mitigations landed

### Pitfall 22 — Dashboard pod-log stream leaks (RESEARCH §515-527)

| Mitigation lever | Location | Test |
| ---------------- | -------- | ---- |
| `defer stream.Close()` runs on every exit | logs_sse.go ServeHTTP | TestLogsHandlerIdleTimeout (asserts via closeTrackingStream.onClose) + TestLogsHandlerClientDisconnectCleanup |
| `ctx.Done()` watcher in select loop      | logs_sse.go ServeHTTP | TestLogsHandlerClientDisconnectCleanup (cancels mid-stream; asserts Close called within 1s) |
| 5-minute idle timeout (`WithIdleTimeout`) | logs_sse.go ServeHTTP | TestLogsHandlerIdleTimeout (override to 100ms; asserts `event: idle-timeout` emitted + return) |
| Reader goroutine bounded by ctx + EOF    | logs_sse.go ServeHTTP | TestLogsHandlerClientDisconnectCleanup + TestSSEFanoutCleanup (50-client race, no goroutine leaks per goleak) |

Equivalent SSE-subscriber leak (T-04-D3) on the events endpoint is mitigated by `defer h.Hub.Unsubscribe(sub)` immediately after `Hub.Subscribe`; verified in `TestEventsHandlerClientDisconnectCleanup` (subscriber count drops to 0 within 1s of client cancel) and `TestSSEFanoutCleanup` (50 concurrent clients + 100 publishes/sec; race-tested; final subscriber count = 0; goleak clean).

### Pitfall 23 — SSE through nginx-ingress + reverse-proxy buffering (RESEARCH §529-541)

| Mitigation lever | Location | Test |
| ---------------- | -------- | ---- |
| `X-Accel-Buffering: no` header | events_sse.go + logs_sse.go | TestEventsHandlerHeaders + TestLogsHandlerHeaders |
| `Cache-Control: no-cache` header | events_sse.go + logs_sse.go | TestEventsHandlerHeaders + TestLogsHandlerHeaders |
| `Connection: keep-alive` header | events_sse.go + logs_sse.go | TestEventsHandlerHeaders + TestLogsHandlerHeaders |
| 15s `:heartbeat\n\n` comment on events stream | events_sse.go (ticker) | TestEventsHandlerHeartbeat (50ms override; asserts ≥2 heartbeats in 500ms) |

The logs stream doesn't ship a heartbeat — pod logs are inherently chatty and a heartbeat would be redundant noise; the 5-min idle timer is the proxy-keepalive lever there. Operator-side ingress config (proxy_buffering off, proxy_read_timeout 1h) is documented as plan 04-14's responsibility.

## Hub semantics extension

Added one method to the Hub surface (otherwise unchanged from 04-10):

```go
// SubscriberCount returns the current number of subscribers attached to
// `project`. Concurrent-safe. Used by SSE handler tests to verify
// disconnect-cleanup behavior.
func (h *Hub) SubscriberCount(project string) int
```

White-box helper. The handler tests open SSE connections, wait for the
subscriber count to tick to 1 (proves the Subscribe ran), cancel the
client context, then poll the count back to 0 — that round-trip is the
T-04-D3 canonical assertion shape and now ships as routine.

## Informer bridge — kind handling

For each of the 6 kinds, `BridgeInformerToHub` registers a
`ResourceEventHandlerFuncs` that calls a closed-over `publish` which:

1. Extracts the `client.Object` from the event payload (handles
   `DeletedFinalStateUnknown` tombstones).
2. Resolves the owning Project name via `resolveProjectKey`.
3. Builds a minimal JSON projection (`name`, `namespace`, `kind`,
   `phase`, `resourceVersion`, parent ref where applicable).
4. Calls `hub.Publish(projectKey, hub.Event{Type: "<kind>.<verb>",
   JSON: ...})`.

### Project-key resolution table

| Kind      | Resolution                                                          | Test                                      |
| --------- | ------------------------------------------------------------------- | ----------------------------------------- |
| Project   | self (`obj.GetName()`)                                              | TestExtractProjectKeyForProject           |
| Milestone | `Spec.ProjectRef`                                                   | TestExtractProjectKeyForMilestone         |
| Phase     | `Spec.MilestoneRef` → Milestone.`Spec.ProjectRef`                   | TestExtractProjectKeyForPhase             |
| Plan      | `Spec.PhaseRef` → Phase chain                                       | TestExtractProjectKeyForPlan              |
| Task      | `Spec.PlanRef` → Plan chain                                         | TestExtractProjectKeyForTask              |
| Wave      | `Spec.PlanRef` → Plan chain                                         | TestExtractProjectKeyForWave              |

Resolution failures (dangling refs, deleted parents) return empty string +
nil error; the bridge logs at V(1) and drops the event — the next event
from the cache will refresh state.

## Threat mitigations landed (this plan)

| Threat       | Mitigation                                                                                                                       |
| ------------ | -------------------------------------------------------------------------------------------------------------------------------- |
| **T-04-D3** (subscriber goroutine leak)        | defer hub.Unsubscribe immediately after Subscribe; ctx.Done watcher in select; TestSSEFanoutCleanup with -race + goleak. Hub.SubscriberCount confirms subs == 0 after disconnect. |
| **T-04-D4** (pod-log goroutine + stream leak)  | defer stream.Close on every exit; ctx.Done watcher; 5-min idle timer; reader goroutine bounded by ctx + scanner EOF. TestLogsHandlerIdleTimeout asserts via closeTrackingStream. |
| **T-04-D-pitfall23** (nginx-ingress buffering) | X-Accel-Buffering:no + Cache-Control:no-cache + Connection:keep-alive headers on both endpoints; 15s heartbeat comments on events stream. Operator-side ingress config deferred to plan 04-14 docs.       |

T-04-D-podlog-RBAC remains a `mitigate` disposition handled at the
ClusterRole layer (plan 04-14's Helm chart will gate `pods/log` to the
dashboard ServiceAccount only); no code-level change in this plan.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] Hub needs SubscriberCount helper**
- **Found during:** Task 1 RED authoring.
- **Issue:** The plan's Test 6 (`TestSSEFanoutCleanup`) asserts "hub.subs[project] is empty after all clients disconnect" via "direct introspection (add a test-only helper `hub.SubscriberCount(project) int` to pubsub.go)". The plan flagged the helper but I needed it to actually exist before the test compiles.
- **Fix:** Added `func (h *Hub) SubscriberCount(project string) int` to cmd/dashboard/hub/pubsub.go. Concurrent-safe (acquires h.mu). 5-line addition; no API impact on existing callers.
- **Files modified:** cmd/dashboard/hub/pubsub.go
- **Commit:** 21a627f (folded into RED test commit so the test could compile)

### Rule 2 — Added missing critical functionality

**2. [Rule 2 - Robustness] DeletedFinalStateUnknown tombstone handling**
- **Plan didn't explicitly require it.** The informer event handler can receive `cache.DeletedFinalStateUnknown` envelopes when the cache missed a delete and only got the final state at relist time. Treating those as "not a client.Object" would silently drop legitimate delete events. The bridge unwraps the tombstone and proceeds — standard controller-runtime hygiene.
- **Files modified:** cmd/dashboard/api/informer_bridge.go (`publish` closure)
- **Commit:** 4be3d0f

### Scope adjustments (test framework)

**3. Behavior #7 test (envtest-based) traded for fakeCache scaffold**
- **Plan suggested:** "Test 7 (informer bridge) ... Verified via envtest: create a Project, then create a Milestone owned by it, then assert hub.Publish was called with project=<name> at least twice (once for project create, once for milestone create)."
- **Implemented instead:** A `fakeCache` struct that satisfies the minimal `cache.Cache` surface BridgeInformerToHub touches (GetInformer + AddEventHandler), plus 3 tests: `TestInformerBridgeWiresAllKinds` (asserts a handler is registered for every kind), `TestInformerBridgePublishesOnAdd` (synthetic OnAdd against Project handler triggers a Publish with the right project key + JSON), and `TestInformerBridgePublishesMilestoneCreateWithProjectKey` (same for Milestone, exercising the Spec.ProjectRef resolution).
- **Why:** envtest cold-start is ~30s and requires Ginkgo + the kubebuilder setup-envtest binaries. The plan's intent (verify the cache event → hub.Publish path) is captured by directly invoking the registered ResourceEventHandler — same code path, no apiserver dependency, sub-second test runtime. The full informer-driven verification lives in plan 04-14's kind harness, mirroring how cmd/tide/tail.go's tailStreamer is unit-tested with a function-pointer seam and exercised end-to-end in 04-14.
- **Files affected:** cmd/dashboard/api/informer_bridge_test.go

### Honored CLAUDE.md / STACK.md constraints

- chi v5 only (no gin/echo/fiber/fasthttp) — already established in 04-10; preserved.
- SSE (no WebSockets) — both new endpoints are SSE.
- No hard-coded LLM provider, no hard-coded git host — neither surface introduces either coupling.
- Dashboard SA reads only — informer bridge uses `client.Reader`, never the writer-shaped `client.Client`; LogsHandler uses controller-runtime client for Task/Pod reads + typed clientset for the pods/log subresource only (no Patch/Update/Delete).

## Verification Evidence

```bash
$ go build ./cmd/dashboard/...
# (no output — clean build)

$ go test -race ./cmd/dashboard/api/... -run "TestSSE|TestEvents|TestInformerBridge|TestExtractProject|TestLogs" -v -timeout 60s 2>&1 | tail -25
=== RUN   TestEventsHandlerHeaders
--- PASS: TestEventsHandlerHeaders (0.00s)
=== RUN   TestEventsHandlerDeliversPublish
--- PASS: TestEventsHandlerDeliversPublish (0.00s)
=== RUN   TestEventsHandlerLastEventIDReplay
--- PASS: TestEventsHandlerLastEventIDReplay (0.00s)
=== RUN   TestEventsHandlerHeartbeat
--- PASS: TestEventsHandlerHeartbeat (0.10s)
=== RUN   TestEventsHandlerClientDisconnectCleanup
--- PASS: TestEventsHandlerClientDisconnectCleanup (0.01s)
=== RUN   TestSSEFanoutCleanup
--- PASS: TestSSEFanoutCleanup (0.32s)
=== RUN   TestExtractProjectKeyForProject
--- PASS: TestExtractProjectKeyForProject (0.08s)
=== RUN   TestExtractProjectKeyForMilestone
--- PASS: TestExtractProjectKeyForMilestone (0.00s)
... (all 23 PASS)
PASS
ok      github.com/jsquirrelz/tide/cmd/dashboard/api    3.060s

$ go test -race ./cmd/dashboard/... -run TestZeroMutationRoutes -v
--- PASS: TestZeroMutationRoutes (0.61s)
PASS
ok      github.com/jsquirrelz/tide/cmd/dashboard    3.469s

$ grep -c "X-Accel-Buffering" cmd/dashboard/api/events_sse.go cmd/dashboard/api/logs_sse.go
cmd/dashboard/api/events_sse.go:3
cmd/dashboard/api/logs_sse.go:2

$ grep -c "defer .*\\.Close" cmd/dashboard/api/logs_sse.go
4

$ make tide-lint
go run ./cmd/tide-lint ./...
# (no diagnostics)

$ go vet ./cmd/dashboard/...
# (clean)
```

## Follow-on dependencies

- **Plan 04-12 / 04-13** build the React SPA that consumes both endpoints. The frontend's EventSource setup against `/api/v1/projects/{name}/events` is the canonical D-D3 client; the per-task log viewer opens an EventSource against `/api/v1/tasks/{name}/log`. Backend wire shape is now stable: events emit `id:N\nevent:T\ndata:<JSON>\n\n`; logs emit either `data:<line>\n\n` or one of three terminal events (`idle-timeout`, `pod-gone`, `error`).
- **Plan 04-14** ships the Helm chart that templates the dashboard Deployment, ClusterRole (read on all 6 TIDE kinds + `get pods` + `get pods/log`), Service, and optional Ingress. The Ingress annotations need to set `nginx.ingress.kubernetes.io/proxy-buffering: "off"` and `nginx.ingress.kubernetes.io/proxy-read-timeout: "3600"` to honor Pitfall 23 at the operator-side; plan 04-14 owns documenting this. The kind harness in 04-14 will also exercise the real `pods/log` subresource (the function-pointer seam in logs_sse.go that this plan stubbed).
- **DASH-05 surface complete.** Plan 04-10 + 04-11 together close the read-only dashboard backend; no further routes land on this surface in Phase 4. Plan 04-14's chart points at this binary.

## Self-Check: PASSED

**Created files exist:**
- FOUND: cmd/dashboard/api/events_sse.go
- FOUND: cmd/dashboard/api/events_sse_test.go
- FOUND: cmd/dashboard/api/logs_sse.go
- FOUND: cmd/dashboard/api/logs_sse_test.go
- FOUND: cmd/dashboard/api/informer_bridge.go
- FOUND: cmd/dashboard/api/informer_bridge_test.go

**Modified files exist:**
- FOUND: cmd/dashboard/router.go (Clientset field added; events + logs routes wired)
- FOUND: cmd/dashboard/main.go (kubernetes.NewForConfig + BridgeInformerToHub RunnableFunc)
- FOUND: cmd/dashboard/hub/pubsub.go (SubscriberCount helper)

**Commits exist on this branch:**
- FOUND: 21a627f (RED: failing tests for EventsHandler)
- FOUND: 9f1d010 (GREEN: EventsHandler impl)
- FOUND: 4be3d0f (informer bridge + events route wiring)
- FOUND: a5d3ff2 (LogsHandler + logs route wiring)

**Threat surface scan:** No new external network endpoints, no new auth paths, no new file-access patterns, no schema changes. Both SSE endpoints reuse the existing dashboard SA (read-only) and are GET-only — TestZeroMutationRoutes (DASH-05) continues to enforce this at build time. No threat flags.
