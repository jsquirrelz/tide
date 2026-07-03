---
created: 2026-07-03T20:30:00.000Z
title: Dashboard — clicking a Planning DAG node shows the artifacts it produced
area: ui
files:
  - dashboard/
---

## Problem

The Planning DAG shows level nodes (Project/Milestone/Phase/Plan) but there is
no way to read the artifacts a level produced (MILESTONES.md, MILESTONE.md,
PHASE.md, PLAN/children JSON). On the first external-repo run (2026-07-03) the only way to review artifacts at the
approve gates was spinning ad-hoc alpine/git reader pods against the project
PVC — three times in one run. Gate approvals (`approve-milestone`,
`approve-phase`) are exactly the moment an operator needs to read these, and
the dashboard offers nothing.

Where artifacts actually live (observed, 1.0.6): only on the per-project PVC
at `/workspace/<project-uid>/workspace/envelopes/<cr-uid>/` (`*.md`,
`children/*.json`, `out.json`). Child CR specs are thin pointers; the run
branch's boundary push carried only task code commits — planning artifacts
never reach git, CRs, or any API-readable surface.

## Solution

TBD — the hard part is transport, since the manager can't mount every
project's (often RWO) PVC across namespaces. Options to weigh at planning:

- Reporter-style in-namespace reader: a small endpoint on the manager that
  dispatches (or reuses) an in-namespace Job/pod to read the envelope dir on
  demand — mirrors the existing tide-reporter pattern.
- Persist artifacts at materialization time: the reporter Job already reads
  out.json in-namespace; it could also write each artifact into a ConfigMap
  (size-capped, labeled by CR UID) that the manager reads via the K8s API.
  Watch etcd limits (PLAN.md can grow; cap + truncate indicator). Not a
  PERSIST-02 violation (artifacts are source docs, not derived schedules) but
  keep per-object size well under the 1.5 MiB etcd limit.
- Commit planning artifacts to the run branch at each boundary push and have
  the dashboard link to the git host (cheapest UI, but artifacts land late —
  useless for the approve-gate review moment).

UI: same drawer surface as the log stream (see
2026-07-03-dashboard-log-stream-drawer-empty.md — fix that drawer's
empty-state handling first / together). Markdown-render the `*.md`
artifacts; pretty-print children JSON. Gate-parked nodes should surface the
artifact front-and-center next to the approve action.
