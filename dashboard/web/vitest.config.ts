/// <reference types="vitest" />
import { defineConfig } from "vitest/config";

// Note: We intentionally don't include `@vitejs/plugin-react` here. Vitest 1.x
// transforms JSX/TSX via its bundled esbuild — the @vitejs/plugin-react import
// would resolve to the user-level `vite` package while `vitest/config` resolves
// to vitest's nested `vite`, producing a Plugin<any> type-mismatch under
// strict TS (the two Plugin types are nominally distinct).
//
// For unit tests that don't exercise Fast Refresh or React DevTools, esbuild's
// classic-runtime JSX transform is sufficient and faster.

export default defineConfig({
  esbuild: {
    jsx: "automatic",
  },
  test: {
    environment: "jsdom",
    setupFiles: ["./src/__tests__/setup.ts"],
    globals: true,
    css: false,
    // Raise the per-test / per-hook budget well above vitest's 5000ms default.
    // The full suite runs 34 files in parallel; on a contended CI runner a
    // render test with multiple sequential findBy* awaits (ArtifactViewer Test 1)
    // can exceed 5s of wall-clock even though it completes in ~200ms in
    // isolation. asyncUtilTimeout (setup.ts) governs how long a single findBy
    // polls; testTimeout must exceed the cumulative so the test doesn't abort
    // mid-await. 20s is ~100x the isolated runtime — generous headroom for CI
    // contention without masking a genuine hang.
    testTimeout: 20_000,
    hookTimeout: 20_000,
  },
});
