---
phase: 46-observability-enrichment-dashboard-deep-link
plan: 05
subsystem: ui
tags: [react, typescript, phoenix, opentelemetry, dashboard, deep-link, vitest]

# Dependency graph
requires:
  - phase: 46-observability-enrichment-dashboard-deep-link (plan 03)
    provides: "GET /api/v1/config phoenixBaseURL field + projectDetail/childRef/taskDetail traceId/traceSpanId payload fields"
provides:
  - "lib/phoenixLink.ts — phoenixTraceURL/phoenixSpanURL, the one place any Phoenix /redirects/ URL is assembled (D-11/D-12)"
  - "components/PhoenixTraceLink.tsx — shared eligibility + anchor render, mounted at both NodeDetailPanel content and TaskDetailDrawer"
  - "App.tsx one-shot GET /api/v1/config fetch for phoenixBaseURL, threaded to both mount points"
affects: []

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "One-place URL-assembly module pattern (D-11): a single lib/*.ts file owns all `${base}/path/{id}` template-string construction; every consumer — including tests — imports the helper instead of inlining the string, grep-provable via `grep -rn '<pattern>' src | grep -v '<module>'` returning 0 lines"
    - "Config-consumption prop-drilling: a single one-shot fetch in App.tsx (mirroring TelemetryView.tsx's existing GET /api/v1/config pattern) passed down as props to multiple mount points, rather than each component re-fetching"

key-files:
  created:
    - dashboard/web/src/lib/phoenixLink.ts
    - dashboard/web/src/lib/phoenixLink.test.ts
    - dashboard/web/src/components/PhoenixTraceLink.tsx
  modified:
    - dashboard/web/src/lib/api.ts
    - dashboard/web/src/App.tsx
    - dashboard/web/src/components/TaskDetailDrawer.tsx
    - dashboard/web/src/lib/tasks.ts
    - dashboard/web/src/components/__tests__/node-panel-integration.test.tsx
    - dashboard/web/src/components/__tests__/drawer.test.tsx

key-decisions:
  - "traceId is accepted by PhoenixTraceLinkProps (not just spanId) even though the current href always uses phoenixSpanURL — it exists so phoenixTraceURL(baseURL, traceId) is a one-line fallback swap confined to this component, per UI-SPEC §Href"
  - "phoenixTraceURL is NOT imported into PhoenixTraceLink.tsx (only referenced in a comment) to avoid an unused-import failure under tsconfig's noUnusedLocals — the component destructures only {baseURL, spanId, edge} from its props, leaving traceId available in the type without triggering unused-var/unused-param errors"
  - "TaskDetailDrawer's new phoenixBaseURL prop defaults to \"\" (optional, not required) so all 9 pre-existing render calls in drawer.test.tsx needed zero changes — absence degrades to hidden, consistent with the config-fetch-failure contract"
  - "lib/tasks.ts (not in the plan's files_modified frontmatter) was touched to thread traceId/traceSpanId through taskDetailJSONToData — the plan's own action text (\"grep the useTaskDetail hook/mapping\") anticipated this; TaskDetailDrawer's TaskDetailData wouldn't receive the fields otherwise"

requirements-completed: [OBS-04]

# Metrics
duration: ~20min
completed: 2026-07-17
---

# Phase 46 Plan 05: SPA Phoenix Deep Link Summary

**Shared `<PhoenixTraceLink>` "View trace in Phoenix" component and `phoenixLink.ts` URL module, mounted at both NodeDetailPanel content (project/milestone/phase/plan) and TaskDetailDrawer (task), wired to Plan 46-03's config/payload trace-identity fields — renders nothing when config or span identity is absent.**

## Performance

- **Duration:** ~20 min
- **Started:** 2026-07-17T05:38:01Z (approx., base commit `caf0125`)
- **Completed:** 2026-07-17T05:54:04Z
- **Tasks:** 2 completed (+ 1 post-task fix commit)
- **Files modified:** 9 (3 created, 6 modified)

## Accomplishments

