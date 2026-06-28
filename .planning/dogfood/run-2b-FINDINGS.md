# Dogfood Run #2b — Findings (2026-06-28)

**Run:** TIDE-on-TIDE on a real Anthropic key (`kind-tide-dogfood`), driving the salvaged
`dogfood-codex-runtime` project (build TIDE's OpenAI/Codex subagent). Claude is the engine;
the Codex subagent is the intended deliverable.
**Outcome:** HALTED on single-node OOM after the planning cascade. Spend stopped by halting the
kind node (`Exited 137`). The run **validated import-resume end-to-end** and surfaced a set of
fixture/runbook gaps (fixed) plus **4 product defects** (need GSD phases).
**Predecessor:** [`run-2-SCOPE.md`](run-2-SCOPE.md) · [`run-2-FINDINGS.md`](run-2-FINDINGS.md) (run #2a, the partial-tree defect → Phase 30/v1.0.5).

---

## What the run PROVED (positive)

The v1.0.5 import-resume path works on a real cluster:

- A fresh Project with `spec.importSource` + a seed ConfigMap **adopted** the salvaged
  3 Milestones + 15 Phases — **zero project/milestone planner Jobs** (the ~$90 of upper-level
  planning from run #1 was NOT re-paid).
- `ImportComplete=True` fired (the real `tide-import:1.0.5` Job ran, skipped the absent envelopes).
- The phase-planners then **regenerated 44 plans via real Anthropic API calls** against the
  in-cluster `main` mirror — the planning cascade is real and produced output.

The "Topologically-Indexed" resumability story (adopt the authored tree, re-plan from there
without re-paying) is functionally validated through the planning cascade.

---

## Setup / fixture / runbook gaps (FIXED in this commit)

These were bring-up friction, not product bugs. All fixed in the artifacts/`RUNBOOK.md`:

| # | Gap | Fix |
|---|-----|-----|
| S1 | cert-manager not installed (chart needs `cert-manager.io/v1`) | install v1.20.2 first |
| S2 | chart PVC defaults RWX; kind local-path is RWO-only → manager Pending | `--set workspaces.pvc.accessModes={ReadWriteOnce}` |
| S3 | stale RWX PVC survives uninstall; accessModes immutable | delete PVC before reinstall |
| S4 | `tide-git-http-server` image unpublished (403) | `make` build + `kind load` |
| S5 | **`tide-git-http-server` nginx 413 on real-repo push** (no `client_max_body_size`) | **root-fixed in `images/tide-git-http-server/nginx.conf`** (unlimited + stream body) |
| S6 | seed manifest statuses `Running` on envelope-less nodes → reporter thrash (the Phase-30 zombie-stall, re-surfaced via hand-authored seed) | statuses → `""` in `seed-manifest.trimmed.json` |
| S7 | `TIDE_IMPORT_IMAGE=""` doesn't dev-skip — `envOrDefault` falls back to a missing default image → import Job hangs | point at real `tide-import:1.0.5` |
| S8 | milestone gate defaults to `approve` → `AwaitingApproval` hold | `gates.milestone/phase: auto` in `project.yaml` |
| S9 | per-namespace `tide-import` SA missing → import Job pod 403 on create | added to `per-namespace-resources.yaml` |
| S10 | **API key trailing newline** → credproxy `X-Api-Key` header invalid → every `claude` exits 1 (0 tokens) | `--from-literal=…"$(tr -d '\n\r' < key)"` |

---

## Product defects (NOT fixed — proposed GSD phases)

These are real code-level defects the run exposed. Listed in priority order.

### D1 — Cost tracking never wires under the import-adoption path *(headline; correctness/safety)*
`Project.status.costSpentCents` and the entire `usage` block stayed **empty** while 44 plans were
authored by real API calls. Status keys observed: `{boundaryPush, budget, conditions, git, phase}`
— no `usage`, no `costSpentCents`. **The metered `budget.absoluteCapCents` cannot enforce if spend
is never tallied** — the run spent blind, and only the node OOM stopped it. This undermines the
budget-safety guarantee the entire v1.0.x line rests on, specifically on the resumption/import path
v1.0.5 just shipped. Likely root cause: the planner→reporter→Project usage rollup is tied to the
normal project lifecycle, which the adoption path bypasses (see D2).
→ **Proposed: Phase "Cost rollup under adoption"** — wire reporter usage rollup for adopted/imported
trees; envtest that an imported Project accrues `costSpentCents` and that the budget gate halts.

### D2 — Project lifecycle stalls at `Initialized` under adoption
After `ImportComplete=True`, the Project stayed `phase: Initialized` even as the phase→plan cascade
ran (44 plans). The adoption path suppresses the project-planner (correct) but then never advances
the Project to `Running`/planning — so anything keyed off the project phase (incl. cost rollup, D1)
doesn't fire.
→ **Proposed: same phase as D1** (they're the same lifecycle seam).

### D3 — No planner-concurrency bound → single-node OOM
The cascade dispatched ~60 subagent pods at once (15 phase + 44 plan planners), each a node/claude
CLI + credproxy sidecar. The single kind node OOM'd (`Exited 137`) and the API server went
unreachable. The spec's "size planner and executor pools separately" is not enforced at dispatch —
there is no max-in-flight wave concurrency cap.
→ **Proposed: Phase "Dispatch concurrency caps"** — per-level max-in-flight (planner/executor pools),
configurable; default sane for a single node.

### D4 — Phase false-`Succeeded` on a failed planner
A phase was marked `Succeeded` ("Plan children materialized") when its planner exited 1 with
`childCount 0` and produced no plans. A failed planner with zero children must not succeed the
parent. (Phase 30 added a childless-success guard for plans; phases appear to lack the equivalent.)
→ **Proposed: Phase "Planner failure semantics"** — fail (or retry) the parent on `exitCode!=0` /
`childCount 0`; envtest the guard at phase + milestone levels.

---

## Cost

**Not precisely measurable — because the meter was broken (D1).** Order-of-magnitude: low
(~$5–20; roughly 60 sonnet planner runs before OOM). The authoritative figure is on the Anthropic
console for the run window (2026-06-28 ~11:30–11:50 UTC).

## Cluster state

`kind-tide-dogfood` node is **stopped** (`docker` `Exited 137`), not deleted — left for inspection.
The namespace held 3 Milestones / 15 Phases / 44 Plans / 0 Tasks at halt. To reclaim resources:
`kind delete cluster --name tide-dogfood`.

## Recommended sequencing

A real completing run needs **D1+D2 and D3 fixed first**, plus a **bigger or multi-node cluster**
(single-node kind cannot hold the parallelism). D4 is a correctness guard that should land alongside.
Suggested: one corrective milestone (D1+D2 together, then D3, then D4), then relaunch run #2 on
adequate infrastructure.
