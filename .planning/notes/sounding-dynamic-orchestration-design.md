# The Sounding — Semantic Orchestration Classifier for Dynamic Workflows

**Status:** Design recommendation — not yet routed through GSD. No code, CRD, or chart changes authorized by this document. Implementation must go through this project's own GSD workflow (research → requirements → milestone) before anything is built.
**Date:** 2026-07-06
**Scope:** a new decision layer that judges each node of TIDE's hierarchy and resolves the *shape* of orchestration applied to it — how many agents, in what topology, with what per-agent reasoning depth, and whether to substitute the node for a child sub-DAG. Layered **on top of** the existing planning/execution DAGs; it does not replace layered Kahn, the wave model, or the `pkg/dispatch.Subagent` seam.

> Code references below come from a one-shot code-map (2026-07-06) and are load-bearing for *where* things slot in, not yet re-verified line-by-line. Re-confirm each `file:line` at planning time per the Observe-First rule.

---

## The idea in one paragraph

Today TIDE decides orchestration shape **structurally and statically**: counts come from the DAG (layered Kahn derives waves), and the *kind* of work per node is one of five compiled-in templates. A trivial phase and a load-bearing one get the identical dispatch shape — `n × 1 × 1`. The **Sounding** adds the missing axis: a per-node classifier that reads the node's spec (or judges its semantic requirements) and resolves a **dynamic workflow shape** for it — a staged width-profile (`Fan and merge`, `Tournament`, `Judge panel`, …), a per-role reasoning budget (`model` + `--effort`), and an optional bounded recursive substitution into a child sub-DAG. The decision is **resolved once and frozen as data** on the CRD, so re-derivation on controller restart replays the same shape rather than re-judging — preserving TIDE's resumability contract exactly.

The name is a nautical fit for the existing water metaphor: a **sounding** measures water depth before you navigate. A shallow sounding → execute in place; a deep one → expand into a tidepool sub-DAG.

---

## Why now / the gap

The code has **no shape-deciding logic anywhere**. Shape is a product of exactly two things, neither content-aware:

1. **Compiled-in structural constants** — role/level/template hard-coded per reconciler (the four `BuildPlannerEnvelope(<level>, …)` sites in `dispatch_helpers.go:223`, and `buildEnvelopeIn` hard-coding `Role: "executor"` / `Level: "task"` at `task_controller.go:1484-1485`).
2. **DAG topology authored by the LLM planners** — the number of subagents per wave is a pure function of the `dependsOn` edges the planners emit, run through `pkg/dag.ComputeWaves` (`kahn.go:46`).

The only "profile"/"strategy" knobs that exist — `FailureProfile` (`strict`/`conservative`) and `planAdmission.fileTouchMode` (`strict`/`warn`) — gate *failure/validation behavior*, not dispatch shape. And `LevelConfig{Model, Params, Image}` (`api/v1alpha2/project_types.go:184`) resolves *which model*, never *how many agents or what shape*. There is **one field short** of a home for a strategy descriptor, and that absence is precisely the gap.

Crucially, the substitution mechanism the Sounding needs **already exists**: `EnvelopeOut.ChildCRDs []ChildCRDSpec` (`pkg/dispatch/envelope.go:207`) → `MaterializeChildCRDs` (`dispatch_helpers.go:256`, Kind allowlist `{Milestone,Phase,Plan,Task,Wave}`) is how a node expands into a new configuration of child nodes today — left entirely to planner discretion with zero shape control. The Sounding decides *what* ChildCRDs a node emits and *how many*.

---

## The Sounding contract (the load-bearing seam)

Every level CRD gains a derived, status-resident `Sounding` — mirroring how waves already live on `.status`, never `.spec` (operators declare intent; TIDE derives shape). Three parts, split along the determinism line:

