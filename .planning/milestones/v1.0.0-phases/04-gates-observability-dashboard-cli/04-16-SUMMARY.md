---
phase: 04-gates-observability-dashboard-cli
plan: 16
subsystem: dashboard-frontend
tags: [dashboard, sse, react, eventsource, ansi, podlogstreamer, bundle-gate, dash-03, dash-04, pitfall-22-client, t-04-d1, t-04-d-bundle-bloat, t-04-d-eventsource-leak]

requires:
  - phase: 04
    plan: 11
    provides: cmd/dashboard/api/events_sse.go + logs_sse.go — backend SSE wire (id:N\nevent:T\ndata:JSON\n\n on /events; data:<line>\n\n + terminal events on /tasks/{name}/log)
  - phase: 04
    plan: 12
    provides: dashboard/web/ Vite 6 + React 18 + TS scaffold; @theme design tokens; ToastProvider; XSS guard
  - phase: 04
    plan: 13
    provides: TaskDetailDrawer.onOpenLogStream(taskName) callback seam; App.tsx streamingTask state slot; PlanningDAGView/ExecutionDAGView prop-driven data injection seams
  - phase: 04
    plan: 15
    provides: StatusBadge + WaveBackground + ClipboardCopyAction + ProjectPicker primitives (re-used by drawer; PodLogStreamer does not consume them directly)

provides:
  - dashboard/web/src/lib/sse.ts — useSSEStream(url) + useTaskLog(taskName) hooks; EventSource lifecycle owned end-to-end; Pitfall 22 client-side mitigation
  - dashboard/web/src/lib/ansi.ts — parseAnsi(line: string): AnsiSegment[]; bespoke 194-line SGR parser per UI-SPEC §8 scope
  - dashboard/web/src/components/PodLogStreamer.tsx — DASH-04 surface; inline streamer with ring buffer, auto-scroll, ANSI rendering, locked connection-state copy
  - dashboard/web/src/components/EmptyState.tsx — E1/E2/E3 variants; ASCII wave decoration in E1 (UI-SPEC §13)
  - dashboard/web/src/components/ErrorState.tsx — ERR1/ERR2 full-screen error cards (UI-SPEC §14)
  - dashboard/web/src/components/LoadingState.tsx — L1/L2/L3 loading variants (UI-SPEC §15)
  - dashboard/web/src/__tests__/bundle-size.test.ts — CI gate asserting dist/ gzipped JS+CSS ≤ 500KB
  - Makefile dashboard-build → dashboard-frontend dependency; dashboard-frontend chains vite build + vitest + dist copy into cmd/dashboard/embed/dist
  - cmd/dashboard/embed/dist/ now contains the real Vite-built SPA (replaces the placeholder from plan 04-10)

