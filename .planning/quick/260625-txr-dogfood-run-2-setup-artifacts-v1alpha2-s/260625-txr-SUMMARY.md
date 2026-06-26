---
quick_id: 260625-txr
title: "Dogfood run #2 setup artifacts — v1alpha2 skeleton CRs + project.yaml + runbook"
date: 2026-06-26
status: complete
---

# Quick Task 260625-txr — Summary

Setup artifacts for dogfood run #2 (per `.planning/dogfood/run-2-SCOPE.md`). Artifacts only — no
live cluster ops were run.

## Produced (`examples/projects/dogfood/run-2/`)

- **`skeleton.yaml`** — 3 Milestone + 15 Phase v1alpha2 CRs, generated from
  `salvage-20260618/seed-manifest.json`. Structure-only (`projectRef`/`milestoneRef` + `dependsOn`);
  no `sharedContext`, no plans, no `importSource`. dependsOn edges copied verbatim; all refs validated
  to resolve; names unique; namespace `tide-dogfood-codex`.
- **`project.yaml`** — fresh v1alpha2 Project `dogfood-codex-runtime`. `outcomePrompt` is
  **byte-identical** to the salvaged refreshed prompt (8000 chars; round-trip asserted). targetRepo +
  git.repoURL = `http://git-http-server.tide-dogfood-codex.svc.cluster.local/tide.git`; budget
  `absoluteCapCents 5000` + rolling 5000/24h; gates plan/task auto, pauseBetweenWaves false;
  failureProfile strict; maxAttemptsPerTask 3; subagent phase/plan `claude-sonnet-4-6`, task
  `claude-haiku-4-5`; providerSecretRef `tide-secrets`. No importSource.
- **`RUNBOOK.md`** — ordered bring-up: fresh kind, v1.0.4 OCI chart (CRDs first), `tide-secrets`
  from `~/.tide/anthropic.key`, per-ns wiring (adapt medium `per-namespace-resources.yaml`),
  in-cluster TIDE mirror seeded from current `main` (`http.receivepack=true`), apply skeleton→project,
  monitoring + $50 kill criteria, run-branch extraction. Includes the pre-run check that the
  project-level adoption guard suppresses the project-planner (scope-doc open item).

## Verification (observed)

- Structural validation passes: 3 Milestones + 15 Phases; Project fields ⊆ v1alpha2 schema (derived
  from the Go struct json tags); no importSource/sharedContext/plans; budget/gates/models as specced.
- outcomePrompt byte-identical to source (asserted in-script).
- No kind/helm/kubectl run against a live cluster.

## Next

Drive the run from `RUNBOOK.md` (live, metered to $50), then extract + hand off to the hardening phase.