```yaml
# .status.sounding on each node CRD  — DERIVED, never authored by the operator
status:
  sounding:
    signals:
      deterministic:            # computed in Go from topology — single-valued, re-derivable
        taskCount: 7
        filesTouchedBreadth: 12
        dependencyFanIn: 3
        dependencyFanOut: 2
      declared:                 # non-deterministic; append-only, timestamped, model-tagged; CAPPED
        - {ts: "2026-07-06T14:02Z", model: "opus-4-8", complexity: high, risk: med,
           breadth: wide, confidence: 0.72, rationale: "…", suggestedShape: {...}}
        - {ts: "2026-07-06T14:05Z", model: "sonnet-5", complexity: med, risk: med,
           breadth: wide, confidence: 0.61, rationale: "…", suggestedShape: {...}}
    resolved:                   # SETTLED, FROZEN — write-once; the operative decision
      shape:
        stages:                 # ordered pipeline — explicit integer widths ONLY, no ranges
          - {role: generate, width: 3}
          - {role: verify,   width: 2}
          - {role: merge,    width: 1}
        iteration: 1            # refinement rounds over the whole pipeline (orchestrator-visible)
        expansionDepth: 1      # recursive child-sub-DAG substitution budget (DFS, depth-bounded)
      reasoning:                # per-role intra-node depth — the second decision dimension
        perRole:
          generate: {model: opus,   effort: xhigh}
          verify:   {model: sonnet, effort: high}
      posture:                  # optional node-internal execution flavor (opaque cyclic protocols)
        protocol: none          # none | debate | blackboard | tree-search | …
        rounds: 0               # bounded internal rounds; realized inside a capable image
      name: "Tournament"        # DERIVED from stages
      label: "Tournament · 3 competitors, 2 verifiers"   # DERIVED, human-readable
      settledFrom: ensemble     # deterministic | fast-path | ensemble | operator-override
      settledAt: "2026-07-06T14:06Z"
      remainingDepthBudget: 1   # the ONLY mutable resolved field — decremented by runtime re-expansion
```

Permissions are **inputs the controller reads** (policy, not derived) — kept out of the controller exactly like `FailureProfile`/`fileTouchMode`:

```yaml
# .spec.subagent.levels.<level>.soundingPolicy — operator/chart authored
soundingPolicy:
  maxShape: {maxStageWidth: 4, maxTotalDispatch: 12, maxIteration: 2}
  maxDepth: 2                          # the hard DFS limit; expansionDepth can never exceed it
  allowRuntimeReexpansion: true
  judgeEscalation: onAmbiguous         # never | onAmbiguous | always
```

**The rules that make this safe:**

- **`signals.deterministic`** — single-valued, computed by Go from the already-materialized child-CRD topology (the same data `ComputeWaves` reads). Re-derivable on every reconcile; needs no persistence to be correct.
- **`signals.declared`** — the timestamped `{ts, model, verdict…}` **array**: an append-only ensemble/drift log. Static prompt assumed, so variance is model + sampling. **Capped** (keep last K, prune on settle) to honor the per-CRD etcd budget — the one growth risk, bounded by policy.
- **`resolved`** — **write-once / frozen**. Once `settledAt` is set, reconcile treats it as immutable input (like a landed wave assignment); re-derivation replays it rather than re-judging. `remainingDepthBudget` is the only mutable field.

The invariant this buys: **`signals.deterministic` + `resolved` are enough to replay any run.** The array is audit/ensemble only. Resumability stays "indegree map + completed-set," extended by exactly one frozen field per node — no new engine, no re-judging.

---

## The shape grammar

A shape is an **ordered pipeline of stages**, each a `{role, width}` with an **explicit integer width** — no ranges, no ambiguity. Ambiguity lives *upstream* in `signals.declared` (`confidence`, `rationale`); the settle step collapses it to explicit integers; `resolved` is explicit-only. (A range would be a choice deferred to dispatch time, which would re-introduce the non-determinism the frozen `resolved` exists to remove.)

The `stages` YAML is the **canonical machine form**. `name` and `label` are **derived** by a pure, deterministic classifier `Classify(shape) → (name, label)`; a React Flow mini-DAG is the visual render (same lib the dashboard already uses for the planning/execution DAGs, dev-tool palette per CLAUDE.md). No arrow-string DSL.

### The six archetypes

