# Phase 43: Task-Level Parity + Trace-Context Propagation - Pattern Map

**Mapped:** 2026-07-16
**Files analyzed:** 13 (all MODIFY — no new files this phase)
**Analogs found:** 13 / 13 (every file's analog is another location in the SAME file set — this phase retrofits an existing four-level pattern to a fifth level and to itself)

**Environment note:** RESEARCH.md flagged this worktree as 45 commits behind `main` at research time. Re-verified at pattern-mapping time: `git merge-base HEAD main` = `97644b4` = `main`'s HEAD, and `git rev-list --left-right --count HEAD...main` = `4\t0` (4 ahead, 0 behind). The worktree is now current — all line numbers below were read directly from this worktree's working tree, not from `main` via `git show`.

## File Classification

| New/Modified File | Role | Data Flow | Closest Analog | Match Quality |
|---|---|---|---|---|
| `internal/controller/span_emission.go` | utility (shared helper) | transform | itself — signature retrofit (input parent SpanID, output SpanID) | exact (self-modify) |
| `internal/controller/milestone_controller.go` (`handleMilestoneJobCompletion` span block, dispatch-prep) | controller | event-driven | `project_controller.go`'s equivalent block (both already fully resolve Project, no new fetch needed) | exact |
| `internal/controller/phase_controller.go` (`handlePhaseJobCompletion` span block, dispatch-prep, `resolveProject`) | controller | event-driven | `milestone_controller.go`'s block, PLUS its own `resolveProject` needs restructuring | exact (self + milestone as behavioral analog) |
| `internal/controller/plan_controller.go` (`handlePlannerJobCompletion` span block, dispatch-prep, `resolveProjectForPlan`) | controller | event-driven | `phase_controller.go`'s block (same new-fetch shape) | exact |
| `internal/controller/project_controller.go` (`handleProjectJobCompletion` span block, dispatch-prep) | controller | event-driven | `milestone_controller.go`'s block (Project is root — simplest case, D-02) | exact |
| `internal/controller/task_controller.go` (`handleJobCompletion` — net-new span block, `prepareDispatch`/`createDispatchJob`) | controller | event-driven | the four existing planner completion handlers, esp. `plan_controller.go` (Plan is Task's structural sibling — both have a label-fast-path `resolveProject` that skips the immediate parent) | role-match (Task's control-flow shape diverges — see Pitfall 1 below) |
| `internal/controller/dispatch_helpers.go` (`spawnReporterIfNeeded`) | utility (shared helper) | transform | itself — add `traceParent string` param | exact (self-modify) |
| `internal/controller/reporter_jobspec.go` (`ReporterOptions`, `BuildReporterJob`) | service (Job-spec builder) | transform | itself — add field + `--traceparent` Arg, mirroring the existing 6-flag Args convention | exact (self-modify) |
| `internal/dispatch/podjob/jobspec.go` (`BuildOptions`, `BuildJobSpec`) | service (Job-spec builder) | transform | itself — the credproxy conditional-env-append block (lines 405-412) is the literal template for `TRACEPARENT` | exact (self-modify, in-file precedent) |
| `cmd/tide-reporter/main.go` (`main`, `reporterConfig`) | utility (CLI entrypoint) | request-response (flag parse) | itself — the existing 6 `fs.String(...)` registrations are the template for a 7th | exact (self-modify) |
| `api/v1alpha3/milestone_types.go`, `phase_types.go`, `plan_types.go`, `project_types.go` (`*Status` structs) | model (CRD status) | CRD (declarative) | the existing `{Level}SpanEmittedUID` flat-string field in each file | exact |
| `api/v1alpha3/task_types.go` (`TaskStatus`) | model (CRD status) | CRD (declarative) | the four sibling `*Status` structs' `{Level}SpanEmittedUID` field (Task has neither this nor the new field today) | role-match (net-new field family, not just net-new value) |
| `internal/controller/span_emission_test.go` | test | event-driven (Ginkgo/Gomega envtest) | the four existing `Describe("SpanEmission — {Level} level", ...)` blocks — mirror exactly for a fifth `"SpanEmission — Task level"` block, plus new parent-linkage assertions in all five | exact |

## Pattern Assignments

### `internal/controller/span_emission.go` — the shared signature retrofit

**Analog:** itself (`synthesizePlannerSpan`, lines 115-164)

**Current signature and body — the exact BEFORE state every call site depends on** (full file, 165 lines, already read in full — no re-read needed):

```go
// lines 115-164
func synthesizePlannerSpan(
	ctx context.Context,
	level string,
	project *tideprojectv1alpha3.Project,
	helmDefaults ProviderDefaults,
	completedJob *batchv1.Job,
	out pkgdispatch.EnvelopeOut,
	envReadOK bool,
) bool {
	endTime, ok := spanEndTime(completedJob)
	if !ok || completedJob.Status.StartTime == nil {
		return false
	}
	startTime := completedJob.Status.StartTime.Time

	// D-04/D-07: second, envelope-independent call — nil-safe for project==nil.
	provider := ResolveProvider(project, level, helmDefaults)

	tracer := otel.Tracer("tide.dispatch")
	spanName := "tide.dispatch." + level
	_, span := tracer.Start(ctx, spanName, trace.WithTimestamp(startTime))

	span.SetAttributes(otelai.AgentInvocation(provider.Vendor, spanName, "planner", level)...)
	span.SetAttributes(otelai.LLMIdentity(provider.Vendor, provider.Model)...)

	if envReadOK {
		promptTokens := out.Usage.InputTokens + out.Usage.CacheReadTokens + out.Usage.CacheCreationTokens
		span.SetAttributes(otelai.TokenCount(
			int(promptTokens),
			int(out.Usage.OutputTokens),
			int(out.Usage.CacheReadTokens),
			int(out.Usage.CacheCreationTokens),
		)...)
	} else {
		span.SetAttributes(otelai.EnvelopeDegraded())
	}

	if isJobFailed(completedJob) {
		span.SetStatus(codes.Error, out.Reason)
		if envReadOK {
			span.SetAttributes(otelai.FailureDetail(out.ExitCode, out.Reason)...)
		}
	} else {
		span.SetStatus(codes.Ok, "")
	}

	span.End(trace.WithTimestamp(endTime))
	return true
}
```

**Required retrofit shape** (RESEARCH.md Pattern 2/3 — two-sided signature change, no custom IDGenerator):
- ADD a `parentSpanID trace.SpanID` input parameter (zero value `trace.SpanID{}` for Project — D-02; the real persisted parent span ID for Milestone/Phase/Plan/Task).
- CHANGE the return type from `bool` to `(trace.SpanID, bool)`.
- Insert, before `tracer.Start`: derive `traceID, err := otelai.TraceIDFromUID(string(project.UID))`; on error, log non-fatally and `return trace.SpanID{}, false` (Pitfall 5).
- Build `sc := trace.NewSpanContext(trace.SpanContextConfig{TraceID: traceID, SpanID: parentSpanID, TraceFlags: trace.FlagsSampled, Remote: true})` and `ctx = trace.ContextWithSpanContext(ctx, sc)` before `tracer.Start(ctx, ...)`.
- Change the final two lines to capture and return the real ID:
  ```go
  span.End(trace.WithTimestamp(endTime))
  return span.SpanContext().SpanID(), true
  ```
- The early-return at the top (`if !ok || completedJob.Status.StartTime == nil { return false }`) must become `return trace.SpanID{}, false`.

**Imports to add:** none beyond what's already imported (`trace` is already imported at line 35; `otelai` at line 42 — `otelai.TraceIDFromUID` is a new call on an already-imported package).

---

### `internal/controller/{milestone,phase,plan,project}_controller.go` — the four existing call sites (retrofit for parenting)

**Analog:** each of the other three (near-identical boilerplate) — verified byte-for-byte structurally identical modulo level name and receiver type.

**BEFORE — milestone_controller.go lines 553-597** (the marker-gate + call, in full):
```go
	// Phase 42 D-01/D-02/D-04: synthesize at most one retroactive AGENT span
	// per planner Job attempt, gated by the durable MilestoneSpanEmittedUID
	// marker keyed by Job UID ...
	if completedJob != nil && ms.Status.MilestoneSpanEmittedUID != string(completedJob.UID) && plannerSpanResolvable(completedJob) {
		stamped := false
		if mErr := retry.RetryOnConflict(retry.DefaultRetry, func() error {
			latest := &tideprojectv1alpha3.Milestone{}
			if err := r.Get(ctx, client.ObjectKeyFromObject(ms), latest); err != nil {
				return err
			}
			if latest.Status.MilestoneSpanEmittedUID == string(completedJob.UID) {
				return nil // already stamped by a concurrent reconcile — its stamper emits
			}
			markerPatch := client.MergeFromWithOptions(latest.DeepCopy(), client.MergeFromWithOptimisticLock{})
			latest.Status.MilestoneSpanEmittedUID = string(completedJob.UID)
			if err := r.Status().Patch(ctx, latest, markerPatch); err != nil {
				return err
			}
			stamped = true
			return nil
		}); mErr != nil {
			logger.Error(mErr, "MilestoneSpanEmittedUID marker patch failed (non-fatal); span deferred to a later reconcile", "milestone", ms.Name)
		} else if stamped {
			synthesizePlannerSpan(ctx, "milestone", project, r.Deps.HelmProviderDefaults, completedJob, out, envReadOK)
		}
	}
```

**BEFORE — phase_controller.go lines 494-538**, **plan_controller.go lines 538-582**, **project_controller.go lines 1803-1853**: byte-identical shape, only the receiver var (`ph`/`plan`/`project`), the marker field name (`PhaseSpanEmittedUID`/`PlanSpanEmittedUID`/`PlannerSpanEmittedUID`), and the level string literal (`"phase"`/`"plan"`/`"project"`) differ. Project's call site additionally carries a comment (lines 1819-1824) clarifying it is deliberately NOT inside the D-11/R-13 ImportSource suppression branch — preserve that comment's intent when retrofitting.

**Required retrofit per site:**
1. Resolve `parentSpanID trace.SpanID` before the `if stamped` block — for Milestone/Project this is free (Project fully resolved already at Milestone's site since Project IS Milestone's immediate parent; Project itself is root → `trace.SpanID{}`). For Phase/Plan, this requires the new immediate-parent fetch (see next section).
2. Change the call: `thisSpanID, emitted := synthesizePlannerSpan(ctx, "milestone", project, r.Deps.HelmProviderDefaults, completedJob, out, envReadOK, parentSpanID)` (or however the planner orders the new param).
3. ADD a second, separately-retried status patch after `emitted` is true, writing the new durable field (e.g. `latest.Status.MilestoneTraceSpanID = thisSpanID.String()`) — Pitfall 2: this is NOT the same `retry.RetryOnConflict` block as the marker stamp (that one runs BEFORE emission; this one runs AFTER, since the SpanID isn't known until `synthesizePlannerSpan` returns). Non-fatal on failure, matching the `MilestoneSpanEmittedUID`-patch-failure logging precedent immediately above.
4. Thread `thisSpanID`'s W3C string (via `otelai.FormatTraceparent(traceID, thisSpanID, true)`) into the `spawnReporterIfNeeded`/`BuildReporterJob` call immediately below (see Shared Patterns — Reporter Traceparent).

---

### `internal/controller/phase_controller.go` — `resolveProject` needs restructuring (new Milestone-surfacing return)

**Analog:** its own current form, lines 814-831 (full function, already read):
```go
// resolveProject walks Phase → Milestone → Project. Returns nil on failure.
func (r *PhaseReconciler) resolveProject(ctx context.Context, ph *tideprojectv1alpha3.Phase) *tideprojectv1alpha3.Project {
	if ph.Spec.MilestoneRef == "" {
		return nil
	}
	var ms tideprojectv1alpha3.Milestone
	if err := r.Get(ctx, client.ObjectKey{Namespace: ph.Namespace, Name: ph.Spec.MilestoneRef}, &ms); err != nil {
		return nil
	}
	if ms.Spec.ProjectRef == "" {
		return nil
	}
	var p tideprojectv1alpha3.Project
	if err := r.Get(ctx, client.ObjectKey{Namespace: ph.Namespace, Name: ms.Spec.ProjectRef}, &p); err != nil {
		return nil
	}
	return &p
}
```
`ms` (the Milestone — Phase's immediate parent, whose persisted span ID Phase needs for both dispatch-prep TRACEPARENT and completion-time real parenting) is fetched then **discarded**. RESEARCH.md's recommendation (Assumption A3, low risk either way): change the return shape to `(*Project, *Milestone)` (or add a second out-param) so both existing call sites (dispatch-prep at `phase_controller.go` and `handleJobCompletion`) get the Milestone object for free, avoiding a redundant `client.Get`. A separate wholly-new fetch is also acceptable if the planner prefers call-site isolation.

---

### `internal/controller/plan_controller.go` — `resolveProjectForPlan` needs a genuinely new Phase fetch

**Analog:** its own current form, lines 930-964 (full function, already read):
```go
// resolveProjectForPlan walks Plan → Phase → Milestone → Project.
func (r *PlanReconciler) resolveProjectForPlan(ctx context.Context, plan *tideprojectv1alpha3.Plan) *tideprojectv1alpha3.Project {
	// Fast path: if the Plan carries the tideproject.k8s/project label ...
	if projectName, ok := plan.Labels[owner.LabelProject]; ok && projectName != "" {
		var p tideprojectv1alpha3.Project
		if err := r.Get(ctx, client.ObjectKey{Namespace: plan.Namespace, Name: projectName}, &p); err == nil {
			return &p
		}
	}
	if plan.Spec.PhaseRef == "" {
		return nil
	}
	var ph tideprojectv1alpha3.Phase
	if err := r.Get(ctx, client.ObjectKey{Namespace: plan.Namespace, Name: plan.Spec.PhaseRef}, &ph); err != nil {
		return nil
	}
	// ... walks to Milestone, then Project, discarding ph and ms ...
	return &p
}
```
Unlike Phase, the common-case **label fast-path returns Project directly and never touches Phase at all** — there's no "fetch-and-discard" to restructure; a genuinely new `r.Get(ctx, client.ObjectKey{Namespace: plan.Namespace, Name: plan.Spec.PhaseRef}, &ph)` is needed at both Plan's dispatch-prep site and its `handleJobCompletion` site to read Phase's persisted span-ID field. `plan.Spec.PhaseRef` is already a plain string field — no schema change needed to read it.

---

### `internal/controller/task_controller.go` — net-new span-emission block + new Plan fetch

**Analog:** the four existing planner-level blocks above (closest structural analog: Plan's `resolveProjectForPlan`, since Task's `resolveProject` has the identical label-fast-path shape that skips the immediate parent — Plan — in the common case).

**BEFORE — `resolveProject`, lines 1124-1144 (full function, already read):**
```go
func (r *TaskReconciler) resolveProject(ctx context.Context, task *tideprojectv1alpha3.Task) (*tideprojectv1alpha3.Project, error) {
	// Fast path: PlanReconciler stamps tideproject.k8s/project=<name> on all Tasks.
	if projectName, ok := task.Labels[owner.LabelProject]; ok && projectName != "" {
		var project tideprojectv1alpha3.Project
		if err := r.Get(ctx, client.ObjectKey{Namespace: task.Namespace, Name: projectName}, &project); err == nil {
			return &project, nil
		}
	}
	// Owner-ref chain walk: Task→Plan→Phase→Milestone→Project (bounded depth 5).
	if parent, err := r.walkOwnerChainToProject(ctx, task); err == nil && parent != nil {
		return parent, nil
	}
	return nil, ErrParentUnresolved
}
```
Same asymmetry as Plan: the label fast-path never touches Plan. A new `r.Get(ctx, client.ObjectKey{Namespace: task.Namespace, Name: task.Spec.PlanRef}, &plan)` is needed — `task.Spec.PlanRef` is already a plain string field (confirmed used elsewhere in this file, e.g. line 1218, 1227).

**BEFORE — `handleJobCompletion`, lines 924-1122 (full function, already read in full — reproduced here only for the three terminal branches Pitfall 1 concerns; do not re-read):**

Branch 1 — `EnvelopeReadFailed` (lines 938-957), the ONLY branch reachable with `envReadOK=false`, and today it **returns before any span logic could run**:
```go
	out, err := r.Deps.EnvReader.ReadOut(ctx, string(project.UID), string(task.UID))
	if err != nil {
		patch := client.MergeFrom(task.DeepCopy())
		task.Status.Phase = tideprojectv1alpha3.LevelPhaseFailed
		meta.SetStatusCondition(&task.Status.Conditions, metav1.Condition{
			Type:               tideprojectv1alpha3.ConditionFailed,
			Status:             metav1.ConditionTrue,
			Reason:             "EnvelopeReadFailed",
			Message:            err.Error(),
			LastTransitionTime: metav1.Now(),
		})
		if patchErr := r.Status().Patch(ctx, task, patch); patchErr != nil {
			return ctrl.Result{}, patchErr
		}
		// Deliberate exclusion: EnvelopeReadFailed performs no budget.RollUpUsage
		// and no emitTaskMetrics ...
		return ctrl.Result{}, nil
	}
```
Branch 2 — `OutputValidationError` (lines 962-998) and Branch 3 — `OutputPathsViolation` (lines 999-1036) both run only AFTER a successful envelope read (`envReadOK` is implicitly true by the time either fires) and both currently `return ctrl.Result{}, nil` without any span call.

Branch 4 — "Standard result interpretation" (lines 1039-1121) is the single site the four planner levels' pattern maps onto directly — same `if out.ExitCode != 0 || ... { Failed } else { Succeeded }` shape as `synthesizePlannerSpan`'s own `isJobFailed` check, same terminal `budget.RollUpUsage`/`emitTaskMetrics` non-fatal-logging convention already used throughout this function (mirror that exact `if xErr := ...; xErr != nil { logger.Error(xErr, "... (non-fatal)", "task", task.Name) }` idiom for the new span call and the second status patch).

**Decision point flagged by RESEARCH.md (not silently resolved):** Pitfall 1 / Assumption A2 — Option A (single call site in Branch 4 only, matching the four planner levels literally) vs. Option B (also call in Branch 1, `EnvelopeReadFailed`, passing `envReadOK=false`, to satisfy D-07's "degraded envelope still emits a span" for Task's one observably-different failure class). RESEARCH.md recommends **Option B**; this is a planning decision, not a pattern-mapping one — flagging it here so the plan's task breakdown makes the choice explicit rather than letting it fall out of wherever code gets inserted.

**Marker field:** Task needs its own new `TaskSpanEmittedUID` (D-04) — copy the exact `if completedJob != nil && task.Status.TaskSpanEmittedUID != string(completedJob.UID) && plannerSpanResolvable(completedJob) { ... }` mark-then-emit block verbatim from any of the four existing controllers (Plan's is the closest receiver-type analog), swapping `tideprojectv1alpha3.Task` as the `latest := &...{}` type and `task` as the receiver var.

**Likely lint consequence:** the four existing completion handlers already carry `//nolint:gocyclo` for this exact reason (commit `9cae6bb`, cited in RESEARCH.md Pitfall 6); `task_controller.go`'s `handleJobCompletion` currently has only `//nolint:unparam` (line 923) — budget adding `//nolint:gocyclo` alongside it as an expected outcome, not a signal to refactor.

---

### `internal/controller/task_controller.go` — `createDispatchJob` (dispatch-prep TRACEPARENT injection site)

**Analog:** the four planner levels' own `podjob.BuildOptions{...}` construction (`milestone_controller.go:444`, `phase_controller.go:409`, `plan_controller.go:429`, `project_controller.go:1720`) — same struct-literal shape.

**BEFORE — lines 819-838 (already read in full):**
```go
	opts := podjob.BuildOptions{
		Kind:                 podjob.JobKindExecutor,
		Task:                 task,
		ParentObj:            task,
		Level:                "task",
		Project:              project,
		Attempt:              spec.attempt,
		SignedToken:          spec.token,
		EnvelopeInJSON:       spec.envInJSON,
		SubagentImage:        resolvedImage,
		AgentName:            agentName,
		AgentEmail:           agentEmail,
		CredproxyImage:       r.Deps.CredproxyImage,
		SecretUID:            secretUID,
		PVCName:              r.sharedPVCName(),
		ProjectUID:           string(project.UID),
		EstimatedCostCents:   r.Deps.ReserveEstimateCents,
		PricingOverridesJSON: r.Deps.PricingOverridesJSON,
	}
	job := podjob.BuildJobSpec(opts)
```
Required addition: fetch Plan (new `client.Get`, see `resolveProject` restructure above) at this dispatch-prep site too, read its persisted span-ID field, format it (`otelai.FormatTraceparent`), and add a `TraceParent: <formatted string>` field to this struct literal — same one-line-per-field convention already used for every other opt.

---

### `internal/dispatch/podjob/jobspec.go` — `BuildOptions` + `BuildJobSpec` (all five levels' dispatch Job)

**Analog:** the file's own existing conditional-env-append pattern for credproxy vars — the literal D-06 mirror target.

**BEFORE — lines 372-418 (subagent env construction, already read in full):**
```go
	// 7. Build the main subagent container. The subagent receives only the signed token
	// (not the raw provider secret) — D-C4 secret isolation contract.
	subagentEnv := []corev1.EnvVar{
		{Name: "TIDE_TASK_UID", Value: envelopeUID},
		{Name: "ANTHROPIC_API_KEY", Value: opts.SignedToken},
		{Name: "ANTHROPIC_AUTH_TOKEN", Value: opts.SignedToken},
		{Name: pkggit.EnvAgentName, Value: opts.AgentName},
		{Name: pkggit.EnvAgentEmail, Value: opts.AgentEmail},
	}
	subagentMounts := []corev1.VolumeMount{ /* ... */ }
	// D-02: stamp TIDE_PRICING_OVERRIDES_JSON only when the operator has configured
	// pricing overrides ...
	if opts.PricingOverridesJSON != "" {
		subagentEnv = append(subagentEnv, corev1.EnvVar{
			Name:  "TIDE_PRICING_OVERRIDES_JSON",
			Value: opts.PricingOverridesJSON,
		})
	}
	// cascade-13: only wire the localhost-credproxy plumbing (base-url + cert trust +
	// cert mount) when credproxy is actually injected ...
	if credproxyEnabled {
		subagentEnv = append(subagentEnv,
			corev1.EnvVar{Name: "ANTHROPIC_BASE_URL", Value: "https://127.0.0.1:8443"},
			corev1.EnvVar{Name: "SSL_CERT_FILE", Value: "/etc/tide/proxy/ca.crt"},
			corev1.EnvVar{Name: "NODE_EXTRA_CA_CERTS", Value: "/etc/tide/proxy/ca.crt"},
		)
		subagentMounts = append(subagentMounts, corev1.VolumeMount{ /* ... */ })
	}
```
**Exact template to copy for `TRACEPARENT` (D-06)** — add to `BuildOptions` struct (near `PricingOverridesJSON`, lines 147-152) a new field:
```go
// TraceParent is the W3C traceparent string for this level's own subagent
// dispatch Job, sourced from the IMMEDIATE PARENT's persisted span ID
// (Phase 43 PROP-01). Empty when there is genuinely no parent span yet
// available (Project's own dispatch is the sole such case — FormatTraceparent
// already returns "" for a zero/invalid parent, so no special-case branch
// is needed at the call site).
TraceParent string
```
Then, mirroring the `PricingOverridesJSON` conditional exactly (not the `credproxyEnabled` one, since this is unconditional-when-present like pricing, not a feature-flag-gated bundle):
```go
	if opts.TraceParent != "" {
		subagentEnv = append(subagentEnv, corev1.EnvVar{
			Name:  "TRACEPARENT",
			Value: opts.TraceParent,
		})
	}
```

---

### `internal/controller/dispatch_helpers.go` — `spawnReporterIfNeeded`

**Analog:** itself, lines 93-133 (full function, already read).

**BEFORE (full function body)** — already reproduced in `<execution_flow>` step above; signature:
```go
func spawnReporterIfNeeded(
	ctx context.Context,
	c client.Client,
	scheme *runtime.Scheme,
	parent metav1.Object,
	project *tideprojectv1alpha3.Project,
	parentKind string,
	reporterImage string,
	pvcName string,
) (bool, error) {
	...
	reporterJob := BuildReporterJob(parent, project, pvcName, string(parent.GetUID()), parentKind,
		ReporterOptions{ReporterImage: reporterImage}, scheme)
	...
}
```
**Required change:** add a `traceParent string` parameter, threaded into `ReporterOptions{ReporterImage: reporterImage, TraceParent: traceParent}`.

**Call sites needing the new arg** — only 2 of the 4 planner levels call this shared helper; the other 2 build `ReporterOptions{ReporterImage: ...}` **inline**, not via this helper:

| Level | Call shape | Location |
|---|---|---|
| Milestone | via helper | `milestone_controller.go:608` — `spawnReporterIfNeeded(ctx, r.Client, r.Scheme, ms, project, "Milestone", r.Deps.ReporterImage, r.sharedPVCName())` |
| Phase | via helper | `phase_controller.go:545` — `spawnReporterIfNeeded(ctx, r.Client, r.Scheme, ph, project, "Phase", r.Deps.ReporterImage, r.sharedPVCName())` |
| Plan | **inline**, not via helper | `plan_controller.go:599-601` — `BuildReporterJob(plan, project, pvcName, string(plan.UID), "Plan", ReporterOptions{ReporterImage: r.Deps.ReporterImage}, r.Scheme)` |
| Project | **inline**, not via helper | `project_controller.go:1876-1877` — `BuildReporterJob(project, project, pvcName, string(project.UID), "Project", ReporterOptions{ReporterImage: r.Deps.ReporterImage}, r.Scheme)` |

All four sites need the new `TraceParent` value added to their respective `ReporterOptions{...}` literal (via the new helper param for Milestone/Phase, directly in the literal for Plan/Project) — do not assume all four route through `spawnReporterIfNeeded`.

---

### `internal/controller/reporter_jobspec.go` — `ReporterOptions` + `BuildReporterJob`

**Analog:** itself, lines 74-80 (`ReporterOptions` struct) and 121-197 (`BuildReporterJob`, Args construction shown at lines 130-137, already read in full).

**BEFORE:**
```go
type ReporterOptions struct {
	// ReporterImage is the image ref for the tide-reporter container.
	ReporterImage string
}
```
```go
	args := []string{
		"--workspace=/workspace",
		"--project-uid=" + string(project.UID),
		"--task-uid=" + taskUID,
		"--parent-name=" + parent.GetName(),
		"--parent-namespace=" + parent.GetNamespace(),
		"--parent-kind=" + parentKind,
	}
```
**Required change (Pitfall 3 — Args, not Env, despite PROP-01's literal "env" wording):** this file sets **zero** `Env` entries on its container today — 100% Args-based via stdlib `flag`. Add `TraceParent string` to `ReporterOptions`, and append `"--traceparent=" + opts.TraceParent` to `args` (conditionally, only when non-empty, matching the file's other optional-arg precedent if any exists, or unconditionally if the file's convention is always-present flags — verify at implementation time which existing flags are conditional vs. always-set before deciding).

---

### `cmd/tide-reporter/main.go` — flag registration (Pitfall 4 — crash-loop risk)

**Analog:** itself, lines 79-101 (full `main` flag block, already read).

**BEFORE:**
```go
func main() {
	fs := flag.NewFlagSet("tide-reporter", flag.ExitOnError)
	workspace := fs.String("workspace", "/workspace", "...")
	projectUID := fs.String("project-uid", "", "...")
	taskUID := fs.String("task-uid", "", "...")
	parentName := fs.String("parent-name", "", "...")
	parentNamespace := fs.String("parent-namespace", "", "...")
	parentKind := fs.String("parent-kind", "", "...")

	if err := fs.Parse(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "tide-reporter: flag parse: %v\n", err)
		os.Exit(exitInvariant)
	}

	cfg := reporterConfig{
		Workspace:       *workspace,
		ProjectUID:      *projectUID,
		TaskUID:         *taskUID,
		ParentName:      *parentName,
		ParentNamespace: *parentNamespace,
		ParentKind:      *parentKind,
	}
	...
}
```
`flag.NewFlagSet(..., flag.ExitOnError)` means an unregistered `--traceparent` Arg (added by `BuildReporterJob` above) crash-loops every reporter Job in the cluster the moment this ships, UNLESS this file registers it in the SAME commit — even though nothing consumes the value until Phase 44. Add:
```go
traceParent := fs.String("traceparent", "", "W3C traceparent for this level's own span (consumed starting Phase 44)")
```
and capture it into `reporterConfig` (add a `TraceParent string` field to the struct at lines 60-67, and `TraceParent: *traceParent` to the literal at lines 94-101) — even though `cfg.TraceParent` has no reader yet this phase.

---

### `api/v1alpha3/{milestone,phase,plan,project}_types.go` — the new durable span-ID field

**Analog:** each file's own existing `{Level}SpanEmittedUID` field — same flat-string, same doc-comment shape, same `+optional` marker.

**BEFORE — `milestone_types.go` lines 50-79 (full `MilestoneStatus`, already read):**
```go
type MilestoneStatus struct {
	// +optional
	Phase string `json:"phase,omitempty"`

	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// MilestoneRolledUpUID is the name of this Milestone's planner Job whose Usage
	// was successfully rolled up into the Project budget. ...
	// +optional
	MilestoneRolledUpUID string `json:"milestoneRolledUpUID,omitempty"`

	// MilestoneSpanEmittedUID is the UID of the planner Job whose completion has
	// already had its dispatch span synthesized. ...
	// +optional
	MilestoneSpanEmittedUID string `json:"milestoneSpanEmittedUID,omitempty"`
}
```
**Field to add** (CONTEXT.md D-03/Claude's Discretion, RESEARCH.md's recommendation): `MilestoneTraceSpanID string` `json:"milestoneTraceSpanID,omitempty"` — flat string, `+optional`, storing only the hex `trace.SpanID.String()` value, never the TraceID (always re-derivable from `Project.UID` — avoid redundant storage). Mirror this exact addition (own field name per level) in `phase_types.go` (`PhaseStatus`, lines 48-74 region), `plan_types.go` (`PlanStatus`, lines 85-130 region), `project_types.go` (`ProjectStatus`, lines 490-529 region).

### `api/v1alpha3/task_types.go` — TWO net-new fields (span-ID field has no sibling to copy from within this file)

**Analog:** the four sibling `*_types.go` files' `{Level}SpanEmittedUID` pattern (cross-file, not in-file — `TaskStatus` currently has neither field family).

**BEFORE — `TaskStatus`, lines 147-170 (full struct, already read):**
```go
type TaskStatus struct {
	// +optional
	Phase string `json:"phase,omitempty"`

	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// +optional
	Attempt int `json:"attempt,omitempty"`

	// +optional
	ExitCode *int `json:"exitCode,omitempty"`

	// StartedAt is the wall-clock time the reconciler dispatched the Job for this
	// Task. ...
	// +optional
	StartedAt *metav1.Time `json:"startedAt,omitempty"`

	// +optional
	CompletedAt *metav1.Time `json:"completedAt,omitempty"`
}
```
Add both (copying the sibling files' doc-comment style, substituting "Task"/"executor Job" for "Milestone"/"planner Job"):
```go
	// TaskSpanEmittedUID is the UID of the executor Job whose completion has
	// already had its dispatch span synthesized. Mirrors {Level}SpanEmittedUID
	// on the four planner-level Status structs (Phase 43 TRACE-01).
	// +optional
	TaskSpanEmittedUID string `json:"taskSpanEmittedUID,omitempty"`

	// TaskTraceSpanID is the OTel SpanID of this Task's own synthesized span,
	// persisted for Phase 46's dashboard deep-link (OBS-04). Never store the
	// TraceID — always re-derivable from Project.UID via otelai.TraceIDFromUID.
	// +optional
	TaskTraceSpanID string `json:"taskTraceSpanID,omitempty"`
```

**Post-change requirement (all five `*_types.go`):** `make manifests generate` to regenerate `config/crd/bases/*.yaml` — this is a prerequisite for envtest to even compile/apply the new field (RESEARCH.md Wave 0 Gaps).

---

### `internal/controller/span_emission_test.go` — new "Task level" `Describe` block + parent-linkage assertions

**Analog:** the four existing per-level `Describe` blocks — verified structurally identical; excerpt below is the Milestone block's fixture setup (lines 120-150, already read), the template for a fifth.

**BEFORE (Milestone block, representative of all four):**
```go
var _ = Describe("SpanEmission — Milestone level", Label("envtest", "heavy"), func() {
	ctx := context.Background()

	const (
		seMSProjName = "span-emission-ms-proj"
		seMSName     = "span-emission-ms"
	)

	var (
		envReader *mapEnvReader
		exp       *tracetest.InMemoryExporter
		prevTP    oteltrace.TracerProvider
	)

	BeforeEach(func() {
		// 42-REVIEW WR-04: capture + swap the global TracerProvider FIRST —
		// before any failable fixture step. ...
		exp = tracetest.NewInMemoryExporter()
		prevTP = otel.GetTracerProvider()
		otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSyncer(exp)))

		spanEmissionProject(ctx, seMSProjName, "claude-test-model")
		envReader = newMapEnvReader()
	})

	AfterEach(func() {
		otel.SetTracerProvider(prevTP)
		ms := &tideprojectv1alpha3.Milestone{}
		if err := k8sClient.Get(ctx, types.NamespacedName{Name: seMSName, Namespace: "default"}, ms); err == nil {
			ms.Finalizers = nil
			_ = k8sClient.Update(ctx, ms)
			_ = k8sClient.Delete(ctx, ms)
		}
		cleanupSpanEmissionProject(ctx, seMSProjName)
	})
	// ... It() specs follow, using succeededPlannerJob/failedPlannerJob fixtures ...
```
**Required additions:**
1. A fifth `Describe("SpanEmission — Task level", Label("envtest", "heavy"), func() { ... })` block, same `BeforeEach`/`AfterEach` shape, swapping `Milestone` for `Task` and the completion-handler call for `TaskReconciler.handleJobCompletion`.
2. NEW assertion category added to ALL FIVE blocks (not present in any today — Phase 42 shipped independent-root spans):
   ```go
   spans := exp.GetSpans()
   Expect(spans).To(HaveLen(1))
   span := spans[0]
   Expect(span.SpanContext.TraceID().String()).To(Equal(expectedDeterministicTraceIDHex))
   Expect(span.Parent.SpanID()).To(Equal(parentLevelsPersistedSpanID))
   ```
3. `succeededPlannerJob`/`failedPlannerJob` fixture helpers (lines 89-119, already read at grep-time — not reproduced here since no change to their body is indicated, only new callers) are reused as-is for Task fixtures.

## Shared Patterns

### Mark-then-emit marker gating (applies to all 5 levels' completion handlers)
**Source:** identical block repeated in `milestone_controller.go:569-596`, `phase_controller.go:510-537`, `plan_controller.go:554-581`, `project_controller.go:1825-1852`.
**Apply to:** `task_controller.go`'s new `TaskSpanEmittedUID` block — copy the `retry.RetryOnConflict` + `client.MergeFromWithOptions(..., client.MergeFromWithOptimisticLock{})` + stamp-before-emit ordering verbatim, substituting the Task type/field names.

### Conditional env-var append (subagent Job)
**Source:** `internal/dispatch/podjob/jobspec.go` lines 396-401 (`PricingOverridesJSON`) — the exact shape for `TRACEPARENT` (unconditional-when-present, not a credproxy-style feature bundle).
**Apply to:** the single `BuildJobSpec` function — one new field, one new `if opts.TraceParent != "" { ... }` block, applies uniformly to all five levels' dispatch Jobs since they all funnel through this one function.

### Two-status-patch sequencing (marker BEFORE emission; span-ID AFTER emission)
**Source:** RESEARCH.md Pitfall 2, generalizing the existing single-patch precedent in all four controllers.
**Apply to:** all five levels' completion handlers — the new `{Level}TraceSpanID` patch is a SEPARATE `retry.RetryOnConflict` block, sequenced strictly after `synthesizePlannerSpan`/its Task equivalent returns `(spanID, true)`. Non-fatal on failure (log and continue), matching the existing `MilestoneSpanEmittedUID marker patch failed (non-fatal)` logging idiom already used at every site.

### Reporter Args convention (not Env)
**Source:** `internal/controller/reporter_jobspec.go` lines 130-137 — 100% Args-based, zero `Env` entries in this file today.
**Apply to:** the new `--traceparent=<value>` addition — append to `args`, do NOT introduce a new `corev1.EnvVar`/`Env` field on the reporter container (Pitfall 3 — departs from D-06's literal "env" wording, which RESEARCH.md scopes correctly to `jobspec.go` only).

### `otelai` primitives (zero-callsite-until-now helpers from Phase 42)
**Source:** `pkg/otelai/tracecontext.go` (full file, 97 lines, already read) — `TraceIDFromUID(uid string) (trace.TraceID, error)`, `FormatTraceparent(traceID, spanID, sampled) string`, `ExtractRemoteParent(ctx, traceparent) context.Context`.
**Apply to:** every completion handler (`TraceIDFromUID` + `FormatTraceparent`) and, per RESEARCH.md, no production call site needs `ExtractRemoteParent` this phase (that's the future consumer side, Phase 44/45) — do not add a speculative call to it.

## No Analog Found

None — every file this phase modifies has either a direct in-file precedent (the credproxy/pricing conditional-env blocks, the existing four-level completion-handler boilerplate) or a clear cross-file sibling analog (the four `*_types.go` files for Task's new fields). This is a pure retrofit-and-extend phase; RESEARCH.md's own "Don't Hand-Roll" table confirms no new OTel-API-layer code is needed either.

## Metadata

**Analog search scope:** `internal/controller/`, `internal/dispatch/podjob/`, `pkg/otelai/`, `api/v1alpha3/`, `cmd/tide-reporter/` — the exact file set RESEARCH.md's "Recommended File-Level Changes" enumerated; no broader repo sweep was needed since every touched file already contains its own closest analog (self-modify) or a byte-identical sibling in the same directory.
**Files scanned (read in full or targeted-range):** `span_emission.go` (full, 165 lines), `pkg/otelai/tracecontext.go` (full, 97 lines), `milestone_controller.go` (lines 540-605), `phase_controller.go` (lines 480-540, 800-845), `plan_controller.go` (lines 525-605, 925-965), `project_controller.go` (lines 1790-1855), `task_controller.go` (lines 700-1145, plus grep-located function index), `dispatch_helpers.go` (lines 85-135), `reporter_jobspec.go` (lines 60-205), `internal/dispatch/podjob/jobspec.go` (lines 82-232, 330-420), `cmd/tide-reporter/main.go` (lines 60-110), `api/v1alpha3/milestone_types.go` (lines 48-80), `api/v1alpha3/task_types.go` (lines 147-177), `span_emission_test.go` (lines 120-160, plus grep index of all 4 Describe blocks).
**Pattern extraction date:** 2026-07-16
