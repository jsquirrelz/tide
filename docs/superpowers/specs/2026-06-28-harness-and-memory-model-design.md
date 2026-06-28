# Design: The Harness & Memory Model

**Status:** Design — for review (not yet planned/implemented)
**Date:** 2026-06-28
**Scope:** The domain *model* (abstraction + CRD/`project.yml` shape + invariants + observability contract). Concrete harnesses (verify/reflect/etc.) and the dashboard build are downstream instances, scoped separately.

---

## 1. Thesis

TIDE today is **act-and-adopt**: each node dispatches one subagent and commits its first-draft envelope as-is. There is no verification layer, planners can be falsely marked `Succeeded` on empty output, and authored work is "first-draft, harden-later." This design adds a **fifth dispatch axis** — the **harness** — alongside the four TIDE already has (`role`, `model`, `effort`, `gate`). A harness is *how* a node produces its one outcome; making it declarative, per-level and per-phase, turns "act-and-adopt" into "**draft-verify-adopt**" without baking any one strategy into the controller.

The model was validated two ways: by **composing** the six Anthropic dynamic-workflow patterns and the six prompting/agent papers (CoVe, Reflexion, ReWOO, Self-Debug, Plan-and-Solve, the Prompt Report survey) from its atoms, and by **grounding** it against GSD's real workflow flows (`plan-phase`, `execute-phase`, `code-review`, `debug`, `plan-review-convergence`, `audit-fix`) — the reference implementation of this exact domain. Grounding is what corrected the naive first draft.

**Headline result:** the model is mostly a **unification of constructs TIDE already has, scattered** (gates-as-holds, FailureProfile, waves, resumption/completed-set, `--effort`, pluggable subagent, the envelope) under one recursive frame, plus a few genuinely new primitives (`compute`, `router`, typed `verdict`, the reflexion `loop`). It is not a rebuild.

## 2. Core model

