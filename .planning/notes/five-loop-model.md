# The Five-Loop Model — TIDE's Loop-Engineering Organizing Frame

**Status:** Adopted 2026-07-18 as the organizing frame for TIDE's loop engineering, during v1.0.9 "Slack Tide" scoping. This note is the durable reference; individual loops become milestones over time.
**Source:** operator design writeup, grounded in Arize's "What is a loop in AI engineering?" (arize.com/blog/what-is-a-loop-in-ai-engineering-anyway) and mapped onto TIDE's existing CRD hierarchy + `internal/` owners.
**Relationship to prior notes:** supersedes the narrower "verify tier as a gate" framing in [vnext-specialist-verify-MILESTONE.md](../milestones/vnext-specialist-verify-MILESTONE.md) — verify is not a gate that halts, it is the feedback signal that closes a loop. Complements [langgraph-successor-runtime-strategy.md](langgraph-successor-runtime-strategy.md) (the LangGraph image is the independent evaluator) and [sounding-dynamic-orchestration-design.md](sounding-dynamic-orchestration-design.md) (orchestration *shape*; the loop model is orchestration *closure*).

**Artifact-level embodiment (already committed):** the loop-engineering template pack `docs/templates/minimal-loop-project/` + the worked example `examples/minimal-loop-project/` (PR #11 / `6f57eb5`) are the markdown contracts this model produces — PROJECT (authority/autonomy table, budget/pause/cull), SLICE (closes on observable outcome, not task-completion), TASK (immutable Draft→Locked→Superseded contract; acceptance signals; three-tier escalation: fresh-attempt / system-escalation / human-decision), and `evals/` (the canonical run-evidence contract + integrity rules: deterministic overrides judge, no evaluator-weakening, no threshold-gaming, don't change candidate+evaluator together). TIDE's CRDs are the runtime embodiment of these contracts — `TaskSpec.verification` ⇔ TASK.template, the Product loop ⇔ SLICE/Signal, the System loop ⇔ `evals/system/` + candidate versioning, the Oversight loop ⇔ PROJECT's authority/budget/kill-switch.

## Target model — five nested loops

| Loop | What iterates | TIDE owner | Closing signal | Human role |
|------|---------------|-----------|----------------|------------|
| **Oversight** | Goals, allocation, autonomy | New portfolio/policy layer | Human decision; no autonomous terminal state | Owns goals, budgets, kill switches |
| **System** | Prompts, models, harnesses, evals | New experiment/evaluation controller | Statistically credible eval improvement | Approves promotion |
| **Product** | Repository and backlog | New continuous product controller | Production/user/review feedback | Sets checkpoints by risk |
| **Task** | One artifact against one specification | `TaskReconciler` | Independent verification passes | Specifies and handles escalation |
| **Execution** | Actions inside one attempt | Subagent runtime and harness | Environment feedback or bounded termination | Normally absent mid-attempt |

## The common loop contract

Do **not** introduce one generic `Loop` controller that owns every behavior. Define shared API types embedded in domain-specific CRDs:

```go
type LoopPolicy struct {
    MaxIterations    int
    MaxDuration      metav1.Duration
    BudgetCents      int64
    Autonomy         AutonomyLevel
    EvaluatorRef     ObjectReference
    EscalationPolicy EscalationPolicy
}

type LoopStatus struct {
    Iteration      int
    ParentRunID    string
    LastEvaluation EvaluationSummary
    ExitReason     ExitReason
    CostCents      int64
    Conditions     []metav1.Condition
}
```

**Every loop needs five explicit elements** — a goal/spec, a mutable candidate, evaluator/environment feedback, a policy deciding whether to repeat, and a bounded exit/escalation condition. Missing any element → it is a **pipeline stage**, not a loop.

**Detailed iteration histories live in trace/artifact storage, not CRD status** — otherwise etcd becomes an event database. `LoopStatus` carries only the current-iteration summary + exit reason.

## Per-loop disposition

### Execution loop — inside the Job (`internal/harness`, `internal/subagent/*`)
Agent iterates `observe → decide → call tool → inspect → update`. Harness already supplies wall-clock/token/iteration caps, worktree isolation, output-path validation, secret redaction, structured envelopes. **Add:** stable `loopRunID`/`attemptID`; a span per tool/action iteration; explicit terminal reasons (`completed`/`cap_exceeded`/`blocked`/`tool_failure`/`invalid_output`); environment evidence on the result (commands, test summaries, changed files, unresolved failures); optional checkpoints for long attempts (not one K8s object per action). **This loop never decides the Task is correct** — it only reports the agent believes the attempt is complete.

### Task loop — `internal/controller/task_controller.go` (highest-value addition)
`create fresh attempt → execute in isolated context → run independent verifier → compliant? complete Task / repairable? construct feedback artifact + fresh attempt / bounded limit? escalate or fail`. Distinctions: **infra-retry** (rerun same attempt after eviction/transient) ≠ **quality-iteration** (new attempt with evaluator feedback). The new attempt receives the **original spec + a compact evidence packet**, not the previous agent's entire context. The verifier is **logically independent** from the implementation agent. Extend `TaskSpec` with a verification contract:

```yaml
verification:
  commands: [make test, make lint]
  requiredArtifacts:
    - path: internal/foo/foo_test.go
  evaluator: { type: deterministic }
  maxIterations: 3
  onExhaustion: requireApproval
```

Composite evaluators later (deterministic tests/static analysis, schema/spec conformance, security, diff-scope/file-touch, LLM judge for the non-mechanical). **A deterministic failure dominates an LLM judge's approval.**

### Product loop — new `Product`/`ProductLoop` CRD above finite Projects
Ingest+normalize external signals → dedupe/prioritize → create finite TIDE Projects for approved outcomes → track implement/review/release/monitor → feed outcomes back. **Operates through Projects, never modifies Tasks directly** (preserves the domain boundary). A current Project is an outcome-bound campaign, not a permanently-running backlog daemon — keep it that way. External signals (GitHub issues, reviews, prod alerts, user feedback) become durable, auditable `Signal` records before they influence work.

### System loop — new experiment/eval controller (seed: `internal/eval`)
`SystemCandidate` (prompt-bundle version, model-routing policy, harness config, evaluator versions, benchmark dataset version) + `SystemExperiment` (baseline, challenger, eval suite, budget, promotion policy). Expand `internal/eval` from fixtures into a first-class eval system measuring: task-completion rate, first-attempt pass rate, cost per accepted Task, time to accepted change, regression/rollback rate, invalid-output/scope-violation rate, human-review burden, variance across repeated runs. **Candidates are immutable + version-addressed;** every run records the exact prompt/model/harness/evaluator/policy versions used.

### Oversight loop — new `OversightPolicy`/`Portfolio` resource above Products
Human-owned: which outcomes matter, repos in scope, total budget/allocation, risk classification, per-inner-loop autonomy levels, promotion/merge authority, stop/cull. **Not another autonomous optimizer.** TIDE's existing gates are the enforcement mechanism; the missing piece is **resolving gate policy from loop level, risk, confidence, and historical performance** rather than only from hierarchy level. Includes `protectedPaths`/`highRiskChanges` (auth, billing, security) and a `killSwitch`.

## Loop-native observability (cross-cutting)
Extend `internal/otelinit`, `internal/metrics`, `pkg/otelai` with: `loop.kind`, `loop.run_id`, `loop.parent_run_id`, `loop.iteration`, `loop.candidate_version`, `loop.exit_reason`, `evaluation.result`, `evaluation.version`, `human_intervention`, plus cost/duration/tokens. **Run IDs stay out of Prometheus labels** (traces + structured logs only); metrics use bounded labels (loop kind, exit reason, evaluator type, risk tier). Dashboard shows nested provenance: Oversight decision → Product cycle → Project run → Task iteration → Execution attempt/tool spans.

## v1.0.9 "Slack Tide" cut (decided 2026-07-18)

**In scope:** the **Task loop** (verification-driven quality iteration, auto-repair via an independent LangGraph evaluator) + a minimal **common loop contract** (`LoopPolicy`/`LoopStatus`) + **Execution-loop hardening** + **loop-native observability** + the read-only **LangGraph evaluator image**. Higher-level verification (Plan/Phase/Milestone/Project) falls out as the same contract parameterized by `LoopPolicy` — Plan `maxIterations:1` (re-plan), Phase/Milestone/Project `maxIterations:0` (escalate to `requireApproval`). This is the unification: my earlier "plan-check loops / level-verify halts" asymmetry was `maxIterations = 1 vs 0` under one contract.

**Deferred → named arc (future milestones):** the **Product**, **System**, and **Oversight** loops, and composite evaluators beyond deterministic+LLM. `internal/eval` is the System loop's existing seed; the existing gates are the Oversight loop's enforcement seam. The full model is a program spanning several milestones — v1.0.9 builds one loop right + the contract underneath it.

**Load-bearing rules to carry into every loop milestone:** shared types in domain CRDs (never one generic Loop controller); iteration history in traces/artifacts (never etcd); five elements or it's a pipeline stage; deterministic failure dominates the LLM judge; the Execution loop never stamps correctness; autonomy/kill-switch is human-owned at the Oversight layer.
