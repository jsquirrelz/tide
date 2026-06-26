# Dogfood Run #2 — Findings (import partial-tree resume defect)

**Date:** 2026-06-26
**Outcome:** Run did NOT execute. $0 spent (no LLM dispatch). Surfaced a real TIDE defect in the
import feature. Decision: fix import properly as a GSD phase before re-attempting the run.
**Cluster:** `kind-tide-dogfood` left UP as the fix's test bed (v1.0.4 chart, in-cluster TIDE
mirror seeded from `main`, `tide-dogfood-codex` ns wiring + `tide-import` SA, secrets).

## The defect — import cannot resume a partially-completed tree

The import feature (Phases 28/29, IMPORT-01) was built to "resume without re-paying planning" and
dogfood run #2 was gated on it (`ROADMAP.md:277`). But it only cleanly handles a **fully-completed**
tree. Resuming a **partial** tree — the primary reason you'd resume at all — produces a stuck state.

**Mechanism (root cause):**
- The seed-manifest lists every node; `ImportController.reconcileCreatingCRs` materializes each as a
  CR **with its salvaged status** (e.g. `Running`).
- The `tide-import` Job's completeness guard (`cmd/tide-import/main.go`, IMPORT-02) **skips**
  envelopes with `exitCode != 0 || len(childCRDs) != childCount` — correct, since a half-authored
  envelope has nothing usable.
- **The two halves don't reconcile:** an incomplete node is materialized as `Running` with **no
  envelope copied behind it**. Its controller then looks for the (deliberately skipped) envelope,
  can't find it, can't materialize children → permanent stall + a reporter-Job thrash loop.

**Evidence (this run):** `salvage-20260618` is run #1 halted mid-authoring. Import copy report:
`{"copied":257,"converted":60,"incomplete":40}` — **40 of ~100 levels had `exitCode=1, childCount=0`
envelopes** (plan-planners killed mid-authoring). Those 40 Plan CRs materialized `Running` with no
envelope → `Task=0`, ~168 failed `tide-reporter` pods, Project stuck `Running`, never progresses.

**Why it shipped green:** the Tier-b kind test (`import_resume_test.go`) uses this *exact* salvage
bundle but only asserts the adoption **gate** — zero `{project,milestone,phase}` planner Jobs, `$0`
re-paid, `ImportComplete=True`. It **never drives the partial tree to execution**, so the
zombie-incomplete-node stall was never exercised. Tested the mechanism, not the end-to-end outcome.

## Fix shape (for the phase)

Partial-tree resume = **adopt-complete + re-plan-incomplete**, driven by per-node envelope
completeness (the signal already computed):
- **Complete envelope** → adopt: materialize with salvaged status, copy envelope (current behavior).
- **Incomplete / missing envelope** → materialize the node in a **re-plannable state** (fresh/Pending)
  so its parent (or itself) re-authors it against current `main` — OR omit it so the parent re-plans.
- This is exactly the "hybrid" that had to be hand-rolled against the feature; it should be **native**.

**Design forks to resolve in discuss/plan:**
1. Status semantics: what exact status makes a node re-plannable without tripping the import platform
   gate or the Job-presence idempotency guard? (Project-level guard is Job-based — see below.)
2. Dependency consistency: when an incomplete node is re-planned but a **completed** node `dependsOn`
   it (adopted), do the regenerated children stay compatible? Global indegree implications.
3. Project-level adoption: `project_controller.go:1037-1052` guard is **Job-presence-based**, not
   child-based — the project planner can still dispatch post-`ImportComplete`. Confirm/tighten so the
   project level adopts cleanly too (this run showed a `tide-project` planner Job dispatching).
4. New test tier: drive a **partial** import (mixed complete/incomplete envelopes) all the way to
   `Project=Complete` — the coverage Tier-b is missing.

## Carried-in infra (reusable for the fix's validation)
- `kind-tide-dogfood` cluster, v1.0.4 chart (manager `TIDE_IMPORT_IMAGE` currently `:1.0.4`).
- In-cluster mirror `http://git-http-server.tide-dogfood-codex.svc.cluster.local/tide.git` (seeded
  from `main`, `client_max_body_size 0` via mounted ConfigMap, `http.receivepack=true`).
- `tide-dogfood-codex` ns: `tide-secrets` (real key), per-ns SA/PVC/signing-key + **`tide-import` SA**
  (the medium per-ns template lacked it — add to the template).
- Run-2 artifacts: `examples/projects/dogfood/run-2/{project.yaml,RUNBOOK.md,seed-manifest.trimmed.json,
  per-namespace-resources.yaml,git-http-server.yaml}` + `.planning/dogfood/run-2-SCOPE.md`.

## After the fix
Re-attempt dogfood run #2 (import the partial salvage tree → adopt-complete + re-plan-incomplete →
execute to completion → review diffs). Still bounded by the $50 metered cap; in-cluster mirror.
