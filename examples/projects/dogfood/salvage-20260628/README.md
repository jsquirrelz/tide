# salvage-20260628 — dogfood run 2b phase-planner envelopes

**Salvaged:** 2026-06-28 from the `kind-tide-dogfood` project PVC (`tide-dogfood-codex/tide-projects`,
project UID `1f6d1f92`) after run 2b halted on single-node OOM. Pulled via `docker cp` from the
stopped node (zero re-spend). Full run writeup: [`../../../../.planning/dogfood/run-2b-FINDINGS.md`](../../../../.planning/dogfood/run-2b-FINDINGS.md).

## What this is

`pvc-envelopes.tgz` — the `envelopes/<nodeUID>/out.json` subtree of the salvaged run, i.e. the
**phase-planner outputs** authored against current `main` for the `dogfood-codex-runtime` project
(build TIDE's OpenAI/Codex subagent).

- **17 phase-planner envelopes**, **13 complete** (`exitCode 0`, real children) authoring **55 Plan
  specs** (e.g. `plan-01-codex-values-schema`, `plan-03-codex-rbac-secret`, …); 4 failed/empty.
- Schema: `tideproject.k8s/v1alpha1` `TaskEnvelopeOut` (same as `salvage-20260618`; the import path
  converts v1alpha1→v1alpha2).
- **Plan-level only** — the plan-planners (authoring Tasks) were mid-flight at OOM, so there are no
  task-level envelopes (0 Tasks were created). Next import adopts down to the **plan** level.

## Why it's worth keeping

Run 2b's expensive output was the regenerated plan tree. Preserving it means a future run #2
(after the v1.0.6 D1–D4 fixes land) can import **M+P skeleton + these 13 phases' Plans** — more
complete than the M+P-only `run-2/seed-manifest.trimmed.json` — and it exercises v1.0.5's real
partial-tree resume on genuine mixed complete/incomplete data (the 13 complete adopt; the rest
re-plan), which is a stronger test than the synthetic fixture.

## Not yet usable as-is

There is **no `seed-manifest.json`** here — generating one needs the live CR tree (FQName ↔ UID ↔
dependsOn ↔ sha256), which the OOM'd cluster could not export. To turn this into an importable
bundle, run `tide export-envelopes` against a healthy re-materialized run, or regenerate the seed
manifest from these envelopes + the `run-2/skeleton` structure. The raw authored plans (the costly
part) are preserved here regardless.
