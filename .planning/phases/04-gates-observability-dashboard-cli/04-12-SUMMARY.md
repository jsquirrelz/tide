---
phase: 04-gates-observability-dashboard-cli
plan: 12
subsystem: dashboard-frontend
tags: [dashboard, react, vite, tailwind-v4, typescript, vitest, react-flow, lucide-react, dash-01]

requires:
  - phase: 04
    plan: 10
    provides: cmd/dashboard chi router + embed.FS SPA shim (cmd/dashboard/embed/dist/) — this plan does NOT overwrite the embed dir; Makefile target dashboard-frontend (from plan 04-10) copies dashboard/web/dist into cmd/dashboard/embed/dist at backend-build time
provides:
  - dashboard/web/ — Vite 6 + React 18 + TypeScript 5 + Tailwind v4 + @xyflow/react v12 scaffold
  - src/index.css single @theme block — all UI-SPEC design tokens (60/30/10 color split + light overrides + 8-point spacing + 3-size + mono type system + 6-family status colors)
  - 4 chrome components — AppShell, Header, ConnectionStatusIndicator, Toast + ToastContainer/Provider
  - src/lib/toast-copy.ts — locked Copywriting Contract strings as named constants
  - src/lib/clsx.ts — conditional className composition re-export
  - Vitest + @testing-library/react bootstrap with 18 passing tests
  - T-04-D1 XSS guard test — fs-scan that fails CI on any dangerouslySetInnerHTML use
affects:
  - 04-13 (DAG views + drawer) — replaces the App.tsx grid-cols-2 placeholder with PlanningDAGView + ExecutionDAGView + resizable divider + TaskDetailDrawer
  - 04-15 (status + primitive components) — builds StatusBadge + CopyCommandButton + Dropdown + ProjectPicker on top of the design tokens declared here
  - 04-16 (log streamer + bundle gate) — adds PodLogStreamer and the <500KB gzipped bundle CI assertion (this plan ships at ~51KB total which leaves comfortable headroom)

