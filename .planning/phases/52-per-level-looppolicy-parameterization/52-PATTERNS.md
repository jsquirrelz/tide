# Phase 52: Per-Level LoopPolicy Parameterization - Pattern Map

**Mapped:** 2026-07-20
**Files analyzed:** 24 (5 schema, 8 controller, 2 podjob, 1 pkg/git, 4 templates, 1 CLI, 3+ test)
**Analogs found:** 24 / 24 (every file generalizes an existing Phase-51 analog; zero genuinely-new patterns except the worktree-provisioning init container, called out below)

This phase is a **pure generalization** phase: every new/modified file has a
direct, already-read analog from Phase 51's Task loop (or the pre-existing
`Gates`/`ResolveProvider` per-level precedent). There is no "invent a new
pattern" file in this phase except the level-verify worktree-checkout init
container (flagged in `## No Analog Found`), which RESEARCH.md itself marks
as new build work.

## File Classification

| New/Modified File | Role | Data Flow | Closest Analog | Match Quality |
|---|---|---|---|---|
| `api/v1alpha3/loop_types.go` (+`Level` field) | model (CRD schema) | n/a (schema) | same file, `EscalationPolicy`/`AutonomyLevel` enum shape | exact (self-extend) |
| `api/v1alpha3/plan_types.go` (+`Verification`, +`LoopStatus`) | model (CRD schema) | n/a (schema) | `api/v1alpha3/task_types.go` `TaskSpec.Verification` / `TaskStatus.LoopStatus` | exact |
| `api/v1alpha3/project_types.go` (+per-level `VerificationSpec` map) | model (CRD schema) | n/a (schema) | `Gates` struct (same file, `project_types.go:39-51`) | exact |
| `api/v1alpha3/shared_types.go` (Verifying/VerifyHalted doc-scope) | model (constants) | n/a (schema) | same file — `LevelPhaseVerifying`/`LevelPhaseVerifyHalted` doc comments | exact (self-extend) |
| `internal/controller/dispatch_helpers.go` (+`ResolveLoopPolicy`) | service (shared resolver) | transform | `ResolveProvider` (same file, `dispatch_helpers.go:283-330`) | exact |
| `internal/controller/plan_controller.go` (+Verifying sub-state, plan-check dispatch/consume, re-plan) | controller | event-driven | `internal/controller/task_controller.go` (`checkVerifyingState`/`dispatchVerifier`/`handleVerifierCompletion`/`repairOrHalt`) | exact (state-machine shape), role-match (Plan vs Task) |
| `internal/controller/phase_controller.go` (+level-verify dispatch pre-`patchPhaseSucceeded`) | controller | event-driven | `phase_controller.go:773-849` own boundary-push-before-Succeeded seam + Task verify dispatch shape | exact (insertion point), role-match (dispatch mechanics) |
| `internal/controller/milestone_controller.go` (+level-verify dispatch pre-`patchMilestoneSucceeded`) | controller | event-driven | same as Phase (mirrors `milestone_controller.go:855-914`) | exact / role-match |
| `internal/controller/project_controller.go` (+level-verify dispatch in `checkProjectComplete`) | controller | event-driven | same as Phase/Milestone | exact / role-match |
| `internal/controller/task_controller.go` (migrate `repairOrHalt`/`prepareDispatch` onto resolver; `onExhaustion` split) | controller | event-driven | same file — `repairOrHalt`/`haltVerify` (own precedent, self-extend) | exact |
| `internal/controller/level_status.go` (+`onExhaustion` requireApproval/escalate branch) | utility (shared status-patch primitive) | CRUD (patch) | same file — `patchLevelStatus`/`consumeApproveAndResume` | exact (self-extend) |
| `internal/controller/verify_halt.go` (unchanged — escalate arm reused) | utility | CRUD (patch) | itself — no change needed | exact |
| `internal/dispatch/podjob/jobspec.go` (`JobKindVerifier` case: `opts.Task`→`opts.ParentObj`) | service (Job-spec builder) | transform | `case JobKindPlanner:` in same file (`jobspec.go:271-284`) | exact |
| `internal/dispatch/podjob/names.go` (`VerifierJobName` signature) | utility (deterministic naming) | transform | `PlannerJobName` in same file (`names.go:52-54`) | exact |
| `pkg/git/worktree.go` (+read-only worktree helper) | utility (file I/O / git) | file-I/O | `AddWorktree` in same file (`worktree.go:59-79`) | role-match (write→read-only variant) |
| `internal/subagent/common/templates/plan_verifier.tmpl` | config (prompt template) | n/a | `internal/subagent/common/templates/task_verifier.tmpl` | exact |
| `internal/subagent/common/templates/phase_verifier.tmpl` | config (prompt template) | n/a | `task_verifier.tmpl` | exact |
| `internal/subagent/common/templates/milestone_verifier.tmpl` | config (prompt template) | n/a | `task_verifier.tmpl` | exact |
| `internal/subagent/common/templates/project_verifier.tmpl` | config (prompt template) | n/a | `task_verifier.tmpl` | exact |
| `cmd/tide/approve.go` (+`findAwaitingProject`) | CLI command | request-response | `findAwaitingMilestone`/`findAwaitingPhase`/`findAwaitingPlan`/`findAwaitingTask` (same file, `approve.go:362-435`) | exact |
| `internal/controller/dispatch_helpers_test.go` (+`TestResolveLoopPolicy*`) | test | unit | `TestResolveProviderPerLevelWins` etc. (same file, `dispatch_helpers_test.go:38-95`) | exact |
| `internal/controller/plan_verify_loop_test.go` (NEW) | test | unit + envtest (Ginkgo) | `internal/controller/task_verify_loop_test.go` | exact |
| `internal/controller/plan_verify_dispatch_test.go` / `level_verify_dispatch_test.go` (NEW) | test | envtest (Ginkgo) | `internal/controller/task_verify_dispatch_test.go` | exact |
| (regression extension) `internal/controller/verify_halt_test.go` | test | unit | itself — extend with new-level coverage | exact |

## Pattern Assignments

### `api/v1alpha3/loop_types.go` (model, +`Level` field on `LoopPolicy`)

**Analog:** same file — `AutonomyLevel`/`EscalationPolicy` enum-with-kubebuilder-validation shape (`loop_types.go:166-195`).

