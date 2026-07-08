---
phase: 37-dashboard-surfaces-artifact-view-project-view-log-drawer-sta
plan: 05
subsystem: ui
tags: [react, react-markdown, remark-gfm, typescript, dashboard, xss, artifacts]

# Dependency graph
requires:
  - phase: 04-gates-observability-dashboard-cli
    provides: ClipboardCopyAction, StatusBadge, Empty/Error/LoadingState primitives, api.ts fetch idiom, @theme tokens
  - phase: 37 (plan 37-04)
    provides: NodeDetailPanel shell that will host these components
provides:
  - fetchNodeArtifacts + fetchProjectSettings typed fetchers mirroring the plan-37-07 Go structs
  - ArtifactState/ArtifactFile/NodeArtifacts/ProjectSettings TS types
  - ArtifactViewer component (tabs, markdown/JSON rendering, five R-04 states, 10s gate-parked polling)
  - ApproveStrip component (pinned AwaitingApproval strip with copyable approve/reject commands)
affects: [37-07 (implements both endpoints to this contract), 37-08 (mounts both components in NodeDetailPanel)]

# Tech tracking
tech-stack:
  added: [react-markdown@10.1.0, remark-gfm@4.0.1]
  patterns:
    - "XSS-safe markdown: react-markdown + remark-gfm only, no raw-HTML plugin; components prop mapped to UI-SPEC token ladder"
    - "R-04 typed-state discriminator rendered as explicit honest copy — never a silent empty panel"
    - "Bounded client polling: setInterval gated on (gateParked && state===absent), cleared on available/unmount"

key-files:
  created:
    - dashboard/web/src/components/ArtifactViewer.tsx
    - dashboard/web/src/components/ArtifactViewer.test.tsx
    - dashboard/web/src/components/ApproveStrip.tsx
    - dashboard/web/src/components/ApproveStrip.test.tsx
  modified:
    - dashboard/web/src/lib/api.ts
    - dashboard/web/package.json
    - dashboard/web/package-lock.json

key-decisions:
  - "materializing is a UI-derived display state (absent + gateParked), not a wire value — keeps the Go discriminator to four states"
  - "JSON path uses JSON.stringify(parse, null, 2) with raw-text fallback on parse failure — no syntax-highlight library (bundle discipline)"
  - "Kept react-markdown/remark-gfm at exact pins via --save-exact per the legitimacy audit"

patterns-established:
  - "Locked-copy state panels: a small StatePanel helper renders heading/body/optional-action with verbatim UI-SPEC strings"
  - "Contract cross-reference comments cite the mirroring Go struct (cmd/dashboard/api/*.go) so a backend rename surfaces as a TS compile error"

requirements-completed: [DASH-01]

coverage:
  - id: D1
    description: "Typed fetchers + types for the artifacts and project-settings endpoints (mirroring plan-37-07 Go structs)"
    requirement: DASH-01
    verification:
      - kind: unit
        ref: "dashboard/web src/lib (tsc -b + existing api.test.ts suite green)"
        status: pass
    human_judgment: false
  - id: D2
    description: "ArtifactViewer — file tabs, GFM markdown + pretty JSON, five R-04 states with locked copy, 10s gate-parked polling, XSS-safe"
    requirement: DASH-01
    verification:
      - kind: unit
        ref: "dashboard/web/src/components/ArtifactViewer.test.tsx (9 tests: available/tabs, XSS+href, five states, polling)"
        status: pass
    human_judgment: false
  - id: D3
    description: "ApproveStrip — pinned AwaitingApproval strip with copy-only approve/reject, zero mutation surface"
    requirement: DASH-01
    verification:
      - kind: unit
        ref: "dashboard/web/src/components/ApproveStrip.test.tsx (3 tests: badge+label, two copy actions, read-only lock)"
        status: pass
    human_judgment: false

# Metrics
duration: 12min
completed: 2026-07-08
status: complete
---

# Phase 37 Plan 05: Artifact Viewer + Approve Strip Content Components Summary

**Built the DASH-01 content layer — an XSS-safe, five-state ArtifactViewer and a copy-only gate-parked ApproveStrip — plus the typed fetch layer for both new endpoints, all standalone and awaiting mount by plan 37-08.**

## Performance

- **Duration:** ~12 min
- **Completed:** 2026-07-08T09:43:04Z
- **Tasks:** 3 completed
- **Files created:** 4 · **Files modified:** 3

## Accomplishments