tech-stack:
  added:
    - react@^18.3.1 + react-dom@^18.3.1 (UI-SPEC pin)
    - typescript@^5.6.3
    - @types/react@^18.3.29 + @types/react-dom@^18.3.7 + @types/node@^22.19.19 + @types/dagre@^0.7.54
    - vite@^6.4.2 + @vitejs/plugin-react@^4.7.0
    - tailwindcss@^4.3.0 + @tailwindcss/vite@^4.3.0
    - "@xyflow/react@^12.10.2 (reserved for plan 04-13's DAG renderer)"
    - dagre@^0.8.5 (reserved for plan 04-13's auto-layout)
    - lucide-react@^1.16.0 (icon set — tree-shakeable, only kind icons + status icons imported downstream)
    - clsx@^2.1.1
    - vitest@^1.6.1 + @testing-library/react@^16.3.2 + @testing-library/dom@^10.4.1 + @testing-library/jest-dom@^6.9.1 + jsdom@^24.1.3
  patterns:
    - Single @theme directive holds the entire design-token surface; .light-theme overrides only the deltas from dark
    - Locked Copywriting Contract strings live in src/lib/toast-copy.ts as TOAST_COPY constants — emitters import, never inline (so gsd-ui-checker can grep one file)
    - useToast() returns a no-op when called outside ToastProvider — leaf components can use the hook without a wrapping provider in unit tests
    - Project-references tsconfig split (tsconfig.app.json for src, tsconfig.node.json for vite/vitest configs) — sidesteps vitest@1's nested-vite Plugin<any> type collision
    - vitest config sets esbuild.jsx = "automatic" so test files don't need `import React`; avoids needing @vitejs/plugin-react in the vitest config which would conflict with the user-level vite

key-files:
  created:
    - dashboard/web/package.json
    - dashboard/web/package-lock.json
    - dashboard/web/.nvmrc
    - dashboard/web/.gitignore
    - dashboard/web/index.html
    - dashboard/web/vite.config.ts
    - dashboard/web/vitest.config.ts
    - dashboard/web/tsconfig.json
    - dashboard/web/tsconfig.app.json
    - dashboard/web/tsconfig.node.json
    - dashboard/web/src/main.tsx
    - dashboard/web/src/App.tsx
    - dashboard/web/src/index.css
    - dashboard/web/src/lib/clsx.ts
    - dashboard/web/src/lib/toast-copy.ts
    - dashboard/web/src/__tests__/setup.ts
    - dashboard/web/src/__tests__/no-dangerous-html.test.ts
    - dashboard/web/src/components/AppShell.tsx
    - dashboard/web/src/components/Header.tsx
    - dashboard/web/src/components/ConnectionStatusIndicator.tsx
    - dashboard/web/src/components/Toast.tsx
    - dashboard/web/src/components/ToastContainer.tsx
    - dashboard/web/src/components/Toast.test.tsx
    - dashboard/web/src/components/ConnectionStatusIndicator.test.tsx
  modified: []

key-decisions:
  - "Split tsconfig.json into project-references (tsconfig.app.json + tsconfig.node.json) to isolate the vite/vitest configs from the src/ type-check, sidestepping the vitest@1 nested-vite vs user-level vite Plugin<any> type collision (the Plugin types are nominally distinct between the two installs)."
  - "Use Tailwind v4 @theme as the SINGLE source of truth for design tokens — no Tailwind plugins, no postcss config, no tailwind.config.ts. Tailwind v4's CSS-first approach reads --color-*, --spacing-*, --text-* from @theme directly."
  - "Locked toast copy strings live in src/lib/toast-copy.ts as TOAST_COPY constants so the gsd-ui-checker (and plan 04-16's CI guard) can verify Copywriting Contract compliance with a single grep."
  - "clsx@^2 is re-exported through src/lib/clsx.ts so plan 04-16 can swap to a 10-line zero-dep variant (UI-SPEC §Stack & Bundle Targets explicit option) without rewriting any call sites if the bundle gate ever shows clsx as a measurable contributor."
  - "@xyflow/react v12 and dagre are pinned in package.json but no module imports them yet — they're reserved for plan 04-13 (DAG renderer + auto-layout). Pinning here so plan 04-13's first commit doesn't fight a fresh-install."

patterns-established:
  - "Design-token-driven component styling: components reference CSS variables via `var(--color-*)` inline styles or Tailwind v4 arbitrary-value class syntax (`bg-[var(--color-surface-raised)]`). Plan 04-15 + 04-13 maintain this — no arbitrary hex literals in component JSX."
  - "Behavior-locked tests assert against locked design tokens (e.g. inline-style border-left color = `var(--color-status-success)`) so plan 04-16 can refactor token values without breaking these tests."
  - "React Context hooks degrade gracefully: useToast() outside a provider returns a no-op `{ toast: () => undefined }` so leaf-component unit tests don't need a provider wrapper."

requirements-completed: [DASH-01]

# Metrics
duration: 3 min
completed: 2026-05-19
---

# Phase 4 Plan 12: dashboard/web Bootstrap Summary

**Greenfield Vite 6 + React 18 + Tailwind v4 + @xyflow/react v12 scaffold for the read-only TIDE dashboard SPA, with the UI-SPEC design-token surface locked in a single @theme directive and the chrome (AppShell + Header + ConnectionStatusIndicator + Toast) shipped with 18 passing Vitest behavior tests.**

## Performance

- **Duration:** 3 min
- **Started:** 2026-05-19T23:40:13Z
- **Completed:** 2026-05-19T23:44Z
- **Tasks:** 2/2
- **Files created:** 24
- **Files modified:** 0 (greenfield)
- **Tests:** 18 passing across 3 test files
- **Bundle (after vite build):** 48.12KB gzipped JS + 3.20KB gzipped CSS = ~51KB total — ~10× headroom under the plan 04-16 <500KB gate.

## Accomplishments

- **Pinned toolchain:** every dependency at the exact major from UI-SPEC §Stack & Bundle Targets. React 18.3.1, Vite 6.4.2, TypeScript 5.6.3, Tailwind v4.3.0 (via @tailwindcss/vite), @xyflow/react 12.10.2, dagre 0.8.5, vitest 1.6.1, @testing-library/react 16.3.2, lucide-react 1.16.0, clsx 2.1.1. Node 22 pinned via `.nvmrc`.
- **Single-file design-token surface:** src/index.css carries one `@theme` block holding the entire dark-default palette (60/30/10 split, status colors, accent reserved-for invariant), the 8-point spacing scale, the 3-size + mono typography system (system font stack only — zero webfont bytes), and a `.light-theme` block declaring only the deltas from dark per UI-SPEC §Color.
- **Chrome shipped:** AppShell (top-level layout with localStorage-persisted splitRatio reserved for plan 04-13's resizable divider), Header (wordmark + ProjectPicker slot + ConnectionStatusIndicator slot + theme cycle button), ConnectionStatusIndicator (3 verbatim-copy states with `role="status" aria-live="polite"`), Toast + ToastContainer/Provider (4 variants × locked border tokens × ARIA role mapping + useToast() hook with no-op fallback outside provider).
- **18 Vitest tests passing:** Toast variants (4), Toast copy contract (3), Toast provider integration (2), Light-theme palette resolution (2), ConnectionStatusIndicator (6), T-04-D1 dangerouslySetInnerHTML guard (1).
- **T-04-D1 XSS mitigation closed at build time:** `src/__tests__/no-dangerous-html.test.ts` walks every .tsx/.ts under src/ and asserts zero `dangerouslySetInnerHTML` uses. The guard test itself is excluded so the literal string above stays the only allowed match in the entire src tree.
- **Dev proxy wired:** `/api` → `:8080` (cmd/dashboard backend from plan 04-10), `/healthz` → `:8081` (manager probe). `npm run dev` boots at `http://localhost:5173`.

## Task Commits

Each task was committed atomically:

1. **Task 1: dashboard/web scaffold + chrome stubs** — `80b6d04` (feat)
2. **Task 2: Chrome behavior tests + T-04-D1 XSS guard** — `d4f7de2` (test)

Plan metadata commit (this SUMMARY.md) follows.

## Files Created

### Build & config

- `dashboard/web/package.json` — pinned dependencies, npm scripts (`dev`/`build`/`preview`/`test`/`test:watch`/`lint`), `"type": "module"`.
- `dashboard/web/package-lock.json` — npm install lockfile.
- `dashboard/web/.nvmrc` — `22` (RESEARCH §A3 pin).
- `dashboard/web/.gitignore` — `node_modules/`, `dist/`, `.vite/`, logs, `.DS_Store`.
- `dashboard/web/index.html` — `<div id="root">` + `<meta name="viewport" content="width=1280">` (UI-SPEC primary target).
- `dashboard/web/vite.config.ts` — @vitejs/plugin-react + @tailwindcss/vite plugins, ES2020 build target, dev proxy.
- `dashboard/web/vitest.config.ts` — jsdom env, setup file, `esbuild.jsx: "automatic"` so tests don't need `import React`.
- `dashboard/web/tsconfig.json` — project-references root.
- `dashboard/web/tsconfig.app.json` — strict src/ config with `noEmit: true` + composite for project references; types include `node`, `vitest/globals`, `@testing-library/jest-dom`.
- `dashboard/web/tsconfig.node.json` — relaxed config for vite.config.ts / vitest.config.ts.

### Source

- `dashboard/web/src/main.tsx` — React 18 `createRoot` entry, StrictMode-wrapped.
- `dashboard/web/src/App.tsx` — `<ToastProvider><AppShell header={<Header connectionStatus={…}>}>…</AppShell></ToastProvider>` with a placeholder grid-cols-2 for the two panes (plan 04-13 replaces).
- `dashboard/web/src/index.css` — single `@theme` block + `.light-theme` overrides + base body + focus ring + `prefers-reduced-motion` query.
- `dashboard/web/src/lib/clsx.ts` — re-export of `clsx` (10-line zero-dep alt acceptable per UI-SPEC).
- `dashboard/web/src/lib/toast-copy.ts` — locked `TOAST_COPY` constants from UI-SPEC §Copywriting Contract.
- `dashboard/web/src/__tests__/setup.ts` — `import '@testing-library/jest-dom'`.
- `dashboard/web/src/__tests__/no-dangerous-html.test.ts` — fs-scan T-04-D1 guard.
- `dashboard/web/src/components/AppShell.tsx` — header + body layout, localStorage `splitRatio` state.
- `dashboard/web/src/components/Header.tsx` — wordmark + projectPicker slot + connectionStatus slot + theme cycle button.
- `dashboard/web/src/components/ConnectionStatusIndicator.tsx` — 3-state pill with verbatim labels + role/aria.
- `dashboard/web/src/components/Toast.tsx` — 4-variant toast with locked border tokens + variant→ARIA role mapping.
- `dashboard/web/src/components/ToastContainer.tsx` — `<ToastProvider>` + `useToast()` hook + portal mount.
- `dashboard/web/src/components/Toast.test.tsx` — 11 Vitest tests.
- `dashboard/web/src/components/ConnectionStatusIndicator.test.tsx` — 6 Vitest tests.

## XSS Guard Test Pattern (T-04-D1)

The XSS gate from the plan's threat model is closed by `src/__tests__/no-dangerous-html.test.ts`. The pattern (Vitest can both import test files and execute fs scans inside `describe`/`it`):

```ts
import { readdirSync, readFileSync, statSync } from "node:fs";
import { join, relative, resolve } from "node:path";

const SRC_DIR = resolve(process.cwd(), "src");

function walk(dir: string, out: string[] = []): string[] {
  for (const entry of readdirSync(dir)) {
    if (entry === "node_modules" || entry.startsWith(".")) continue;
    const abs = join(dir, entry);
    const st = statSync(abs);
    if (st.isDirectory()) walk(abs, out);
    else if (/\.(tsx?|jsx?)$/.test(entry)) out.push(abs);
  }
  return out;
}

it("no .tsx/.ts file under src/ contains the dangerouslySetInnerHTML prop", () => {
  const offenders: string[] = [];
  for (const f of walk(SRC_DIR)) {
    if (f.endsWith("no-dangerous-html.test.ts")) continue; // skip self
    if (readFileSync(f, "utf-8").includes("dangerouslySetInnerHTML")) {
      offenders.push(relative(SRC_DIR, f));
    }
  }
  expect(offenders).toEqual([]);
});
```

The guard self-exclusion is essential — without it, the literal string in the assertion would trigger the test. Plan 04-15 and 04-13 inherit this gate transparently because any new .tsx file gets scanned.

## Decisions Made

- **Tailwind v4 @theme as the only token surface (no tailwind.config.ts, no postcss config).** Tailwind v4's CSS-first approach reads `--color-*`, `--spacing-*`, `--text-*` from `@theme` directly; no JS/TS config is necessary. Keeps the design-token edit surface in one file.
- **TS project references (tsconfig.app.json + tsconfig.node.json) sidesteps vitest@1's nested-vite type collision.** vitest@1 ships its own nested `vite` package; the `Plugin<any>` exported by `vitest/config` is nominally distinct from the `Plugin<any>` exported by the user-level `vite`. Type-checking vite.config.ts (which imports @vitejs/plugin-react against the user-level vite) and vitest.config.ts in the same tsconfig produces a structural-but-not-nominal mismatch. Splitting the configs (and dropping `@vitejs/plugin-react` from vitest.config.ts — vitest 1.x's bundled esbuild handles JSX) eliminates the conflict.
- **vitest esbuild.jsx = "automatic".** Test files don't need an explicit `import React`. Required because we dropped `@vitejs/plugin-react` from vitest.config.ts (see previous bullet).
- **clsx re-exported via src/lib/clsx.ts.** UI-SPEC §Stack & Bundle Targets allows a 10-line zero-dep alt; the indirection lets plan 04-16 swap implementations without touching call sites if the bundle gate flags clsx.
- **@xyflow/react v12 + dagre pinned but not yet imported.** Plan 04-13 owns the DAG renderer; pinning here so the first 04-13 commit doesn't fight a fresh `npm install`.
- **App.tsx wraps AppShell in ToastProvider, not the other way around.** ToastProvider is global (any subtree can `useToast()`); AppShell is layout. Keeps the toast portal alive even if AppShell ever conditionally unmounts.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 — Blocking] Split tsconfig.json into project references**
- **Found during:** Task 1 verification (`npm run build`).
- **Issue:** Single `tsconfig.json` with both `vite.config.ts` and `vitest.config.ts` in `include` caused `tsc --noEmit` to fail with a `Plugin<any>` type-mismatch between the user-level `vite` and `vitest`'s bundled nested `vite`.
- **Fix:** Replaced the single tsconfig with `tsconfig.json` (project-references root) + `tsconfig.app.json` (src/) + `tsconfig.node.json` (vite + vitest configs). Each project type-checks against its own vite resolution, eliminating the conflict.
- **Files modified:** dashboard/web/tsconfig.json (created), dashboard/web/tsconfig.app.json (created), dashboard/web/tsconfig.node.json (created), dashboard/web/package.json (`lint` and `build` scripts switched to `tsc -b`).
- **Verification:** `npm run build` clean, `npm run test` clean.
- **Committed in:** 80b6d04 (Task 1 commit)

