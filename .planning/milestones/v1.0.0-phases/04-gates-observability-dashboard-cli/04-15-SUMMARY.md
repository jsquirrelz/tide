---
phase: 04-gates-observability-dashboard-cli
plan: 15
subsystem: dashboard-frontend
tags: [dashboard, react, vite, tailwind-v4, status-badge, project-picker, clipboard-copy, wave-background, lucide-react, dash-01]

requires:
  - phase: 04
    plan: 12
    provides: design tokens (--color-status-*, --color-accent, --color-destructive, --color-surface-*) + Toast + ToastProvider + useToast() + TOAST_COPY constants
provides:
  - dashboard/web/src/components/StatusBadge.tsx — 10 CRD .status.phase variants per UI-SPEC §Status Vocabulary; table-driven STATUS_TABLE exported for downstream iteration
  - dashboard/web/src/components/WaveBackground.tsx — SVG <g><rect><text> band component consumed by plan 04-13's ExecutionDAGView at z-index 0
  - dashboard/web/src/components/ProjectPicker.tsx — header dropdown for the Project list; empty / single / multi states with verbatim UI-SPEC empty-state copy
  - dashboard/web/src/components/ClipboardCopyAction.tsx — the D-D6 surface; navigator.clipboard.writeText + locked Toast emission via useToast()
affects:
  - 04-13 (DAG views + drawer) — imports StatusBadge in TaskNode + drawer status row, WaveBackground as the per-wave SVG layer behind dagre task nodes, ClipboardCopyAction in the drawer Actions row, ProjectPicker mounted via Header.projectPicker slot
  - 04-16 (log streamer + bundle gate) — no new components needed here; bundle currently 48.12KB gzipped JS (~10× headroom under the <500KB gate)

tech-stack:
  added:
    - "lucide icons imported individually (tree-shake-friendly): Ban, Circle, CircleCheck, CircleDot, CircleX, Hand, Hourglass, LockKeyhole, Loader2, Pause, ShieldAlert, ChevronDown"
  patterns:
    - "Table-driven status presentation: a Record<StatusValue, {icon, iconName, label, colorVar, srDescription, animationClass?}> literal sourced verbatim from UI-SPEC §Status Vocabulary; the component reads the row and renders. STATUS_TABLE is exported so any downstream code that needs to enumerate the 10 phases (e.g. a primitives gallery, a filter dropdown) does not duplicate the surface."
    - "Status color threading via CSS variable + color-mix: the badge sets `color: var(--color-status-*)` (full saturation foreground) and derives bg/border via `color-mix(in srgb, var(...) 15%, transparent)` / 40%. One token drives all three surfaces — light/dark theme switches by overriding the variable in .light-theme."
    - "Lucide icons wrapped in a <span data-icon=\"<name>\"> so tests assert icon identity by a stable attribute (no React-internal `displayName` or import-reference equality required)."
    - "ProjectPicker empty-state copy renders inline beside the trigger (not gated on isOpen) — operators landing on an empty cluster see the explanation without clicking; non-empty dropdowns still gate on isOpen for the operator's-eye-out-of-the-way default."
    - "ClipboardCopyAction routes all toast copy through TOAST_COPY constants from src/lib/toast-copy.ts — never inlines a string. Single grep verifies Copywriting Contract compliance."

key-files:
  created:
    - dashboard/web/src/components/StatusBadge.tsx
    - dashboard/web/src/components/StatusBadge.test.tsx
    - dashboard/web/src/components/WaveBackground.tsx
    - dashboard/web/src/components/WaveBackground.test.tsx
    - dashboard/web/src/components/ProjectPicker.tsx
    - dashboard/web/src/components/ProjectPicker.test.tsx
    - dashboard/web/src/components/ClipboardCopyAction.tsx
    - dashboard/web/src/components/ClipboardCopyAction.test.tsx
  modified: []