| # | Name | Canonical `stages` | Sounding picks it when… | Roles | Reduce | Dispatch cost |
|---|------|--------------------|--------------------------|-------|--------|---------------|
| 1 | **Solo** | `[{execute,1}]` | trivial / low-risk / high-confidence | 1 executor or planner | none | `1` |
| 2 | **Pipeline** | `[{s,1},{s,1},…]` | inherently staged work (draft→refine→finalize) | staged executors + optional critic | last stage wins | `#stages` |
| 3 | **Fan and merge** | `[{generate,N},{merge,1}]` | diverse approaches help (design, decomposition) | N generators + 1 merger | merger selects/combines | `N+1` |
| 4 | **Judge panel** | `[{generate,1},{verify,K}]` | high-stakes verification | 1 generator + K verifiers | vote/threshold | `1+K` |
| 5 | **Tournament** | `[{generate,N},{verify,K},{merge,1}]` | hardest, highest-value — diversity + rigor | N gen + K verify + 1 merge | score → best advances, graft runners-up | `N·(1+K)+1` |
| 6 | **Loop-until-dry** | any base + `iteration:k` + critic | unknown-size discovery (find *all* X) | base roles + completion critic | critic says "dry" or budget hit | `base × k` |

**Tidepool expansion is the recursion axis, not a 7th shape.** Any archetype may set `expansionDepth: d`, substituting the node for a child sub-DAG (each child gets its *own* Sounding), DFS depth-bounded. It composes with all six.

**Cost is real, not free.** Shapes 4–6 multiply pod count (a Tournament node = `N·(1+K)+1` Jobs). Dogfood run 2b OOM'd a single-node kind at ~60 concurrent pods (defect D3); concurrency caps only landed in v1.0.6 (Phase 32). `soundingPolicy.maxShape` ceilings + the existing `executorConcurrency` semaphore (`internal/pool/pool.go:43`) are the load-bearing guardrail: the Sounding can *want* a big shape, but the pool cap is what it actually gets.

---

## Two decision dimensions

Research (2026-07-06) confirmed the field of agentic patterns splits exactly along TIDE's dispatch granularity. The Sounding resolves **two orthogonal things**:

1. **Inter-node topology** — the `stages` shape above. This is what the orchestrator schedules across Jobs.
2. **Intra-node reasoning depth** — `model` + `--effort` per role, and optionally an opaque node-internal `posture` (a bounded cyclic protocol run inside one image). This is what happens *below* the envelope seam.

This subsumes the currently-static per-level model config into the Sounding, and **activates the unused `--effort` lever** — flagged in CLAUDE.md as "the highest-value tuning change." "This node is gnarly" now buys *both* a richer topology *and* deeper per-agent reasoning, judged from the same complexity/risk signals.

---

## Two workstreams, one contract (build in parallel)

A and B meet at the `Sounding` schema. **A produces `signals.declared`; B consumes signals and writes `resolved`.** Neither blocks the other; a golden/contract test on the schema is the handshake, and A is not thrown away when B lands — it becomes B's richest signal source.

### Workstream A — planner-embedded (ships fast)

Extend the five planner templates (`internal/subagent/common/templates/*.tmpl`) so that **for every child CRD a planner emits** it *also* emits a sounding assessment (`complexity / risk / breadth / confidence / rationale / suggestedShape`). Plumbing: add an optional `Sounding` block to `ChildCRDSpec` (`pkg/dispatch/envelope.go`); `MaterializeChildCRDs` (`dispatch_helpers.go:256`) writes it into the child's `.status.sounding.signals.declared`. Per CLAUDE.md's Opus-4.8 note, the template directive states scope **explicitly** ("for *every* child CRD you emit…") — the model won't generalize it from one example. A can ship a stopgap resolution ("use `suggestedShape` verbatim") to prove value before B exists.

### Workstream B — controller `StrategyResolver` + judge subagent + settle (the durable engine)

A new Go resolver mirroring `ResolveProvider`'s precedence walk (`dispatch_helpers.go:138`). Per node it:

1. computes `signals.deterministic` in Go from materialized child-CRD topology (same data `pkg/dag.ComputeWaves` reads);
2. reads `signals.declared` (from A and/or the judge);
3. applies deterministic fast-path thresholds → `settledFrom: fast-path`;
4. for ambiguous/high-stakes nodes (per `soundingPolicy.judgeEscalation`), dispatches a **6th template** — the *sounding judge* — through the same envelope/Job seam, appending `{ts, model, verdict}` to the array;
5. **settles**: aggregate (vote/median/highest-confidence) + clamp to `maxShape`/`maxDepth`, write `resolved` **write-once**.

