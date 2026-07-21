# Requirements — v1.0.9 Slack Tide: The Task Loop (Verification-Driven Quality Iteration)

**Defined:** 2026-07-18
**Core Value:** The five-level paradigm (Milestone → Phase → Plan → Task → Wave) runs as a real K8s orchestrator that can drive its own next milestone end-to-end.
**Milestone goal:** TIDE closes its first real feedback loop — the **Task loop**: each Task's artifact is checked by an independent evaluator (a read-only LangGraph image), and a repairable failure drives a *fresh attempt with a compact evidence packet*, bounded by a `LoopPolicy`, escalating to a human on exhaustion. Ships on a minimal common loop contract (`LoopPolicy`/`LoopStatus`) that the wider five-loop model reuses, plus Execution-loop hardening and loop-native observability.

Grounded in `.planning/research/` (SUMMARY/STACK/FEATURES/ARCHITECTURE/PITFALLS, committed `f85ee3d`), the [five-loop model](notes/five-loop-model.md), the committed **loop-engineering template pack** (`docs/templates/minimal-loop-project/` + the worked example under `examples/minimal-loop-project/`, PR #11 / `6f57eb5`), and scoping decisions confirmed at new-milestone: cut = **Task loop + shared contract** (Product/System/Oversight loops = named future arc); the pass-criterion command is a **planner-authored explicit field**; re-plan budget **N=1**; default posture **milestone+project scope, off on in-place upgrade**.

**Alignment principle:** TIDE's `TaskSpec.verification` CRD is the runtime embodiment of the committed markdown TASK contract (`docs/templates/minimal-loop-project/tasks/TASK.template.md`) — acceptance signals, prohibited-changes, evidence-required, and three-tier escalation converge on one model, so a TIDE-driven project's authored artifacts and the CRD it dispatches are the same shape. The canonical **run-evidence contract** lives in `evals/README.md`; requirements reference it rather than re-deriving it.

**The reframe:** verification is not a gate that *halts* — it is the feedback signal that *closes a loop*. The earlier "three separate verify stages" collapse into ONE verification-driven loop parameterized per level by `LoopPolicy` (Task auto-repairs, Plan re-plans, Phase/Milestone/Project escalate). Gate policy resolves from loop level + risk + confidence, not hierarchy position.

## v1.0.9 Requirements

Requirements for this milestone. Each maps to exactly one roadmap phase.

### Common Loop Contract (LOOP)

- [x] **LOOP-01**: `LoopPolicy` (MaxIterations, MaxDuration, BudgetCents, Autonomy, EvaluatorRef, EscalationPolicy) and `LoopStatus` (Iteration, ParentRunID, LastEvaluation, ExitReason, CostCents, Conditions) exist as **shared API types embedded in domain CRDs** — never a single generic `Loop` controller that owns every behavior
- [x] **LOOP-02**: A construct is treated as a loop only when it has all five elements — a goal/spec, a mutable candidate, evaluator/environment feedback, a repeat policy, and a bounded exit/escalation — anything missing an element is modeled (and named) as a pipeline stage, not a loop
- [x] **LOOP-03**: Detailed iteration history lives in trace/artifact storage; the CRD `.status` `LoopStatus` carries only the current-iteration summary + exit reason — etcd stays a state store, never an event database

### Execution Loop (EXEC) — inside the Job

- [x] **EXEC-01**: Every attempt carries a stable `loopRunID` + `attemptID` and emits a span per tool/action iteration
- [x] **EXEC-02**: The result envelope carries an explicit terminal reason — `completed | cap_exceeded | blocked | tool_failure | invalid_output`
- [x] **EXEC-03**: The result envelope satisfies the canonical **run-evidence contract** (`docs/templates/minimal-loop-project/evals/README.md`) — Task+Spec IDs and the contract's locking commit, commands + evaluator versions executed, test/eval results, changed-file manifest, runtime/model/prompt version, cost/duration, terminal reason, and any bounded feedback passed to a new attempt — referenced, not re-derived
- [x] **EXEC-04**: The Execution loop reports only that the agent *believes* the attempt is complete; it never stamps the Task correct (correctness is exclusively the Task loop's decision)

### Task Loop (TASK) — the core

- [x] **TASK-01**: `TaskSpec` gains a verification contract embodying the committed TASK-template shape — **planner-authored** acceptance-signal `commands` (executed for real by the evaluator, exit codes parsed, never self-reported), `requiredArtifacts`, `evaluator` (type), constraints/prohibited-changes, `maxIterations`, `onExhaustion` — and the contract is **immutable once locked** (Draft→Locked→Superseded + version): a run references the locking commit so `git show <sha>` reproduces exactly what was dispatched, and a fresh attempt re-uses that locked spec
- [x] **TASK-02**: A verification result classified **repairable** creates a FRESH attempt seeded with the original spec + a **compact evidence packet** (the relevant failures/diffs/test output) — never the previous agent's entire context
- [x] **TASK-03**: **Infrastructure-retry** (rerun the same attempt after eviction/transient failure) is distinct from **quality-iteration** (a new attempt with evaluator feedback): the eviction-retry path is preserved and the blind `maxAttemptsPerTask` quality-retry is superseded by evaluator-driven attempts
- [x] **TASK-04**: The evaluator is **logically independent** of the implementation agent (a distinct runtime/process — the LangGraph image), and a **deterministic security, compile, or test failure dominates** any probabilistic LLM-judge approval (a red gate can never be overridden by a judge)
- [x] **TASK-05**: The Task loop is bounded by `maxIterations` with `onExhaustion` → escalate / `requireApproval` (never unbounded), and its iteration/cost/budget state is re-derivable and **resumable across a controller restart**
- [x] **TASK-06**: The Task contract's escalation is **three-tier** — *fresh attempt* (repairable failures return to the Task loop), *system escalation* (a recurring cross-attempt pattern — notably an attempt that edits fixtures/thresholds/the evaluator itself instead of the code — is flagged as systemic, never counted as a pass), *human decision* (spec/architecture/risk changes → `requireApproval`); the **anti-gaming invariant** "do not weaken or delete an evaluator to make a Task pass" is enforced, not merely documented

### Evaluator Image & Verdict (EVAL)

- [x] **EVAL-01**: A read-only Python/LangGraph evaluator image runs behind the **unchanged** `pkg/dispatch.Subagent` + envelope seam (git-read + a bash gate-command tool only; no file-edit/commit/push tools; no checkpointer), with read-only enforced **structurally** — ReadOnly worktree mount + no git-write/push credentials in env + no manager-side child-CRD consumption path — and proven by an adversarial commit/push fixture test
- [x] **EVAL-02**: Credproxy TLS trust is proven by a **live pass/fail spike** (`SSL_CERT_FILE` alone through the real `ChatAnthropic` construction path) before any evaluation logic depends on it, with an `http_client=`/`anthropic_client=` fallback planned as a real contingency (langchain#35843); dependencies are patch-exact pinned with a CI gate rejecting ranges
- [x] **EVAL-03**: One `gate_decision` verdict schema — `APPROVED | REPAIRABLE | BLOCKED` plus `findings[]` (dimension/severity/confidence/evidence/suggested_fix) and a summary — authored as a matched Go + Pydantic pair, handled **fail-closed** (empty/partial/unparseable → escalation, never APPROVED; a regression test covers empty-JSON, missing-verdict-field, and malformed shapes); individual evaluator commands honor the `evals/README.md` evaluation contract (exit `0`=pass / non-zero=fail, structured result when scores/confidence matter)
- [x] **EVAL-04**: Evaluator prompts render **orchestrator-side** as a `role="verifier"` Go template family (no Python port), and the **coverage-not-conservatism** split holds — the evaluator emits a finding for *every* deviation with severity + confidence tags, and gate policy (config) alone decides what blocks
- [x] **EVAL-05**: Findings persist under the size×locality rule — a ≤4 KB verdict+counts summary on `TerminationStub`, a small per-CRD status summary, and the **full findings artifact staged on the run branch** via the v1.0.7 git-artifact-store (`collectStageEnvelopes` extended) — never an etcd blob, never a new PVC path

### Per-Level Escalation & Halt (ESC)

- [x] **ESC-01**: The same verification contract runs at every level, parameterized by `LoopPolicy` — **Task** `maxIterations:N` (auto-repair); **Plan/plan-check** `maxIterations:1` (goal-backward rubric — goal alignment, file-touch plausibility, dependency correctness, verification derivability — → re-plan with its **own** counter default 1 and **severity-weighted** stall detection); **Phase/Milestone/Project** `maxIterations:0` (escalate, because a level closes on its **observable outcome**, not on task-completion — the Slice-template principle) — gate policy resolved from **loop level**, not hierarchy position
- [x] **ESC-02**: `ConditionVerifyHalt` mirrors `failure_halt.go` file-for-file **including Phase 25's resume time-fence**, gates **both** the planner tier (`checkDispatchHolds`) and the task tier (`TaskReconciler.gateChecks`), and enforces `onExhaustion: requireApproval` through the existing gate machinery
- [x] **ESC-03**: A BLOCKED / exhausted verify is a **distinct halt class**, never a reinterpretation of `Failed` wave semantics — a regression test asserts the checked level's phase, wave siblings, and conservative-profile propagation are untouched by a VerifyHalt
- [x] **ESC-04**: Evaluator dispatches are **counted against the concurrency gate** (extend `plannerInFlightCount` or add a dedicated `verifierInFlightCount`, Phase-32 shape) in the **same phase** as the dispatch sites, and `LoopPolicy.BudgetCents` bounds cost through the existing reservation store — preventing a repeat of the run-2b D3 single-node OOM

### Loop-Native Observability (OBS)

- [x] **OBS-01**: Spans carry `loop.kind`, `loop.run_id`, `loop.parent_run_id`, `loop.iteration`, `loop.candidate_version`, `loop.exit_reason`, `evaluation.result`, `evaluation.version`, and `human_intervention`, plus cost/duration/token usage
- [x] **OBS-02**: Run IDs stay **out of Prometheus labels** (traces + structured logs only); metrics use bounded labels (loop kind, exit reason, evaluator type, risk tier)
- [x] **OBS-03**: The LangGraph evaluator's vendor registers in `pkg/dispatch.SelfInstruments` (reporter skips `events.jsonl` span synthesis) in the **same phase** as its dispatch sites, and it emits a distinct OpenInference `EVALUATOR`-kind span parented as a **sibling** to the checked level's `AGENT` span — no double-emission into the v1.0.8 trace tree
- [ ] **OBS-04**: The dashboard shows nested loop provenance (Project run → Task iteration → Execution attempt/tool spans) and surfaces `VerifyHalt` as a visually **distinct** state from `Failed`, with the staged findings browsable through the existing gitfetch/artifacts API (no new endpoint)

### Config & Surfacing (CFG)

- [ ] **CFG-01**: A chart-first config surface for the verify/loop tier (evaluator image/model + per-level `LoopPolicy` defaults) follows the existing `subagent.levels`/`resolveImage` precedence chain; `charts/tide/values.yaml` remains the FIXED contract (binary catches up to chart)
- [ ] **CFG-02**: The default posture is bounded and least-surprising — Task-loop auto-repair + Plan/Milestone/Project escalation enabled at the **milestone+project scope** for new installs, and **off** for in-place `helm upgrade` (a verify loop must not silently start spending on upgrade)

## Future Requirements

The named five-loop arc (`notes/five-loop-model.md`) and adjacent deferrals:

- **Product loop** — a `Product`/`ProductLoop` CRD above finite Projects: ingest+normalize durable `Signal` records (GitHub issues, reviews, prod incidents, user feedback) → dedupe/prioritize → spawn finite TIDE Projects → feed outcomes back. Operates through Projects, never modifies Tasks. Its own milestone.
- **System loop** — `SystemCandidate` (versioned prompt/model/harness/evaluator/dataset) + `SystemExperiment` (baseline vs challenger, eval suite, promotion policy). Expand `internal/eval` into a first-class eval system (task-completion rate, first-attempt pass rate, cost per accepted Task, regression/rollback rate, human-review burden, variance). Immutable, version-addressed candidates. Its own milestone.
- **Oversight loop** — an `OversightPolicy`/`Portfolio` resource above Products: human-owned goals/allocation/autonomy/risk/kill-switch; the existing gates are the enforcement seam, the missing piece being gate policy resolved from loop level + risk + confidence + historical performance; `protectedPaths`/`highRiskChanges` (auth, billing, security). Its own milestone.
- **Composite evaluators** — schema/spec conformance, security checks, diff-scope/file-touch validation beyond the deterministic + single-LLM-judge pair
- **Quality-tier stages** (from the seed) — standalone code-review / research / learnings / tournament subagents (the Task loop subsumes the "smart retry / debug" piece; the rest defer)
- **CACHE-F1 & Provider.Params passthrough** — `cache_control` breakpoints + temperature/thinking/top_p/top_k via `ChatAnthropic` kwargs, once authoring roles migrate onto the LangGraph runtime

## Out of Scope

Explicit exclusions — the research anti-features and the five-loop model's conscious non-goals.

| Feature | Reason |
|---------|--------|
| One generic `Loop` controller owning every behavior | Shared API types embedded in domain CRDs instead — a single loop engine would recreate the "second control plane" anti-pattern |
| Iteration histories in CRD `.status` | etcd is a state store, not an event DB — history goes to traces/artifacts; `LoopStatus` keeps only the current summary |
| The Execution loop deciding Task correctness | It reports "attempt believed complete" only — correctness is the Task loop's independent call |
| Blind `maxAttemptsPerTask` quality-retry | Superseded by evaluator-driven fresh attempts; only the infra/eviction-retry path survives |
| An LLM judge overriding a deterministic failure | A red `make test`/lint/static-analysis result dominates — the judge can never approve over it |
| Unbounded / silent retry-until-pass | Every loop is bounded (`maxIterations`) with `onExhaustion` escalation; silent retry masks real failure |
| Auto-repair at Phase/Milestone/Project level | Post-execution rework there discards paid work — those levels escalate (`maxIterations:0` → `requireApproval`), they do not iterate |
| Trusting the model's textual "tests pass" claim | The gate command runs for real and its exit code is parsed — a textual claim is exactly the 2026-07-03 failure |
| Python port of the Go authoring/verifier templates | Prompts render orchestrator-side; no second-language authoring surface |
| `deepagents` / file-edit tooling in the evaluator image | The read-only contract forbids it; hand-authored `@tool` `subprocess` calls suffice |
| `REQUESTS_CA_BUNDLE` in the TLS contract | httpx does not read it (A1 correction) — `SSL_CERT_FILE` only |
| Product / System / Oversight loops; a permanently-running backlog daemon | Named future arc — Projects stay finite/outcome-bound; the Product loop is a separate layer above them |
| Runtime DAG mutation / dynamic re-shaping | The execution DAG stays static + derived; dynamism lives inside the pod and at loop seams (the "Sounding" is a separate future arc) |

## Traceability

Locked at roadmap creation 2026-07-18 (`ROADMAP.md`): Phase 48 LangGraph evaluator image + credproxy-TLS spike, Phase 49 common loop contract + verdict/envelope/persistence schema, Phase 50 Execution-loop hardening + loop-native observability, Phase 51 the Task loop (+ `ConditionVerifyHalt` + concurrency accounting), Phase 52 per-level `LoopPolicy` parameterization, Phase 53 chart config + dashboard provenance surfacing. Strict dependency chain 48→49→50→51→52→53 — matches the research-suggested order with no deviation.

| Requirement | Phase | Status |
|-------------|-------|--------|
| LOOP-01 | Phase 49 | Complete |
| LOOP-02 | Phase 49 | Complete |
| LOOP-03 | Phase 49 | Complete |
| EXEC-01 | Phase 50 | Complete |
| EXEC-02 | Phase 50 | Complete |
| EXEC-03 | Phase 50 | Complete |
| EXEC-04 | Phase 50 | Complete |
| TASK-01 | Phase 51 | Complete |
| TASK-02 | Phase 51 | Complete |
| TASK-03 | Phase 51 | Complete |
| TASK-04 | Phase 51 | Complete |
| TASK-05 | Phase 51 | Complete |
| TASK-06 | Phase 51 | Complete |
| EVAL-01 | Phase 48 | Complete |
| EVAL-02 | Phase 48 | Complete |
| EVAL-03 | Phase 49 | Complete |
| EVAL-04 | Phase 51 | Complete |
| EVAL-05 | Phase 49 | Complete |
| ESC-01 | Phase 52 | Complete |
| ESC-02 | Phase 51 | Complete |
| ESC-03 | Phase 51 | Complete |
| ESC-04 | Phase 51 | Complete |
| OBS-01 | Phase 50 | Complete |
| OBS-02 | Phase 50 | Complete |
| OBS-03 | Phase 51 | Complete |
| OBS-04 | Phase 53 | Pending |
| CFG-01 | Phase 53 | Pending |
| CFG-02 | Phase 53 | Pending |

**Coverage:**
- v1.0.9 requirements: 28 total
- Mapped to phases: 28 (Phases 48–53) ✓
- Unmapped: 0 ✓

---
*Requirements defined: 2026-07-18*
*Last updated: 2026-07-18 — traceability filled at roadmap creation (Phases 48–53, 28/28 mapped)*
