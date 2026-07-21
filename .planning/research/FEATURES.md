# Feature Research

**Domain:** Kubernetes-native agentic-orchestration platform — v1.0.10 "King Tide" (LangGraph authoring migration, Product/System/Oversight loops, dynamic-workflow patterns)
**Researched:** 2026-07-21
**Confidence:** MEDIUM (industry ecosystem patterns are HIGH-confidence and well-established for MLOps/LLMOps promotion mechanics and HITL autonomy; TIDE-specific application is a novel synthesis with LOW-confidence points flagged inline — TIDE's Job-per-dispatch, no-external-DB, derived-waves constraints have no direct industry analog)

## Framing note

TIDE already has extensive internal design work covering this milestone's scope:
`.planning/notes/five-loop-model.md` (the Product/System/Oversight loop dispositions),
`.planning/notes/langgraph-successor-runtime-strategy.md` (the evidence-gated migration ladder),
`.planning/notes/sounding-dynamic-orchestration-design.md` (the full dynamic-workflow shape grammar — Judge panel/Fan-and-merge/Tournament archetypes), and
`.planning/seeds/verify-level-subagent.md` (the original gap map). This research does **not** re-derive those designs — it grounds them against how the wider agentic-orchestration/MLOps ecosystem actually builds these five capabilities today, flags where TIDE's existing design already matches best practice, and flags where it diverges (by necessity, given TIDE's constraints) or is over/under-scoped relative to what ships as v1.

Per the quality gate: each capability below is framed as a **five-element loop** (goal/spec, mutable candidate, evaluator, repeat-policy, bounded exit) where it qualifies, or explicitly flagged as a **pipeline stage / evaluator strategy / continuous policy layer** where it doesn't.

## Feature Landscape

### Table Stakes (Users Expect These)

Features an operator/adopter of a "loop-engineering" agentic platform assumes exist once Product/System/Oversight are advertised as "live," and features required for the dynamic-workflow patterns to be safe rather than a cost/quality liability.

| Feature | Why Expected | Complexity | Notes |
|---------|--------------|------------|-------|
| **Outcome-level judge at Project/Milestone boundary** (Product loop, narrow slice) | "Product loop" implies the system checks its own work against the *original ask*, not just per-task specs — otherwise it's just the Task loop renamed | MEDIUM | Five elements: goal=outcome prompt, candidate=artifact tree at boundary, evaluator=an outcome-judge dispatch (reuses verifier seam), repeat=author a corrective Phase/Milestone, bounded exit=judge approves OR `maxIterations` (precedent: plan-check's `1`) exhausts → escalate. Reuses `LoopPolicy`/`LoopStatus` at Project/Milestone level rather than a new contract. |
| **Immutable, version-addressed System candidates** (System loop) | Every MLOps/LLMOps eval-gate pattern (LangSmith, Braintrust, generic 5-gate CI) treats the thing under test as a pinned, reproducible version — "what changed" must be answerable after the fact | LOW–MEDIUM | Matches five-loop-model's existing design (`SystemCandidate`: prompt-bundle version, model-routing policy, harness config, evaluator version, benchmark dataset version — all pinned). This is the cheapest, most load-bearing piece of the System loop; build first. |
| **Regression eval suite gates promotion, not a single run** | Industry consensus: a lone eval pass is noise: LLMOps sources describe 5-gate pipelines (lint → offline eval → cost budget → shadow eval → canary+rollback); promotion runs against a *trailing baseline*, not one frozen number | MEDIUM | `internal/eval` (Phase 18 harness) already exists as the seed. Needs: repeated-trial statistics (not N=1), a stored trailing baseline (not a fixed golden number), and a documented promotion threshold. |
| **Rollback is cheap and always available** | Every champion/challenger pattern surveyed treats "keep the champion fully operational" as non-negotiable — sub-60-second rollback is the industry bar for prompt/model swaps | LOW | Directly matches the langgraph-runtime-strategy doc's explicit risk mitigation ("any rung can stop the ladder with the CLI image still fully operational"). For TIDE this is even simpler than live-traffic rollback: per-role `LevelConfig.Image` already resolves per level — "rollback" = flip the image pointer back, no traffic to drain. |
| **Deterministic checks dominate every LLM judge, panel or single** | Structural anti-gaming requirement already adopted by TIDE (`five-loop-model.md`); the wider LLM-judge literature independently confirms judges are gameable (self-preference, verbosity bias, "more convincing not more correct" reward hacking) | LOW (policy) / MEDIUM (enforcement) | Must extend past the single-judge Task loop into panels/tournaments — a panel majority vote still loses to one failing `make test`. |
| **Cost-gated fan-out, default OFF or N=1** | The seed doc (`verify-level-subagent.md`) already flags this; every dynamic-workflow pattern (panel, generate-and-filter, tournament) multiplies pod/Job count directly | LOW (policy) / MEDIUM (enforcement against `ESC-04` caps) | `soundingPolicy.maxShape` (already designed) must bind to the *existing* `executorConcurrency` semaphore and budget reservation rails — this is not new plumbing, it's a ceiling on existing plumbing. |
| **Confidence-based autonomy still fails closed on unparseable/low-confidence output** | HITL literature: "an overconfident model routes too many wrong actions autonomously" is the #1 failure mode named across sources; TIDE's own fail-closed doctrine (unparseable verdict → BLOCKED) must extend to confidence scoring itself | MEDIUM | Oversight's confidence signal is itself a candidate for gaming/miscalibration — treat low/unavailable confidence as "route to human," never as "assume safe." |
| **Per-role (not all-or-nothing) runtime cutover** | Standard MLOps champion/challenger practice migrates one model/segment at a time; TIDE's own ladder design already does this (planners first, executor last) | LOW (already designed) | Confirms the existing design choice rather than adding scope — call out as validated, not net-new. |
| **Paired/shadow comparison on identical input, not live-traffic split** | Because TIDE has no persistent live traffic (Job-per-dispatch, not a request-serving system), the industry's "shadow mode" (mirror every request, discard challenger output) maps to "dual-dispatch the same envelope to both runtimes, discard/quarantine the challenger's artifact" | MEDIUM–HIGH | This is the load-bearing adaptation: TIDE cannot canary a percentage of *traffic* the way a serving stack does — it can only canary a percentage of *dispatches/tasks*. Needs the same "N-way dispatch of one logical node" primitive the Sounding doc calls "the one genuinely new execution mechanism." |

