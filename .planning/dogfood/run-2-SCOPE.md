# Dogfood Run #2 — Scope

**Status:** Scoped, awaiting review → execution routes through GSD
**Date:** 2026-06-25
**Type:** Operational dogfood run (TIDE-on-TIDE) with a code deliverable produced *by* the run

---

## Objective

TIDE-on-TIDE: drive the salvaged **`dogfood-codex-runtime`** project to build TIDE's own
**OpenAI/Codex subagent** plus the **per-level provider switch**, so that `openai` becomes a
selectable per-level `vendor` in `project.yaml`. The Claude backend is the engine that drives
the build; the OpenAI/Codex subagent is the deliverable.

This completes dogfood run #2 — the run that run #1 halted on (the foundational global-Execution-DAG
defect, since fixed by v1.0.2 "Spring Tide").

## Definition of Done

- `Project=Complete` for all 3 milestones **or** a metered halt with real progress (see Budget).
- Authored code on the run branch (`internal/subagent/codex/`, per-level vendor switch, chart wiring).
- **Out of scope here:** code-correctness, tests, and live heterogeneous-dispatch validation —
  those are a **separate hardening phase** (see Deferred).

Bounded by completion, not by code-correctness.

---

## Approach — hybrid skeleton-reuse

The salvaged plans are stale (authored against the retired **v1alpha1** schema + pre-Spring-Tide
code: they say "edit `api/v1alpha1/project_types.go`", "schema-valid against the v1alpha1 CRD",
and assume an unmodified `dispatch_helpers.go`). v1alpha2 is now the storage version; executing
them verbatim would author code aimed at the wrong API version. So:

- **Adopt the skeleton** — 3 milestones + 15 phases. Verified to be pure structure
  (`PhaseSpec` = `milestoneRef` + `dependsOn` + `sharedContext`; **no prose brief**), self-contained
  (no milestone/phase `dependsOn` targets a plan), and carrying no `sharedContext`. Adopting it skips
  re-deriving the decomposition (the ~$90 of upper-level planning run #1 already paid).
- **Regenerate all plans + tasks** — phase-planners author fresh plans from the **refreshed
  v1alpha2-aware `outcomePrompt`** (below) + the descriptive phase names → plans target current `main`.
- **Discard the salvaged task envelopes** (8.4 MB `pvc-envelopes.tgz`) — moot once re-planning.
  Therefore **the import feature's copy/rekey machinery is not used**; we hand-apply the
  milestone/phase CRs as v1alpha2 under a fresh Project, and the normal adoption guards
  (`milestone_controller.go`/`phase_controller.go`: skip planner dispatch when a child already exists)
  suppress milestone/phase planning. No `spec.importSource` is set.

Why the refreshed prompt makes this work: run #1's `outcomePrompt` was already updated to be
Spring-Tide-aware — it explicitly says *"ALREADY IN MAIN, build ON it, do NOT rebuild"* the
v1alpha2/global-DAG infrastructure and scopes the deliverable to *"the Codex subagent package +
the per-level provider switch."* It also carries the locked architecture decisions
(`internal/subagent/codex/` mirroring `anthropic/`, per-level (not per-task/per-project) selection,
Secret-ref credential path, Codex CLI facts, and the `pkg/dispatch.Usage` cost-normalization
requirement). Regenerating plans from this prompt yields v1alpha2-correct plans.

## Run configuration (fresh `project.yaml`)

| Field | Value |
|---|---|
| `metadata.name` | `dogfood-codex-runtime` (namespace `tide-dogfood-codex`) |
| `spec.schemaRevision` | `v1alpha2` |
| `spec.outcomePrompt` | the refreshed Spring-Tide-aware prompt, **verbatim** (sourced from the salvage `projects.yaml`) |
| `spec.targetRepo` / `git.repoURL` | **in-cluster TIDE mirror** — an http git-server seeded from current `main` |
| `spec.budget.absoluteCapCents` | **5000 ($50)**; `rollingWindowCapCents` matched; the budget gate halts the run at the cap |
| `spec.gates` | `plan: auto`, `task: auto` (milestone/phase moot — adopted); `pauseBetweenWaves: false` |
| `spec.failureProfile` | `strict` |
| `spec.maxAttemptsPerTask` | `3` |
| `spec.subagent.levels` | phase/plan planners `claude-sonnet-4-6`; task executors `claude-haiku-4-5` (default `sonnet-4-6`). **Cost/quality lever:** sonnet executors = better first-draft code, faster cap burn. |
| `spec.providerSecretRef` / `git.credsSecretRef` | `tide-secrets` ← real Anthropic key at `~/.tide/anthropic.key`. **No OpenAI key needed** — the run *builds* OpenAI support, it does not *use* it. |

During the run, every dispatch is `vendor: anthropic` (the only built provider). OpenAI is the output.

## Infrastructure & setup

1. Fresh single-node **kind** cluster (one cluster at a time — OOM discipline per prior runs).
2. Deploy the **published v1.0.4** chart (now includes `tide-import`; not actually exercised here but proves the install).
3. Stand up the **in-cluster TIDE mirror**: http git-server seeded from current `main` (pattern: the medium sample's `demo-remote`), with `http.receivepack=true`.
4. Create `tide-secrets` from `~/.tide/anthropic.key`; mirror per-namespace SA/PVC/signing-key wiring as the live runs require.
5. Hand-apply the v1alpha2 milestone + phase skeleton CRs, then the fresh Project.

## Execution, monitoring, kill criteria

Drive autonomously. Halt + report on any of:
- **Budget gate** trips at $50 (`absoluteCapCents`).
- **Stall**: no status advance / reconcile loop / repeated requeue with no progress.
- **Executor DeadlineExceeded** pattern (diagnose from pod `state.terminated`, not logs; capture before the 600s Job TTL GC).
- **Completion**: `Project=Complete`.

Metered posture: on a budget halt with real progress, report cost + state and top up rather than abandon.

## Extraction & acceptance

Pull the `tide/run-*` branch out of the in-cluster mirror; report:
- What landed (`internal/subagent/codex/{client,run,doc}.go` + `Dockerfile`; the vendor switch in
  `dispatch_helpers.go` + schema; chart values/manager env wiring).
- Total cost and how far the cascade reached.
- A handoff for the hardening phase.

## Deferred (separate GSD phase)

- Code review / test / harden the authored Codex subagent to mergeable + green.
- Live heterogeneous-dispatch validation (planner=Claude, executor=Codex) — needs a real **OpenAI key**.
- Publishing `tide-codex-subagent` image (follows the v1.0.4 matrix + the new chart-image guardrail).

## Risks & mitigations

| Risk | Mitigation |
|---|---|
| Phase-planners author from bare phase names + prompt → plan quality | The refreshed `outcomePrompt` is detailed and locked; phase names are descriptive. Accept "first draft, harden after." |
| $50 won't finish all 3 milestones | Metered — halt + review + top up. |
| haiku executors → weaker first-draft code | Acceptable under "harden after"; switchable to sonnet (faster cap burn). |
| Residual staleness leaks into regenerated plans | Plans are authored against the freshly-cloned current `main`; the prompt forbids rebuilding Spring-Tide infra. |
| Single-node kind OOM | One cluster at a time; pre-warm; never run acceptance + test clusters concurrently. |

## Open execution decisions (resolve at GSD execution time)

- Exact v1alpha1→v1alpha2 transform for the skeleton CRs (milestones.yaml/phases.yaml → v1alpha2 manifests). Mechanical.
- Whether the skeleton apply + cluster bring-up is one `/gsd:quick` task or a small phase.
