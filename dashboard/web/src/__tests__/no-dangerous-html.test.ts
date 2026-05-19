import { describe, it, expect } from "vitest";
import { readdirSync, readFileSync, statSync } from "node:fs";
import { join, relative, resolve } from "node:path";

// Walks every .tsx/.ts file under dashboard/web/src and asserts none uses
// `dangerouslySetInnerHTML`. T-04-D1 XSS mitigation per UI-SPEC §Threat Model:
// React escapes text nodes by default, so the *only* path to XSS via a
// backend-supplied project name is `dangerouslySetInnerHTML`. Zero uses =
// closed at build time, not runtime.
//
// Vitest runs from the package root (dashboard/web/) so `process.cwd()` resolves
// to that directory — easier and more portable than `import.meta.url` under
// jsdom's URL polyfill (which rejects file:// URLs).

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

describe("T-04-D1 XSS mitigation — `dangerouslySetInnerHTML` is forbidden", () => {
  it("no .tsx/.ts file under src/ contains the dangerouslySetInnerHTML prop", () => {
    const files = walk(SRC_DIR);
    expect(files.length).toBeGreaterThan(0);

    const offenders: string[] = [];
    for (const f of files) {
      // Skip this guard file itself — the literal string above is the only
      // legitimate match in the entire src tree.
      if (f.endsWith("no-dangerous-html.test.ts")) continue;
      const content = readFileSync(f, "utf-8");
      if (content.includes("dangerouslySetInnerHTML")) {
        offenders.push(relative(SRC_DIR, f));
      }
    }
    expect(offenders).toEqual([]);
  });
});
