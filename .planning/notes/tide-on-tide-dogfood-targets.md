---
title: TIDE-on-TIDE dogfood targets — analytics & Codex runtime
date: 2026-06-10
context: /gsd:explore session after v1.0.0 shipped; picking the first two projects TIDE drives against its own repo
---

# TIDE-on-TIDE dogfood targets — analytics & Codex runtime

Two internal projects chosen as the first real TIDE-on-TIDE runs (Project CRDs applied
against the TIDE repo itself, TIDE authoring the milestone/phase/plan artifacts).

## Project 1: Analytics

**Scope:** run telemetry & cost (token spend, wall-clock, dispatch counts, failure rates
per project/phase/wave) plus new React dashboard surfaces showcasing those metrics.

**Decisions:**
- **Prometheus is the DB.** The orchestrator exposes everything as Prometheus metrics
  (client_golang, already in stack); the dashboard queries Prometheus for history.
  The CRD-`.status`-only persistence constraint stays untouched — history lives outside
  TIDE and is optional (chart keeps `prometheus.enabled=false` default).
- **Label-cardinality budget:** project/phase/wave labels are fine; per-task labels are
  not (cardinality explosion). Per-task detail stays in CRD `.status` / OTel spans.

## Project 2: Codex subagent runtime

**Scope:** real heterogeneous use — actually dispatching work to Codex alongside Claude,
not just proving the `Subagent` interface.

**Decisions:**
- **Per-level runtime selection.** Planner pool and executor pool each pin a runtime
  (e.g. Claude plans, Codex executes). Matches the spec's separate-pools argument;
  smallest API change with real heterogeneity. Per-task selection rejected for now
  (large API/scheduler surface); per-project rejected (no mixing within a run).
- Needs a second credential path (`OPENAI_API_KEY`/`CODEX_API_KEY` Secret), chart
  values, and mixed-provider waves with identical failure semantics.

**Codex CLI research findings (official docs, 2026-06):**
- `codex exec` is first-class headless: one-shot, no TTY, final message to stdout;
  `--ephemeral`, `--skip-git-repo-check`, `--ignore-user-config` for clean containers.
- `--json` (JSONL event stream), `--output-schema <file>` (schema-constrained final
  response), `-o/--output-last-message <file>`.
- API-key env auth is the documented container path — no headless-OAuth problem.
- MCP client support (STDIO + streamable HTTP) via config.toml or `codex mcp add`.
- Sandbox defaults read-only; runner needs `--sandbox workspace-write` (fine inside an
  already-isolated pod — mirrors the Claude runner posture).
- Sources: developers.openai.com/codex/{noninteractive,cli/reference,auth,cli/features}

## Ordering

**Analytics first.** The Codex dogfood run then becomes observable through the surfaces
the first run built — token spend and dispatch behavior of the heterogeneous run watched
live on the new dashboard.
