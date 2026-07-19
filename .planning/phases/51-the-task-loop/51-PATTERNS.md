# Phase 51: The Task Loop - Pattern Map

**Mapped:** 2026-07-19
**Files analyzed:** 20 (7 new, 13 modified)
**Analogs found:** 19 / 20 (one net-new pattern: CEL transition immutability — no in-repo `oldSelf` precedent, use RESEARCH Pattern 2)

> **How to read this file (planner):** Phase 51 is ~90% clone-and-wire. Every
> row below points at an existing seam read at HEAD. Copy the excerpt, swap the
> vocabulary, keep the load-bearing invariants (nil-safety, fail-closed,
> cap-before-acquire, time-fence). The genuinely-new logic is narrow: (a) the
> verifier dispatch/consume sub-state-machine in `task_controller.go`, (b) the
> Python out-of-band deterministic gate capture, (c) the anti-gaming path
> intersection, (d) the CEL transition-immutability rule (net-new marker shape).

## File Classification

| New/Modified File | Role | Data Flow | Closest Analog | Match Quality |
|-------------------|------|-----------|----------------|---------------|
| `internal/controller/verify_halt.go` (NEW) | controller / halt-helper | event-driven | `internal/controller/failure_halt.go` | exact (file-for-file clone) |
| `internal/subagent/common/templates/task_verifier.tmpl` (NEW) | template / prompt | transform | `templates/task_executor.tmpl` + `*_planner.tmpl` | role-match |
| `api/v1alpha3/task_types.go` (MOD) | model / CRD schema | CRUD | `TaskSpec.Gates` + `loop_types.go` `LoopStatus` | exact |
| `api/v1alpha3/shared_types.go` (MOD) | model / vocabulary consts | event-driven | `ConditionFailureHalt` block (:318-335) | exact |
| `api/v1alpha3/zz_generated.deepcopy.go` (MOD) | generated | — | `make generate` output | N/A (regen) |
| `pkg/dispatch/vendor_capabilities.go` (MOD) | utility / predicate | transform | `SelfInstruments` itself (:38) | exact |
| `pkg/dispatch/provider.go` (MOD) | config / doc | — | `ProviderSpec.Vendor` doc comment (:37-40) | exact |
| `pkg/otelai/attrs.go` (MOD) | utility / span-attrs | transform | `AgentInvocation` / `EvaluationAttributes` (:240,:480) | exact |
| `internal/controller/task_controller.go` (MOD) | controller / state machine | event-driven | its own dispatch/gate/reservation path | exact |
| `internal/controller/dispatch_helpers.go` (MOD) | controller / helper | event-driven | `plannerInFlightCount` + `checkDispatchHolds` (:490,:580) | exact |
| `internal/controller/project_controller.go` (MOD) | controller | event-driven | `milestone_controller.go:339` `checkDispatchHolds` call | role-match |
| `internal/controller/span_emission.go` (MOD) | controller / observability | event-driven | `synthesizePlannerSpan` (:156) | role-match |
| `internal/dispatch/podjob/caps.go` (MOD) | config / JobKind | — | `JobKindExecutor`/`JobKindPlanner` (:33-38) | exact |
| `internal/dispatch/podjob/jobspec.go` (MOD) | builder / job spec | request-response | `subagentEnv` conditional-append + `ReadOnly` (:407,:424) | exact |
| `internal/dispatch/podjob/names.go` (MOD) | utility / name | — | `JobName` / `PlannerJobName` (:37,:52) | exact |
| `cmd/tide-langgraph-verifier/verifier/__main__.py` (MOD) | service / entrypoint | request-response | anthropic vendor sentinel + existing `__main__.py` | role-match |
| `internal/controller/verify_halt_test.go` (NEW) | test | — | `failure_halt` test suite | role-match |
| `internal/controller/task_verify_loop_test.go` (NEW) | test | — | task-loop envtest patterns | role-match |
| `internal/controller/co_occurring_holds_test.go` (NEW) | test | — | dispatch-hold envtest | partial |
| `test/integration/kind/verifier_concurrency_test.go` (NEW) | test | — | `configmap_planner_concurrency_test.go` / `chaos_resume_test.go` | role-match |

---

## Pattern Assignments

### `internal/controller/verify_halt.go` (NEW — controller/halt-helper, event-driven)

**Analog:** `internal/controller/failure_halt.go` (D-09: file-for-file clone). This is a 115-line, self-contained, two-function file — `checkVerifyHalt` ↔ `checkFailureHalt`, `setVerifyHaltIfNeeded` ↔ `setFailureHaltIfNeeded` — **including the Phase-25 resume time-fence verbatim.**

