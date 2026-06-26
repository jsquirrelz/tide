---
quick_id: 260625-txr
title: "Dogfood run #2 setup artifacts — v1alpha2 skeleton CRs + project.yaml + runbook"
date: 2026-06-26
status: planned
---

# Quick Task 260625-txr

Setup artifacts for dogfood run #2 (per `.planning/dogfood/run-2-SCOPE.md`). Artifacts only —
no live cluster ops.

## Tasks

### Task 1 — v1alpha2 skeleton CRs (3 Milestones + 15 Phases)
- **files:** `examples/projects/dogfood/run-2/skeleton.yaml`
- **action:** Generate from `salvage-20260618/seed-manifest.json` (authoritative). Milestone CR =
  `apiVersion: tideproject.k8s/v1alpha2, kind: Milestone, metadata.name, spec.projectRef:
  dogfood-codex-runtime + spec.dependsOn`. Phase CR = `kind: Phase, metadata.name,
  spec.milestoneRef + spec.dependsOn`. Structure-only: NO sharedContext, NO plans, NO importSource.
  dependsOn edges copied verbatim from seed-manifest. All in namespace `tide-dogfood-codex`.
- **verify:** 3 Milestones + 15 Phases; names unique; every phase.milestoneRef + every dependsOn
  target resolves to a generated node; YAML parses.
- **done:** applyable skeleton that the adoption guards treat as "already authored."

### Task 2 — fresh project.yaml
- **files:** `examples/projects/dogfood/run-2/project.yaml`
- **action:** name `dogfood-codex-runtime`, ns `tide-dogfood-codex`, `schemaRevision: v1alpha2`,
  `outcomePrompt` VERBATIM from salvage `projects.yaml`, `targetRepo` +
  `git.repoURL=http://git-http-server.tide-dogfood-codex.svc.cluster.local/tide.git`
  (`git.credsSecretRef: tide-secrets`), `budget.absoluteCapCents: 5000` +
  `rollingWindowCapCents: 5000` + `rollingWindowDuration: 24h`, `gates {plan: auto, task: auto,
  pauseBetweenWaves: false}`, `failureProfile: strict`, `maxAttemptsPerTask: 3`,
  `subagent {model: claude-sonnet-4-6, levels {phase/plan: claude-sonnet-4-6, task:
  claude-haiku-4-5}}`, `providerSecretRef: tide-secrets`. NO importSource.
- **verify:** YAML parses; outcomePrompt byte-identical to source; fields match scope doc.
- **done:** a runnable real Project (not the stub fixture).

### Task 3 — bring-up runbook
- **files:** `examples/projects/dogfood/run-2/RUNBOOK.md`
- **action:** Ordered, copy-pasteable: fresh kind cluster; deploy published v1.0.4 chart; stand up
  in-cluster git mirror (adapt `examples/projects/medium/git-http-server-deployment.yaml` +
  demo-remote-init, seed from current `main`, `http.receivepack=true`); create `tide-secrets` from
  `~/.tide/anthropic.key`; per-ns SA/PVC/signing-key wiring; apply skeleton then project; monitoring
  + $50 kill criteria; extraction of the `tide/run-*` branch.
- **verify:** every command references a real file/target; no live execution here.
- **done:** an operator (me, next step) can drive the run from it.

## must_haves
- artifacts: examples/projects/dogfood/run-2/{skeleton.yaml,project.yaml,RUNBOOK.md}
- truths:
  - skeleton is structure-only v1alpha2 (no plans/importSource/sharedContext)
  - project.yaml outcomePrompt is byte-identical to the salvaged refreshed prompt
  - no kind/helm/kubectl run against a live cluster in this task
- key_links: examples/projects/dogfood/salvage-20260618/seed-manifest.json, .planning/dogfood/run-2-SCOPE.md

## Out of scope
Live cluster bring-up and driving the run (next step, operated directly).
