# Project Research Summary

**Project:** TIDE v1.0.10 "King Tide"
**Domain:** Kubernetes-native agentic-orchestration platform — LangGraph authoring-runtime migration, multi-provider endgame, completion of the five-loop model (Product/System/Oversight join Execution/Task), three dynamic-workflow fan-out patterns
**Researched:** 2026-07-21
**Confidence:** MEDIUM-HIGH

## Executive Summary

King Tide is not one feature, it's four coupled builds that share almost all their plumbing: (1) a WRITE-capable LangGraph authoring image migrating planners then the executor off the free-text-parsing CLI path, gated rung-by-rung on real evidence; (2) `init_chat_model` multi-provider dispatch (OpenAI first, Gemini as a flagged stretch); (3) generic eval-gated promotion machinery (the System loop) that the migration ladder consumes as its first real workload; and (4) three dynamic-workflow fan-out patterns (adversarial verification/judge panels, generate-and-filter, tournament) plus the Product and Oversight loops that close the five-loop model. All four research passes converge on the same finding: almost nothing here needs a new subsystem. `ResolveProvider`, `podjob.BuildOptions.ReadOnly`, `ResolveLoopPolicy`, and `pkg/dispatch.Subagent`'s envelope contract are all extended, never duplicated — the discipline that made the read-only LangGraph verifier (Phase 48-53) work is the same discipline that closes every open question this milestone raises.

The one genuinely new piece of infrastructure — and the single highest-leverage build item in the whole milestone — is a shared "N-way dispatch of one logical node, then reduce" primitive. Shadow-pair runtime comparison (CLI vs. LangGraph, N=2, fixed candidates) and all three dynamic-workflow patterns (judge panels, generate-and-filter, tournament) are the SAME mechanism with different N and reduce strategies. Building it once, generically, and landing its cost/concurrency guardrails (`soundingPolicy.maxShape`, per-wave aggregate cap) in the SAME phase as the first fan-out shape ships is non-negotiable — this project has already OOM'd a single-node kind cluster once on flat 60-pod fan-out (dogfood run-2b) with no multiplier involved; a Tournament node's `N·(1+K)+1` shape makes that math structurally worse per-node if the ceiling isn't enforced from day one, not retrofitted after an incident.