- A **harness** is a node's *internal* production strategy: a graph of **stages** that yields **exactly one outcome**.
- A **stage** is either a **leaf** (one subagent dispatch) or a **sub-harness** (recursion — the fractal).
- **Two composition axes:** *horizontal* (stages in `sequence` / `parallel`) and *vertical/fractal* (a stage is itself a harness). The five-level hierarchy is already a fractal unrolling of one harness: "decompose into a child DAG," applied recursively, bottoming out at the executor leaf.
- **Outcome** ∈ `{artifact | child-set | skip | block | deferred}` — *not* "exactly one file." (`child-set` is today's planner.)

**Modeling depth — decision: option (c), "scale-invariant but layered."** Harnesses are modeled as recursive *data* inside a contained sub-schema; the five **named, typed levels stay on top** (Milestone/Phase/Plan/Task keep their distinct artifacts and gate semantics); the global Execution DAG stays on the leaf layer untouched. Rejected: (a) node-local-enum-only (no fractal, blends need code), and (b) full-fractal-primitive (collapses the typed levels and re-nests the just-shipped global DAG — too much risk for unnamed expressiveness).

### Load-bearing invariants (must not weaken)

1. **One outcome per node → the DAG is over outcomes.** Whatever a harness does internally, the node exposes one outcome slot (`Succeeded`/`Skipped`/`Blocked`/…). So **cycle detection, layered-Kahn wave derivation, and Spring Tide's global Execution DAG are untouched.**
2. **A back-edge is legal only if it carries a cap.** A declared bounded iteration (`loop`, ≤N) is a *different construct* from a dependency cycle (unbounded deadlock — still a bug, still rejected at validation). The cap is the safety property. `MaxAttemptsPerTask` is the existing seed of this.
3. **Production-recursion ≠ the global Execution DAG.** The fractal lives in *how each artifact is produced* (planning/authoring). The global DAG is the *flattened leaf-dependency order* for dispatch. These are the two DAGs; the harness must not silently re-nest the execution DAG.
4. **Default harness = `decompose` = today's behavior.** Fully opt-in, backward-compatible.

## 3. Atom catalog

`[have]` = exists in TIDE today (named/composed here) · `[new]` · `[partial]` = exists in fragment.

**Doers** (produce a value/outcome):
- **`leaf`** `[have]` — one dispatch. Pluggable target (Claude subagent | external CLI | script) `[partial]`; params `model` / `effort` / `mode` `[have]`.
- **`compute`** `[new]` — deterministic, non-LLM transform/check (`grep`, `make test`, scope-resolve, SDK query). *The largest omission in the draft; most real workflow steps are this.*

**Combinators** (compose stages; produce nothing themselves):
- **`sequence`** `[have, implicit]`
- **`parallel`** + join `{barrier-reduce [have=waves] | select/vote [new]}`; `select` may pick **top-k** (subset/beam).
- **`loop`** — body + **feedback-state** `[new]` (may be **set-valued** — a beam/frontier) + cap `{count [have=maxAttempts] | convergence/stall [new] | budget [have]}` + onExhaust `{reject | escalate-to-gate | accept-degraded}` `[new]` + failure-policy `{strict | conservative}` `[have]`.
- **`router`** `[new]` — branch on a `verdict` to a sub-path (this *is* classify-and-act); may select **top-k**.

**Holds:**
- **`gate`** `[have = gates-as-holds, Phase 25]` — human | policy | config; may block-and-suppress downstream.

**Recursion:**
- **`sub-harness`** `[partial]` — a stage that is itself a harness (the level hierarchy already is this).

**Typed glue:**
- **`verdict`** `[new]` — typed enum + **structured payload** (scores/weights/severity); `gating | advisory`. A `leaf`/`compute` *in oracle role* emits one.
- **`outcome`** — `{artifact | child-set [have] | skip [new] | block [partial] | deferred [new]}`.

**Cross-cutting modifiers:**
- satisfied-guard / skip-if-done `[have = completed-set]`; the **threaded context = the envelope** `[have]` (the data bus — see §4).

## 4. The data substrate (the envelope as bus)

Atoms describe *control* flow (when stages run); harnesses also need *data* flow (what each stage reads/writes). TIDE already has the substrate: **the envelope.** Every stage `reads` from and `emits` to the node's shared context; a `loop`'s feedback is "a stage emits a reflection to context, the next iteration reads it" (Reflexion); ReWOO's `#E` and GSD's scope-injection are the same. No new machinery — name the envelope as the harness data bus, and let stages declare `reads`/`emits`.

## 5. Composition — building the patterns from atoms

| Harness | Composition |
|---|---|
| decompose (default) | `leaf(planner)` → outcome=child-set |
| classify-and-act / MoE (1 expert) | `oracle ⇒ verdict` → `router{→leaf_A | leaf_B}` |
| fan-out-synthesize (= waves) | `parallel.barrier-reduce[leaf×N]` → `leaf(synthesize)` |
| verify (CoVe / plan-then-check) | `leaf(generate)` → `loop{ body: leaf(revise) reads verdict; until: oracle.passed; cap.count; onExhaust: escalate }` |
| generate-and-filter | `parallel[leaf×N]` → `compute(dedup)` → `leaf(filter-by-rubric).select` |
| tournament | `parallel[leaf×N]` → `loop.bracket{ leaf(judge pairwise) }` → select |
| reflect-retry (Reflexion / Self-Debug) | `loop{ body: leaf(act) emits reflection; until: oracle(tests-green); cap.count; onExhaust: gate(human) }` |
| verify-then-route (GSD verify-phase) | `oracle(verify) ⇒ typed-verdict` → `router{ passed→done | gaps→sub-harness(decompose fixes) | human→gate }` |
| MoE (top-k) | `router(top-k) → parallel → compute(weighted reduce)` |
| beam-ToT | `loop{ parallel[expand frontier→K] → oracle(score) → select top-M }` (frontier = set-valued feedback) |

All six diagram patterns and the GSD presets compose — the completeness proof. **Cross-node correction loops** (verify→replan→re-execute / "gap closure") are not special: a node-internal harness can't see siblings, but its **parent harness can**, and that loop is simply the *parent's* `loop`. The recursion pays for itself.

## 6. Configuration & schema (illustrative; terms TBD)

Harness is a field on the existing per-level / per-phase config. **Named presets** (noun-shaped, like the rest of TIDE) for the common case; **explicit `pipeline`** composition + nesting for blends.

```yaml
subagent:
  levels:
    phase: { model: sonnet-4-6, harness: { preset: verify, effort: xhigh, grounded: true } }
    task:  { model: haiku-4-5,  harness: { preset: reflect, oracle: tests, cap: 3 } }
  phaseOverrides:
    - phase: phase-03-tricky-migration
      harness: { preset: tournament, attempts: 3 }
```

Explicit blend (generate → check → bounded revise loop):

```yaml
harness:
  pipeline:
    - id: draft
      leaf:  { role: planner }
    - id: check
      oracle:{ by: leaf, effort: xhigh, grounded: true, reads: [draft], emits: verdict }
    - loop:
        until:     { verdict: { from: check, equals: passed } }
        cap:       { count: 2 }          # MANDATORY — uncapped loop rejected like a cycle
        onExhaust: { gate: { human: "proceed | retry | abandon" } }
        body:
          - leaf:   { role: planner, reads: [draft, check.verdict] }
          - oracle: { by: leaf, reads: [draft], emits: verdict, id: check }
```

Router and fractal nesting:

```yaml
harness:
  pipeline:
    - id: verify
      oracle: { by: leaf, emits: verdict }          # {passed, gaps_found, human_needed}
    - router:
        on: verify.verdict
        routes:
          passed:       { outcome: done }
          gaps_found:   { sub-harness: { preset: decompose, level: plan } }
          human_needed: { gate: { human: true } }
    - parallel:                                       # tournament whose each attempt is a reflect harness
        join: select
        of:  { count: 3, each: { preset: reflect, oracle: tests, cap: 2 } }
```

Validation guardrails: mandatory `cap` on every `loop`; bounded nesting depth; allowed stage types only; CEL-validatable presets.

## 7. Memory model

Memory is the **irreducible state harnesses read/write** — it extends TIDE's existing rule (*resumption = indegree + completed-set; re-derive, don't cache*) to: **persist only what cannot be re-derived; re-compute the rest.** Grounded in Behrouz, *Nested Learning* (NeurIPS 2025) — whose machinery is leaf-internal (a category error to build here) but whose **principles** transfer. (TurboQuant, arXiv:2504.19874, was reviewed and ruled out as a model-internal/vector-DB codec — one crumb: compress a store for the *query*, not faithful reconstruction.)

