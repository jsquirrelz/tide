# Pitfalls Research

**Domain:** Adding a successor-runtime migration + four new feedback loops (Product/System/Oversight join Execution/Task) + adversarial fan-out patterns (panels/tournaments/generate-and-filter) to an existing production K8s LLM orchestrator that already runs fail-closed, budget-capped, evidence-gated.
**Researched:** 2026-07-21
**Confidence:** MEDIUM-HIGH — TIDE-specific findings (cited incidents, decision records) are HIGH confidence (sourced from `.planning/PROJECT.md`, `.planning/notes/five-loop-model.md`, `.planning/notes/langgraph-successor-runtime-strategy.md`, `.planning/notes/sounding-dynamic-orchestration-design.md`). External ecosystem findings (LLM-judge bias, reward hacking, multi-agent debate cost, autonomy trust calibration, multi-provider structured output) are MEDIUM confidence — WebSearch-verified across multiple independent sources but not Context7/official-doc-tier, since this is a systems-design/research-literature question rather than a library-API question.

## Critical Pitfalls

### Pitfall 1: Evidence-gating measures the wrong thing at the hardest rung

**What goes wrong:**
The Phase-18 eval harness measures template-prompt output quality (structured child-CRD correctness, planning fidelity) — a good fit for planner rungs, where "structured output is a native win." The task-executor rung is the hardest parity bet (Claude Code's agent loop and file tools are battle-tested; hand-authored LangGraph tools start from zero), and it needs eval dimensions the harness doesn't have: tool-call efficiency, edit correctness against a golden diff, recovery-from-tool-error rate. Gating the executor rung on a suite built for planner output lets a worse coding agent pass because it "looks right" on planner-shaped metrics.

**Why it happens:**
This project's own strategy note names the gap without resolving it: "the eval harness measures template-prompt output quality; agent-loop quality... may need new eval dimensions before the executor rung." Reusing an existing, working eval harness across a fundamentally different workload is the path of least resistance.

**How to avoid:**
Before gating the executor rung, add agent-loop-specific dimensions to `internal/eval` (tool-call efficiency, diff-correctness, tool-error-recovery rate). Treat "matches CLI baseline on planner-shaped metrics" as necessary but not sufficient for executor promotion — require a distinct pass/fail line for the new dimensions.

**Warning signs:**
An executor-rung image passes the eval gate using a task set authored/reused from planner-rung evals; the eval report has no process/tool-use signal, only end-state diff quality.

**Phase to address:**
Early — before the executor rung's gate is defined, ideally at ladder-design time (the phase that sets up per-rung eval criteria), not discovered after the rung ships.

---

### Pitfall 2: CLI/SDK parity blind spot — incidental CLI behavior nothing measures

**What goes wrong:**
The CLI subagent carries incidental behaviors (system-prompt scaffold, `--add-dir` CWD injection, a per-request billing nonce, `--bare` stripping `settings.json`) that a hand-built LangGraph image won't replicate automatically. A migration rung can pass evals built on CLI-shaped expectations while silently losing or changing behavior no eval measured — cache economics, exact tool surface, working-directory-based scoping.

**Why it happens:**
This project already hit exactly this class: the CACHE-01 spike found the CLI silently front-loads a per-request-random `cch` billing nonce ahead of caller content, invisible from eval scores and only found by teeing the raw credproxy request bodies across two dispatches. Eval scores compare outputs; they don't compare the request/response wire format underneath.

**How to avoid:**
For each migration rung, diff-inspect raw request/response bodies between the CLI and LangGraph paths at least once (the CACHE-01 method), not just eval scores. Keep a documented parity checklist per rung: tool surface, system-prompt content, credential handling, budget/cost accounting — not just "eval score ≥ baseline."

**Warning signs:**
Eval score is comparable but cost-per-task or token count diverges unexplained after a rung ships; the pricing table shows unexpected model/provider rows post-migration.

**Phase to address:**
Per-rung gate — every authoring-migration rung, not a one-time check at the end of the ladder.

---

### Pitfall 3: "Wired but never ran the shipped path" — this project's recurring defect class

**What goes wrong:**
TIDE has repeatedly shipped code that compiles and passes green tests while the real production path was never exercised end-to-end: Phase 22's dashboard image embedded a stale pre-telemetry SPA (frozen `embed/dist`, release never rebuilt the frontend); Phase 51's verdict-relay ship-blocker (verifier wrote a `TerminationStub`, the controller parsed a different field, `Verdict` was always nil — found only by a live billable proof run, not by green tests); Phase 52's DEFECT-B/C (attempt-blind reporter Job name causing a re-plan dead-stall; a swallowed operator approval of an exhausted plan-check loop) — both surfaced only by live proof, not by envtest/kind green.

**Why it happens:**
envtest/kind fixtures mock or bypass the exact data flow that only fires when a real billable run drives a real escalation/re-attempt/promotion cycle end to end through the artifact/envelope contract. Unit and integration tests validate reconciler logic in isolation, not the full wire path under real conditions.

**How to avoid:**
Every phase that introduces a NEW loop-closing path (a Product-loop re-plan cycle, a System-loop promotion, an Oversight autonomy escalation, a fan-out pattern's reduce/merge step) requires a live billable proof run — not just green CI — before being declared done, exercising that specific new path at least once end-to-end on a real cluster with a real LLM call. This is already this project's own doctrine (Phase 51/52 precedent); carry it forward explicitly for every new loop, not only Task.

**Warning signs:**
"All envtest/kind green" is treated as sufficient without a live-proof artifact in the phase's verification record; a new escalation/promotion/gate path has zero commits or logs showing it actually fired in a real run.

**Phase to address:**
Final conformance / verification for every phase that introduces a new loop-closing path — not deferred to milestone close.

---

### Pitfall 4: Shadow-mode evidence-gathering spend is invisible to the budget guardrail

**What goes wrong:**
Comparing the LangGraph image against the CLI baseline (shadow mode) doubles LLM spend per compared task — on a system whose own incident history (run-1's ~$80 dry-out across two credit exhaustions, run-2b's OOM from underestimated fan-out) came from underestimated cost. The eval harness historically runs outside the Project/`BudgetCents` reservation flow (`make eval` stands up its own credproxy+harness, not a `Project` CRD), so shadow-mode comparison spend across N rungs × M comparison tasks is invisible to the guardrail that stops production runs.

**Why it happens:**
`ReservationStore`/`BudgetCents` accounting is scoped to Project-driven dispatch; eval-harness runs were built as a separate, ungated recipe for a different purpose (regression fixtures), and nobody re-scoped it when it became a live-API-spending gate for ladder promotion.

**How to avoid:**
Budget eval/shadow-mode spend explicitly as its own line item before a ladder rung starts, not implicitly via "run `make eval`." Cap total shadow-mode dollar spend per rung. Size eval task sets to the specific comparison question rather than full-milestone-scale reruns.

**Warning signs:**
Eval runs against a real API key proceed without a pre-declared budget; nobody totals dollars spent across ladder rungs; the eval harness has no cap analogous to `BudgetCents`.

**Phase to address:**
Ladder-design / early schema phase — declare the eval budget model before Rung 1 starts.

---

### Pitfall 5: Product loop re-planning discards paid work

**What goes wrong:**
The loop contract already encodes the fix for the Task loop's neighbors (Phase/Milestone/Project `maxIterations:0`, escalate-only, "because post-execution rework discards paid work") — but the Product loop is a NEW loop above finite Projects, with a worse natural failure mode: an outcome-level judge deciding the artifact tree doesn't match the outcome prompt could trigger a full re-plan that discards multiple already-executed, already-paid Phases, not just one Task's attempt.

**Why it happens:**
The five-loop model states Product "operates through Projects, never modifies Tasks directly," but doesn't yet distinguish "re-plan" meaning a brand-new, additive Project (cheap) from mutating/superseding an in-flight one (destructive of sunk cost).

**How to avoid:**
Product-loop re-planning must always be additive — spawn a new, scoped-down finite Project for the gap — never destructive (never cancel/rewrite an in-flight Project's completed Phases). Apply the same "escalate, don't auto-repeat" discipline one level up: outcome-judge failure escalates to human-approved new-Project creation; it does not auto-loop.

**Warning signs:**
A Product-loop design that lets the judge directly PATCH or cancel Phases/Plans of an existing Project; no CRD-level boundary preventing Product from writing to Task-level specs.

**Phase to address:**
Product loop schema phase — the CRD boundary is the prevention, must exist before controller logic.

---

### Pitfall 6: Judge drift and unbounded refinement in the Product loop's outcome judge

**What goes wrong:**
An LLM judge scoring "does this artifact tree satisfy the outcome prompt" exhibits documented position/verbosity/self-preference bias and drifts across model version bumps — a judge re-run after a model upgrade can flip a previously-approved outcome to rejected (or vice versa) for identical content. A loop with no iteration ceiling plus a drifting judge can thrash rather than converge.

**Why it happens:**
Position bias, verbosity bias (favoring longer diffs/more artifacts), self-preference bias, and version-drift (scores shift for identical content on judge-model upgrade) are well-documented LLM-as-judge failure modes; an outcome-level judge over a whole artifact tree is exactly the highest-surface-area target for them.

**How to avoid:**
Give the Product loop a bounded iteration cap (mirrors the Task loop's `maxIterations`) even though it is outcome-level, not infinite refinement. Pin/version the judge model per Project (record model+prompt version alongside the verdict, same discipline as System-loop candidate versioning) so a judge upgrade doesn't retroactively re-litigate a closed Project. A deterministic check (declared acceptance-criteria commands) should dominate the LLM judge — same rule as the Task loop: "a deterministic failure dominates an LLM judge's approval."

**Warning signs:**
Two runs of the outcome judge against an identical artifact tree produce different verdicts; the Product loop's `LoopPolicy` instantiation has no `MaxIterations` analog.

**Phase to address:**
Product loop's `LoopPolicy` parameterization phase (mirrors Phase 52's per-level resolver work).

---

### Pitfall 7: System loop gaming its own evals / promotion without rollback

**What goes wrong:**
`internal/eval` growing into a real promotion gate creates a textbook reward-hacking setup: if the same change can touch both a `SystemCandidate` (prompt/harness/evaluator version) AND the eval suite that gates its promotion, the system can "pass" by narrowing the eval rather than improving the candidate — collapsing the actor separation that made the Task loop's anti-gaming rule meaningful.

**Why it happens:**
This project's own Task-loop anti-gaming precedent names the exact risk in a neighboring context: "evaluator independence is structural (read-only mounts + credential omission), never prompt-enforced; anti-gaming = fixture/evaluator edits are system escalations." The System loop is uniquely exposed because, unlike Task (two different actors: implementation agent vs. independent verifier), a SystemExperiment can bundle candidate AND evaluator/benchmark changes in one commit.

**How to avoid:**
Carry the structural-independence rule up a level: (a) candidate and evaluator versioning stay separate, immutable, version-addressed artifacts (already scoped — every run records exact prompt/model/harness/evaluator/policy versions); (b) a SystemExperiment that changes both the candidate AND the evaluator/benchmark in the same version bump is a hard-block or requires elevated human approval — mirroring the already-committed loop-engineering template rule "don't change candidate+evaluator together"; (c) promotion needs an explicit, tested rollback path (keep the N-1 candidate pinned, auto-revert on post-promotion regression detection), not a one-way ratchet.

**Warning signs:**
A single PR/commit touches both `internal/eval` fixtures/benchmarks and the prompt template/harness under evaluation; no documented rollback procedure exists for a promoted `SystemCandidate`; promotion history never shows a reversion (suspicious rather than reassuring).

**Phase to address:**
System loop's evidence/promotion-policy phase — this is schema/policy work on top of the existing Phase-18 eval seed, not new infra; add candidate/evaluator separation + rollback fields before wiring the promotion controller.

---

### Pitfall 8: Eval overfitting — the System loop optimizes the metric, not the product

**What goes wrong:**
Repeated eval-gated iteration on a fixed benchmark dataset (the Phase-18 task-completion/cost/pass-rate suite) will, over enough `SystemExperiment` cycles, optimize prompts/harness to that dataset's specific blind spots rather than general coding quality (Goodhart's Law) — and the same fixed dataset is now under double pressure, reused both for ladder-rung gating (Pitfall 1) and System-loop promotion.

**Why it happens:**
Any fixed eval suite reused as a continuous optimization target degrades as a measure of true quality the more it's optimized against — a well-documented pattern in reward-hacking and eval-gaming literature, and structurally inevitable once the same benchmark is the target of two separate optimization loops.

**How to avoid:**
Version and periodically rotate/expand the benchmark dataset (own version field, like `SystemCandidate`s are versioned). Hold out a subset never used for promotion decisions, only for periodic health-checks. Track variance across repeated runs (already a listed System-loop eval dimension) as an overfitting smell — decreasing variance with flat real-world outcomes is a red flag, not a win.

**Warning signs:**
Eval pass-rate climbs every cycle but human-review burden or dogfood-run defect rate doesn't improve in parallel; the benchmark dataset version hasn't changed since Phase 18.

**Phase to address:**
System loop phase, dataset-versioning sub-task — schema decision (dataset carries its own version) made early, held-out-set policy set before the first promotion.

---

### Pitfall 9: Oversight loop ratchets autonomy up on thin evidence (confidence miscalibration + history poisoning)

**What goes wrong:**
Resolving gate policy from "level + risk + confidence + history" is a new failure surface this project hasn't faced yet: a short run of lucky approvals could ratchet a `LoopPolicy.Autonomy` level up (fewer human gates) for a risk class that isn't actually proven safe. Once autonomy is up, fewer human touchpoints mean fewer future data points to detect regression — history poisoning: early good outcomes bias the trust signal that governs whether later, worse outcomes even get reviewed.

**Why it happens:**
This maps directly to documented automation-complacency and trust-miscalibration research: "the more reliable a system appears, the less vigilant its human overseers become," and supervisors reviewing AI decisions at high volume develop a pattern of approval that mirrors the AI's outputs — rubber-stamping that hides the very regressions the oversight mechanism exists to catch.

**How to avoid:**
Require a minimum sample size AND a minimum time window before autonomy escalation, not just N consecutive successes (5 successes in 5 minutes is materially weaker evidence than 5 successes over 2 weeks across varied risk profiles). Make autonomy monotonic-down-fast / monotonic-up-slow: one bad outcome should be able to instantly drop autonomy (fail closed), but raising it requires sustained evidence and, per the five-loop model's own rule, stays human-owned — "autonomy/kill-switch is human-owned at the Oversight layer... not another autonomous optimizer" — never fully automatic. Build in mandatory random-sample audits of auto-approved decisions (not only near-threshold ones) to counter supervision fatigue.

**Warning signs:**
An autonomy level for a risk class only ever goes up in the history log, never down; the confidence score feeding the resolver is itself LLM-judge-derived (same drift/bias risk as Pitfall 6) rather than grounded in deterministic outcome signals; no random-audit mechanism exists, only threshold-triggered review (which by construction never samples the "confident" bucket).

**Phase to address:**
Oversight loop schema phase — `MinSampleSize`/`MinWindow` and the down-fast/up-slow asymmetry belong in the `AutonomyLevel` resolver's contract from day one, not retrofitted after a live incident.

---

### Pitfall 10: Fan-out patterns multiply pods faster than the existing guardrail accounts for

**What goes wrong:**
The Sounding design's own cost table shows Tournament shape cost = `N·(1+K)+1` Jobs — for N=3 generators, K=2 verifiers, that's already 9 Jobs for ONE node; several such nodes in one wave compound fast. This project has a live incident of exactly this failure class already: dogfood run-2b's D3 defect dispatched ~60 concurrent planner pods and OOM'd a single-node kind cluster, with no per-node fan-out multiplier even involved — Judge-panel/Tournament patterns make the same math structurally worse per node.

**Why it happens:**
`executorConcurrency`/planner-pool semaphores (the Phase 32 fix) cap concurrent `r.Create` calls / in-flight pods GLOBALLY, but a single Tournament-shaped node can burn most of that budget by itself, starving every other concurrent node in the same wave — the existing guardrail caps total concurrency, not per-node burst share.

**How to avoid:**
Enforce `soundingPolicy.maxShape` ceilings (already speced: `maxStageWidth`, `maxTotalDispatch`, `maxIteration`) BEFORE any fan-out pattern ships, not added reactively after an OOM. Additionally cap per-wave AGGREGATE fan-out (sum of all nodes' resolved shapes dispatching concurrently in one wave), since multiple Tournament nodes in the same wave multiply the OOM risk the same way run-2b's flat 60-planner fan-out did. Default single-node/kind-safe ceilings should be conservative (Phase 32's precedent used a default of 4) and must be proven on a real single-node kind cluster before shipping, not just unit-tested with mocked Job creation.

**Warning signs:**
`soundingPolicy.maxShape` exists in the schema but no reconciler path actually reads/enforces it before dispatch (the wired-but-never-ran class again, Pitfall 3); no live single-node kind proof run exists for a Judge-panel/Tournament node, only envtest with mocked Job creation.

**Phase to address:**
Dynamic-workflow fan-out phase(s) — enforce `soundingPolicy` ceilings and the per-wave aggregate cap in the SAME phase that introduces the first fan-out shape. Ship Judge-panel first (cheapest: `1+K` vs. Tournament's `N·(1+K)+1`); require a live single-node kind proof before Tournament ships.

---

### Pitfall 11: Judge collusion / correlation in adversarial panels — false confidence from correlated verifiers

**What goes wrong:**
A Judge-panel or Tournament shape's K verifiers, if all instantiated from the same model/prompt/image, are not independent samples — they share the same blind spots and biases (position/verbosity/self-preference bias, documented above), so "K of K verifiers agree" is weaker evidence than it looks. Worse, if verifiers are cheaper/faster models than the generator (a plausible cost optimization), they may systematically rubber-stamp output they can't actually evaluate.

**Why it happens:**
Multi-agent-debate research shows debate/panel patterns run at roughly 2.5×+ the cost of a single call while sometimes underperforming isolated self-correction — the consensus signal panels are supposed to buy can be illusory when verifiers are correlated. This project's existing anti-gaming precedent (structural evaluator independence) was designed for ONE independent evaluator, not yet extended to a panel of them.

**How to avoid:**
Diversify verifier configuration within a panel (different model/prompt-version/temperature per seat, not K clones of one config) so agreement carries genuinely independent evidence weight. Require a deterministic check to dominate the panel vote — same rule as the Task loop — rather than trusting LLM-vote consensus alone. Track and alert on panel agreement rate: near-100% agreement across many runs is itself a signal of correlation, not confidence, and should trigger a design review, not more trust.

**Warning signs:**
All K verifier seats in a Judge-panel/Tournament resolve to the same model+prompt version; panel vote agreement is ~100% across dozens of runs; no deterministic check gates the panel's vote.

**Phase to address:**
Dynamic-workflow fan-out phase, Judge-panel design sub-step — verifier diversity is a cheap schema/config decision to bake in early, expensive to retrofit after a Tournament ships with cloned verifiers.

---

### Pitfall 12: Multi-provider structured-output / verdict-schema drift

**What goes wrong:**
`init_chat_model`'s single-string provider dispatch hides real mechanism differences: OpenAI's `with_structured_output` defaults to native `json_schema`/function-calling, while Anthropic implements structured output via forced tool-choice — a different code path with different failure modes and documented reliability regressions in the wild. A verdict schema (`gate_decision`, already matched Go↔Pydantic per Phase 49) validated against Anthropic's tool-forcing behavior is not guaranteed to parse identically once `init_chat_model` routes to a different provider — the same class of risk as the CLI/SDK parity gap (Pitfall 2), now living inside the SDK layer across providers instead of CLI-vs-SDK.

**Why it happens:**
This project's fail-closed doctrine ("unparseable/empty verdict → escalate, never APPROVED") is the correct backstop, but silent schema drift that still PARSES — e.g., a provider that omits an optional field OpenAI treats as always-present, or emits an extra JSON key — could pass validation while carrying subtly wrong semantics, undetected by the "unparseable→escalate" check because it technically parses.

**How to avoid:**
Add a provider-tagged conformance test to the existing verdict-schema golden fixture (Phase 49's shared Go↔Pydantic fixture) — one golden verdict validated against EACH supported provider's actual structured-output call, not just Anthropic. Keep the fail-closed rule but add schema-level strictness (reject unknown fields, require all fields present) so drift that parses-but-differs still fails closed instead of silently succeeding.

**Warning signs:**
The verdict golden fixture is only ever exercised against one provider in CI; a provider swap changes verdict field-population rates (e.g., `confidence` populated 100% on Anthropic, 60% on OpenAI) without a test catching it.

**Phase to address:**
Multi-provider endgame phase (the `init_chat_model` rollout) — extend Phase 49's golden-fixture contract test per new provider BEFORE that provider ships, not after.

---

### Pitfall 13: Pricing-table coverage gaps for new providers

**What goes wrong:**
This project already has a documented per-provider cache-floor table (CACHE-01/CACHE-05: "NEVER hardcode 1,024, always the active model's documented floor") proving pricing/cost-accounting is provider-sensitive and easy to get wrong. Adding OpenAI/Gemini/etc. via `init_chat_model` without a matching pricing-table row per new model means `BudgetCents`/`ReservationStore` accounting silently mis-costs (or zero-costs) real spend the moment a non-Anthropic model is selected.

**Why it happens:**
This project's own Phase 7 budget-overcount incident (a 2.8× Claude-5 undercount, fixed via exact-ID pricing + an empirically-probed cache-write multiplier) shows pricing tables need per-model-ID precision, not per-provider defaults. A new provider without a validated row repeats that incident — but silently (undercounting spend) rather than loudly (visible overcounting).

**How to avoid:**
Make pricing-table coverage a hard gate on the multi-provider endgame — a CI check that fails if a model referenced in any `LevelConfig` or chart default lacks a pricing-table row. Empirically probe (not assume) cache-write/cache-read multipliers per new provider the way Phase 7 did for Claude-5, before trusting budget caps against it.

**Warning signs:**
A new provider ships with a placeholder/default price row instead of an empirically-verified one; `BudgetCents` enforcement against a new-provider run shows suspiciously low or zero accrued cost.

**Phase to address:**
Multi-provider endgame phase, pricing-table sub-task — gate BEFORE the provider is exposed as selectable in any chart default or `LevelConfig`.

---

## Technical Debt Patterns

| Shortcut | Immediate Benefit | Long-term Cost | When Acceptable |
|----------|-------------------|-----------------|------------------|
| Reusing one fixed eval dataset for BOTH ladder-rung gating and System-loop promotion | Saves harness-building effort | Doubles overfitting pressure on one dataset (Pitfall 8) | Only if the dataset carries its own version and is periodically rotated/expanded |
| Skipping the live billable proof for "safe-looking" shapes (Solo/Pipeline) | Saves $ and time | The wired-but-never-ran class strikes low-risk-looking code too — Phase 51's verdict relay looked safe (Pitfall 3) | Never for the first instance of any new loop/shape; fine for repeat runs of an already-proven shape |
| Cloning verifier config across all panel seats (K identical model/prompt copies) | Simplest to implement | Illusory consensus, correlated errors (Pitfall 11) | Only as a throwaway prototype; must diversify before trusting the vote in production |
| Same commit touching both eval fixtures and the harness/prompt under test | Faster iteration during ladder spikes | Direct eval-gaming vector once System loop live-promotes (Pitfall 7) | Fine pre-hardening / exploratory spikes only; never once promotion is live-gating |
| Autonomy auto-ratchet with no minimum sample size | Faster reduction in human toil | History poisoning, rubber-stamped regressions (Pitfall 9) | Never in production Oversight policy; acceptable only as a stubbed always-manual default during schema-only phases |

## Integration Gotchas

| Integration | Common Mistake | Correct Approach |
|-------------|-----------------|-------------------|
| `langchain-anthropic` / `with_structured_output` | Assuming `ToolStrategy`/`JSONStrategy` behave identically across providers by default resolution | Pin the strategy explicitly per provider; verify the actual mechanism (native `json_schema` vs. forced tool-choice) rather than trusting `init_chat_model`'s default |
| credproxy + `init_chat_model` multi-provider | Assuming the existing Anthropic-only TLS/credproxy route allowlist "just works" for OpenAI/Gemini paths | This project's own strategy note already flags the credproxy route-allowlist extension as required, non-automatic work — build and test it explicitly per new provider |
| Provider structured-output field population | Assuming JSON-mode/`json_schema` guarantees ALL optional fields populated the same way across providers | Validate required-field presence explicitly in the parser; don't rely on one provider's behavior as a spec for all |
| `make eval` harness | Assuming eval-harness spend is capped like production spend because it uses the same credproxy | It isn't — the harness runs outside `Project`/`BudgetCents` flow (Pitfall 4); wire it through the same reservation accounting or budget it explicitly out-of-band |

## Performance Traps

| Trap | Symptoms | Prevention | When It Breaks |
|------|----------|------------|-----------------|
| Judge/Tournament panels at single-node kind scale | OOM (exit 137) — same shape as run-2b's D3 | `soundingPolicy.maxShape` + per-wave aggregate cap, proven live on kind before shipping | `N·(1+K)+1` Jobs exceeding node pod-scheduling headroom — ~60 concurrent pods was the empirical run-2b OOM threshold on the dev VM |
| Multi-agent debate/consensus rounds | Quadratic token cost per added agent/round (~2.5×+ a single call per research) | Hard-cap `iteration` in `LoopPolicy`; prefer isolated self-correction where equivalent quality is achievable cheaper | Any all-to-all communication pattern beyond 2–3 rounds or a handful of agents |
| Shadow-mode eval comparisons at ladder scale | Real API spend doubles per rung compared, invisible to production budget caps | Pre-declared eval budget, sized comparison task sets (Pitfall 4) | N rungs × M comparison tasks × real API pricing, unbounded without an explicit cap |

## Security Mistakes

| Mistake | Risk | Prevention |
|---------|------|------------|
| Read-only evaluator independence enforced only by prompt (not structurally) once new judge roles (Product/System/Oversight) are added | A judge with authoring credentials/tool access can be talked into approving or gaming its own output | Extend the same structural pattern (ReadOnly mounts + credential omission + vendor-tag detection) proven for the Task-loop verifier to every new judge role |
| System-loop candidate/evaluator co-mutation as a covert self-approval channel | An actor with commit access to both harness and benchmark can quietly weaken the promotion gate | Require separate approval/ownership paths, or at minimum a diff-scope check flagging same-PR touches to both candidate and evaluator |
| Oversight autonomy history influenced by the same agent it governs | Textbook trust-then-exploit: an agent that produces artificially clean runs to earn autonomy, then exploits the reduced oversight | Ground autonomy history in signals the governed agent cannot influence (deterministic test outcomes, external review), not self-reported confidence |

## UX Pitfalls

| Pitfall | User Impact | Better Approach |
|---------|-------------|-------------------|
| Dashboard shows "autonomy went up" with no explanation of which evidence triggered it | Operator can't audit or contest the escalation | Log/display the exact sample (which runs, what confidence scores) behind each autonomy change — same provenance discipline the loop-native observability contract already requires |
| Product-loop re-plan surfaced identically to a normal Task retry | Operator doesn't realize a re-plan may be spawning a NEW Project (cost implications) vs. iterating in place | Visually distinguish "new scoped Project spawned" from "iteration within existing Project," consistent with the never-destructive re-plan rule (Pitfall 5) |
| Tournament/panel fan-out rendered as a single node in the DAG view | Operator underestimates the real cost/pod count of a "gnarly" node | Render the Sounding's own derived `label` field ("Tournament · 3 competitors, 2 verifiers") in the dashboard DAG, not only in `.status` |

## "Looks Done But Isn't" Checklist

- [ ] **LangGraph authoring rung:** Often missing a live billable comparison run — verify a real end-to-end proof artifact exists in the phase record, not just eval-harness green (Pitfall 3)
- [ ] **Product loop re-plan:** Often missing the "additive not destructive" CRD-boundary enforcement — verify no code path lets Product PATCH/cancel an in-flight Phase/Plan
- [ ] **System loop promotion:** Often missing a rollback mechanism — verify a documented, tested revert path exists, not just a promote path
- [ ] **Oversight autonomy resolver:** Often missing minimum-sample-size/minimum-window gating — verify the resolver can't ratchet autonomy up on a handful of successes in a short window
- [ ] **Judge/Tournament panel:** Often missing verifier diversity — verify K seats aren't K clones of the same model+prompt+temperature
- [ ] **Multi-provider verdict schema:** Often missing per-provider golden-fixture coverage — verify the shared Go↔Pydantic fixture is exercised against every supported provider, not only Anthropic
- [ ] **New provider pricing row:** Often missing empirical cache-multiplier probing — verify the row was measured live, not copy-pasted from another provider's numbers

## Recovery Strategies

| Pitfall | Recovery Cost | Recovery Steps |
|---------|----------------|------------------|
| Eval gate measuring the wrong thing (Pitfall 1) | MEDIUM | Add the missing eval dimensions; re-run the affected rung's gate before any later rung proceeds — don't retroactively grandfather a shipped rung |
| Judge drift flips a Product-loop verdict (Pitfall 6) | LOW | Pin judge model/version going forward; a closed Project's verdict is not reopened — append a note, don't re-litigate |
| System loop promotes a gamed candidate (Pitfall 7) | HIGH | Revert to the pinned N-1 candidate (this is why rollback must exist); treat the gaming incident as a required schema/policy fix — candidate/evaluator separation — not a one-off patch |
| Fan-out OOM on kind (Pitfall 10) | LOW — this project has already recovered from this shape twice | Reduce the concurrency/shape ceiling; same recipe as the Phase 32 fix |
| Pricing-table gap silently undercounts spend (Pitfall 13) | MEDIUM | Same recipe as the Phase 7 budget-overcount fix — exact-ID pricing rows + empirical multiplier probe, then audit any run that used the gap-having model for retroactive true-up |

## Pitfall-to-Phase Mapping

| Pitfall | Prevention Phase-Position | Verification |
|---------|----------------------------|----------------|
| 1. Eval gate measures wrong thing | Early — ladder-design phase, before executor rung | New eval dimensions (tool-call efficiency, diff-correctness) exist and gate the executor rung distinctly from planner rungs |
| 2. CLI/SDK parity blind spot | Per-rung gate | A raw request/response diff check exists per rung, not only an eval score comparison |
| 3. "Wired but never ran" class | Final conformance, every new-loop phase | A live billable proof artifact is attached to the phase's verification record |
| 4. Shadow-mode spend invisible to budget cap | Early — ladder-design phase | Eval/shadow spend has a pre-declared, enforced cap distinct from production `BudgetCents` |
| 5. Product loop discards paid work | Product loop schema phase | CRD boundary test: no code path lets Product mutate/cancel in-flight Phase/Plan objects |
| 6. Product-loop judge drift/thrash | Product loop `LoopPolicy` parameterization phase | `MaxIterations` set on Product `LoopPolicy`; judge version pinned per Project |
| 7. System loop gaming its own evals | System loop promotion-policy phase | Candidate/evaluator co-mutation blocked or requires elevated approval; rollback path tested |
| 8. Eval overfitting | System loop, dataset-versioning sub-task | Dataset carries a version field; held-out subset excluded from promotion decisions |
| 9. Oversight autonomy ratchets on thin evidence | Oversight loop schema phase | `MinSampleSize`/`MinWindow` enforced in the `AutonomyLevel` resolver; down-fast/up-slow asymmetry tested |
| 10. Fan-out OOM / cost multiplication | Dynamic-workflow fan-out phase(s) | `soundingPolicy.maxShape` + per-wave aggregate cap enforced in code (not just schema); live single-node kind proof for the first fan-out shape shipped |
| 11. Judge panel correlation | Dynamic-workflow fan-out phase, Judge-panel sub-step | Panel seats use distinct model/prompt/temperature configs; agreement-rate monitoring exists |
| 12. Multi-provider verdict schema drift | Multi-provider endgame phase | Golden verdict fixture exercised against every supported provider's real structured-output call |
| 13. Pricing-table coverage gaps | Multi-provider endgame phase, pricing sub-task | CI gate fails if a referenced model lacks a pricing-table row; new rows are empirically probed, not assumed |

## Sources

**Internal (HIGH confidence — this project's own recorded incidents and decisions):**
- `.planning/PROJECT.md` — Key Decisions table (CACHE-01 spike, Phase 7 budget-overcount, Phase 32 concurrency cap, Phase 49/51/52 loop-contract and anti-gaming decisions, run-1 $80 dry-out, run-2b OOM)
- `.planning/notes/five-loop-model.md` — the five-loop contract, load-bearing rules, per-loop dispositions
- `.planning/notes/langgraph-successor-runtime-strategy.md` — evidence-gated migration ladder, eval-dimension risk, `init_chat_model` endgame
- `.planning/notes/sounding-dynamic-orchestration-design.md` — fan-out shape cost table, run-2b OOM citation, `soundingPolicy.maxShape` guardrail design

**External (MEDIUM confidence — WebSearch-verified, multi-source agreement, not Context7/official-doc-tier):**
- [Justice or Prejudice? Quantifying Biases in LLM-as-a-Judge](https://llm-judge-bias.github.io/) — position/verbosity bias
- [Self-Preference Bias in LLM-as-a-Judge](https://arxiv.org/pdf/2410.21819)
- [What Is LLM-as-a-Judge Calibration? Power & Limits](https://deepchecks.com/llm-judge-calibration-automated-issues/) — judge drift on model version bumps
- [Specification Gaming and Reward Hacking — AI Risk Assessment](https://www.donets.org/risks/specification-gaming-and-reward-hacking)
- [Detecting Proxy Gaming in RL and LLM Alignment via Evaluator Stress Tests](https://arxiv.org/pdf/2507.05619)
- [The Cost of Consensus: Isolated Self-Correction Prevails Over Unguided Homogeneous Multi-Agent Debate](https://arxiv.org/html/2605.00914v1) — quadratic debate cost, ~2.5x single-call baseline
- [Between autonomy and oversight: Trust calibration and human controllability in agentic AI systems](https://gjeta.com/content/between-autonomy-and-oversight-trust-calibration-and-human-controllability-agentic-ai)
- [How to Build Human-in-the-Loop Oversight for AI Agents — Galileo](https://galileo.ai/blog/human-in-the-loop-agent-oversight) — automation complacency, supervision fatigue/rubber-stamping
- [with_structured_output — langchain_anthropic reference](https://reference.langchain.com/python/langchain-anthropic/chat_models/ChatAnthropic/with_structured_output) — Anthropic's tool-forcing structured-output mechanism
- [OpenAI and Anthropic Integrations — LangChain DeepWiki](https://deepwiki.com/langchain-ai/langchain/3.1-openai-and-anthropic-integrations) — per-provider structured-output method differences
- [anthropic structured output does not work · langchain-ai/langchain#30158](https://github.com/langchain-ai/langchain/issues/30158) — documented reliability regression
- [Shadow Mode Rollouts for AI Agents](https://brightlume.ai/blog/shadow-mode-rollouts-ai-agents-pilot-production) — parity-testing and dual-environment cost pattern

---
*Pitfalls research for: TIDE v1.0.10 "King Tide" — LangGraph authoring migration, Product/System/Oversight loops, adversarial fan-out patterns*
*Researched: 2026-07-21*
