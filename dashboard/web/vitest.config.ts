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
  test: {
    environment: "jsdom",
    setupFiles: ["./src/__tests__/setup.ts"],
    globals: true,
    css: false,
  },
});
