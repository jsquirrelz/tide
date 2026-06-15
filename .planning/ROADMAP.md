# Roadmap: TIDE — Topologically-Indexed Dependency Execution

## Milestones

- ✅ **v1.0.0 — Self-Hosting MVP** — Phases 1–11 (shipped 2026-06-11)
- ✅ **v1.0.1 — Orchestrator Trustworthiness + Telemetry Completion** — Phases 12–17 (shipped 2026-06-13)
- 🚧 **v1.0.2 — Ebb Tide: Token & Cost Optimization** — Phases 18–21 (in progress)
- 📋 **vNext — OpenAI Backend + Dogfood Run #2** — (planned; phases TBD)
- 📋 **v1.x — Polyglot Subagent Runtimes: LangGraph Strategy** — (backlog; architecture locked, phases TBD) — [framing doc](milestones/v1.x-polyglot-subagent-MILESTONE.md)

## Phases

<details>
<summary>✅ v1.0.0 — Self-Hosting MVP (Phases 1–11) — SHIPPED 2026-06-11</summary>

14 phase directories (11 planned + 02.1/02.2/04.1/10/11 inserted) · 137 plans · 965 commits · ~66k LOC Go. Six CRDs + layered-Kahn waves + pluggable subagent dispatch + gates/observability/dashboard/CLI + Helm distribution; release published (binaries, 7 images, 2 OCI charts).

Full archive: [milestones/v1.0.0-ROADMAP.md](milestones/v1.0.0-ROADMAP.md) · [milestones/v1.0.0-REQUIREMENTS.md](milestones/v1.0.0-REQUIREMENTS.md)

</details>

<details>
<summary>✅ v1.0.1 — Orchestrator Trustworthiness + Telemetry Completion (Phases 12–17) — SHIPPED 2026-06-13</summary>

- [x] Phase 12: Gate Semantics + Reject/Resume (5/5 plans) — completed 2026-06-11
- [x] Phase 13: Dispatch Image Resolution + Provider Halt (7/7 plans) — completed 2026-06-11
- [x] Phase 14: Budget Enforcement + Pricing (7/7 plans) — completed 2026-06-12
- [x] Phase 15: Paper Cuts (7/7 plans) — completed 2026-06-12
- [x] Phase 16: Telemetry Completion (8/8 plans) — completed 2026-06-12
- [x] Phase 17: Tech Debt — Plan Label Backfill + Gate Hardening (4/4 plans) — completed 2026-06-13

38 plans · 46 tasks · 28/28 requirements satisfied (milestone audit: passed).

Full archive: [milestones/v1.0.1-ROADMAP.md](milestones/v1.0.1-ROADMAP.md) · [milestones/v1.0.1-REQUIREMENTS.md](milestones/v1.0.1-REQUIREMENTS.md) · [milestones/v1.0.1-MILESTONE-AUDIT.md](milestones/v1.0.1-MILESTONE-AUDIT.md)

</details>

### 🚧 v1.0.2 — Ebb Tide: Token & Cost Optimization (In Progress)

**Milestone Goal:** Cut TIDE's per-run token spend without degrading output quality — the cost-reduction prep that makes a second TIDE-on-TIDE dogfood run affordable.

- [ ] **Phase 18: Eval Harness** - Freeze a v1.0.1 baseline and build the quality gate before any template change
- [x] **Phase 19: Template Reorder + Token Minimization** - Reorder all five templates stable-prefix-first and trim non-essential boilerplate, gated by the harness
- [ ] **Phase 20: SharedContext Injection + Cache Verification Spike** - Spike cross-pod cache scoping, then add SharedContext to grow the cacheable shared prefix (or reframe to token-minimization-only)
- [ ] **Phase 21: Cost & Cache Observability** - Surface per-level token accounting and cache-hit metrics on the dashboard

## Phase Details

### Phase 18: Eval Harness

**Goal**: A frozen v1.0.1 baseline and deterministic quality gate exist, so every subsequent template or prompt change can be measured and regression-gated without manual review.
**Depends on**: Phase 17 (v1.0.1 shipped)
**Requirements**: EVAL-01, EVAL-02, EVAL-03, EVAL-04, EVAL-05, EVAL-06
**Success Criteria** (what must be TRUE):

  1. A maintainer runs `make test-unit` and the golden-file snapshot tests confirm that all five rendered templates match the committed `testdata/baselines/` snapshots (zero-network, deterministic).
  2. A PR that grows any template's token count beyond the tuned threshold fails CI automatically, without requiring manual inspection.
  3. A PR that breaks child-CRD parse success, declared output-path presence, or DAG acyclicity is caught by the protocol-compliance gate in `make test-unit`.
  4. `make eval` (behind `//go:build eval`) counts tokens via the Anthropic `count_tokens` endpoint through the existing credproxy using stdlib `net/http`, and prints per-template counts.
  5. Cost deltas computed by the harness delegate to the existing `estimatedCostCents` function and report REALIZED savings per wave (cache-write premium subtracted), not gross per-dispatch read discount.

