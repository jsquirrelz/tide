---
status: passed
phase: 22-dashboard-embed-freshness-fix
source: [22-VERIFICATION.md]
started: 2026-06-16T10:55:00Z
updated: 2026-06-16T11:05:00Z
---

## Current Test

[complete — all tests passed]

## Tests

### 1. Telemetry tab renders from a freshly built image (ROADMAP success criterion #3)
expected: The Telemetry tab renders (cache-efficiency panel visible), proving the embedded bundle is the current post-telemetry SPA — not the frozen pre-telemetry bundle from commit `6d7a28f`. Reproduce via `docker build -f Dockerfile.dashboard -t tide-dashboard:verify .`, run the image against a live cluster, open the dashboard, click the Telemetry tab.
result: passed — Built `tide-dashboard:verify` from a clean checkout (spa-builder regenerated `cmd/dashboard/embed/dist`), `kind load`ed it into `tide-dogfood`, rolled out `deployment/tide-dashboard` in `tide-system` to the fresh image (imagePullPolicy: Never), port-forwarded the service, and drove the browser. The served bundle is `index-BEfeN1Kf.js` (same hash as the fresh build) and contains the `panel-cache-efficiency` marker. The header shows a **Telemetry** tab (absent from the pre-telemetry `6d7a28f` bundle); clicking it renders all panels — BUDGET ($29.72), COST OVER TIME, DISPATCH COUNTS, FAILURE RATE, TOKEN BREAKDOWN, and CACHE EFFICIENCY (hit-rate over time). Visual proof: `22-telemetry-render-proof.png`.

## Summary

total: 1
passed: 1
issues: 0
pending: 0
skipped: 0
blocked: 0

## Gaps