**2. [Rule 3 — Blocking] Set noEmit: true in tsconfig.app.json**
- **Found during:** Task 1 verification.
- **Issue:** TypeScript's `composite: true` (required for project references) forced declaration emit and JSX emit, which sprayed `.d.ts` and `.jsx` files into `src/`.
- **Fix:** Added `"noEmit": true` to `tsconfig.app.json`. TypeScript 5.6 supports composite + noEmit (each project tracks dependencies via `.tsbuildinfo` written to `node_modules/.tmp/`).
- **Files modified:** dashboard/web/tsconfig.app.json.
- **Verification:** `find dashboard/web/src -type f \( -name '*.d.ts' -o -name '*.jsx' -o -name '*.js' \)` is empty.
- **Committed in:** 80b6d04 (Task 1 commit)

**3. [Rule 3 — Blocking] Dropped @vitejs/plugin-react from vitest.config.ts + switched to esbuild automatic JSX**
- **Found during:** First Vitest run after committing Task 2 RED-shaped tests.
- **Issue:** Importing `@vitejs/plugin-react` from `vitest.config.ts` triggered the same `Plugin<any>` type mismatch as in #1 (the plugin resolves against user-level vite, but `defineConfig` resolves against vitest's nested vite). Removing the plugin then surfaced "ReferenceError: React is not defined" because vitest's default esbuild JSX is classic-runtime.
- **Fix:** Dropped `@vitejs/plugin-react` from vitest.config.ts (vitest 1.x transforms JSX via its bundled esbuild). Added `esbuild: { jsx: "automatic" }` to vitest.config.ts so test files don't need an explicit `import React`.
- **Files modified:** dashboard/web/vitest.config.ts.
- **Verification:** `npm run test` passes 18/18.
- **Committed in:** d4f7de2 (Task 2 commit)

