# Phase 52: Per-Level LoopPolicy Parameterization - Research

**Researched:** 2026-07-20
**Domain:** Kubernetes controller-runtime generalization of an existing verification-loop state machine (Go, `api/v1alpha3` CRD schema + `internal/controller` reconcilers + `internal/subagent/common` prompt templates)
**Confidence:** HIGH for the seams that exist and were read directly; MEDIUM-HIGH for the two genuinely novel mechanics this phase must invent (child-CRD supersede-on-re-plan, level-verify worktree provisioning) since no prior TIDE phase built either

## Summary

Phase 52 generalizes the Phase-51 Task-loop state machine (`checkVerifyingState` /
`dispatchVerifier` / `handleVerifierCompletion` / `repairOrHalt`) to Plan
(plan-check, `maxIterations:1`) and Phase/Milestone/Project
(`maxIterations:0`, escalate-only). Every wire-format and CRD-schema seam the
Task loop uses is already level-agnostic — `pkg/dispatch.VerifyContext`,
`pkg/dispatch.ClassifyVerdict`, `LoadPromptTemplate(role, level)`,
`ResolveProvider(project, level, …)` — so most of this phase is genuinely
mechanical extension. Two seams are NOT yet level-agnostic and are the
highest-risk parts of the plan:

1. **`podjob.BuildOptions`/`BuildJobSpec`'s `JobKindVerifier` case is
   hard-wired to `opts.Task *tidev1alpha3.Task`** (`jobspec.go:285-303`) —
   unlike the `JobKindPlanner` case, which already takes a generic
   `opts.ParentObj metav1.Object`. A Plan/Phase/Milestone/Project verifier
   dispatch cannot set `opts.Task` (wrong type). `VerifierJobName` is
   similarly hard-typed to `types.UID` with no `level` parameter
   (`names.go:69`), unlike `PlannerJobName(level, parentUID string, attempt
   int)` (`names.go:52`), which already has the right shape. This is a
   small, mechanical fix (mirror the Planner case) but it is NOT optional —
   the code will not compile/link a non-Task verifier dispatch without it.

