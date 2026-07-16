# Phase 43: Task-Level Parity + Trace-Context Propagation - Research

**Researched:** 2026-07-16
**Domain:** OpenTelemetry Go SDK trace-context mechanics (deterministic TraceID + remote-parent SpanContext threading) applied to TIDE's stateless, retroactive, K8s-controller dispatch model
**Confidence:** HIGH — every claim below was checked against the actual code on `main` (not this worktree's stale branch — see the CRITICAL environment finding first) and against the OpenTelemetry Go SDK source via Context7. One implementation-shape decision (Task's envelope-read-hard-failure span coverage) is a genuine open question, flagged explicitly, not silently resolved.

## CRITICAL ENVIRONMENT FINDING — read this before anything else

**This worktree (`worktree-phase-43-discuss`, branched at `daff39c`) does NOT contain Phase 42's code.** It branched before Phase 42 was executed and is **45 commits behind `main`**, which has Phase 42 fully complete (`main` HEAD `97644b4`, "docs(phase-42): add security threat verification"). Verified:

```
git merge-base --is-ancestor b4b15f2 HEAD   # phase-42 commit → NOT ANCESTOR in this worktree
git rev-list --left-right --count HEAD...main   # → 2 ahead, 45 behind
```

None of the files 43-CONTEXT.md cites (`pkg/otelai/tracecontext.go`, `internal/controller/span_emission.go`, the four `*SpanEmittedUID` marker fields, etc.) exist in this worktree's tree at all — `find . -iname "span_emission*.go"` returns nothing here. Every code citation in this research document was verified by reading `main`'s tree directly (`git show main:<path>`), not this worktree's working copy.

**Action required before planning/execution proceeds:** this worktree must be synced with `main` (merge or rebase) before Phase 43 work starts, or planning/execution must happen directly against `main`. Otherwise the planner will generate a plan whose prerequisite files don't exist in the execution environment, and the executor will hit missing-symbol compile errors on line 1. This is exactly the "worktree cwd-drift" class of failure the project's own CLAUDE.md calls out — confirm this is resolved (worktree rebased, or a fresh worktree cut from current `main`) as the literal first step of the plan, before Task 1.

All line numbers, signatures, and code below are cited **against `main`** and were independently re-verified (not merely copied from 43-CONTEXT.md) — they matched 43-CONTEXT.md's citations almost exactly (a few line numbers were off by 0-3 lines, within normal drift), confirming 43-CONTEXT.md itself was authored against a correctly-synced tree. The problem is this execution environment, not the prior research.

<user_constraints>
## User Constraints (from CONTEXT.md)

### Locked Decisions

- **D-00 (scope correction):** ARCHITECTURE.md's Suggested Build Order (steps 2-4) reads as if Phase 42 already added `Status.Trace.SpanID` and Phase 43 only extends propagation to Task. This is drift — Phase 42 added none of it. Phase 43 must MODIFY all four existing `synthesizePlannerSpan` call sites for real parenting, in addition to adding Task's.
- **D-01 (parent-child linking mechanism):** Follow ARCHITECTURE.md Pattern 2: a child level's dispatch/completion handler reads its **parent's** persisted span-ID field, builds a remote `trace.SpanContext` via `trace.NewSpanContext(...)` + `trace.ContextWithSpanContext`, and passes that context into `tracer.Start()`. TraceID for every level is `otelai.TraceIDFromUID(string(project.UID))`.
- **D-02:** Project's span has no parent above it — stays a root span, but anchored to the deterministic TraceID rather than an arbitrary one.
- **D-03:** New field is additive on **all five** CRDs' `.status`, including Task (Task is a leaf w.r.t. CRD children, but Phase 46's dashboard deep-link needs its span ID too).
- **D-04:** New field is **separate from** the existing `{Level}SpanEmittedUID` markers — do not fold span-identity storage into the idempotency-marker field. Task needs its own new `TaskSpanEmittedUID` mirroring the other four.
- **Claude's Discretion (field shape):** prefer a flat `{Level}TraceSpanID string` field over a nested `Status.Trace *TraceStatus{SpanID string}`, matching the existing flat `{Level}SpanEmittedUID` style — either acceptable, planner's call.
- **D-05:** `TRACEPARENT` injection applies to all five levels' subagent dispatch Jobs, plus the four existing reporter Jobs (Milestone/Phase/Plan/Project — Task has no reporter Job yet).
- **D-06:** Mirror the existing conditional-env-append pattern for credproxy vars in `jobspec.go` for the new `TRACEPARENT` var.
- **D-07:** Task-level span emission inherits Phase 42's D-01..D-04 policies verbatim: succeeded AND failed Task Jobs get spans; one span per Job attempt (gated by `TaskSpanEmittedUID` keyed by Job UID); failure detail via span status `Error` + `FailureDetail` attributes; degraded envelope (`envReadOK=false`) still emits a span with usage attributes absent + `tide.envelope.degraded` marker.
- **D-08 (signature drift):** `otelai.TraceIDFromUID` is `TraceIDFromUID(uid string) (trace.TraceID, error)` — NOT `TraceIDFromUID(uid types.UID) trace.TraceID` as ARCHITECTURE.md's sketch shows. Confirmed correct against `main` — see Existing Code section below.

### Claude's Discretion

- Exact field name/shape for the new durable span-ID field (flat string preferred, nested struct acceptable).

### Deferred Ideas (OUT OF SCOPE)

- `2026-07-12-project-dispatch-missing-failurehalt-gate.md` / `2026-07-12-task-dispatch-gate-order-divergence.md` (W-2 candidates) — dispatch-gate ordering, distinct from span emission, next-milestone candidates.
- `cache-f1-direct-sdk-cross-pod-caching.md` — no overlap.
- `2026-07-03-signed-commits-verified-badge.md` — no overlap.
- Also explicitly out of THIS phase (per phase boundary): LLM message-array spans / `events.jsonl` parsing (Phase 44 MSG-01..03), Task's own trace-only reporter Job + TRACE-03's reporter TracerProvider/flush discipline (Phase 44), the self-instrumenting adapter seam (Phase 45 ADAPT-01), sampler/session.id/metadata enrichment + dashboard deep-link (Phase 46 OBS-01..04), self-hosted Phoenix install + live proof (Phase 47).

</user_constraints>

<phase_requirements>
## Phase Requirements

