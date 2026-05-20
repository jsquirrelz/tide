/*
 * ansi.test.ts (plan 04-16 Task 1)
 *
 *   Covers Tests 4-6 of the plan: bounded ANSI SGR parser per UI-SPEC §8.
 *   Scope: SGR 30-37 / 90-97 / 40-47 / 100-107 / 1 / 22 / 39 / 49 / 0.
 *   Everything else is stripped (the entire `\x1b[Nm` sequence) but the
 *   surrounding text passes through unchanged.
 *
 *   Test 4 — supported codes: foreground colors + bold + reset.
 *   Test 5 — unsupported codes (256-color) stripped; text preserved.
 *   Test 6 — source-level line bound (≤ 200 lines).
 */
import { describe, expect, it } from "vitest";
import { readFileSync } from "node:fs";
import { resolve } from "node:path";

import { parseAnsi } from "./ansi";

describe("parseAnsi (Test 4)", () => {
  it("parses fg color + bold + reset SGR codes", () => {
    const out = parseAnsi(
      "\x1b[31mERROR\x1b[0m normal\x1b[1;36mBOLD CYAN\x1b[0m",
    );
    // Three segments: red ERROR, plain " normal", bold cyan "BOLD CYAN".
    expect(out).toEqual([
      { text: "ERROR", color: "red" },
      { text: " normal" },
      { text: "BOLD CYAN", color: "cyan", bold: true },
    ]);
  });

  it("returns a single plain segment for plain text", () => {
    expect(parseAnsi("hello world")).toEqual([{ text: "hello world" }]);
  });

  it("handles default-fg (39) and default-bg (49) by clearing attributes", () => {
    const out = parseAnsi("\x1b[31mred\x1b[39mreset");
    expect(out).toEqual([
      { text: "red", color: "red" },
      { text: "reset" },
    ]);
  });
});

describe("parseAnsi (Test 5) — unsupported SGR codes are stripped", () => {
  it("strips 256-color (38;5;208) entirely; text passes through", () => {
    const out = parseAnsi("\x1b[38;5;208morange\x1b[0m");
    expect(out).toEqual([{ text: "orange" }]);
  });

  it("strips truecolor (38;2;r;g;b) entirely; text passes through", () => {
    const out = parseAnsi("\x1b[38;2;200;100;50mfancy\x1b[0m");
    expect(out).toEqual([{ text: "fancy" }]);
  });

  it("never crashes on malformed escape sequences", () => {
    expect(() => parseAnsi("\x1b[hello")).not.toThrow();
    expect(() => parseAnsi("\x1b[")).not.toThrow();
    expect(() => parseAnsi("\x1b")).not.toThrow();
  });
});

describe("parseAnsi (Test 6) — source-level line bound", () => {
  it("ansi.ts source file stays ≤ 200 lines", () => {
    const path = resolve(process.cwd(), "src/lib/ansi.ts");
    const lines = readFileSync(path, "utf-8").split("\n").length;
    // UI-SPEC §8 says "80-line scope" — we allow up to 200 for test/import
    // overhead per the plan's must-haves bullet.
    expect(lines).toBeLessThanOrEqual(200);
  });
});
