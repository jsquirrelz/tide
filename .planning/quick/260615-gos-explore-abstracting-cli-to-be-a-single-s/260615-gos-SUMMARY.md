---
phase: quick-260615-gos
plan: "01"
subsystem: planning-artifacts
tags:
  - backlog
  - milestone-framing
  - polyglot-subagent
  - langraph
dependency_graph:
  requires: []
  provides:
    - v1.x-polyglot-subagent-MILESTONE.md
    - ROADMAP.md backlog entry
  affects:
    - .planning/ROADMAP.md
    - .planning/milestones/
tech_stack:
  added: []
  patterns:
    - semi-scoped milestone framing doc
    - alternatives-considered argumentative shape
key_files:
  created:
    - .planning/milestones/v1.x-polyglot-subagent-MILESTONE.md
  modified:
    - .planning/ROADMAP.md
decisions:
  - Architecture locked: seam stays at image boundary (pkg/dispatch.Subagent + envelope contract)
  - Second runtime is Python/LangGraph per-task image, not a long-lived HTTP service
  - Full agent-loop parity targeted; hooks/skills intentionally N/A
  - Task breakdown explicitly deferred to a future plan-phase cycle
metrics:
  duration: ~5m
  completed: "2026-06-15"
---

# Phase quick-260615-gos Plan 01: Polyglot Subagent Runtimes — Backlog Milestone Framing

**One-liner:** Locked three-pillar architecture (image-boundary seam + Python/LangGraph per-task image + full parity) into a semi-scoped backlog milestone doc with parity inventory, contract-conformance table, provider-firewall gap analysis, five open questions, and three alternatives rejected.

## Tasks Completed

| Task | Name | Files |
|------|------|-------|
| 1 | Create polyglot subagent milestone framing doc | `.planning/milestones/v1.x-polyglot-subagent-MILESTONE.md` (created) |
| 2 | Add backlog milestone entry to ROADMAP.md | `.planning/ROADMAP.md` (2 inserts: Milestones list + details blocks) |

## Deviations from Plan

None — plan executed exactly as written.

## Known Stubs

None — this is a documentation-only task; no data sources or UI components.

## Threat Flags

None — writes land in `.planning/` only; no code, secrets, or network surface introduced.

## Self-Check: PASSED

- `.planning/milestones/v1.x-polyglot-subagent-MILESTONE.md` exists.
- `grep "Polyglot Subagent Runtimes" .planning/ROADMAP.md` returns 2 matches (Milestones list + details block).
- `grep "Alternatives Considered" ...MILESTONE.md` returns 1 match.
- Deferred section present; no phase/task/requirements breakdown crept into the milestone doc.
- Progress table in ROADMAP.md unchanged (no new rows added).