**Plans**: 3 plans

Plans:
**Wave 1**

- [x] 18-01-PLAN.md — Freeze v1.0.1 baseline: eval package + goldie golden renders + byte ratchets (EVAL-01/03/06)
- [x] 18-03-PLAN.md — cmd/tide-eval count_tokens pre-flight + make eval target (EVAL-05)

**Wave 2** *(blocked on Wave 1 completion)*

- [x] 18-02-PLAN.md — Deterministic protocol-compliance gate + estimatedCostCents parity/realized-savings (EVAL-02/04)

### Phase 19: Template Reorder + Token Minimization

**Goal**: All five prompt templates are restructured stable-prefix-first so wave-sibling dispatches share an identical cache-eligible prefix, and non-essential boilerplate is trimmed — each change gated green by the Phase 18 eval harness.
**Depends on**: Phase 18
**Requirements**: PROMPT-01, PROMPT-02, PROMPT-03, PROMPT-04, PROMPT-05
**Success Criteria** (what must be TRUE):

  1. All five templates follow the order: role preamble → fixed instructions → shared context → volatile metadata → per-task prompt; the `{{.TaskUID}}` and `{{.Provider.*}}` fields appear only in the volatile suffix.
  2. Every template section carries a "why-this-line" annotation committed before any trim, so load-bearing instructions (proven by prior production cascades) are identifiable and preserved.
  3. Structured data interpolated into the stable prefix is serialized with stable key order so identical inputs render identical bytes (deterministic diffing under goldie).
  4. `make test-unit` runs green after each boilerplate-trim commit, confirming protocol-compliance (child-CRD parse, output paths, DAG acyclicity) is preserved throughout.
  5. The golden baselines in `testdata/baselines/` are updated to reflect the reordered templates, and the token-count ratchet confirms no accidental growth relative to the trimmed target.

**Plans**: 4 plans

Plans:
**Wave 1**

- [x] 19-01-PLAN.md — Annotate/reorder/trim milestone_planner + project_planner (golden+ratchet)
- [x] 19-02-PLAN.md — Annotate/reorder/trim phase_planner + plan_planner (FILE-TOUCH/JSON-escape preserved)
- [x] 19-03-PLAN.md — Annotate/reorder/trim task_executor (consolidate 6 UID occurrences to volatile suffix)

**Wave 2** *(blocked on Wave 1 completion)*

- [x] 19-04-PLAN.md — PROMPT-05 regression guard + full make test gate + D-05 human-review checkpoint

**Cross-cutting constraints:**