**Pattern to copy** (add a `LoopLevel` enum type + a `Level` field on `LoopPolicy`, same doc-comment density and `+kubebuilder:validation:Enum` idiom already used twice in this file):
```go
// EscalationPolicy declares the bounded exit path a loop takes once its
// repeat policy (MaxIterations/MaxDuration/BudgetCents) is exhausted without
// an APPROVED evaluation.
// +kubebuilder:validation:Enum=escalate;requireApproval
type EscalationPolicy string

const (
	EscalationEscalate         EscalationPolicy = "escalate"
	EscalationRequireApproval  EscalationPolicy = "requireApproval"
)
```
`LoopLevel` mirrors this exact shape: `+kubebuilder:validation:Enum=task;plan;phase;milestone;project`, with the five constants named `LoopLevelTask`/`LoopLevelPlan`/etc. Add `Level LoopLevel` as an `+optional` field on `LoopPolicy` (`loop_types.go:37-73`), documented as "stamped by the resolver, never authored directly" per D-02.

**LoopStatus / EvaluationSummary — read, not modified.** `EvaluationSummary.FindingsCount`/`HighSeverityCount` (`loop_types.go:141-164`) are D-05's stall-detection inputs — no shape change needed, just consumed by the new `repairOrHaltPlan` stall check.

---

### `api/v1alpha3/plan_types.go` (model, +`Verification`, +`LoopStatus`)

**Analog:** `api/v1alpha3/task_types.go` `TaskSpec.Verification` (`task_types.go:213-219`) and `TaskStatus.LoopStatus` (`task_types.go:324-330`).

**Spec field pattern** (copy verbatim onto `PlanSpec`, same doc-comment shape citing the precedent it mirrors):
```go
// Verification declares the planner-authored, immutable-once-locked
// pass-criteria contract for this Task (Phase 51 TASK-01). Mirrors the
// Gates precedence-doc pattern: Task-scoped only in this phase; the
// identical VerificationSpec shape generalizes to Plan.Spec/
// Project.Spec with a Task > Plan > Project precedence in Phase 52.
// +optional
Verification VerificationSpec `json:"verification,omitempty"`
```
On `PlanSpec` (`plan_types.go:24-48`) this becomes `Verification VerificationSpec` — the SAME `VerificationSpec` type from `task_types.go:91`, not a new type (D-01 is explicit: zero shape changes, re-embed only). The type's `+kubebuilder:validation:XValidation` immutable-once-Locked CEL rule (`task_types.go:90`) travels automatically since it is attached to the type, not the embedding site.