**4. [Rule 2 — Missing Critical] Added @types/node to devDependencies**
- **Found during:** Task 2 final verification (`npm run build` after adding the fs-scan test).
- **Issue:** The T-04-D1 guard test imports from `node:fs` and `node:path` and references `process.cwd()`. Without `@types/node`, `tsc -b` failed with `TS2307 Cannot find module 'node:fs'` and `TS2591 Cannot find name 'process'`. This is "missing critical functionality" because the guard test is part of the plan's verification surface and won't compile without the types.
- **Fix:** `npm install --save-dev @types/node@^22` and added `"node"` to the `types` array in tsconfig.app.json.
- **Files modified:** dashboard/web/package.json, dashboard/web/package-lock.json, dashboard/web/tsconfig.app.json.
- **Verification:** `npm run build` clean.
- **Committed in:** d4f7de2 (Task 2 commit)

**5. [Rule 1 — Bug] Wrap synchronous toast() emit in act() for the provider integration test**
- **Found during:** First Vitest run after the JSX-automatic fix.
- **Issue:** The `useToast()` integration test calls `.click()` directly on a button; the resulting `setStack` is batched by React 18 and the DOM doesn't update synchronously, so `getByText("Command copied")` failed.
- **Fix:** Wrapped the `.click()` call in `act(() => { … })` from `@testing-library/react` so React flushes the state update before the assertion runs.
- **Files modified:** dashboard/web/src/components/Toast.test.tsx.
- **Verification:** Test passes; entire suite 18/18.
- **Committed in:** d4f7de2 (Task 2 commit)