- **Task 1 — deps + fetch layer:** installed `react-markdown@10.1.0` + `remark-gfm@4.0.1` at exact pins (legitimacy-audited, zero postinstall), and extended `api.ts` with `fetchNodeArtifacts` / `fetchProjectSettings` plus the `ArtifactState`/`ArtifactFile`/`NodeArtifacts`/`ProjectSettings` types, each carrying a contract cross-reference comment to the plan-37-07 Go struct it mirrors.
- **Task 2 — ArtifactViewer:** tabbed file strip (`role="tablist"`, arrow-key nav, first `*.md` default-selected), GFM markdown rendering mapped to the UI-SPEC typography ladder, pretty-printed JSON for `children/*.json`, all five R-04 states with LOCKED verbatim copy, and a bounded 10s poll while gate-parked + absent. No truncation anywhere (D-03).
- **Task 3 — ApproveStrip:** pinned `AwaitingApproval` strip reusing `StatusBadge` + two `ClipboardCopyAction` buttons (`tide approve <project>` primary, `tide reject <project>` destructive). Clipboard-copy only — no form, no fetch, no confirmation modal.

## Security — Threat Mitigations Applied

- **T-37-05-01 (XSS via artifact markdown, high, mitigate):** the markdown path passes GFM as its only remark plugin and NO raw-HTML plugin layer. LLM-authored HTML renders as escaped text and `javascript:`-scheme URLs are stripped by react-markdown's default URL transform. A hostile-fixture test asserts `document.querySelector("script")` is null and no anchor carries a `javascript:` href (Assumption A2 verified). Acceptance-gated: `grep -ci rehype` == 0 in the component.
- **T-37-SC (npm installs, high, mitigate):** exactly the two audited packages installed at exact pins; the SUS-flagged `react-resizable-panels` was NOT installed. `npm audit` shows only pre-existing dev-tooling advisories (esbuild/vitest, form-data, ws) — none from react-markdown/remark-gfm.
- **T-37-05-02 (clipboard command injection, low, accept):** projectName is a K8s DNS-1123 name; the command is pasted by the operator, not executed by the browser — accepted per the threat register.

## Deviations from Plan

### Environment adjustment (not a code deviation)

- The worktree has no `.tool-versions`, so `asdf` refused to pick a node version. Pinned `ASDF_NODEJS_VERSION=22.22.3` (one of the two installed versions) per-command for all npm/npx invocations rather than writing a config file into the repo. No file added.

### Auto-fixed during execution

**1. [Rule 1 - Bug] Exact version pins**
- **Found during:** Task 1 — initial `npm install` wrote caret ranges (`^10.1.0`), failing the exact-pin acceptance check.
- **Fix:** reinstalled with `--save-exact`; package.json now pins `10.1.0` / `4.0.1` exactly.
- **Files modified:** dashboard/web/package.json, package-lock.json
- **Commit:** 4425a90

**2. [Rule 3 - Blocking] Acceptance-grep collisions in comments**
- **Found during:** Tasks 2 & 3 — doc-comment prose contained the literal tokens the acceptance greps count (`remark-gfm`, `rehype`, `tide approve`), inflating the counts.
- **Fix:** reworded comments so those literals appear only in load-bearing code (the import / the command template).
- **Files modified:** ArtifactViewer.tsx, ApproveStrip.tsx
- **Commits:** 32a17fe, 2321cb5

## Verification

- `npx vitest run src/lib src/components/ArtifactViewer.test.tsx src/components/ApproveStrip.test.tsx` → 8 files, 52 tests green
- `npx tsc -b` → exit 0
- Full suite regression check: `npx vitest run` → 30 files, 244 tests green (api.ts additions broke nothing)

## Known Stubs

None. Both components are fully wired to the (plan-37-07) endpoint contract; they receive real data once 37-08 mounts them and 37-07 serves the routes. The `ProjectSettings` type is added for the sibling ProjectSettingsPanel (37-06) — intentional, consumed next.

## Notes for Downstream Plans

- **37-07** must serve `GET /api/v1/nodes/{kind}/{name}/artifacts?project=&namespace=` returning `{state, branch?, commitSHA?, files: [], error?}` and `GET /api/v1/projects/{name}/settings` returning the redacted `ProjectSettings` shape. The TS types are the contract mirror; a field rename on the Go side should surface here as a compile error.
- **37-08** mounts `<ArtifactViewer kind name project namespace gateParked />` in NodeDetailPanel (gateParked from node status) and `<ApproveStrip projectName />` sticky at the bottom of the panel scroll container below the artifact content.

## Self-Check: PASSED

- Files created — all present:
  - FOUND: dashboard/web/src/components/ArtifactViewer.tsx
  - FOUND: dashboard/web/src/components/ArtifactViewer.test.tsx
  - FOUND: dashboard/web/src/components/ApproveStrip.tsx
  - FOUND: dashboard/web/src/components/ApproveStrip.test.tsx
- Commits — all present: 4425a90, 32a17fe, 2321cb5