**Status field pattern** (copy onto `PlanStatus`, same doc comment citing `task_types.go:330` as precedent):
```go
// LoopStatus carries the Task loop's current-iteration summary + exit
// reason only (Phase 51 D-07/LOOP-03 — no accumulating iteration
// history; see TestLoopStatus_NoForbiddenFields). Re-derivable across a
// restart from Attempt above + the completed-set, never a cache of
// iteration history.
// +optional
LoopStatus LoopStatus `json:"loopStatus,omitempty"`
```
Add to `PlanStatus` (`plan_types.go:85-156`) as its own field, DISTINCT from `WaveIntegrationStatus.Attempts` (`plan_types.go:56-79`, already on `PlanStatus.WaveIntegration`) — D-06 requires these stay two separate counters (mirrors Phase 51 D-05's infra-retry ≠ quality-iteration split, applied one level up). Do not fold plan-check `LoopStatus.Iteration` into `WaveIntegration.Attempts`.

**Compile-time guard to extend:** `TestLoopStatus_NoForbiddenFields` in `api/v1alpha3/loop_types_test.go` — add `PlanStatus` to whatever reflection/field-list this test walks so the new embedding is covered by the same no-history enforcement Task already has.

---

### `api/v1alpha3/project_types.go` (model, +per-level `VerificationSpec` defaults map)

**Analog:** `Gates` struct, same file (`project_types.go:39-51`):
```go
// Gates declares per-level gate policy. Phase 1 ships field; Phase 4 wires.
type Gates struct {
	// +optional
	Milestone GatePolicy `json:"milestone,omitempty"`
	// +optional
	Phase GatePolicy `json:"phase,omitempty"`
	// +optional
	Plan GatePolicy `json:"plan,omitempty"`
	// +optional
	Task GatePolicy `json:"task,omitempty"`
	// +optional
	PauseBetweenWaves bool `json:"pauseBetweenWaves,omitempty"`
}
```
Mirror this EXACTLY for the new per-level verification-defaults map — a new `VerificationDefaults` struct (Claude's Discretion on the exact name) with `Milestone`/`Phase`/`Plan`/`Project` fields of type `VerificationSpec` (Task itself resolves task-scoped only, per D-01 — this map covers phase/milestone/project scope which have NO spec-level field, plus a project-scope `Plan`/`Task` default slot for when the level's own spec is empty). Add a `Verification VerificationDefaults` field on `ProjectSpec` (`project_types.go:365-458`), positioned next to `Gates` (`project_types.go:400-402`) since it is the same "per-level defaults map" shape.

**Pitfall 4 (from RESEARCH.md) applies here directly:** the SAME `VerificationSpec` CEL immutable-once-Locked rule (`task_types.go:90`) attaches to every embedding, including this Project-level defaults map. Document (in the new field's doc comment) that operators/chart authors should author these entries at `Phase: "Draft"` (or empty), not `"Locked"` — a locked default cannot be edited later without the Locked→Superseded dance. This is convention, not schema — do not add a new CEL rule to work around it (D-01 is locked).

---

### `api/v1alpha3/shared_types.go` (model, Verifying/VerifyHalted doc-scope)

**Analog:** same file — the `LevelPhaseVerifying`/`LevelPhaseVerifyHalted` constants (`shared_types.go:491-511`), currently doc-commented "Task-only":
```go
// LevelPhaseVerifying is Task-only (Phase 51 TASK-01/EXEC-04): set when
// the executor Job exits 0 ... an independent verifier Job has been
// dispatched against it.
LevelPhaseVerifying = "Verifying"
// LevelPhaseVerifyHalted is Task-only (Phase 51 ESC-03): the terminal a
// verification loop reaches when it exhausts MaxIterations ...
LevelPhaseVerifyHalted = "VerifyHalted"
```
Per D-03's Claude's-Discretion point, either (a) update these two doc comments to drop "Task-only" and describe the generalized meaning (Plan reuses `LevelPhaseVerifying` for its own Verifying sub-state; Phase/Milestone/Project use it too per D-07), or (b) mint sibling constants. Whichever is chosen, the existing `ConditionVerifyHalt`/`ReasonVerifyExhausted`/`AnnotationVerifyResumedAt` vocabulary (`shared_types.go:337-374`) is REUSED VERBATIM — do not mint a second halt-condition family. The `checkVerifyHalt`/`setVerifyHaltIfNeeded` doc block at `shared_types.go:337-357` already documents "escalate arm reused" — just extend the "what SETS it" prose to name the new Phase/Milestone/Project/Plan call sites, mirroring how it already documents Task's.

---

### `internal/controller/dispatch_helpers.go` (service, +`ResolveLoopPolicy`)

**Analog:** `ResolveProvider` (same file, `dispatch_helpers.go:269-330`) — read in full below because this is the literal shape the resolver must follow:

```go
// ResolveProvider walks Project.Spec.Subagent precedence chain for the given
// dispatch level (D-C2), first mapping it to its Levels.<overrideKey> slot via
// levelOverrideKey (D-02). Returns a ProviderSpec with Vendor pinned to
// "anthropic" in v1.0 ... Model and Params resolve via:
//
//	project.Spec.Subagent.Levels.<overrideKey>.Model ->
//	project.Spec.Subagent.Model ->
//	helmDefaults.Models[<overrideKey>] ->
//	"" (caller surfaces missing-config error)
func ResolveProvider(project *tideprojectv1alpha3.Project, level string, helmDefaults ProviderDefaults) pkgdispatch.ProviderSpec {
	key := levelOverrideKey(level)
	var levelCfg *tideprojectv1alpha3.LevelConfig
	if project != nil {
		switch key {
		case "milestone":
			levelCfg = project.Spec.Subagent.Levels.Milestone
		// ... phase/plan/task
		}
	}
	var model string
	switch {
	case levelCfg != nil && levelCfg.Model != "":
		model = levelCfg.Model
	case project != nil && project.Spec.Subagent.Model != "":
		model = project.Spec.Subagent.Model
	default:
		if helmDefaults.Models != nil {
			model = helmDefaults.Models[key]
		}
	}
	// ... Params merge, level wins on conflict
	return pkgdispatch.ProviderSpec{Vendor: "anthropic", Model: model, Params: params}
}
```

**`ResolveLoopPolicy`'s shape** — RESEARCH.md already worked out the full function body (`52-RESEARCH.md` "Code Examples" section, lines ~772-822); reproduced here as the pattern to implement verbatim, placed in the SAME file next to `ResolveProvider`:

```go
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

**Anti-pattern to grep for (SC3):** no reconciler may `switch obj.(type)` or compare a CRD kind string to pick gate policy — every one of the five reconcilers calls `ResolveLoopPolicy(...)` and branches ONLY on the returned `LoopPolicy.MaxIterations`/`EscalationPolicy`.

**`resolveImage`'s per-level-key switch** (`dispatch_helpers.go:419-433`) is the concrete precedent for `projectLevelVerificationDefault`'s own per-level-key switch on the new `Project.Spec.Verification` map fields — same `switch key { case "milestone": ...; case "phase": ...; }` shape, just returning `*VerificationSpec` instead of `*LevelConfig`.

---

### `internal/controller/plan_controller.go` (controller, plan-check loop)

**Analog:** the ENTIRE Task-loop state machine in `internal/controller/task_controller.go` — this is the dominant analog for this phase; read every excerpt below before writing the Plan-side equivalents.

**1. `checkVerifyingState` → new `checkPlanVerifyingState`.** Exact shape to mirror (`task_controller.go:664-695`):
```go
func (r *TaskReconciler) checkVerifyingState(ctx context.Context, task *tideprojectv1alpha3.Task) (taskGateResult, error) {
	jobName := podjob.VerifierJobName(task.UID, task.Status.Attempt)
	var job batchv1.Job
	if err := r.Get(ctx, client.ObjectKey{Namespace: task.Namespace, Name: jobName}, &job); err != nil {
		if !apierrors.IsNotFound(err) {
			return taskGateResult{}, err
		}
		// NotFound is a legitimate "cap-hit deferred the dispatch" state — retry
		// dispatchVerifier (idempotent via deterministic Job name).
		result, _, dErr := r.dispatchVerifier(ctx, task, project)
		return taskGateResult{shouldHalt: true, result: result}, dErr
	}
	if isJobTerminal(&job) {
		result, hErr := r.handleVerifierCompletion(ctx, task, project, &job)
		return taskGateResult{shouldHalt: true, result: result}, hErr
	}
	return taskGateResult{shouldHalt: true, result: ctrl.Result{}}, nil
}
```
Plan-side: `checkPlanVerifyingState` does the same NotFound→dispatch / terminal→consume / still-running→no-op three-way branch, keyed off `VerifierJobName("plan", plan.UID, attempt)` (post-Pitfall-1-fix signature).

**2. `dispatchVerifier` → new `dispatchPlanVerifier`.** The cap-before-reserve ordering (`task_controller.go:2119-2143`) is load-bearing — copy verbatim:
```go
// ESC-04/D-10: cap-before-acquire (Pitfall 6). Self-excludes
// verifierJobName so a re-reconcile of an already-dispatched verifier
// never counts itself.
inFlight, cErr := verifierInFlightCount(ctx, r.Client, task.Namespace, project.Name, verifierJobName)
if cErr != nil {
	return ctrl.Result{}, false, fmt.Errorf("verifier in-flight count: %w", cErr)
}
if inFlight >= defaultVerifierConcurrencyCap {
	return ctrl.Result{RequeueAfter: 10 * time.Second}, false, nil
}
if r.Deps.ReserveEstimateCents > 0 {
	r.Deps.Reservations.Reserve(string(task.UID), r.Deps.ReserveEstimateCents)
	reserved = true
}
```
D-10 requires the SAME `verifierInFlightCount`/`ReservationStore` calls, unmodified, for the Plan-level dispatch — just keyed by `plan.UID` instead of `task.UID`.

**3. `buildVerifierEnvelopeIn` → new `buildPlanVerifierEnvelopeIn`.** Mirror the ordered-commands-union + orchestrator-side prompt render shape (`task_controller.go:2242-2309`):
```go
var commands []string
if verification.GateCommand != "" {
	commands = append(commands, verification.GateCommand)
}
commands = append(commands, verification.Commands...)

envIn := pkgdispatch.EnvelopeIn{
	Role: "verifier", Level: "plan", // ← the only Level-string change
	Verify: &pkgdispatch.VerifyContext{
		GateCommand: verification.GateCommand, Commands: commands,
		RequiredArtifacts: verification.RequiredArtifacts, EvaluatorRef: verification.Evaluator,
	},
}
tmpl, tErr := common.LoadPromptTemplate("verifier", "plan") // ← LoadPromptTemplate is ALREADY level-agnostic
```
`common.LoadPromptTemplate(role, level)` needs ZERO code changes (`prompt_templates.go:82-89`) — passing `level="plan"` resolves `templates/plan_verifier.tmpl` automatically via the existing `fmt.Sprintf("templates/%s_%s.tmpl", level, role)` convention.

**4. `handleVerifierCompletion` → new `handlePlanVerifierCompletion`.** The three-tier `ClassifyVerdict` switch is copy-exact (`task_controller.go:2894-2906`):
```go
switch pkgdispatch.ClassifyVerdict(raw) {
case pkgdispatch.VerdictApproved:
	if hasDeterministicFailure(out.Verdict) {
		return r.repairOrHalt(ctx, task, project, out) // → repairOrHaltPlan
	}
	return r.markVerifiedSucceeded(ctx, task, project, out) // → clear Verifying, unblock Task dispatch
case pkgdispatch.VerdictRepairable:
	return r.repairOrHalt(ctx, task, project, out) // → repairOrHaltPlan
default: // BLOCKED + ClassifyVerdict's own fail-closed default
	return r.haltVerify(ctx, task, project, out, out.Verdict.Summary, "VerifyBlocked", tideprojectv1alpha3.ExitEscalated)
}
```
Fail-closed pattern to copy verbatim (`task_controller.go:2861-2876`): an unreadable envelope OR a nil `Verdict` ALWAYS halts, never succeeds.

**5. `repairOrHalt` → new `repairOrHaltPlan` (D-05 stall detection is the ONE new piece, not present in Task's version).** Task's shape (`task_controller.go:2638-2649`):
```go
func (r *TaskReconciler) repairOrHalt(ctx context.Context, task *tideprojectv1alpha3.Task, project *tideprojectv1alpha3.Project, out pkgdispatch.EnvelopeOut) (ctrl.Result, error) {
	if task.Status.LastAttemptEvidence != nil && intersectsProtectedRefs(...) {
		return r.escalateSystem(ctx, task, project, out)
	}
	if task.Status.Attempt >= int(task.Spec.Verification.MaxIterations) {
		return r.haltVerify(ctx, task, project, out, "...", "VerifyIterationsExhausted", tideprojectv1alpha3.ExitIterationsExhausted)
	}
	return r.dispatchRepairAttempt(ctx, task, project, out)
}
```
`repairOrHaltPlan` inserts the D-05 stall check BEFORE the `MaxIterations` check (or immediately after — the ordering just needs to guarantee neither burns an iteration on a non-improving re-plan): compare the new verdict's severity-weighted score (from `out.Verdict.Findings`, exactly how `applyLoopStatus` already computes `HighSeverityCount`, `task_controller.go:2449-2454`) against `plan.Status.LoopStatus.LastEvaluation.FindingsCount`/`HighSeverityCount` — the SAME two fields Task's `applyLoopStatus` already persists (`loop_types.go:149-159`). If not strictly decreasing → halt with a dedicated `ExitReason`/message ("re-plan loop stalled"), reusing the SAME halt call (`haltVerify`-equivalent) MaxIterations-exhaustion uses.

**6. `dispatchRepairAttempt` → new `dispatchPlanRepair` (D-04's re-plan, includes the NEW delete-then-recreate step RESEARCH.md's Pitfall 3 requires).** Task's fresh-attempt pattern (`task_controller.go:2736-2829`) — the applyLoopStatus-before-Attempt-reassignment ordering is load-bearing, copy exactly:
```go
// applyLoopStatus BEFORE reassigning Status.Attempt below: LastEvaluation
// must summarize the attempt that was JUST verified (Iteration mirrors
// the OLD Status.Attempt), never the fresh attempt about to dispatch.
applyLoopStatus(task, out, "") // loop continues: ExitReason stays empty
task.Status.Attempt = attempt
task.Status.Phase = tideprojectv1alpha3.LevelPhaseRunning
```
Plan-side: `applyLoopStatus`-equivalent updates `Plan.Status.LoopStatus` BEFORE minting the fresh planner attempt number, then sets `Plan.Status.Phase = LevelPhaseVerifying` (NOT Running — the Plan re-enters Verifying only after the NEW planner Job completes and re-materializes children, mirroring D-03's own trigger). The NEW step this function needs that Task's `dispatchRepairAttempt` does NOT have: **delete the rejected attempt's child Tasks FIRST** — see RESEARCH.md "Child-Task reconciliation on re-plan" for the exact list-then-delete shape, using the SAME `client.MatchingFields{taskPlanRefIndexKey: plan.Name}` list `reconcilePlannerDispatch` already runs (`plan_controller.go:267-273`) as the delete target set. This single delete unblocks `reconcilePlannerDispatch`'s existing `len(taskList.Items) > 0` early return (`plan_controller.go:274-276`) for the fresh planner Job.

**7. `nextAttempt` — Plan needs its own planner-Job attempt counter (currently hardcoded `attempt := 1`, `plan_controller.go:283,388`).** Mirror Task's List-Jobs-by-label-then-max pattern exactly (`task_controller.go:1918-1950`):
```go
func (r *TaskReconciler) nextAttempt(ctx context.Context, task *tideprojectv1alpha3.Task) (int, error) {
	var jobList batchv1.JobList
	if err := r.List(ctx, &jobList, client.InNamespace(task.Namespace),
		client.MatchingLabels{"tideproject.k8s/task-uid": string(task.UID)}); err != nil { ... }
	maxAttempt := 0
	for _, j := range jobList.Items {
		attempt, ok := j.Labels[owner.LabelAttempt]
		if !ok { continue }
		n, err := strconv.Atoi(attempt)
		if err != nil || n < 0 { continue } // WR-03 malformed-label guard
		if n > maxAttempt { maxAttempt = n }
	}
	return maxAttempt + 1, nil
}
```
A Plan-scoped equivalent lists by `tideproject.k8s/plan-uid` (planner Jobs already carry `tideproject.k8s/<level>-uid` per `jobspec.go:279-280`), takes max+1. Per RESEARCH Open Question 3, `PlanStatus.LoopStatus.Iteration` may double as this counter since Plan planner dispatch had NO pre-existing infra-retry semantics to preserve (unlike Task) — evaluate at plan-write time; the WR-03 malformed-label guard above should be copied regardless of which source wins.

**8. `checkParentApproval` extension for the new Verifying hold (D-03).** RESEARCH.md's Code Examples section already gives the exact diff — extend the existing `case "Plan":` arm (`dispatch_helpers.go:595-601`) with one more OR clause:
```go
case "Plan":
	var plan tideprojectv1alpha3.Plan
	if err := c.Get(ctx, client.ObjectKey{Namespace: ns, Name: parentName}, &plan); err != nil {
		return false, client.IgnoreNotFound(err)
	}
	return plan.Status.Phase == tideprojectv1alpha3.LevelPhaseAwaitingApproval ||
		plan.Status.Phase == tideprojectv1alpha3.LevelPhaseVerifying, nil
```
This is the SAME `checkParentApproval` function Task's `gateChecks` already calls at `task_controller.go:449` — no new call site needed, just the extra OR clause. This structurally guarantees D-03's "no child Task dispatches until APPROVED" without a new hold mechanism.

**9. `reconcilePlannerDispatch`'s own AwaitingApproval-early-return shape (`plan_controller.go:236-263`) is the direct precedent for the NEW Verifying early-return** this phase adds to the SAME function — same "check at the VERY TOP, before the tasks-exist List" placement rationale (the comment at `plan_controller.go:237-241` explains exactly why order matters here; the new Verifying check needs the identical placement discipline).

---

### `internal/controller/{phase,milestone,project}_controller.go` (controller, level-verify pre-Succeeded hook)

**Analog:** each reconciler's own existing "boundary push trigger AFTER gate, BEFORE patchSucceeded" seam. Read directly (`phase_controller.go:773-849`):
```go
// Plan 04-06 W-2: boundary push trigger AFTER gate, BEFORE patchSucceeded.
if envReadOK {
	expected := out.ChildCount
	if expected == 0 {
		return r.patchPhaseSucceeded(ctx, ph) // genuine leaf
	}
	observed := r.countChildPlans(ctx, ph)
	if observed < expected {
		return ctrl.Result{RequeueAfter: 5 * time.Second}, nil // still materializing
	}
	detected, derr := gates.BoundaryDetected(ctx, r.Client, ph, "Plan")
	if detected {
		if err := r.maybeTriggerBoundaryPush(ctx, ph, project); err != nil { ... }
		return r.patchPhaseSucceeded(ctx, ph)
	}
	return ctrl.Result{RequeueAfter: 5 * time.Second}, nil // not all children Succeeded yet
}
```
D-07's level-verify dispatch is a SIBLING insertion at the EXACT SAME seam — immediately before the `return r.patchPhaseSucceeded(ctx, ph)` calls (there are 3 call sites in `phase_controller.go`, mirrored in `milestone_controller.go` at its own `patchMilestoneSucceeded` call sites, and inside `project_controller.go`'s `checkProjectComplete`): when `ResolveLoopPolicy(...)` resolves a non-empty contract for that level, dispatch the level-verify Job (mirroring `dispatchVerifier`'s shape from Task, keyed by the level's own UID) INSTEAD of immediately calling `patch{Level}Succeeded` — the Succeeded call only fires after the level-verify verdict comes back APPROVED. A level with NO resolved contract (`ResolveLoopPolicy` returns a policy with empty `GateCommand`/`MaxIterations==0` because nothing was authored) falls through to `patch{Level}Succeeded` exactly as today — this is what makes "absence of config = today's behavior" (Phase 53's off-on-upgrade posture) trivially true.

**`maybeTriggerBoundaryPush`'s existing `errGitWriterBusy` sentinel-error requeue pattern** (`phase_controller.go:807-810`) is the precedent for how the new level-verify dispatch should signal "cap reached, requeue" vs. a genuine error — return `ctrl.Result{RequeueAfter: ...}, nil` for a transient defer, propagate only real `error`s.

---

### `internal/controller/task_controller.go` (controller, resolver migration + `onExhaustion` split)

**Analog:** the function itself, self-extended.

**Migration pattern (D-02's "no behavior change at defaults" bar).** `repairOrHalt`'s current raw-spec read (`task_controller.go:2643`):
```go
if task.Status.Attempt >= int(task.Spec.Verification.MaxIterations) {
```
becomes:
```go
policy := ResolveLoopPolicy(project, nil, task, "task")
if task.Status.Attempt >= int(policy.MaxIterations) {
```
Same migration shape applies to `prepareDispatch`/`hasVerificationContract` wherever `task.Spec.Verification.MaxIterations`/`.OnExhaustion` is read directly today — RESEARCH.md's "State of the Art" table names this explicitly as the ONE deliberate behavior change: `onExhaustion: requireApproval` previously collapsed to the same `haltVerify`/`ConditionVerifyHalt` path as `escalate` (Phase 51 shipped both uniform); post-migration it must route through the NEW `requireApproval` branch (see `level_status.go` below) instead.

**`onExhaustion` differentiation — the new branch point.** Where `haltVerify` currently unconditionally calls `setVerifyHaltIfNeeded` (`task_controller.go:2571-2576`), the migrated call site branches on `policy.EscalationPolicy`:
```go
switch policy.EscalationPolicy {
case tideprojectv1alpha3.EscalationRequireApproval:
	// route through patchTaskAwaitingApproval / consumeApproveAndResume machinery
case tideprojectv1alpha3.EscalationEscalate, "":
	// existing setVerifyHaltIfNeeded path, unchanged
}
```
`patchTaskAwaitingApproval` (`task_controller.go:991-1034`, not yet read in full but named directly by RESEARCH.md's file-touch list) is the EXISTING function this new branch calls into — no new parking primitive.

---

### `internal/controller/level_status.go` (utility, `onExhaustion` shared branch)

**Analog:** `consumeApproveAndResume` (same file, `level_status.go:106-155`) — the EXACT machinery `requireApproval` routes through:
```go
// consumeApproveAndResume performs the approve-annotation consume-and-resume
// two-step ... (a) consume the one-shot approve annotation via the
// gates package + SetAnnotations + a plain MergeFrom annotation Patch FIRST
// ... then (b) a status patch (via patchLevelStatus) setting the level's
// phase field to LevelPhaseRunning and ConditionWaveOrLevelPaused=False/
// ReasonApprovedByUser with the caller's resumeMessage.
func consumeApproveAndResume(ctx context.Context, c client.Client, obj client.Object, conditions *[]metav1.Condition, fieldPtr *string, level string, resumeMessage string) (ctrl.Result, error) {
	newAnno := gates.ConsumeApprove(obj, level)
	// ... two-step patch
}
```
This is the function D-08's `requireApproval` arm calls to PARK the level (the entry side is a NEW `patchLevelAwaitingApproval`-equivalent call at the exhaustion site, mirroring `patchTaskAwaitingApproval`'s existing shape for gate-policy pauses; the EXIT side, when the operator runs `tide approve`, is `consumeApproveAndResume` UNCHANGED — same annotation-consume-then-Running-patch two-step). `patchLevelStatus` (`level_status.go:69-104`) is the shared leaf both the entry and exit paths already funnel through — no new leaf primitive needed, only new call sites.

---

### `internal/dispatch/podjob/jobspec.go` (service, `JobKindVerifier` generalization)

**Analog:** `case JobKindPlanner:` in the SAME file (`jobspec.go:271-284`) — this is the exact shape `JobKindVerifier` must be rewritten to match:
```go
case JobKindPlanner:
	if opts.ParentObj != nil {
		parentUID = string(opts.ParentObj.GetUID())
	}
	jobName = PlannerJobName(opts.Level, parentUID, opts.Attempt)
	labels["tideproject.k8s/role"] = "planner"
	labels["tideproject.k8s/level"] = opts.Level
	if opts.Level != "" && parentUID != "" {
		labels[fmt.Sprintf("tideproject.k8s/%s-uid", opts.Level)] = parentUID
		labels["tideproject.k8s/task-uid"] = parentUID
	}
```

**Current broken `JobKindVerifier` case (`jobspec.go:285-303`), read directly — this is what must change:**
```go
case JobKindVerifier:
	// Phase 51 TASK-04/ESC-04: the verifier dispatches per-Task, same as
	// the executor, but with its own deterministic name + role label ...
	if opts.Task != nil {
		parentUID = string(opts.Task.UID)
	}
	jobName = VerifierJobName(opts.Task.UID, opts.Attempt)   // ← PANICS if opts.Task is nil (Plan/Phase/Milestone/Project callers)
	labels["tideproject.k8s/task-uid"] = string(opts.Task.UID) // ← same nil-deref risk
	labels["tideproject.k8s/role"] = "verifier"
	if opts.EstimatedCostCents > 0 {
		labels["tideproject.k8s/estimated-cost"] = strconv.FormatInt(opts.EstimatedCostCents, 10)
	}
```
Fix (mirrors `JobKindPlanner` exactly, per RESEARCH.md's Code Examples): read `parentUID` from `opts.ParentObj.GetUID()`, add `opts.Level` to the label set, call `VerifierJobName(opts.Level, parentUID, opts.Attempt)`. Update the ONE existing Task call site (`task_controller.go:2167-2190`, `dispatchVerifier`) to pass `ParentObj: task` alongside `Task: task` in the same commit — `dispatchVerifier` already passes `ParentObj: task` (line 2170), so this specific call site needs no change beyond the signature migration.

**Note the existing label-stamping-at-call-site precedent** (`task_controller.go:2191-2204`, right after `BuildJobSpec` returns) — `dispatchVerifier` ALREADY stamps `owner.LabelProject` onto the returned Job after the fact because `BuildJobSpec`'s `JobKindVerifier` case doesn't set it. This is the exact idiom a Plan-level `dispatchPlanVerifier` should reuse for any label `BuildJobSpec` still doesn't set generically.

---

### `internal/dispatch/podjob/names.go` (utility, `VerifierJobName` signature)

**Analog:** `PlannerJobName`, same file (`names.go:41-54`):
```go
// PlannerJobName returns the deterministic Job name for a planner dispatch at the
// given level.
//
// Format: "tide-{level}-{parentUID}-{attempt}".
func PlannerJobName(level, parentUID string, attempt int) string {
	return fmt.Sprintf("tide-%s-%s-%d", level, parentUID, attempt)
}
```
**Current `VerifierJobName` (`names.go:56-71`):**
```go
func VerifierJobName(taskUID types.UID, attempt int) string {
	return fmt.Sprintf("tide-verifier-%s-%d", taskUID, attempt)
}
```
New signature mirrors `PlannerJobName` exactly: `func VerifierJobName(level, parentUID string, attempt int) string { return fmt.Sprintf("tide-verifier-%s-%s-%d", level, parentUID, attempt) }`. **Open Question 2 (RESEARCH.md):** grep `tide-verifier-` across `test/integration/kind/` and `cmd/dashboard/` before finalizing — if any hard-coded assertion pins the CURRENT two-segment Task verifier Job name, either update it in the same commit or special-case `level=="task"` to omit the level segment.

---

### `pkg/git/worktree.go` (utility, read-only worktree helper)

**Analog:** `AddWorktree`, same file (`worktree.go:59-79`) — read in full:
```go
func AddWorktree(repoPath, taskUID, runBranch string) (string, error) {
	if repoPath == "" { return "", fmt.Errorf("git worktree: empty repoPath") }
	if taskUID == "" { return "", fmt.Errorf("git worktree: empty taskUID") }
	if runBranch == "" { return "", fmt.Errorf("git worktree: empty branch") }

	worktreeDir := filepath.Join(filepath.Dir(repoPath), "worktrees", taskUID)
	taskBranch := TaskBranchName(taskUID)

	cmd := exec.Command("git", "-C", repoPath, "worktree", "add", "-b", taskBranch, worktreeDir, runBranch)
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("git worktree add %s @ %s: %w: %s", taskUID, runBranch, err, string(out))
	}
	return worktreeDir, nil
}
```
The new read-only variant (name Claude's Discretion, e.g. `AddReadOnlyWorktree`) drops the `-b <branch>` flag entirely (checks out `runBranch` directly, no new branch minted — Anti-Pattern in RESEARCH.md: reusing `AddWorktree`'s write-branch semantics would leave dangling `tide/wt-<uid>` branches on every verify run since a level-verify never commits):
```go
cmd := exec.Command("git", "-C", repoPath, "worktree", "add", worktreeDir, runBranch) // no -b
```
Same three empty-string guards, same `CombinedOutput`-wrapped-error pattern, same `filepath.Join(filepath.Dir(repoPath), "worktrees", <uid>)` directory convention (keyed by the level's own UID per RESEARCH.md's recommended shape, mirroring `envelopeUID`). `EnsureWorktree`'s `harness/worktree.go:58-84` Role-gate (`if in.Role != "executor" { return nil }`) is the reason this canNOT be reused as-is for level-verify — the harness's planner-Role short-circuit is Go code running inside the SAME pod as the subagent, but level-verify's checkout must happen in a SEPARATE init container OUTSIDE the Python verifier image (EVAL-01's import-firewall contract) — see `## No Analog Found` below for the wiring this genuinely new piece needs.

---

### `internal/subagent/common/templates/{plan,phase,milestone,project}_verifier.tmpl` (config, D-09)

**Analog:** `internal/subagent/common/templates/task_verifier.tmpl` — read in full above; reuse its structure section-for-section:

1. **Role framing** ("You are TIDE's `<level>` verifier — a read-only evaluator...") — same opening two paragraphs, level name substituted.
2. **Tools-honesty block** — task's tools are `git_read` (worktree-scoped) + `run_gate_command`; the SAME two tools apply at every level (D-07's worktree-provisioning makes this literally true once wired) — copy verbatim.
3. **Verdict semantics block** (APPROVED/REPAIRABLE/BLOCKED with the "non-zero gate exit ≠ automatically BLOCKED" nuance) — copy VERBATIM for `plan_verifier.tmpl` (D-04's re-plan needs the SAME repairability judgment) but note: `phase_verifier.tmpl`/`milestone_verifier.tmpl`/`project_verifier.tmpl` run under `maxIterations:0` (no repair branch) — their verdict-semantics prose should still offer the same three-way vocabulary (ClassifyVerdict is unconditional), but the "REPAIRABLE sends back for repair" framing can note that at THIS level exhaustion escalates rather than iterates, so the model's REPAIRABLE-vs-BLOCKED judgment is still meaningful (drives `requireApproval` vs `escalate` severity signaling) even though no fresh attempt follows.
4. **Coverage-not-conservatism finding directive** — copy verbatim at all four new templates (D-09 explicit requirement): "Report a finding for EVERY deviation ... Coverage is your job, not triage."
5. **Gate command + required artifacts + repair-recheck templating** (`{{.Verify.GateCommand}}`, `{{range .Verify.RequiredArtifacts}}`, `{{if .Verify.EvidencePacketPath}}`) — copy verbatim; `VerifyContext` is already level-agnostic (`pkg/dispatch/envelope.go:438-471`, confirmed by direct read — no Task-only field).
6. **`plan_verifier.tmpl`'s DISTINCT content (ESC-01's four named dimensions):** replace `task_verifier.tmpl`'s "candidate's worktree is your entire reachable filesystem" framing with the goal-backward rubric explicitly: goal alignment (does the plan serve the phase brief's stated outcome), file-touch plausibility (do `FilesTouched` declarations match the task DAG's actual scope), dependency correctness (does `DependsOn` reflect real ordering constraints), verification derivability (can each child Task's `VerificationSpec` actually be evaluated against its own `DeclaredOutputPaths`). These four replace the "run the gate command for real" framing as the PRIMARY judgment (a plan-check may have no meaningful gate command of its own — it is reading authored child specs off the PVC, not running code).
7. **`phase_verifier.tmpl`/`milestone_verifier.tmpl`/`project_verifier.tmpl`'s DISTINCT content:** observable-outcome framing — "deliverables exist on the run branch, gate command exit honored, constraint paths untouched" (RESEARCH.md's own phrasing) — closer to `task_verifier.tmpl`'s own framing than plan's, since these DO run a real gate command in a real (new, D-07-provisioned) worktree.

**Loader — zero code changes needed.** `LoadPromptTemplate("verifier", "plan")` already resolves `templates/plan_verifier.tmpl` via the existing `fmt.Sprintf("templates/%s_%s.tmpl", level, role)` convention (`prompt_templates.go:82-89`) — just add the four `.tmpl` files under `templates/`; the `//go:embed templates/*.tmpl` directive (`prompt_templates.go:32`) picks them up automatically.

**`PromptTemplateVersion` bump (MAINTENANCE RULE, `prompt_templates.go:42-48`):** bump the `const PromptTemplateVersion = "v4"` in the SAME commit as adding these four templates — this is a package-level version covering every template, not per-template.

---

### `cmd/tide/approve.go` (CLI, +`findAwaitingProject`)

**Analog:** `findAwaitingPlan`, same file (`approve.go:398-415`, adjacent to the three siblings) — read the chain shape directly:
```go
func approveLevel(ctx context.Context, c client.Client, ns, projectName string) error {
	// Option A (DEBT-03): discover the AwaitingApproval target FIRST in
	// dependency-order. Milestone → Phase → Plan → Task. The first matching
	// child is the one the operator is gating on.
	if obj, level, err := findAwaitingMilestone(ctx, c, ns, projectName); err != nil {
		return err
	} else if obj != nil {
		return approveLevelTarget(ctx, c, obj, level, projectName)
	}
	if obj, level, err := findAwaitingPhase(ctx, c, ns, projectName); err != nil { ... }
	if obj, level, err := findAwaitingPlan(ctx, c, ns, projectName); err != nil { ... }
	if obj, level, err := findAwaitingTask(ctx, c, ns, projectName); err != nil { ... }
	// ... fall through to findFailedLevel UX hint, then "no level awaiting approval"
}
```
`findAwaitingProject` is a NEW function following the SAME `findAwaitingPlan`-shaped body (Get the single Project by name, check `Status.Phase == LevelPhaseAwaitingApproval`, return `(obj, "project", nil)` or `(nil, "", nil)`), inserted at the FRONT of `approveLevel`'s chain — Project is the ROOT of the hierarchy, so per the Security Domain threat pattern in RESEARCH.md ("An operator's `tide approve` targets the wrong parked level when Plan-check AND a downstream level's `requireApproval` are BOTH parked simultaneously"), the deterministic dependency-order MUST become **Project → Milestone → Phase → Plan → Task**, checked BEFORE `findAwaitingMilestone`, not after.

---

## Shared Patterns

### Fail-closed verdict classification
**Source:** `pkg/dispatch/verdict.go:95-118` (`ClassifyVerdict`)
**Apply to:** every new verify-consumption call site (`handlePlanVerifierCompletion`, and the three level-verify completion handlers in `phase_controller.go`/`milestone_controller.go`/`project_controller.go`)
```go
func ClassifyVerdict(raw json.RawMessage) Verdict {
	if len(raw) == 0 { return VerdictBlocked } // empty JSON
	var parsed struct{ Verdict string `json:"verdict"` }
	if err := json.Unmarshal(raw, &parsed); err != nil { return VerdictBlocked } // malformed
	switch Verdict(parsed.Verdict) {
	case VerdictApproved, VerdictRepairable, VerdictBlocked:
		return Verdict(parsed.Verdict)
	default:
		return VerdictBlocked // missing/unrecognized verdict field
	}
}
```
Unchanged — reuse this function directly, do not re-implement a second classifier per level.

### Verifier concurrency cap + budget reservation
**Source:** `internal/controller/dispatch_helpers.go:518-559` (`verifierInFlightCount`) + `task_controller.go:2119-2143` (cap-before-reserve ordering)
**Apply to:** every new verifier dispatch site (Plan, Phase, Milestone, Project)
```go
func verifierInFlightCount(ctx context.Context, c client.Client, ns, projectName, excludeJobName string) (int, error) {
	var jobs batchv1.JobList
	if err := c.List(ctx, &jobs, client.InNamespace(ns),
		client.MatchingLabels{"tideproject.k8s/role": "verifier", owner.LabelProject: projectName},
	); err != nil { return 0, err }
	n := 0
	for i := range jobs.Items {
		if jobs.Items[i].Name == excludeJobName { continue }
		if jobs.Items[i].DeletionTimestamp != nil { continue }
		if !isJobTerminal(&jobs.Items[i]) { n++ }
	}
	return n, nil
}
```
Project-scoped (not global), already level-agnostic (filters on `role=verifier` + project label, never task-uid VALUE) — D-10 requires this exact function, unmodified, at every new site. Cap check MUST run BEFORE `Reservations.Reserve` (Pitfall 6 — no reservation leak on cap-hit).

### Human-approval parking / `tide approve` resume
**Source:** `internal/controller/level_status.go:106-155` (`consumeApproveAndResume`) + `internal/gates` package (`CheckApprove`/`ConsumeApprove`/`CheckRejected`)
**Apply to:** every level's D-08 `requireApproval` exhaustion arm
```go
func consumeApproveAndResume(ctx context.Context, c client.Client, obj client.Object, conditions *[]metav1.Condition, fieldPtr *string, level string, resumeMessage string) (ctrl.Result, error) {
	newAnno := gates.ConsumeApprove(obj, level)
	// consume annotation FIRST (T-04-G2: crash-safety — no double-fire)
	// ... then patchLevelStatus sets phase→Running + WaveOrLevelPaused=False/ReasonApprovedByUser
}
```
D-04's own invariant applies unchanged: this helper NEVER advances a level to Succeeded — only back to Running/its own Verifying-successor-check.

### `escalate` project-wide freeze
**Source:** `internal/controller/verify_halt.go` (`checkVerifyHalt`/`setVerifyHaltIfNeeded`) — read in full above
**Apply to:** every level's D-08 `escalate` exhaustion arm (including Task, post-migration)
```go
func setVerifyHaltIfNeeded(ctx context.Context, c client.Client, project *tideprojectv1alpha3.Project, taskCompletedAt time.Time) error {
	if meta.IsStatusConditionTrue(project.Status.Conditions, tideprojectv1alpha3.ConditionVerifyHalt) {
		return nil // idempotent no-op
	}
	// CR-02 resume time-fence: refuse to re-stamp a halt for an exhaustion that
	// predates the operator's `tide resume`.
	...
	meta.SetStatusCondition(&project.Status.Conditions, metav1.Condition{
		Type: tideprojectv1alpha3.ConditionVerifyHalt, Status: metav1.ConditionTrue,
		Reason: tideprojectv1alpha3.ReasonVerifyExhausted, ...
	})
	return c.Status().Patch(ctx, project, patch)
}
```
Reused VERBATIM (file is not modified this phase) — every new escalation call site (Plan/Phase/Milestone/Project) calls this SAME function with its own `completedAt` timestamp; no per-level halt-condition variant is minted.

### Job-name determinism / idempotent AlreadyExists-as-success (SUB-03)
**Source:** `internal/dispatch/podjob/names.go` doc comments (`JobName`, `PlannerJobName`, `VerifierJobName`) + every dispatch call site's `if !apierrors.IsAlreadyExists(err) { return err }` guard (e.g. `task_controller.go:2210-2219`, `plan_controller.go:470-475`)
**Apply to:** every new Job-create call site (`dispatchPlanVerifier`, the three level-verify dispatchers, the new worktree-checkout init container's parent Job if it is a distinct Job rather than an additional init container on the existing verifier Job)
```go
if createErr := r.Create(ctx, job); createErr != nil {
	if !apierrors.IsAlreadyExists(createErr) {
		releaseOnError()
		return ctrl.Result{}, false, fmt.Errorf("create verifier job: %w", createErr)
	}
	logger.Info("verifier job already exists; treating as successful dispatch", "job", job.Name)
}
```

### Coverage-not-conservatism finding directive
**Source:** `internal/subagent/common/templates/task_verifier.tmpl` lines 58-69
**Apply to:** all four new verifier templates (D-09 explicit)
```
Report a finding for EVERY deviation you observe, however small, tagged with
an explicit severity (e.g. blocker, major, advisory) and an explicit
confidence (e.g. high, medium, low) on each one. Coverage is your job, not
triage — record the full finding set and let severity/confidence speak for
each entry. Config-driven gate policy, not you, decides which findings
actually block ...
```

---

## No Analog Found

Files/mechanisms with no close match in the codebase — RESEARCH.md's own "genuinely novel" flags, planner should treat these as new-build design decisions rather than pattern extraction:

| File / mechanism | Role | Data Flow | Reason |
|---|---|---|---|
| Level-verify worktree-checkout init container (new wiring in `jobspec.go`'s `BuildJobSpec` init-container composition, `jobspec.go:558` `initContainers := []corev1.Container{envelopeWriter}`) | Dispatch/Pod (init container) | file-I/O | No existing mechanism provisions a worktree for a level that only ever dispatches PLANNER Jobs (`EnsureWorktree` explicitly no-ops for `Role != "executor"`, `harness/worktree.go:59-63`). RESEARCH.md's own confidence rating for this piece is MEDIUM (the gap is HIGH-confidence; the recommended shape — reuse the `tide-push` git-writer image as a second init container — is a reasoned recommendation, not something any prior phase built). Flagged as Assumption A1 in RESEARCH.md. |
| Delete-then-recreate child-Task reconciliation on re-plan (new controller-side step, NOT inside `reporter.MaterializeChildCRDs`) | Controller | CRUD (Delete + re-Create) | `reporter.MaterializeChildCRDs` is Create-only by design (`materialize.go:297-302`, confirmed by direct read) and used by 4 OTHER call sites (Milestone/Phase/Project/Task materialization) that never re-plan — the delete step is a NEW, Plan-only, controller-owned action with no existing precedent to copy. RESEARCH.md's own Assumption A2 flags this as constrained-but-not-proven. |

Both mechanisms have a RESEARCH.md-authored recommended shape (see `52-RESEARCH.md` §"The missing worktree" and §"Child-Task reconciliation on re-plan") — treat that recommended shape as the design starting point, not a pattern to extract from existing code, since none exists.

## Metadata

**Analog search scope:** `api/v1alpha3/`, `internal/controller/`, `internal/dispatch/podjob/`, `internal/subagent/common/`, `pkg/dispatch/`, `pkg/git/`, `internal/harness/`, `internal/reporter/`, `internal/gates/`, `cmd/tide/`
**Files read in full or targeted-non-overlapping-range this session:** `loop_types.go`, `task_types.go`, `plan_types.go`, `project_types.go`, `shared_types.go`, `level_status.go`, `verify_halt.go`, `names.go`, `prompt_templates.go`, `task_verifier.tmpl`, `harness/worktree.go`, `pkg/git/worktree.go`, `dispatch_helpers.go` (full), `dispatch_helpers_test.go` (excerpt), `task_controller.go` (targeted: gateChecks/checkReadinessGates region, dispatchVerifier/buildVerifierEnvelopeIn/hasVerificationContract region, checkVerifyingState, applyLoopStatus/emitEvaluatorSpanForVerifier/settleVerifierSpend/haltVerify/markVerifiedSucceeded, repairOrHalt/dispatchRepairAttempt, handleVerifierCompletion, nextAttempt), `plan_controller.go` (reconcilePlannerDispatch, patchPlanSucceeded), `phase_controller.go` (boundary-push-before-Succeeded seam), `jobspec.go` (BuildOptions + BuildJobSpec through JobKindVerifier case), `verdict.go`, `envelope.go` (VerifyContext excerpt), `approve.go` (approveLevel chain), `materialize.go` (MaterializeChildCRDs), `task_verify_loop_test.go` (header + one Ginkgo `It`), `task_verify_dispatch_test.go`/`verify_halt_test.go` (test-name inventory only)
**Pattern extraction date:** 2026-07-20