2. **There is no worktree at a Phase/Milestone/Project UID's PVC subPath for
   a level-verify Job to read.** The Task verifier's "candidate worktree"
   (`task_verifier.tmpl`'s entire tool-reachable filesystem) is NOT created
   by the verifier — it is inherited from the PRIOR executor Job, which ran
   `harness.EnsureWorktree` and left a `git worktree add -b
   tide/wt-<taskUID>` checkout at `/workspace/worktrees/<taskUID>/`
   (`internal/harness/worktree.go:58-84`, `pkg/git/worktree.go:59-79`). Only
   `Role=="executor"` Jobs run `EnsureWorktree`; Phase/Milestone/Project
   levels only ever dispatch PLANNER Jobs, which explicitly skip worktree
   creation (`worktree.go:39-43`: "Planner Tasks… never touch the working
   repo"). D-07's own text ("gate command run for real in a worktree at the
   run-branch tip") requires the plan to invent NEW worktree provisioning
   for these four dispatch sites — there is no existing mechanism to
   generalize. See Architecture Patterns §"The missing worktree" for a
   concrete recommended shape.

A third finding is less about missing plumbing and more a genuine
correctness gap in the existing design that D-04's re-plan mechanic will hit
immediately: `reporter.MaterializeChildCRDs` is `Create`-with-
`AlreadyExists-is-success` only (`materialize.go:297-302`) — it has no
update/delete/replace path — and `PlanReconciler.reconcilePlannerDispatch`
short-circuits to skip planner dispatch entirely the moment ANY Task exists
for the Plan (`plan_controller.go:267-276`). A REPAIRABLE plan-check verdict
cannot practically re-dispatch the planner without first deleting the
rejected attempt's child Tasks — otherwise the re-dispatch gate never fires
AND any Task name collision would silently freeze the rejected attempt's
stale spec in place. See Architecture Patterns §"Child-Task reconciliation
on re-plan" for the recommended delete-then-recreate shape.

**Primary recommendation:** Build the resolver (`ResolveLoopPolicy`) and the
CRD schema additions first (mechanical, low-risk, directly mirrors
`ResolveProvider`/`Gates`), land the `BuildOptions`/`VerifierJobName`
generalization second (small, load-bearing for every subsequent verify
dispatch), then sequence Plan (plan-check + delete-then-recreate re-plan) and
Phase/Milestone/Project (level-verify + new worktree provisioning) as
separate waves — they exercise almost entirely disjoint code paths and the
worktree-provisioning gap is real new-build work, not extension.

## Architectural Responsibility Map

TIDE has no browser/frontend/CDN tiers relevant to this phase — the mapping
below uses TIDE's own tiers: **Controller** (the `internal/controller`
reconcile loop, in-process, no pod), **Dispatch/Pod** (a `batchv1.Job`
TIDE creates and observes), **CRD/etcd** (the persisted `.status`/`.spec`
surface), **PVC** (the shared per-Project workspace filesystem).

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|-------------|----------------|-----------|
| Loop-level policy resolution (`ResolveLoopPolicy`) | Controller | CRD/etcd (reads Spec) | Pure function over already-fetched CRD objects, mirrors `ResolveProvider` — no pod involved |
| Plan-check verdict dispatch + consumption | Controller | Dispatch/Pod (LangGraph verifier Job) | State-machine transitions live in `PlanReconciler`; the verdict itself is produced by an out-of-process Job |
| Plan-check rubric evaluation (goal-backward) | Dispatch/Pod | PVC (reads authored child specs) | The LangGraph verifier image reads child Task JSON off the PVC and judges structure — orchestrator never re-implements the rubric in Go |
| Level-verify worktree provisioning (Phase/Milestone/Project) | **NEW: Dispatch/Pod (init container) or Controller-orchestrated Job** | PVC | No existing tier owns this — see Architecture Patterns §"The missing worktree" |
| Child-Task supersede on re-plan | Controller | CRD/etcd (Delete + re-Create) | `reporter.MaterializeChildCRDs` only knows Create; the delete step must be a NEW controller-side action gated on the plan-check verdict, not delegated to the reporter |
| `requireApproval` parking | Controller | CRD/etcd (`AwaitingApproval` phase + annotation) | Reuses `consumeApproveAndResume`/`gates` package verbatim — no new tier |
| `escalate` project freeze | Controller | CRD/etcd (`ConditionVerifyHalt`) | Reuses `verify_halt.go` verbatim |
| Verifier prompt rendering | Controller | — | Go `text/template`, orchestrator-side (EVAL-04) — never the Pod |

## User Constraints (from CONTEXT.md)

<user_constraints>

### Locked Decisions

- **D-01 (schema lands on `Plan.Spec` + `Project.Spec` ONLY):** Add
  `Verification VerificationSpec` to `Plan.Spec` (the plan-check contract)
  and a per-level defaults map to `Project.Spec` mirroring the `Gates`
  per-level shape (`project_types.go:39` — one optional `VerificationSpec`
  per level: `task`/`plan`/`phase`/`milestone`/`project`). No `Phase.Spec`
  / `Milestone.Spec` verification fields — those levels resolve from the
  Project map, exactly as `Gates` already covers them from Project scope.
  Resolution precedence: Task > Plan > Project (`ResolveProvider` / `Gates`
  precedent) — an explicit contract on the level's own spec wins, else the
  Project per-level default applies, else the stage is off (no contract →
  no verifier dispatch → today's behavior). The same CEL
  immutable-once-Locked rule from `task_types.go` applies verbatim to the
  new `VerificationSpec` sites (it's the same standalone type).

- **D-02 (add `Level` to `LoopPolicy` + a single resolver function):**
  `LoopPolicy` (`api/v1alpha3/loop_types.go:37`) gains a `Level` field
  (enum: `task|plan|phase|milestone|project`) — SC3's "loop-level field on
  `LoopPolicy`" is literal. One resolver (shape:
  `ResolveLoopPolicy(project, plan, task, level) → effective LoopPolicy`,
  exact signature Claude's discretion) maps level → default
  parameterization — task: `maxIterations:N` (from the contract,
  auto-repair), plan: `maxIterations:1`, phase/milestone/project:
  `maxIterations:0` — then merges the explicitly-authored
  `VerificationSpec`/`LoopPolicy` values over those defaults. All five
  controllers call this one function; none of them switch on CRD kind to
  pick gate policy. The existing Task-loop call sites migrate onto it (no
  behavior change for Task at default settings — proven by the existing
  51-suite staying green).

- **D-03 (trigger = Plan `Verifying` sub-state after child materialization;
  Wave-1 task dispatch gates on APPROVED):** Plan-check dispatches when the
  planner Job has completed AND the reporter has finished materializing
  child Tasks (`plan_controller.go:866` seam) — the rubric's
  dependency-correctness / verification-derivability checks need the
  authored child specs to exist. The Plan enters a `Verifying` phase
  (mirroring Task's `LevelPhaseVerifying`, generalized or a sibling
  constant — Claude's discretion) and no child Task dispatches until the
  plan-check verdict is APPROVED — child CRDs existing is free; dispatch
  is what spends. This is the cheapest pre-spend rejection point.

- **D-04 (re-plan = ONE fresh planner attempt, same template + findings
  appended):** REPAIRABLE → re-dispatch the same `plan_planner.tmpl` (no
  new authoring template) with the verdict's `findings[]` appended as a
  compact evidence block (the plan-level analog of Task's D-04 evidence
  packet: original spec + bounded findings, never the prior planner's full
  context). The re-plan mints a fresh planner attempt (existing
  `tide-plan-<uid>-<attempt>` identity); superseded children from the
  rejected attempt must never dispatch (they are replaced/reconciled by
  the re-materialization — exact child-reconciliation mechanics are a
  research/planning question, but the invariant is fixed: no
  stale-attempt Task ever dispatches, which the Verifying gate already
  guarantees).

- **D-05 (severity-weighted stall detection, LOOP-03-compliant):** Before
  consuming a remaining iteration, compare the new verdict's
  severity-weighted finding score against the previous iteration's —
  derivable entirely from `LoopStatus.LastEvaluation` (`FindingsCount` +
  `HighSeverityCount` — current-iteration summary only, NO history array).
  If the score does not strictly decrease, halt immediately ("re-plan loop
  stalled") rather than burn remaining budget. Weighting scheme is
  Claude's discretion; the structural requirement is that a non-improving
  re-plan exits early even when `maxIterations` would allow another
  attempt. With the default `maxIterations:1` the stall check is exercised
  when operators raise the bound — cover it with a `maxIterations:2` test.

- **D-06 (plan-check gets its OWN counter — `LoopStatus` embedded in
  `PlanStatus`):** Embed the shared `LoopStatus` in `Plan.Status`
  (`LoopStatus.Iteration` counts quality re-plans only), exactly as Task
  did (`task_types.go:330`). It is distinct from: the Task loop's counter,
  the planner's infra-retry attempt identity, and Phase-34's
  wave-integration `Attempts` (`plan_types.go:67`) — infra-retry ≠
  quality-iteration holds at plan level too. The
  `TestLoopStatus_NoForbiddenFields`-style compile-time guard extends to
  the new embedding.

- **D-07 (trigger = after all children Succeeded, before the level stamps
  `Succeeded` / boundary push):** For Phase/Milestone/Project with a
  resolved verification contract, the verifier dispatches at the moment
  the level would otherwise stamp `Succeeded` (`level_status.go`
  `patch{Level}Succeeded` seam) — semantic verification of the observable
  outcome (gate command run for real in a worktree at the run-branch tip,
  declared artifacts exist), running alongside the existing mechanical
  merge-ancestry completeness gate. `maxIterations:0` means any
  non-APPROVED verdict escalates immediately — there is no repair branch
  at these levels, and the resolver (D-02) is what encodes that, not
  level-specific if-statements.

- **D-08 (per-value `onExhaustion` semantics — uniform across ALL levels,
  including Task):** This phase closes Phase 51's declared-but-uniform gap
  (`task_types.go:145`): `requireApproval` routes through the existing
  human-gate machinery (`AwaitingApproval` + `consumeApproveAndResume`,
  `level_status.go:128` — ESC-02's "enforces requireApproval through the
  existing gate machinery" is literal), parking the checked level for a
  `tide approve`-shaped resume; `escalate` fires the project-wide
  `ConditionVerifyHalt` (`verify_halt.go`, cleared by `tide resume` with
  the time-fence). The differentiation lives in ONE place (the
  exhaustion/escalation path fed by D-02's resolver), so Task inherits it
  in the same commit that gives Phase/Milestone/Project their semantics.
  ESC-03's invariant extends: neither exit reinterprets `Failed` wave
  semantics — regression coverage asserts siblings/conservative-profile
  propagation stay untouched at the newly-verified levels too.

- **D-09 (per-level `<level>_verifier.tmpl` files, existing loader, no new
  machinery):** Add `plan_verifier.tmpl` carrying the goal-backward rubric
  (goal alignment, file-touch plausibility, dependency correctness,
  verification derivability — ESC-01's four named dimensions) and
  `phase_verifier.tmpl` / `milestone_verifier.tmpl` / `project_verifier.tmpl`
  prompting observable-outcome verification (deliverables exist on the run
  branch, gate command exit honored, constraint paths untouched) — all
  behind the existing `LoadPromptTemplate("verifier", level)` convention
  (`prompt_templates.go`), rendered orchestrator-side (EVAL-04: no Python
  port). All four follow coverage-not-conservatism: emit a finding for
  every deviation with severity + confidence tags; the resolver/config
  decides what blocks. `task_verifier.tmpl` is untouched.

- **D-10 (the Phase-51 safety rails apply to every new dispatch site
  as-is):** Per-level verifier dispatches count against the existing
  `verifierInFlightCount` cap and reserve through `BudgetCents`/the
  `ReservationStore` (ESC-04 machinery — same pool, no new counter);
  verdicts flow through the fail-closed `ClassifyVerdict` (empty/malformed
  → BLOCKED, never APPROVED); deterministic gate-command failure dominates
  the judge at every level; each new site emits the `EVALUATOR`-kind
  sibling span via the existing `synthesizeEvaluatorSpan` path with the
  level-appropriate `tide.dispatch.<level>.verify` name. The plan must
  include tests proving these hold at the NEW sites (a plan-check dispatch
  is counted + reserved + fail-closed), not re-implementations.

### Claude's Discretion

- Exact Go field names / JSON tags for the `Project.Spec` per-level
  verification map and `LoopPolicy.Level`; the resolver's signature and
  package home (`dispatch_helpers.go`'s `ResolveProvider` idiom vs a
  dedicated file) — within D-01/D-02.
- Whether Plan reuses `LevelPhaseVerifying` or mints a sibling constant
  (the constant is currently doc-commented Task-only) — within D-03.
- The exact severity-weighting scheme for stall detection — within D-05's
  strictly-decreasing requirement.
- Child-Task reconciliation mechanics on re-plan (supersede/delete/replace)
  — within D-04's no-stale-dispatch invariant.
- The `EVALUATOR`-span attribute details and `LoopStatus` embedding sites
  for phase/milestone/project statuses (embed only where a loop actually
  runs; `maxIterations:0` levels may need only `LastEvaluation` + exit
  surface) — within D-07/LOOP-03.

### Deferred Ideas (OUT OF SCOPE)

- Chart-first config surface (per-level `LoopPolicy` defaults in
  `values.yaml`, evaluator image/model, `subagent.levels`/`resolveImage`
  precedence, new-install vs in-place-upgrade posture) → Phase 53
  (CFG-01/02). This phase's CRD fields are the API that chart populates.
- Dashboard nested loop provenance + `VerifyHalt` visual state + staged
  findings browsing → Phase 53 (OBS-04).
- Integration-check as a distinct cross-level stage (research Q7 —
  build/test the whole run branch at milestone/project boundary) — insofar
  as it exceeds a `project`-level verification contract, it is Phase 53+/
  future; this phase ships the project-level contract only.
- Gate policy from risk + confidence + historical performance → Oversight-
  loop arc (five-loop model).
- Composite evaluators (schema conformance, security, diff-scope beyond
  deterministic + single LLM judge) → named future arc.

</user_constraints>

<phase_requirements>
## Phase Requirements

| ID | Description | Research Support |
|----|-------------|------------------|
| ESC-01 | The same verification contract runs at every level, parameterized by `LoopPolicy` — Task `maxIterations:N` (auto-repair); Plan/plan-check `maxIterations:1` (goal-backward rubric → re-plan with its own counter default 1 and severity-weighted stall detection); Phase/Milestone/Project `maxIterations:0` (escalate); gate policy resolved from loop level, not hierarchy position | All Architecture Patterns sections below map directly to ESC-01's four clauses: the resolver (gate-from-level), the plan-check loop mechanics, the Phase/Milestone/Project escalate-only path, and the `onExhaustion` per-value differentiation. Code Examples section gives concrete before/after signatures for every touched function. |

</phase_requirements>

## Project Constraints (from CLAUDE.md)

- **GSD Workflow Enforcement:** all file-changing work must route through a
  GSD command (`/gsd:execute-phase` for this phase) — no direct edits.
- **`charts/tide/values.yaml` is a FIXED contract** — this phase must not
  edit it (chart config is Phase 53's CFG-01/02).
- **Never mount host `~/.claude/`** into executor/verifier containers — N/A
  here (no new dispatch image), but the new verifier dispatch sites must
  keep using `ANTHROPIC_API_KEY`/credproxy the same way Task's does.
- **Don't hard-code one LLM provider, one git host, or one auth model** —
  the resolver must stay provider-agnostic; it already inherits this from
  `ResolveProvider`.
- **Don't accept a wave list as CRD input; don't cache the schedule in
  `.status`; resumption = indegree map + completed-task set** — the new
  `LoopStatus` embeddings on `Plan.Status`/level statuses must stay
  re-derivable, matching LOOP-03's "no history in `.status`" rule already
  enforced by `TestLoopStatus_NoForbiddenFields`.
- **CEL CRD validation (`x-kubernetes-validations`), not admission
  webhooks** — the new `VerificationSpec` sites inherit `task_types.go`'s
  existing CEL rule automatically (same type); no new webhook logic.
- **`make test-int` exit ≠ Ginkgo green** — the `test/integration/kind`
  package bundles plain go-tests; a plan-check/level-verify regression
  test added there must be checked via `grep -nE '^--- FAIL|^FAIL\s'`,
  not just the Ginkgo summary line.
- **A subagent's "pre-existing/unrelated" dismissal of a failing test is a
  claim, not verification** — applies to any test flakiness surfaced while
  landing this phase's five new dispatch sites.
- **Constrained-VM full-suite recipe** — a clean `kind` cluster per heavy
  run; never run `make acceptance-v1-smoke` alongside a live `tide-test`
  cluster (OOM risk on the 7.65 GiB dev VM).

## Standard Stack

This phase adds **zero new external dependencies**. It is pure extension of
already-vendored, already-verified machinery:

### Core (already in the repo — reused, not newly added)
| Component | Location | Purpose | Why reused, not replaced |
|-----------|----------|---------|---------------------------|
| `pkg/dispatch.VerifyContext`/`ClassifyVerdict`/`GateDecision` | `pkg/dispatch/envelope.go`, `verdict.go` | Wire-format verdict + verify-dispatch input, already level-agnostic | `VerifyContext`'s fields (`GateCommand`, `Commands`, `RequiredArtifacts`, `EvaluatorRef`, `EvidencePacketPath`) carry no Task-only assumption — confirmed by direct read `[VERIFIED: pkg/dispatch/envelope.go:438-471]` |
| `internal/subagent/common.LoadPromptTemplate(role, level)` | `prompt_templates.go:82-89` | Compiled-in Go template loader | Filename convention `templates/<level>_<role>.tmpl` already resolves `plan_verifier`/`phase_verifier`/`milestone_verifier`/`project_verifier` with ZERO loader code changes `[VERIFIED: prompt_templates.go:83]` |
| `internal/controller.ResolveProvider(project, level, defaults)` | `dispatch_helpers.go:283-330` | Model/vendor resolution per level | Already takes an arbitrary `level string` and maps via `levelOverrideKey`, which already covers all 5 levels including `"project"` `[VERIFIED: dispatch_helpers.go:252-267]` |
| `internal/gates` package (`EvaluatePolicy`, `CheckApprove`, `ConsumeApprove`, `CheckRejected`, `BoundaryDetected`) | `internal/gates/*.go` | Human-gate + boundary-detection primitives | D-08's `requireApproval` reuses this verbatim; no new gate vocabulary needed `[VERIFIED: internal/gates/policy.go:71, annotation.go:71,94,118]` |
| `cmd/tide` CLI `approve`/`resume` verbs | `cmd/tide/approve.go`, `resume.go` | Operator unparking surface | `findAwaitingMilestone`/`findAwaitingPhase`/`findAwaitingPlan`/`findAwaitingTask` already exist; only `findAwaitingProject` is missing (see Common Pitfalls) `[VERIFIED: cmd/tide/approve.go:176-191,358-415]` |

### Supporting
| Component | Purpose | When touched |
|-----------|---------|---------------|
| `internal/dispatch/podjob.BuildOptions`/`BuildJobSpec` | Job-spec construction for planner/executor/verifier Jobs | MUST be extended — `JobKindVerifier` case hard-requires `opts.Task *Task` (see Common Pitfalls #1) |
| `internal/dispatch/podjob.VerifierJobName` | Deterministic verifier Job naming | MUST be extended — hard-typed to `types.UID`, no `level` param (see Common Pitfalls #1) |
| `internal/harness.EnsureWorktree` / `pkg/git.AddWorktree` | Executor-only worktree provisioning | Cannot be reused as-is for level-verify — see Common Pitfalls #2 / Architecture Patterns |
| `internal/reporter.MaterializeChildCRDs` | Server-side-create child CRDs from `EnvelopeOut.ChildCRDs` | `Create`-only; a re-plan needs a NEW delete step ahead of it — see Common Pitfalls #3 |

### Alternatives Considered
| Instead of | Could Use | Tradeoff |
|------------|-----------|----------|
| Delete-then-recreate child Tasks on re-plan (recommended) | In-place `Update` inside `MaterializeChildCRDs` when content drifts | Update-on-drift doesn't handle a re-plan DROPPING a Task name (orphan) or RENAMING one (duplicate); would need its own reconciliation-diff logic layered on top of an already-shared, multi-consumer helper — higher blast radius for a mechanism only Plan uses |
| A new init container for level-verify worktree setup (recommended) | Extend `harness.EnsureWorktree`'s Go path to also run for `Role=="verifier"` at phase/milestone/project levels | Not possible without breaking EVAL-01's read-only-Python-image contract — the LangGraph verifier image never imports `internal/harness` (Go) per the `cmd/tide-langgraph-verifier/Dockerfile` header comment; the checkout MUST happen before/outside the Python container |
| A new `VerifierJobName(level, parentUID string, attempt int)` mirroring `PlannerJobName` (recommended) | Keep `VerifierJobName(taskUID, attempt)` and encode level into a synthetic UID string | Silently loses type safety and grep-ability; every other Job-name function in the package takes typed, named params |

**Installation:** None — no `go.mod`/`requirements.txt` changes this phase.

**Version verification:** N/A — no package versions to verify (schema +
controller logic only). Go toolchain stays pinned at `go 1.26.0`
`[VERIFIED: go.mod:3]`.

## Package Legitimacy Audit

**No external packages are installed by this phase.** All work is CRD
schema (`api/v1alpha3`), controller logic (`internal/controller`,
`internal/dispatch/podjob`, `internal/reporter`), and compiled-in Go
template additions (`internal/subagent/common/templates/*.tmpl`). The
Package Legitimacy Gate is not applicable; `slopcheck` was not run because
there is nothing to check.

## Architecture Patterns

### System flow: the five-level verify state machine after this phase

```
                    ┌─────────────────────────────────────────┐
                    │  ResolveLoopPolicy(project, plan, task,  │
                    │  level) — ONE function, all 5 levels     │
                    │  (dispatch_helpers.go, new file)          │
                    └───────────────┬───────────────────────────┘
                                    │ effective LoopPolicy
                                    │ {MaxIterations, EscalationPolicy, Level}
              ┌─────────────────────┼─────────────────────────────┐
              │                     │                               │
     Task loop (existing,           │                    Phase/Milestone/Project
     Phase 51 — unchanged           │                    (NEW, maxIterations:0)
     shape, resolver-fed)           │
              │                     │
   executor exits 0                 │           children all Succeeded
        │                           │           (patch{Level}Succeeded seam,
        ▼                           │            level_status.go)
   Verifying ──dispatch──▶ verifier │                    │
   (checkVerifyingState)     Job    │                    ▼
        │                           │          NEW: dispatch level-verify
   APPROVED / REPAIRABLE /          │          (needs a FRESH worktree
   BLOCKED (handleVerifierCompletion)│          checkout at run-branch tip —
        │                           │          see "The missing worktree"
   ┌────┴────┐                      │          below; no prior executor
   │         │                      │          worktree exists at this UID)
Succeeded  repairOrHalt             │                    │
           (MaxIterations           │            APPROVED / else
            from resolver,          │                    │
            not raw spec field)     │             ┌──────┴──────┐
                                     │         Succeeded    non-APPROVED
                                     │                       │
              Plan loop (NEW)       │              onExhaustion (resolver):
              ────────────────      │              escalate → ConditionVerifyHalt
   planner Job completes AND        │              requireApproval → AwaitingApproval
   reporter finishes materializing  │              (consumeApproveAndResume,
   child Tasks (handlePlannerJob-   │               tide approve)
   Completion seam)                 │
        │                           │
   Plan → Verifying (NEW phase,     │
   Task dispatch HELD — new check   │
   mirroring checkParentApproval)   │
        │                           │
   dispatch plan-check verifier ────┘
        │
   APPROVED ──▶ clear Verifying, Task dispatch unblocked (existing wave path)
        │
   REPAIRABLE, score improving, iterations remain:
        │
   DELETE rejected attempt's child Tasks (NEW — see
   "Child-Task reconciliation on re-plan" below)
        │
   re-dispatch plan_planner.tmpl, attempt+1, findings appended
        │
   REPAIRABLE + stalled (D-05) OR MaxIterations exhausted:
        │
   onExhaustion (resolver): escalate / requireApproval — same as above
```

### The missing worktree (Phase/Milestone/Project level-verify)

**This is new build work, not extension — read this before scoping the plan.**

The Task verifier's entire "candidate worktree" is inherited state: the
executor Job for the SAME Task UID already ran
`harness.EnsureWorktree(env, workspaceRoot, env.Branch)`
`[VERIFIED: internal/harness/worktree.go:58]`, which shells out to
`pkggit.AddWorktree(bareRepoPath, taskUID, branch)`
`[VERIFIED: pkg/git/worktree.go:59]` — this creates
`/workspace/worktrees/<taskUID>/` on a FRESH branch `tide/wt-<taskUID>`
forked from the run branch. The verifier Job's `ReadOnly` mount targets the
SAME subPath (`envelopeUID = parentUID = task.UID` for both Kinds,
`[VERIFIED: jobspec.go:291,332]`), so it transparently reads what the
executor already checked out.

Phase/Milestone/Project levels NEVER dispatch an executor — only planner
Jobs (`JobKindPlanner`), and `EnsureWorktree` explicitly no-ops for
`Role != "executor"` `[VERIFIED: internal/harness/worktree.go:59-63,
comment at :39-43]`. So a level-verify dispatch at `phase-uid`/
`milestone-uid`/`project-uid` finds `/workspace/worktrees/<that-uid>/`
simply absent. D-07's own text ("gate command run for real in a worktree
at the run-branch tip") cannot be satisfied without new provisioning.

**Recommended shape** (Claude's Discretion — not locked by CONTEXT.md, but
directly evidenced by the code read above):

1. Add a small `pkg/git` helper that does a **read-only** worktree checkout
   with NO new branch — `git worktree add <dir> <runBranch>` (no `-b`),
   distinct from `AddWorktree`'s write-oriented `-b tide/wt-<uid>` shape,
   because a level-verify never commits.
2. Run this as a **second init container** in the verifier Job's PodSpec
   for Level != "task" (BuildJobSpec already composes
   `initContainers := []corev1.Container{envelopeWriter}` —
   `[VERIFIED: jobspec.go:558]` — a second entry is additive, not a
   redesign). This keeps the checkout OUTSIDE the read-only Python
   LangGraph container, preserving EVAL-01's "no git-write tooling in the
   verifier image" contract, and outside the Go controller process (no
   git binary/credentials needed in the manager).
3. Key the checkout directory by the checked level's own UID (mirrors
   `envelopeUID`), e.g. `/workspace/worktrees/<phase-uid>/`, so
   `phase_verifier.tmpl` can reference the same "candidate's worktree is
   your entire reachable filesystem" framing `task_verifier.tmpl` uses.
4. The `runBranch` value is `project.Status.Git.BranchName` at CURRENT
   HEAD — this already exists on every `EnvelopeIn.Branch` the executor
   reads; a level-verify dispatch just needs to populate the SAME field
   (currently the verifier envelope builder — `buildVerifierEnvelopeIn` —
   never sets `Branch` at all, since the Task verifier doesn't need it;
   the level-verify variant does).

This new init-container git image needs SOME git-capable binary — reusing
the existing `credproxy` image is wrong (no git); the cheapest option is
likely the SAME `tide-push`/git-writer image already built for boundary
pushes (`r.Deps.TidePushImage`), since it already ships a git binary and is
already deployed. Flag this as a concrete decision point for the plan.

### Child-Task reconciliation on re-plan

**This is a real correctness gap in the existing materialization design,
not a stylistic choice — read the evidence before picking an approach.**

`reporter.MaterializeChildCRDs` treats `apierrors.IsAlreadyExists` as
idempotent success with NO update `[VERIFIED: internal/reporter/
materialize.go:297-302]`. It was built for "materialize once ever" — Phase
51's own design never anticipated a REJECTED-then-retried materialization.

Independently, `PlanReconciler.reconcilePlannerDispatch` has this order at
its top:

```go
// plan_controller.go:265-276
// If Tasks already exist for this Plan, skip planner dispatch — the
// Phase 2 Wave path runs.
var taskList tideprojectv1alpha3.TaskList
if err := r.List(ctx, &taskList, ...); err != nil { ... }
if len(taskList.Items) > 0 {
    return ctrl.Result{}, false, nil
}
```

`[VERIFIED: internal/controller/plan_controller.go:267-276]`. This check
runs BEFORE any phase-based branching. Since D-03's plan-check dispatches
AFTER child Tasks are already materialized, the rejected attempt's
children ALREADY exist by the time a REPAIRABLE verdict comes back — so
this early return will ALWAYS fire and the re-plan's planner Job will
NEVER dispatch, regardless of what `Plan.Status.Phase` is set to. Also note
`attempt := 1 // plan planner dispatch is single-shot per ROADMAP scope`
and `jobName := fmt.Sprintf("tide-plan-%s-1", plan.UID)` are BOTH hardcoded
to attempt 1 `[VERIFIED: plan_controller.go:283,388]` — there is no
existing attempt-counter for planner Jobs (Task's `nextAttempt` pattern,
`task_controller.go:1920-1949`, has no Plan-level analog yet).

**Recommended shape** (Claude's Discretion — D-04 explicitly leaves this
open, but the evidence above makes the shape fairly constrained):

1. Before re-dispatching the fresh planner attempt, **delete every child
   Task CRD owned by this Plan** (`client.MatchingFields{taskPlanRefIndexKey:
   plan.Name}`, the SAME list `reconcilePlannerDispatch` already runs at
   `plan_controller.go:267-273`). This is safe because D-03 guarantees no
   child Task has dispatched yet (Plan is parked in `Verifying`, and the
   new hold described below prevents Task dispatch during that phase) —
   deleting an un-dispatched Task CRD has no in-flight Job to worry about.
2. This single delete step satisfies BOTH problems at once: it unblocks
   `reconcilePlannerDispatch`'s `len(taskList.Items) > 0` early return (so
   the fresh planner Job actually gets created), and it structurally
   guarantees "no stale-attempt Task ever dispatches" (there is nothing
   stale left to dispatch).
3. Add an attempt counter for Plan planner Jobs mirroring `nextAttempt`
   (list Jobs by `tideproject.k8s/role=planner` +
   `tideproject.k8s/plan-uid=<planUID>`, take `max(owner.LabelAttempt)+1`)
   so the re-plan Job gets a genuinely new deterministic name
   (`tide-plan-<uid>-2`) rather than colliding with the rejected attempt's
   `-1` Job. `PlanStatus.LoopStatus.Iteration` (D-06) is a natural source
   for this same number, avoiding a second List — worth considering in
   the plan rather than a fresh Job-label scan.

### Extending `checkParentApproval` for the new `Verifying` hold

D-03 requires "no child Task dispatches until the plan-check verdict is
APPROVED." Today, `checkParentApproval` — the ONLY existing hold checking a
parent's phase — matches `LevelPhaseAwaitingApproval` alone
`[VERIFIED: dispatch_helpers.go:578-603]`; a Plan whose phase clears to `""`
(the current post-materialization behavior,
`[VERIFIED: plan_controller.go:907-919]`) is NOT held. This hold must be
extended (new sibling check, or an additional case in the same switch) to
ALSO park Task dispatch when the parent Plan's `Status.Phase ==
LevelPhaseVerifying` (or the sibling constant chosen per D-03's discretion
point) — mirroring the EXACT same shape already used for
`AwaitingApproval`, at the exact same call site
(`task_controller.go:449-455`).

### Recommended project structure (files touched, not new packages)

```
api/v1alpha3/
├── loop_types.go        # + Level field on LoopPolicy
├── task_types.go        # VerificationSpec unchanged (reused as-is)
├── plan_types.go         # + Verification VerificationSpec, + LoopStatus on PlanStatus
├── project_types.go      # + per-level VerificationSpec defaults map (mirrors LevelOverrides/Gates)
└── shared_types.go        # LevelPhaseVerifying/VerifyHalted doc-scope updates (or sibling consts)

internal/controller/
├── dispatch_helpers.go    # + ResolveLoopPolicy (next to ResolveProvider)
├── plan_controller.go     # + Verifying sub-state, dispatchPlanVerifier, handlePlanVerifierCompletion,
│                          #   repairOrHaltPlan (stall detection), delete-then-recreate on re-plan
├── phase_controller.go    # + level-verify dispatch before patchPhaseSucceeded (3 call sites)
├── milestone_controller.go# + level-verify dispatch before patchMilestoneSucceeded (3 call sites)
├── project_controller.go  # + level-verify dispatch before/inside checkProjectComplete
├── task_controller.go     # repairOrHalt/prepareDispatch migrate onto resolver output (behavior-preserving)
├── level_status.go        # onExhaustion requireApproval/escalate branch (shared, feeds all 5 levels)
└── verify_halt.go         # unchanged — escalate arm reused verbatim

internal/dispatch/podjob/
├── jobspec.go             # JobKindVerifier case: opts.Task → generic opts.ParentObj (mirrors JobKindPlanner)
└── names.go                # VerifierJobName(taskUID, attempt) → VerifierJobName(level, parentUID string, attempt int)

pkg/git/
└── worktree.go             # + read-only worktree helper (no new branch) for level-verify checkouts

internal/subagent/common/templates/
├── plan_verifier.tmpl      # NEW — goal-backward rubric
├── phase_verifier.tmpl     # NEW — observable-outcome rubric
├── milestone_verifier.tmpl # NEW — observable-outcome rubric
└── project_verifier.tmpl   # NEW — observable-outcome rubric

cmd/tide/
└── approve.go               # + findAwaitingProject (Project has no existing AwaitingApproval case)
```

### Anti-Patterns to Avoid

- **Switching on CRD Kind to pick gate policy.** SC3 is explicit and a
  plan-checker will grep for this: every one of the five reconcilers must
  call the SAME `ResolveLoopPolicy(project, plan, task, level)` and branch
  only on the RETURNED `LoopPolicy.MaxIterations`/`EscalationPolicy`, never
  on `switch obj.(type)` or a level string comparison duplicated per file.
- **Reading `task.Spec.Verification.MaxIterations`/`.OnExhaustion` directly
  post-this-phase.** These raw spec fields become INPUTS to the resolver,
  not the decision surface itself — `repairOrHalt`'s existing
  `task.Status.Attempt >= int(task.Spec.Verification.MaxIterations)`
  `[VERIFIED: task_controller.go:2643]` must migrate to read the
  resolver's effective `LoopPolicy.MaxIterations` (D-02 explicitly calls
  this migration out: "no behavior change for Task at default settings").
- **Assuming the verifier envelope's `Level` field only ever holds
  `"task"`.** `EnvelopeIn.Level`'s doc comment currently says `"milestone"
  | "phase" | "plan" | "task"` `[VERIFIED: pkg/dispatch/envelope.go:62-64]`
  — this is stale; `"project"` is ALREADY a live value elsewhere
  (`podjob.BuildOptions.Level` doc comment correctly lists all 5,
  `project_controller.go:1469` sets `Level: "project"` for its own planner
  dispatch). Update the stale doc comment in the same commit that adds
  project-level verify dispatch, so it doesn't mislead the next reader.
- **Reusing `AddWorktree`'s write-branch semantics for level-verify.**
  `AddWorktree` mints a NEW branch (`tide/wt-<uid>`) intended for an
  executor's commits — a level-verify dispatch never commits and reusing
  this would leave dangling throwaway branches on every verify run. Use a
  plain (no `-b`) worktree checkout instead.

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| Fail-closed verdict parsing | A second verdict classifier for plan/phase/milestone/project | `pkg/dispatch.ClassifyVerdict` (unchanged) | It is already level-agnostic — takes raw JSON, has no Task-specific field reads `[VERIFIED: pkg/dispatch/verdict.go:102-118]` |
| Verifier prompt loading | A new template-resolution mechanism per level | `common.LoadPromptTemplate(role, level)` | Filename convention already covers the four new templates with zero code change `[VERIFIED: prompt_templates.go:82-89]` |
| Human-approval parking | A new pause/resume primitive for Phase/Milestone/Project | `consumeApproveAndResume` + `gates.CheckApprove`/`ConsumeApprove` + `LevelPhaseAwaitingApproval` | Same shared helper already backs Milestone/Phase/Plan's existing `Gates`-driven approve gates `[VERIFIED: level_status.go:106-155]` |
| Concurrency/budget accounting for new verifier dispatch sites | A second reservation/cap system per level | `verifierInFlightCount` (project-scoped, kind-agnostic) + `r.Deps.Reservations` | Already scoped by `role=verifier` + project label, not task-uid — works unmodified for any level `[VERIFIED: dispatch_helpers.go:535-559]` |
| Model/vendor resolution for the four new verifier dispatch sites | A parallel resolver | `ResolveProvider(project, level, helmDefaults)` | Already takes any of the 5 level strings via `levelOverrideKey` `[VERIFIED: dispatch_helpers.go:252-330]` |

**Key insight:** almost everything on the wire/dispatch side of this phase
is reuse, not new code — the entire risk budget is in the two places (Job
building for non-Task verifiers, worktree provisioning) that were built
Task-first and never generalized, plus the one place (child-CRD
materialization) that was built for a "materialize once" world colliding
with a genuinely new "materialize, reject, re-materialize" requirement.

## Common Pitfalls

### Pitfall 1: `podjob.BuildOptions`/`VerifierJobName` silently assume Task

**What goes wrong:** A plan/phase/milestone/project verifier dispatch sets
`podjob.BuildOptions{Kind: podjob.JobKindVerifier, ParentObj: plan, ...}`
(mirroring the Planner pattern) but `BuildJobSpec`'s `case JobKindVerifier:`
branch reads `opts.Task.UID` unconditionally
(`jobspec.go:290-293` — `if opts.Task != nil { parentUID =
string(opts.Task.UID) }` then `jobName = VerifierJobName(opts.Task.UID,
opts.Attempt)`), and a nil `opts.Task` (which is what a Plan-level caller
will pass, since `Plan` doesn't satisfy `*Task`) leaves `parentUID` empty
and then panics on `VerifierJobName(opts.Task.UID, ...)` dereferencing a
nil pointer's `.UID`.

**Why it happens:** `JobKindVerifier` was added in Phase 51, Task-only, and
copied the executor case's shape (`opts.Task`) rather than the planner
case's shape (`opts.ParentObj`) because at the time only Tasks had
verifiers.

**How to avoid:** Change `case JobKindVerifier:` to read `parentUID` from
`opts.ParentObj.GetUID()` (exactly like `case JobKindPlanner:` already
does), and change `VerifierJobName(taskUID types.UID, attempt int)` to
`VerifierJobName(level, parentUID string, attempt int)` mirroring
`PlannerJobName`'s exact signature. Update the one existing Task call site
(`task_controller.go:2102`, `dispatchVerifier`) in the same commit so the
Task verifier's Job name stays `tide-verifier-<taskUID>-<attempt>` (add
`level="task"` explicitly, or fold level into the name format only if it's
acceptable for the Task verifier's Job name to change shape — check
existing kind specs/tests that assert on the exact name string before
deciding).

**Warning signs:** A verifier dispatch at a non-Task level panics
immediately at `BuildJobSpec` call time (nil-pointer on `.UID`), or (if a
defensive nil-check is added without fixing the root cause) silently
produces an empty-string Job name that collides across every
Plan/Phase/Milestone/Project verifier dispatch in the same namespace.

### Pitfall 2: The level-verify worktree does not exist yet

**What goes wrong:** A Phase/Milestone/Project verifier Job dispatches,
mounts `/workspace` ReadOnly at a subPath keyed by that level's own UID,
and the LangGraph verifier's `git_read`/`run_gate_command` tools find an
empty directory (no `.git`) — every gate command fails, every finding is
"worktree missing," and the loop escalates on the FIRST run regardless of
actual project state.

**Why it happens:** See Architecture Patterns §"The missing worktree" —
only executor Jobs (Task-only) run `harness.EnsureWorktree`; nothing
provisions a worktree for a level that only ever dispatches planner Jobs.

**How to avoid:** Build the new init-container-based worktree provisioning
described above BEFORE writing the `phase_verifier.tmpl`/etc. prompts or
any Phase/Milestone/Project verify-dispatch test — a test written against
a Job spec that never gets a real worktree will pass in envtest (no real
PVC) and fail live, repeating the exact "test coverage vs. live proof"
gap Phase 51 hit with `TaskReconcilerDeps.VerifierImage` being unwired.

**Warning signs:** envtest specs for the new dispatch sites all pass
(envtest has no real PVC, so a missing worktree is invisible there) while
a live kind run shows every level-verify finding as "artifact/worktree
not found" regardless of what actually shipped.

### Pitfall 3: Re-plan re-dispatch is a silent no-op without child-Task deletion

**What goes wrong:** A REPAIRABLE plan-check verdict triggers the intended
"dispatch a fresh planner attempt" code path, but
`PlanReconciler.reconcilePlannerDispatch`'s existing
`len(taskList.Items) > 0 → return dispatched=false` early return
(`plan_controller.go:267-276`) fires first (the rejected attempt's
children already exist), so NOTHING happens — the Plan appears stuck in
`Verifying` forever with no forward progress and no error.

**Why it happens:** `reconcilePlannerDispatch` was designed under a
"materialize once, then hand off to wave path forever" assumption; it has
no concept of "materialized, but the materialization was itself rejected."

**How to avoid:** Delete the rejected attempt's child Tasks (Architecture
Patterns §"Child-Task reconciliation on re-plan") as the FIRST step of the
re-plan dispatch path, before calling into (or falling through to)
`reconcilePlannerDispatch`'s existing dispatch logic.

**Warning signs:** A `maxIterations:2` stall-detection test (D-05's own
required coverage) never observes a SECOND planner Job — only ever the
first — because the re-dispatch silently no-ops.

### Pitfall 4: Project-level `VerificationSpec` defaults inherit Task's Locked-immutability CEL rule

**What goes wrong:** An operator (or Phase 53's chart-templated default)
sets a `Phase: "Locked"` value on a Project-level per-level
`VerificationSpec` default entry (to make it "active," mirroring how a
Task's contract must be Locked before the verifier trusts it) — but the
SAME CEL rule that makes a locked Task contract immutable
(`+kubebuilder:validation:XValidation:rule="oldSelf.phase != 'Locked' ||
self == oldSelf || self.phase == 'Superseded'"`,
`[VERIFIED: task_types.go:90]`) now ALSO applies to this Project-level
default, since it's the identical `VerificationSpec` type embedded
verbatim (D-01 explicitly confirms: "The same CEL immutable-once-Locked
rule from `task_types.go` applies verbatim to the new `VerificationSpec`
sites"). A later `helm upgrade` or `kubectl edit project` attempting to
change that "Locked" default now fails CEL admission unless it goes
through the Locked→Superseded dance.

**Why it happens:** Type reuse (a deliberate, locked D-01 choice) means
CEL rules attached at the type level propagate to every embedding site,
including ones that were conceptually meant to be simple mutable admin
config, not planner-authored contracts.

**How to avoid:** This is a LOCKED decision (not to be re-opened this
phase), so the mitigation is documentation + convention, not schema
change: Project-level per-level `VerificationSpec` defaults should
probably stay at `Phase: "Draft"` (or empty) rather than "Locked" — Draft
is "freely editable" per the CEL rule's own semantics
`[VERIFIED: task_types.go:92-96]`. Flag this explicitly for Phase 53's
chart-default authoring so CFG-02's "off on in-place upgrade" posture
doesn't accidentally ship a Locked default that then can't be toggled off
without a Superseded bump.

**Warning signs:** A `helm upgrade` that tries to flip a Project-level
verify default from on to off (or change its `gateCommand`) fails CRD
admission with the CEL immutability message, and the failure mode reads
identically to a genuine Task-contract-tampering rejection (making it
confusing to debug).

### Pitfall 5: Vacuous test filters against `TestControllers`

**What goes wrong (carried forward from Phase 51's own decision log):**
`internal/controller`'s sole Ginkgo entry point is `TestControllers`
(`suite_test.go:115`). A plan-check/level-verify unit test written with
`go test ./internal/controller/... -run TestPlanCheck` (or any name that
isn't `TestControllers` or an actual `func TestXxx` in a plain-Go file)
vacuously passes zero specs.

**Why it happens:** Ginkgo specs registered via `Describe`/`It` are only
reachable through the single `TestControllers` `go test` entry point;
`-run` filters against Ginkgo's OWN focus mechanism
(`--ginkgo.focus=`), not `go test -run`.

**How to avoid:** New Ginkgo-shaped specs for plan-check/level-verify must
run via `go test ./internal/controller/... -run TestControllers
--ginkgo.focus='PlanCheck|LevelVerify'` (or the unfiltered suite). Pure-Go
unit tests (the `repairOrHalt`/stall-detection-math shape, mirroring
`task_verify_loop_test.go`'s plain `func TestXxx(t *testing.T)` style)
should go in their OWN `plan_verify_loop_test.go`-shaped file exactly as
Phase 51 did (`51-03` decision log: "internal/controller's sole Ginkgo
entry point is TestControllers... the unit-test file is the repo's own
documented home for pure-function span tests").

**Warning signs:** A "green" CI run that never actually exercised the new
assertions — the exact class of gap `51-03`'s decision log names
explicitly.

## Code Examples

### Resolver shape (D-02), next to `ResolveProvider`

```go
// Source: pattern mirrors internal/controller/dispatch_helpers.go:283-330
// (ResolveProvider) — same file is a reasonable home per its own package
// doc comment ("three planner dispatch helpers... share").
//
// ResolveLoopPolicy computes the effective LoopPolicy for a dispatch at
// level, merging (in order): the level-appropriate default parameterization
// (task: from spec-authored MaxIterations, auto-repair; plan: MaxIterations:1;
// phase/milestone/project: MaxIterations:0) with the explicitly-authored
// VerificationSpec (Task > Plan > Project precedence, D-01) overriding those
// defaults on any non-zero field. Level is stamped onto the returned
// LoopPolicy.Level unconditionally — it is never authored, always resolved.
func ResolveLoopPolicy(
    project *tideprojectv1alpha3.Project,
    plan *tideprojectv1alpha3.Plan,
    task *tideprojectv1alpha3.Task,
    level string,
) tideprojectv1alpha3.LoopPolicy {
    // 1. Resolve the authored VerificationSpec per Task > Plan > Project.
    var spec tideprojectv1alpha3.VerificationSpec
    switch {
    case level == "task" && task != nil && task.Spec.Verification.GateCommand != "":
        spec = task.Spec.Verification
    case level == "plan" && plan != nil && plan.Spec.Verification.GateCommand != "":
        spec = plan.Spec.Verification
    default:
        if project != nil {
            if lvl := projectLevelVerificationDefault(project, level); lvl != nil {
                spec = *lvl
            }
        }
    }

    // 2. Level default parameterization when the spec leaves MaxIterations unset.
    maxIter := spec.MaxIterations
    if maxIter == 0 && spec.GateCommand != "" {
        switch level {
        case "plan":
            maxIter = 1
        case "phase", "milestone", "project":
            maxIter = 0 // never repair — always escalate
        }
    }

    return tideprojectv1alpha3.LoopPolicy{
        Level:            tideprojectv1alpha3.LoopLevel(level),
        MaxIterations:    maxIter,
        EscalationPolicy: tideprojectv1alpha3.EscalationPolicy(spec.OnExhaustion),
        EvaluatorRef:     spec.Evaluator,
    }
}
```

### `BuildOptions`/`BuildJobSpec` generalization (Pitfall 1's fix)

```go
// Source: mirrors internal/dispatch/podjob/jobspec.go:271-284 (the EXISTING
// JobKindPlanner case) applied to JobKindVerifier.
case JobKindVerifier:
    if opts.ParentObj != nil {
        parentUID = string(opts.ParentObj.GetUID())
    }
    jobName = VerifierJobName(opts.Level, parentUID, opts.Attempt)
    labels["tideproject.k8s/task-uid"] = parentUID // keep existing label key for verifierInFlightCount compat, or migrate to a level-generic key in the same commit
    labels["tideproject.k8s/role"] = "verifier"
    labels["tideproject.k8s/level"] = opts.Level
    if opts.EstimatedCostCents > 0 {
        labels["tideproject.k8s/estimated-cost"] = strconv.FormatInt(opts.EstimatedCostCents, 10)
    }
```

```go
// Source: mirrors internal/dispatch/podjob/names.go:52-54 (PlannerJobName).
func VerifierJobName(level, parentUID string, attempt int) string {
    return fmt.Sprintf("tide-verifier-%s-%s-%d", level, parentUID, attempt)
}
```

### `checkParentApproval`-style hold for Plan's `Verifying` phase (D-03)

```go
// Source: mirrors internal/controller/dispatch_helpers.go:578-603 exactly,
// extended with one more phase value.
func checkParentApproval(ctx context.Context, c client.Client, ns, parentName, parentKind string) (bool, error) {
    ...
    case "Plan":
        var plan tideprojectv1alpha3.Plan
        if err := c.Get(ctx, client.ObjectKey{Namespace: ns, Name: parentName}, &plan); err != nil {
            return false, client.IgnoreNotFound(err)
        }
        return plan.Status.Phase == tideprojectv1alpha3.LevelPhaseAwaitingApproval ||
            plan.Status.Phase == tideprojectv1alpha3.LevelPhaseVerifying, nil
    ...
}
```

## State of the Art

| Old Approach (Phase 51) | New Approach (Phase 52) | When Changed | Impact |
|--------------------------|---------------------------|---------------|--------|
| `task.Spec.Verification.MaxIterations`/`.OnExhaustion` read directly in `repairOrHalt`/`prepareDispatch` | `ResolveLoopPolicy(...).MaxIterations`/`.EscalationPolicy` read instead | This phase | Task's own behavior stays identical at default settings (D-02's explicit no-regression bar), but the DECISION SURFACE moves from raw spec field to resolved value — any future code must read through the resolver, not the spec, to stay consistent with SC3 |
| `OnExhaustion` declared but never read (`escalate`/`requireApproval` collapse to the same `haltVerify` call) `[VERIFIED: zero controller reads of OnExhaustion via grep]` | `EscalationPolicy` differentiated: `requireApproval` → `AwaitingApproval` park; `escalate` → `ConditionVerifyHalt` | This phase | Task's own exhaustion behavior CHANGES for `onExhaustion: requireApproval` contracts — previously identical to `escalate`, now genuinely parks instead of freezing the whole project. Flag this explicitly in the plan's verification steps — it is a real behavior change for Task, not just new levels. |
| `podjob.BuildOptions.Kind == JobKindVerifier` requires `opts.Task` | Requires `opts.ParentObj` (any level), mirrors `JobKindPlanner` | This phase | Existing Task verifier dispatch call site (`task_controller.go:2167`) must pass `ParentObj: task` alongside `Task: task` (or drop the now-redundant `Task` field usage inside the Kind switch) |
| `reporter.MaterializeChildCRDs` never deletes | A NEW controller-side delete step precedes re-materialization on REPAIRABLE plan-check | This phase | `MaterializeChildCRDs` itself is UNCHANGED (still Create-only) — the delete is a separate, PlanReconciler-owned action, keeping the shared reporter helper's contract simple for its other 4 callers (Milestone/Phase/Project/Task materialization, none of which re-plan) |

**Deprecated/outdated:** None — this phase does not remove any existing
capability; it strictly adds parameterization and closes a declared-but-
dead field (`OnExhaustion`).

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|-------|---------|---------------|
| A1 | The recommended worktree-provisioning shape (new init container, reusing the `tide-push` git-writer image, no new branch) is the right mechanism — CONTEXT.md does not lock this, and no prior phase built anything like it | Architecture Patterns §"The missing worktree" | If the plan picks a different mechanism (e.g., a controller-orchestrated pre-dispatch Job instead of an init container), the estimated complexity/risk for Phase/Milestone/Project verify dispatch could be materially different — this is the single largest unknown in the phase |
| A2 | Delete-then-recreate is the right re-plan child-Task reconciliation shape — CONTEXT.md explicitly leaves this "Claude's Discretion," and the evidence (Create-only materializer + the `len(taskList.Items) > 0` early return) strongly constrains the space but doesn't prove no alternative works | Architecture Patterns §"Child-Task reconciliation on re-plan" | An in-place-update alternative would touch shared `reporter` code used by 4 other materialization call sites — higher blast radius than assumed here if the planner picks that path instead |
| A3 | Reusing the existing `tideproject.k8s/task-uid` label key for non-Task verifier Jobs (rather than introducing a level-generic label name) is acceptable, since `verifierInFlightCount` only filters on `role=verifier` + project label, never on the task-uid label's VALUE matching a real Task | Code Examples, `BuildOptions` generalization | If some other consumer (dashboard, a future audit tool) greps for `tideproject.k8s/task-uid` expecting it to always resolve to a real Task CRD, a Plan/Phase/Milestone/Project verifier Job carrying that label with a non-Task UID would break that assumption silently |
| A4 | Project-level per-level `VerificationSpec` defaults should be authored as `Phase: "Draft"` (not `"Locked"`) to avoid the CEL immutability trap (Pitfall 4) | Common Pitfalls #4 | This is a documentation/convention recommendation, not enforced by any new schema constraint — if Phase 53's chart defaults ship `Locked` anyway, the trap fires exactly as described |

**If this table is empty:** N/A — see entries above; all four are
genuinely open design choices this research could evidence but not fully
resolve from existing code alone.

## Open Questions

1. **What image runs the new level-verify worktree-checkout init container?**
   - What we know: `pkg/git.AddWorktree` already exists (Go, uses the
     system `git` CLI) and is currently only invoked from
     `cmd/claude-subagent` (the executor image) via `internal/harness`. The
     `tide-push`/git-writer image already ships a git binary for boundary
     pushes.
   - What's unclear: whether reusing the `tide-push` image (adding a new
     CLI mode/flag to it) or building a small dedicated init-container
     binary is preferred — this affects `r.Deps` wiring in `cmd/manager/
     main.go` and Helm chart plumbing (though the chart itself is Phase 53).
   - Recommendation: the plan should treat this as its own scoped design
     decision at plan-write time, informed by whichever is cheaper to wire
     through the existing `BuildOptions`/`BuildJobSpec` init-container
     composition point.

2. **Does the Task verifier's Job name change shape?**
   - What we know: `VerifierJobName(taskUID, attempt)` → `tide-verifier-
     <taskUID>-<attempt>` today; the recommended generalization is
     `VerifierJobName(level, parentUID, attempt)` → `tide-verifier-<level>-
     <parentUID>-<attempt>`.
   - What's unclear: whether any existing kind spec, dashboard query, or
     test asserts on the EXACT current Task verifier Job name string. A
     grep pass at plan-write time should confirm before committing to the
     name-format change.
   - Recommendation: grep `tide-verifier-` across `test/integration/kind/`
     and `cmd/dashboard/` before finalizing the signature change; if any
     hard-coded assertion exists, either update it in the same commit or
     special-case `level=="task"` to omit the level segment for backward
     compatibility.

3. **Does `Plan.Status.LoopStatus.Iteration` double as the planner-attempt
   counter, or are they two separate numbers?**
   - What we know: D-06 locks `LoopStatus.Iteration` to "quality re-plans
     only," explicitly distinct from planner infra-retry identity. The
     Job-name attempt number (`tide-plan-<uid>-<attempt>`) has historically
     been the INFRA-retry identity slot on every other level (mirrors
     Task's `Attempt` vs `LoopStatus.Iteration` split, Phase 51 D-05).
   - What's unclear: whether Plan needs a wholly separate infra-retry
     attempt counter distinct from `LoopStatus.Iteration`, or whether
     (since plan planner dispatch has never had genuine infra-retry before
     this phase — it was hardcoded to attempt=1) the two can be the SAME
     number for Plan specifically, since Plan never had eviction-retry
     semantics to preserve.
   - Recommendation: treat as a plan-time decision; the safest default
     (matching Task's precedent exactly) is two separate counters, but the
     added complexity may not be justified given Plan planner Jobs have no
     pre-existing infra-retry path to preserve.

## Environment Availability

No new external tools/services are introduced by this phase — it dispatches
already-configured images (`tide-langgraph-verifier`, `tide-push`) at new
call sites and adds Go/CRD code. The existing dev environment (kind, `make
test-int`, `make lint`, envtest) already covers everything this phase
needs; no new probes are required.

| Dependency | Required By | Available | Version | Fallback |
|------------|------------|-----------|---------|----------|
| `tide-langgraph-verifier` image | New verify dispatch sites (plan/phase/milestone/project) | ✓ (already built, Phase 48/51) | current dev-head | — |
| `tide-push` (or equivalent git-writer) image | Recommended worktree-provisioning init container | ✓ (already built, boundary pushes) | current dev-head | A dedicated new small image if reuse proves awkward |
| kind / kubectl / go 1.26 toolchain | Live verification of new dispatch sites | ✓ (established, per CLAUDE.md operating notes) | pinned per `go.mod`/CLAUDE.md | — |

**Missing dependencies with no fallback:** none.

**Missing dependencies with fallback:** none — the one genuinely open
question (which image runs the worktree-checkout init container) has a
viable existing-image fallback (`tide-push`) even if a dedicated image is
preferred later.

## Validation Architecture

### Test Framework

| Property | Value |
|----------|-------|
| Framework | Go `testing` (plain unit tests) + Ginkgo v2.28/Gomega (envtest specs) |
| Config file | none — `internal/controller/suite_test.go` boots envtest programmatically |
| Quick run command | `go test ./internal/controller/... -run TestXxx` (plain-Go unit tests) or `go test ./api/v1alpha3/... -run TestXxx` for schema-only tests |
| Full suite command | `go test ./internal/controller/... -run TestControllers --ginkgo.focus='<Spec name>'` for Ginkgo specs; `make test-int` for the full kind-backed integration layer (note: bundles plain go-tests too — always grep `^--- FAIL\|^FAIL\s`, never trust the Ginkgo summary line alone, per CLAUDE.md) |

### Phase Requirements → Test Map

| Req ID | Behavior | Test Type | Automated Command | File Exists? |
|--------|----------|-----------|-------------------|-------------|
| ESC-01 (resolver) | `ResolveLoopPolicy` returns level-correct defaults + honors Task>Plan>Project precedence | unit | `go test ./internal/controller/... -run TestResolveLoopPolicy` | ❌ Wave 0 |
| ESC-01 (SC1: plan-check own counter, `maxIterations:1`) | REPAIRABLE verdict triggers exactly one re-plan, then escalates | envtest (Ginkgo) | `go test ./internal/controller/... -run TestControllers --ginkgo.focus='PlanCheck'` | ❌ Wave 0 |
| ESC-01 (SC1: severity-weighted stall) | A `maxIterations:2` re-plan that does NOT improve its finding score halts before consuming the 2nd iteration | unit | `go test ./internal/controller/... -run TestPlanVerifyLoop_Stall` | ❌ Wave 0 |
| ESC-01 (SC2: maxIterations:0 escalation) | Phase/Milestone/Project verify finding escalates straight to `requireApproval`/`escalate`, never repairs | envtest (Ginkgo) | `go test ./internal/controller/... -run TestControllers --ginkgo.focus='LevelVerify'` | ❌ Wave 0 |
| ESC-01 (SC3: single resolver, no kind-switch) | No controller file contains a CRD-kind switch to pick gate policy | static grep-proof | `grep -rn "switch.*\.(type)" internal/controller/*.go \| grep -i "gatepolicy\|escalat"` (or an equivalent compile-time/lint assertion) | ❌ Wave 0 |
| D-04 (re-plan child-Task supersede) | A re-plan deletes the rejected attempt's Tasks before re-dispatching; a stale Task never reaches Running | envtest (Ginkgo) | `go test ./internal/controller/... -run TestControllers --ginkgo.focus='RePlan'` | ❌ Wave 0 |
| D-08 (onExhaustion differentiation, all levels incl. Task) | `requireApproval` parks at `AwaitingApproval`; `escalate` stamps `ConditionVerifyHalt` — proven at Task AND at least one new level | unit + envtest | `go test ./internal/controller/... -run TestOnExhaustion` + Ginkgo focus | ❌ Wave 0 |
| ESC-03 (regression: VerifyHalt at new levels doesn't reinterpret Failed) | Sibling/conservative-profile propagation untouched by a Phase/Milestone/Project VerifyHalt | envtest (Ginkgo) | `go test ./internal/controller/... -run TestControllers --ginkgo.focus='VerifyHalt.*Sibling'` | ❌ Wave 0 (extends existing `verify_halt_test.go` coverage to new levels) |
| D-10 (concurrency/budget/span at new sites) | New verifier dispatch sites are counted by `verifierInFlightCount`, reserve via `ReservationStore`, fail-closed via `ClassifyVerdict`, emit `EVALUATOR` sibling spans | envtest (Ginkgo) + unit | extends existing `TestVerifierInFlightCount`-style coverage per new dispatch site | ❌ Wave 0 |

### Sampling Rate
- **Per task commit:** the relevant plain-Go unit test file for the
  function just touched (e.g. `go test ./internal/controller/... -run
  TestResolveLoopPolicy`).
- **Per wave merge:** `go test ./internal/controller/... -run
  TestControllers --ginkgo.focus='<this wave's specs>'`.
- **Phase gate:** full `make test-int` green (grep `^--- FAIL\|^FAIL\s`,
  not just the Ginkgo summary) before `/gsd:verify-work`.

### Wave 0 Gaps
- [ ] `internal/controller/dispatch_helpers_loop_policy_test.go` (or
      similarly named) — `TestResolveLoopPolicy` covering Task>Plan>Project
      precedence + level-default parameterization.
- [ ] `internal/controller/plan_verify_loop_test.go` — plain-Go unit tests
      for plan-check stall detection math, mirroring
      `task_verify_loop_test.go`'s shape.
- [ ] New `Describe`/`It` blocks in the Ginkgo suite (likely a new
      `plan_verify_dispatch_test.go`/`level_verify_dispatch_test.go`
      alongside the existing `task_verify_dispatch_test.go`) for the
      dispatch-and-consume state machine at each new level.
- [ ] A live kind proof (mirroring Phase 51's `51-08` checkpoint) for AT
      LEAST the worktree-provisioning mechanism — envtest cannot observe a
      real PVC/worktree, so this gap is only closeable live (see Common
      Pitfall #2).

*(No existing test infrastructure covers per-level parameterization —
`task_verify_dispatch_test.go`/`task_verify_loop_test.go`/
`verify_halt_test.go` are the direct precedent shape to extend, not reuse
directly.)*

## Security Domain

### Applicable ASVS Categories

| ASVS Category | Applies | Standard Control |
|---------------|---------|-----------------|
| V1 Architecture | yes | Fail-closed by construction is the phase's own design bar (D-10): `ClassifyVerdict` never defaults to APPROVED; the resolver must preserve this for every new level, not just Task |
| V2 Authentication | no | No new auth surface — reuses existing credproxy/signed-token flow unmodified |
| V4 Access Control | yes | `requireApproval` parking is a human-authorization gate; must not be bypassable by an operator racing `tide approve` against a still-in-flight verify (mirrors the existing `consumeApproveAndResume` race protections, reused verbatim) |
| V5 Input Validation | yes | CEL `x-kubernetes-validations` on `VerificationSpec` (immutable-once-Locked) — new embedding sites inherit this automatically; the resolver must not accept an unauthenticated/unlocked `GateCommand` as authoritative at any level, matching Task's existing `hasVerificationContract` requiring `Phase=="Locked"` |
| V6 Cryptography | no | No new cryptographic surface — signed-token minting is unchanged, reused as-is for the new verifier dispatch sites |

### Known Threat Patterns for this stack

| Pattern | STRIDE | Standard Mitigation |
|---------|--------|---------------------|
| A malicious/buggy re-plan author edits a protected fixture/evaluator path to force a future APPROVED at a level other than Task | Tampering | Task-06's anti-gaming `protectedEvaluatorFixturePaths` check (`task_controller.go:2337-2367`) is currently wired ONLY into Task's `repairOrHalt` — confirm whether Plan's re-plan path needs the SAME belt-and-suspenders check (a re-plan editing `internal/eval/`/`evals/`/verifier templates to game its own rubric is structurally the same threat class Task-06 already defends against) |
| A stale/superseded child Task CRD dispatches despite a REPAIRABLE plan-check verdict | Tampering / Elevation of Privilege (spends budget on rejected work) | The delete-then-recreate re-plan mechanism (Architecture Patterns) + the new `checkParentApproval` extension for `Verifying` — BOTH must land together; either alone is an incomplete mitigation |
| An operator's `tide approve` targets the wrong parked level when Plan-check AND a downstream level's `requireApproval` are BOTH parked simultaneously | Repudiation / confused-deputy | `cmd/tide/approve.go`'s existing `findAwaiting*` chain already walks Milestone→Phase→Plan→Task in a fixed, deterministic order (`approve.go:176-191`) — confirm the new `findAwaitingProject` (if added) is inserted at the CORRECT position in that chain (before Milestone, since Project is the root) so the deterministic-order guarantee holds |
| A level-verify worktree checkout (new init container) is given write credentials it doesn't need | Elevation of Privilege | The new worktree-provisioning mechanism must mount the SAME read-only-worktree contract Task's verifier already enforces (ReadOnly `/workspace`, no git-push credentials, no `ANTHROPIC_API_KEY` in the checkout container) — do not reuse `tide-push`'s FULL credential set for a checkout-only init container; scope it to read access on the bare repo only |

## Sources

### Primary (HIGH confidence — direct source reads this session)
- `api/v1alpha3/loop_types.go`, `task_types.go`, `plan_types.go`,
  `project_types.go`, `shared_types.go` — full CRD schema for `LoopPolicy`/
  `LoopStatus`/`VerificationSpec`/`Gates`/`LevelPhase*`/`ConditionVerifyHalt`
- `internal/controller/task_controller.go` (lines 346-518, 624-2929) — the
  entire Task-loop state machine: `gateChecks`, `checkRunningState`,
  `checkVerifyingState`, `dispatchVerifier`, `buildVerifierEnvelopeIn`,
  `haltVerify`, `markVerifiedSucceeded`, `repairOrHalt`,
  `dispatchRepairAttempt`, `handleVerifierCompletion`
- `internal/controller/dispatch_helpers.go` (lines 1-604) — `ResolveProvider`,
  `levelOverrideKey`, `plannerInFlightCount`, `verifierInFlightCount`,
  `checkParentApproval`
- `internal/controller/plan_controller.go` (lines 121-945, 1299-1470) —
  `Reconcile`, `reconcilePlannerDispatch`, `handlePlannerJobCompletion`,
  `patchPlanSucceeded`, `reconcileWaveMaterialization`
- `internal/controller/phase_controller.go` (lines 760-889),
  `internal/controller/project_controller.go` (lines 505-596, 1440-1520) —
  the three `patch{Level}Succeeded` call-site families
- `internal/controller/level_status.go`, `verify_halt.go` — shared
  status-patch primitives, `ConditionVerifyHalt` machinery
- `internal/reporter/materialize.go` — `MaterializeChildCRDs`,
  `ChildrenAlreadyMaterialized` (Create-only confirmed)
- `internal/dispatch/podjob/jobspec.go`, `names.go` — `BuildOptions`,
  `BuildJobSpec`'s `JobKindVerifier` case, `VerifierJobName`,
  `PlannerJobName`
- `internal/harness/worktree.go`, `pkg/git/worktree.go`,
  `pkg/git/integrate.go` — `EnsureWorktree`, `AddWorktree`,
  `TaskBranchName` (executor-only worktree provisioning confirmed)
- `pkg/dispatch/envelope.go`, `verdict.go` — `EnvelopeIn`/`EnvelopeOut`,
  `VerifyContext`, `ClassifyVerdict`, `GateDecision`
- `internal/subagent/common/prompt_templates.go`,
  `internal/subagent/common/templates/task_verifier.tmpl` — the loader
  contract and the exact prompt shape to mirror for the four new templates
- `internal/gates/policy.go`, `annotation.go`, `boundary.go` —
  `EvaluatePolicy`, `CheckApprove`/`ConsumeApprove`/`CheckRejected`,
  `BoundaryDetected`
- `cmd/tide/approve.go`, `resume.go` — `findAwaiting*` chain, existing
  `tide approve`/`tide resume` CLI surfaces
- `api/v1alpha3/loop_types_test.go` — `TestLoopStatus_NoForbiddenFields`
  compile-time guard pattern
- `.planning/phases/52-.../52-CONTEXT.md`, `.planning/REQUIREMENTS.md`,
  `.planning/STATE.md` — locked decisions, ESC-01 text, Phase 48-51
  decision-log precedents cited throughout

### Secondary (MEDIUM confidence)
None — every claim in this document traces to a direct source read or the
CONTEXT.md/REQUIREMENTS.md/STATE.md inputs listed above. No web search was
performed; this is a pure internal-codebase generalization phase with no
external library/API surface to verify against Context7 or official docs.

### Tertiary (LOW confidence)
None.

## Metadata

**Confidence breakdown:**
- Standard stack (reused machinery): HIGH — every cited function/type was
  read directly this session, with line numbers.
- Architecture (resolver, Plan-check mechanics, Task migration): HIGH — the
  resolver shape and Task-migration path are directly evidenced by
  existing `ResolveProvider`/`Gates` precedent and the CONTEXT.md's own
  explicit mapping.
- Architecture (level-verify worktree provisioning): MEDIUM — the GAP is
  HIGH confidence (directly proven by reading `EnsureWorktree`'s
  Role-gate), but the RECOMMENDED SHAPE (new init container, reuse
  `tide-push` image) is a reasoned recommendation, not something any prior
  phase built or validated — flagged as Assumption A1.
- Architecture (child-Task re-plan reconciliation): MEDIUM — the GAP is
  HIGH confidence (directly proven by reading `MaterializeChildCRDs` and
  `reconcilePlannerDispatch`'s early-return), the RECOMMENDED SHAPE
  (delete-then-recreate) is well-constrained by the evidence but is still
  a design choice — flagged as Assumption A2.
- Pitfalls: HIGH — all five are directly evidenced by source reads, not
  inferred from documentation or training knowledge.

**Research date:** 2026-07-20
**Valid until:** Effectively pinned to the current `main` HEAD of this
repository — any subsequent commit touching `task_controller.go`,
`plan_controller.go`, `podjob/jobspec.go`, or `harness/worktree.go` before
this phase is planned should trigger a re-read of the affected section
before the plan locks task actions.
