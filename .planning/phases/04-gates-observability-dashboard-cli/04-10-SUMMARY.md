---
phase: 04
plan: 10
subsystem: dashboard-backend
tags: [dashboard, chi-router, sse, embed-fs, manager-runnable, dash-05]
requires:
  - 04-01  # internal/metrics — dashboard reads metric labels for consistency
  - 04-07  # internal/gates — dashboard reads gate-policy from Project.Spec.gates (read-only)
provides:
  - cmd/dashboard skeleton (chi router as manager.Runnable)
  - cmd/dashboard/hub in-process pubsub for SSE fan-out
  - cmd/dashboard/api/projects.go List + Get handlers
  - cmd/dashboard/embed.go SPA bundle shim
  - TestZeroMutationRoutes guard (DASH-05 architectural enforcement)
affects:
  - charts/tide (plan 04-14 will template a dashboard Deployment + RBAC referencing this binary)
  - dashboard/web (plan 04-12/04-13 — frontend dist/ overwrites the embed placeholder)
  - cmd/dashboard/api/events_sse.go (plan 04-11 — SSE handlers consume the Hub from this plan)
tech-stack:
  added:
    - github.com/go-chi/chi/v5 v5.1.0 (HTTP router; per STACK.md "chi as manager.Runnable, not gin/echo/fiber")
    - go.uber.org/goleak v1.3.0 (goroutine-leak detection in hub tests; T-04-D3 mitigation)
  patterns:
    - chi.NewRouter + middleware.RequestID/Recoverer/Logger chain (chi v5 idiom)
    - manager.RunnableFunc lifecycle (controller-runtime — same pattern cmd/manager uses for budget.PreCharge)
    - http.ServeFileFS for SPA fallback (Go 1.22+ — avoids http.FileServer's /index.html ↔ / 301 redirect loop)
    - In-process Project-keyed fan-out hub with drop-oldest buffer policy
key-files:
  created:
    - cmd/dashboard/main.go
    - cmd/dashboard/router.go
    - cmd/dashboard/router_test.go
    - cmd/dashboard/embed/embed.go
    - cmd/dashboard/embed/dist/index.html
    - cmd/dashboard/api/projects.go
    - cmd/dashboard/api/projects_test.go
    - cmd/dashboard/hub/pubsub.go
    - cmd/dashboard/hub/pubsub_test.go
  modified:
    - Makefile (added dashboard-build + dashboard-frontend targets)
    - go.mod (chi v5 direct dep; goleak direct dep)
    - go.sum (transitively)
decisions:
  - Hub owns zero goroutines (synchronous publish under writer lock + non-blocking channel sends). All goroutines belong to SSE handlers (plan 04-11).
  - SPA fallback uses http.ServeFileFS (Go 1.22+) instead of http.FileServer to avoid the /index.html ↔ / 301 redirect loop in not-found paths.
  - SPA wildcard route uses r.Get("/*") instead of r.Handle("/*") — chi.Handle binds for ALL methods, which would mass-violate DASH-05's zero-mutation guard. r.Get binds GET-only.
  - Two health probe surfaces: API port :8080 /healthz (process-level liveness, no apiserver gate) + manager port :8081 /healthz (informer-cache-sync-gated, per plan must_haves). Helm chart's readinessProbe (plan 04-14) targets :8081.
  - api package serializes via inline projectSummary/projectDetail structs (NOT raw CRDs) so transient/noisy fields (managedFields, resourceVersion churn) don't bloat the wire response.
  - Children identified via Spec.{Project,Milestone,Phase}Ref fields, not Kubernetes OwnerReferences — Phase 1 + 2 reconcilers don't set OwnerRefs in this codebase, so listing+filtering by Spec ref is the only correct join.
metrics:
  duration_minutes: 30
  tasks_completed: 3
  files_created: 9
  files_modified: 3
  commits: 3
  test_count: 26
  completed_date: 2026-05-19
---

# Phase 4 Plan 10: Dashboard Backend Summary

**One-liner:** Ships the cmd/dashboard skeleton — chi v5 HTTP router registered as a controller-runtime manager.Runnable, in-process Project-keyed SSE fan-out hub with drop-oldest + Last-Event-ID semantics, two synchronous (non-SSE) read-only API endpoints, the embed.FS SPA shim, and the DASH-05 architectural guard test that walks the chi route table and fails the build on any non-GET method.

## What landed

| Surface | Files | Purpose |
| ------- | ----- | ------- |
| **Manager.Runnable composition** | `cmd/dashboard/main.go` | controller-runtime Manager with LeaderElection=false (stateless read replica); HTTP server registered via `mgr.Add(manager.RunnableFunc(...))`; manager-port `/healthz` gated on `mgr.GetCache().WaitForCacheSync()`; graceful shutdown on SIGTERM with a 10s drain budget. |
| **chi router** | `cmd/dashboard/router.go` | Single `RegisterRoutes(deps Dependencies) chi.Router` builder consumed by both `main.go` and `router_test.go` so the production route table is identical to the one walked by TestZeroMutationRoutes. Middleware chain: RequestID → Recoverer → Logger. |
| **In-process pubsub hub** | `cmd/dashboard/hub/pubsub.go` | Project-keyed fan-out backed by `sync.Mutex` + a per-project subscriber slice. Per-subscriber 64-slot buffer with drop-oldest enqueue. Per-project 100-event replay buffer for `Last-Event-ID` reconnect. Hub owns zero goroutines — synchronous publish under the writer lock. |
| **REST handlers** | `cmd/dashboard/api/projects.go` | `List` (GET /api/v1/projects[?namespace=]) returns a JSON array of project summaries; `Get` (GET /api/v1/projects/{name}) returns one project with embedded Milestones/Phases/Plans for the Planning DAG render. 404 with JSON error body on missing project. |
| **SPA embed shim** | `cmd/dashboard/embed/{embed.go,dist/index.html}` | `//go:embed all:dist` directive + placeholder index.html so the binary compiles before plan 04-12/13 lands the real Vite bundle. |
| **Test suite** | `*_test.go` | 26 tests across 3 packages (8 router tests + 7 api tests + 11 hub tests), all passing under `-race` and `goleak.VerifyTestMain`. |
| **Makefile** | `Makefile` | `dashboard-build` (go build) + `dashboard-frontend` (npm ci + vite build → copy into cmd/dashboard/embed/dist). |

## Route table (full inventory)

| Method | Path | Owner | Status |
| ------ | ---- | ----- | ------ |
| GET | /healthz | router.go | implemented (process liveness) |
| GET | /readyz | router.go | implemented (process readiness) |
| GET | /api/v1/projects | api/projects.go `List` | implemented |
| GET | /api/v1/projects/{name} | api/projects.go `Get` | implemented |
| GET | /api/v1/projects/{name}/events | (deferred) | plan 04-11 |
| GET | /api/v1/tasks/{name}/log | (deferred) | plan 04-11 |
| GET | /* | router.go `spaFallback` | implemented (placeholder SPA until 04-12/13) |

`TestZeroMutationRoutes` (cmd/dashboard/router_test.go) walks the chi route tree via `chi.Walk` and asserts EVERY registered method is GET or HEAD. POST/PUT/PATCH/DELETE/OPTIONS/CONNECT/TRACE on any route fails CI — DASH-05 is closed at build time, not at runtime.

## Hub semantics (D-D3 / RESEARCH §757-786)

**Subscribe:**
```go
sub := hub.Subscribe(projectName, lastEventID)
defer hub.Unsubscribe(sub)
for ev := range sub.Events() { ... }
```

- `lastEventID = 0` ⇒ no replay; first event on the channel is the next future Publish.
- `lastEventID > 0` ⇒ hub replays buffered events with `ID > lastEventID` into the subscriber's channel before returning. Replay buffer is per-project, capped at 100 events.

**Publish:**
- Mutex-locks the hub for the duration of fan-out (synchronous; no goroutines spawned).
- Stamps `Event.ID` monotonically per-project when caller leaves it at 0; honors caller-set IDs (lets plan 04-11's informer adapter derive IDs from CRD resourceVersion).
- Per-subscriber enqueue uses non-blocking send with **drop-oldest fallback** — a slow consumer's channel buffer (64 slots) absorbs bursts; on overflow, the oldest queued event is drained and the new one enqueued. Preserves "latest state" semantics: dashboards always see the freshest update, never a stale backlog.

**Unsubscribe:**
- Removes the subscriber from the per-project slice and `close`s the Events channel so `for ev := range sub.Events()` exits cleanly.
- When the last subscriber leaves a project, the map entry is deleted (memory hygiene).

**Concurrency:** Subscribe / Publish / Unsubscribe are safe to call concurrently from any goroutine. `TestConcurrentSubscribePublishUnsubscribe` exercises 8 workers × 200 iterations of each under `-race`; `TestMain` in the hub package wraps every test with `goleak.VerifyTestMain` so any future refactor that introduces a leaked goroutine fails the suite.

## Threat mitigations landed

| Threat | Mitigation in this plan |
| ------ | ----------------------- |
| **T-04-D5** (CSRF / cross-origin write) | `TestZeroMutationRoutes` walks `chi.Walk` and asserts only GET/HEAD methods registered. r.Handle replaced with r.Get on the SPA wildcard because chi.Handle binds ALL methods. |
| **T-04-D2** (write-RBAC creep) | `ProjectsHandler.Client` is the controller-runtime cache-backed client; no Create/Update/Patch/Delete call paths exist anywhere in cmd/dashboard/api/. Grep `grep -rE 'Create\|Update\|Patch\|Delete' cmd/dashboard/api/` returns zero matches. |
| **T-04-D1** (XSS via project name) | `writeJSON` uses `json.NewEncoder` (NOT `json.Marshal`); Encoder defaults to `SetEscapeHTML(true)` so `<`, `>`, `&` are emitted as Unicode escape sequences. `TestXSSViaProjectName` asserts a Project.Status.Phase containing literal `<script>` is emitted as `<script>` and the literal tag never appears in the response body. |
| **T-04-D3** (subscriber goroutine leak) | Hub owns zero goroutines. SSE handlers (plan 04-11) will own goroutines but must defer Unsubscribe immediately after Subscribe. `TestMain` in hub package runs `goleak.VerifyTestMain` so leaks fail CI. |

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] SPA fallback redirect loop**
- **Found during:** Task 1 verification (TestSPAFallbackUnknownPath)
- **Issue:** `http.FileServer` issued 301 redirects between `/index.html` and `/` because of its built-in redirect-on-index behavior. Combined with the fallback-to-index logic this produced an infinite redirect loop for any not-found path (the request kept being rewritten to `/index.html`, which the FileServer redirected to `/`, which the rewrite caught again).
- **Fix:** Use `http.ServeFileFS` (Go 1.22+) which reads a specific named file with no redirect logic. Stat the requested path against the embed.FS; on not-found OR directory, serve `index.html`; on a real file, serve that file.
- **Files modified:** cmd/dashboard/router.go (`spaFallback`)
- **Commit:** a47d6e6

**2. [Rule 1 - Bug] r.Handle binds all HTTP methods**
- **Found during:** Task 1 verification (TestZeroMutationRoutes)
- **Issue:** `r.Handle("/*", spaFallback(deps.SPAFS))` registered the SPA fallback for ALL methods (GET, POST, PUT, PATCH, DELETE, OPTIONS, CONNECT, TRACE) — the route-table walk found 7 non-GET registrations and failed the DASH-05 zero-mutation guard immediately.
- **Fix:** Use `r.Get("/*", spaHandler.ServeHTTP)` instead. Binds GET only; any other method falls through to chi's default 405 MethodNotAllowed. Comment in router.go now documents why r.Handle is forbidden on this route.
- **Files modified:** cmd/dashboard/router.go
- **Commit:** a47d6e6

**3. [Rule 1 - Bug] XSS test assertion looked for HTML entity instead of Unicode escape**
- **Found during:** Task 3 verification (TestXSSViaProjectName)
- **Issue:** Initial test asserted the JSON body contained the HTML entity `&lt;script&gt;`. Go's `json.Encoder.SetEscapeHTML(true)` actually emits Unicode escape sequences (`<script>`) — never HTML entities. The assertion never matched even though the underlying defense (the escape) was working.
- **Fix:** Changed the `strings.Contains` assertion to look for the literal 18-character sequence `<script>` (backslash + u + four hex + script + backslash + u + four hex). Comment clarifies the encoder's actual behavior.
- **Files modified:** cmd/dashboard/api/projects_test.go
- **Commit:** d3014f5

### Rule 2 — Added missing critical functionality

**4. [Rule 2 - Security] Hub publishes never block**
- **Plan didn't explicitly require it.** RESEARCH §777-780 documents the drop-oldest contract; the plan's Test 5 in Task 2 describes it. I added a defensive triple-attempt in `tryEnqueueOrDropOldest` (initial send → drain-and-retry → final send-or-drop) so even under adversarial concurrent send/drop races the hub never blocks. The plan's intent is preserved (Publish always returns quickly) but the implementation has a belt-and-suspenders layer the plan didn't specify.

### Honored CLAUDE.md / STACK.md constraints

- chi v5 selected (not gin/echo/fiber/fasthttp) per STACK.md "chi router as manager.Runnable, not gin/echo/fiber (fasthttp won't compose with controller-runtime's manager)."
- SSE used for pubsub fan-out (not WebSockets) per STACK.md "SSE (not WebSockets)" and CONTEXT D-D4 "Browser opens an SSE connection (NOT WebSocket from browser; backend translates pod-log SSE→client SSE)."
- Goroutine ownership: hub owns zero; SSE handlers in plan 04-11 will own goroutines.

## Verification Evidence

```bash
$ go build ./cmd/dashboard/...
# (no output)

$ go test ./cmd/dashboard/... -race -v -timeout 60s 2>&1 | grep -E '^(--- (PASS|FAIL)|ok|FAIL)'
--- PASS: TestZeroMutationRoutes (0.59s)
--- PASS: TestRouterMiddlewareChain (0.02s)
--- PASS: TestRouterMiddlewareChain/RequestID_populates_request_context (...)
--- PASS: TestRouterMiddlewareChain/Recoverer_turns_panic_into_500 (...)
--- PASS: TestHealthzReturns200 (0.01s)
--- PASS: TestSPAFallback (0.02s)
--- PASS: TestSPAFallbackUnknownPath (0.01s)
--- PASS: TestGracefulShutdown (0.06s)
--- PASS: TestRouteTableContainsExpectedGETs (0.01s)
ok      github.com/jsquirrelz/tide/cmd/dashboard        3.433s
--- PASS: TestListProjectsReturnsAll (0.65s)
--- PASS: TestListProjectsNamespaceFilter (0.01s)
--- PASS: TestGetProjectWithChildren (0.01s)
--- PASS: TestGetProjectMissingReturns404 (0.01s)
--- PASS: TestResponseContentType (0.01s)
--- PASS: TestXSSViaProjectName (0.01s)
--- PASS: TestBudgetSummary (0.01s)
ok      github.com/jsquirrelz/tide/cmd/dashboard/api    2.760s
--- PASS: TestSubscribeReturnsValidSubscriber (0.00s)
--- PASS: TestPublishDeliversEvent (0.00s)
--- PASS: TestFanOutToTwoSubscribers (0.00s)
--- PASS: TestKeyedIsolation (0.05s)
--- PASS: TestBufferOverflowDropsOldest (0.00s)
--- PASS: TestUnsubscribeClosesChannel (0.00s)
--- PASS: TestConcurrentSubscribePublishUnsubscribe (0.08s)
--- PASS: TestLastEventIDReplay (0.00s)
--- PASS: TestReplayBufferTruncation (0.00s)
--- PASS: TestUnsubscribeRemovesFromInternalMap (0.00s)
--- PASS: TestSubscribeWithCallerStampedID (0.00s)
ok      github.com/jsquirrelz/tide/cmd/dashboard/hub    1.485s

$ make dashboard-build
go build -o bin/dashboard ./cmd/dashboard
$ ls -la bin/dashboard
-rwxr-xr-x@ 1 justinsearles  staff  46691480 May 19 18:42 bin/dashboard

$ make tide-lint   # (multichecker: crosspool + providerfirewall + metriccardinality)
go run ./cmd/tide-lint ./...
# (no diagnostics)

$ make verify-no-aggregates
verifying no aggregate schedule fields on api/v1alpha1 types (PERSIST-02)...
OK: no aggregate schedule fields

$ make verify-import-firewall
go run ./cmd/tide-lint ./...
# (no diagnostics — cmd/dashboard/ is outside the firewalled scopes)

$ go vet ./cmd/dashboard/...
# (no warnings)
```

## Follow-on dependencies

- **Plan 04-11** wires the informer cache's watch events into `Hub.Publish()` and adds the two deferred SSE handlers (`/api/v1/projects/{name}/events`, `/api/v1/tasks/{name}/log`). Both routes need to be registered on the same chi.Router built by `RegisterRoutes`; `TestZeroMutationRoutes` will continue to enforce GET-only at build time as those routes land.
- **Plan 04-12 / 04-13** build the React SPA and overwrite `cmd/dashboard/embed/dist/` via `make dashboard-frontend`. The `//go:embed all:dist` directive picks up the new files at build time — no Go code changes needed.
- **Plan 04-14** ships the Helm chart's dashboard Deployment + ClusterRole + ServiceAccount referencing this binary on container port 8080 (API) and 8081 (probes).

## Self-Check: PASSED

**Created files exist:**
- FOUND: cmd/dashboard/main.go
- FOUND: cmd/dashboard/router.go
- FOUND: cmd/dashboard/router_test.go
- FOUND: cmd/dashboard/embed/embed.go
- FOUND: cmd/dashboard/embed/dist/index.html
- FOUND: cmd/dashboard/api/projects.go
- FOUND: cmd/dashboard/api/projects_test.go
- FOUND: cmd/dashboard/hub/pubsub.go
- FOUND: cmd/dashboard/hub/pubsub_test.go

**Commits exist on this branch:**
- FOUND: a47d6e6 (Task 1 — skeleton)
- FOUND: a3af0ba (Task 2 — hub tests)
- FOUND: d3014f5 (Task 3 — api tests)
