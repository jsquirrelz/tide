---
title: Draft the two Project CRD outcome prompts for the dogfood runs
date: 2026-06-10
priority: high
---

# Draft the two Project CRD outcome prompts for the dogfood runs

Before TIDE can drive either internal project, each needs a `Project` CR: outcome
prompt + target repo (TIDE itself) + creds.

- [ ] Analytics project prompt — Prometheus-backed run telemetry & cost metrics
      (project/phase/wave granularity, cardinality budget) + React dashboard surfaces.
      Encode the "Prometheus is the DB" decision so the planner doesn't reinvent
      persistence.
- [ ] Codex runtime project prompt — second `Subagent` impl in
      `internal/subagent/codex/`, per-level (planner-pool vs executor-pool) runtime
      selection, `OPENAI_API_KEY` Secret path, chart values, identical wave failure
      semantics. Include the Codex CLI flag findings from the exploration note.
- [ ] Decide run order mechanics: analytics first (see note
      `tide-on-tide-dogfood-targets.md`), confirm cluster sizing for a full dogfood run
      (constrained-VM recipe in CLAUDE.md).