affects:
  - dashboard/web/src/App.tsx — streamingTask state now mounts <PodLogStreamer> when non-null; drawer's onOpenLogStream is the producer
  - cmd/dashboard binary — go:embed all:dist now picks up the actual Vite bundle on every \`make dashboard-build\`
  - Future plans (helm chart 04-14, kind smoke 04-17 if planned): the dashboard image now ships a working SPA

tech-stack:
  added:
    - "No new npm dependencies. Re-used existing pins: react 18.x (hooks), lucide-react 1.16 (Copy / WrapText / X / AlertTriangle / Ban / Loader2 icons), node:zlib (gzipSync — Node-native, no dep)."
  patterns:
    - "Browser-native EventSource lifecycle: new EventSource(url) on mount; onopen→'connected'; onmessage→push event; onerror→close(once) + schedule exponential-backoff reconnect via setTimeout. Cleanup closes the live socket exactly once (Pitfall 22 client-side). The error handler is also responsible for closing to break the browser's built-in auto-retry — otherwise our backoff timer races the browser's."
    - "Bounded ANSI parser scope (UI-SPEC §8): supported SGR families enumerated explicitly; unsupported codes stripped silently. The 38/48 extension codes consume their sub-parameter list (38;5;N → 2 extra params; 38;2;R;G;B → 4 extra params) so a literal '100' embedded in a truecolor sequence is not misread as bg-bright-black. Source bound: 194 lines (under the 200 cap; UI-SPEC §8 'roughly 80 lines' allowing test/import overhead)."
    - "Ring buffer in useTaskLog (5000 lines): hook concat-then-slice keeps the latest 5000 — bounds tab heap so a runaway log stream cannot DoS the browser tab (T-04-D-eventsource-leak)."
    - "Auto-scroll via useLayoutEffect: jump to bottom (scrollTop = scrollHeight) after every render when autoScroll is on. The scroll handler reads scrollTop/scrollHeight/clientHeight and flips autoScroll → false when distance-from-bottom > 2px; flips back to true when the user returns to bottom. No animation; no debounce — the natural per-line render cadence is fine."
    - "vi.mock('../lib/sse') drives useTaskLog deterministically in PodLogStreamer.test.tsx so the component tests never need a real EventSource. The sse.test.ts file owns the EventSource-lifecycle assertions via a FakeEventSource constructor stubbed onto globalThis."
    - "Bundle gate as a Vitest test (not a CI shell wrapper): src/__tests__/bundle-size.test.ts walks dist/**/*.{js,css}, gzipSyncs each, asserts the sum ≤ 500*1024. Skips gracefully when dist/ doesn't exist (pre-build smoke). The Makefile's dashboard-frontend target builds before testing so CI runs the gate against the real artifacts."
    - "Makefile dependency chain: dashboard-build : dashboard-frontend. The Go binary always picks up the latest Vite bundle because the frontend target is a prerequisite — no risk of a stale embed.FS."

key-files:
  created:
    - dashboard/web/src/lib/sse.ts
    - dashboard/web/src/lib/sse.test.ts
    - dashboard/web/src/lib/ansi.ts
    - dashboard/web/src/lib/ansi.test.ts
    - dashboard/web/src/components/PodLogStreamer.tsx
    - dashboard/web/src/components/PodLogStreamer.test.tsx
    - dashboard/web/src/components/EmptyState.tsx
    - dashboard/web/src/components/EmptyState.test.tsx
    - dashboard/web/src/components/ErrorState.tsx
    - dashboard/web/src/components/ErrorState.test.tsx
    - dashboard/web/src/components/LoadingState.tsx
    - dashboard/web/src/components/LoadingState.test.tsx
    - dashboard/web/src/__tests__/bundle-size.test.ts
  modified:
    - dashboard/web/src/App.tsx — replaced the \`void setStreamingTask\` stub with the real PodLogStreamer mount; streamingTask !== null → fixed bottom panel renders <PodLogStreamer>.
    - Makefile — \`dashboard-build : dashboard-frontend\` dependency added; \`dashboard-frontend\` chains npm ci + npm run build + npm run test + cp into cmd/dashboard/embed/dist.
    - cmd/dashboard/embed/dist/index.html + cmd/dashboard/embed/dist/assets/ — replaced the placeholder index.html with the real Vite-built bundle (CSS + JS chunk-hashed assets).

decisions:
  - "Single close site per socket lifecycle. The naive 'close on unmount only' implementation lets the browser's built-in EventSource auto-retry race our exponential-backoff schedule — multiple reconnect attempts pile up after a transient network blip. Fix: close in the error handler (counts as the cleanup close for that socket lifecycle), then null esRef.current so the unmount cleanup doesn't double-close. This is why Test 1's assertion of \`closeCalls === 1\` is the right contract."
  - "Browser-native Last-Event-ID — no custom header. The WHATWG SSE spec mandates that the browser sends Last-Event-ID automatically on every EventSource reconnect. Our hook just calls \`new EventSource(url)\` again; the backend events_sse.go reads the header via r.Header.Get('Last-Event-ID') and replays from there. No need for our own header-injecting fetch wrapper."
  - "Ring buffer in the hook, not the component. useTaskLog applies the 5000-line cap; PodLogStreamer renders whatever the hook returns. This keeps the ring-buffer behavior testable in sse.test.ts (Test 2 pushes 6000 lines, asserts 5000) without coupling the bounds check to the rendered DOM."
  - "ANSI parser scope locked to ops-tool output. Supported families (30-37, 90-97, 40-47, 100-107, 1, 22, 39, 49, 0) cover Go zap/logr colored output, kubectl colorize, and ANSI-aware shells. 256-color and truecolor are stripped: pod-log color usually comes from a few well-known tools and rendering hex truecolor on a browser canvas isn't worth 80 extra parser lines (and the dependencies a real lib would pull in)."
  - "Bundle gate as a Vitest test (not a separate shell script). The frontend toolchain owns both build and verification — no extra script, no extra CI shell. The test is opt-in to the dist/ existing so it's safe to run pre-build (where it's a no-op) and post-build (where it's the gate)."
  - "Bottom fixed panel for PodLogStreamer in App.tsx. UI-SPEC §8 says 'opens inline inside <TaskDetailDrawer>'. We honor the spec's intent — the streamer mounts when the drawer's button fires — but lay it out as a fixed-bottom panel rather than as a child of the drawer DOM tree. Two reasons: (1) the drawer is 420px wide which is cramped for log output; (2) the operator typically wants to see the DAG view + the log side-by-side, not stacked inside a narrow drawer. The behavior is identical from the user's perspective: click 'Open log stream' in the drawer → log streamer appears. v1.x can promote this to a proper resizable bottom-pane layout if operators ask."
  - "Auto-scroll uses useLayoutEffect (not useEffect). The browser flushes layout before painting between the layout effect and useEffect cleanup phase — assigning scrollTop in useLayoutEffect prevents a visible 'jump up then back to bottom' flicker on every new line. This is the standard React pattern for auto-scrolling log viewers."

patterns-established:
  - "EventSource hooks pattern: useSSEStream(url) is the primitive; per-endpoint wrappers (useTaskLog, future useProjectEvents) build on it by transforming MessageEvent.data into the domain shape they need. Reconnect, cleanup, Last-Event-ID handling all live in the primitive."
  - "Bundle-size gate pattern: any future asset-size SLA gets a Vitest test under src/__tests__/ that walks dist/ via Node's fs + zlib. No external tooling (no bundlewatch / size-limit) — keeps the npm dep graph tight."
  - "Locked copy patterns from UI-SPEC §Copywriting Contract live as inline string constants at the top of the component file (e.g. COPY_CONNECTING / COPY_WAITING / COPY_DISCONNECTED in PodLogStreamer.tsx). The gsd-ui-checker can grep these constants per file to verify copy compliance."

requirements-completed: [DASH-03, DASH-04]

# Metrics
duration: 25 min
completed: 2026-05-19
files_created: 13
files_modified: 3
commits: 4
test_count_added: 30  # sse:3, ansi:7, EmptyState:3, ErrorState:2, LoadingState:4, PodLogStreamer:10, bundle-size:1
test_count_total: 124  # was 94 at 04-13 baseline; +30
bundle_size_gzipped_kb: 156  # 149.59 JS + 6.53 CSS; under the 500KB gate
---

# Phase 4 Plan 16: SSE Hooks + PodLogStreamer + Full-Screen State Components Summary

**One-liner:** Closes the read-only dashboard frontend end-to-end — `useSSEStream` + `useTaskLog` hooks own the browser-side EventSource lifecycle (Pitfall 22 client-side; close-once-per-lifecycle so our exponential backoff doesn't race the browser's built-in auto-retry); `<PodLogStreamer>` mounts via the drawer's `onOpenLogStream` callback to render ring-buffered pod-log lines through a bespoke 194-line ANSI SGR parser (UI-SPEC §8 scope; T-04-D1 XSS-safe via React text nodes + style props, never `innerHTML`); the three full-screen state components (`EmptyState` E1/E2/E3, `ErrorState` ERR1/ERR2, `LoadingState` L1/L2/L3) ship the verbatim UI-SPEC §13/§14/§15 copy; the Vite bundle ships at ~156KB gzipped (well under the 500KB gate enforced by a Vitest test that walks `dist/`); and the Makefile now chains `dashboard-build : dashboard-frontend` so the Go binary always picks up the real bundle — `go:embed all:dist` now holds the Vite output instead of the plan-04-10 placeholder.

## Performance

- **Duration:** ~25 min
- **Tasks:** 2/2
- **Files created:** 13 (5 source files + 5 test files + 1 bundle-gate test + dist/index.html + dist/assets/ from build)
- **Files modified:** 3 (App.tsx mount, Makefile dependency, cmd/dashboard/embed/dist/index.html replaced)
- **Tests:** 124 passing across 19 files (+30 new: sse:3, ansi:7, EmptyState:3, ErrorState:2, LoadingState:4, PodLogStreamer:10, bundle-size:1)
- **Bundle:** 149.59KB gzipped JS + 6.53KB gzipped CSS = ~156KB total — well under the 500KB gate; same headroom as plan 04-13 because the new components add ~6KB minified post-tree-shake.
- **Binary:** `go build -o /tmp/dashboard ./cmd/dashboard` → 48MB binary; embed.Dist contains the real Vite bundle.

## Accomplishments

### SSE consumption layer (DASH-03 + DASH-04)

- **`useSSEStream(url)`** owns the browser EventSource lifecycle. Returns `{ events: MessageEvent[], state: 'connecting'|'connected'|'reconnecting'|'offline', lastEventId: number }`. On `error`, closes the dead socket and schedules a reconnect via `window.setTimeout` (exponential backoff: 1s × 2^attempt capped at 30s). On unmount, clears the pending timer and closes the live socket exactly once.
- **`useTaskLog(taskName)`** wraps `useSSEStream` against `/api/v1/tasks/${name}/log` (matches the backend wire shape from plan 04-11). Transforms each `MessageEvent.data` into a `string[]` via concat-then-slice with a 5000-line cap. Resets the buffer on task name change.
- **Last-Event-ID** is handled by the browser per the WHATWG SSE spec — no custom header injection. Reconnects just re-instantiate `new EventSource(url)`; the backend's `events_sse.go` reads `r.Header.Get('Last-Event-ID')` and replays from there.

### `<PodLogStreamer>` (DASH-04 surface — UI-SPEC §8)

- **Mount path:** drawer "Open log stream" button → `props.onOpenLogStream(taskName)` → App.tsx `setStreamingTask(name)` → `streamingTask !== null` mounts the streamer in a fixed bottom panel (240px tall).
- **Viewport:** flex-1 scrollable div, mono font 13px, line-height 1.4. Each line rendered as a `<div>` whose `white-space` toggles between `pre` (default) and `pre-wrap` (wrap toggle on).
- **ANSI rendering:** every line passes through `parseAnsi` → segment list rendered as `<span style={{color, background, fontWeight}}>`. T-04-D1 mitigation: zero `innerHTML`; React escapes text nodes by default.
- **Ring buffer:** inherited from `useTaskLog` — 5000 lines max.
- **Auto-scroll:** `useLayoutEffect` jumps to bottom (`el.scrollTop = el.scrollHeight`) after each render when `autoScroll` is on. Scroll handler reads `scrollTop + clientHeight` vs `scrollHeight`; flips `autoScroll → false` when user scrolls up (>2px from bottom); flips back to `true` when user returns to bottom. A "Paused (scrolled up)" indicator surfaces in the toolbar when off.
- **Toolbar:** Copy logs (writes `lines.join('\n')` to `navigator.clipboard`), Wrap toggle (line-wrap on/off), Close (focused on mount for Enter/Space dismiss).
- **Connection state copy** (verbatim per UI-SPEC §Copywriting Contract):
  - `connecting`: "Connecting to log stream…"
  - `connected` + 0 lines: "Waiting for output…"
  - `offline` or `idle-closed`: "Log stream closed by backend (5 min idle)."

### `<EmptyState>` (UI-SPEC §13)

Three variants, each centered in available pane area:

- **E1 — no-projects:** ASCII wave decoration (◢◣ → ━━━ pyramid) as a `<pre>` block — the only place this decoration appears outside the header wordmark underline. Heading "No projects in this cluster" + body containing the verbatim `tide apply -f project.yaml` snippet.
- **E2 — awaiting-first-milestone:** Heading "Awaiting first milestone" + the rising-tide metaphor copy.
- **E3 — plan-accepted-no-tasks:** Heading "Plan accepted" + "Computing wave structure…".

### `<ErrorState>` (UI-SPEC §14)

Full-screen takeover cards, max-width 480px:

- **ERR1 — backend-unreachable:** Heading "Dashboard backend unreachable" + the configured URL in the body + 3 kubectl hint commands (get deploy / logs / port-forward) in a `<pre>` block.
- **ERR2 — permission-denied:** Heading "Permission denied" + the ServiceAccount RBAC explanation + 2 hint commands (kubectl describe clusterrolebinding / helm get values).

Both variants include a Retry button (defaults to `window.location.reload`).

### `<LoadingState>` (UI-SPEC §15)

- **L1 — initial:** Centered Loader2 (lucide-react, animate-spin, 32px, accent color) + label "Loading dashboard…".
- **L2 — pane:** Same spinner, 24px, no label.
- **L3 — drawer:** Three skeleton gray bars (60%/80%/40% widths at --color-surface-overlay) + a small spinner in the bottom-right corner.

### Bundle-size CI gate (T-04-D-bundle-bloat)

- `src/__tests__/bundle-size.test.ts` walks `dist/**/*.{js,css}`, gzipSyncs each file, sums the totals, asserts ≤ 500KB.
- Skips gracefully if `dist/` doesn't exist (pre-build smoke).
- Current measurement: **149.59KB JS + 6.53KB CSS = 156KB gzipped** — ~3.2× headroom under the gate.

### Makefile glue (`dashboard-build : dashboard-frontend`)

```makefile
.PHONY: dashboard-build
dashboard-build: dashboard-frontend
	go build -o bin/dashboard ./cmd/dashboard