**Check helper** (`failure_halt.go:56-61`) — copy exactly, swap `Failure`→`Verify`:
```go
func checkFailureHalt(project *tideprojectv1alpha3.Project) bool {
	if project == nil {
		return false
	}
	return meta.IsStatusConditionTrue(project.Status.Conditions, tideprojectv1alpha3.ConditionFailureHalt)
}
```

**Set-if-needed with the load-bearing resume time-fence** (`failure_halt.go:79-114`) — the time-fence is CR-02 (Phase 25) and MUST be preserved; a fresh impl re-introduces the pre-resume-straggler re-freeze bug (RESEARCH "Don't Hand-Roll"):
```go
func setFailureHaltIfNeeded(ctx context.Context, c client.Client, project *tideprojectv1alpha3.Project, taskCompletedAt time.Time) error {
	if project == nil {
		return nil
	}
	if project.Spec.FailureProfile != tideprojectv1alpha3.FailureProfileConservative {
		return nil // strict profile (or unset default): no-op
	}
	if meta.IsStatusConditionTrue(project.Status.Conditions, tideprojectv1alpha3.ConditionFailureHalt) {
		return nil // idempotent no-op
	}
	// CR-02 resume time-fence — refuse to re-stamp for a pre-resume straggler:
	if !taskCompletedAt.IsZero() {
		if resumeVal, ok := project.Annotations[tideprojectv1alpha3.AnnotationFailureResumedAt]; ok {
			if resumedAt, err := time.Parse(time.RFC3339, resumeVal); err == nil {
				if taskCompletedAt.Before(resumedAt) {
					return nil // stale pre-resume straggler; no-op
				}
			}
		}
	}
	patch := client.MergeFrom(project.DeepCopy())
	meta.SetStatusCondition(&project.Status.Conditions, metav1.Condition{
		Type:   tideprojectv1alpha3.ConditionFailureHalt,
		Status: metav1.ConditionTrue,
		Reason: tideprojectv1alpha3.ReasonTaskFailedHalt,
		Message: "...Run `tide resume --retry-failed`...",
		LastTransitionTime: metav1.Now(),
	})
	return c.Status().Patch(ctx, project, patch)
}
```

