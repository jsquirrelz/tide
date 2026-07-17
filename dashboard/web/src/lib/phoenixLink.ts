/**
 * phoenixLink.ts (plan 46-05, D-11) — the ONE place any Phoenix
 * `/redirects/...` URL is assembled. No other file in this SPA (including
 * tests) may inline a `${baseURL}/redirects/...` template string; every
 * consumer imports these helpers instead.
 *
 * Phoenix >= 14.2.0 ships shareable-URL "redirect" routes that resolve a
 * trace or span ID to its UI location without needing a project ID (46-
 * RESEARCH.md's D-12 resolution) — the dashboard never learns which Phoenix
 * project a run's spans land in, so the prior `/projects/{project}/...`
 * URL shape was unusable from here.
 *
 * `phoenixSpanURL` is the href PhoenixTraceLink renders (span-anchored,
 * D-12's enhancement tier). `phoenixTraceURL` is the documented fallback
 * floor — if implementation-time verification finds the span redirect
 * unusable on the target Phoenix, swap PhoenixTraceLink's href to
 * `phoenixTraceURL(baseURL, traceId)`, a one-line change confined to that
 * component.
 */

// Server sends the raw PHOENIX_BASE_URL env value (Plan 46-03 contract) —
// normalization (stripping one trailing slash) happens HERE, and only here.
function normalizeBaseURL(baseURL: string): string {
  return baseURL.endsWith("/") ? baseURL.slice(0, -1) : baseURL;
}

export function phoenixTraceURL(baseURL: string, traceId: string): string {
  return `${normalizeBaseURL(baseURL)}/redirects/traces/${encodeURIComponent(traceId)}`;
}

export function phoenixSpanURL(baseURL: string, spanId: string): string {
  return `${normalizeBaseURL(baseURL)}/redirects/spans/${encodeURIComponent(spanId)}`;
}