### Differentiators (Competitive Advantage)

Features that go beyond "loop exists" into genuinely distinguishing TIDE from a generic prompt-eval-gate tool or a generic multi-agent framework — these compound with TIDE's derived-DAG, CRD-native architecture rather than bolting a separate control plane on top.

| Feature | Value Proposition | Complexity | Notes |
|---------|-------------------|------------|-------|
| **Escalation cascade for judge cost** (cheap deterministic fast-path → expensive panel only on ambiguity) | Directly matches the ecosystem finding that a well-tuned cheap judge can be 47× faster / 18× cheaper than a multi-agent judge panel at comparable accuracy; naive "always run the panel" is the expensive, low-ROI default | MEDIUM | This is exactly the Sounding doc's `judgeEscalation: onAmbiguous` — validate it against the eval-harness data before defaulting panels ON anywhere. |
| **Bracket/knockout tournament (O(n) comparisons), not round-robin (O(n²))** | Research (Rank/Bracket-style LLM pairwise judging) shows bracket selection scales where pointwise/continuous scoring plateaus past n≈5; this is a direct, cite-able cost-efficiency win over the naive "score everything, pick max" approach | MEDIUM | Confirms the Sounding doc's Tournament archetype cost formula (`N·(1+K)+1`) — the *judging* sub-step inside Tournament should itself be bracket-style, not all-pairs, when K>1 verifiers also compare candidates pairwise. |
| **Diverse judge/generator composition, not N calls to one prompt** | Ensemble literature is explicit: multi-judge value comes from *decorrelated* errors (different models or divergence directives), not from repeating one judge N times, which mostly re-samples the same bias | MEDIUM | Matches the Sounding doc's "Generator is a posture with a divergence directive (risk-first/MVP-first/user-first)" and "Verifier and Judge are separate roles" design calls — validate that panels vary model and/or prompt angle, not just seed. |
| **Confidence calibrated against TIDE's own track record, not the LLM's self-report** | HITL literature's central engineering challenge is exactly this: an LLM's stated confidence is not inherently calibrated; systems that work tie routing to *measured* historical accuracy per risk tier, not the model's self-assessment | MEDIUM–HIGH | Oversight loop's "history" signal (per five-loop-model's `risk + confidence + history`) should be a TIDE-computed rolling pass/verdict rate (from `internal/eval` + Task-loop verdicts), with the LLM's self-reported confidence as one input, never the sole one. |
| **Autonomy earned/lost dynamically per level+repo+risk-tier** | "Track record modulates autonomy over time" is named across multiple HITL sources as the maturity marker beyond static human-in-the-loop toggles | HIGH | This is the deferred `LoopPolicy.Autonomy` field finally being consumed with real inputs rather than a static config value — biggest net-new Oversight surface. Start with a documented heuristic formula (not ML) per the Sounding doc's staged-maturity precedent ("cheap-deterministic → richer signals → ML/judge"). |
| **Shared "N-way dispatch of one logical node" primitive reused by BOTH (d) runtime migration's shadow-pair AND (e) generate-and-filter/tournament's fan-out** | Building this once as core infra (rather than twice, bespoke) is the single highest-leverage structural decision in this milestone | HIGH | Today "1 CRD = 1 Job." Both capabilities need N sibling Jobs from one node + a reduce/compare step. Build it as the Sounding doc's "wave-internal sub-scheduler behind Kahn" ONE TIME; the runtime-migration shadow-pair is degenerate N=2 fixed-candidates (CLI vs LangGraph) of the same mechanism generate-and-filter uses for N LLM-generated candidates. |
| **System loop feeds Oversight's confidence/history signal; Product loop's re-plan feeds System loop's regression corpus** | Cross-loop data reuse — a re-plan triggered by the Product loop is itself a data point ("did the original plan under-deliver") the System loop can eval against; Oversight's autonomy decisions become training signal for track record | HIGH | Emergent value from loop composition, not a standalone build — call out explicitly in phase sequencing so later loops are designed to consume earlier loops' artifacts, not just their own. |

### Anti-Features (Commonly Requested, Often Problematic)

Patterns that look attractive from the ecosystem literature but are wrong for TIDE's specific constraints (CRD-status-only, no live traffic, derived DAG, cycles-are-bugs, cost-bounded).

| Feature | Why Requested | Why Problematic | Alternative |
|---------|---------------|------------------|-------------|
| **Live-traffic percentage canary (route 5–10% of "traffic" to the challenger runtime)** | It's the default MLOps/LLMOps pattern for model rollout (canary deployment, statistically rigorous online experiments) | TIDE has no persistent request-serving traffic to split — every dispatch is a discrete, already-budgeted Job. Forcing a "percentage of traffic" model onto a batch-dispatch system either fakes it badly or requires inventing traffic that doesn't exist | Sample a percentage of **dispatches** (per-Task, per-Plan) for shadow-pair comparison instead of traffic percentage; the unit of canary is "1 in N Jobs," not "1 in N requests" |
| **A permanently-running Product-loop "backlog daemon" ingesting external signals (GitHub issues, prod alerts, user feedback) continuously** | This is the full five-loop-model vision and matches how GitHub Agentic Workflows / Continuous AI triage bots work in the wider ecosystem | Explicitly rejected by TIDE's own design ("a current Project is an outcome-bound campaign, not a permanently-running backlog daemon — keep it that way"); it's also a large net-new surface (external signal ingestion, dedup, durable `Signal` records) disproportionate to "3× v1.0.9 scope" already being a stretch | Ship the narrow slice: outcome-judge-at-boundary closing into re-planning for a *bounded* Project/Milestone. Defer full external-signal ingestion to a future milestone — it's a genuinely separate build (new ingestion surfaces, new CRDs, new auth/trust boundary for external input) |
| **Full ML-learned confidence/autonomy model for Oversight in this milestone** | "Adaptive autonomy" sounds like it wants a trained classifier from day one | The Sounding doc's own staged-maturity ladder puts "ML classifier" at Milestone 5+ for the sibling Sounding capability — jumping straight to a learned model for Oversight autonomy skips the cheap-deterministic-fast-path validation step and produces an unauditable, hard-to-debug autonomy dial exactly where operators need the MOST trust | Ship a documented heuristic formula (risk tier × confidence bucket × rolling pass-rate) that is human-readable and overridable; earn the right to learn a model later with real track-record data this milestone generates |
| **Round-robin all-pairs tournament (every candidate judged against every other candidate)** | It's the naive, obvious implementation of "tournament" | O(n²) judge dispatches — at N=4 generators that's 6 pairwise judge calls before any merge step; cost scales quadratically exactly where the seed doc already flags tournament as "the cost-multiplier tier, last and config-gated" | Bracket/knockout selection: O(n) comparisons, matches the Sounding doc's `Tournament` archetype cost formula and the ecosystem's bracket-ranking research finding |
| **Letting the Oversight loop adjust its own autonomy policy autonomously** | Tempting once History/track-record data exists — "the system already knows it's been reliable, let it self-promote" | Directly contradicts the five-loop-model's explicit rule: Oversight is "Not another autonomous optimizer" — autonomy/kill-switch is human-owned. Self-granted trust is also the textbook LLM-judge reward-hacking failure mode applied to policy instead of output | Oversight *computes and surfaces* a recommended autonomy adjustment (with the track-record evidence attached); a human approves the policy change, same gate machinery as everything else |
| **Treating an eval-suite pass as sufficient proof for the executor-rung runtime migration (the hardest parity bet)** | The eval harness already exists and "just run it" is the path of least resistance | The langgraph-runtime-strategy doc already flags this: the harness measures template-prompt output quality; **agent-loop quality** (tool-use efficiency, edit correctness, recovery from tool errors) is a different axis the current harness doesn't cover. Promoting the executor on eval-suite-green alone risks a false-positive promotion on the highest-blast-radius rung | Add agent-loop-specific eval dimensions (tool-call efficiency, diff correctness, no-op/thrash detection) BEFORE gating the executor rung — this is the doc's own stated risk, worth surfacing as a hard phase dependency, not an assumption |
| **Building generate-and-filter/tournament and the full Sounding classifier as one phase** | They share vocabulary (both are "dynamic workflow shapes") so it's tempting to build the shape-classifier and the shapes together | The Sounding doc explicitly stages this: Milestone 1 = contract + deterministic fast-path (no LLM judge), Milestone 2 = judge subagent + settle, Milestone 3 = the fan-and-reduce execution mechanism + richer shapes. Collapsing these loses the "prove the mechanism deterministically first" discipline that kept every other TIDE loop honest | Sequence per the Sounding doc's own staged ladder — this milestone's dynamic-workflow slice can hand-configure shapes (`soundingPolicy` overrides, no judge) rather than requiring the classifier to exist first |

## Feature Dependencies

```
[System loop: candidate versioning + eval-gated promotion]
    └──requires──> [Phase-18 eval harness, extended with repeated-trial statistics + trailing baseline]
                       └──requires──> [internal/eval (already exists)]

[Evidence-gated runtime migration, planner rungs]
    └──requires──> [System loop's promotion/rollback machinery]  (shares the SAME candidate/eval/promote contract — build System loop generically, runtime migration is its first real workload)

[Evidence-gated runtime migration, executor rung]
    └──requires──> [Evidence-gated runtime migration, planner rungs succeeding]  (ladder is sequential by design)
    └──requires──> [Agent-loop-specific eval dimensions added to the harness]  (NOT satisfied by today's harness — flagged anti-feature above)

[Product loop: outcome-judge-at-boundary]
    └──requires──> [LoopPolicy/LoopStatus contract at Project/Milestone level]  (already exists from v1.0.9; extend escalation path to attach a re-plan proposal, not just halt)
    └──enhances──> [System loop's regression corpus]  (a triggered re-plan is itself eval-relevant history)

[Oversight loop: risk+confidence+history gate resolution]
    └──requires──> [Task loop's verdict/finding history]  (already exists from v1.0.9 — the confidence/history INPUT)
    └──requires──> [System loop's eval track record]  (candidate promotion history feeds "history" signal)
    └──enhances──> [every other loop's escalation policy]  (Oversight resolves onExhaustion/autonomy for ALL loops, not a standalone surface)

[Adversarial verification (Judge panel)]
    └──requires──> [N-way dispatch of one logical node primitive]  (K parallel verifier Jobs + vote-reduce)
    └──requires──> [Composite evaluator type on TaskSpec.verification]  (extends `evaluator: {type: deterministic}` → `{type: panel, judges: [...]}`, flagged as future work in five-loop-model.md)

[Generate-and-filter (Fan-and-merge)]
    └──requires──> [N-way dispatch of one logical node primitive]  (SAME primitive as above — build once)
    └──requires──> [Merger/Synthesizer role]  (new template class)

[Tournament]
    └──requires──> [Generate-and-filter]  (candidate generation stage)
    └──requires──> [Adversarial verification]  (judging stage)
    └──requires──> [Bracket/knockout comparison logic]  (O(n), not O(n²) — cost-control requirement, not optional)

[Evidence-gated runtime migration's shadow-pair comparison]
    └──shares-mechanism-with──> [Generate-and-filter's N-way dispatch]  (degenerate N=2, fixed-candidate case of the same primitive — build the primitive generically once, not twice)

[Full continuous Product loop (external signal ingestion)]
    └──deferred-beyond──> [v1.0.10's outcome-judge-at-boundary slice]  (explicitly out of this milestone's scope per five-loop-model.md's own "not a backlog daemon" rule)

[Full Sounding ML classifier]
    └──deferred-beyond──> [v1.0.10's hand-configured dynamic-workflow slice]  (Sounding doc's own Milestone 5+ staging)
```

### Dependency Notes

- **Runtime migration (d) requires System loop (b), not the reverse.** The langgraph-runtime-strategy doc frames each migration rung as "gated on eval-harness evidence," which IS the System loop's promotion mechanic. Building System loop's candidate-versioning + eval-gate-promotion machinery generically (not migration-specific) means the runtime ladder becomes System loop's first consumer rather than a parallel, duplicate mechanism. Sequence System loop's core contract before or alongside the first migration rung, not after.
- **The executor rung is dependency-gated on a harness capability that doesn't exist yet.** This is the single highest-risk dependency in the whole milestone — the langgraph doc names it as an open risk ("may need new eval dimensions before the executor rung"). Treat "agent-loop eval dimensions" as a hard phase deliverable before scheduling the executor migration, not an assumption that falls out of the planner rungs.
- **Oversight (c) is a consumer of Task-loop and System-loop history, not a standalone data source.** Sequencing Oversight before enough Task/System loop history exists produces an autonomy resolver with nothing real to compute from — it would fall back to static config, which is what already exists. Oversight's differentiating value (dynamic, track-record-driven autonomy) is only real once there's a few cycles of real verdict/promotion history to compute over.
- **Generate-and-filter (e) enhances Product loop (a).** "Diverse approaches help" (Fan-and-merge) is exactly the right shape for the corrective-re-plan step of the Product loop's outcome-judge escalation — instead of one re-plan proposal, N re-plan candidates judged and merged. Not a hard requirement, but a natural composition once both exist; don't build them as unrelated features.
- **Shared N-way dispatch primitive is the one item that, if under-scoped, forces rework.** Both (d)'s shadow-pair and (e)'s fan-out are the same "1 node → N sibling Jobs → reduce" mechanism the Sounding doc calls the biggest net-new build item. Scope it once, generically (N and reduce-strategy as parameters), even if the milestone only exercises N=2 (shadow-pair) and small N (panel/fan-and-merge) — building it narrowly for shadow-pair only would mean re-building it for generate-and-filter.

## MVP Definition

### Launch With (v1.0.10 core slice)

The minimum that makes all five capabilities *real* (five-element loops, not renamed pipeline stages) without redoing the full five-loop-model's long-run vision in one milestone.

- [ ] **Evidence-gated planner-rung migration** (through the langgraph ladder) — proves (d) with the LOWEST-risk shape: planners are already 1-CRD=1-Job, so no new execution primitive is needed yet, just candidate-versioning + eval-gate-promotion (which the System loop MVP below provides generically)
- [ ] **System loop MVP: candidate versioning + eval-gated promotion/rollback** — `internal/eval` grows a `SystemCandidate`/promotion-decision surface with a trailing baseline and repeated-trial statistics; this is what the runtime migration ladder consumes, so it must exist before/alongside the first gated rung
- [ ] **Product loop MVP: outcome-judge-at-Project/Milestone-boundary, bounded re-plan** — narrow slice only (NOT the continuous external-signal backlog daemon); reuses `LoopPolicy` at Project/Milestone level, extends the escalation path to optionally author a corrective Phase/Milestone
- [ ] **Oversight loop MVP: documented heuristic autonomy resolver (risk tier × confidence bucket × rolling pass-rate)** — consumes Task-loop verdict history (already exists) + System-loop promotion history; explicitly NOT an ML model this milestone
- [ ] **Adversarial verification MVP: Judge-panel evaluator type on the existing `TaskSpec.verification` contract** (K≥2 independent verifiers, vote-reduce, deterministic checks still dominate) — the smallest possible slice of the shared N-way dispatch primitive (K is typically 2–3, not N=generators-scale)
- [ ] **CLI-deprecation decision point reached** for whichever rungs completed this milestone (the decision itself, not necessarily full deprecation execution) — this is the ladder's own stated "decision point, not a day-one commitment"

### Add After Validation (later in v1.0.10 or a fast-follow)

- [ ] **Generate-and-filter (Fan-and-merge) at the plan seam** — trigger: the shared N-way dispatch primitive is proven by the planner-rung shadow-pair + Judge-panel work above; extending it to N generators is incremental once the mechanism exists
- [ ] **Executor-rung runtime migration** — trigger: agent-loop-specific eval dimensions exist in the harness (hard dependency, not automatic); this is deliberately last per the ladder's own risk framing
- [ ] **Tournament pattern** (composes generate-and-filter + judge-panel + bracket/knockout merge) — trigger: both component patterns are live and the fan-out cost is proven bounded by the concurrency/budget rails in real dispatches, not just design
- [ ] **Dynamic autonomy adjustments proposed by Oversight, human-approved** (vs. the MVP's static heuristic formula) — trigger: enough track-record cycles exist to make "recommend an adjustment" meaningful rather than noise

### Future Consideration (v2+ / beyond this milestone)

- [ ] **Full continuous Product loop with external signal ingestion** (GitHub issues, prod monitoring, user feedback as durable `Signal` records feeding an ongoing backlog) — defer: this is a genuinely separate build (new ingestion surfaces, trust boundary for external/untrusted input, dedup/prioritization logic) disproportionate to fitting inside an already-3×-scoped milestone
- [ ] **ML-learned confidence/autonomy model** — defer until the MVP heuristic has produced enough labeled track-record data to train/validate against; matches the Sounding doc's own Milestone-5+ staging for the sibling classifier problem
- [ ] **Full Sounding classifier (semantic per-node shape resolution, judge-driven)** — defer per the Sounding doc's own staged roadmap; this milestone should hand-configure shapes via `soundingPolicy`, not build the classifier that picks them automatically
- [ ] **Node-internal cyclic postures** (debate/blackboard/tree-search protocols running inside one Job) — defer until the LangGraph runtime image is mature enough to host them safely behind the envelope seam (Sounding doc §③'s own precondition)
- [ ] **Multi-provider `init_chat_model` endgame** — sequenced explicitly AFTER all authoring rungs prove out per the ladder; don't pull it forward just because it's mechanically simple (`init_chat_model` is a known, confirmed capability) — the endgame's value depends on the ladder being complete, not on the mechanism being available early

## Feature Prioritization Matrix

| Feature | User Value | Implementation Cost | Priority |
|---------|------------|----------------------|----------|
| System loop candidate versioning + eval-gated promotion | HIGH | MEDIUM | P1 |
| Evidence-gated planner-rung migration | HIGH | MEDIUM | P1 |
| Product loop outcome-judge-at-boundary (narrow slice) | HIGH | MEDIUM | P1 |
| Oversight loop heuristic autonomy resolver | HIGH | MEDIUM–HIGH | P1 |
| Adversarial verification (Judge panel evaluator type) | MEDIUM–HIGH | MEDIUM | P1 |
| N-way dispatch primitive (shared infra) | HIGH (unlocks P1/P2 items) | HIGH | P1 (build once, early) |
| CLI-deprecation decision point | MEDIUM | LOW (decision, not build) | P2 |
| Generate-and-filter (Fan-and-merge) | MEDIUM | MEDIUM–HIGH | P2 |
| Executor-rung runtime migration | HIGH | HIGH (+ blocked on new eval dims) | P2 |
| Agent-loop eval dimensions (harness extension) | HIGH (blocks executor rung) | MEDIUM | P2 (must precede executor rung) |
| Tournament pattern | MEDIUM | HIGH | P3 |
| Dynamic (learned) autonomy adjustment | MEDIUM | HIGH | P3 |
| Full continuous Product loop (external signals) | MEDIUM (long-run) | VERY HIGH | P3 (future milestone) |
| Full Sounding ML classifier | LOW (this milestone) / HIGH (long-run) | VERY HIGH | P3 (future milestone) |

**Priority key:**
- P1: Must have — proves each of the five capabilities is a real, bounded loop (or correctly-flagged pipeline stage) this milestone
- P2: Should have — extends P1 items to their next natural rung once the underlying mechanism is proven
- P3: Nice to have / explicitly future — named in the five-loop-model/Sounding docs' own staged ladders as later maturity steps

## Comparable Systems Analysis

Not a consumer-product competitor set — the comparison is against how the wider MLOps/LLMOps/agentic-orchestration ecosystem builds each capability, and how TIDE's CRD-native, no-live-traffic, derived-DAG constraints force a different shape.

| Capability | How LangSmith/Braintrust-style LLMOps do it | How MLOps (classic champion/challenger) does it | TIDE's approach |
|---------|--------------|--------------|--------------|
| Eval-gated promotion | 5-gate CI pipeline (lint → offline eval → cost budget → shadow eval on production traces → canary + auto-rollback); promotion runs against a trailing 7-day baseline | Champion/challenger with agreement-rate, score-correlation, distribution-comparison (KS test) metrics before canary | Same trailing-baseline + repeated-trial discipline, but "shadow eval on production traces" becomes "shadow-pair dispatch on identical Task envelopes" — no live traffic to sample from |
| Runtime/model migration | Gradual traffic-percentage canary with real-time quality/cost/safety monitoring | Staged: offline validation → shadow deployment (100% mirrored, 0% user-facing) → canary (5–10%) → full statistical online experiment | Per-role (not per-request) staged cutover — the "unit of canary" is a dispatch role (planner/executor), matching the ladder's own planner-first/executor-last design, not a traffic percentage |
| Adaptive autonomy / HITL | Confidence-based approval routing: above-threshold auto-proceeds, below-threshold queues for human review; calibration is the named hard problem | N/A (not typically an MLOps concern — more of an agentic-ops concern) | Matches the pattern closely, but must fold in TIDE's existing fail-closed doctrine (unparseable/low-confidence → escalate, never assume-safe) and the deterministic-dominates rule (a failing gate command overrides ANY confidence score) |
| Multi-judge verification | Ensemble evaluation increasingly named as one of the "next 18 months" architectural moves; cascade (cheap judge first, escalate) beats flat multi-agent judge on accuracy-per-dollar | N/A | Matches — the Sounding doc's `judgeEscalation: onAmbiguous` is already this cascade pattern; needs diverse (not identical) judges to avoid correlated-error ensembles per the research finding |
| Tournament/best-of-N selection | Bracket/knockout pairwise-judging (O(n)) outperforms pointwise scoring past n≈5 in cited research | N/A | Matches the Sounding doc's Tournament archetype; the judging sub-step within Tournament should itself be bracket-style when comparing >2 candidates, not all-pairs |
| Backlog/product-signal ingestion | GitHub Agentic Workflows / "Continuous AI": autonomous triage loops label/route issues continuously, PRs never auto-merge, humans stay in the broader loop via reports/issues/PRs | N/A | TIDE explicitly scopes this DOWN this milestone — the full continuous-ingestion pattern is deferred; v1.0.10 ships only the bounded outcome-judge-at-boundary slice, deliberately narrower than the GitHub Agentic Workflows model |

## Sources

- Arize, "What is a loop in AI engineering, anyway?" (arize.com/blog/what-is-a-loop-in-ai-engineering-anyway) — cited as the five-loop-model's own foundational source; not re-fetched here, treated as HIGH confidence via the existing internal doc's citation
- LLMOps CI/CD, eval gates & prompt versioning: [myengineeringpath.dev/genai-engineer/llmops](https://myengineeringpath.dev/genai-engineer/llmops/), [AppScale — AI-Native CI/CD for LLM Features, 5 Gates](https://appscale.blog/en/blog/ai-native-cicd-for-llm-features-eval-gates-prompt-diff-canary-rollouts-2026), [Braintrust — What is prompt management?](https://www.braintrust.dev/articles/what-is-prompt-management), [Towards AI — LLM Observability with LangSmith Part 2: Eval Gates](https://pub.towardsai.net/llm-observability-with-langsmith-part-2-eval-gates-prompt-versioning-choosing-your-stack-e607473320b5) — MEDIUM confidence, multiple independent sources agree on the 5-gate/trailing-baseline pattern
- Shadow-mode / champion-challenger deployment: [FutureAGI — LLM Eval with Shadow Traffic and Canary Deployment](https://futureagi.com/blog/llm-eval-shadow-traffic-canary-2026/), [CalibreOS — Safe ML Model Rollout: Canary, Shadow Mode, Rollback](https://www.calibreos.com/learn/mlsd-canary-deployment), [Medium — Deployment Evaluation Strategies in MLOps](https://medium.com/@fraidoonomarzai99/deployment-evaluation-strategies-in-mlops-c208585aa3bd), [AWS Builder Center — Model Lifecycle Management](https://builder.aws.com/content/3B2isWvUG5GpbavOQoHed2Q9HAW/your-ai-models-have-an-expiry-date-a-practical-guide-to-model-lifecycle-management) — MEDIUM-HIGH confidence, classic MLOps pattern well-documented across sources; TIDE's no-live-traffic adaptation is a novel synthesis (LOW confidence on that specific mapping, flagged above)
- Adaptive autonomy / HITL: [Illumination Works — Balancing AI Autonomy & Human Oversight with Adaptive HITL](https://ilwllc.com/2025/12/balancing-ai-autonomy-human-oversight-with-adaptive-human-in-the-loop/), [Grizzly Peak / myengineeringpath — HITL Patterns for AI Agents](https://myengineeringpath.dev/genai-engineer/human-in-the-loop/), [ideaforgestudios — How Much Autonomy Should You Give Your AI Agents?](https://ideaforgestudios.com/2026/07/17/human-in-the-loop-ai-agents-autonomy-playbook/), [ByteBridge — From Human-in-the-Loop to Human-on-the-Loop](https://bytebridge.medium.com/from-human-in-the-loop-to-human-on-the-loop-evolving-ai-agent-autonomy-c0ae62c3bf91) — MEDIUM confidence, consistent pattern (confidence-threshold routing, calibration as the hard problem, track-record-modulated autonomy) across independent sources
- Multi-judge / adversarial verification / ensemble cost tradeoffs: [Galileo — Scaling Judge Compute: The Next Frontier in AI Evaluation](https://galileo.ai/blog/scaling-judge-compute-ai-evaluation), [orq.ai — Weak judges, strong panel: an ensemble approach to LLM eval](https://orq.ai/blog/llm-juries-in-practice), [ScopeJudge — Cost-Aware Pre-Execution Gating (arXiv)](https://arxiv.org/pdf/2607.07774), [Zylos Research — LLM-as-Judge in Production](https://zylos.ai/research/2026-04-10-llm-as-judge-production-agent-verification-2026/) — MEDIUM confidence; the "structural limitations of LLM judges/reviewer ensembles" finding is corroborated by multiple independent research sources, treated as a real risk not just a hedge
- Best-of-N / tournament / bracket selection cost control: research on pairwise-ranking/bracket methods for LLM-judged candidates (arXiv preprints on proof ranking and inference-time selection, surfaced via WebSearch — titles/URLs not independently re-verified beyond the search snippet; treat the specific "O(n) bracket beats O(n²) round-robin past n≈5" claim as MEDIUM-LOW confidence, single-search-pass sourced) — directionally consistent with, and reinforces, the Sounding doc's own Tournament cost-formula design (`N·(1+K)+1`), which was independently arrived at
- GitHub Agentic Workflows / Continuous AI (backlog/product-signal ingestion pattern for comparison, explicitly NOT adopted at that scope this milestone): [GitHub Blog — Automate repository tasks with GitHub Agentic Workflows](https://github.blog/ai-and-ml/automate-repository-tasks-with-github-agentic-workflows/), [GitHub — Meet the Workflows: Issue Triage](https://github.github.com/gh-aw/blog/2026-01-13-meet-the-workflows/) — MEDIUM confidence, official GitHub sources
- Internal design docs (primary source for TIDE-specific mechanics, not re-derived, only grounded against the above): `.planning/notes/five-loop-model.md`, `.planning/notes/langgraph-successor-runtime-strategy.md`, `.planning/notes/sounding-dynamic-orchestration-design.md`, `.planning/seeds/verify-level-subagent.md`, `.planning/PROJECT.md`

---
*Feature research for: TIDE v1.0.10 "King Tide" — Five Loops, One Successor Runtime, Dynamic Workflows*
*Researched: 2026-07-21*
