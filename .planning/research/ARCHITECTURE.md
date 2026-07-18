# Architecture Research — v1.0.9 "Slack Tide": Verify Tier Integration

**Domain:** In-cluster verify tier for a K8s-native hierarchical-DAG orchestrator (TIDE)
**Researched:** 2026-07-18
**Confidence:** HIGH (every integration point below is grep-confirmed against the current `main` tree; the prompt-source and findings-persistence calls are MEDIUM — reasoned recommendations, not yet built)

## Standard Architecture

### System Overview

```
┌───────────────────────────────────────────────────────────────────────────┐
│  Reconcilers (internal/controller) — one per CRD level                    │
│  Project → Milestone → Phase → Plan → Task                                │
│                                                                             │
│  Each level's planner dispatch produces children via                      │
│  BuildPlannerEnvelope + MaterializeChildCRDs. Each level's completion      │
│  is detected by gates.BoundaryDetected(parent, childKind) — the SAME       │
│  function at all 4 levels — which gates patch*Succeeded / Complete.       │
├───────────────────────────────────────────────────────────────────────────┤
│  NEW: verify tier hooks into the two state-transition edges every level   │
│  already has:                                                             │
│                                                                             │
│   edge A — planner Job completed, children about to materialize/dispatch  │
│            (plan_controller.go handlePlannerJobCompletion, ValidationState │
│            gate at reconcileWaveMaterialization:1304)                     │
│            → PLAN-CHECK hooks here (Plan level only, per milestone doc)   │
│                                                                             │
│   edge B — gates.BoundaryDetected(parent, childKind) == true, BEFORE      │
│            patch*Succeeded/Complete                                      │
│            → LEVEL-VERIFY hooks here at Plan/Phase/Milestone              │
│            → INTEGRATION-CHECK hooks at the SAME edge B, Milestone/Project│
├───────────────────────────────────────────────────────────────────────────┤
│  pkg/dispatch.Subagent (UNCHANGED interface) — PodJob backend             │
│  (internal/dispatch/podjob/backend.go) creates a per-dispatch K8s Job;    │
│  credproxy (internal/credproxy) mints/validates the signed token          │
│                                                                             │
│  NEW: role="verifier" dispatches on a NEW LangGraph/Python image,         │
│  selected the same way every other dispatch selects its image today:     │
│  Levels.<level>.Image → Spec.Subagent.Image → helm default (resolveImage,│
│  dispatch_helpers.go:418)                                                 │
├───────────────────────────────────────────────────────────────────────────┤
│  Project-level halt classes (status conditions, checked at every dispatch │
│  gate before Job creation): BillingHalt, FailureHalt (execution-only)     │
│  → NEW: VerifyHalt (mirrors FailureHalt exactly, including the Phase 25   │
│  resume-ordering fix)                                                     │
├───────────────────────────────────────────────────────────────────────────┤
│  Git-as-artifact-store (v1.0.7): .tide/planning/<kind>/<name>/ on the run │
│  branch, staged via collectStageEnvelopes + cmd/tide-push, read via       │
│  gitfetch (cmd/dashboard/gitfetch.go)                                     │
│  → NEW: same tree gets a verify.json (or per-stage) findings artifact     │
└───────────────────────────────────────────────────────────────────────────┘
```

### Component Responsibilities

| Component | Responsibility | File:line (grep-confirmed) |
|-----------|----------------|------------------------|
| `gates.BoundaryDetected` | "All children of childKind Succeeded" — the ONE function all 4 levels call to gate their own Succeeded/Complete stamp | `internal/gates/boundary.go:66` |
| Plan `handlePlannerJobCompletion` | Planner Job completes → reporter materializes Task/Wave children → `ValidationState=Validated` stamped → arms `reconcileWaveMaterialization` | `internal/controller/plan_controller.go:507`, stamp at `:794`, gate consumed at `:1304` |
| `reconcileWaveMaterialization` | Derives waves from the Task DAG once `ValidationState` is armed; this is the point past which Tasks become dispatchable | `internal/controller/plan_controller.go:1299`, gate check `:1304` |
| `patchPlanSucceeded` / `patchPhaseSucceeded` / `patchMilestoneSucceeded` / `checkProjectComplete` | Stamp the terminal Succeeded/Complete transition — called ONLY after `BoundaryDetected==true` | `plan_controller.go:927`, `phase_controller.go:879`, `milestone_controller.go:945`, `project_controller.go:1442` |
| `checkBillingHalt` / `checkFailureHalt` | Project-wide dispatch-hold predicates read at every dispatch gate | `internal/controller/billing_halt.go:78`, `internal/controller/failure_halt.go:56` |
| `checkDispatchHolds` | Shared planner-tier hold chain (Milestone/Phase/Plan) — Billing→Failure→Budget→Import | `internal/controller/dispatch_helpers.go:580` |
| `TaskReconciler.gateChecks` | Task-tier hold chain (own order: Reject→ParentApproval→Import→Billing→Failure→Budget→reservation) | `internal/controller/task_controller.go:334` |
| `cmd/tide/resume.go:resumeRun` | The one sanctioned recovery verb; `--retry-failed` resets Failed levels THEN clears FailureHalt (Phase 25 CR-01 ordering fix) | `cmd/tide/resume.go:73`, ordering comment `:154-165` |
| `BuildPlannerEnvelope` / `TaskReconciler.buildEnvelopeIn` | Construct `EnvelopeIn` for planner/executor dispatch — the pattern a verifier dispatch reuses | `internal/controller/dispatch_helpers.go:369`, `internal/controller/task_controller.go:1750` |
| `common.LoadPromptTemplate(role, level)` | Compiled-in Go template loader, `templates/<level>_<role>.tmpl` via `go:embed` | `internal/subagent/common/prompt_templates.go:65` |
| `resolveImage` | Levels.\<level\>.Image → Spec.Subagent.Image → helm default | `internal/controller/dispatch_helpers.go:418` |
| `collectStageEnvelopes` + `cmd/tide-push` | Stages planning artifacts into `.tide/planning/<kind>/<name>/` on the run branch at boundary push | `internal/controller/artifact_push.go:84`, `cmd/tide-push/main.go:1128` |
| `pkg/dispatch.SelfInstruments(vendor)` | Runtime-neutral capability flag deciding whether the reporter synthesizes spans from `events.jsonl` or the runtime emits natively | `pkg/dispatch/vendor_capabilities.go:35` |

