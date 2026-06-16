---
status: partial
phase: 21-cost-cache-observability
source: [21-VERIFICATION.md]
started: 2026-06-16T05:45:00Z
updated: 2026-06-16T05:45:00Z
---

## Current Test

[awaiting human testing]

## Tests

### 1. Cache Efficiency panel renders live in a running cluster
expected: Deploy the dashboard against a cluster with Prometheus scraping TIDE, open TelemetryView, and confirm the fifth panel labeled "Cache Efficiency" shows three live figures (hit ratio %, cache-creation tokens, realized savings $) plus a hit-rate sparkline — values sourced from real `tide_tokens_cache_*` and `tide_cache_savings_cents_total` series.
result: [pending]

### 2. Per-level selector slices all panels against live data
expected: In TelemetryView, click the per-level selector (Project/Phase/Plan/Wave) and confirm every panel re-queries with `sum by(<dim>)(...)` aggregation and renders per-level series; selecting Project restores ungrouped totals.
result: [pending]

## Summary

total: 2
passed: 0
issues: 0
pending: 2
skipped: 0
blocked: 0

## Gaps