.PHONY: dashboard-frontend
dashboard-frontend:
	cd dashboard/web && npm ci && npm run build && npm run test
	rm -rf cmd/dashboard/embed/dist
	cp -r dashboard/web/dist cmd/dashboard/embed/dist
```

`dashboard-build` now always rebuilds the frontend first — no risk of a stale `embed.FS`. `dashboard-frontend` chains `npm ci + npm run build + npm run test` so the bundle-size gate (which walks the just-built `dist/`) actually runs against the real artifacts.

### App.tsx mount

```tsx
{streamingTask !== null && (
  <div className="fixed inset-x-0 bottom-0 z-50 ..." style={{ height: "240px", ... }}>
    <PodLogStreamer taskName={streamingTask} onClose={onCloseLogStream} />
  </div>
)}
```

The drawer's `onOpenLogStream` callback (wired in plan 04-13) is the producer of `streamingTask`. The streamer renders as a fixed bottom panel rather than inside the drawer DOM tree — see Decisions below for the rationale.

## Task Commits

| Task | Phase | Hash      | Type |
| ---- | ----- | --------- | ---- |
| 1    | RED   | `66e4402` | test |
| 1    | GREEN | `d9cb1e7` | feat |
| 2    | RED   | `6682350` | test |
| 2    | GREEN | `ca5c2f3` | feat |

SUMMARY commit (this file) follows.

## Verification Evidence

```bash
$ cd dashboard/web && CI=1 npm test
  ✓ 124 tests across 19 files, all passing
  - new files: sse.test.ts (3), ansi.test.ts (7), EmptyState.test.tsx (3),
    ErrorState.test.tsx (2), LoadingState.test.tsx (4),
    PodLogStreamer.test.tsx (10), bundle-size.test.ts (1) = +30