key-decisions:
  - "Re-export Hourglass from StatusBadge.tsx so downstream drawer chronograph affordances can share the icon set without re-importing lucide-react. Listed in plan must_haves.artifacts[StatusBadge].contains."
  - "Coerce unknown project phase strings to 'Pending' in ProjectPicker's StatusBadge consumer (KNOWN_STATUSES guard). Backend ground truth is the CRD `.status.phase` field validated by CEL, but the frontend is defensive against schema drift between K8s API server and dashboard."
  - "Empty-state copy in ProjectPicker is surfaced WITHOUT requiring the operator to click the trigger (deviation from a strict 'open-on-click' reading of UI-SPEC §9). UI-SPEC §13 E1 is the authoritative empty-state surface; rendering the copy inline-beside-trigger satisfies both §9 (the trigger renders a disabled-affordance label) and §13 (the copy is visible at all times when there are no projects to pick). Rule 2 — Missing Critical UX."
  - "ClipboardCopyAction does not throw on toast-emission failure (i.e. when rendered outside a ToastProvider). The useToast() hook already returns a no-op { toast: () => undefined } per plan 04-12's hook contract, so unit tests of leaf composition (e.g. plan 04-13's drawer) can render this button without wrapping in a provider when toast emission is not under test."
  - "WaveBackground uses camelCase SVG attribute names (strokeDasharray, fillOpacity, strokeWidth) — React serializes these to the kebab-case DOM attributes. Tests then assert against rect.getAttribute('stroke-dasharray') / 'fill-opacity' which both forms produce. Using kebab-case JSX props produced React 'invalid DOM property' warnings even though the rendered attribute was correct — switching to camelCase silences the warnings and matches @xyflow/react's own SVG attribute style."

patterns-established:
  - "StatusBadge.STATUS_TABLE is the canonical phase→presentation map; any new component (TaskNode, drawer status row) imports the table rather than re-deriving."
  - "ClipboardCopyAction is the only call site for navigator.clipboard.writeText in the dashboard SPA — downstream code should compose this component, not call the API directly. Centralizing the call keeps the locked toast-copy emission consistent and gives plan 04-16's CI bundle-gate one place to look for clipboard usage."
  - "WaveBackground is geometry-only — it accepts a precomputed `bounds` rect from the parent. Plan 04-13's ExecutionDAGView will derive the per-wave bounds from the dagre layout (min/max of member task positions + padding) and feed each band one render call."

requirements-completed: [DASH-01]

# Metrics
duration: 8 min
completed: 2026-05-19
---

# Phase 4 Plan 15: Status + Primitive Components Summary

**Shipped the 4 dashboard primitives plan 04-13 (DAG views + drawer) and 04-16 (log streamer) depend on: `<StatusBadge>` (all 10 UI-SPEC Status Vocabulary variants — icon + label + color + animation + screen-reader description, table-driven), `<WaveBackground>` (the SVG <rect> band consumed by ExecutionDAGView at z-index 0), `<ProjectPicker>` (header dropdown with empty / single / multi states), `<ClipboardCopyAction>` (the D-D6 navigator.clipboard surface routed through the locked TOAST_COPY constants).**

## Performance

- **Duration:** 8 min
- **Tasks:** 2/2
- **Files created:** 8 (4 components + 4 test files)
- **Files modified:** 0
- **Tests:** 52 passing across 7 files (+34 new: 21 StatusBadge + 5 WaveBackground + 3 ProjectPicker + 5 ClipboardCopyAction)
- **Bundle (after vite build):** 48.12KB gzipped JS + 3.46KB gzipped CSS = ~51.6KB total — well under the plan 04-16 <500KB gate.

## Accomplishments