## Answering the seven integration questions

### Q1 — Where do the 3 stages dispatch from, gated on which state-transition edges?

All three stages gate off **existing durable status transitions**, never an in-memory "did I already do this" (same discipline the retroactive-span-synthesis decision already locked for Phase 42/43 — see PROJECT.md Key Decisions: "creation is gated off the same state-transition edges that gate Job creation").

**plan-check** — Plan level only (per the locked architecture doc; not Phase/Milestone, because those levels' "children" are lower CRDs whose own plan-check happens one level down at the Plan that authors Tasks).

- Edge: `handlePlannerJobCompletion` (`plan_controller.go:507`) has already (a) spawned the reporter Job that materializes Task/Wave CRDs from the planner's `out.json`, and (b) is about to stamp `plan.Status.ValidationState = "Validated"` (`plan_controller.go:794`) — the exact stamp that arms `reconcileWaveMaterialization`'s dispatch gate (`plan_controller.go:1304`: `if plan.Status.ValidationState != "Validated" && ... != "FileTouchMismatch"`).
- **Recommended hook:** insert a NEW status field, e.g. `Plan.Status.PlanCheckState` (`""`/`Pending`/`Approved`/`Rejected`), orthogonal to `ValidationState` (which already carries the DAG-admission-webhook's Validated/CycleDetected/FileTouchMismatch vocabulary — overloading a 4th unrelated meaning into that one string is exactly the kind of collapsed-level smell CLAUDE.md warns against). Stamp `PlanCheckState=Pending` immediately after the reporter spawn / ChildCount-gated succession completes (i.e., once Tasks are materialized on the API server, not before — the verifier needs real Task CRDs + the run-branch PLAN.md to read). Dispatch the plan-check verifier Job there. `reconcileWaveMaterialization`'s existing gate at `:1304` gets ONE additional AND-clause: `plan.Status.PlanCheckState == "Approved"` (skip the clause entirely when the stage is configured off — see Q6). This is a state-transition edge exactly like the existing `ValidationState` gate, not a new in-memory check.
- On REJECT: do NOT touch `ValidationState`. Instead re-dispatch the Plan's own planner (same seam as the original `reconcilePlannerDispatch`, but with the plan-check findings appended to the prompt) and increment a NEW counter, e.g. `Plan.Status.PlanCheckAttempts int32`. This mirrors the existing `Task.Status` attempt-counter shape (`task_controller.go:729`, gated by `project.Spec.MaxAttemptsPerTask`) but is its own field — see Q2 rationale for why it should NOT share `MaxAttemptsPerTask`.

**level-verify** — Plan / Phase / Milestone (the three levels with real children below them).

- Edge: the moment `gates.BoundaryDetected(ctx, r.Client, parent, childKind) == true`, BEFORE the corresponding `patch*Succeeded` call:
  - Plan: `plan_controller.go:1428` (`BoundaryDetected(plan, "Task")`) → `:1433` (`patchPlanSucceeded`)
  - Phase: `phase_controller.go:801` and `:826` (`BoundaryDetected(ph, "Plan")`) → `patchPhaseSucceeded` (`:879`)
  - Milestone: `milestone_controller.go:864` and `:891` (`BoundaryDetected(ms, "Phase")`) → `patchMilestoneSucceeded` (`:945`)
- **Recommended hook:** the same shape as plan-check — a new orthogonal field, e.g. `<Level>.Status.VerifyState` (`""`/`Pending`/`Approved`/`Blocked`), stamped to `Pending` + verifier Job dispatched the instant `BoundaryDetected` first returns true (that "true" observation is itself a durable transition worth recording — don't recompute `BoundaryDetected` from scratch on every reconcile to decide whether to (re-)dispatch; latch on the level's own status once dispatched, exactly like `PlanRolledUpUID`/`PlanSpanEmittedUID` latch idempotent side effects elsewhere in this exact function). `patch*Succeeded` gains one AND-clause: `VerifyState == "Approved"`.
- BLOCKED → do NOT re-dispatch anything. Halt the whole project via `ConditionVerifyHalt` (Q2) — this is a "discard paid work only with a human's say-so" call per the locked Pillar 3.

**integration-check** — Milestone and Project boundaries. Same edge as level-verify:
- Milestone: same `BoundaryDetected(ms,"Phase")` edge as level-verify above (Milestone's level-verify and its integration-check are candidates for the SAME dispatch — see Q7).
- Project: `checkProjectComplete` (`project_controller.go:1442`), gated on `BoundaryDetected(project, "Milestone")` (`:1443`), before `project.Status.Phase = PhaseComplete` (`:1451`).

### Q2 — `ConditionVerifyHalt` + resume path (Phase 25 ordering fix applied)

Mirror `ConditionFailureHalt` (`failure_halt.go`) exactly:

```go
const ConditionVerifyHalt = "VerifyHalt"        // api/v1alpha3/shared_types.go, alongside
                                                  // ConditionBillingHalt (:270) / ConditionFailureHalt (:324)
const ReasonVerifyBlocked = "VerifyBlocked"
const AnnotationVerifyResumedAt = "tideproject.k8s/verify-resumed-at"  // mirrors
                                                  // AnnotationFailureResumedAt
```

- `checkVerifyHalt(project) bool` — nil-safe, `meta.IsStatusConditionTrue(..., ConditionVerifyHalt)` — same shape as `checkFailureHalt` (`failure_halt.go:56`).
- `setVerifyHaltIfNeeded(ctx, c, project, verifyCompletedAt time.Time) error` — stamped when a level-verify or integration-check dispatch returns `gate_decision: BLOCKED`. Idempotent (no-op if already True). **Apply the Phase 25 CR-02 time-fence exactly**: refuse to re-stamp when `verifyCompletedAt` predates `AnnotationVerifyResumedAt` — this is the identical bug class Phase 25 hit (a straggler verify dispatch reconciling after resume re-freezes the project).
- Add `checkVerifyHalt` to `checkDispatchHolds` (`dispatch_helpers.go:580`, planner tier) AND to `TaskReconciler.gateChecks` (`task_controller.go:334`, task tier) — VerifyHalt must block BOTH tiers, unlike `FailureHalt` which is deliberately execution-only (its doc comment at `failure_halt.go:33-37` explains why planning must stay ungated for FailureHalt). VerifyHalt is different: a BLOCKED integration-check or level-verify means the artifact tree is suspect — new planning built on top of unverified artifacts compounds the problem. **Recommendation: gate both tiers**, and say so explicitly in the implementing plan (don't silently copy FailureHalt's planner-exemption).
- Resume path — extend `cmd/tide/resume.go:resumeRun` with the SAME two-phase ordering Phase 25 proved necessary for FailureHalt (`resume.go:154-165` comment block): **reset the BLOCKED level(s) FIRST, clear `VerifyHalt` LAST**, then stamp `AnnotationVerifyResumedAt`. A new `--retry-failed`-style flag (or folding into the existing flag) resets any level parked at the new `VerifyState=Blocked` back to a re-dispatchable state before the halt clears — otherwise the same race Phase 25 CR-01 fixed (a still-Blocked level re-stamping the halt between the clear and the reset) reproduces identically for VerifyHalt.
- `plan-check REJECT` does NOT go through `ConditionVerifyHalt` at all — it's the bounded re-plan loop (Q1), which only escalates to a halt after `PlanCheckAttempts` exceeds its cap (see Q2 counter design below). Only `level-verify`/`integration-check` BLOCKED trips `ConditionVerifyHalt` directly (per Pillar 3 in the milestone doc: "post-execution re-work discards paid work; that call is the operator's").
- **Re-plan loop counter: give it its own field, not `MaxAttemptsPerTask`.** `MaxAttemptsPerTask` (`project_types.go:420`) is scoped to Task-level EXECUTION retries — a materially different cost class (Haiku-tier task re-dispatch) than a Plan-level re-authoring loop (planner-tier model, full PLAN.md re-author). Conflating them means an operator who raises `MaxAttemptsPerTask` for flaky task retries silently also raises re-plan cost, and vice versa. Add `Project.Spec.MaxPlanCheckAttempts int32` (Helm default 1–2, per the open question) and `Plan.Status.PlanCheckAttempts int32`, checked the same way `prepareDispatch` checks `maxAttempts` (`task_controller.go:729-742`) — exceed it → `ConditionVerifyHalt` (not `Failed`; a plan that can't pass plan-check after N tries needs a human, not a terminal failure stamp that discards the Plan's DAG state).

### Q3 — Verifier prompt source: orchestrator-side Go template (recommend, not in-image Python)

**Recommendation: orchestrator-side, a new `role="verifier"` template class rendered by the manager exactly like the 5 existing templates, passed into the envelope's `Prompt` field verbatim (`EnvelopeIn.Prompt`, `pkg/dispatch/envelope.go:65`) — the same channel `BuildPlannerEnvelope` already uses (`dispatch_helpers.go:369`, `prompt` param assigned to `envIn.Prompt` at `:376`).**

Why, weighed against in-image Python:

| | Orchestrator-side Go template (recommended) | In-image Python template |
|---|---|---|
| Consistency with existing 5 templates | Identical mechanism — `common.LoadPromptTemplate(role, level)` (`prompt_templates.go:65`), `go:embed`, zero runtime FS dependency (the CLAUDE.md anti-pattern comment at `prompt_templates.go:25-30` applies verbatim) | A SIXTH prompt-authoring mechanism, in a second language, reviewed by a different toolchain |
| Prompt drift across languages | None — one source of truth in Go, byte-identical rendering logic reused (`text/template`, same `EnvelopeIn`-shaped context) | The polyglot doc's own "Q2 drift problem" (referenced directly in the milestone doc's Open Questions #3) — a Python Jinja2/f-string reimplementation WILL drift from the Go templates' structure/conventions over multiple iterations, with no compiler or shared test enforcing parity |
| Iteration loop | Operators/maintainers who tune the 5 existing templates already know where to look; a verifier prompt sits in the same directory (`internal/subagent/common/templates/`), reviewed in the same PRs | Requires touching the Python image's own prompt module, a different build/test/release cadence than the Go binary |
| Coverage-not-conservatism prompting note | Directly reusable — CLAUDE.md's subagent-tuning note is written for Go compiled-in templates ("Write compiled-in templates for literal instruction-following") and already anticipates this exact verifier ("A future review/verify subagent must prompt for coverage, not conservatism") | Same content would need re-deriving/re-validating against Python string formatting idioms |
| Envelope contract purity | `EnvelopeIn.Prompt` already exists and is passed for planner dispatches; the verifier just becomes a third `Role` value alongside `"planner"`/`"executor"` — literally zero new envelope fields needed for the prompt itself (only NEW envelope fields are the verify-specific data payload, Q4) | Requires the LangGraph image to independently interpret `Level`, findings-severity conventions, deliverable lists, etc. from raw structured envelope data with NO governing template — reinventing the entire prompt-construction logic per image |
| LangGraph fit | LangGraph's node functions consume a plain string system/user prompt just as readily as a Python-authored one — `with_structured_output` operates on the OUTPUT schema, not the input prompt construction. Rendering server-side changes nothing about how LangGraph nodes reason. | No fit advantage — LangGraph does not require Python-side prompt templating |
| Cost | One new `.tmpl` file (or 3, per Q7) + `LoadPromptTemplate` call site in a new `internal/controller/dispatch_helpers.go`-style `BuildVerifierEnvelope` function, following `BuildPlannerEnvelope`'s exact shape | A second templating system, second review surface, second source of "prompt truth" |

The only argument for in-image Python is "the verifier owns its own reasoning strategy end-to-end" — but that argument is about the LangGraph *graph* (retry loops, self-check nodes), not the *initial prompt text*, which is exactly what `EnvelopeIn.Prompt` already carries into every dispatch today. **Verdict: orchestrator-side wins on every axis that matters for this codebase's existing conventions.** This is also what the milestone doc itself is "leaning" toward (`vnext-specialist-verify-MILESTONE.md:50`) — this research confirms rather than overturns that lean.

### Q4 — What the verifier envelope must carry

Reuse `EnvelopeIn` (`pkg/dispatch/envelope.go:45`) as-is for the generic fields already present (`Role="verifier"`, `Level`, `Prompt`, `Provider`, `Caps`, `ProxyEndpoint`, `SignedToken`, `Branch` — verifier needs `Branch` for git-read access to the run branch worktree, same field executor dispatches already populate). New verify-specific data belongs in a NEW pointer field, following the exact precedent of `Dispatch *DispatchMeta` (`envelope.go:146`, "carries orchestrator-side dispatch metadata ... distinct from Provider.Params") and `Dev *Dev` (`envelope.go:152`) — pointer + `omitempty` so non-verifier dispatches never serialize a null placeholder:

```go
// VerifyContext carries verify-stage-specific data the verifier subagent
// needs. Populated only when Role=="verifier"; omitted from JSON otherwise.
type VerifyContext struct {
    Stage               string   `json:"stage"`               // "plan-check" | "level-verify" | "integration-check"
    Objective           string   `json:"objective"`            // the level's declared objective/outcome text
    DeclaredDeliverables []string `json:"declaredDeliverables"` // e.g. PLAN.md-declared file list, or a level's FilesTouched union
    GateCommand         string   `json:"gateCommand,omitempty"` // the declared pass-criteria command (e.g. "make test")
    DoNotTouch          []string `json:"doNotTouch,omitempty"`  // constraint paths the level declared off-limits
    ArtifactRefs        []string `json:"artifactRefs"`          // .tide/planning/<kind>/<name>/ paths on the run branch to read
}
```

- `Objective`/`DeclaredDeliverables`/`GateCommand`/`DoNotTouch` are exactly the fields the milestone doc's "Orchestrator surface" section already names verbatim ("level objective, declared deliverables, the gate command, constraints, and pointers to level artifacts" — `vnext-specialist-verify-MILESTONE.md:43`).
- `ArtifactRefs` lets the verifier's git-read step (Pillar 2: "git read access to the run branch / worktree") locate exactly the `.tide/planning/<kind>/<name>/` subtree relevant to its own level, the same prefix the dashboard's artifact API already computes (`cmd/dashboard/api/artifacts.go:184`, `fmt.Sprintf(".tide/planning/%s/%s/", kind, name)`) — reuse that prefix convention rather than inventing a new one.
- `GateCommand` needs a schema source: today no CRD field carries a declarative "pass criterion command" per level. This is a genuine NEW requirement surface (likely a `Plan.Spec`/`Project.Spec` addition, e.g. an optional `gateCommand` string the planner's own authored artifact declares, or a convention like reading a `Makefile`/`package.json` "test" target) — flag this as an open requirement for the roadmap phase, not solved by this research.
- `Caps` (existing) bounds the verifier's own wall-clock/iteration/token budget — reuse verbatim, no new struct needed.
- The verifier's structured OUTPUT (the `gate_decision` + findings) is NOT carried on `EnvelopeIn` — it's what the verifier WRITES to `EnvelopeOut` (Q5).

### Q5 — Findings persistence (size×locality rule: no blobs in etcd)

**Recommendation: small in-CRD condition + summary counts in `.status`, full findings artifact on the run branch under `.tide/planning/<kind>/<name>/`, reusing the v1.0.7 git-as-artifact-store mechanism — NOT a new PVC path, NOT the termination message beyond a tiny summary.**

Reasoning, by locality tier (matching the project's own decided size×locality rule from `project_envelopes_as_artifacts.md`):

1. **`TerminationStub` (4 KB hard cap, `envelope.go:394`)** — add nothing beyond what's proportional: a `GateDecision string` (`"APPROVED"`/`"REJECTED"`/`"BLOCKED"`) and maybe a `FindingsCount int` / `HighSeverityCount int`, mirroring how `TerminationStub` already carries `ExitCode`/`Reason`/`ChildCount` as tiny cross-namespace summaries while the VERBOSE data (`Result`, full `ChildCRDs`) stays on the namespace-local PVC (`envelope.go:379-393` doc comment is explicit about this split — "ChildCRDs payloads and the verbose Result are intentionally excluded"). Findings text follows the SAME split.
2. **`EnvelopeOut` on the per-Project PVC (`out.json`)** — the FULL findings list (severity + confidence + description + file refs per finding, per the coverage-not-conservatism prompting requirement) lives here, exactly like `Result`/`Artifacts` do today. This is read in-namespace by the reporter Job (the same actor that already reads planner `out.json` to materialize children — `internal/reporter`), never cross-namespace by the Manager (mirrors the existing PVC cross-namespace restriction that drove the reporter's existence in the first place, per `project_envelopes_as_artifacts.md`).
3. **`.status` condition on the level CRD** — a `VerifyResult` status struct (`Decision`, `FindingsCount`, `HighSeverityCount`, `Stage`, `CompletedAt`) — small, bounded, queryable by the dashboard/CLI without reading git. This is the operator-visible signal, same tier as `Plan.Status.ValidationState` today.
4. **Run-branch git artifact** — the reporter Job stages the full findings JSON into `.tide/planning/<kind>/<name>/verify-<stage>.json` (or a single `verify.json` if a level only ever gets one stage), using the EXACT SAME staging path v1.0.7 built: `collectStageEnvelopes` (`artifact_push.go:84`) already walks Milestone/Phase/Plan and stages `plannerMaterialized` levels' artifacts into this tree at boundary-push time; extend it (not replace it) to also stage a verify-result entry when `VerifyState`/`PlanCheckState` has landed. This gets findings onto the dashboard for free via the EXISTING `cmd/dashboard/gitfetch.go` + `cmd/dashboard/api/artifacts.go` surface — no new dashboard endpoint, no new size-capped ConfigMap display cache (explicitly rejected in Key Decisions: "User rejected truncated artifact display").

**Why not a dedicated PVC path or a bigger termination message:** the project's own locality rule (`project_envelopes_as_artifacts.md`) is explicit — PVC is for "large same-namespace," API-created CRs are for "small cross-namespace," termination message is for "tiny," and object-store-behind-interface is reserved for "future large cross-namespace." Findings are namespace-local (the verifier runs in the project namespace, same as every other dispatch) and are exactly the shape v1.0.7 already solved for planning artifacts (small-to-medium, human-reviewable, needs full-fidelity display, benefits from git history/diffing across re-plan attempts). Reusing the existing mechanism is a straight win; inventing a parallel one duplicates the size-cap problem the project already rejected once.

### Q6 — Per-level stage-dispatch config surface (chart-first)

`charts/tide/values.yaml` is a FIXED contract (binary catches up to chart, never reverse — CLAUDE.md, repeated in this file's own "Anti-patterns" and Working Rules). The existing `subagent.levels` block (`charts/tide/values.yaml:252-264`) already has the exact per-level shape to extend:

```yaml
subagent:
  defaults:
    image: ghcr.io/jsquirrelz/tide-claude-subagent
    model: claude-sonnet-4-6
  levels:
    milestone:
      model: claude-opus-4-8
    phase:
      model: claude-sonnet-4-6
    plan:
      model: claude-sonnet-4-6
    task:
      model: claude-haiku-4-5

  # NEW — v1.0.9 Slack Tide
  verify:
    image: ghcr.io/jsquirrelz/tide-langgraph-verifier   # separate image family from
                                                          # the Claude CLI subagent image
    model: claude-haiku-4-5      # cheap-tier default per Open Question #4 in the seed doc
    stages:
      planCheck:
        enabled: false           # per-stage on/off — default OFF (see posture options below)
        maxAttempts: 2
      levelVerify:
        enabled: false
        levels: []               # e.g. ["milestone", "project"] — which LEVELS run this stage
      integrationCheck:
        enabled: false
        levels: ["milestone", "project"]
```

**Default posture options (the milestone doc's open question #6), with a recommendation:**

| Posture | Shape | Cost profile | Recommendation |
|---|---|---|---|
| **off** | All three stages `enabled: false` | Zero verify spend; today's behavior unchanged | Safe chart default for existing installs upgrading in-place — a verify tier that silently starts spending money on `helm upgrade` violates the "gate policy stays in config" principle as much as it violates least-surprise |
| **milestone+project only** | `levelVerify.levels: ["milestone"]`, `integrationCheck.levels: ["milestone","project"]`, `planCheck.enabled: false` | Bounded, small number of dispatches per run (one per Milestone + one at Project close) — cheapest non-zero posture that still catches the exact bug class that motivated this milestone (the wave-integration-miss / declared-deliverable-missing class) | **Recommended default for NEW installs** (chart's own default, distinct from upgrade-safe `off` — Helm can express this via `--set` at install time or a values-file example in INSTALL.md, same pattern the chart already uses for `ServiceMonitor` opt-in) |
| **all levels** | Every stage enabled at every applicable level, including per-Plan `planCheck` | Highest cost — a verify dispatch per Plan AND per Task-bearing boundary | Document as the "maximum assurance" posture for high-stakes runs; not the default |

Config-shape notes:
- `verify.stages.levelVerify.levels` and `.integrationCheck.levels` are EXPLICIT lists, not booleans-per-level, because `levelVerify` at Milestone and `integrationCheck` at Milestone may both want to be independently toggleable (see Q7) — a single list per stage keeps the two orthogonal without needing `levels.milestone.verify.levelVerify` / `levels.milestone.verify.integrationCheck` nesting.
- Mirrors the existing `Project.Spec.Subagent.Levels` → chart-default precedence chain (`resolveImage`, `dispatch_helpers.go:418`) — a per-Project override block (`Project.Spec.Verify` analog to `Project.Spec.Gates`) should resolve the same way: Project override → chart default → off.
- Gate policy for verify failures stays out of the controller exactly like today's `Gates` policy (`internal/gates/policy.go`) — `EvaluatePolicy`-style resolution, not hard-coded thresholds.

### Q7 — Integration-check: distinct template or level-verify parameterized?

**Recommendation: level-verify parameterized by level, NOT a distinct template.** Evidence from the codebase's own structure:

- `gates.BoundaryDetected` (`internal/gates/boundary.go:66`) is ALREADY the single function serving all 4 levels' "children done" check — Milestone/Phase/Plan/Task all funnel through the identical code path with `childKind` as the only parameter. The verify tier should mirror this: one verifier logic path, parameterized.
- The distinction the milestone doc draws — "level-verify... checks one level's own deliverables" vs. "integration-check... do sibling [phases'/milestones'] outputs actually compose, does the full run branch build/test as a whole" — is a difference in SCOPE of what's being checked (one level's artifact vs. cross-sibling composition + whole-branch build), not a difference in WHERE it hooks or WHEN it fires. Both fire at the exact same edge: `BoundaryDetected==true`, before the terminal stamp.
- Concretely: Milestone's own level-verify (checking the Milestone's own declared deliverables — e.g. did `MILESTONE.md` get authored correctly) and Milestone's integration-check (do the Phases underneath it actually compose) are two DIFFERENT verify dispatches that could both fire at the SAME `BoundaryDetected(ms, "Phase")` edge — but they are two STAGE VALUES (`"level-verify"` vs `"integration-check"` in the new `VerifyContext.Stage` field, Q4) of the SAME template + SAME dispatch machinery, differing only in `Objective`/`GateCommand`/`ArtifactRefs` scope (single-level artifacts vs. the cumulative run-branch tree).
- This also directly resolves the template-count question from Q3: **recommend 3 template files** — `plan-check_verifier.tmpl`, `level-verify_verifier.tmpl`, `integration-check_verifier.tmpl` — keyed by STAGE, not by CRD level. `level-verify_verifier.tmpl` serves Plan, Phase, AND Milestone (with `{{.VerifyContext.Objective}}` etc. varying per dispatch); `integration-check_verifier.tmpl` serves Milestone AND Project. This requires a small, additive change to `LoadPromptTemplate`'s naming convention (`prompt_templates.go:66`, `fmt.Sprintf("templates/%s_%s.tmpl", level, role)`) — either a new loader variant keyed by `(role, stage)` instead of `(role, level)`, or reusing the existing `level` parameter slot to carry the stage name for `role="verifier"` dispatches specifically (document the overload clearly if chosen, since it's a deliberate one-off deviation from the level vocabulary CLAUDE.md protects elsewhere).

## Recommended Project Structure (new/changed files)

```
api/v1alpha3/
├── shared_types.go              # + ConditionVerifyHalt, ReasonVerifyBlocked,
│                                 #   AnnotationVerifyResumedAt (mirrors :270/:324)
├── plan_types.go                # + PlanCheckState, PlanCheckAttempts, VerifyState, VerifyResult
├── phase_types.go                # + VerifyState, VerifyResult
├── milestone_types.go            # + VerifyState, VerifyResult (level-verify AND integration-check)
├── project_types.go              # + MaxPlanCheckAttempts; + VerifyState/VerifyResult (integration-check);
│                                 #   + Spec.Verify override block (mirrors Spec.Subagent.Levels precedence)

pkg/dispatch/
├── envelope.go                   # + VerifyContext struct (pointer + omitempty, mirrors DispatchMeta/Dev);
│                                 #   + GateDecision/FindingsCount on TerminationStub
├── vendor_capabilities.go        # (touch point only if the LangGraph image is a genuinely new
│                                 #   Vendor sentinel rather than reusing "anthropic" via langchain-anthropic)

internal/subagent/common/templates/
├── plan-check_verifier.tmpl      # NEW
├── level-verify_verifier.tmpl    # NEW — serves plan/phase/milestone
├── integration-check_verifier.tmpl # NEW — serves milestone/project

internal/controller/
├── verify_halt.go                # NEW — mirrors failure_halt.go exactly (checkVerifyHalt,
│                                 #   setVerifyHaltIfNeeded with the CR-02-style time-fence)
├── dispatch_helpers.go           # + BuildVerifierEnvelope (mirrors BuildPlannerEnvelope:369);
│                                 #   checkDispatchHolds gains checkVerifyHalt
├── plan_controller.go            # + PlanCheckState gate at reconcileWaveMaterialization:1304;
│                                 #   + re-plan-on-REJECT path
├── phase_controller.go           # + VerifyState gate before patchPhaseSucceeded:879
├── milestone_controller.go       # + VerifyState gate before patchMilestoneSucceeded:945
│                                 #   (both level-verify AND integration-check stages)
├── project_controller.go         # + VerifyState gate before checkProjectComplete's Complete stamp:1451
├── task_controller.go            # gateChecks + checkVerifyHalt (task tier gated too, per Q2)
├── artifact_push.go              # collectStageEnvelopes extended to stage verify findings

cmd/tide/resume.go                # --retry-failed (or new flag) resets Blocked verify levels
                                   # FIRST, clears ConditionVerifyHalt LAST, stamps
                                   # AnnotationVerifyResumedAt (Phase 25 CR-01 ordering, verbatim)

charts/tide/values.yaml           # + subagent.verify block (image/model/stages/posture)

# NEW image (out-of-tree from the Go module, separate build/release pipeline):
cmd/tide-langgraph-verifier/ (or a sibling repo/dir) — Python/LangGraph, read-only,
  implements pkg/dispatch.Subagent's CONTRACT (not the Go interface directly — it's a
  separate container image satisfying the same envelope contract, like cmd/stub-subagent
  does today for the dev/test path)
```

### Structure Rationale

- **`verify_halt.go` as its own file**, not folded into `failure_halt.go` — matches the existing one-file-per-halt-class convention (`billing_halt.go`, `failure_halt.go`, `budget_blocked.go` are each separate files despite sharing the `checkDispatchHolds`/`gateChecks` call sites).
- **Stage-keyed templates, not level-keyed** — see Q7 rationale; avoids a combinatorial explosion (4 levels × up to 3 stages = up to 12 files) when 3 stage-scoped files with a `Level`/`Objective` field cover the same ground.
- **`VerifyContext` as a pointer field on the existing `EnvelopeIn`**, not a new envelope kind — preserves the "one envelope contract, `Role` discriminates behavior" pattern already established for `"planner"` vs `"executor"`; a fourth `Role` value (`"verifier"`) is a smaller diff than a parallel envelope type.

## Architectural Patterns

### Pattern 1: Gate off a durable status field, never an in-memory boolean

**What:** Every dispatch-gating decision in this codebase (billing halt, failure halt, budget blocked, parent approval, `ValidationState`, `BoundaryDetected`) reads a field persisted in `.status` (or derives one live from owned children), never a controller-process-local variable. A controller restart must reproduce the exact same gate decision from CRD state alone.
**When to use:** Every new hold/gate the verify tier introduces (`PlanCheckState`, `VerifyState`, `ConditionVerifyHalt`).
**Trade-offs:** Slightly more status-patch traffic (each stage transition is a `Status().Patch` call) versus an in-memory cache; but this is the resumability contract the whole project is built on (CLAUDE.md: "Resumption state is minimal: indegree map + completed-task set... re-derive, don't cache").

**Example (mirrors `plan_controller.go:1304`):**
```go
if plan.Status.ValidationState != "Validated" && plan.Status.ValidationState != "FileTouchMismatch" {
    return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
}
// NEW: additive AND-clause, same shape
if verifyEnabled(project, "planCheck") && plan.Status.PlanCheckState != "Approved" {
    return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
}
```

### Pattern 2: Halt classes are additive project-wide conditions, checked at every dispatch gate, cleared by one CLI verb with a time-fence

**What:** `BillingHalt`/`FailureHalt`/(new)`VerifyHalt` are independent boolean conditions on `Project.Status.Conditions`. They are NOT mutually exclusive (see `budget_blocked.go:25` comment: "BudgetBlocked and BillingHalt are NOT mutually exclusive — both may be true simultaneously"). Each has its own resume annotation and its own resume-time-fence check.
**When to use:** Any new project-wide "stop all new spend" signal.
**Trade-offs:** Adds one more condition + one more gate-chain call site per halt class, but keeps each failure mode independently diagnosable (`kubectl get project -o yaml` shows exactly which halt(s) are active) instead of collapsing them into one ambiguous "Halted" boolean.

### Pattern 3: Reporter Job (in-namespace) does the PVC read; Manager never crosses namespaces

**What:** The Manager cannot mount project-namespace PVCs (cross-namespace mount is not how K8s PVCs work); a small in-namespace "reporter" Job reads `out.json` off the PVC and either materializes child CRDs (planner case) or emits spans (Phase 44's `events.jsonl` case). This is WHY `TerminationStub` (cross-namespace-safe, ≤4KB, embedded in Pod termination message) exists as a SEPARATE channel from the full `out.json`.
**When to use:** The verifier's full findings list is exactly this shape — read in-namespace by the reporter, staged to git by the SAME reporter/push-Job pipeline, summarized cross-namespace via `TerminationStub`.
**Trade-offs:** None new — this is pure reuse of an existing, proven pattern (Phase 09-05/09-06 reporter architecture, Phase 44 tracesynth architecture).

## Anti-Patterns to Avoid

### Anti-Pattern 1: Making `ValidationState` a 4-state enum instead of adding an orthogonal field

**What people might do:** Extend `Plan.Status.ValidationState` with a 4th value like `"PlanCheckRejected"` alongside `Validated`/`CycleDetected`/`FileTouchMismatch`.
**Why it's wrong:** `ValidationState` today encodes ONE concern (DAG/admission-webhook validity, decided once, synchronously, at plan-authoring time). Plan-check is a SEPARATE concern (semantic goal-backward review, decided asynchronously by an LLM dispatch, potentially retried N times). Conflating them means a webhook-level `CycleDetected` and an LLM-level plan-check REJECT become indistinguishable states with different recovery paths sharing one field — a collapsed-levels smell CLAUDE.md explicitly warns against ("Don't collapse levels or invent new ones").
**Instead:** A new orthogonal field (`PlanCheckState`), ANDed into the same gate.

### Anti-Pattern 2: Recomputing `BoundaryDetected` inside the verify dispatch itself as the "did I already dispatch" check

**What people might do:** Use `gates.BoundaryDetected(parent, childKind)==true` as BOTH the trigger to dispatch level-verify AND the idempotency check on every subsequent reconcile (i.e., re-check the live child list every time instead of latching a status marker).
**Why it's wrong:** `BoundaryDetected` performs a live `client.List` + owner-ref filter every call (`boundary.go:66` — "no writes; calling it twice on the same state returns the same value") — it's pure-over-current-state, not a "have we handled this" latch. Using it as a re-dispatch guard risks re-creating the verifier Job on every reconcile once children are Succeeded but before the verify status field lands (a `client.Create` race window), exactly the class of bug the `PlanReporterSpawnedUID`/`PlanSpanEmittedUID` durable-marker pattern (`plan_controller.go:663-664`, `:578`) was built to prevent.
**Instead:** Latch a durable per-attempt marker (Job UID or deterministic name) on the level's own status the moment the verifier Job is created, following the exact `*SpawnedUID`/`*EmittedUID` idiom already used twice in this same file.

### Anti-Pattern 3: Giving the LangGraph verifier image git-write or file-edit tools "just in case"

**What people might do:** Since the image already has `bash` (to run the gate command) and git-read, it would be tempting to also grant `git commit`/`git push` "for convenience" (e.g., so it can stash a scratch note).
**Why it's wrong:** Directly violates the locked Pillar 2 constraint ("the image never commits, never pushes, never authors run-branch artifacts") and reopens the exact trust boundary the read-only design exists to close — a read-only verifier is what makes its `gate_decision` trustworthy as an independent check rather than a self-graded one.
**Instead:** Ephemeral pod-local scratch space only (already explicitly allowed: "incidental writes... in its ephemeral workspace are fine"); all durable output goes through `EnvelopeOut`/`TerminationStub`, never a git operation performed by the verifier image itself.

## Scalability Considerations

| Concern | Off (default) | Milestone+Project only | All levels |
|---------|--------------|--------------|-------------|
| Verify dispatches per run | 0 | O(milestones) + 1 | O(plans + phases + milestones) — potentially dominates task-count |
| New halt-class blast radius | None | Bounded — only fires at few, high-value boundaries | Higher chance of a false-positive BLOCKED freezing a run mid-flight |
| Re-plan loop spend | 0 | 0 (plan-check off in this posture) | Bounded by `MaxPlanCheckAttempts`, but multiplies planner-tier (expensive) dispatches |

### Scaling Priorities

1. **First bottleneck: verify-dispatch cost at "all levels" posture on large plans.** A Plan with many Tasks still gets exactly ONE plan-check dispatch (Plan-scoped, not per-Task) — the cost scales with plan/phase/milestone COUNT, not task count, so this stays bounded even on wide task DAGs. Mitigate via the cheap-model-tier default (`subagent.verify.model: claude-haiku-4-5`).
2. **Second bottleneck: re-plan loop amplifying planner-tier spend.** Bound via `MaxPlanCheckAttempts` (own counter, not shared with `MaxAttemptsPerTask` — Q2) with a low default (1–2) and escalation to `ConditionVerifyHalt` rather than unbounded retry.

## Integration Points

### Internal Boundaries

| Boundary | Communication | Notes |
|----------|---------------|-------|
| Reconciler ↔ verifier subagent | `pkg/dispatch.Subagent.Run(ctx, EnvelopeIn) (EnvelopeOut, error)` via PodJob (`internal/dispatch/podjob/backend.go`) | UNCHANGED interface — verifier is just a new `Role`/image, no interface change |
| Reconciler ↔ credproxy | Signed HMAC token minted at Job-create time, validated by the sidecar | UNCHANGED — verifier dispatch mints/validates a token exactly like planner/executor |
| Manager ↔ reporter Job (in-namespace) | Reporter reads `out.json` off the per-Project PVC, materializes status + stages git artifacts | Verifier's full findings ride this SAME channel — no new cross-namespace read path |
| Manager ↔ run branch (git) | `cmd/tide-push` stages `.tide/planning/<kind>/<name>/` at boundary push | Verifier findings extend this tree; dashboard gitfetch surfaces them for free |
| `tide resume` CLI ↔ Project status | Status-subresource patch + metadata-annotation patch, two-phase ordering (reset-then-clear) | `ConditionVerifyHalt` recovery MUST follow the exact Phase 25 CR-01 sequence (`resume.go:154-165`) — reset Blocked levels first, clear the halt condition last, stamp the resume-fence annotation |

## Sources

- Direct repository inspection (grep + Read), 2026-07-18, `main` branch of `/Users/justinsearles/Projects/tide`. Every file:line citation above was confirmed live against the current tree, not inferred from documentation.
- `.planning/PROJECT.md` — Key Decisions table (halt-condition pattern, git-as-artifact-store, resume-verb rows), Current Milestone section.
- `.planning/milestones/vnext-specialist-verify-MILESTONE.md` — locked architecture pillars, orchestrator surface, open questions (this research directly answers all 7).
- `.planning/notes/langgraph-successor-runtime-strategy.md` — static-derived-DAG constraint, runtime-neutrality precedent from Phase 45.
- `.planning/seeds/verify-level-subagent.md` — original stage inventory and sequencing rationale.
- `CLAUDE.md` (repo) — failure semantics, values.yaml FIXED-contract rule, coverage-not-conservatism prompting note, level-collapse anti-pattern.

---
*Architecture research for: TIDE v1.0.9 "Slack Tide" — in-cluster verify tier integration*
*Researched: 2026-07-18*
