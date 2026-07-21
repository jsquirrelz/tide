# Requirements: TIDE v1.0.10 — King Tide

**Defined:** 2026-07-21
**Milestone:** v1.0.10 — King Tide: Five Loops, One Successor Runtime, Dynamic Workflows
**Sources:** `.planning/research/SUMMARY.md` (+ STACK/FEATURES/ARCHITECTURE/PITFALLS, committed `c00a7ffc`), `notes/langgraph-successor-runtime-strategy.md`, `notes/five-loop-model.md`, `notes/sounding-dynamic-orchestration-design.md` (fan-out/cost framing only — not pre-approved scope)

## v1.0.10 Requirements

### Authoring Migration (MIG) — the evidence-gated ladder

- [ ] **MIG-01**: A write-capable LangGraph authoring image exists behind the unchanged `pkg/dispatch.Subagent` envelope seam — verifier pin set reused, hand-authored `@tool` functions (no deepagents), commit-in-pod via the existing two-tier git model (agent identity in pod; `tide-push` keeps remote creds)
- [ ] **MIG-02**: Child-CRD emission from the LangGraph image is schema-constrained structured output (the verifier's `ToolStrategy` pattern pointed at `ChildCRDSpec`) — replacing prompt-and-parse for LangGraph-authored levels; the reporter path stays byte-identical for CLI-authored levels (mixed fleets)
- [ ] **MIG-03**: Per-rung runtime selection resolves through the existing config chain — `ResolveProvider` generalizes past hardcoded `"anthropic"` so `LevelConfig`/chart defaults can select vendor per level, replacing the three hand-rolled inline `Vendor: "langgraph"` sites
- [ ] **MIG-04**: Each planner rung promotes only on evidence — a shadow-pair comparison (both runtimes dispatch the same fixture; comparative judge on the Phase-49 verdict schema) gated through the System loop's promotion machinery (SYS-02), with per-rung rollback to the CLI runtime
- [ ] **MIG-05**: The task-executor rung migrates last, gated on new agent-loop-quality eval dimensions (tool-loop behavior, commit protocol fidelity) added to the harness BEFORE the rung is judged — the research-flagged build gap, closed not skipped
- [ ] **MIG-06**: The CLI-deprecation decision is a recorded artifact — a decision doc driven by the accumulated rung evidence (parity table per role, cost deltas, incident list), with the chosen posture (deprecate / retain-as-fallback / retain) wired into chart defaults

### Multi-Provider Endgame (PROV)

- [ ] **PROV-01**: The authoring image resolves its model via `init_chat_model` so provider selection is config, not code — Anthropic + OpenAI (`langchain-openai`, native `ProviderStrategy` structured output) both dispatchable through the same envelope
- [ ] **PROV-02**: credproxy is provider-aware — per-provider upstream base URLs and billing-exhaustion classification (today: singular `UpstreamBaseURL`, Anthropic-string matcher), with the existing HMAC/token flow preserved
- [ ] **PROV-03**: A per-provider conformance suite proves the Phase-49 golden verdict fixture and the MIG-02 child-CRD emission round-trip on every enabled provider BEFORE that provider ships — structured-output mechanisms differ (tool-forcing vs `json_schema`); fail-closed on nonconformance
- [ ] **PROV-04**: New provider pricing rows are empirically probed (the CACHE-01/pricing-drift lesson: never assume) and covered by the drift-check mechanism before a provider can dispatch billable work

### Product Loop (PROD)

- [ ] **PROD-01**: The Product loop closes at the project/milestone boundary as a five-element loop — the artifact tree is judged against the Project's outcome prompt (independent evaluator, verdict on the shared schema); a REPAIRABLE-class verdict drives re-planning by authoring a NEW Milestone (never reopening the D-07 `maxIterations=0` clamp; approve-at-descent preserved)
- [ ] **PROD-02**: Product-loop re-planning is bounded and thrash-guarded — `LoopPolicy`-bounded iterations, severity-weighted non-improvement halt (the Phase-52 stall-detection precedent one level up), and paid-work preservation (re-plan scopes deltas, never discards completed subtrees wholesale)
- [ ] **PROD-03**: The Product loop is proven by a live billable run on kind — outcome-judged red → new-milestone re-plan → converge/halt — before the milestone closes (the per-new-loop live-proof rule)

### System Loop (SYS)

- [ ] **SYS-01**: Generic eval-gated promotion machinery exists — a candidate (template/prompt/runtime/config change) is dispatched through the eval harness against a trailing baseline, promoted only on evidence, with a TESTED rollback path; candidate identity/version and experiment outcome are recorded artifacts
- [ ] **SYS-02**: The LangGraph rung ladder (MIG-04/05) rides SYS-01 as its first consumer — one promotion mechanism, not a parallel bespoke system
- [ ] **SYS-03**: System-loop anti-gaming is structural — a candidate change that also touches its own evaluator/baseline/fixtures in the same experiment is a system escalation, never a promotion (the Task-loop actor-separation invariant one level up), enforced by manifest intersection not prompt
- [ ] **SYS-04**: Eval/shadow spend is budget-capped through the existing reservation accounting — System-loop experiments and MIG shadow-pairs reserve and settle like any dispatch; a capped eval budget halts experiments, never production work

### Oversight Loop (OVR)

- [ ] **OVR-01**: `LoopPolicy.Autonomy` is consumed — gate policy resolves from loop level + risk tier + confidence + measured track record via an extension of the single `ResolveLoopPolicy` resolver (no per-controller switches), ML-model-free this milestone
- [ ] **OVR-02**: Autonomy adjustment is asymmetric and evidence-gated — down-fast on failure, up-slow with minimum-sample-size/window gating; LLM self-reported confidence is never an input (only TIDE's own measured pass/fail history)
- [ ] **OVR-03**: Track-record state respects LOOP-03 — rolling summaries on status (bounded counters/windows), never history arrays in etcd; full history lives in traces/artifacts
- [ ] **OVR-04**: Oversight behavior is proven live — a track-record-driven gate-policy change (auto→approve after failures; approve→auto after sustained passes at low risk) observed on a real cluster with the human override always available
- [ ] **OVR-05**: Every level's CRD carries classifier feature fields — at minimum a `deterministic | non-deterministic` classification of the node's verifiability (authored/derived at authoring time), plus the bounded scalar features the heuristic resolver already consumes (risk tier, confidence bucket, outcome) — schema-validated enums, LOOP-03-compliant (no feature history arrays in etcd)
- [ ] **OVR-06**: Every loop iteration and gate decision emits a training-ready labeled record — classifier features + the eventual measured outcome label — to the artifact store/trace stream (not etcd), building the corpus future ML classifiers train on; no model training or serving this milestone

### Dynamic Workflows (FAN)

- [ ] **FAN-01**: A shared N-way fan-out + reduce primitive exists — one logical node dispatches N sibling Jobs with a reduce/aggregation step, generalizing the existing `ChildCount` succession-gating; waves stay derived, no runtime DAG mutation; the MIG-04 shadow-pair is its degenerate N=2 consumer
- [ ] **FAN-02**: Cost/OOM rails land in the SAME phase as the first fan-out shape — per-shape `maxShape` bounds + a per-wave aggregate cap + reservation accounting for every fanned Job, proven under load on kind (run-2b OOM precedent) before any second shape ships
- [ ] **FAN-03**: Adversarial verification runs at verify seams — N independent refuter-evaluators with a deterministic reduce policy (quorum/dominance rules recorded in config), diversity-aware (distinct prompts/lenses), riding the existing verdict schema
- [ ] **FAN-04**: Generate-and-filter runs at planner seams — N candidate artifacts generated in parallel, judged on the shared verdict schema, winner proceeds (with runner-up salvage recorded), bounded by FAN-02 rails
- [ ] **FAN-05**: Tournament selection is available as an explicit cost-gated opt-in — bracket (O(n)) not round-robin, budget precomputed from the `N·(1+K)+1` shape and reserved up front, never a default posture

### Observability & Proof (OBS — continues v1.0.9 numbering)

- [ ] **OBS-05**: The shipped `SelfInstruments("langgraph")` zero-spans gap is closed — OpenInference LangChain instrumentation + OTLP exporter added to both LangGraph images, spans verified live parenting correctly under the W3C traceparent contract
- [ ] **OBS-06**: Every new loop and fan-out shape emits loop-native provenance on the existing `loop.*`/`evaluation.*` conventions — Product/System/Oversight iterations and fan-out sibling groups are queryable in Phoenix without bespoke instrumentation

## Future Requirements

Deferred to later milestones:

- **Gemini third provider** — dual CA-trust path (`SSL_CERT_FILE` + `REQUESTS_CA_BUNDLE`) unverified through credproxy; structured-output support MEDIUM confidence. Needs its own build spike first.
- **Product-loop external-signal daemon** — continuous backlog triage (GitHub-issues-style) feeding the Product loop; the five-loop model's long-run vision.
- **ML-based Oversight scoring** — training/serving confidence-risk classifiers beyond the heuristic resolver (staged-maturity precedent). The feature schema and labeled-data corpus land THIS milestone (OVR-05/06); the models train on it later.
- **GPG signing (SIGN-02/03/04)** and **CACHE-F1 direct-SDK caching** — carried forward unchanged.

## Out of Scope

| Item | Reasoning |
|------|-----------|
| A second graph engine / cross-pod LangGraph `Send`/supervisor | Locked invariant: waves stay derived, cycles are bugs, no runtime DAG mutation — dynamism lives in-pod and at dispatch seams |
| deepagents dependency in the authoring image | Mock-filesystem abstraction fights real git writes; widens the audit surface of a prompt-injection-exposed write path |
| Hosted eval SaaS (LangSmith evals, promptfoo, DeepEval) | `internal/eval` + the shared verdict schema cover the need; no new external dependency for the System loop |
| Reopening the D-07 `maxIterations=0` clamp at phase/milestone/project | Product-loop re-planning routes through NEW milestone authoring; post-execution rework discards paid work |
| Round-robin tournament judging | O(n²) cost; bracket/cascade shapes only |

## Traceability

<!-- Filled by roadmap creation. Maps REQ-IDs to phases. -->

| Requirement | Phase | Status |
|-------------|-------|--------|
| MIG-01 | Phase 58 | Pending |
| MIG-02 | Phase 58 | Pending |
| MIG-03 | Phase 54 | Pending |
| MIG-04 | Phase 58 | Pending |
| MIG-05 | Phase 61 | Pending |
| MIG-06 | Phase 65 | Pending |
| PROV-01 | Phase 65 | Pending |
| PROV-02 | Phase 65 | Pending |
| PROV-03 | Phase 65 | Pending |
| PROV-04 | Phase 65 | Pending |
| PROD-01 | Phase 63 | Pending |
| PROD-02 | Phase 63 | Pending |
| PROD-03 | Phase 63 | Pending |
| SYS-01 | Phase 56 | Pending |
| SYS-02 | Phase 58 | Pending |
| SYS-03 | Phase 56 | Pending |
| SYS-04 | Phase 56 | Pending |
| OVR-01 | Phase 64 | Pending |
| OVR-02 | Phase 64 | Pending |
| OVR-03 | Phase 64 | Pending |
| OVR-04 | Phase 64 | Pending |
| OVR-05 | Phase 55 | Pending |
| OVR-06 | Phase 55 | Pending |
| FAN-01 | Phase 57 | Pending |
| FAN-02 | Phase 57 | Pending |
| FAN-03 | Phase 59 | Pending |
| FAN-04 | Phase 60 | Pending |
| FAN-05 | Phase 62 | Pending |
| OBS-05 | Phase 54 | Pending |
| OBS-06 | Phase 64 | Pending |

**Coverage:** 30/30 requirements mapped, 0 orphans, 0 duplicates.
