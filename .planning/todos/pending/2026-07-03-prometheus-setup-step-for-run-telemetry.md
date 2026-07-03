---
created: 2026-07-03T18:55:00.000Z
title: Add a Prometheus setup step so run telemetry beyond budget is present
area: docs
files:
  - charts/tide/values.yaml
  - docs/INSTALL.md
---

## Problem

On the first external-repo run (2026-07-03), the only observable run telemetry was the budget tally on
`Project.status`. Everything else (token spend over time, dispatch counts,
per-level durations, failure rates) was absent because the chart defaults to
`prometheus.enabled=false` (deliberate — avoids ServiceMonitor
CRD-not-found on plain clusters) and nothing in the install/run path tells
the operator that telemetry is dark or how to light it up.

The dogfood-analytics milestone built the metrics + dashboard surfaces; a
fresh operator install never sees them without hand-discovering the values
toggle.

## Solution

TBD — candidates, smallest first:

- docs: an explicit "enable telemetry" step in INSTALL.md + the
  project-authoring guide (install kube-prometheus-stack or point at an
  existing Prometheus, set `prometheus.enabled=true`, verify the
  ServiceMonitor scrapes).
- chart: a NOTES.txt warning when `prometheus.enabled=false` ("run
  telemetry beyond budget will be unavailable").
- dashboard: surface a "telemetry disabled" banner instead of empty panels
  (the graceful-degradation contract from the dogfood-analytics outcome —
  verify it actually renders that way on a prometheus-less install).