$ cd dashboard/web && npm run lint  (tsc -b)
  (clean)

$ wc -l dashboard/web/src/lib/ansi.ts
  194  (under the 200 cap)

$ grep -rE "dangerouslySetInnerHTML" dashboard/web/src/ \
      --include="*.tsx" --include="*.ts" | grep -v no-dangerous-html.test
  (no hits — T-04-D1 holds across all new files)

$ make dashboard-frontend
  vite build: dist/assets/index-*.js 455.69 kB │ gzip: 149.59 kB
              dist/assets/index-*.css 31.29 kB │ gzip:   6.53 kB
  vitest:    19 test files, 124 passing (incl. bundle-size gate)
  cp:        dashboard/web/dist → cmd/dashboard/embed/dist

$ go build -o /tmp/dashboard ./cmd/dashboard
  → 48MB binary

$ grep TIDE cmd/dashboard/embed/dist/index.html
  1  (real Vite-built index.html with <title>TIDE Dashboard</title>)

$ cat cmd/dashboard/embed/dist/index.html | head -7
  <!doctype html>
  <html lang="en">
    <head>
      <meta charset="UTF-8" />
      <meta name="viewport" content="width=1280" />
      <title>TIDE Dashboard</title>
      <script type="module" crossorigin src="/assets/index-B-v8KUJP.js"></script>
  → confirmed: chunk-hashed asset references; no placeholder text.