The dispatch sites (`BuildPlannerEnvelope` / `buildEnvelopeIn`) then read `resolved.shape` instead of hard-coding "1 planner / 1 executor."

### The one genuinely new execution mechanism

Today `1 CRD = 1 Job`. A `generate: 3` / `verify: 2` shape means a node fans into **multiple sibling Jobs plus a reduce/merge step**. That is precisely the **"wave-internal sub-scheduler behind Kahn"** CLAUDE.md prescribes — it lives *below* the DAG (inside one node), so wave derivation is untouched. It is the biggest build item in B and the thing to prototype first.

---

## Hybrid timing — plan-time freeze + bounded runtime re-expansion

- **Plan time (rising tide):** the classifier fires during planning and materializes the initial depth-bounded tree as child CRDs before execution. An LLM judge is safe here because its output is frozen into CRDs, never re-judged.
- **Runtime:** under `allowRuntimeReexpansion` + `remainingDepthBudget`, a node may re-classify and expand **once more** (e.g. on failure, or when it discovers the work is bigger than judged). New child CRDs join the graph; the reconcile loop re-derives waves — which TIDE already does on every plan edit. Adaptivity without unbounded runtime mutation.

Every runtime expansion must be **idempotent and persisted-once as CRDs**, or re-derivation on controller restart diverges. Because a node stays atomic to the orchestrator, a node-internal `posture` cycle must terminate and **its internal trace is never resumption state** — if a Job dies, TIDE re-runs the whole node from its input envelope. Downstream depends only on the output envelope, never the transcript.

---

## Pattern validation — the model is a superset behind one boundary rule

The field of named patterns (ReAct, CoVe, ReWOO, Self-Debug, MoE, MoA, ToT, debate, best-of-N, evaluator-optimizer, orchestrator-workers, and the Anthropic *Building Effective Agents* taxonomy) maps onto this model as follows.

**The load-bearing principle:** *TIDE's dispatch granularity is one subagent turn. Everything below it is the image's business; everything above it is topology the Sounding may shape.* That single sentence tells you ReAct is opaque and Judge-panel is not.

### ① Intra-node — the image owns it (the Sounding never reaches inside a Job)

| Pattern | Lands as |
|---|---|
| ReAct, CoT, Plan-and-Solve, Least-to-Most, PAL | inherent executor tool-loop / prompting — nothing to model |
| Reflexion, Self-Refine, CoVe | intra-node self-correction — at most an executor directive |
| **Self-Debugging** | **Executor + "debugger" posture; the run→read-error→fix loop stays inside one Job** — an outer orchestrator retry-loop would fragment debugging context across processes |
| Reasoning-model long-CoT (o1/R1-style) | **bought via `model` + `--effort`, not via dispatch** |

### ② Topology we already have (the six shapes + `iteration` + expansion absorb these)

| Pattern | Lands as |
|---|---|
| Parallelization/voting, Self-Consistency | Fan and merge / Judge panel (vote reducer) |
| Parallelization/sectioning | Fan and merge (distinct subtasks) |
| Best-of-N / verifier-guided decoding (PRM) | Tournament / Judge panel (verifier reducer) |
| Evaluator-optimizer | Loop-until-dry + eval critic |
| Orchestrator-workers (dynamic spawn) | Tidepool expansion + runtime re-expansion |
| MoA (single / multi-layer) | Fan and merge (+ `iteration` for layers) |
| Skeleton-of-Thought | planner → one wave (already how planning fans out) |
| Prompt-chaining / ADK-Sequential | Pipeline |
| **Routing / agentic-MoE** | **the Sounding itself** — the classifier we're building *is* this named pattern |
| **ReWOO** | **TIDE's planning-DAG→execution-DAG split — we don't embed it, we *are* it, generalized to inter-agent** |
| **LLM-as-judge / verifier gate** | **Verifier/Judge role + slack-tide checkpoint** — the review subagent CLAUDE.md already anticipates |