- `lib/phoenixLink.ts` exports `phoenixTraceURL`/`phoenixSpanURL`, the ONE place any `/redirects/traces|spans/{id}` URL is built — trailing-slash normalization + `encodeURIComponent` on the interpolated ID, unit-tested for route shape, normalization, and encoding (fixture `"a/b"` → `a%2Fb`).
- `<PhoenixTraceLink>` owns eligibility (render `null` unless `baseURL` AND `spanId` are both non-empty — `traceId` alone does not qualify), copy, icon, typography, and states per the locked UI-SPEC contract; zero fetch calls (data arrives via props only).
- App.tsx fetches `GET /api/v1/config` once per page load (mirroring `TelemetryView.tsx`'s existing pattern, left untouched) and threads `phoenixBaseURL` to both mount points: the first child inside `nodePanelContent` (both the project branch and the milestone/phase/plan branch) and a new `phoenixBaseURL` prop on `<TaskDetailDrawer>`.
- `TaskDetailDrawer` mounts `<PhoenixTraceLink edge="top">` immediately after the metadata grid, before the Actions row; `api.ts` gained additive `traceId`/`traceSpanId` mirrors on `ProjectDetail`/`ChildRef`/`TaskDetailJSON` matching Plan 46-03's wire contract, plus a `DashboardConfig` type for `phoenixBaseURL`.
- Test coverage: `phoenixLink.test.ts` (6 tests, pure functions) plus 6 new render/hide/placement cases across `node-panel-integration.test.tsx` and `drawer.test.tsx` — full suite `npm test` green at 34/34 files, 296/296 tests; `npx tsc -b` clean.

## Task Commits

Each task was committed atomically:

1. **Task 1: phoenixLink.ts URL module + PhoenixTraceLink component** - `f981f82` (feat)
2. **Task 2: Wire both mount points + config fetch + type mirrors + tests** - `b8d1900` (feat)
3. **Post-task fix: one-place-rule href assertions** - `06b3ee6` (fix) — see Deviations

_No plan-metadata commit yet — SUMMARY.md commit follows this one per worktree protocol._

## Files Created/Modified

- `dashboard/web/src/lib/phoenixLink.ts` - `phoenixTraceURL`/`phoenixSpanURL`, trailing-slash normalization, `encodeURIComponent`
- `dashboard/web/src/lib/phoenixLink.test.ts` - 6 unit tests for the above
- `dashboard/web/src/components/PhoenixTraceLink.tsx` - shared eligibility + anchor render (UI-SPEC rendered shape verbatim)
- `dashboard/web/src/lib/api.ts` - `ChildRef.traceSpanId?`, `ProjectDetail.traceId?/traceSpanId?`, `TaskDetailJSON.traceId?/traceSpanId?`, new `DashboardConfig` type
- `dashboard/web/src/App.tsx` - one-shot `GET /api/v1/config` fetch for `phoenixBaseURL`; `<PhoenixTraceLink>` mounted in both `nodePanelContent` branches and passed to `<TaskDetailDrawer>`
- `dashboard/web/src/components/TaskDetailDrawer.tsx` - `TaskDetailData.traceId?/traceSpanId?`, optional `phoenixBaseURL` prop, `<PhoenixTraceLink edge="top">` after the metadata grid
- `dashboard/web/src/lib/tasks.ts` - `taskDetailJSONToData` threads `traceId`/`traceSpanId` through to `TaskDetailData`
- `dashboard/web/src/components/__tests__/node-panel-integration.test.tsx` - 3 new PhoenixTraceLink render/hide cases (project mount)
- `dashboard/web/src/components/__tests__/drawer.test.tsx` - 4 new PhoenixTraceLink render/hide/placement cases (task mount)

## Decisions Made

- `traceId` is accepted by `PhoenixTraceLinkProps` even though it's unused in the current render path — reserved as the one-line fallback-swap point (`phoenixTraceURL` instead of `phoenixSpanURL`) per UI-SPEC §Href, confined entirely to this component.
- `phoenixTraceURL` is intentionally NOT imported into `PhoenixTraceLink.tsx` (only referenced in a doc comment) — importing an unused symbol would fail `tsc -b` under `noUnusedLocals`; the component destructures only the props it renders with (`{baseURL, spanId, edge}`), leaving `traceId` present in the type without being pulled into scope.
- `TaskDetailDrawer`'s new `phoenixBaseURL` prop defaults to `""` (optional) rather than required, so the 9 pre-existing drawer test render calls needed zero changes — absence degrades to hidden, matching the "fetch failure → hidden" contract.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] Installed dashboard/web npm dependencies before any test could run**
- **Found during:** Task 1
- **Issue:** `dashboard/web/node_modules` did not exist in the worktree; `npx vitest run` failed with `ERR_MODULE_NOT_FOUND` for `vitest`.
- **Fix:** Ran `npm ci` (installs from the existing `package-lock.json` — no new package added, so the package-manager-install exclusion for Rule 3 does not apply; this is a plain reproducible install of already-locked, already-vetted dependencies).
- **Files modified:** none (node_modules is gitignored; no package.json/package-lock.json changes)
- **Verification:** `npx vitest run src/lib/phoenixLink.test.ts` then ran successfully.
- **Committed in:** n/a (no tracked files changed)

**2. [Rule 1 - Bug] Fixed two test assertions that violated the plan's own one-place-URL-assembly acceptance criterion**
- **Found during:** post-Task-2 verification pass (running the plan's stated `<verification>` block: "One-place grep: no `/redirects/` template strings outside lib/phoenixLink.ts")
- **Issue:** Two new href assertions (`node-panel-integration.test.tsx`, `drawer.test.tsx`) hardcoded the expected URL as a literal string (`"http://phoenix:6006/redirects/spans/span456"`) instead of computing it via the exported helper — violating D-11/the plan's explicit acceptance criteria ("including tests, which import the helpers").
- **Fix:** Imported `phoenixSpanURL` from `../../lib/phoenixLink` in both test files and replaced the literal strings with `phoenixSpanURL("http://phoenix:6006", "span456")`.
- **Files modified:** `dashboard/web/src/components/__tests__/node-panel-integration.test.tsx`, `dashboard/web/src/components/__tests__/drawer.test.tsx`
- **Verification:** `grep -rn 'redirects/' dashboard/web/src --include='*.ts' --include='*.tsx' | grep -v 'lib/phoenixLink'` now returns 0 lines; `npm test` still 296/296 green.
- **Committed in:** `06b3ee6`

**3. [Rule 3 - Blocking] Touched lib/tasks.ts, one file outside the plan's `files_modified` frontmatter list**
- **Found during:** Task 2
- **Issue:** The plan's frontmatter `files_modified` list omits `dashboard/web/src/lib/tasks.ts`, but its `taskDetailJSONToData` mapping function is the only place `TaskDetailJSON` (API wire shape) becomes `TaskDetailData` (the type `TaskDetailDrawer` renders from) — without touching it, the newly-added `traceId`/`traceSpanId` fields on `TaskDetailJSON` would never reach the drawer.
- **Fix:** Added `traceId: t.traceId, traceSpanId: t.traceSpanId` to the existing field-by-field mapping in `taskDetailJSONToData`.
- **Files modified:** `dashboard/web/src/lib/tasks.ts` (2 lines)
- **Verification:** the drawer test's placement/render cases pass, proving the fields flow end-to-end from the mocked `TaskDetailJSON` shape through the hook to the rendered component.
- **Committed in:** `b8d1900` (part of Task 2 commit)

---

**Total deviations:** 3 auto-fixed (1 blocking/tooling, 1 bug, 1 blocking/scope-completion)
**Impact on plan:** All three are direct, necessary consequences of completing the plan's own stated action items and acceptance criteria. No scope creep — no files outside the OBS-04 deep-link surface were touched.

## Issues Encountered

- Accidentally ran `git stash` mid-Task-2 while inspecting an unrelated pre-existing test flake — recovered immediately without using any further `git stash` subcommand: confirmed the stash commit's first parent matched current HEAD (`f981f82`) via `git show --no-patch --format="%P"`, then restored the exact 6 files via `git checkout <stash-hash> -- <paths>` and verified a byte-identical diff (`git diff --cached <stash-hash> -- <paths>` returned empty) before proceeding. No data lost; documented here per the destructive-git-prohibition's transparency expectation.
- `ArtifactViewer.test.tsx` (a file this plan never touches) intermittently failed with a React `act()`-timing-related `findByTestId` timeout when run as part of the full 34-file suite, but passed reliably (3/3) in isolation. Confirmed pre-existing and environmental (resource contention under parallel test-file execution), not caused by this plan's changes — out of scope per the deviation-rules scope boundary; not fixed. A subsequent full `npm test` run came back 34/34 green.

## User Setup Required

None - no external service configuration required.

## Next Phase Readiness

- OBS-04 is now fully shipped end-to-end (backend from Plan 46-03 + this plan's SPA surface): every Planning/Execution DAG node's detail surface deep-links to its Phoenix span when `PHOENIX_BASE_URL` is configured and the span has emitted; renders nothing otherwise.
- `npm test` (34/34 files, 296/296 tests) and `npx tsc -b` both clean at plan close.
- No blockers for the remainder of Phase 46 or Phase 47 (self-hosted Phoenix install docs + live end-to-end proof).

---
*Phase: 46-observability-enrichment-dashboard-deep-link*
*Completed: 2026-07-17*

## Self-Check: PASSED

All 9 modified/created source files + SUMMARY.md verified present on disk; all 4 commits (`f981f82`, `b8d1900`, `06b3ee6`, `b2cd32f`) verified present in `git log --oneline --all`.