- The STABLE PREFIX (everything before the `SharedContext slot` marker) of each template contains ZERO {{.TaskUID}}, {{.Provider.*}}, `Level:`, or `Role:` tokens; all {{.TaskUID}} occurrences are concentrated in the volatile suffix AFTER the marker (the suffix legitimately has 2: the `TaskUID:` label line + the task-dir path-mapping line, per D-01)
- No {{.Provider.*}}, {{.Level}}, or {{.Role}} interpolation remains anywhere in either template
- Every surviving load-bearing line carries a {{/* WHY ... */ -}} annotation committed before any trim
- Locked decisions implemented by this plan: D-02 (drop printed Level/Role/Provider.* lines; .Provider stays on the struct), D-04 (conservative trim — pure redundancy/formatting slack only, every directive's semantics preserved), D-06 (inline {{/* WHY */ -}} annotations for surviving load-bearing lines; removal rationale in commit message), D-07 (bare {{/* SharedContext slot */}} marker, no trim markers)
- go test ./internal/eval/ exits 0 at every commit boundary

### Phase 20: SharedContext Injection + Cache Verification Spike

**Goal**: A spike verifies whether stable-prefix-first ordering yields cross-pod prefix-cache hits across wave siblings under `claude -p --bare`; if it does, `EnvelopeIn` gains a `SharedContext` field populated identically for all wave siblings to grow the shared cacheable prefix to ≥1,024 tokens; if it does not, this phase closes as best-effort token minimization with the spike decision recorded.
**Depends on**: Phase 19
**Requirements**: CACHE-01, CACHE-02, CACHE-03, CACHE-04, CACHE-05
**Notes/Risks**: CACHE-01 is a verification spike that gates CACHE-02/03. If cross-pod caching does not fire (because the CLI embeds a working-directory path that differs per pod, making each dispatch's prefix unique), CACHE-02/03 reframe to token-minimization-only and the SharedContext field is still added but only for reducing context size — not for cache benefit. This contingency must be explicitly recorded as a decision in PROJECT.md regardless of outcome.
**Success Criteria** (what must be TRUE):

  1. The spike result is committed as a decision in PROJECT.md — either "cross-pod prefix-cache hits confirmed under `claude -p --bare`" or "cross-pod caching does not fire; reframed to token-minimization-only."
  2. `EnvelopeIn` gains a `SharedContext string` (omitempty) field that the executor path ignores, and `BuildPlannerEnvelope` populates it identically for all tasks in a wave.
  3. Planner templates reference `{{.SharedContext}}` in the stable prefix section; the combined stable prefix (role + instructions + SharedContext) reaches ≥1,024 tokens on Sonnet/Opus (or the Haiku gap is documented explicitly).
  4. SharedContext is populated from curated summaries rather than verbatim PLAN.md / phase-brief dumps, so the token growth is efficient rather than additive bulk.
  5. The design carries no Anthropic-only assumptions: SharedContext is a field on the provider-agnostic `EnvelopeIn`, and the decision record notes OpenAI/Codex parity is deferred to the next milestone.

**Plans**: 5 plans

Plans:
**Wave 1**

- [x] 20-01-PLAN.md — SharedContext fields on EnvelopeIn/Out, ChildCRDSpec, and all four CRD specs (CACHE-02 contract)
- [x] 20-04-PLAN.md — tide-spike cross-pod cache harness + credproxy FAIL-path body tee (CACHE-01)

**Wave 2** *(blocked on Wave 1 completion)*

- [x] 20-02-PLAN.md — {{.SharedContext}} interpolation in four planner templates + golden/ratchet re-baseline (CACHE-03)
- [ ] 20-03-PLAN.md — BuildPlannerEnvelope stamp + materializer byte-identical carry + size cap + executor-omit lock (CACHE-02/04)

**Wave 3** *(blocked on Wave 2 completion)*

- [ ] 20-05-PLAN.md — live spike run, PROJECT.md decision, provider-neutrality, D-08 contingency (CACHE-01/05)

**UI hint**: no

### Phase 21: Cost & Cache Observability

**Goal**: Per-level token accounting and cache efficiency are visible on the dashboard so operators can observe the results of the Phase 18–20 work in a running cluster.
**Depends on**: Phase 18 (metric names confirmed); can parallelize with Phases 19–20 once Phase 18 is complete
**Requirements**: OBSV-01, OBSV-02, OBSV-03
**Success Criteria** (what must be TRUE):

  1. The existing Prometheus token counters carry labels that attribute spend per level (project/phase/plan/wave), queryable via PromQL without additional instrumentation.
  2. A `tide_cache_hit_rate` (or equivalent) gauge derived from `cache_read` vs `cache_creation` dispatch usage is emitted via the existing Prometheus surface and visible in the TelemetryView.
  3. The read-only dashboard's TelemetryView includes a cache-efficiency panel displaying hit ratio, cache-creation tokens, and realized savings — reading the existing counters with no backend dispatch-path changes.

**Plans**: TBD
**UI hint**: yes

<details>
<summary>📋 vNext — OpenAI Backend + Dogfood Run #2 (Planned)</summary>

Scope TBD. Extends credproxy route allowlist for OpenAI paths, wires an OpenAI provider into the dispatch chain, and runs dogfood run #2. Sequenced after v1.0.2 "Ebb Tide."

</details>

<details>
<summary>📋 v1.x — Polyglot Subagent Runtimes: LangGraph Strategy (Backlog)</summary>

Architecture locked; task breakdown deferred. The `claude` CLI subagent becomes one named strategy behind the existing `pkg/dispatch.Subagent` image contract; a second Python/LangGraph container image implements the same envelope contract for full agent-loop parity. Sequenced after v1.0.2 "Ebb Tide" and after the OpenAI-backend milestone.

See [milestones/v1.x-polyglot-subagent-MILESTONE.md](milestones/v1.x-polyglot-subagent-MILESTONE.md) for the full framing: parity inventory, contract-conformance table, provider-firewall gap analysis, alternatives considered, and open questions.

</details>

## Progress

| Phase | Milestone | Plans Complete | Status | Completed |
|-------|-----------|----------------|--------|-----------|
| 1–11 (see archive) | v1.0.0 | 137/137 | Complete | 2026-06-11 |
| 12. Gate Semantics + Reject/Resume | v1.0.1 | 5/5 | Complete | 2026-06-11 |
| 13. Dispatch Image Resolution + Provider Halt | v1.0.1 | 7/7 | Complete | 2026-06-11 |
| 14. Budget Enforcement + Pricing | v1.0.1 | 7/7 | Complete | 2026-06-12 |
| 15. Paper Cuts | v1.0.1 | 7/7 | Complete | 2026-06-12 |
| 16. Telemetry Completion | v1.0.1 | 8/8 | Complete | 2026-06-12 |
| 17. Tech Debt — Plan Label Backfill + Gate Hardening | v1.0.1 | 4/4 | Complete | 2026-06-13 |
| 18. Eval Harness | v1.0.2 | 3/3 | Complete    | 2026-06-15 |
| 19. Template Reorder + Token Minimization | v1.0.2 | 4/4 | Complete   | 2026-06-15 |
| 20. SharedContext Injection + Cache Verification Spike | v1.0.2 | 3/5 | In Progress|  |
| 21. Cost & Cache Observability | v1.0.2 | 0/TBD | Not started | - |