| ID | Description | Research Support |
|----|-------------|------------------|
| TRACE-01 | Manager emits retroactive AGENT spans at all five level-completion sites (Task currently has zero) | Confirmed `task_controller.go` has zero otel imports/calls today. `synthesizePlannerSpan` (span_emission.go) is directly reusable for Task; see Architecture Patterns and the Task control-flow gap (Pitfall: Task's Multi-Branch Completion). |
| TRACE-02 | One run renders as ONE trace: deterministic TraceID from Project UID, every level's span parents under its parent's span | Resolved the OTel-mechanics question of how a deterministic root TraceID coexists with "no custom IDGenerator" — see Architecture Pattern 2 (verified via Context7 against SDK source). Concrete per-level parent-fetch gap identified — see "The Immediate-Parent-Fetch Asymmetry" pitfall. |
| PROP-01 | W3C traceparent injected into subagent Job env AND reporter Job env/args at dispatch time | Confirmed `jobspec.go` uses `corev1.EnvVar` (D-06's mirror target); confirmed `reporter_jobspec.go`'s container sets ZERO env vars today — 100% Args-based. Recommend Args for reporter, not env, despite PROP-01's literal wording — see Pitfall "reporter Job traceparent mechanism." Also surfaced a reporter-crash-loop risk if `cmd/tide-reporter/main.go`'s flag set isn't updated — see Pitfall "undeclared flag breaks the reporter Job." |
| PROP-02 | Per-level trace/span IDs persist in `.status` | Confirmed all five `*_types.go` Status structs lack this field today (matches D-03). Confirmed the field must be written in a SEPARATE, second status patch AFTER span emission (span ID isn't known until `tracer.Start()` returns) — distinct from the existing pre-emission marker-stamp patch. See "Two Status Patches, Not One" pitfall. |

</phase_requirements>

## Summary

Phase 43 is real trace-context plumbing, not attribute work — Phase 42 already proved the attribute/idempotency/failure-coverage patterns (`synthesizePlannerSpan`, the mark-then-emit marker convention, `plannerSpanResolvable`). What Phase 43 adds is: (1) a net-new span-emission call site in `TaskReconciler.handleJobCompletion` mirroring the four existing ones, (2) retrofitting all five levels' retroactive spans to parent under their real ancestor instead of standing alone as independent roots, and (3) injecting inert-today, load-bearing-later `TRACEPARENT` data into every dispatch Job and the four existing reporter Jobs.

The one genuinely tricky design question — how a *deterministic* root TraceID for Project's span coexists with the project's own "no custom IDGenerator" constraint, given that a level's parent span doesn't exist until *after* completion — is fully resolved by this research: the OTel Go SDK's `tracer.newSpan()` inherits the incoming context's TraceID whenever `psc.TraceID().IsValid()` is true, **independent of whether the SpanID portion is valid**. This means constructing `trace.SpanContext{TraceID: <deterministic>, SpanID: <parent's real ID, or zero for Project>}` and passing it via `trace.ContextWithSpanContext` into `tracer.Start()` produces exactly the desired behavior uniformly across all five levels — real parenting where a parent exists, and a clean deterministic-TraceID root where it doesn't (Project) — with zero custom IDGenerator code. This was verified directly against the OTel Go SDK source (Context7), not assumed from the architecture doc's prose.

The second material finding is an asymmetry the milestone research didn't fully spell out: `synthesizePlannerSpan` currently returns only `bool` — it discards the newly-minted span's own `SpanID` after calling `span.End()`. The retrofit needs that value on the **output** side (to persist into `.status` and to hand to `spawnReporterIfNeeded`), not just a new parent parameter on the **input** side. Separately, three of the four *existing* dispatch/completion call sites (Phase, Plan, Task) resolve only the top-level `Project` today, not their own immediate parent CRD — the object whose span ID they actually need. Milestone gets this "for free" (its immediate parent IS Project, already fully resolved at both its call sites); Phase/Plan/Task each need this fetched at **two** separate points (dispatch-time, for their own subagent Job's TRACEPARENT; completion-time, for their own span's real parent context) — six new or restructured `client.Get` calls total, not the "already happening" pattern CONTEXT.md's code_context implied.

**Primary recommendation:** Implement Architecture Pattern 2 exactly as designed (deterministic TraceID + zero-or-real parent SpanID, no custom IDGenerator), but budget explicit tasks for: (a) `synthesizePlannerSpan`'s two-sided signature change (accept parent SpanID in, return own SpanID out), (b) the three levels' new immediate-parent fetches (cheapest for Phase, which already fetches-and-discards Milestone internally; a new dedicated Get for Plan and Task, whose label fast-paths skip the intermediate object), (c) a second, separately-retried status patch per level for the durable span-ID field, and (d) a one-line flag-registration fix in `cmd/tide-reporter/main.go` so adding `--traceparent` to the reporter Job's Args doesn't crash-loop it on an unrecognized flag.

## Architectural Responsibility Map

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|-------------|----------------|-----------|
| Retroactive span synthesis (TRACE-01) | Manager reconciler (API/Backend equivalent — the K8s controller) | — | Spans are created/closed within a single `handleJobCompletion` call using `completedJob.Status` timestamps; no external process, no live/held-open span. |
| Deterministic TraceID + parent-context threading (TRACE-02) | Manager reconciler | CRD `.status` (Database/Storage tier) | The Go-side context-building is reconciler logic; the parent's span ID it reads comes from the durable status field, not from any live in-process state. |
| TRACEPARENT env/arg injection (PROP-01) | Manager reconciler (dispatch-time, writes Job spec) | Dispatched pod (Subagent/Reporter — inert consumer this phase) | The manager is the sole author of Job specs; the pod-side consumer doesn't exist until Phase 45 (self-instrumenting runtime) / Phase 44 (reporter's own emission). |
| Durable span-ID persistence (PROP-02) | CRD `.status` (Database/Storage tier) | Manager reconciler (writer) | Matches TIDE's existing "level boundary = durable artifact" philosophy — no external DB, no in-memory carrier across reconciles. |

## Standard Stack

No new external packages this phase. `go.opentelemetry.io/otel{,/trace,/sdk}` v1.43.0 and `github.com/Arize-ai/openinference/go/openinference-semantic-conventions` v0.1.1 are already pinned in `go.mod` (added Phase 42; confirmed via `git show main:go.mod`). Phase 43 is pure composition of already-vendored `go.opentelemetry.io/otel/{trace,propagation}` primitives (`trace.NewSpanContext`, `trace.ContextWithSpanContext`, `propagation.TraceContext{}`) that `pkg/otelai/tracecontext.go` already wraps — no `go get`, no `go.mod` diff expected.

**Package Legitimacy Audit:** N/A — this phase installs no new packages. Skipping the audit per the protocol's own scope ("required whenever this phase installs external packages").

## Architecture Patterns

### System Architecture Diagram

```
                         ┌─────────────────────────────────────────────┐
                         │        Manager Pod (controller-runtime)      │
                         │                                                │
  Job completes  ───────▶│  {Milestone,Phase,Plan,Project,Task}         │
  (K8s watch event)      │  Reconciler.handleJobCompletion(ctx,obj,job) │
                         │                                                │
                         │   1. Read PARENT's persisted span-ID          │
                         │      (client.Get on immediate parent CRD —    │
                         │       NEW fetch for Phase/Plan/Task; already   │
                         │       in-scope for Milestone)                 │
                         │           │                                    │
                         │           ▼                                    │
                         │   2. traceID := TraceIDFromUID(Project.UID)   │
                         │      sc := SpanContext{TraceID, parentSpanID  │
                         │             (zero for Project)}                │
                         │      ctx := ContextWithSpanContext(ctx, sc)   │
                         │           │                                    │
                         │           ▼                                    │
                         │   3. synthesizePlannerSpan(ctx, ...)          │
                         │      → tracer.Start (inherits traceID,        │
                         │        mints fresh SpanID, parents on sc)     │
                         │      → SetAttributes / SetStatus / End        │
                         │      → returns (thisLevelSpanID, emitted)     │
                         │           │                                    │
                         │           ▼                                    │
                         │   4. Patch .status.{level}TraceSpanID          │
                         │      = thisLevelSpanID  (2nd status patch,     │
                         │        AFTER emission — separately retried)    │
                         │           │                                    │
                         │           ▼                                    │
                         │   5. spawnReporterIfNeeded(                   │
                         │        traceParent = FormatTraceparent(        │
                         │          traceID, thisLevelSpanID) )          │
                         └───────────┬─────────────────────┬─────────────┘
                                     │                       │
                     TRACEPARENT=<parent's real span>        │ traceparent CLI arg
                     injected into NEXT level's own           │ = THIS level's own
                     subagent Job env (dispatch-prep,          │ span (reporter's future
                     a LATER reconcile — reads the             │ work nests under THIS
                     persisted field written in step 4)        │ level, not the grandparent)
                                     │                       │
                                     ▼                       ▼
                     ┌───────────────────────┐   ┌─────────────────────────┐
                     │ Subagent dispatch Job  │   │ tide-reporter Job        │
                     │ (inert consumer today; │   │ (inert consumer today;   │
                     │  Phase 45 self-instr.  │   │  Phase 44 adds its own   │
                     │  runtime reads this)   │   │  TracerProvider + emits) │
                     └───────────────────────┘   └─────────────────────────┘
```

A reader can trace one dispatch's full lifecycle: Job completes → parent's span ID read from durable status → deterministic TraceID + real-or-zero parent SpanContext built → this level's span synthesized and its own ID captured → ID persisted (second patch) → both the next level's dispatch and this level's reporter get a traceparent string derived from data now durable, never from in-memory state carried across reconciles.

### Recommended File-Level Changes (no new files)

```
pkg/otelai/tracecontext.go          # UNCHANGED — already correct (D-08), zero call sites → first callers land here
internal/controller/
├── span_emission.go                # MODIFY: synthesizePlannerSpan signature — accept parent
│                                    #   trace.SpanID in, return (trace.SpanID, bool) out
├── dispatch_helpers.go             # MODIFY: spawnReporterIfNeeded — + traceParent string param
├── reporter_jobspec.go             # MODIFY: ReporterOptions + BuildReporterJob — + traceparent Arg
├── {milestone,phase,plan}_controller.go, project_controller.go
│                                    # MODIFY: both the dispatch-prep site (BuildOptions — inject
│                                    #   TRACEPARENT for THIS level's own subagent Job, sourced from
│                                    #   the IMMEDIATE PARENT's persisted span ID) and the
│                                    #   handleJobCompletion site (parent-context threading +
│                                    #   2nd status patch for the new field)
└── task_controller.go               # MODIFY: prepareDispatch/createDispatchJob (new Plan fetch +
                                     #   TRACEPARENT env) and handleJobCompletion (net-new span
                                     #   emission block, TaskSpanEmittedUID marker, 2nd status patch)
internal/dispatch/podjob/jobspec.go  # MODIFY: BuildOptions + TraceParent string field;
                                     #   BuildJobSpec appends TRACEPARENT env conditionally
                                     #   (empty string → omitted, mirrors credproxy pattern)
cmd/tide-reporter/main.go           # MODIFY: register --traceparent in the flag.NewFlagSet
                                     #   (see Pitfall — required even though nothing consumes
                                     #   the value yet, or flag.Parse errors on the new Arg)
api/v1alpha3/{milestone,phase,plan,project,task}_types.go
                                     # MODIFY: additive {Level}TraceSpanID string field (or
                                     #   nested, planner's call) + TaskSpanEmittedUID (task only)
config/crd/bases/*.yaml             # REGENERATE via `make manifests generate`
```

### Pattern 1: Retroactive span synthesis (already implemented, unchanged this phase)

Already shipped in `span_emission.go`'s `synthesizePlannerSpan` (lines 115-164 on `main`) and the four call sites. No behavior change to *what* gets synthesized — only *the parent context it's synthesized under* and *what it returns* change this phase. See Phase 42's own code for the full attribute/failure/degraded-envelope logic — it is directly reused, not reimplemented, for Task.

### Pattern 2: Deterministic TraceID + real-or-zero parent SpanID (verified against OTel SDK source, not just the architecture doc's prose)

**What:** `main`'s `pkg/otelai/tracecontext.go` (Phase 42, unit-tested, zero call sites today) already provides everything needed:

```go
// Source: main, pkg/otelai/tracecontext.go (unchanged, D-08 signature confirmed)
func TraceIDFromUID(uid string) (trace.TraceID, error) {
    hex := strings.ToLower(strings.ReplaceAll(uid, "-", ""))
    return trace.TraceIDFromHex(hex)
}
```

The open question this research resolved: **how can Project's own span get the deterministic TraceID as a "root" span (D-02) when `tracer.Start()`'s only lever is the incoming `ctx`'s SpanContext, and a fully-invalid SpanContext (no parent at all) causes the SDK to mint a *brand-new random* TraceID, not honor a caller-supplied one?**

Verified directly against `go.opentelemetry.io/otel/sdk/trace/tracer.go`'s `newSpan()` (Context7, HIGH confidence, official source):

```go
// Source: github.com/open-telemetry/opentelemetry-go/blob/main/sdk/trace/tracer.go
var tid trace.TraceID
var sid trace.SpanID
if !psc.TraceID().IsValid() {
    tid, sid = tr.provider.idGenerator.NewIDs(ctx)
} else {
    tid = psc.TraceID()
    sid = tr.provider.idGenerator.NewSpanID(ctx, tid)
}
```

The check is `psc.TraceID().IsValid()` — **only the TraceID half**, not `psc.IsValid()` (which requires both TraceID AND SpanID valid). This means a `trace.SpanContext{TraceID: <deterministic, valid>, SpanID: trace.SpanID{}  /* zero, invalid */}` still causes the SDK to inherit the deterministic TraceID while minting a fresh random SpanID for the new span — exactly the desired root-span behavior for Project, with **zero custom IDGenerator**, using the *same* code path as every other level:

```go
// One code path for ALL five levels — parentSpanID is trace.SpanID{} (zero) for Project,
// the real persisted parent span ID for Milestone/Phase/Plan/Task.
traceID, err := otelai.TraceIDFromUID(string(project.UID))
if err != nil {
    // Non-fatal: log and skip emission for this attempt (see Pitfalls — TraceIDFromUID error).
}
sc := trace.NewSpanContext(trace.SpanContextConfig{
    TraceID: traceID, SpanID: parentSpanID, TraceFlags: trace.FlagsSampled, Remote: true,
})
ctx := trace.ContextWithSpanContext(ctx, sc)
_, span := tracer.Start(ctx, spanName, trace.WithTimestamp(startTime))
// ... SetAttributes / SetStatus as today ...
span.End(trace.WithTimestamp(endTime))
thisLevelSpanID := span.SpanContext().SpanID()  // real value, now known — return it, persist it
```

**Why `Remote: true` even for same-process synthesis:** this is reconstructed from durable `.status` data written by a *different, earlier* reconcile invocation (possibly a different manager replica after a restart) — not an in-process context handoff. Marking it `Remote: true` is the semantically correct W3C convention (matches ARCHITECTURE.md's own Pattern 2 sketch) even though no network hop is literally involved for the manager-internal parenting case.

**Confidence:** HIGH — the SDK behavior was read directly from source via Context7 (`open-telemetry/opentelemetry-go` `sdk/trace/tracer.go`), not inferred from the architecture doc's prose (which asserted the conclusion — "no custom IDGenerator needed" — but did not cite the specific SDK check that makes it true).

### Pattern 3: `synthesizePlannerSpan` needs a two-sided signature change — input parent AND output SpanID

43-CONTEXT.md's D-01/code_context flags the **input** side ("needs a parenting-aware parameter... threaded in") but does not mention the **output** side. Currently (`main`, `span_emission.go`):

```go
func synthesizePlannerSpan(
    ctx context.Context, level string, project *tideprojectv1alpha3.Project,
    helmDefaults ProviderDefaults, completedJob *batchv1.Job,
    out pkgdispatch.EnvelopeOut, envReadOK bool,
) bool {
    // ...
    _, span := tracer.Start(ctx, spanName, trace.WithTimestamp(startTime))
    // ... attributes/status ...
    span.End(trace.WithTimestamp(endTime))
    return true   // <-- SpanID is discarded here
}
```

This must become something like `func synthesizePlannerSpan(..., parentSpanID trace.SpanID) (trace.SpanID, bool)` — a caller needs the real, SDK-minted `span.SpanContext().SpanID()` for two downstream uses that don't exist in the current signature: (a) persisting it into the new `.status` field (PROP-02), and (b) passing it as `traceParent` into `spawnReporterIfNeeded` (so the reporter Job — even though it does nothing with the value yet — carries the correct W3C string for this level's own span, not the grandparent's). All five call sites (four existing + the new Task one) need updating for both sides of this signature change.

### Pattern 4: Two distinct "one hop" propagation targets — do not conflate them

The phrase "one hop at a time" (ARCHITECTURE.md Pattern 2, phase description) refers to **two different recipients with two different parent references**, confirmed by reading ARCHITECTURE.md's own Data Flow section closely plus the actual reporter-spawn call ordering in the completion handlers:

| Recipient | What traceparent it gets | Why | When it's known |
|-----------|--------------------------|-----|------------------|
| **This level's own reporter Job** (Milestone/Phase/Plan/Project only — Task has none yet) | **This level's own** just-synthesized span ID | The reporter's future work (Phase 44 message-array spans) should nest under *this* level's dispatch, not the grandparent | Already in-process, same `handleJobCompletion` call, right after `synthesizePlannerSpan` returns — no extra fetch |
| **The next level's own subagent dispatch Job** (e.g. Task's Job, dispatched by TaskReconciler) | **The immediate parent's** persisted span ID (e.g. Plan's) | A future self-instrumenting runtime inside that pod should nest under the dispatch chain it was launched from | Read from the parent's `.status` field at the CHILD's dispatch-prep time — a **separate, later** reconcile of a **different** reconciler |

Do not build one `traceParent` value and reuse it for both purposes — they are different span IDs in the general case (only coincide trivially at Project, which has no parent).

### The Immediate-Parent-Fetch Asymmetry (concrete, per-level gap — the most load-bearing finding in this document)

43-CONTEXT.md's D-01 states the parent's span ID is read "via a `client.Get` most handlers already perform for parent-ref resolution." Verified against `main` — this holds precisely for **one** of the five levels, not "most":

| Level | Immediate parent | Already resolved at dispatch-prep? | Already resolved at completion (`handleJobCompletion`)? | Fetch needed |
|-------|-------------------|--------------------------------------|-----------------------------------------------------------|---------------|
| **Milestone** | Project | YES — `project` fully resolved via `ms.Spec.ProjectRef` at the dispatch site (line ~395 area) | YES — same fetch pattern at line ~505 (`milestone_controller.go`) | **None.** Project is Milestone's immediate parent AND the object every existing call site already fully resolves for unrelated reasons (namespace, ProviderSecretRef). Free reuse. |
| **Phase** | Milestone | NO — dispatch-prep calls `project := r.resolveProject(ctx, ph)`, which walks Phase→Milestone→Project but **returns only `*Project`**, discarding the Milestone object it fetches internally (confirmed: `phase_controller.go:815-830`, `resolveProject` fetches `var ms tideprojectv1alpha3.Milestone` then only returns `&p` / Project) | Same — `handleJobCompletion` calls the identical `resolveProject` helper | **Cheap restructure, not a new API call**: `resolveProject` already fetches Milestone internally — change its return shape to also surface it (e.g. `(project *Project, milestone *Milestone)`), touching both call sites. |
| **Plan** | Phase | NO — `resolveProjectForPlan` has a **label fast-path** (`tideproject.k8s/project` label) that, in the common case, resolves straight to Project and never fetches Phase at all; only the slow owner-walk fallback transiently fetches-and-discards Phase | Same helper reused at completion | **Genuinely new `client.Get`** on Phase (via `plan.Spec.PhaseRef`, already a plain string field) — cannot reliably piggyback on the label fast-path, which by design skips the intermediate object. |
| **Task** | Plan | NO — `TaskReconciler.resolveProject` has the same label-fast-path shape (`tideproject.k8s/project`), skipping `task.Spec.PlanRef` entirely in the common case | Same | **Genuinely new `client.Get`** on Plan (via `task.Spec.PlanRef`) — same reasoning as Plan/Phase. |
| **Project** | none (root) | N/A | N/A | N/A — D-02, stays root, `parentSpanID = trace.SpanID{}`. |

**And this fetch is needed at *two separate points in time* for Phase/Plan/Task** (not once): once in dispatch-prep (to inject TRACEPARENT into that level's own subagent Job env, reading the parent's *already-durable* span ID from a prior reconcile), and again in `handleJobCompletion` (to correctly parent that level's own retroactive span). These are different reconciler invocations, potentially far apart in time — the fetched value cannot be cached or threaded between them. Budget **6 new/restructured fetches** total (2 each for Phase, Plan, Task), not "reuse an existing Get."

### Anti-Pattern (from ARCHITECTURE.md, reconfirmed): forcing exact SpanIDs via a custom IDGenerator

Already documented in `.planning/research/ARCHITECTURE.md` Anti-Pattern 2 and STATE.md's binding constraints — reconfirmed unnecessary by the Pattern 2 verification above. Do not reach for `sdktrace.WithIDGenerator`; the `psc.TraceID().IsValid()`-only check in the SDK's `newSpan()` makes it unneeded.

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| W3C traceparent string formatting | A hand-rolled `fmt.Sprintf("00-%s-%s-%s", ...)` | `otelai.FormatTraceparent` (already implemented, Phase 42) — wraps `propagation.TraceContext{}.Inject` on a `MapCarrier` | Already handles the empty-string-on-invalid-input case correctly (verified: returns `""` when either ID is zero/invalid) — exactly the behavior needed for Project's "no parent" case, for free. |
| Remote-parent context construction | Manual byte-slicing of a traceparent string | `otelai.ExtractRemoteParent` (Phase 42, wraps `propagation.TraceContext{}.Extract`) | Handles malformed input without panicking; not needed by the manager this phase (manager builds SpanContext directly from `.status` data, not from a traceparent string it received) but is the correct tool for the future self-instrumenting runtime side. |
| Deterministic 128-bit ID from a K8s UID | Custom UUID→byte-array parsing | `otelai.TraceIDFromUID` (Phase 42) | Already unit-tested for determinism, mixed-case normalization, and invalid-input rejection. |
| Forcing a specific SpanID to appear in an exported span | A custom `IDGenerator` | Pattern 2 above — construct the parent `SpanContext` with a real-or-zero SpanID and let the SDK mint fresh IDs naturally | STATE.md binding constraint; confirmed genuinely unneeded (see Pattern 2). |

**Key insight:** every OTel-mechanics primitive this phase needs was already built, unit-tested, and left with zero call sites in Phase 42 specifically so Phase 43 wouldn't need to invent anything new at the OTel-API layer. The actual work is K8s-controller plumbing (fetching the right parent object at the right time, threading two new function-signature sides, sequencing two status patches) — not OTel API surface.

## Common Pitfalls

### Pitfall 1: Task's multi-branch completion handler has no single natural span-emission call site

**What goes wrong:** All four existing planner levels have exactly ONE terminal path through `handleJobCompletion` after the envelope read (either `envReadOK=true` with real data, or `envReadOK=false` degraded — both continue to the same span-emission block). `TaskReconciler.handleJobCompletion` (`main`, lines 924-1122) has **three distinct early-return terminal branches** before reaching the "standard result interpretation" section: `EnvelopeReadFailed` (hard controller-side read error — returns immediately, `out`/`envReadOK` never populated, no budget rollup, no metrics), `OutputValidationError`, and `OutputPathsViolation` (both reached only after a *successful* envelope read). D-07 says Task must inherit "degraded envelope (`envReadOK=false`) still emits a span" verbatim — but Task's structure means that state is **only reachable via the `EnvelopeReadFailed` branch**, which currently returns before any span-emission logic could run at all.

**Why it happens:** Task's `handleJobCompletion` predates this phase's span requirement and was built with output-path validation Task alone needs (planner levels don't validate declared output paths) — its control flow diverged from the four planner levels' shape over several earlier phases, not from any Phase-43-specific decision.

**How to avoid — this is a genuine open decision, not a silently-resolved detail:**
- **Option A (single call site, mirrors the other four exactly):** place span emission only in the "standard result interpretation" path (after a successful envelope read). Simple, matches "same call-site pattern as the other four levels" literally. But a Task whose envelope literally cannot be read produces **zero span** — silently reintroducing a narrower version of exactly the gap TRACE-01 exists to close.
- **Option B (two call sites):** ALSO add a span-emission call in the `EnvelopeReadFailed` branch, passing `envReadOK=false` (matching D-07's degraded-envelope language, which is otherwise unobservable at Task level). Closes the gap fully; costs a second marker-check-and-emit block and very likely a `nolint:gocyclo` bump (see Pitfall 6).
- **Recommendation:** Option B — TRACE-01's stated goal is closing the "zero span emission" gap at Task level; leaving one whole failure class silently un-instrumented undercuts that goal. But flag this explicitly as a plan decision point, not something to resolve implicitly while writing the code.

### Pitfall 2: The durable span-ID persist is a second, separately-sequenced status patch — not folded into the existing marker stamp

**What goes wrong:** The existing mark-then-emit pattern (42-REVIEW WR-01) stamps `{Level}SpanEmittedUID` via `retry.RetryOnConflict` **before** calling `synthesizePlannerSpan` (so a crash between stamp and emission loses at most one span, never double-emits). But the new durable span-ID field's *value* (the real `trace.SpanID` from Pattern 3 above) is only known **after** `tracer.Start()` returns inside `synthesizePlannerSpan` — it cannot be included in that same pre-emission patch. A second, distinct `Status().Patch` call is required, sequenced strictly after emission succeeds.

**Why it happens:** the two writes answer genuinely different questions at genuinely different points in time (marker = "have I already emitted for this Job UID," written before emission to prevent duplicates; span-ID = "what identity did the span I just emitted get," written after, since it can't be known any earlier).

**How to avoid:** wrap the second patch in its own `retry.RetryOnConflict`, non-fatal on persistent failure (log and continue — mirrors the existing WR-03 precedent: "telemetry bookkeeping is subordinate to pipeline progression"). Document explicitly: if this second patch fails permanently, the span still exists in Phoenix (correctly attributed, standalone) but the *next* hop's parent-child edge is missing for that one level — a real but bounded degraded case, not a correctness bug (the shared deterministic TraceID means it still groups into the same trace in Phoenix, just not correctly nested).

**Warning signs:** a test that only checks the marker patch and never independently exercises "marker patch succeeds, span-ID patch fails" as a distinct failure mode.

### Pitfall 3: reporter Job traceparent mechanism — Args, not Env, despite PROP-01's literal wording

**What goes wrong:** PROP-01's text says "injected as data into both the subagent Job env and the reporter Job env." D-06 says to mirror `jobspec.go`'s conditional `corev1.EnvVar` append pattern. But `reporter_jobspec.go`'s `BuildReporterJob` (confirmed, `main`) sets **zero** `Env` entries on its container today — every piece of configuration (`--workspace`, `--project-uid`, `--task-uid`, `--parent-name`, `--parent-namespace`, `--parent-kind`) flows through `Args` via stdlib `flag`. Introducing a brand-new `Env` mechanism into this one file, just for `TRACEPARENT`, breaks the file's 100%-consistent existing convention for no real benefit.

**How to avoid:** add `--traceparent=<value>` to the reporter Job's `Args` (matching the file's established pattern exactly), not a new `Env` entry. Treat PROP-01/D-05's "env" wording as informal shorthand for "injected as data available to the process," not a literal `corev1.EnvVar` mandate — D-06's mirror-the-credproxy-pattern advice is correctly scoped to `jobspec.go` (which genuinely is Env-based) and should not be over-applied to `reporter_jobspec.go`.

### Pitfall 4: an undeclared `--traceparent` flag will crash-loop the reporter Job

**What goes wrong:** `cmd/tide-reporter/main.go` (`main`, confirmed) parses its Args with `flag.NewFlagSet("tide-reporter", flag.ExitOnError)` — stdlib `flag` rejects any Arg starting with `-`/`--` that wasn't registered via `fs.String`/`fs.Bool`/etc., and `flag.ExitOnError` means an unrecognized flag terminates the process (`os.Exit`) with a parse error. If `BuildReporterJob` starts appending `--traceparent=<value>` to `Args` without a corresponding `fs.String("traceparent", ...)` registration in `main.go`, every reporter Job in the cluster starts crash-looping the moment this phase ships — even though PROP-01's actual requirement ("present as data") doesn't require the reporter to *do* anything with the value yet (Phase 44 is where `cmd/tide-reporter` gains its first `otelinit.NewTracerProvider` call and starts consuming this).

**How to avoid:** add the flag declaration (`traceParent := fs.String("traceparent", "", "W3C traceparent for this level's own span (consumed starting Phase 44)")`) and capture it into `reporterConfig` in the SAME commit that starts populating `Args` with it — even though nothing reads `cfg.TraceParent` downstream yet this phase. A one-line addition with an outsized blast radius if missed.

**Warning signs:** any test that builds the reporter Job spec (`reporter_jobspec_test.go` if it exists) but never actually invokes `cmd/tide-reporter`'s `run()`/`runWithClient()` with the new Arg present — would pass while still shipping a crash-looping reporter.

### Pitfall 5: `TraceIDFromUID`'s error path needs an explicit, non-fatal policy

**What goes wrong:** `otelai.TraceIDFromUID(uid string) (trace.TraceID, error)` can fail (malformed UUID string) even though, per D-08's own framing, "K8s UIDs are always valid UUIDs in practice." If a caller doesn't decide what to do on error, this either panics (bad), silently emits a span with a zero-value TraceID (breaks TRACE-02's single-connected-trace guarantee for that one span), or falls back to a random TraceID (also breaks TRACE-02 for that span, less visibly).

**How to avoid:** treat it the same way `plannerSpanResolvable` already gates entry to `synthesizePlannerSpan` — log the error (non-fatal) and skip emission for that attempt entirely. This is consistent with the already-accepted "span loss preferred over incorrect/duplicated Phoenix data" precedent from Phase 42 (42-REVIEW WR-01's reasoning), extended to a new failure mode with the same philosophy. Given the practical near-impossibility of this path firing, this is a cheap, low-risk decision to lock down explicitly rather than leave implicit.

### Pitfall 6: Task's `handleJobCompletion` will likely need its own `nolint:gocyclo`

**What goes wrong:** all four existing planner-level completion handlers already carry `//nolint:gocyclo` (confirmed: `git show 9cae6bb` — "mark-then-emit restructure raised `handleProjectJobCompletion`'s cyclomatic complexity 31→33, past the gocyclo threshold... the milestone/phase/plan completion handlers carry `nolint:gocyclo` with the same justification"). Task's `handleJobCompletion` is *already* the most branch-heavy of the five (three early-return terminal paths vs. the planner levels' one) and currently has only `//nolint:unparam`. Adding span-emission logic — especially under Pitfall 1's Option B (two call sites) — will almost certainly push it over the same threshold.

**How to avoid:** budget the `nolint:gocyclo` addition as an expected, not surprising, outcome — matching the established codebase precedent ("a flat state machine of mutually-exclusive completion arms; splitting obscures the contract") rather than treating a lint failure here as a signal to awkwardly extract a sub-function that fragments the existing terminal-branch structure.

## Code Examples

### Building the parent SpanContext uniformly across all five levels

```go
// Source: synthesized from main's pkg/otelai/tracecontext.go + verified OTel SDK
// behavior (Context7, go.opentelemetry.io/otel/sdk/trace/tracer.go newSpan()).
// parentSpanID is trace.SpanID{} (zero value) for Project; the real persisted
// parent span ID (read from the immediate parent's new .status field) for
// Milestone/Phase/Plan/Task.
traceID, err := otelai.TraceIDFromUID(string(project.UID))
if err != nil {
    logger.Error(err, "TraceIDFromUID failed (non-fatal); skipping span emission for this attempt")
    return trace.SpanID{}, false
}
sc := trace.NewSpanContext(trace.SpanContextConfig{
    TraceID:    traceID,
    SpanID:     parentSpanID, // zero for Project — SDK still inherits traceID (verified)
    TraceFlags: trace.FlagsSampled,
    Remote:     true,
})
spanCtx := trace.ContextWithSpanContext(ctx, sc)
tracer := otel.Tracer("tide.dispatch")
_, span := tracer.Start(spanCtx, "tide.dispatch."+level, trace.WithTimestamp(startTime))
// ... existing SetAttributes/SetStatus logic, unchanged from Phase 42 ...
span.End(trace.WithTimestamp(endTime))
return span.SpanContext().SpanID(), true
```

### Conditional TRACEPARENT env append (mirrors the existing credproxy pattern exactly)

```go
// Source: mirrors main's internal/dispatch/podjob/jobspec.go:374-412 credproxy pattern.
// traceParent is "" when there is genuinely no parent span yet available
// (Project's own dispatch — the sole case; FormatTraceparent already returns ""
// for an invalid/zero SpanID input, so no special-case branch is needed here).
if opts.TraceParent != "" {
    subagentEnv = append(subagentEnv, corev1.EnvVar{
        Name: "TRACEPARENT", Value: opts.TraceParent,
    })
}
```

### Test assertion shape for TRACE-02 parenting (new — not exercised by Phase 42's suite)

```go
// Source: extends main's internal/controller/span_emission_test.go pattern
// (tracetest.InMemoryExporter + swapped global TracerProvider, WR-04 capture-
// before-any-failable-step ordering). Phase 42's tests only assert attribute
// content; TRACE-02 needs a NEW assertion category: parent linkage.
spans := exp.GetSpans()
Expect(spans).To(HaveLen(1))
span := spans[0]
Expect(span.SpanContext.TraceID().String()).To(Equal(expectedDeterministicTraceIDHex))
Expect(span.Parent.SpanID()).To(Equal(parentLevelsPersistedSpanID))
```

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|-------|---------|---------------|
| A1 | Recommending Args (not Env) for the reporter Job's traceparent, departing from D-06's literal env-var suggestion | Pitfall 3 | Low — if the planner instead adds an unused `Env` entry to match D-06 literally, it still works (just introduces the file's first Env usage for no functional reason); purely a style/consistency call, not a correctness risk. |
| A2 | Option B (two span-emission call sites in Task's `handleJobCompletion`) is the recommended resolution to Pitfall 1 | Pitfall 1 | Medium — if the planner picks Option A instead, a Task whose envelope read fails at the controller level (rare, but a real failure mode: PVC read errors, termination-message parse failures) produces no span at all; TRACE-01's "all five level-completion sites" success criterion would be technically true (a call site exists) but incomplete in coverage for one failure class. This is presented as an explicit decision, not silently assumed either way, but I(researcher) recommend B and it's worth confirming with the user/planner rather than defaulting silently to A. |
| A3 | Restructuring `PhaseReconciler.resolveProject`'s return signature (to also surface the already-fetched Milestone) is cheaper/preferred over adding a wholly separate fetch | The Immediate-Parent-Fetch Asymmetry | Low — either approach is correct; the restructure just avoids one redundant `client.Get` per Phase dispatch/completion. If the planner adds a separate fetch instead for isolation/blast-radius reasons, that's also fine, just slightly more API load. |

## Open Questions

1. **Task's envelope-read-hard-failure span coverage (Pitfall 1 / A2)**
   - What we know: Task's `handleJobCompletion` has a structurally different, more-branched control flow than the four planner levels; D-07 mandates degraded-envelope span coverage that is only reachable via one specific early-return branch.
   - What's unclear: whether the phase's acceptance bar (TRACE-01's "same span-emission call-site pattern") is satisfied by a single call site that skips the hard-failure branch, or requires genuinely universal terminal-state coverage.
   - Recommendation: resolve explicitly as a plan decision (Option B recommended) rather than let it fall out implicitly from wherever the span-emission code happens to get inserted.

2. **Field-name/shape for the durable span-ID field (D-03's explicit discretion)**
   - What we know: existing precedent is flat string fields (`MilestoneSpanEmittedUID string`); CONTEXT.md explicitly leaves this to the planner.
   - What's unclear: nothing blocking — purely a naming/shape call already flagged as discretionary upstream.
   - Recommendation: `{Level}TraceSpanID string` (e.g. `MilestoneTraceSpanID`, `TaskTraceSpanID`) for consistency with the existing flat-field style; store only the SpanID (hex string via `trace.SpanID.String()`), never re-persist the TraceID (always re-derivable from `Project.UID`, avoiding redundant storage).

## Environment Availability

Not applicable — this phase has no new external tool/service/runtime dependencies. It composes Go stdlib + already-vendored `go.opentelemetry.io/otel` packages (confirmed present in `go.mod` at v1.43.0) within the existing manager binary. No new Docker images, no new Helm values, no new CLI tools.

## Validation Architecture

### Test Framework

| Property | Value |
|----------|-------|
| Framework | Ginkgo v2.28 + Gomega (envtest, `Label("envtest","heavy")`) for handler-level tests; plain `testing.T` for pure-function tests |
| Config file | none dedicated — driven by `internal/controller/suite_test.go`'s existing `BeforeSuite`/envtest bootstrap (unchanged this phase) |
| Quick run command | `go test ./internal/controller/ -run 'TestSpanEndTime\|TestSynthesizePlannerSpan'` (pure-function tier, no envtest bootstrap needed) |
| Full suite command | `make test-heavy` (label-filtered `heavy` envtest specs, ~20m budget) or `make test-int-fast` for the broader Layer A tier |

### Phase Requirements → Test Map

| Req ID | Behavior | Test Type | Automated Command | File Exists? |
|--------|----------|-----------|-------------------|-------------|
| TRACE-01 | Task-level span emitted for succeeded/failed Job completion | envtest (Ginkgo, `Label("envtest","heavy")`) | `go test ./internal/controller/... -ginkgo.label-filter='heavy' -ginkgo.focus='SpanEmission — Task level'` | ❌ Wave 0 — new `Describe` block needed in `span_emission_test.go`, mirroring the four existing per-level blocks exactly (same `tracetest.InMemoryExporter` + WR-04 TracerProvider-swap-first ordering) |
| TRACE-02 | Span parents correctly under immediate ancestor's real span; TraceID is deterministic from Project.UID across all 5 levels | envtest (Ginkgo) | same file, new assertions per existing `Describe` block (`span.Parent.SpanID()`, `span.SpanContext.TraceID()`) | ❌ Wave 0 — NEW assertion category; Phase 42's existing specs never check parent linkage (Option A shipped independent roots) |
| PROP-01 | `TRACEPARENT` present in subagent Job env at dispatch time; `--traceparent` present in reporter Job Args | unit (plain `testing.T` or existing Ginkgo dispatch-prep specs) | `go test ./internal/dispatch/podjob/... -run TestBuildJobSpec` / `go test ./internal/controller/... -run TestBuildReporterJob` | ❌ Wave 0 — new assertions on existing `jobspec_test.go`/`reporter_jobspec_test.go` (confirm exact test file names during planning; not enumerated in this pass) |
| PROP-02 | `.status.{level}TraceSpanID` persists after emission, survives a second `client.Get` | envtest (Ginkgo) | same `span_emission_test.go` specs — assert on the re-fetched CRD's status field, not just the exporter's captured spans | ❌ Wave 0 — new assertion; requires the new CRD field to exist first (`api/v1alpha3/*_types.go` + `make manifests generate`) |

### Sampling Rate

- **Per task commit:** `go test ./internal/controller/ -run 'TestSpanEndTime|TestSynthesizePlannerSpan'` (pure-function tier, seconds) plus a targeted `-ginkgo.focus` run for the level under active work.
- **Per wave merge:** `make test-heavy` (full `heavy`-labeled envtest tier, ~20m).
- **Phase gate:** `make test-int-fast` (Layer A envtest, no kind needed — this phase touches no kind-tier fixtures) green before `/gsd:verify-work`.

### Wave 0 Gaps

- [ ] `internal/controller/span_emission_test.go` — new `Describe("SpanEmission — Task level", ...)` block, mirroring the four existing ones (same fixture helpers: `succeededPlannerJob`/`failedPlannerJob`-equivalent, `mapEnvReader`, WR-04 TracerProvider capture-before-failable-step ordering)
- [ ] New parent-linkage assertions added to ALL FIVE existing/new `Describe` blocks (`span.Parent.SpanID()` checks) — genuinely new test surface, not just a Task addition
- [ ] `api/v1alpha3/*_types.go` CRD field additions + `make manifests generate` regen — a prerequisite for the PROP-02 status-field assertions to even compile
- [ ] Confirm exact existing test file names for `jobspec.go`/`reporter_jobspec.go` unit coverage during planning (not verified in this pass — grep `internal/dispatch/podjob/*_test.go` and `internal/controller/reporter_jobspec*_test.go` at plan time)

## Security Domain

### Applicable ASVS Categories

| ASVS Category | Applies | Standard Control |
|---------------|---------|-------------------|
| V2 Authentication | No | No new auth surface this phase. |
| V3 Session Management | No | N/A. |
| V4 Access Control | No | No new RBAC surface — TRACEPARENT is manager-authored data injected into Jobs the manager already creates; no new API verbs. |
| V5 Input Validation | Marginal | `otelai.ExtractRemoteParent`/`propagation.TraceContext{}.Extract` already handle malformed traceparent strings without panicking (verified against OTel SDK source: `Extract` returns the original `ctx` unchanged on any parse failure) — relevant only to the future consumer side (Phase 45+), not this phase's manager-side code, which only *authors* traceparent strings via the already-tested `FormatTraceparent`. |
| V6 Cryptography | No | No cryptographic material involved — trace/span IDs are not secrets (they're already visible in exported spans and, per this phase, in Job env/args, which is consistent with OTel's standard threat model: trace IDs are not sensitive). |

### Known Threat Patterns for this stack

| Pattern | STRIDE | Standard Mitigation |
|---------|--------|-----------------------|
| Untrusted data flowing into a span attribute or Job env from an LLM-controlled process | Tampering / Information Disclosure | Not new this phase — the traceparent value injected into Jobs is entirely manager-derived (from `.status` fields the manager itself wrote), never sourced from subagent/reporter output. Consistent with the existing T-308 threat model (manager is the trust boundary, never the pod). |
| A malformed/adversarial `--traceparent` value causing the reporter binary to misbehave | Denial of Service | Not exploitable this phase specifically because the manager is the sole author of the value (see above) — but worth noting for Phase 44/45 when a self-instrumenting runtime or the reporter's own OTel init starts *parsing* this value: `ExtractRemoteParent`'s no-panic-on-malformed-input guarantee (already unit-tested, Phase 42) is the correct existing control; no new work needed here, just don't regress it. |

## Sources

### Primary (HIGH confidence)

- `github.com/open-telemetry/opentelemetry-go` (Context7 `/open-telemetry/opentelemetry-go`) — `sdk/trace/tracer.go` `newSpan()` TraceID-inheritance-vs-SpanID-validity behavior; `trace/trace.go` `SpanContext.IsValid()`; `propagation/trace_context.go` `Inject`/`Extract` semantics. Verified directly, not inferred.
- `main` branch direct source read (via `git show main:<path>`, this session): `pkg/otelai/tracecontext.go`, `internal/controller/span_emission.go`, `internal/controller/{milestone,phase,plan,project,task}_controller.go`, `internal/controller/dispatch_helpers.go`, `internal/controller/reporter_jobspec.go`, `internal/dispatch/podjob/jobspec.go`, `api/v1alpha3/{milestone,phase,plan,project,task}_types.go`, `cmd/tide-reporter/main.go`, `internal/controller/span_emission_test.go`, `pkg/otelai/tracecontext_test.go`, `go.mod`, `Makefile`, commit `9cae6bb` (gocyclo precedent).
- `.planning/research/ARCHITECTURE.md`, `PITFALLS.md` (this repo, committed `c817f95`/`99d12bd`) — milestone-level research, cross-checked against live code rather than trusted at face value.

### Secondary (MEDIUM confidence)

- `.planning/phases/43-task-level-parity-trace-context-propagation/43-CONTEXT.md` — independently re-verified every line-number/signature citation against `main`; all confirmed accurate (this document supersedes it only on the two points explicitly called out: the two-sided `synthesizePlannerSpan` signature change, and the immediate-parent-fetch asymmetry table).

### Tertiary (LOW confidence)

- None — every claim in this document was either verified against `main`'s source or against the OTel SDK source via Context7.

## Metadata

**Confidence breakdown:**
- Standard stack: HIGH — no new packages; existing pins confirmed in `go.mod`.
- Architecture: HIGH — the core "how does deterministic-TraceID-as-root-span coexist with no-custom-IDGenerator" question was resolved by reading actual SDK source, not left as an assumption; the per-level parent-fetch asymmetry was independently traced through every relevant function on `main`.
- Pitfalls: HIGH for the mechanical ones (reporter Args-vs-Env, flag-registration crash-loop, two-status-patch sequencing); MEDIUM for Task's control-flow decision (Pitfall 1) — presented as an open decision with a recommendation, not asserted as settled fact.

**Research date:** 2026-07-16
**Valid until:** ~14 days (this is an internal-codebase-mechanics phase, not a fast-moving external dependency — the main decay risk is this worktree's drift from `main` getting worse, not the OTel SDK changing) — re-verify line numbers against whatever branch/worktree actually executes the plan if more than a few days pass or if the worktree-sync action item above hasn't landed yet.
