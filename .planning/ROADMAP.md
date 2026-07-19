# Roadmap: TIDE — Topologically-Indexed Dependency Execution

## Milestones

- ✅ **v1.0.0 — Self-Hosting MVP** — Phases 1–11 (shipped 2026-06-11) — ⚠ shipped on an invalid execution foundation (per-plan waves; see v1.0.2 Spring Tide)
- ✅ **v1.0.1 — Orchestrator Trustworthiness + Telemetry Completion** — Phases 12–17 (shipped 2026-06-13) — ⚠ same invalid foundation
- ⊘ **v1.0.2 — Ebb Tide: Token & Cost Optimization** — Phases 18–21 (completed; **SUPERSEDED — will not be released**, artifacts preserved). Superseded after dogfood run #2 surfaced the per-plan-waves defect.
- ✅ **v1.0.2 — Spring Tide: Global Execution DAG (severe corrective patch)** — Phases 22–26 (complete; **shipped within the v1.0.3 tag**, not separately tagged). Re-architected execution to ONE global Execution DAG spanning the entire Project — the patch that makes the Topologically-Indexed paradigm real. Superseded Ebb Tide.
- ✅ **v1.0.3 — Spring Tide + Planning Resumption & Cost Resilience** — Phases 22–29 (shipped 2026-06-25, tag `v1.0.3`, published: 7 images + 2 OCI charts). Global Execution DAG end-to-end (22–26) + operator resumption tooling (27–29): budget-bypass resume correctness, plan-import core, and `tide` export/import-envelopes with a kind E2E acceptance gate.
- ✅ **v1.0.4 — tide-import image publish + release-matrix guardrail** — (shipped 2026-06-25, tag `v1.0.4`, published). Patch: publishes the `tide-import` image in the build-images matrix and adds the matrix↔chart image-coverage release gate.
- ✅ **v1.0.5 — Resumable Import: Partial-Tree Resume** — Phase 30 (shipped 2026-06-27, tag `v1.0.5`, published: 8 images + 2 OCI charts + 5 binaries @ 1.0.5, verified anon). adopt-complete + re-plan-incomplete: fixes the v1.0.3 import defect dogfood run #2 surfaced (incomplete-envelope nodes materialized as `Running`-with-no-envelope zombies → stall). Unblocked deferred dogfood run #2. Full archive: [milestones/v1.0.5-ROADMAP.md](milestones/v1.0.5-ROADMAP.md) · [milestones/v1.0.5-REQUIREMENTS.md](milestones/v1.0.5-REQUIREMENTS.md).
- ✅ **v1.0.6 — Adoption-Path Correctness & Dispatch Safety** — Phases 31–33 (shipped 2026-06-29, tag `v1.0.6`, published: 8 images + 2 OCI charts + 5 binaries @ 1.0.6, verified anon). Corrective patch closing the four code-level defects dogfood run #2b surfaced on the adoption path: D1+D2 lifecycle/cost seam (Phase 31), D3 dispatch concurrency cap (Phase 32), D4 planner failure semantics (Phase 33). Audit: tech_debt (13/13 reqs, 0 blockers). Full archive: [milestones/v1.0.6-ROADMAP.md](milestones/v1.0.6-ROADMAP.md) · [milestones/v1.0.6-REQUIREMENTS.md](milestones/v1.0.6-REQUIREMENTS.md) · [milestones/v1.0.6-MILESTONE-AUDIT.md](milestones/v1.0.6-MILESTONE-AUDIT.md).
- ✅ **v1.0.7 — First-Run Paper Cuts: Run Integrity & Operator Ergonomics** — Phases 34–41 (shipped 2026-07-15, tag `v1.0.7`). Closed what the first external-repo run (2026-07-03) surfaced short of new subagent stages: the silent wave-parallel integration miss, the 2.8× Claude-5 budget overcount, git ergonomics (baseRef, agent identity, promptFile), dashboard blind spots (artifact view at approve gates, project view, log-drawer states), the Prometheus setup step, and the v1.0.6 audit tech-debt carry — plus the Phase 40 full API version-lifecycle crank (v1alpha3 sole version) and the Phase 41 12-item refactoring review. 44 requirements, 44 satisfied; audit tech_debt, 0 blockers. Full archive: [milestones/v1.0.7-ROADMAP.md](milestones/v1.0.7-ROADMAP.md) · [milestones/v1.0.7-REQUIREMENTS.md](milestones/v1.0.7-REQUIREMENTS.md) · [milestones/v1.0.7-MILESTONE-AUDIT.md](milestones/v1.0.7-MILESTONE-AUDIT.md).
- ✅ **v1.0.8 — Phoenix Rising: OpenInference Trace Emission + Self-Hosted Phoenix** — Phases 42–47 (shipped 2026-07-17, tag `v1.0.8`, published: 8 images + 2 OCI charts + 5 binaries, verified anon). TIDE runs are observable in a self-hosted Arize Phoenix — the Milestone→Phase→Plan→Task dispatch chain emits real OpenInference/OTel spans (dispatch-chain AGENT spans, full LLM message arrays, W3C traceparent propagation), a documented self-hosted Phoenix recipe wires the chart’s existing OTLP endpoint end-to-end, and a runtime-neutral adapter seam keeps the trace contract stable ahead of the LangGraph beachhead. Live PROOF-01: a 392-span five-level trace tree, human-signed-off. Full archive: [milestones/v1.0.8-ROADMAP.md](milestones/v1.0.8-ROADMAP.md) · [milestones/v1.0.8-REQUIREMENTS.md](milestones/v1.0.8-REQUIREMENTS.md).
- 🚧 **v1.0.9 — Slack Tide: The Task Loop (Verification-Driven Quality Iteration)** — Phases 48–53 (roadmapped 2026-07-18) — TIDE closes its first real feedback loop: each Task's artifact is checked by an independent read-only LangGraph evaluator, and a repairable failure drives a fresh attempt with a compact evidence packet, bounded by a `LoopPolicy`, escalating to a human on exhaustion. Ships on a minimal common loop contract (`LoopPolicy`/`LoopStatus`) the wider [five-loop model](notes/five-loop-model.md) reuses, plus Execution-loop hardening and loop-native observability. Supersedes the "vNext — Specialist Verify Tier" framing below (reframed 2026-07-18 from a gate that halts to a loop that closes). See PROJECT.md "Current Milestone" for full detail.
- ⊘ **vNext — Specialist Verify Tier + LangGraph Beachhead** — (scoped 2026-07-06 via /gsd:explore) — **SUPERSEDED by v1.0.9 "Slack Tide"** (reframed 2026-07-18: verification is not a gate that halts, it's the feedback signal that closes a loop; the plan-check/level-verify/integration-check three-stage framing collapsed into ONE loop contract parameterized per level). Framing preserved for reference — [framing doc](milestones/vnext-specialist-verify-MILESTONE.md) · [strategy note](notes/langgraph-successor-runtime-strategy.md)
- 📋 **v1.x — LangGraph Authoring Migration (evidence-gated)** — (backlog; reframed 2026-07-06 from "Polyglot Subagent Runtimes: LangGraph Strategy") — planner roles migrate first, executor last, each rung gated on eval-harness evidence; endgame = CLI-deprecation decision + multi-provider via `init_chat_model`, dissolving the standalone OpenAI backend — [framing doc](milestones/v1.x-polyglot-subagent-MILESTONE.md) · [strategy note](notes/langgraph-successor-runtime-strategy.md)
- 📋 **vLater — Dogfood Run #2 (retarget pending)** — (original deliverable — TIDE builds the OpenAI backend — dissolved by multi-provider-via-LangGraph; new build target chosen at scoping; still gated on multi-node infrastructure) — archived Flood Tide phase details remain a starting point: [milestones/v1.0.7-floodtide-ROADMAP.md](milestones/v1.0.7-floodtide-ROADMAP.md)

## Phases

**Phase Numbering:**

- Integer phases (48, 49, 50...): Planned milestone work
- Decimal phases (48.1, 48.2): Urgent insertions (marked with INSERTED)

Phase numbering continues from v1.0.8 (last phase was 47); v1.0.9 starts at Phase 48.

- [x] **Phase 48: LangGraph Evaluator Image + Credproxy-TLS Spike** - Read-only LangGraph image behind the unchanged Subagent seam; live TLS trust spike de-risks everything downstream (completed 2026-07-18)
- [x] **Phase 49: Common Loop Contract + Verdict/Envelope/Persistence Schema** - `LoopPolicy`/`LoopStatus` + `gate_decision` schema + findings size×locality persistence, locked before any consumer logic (completed 2026-07-18)
- [ ] **Phase 50: Execution-Loop Hardening + Loop-Native Observability** - `loopRunID`/terminal reasons/run-evidence envelope + `loop.*`/`evaluation.*` spans the Task loop will consume
- [ ] **Phase 51: The Task Loop** - Independent-evaluator-driven verification contract, fresh-attempt-with-evidence-packet, three-tier escalation, concurrency/tracing safety wired at the same dispatch sites
- [ ] **Phase 52: Per-Level LoopPolicy Parameterization** - The same verification contract runs at every level as a `LoopPolicy` parameterization — plan-check re-plan, Phase/Milestone/Project escalation
- [ ] **Phase 53: Chart Config + Dashboard Provenance Surfacing** - Chart-first per-level defaults (safe on upgrade) + nested loop provenance on the dashboard

## Phase Details

### Phase 48: LangGraph Evaluator Image + Credproxy-TLS Spike

**Goal**: A minimal read-only Python/LangGraph evaluator image runs behind the unchanged `pkg/dispatch.Subagent` + envelope seam, and its credproxy TLS trust path is proven live — de-risking the new runtime's trust seam before any evaluation/verdict logic is built on top of it.
**Depends on**: Phase 47 (v1.0.8, last shipped phase)
**Requirements**: EVAL-01, EVAL-02
**Success Criteria** (what must be TRUE):

  1. A minimal read-only Python/LangGraph container dispatches through the unchanged `pkg/dispatch.Subagent` + envelope seam, with git-read + bash gate-command tools only (no file-edit/commit/push tools, no checkpointer).
  2. A live pass/fail spike proves `SSL_CERT_FILE` alone (or the documented `http_client=`/`anthropic_client=` fallback) trusts credproxy's CA through the real `ChatAnthropic` construction path.
  3. An adversarial test attempting `git commit`/push against a fixture worktree fails at the mount/credential layer (ReadOnly worktree mount + omitted git-write/push credentials) — not merely by prompt refusal.
  4. Every Python dependency is patch-exact pinned, and a CI gate rejects any unpinned/range specifier.

**Plans**: 5 plans

Plans:
**Wave 1**

- [x] 48-01-PLAN.md — Python scaffolding: patch-exact pins + hash-locked lockfiles, pytest infra (Wave 0), `make verify-langgraph-pins` CI gate
- [x] 48-02-PLAN.md — Read-only jobspec variant: `ReadOnly bool` on BuildOptions + TestBuildJobSpec_Verifier_* static/credential-absence assertions (D-08/D-09a)

**Wave 2** *(blocked on Wave 1 completion)*

- [x] 48-03-PLAN.md — Verifier runtime: envelope wire-shape re-implementation, git_read + run_gate_command tools, create_agent loop + fail-closed entrypoint

**Wave 3** *(blocked on Wave 2 completion)*

- [x] 48-04-PLAN.md — Image build (digest-pinned, --require-hashes) + release-matrix wiring + D-09b adversarial behavioral test (`make test-verifier-readonly`)

**Wave 4** *(blocked on Wave 3 completion)*

- [x] 48-05-PLAN.md — Live credproxy-TLS spike (plain ChatAnthropic, SSL_CERT_FILE alone) + recorded verdict artifact gating Phase 49 (checkpoint)

**Research flag**: yes — the TLS spike outcome is genuinely unknown until executed live; the fallback contingency is designed against the MEASURED error at the 48-05 checkpoint (D-07 revised post-research: the `http_client=`/`anthropic_client=` hook does not exist at the pinned version — RESEARCH Pitfall A).

### Phase 49: Common Loop Contract + Verdict/Envelope/Persistence Schema

**Goal**: The `LoopPolicy`/`LoopStatus` shared API types, the `gate_decision` verdict schema, and the findings size×locality persistence contract are locked as shared, reusable primitives — before any halt-condition or reconciler logic is written on top of them.
**Depends on**: Phase 48
**Requirements**: LOOP-01, LOOP-02, LOOP-03, EVAL-03, EVAL-05
**Success Criteria** (what must be TRUE):

  1. `LoopPolicy` (MaxIterations/MaxDuration/BudgetCents/Autonomy/EvaluatorRef/EscalationPolicy) and `LoopStatus` (Iteration/ParentRunID/LastEvaluation/ExitReason/CostCents/Conditions) exist as shared Go API types embeddable in any domain CRD, with type-level doc-comments applying the five-element loop test (goal/candidate/evaluator-feedback/repeat-policy/bounded-exit) — no generic `Loop` controller exists.
  2. A `VerifyContext` pointer field on `EnvelopeIn` and a matched Go+Pydantic `GateDecision`/`Finding` pair (`APPROVED | REPAIRABLE | BLOCKED` + `findings[]` with dimension/severity/confidence/evidence/suggested_fix) round-trip through the envelope seam.
  3. An unparseable, empty, or partially-validated verdict is classified fail-closed (never collapses to APPROVED), proven by a regression test covering empty-JSON, missing-verdict-field, and malformed shapes.
  4. Findings persist under the size×locality rule — a ≤4KB summary on `TerminationStub`, a small per-CRD status summary, and the full findings artifact staged onto the run branch via the extended `collectStageEnvelopes` — never an etcd blob, never a new PVC path.
  5. A size test proves `LoopStatus` on any consuming CRD carries only the current-iteration summary + exit reason, never an accumulating iteration history.

**Plans**: 4 plans

Plans:
**Wave 1**

- [x] 49-01-PLAN.md — `api/v1alpha3/loop_types.go`: `LoopPolicy`/`LoopStatus`/`EvaluationSummary` standalone types + five-element doc-comments (D-06) + LOOP-03 compile-time structural guard + synthetic-embedder proof; zero `make manifests` diff (LOOP-01/02/03)
- [x] 49-02-PLAN.md — `pkg/dispatch/verdict.go`: `Verdict`/`Finding`/`GateDecision` + fail-closed `ClassifyVerdict` + canonical golden fixture; `VerifyContext` on `EnvelopeIn`, `Verdict` on `EnvelopeOut`, bounded verdict summary on `TerminationStub` (EVAL-03 Go half, EVAL-05a)
- [x] 49-04-PLAN.md — `stageEnvelopeArtifacts` glob generalization (task-kind stages `findings.json`-only, planner `*.md` hard-fail preserved) + regression test; `collectStageEnvelopes` untouched (EVAL-05c)

**Wave 2** *(blocked on 49-02 — the shared golden fixture + verdict JSON shape)*

- [x] 49-03-PLAN.md — Python parity: `verifier/verdict.py` pydantic `GateDecision`/`Finding` + mirrored `classify_verdict` reading the SAME golden fixture + `verify` extraction on `EnvelopeIn` + extended `write_termination_stub` (EVAL-03 Python half)

### Phase 50: Execution-Loop Hardening + Loop-Native Observability

**Goal**: The in-Job execution loop (a pipeline stage, not a loop) produces machine-checkable run evidence and emits the loop-native trace/metric attributes the Task loop will consume — before the Task loop is built on top of it.
**Depends on**: Phase 49
**Requirements**: EXEC-01, EXEC-02, EXEC-03, EXEC-04, OBS-01, OBS-02
**Success Criteria** (what must be TRUE):

  1. Every attempt carries a stable `loopRunID` + `attemptID` and emits a span per tool/action iteration.
  2. The result envelope carries an explicit terminal reason (`completed | cap_exceeded | blocked | tool_failure | invalid_output`) — never a silent default.
  3. The result envelope satisfies the run-evidence contract (`docs/templates/minimal-loop-project/evals/README.md`): Task+Spec IDs and locking commit, commands + evaluator versions executed, test/eval results, changed-file manifest, runtime/model/prompt version, cost/duration, terminal reason, bounded feedback — referenced, not re-derived.
  4. The envelope's completion field reports only that the agent *believes* the attempt is complete — no field or code path lets the Execution loop stamp Task correctness.
  5. Spans carry `loop.kind`/`loop.run_id`/`loop.parent_run_id`/`loop.iteration`/`loop.candidate_version`/`loop.exit_reason`/`evaluation.result`/`evaluation.version`/`human_intervention` plus cost/duration/token usage, and run IDs never appear in a Prometheus label — proven by a label-cardinality test.

**Plans**: 7 plans

Plans:
**Wave 1**

- [x] 50-01-PLAN.md — Envelope wire contract: `TerminalReason` enum + `RunEvidence` + loopRunID/attemptID fields + shared golden fixture + EXEC-04 no-correctness-field guard (Wave 1)
- [ ] 50-02-PLAN.md — `pkg/otelai` loop.*/evaluation.*/human_intervention attribute helpers, deliberately not tide.-prefixed (Wave 1)
- [ ] 50-03-PLAN.md — Prometheus cardinality dual guard: metriccardinality analyzer + wave_label_test extended to the 9-name run-ID-shaped forbidden list; no new metric (Wave 1)

**Wave 2** *(blocked on Wave 1 completion)*

- [ ] 50-04-PLAN.md — Executor write-site population: three real write sites set TerminalReason per mapping table, CheckCaps wired (in-pod cap_exceeded), bounded RunEvidence assembly, AST fail-closed guard (Wave 2)
- [ ] 50-05-PLAN.md — Python envelope mirror: envelope.py + test_envelope.py parity against the shared golden fixture (Wave 2)
- [ ] 50-06-PLAN.md — Controller identity stamping (buildEnvelopeIn), cap_exceeded synthesis for DeadlineExceeded-killed Jobs, AGENT-span loop.* attributes (Wave 2)

**Wave 3** *(blocked on Wave 2 completion)*

- [ ] 50-07-PLAN.md — Reporter LLM-span loop-identity threading: Args → tide-reporter flags → EmitSpans indexed loop.run_id/loop.iteration (Wave 3)

### Phase 51: The Task Loop

**Goal**: `TaskReconciler` drives a real verification-driven quality loop — a locked, planner-authored verification contract dispatches an independent LangGraph evaluator against the real gate command, and a repairable failure produces a fresh, evidence-seeded attempt bounded by `maxIterations`, with concurrency/tracing/halt safety wired at the same dispatch sites (not deferred to a follow-up phase).
**Depends on**: Phase 50
**Requirements**: TASK-01, TASK-02, TASK-03, TASK-04, TASK-05, TASK-06, EVAL-04, ESC-02, ESC-03, ESC-04, OBS-03
**Success Criteria** (what must be TRUE):

  1. `TaskSpec.verification` (planner-authored `commands`, `requiredArtifacts`, `evaluator`, `maxIterations`, `onExhaustion`, plus the resolved `GateCommand` field location) is immutable once locked (Draft→Locked→Superseded + version), and `git show <locking-sha>` reproduces exactly what was dispatched.
  2. A verification result classified REPAIRABLE creates a fresh attempt seeded with the original locked spec + a compact evidence packet (failures/diffs/test output) — never the prior agent's full context — while infra-retry (eviction/transient rerun of the same attempt) remains a distinct, preserved path.
  3. The evaluator dispatch (the LangGraph image, with its `SelfInstruments` vendor sentinel registered in this same phase) runs as a logically independent process from the implementation agent; a deterministic command failure in its findings always dominates — a Task can never pass on an LLM-judge APPROVED over a red gate command.
  4. The Task loop is bounded by `maxIterations` with `onExhaustion` routing to `ConditionVerifyHalt` (gating both planner and task tiers, mirroring `failure_halt.go` + the Phase 25 resume time-fence) as a halt class distinct from `Failed` wave semantics; its state is resumable across a controller restart; and a fresh attempt that edits fixtures/thresholds/the evaluator itself is flagged as a system escalation, never counted as a pass — the anti-gaming invariant is enforced, not documented.
  5. Evaluator dispatches count against the concurrency gate (extended `plannerInFlightCount` or a new `verifierInFlightCount`) and `LoopPolicy.BudgetCents` bounds cost via the existing reservation store — verified by a kind-cluster concurrent-dispatch test that stays under the sized cap.

**Plans**: TBD
**Research flag**: yes — two genuinely open calls gate this phase's plan: (1) where the per-level `GateCommand` ("pass criterion command") is declared in the CRD schema — a new `Plan.Spec`/`Project.Spec` field vs. a convention-based lookup, a real requirements decision with no existing source; (2) whether the LangGraph runtime needs a new `Vendor` sentinel (e.g. `"langgraph"`) in `pkg/dispatch.SelfInstruments`/`ResolveProvider(...).Vendor` or can reuse `"anthropic"` with a runtime discriminator — must be locked before this phase's `SelfInstruments` wiring.

### Phase 52: Per-Level LoopPolicy Parameterization

**Goal**: The same verification contract runs at every level — Task, Plan/plan-check, Phase/Milestone/Project — purely as different `LoopPolicy` parameterizations, with gate policy resolved from loop level rather than hierarchy position. Falls out cleanly once the contract (Phase 49) and the Task loop (Phase 51) exist.
**Depends on**: Phase 51
**Requirements**: ESC-01
**Success Criteria** (what must be TRUE):

  1. Plan/plan-check runs with `maxIterations:1` (its own counter, default 1, never shared with the Task loop's counter) against a goal-backward rubric (goal alignment, file-touch plausibility, dependency correctness, verification derivability) and applies severity-weighted stall detection before escalating.
  2. Phase/Milestone/Project run with `maxIterations:0` — any verify finding at these levels escalates straight to `requireApproval` rather than auto-repairing, because these levels close on their observable outcome, not task-completion.
  3. Gate policy is resolved from the loop-level field on `LoopPolicy`, not from CRD kind/hierarchy position — one resolver function serves all levels.

**Plans**: TBD

### Phase 53: Chart Config + Dashboard Provenance Surfacing

**Goal**: Operators configure the loop/verify tier through the existing chart-first precedence chain with a safe default posture, and the dashboard surfaces nested loop provenance plus a `VerifyHalt` state visually distinct from `Failed`.
**Depends on**: Phase 52
**Requirements**: CFG-01, CFG-02, OBS-04
**Success Criteria** (what must be TRUE):

  1. A chart-first config surface (evaluator image/model + per-level `LoopPolicy` defaults) follows the existing `subagent.levels`/`resolveImage` precedence chain, with `charts/tide/values.yaml` remaining the FIXED contract (binary catches up to chart).
  2. A fresh install gets Task-loop auto-repair + Plan/Milestone/Project escalation enabled at milestone+project scope by default; an in-place `helm upgrade` on an existing install leaves the verify/loop tier off — proven by an upgrade-path test.
  3. The dashboard shows nested loop provenance (Project run → Task iteration → Execution attempt/tool spans) and renders `VerifyHalt` as a visually distinct state from `Failed`, with staged findings browsable through the existing gitfetch/artifacts API (no new endpoint).

**Plans**: TBD
**UI hint**: yes

## Progress

**Execution Order:**
Phases execute in numeric order: 48 → 49 → 50 → 51 → 52 → 53

| Phase | Milestone | Plans Complete | Status | Completed |
|-------|-----------|----------------|--------|-----------|
| 1–11 (see archive) | v1.0.0 | 137/137 | Complete | 2026-06-11 |
| 12–17 (see archive) | v1.0.1 | 38/38 | Complete | 2026-06-13 |
| 18–21 (superseded) | v1.0.2 (Ebb) | 14/14 | Complete (superseded) | 2026-06-16 |
| 22–26 (see archive) | v1.0.2 (Spring Tide) | 19/19 | Complete | 2026-06-17 |
| 27–29 (see archive) | v1.0.3 | 14/14 | Complete | 2026-06-22 |
| 30 (see archive) | v1.0.5 | 3/3 | Complete | 2026-06-27 |
| 31–33 (see archive) | v1.0.6 | 8/8 | Complete | 2026-06-29 |
| 34–41 (see archive) | v1.0.7 | 51/51 | Complete | 2026-07-15 |
| 42–47 (see archive) | v1.0.8 | 32/32 | Complete | 2026-07-17 |
| 48. LangGraph Evaluator Image + Credproxy-TLS Spike | v1.0.9 | 5/5 | Complete   | 2026-07-18 |
| 49. Common Loop Contract + Verdict/Envelope/Persistence Schema | v1.0.9 | 4/4 | Complete    | 2026-07-18 |
| 50. Execution-Loop Hardening + Loop-Native Observability | v1.0.9 | 1/7 | In Progress|  |
| 51. The Task Loop | v1.0.9 | 0/TBD | Not started | - |
| 52. Per-Level LoopPolicy Parameterization | v1.0.9 | 0/TBD | Not started | - |
| 53. Chart Config + Dashboard Provenance Surfacing | v1.0.9 | 0/TBD | Not started | - |