- **StatusBadge — all 10 CRD `.status.phase` variants** rendered exactly per UI-SPEC §Status Vocabulary. STATUS_TABLE map drives icon identity (lucide), label text, color token, and animation class. `aria-label = "Status: <verbatim sr description>"` from the spec table. `animate-spin` on Dispatching, `animate-pulse` on Running; both honor `prefers-reduced-motion` via the global @media rule from plan 04-12. Tinted background + border derived from the same status color via `color-mix(in srgb, var(--color-status-*) 15% | 40%, transparent)` — one variable drives all three surfaces so light/dark theme switches by overriding the variable in `.light-theme`.

- **WaveBackground — SVG band component** ready for plan 04-13's ExecutionDAGView. Pure `<g>` containing `<rect>` (the band) + `<text>` (the "WAVE N · X tasks" label). Inactive band: surface-overlay fill @ 0.4 opacity. Active dispatch: surface-overlay fill @ 0.6 + 1px accent stroke + 4/2 dasharray (UI-SPEC §6 active band styling). Failed band (`failedCount > 0`): status-blocked fill @ 0.3 opacity. The component is geometry-only — the parent owns the dagre layout and feeds bounds in.

- **ProjectPicker — header dropdown** for the Project list. Empty cluster surfaces the verbatim UI-SPEC §13 E1 copy ("No projects in this cluster") inline beneath the trigger; non-empty dropdowns gate the panel on `isOpen` to stay out of the operator's way. Multi-state rows render `<namespace>/<name>` (namespace muted, name primary) + a `<StatusBadge>` for the project's current phase. Closes on outside-click + Escape (UI-SPEC §9). Selecting a row fires `onChange(name)` and closes the panel. Defensive against backend schema drift — any unknown phase string coerces to "Pending" before reaching StatusBadge.

- **ClipboardCopyAction — the D-D6 surface.** Single button. `await navigator.clipboard.writeText(command)` then emits the locked TOAST_COPY.clipboardCopySuccess toast (`"Command copied"` / `"Paste in your terminal to run: <command>"`). On rejection: TOAST_COPY.clipboardCopyFailure (`"Couldn't copy"` / `"Clipboard API blocked. Command: <command>"`, duration 8000ms). Three variants: `primary` (bg-accent), `destructive` (border-destructive), `secondary` (border-subtle). The component is the ONLY call site for `navigator.clipboard.writeText` in the dashboard SPA — centralizing the call keeps the locked toast-copy emission consistent and gives plan 04-16's CI bundle-gate one grep to verify.

- **T-04-D1 XSS mitigation re-asserted by the existing `no-dangerous-html.test.ts` guard from plan 04-12 — zero `dangerouslySetInnerHTML` uses in any of the 4 new `.tsx` files.

## Task Commits

| Task | Phase  | Hash      | Type    |
| ---- | ------ | --------- | ------- |
| 1    | RED    | `9800cb6` | test    |
| 1    | GREEN  | `65d8608` | feat    |
| 2    | RED    | `026f7a2` | test    |
| 2    | GREEN  | `2db6fd5` | feat    |

Plan metadata commit (this SUMMARY.md) follows.

## Files Created

### Source components

- `dashboard/web/src/components/StatusBadge.tsx` — 10-variant table-driven status pill; exports `StatusValue` type, `StatusBadge` default, `STATUS_TABLE` map, and re-exports `Hourglass` for downstream chronograph use.
- `dashboard/web/src/components/WaveBackground.tsx` — SVG band component; props `{ waveIndex, bounds: {x,y,width,height}, isActiveDispatch, taskCount, failedCount? }`.
- `dashboard/web/src/components/ProjectPicker.tsx` — header dropdown; props `{ projects: ProjectEntry[], value: string|null, onChange: (name) => void }`; closes on outside-click + Escape.
- `dashboard/web/src/components/ClipboardCopyAction.tsx` — D-D6 clipboard button; props `{ command, label, variant?, description? }`.

### Tests