**Divergences the planner must apply:**
- Trigger condition differs: `setFailureHaltIfNeeded` gates on `FailureProfile == conservative`; `setVerifyHaltIfNeeded` gates on **loop exhaustion** (`Attempt >= MaxIterations` with a non-APPROVED verdict) — no FailureProfile check.
- Vocabulary: new `ConditionVerifyHalt` / `ReasonVerifyExhausted` / `AnnotationVerifyResumedAt` (see shared_types.go assignment below).
- **Distinct halt class (D-09/ESC-03):** VerifyHalt is NOT a reinterpretation of `Failed` wave semantics. The ESC-03 regression test asserts a VerifyHalt leaves phase/wave-siblings/conservative-profile propagation untouched.
- **Header-doc divergence:** `failure_halt.go`'s header (:33-37) explicitly notes it is NOT added to `project_controller.go` (execution-only, D-03). Phase 51's D-09 REVERSES this for VerifyHalt AND retro-adds FailureHalt to the Project chain (folded todo #1) — update the header comment accordingly.
- **Sharing decision (Claude's Discretion, D-09):** clone vs. share helper code — the metriccardinality precedent favors deliberate non-sharing of guard layers. Recommend clone (matches the BillingHalt→FailureHalt→VerifyHalt three-generation precedent).

---

### `internal/controller/dispatch_helpers.go` (MOD — controller/helper, event-driven)

**Analog A — `verifierInFlightCount`:** copy `plannerInFlightCount` (`dispatch_helpers.go:490-515`) OR `gitWriterInFlightCount` (`git_writer.go:100-124`). D-10 locks a **new dedicated count** (verifier is a distinct pool). The load-bearing exclusions are `DeletionTimestamp != nil` (skip GC-pending) and `!isJobTerminal`:
```go
func plannerInFlightCount(ctx context.Context, c client.Client, watchNamespace string) (int, error) {
	var jobs batchv1.JobList
	opts := []client.ListOption{client.MatchingLabels{"tideproject.k8s/role": "planner"}}
	if watchNamespace != "" {
		opts = append(opts, client.InNamespace(watchNamespace))
	}
	if err := c.List(ctx, &jobs, opts...); err != nil {
		return 0, err
	}
	n := 0
	for i := range jobs.Items {
		if jobs.Items[i].DeletionTimestamp != nil {
			continue
		}
		if !isJobTerminal(&jobs.Items[i]) {
			n++
		}
	}
	return n, nil
}
```
Swap the label to `{"tideproject.k8s/role": "verifier"}`. **Consider the `excludeJobName` param** from `gitWriterInFlightCount` (`git_writer.go:100`) — the verifier state machine dispatches/observes a deterministic Job name, so it must not count its own in-flight Job as "another" verifier (Pitfall 7 self-exclusion). Given the Task loop re-reads a deterministic verifier Job name, the git_writer shape (with `excludeJobName`) is the safer analog.

**Analog B — wire `checkVerifyHalt` into `checkDispatchHolds`** (`dispatch_helpers.go:580-620`). The chain order is load-bearing (Billing→Failure→Budget→Import); insert VerifyHalt adjacent to Failure:
```go
if checkFailureHalt(project) {
	logf.FromContext(ctx).V(1).Info("dispatch held: project failure halt (conservative profile)",
		"level", level, "name", objName, "project", project.Name)
	return true, ctrl.Result{RequeueAfter: 30 * time.Second}
}
// NEW: Phase 51 ESC-02 — VerifyHalt gates the planner tier too.
if checkVerifyHalt(project) {
	return true, ctrl.Result{RequeueAfter: 30 * time.Second}
}
```
**D-09 fold:** the doc comment at `:571-576` currently documents that `TaskReconciler` intentionally does NOT call `checkDispatchHolds` (Import-position divergence). D-09 Option 1 REVERSES this — `gateChecks` migrates onto `checkDispatchHolds`, normalizing the order, gated behind a co-occurring-holds envtest. Update this doc comment when the divergence is resolved.

---

### `internal/controller/task_controller.go` (MOD — controller/state machine, event-driven)

This is the largest surface. Multiple sub-patterns, all analogs are in-file.

**(1) `gateChecks` VerifyHalt wiring + chain normalization (D-09).** Current inline chain with the documented Import-order divergence is at `:390-402`:
```go
// Item 7 (Phase 41 D-07): this gate chain intentionally stays inline and is
// NOT a caller of checkDispatchHolds ... Import is checked SECOND ... whereas
// the planner tier (via checkDispatchHolds) checks Import LAST. ...
// See .planning/todos/pending/2026-07-12-task-dispatch-gate-order-divergence.md
```
D-09 folds this todo: migrate onto `checkDispatchHolds`. The terminal short-circuit + FailureHalt-at-terminal pattern (`:336-360`) is the model for a VerifyHalt-at-terminal hook if needed. Note `gateChecks` already stamps FailureHalt on the `Failed` terminal branch with the CR-02 time-fence (`:347-357`) — mirror this exactly for the verify-exhausted path:
```go
if project, pErr := r.resolveProject(ctx, task); pErr == nil && project != nil {
	var taskCompletedAt time.Time
	if task.Status.CompletedAt != nil {
		taskCompletedAt = task.Status.CompletedAt.Time
	}
	if hErr := setFailureHaltIfNeeded(ctx, r.Client, project, taskCompletedAt); hErr != nil { ... }
}
```

**(2) Cap-gate-before-acquire + deferred reservation release (Pitfall 6).** The `reconcileDispatch` committed-flag pattern (`:301-306`) is the deferred-release template — the `verifierInFlightCount` cap check goes BEFORE any pool acquire and BEFORE `Reserve`:
```go
committed := false
defer func() {
	if !committed {
		release()
	}
}()
```
Mirror the planner cap-before-acquire from `milestone_controller.go:344-357` at the verifier dispatch site (RESEARCH Pattern 3):
```go
if r.PlannerPool != nil { // → verifier pool
	inFlight, err := plannerInFlightCount(ctx, r.Client, r.WatchNamespace) // → verifierInFlightCount
	if err != nil { return ctrl.Result{}, fmt.Errorf("planner in-flight count: %w", err) }
	if inFlight >= r.PlannerPool.Capacity() {
		return ctrl.Result{RequeueAfter: 10 * time.Second}, nil // defer, no slot leak
	}
}
```

**(3) Reservation reserve/settle for BudgetCents (D-10).** The `Reserve`/`Settle`/`Release` triad: `Reserve` at `:889`, `defer ...Settle` at `:1247`, `Release` at `:202`. `Reservations *budget.ReservationStore` at `:107-109`, nil-receiver-safe. `LoopPolicy.BudgetCents` rides this existing accounting.

**(4) Verdict consumption decision tree (D-06).** Reuse `dispatch.ClassifyVerdict` (fail-closed). RESEARCH Code Example grounds this in `handleJobCompletion:1249-1471` + `verdict.go`. The `EnvReader.ReadOut` + unreadable→fail-closed pattern mirrors the existing `EnvelopeReadFailed` path (`:1251`):
```go
out, err := r.Deps.EnvReader.ReadOut(ctx, string(project.UID), string(task.UID))
if err != nil || out.Verdict == nil {
	return r.haltVerify(ctx, task, project, "verifier envelope unreadable") // fail-closed = BLOCKED
}
switch dispatch.ClassifyVerdict(raw) {
case dispatch.VerdictApproved:
	if hasDeterministicFailure(out.Verdict) { return r.repairOrHalt(...) } // D-06 defence-in-depth
	return r.markSucceeded(ctx, task)
case dispatch.VerdictRepairable:
	return r.repairOrHalt(...)
default:
	return r.haltVerify(...)
}
```

**(5) Fresh-attempt vs infra-retry (D-05, Pitfall 4).** `nextAttempt` at `:1769-1810` (current max + 1). The blind `maxAttemptsPerTask` quality-retry (`:729` region) is **superseded** by evaluator-driven attempts — NOT the eviction path. Keep the two grep-distinguishable: quality-iteration increments `Task.Status.Attempt` → new `attemptID` tuple; eviction rerun keeps the same `attemptID`.

**(6) VerifyContext-populated envelope (D-01/D-04).** `buildEnvelopeIn` at `:1831` is the shape. Populate `EnvelopeIn.Verify` (`*VerifyContext`) with `GateCommand ← task.Spec.Verification.GateCommand` (the LOCKED command), `EvidencePacketPath` (empty on first verify, staged path on repair), `Provider.Vendor: "langgraph"`, `Provider.Model: ResolveProvider(project,"task",defs).Model`.

**(7) SelfInstruments call site (D-02/D-11).** Already flows through `:1117`:
```go
skipMessageSpans := pkgdispatch.SelfInstruments(ResolveProvider(project, "task", r.Deps.HelmProviderDefaults).Vendor)
```
With `SelfInstruments("langgraph")=true`, this now returns `true` for a langgraph verifier dispatch → reporter skips `events.jsonl` synthesis. No signature change needed.

**(8) Verifier image ref reaches manager via a new `Deps` field (RESEARCH A5).** Mirror the existing `Deps` field pattern (`ReserveEstimateCents`, `HelmProviderDefaults`, `WatchNamespace`); a dev-head default flag suffices — the chart surface is Phase 53.

---

### `internal/controller/project_controller.go` (MOD — controller, event-driven; folds todo #1)

**Analog:** the planner-dispatch hold block at `:1540-1564` currently has `checkBillingHalt` + `checkBudgetBlocked` + Import but **NO `checkFailureHalt`** (confirmed at HEAD):
```go
if checkBillingHalt(project) { ... return ctrl.Result{RequeueAfter: 30 * time.Second}, nil }
if checkBudgetBlocked(project) && !budget.IsBypassed(project, time.Now()) { ... }
// <-- no checkFailureHalt here (the gap)
if project.Spec.ImportSource != nil { ... } // Import
```
D-09 folds `2026-07-12-project-dispatch-missing-failurehalt-gate`: add BOTH `checkFailureHalt(project)` and `checkVerifyHalt(project)` to this block (30s requeue each), matching `checkDispatchHolds`'s order, gated behind a conservative-profile envtest. This is a deliberate, tested behavior change — the Project planner currently spends under a conservative halt.

---

### `internal/controller/span_emission.go` (MOD — controller/observability, event-driven)

**Analog:** `synthesizePlannerSpan` (`:156-269`) is the AGENT-span emitter. Note its explicit forward-reference at `:239-241`:
```go
// evaluation.result / evaluation.version / human_intervention are NOT
// stamped here — Phase 51's EVALUATOR span populates them (CONTEXT
// <specifics>: do not fake-populate ahead of the owning phase).
```
D-11: the EVALUATOR span is a **sibling** of the checked level's AGENT span (not a child). Reuse the exact trace-anchoring spine: `otelai.TraceIDFromUID(project.UID)` (`:180`), `trace.NewSpanContext{..., Remote: true}` (`:189-194`), `otel.Tracer("tide.dispatch")` (`:200`). The AGENT-span attribute pattern to parallel (`:210`, `:247-251`):
```go
span.SetAttributes(otelai.AgentInvocation(provider.Vendor, spanName, role, level)...)
span.SetAttributes(otelai.LoopAttributes(otelai.LoopKindExecution, out.AttemptID, out.LoopRunID,
	out.Usage.Iterations, candidateVersion, string(out.TerminalReason))...)
```
For the EVALUATOR span, swap `AgentInvocation` → a new `EvaluatorInvocation` (SpanKindEvaluator, see attrs.go assignment), use a new `LoopKindEvaluator` (or the task loop kind), and add `otelai.EvaluationAttributes(...)` + `otelai.HumanIntervention()`. **No double-emission (Pitfall):** because `SelfInstruments("langgraph")=true` the reporter skips `events.jsonl` synthesis — the EVALUATOR span is the sole loop-native evaluator provenance.

---

### `pkg/otelai/attrs.go` (MOD — utility/span-attrs, transform)

**Analog:** `AgentInvocation` (`:240-248`) is the template for a new `EvaluatorInvocation` helper — same shape, swap `SpanKindAgent`→`SpanKindEvaluator`:
```go
func AgentInvocation(system, name, role, level string) []attribute.KeyValue {
	return []attribute.KeyValue{
		attribute.String(semconv.OpenInferenceSpanKind, semconv.SpanKindAgent),
		attribute.String(semconv.LLMSystem, system),
		attribute.String(semconv.AgentName, name),
		attribute.String(keyAgentRole, role),
		attribute.String(keyAgentInvocationLevel, level),
	}
}
```
`semconv.SpanKindEvaluator` ("EVALUATOR") is VERIFIED present in the pinned OpenInference module (v0.1.1). The `evaluation.*` / `human_intervention` helpers already exist **defined-but-empty** and Phase 51 is their first consumer (`:476-492`):
```go
func EvaluationAttributes(result, version string) []attribute.KeyValue {
	return []attribute.KeyValue{
		attribute.String(keyEvaluationResult, result),
		attribute.String(keyEvaluationVersion, version),
	}
}
func HumanIntervention() attribute.KeyValue {
	return attribute.Bool(keyHumanIntervention, true)
}
```
**Don't hand-roll string literals** — `TestKeysUseSemconvModule` grep-guard rejects raw `"EVALUATOR"` / `"evaluation.result"`; always route through these helpers + the module const. `LoopKindExecution` const at `:430` notes "Phase 51 adds evaluator/task loop kinds" — add the new kind const here.

---

### `pkg/dispatch/vendor_capabilities.go` (MOD — utility/predicate, transform) + `provider.go`

**Analog:** `SelfInstruments` itself (`vendor_capabilities.go:38-45`). D-02: add the `"langgraph"→true` case; keep the signature (pure vendor predicate, no arg change — preserves the Phase-45 ADAPT-01 seam):
```go
func SelfInstruments(vendor string) bool {
	switch vendor {
	case "langgraph":
		return true // self-instruments via openinference-instrumentation-langchain; reporter skips synthesis
	case "anthropic", "openai", "google", "xai", "opencode":
		return false
	default:
		return false // fail-closed
	}
}
```
**Keep the vendor literals identical** to `ProviderSpec.Vendor`'s doc comment (`provider.go:37-40`) — the doc-comment says so explicitly. D-02 adds `"langgraph"` to that canonical set:
```go
// Vendor is the provider sentinel string the subagent image checks at
// startup. Canonical values: "anthropic", "openai", "google", "xai",
// "opencode".  // ← add "langgraph"
```

---

### `api/v1alpha3/task_types.go` (MOD — model/CRD schema, CRUD)

**Analog A — the `Gates` embedding + precedence doc** (`task_types.go:114-122`): the `verification` block is a new zero-value or pointer embed on `TaskSpec`, mirroring how `Gates` embeds with a documented precedence:
```go
// Gates ... Mirrors the Gates pattern on ProjectSpec; the Task-level field
// takes precedence over the Project-level Project.Spec.Gates.Task ...
// +optional
Gates Gates `json:"gates,omitempty"`
```
D-01 notes the identical shape generalizes to `Plan.Spec`/`Project.Spec` in Phase 52 with Task > Plan > Project precedence — so structure `VerificationSpec` as a standalone type (like `Gates` / `Caps`), not inline fields.

**Analog B — `Caps` two-homes decoupling** (`task_types.go:23-51`) is the design-note template for keeping `api/v1alpha3.VerificationSpec` (CEL-validated CRD type) distinct from `pkg/dispatch.VerifyContext` (Go wire type). Same pattern as `EvaluationSummary` ↔ `GateDecision` (`loop_types.go:129-140`). The reconciler translates one to the other at dispatch time.

**Analog C — CEL marker syntax** (`task_types.go:88` and `:203`) — existing `XValidation` markers in this exact file show the syntax:
```go
// +kubebuilder:validation:XValidation:rule="!self.exists(d, d == '')",message="dependsOn entries must not be empty strings"
// +kubebuilder:validation:XValidation:rule="!has(self.spec.dependsOn) || !(self.metadata.name in self.spec.dependsOn)",message="a task cannot depend on itself"
```
**CAUTION (see No-Analog section):** these are same-state rules; the immutability rule needs an `oldSelf` **transition** rule — no in-repo precedent. Use RESEARCH Pattern 2.

**Analog D — `LoopStatus` embedding on status** (`loop_types.go:75-127`): D-07 embeds `LoopStatus` on `TaskStatus` (current-iteration summary + exit reason only — LOOP-03, no history). `TaskStatus` already carries `Attempt int` (`:157`), `Conditions` (`:151-154`), and the optional-timestamp/marker-UID pattern (`:162-193`). Add `lockedSHA` (a runtime observation) here per D-03/OQ1 — NOT the governing `phase`/`version` enum (those go on spec so the CEL transition rule can read `oldSelf`).

---

### `api/v1alpha3/shared_types.go` (MOD — model/vocabulary consts, event-driven)

**Analog:** the `ConditionFailureHalt` block (`:318-335`) — clone verbatim, swap `Failure`→`Verify`:
```go
const (
	ConditionFailureHalt = "FailureHalt"
	ReasonTaskFailedHalt = "TaskFailedHalt"
	AnnotationFailureResumedAt = "tideproject.k8s/failure-resumed-at"
)
```
New block: `ConditionVerifyHalt = "VerifyHalt"`, `ReasonVerifyExhausted = "VerifyExhausted"` (or similar), `AnnotationVerifyResumedAt = "tideproject.k8s/verify-resumed-at"`. The three-generation vocabulary precedent (BillingHalt `:264-282` → FailureHalt `:318-335`) is the established pattern. Keep the doc-comment shape (what sets it, what reads it, what clears it).

---

### `internal/dispatch/podjob/caps.go` + `names.go` + `jobspec.go` (MOD — builder/job spec)

**`caps.go` analog:** `JobKind` enum (`:33-38`). RESEARCH A1 recommends a dedicated `JobKindVerifier` (grep-distinct verify caps + `role=verifier` concurrency label) over reusing `ReadOnly` on `JobKindExecutor`:
```go
const (
	JobKindExecutor JobKind = "executor" // 1200s floor
	JobKindPlanner  JobKind = "planner"  // 1800s floor
	// NEW: JobKindVerifier — shorter verify floor
)
```
Add a `verifierCapsFloorSeconds` const + a `DefaultCaps` switch arm (`:99-122`). The floor-raising doc-comment discipline (`:40-65`) shows the convention for documenting why a floor value is what it is.

**`names.go` analog:** `JobName` / `PlannerJobName` (`:37-54`) — the deterministic-name idempotency tuple. Add a `VerifierJobName(taskUID, attempt)` mirroring the `tide-{kind}-{uid}-{attempt}` shape. The name IS the dedup key (AlreadyExists == idempotent success) and the label-selector target for `verifierInFlightCount`.

**`jobspec.go` analog — env injection:** the conditional `subagentEnv` append (`:444-462`, TIDE_PRICING_OVERRIDES_JSON / TRACEPARENT) is the exact shape for injecting `TIDE_GATE_COMMAND` from `VerifyContext.GateCommand`:
```go
if opts.PricingOverridesJSON != "" {
	subagentEnv = append(subagentEnv, corev1.EnvVar{Name: "TIDE_PRICING_OVERRIDES_JSON", Value: opts.PricingOverridesJSON})
}
```
**`ReadOnly` variant already wired** (`:188-206`, `:424-442`): RO `/workspace` mount + verifier-scratch `/scratch` emptyDir + `TMPDIR`/`HOME`→`/scratch`. **The critical forward-note at `:199-204`:** dispatch must add a **separate read-write `envelopes/` subPath mount** of the same volume because `out.json` cannot be written through the ReadOnly mount — "Resolving that mount split is explicitly Phase 51's job." This is genuinely-new wiring, not a clone.

---

### `internal/subagent/common/templates/task_verifier.tmpl` (NEW — template/prompt, transform)

**Analog:** `templates/task_executor.tmpl` (structure) + the loader `LoadPromptTemplate` (`prompt_templates.go:80-87`). The `<level>_<role>.tmpl` convention means the file MUST be named `task_verifier.tmpl` to load via `LoadPromptTemplate("verifier", "task")` — zero new loader machinery (`fmt.Sprintf("templates/%s_%s.tmpl", level, role)`):
```go
func LoadPromptTemplate(role, level string) (*template.Template, error) {
	name := fmt.Sprintf("templates/%s_%s.tmpl", level, role)
	tmpl, err := template.ParseFS(templateFS, name)
	...
}
```
The template execution context is a `pkgdispatch.EnvelopeIn` value (`{{.TaskUID}}`, `{{.Provider.Model}}`, `{{.Prompt}}`, etc.) — same as `task_executor.tmpl`. **Content directive (D-12/EVAL-04):** prompt for **coverage** — emit a finding for *every* deviation with severity + confidence tags; config/policy alone decides what blocks. Per the Opus-4.8 tuning note, do NOT prompt "be conservative / only high-severity."

**MAINTENANCE RULE (prompt_templates.go:42-48):** bump `PromptTemplateVersion` in the SAME commit as adding this template — it is a run-evidence field consumed for cross-attempt comparison; a stale value silently corrupts it.

---

### `cmd/tide-langgraph-verifier/verifier/__main__.py` (MOD — service/entrypoint, request-response)

**Analog A — vendor sentinel refusal** mirrors the Go anthropic subagent (`internal/subagent/anthropic/subagent.go:62` + `:219-221`):
```go
const vendorSentinel = "anthropic"
// ...
if in.Provider.Vendor != vendorSentinel {
	return pkgdispatch.EnvelopeOut{}, fmt.Errorf("anthropic subagent: refusing vendor=%q (expected %q)", ...)
}
```
D-02: flip the Python `SUPPORTED_VENDOR = "anthropic"` (`__main__.py:30`) → `"langgraph"`; the verifier image refuses any other vendor at startup. The current code even has a forward-note: `# The "langgraph" sentinel question is deferred to Phase 51`.

**Analog B — `TIDE_GATE_COMMAND` env is already the sole gate source** (`tools.py:56`, `run_gate_command:126`) — the tool reads ONLY the orchestrator-set env var, fail-closed if empty, `del command` discards the model-supplied arg (security invariant). Phase 51's entrypoint must **set** `TIDE_GATE_COMMAND` from `env.verify.gateCommand` before the agent runs. The out-of-band deterministic capture (D-06, RESEARCH OQ3): entrypoint runs the gate command once deterministically, captures returncode, forces `Verdict ∈ {REPAIRABLE, BLOCKED}` on non-zero regardless of the LLM verdict, emits a `blocker`/`gate-command` finding carrying `exit_code=N`.

**Analog C — injectable seams for offline tests** (`__main__.py:74-75`): `build_model` / `run_agent_fn` params let envtest-adjacent tests drive the full flow with a fake chat model — reuse for the D-06 dominance test.

**Analog D — verdict assembly** reuses the already-ported `GateDecision`/`classify_verdict` (`verdict.py`, Phase 49) — the Go↔Python duality means any wire-field change lands in both with matching tests. Prompts stay Go-only (no Python port).

---

## Shared Patterns

### Fail-closed verdict classification
**Source:** `pkg/dispatch/verdict.go` (`ClassifyVerdict:102-118`)
**Apply to:** `task_controller.go` verdict consumption + `__main__.py` verdict assembly
Empty/malformed/missing-verdict → `VerdictBlocked`, never `VerdictApproved`. The bare `Verdict` return (no error) makes "unknown" inexpressible as anything but BLOCKED. D-06 defence-in-depth: even a top-level APPROVED loses to a `Finding{Severity:"blocker", Dimension:"gate-command"}`.

### Cap-before-acquire + deferred release (no slot/reservation leak)
**Source:** `milestone_controller.go:344-357` (cap gate) + `task_controller.go:301-306` (committed-flag deferred release)
**Apply to:** the verifier dispatch site (D-10, Pitfall 6)
Order: `verifierInFlightCount` cap check → `Reserve` → pool acquire → `createJob` → set `committed=true`. On any early return before commit, the deferred `release()` + `Settle`/`Release` fire.

### Resume time-fence on halt re-stamping
**Source:** `failure_halt.go:90-103` (CR-02)
**Apply to:** `verify_halt.go` `setVerifyHaltIfNeeded`
A Verifying Task can reconcile between a `tide resume` clear and its own reset; the time-fence refuses to re-stamp for a completion predating the resume annotation. Fail-closed (zero timestamp / unparseable annotation → stamp).

### Bounded run-evidence + anti-gaming intersection
**Source:** `pkg/dispatch/run_evidence.go` (`ChangedFiles:70`, `Bounded()`, `MaxRunEvidenceChangedFiles=100`)
**Apply to:** `task_controller.go` anti-gaming detector (D-08)
Intersect `out.RunEvidence.ChangedFiles` (path+status, already bounded) with a planner-declared/config **protected-path set** scoped to the evaluator + fixtures the contract depends on (NOT all `*_test.go` — Pitfall 5). An intersecting attempt is a system escalation, never a pass. Do NOT re-run `git diff` in the controller — the manifest is already produced (Phase 50).

### Deterministic TraceID + sibling-span emission
**Source:** `span_emission.go:180-210` (`TraceIDFromUID` + `NewSpanContext{Remote:true}` + `AgentInvocation`)
**Apply to:** the EVALUATOR span (D-11)
Ride the v1.0.8 trace spine — no new trace plumbing. EVALUATOR is a **sibling** of the checked AGENT span; `SelfInstruments("langgraph")=true` prevents reporter double-emission.

### Compiled-in template loader (no runtime FS dependency)
**Source:** `prompt_templates.go:80` (`LoadPromptTemplate` + `//go:embed templates/*.tmpl`)
**Apply to:** `task_verifier.tmpl`
Filename convention `<level>_<role>.tmpl` auto-resolves; bump `PromptTemplateVersion` same-commit.

---

## No Analog Found

Files/patterns with no close in-repo match (planner should use RESEARCH patterns instead):

| Pattern | Role | Data Flow | Reason | Use Instead |
|---------|------|-----------|--------|-------------|
| CEL transition immutability on `spec.verification` | model / admission | — | No `oldSelf`-based transition rule exists anywhere in `api/v1alpha3/` (only same-state `!self.exists` / self-dep rules). Grep for `oldSelf`/`self == oldSelf`/`Immutable` returns nothing. | RESEARCH Pattern 2: `+kubebuilder:validation:XValidation:rule="oldSelf.phase != 'Locked' \|\| self == oldSelf \|\| self.phase == 'Superseded'"`. **KEY (OQ1/Pitfall 2):** the governing `phase`/`version` MUST live on `spec.verification` (a spec transition rule cannot reference `status`); only the observed `lockedSHA` lives on `Task.Status`. Verify at `make manifests` that the CRD carries the rule. |
| Verifier dispatch/consume sub-state-machine (new Task "Verifying" phase) | controller / state machine | event-driven | No controller code dispatches a verifier Job, sets `TIDE_GATE_COMMAND`, or reads `EnvelopeOut.Verdict` today — the Phase-48 image is buildable but dispatched nowhere. | Compose from in-file analogs: `checkRunningState:620` (deterministic-Job re-read/resume) + `handleJobCompletion:1236` (terminal branch) + the `buildEnvelopeIn:1831` shape. RESEARCH A4: decide whether to add a new `LevelPhase` value ("Verifying") or reuse `Running`+condition (affects the `gateChecks` terminal short-circuit at `:336`). |
| Out-of-band deterministic gate-exit capture (Python) | service / entrypoint | request-response | The current `run_gate_command` returns the exit as tool text to the model; nothing forces the assembled verdict to honor it. | RESEARCH OQ3: entrypoint runs the gate command once deterministically, forces non-APPROVED on non-zero exit, emits a structural `blocker`/`gate-command` finding. New logic, not a clone. |
| RW `envelopes/` subPath mount alongside the RO `/workspace` verifier mount | builder / job spec | — | The `ReadOnly` variant (`jobspec.go:188`) exists but `out.json` cannot be written through it; the mount-split is explicitly deferred to Phase 51 (forward-note `:199-204`). | New wiring: add a second read-write subPath VolumeMount of `VolumeProjectWorkspace` scoped to `envelopes/<uid>/`. |

---

## Metadata

**Analog search scope:** `internal/controller/`, `internal/dispatch/podjob/`, `internal/subagent/`, `pkg/dispatch/`, `pkg/otelai/`, `api/v1alpha3/`, `cmd/tide-langgraph-verifier/verifier/`
**Files scanned:** ~18 source files read at HEAD (all cited with file:line in RESEARCH §Sources)
**Pattern extraction date:** 2026-07-19
**Note:** All line numbers verified against HEAD this session; RESEARCH.md §Sources cross-references them. Re-verify if the branch advances significantly (RESEARCH "Valid until: 2026-08-18").