**6. [Rule 1 — Bug] Replaced `fileURLToPath(new URL(...))` with `process.cwd()` resolution**
- **Found during:** First Vitest run of `no-dangerous-html.test.ts`.
- **Issue:** Under jsdom, `fileURLToPath(new URL("../", import.meta.url))` threw `ERR_INVALID_URL_SCHEME` because jsdom's URL polyfill rejects `file://` URLs.
- **Fix:** Use `resolve(process.cwd(), "src")` — vitest invokes tests with `process.cwd()` set to the package root, so this is both portable and simpler.
- **Files modified:** dashboard/web/src/__tests__/no-dangerous-html.test.ts.
- **Verification:** Test passes.
- **Committed in:** d4f7de2 (Task 2 commit)

---

**Total deviations:** 6 auto-fixed (4 Rule 3 blocking, 1 Rule 2 missing-critical, 1 Rule 1 bug).
**Impact on plan:** All 6 fixes were tooling-shape adjustments required by the exact stack the plan pins (vitest@1 + vite@6 type-resolution shape, TypeScript composite/noEmit semantics, jsdom URL polyfill). None changed the design-token surface, the chrome contract, or the test behaviors. Zero scope creep — every fix unblocked a verification gate the plan already required.

## Known Stubs

The chrome ships with two intentional placeholders that downstream plans will resolve:

- **Header projectPicker slot** — passes through `props.projectPicker` (defaults to `undefined`). Plan 04-15 builds `<ProjectPicker>` and wires it through App.tsx.
- **AppShell splitRatio state** — persisted to localStorage but no UI affordance to change it (locked at 0.5). Plan 04-13 adds the resizable divider drag handle that updates the state.
- **App.tsx grid-cols-2 placeholder content** — the two pane divs render literal text "Planning DAG placeholder (plan 04-13)" / "Execution DAG placeholder (plan 04-13)" in `--color-text-muted`. Plan 04-13 replaces with `<PlanningDAGView>` + `<ExecutionDAGView>`.

These are NOT XSS-via-empty-data risks — the text is hardcoded English strings, not data flowing from any backend. They're explicit affordances the plan calls out (line 156-157 of 04-12-PLAN.md).

## Issues Encountered

None beyond the deviations documented above. All deviations were tooling-config-shape mismatches surfaced by `npm run build` / `npm run test` and fixed by reading the error trace and applying the standard vite-template idiom (project references, esbuild jsx, @types/node).

## Self-Check: PASSED

- Files exist: all 24 created files present under `dashboard/web/`.
- Commits exist: `80b6d04` and `d4f7de2` both visible in `git log --oneline`.
- Verification gates green:
  - `npm ci && npm run build` succeeds (48.12KB gzipped JS + 3.20KB gzipped CSS).
  - `npm run test` passes 18/18 across 3 test files.
  - `grep -r dangerouslySetInnerHTML src/` returns only the guard test (which self-excludes).
  - `grep -rE "@font-face|@import url.*fonts" src/` returns zero hits.
  - `grep -cE "^[^#]*@theme" src/index.css` returns exactly 1.

## Next Plan Readiness

- **Plan 04-13** (DAG views + drawer): can import `@xyflow/react` v12 and `dagre` without further install. Design tokens for selected-node ring (`--color-accent`), border (`--color-border-subtle`/`--color-border-strong`), and node body fill (`--color-surface-raised`) are already declared. App.tsx's `grid-cols-2` placeholder is the seam where `PlanningDAGView` (left) and `ExecutionDAGView` (right) plug in.
- **Plan 04-15** (status + primitive components): can build `<StatusBadge>` (UI-SPEC §Status Vocabulary 10 statuses), `<CopyCommandButton>`, `<Dropdown>`, `<ProjectPicker>` against the existing token surface. Toast emission via `useToast()` is already wired so CopyCommandButton can emit the locked clipboard-copy toast (`TOAST_COPY.clipboardCopySuccess`/`clipboardCopyFailure`) without re-declaring the strings.
- **Plan 04-16** (log streamer + bundle gate): can add the `<500KB` CI assertion confidently — current bundle is 48.12KB JS + 3.20KB CSS = ~51KB, leaves ~10× headroom for the four upstream component plans.

---
*Phase: 04-gates-observability-dashboard-cli*
*Plan: 12*
*Completed: 2026-05-19*