- `dashboard/web/src/components/StatusBadge.test.tsx` — 21 tests: 10 table-driven (icon + label + aria-label + color), 10 table-driven (animation class routing), 1 shape assertion.
- `dashboard/web/src/components/WaveBackground.test.tsx` — 5 tests: geometry, label text, active-dispatch styling, inactive (no stroke-dasharray), failed-color routing, inactive fill.
- `dashboard/web/src/components/ProjectPicker.test.tsx` — 3 tests: empty state copy, single-project dropdown open+select fires onChange, multi-project rows + StatusBadge containment.
- `dashboard/web/src/components/ClipboardCopyAction.test.tsx` — 5 tests: success path writes + emits Command-copied toast, failure path emits Couldn't-copy alert toast, 3 variant classNames.

## StatusBadge Table-Driven Shape

The component never branches on a `switch(status)` — it indexes into the table:

```ts
const row = STATUS_TABLE[status];
const Icon = row.icon;
// inline-flex pill: color = row.colorVar; bg = color-mix(15%); border = color-mix(40%)
return (
  <span aria-label={`Status: ${row.srDescription}`} ...>
    <span data-icon={row.iconName} className={clsx("inline-flex", row.animationClass)}>
      <Icon size={14} aria-hidden="true" />
    </span>
    <span>{row.label}</span>
  </span>
);
```

Tests iterate `Object.keys(EXPECTED)` instead of hand-listing 10 spec'd cases — adding a future status is one row in `STATUS_TABLE` + one row in `EXPECTED` and the tests automatically cover it.

## TOAST_COPY Constants Used (no new additions)

Plan 04-12 already shipped `clipboardCopySuccess` and `clipboardCopyFailure` in `src/lib/toast-copy.ts`. This plan consumes both verbatim via `TOAST_COPY.clipboardCopySuccess.title` / `.body(command)` and `TOAST_COPY.clipboardCopyFailure.title` / `.body(command)` / `.duration` — no new constants needed. The plan's optional "extend that module with the two new constants if not present" branch was a no-op for this implementation.

## WaveBackground SVG Geometry Consumed by ExecutionDAGView

The component emits a `<g data-testid="wave-background-<N>" data-wave-index="N" data-active-dispatch="…" data-failed="…">` containing:

```svg
<rect x={bounds.x} y={bounds.y} width={bounds.width} height={bounds.height}
      fill="var(--color-surface-overlay)|var(--color-status-blocked)"
      fill-opacity="0.4|0.6|0.3"
      stroke="var(--color-accent)"      <!-- only when isActiveDispatch -->
      stroke-width="1"                  <!-- only when isActiveDispatch -->
      stroke-dasharray="4 2"            <!-- only when isActiveDispatch -->
      aria-hidden="true" />
<text x={bounds.x + 8} y={bounds.y + 16}
      fill="var(--color-text-muted)"
      style="font-mono; 12px; 600"
      aria-hidden="true">
  WAVE {waveIndex} · {taskCount} tasks
</text>
```

Plan 04-13's ExecutionDAGView feeds this component once per wave with `bounds` computed from the dagre-laid-out member task positions (min/max + padding). The `aria-hidden="true"` is intentional — wave relationships are conveyed via `<TaskNode>` aria-labels; the band label is sighted-scanning only.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 2 — Missing Critical UX] ProjectPicker empty-state copy renders inline (not gated on isOpen)**
- **Found during:** Task 2 GREEN — the failing test `getByText("No projects in this cluster")` expects the copy to be visible immediately, but my first implementation only rendered it inside the panel (`isOpen=true`).
- **Issue:** A strict "click trigger to open panel" reading of UI-SPEC §9 would make the empty-state copy invisible until interaction. UI-SPEC §13 E1 is the authoritative empty-state surface and says the copy is centered in the pane — i.e. always visible when the cluster has no projects.
- **Fix:** Render the empty-state copy inline beneath the trigger whenever `projects.length === 0`. The trigger still shows the "No projects" label + chevron (UI-SPEC §9 visual consistency); the §13 E1 explanation appears below it without requiring a click. Non-empty dropdowns still gate the panel on `isOpen` for the operator's-eye-out-of-the-way default.
- **Files modified:** `dashboard/web/src/components/ProjectPicker.tsx` (single-commit GREEN — no separate commit).
- **Committed in:** `2db6fd5` (Task 2 GREEN commit).

