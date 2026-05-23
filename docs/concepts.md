# TIDE Concepts

**Audience:** Operators learning TIDE's mental model before authoring their first Project.

**Status:** v1.0; complements [README.md](../README.md) (which is the design spec) with operator-readable framings of the five-level paradigm, the two-DAG split, derived waves, and the water metaphor.

**Scope of this doc:**

- The five-level hierarchy: what each level *is* in operator terms (not what its CRD field looks like).
- The two distinct DAGs (Planning vs Execution) and why they fan out differently.
- How TIDE derives waves automatically from your declared task graph — you never write a wave list.
- The water metaphor (rising tide, slack tide, tidal lock, tidepool, TIDE pod) mapped to operator-visible behaviors.
- Where to go next once the mental model clicks.

## The five-level hierarchy

A TIDE workflow nests five units of work, from the outcome you care about down to a single subagent dispatch. You author the top — TIDE authors the rest by recursive planning.

- **Project.** Your outcome statement: *"Add OpenAI provider support to TIDE."* You apply a `Project` CRD with that outcome prompt, a target git remote, and credentials. The Project is the operator-visible root; everything else is authored downstream by TIDE.
- **Milestone.** An outcome-bearing capability slice — the largest unit a human stakeholder reviews. The README's worked example: an entire add-OpenAI-support deliverable. Milestones depend on each other in a DAG, not a line. TIDE writes one `MILESTONE.md` per milestone.
- **Phase.** A coherent piece of a milestone that delivers something incrementally testable. A milestone usually has 3-8 phases. TIDE writes a phase brief per phase.
- **Plan.** The file-and-line-level specification of one phase slice — paths, signatures, tests, acceptance checks. A phase usually has 2-15 plans. Plans within a phase can run in parallel or serially depending on their declared task dependencies. TIDE writes one `PLAN.md` per plan.
- **Task.** The atomic unit of code mutation — small enough to finish in one subagent pass, large enough to be meaningful. A plan usually has 3-12 tasks. Each task produces a diff, a test, or a file.

Waves are *derived* from the task DAG — they are not a sixth level. You declare which tasks depend on which other tasks; TIDE computes wave layout via layered Kahn's algorithm at dispatch time. Operators never write a wave list, never see a wave field on the CRD spec — only on the status.

## Two distinct DAGs

The same Kahn-layered algorithm runs on two structurally different graphs. Confusing them is the most common operator pitfall.

- **Planning DAG.** *Which artifacts must exist before another artifact can be authored.* Shallow and wide. Most phases within a milestone can be planned the moment the milestone brief is locked — the planning DAG fans out across the breadth of your subagent pool. Example: once `01-foundation/PHASE.md` is written, plans `01-A`, `01-B`, and `01-C` can all be authored in parallel.
- **Execution DAG.** *Which code must exist before another piece of code can be written or run.* Deeper and narrower. Reflects real interface dependencies between files. Example: a task that wires `OpenAIClient.New()` cannot run until the task that defines the `OpenAIClient` struct has landed.

The wave model only matters for the Execution DAG. Planning work is almost always fan-out-able to the breadth of subagent capacity; execution work has to respect real code dependencies. TIDE keeps these two DAGs *typed apart* — they share the algorithm but never the data structure.

## Wave derivation

You declare task edges. TIDE derives waves.

Concrete example using the canonical α-θ fixture cited in `pkg/dag`'s worked example. You write task dependencies in `PLAN.md`:

```
α  (no dependencies)
β  depends on α
γ  depends on α
δ  depends on β, γ
ε  depends on β
ζ  depends on γ
η  depends on δ, ε
θ  depends on η, ζ
```

TIDE runs layered Kahn's algorithm and produces the wave layout automatically:

```
Wave 0: α
Wave 1: β, γ          (parallel — both unblocked once α completes)
Wave 2: δ, ε, ζ       (parallel)
Wave 3: η
Wave 4: θ
```

You never wrote `wave: 0` or `wave: 1` anywhere. You declared edges; TIDE produced the schedule. Re-derivation is cheap (O(V+E)) — on every plan edit TIDE recomputes, so there is no stale-schedule caching to invalidate. If you add a new task with the right dependencies, the wave layout updates on the next reconcile.

If a task fails mid-run, TIDE keeps a resumption state of exactly two things: the indegree map and the completed-task set. A fresh controller restart re-derives the remaining waves from those two artifacts in O(V+E) — no recovery state to corrupt.

## The water metaphor

The vocabulary is intentional and shows up in CRD names, log lines, and metrics. Operators see these terms in the dashboard and in `kubectl describe` output.

| Term | Operator-visible behavior |
|------|---------------------------|
| **rising tide** | A planning wave fanning out across subagents — N planner Jobs dispatched in parallel for the same level. You see this in `kubectl get jobs` as a burst of Pods starting at once. |
| **slack tide** | A review checkpoint between waves where TIDE pauses for an approval gate (per `Phase.Spec.gatePolicy`). The dashboard shows the gate awaiting approval; `tide approve` or `kubectl annotate` advances it. |
| **tidal lock** | A phase whose upstream dependencies have all resolved — it is ready to dispatch. The Project status shows the phase transitioning from `Pending` to `Ready` once the lock condition holds. |
| **tidepool** | A sub-DAG developed in parallel isolation — typically a plan within a phase that fans out internally without affecting siblings. Useful framing when one plan owns a self-contained surface (e.g., the `internal/subagent/openai/` skeleton). |
| **TIDE pod** | The K8s Deployment running the TIDE orchestrator — `deploy/tide-controller-manager` in the `tide-system` namespace. The K8s-pun is load-bearing: TIDE is a Kubernetes-native operator, not a separate scheduler. |

## Where to next

Mental model in place? Two natural next steps:

- [INSTALL.md](INSTALL.md) — install + first sample. Get TIDE running against a fresh kind cluster in under 5 minutes.
- [project-authoring.md](project-authoring.md) — write your first Project. Walks the `Project.Spec` field reference and the three sample Projects (small / medium / large) shipped under `examples/projects/`.