### ③ Node-internal cyclic protocols — allowed behind one boundary rule

A cyclic protocol (debate, blackboard, dialogue, tree-search) is a **legal node** iff it has (1) a defined input envelope and output envelope so it fits the DAG as one node with edges, (2) bounded/terminating internal rounds, and (3) no orchestrator-held shared state — the cycle lives entirely below the envelope seam.

- **Acyclicity is a property of the node-DAG, not of node internals.** "Cycles are bugs" still governs the dependency graph the orchestrator schedules; a node's insides may cycle freely, bounded.
- This is exactly what the **LangGraph strategy image** (locked polyglot-subagent milestone) is for — internal agent-loop graphs behind `pkg/dispatch.Subagent` — and consistent with the ADK note's own carve-out: a graph engine is fine *"image-internal, per-task, stateless across Jobs."* It reopens nothing.
- It is **not a 7th shape** — it's a `posture` on the execute stage, the same slot as the model/effort dimension. `Solo` + `posture: {protocol: debate, rounds: 3}`; the orchestrator sees one node, the image runs the cycle. Debate/CAMEL/blackboard/GroupChat/ToT all become postures the Sounding may pick, realized by a capable image.

---

## Non-Goals (deliberate, not oversights)

Exactly one anti-pattern is ruled out, plus one non-agentic item:

- **Orchestrator-held shared mutable state across independent DAG nodes** — a controller-maintained shared conversation thread / mesh with no per-node I/O boundary and no termination guarantee. This is the genuine "second control plane" — against DAG-only scheduling, Job isolation, and the "no second graph engine" anti-pattern (echoing the ADK evaluation's rejection of the `workflow`/A2A engine). The discriminator is precisely: *does the pattern present a defined input and output that fits the larger DAG?* If not, it's out.
- **Model-architecture MoE** — intra-model token gating in one forward pass. Not agentic; not in scope on any axis.

Writing these down as conscious non-goals — with the ReWOO/Routing/judge validations as the positive frame — is what keeps a future contributor from "adding debate support" and dragging a message bus into the controller. (Node-internal debate is already allowed under §③; an orchestrator-level message bus is not.)

---

## Role specialists

Today: `planner` (four level templates) + `executor` (one). The shapes compose from finer roles — most need only one level-agnostic template, so this adds ~5 templates, not ~30.

| Role | Purpose | Load-bearing spec line | Emits | Status |
|------|---------|------------------------|-------|--------|
| **Planner** (×4 levels) | decompose a level into child CRDs | existing 4 templates | `ChildCRDs` | exists |
| **Executor** (task) | produce the diff/artifact | existing template | work product | exists |
| **Generator** | one candidate with a *distinct angle* | a planner/executor invoked N-parallel with a divergence directive (risk-first / MVP-first / user-first) — **a posture, not a new binary** | one candidate | new posture |
| **Verifier / Critic** | adversarially check a product | **prompt for coverage, not conservatism** — coverage + severity + confidence tags, filter downstream (CLAUDE.md calls this out for exactly this future role) | findings[] w/ severity+confidence | new |
| **Judge / Scorer** | rank competitors (Tournament) | scores/orders, doesn't just find bugs; deterministic tie-break | ranking + scores | new |
| **Merger / Synthesizer** | combine/select into one product | synthesize from the winner while grafting the best of runners-up | merged product | new |
| **Sounding judge** | classify a node → shape verdict | the 6th-template classifier; coverage-tagged, confidence-scored | `signals.declared` entry | new |
| **Completion critic** | "is there more to find?" (Loop-until-dry) | asks *what's missing* — modality not run, claim unverified | done + gap list | new |

Design calls worth a reviewer's veto: **Generator is a posture, not a binary** (keeps the image matrix from exploding); **Verifier and Judge are separate roles** (defect-coverage-first vs. quality-comparison-first — opposite prompt postures).

---

## Invariants — how this satisfies TIDE's constraints

| Constraint | How the Sounding stays inside it |
|---|---|
| **Waves derived, not declared** | The Sounding shapes *within* a node (intra-node topology) or *emits child CRDs* that layered Kahn still derives waves over. The DAG remains the only scheduling input; no wave list is ever accepted. |
| **Determinism / resumability** | `signals.deterministic` is re-derivable; `signals.declared` is audit-only; `resolved` is write-once frozen; runtime re-expansion is idempotent + persisted-once as CRDs. Replay = re-derive over the materialized graph + the frozen `resolved`. |
| **No second graph engine** | No cyclic message-passing, blackboard, mesh, or runtime peer-routing at the orchestrator level. Everything is still a dependency DAG of native K8s Jobs; the intra-node fan-and-reduce is a bounded sub-scheduler, not a graph engine; node-internal cycles are opaque and enveloped. |
| **Cycles are bugs** | Applies to the node-DAG (unchanged). Node internals may cycle, but only bounded and behind a defined I/O envelope. Expansion is DFS depth-bounded → terminates. |
| **CRD `.status` only, etcd budget** | Sounding is status-resident; the only unbounded field (`signals.declared`) is capped/pruned; per-CRD stays well under the etcd limit. |
| **Cost / pod-count (D3)** | `soundingPolicy.maxShape` ceilings + `executorConcurrency` semaphore bound realized dispatch regardless of what the Sounding wants. |
| **Provider/host/auth neutrality** | The Sounding carries no provider assumptions; per-role `model` resolves through the existing `ResolveProvider` precedence and vendor abstraction. |

---

## Staged roadmap (the maturity ladder)

The classifier evolves `cheap-deterministic → richer Go signals → ML/judge`. Each stage is a GSD milestone; each swaps a component behind the one `StrategyResolver` interface without disturbing the others.

1. **Milestone 1 — Contract + deterministic fast-path + Workstream A.** The `Sounding` CRD schema, `soundingPolicy`, the Go `StrategyResolver` with deterministic signals + fast-path rules, planner-emitted `signals.declared`, the derived name/label/render, and the two cheapest shapes wired (Solo baseline + Judge-panel or Fan-and-merge). No LLM judge yet — the whole seam ships deterministically.
2. **Milestone 2 — Judge subagent + settle + ensemble.** The 6th template (sounding judge), the `{ts, model, verdict}` array, settle/aggregate/freeze, escalation policy. Adds the non-deterministic tier.
3. **Milestone 3 — Intra-node fan-and-reduce + richer shapes.** The wave-internal sub-scheduler (node → N Jobs + reducer), Tournament, Loop-until-dry, and the Verifier/Judge/Merger/Completion-critic roles.
4. **Milestone 4 — Tidepool runtime re-expansion + reasoning dial.** Bounded runtime re-expansion under `remainingDepthBudget`; per-node `model` + `--effort` resolution (activates the effort lever); node-internal `posture` protocols realized in the LangGraph image.
5. **Milestone 5+ — ML classifier.** Swap deterministic rules for a learned model behind the same resolver interface — the "other ML" branch.

---

## Open questions / decisions deferred to GSD

- **Settle aggregation function** — vote vs. median vs. highest-confidence vs. weighted-by-model. Likely per-`soundingPolicy` configurable.
- **`signals.declared` cap K** and prune policy — smallest value that preserves a useful ensemble/drift signal without straining etcd.
- **Where the Sounding fires relative to planner dispatch** — a distinct pre-planner pass vs. folded into the same reconcile tick. Affects latency and the plan-time-freeze ordering.
- **Which levels get per-level sounding-judge templates** vs. one level-agnostic template — probably start level-agnostic, specialize only if signal quality demands it.
- **Divergence-directive vocabulary for Generators** — the concrete set of "angles" (risk-first / MVP-first / user-first / …) and whether it's fixed or Sounding-selected.
- **Interaction with `FailureProfile`** — how a failed sibling inside a Fan/Tournament stage propagates under `strict` vs. `conservative`.

---

## Process note

This is a design recommendation produced through a brainstorming dialogue, outside the GSD workflow. It authorizes no code, CRD, or chart changes. If TIDE adopts the Sounding, it must go through this project's own GSD workflow — research → requirements → milestone → phase plans — before any implementation begins, staged per the roadmap above. `charts/tide/values.yaml` remains the FIXED contract: the binary catches up to the chart, never the reverse, so the `soundingPolicy` surface is designed chart-first when the time comes.