**2. [Rule 1 — Bug] WaveBackground React 'invalid DOM property' warnings**
- **Found during:** Task 1 GREEN — initial run showed `Warning: Invalid DOM property fill-opacity. Did you mean fillOpacity?` for each rect attribute.
- **Issue:** I passed kebab-case strings (`"fill-opacity"`, `"stroke-width"`, `"stroke-dasharray"`) as JSX prop names. React's SVG attribute serializer expects camelCase prop names and emits a warning when it has to normalize. The rendered DOM attributes were correct (kebab-case is what the browser sees) but the test stderr was noisy.
- **Fix:** Switched the rect prop bag to a typed `RectProps` literal using camelCase prop names (`fillOpacity`, `strokeWidth`, `strokeDasharray`). React still serializes to kebab-case attributes on the DOM, and tests' `getAttribute("stroke-dasharray")` calls still pass.
- **Files modified:** `dashboard/web/src/components/WaveBackground.tsx`.
- **Committed in:** `65d8608` (Task 1 GREEN commit).

---

**Total deviations:** 2 auto-fixed (1 Rule 2 missing-critical-UX, 1 Rule 1 bug). No scope creep — both fixes were inside the per-task envelope and unblocked tests the plan already required.

## Known Stubs

None. The 4 primitives ship with all behavior the plan specified; no placeholder content. ProjectPicker mounts via `Header.projectPicker` slot in plan 04-13 (App.tsx integration), but that is a normal cross-plan wiring point — not a stub in the components themselves.

## Issues Encountered

None beyond the deviations above.

## Self-Check: PASSED

- **Files exist:** all 8 created files present under `dashboard/web/src/components/`.
- **Commits exist:** `9800cb6`, `65d8608`, `026f7a2`, `2db6fd5` all visible in `git log --oneline`.
- **Verification gates green:**
  - `npm run test` → 52 passing across 7 files (was 18 at 04-12 baseline; +34 new).
  - `npm run build` → 48.12KB gzipped JS + 3.46KB gzipped CSS.
  - `npm run lint` (tsc -b) → clean.
  - `grep -r "dangerouslySetInnerHTML" dashboard/web/src/components/{StatusBadge,WaveBackground,ProjectPicker,ClipboardCopyAction}.tsx` → 0 hits.
  - StatusBadge renders all 10 UI-SPEC variants (21 assertions in StatusBadge.test.tsx confirm icon + label + aria-label + color + animation + shape).
  - Plan must_haves.artifacts.contains satisfied: `Hourglass` in StatusBadge.tsx (5 hits), `ProjectPicker` in ProjectPicker.tsx (4 hits), `navigator.clipboard` in ClipboardCopyAction.tsx (3 hits), `WAVE` in WaveBackground.tsx (4 hits).

## Next Plan Readiness

- **Plan 04-13** (DAG views + drawer): can import `StatusBadge` for TaskNode + drawer status row, `WaveBackground` as the per-wave SVG layer (one render per wave inside the @xyflow `<Background>` slot), `ClipboardCopyAction` for the drawer Actions row (the 7 status×action pairings from UI-SPEC §10 are all one-liners over this component), `ProjectPicker` wired through `<Header projectPicker={…} />` in App.tsx.
- **Plan 04-16** (log streamer + bundle gate): no new component dependencies here; current bundle is 48.12KB gzipped JS (~10× headroom under the <500KB gate).

---
*Phase: 04-gates-observability-dashboard-cli*
*Plan: 15*
*Completed: 2026-05-19*