The dominant risk is not technical unknowns (the stack pins are live-verified HIGH confidence, and the architecture recommendations are grounded in file:line reads of shipped code) — it's this project's own recurring defect class: shipping a wired-but-never-exercised path that passes green tests while the real production flow was never driven end-to-end (Phase 22's stale dashboard embed, Phase 51's nil-verdict relay, Phase 52's DEFECT-B/C). Every phase that introduces a NEW loop-closing path — a Product-loop re-plan, a System-loop promotion, an Oversight autonomy escalation, a fan-out reduce step — needs a live billable proof run before being declared done, not just envtest/kind green. A second, quieter risk already exists in shipped code: `SelfInstruments("langgraph")` returns `true` today with no `openinference-instrumentation-langchain` behind it, so every live LangGraph dispatch produces zero trace spans right now — this must be closed in the same phase that ships the write-capable image, not treated as a pre-existing condition to inherit silently.

## Key Findings

### Recommended Stack

Both new Python images (the existing read-only verifier and the new write-capable authoring image) share one pin set — `langgraph==1.2.9`, `langchain==1.3.14`, `langchain-anthropic==1.4.8`, `langchain-core==1.4.9` (explicitly hold off the hours-old 1.5.0), `anthropic==0.117.0`, `pydantic==2.13.4`, `httpx==0.28.1` — re-verified live against PyPI on the research date with zero drift. Child-CRD emission moves from free-text-then-sanitize to `create_agent(response_format=ChildCRDBatch)`, the same `ToolStrategy`/structured-output idiom already proven for the verifier's `GateDecision`. Multi-provider dispatch needs no new mechanism (`init_chat_model` is already in `langchain`) but does need explicit per-provider integration packages, a vendor-aware `paramsAllowList` (Anthropic-only levers like `effort`/`thinking` must reject on other vendors, not silently no-op), and real Go-side work: `internal/credproxy` needs a provider-keyed upstream table and per-provider billing-exhaustion classifiers before `BillingHalt` semantics work cross-provider — this is architecture work, not a library choice.

**Core technologies:**
- `langchain-openai` 1.3.5 + `openai` 2.46.0 — OpenAI is the ladder's stated endgame provider; confirmed native `ProviderStrategy` structured output, `httpx`-based, same `SSL_CERT_FILE` CA-trust proof as Anthropic
- `openinference-instrumentation-langchain` 0.1.67 + `opentelemetry-sdk`/`opentelemetry-exporter-otlp-proto-grpc` 1.44.0 — closes the live zero-spans gap; additive dependency + one init call, chart already injects the needed OTEL env vars
- Eval-gating machinery — zero new dependencies; extend `internal/eval` in Go, reuse `verdict.go`'s schema as the comparative judge, reject LangSmith's hosted `evaluate()` (phones home to `smith.langchain.com`, contradicts self-hosted-only observability posture)
- `langchain-google-genai` 4.2.7 — Gemini, explicitly a stretch-only provider: its `google-auth`/`requests` transitive dependency does not honor `SSL_CERT_FILE`, needs a separate `REQUESTS_CA_BUNDLE` proof spike before any rung targets it

### Expected Features

Every capability in scope is graded against the five-element loop bar (goal/candidate/evaluator/repeat-policy/bounded-exit) — where a proposed feature is really a pipeline stage or continuous policy layer, the research says so explicitly rather than force-fitting it.

**Must have (table stakes):**
- System loop candidate versioning + eval-gated promotion, with a documented trailing baseline (not a single frozen number) — the load-bearing, cheapest-to-build piece; everything else depends on it existing
- Cost-gated fan-out defaulting OFF or N=1, bound to the existing `executorConcurrency` semaphore and budget reservation rails
- Deterministic checks dominate every LLM judge (single or panel) — a failing gate command overrides any confidence score, no exception
- Rollback that's cheap and always available — for TIDE this is even simpler than industry live-traffic rollback: flip a per-role `Image`/`Vendor` pointer back, no traffic to drain
- Confidence-based autonomy that fails closed on unparseable/low-confidence output — never "assume safe"
- Paired/shadow comparison on identical dispatched input (not a live-traffic split TIDE has no analog for)

**Should have (competitive/differentiating):**
- Escalation cascade for judge cost (cheap deterministic fast-path → expensive panel only on ambiguity) — a well-tuned cheap judge can be far cheaper at comparable accuracy than "always run the panel"
- Bracket/knockout tournament (O(n)), never round-robin (O(n²))
- Diverse judge/generator composition (different model/prompt-angle per seat), never K clones of one config re-sampling the same bias
- The shared N-way dispatch primitive itself, reused by both shadow-pair migration and all three dynamic patterns — build once
- Cross-loop data reuse: a Product-loop re-plan is System-loop-relevant history; System-loop promotion history feeds Oversight's confidence/history signal

**Defer (v2+ / beyond this milestone):**
- A permanently-running Product-loop backlog daemon ingesting external signals (GitHub issues, prod alerts) — explicitly rejected by TIDE's own design; ship only the bounded outcome-judge-at-boundary slice
- Full ML-learned confidence/autonomy model for Oversight — ship a documented heuristic formula first, earn the ML model later with real track-record data this milestone generates
- The full Sounding semantic shape-classifier — this milestone hand-configures shapes via `soundingPolicy`, doesn't build the auto-picker

### Architecture Approach

Nothing about the dispatch spine changes. Every new capability rides the same seam the read-only verifier already proved: `EnvelopeIn`/`EnvelopeOut` on the per-Project PVC, `pkg/dispatch.Subagent`, one K8s Job per dispatch, and three precedence-chain resolvers (`ResolveProvider`, `resolveImage`, `ResolveLoopPolicy`) as the only chokepoints config should flow through. The write-capable image is `ReadOnly: false` through the SAME `JobKindPlanner`/`JobKindExecutor` build path, not a new Job kind. Fan-out patterns are Jobs *within* one dispatch (siblings sharing a parent), never new DAG edges — no second scheduling engine, consistent with CLAUDE.md's locked "don't replace layered Kahn" anti-pattern.

**Major components:**
1. `ProviderSpec.Vendor` precedence chain (extends `ResolveProvider`, today hardcoded to `"anthropic"`; three verifier call sites already hand-roll the exact override this needs generalized) — per-level runtime/vendor selection, must resolve together with `Image`
2. Fan-out + reduce dispatch primitive (new, shared) — wait-for-N-siblings sub-state generalizing the existing `ChildCount` race-free gating pattern, plus a pluggable reduce function (vote/quorum for judges, select-and-discard for generators); pre-charges N× budget as ONE pre-flight reservation, never per-Job
3. `internal/eval` extension + new `SystemCandidate`/`SystemExperiment` reconciler — one-directional dependency only (`internal/eval`'s import firewall stays intact; the new controller consumes it as a library, never the reverse)
4. New `Product`/`ProductLoop` CRD sitting above `Project` (not a sixth level in the locked five-level hierarchy) — re-plans by authoring a NEW Milestone/Project via the existing `ChildCRDSpec` mechanism, never by reopening the locked `MaxIterations=0` clamp at phase/milestone/project
5. `ResolveLoopPolicy` extension to finally resolve `LoopPolicy.Autonomy` (declared since v1.0.9, never read or written) from a risk/confidence/history-aware tier — `LoopStatus` gains one more resolved scalar, never an accumulating history slice (LOOP-03's compile-time guard stays intact)

### Critical Pitfalls

1. **Evidence-gating measures the wrong thing at the executor rung** — the Phase-18 harness proves template-prompt/structured-output quality, not agent-loop quality (tool-call efficiency, diff correctness, error recovery). This project's own strategy note names the gap without resolving it. Add agent-loop-specific eval dimensions as a hard phase dependency BEFORE the executor rung is gated — treat it as a blocking build item, not an assumption that falls out of the planner rungs.
2. **"Wired but never ran the shipped path" — this project's own recurring defect class.** Phase 22's stale dashboard embed, Phase 51's nil-verdict relay, and Phase 52's DEFECT-B/C all passed green tests while the real production path was never exercised end-to-end. Every phase introducing a NEW loop-closing path (Product re-plan, System promotion, Oversight escalation, a fan-out reduce step) requires a live billable proof run attached to its verification record — not deferred to milestone close.
3. **Fan-out patterns multiply pods faster than the existing guardrail accounts for.** `executorConcurrency` caps total concurrency, not per-node burst share; a single Tournament node (`N·(1+K)+1` Jobs) can burn most of the budget alone and starve every other concurrent node in the wave — the same failure class that already OOM'd dogfood run-2b at ~60 flat concurrent planner pods. `soundingPolicy.maxShape` ceilings + a per-wave aggregate cap must ship in the SAME phase as the first fan-out shape, proven live on single-node kind before Tournament (the highest-multiplier shape) ships.
4. **Shadow-mode evidence-gathering spend is invisible to the budget guardrail.** `make eval` runs outside the `Project`/`BudgetCents` reservation flow entirely; comparing LangGraph against the CLI baseline doubles LLM spend per compared task with no cap analogous to production budgets. Declare and enforce a pre-set eval/shadow-mode dollar cap before Rung 1 starts, distinct from `BudgetCents`.
5. **System loop gaming its own evals / promotion without rollback.** Unlike the Task loop (two separated actors: implementer vs. independent verifier), a `SystemExperiment` can bundle candidate AND evaluator/benchmark changes in one commit — a textbook reward-hacking setup. Candidate and evaluator versioning must stay separate, immutable artifacts; a same-commit touch to both is a hard-block or requires elevated approval; promotion needs a tested rollback path from day one, not a one-way ratchet.

## Implications for Roadmap

Based on combined research (the Architecture doc's own dependency-ordered "Suggested Build Order" is the strongest single signal here, cross-checked against Features' MVP staging and Pitfalls' phase-position mapping), suggested phase structure:

### Phase 1: Runtime Selection Foundation + Observability Gap Closure
**Rationale:** Prerequisite for every subsequent rung — without a real `Levels.<level>.{Image,Vendor}` config seam, each later migration repeats the verifier's inline-literal override pattern a third and fourth time. Small and mechanical; mirrors the already-proven `resolveImage` precedence chain exactly.
**Delivers:** `ProviderSpec.Vendor` resolved through `ResolveProvider`'s precedence chain, Vendor+Image resolving together with a fail-fast sentinel check; `openinference-instrumentation-langchain` wired into both Python images closing the live zero-spans gap.
**Addresses:** Per-role runtime cutover (FEATURES table stakes); closes the SelfInstruments gap flagged as carried-in debt in STACK.md.
**Avoids:** Pitfall — silently inheriting the zero-span observability gap into a second (write-capable) image.

### Phase 2: System Loop CI-Gate Stage (Evidence Engine, First Half)
**Rationale:** "Each rung gated on eval evidence vs. the CLI baseline" is a hard dependency of the authoring ladder itself — must exist before the first planner rung migrates, not after.
**Delivers:** `cmd/tide-eval`/`make eval` extended from "measure one template's tokens" to "compare a candidate rung's output against the CLI baseline," pass/fail against a promotion threshold; pre-declared, capped eval/shadow-mode budget line item distinct from `BudgetCents`.
**Addresses:** System loop candidate versioning (table stakes); CLI/SDK parity blind spot per-rung checklist (raw request/response diff, not just eval score).
**Avoids:** Pitfall 4 (shadow-mode spend invisible to budget guardrail); lays groundwork against Pitfall 1 (executor-rung eval-dimension gap) by establishing the comparison mechanism early.

### Phase 3: Shared Fan-Out + Reduce Primitive + Cost/OOM Rails
**Rationale:** The single highest-leverage build item in the milestone — shadow-pair comparison and all three dynamic-workflow patterns are the same "N sibling Jobs → reduce" mechanism with different N and reduce strategy. Building it once, generically, and landing its guardrails in the SAME phase (not retrofitted after an incident) is a direct, explicit lesson from this project's own OOM history.
**Delivers:** Wait-for-N-siblings gating (generalizing the existing `ChildCount` pattern), pluggable reduce function, one pre-flight N× budget reservation, `soundingPolicy.maxShape` + per-wave aggregate concurrency ceiling enforced in code (not just schema), proven live on single-node kind.
**Uses:** Existing `verifierInFlightCount`/`plannerInFlightCount` pool accounting, `budget.ReservationStore` — additive, not new plumbing.
**Implements:** Fan-out + reduce dispatch primitive (Architecture component).

### Phase 4: Write-Capable LangGraph Authoring Image — Planner Rungs
**Rationale:** Planner rungs need no git-write path (they only emit `ChildCRDSpec`s) — structured-output child-CRD emission is a direct reuse of the verifier's already-proven `ToolStrategy(PydanticModel)` pattern, making this architecturally cheaper than "safer" alone would suggest. Depends on Phase 1 (vendor selection) and benefits from Phase 2 (evidence gate) being live.
**Delivers:** New write-capable image (`ReadOnly: false` through existing `JobKindPlanner` path), `ChildCRDBatch` structured emission replacing sanitize-and-parse, hand-authored `@tool` functions (not `deepagents`).
**Addresses:** Structured child-CRD JSON emission — the actual reason the migration exists.
**Avoids:** Pitfall 2 (CLI/SDK parity blind spot — per-rung raw wire-format diff required, not just eval score); requires a live billable proof per Pitfall 3.

### Phase 5: Adversarial Verification (Judge Panel) at Verify Seams
**Rationale:** First and simplest consumer of the Phase 3 primitive — verify seams already have per-level `LoopPolicy` + `verifierInFlightCount` pool accounting, so extending width from 1 to K is close to purely additive.
**Delivers:** K sibling `JobKindVerifier` Jobs with a vote-reduce step; diversified verifier configuration per seat (different model/prompt-version, not K clones) to avoid illusory-consensus correlation.
**Addresses:** Composite evaluator type on `TaskSpec.verification` (Features dependency graph).
**Avoids:** Pitfall 11 (judge collusion/correlation) — bake seat diversity in at design time, expensive to retrofit after a Tournament ships with cloned verifiers.

### Phase 6: Generate-and-Filter (Fan-and-Merge) at Planner Seams
**Rationale:** Second consumer of the Phase 3 primitive; needs a genuinely new "materialize only the champion's children" reduce step (unlike Phase 5, not purely additive) — materializing all N candidates' ChildCRDs would silently multiply the DAG.
**Delivers:** N sibling `JobKindPlanner` Jobs with a divergence directive, a Merger/Synthesizer role selecting one candidate to materialize.
**Addresses:** Generate-and-filter (Features differentiator); shares the shadow-pair mechanism with Phase 4 at N=2 degenerate case.

### Phase 7: Executor Rung Migration
**Rationale:** Hardest parity bet in the ladder — Claude Code's battle-tested file-edit loop vs. a hand-built LangGraph tool loop starting from zero. Sequenced last per the ladder's own explicit reasoning, and hard-gated on a capability that does not exist yet.
**Delivers:** Git-write path inside the LangGraph container (local commit via the existing `resolveAgentIdentity` contract, `tide-push` remote-push seam unchanged); agent-loop-specific eval dimensions (tool-call efficiency, diff-correctness, tool-error-recovery rate) added to `internal/eval` BEFORE this rung is gated.
**Addresses:** The migration ladder's stated highest-risk item.
**Avoids:** Pitfall 1 (eval gate measuring the wrong thing) — this is the pitfall's own named prevention phase, not optional scope.

### Phase 8: Tournament Pattern
**Rationale:** Hardest consumer of the Phase 3 primitive — combines Phases 5+6, adds budget pre-flight gating across the full `N·(1+K)+1` shape and a tie-break merge rule. Sequenced last among dynamic patterns so its budget/merge logic reuses both simpler patterns' proven reduce steps.
**Delivers:** Bracket/knockout (O(n)) judging within the tournament, not round-robin; single pre-flight reservation for the full fan-out width; a live single-node kind proof required before ship.
**Avoids:** Pitfall 10 (fan-out OOM) at its highest-multiplier configuration — this is the shape most likely to reproduce the run-2b incident if ceilings aren't proven live first.

### Phase 9: Product Loop
**Rationale:** Its dispatch/gate integration points (project-level `LoopPolicy`, `checkParentApproval`/`gates.EvaluatePolicy`) already exist from Phase 52 — the new work is a genuinely new CRD/reconciler and an outcome-focused evaluator role. Sequenced after the System loop's evidence-gating exists, since Product-loop-driven re-planning is exactly the high-stakes autonomous action this milestone wants evidence behind.
**Delivers:** New `Product`/`ProductLoop` CRD above `Project`, outcome-judge-at-boundary with bounded `MaxIterations` and a pinned/versioned judge model, additive-only re-planning (spawns a new scoped Project, never patches/cancels an in-flight one).
**Avoids:** Pitfall 5 (re-planning discards paid work) via the additive-not-destructive CRD boundary; Pitfall 6 (judge drift/thrash) via bounded iteration + pinned judge version.

### Phase 10: System Loop Controller-CRD Stage
**Rationale:** Operationalizes Phase 2's comparison persistently; naturally follows once there's enough rung/candidate history to make persistence worth it.
**Delivers:** `SystemCandidate`/`SystemExperiment` as persisted, version-addressed CRDs; candidate/evaluator separation enforced (same-commit co-mutation hard-blocked or elevated-approval-gated); tested rollback path; versioned/rotatable benchmark dataset with a held-out subset excluded from promotion decisions.
**Avoids:** Pitfall 7 (gaming promotion without rollback) and Pitfall 8 (eval overfitting) — both are schema/policy decisions that are cheap now and expensive to retrofit after promotion is live-gating.

### Phase 11: Oversight Loop
**Rationale:** Thinnest build (one `ResolveLoopPolicy` extension + one new `LoopStatus` field) but most valuable once Phases 9 and 10 exist to feed it real risk/confidence/history signal — resolving `Autonomy` against signals that don't exist yet is premature. Sequenced last by design, not as an afterthought.
**Delivers:** `LoopPolicy.Autonomy` finally resolved via a documented heuristic (risk tier × confidence bucket × rolling pass-rate, NOT an ML model this milestone); `MinSampleSize`/`MinWindow` gating with a monotonic-down-fast/monotonic-up-slow asymmetry; mandatory random-sample audits of auto-approved decisions.
**Avoids:** Pitfall 9 (autonomy ratchets up on thin evidence / history poisoning) — the down-fast/up-slow asymmetry and minimum-window requirement must be in the resolver's contract from day one.

### Phase 12: Multi-Provider Endgame + CLI-Deprecation Decision
**Rationale:** A decision point gated on the accumulated evidence from Phases 1-7 (executor rung proven), not an independent build item — don't pull it forward just because `init_chat_model` is mechanically simple.
**Delivers:** OpenAI provider rung (`langchain-openai`/`openai` pinned, confirmed native `ProviderStrategy`), credproxy provider-keyed upstream table + per-provider billing-exhaustion classifiers, per-provider golden verdict-fixture conformance test, empirically-probed pricing-table row per new model gated as a hard CI check.
**Avoids:** Pitfall 12 (multi-provider verdict schema drift) and Pitfall 13 (pricing-table coverage gaps) — both explicitly named as this phase's own required gates, before the provider is exposed as selectable anywhere.

### Phase Ordering Rationale

- **The fan-out primitive (Phase 3) is deliberately early and generic**, not built narrowly for shadow-pair and rebuilt later for dynamic patterns — Features and Architecture both independently identify this as the one item that forces rework if under-scoped.
- **System loop precedes the rungs that consume it** (Phase 2 before Phase 4) because the migration ladder's own evidence-gate requirement makes System loop's core contract a hard dependency, not a parallel track — this mirrors Features' explicit dependency note ("runtime migration requires System loop, not the reverse").
- **Cost/OOM guardrails land in the same phase as the first fan-out shape (Phase 3), never after** — a direct lesson from this project's own run-2b OOM incident, called out explicitly in the milestone brief and reinforced independently by Pitfalls research.
- **The executor rung (Phase 7) is gated on a harness capability that doesn't exist yet** (agent-loop eval dimensions) — Pitfalls names this the single highest-risk dependency in the milestone; it must be a scheduled deliverable, not an assumption.
- **Product, System's controller-CRD stage, and Oversight are sequenced last** because each needs real signal (evidence history, promotion history, verdict/track-record data) to be meaningful rather than falling back to static config — building them earlier would produce loops with nothing real to compute over.
- **Multi-provider is explicitly the closing move**, sequenced after the ladder proves out — its value depends on the ladder being complete, not on `init_chat_model`'s mechanical availability.

### Research Flags

Phases likely needing deeper research during planning:
- **Phase 3 (fan-out + reduce primitive):** No existing code precedent beyond `ChildCount`'s narrower gating pattern; Architecture research flags this as MEDIUM confidence, extending an unrouted design note (`sounding-dynamic-orchestration-design.md`) not yet approved through GSD — confirm scope against this milestone's actual requirements before assuming the Sounding doc's full apparatus is in scope.
- **Phase 7 (executor rung):** The hardest parity bet in the ladder; needs the new eval-dimension design work (tool-call efficiency, diff correctness, recovery-from-tool-error) that doesn't exist in any form today.
- **Phase 8 (Tournament):** Budget pre-flight math across the full `N·(1+K)+1` shape and tie-break merge rules are novel; requires a live single-node kind proof, not just design.
- **Phase 11 (Oversight):** The autonomy-resolver heuristic formula (risk × confidence × history) is a from-scratch design with no direct TIDE precedent; Features research flags this as its own HIGH-complexity item.
- **Phase 12 (multi-provider, Gemini specifically if pursued):** `google-genai`'s dual CA-trust path (`SSL_CERT_FILE` + `REQUESTS_CA_BUNDLE`) is unverified against the credproxy sidecar — Stack research explicitly flags this LOW confidence, "spike before committing a rung."

Phases with standard patterns (skip research-phase):
- **Phase 1 (vendor selection):** Mirrors the already-shipped `resolveImage` precedence chain exactly; mechanical.
- **Phase 4 (planner-rung authoring image):** Direct reuse of the verifier's already-proven `create_agent`/`ToolStrategy` pattern; only the schema shape (batch vs. singleton) is new.
- **Phase 5 (Judge panel):** Verify seams already have the pool-accounting and `LoopPolicy` infrastructure; extending width is close to purely additive.
- **Phase 12 (OpenAI rung specifically):** Confirmed native `ProviderStrategy` structured output, same `httpx`/`SSL_CERT_FILE` CA-trust proof already validated for Anthropic — the OpenAI half of Phase 12 is well-documented, unlike the Gemini stretch.

## Confidence Assessment

| Area | Confidence | Notes |
|------|------------|-------|
| Stack | HIGH | Every version pin re-verified live against PyPI JSON the same day as research, zero drift found. MEDIUM specifically on structured-output reliability for non-Anthropic providers (LangChain's own docs, not independently load-tested against this repo's schemas) and on Gemini's CA-trust path (inferred from dependency graph, not build-verified) — both explicitly flagged inline in STACK.md. |
| Features | MEDIUM | Industry ecosystem patterns (MLOps/LLMOps promotion mechanics, HITL autonomy) are HIGH-confidence and well-established across multiple independent sources. TIDE-specific application is a genuinely novel synthesis — TIDE's Job-per-dispatch, no-external-DB, derived-waves constraints have no direct industry analog, so mapping (e.g., "shadow traffic" → "shadow dispatch") carries real translation risk even where the source pattern is solid. |
| Architecture | HIGH on today's-state claims (every "current gap" verified by direct file:line reads against `main`, e.g. the inline `Vendor: "langgraph"` at three call sites, the never-read `LoopPolicy.Autonomy` field). MEDIUM on integration-point recommendations that extend `sounding-dynamic-orchestration-design.md`, which the doc itself states "authorizes no code, CRD, or chart changes." | Treat the Sounding doc's fan-out shape grammar as the closest extant precedent, not a locked decision — confirm scope at phase-planning time. |
| Pitfalls | MEDIUM-HIGH | TIDE-specific findings (cited incidents: run-2b OOM, Phase 7 budget-overcount, Phase 51/52 wired-but-never-ran defects) are HIGH confidence, sourced directly from this project's own decision log. External ecosystem findings (LLM-judge bias, reward hacking, multi-agent debate cost, trust-calibration research) are MEDIUM confidence — WebSearch-verified across multiple independent sources but not Context7/official-doc-tier. |

**Overall confidence:** MEDIUM-HIGH

### Gaps to Address

- **Gemini's dual CA-trust path is unverified**: `SSL_CERT_FILE` covers `httpx`-based calls but `google-auth`'s `requests`-based auth flow needs `REQUESTS_CA_BUNDLE` separately — resolve with a live build-time spike before any phase commits to a Gemini rung, don't assume it "just works" like OpenAI did.
- **`sounding-dynamic-orchestration-design.md` is not yet routed through GSD**: every fan-out-pattern architecture recommendation in this research treats it as the closest precedent, not an approved design. Re-confirm the fan-out primitive's actual scope against this milestone's requirements at phase-planning time rather than assuming the Sounding doc's full apparatus (judge subagent escalation, ML classifier tiers) is in scope for v1.0.10.
- **Agent-loop eval dimensions for the executor rung don't exist in any form today**: this is a genuine build gap, not a research gap — Phase 7 cannot be scheduled meaningfully until this is designed as its own deliverable.
- **Credproxy's multi-provider extension is architecture work, not a library decision, and is not yet designed in detail**: a provider-keyed upstream table (vs. one sidecar per provider) and per-provider billing-exhaustion classifiers (OpenAI's `insufficient_quota`, Gemini's `RESOURCE_EXHAUSTED`) both need concrete design before Phase 12 can execute.
- **The bracket/knockout tournament cost-efficiency claim (O(n) beats O(n²) past n≈5) is single-WebSearch-pass sourced**, flagged LOW-MEDIUM confidence in FEATURES.md — directionally consistent with, and independently reinforced by, the Sounding doc's own cost formula (arrived at separately), which raises confidence somewhat but the citation itself should not be treated as authoritative without further validation if the exact threshold matters for a design decision.

## Sources

### Primary (HIGH confidence)
- PyPI JSON API, fetched live 2026-07-21 for every package cited in STACK.md (direct `requires_dist`/`upload_time_iso_8601` reads)
- In-repo code, read directly: `internal/controller/dispatch_helpers.go`, `pkg/dispatch/envelope.go`, `api/v1alpha3/loop_types.go`, `internal/gates/policy.go`, `internal/eval/doc.go`, `internal/dispatch/podjob/jobspec.go`, `pkg/dispatch/childcrd.go`, `internal/reporter/materialize.go`, `pkg/dispatch/vendor_capabilities.go`, `internal/harness/commit.go`, `internal/credproxy/server.go`, `cmd/tide-langgraph-verifier/verifier/{agent,envelope}.py`
- `.planning/PROJECT.md` — Key Decisions table (CACHE-01, Phase 7 budget-overcount, Phase 32 concurrency cap, run-1 $80 dry-out, run-2b OOM)
- `.planning/notes/five-loop-model.md`, `.planning/notes/langgraph-successor-runtime-strategy.md` — committed design notes referenced directly by PROJECT.md's current milestone

### Secondary (MEDIUM confidence)
- Context7 `/websites/langchain_oss_python_langchain` — `init_chat_model` dispatch, `ProviderStrategy`/`ToolStrategy` selection rules
- `.planning/notes/sounding-dynamic-orchestration-design.md` — fan-out shape grammar and cost formulas; explicitly not yet routed through GSD
- LLMOps ecosystem sources (multiple independent, cross-corroborating): eval-gate/prompt-versioning CI pipelines, shadow-mode/champion-challenger deployment, adaptive HITL/trust-calibration research, multi-judge ensemble and multi-agent-debate cost tradeoffs — full citation list in FEATURES.md and PITFALLS.md Sources sections

### Tertiary (LOW confidence)
- Bracket/knockout tournament cost-scaling claim — single WebSearch-pass sourced, directionally consistent with but not independently confirming the Sounding doc's own formula
- Gemini's `google-genai` dual CA-trust requirement — inferred from PyPI dependency graph, not build-verified

---
*Research completed: 2026-07-21*
*Ready for roadmap: yes*
