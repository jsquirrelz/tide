/*
 * ansi.ts (plan 04-16)
 *
 *   Bespoke SGR escape parser per UI-SPEC §8 PodLogStreamer scope. The
 *   scope is deliberately tight: the dashboard renders pod logs whose
 *   color comes from common ops tooling (Go logr/zap colored output,
 *   kubectl colorize, ANSI-aware shells). Truecolor / 256-color / other
 *   SGR families are stripped (the escape sequence is removed; the
 *   surrounding text passes through unchanged).
 *
 *   Supported SGR codes (UI-SPEC §8):
 *     30-37   — foreground colors (black/red/green/yellow/blue/magenta/cyan/white)
 *     90-97   — bright foreground
 *     40-47   — background colors
 *     100-107 — bright background
 *     1       — bold
 *     22      — no-bold
 *     39      — default foreground
 *     49      — default background
 *     0       — reset all
 *
 *   Output shape: AnsiSegment[] — each segment is plain text + optional
 *   color / bgColor / bold flags. Consumers render via React text nodes
 *   + style props (T-04-D1 XSS mitigation — never innerHTML).
 *
 *   No regex backtracking risk: the split regex is anchored and bounded.
 */

export type AnsiColor =
  | "black"
  | "red"
  | "green"
  | "yellow"
  | "blue"
  | "magenta"
  | "cyan"
  | "white";

export type AnsiSegment = {
  text: string;
  color?: AnsiColor;
  bgColor?: AnsiColor;
  bold?: boolean;
};

const FG_BASE = 30;
const BG_BASE = 40;
const FG_BRIGHT_BASE = 90;
const BG_BRIGHT_BASE = 100;

const COLOR_NAMES: AnsiColor[] = [
  "black",
  "red",
  "green",
  "yellow",
  "blue",
  "magenta",
  "cyan",
  "white",
];

// Matches a complete SGR sequence: ESC [ params m. Capture group is the
// semicolon-separated parameter list. Sequences without the trailing 'm'
// (or with non-numeric/semicolon params) fall through to the literal-text
// branch — we never throw on malformed input.
const SGR_RE = /\x1b\[([\d;]*)m/g;

type Attrs = {
  color?: AnsiColor;
  bgColor?: AnsiColor;
  bold?: boolean;
};

function colorForCode(code: number): AnsiColor | undefined {
  if (code >= FG_BASE && code <= FG_BASE + 7) return COLOR_NAMES[code - FG_BASE];
  if (code >= FG_BRIGHT_BASE && code <= FG_BRIGHT_BASE + 7) {
    return COLOR_NAMES[code - FG_BRIGHT_BASE];
  }
  return undefined;
}

function bgColorForCode(code: number): AnsiColor | undefined {
  if (code >= BG_BASE && code <= BG_BASE + 7) return COLOR_NAMES[code - BG_BASE];
  if (code >= BG_BRIGHT_BASE && code <= BG_BRIGHT_BASE + 7) {
    return COLOR_NAMES[code - BG_BRIGHT_BASE];
  }
  return undefined;
}

/**
 * Apply a parameter list (e.g. "1;36" → [1, 36]) to the current
 * attribute state. Unrecognized codes are silently ignored so the
 * surrounding text still renders.
 *
 * Codes 38 / 48 (extended fg / bg) introduce a sub-sequence that pulls
 * additional parameters off the list:
 *   38;5;N      — 256-color fg (consumes 2 extra params)
 *   38;2;R;G;B  — truecolor fg (consumes 4 extra params)
 *   48;5;N / 48;2;R;G;B — same for bg.
 * UI-SPEC §8 strips all of these. We must still SKIP those parameter
 * positions so a literal "100" later in the list isn't misread as a
 * bg-bright-black SGR.
 */
function applyParams(params: number[], attrs: Attrs): Attrs {
  let next = attrs;
  for (let i = 0; i < params.length; i++) {
    const code = params[i];
    if (code === 0) {
      next = {};
      continue;
    }
    if (code === 1) {
      next = { ...next, bold: true };
      continue;
    }
    if (code === 22) {
      next = { ...next, bold: false };
      continue;
    }
    if (code === 39) {
      next = { ...next, color: undefined };
      continue;
    }
    if (code === 49) {
      next = { ...next, bgColor: undefined };
      continue;
    }
    // 256-color / truecolor — consume the extended params and ignore.
    if (code === 38 || code === 48) {
      const sub = params[i + 1];
      if (sub === 5) {
        i += 2; // consume `;5;N`
      } else if (sub === 2) {
        i += 4; // consume `;2;R;G;B`
      } else {
        i += 1; // unknown extension — skip the next token defensively
      }
      continue;
    }
    const fg = colorForCode(code);
    if (fg) {
      next = { ...next, color: fg };
      continue;
    }
    const bg = bgColorForCode(code);
    if (bg) {
      next = { ...next, bgColor: bg };
      continue;
    }
    // Unsupported single code — strip; text passes through.
  }
  return next;
}

function attrsToSegment(text: string, attrs: Attrs): AnsiSegment {
  const seg: AnsiSegment = { text };
  if (attrs.color) seg.color = attrs.color;
  if (attrs.bgColor) seg.bgColor = attrs.bgColor;
  if (attrs.bold) seg.bold = true;
  return seg;
}

/**
 * parseAnsi(line) walks an input string once, splitting on SGR escape
 * sequences. Returns a flat list of segments. Empty input returns [].
 * Malformed escapes never throw — the parser advances past them.
 */
export function parseAnsi(line: string): AnsiSegment[] {
  if (line.length === 0) return [];
  const out: AnsiSegment[] = [];
  let attrs: Attrs = {};
  let cursor = 0;

  // Reset the global regex's lastIndex so repeated calls don't accumulate.
  SGR_RE.lastIndex = 0;
  let match: RegExpExecArray | null;
  while ((match = SGR_RE.exec(line)) !== null) {
    if (match.index > cursor) {
      out.push(attrsToSegment(line.slice(cursor, match.index), attrs));
    }
    const params = match[1]
      .split(";")
      .filter((p) => p.length > 0)
      .map((p) => parseInt(p, 10))
      .filter((n) => Number.isFinite(n));
    // Empty params list = treat as reset (ANSI convention for ESC[m).
    attrs = applyParams(params.length === 0 ? [0] : params, attrs);
    cursor = match.index + match[0].length;
  }
  if (cursor < line.length) {
    out.push(attrsToSegment(line.slice(cursor), attrs));
  }
  return out;
}
