---
status: partial
phase: 22-dashboard-embed-freshness-fix
source: [22-VERIFICATION.md]
started: 2026-06-16T10:55:00Z
updated: 2026-06-16T10:55:00Z
---

## Current Test

[awaiting human testing]

## Tests

### 1. Telemetry tab renders from a freshly built image (ROADMAP success criterion #3)
expected: The Telemetry tab renders (cache-efficiency panel visible), proving the embedded bundle is the current post-telemetry SPA — not the frozen pre-telemetry bundle from commit `6d7a28f`. Reproduce via `docker build -f Dockerfile.dashboard -t tide-dashboard:verify .`, run the image against a live cluster, open the dashboard, click the Telemetry tab.
result: [pending]

## Summary

total: 1
passed: 0
issues: 0
pending: 1
skipped: 0
blocked: 0

## Gaps