**Three scopes = a frequency spectrum, wired (not siloed):**

| Scope | Update frequency | TIDE substrate |
|---|---|---|
| **working / node-local** | per stage/step (high, low capacity) | the **envelope** |
| **episodic / run-local** | per node (mid) | **SharedContext**, completed-set, envelopes-as-artifacts |
| **long-term / cross-run** | per run (low, high capacity, most persistent) | **salvage bundles** + (new) **surprise records** |

Capacity *increases* as frequency drops; a slow lesson can **read-back** into a fast context (circulation).

**Write rule — surprise-gated consolidation (decision: mechanical-only for v1, option (a)).** At each scope boundary, a **consolidation** step promotes only *surprising* state up one timescale: node-exit distills surprising working state → episodic; run-close → cross-run. **Surprise is mechanical and free** — signals the harness already emits: a `verdict` diverging from the plan's prediction, a `gate` flip, a task failing where it predicted success, a cascade, a budget halt. **The irreducible thing *is* the surprises** — so re-derive mechanical state (indegree/waves/readiness) as before, but **persist the surprise records.**

With mechanical-only, the persisted "lesson" **is the structured surprise record itself** (gate flip, failure-vs-prediction delta, cascade signature) — *not* an LLM-authored prose lesson. **Recall/read-back is also mechanical** (match by structural keys — level, node-type, signature). This is **zero extra LLM cost** (memory writes are structured strings to `.status`/PVC) and fits CRD-only persistence (records are tiny, well under the 1.5 MiB etcd cap).