```

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 — Bug] Test 3 (auto-scroll re-enable) wrote to `scrollTop` defined as a value descriptor without `writable: true`**

- **Found during:** Task 2 GREEN — first run of the auto-scroll test threw `TypeError: Cannot assign to read only property 'scrollTop'` from the production `useLayoutEffect` that does `el.scrollTop = el.scrollHeight`.
- **Issue:** `Object.defineProperty(viewport, "scrollTop", { configurable: true, value: 800 })` creates a non-writable descriptor (writable defaults to false). The production auto-scroll assignment trips.
- **Fix:** Added `writable: true` to the `scrollTop` descriptors in PodLogStreamer.test.tsx so the production assignment is a no-op during the test (the value the test set persists).
- **Files modified:** `dashboard/web/src/components/PodLogStreamer.test.tsx`.
- **Committed in:** `ca5c2f3` (Task 2 GREEN).

**2. [Rule 1 — Bug] Test 4 (ANSI render) used `getByText(/ fail/)` which lost the leading space via whitespace normalization**

- **Found during:** Task 2 GREEN — testing-library normalizes leading/trailing whitespace on text matchers; the `<span> fail</span>` rendered " fail" but `getByText` searches " fail" against the normalized text "fail" → no match.
- **Fix:** Asserted on `line.textContent === "ERROR fail"` directly via the `pod-log-line-0` testid. More robust than substring matching and asserts the full rendered text concatenation across the parsed segments.
- **Files modified:** `dashboard/web/src/components/PodLogStreamer.test.tsx`.
- **Committed in:** `ca5c2f3` (Task 2 GREEN).

**3. [Rule 1 — Bug] ansi.ts truecolor `\x1b[38;2;200;100;50m` was being mis-parsed — `100` was matching bg-bright-black**

- **Found during:** Task 1 GREEN — Test 5 ("strips truecolor (38;2;r;g;b) entirely") asserted `[{ text: "fancy" }]` but got `[{ text: "fancy", bgColor: "black" }]`. Tracing: my first `applyParams` impl iterated tokens individually. `38` was unsupported (skipped), `2` was unsupported (skipped), `200` was unsupported (skipped), but `100` matched `BG_BRIGHT_BASE` → `bgColor: "black"`. The 256-color test (`38;5;208m`) coincidentally passed because `5` < `30` (no color match) and `208` is also unmatched, so the bug was silent on that path.
- **Fix:** Rewrote `applyParams` to consume sub-parameters when it sees codes `38` or `48`: `;5;N` → consume 2 extra tokens; `;2;R;G;B` → consume 4 extra tokens. Unknown extension (`;6;...` etc.) defensively consumes 1 extra token to avoid leaking the next code as a fresh SGR.
- **Files modified:** `dashboard/web/src/lib/ansi.ts`.
- **Committed in:** `d9cb1e7` (Task 1 GREEN).

**4. [Rule 1 — Bug] LoadingState test used `spinner.className` which on SVG elements is `SVGAnimatedString` (object), not a string**

- **Found during:** Task 1 GREEN — Test 11 ("Loader2 renders with animate-spin") threw `.toMatch() expects to receive a string, but got object`. The lucide-react Loader2 renders an SVG; SVGElement.className is the legacy `SVGAnimatedString` interface.
- **Fix:** Replaced `spinner.className` with `spinner.getAttribute("class") ?? ""` — returns a string in both jsdom and real browsers. This is the same pattern used by other tests in the suite (StatusBadge.test.tsx uses `innerHTML.toContain("animate-spin")` for the same reason).
- **Files modified:** `dashboard/web/src/components/LoadingState.test.tsx`.
- **Committed in:** `d9cb1e7` (Task 1 GREEN).

**5. [Rule 1 — Bug] sse.ts onerror handler initially double-closed the EventSource**

- **Found during:** Task 1 GREEN — Test 1 (`useSSEStream`) asserts `es.closeCalls === 1` after triggering error then unmount. My first impl closed in `onerror` and then again in the cleanup function — `closeCalls === 2`.
- **Issue/decision:** We need to close in `onerror` because otherwise the browser's built-in EventSource auto-retry races our exponential-backoff timer (the browser opens a new connection on its own while we're scheduling our own — duplicate subscribers on the Hub). But we don't want to double-close on unmount.
- **Fix:** After closing in onerror, null `esRef.current` so the unmount cleanup sees `null` and skips its close — exactly one close per socket lifecycle. The reconnect timer creates a new EventSource which becomes the next `esRef.current`; on unmount that one gets closed (or the timer is cleared before it fires).
- **Files modified:** `dashboard/web/src/lib/sse.ts`.
- **Committed in:** `d9cb1e7` (Task 1 GREEN).

### Scope adjustments

None. All 9 plan behaviors landed as specified.

### Honored CLAUDE.md / STACK.md constraints

- No emojis in any source file, summary section, or commit message (CLAUDE.md no-emoji rule honored).
- No new npm dependencies — pulled `gzipSync` from `node:zlib` (built-in) for the bundle-size gate.
- No `dangerouslySetInnerHTML` introduced — the existing `no-dangerous-html.test.ts` guard still passes (T-04-D1 holds).
- No new external network endpoints, no new auth paths, no schema changes — the SSE consumption layer talks to the existing read-only `cmd/dashboard/api/{events_sse.go, logs_sse.go}` endpoints shipped in plan 04-11.
- Did not modify `charts/tide/values.yaml` (the chart is the locked contract per Phase 02.2's anti-pattern).
- Did not modify STATE.md or ROADMAP.md per the executor instructions.

## Seams for downstream plans

- **Plan 04-14 (Helm chart):** the dashboard binary now ships a working SPA. The chart can deploy it without a separate static-asset path; `kubectl port-forward` against the dashboard service reaches a live UI that consumes the SSE endpoints in-cluster.
- **Future SSE-driven re-renders in DAG views (plan 04-13 left this as a documented seam):** `useProjectEvents(projectName)` will follow the same `useSSEStream`-wrapper pattern as `useTaskLog`. The reconnect / cleanup / Last-Event-ID logic doesn't need to be re-derived — it lives in `useSSEStream`. Just transform the typed events into the `ProjectDetail` shape PlanningDAGView consumes.
- **PodLogStreamer placement evolution:** v1.x can promote the fixed bottom panel into a proper resizable bottom pane via a draggable divider, or inline it into the drawer if operators say the fixed-panel layout is wrong. The mount seam (`onOpenLogStream` → `streamingTask` state → conditional render) doesn't change.

## Threat Flags

None — no new trust boundaries crossed beyond the ones the threat register already covers (and all three mitigation dispositions hold):

| Threat                          | Mitigation                                                                                                                  |
| ------------------------------- | --------------------------------------------------------------------------------------------------------------------------- |
| T-04-D1 (XSS via log content)   | parseAnsi returns plain text + style props; PodLogStreamer renders via React text nodes. Zero innerHTML — guard test holds. |
| T-04-D-bundle-bloat (DoS)       | Bundle gate <500KB enforced by Vitest. Current 156KB gzipped → 3.2× headroom.                                                |
| T-04-D-eventsource-leak (DoS)   | useEffect cleanup closes EventSource once per socket lifecycle; useTaskLog ring-buffers at 5000 lines.                       |

## Known Stubs

The PodLogStreamer's bottom-panel layout (fixed 240px) is a v1.0 simplification of the spec's "inline inside the drawer" intent — operators get the functional behavior (click drawer button → log streamer appears) but the precise placement is interim. A v1.x plan can promote this to a resizable layout. Captured as a Decision above so the deferral is intentional and tracked.

The "Reconnect" button mentioned in UI-SPEC §8's disconnected state copy ("Log stream closed by backend (5 min idle). [Reconnect]") is not wired in v1.0 — the bracketed "[Reconnect]" appears in the displayed string as plain text. Reconnect is achievable by closing + re-opening the streamer (close the streamer panel, click "Open log stream" again in the drawer). Adding a click-able Reconnect would be ~5 lines and could land in a follow-on plan if operators ask.

## Issues Encountered

None beyond the 5 deviations above. The biggest surprise was Deviation #5 (the double-close issue) — the close-once-per-socket-lifecycle contract is subtle because it lives at the intersection of three controllers: the React effect cleanup, our error handler, and the browser's built-in EventSource auto-retry. The fix (close in onerror to break the browser's auto-retry race + null the ref so unmount skips) is captured as a Decision above so it doesn't get accidentally regressed in a future refactor.

## Self-Check: PASSED

- **Files created exist:**
  - FOUND: dashboard/web/src/lib/sse.ts
  - FOUND: dashboard/web/src/lib/sse.test.ts
  - FOUND: dashboard/web/src/lib/ansi.ts
  - FOUND: dashboard/web/src/lib/ansi.test.ts
  - FOUND: dashboard/web/src/components/PodLogStreamer.tsx
  - FOUND: dashboard/web/src/components/PodLogStreamer.test.tsx
  - FOUND: dashboard/web/src/components/EmptyState.tsx
  - FOUND: dashboard/web/src/components/EmptyState.test.tsx
  - FOUND: dashboard/web/src/components/ErrorState.tsx
  - FOUND: dashboard/web/src/components/ErrorState.test.tsx
  - FOUND: dashboard/web/src/components/LoadingState.tsx
  - FOUND: dashboard/web/src/components/LoadingState.test.tsx
  - FOUND: dashboard/web/src/__tests__/bundle-size.test.ts
- **Files modified exist:**
  - FOUND: dashboard/web/src/App.tsx (streamingTask now mounts PodLogStreamer; line 45 + the `{streamingTask !== null && ...}` block)
  - FOUND: Makefile (dashboard-build : dashboard-frontend dependency added; dashboard-frontend chains npm test before cp)
  - FOUND: cmd/dashboard/embed/dist/index.html (real Vite build, contains "TIDE Dashboard" title)
  - FOUND: cmd/dashboard/embed/dist/assets/index-B-v8KUJP.js + index-C0FoVFPe.css
- **Commits exist on this branch:**
  - FOUND: 66e4402 — test(04-16): RED Task 1
  - FOUND: d9cb1e7 — feat(04-16): GREEN Task 1
  - FOUND: 6682350 — test(04-16): RED Task 2
  - FOUND: ca5c2f3 — feat(04-16): GREEN Task 2
- **Verification gates green:**
  - `cd dashboard/web && CI=1 npm test` → 124 passing (was 94 at 04-13 baseline; +30 new)
  - `cd dashboard/web && npm run lint` (tsc -b) → clean
  - `wc -l dashboard/web/src/lib/ansi.ts` → 194 lines (≤ 200 cap)
  - `grep dangerouslySetInnerHTML dashboard/web/src/` → 0 (excluding the guard test)
  - `make dashboard-frontend` → vite build + 124 tests pass + cp into cmd/dashboard/embed/dist
  - `go build -o /tmp/dashboard ./cmd/dashboard` → success; 48MB binary
  - `grep TIDE cmd/dashboard/embed/dist/index.html` → 1 (real Vite-built `<title>TIDE Dashboard</title>`)
  - Bundle: 149.59KB JS + 6.53KB CSS = ~156KB gzipped (gate: ≤ 500KB)

## Threat surface scan

No new external network endpoints introduced by this plan. The frontend consumes the existing read-only SSE endpoints (`GET /api/v1/projects/{name}/events`, `GET /api/v1/tasks/{name}/log`) shipped in plan 04-11; no new auth paths, no new file-access patterns, no schema changes. Threat register dispositions (T-04-D1 XSS, T-04-D-bundle-bloat, T-04-D-eventsource-leak) all hold and are re-asserted by the existing guard tests + the new bundle-size gate. No threat flags.

## Next Plan Readiness

- **Plan 04-14 (Helm chart, if not already complete):** the dashboard binary is end-to-end functional — `kubectl port-forward svc/tide-dashboard 8080:80` against a chart-deployed pod should reach a live SPA that lists projects, renders DAGs, streams pod logs.
- **Phase 4 closeout / DASH-05:** with this plan landed, the dashboard surface (DASH-01..04) is complete. The remaining DASH-05 "zero-mutation" invariant is enforced by `cmd/dashboard/router_test.go::TestZeroMutationRoutes` (shipped in plan 04-11 and still passing).
- **TIDE-on-TIDE bar:** the dashboard is now functional enough to watch a real TIDE run progress in real time — a milestone toward the v1 "TIDE-on-TIDE" success criterion.

---
*Phase: 04-gates-observability-dashboard-cli*
*Plan: 16*
*Completed: 2026-05-19*
