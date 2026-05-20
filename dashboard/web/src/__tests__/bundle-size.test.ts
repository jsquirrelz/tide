/*
 * bundle-size.test.ts (plan 04-16 Task 2)
 *
 *   CI gate per UI-SPEC §Stack & Bundle Targets: total gzipped JS + CSS
 *   under dashboard/web/dist/ must be ≤ 500KB.
 *
 *   Pre-build smoke: if dist/ doesn't exist (test run before `npm run
 *   build`), the assertion is skipped — the gate runs as part of the
 *   `make dashboard-frontend` invocation which builds before testing.
 *
 *   T-04-D-bundle-bloat mitigation: an unintentional bundle bloat (a
 *   forbidden dependency, a 256-color ANSI lib, a stray import-all of
 *   lucide-react) will trip this gate immediately and fail the build.
 */
import { describe, expect, it } from "vitest";
import { readFileSync, existsSync, readdirSync, statSync } from "node:fs";
import { join, resolve } from "node:path";
import { gzipSync } from "node:zlib";

const BUNDLE_LIMIT_BYTES = 500 * 1024;

describe("Bundle size gate (UI-SPEC §Stack & Bundle Targets)", () => {
  it("dist/ gzipped JS+CSS total ≤ 500KB", () => {
    // dashboard/web/ is the package root → resolve dist relative to cwd.
    const distDir = resolve(process.cwd(), "dist");
    if (!existsSync(distDir)) {
      // Pre-build smoke — skip without failing. The `make
      // dashboard-frontend` target builds before running tests.
      return;
    }

    const total = sumGzippedAssetsRecursive(distDir);
    expect(total).toBeLessThanOrEqual(BUNDLE_LIMIT_BYTES);
  });
});

function sumGzippedAssetsRecursive(dir: string): number {
  let total = 0;
  for (const entry of readdirSync(dir)) {
    const abs = join(dir, entry);
    const st = statSync(abs);
    if (st.isDirectory()) {
      total += sumGzippedAssetsRecursive(abs);
      continue;
    }
    if (/\.(js|css)$/.test(entry)) {
      total += gzipSync(readFileSync(abs)).byteLength;
    }
  }
  return total;
}