**Memory adds no new atoms:** *distill* and *recall* are `leaf`/`compute` stages; the bus is the envelope; the surprise signal is the `verdict`/`oracle` we already have; *consolidation* is a stage that fires at a scope boundary.

## 8. Non-goals (explicit boundaries)

- **Leaf-internal techniques** — CoT, micro-ReAct, tool use, prompt style — are *not* harness atoms; they live inside a `leaf` (`effort`/`mode`).
- **Open-ended search with backtracking** (full ToT / general search) — out of scope; encapsulated in a `leaf`/`sub-harness` as a black box, never the declarative grammar. The harness models *bounded, mostly feed-forward* orchestration, not a search algebra.
- **LLM-distilled prose lessons (memory option (c))** — deferred behind a cost model; the schema reserves the seam.
- **Semantic retrieval / external vector store** for recall — forbidden by CRD-`.status`+PVC-only; recall stays mechanical (structural-key match) for v1.
- **Implementing CMS/Hope/test-time-weight-learning** in the orchestrator — a category error; TIDE dispatches black-box CLIs and cannot touch weights.

## 9. Composes with existing TIDE (the unification)

`gate` = gates-as-holds (Phase 25) · loop failure-policy = FailureProfile (strict/conservative) · `parallel.barrier-reduce` = waves (Spring Tide) · loop count-cap = `MaxAttemptsPerTask` · satisfied-guard / re-derive = completed-set + indegree resumption · `leaf` params = per-level `model` + `--effort` · pluggable `leaf` = the Subagent interface / cross-AI delegation · the data bus = the envelope · episodic memory = SharedContext (Phase 20). The harness/memory model *names and composes* these under one recursive frame; the genuinely new pieces are `compute`, `router`, typed `verdict`, the reflexion `loop`, and surprise-gated consolidation.

## 10. Observability contract (dashboard)

Harness internals are **invisible to the DAG** (preserving Kahn/waves/global-DAG) but **fully visible to the operator.** The model therefore carries a **status sub-structure** recording harness execution — current stage, loop iteration *n/N*, each `verdict`, nested sub-harness, consolidation/surprise events — that the dashboard renders as **node-detail** (expand a node to see its harness "filmstrip"), **not** as new graph nodes. This is the same status data resumption already needs.

## 11. First instances (downstream, not this spec)

D1–D4 from the dogfood-run-2b findings become the model's first preset instances: D4 (false-`Succeeded`) → the binary-reward `oracle`/`gate`; "first-draft" quality → the `verify`/`reflect` presets; D1/D2 (cost/lifecycle under adoption) → the ReWOO-style per-level *Solver*/consolidation seam; D3 (concurrency) lands alongside (verify/reflect add fan-out). The likely *implementation vehicle* is **dogfood run #3** (TIDE building its own verification harness, watched executing in the now-harness-aware dashboard) — decided separately.

## 12. Open questions (deferred)

- Cost model for the LLM distill-writer (gates the memory option-(c) upgrade).
- Exact `verdict` enum vocabulary per level (GSD offers a ready taxonomy: `passed | gaps_found | human_needed`, `SECURED | OPEN_THREATS | ESCALATE`, severity buckets).
- Max nesting depth + the precise CEL/webhook validation surface for the `pipeline` grammar.
- Whether the per-phase override is set only in `project.yml` or a planner may *assign* a child's harness at authoring time (dynamic classify-and-act on harness selection).
