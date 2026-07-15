# Architecture Research

**Domain:** OpenInference trace emission into a self-hosted Arize Phoenix, wired into TIDE's existing K8s-native dispatch architecture
**Researched:** 2026-07-15
**Confidence:** MEDIUM-HIGH (retroactive-span mechanics verified against official `go.opentelemetry.io/otel` docs; trace-context propagation is standard W3C/OTel; the exact events.jsonl multi-turn schema and the future LangGraph-side propagation code are unverified/deferred — flagged inline)

## Standard Architecture

### System Overview

```
┌──────────────────────────────────────────────────────────────────────────┐
│ Manager Pod (controller-runtime, stateless/requeue-driven reconcilers)   │
│                                                                            │
│  MilestoneReconciler / PhaseReconciler / PlanReconciler / ProjectRecon.  │
│  TaskReconciler  ── handleJobCompletion(ctx, crd, completedJob) ──┐      │
│                                                                     │      │
│   1. Read TerminationStub (pod termination msg — NOT the PVC)     │      │
│   2. SYNTHESIZE this level's dispatch span retroactively:         │      │
│        tracer.Start(ctx, "tide.dispatch.<level>",                 │      │
│          trace.WithTimestamp(completedJob.Status.StartTime))      │      │
│        span.SetAttributes(otelai.AgentInvocation(...),            │      │
│          otelai.TokenCount(...), otelai.ArtifactPath(...))        │      │
│        span.End(trace.WithTimestamp(completedJob.Status           │      │
│          .CompletionTime))                                        │      │
│   3. Patch CRD .status.trace.spanID = span.SpanContext().SpanID() │      │
│   4. spawnReporterIfNeeded(...) — NOW also for Task level         │      │
│        passes --traceparent=<this span's W3C string>              │      │
│        passes --emit-message-spans=<self-instrument capability>   │      │
└─────────────────────────────────────────────────────────────────┬┘      │
                                                                     │       │
                              TRACEPARENT env / envelope field       │       │
                              (parent = ancestor's already-          │       │
                               synthesized span, read from the       │       │
                               CRD's own .status.trace.spanID        │       │
                               at DISPATCH time, one level earlier)  │       │
                                                                     ▼       │
┌────────────────────────────┐          ┌───────────────────────────────┐  │
│ Subagent dispatch Job (pod) │          │ tide-reporter Job (in-ns,      │  │
│ internal/subagent/anthropic │          │ PVC-mounted, spawned AFTER     │  │
│ (CLI-shim, NOT self-        │          │ the dispatch Job completes)    │  │
│ instrumenting today)        │          │                                 │  │
│                              │          │  1. (existing) materialize     │  │
│ writes events.jsonl (raw    │          │     child CRDs from out.json   │  │
│ stream-json tee) + out.json │          │     — planner levels only      │  │
│ to PVC. Ignores TRACEPARENT │          │  2. (NEW) if --emit-message-   │  │
│ today — no OTel SDK in this │          │     spans=true: parse          │  │
│ image.                       │          │     events.jsonl → one LLM-   │  │
│                              │          │     kind span per model turn, │  │
│ [FUTURE: self-instrumenting  │          │     parented via Extract() on │  │
│  LangGraph image reads       │          │     --traceparent, explicit   │  │
│  TRACEPARENT, extracts it,   │          │     WithTimestamp from the    │  │
│  emits openinference-        │          │     stream event's own        │  │
│  instrumentation-langchain   │          │     timestamps                │  │
│  spans LIVE via its own      │          │  3. needs its OWN TracerProvider│ │
│  OTLP exporter — no          │          │     (otelinit.NewTracerProvider│  │
│  retroactive synthesis       │          │     call site does NOT exist  │  │
│  needed; Run() is a single   │          │     yet in cmd/tide-reporter)  │  │
│  synchronous in-pod call]    │          └──────────────┬──────────────────┘ │
└──────────────────────────────┘                          │                  │
                                                            │ OTLP gRPC       │
                          OTLP gRPC (same OTEL_EXPORTER_    │                 │
                          OTLP_ENDPOINT the manager has —    ▼                 │
                          currently NOT forwarded into      ┌──────────────────┴─┐
                          Job specs; new wiring needed)     │  OTel Collector /   │
                                                             │  direct OTLP        │
                                                             │  ── Self-hosted     │
                                                             │  Arize Phoenix      │
                                                             │  (chart.otel        │
                                                             │  .exporter.endpoint)│
                                                             └─────────────────────┘
```

### Component Responsibilities

| Component | Responsibility (v1.0.8 addition) | Existing / New |
|-----------|------------------------------------|----------------|
| `internal/otelinit` | TracerProvider construction, env-driven sampler/endpoint | Existing (manager + dashboard only) — **needs a new call site in `cmd/tide-reporter/main.go`** |
| `pkg/otelai` | Thin OpenInference attribute helpers (`AgentInvocation`, `TokenCount`, `LLMInputMessages/Output`, `ArtifactPath`) | Existing, **zero call sites today** — this milestone is entirely about wiring these in, not adding new ones (only a small trace-context helper is new, see below) |
| Manager reconcilers (`internal/controller/*_controller.go`) | Retroactively synthesize each level's own `tide.dispatch.<level>` span at `handleJobCompletion`, using `completedJob.Status.{StartTime,CompletionTime}` (already in scope — no new I/O) | Modified — 5 call sites (Milestone, Phase, Plan, Project, **Task — currently has none**) |
| CRD `.status` (`api/v1alpha3/*_types.go`) | Persist this level's synthesized `spanID` so descendant dispatches can read their parent's span identity across reconcile/process boundaries | New additive field: `Status.Trace.SpanID string` on Milestone/Phase/Plan/Project (Task optional, leaf) |
| `internal/dispatch/podjob` (`jobspec.go`) | Inject `TRACEPARENT` env var into the subagent container at dispatch time (parent = the dispatching CRD's own just-synthesized span) | New env var, informational for today's CLI subagent, load-bearing for a future self-instrumenting runtime |
| `internal/controller/reporter_jobspec.go` (`BuildReporterJob`) | Inject `TRACEPARENT`, `OTEL_EXPORTER_OTLP_ENDPOINT`, `OTEL_SERVICE_NAME=tide-reporter`, `OTEL_TRACES_SAMPLER(_ARG)` into the reporter Job; extend spawn trigger to Task-level completions | Modified — new `ReporterOptions` fields; new spawn call site |
| `cmd/tide-reporter` + `internal/reporter` | Parse `events.jsonl` (same directory as `out.json`, already mounted) into `LLMInputMessages`/`LLMOutputMessages` spans; skip when the dispatch was self-instrumenting | New file (e.g. `internal/reporter/tracesynth.go`) |
| `pkg/dispatch` | Small vendor-capability lookup: does `Provider.Vendor` self-instrument? | New — a handful of lines, not a new package |
| Self-hosted Phoenix (Helm/docs) | Consumes the OTLP endpoint the chart already exposes (`otel.exporter.endpoint`) — no chart-side change to the trace pipeline itself | New — deployment recipe + docs only |

## Recommended Project Structure

```
pkg/
├── otelai/
│   ├── attrs.go              # UNCHANGED — 5 helpers stay locked (D-O4/D-O5)
│   ├── tracecontext.go       # NEW — pure functions, no K8s deps:
│   │                         #   TraceIDFromUID(uid types.UID) trace.TraceID
│   │                         #   FormatTraceparent(traceID, spanID, sampled) string
│   │                         #   ExtractRemoteParent(ctx, traceparent string) context.Context
│   └── doc.go                # UPDATE — document the new helper's scope (still NOT a 6th
│                              # payload-bearing helper — D-O5 enforcement test is unaffected)
├── dispatch/
│   └── vendor_capabilities.go  # NEW — SelfInstruments(vendor string) bool,
│                                # {"anthropic","openai","google","xai","opencode"}: false today
internal/
├── otelinit/
│   └── provider.go           # UNCHANGED signature — new call site in cmd/tide-reporter only
├── controller/
│   ├── dispatch_helpers.go   # MODIFY spawnReporterIfNeeded: + traceParent, + emitMessageSpans args
│   ├── reporter_jobspec.go   # MODIFY BuildReporterJob / ReporterOptions: + OTLP env, + --traceparent,
│   │                         #   + --emit-message-spans
│   ├── milestone_controller.go / phase_controller.go / plan_controller.go / project_controller.go
│   │                         # MODIFY handleJobCompletion: span synthesis + status.trace.spanID patch
│   └── task_controller.go    # MODIFY handleJobCompletion: span synthesis (NEW for this level)
│                             #   + NEW spawnReporterIfNeeded-equivalent call (trace-only mode,
│                             #     no child materialization — Task is a leaf)
├── dispatch/podjob/
│   └── jobspec.go            # MODIFY BuildJobSpec: + TRACEPARENT env on the subagent container
└── reporter/
    ├── materialize.go        # UNCHANGED
    └── tracesynth.go         # NEW — events.jsonl → OpenInference LLM-kind spans
api/v1alpha3/
├── milestone_types.go / phase_types.go / plan_types.go / project_types.go
│                             # ADDITIVE: Status.Trace *TraceStatus { SpanID string }
│                             #   (non-breaking; v1alpha3 stays sole served+storage version
│                             #   per Phase 40 — no version crank needed for an optional field)
docs/
└── observability.md          # EXPAND — "Tracing" section: traceparent contract, Phoenix recipe
INSTALL.md                    # EXPAND — Phoenix self-host quickstart pointer
```

### Structure Rationale

- **`pkg/otelai/tracecontext.go` as a NEW small file, not a new package:** keeps the "thin helper, five (now effectively six) pure functions" philosophy from D-O4 intact — this is context-string plumbing, not a span-lifecycle DSL, so it doesn't violate "no Go OpenInference SDK" scope. It is also the one piece of genuinely new logic that is 100% unit-testable with no K8s fixtures (pure `trace.TraceID`/`context.Context` manipulation), so it should land first (see Suggested Build Order).
- **`pkg/dispatch/vendor_capabilities.go` lives in `pkg/dispatch`, not `internal/subagent/*`:** the reporter binary is deliberately import-safe from `internal/controller`/`internal/subagent` (see `internal/reporter/materialize.go` package doc: "no `internal/controller` back-edge"). The manager already resolves `Provider.Vendor` at dispatch time via `ResolveProvider` (D-C2 precedence chain); a lookup table in the provider-neutral `pkg/dispatch` package (already imported by both the manager and the reporter) is the only place both sides can consult it without a layering violation.
- **CRD status field, not a Job annotation:** the parent's span ID must survive from the moment it's synthesized (at the parent's `handleJobCompletion`) to the moment a child is dispatched (a *later*, independent reconcile of a *different* reconciler, potentially after a manager restart). `.status` is exactly TIDE's existing "level-boundary durable state" mechanism — Job annotations don't survive Job GC (`TTLSecondsAfterFinished`), and re-deriving from the Job object would violate the "CRD-status-only, indegree-map-and-completed-set" resumability contract already established for the rest of the system.
- **`internal/reporter/tracesynth.go` as a new file, not folded into `materialize.go`:** `MaterializeChildCRDs` and `ChildrenAlreadyMaterialized` are child-CRD-authority logic (T-308 allowlist, spec-parent-ref stamping); message-span synthesis is a completely orthogonal read-only side effect against a different PVC file (`events.jsonl` vs `out.json`). Keeping them separate means the new Task-level "trace-only" reporter invocation can call `tracesynth.go` alone without pulling in the child-materialization code path that Task never needs.

## Architectural Patterns

### Pattern 1: Retroactive span synthesis at existing Job-completion call sites (not live, not held-open)

**What:** Every one of TIDE's five completion handlers (`MilestoneReconciler.handleJobCompletion`, `PhaseReconciler.handleJobCompletion`, `PlanReconciler`'s inline equivalent, `ProjectReconciler`'s inline equivalent, and the new `TaskReconciler.handleJobCompletion` addition) already receives `completedJob *batchv1.Job` with `.Status.StartTime` and `.Status.CompletionTime` populated by the time the reconciler observes the Job as complete. A dispatch span for that level is created and immediately closed in the same function call, using both timestamps explicitly — never held open across a `Reconcile()` return.

```go
tracer := otel.Tracer("tide.dispatch")
ctx, span := tracer.Start(ctx, "tide.dispatch.milestone",
    trace.WithTimestamp(completedJob.Status.StartTime.Time))
span.SetAttributes(otelai.AgentInvocation(ms.Name, "planner", "milestone")...)
span.SetAttributes(otelai.TokenCount(out.Usage.InputTokens, out.Usage.OutputTokens,
    out.Usage.CacheReadTokens, out.Usage.CacheCreationTokens)...)
span.End(trace.WithTimestamp(completedJob.Status.CompletionTime.Time))
spanID := span.SpanContext().SpanID() // real value, now known — thread it forward
```

**When to use:** Any span whose lifetime maps to a K8s object's observed lifecycle (a Job, a Pod) in a controller that is fundamentally stateless/requeue-driven. This is the correct pattern for TIDE's manager — it does not depend on the reconciler surviving between the object's creation and completion (a manager restart mid-dispatch is a first-class case the rest of the system already handles via re-derivation from `.status` + the indegree map).

**Trade-offs:** The span's exported timestamp is **event time**, not **emit time** — Phoenix will show the span "in the past" relative to when the collector actually received it, which is correct and desirable (it reflects when the work actually happened), but is worth documenting explicitly so an operator watching Phoenix live doesn't expect spans to appear the instant a Job starts. There is no way to see an *in-flight* dispatch as a live/open span in Phoenix under this pattern — only completed dispatches produce spans. That's an accepted limitation of the CRD-status-only, no-external-DB persistence model, not a bug: showing "duration so far" for a running Job would require either a live span (impossible across reconciles without a long-lived carrier) or a second write at dispatch-start-time purely for tracing, which the milestone's scope does not call for.

**Verification note:** `trace.WithTimestamp` is documented as a `SpanEventOption` that satisfies both `SpanStartOption` and `SpanEndOption` — confirmed via `pkg.go.dev/go.opentelemetry.io/otel/trace`, HIGH confidence, official source. Passing two different `WithTimestamp` values to `Start` and `End` produces a span whose SDK-recorded duration is the *difference between the two supplied timestamps*, not the wall-clock time elapsed between the two Go calls (which, in this retroactive pattern, are executed back-to-back in the same goroutine, microseconds apart).

### Pattern 2: Deterministic TraceID + explicitly-threaded parent SpanID (no custom IDGenerator needed)

**What:** A K8s UID is a standard 128-bit UUID — exactly the width of an OTel `trace.TraceID` ([16]byte). Every level of the hierarchy already has (or can resolve) the owning `Project.UID` in scope. Deriving the trace ID deterministically from it eliminates the need to propagate the trace ID at all — every reconciler, the reporter, and a future self-instrumenting subagent independently compute the *same* trace ID without any wire transfer:

```go
func TraceIDFromUID(uid types.UID) (trace.TraceID, error) {
    u, err := uuid.Parse(string(uid))
    if err != nil {
        return trace.TraceID{}, err
    }
    return trace.TraceID(u), nil // uuid.UUID is [16]byte; trace.TraceID is [16]byte
}
```

Only the **parent span ID** needs to travel — and only downward, one hop at a time, exactly along the existing dispatch-descent path: a level's own span ID becomes known (Pattern 1) at the *same reconcile* that decides to spawn its reporter / materialize its children, so it is available in-process, before it needs to go anywhere. It is (a) formatted as a W3C `traceparent` string and handed to that same reconcile's reporter-spawn call, and (b) persisted to `.status.trace.spanID` so that when a **child** CRD is later dispatched (a different reconciler, a later point in time, possibly after a restart), the child's dispatch site reads its **parent's** `.status.trace.spanID` (a normal `client.Get` the reconciler is already doing to resolve the parent) and formats the `TRACEPARENT` it injects into its own Job.

```go
sc := trace.NewSpanContext(trace.SpanContextConfig{
    TraceID: traceID, SpanID: parentSpanID, TraceFlags: trace.FlagsSampled, Remote: true,
})
ctx := trace.ContextWithSpanContext(context.Background(), sc)
carrier := propagation.MapCarrier{}
propagation.TraceContext{}.Inject(ctx, carrier)
traceparent := carrier.Get("traceparent") // "00-<trace-id>-<span-id>-01"
```

**Why this avoids the "held-open span" trap the milestone brief flags:** the naive design — pre-committing a level's *own* span ID before dispatch so a child can reference "me" as parent even though "I" haven't completed yet — is what would force a custom `IDGenerator` hack (Go's `tracer.Start()` has no public API to force a specific `SpanID` on the new span it creates; only the incoming parent context's `TraceID` is honored). TIDE's dispatch topology sidesteps this entirely: a level's dispatch span only needs to exist *after* its own Job completes, and by construction nothing downstream (its own reporter, or any child level) is dispatched *before* that point. So every consumer of "level N's span ID" chronologically follows the moment N's span is synthesized — a plain `tracer.Start()` call (fresh random `SpanID`, inherited `TraceID` from the deterministic derivation) is sufficient; no ID must ever be forced.

**When to use:** Any hierarchical dispatch system where a "child" is only ever created after the "parent" has reached a specific, observable milestone (here: Job completion). If TIDE ever adds *speculative* child dispatch before the parent settles, this pattern breaks and would need the heavier custom-IDGenerator technique.

**Confidence:** HIGH on the primitives (`trace.NewSpanContext`, `propagation.TraceContext{}.Inject/Extract`, `trace.TraceID`/`SpanID` byte widths) — all stable, documented `go.opentelemetry.io/otel` public API. MEDIUM on "this is definitely how TIDE should sequence it" — this is original design synthesis from the codebase's actual call ordering (verified: all 5 completion handlers receive `completedJob` with populated `Status.StartTime`/`CompletionTime`, and reporter-spawn already happens in the same function), not something documented anywhere externally.

### Pattern 3: The reporter Job is the sole `events.jsonl` parser — extended to a level that has none today

**What:** `events.jsonl` (the raw Claude Code `stream-json` tee, written by `internal/subagent/anthropic/stream_parser.go` at `<workspace>/envelopes/<UID>/events.jsonl`) already exists at **every** dispatch level, planner and executor alike — `ParseStream` is shared code, keyed only by `Role` ("planner" vs "executor"). But today, the in-namespace reporter Job that mounts the PVC and could read that file is spawned **only** for planner-level completions (Milestone, Phase, Plan, Project-as-milestone-planner) — because its original purpose (`MaterializeChildCRDs`) only applies where there are children to create. **`TaskReconciler.handleJobCompletion` spawns no reporter-equivalent Job at all today** — Task completions are read entirely from the pod termination message (`PodStatusEnvelopeReader`, cross-namespace-safe, tiny), which never touches the PVC. This means the executor level — arguably where the richest LLM conversation happens (the actual code-writing turns) — currently has **zero** path to its own `events.jsonl`.

Fix: generalize the reporter spawn trigger to Task completions too, with a **trace-only mode** — the same binary, same PVC mount pattern (`SubPath: <project-uid>/workspace`, same `--workspace=/workspace --task-uid=<UID>` args tide-reporter already accepts), but skipping `MaterializeChildCRDs` (Task has no children) and running only the new `tracesynth` step.

```go
// task_controller.go, new call inside handleJobCompletion, mirroring
// spawnReporterIfNeeded but without child materialization:
spawnTraceReporterIfNeeded(ctx, r.Client, r.Scheme, task, project, "Task",
    r.Deps.ReporterImage, r.sharedPVCName(), traceParent, emitMessageSpans)
```

**When to use:** Any time a new artifact (here: `events.jsonl`) is written to a namespace-local PVC by a pod the manager cannot mount, and a NEW consumer of that artifact is needed at a level that previously had no reason to spawn an in-namespace reader. The existing "in-namespace reporter Job, least-privilege SA, PVC subPath, idempotent deterministic name" scaffolding (`internal/controller/reporter_jobspec.go`, `reporter-rbac.yaml`) is directly reusable — this is an *extension* of an existing pattern, not a new one.

**Confidence:** HIGH — grounded entirely in reading the actual reconciler code (`task_controller.go` `handleJobCompletion` signature, `PodStatusEnvelopeReader.ReadOut`, the four existing `spawnReporterIfNeeded`/inline-equivalent call sites).

### Pattern 4: Self-instrumenting-runtime adapter seam (vendor capability flag, not a Go interface call)

**What:** `pkg/dispatch.Subagent.Run(ctx, in EnvelopeIn) (EnvelopeOut, error)` executes synchronously, entirely within one dispatch pod's process lifetime — it is **not** a reconciler and does **not** span reconcile boundaries. This means the "held-open spans look wrong" concern from Pattern 1 simply does not apply to a self-instrumenting `Subagent` implementation: a future LangGraph-backed implementation is free to do ordinary live instrumentation — `tracer.Start()` at the top of `Run()`, `defer span.End()` at the bottom, with `openinference-instrumentation-langchain` auto-instrumenting nested LLM/tool spans underneath — because "the whole call" is a single, bounded, synchronous unit, unlike a controller's `Reconcile()`.

The reporter (a *separate binary*, spawned as a *separate Job*, with no Go-level access to whichever `Subagent` implementation ran) cannot ask "did you already emit spans?" via an interface call — the flag has to travel as **data**, not code. The manager already resolves `Provider.Vendor` at dispatch time (`ResolveProvider`, D-C2 precedence chain: `Project.Spec.subagent.levels.<level>.vendor` → Project default → Helm default) — this is the one place both dispatch-time (subagent Job) and completion-time (reporter Job spawn) logic can consult the same fact, using a small allowlist rather than trusting the subagent pod's own self-report (subagent pods are semi-trusted output producers under the existing `T-308` threat model — the manager, not the pod, should be the authority on whether double-emission is possible):

```go
// pkg/dispatch/vendor_capabilities.go
func SelfInstruments(vendor string) bool {
    switch vendor {
    case "anthropic", "openai", "google", "xai", "opencode":
        return false // CLI/wrapper-shimmed — no in-process OTel SDK
    default:
        return false // fail-closed: unknown vendor never skips synthesis
    }
}
```

The reporter's `--emit-message-spans` flag is `!SelfInstruments(vendor)`, computed by the manager at the same call site that already resolves `Provider` for the dispatch and threaded through `ReporterOptions` alongside `ReporterImage`.

**When the future LangGraph vendor lands**, three things change together, not just one: (1) `SelfInstruments("langgraph") → true`, (2) the LangGraph image's own `main()` must call an OTel init equivalent to `internal/otelinit.NewTracerProvider` (reading `OTEL_EXPORTER_OTLP_ENDPOINT` forwarded into its Job env — the same new wiring this milestone adds for the reporter) and extract the injected `TRACEPARENT` into its base context before invoking the graph, and (3) attribute/span-kind conventions must match what `openinference-instrumentation-langchain` emits by default so a Phoenix query written against synthesized spans (this milestone) still matches native spans (post-LangGraph) without a query rewrite. Verified: OpenInference's spec defines ten span kinds (`LLM`, `CHAIN`, `TOOL`, `AGENT`, `RETRIEVER`, `EMBEDDING`, `RERANKER`, `GUARDRAIL`, `EVALUATOR`, `PROMPT`) and the identical flat-keyed `llm.input_messages.<i>.message.{role,content}` encoding TIDE's `pkg/otelai.LLMInputMessages` already implements — this milestone's synthesized message spans should use `openinference.span.kind=LLM` (a **new** attribute value; today's `AgentInvocation` helper only ever stamps `AGENT`, so per-message spans need `otelai.LLMInputMessages/Output` + `TokenCount` + a manually-set `attribute.String("openinference.span.kind", "LLM")`, not a call to `AgentInvocation`), nested under the level's `AGENT`-kind dispatch span from Pattern 1 — this mirrors exactly how the OpenInference spec describes an AGENT span "encompassing calls to LLMs and Tools."

**Confidence:** MEDIUM. The `Subagent.Run()`-is-synchronous-so-live-instrumentation-is-fine reasoning and the vendor-capability-as-data (not interface) design are HIGH confidence (grounded in the actual `pkg/dispatch.Subagent` interface and the actual reporter/manager process boundary). The OpenInference span-kind vocabulary and message-array encoding are HIGH confidence (verified against the spec doc directly). Exactly how `openinference-instrumentation-langchain` structures its own span tree (which of CHAIN vs AGENT it puts at the graph-invocation root, whether tool-call spans nest under LLM or sibling to it) is **not verified in this pass** — Context7/WebSearch surfaced the package's existence and general LangChain-callback-hook mechanism but not a concrete span-tree example. Flag as an open question for the vNext LangGraph milestone's own research, not something to guess at now.

### Pattern 5: The D-O5 payload boundary — inline messages by default, redact first, keep ArtifactPath as a co-attribute

**What:** `pkg/otelai/doc.go`'s existing D-O5 rule bans an `InlinePayload`/`RawContent`/`Body` helper on the *public* surface, but explicitly carves out `LLMInputMessages`/`LLMOutputMessages`'s `Message.Content` field for verbatim text "when [the caller has] already decided the payload is safe." The v1.0.8 milestone's stated goal is explicit: spans must carry "full LLM input/output message arrays," not just artifact references — so unlike a hypothetical stricter interpretation, this milestone's reporter **is** a caller that has made that D-O5 judgment call, by design.

Concrete recommendation: the reporter's `tracesynth.go` should attach **both** attribute groups to the same LLM-kind span — `LLMInputMessages`/`LLMOutputMessages` (the actual deliverable, populated from the reconstructed per-turn content) **and** `ArtifactPath(eventsJSONLPath)` (the existing, already-safe reference to the full raw log, which carries tool-call arguments and other detail that doesn't cleanly fit the flat role/content shape). This is not redundant: Phoenix's UI renders message content inline from the first group (satisfying the milestone goal) while the second group remains the durable fetch-on-demand path for anything that overflows or predates the message-array attributes.

Before stamping `Message.Content`, run it through TIDE's existing secret-redaction machinery rather than inventing new pattern matching — `internal/harness/redact.SecretPatterns` (already used to scrub subagent stdout/stderr in-pod) is a directly reusable regex list; a small non-streaming wrapper (`redact.String(s string) string`, reusing `SecretPatterns`) applied to each message's content before it becomes a span attribute value closes the "PII/secret leakage into long-term trace storage" risk D-O5's original rationale calls out, extended from "don't inline at all" to "don't inline *unredacted*." This is consistent with the existing precedent of `internal/gitleaks` scanning outbound git diffs before they leave the trust boundary — span attributes flowing to an external OTLP collector are the same class of egress.

**Trade-off made explicit:** sampling (default `parentbased_traceidratio` @ 0.1, D-O3) bounds this cost to roughly 10% of traces by default — an unsampled trace's spans are never constructed with full attribute sets because the SDK short-circuits on the sampling decision before attribute population is worth doing. Operators who want stricter data minimization can lower the sampler ratio, or a future toggle (`otel.redactMessageContent` / a Project-level opt-out) can force `ArtifactPath`-only mode — recommended as a fast-follow, not blocking v1.0.8, since the milestone's explicit goal commits to full message arrays as the default behavior.

## Data Flow

### Dispatch-time flow (parent → child, before a Job runs)

```
Parent CRD (e.g. Plan) already has .status.trace.spanID = <span synthesized when
Plan's own planner Job completed>
    ↓
Child dispatch site (e.g. TaskReconciler building a new Task's EnvelopeIn/Job)
reads Plan.status.trace.spanID (already fetching the parent via client.Get for
other reasons — parentRef resolution) + derives TraceID from Project.UID
    ↓
Formats traceparent := FormatTraceparent(traceID, parentSpanID, sampled)
    ↓
Injects into:
  - Task Job env: TRACEPARENT=<traceparent>   (podjob.BuildJobSpec — new env var)
  - EnvelopeIn (optional, for a future runtime that prefers envelope over env)
```

### Completion-time flow (this level's own span, then fan-out to reporter)

```
Job completes → manager reconciler's handleJobCompletion(ctx, crd, completedJob)
    ↓
1. Read TerminationStub (existing — no PVC access)
2. Synthesize tide.dispatch.<level> span:
     Start(WithTimestamp(completedJob.Status.StartTime))
     SetAttributes(AgentInvocation + TokenCount + ArtifactPath[out.json])
     End(WithTimestamp(completedJob.Status.CompletionTime))
3. Patch crd.status.trace.spanID = span.SpanContext().SpanID()  [NEW status write]
4. spawnReporterIfNeeded(..., traceParent=<this span's W3C string>,
     emitMessageSpans=!SelfInstruments(vendor))
    ↓
Reporter Job (PVC-mounted, in project namespace):
  - [existing] MaterializeChildCRDs from out.json  (planner levels only)
  - [NEW] if emitMessageSpans: tracesynth.Emit(eventsJSONLPath, traceParent)
      → one LLM-kind span per model turn, Extract(traceParent) as parent context,
        WithTimestamp from the stream event's own per-turn timestamps
      → LLMInputMessages/LLMOutputMessages (redacted) + ArtifactPath co-attribute
```

### Key Data Flows

1. **TraceID never travels on the wire** — every participant (manager, reporter, future self-instrumenting subagent) independently derives it from `Project.UID`, which all three already have in scope by construction.
2. **SpanID travels exactly one hop per dispatch, always parent → child, always chronologically after the parent's own span exists** — via `.status.trace.spanID` (survives restarts, matches TIDE's existing "level boundary = durable artifact" philosophy) and via `TRACEPARENT` env (for the currently-running Job/reporter pair that needs it *now*, not persisted beyond that Job's lifetime).
3. **`events.jsonl` never leaves the project-namespace PVC boundary** — only the reporter (already trusted, in-namespace, least-privilege SA) reads it; the manager still never mounts a project PVC, preserving the Phase 9 Option C cross-namespace architecture untouched.

## Scaling Considerations

| Concern | Small run (10s of Tasks) | Medium run (100s of Tasks) | Large run (1000s of Tasks, multi-milestone) |
|---------|---------------------------|------------------------------|-----------------------------------------------|
| Reporter Job count | +1 Job per Task now (previously 0) — negligible | Job churn roughly doubles vs. today (planner-only reporters); each has `TTLSecondsAfterFinished: 300` so no accumulation | Same headroom concern as the existing planner-concurrency cap (Phase 32) — the Task-level trace-reporter spawn should respect the same in-flight dispatch gate, not bypass it |
| Span volume | Trivial | `parentbased_traceidratio(0.1)` bounds constructed spans to ~10% of dispatches by default | Sampler is the only lever that matters at this scale — no code change needed, `OTEL_TRACES_SAMPLER_ARG` is already env-overridable per-install |
| etcd impact | One new `int64`/`string`-sized status field × 5 CRD kinds — negligible | Same — status field is O(1) per object, not O(children) | Confirmed non-issue: `.status.trace.spanID` is a fixed 16-hex-char string, nowhere near the `make verify-no-aggregates` guard's concern (no `Schedule`/`Waves[]`/`IndegreeMap`-shaped growth) |
| Collector/Phoenix ingest | N/A | N/A | Out of TIDE's control — standard OTel Collector batching/backpressure applies; TIDE emits via `otlptracegrpc` batch processor already in `otelinit`, no change needed |

## Anti-Patterns

### Anti-Pattern 1: Holding a span open across `Reconcile()` calls

**What people do:** Call `tracer.Start()` when a Job is *dispatched* and try to stash the returned `span`/`ctx` somewhere (a package-level map keyed by UID, a context passed through the work-queue) to `End()` it later when the Job completes.
**Why it's wrong:** controller-runtime reconcilers are explicitly stateless and requeue-driven — a manager restart, a leader-election handoff, or simply the normal requeue/re-entry model means there is no guaranteed in-process continuity between the `Reconcile()` call that dispatches and the (possibly much later, possibly different-process) `Reconcile()` call that observes completion. A span held in an in-memory map silently vanishes (and leaks) on restart, and produces spans with wrong/missing end times if the map entry survives but the "real" completion was observed by a different manager replica.
**Instead:** synthesize retroactively (Pattern 1) — anchor both timestamps to data that is *itself* durable and re-derivable (`completedJob.Status.StartTime`/`CompletionTime`, sourced fresh from the K8s API on every reconcile, exactly like every other piece of TIDE's resumption-state design).

### Anti-Pattern 2: Forcing exact SpanIDs via a custom IDGenerator to solve a problem TIDE doesn't have

**What people do:** Reach for `sdktrace.WithIDGenerator(...)` + a context-stashed override so a synthesized span can be given a specific, pre-agreed `SpanID` another process already committed to.
**Why it's wrong here:** it's real, documented-as-tricky OTel Go surface (no first-party blessed example; community discussion explicitly calls it "convoluted") that TIDE's dispatch topology doesn't actually need — see Pattern 2's reasoning: nothing ever needs to know a level's own span ID *before* that level's Job completes, because nothing is ever dispatched before that point. Reaching for this technique is a sign the trace-context design has accidentally reintroduced a chicken-and-egg dependency that the natural dispatch ordering doesn't have.
**Instead:** let `tracer.Start()` mint a fresh random `SpanID` at synthesis time (Pattern 1/2) and thread the *resulting* concrete ID forward to the (chronologically later) consumers that need it.

### Anti-Pattern 3: Spawning a full reporter Job (with child-materialization machinery) for every level unconditionally

**What people do:** Generalize `spawnReporterIfNeeded` to fire identically at all five levels, including Task, running the same `MaterializeChildCRDs` code path regardless of whether there's anything to materialize.
**Why it's wrong:** Task has no children — running the materialization step is dead work every time, and more importantly it couples "does this level need trace synthesis" to "does this level need child materialization," which are orthogonal concerns (Pattern 3). It also risks silently breaking the existing `ChildrenAlreadyMaterialized` idempotency guard's fail-open default (`case default: return false, nil`) if a Task ever gets routed through the `MaterializeChildCRDs` switch, since Task is not one of the `ChildKindAllowlist` producer types.
**Instead:** a trace-only reporter mode/flag (Pattern 3) that skips materialization entirely for leaf levels — reuses the Job-building scaffolding (PVC mount, SA, owner ref) but not the materialization business logic.

### Anti-Pattern 4: Trusting the subagent pod to self-report whether it already emitted spans

**What people do:** Add a `SelfInstrumented bool` field to `EnvelopeOut`, populated by the subagent binary itself, and have the reporter trust it directly.
**Why it's wrong:** subagent pods are semi-trusted output producers under TIDE's existing threat model (`T-308` — the `ChildKindAllowlist` gate exists precisely because planner-authored output isn't blindly trusted). A self-reported flag on `EnvelopeOut` is one more field a buggy or malicious image could misreport, and it makes the manager's own dispatch-time decision (which vendor/image was resolved for this level) and the reporter's completion-time decision (should I synthesize) drift from a single source of truth.
**Instead:** the manager computes the flag itself (Pattern 4) from `Provider.Vendor`, already resolved at dispatch time, and passes it explicitly to the reporter as a Job argument — same trust boundary the rest of the dispatch chain already uses (the manager resolves `Provider`, not the pod).

## Integration Points

### External Services

| Service | Integration Pattern | Notes |
|---------|---------------------|-------|
| Arize Phoenix (self-hosted) | Standard OTLP gRPC ingestion — TIDE already exposes `otel.exporter.endpoint` as a chart value pointed at any OTLP-compatible collector/backend; Phoenix's official Helm chart/manifests are deployed **alongside**, not as a subchart dependency (per the milestone's stated `TELEM-01` pattern, matching the existing `prometheus.enabled=false`-by-default posture — no coupling to a specific observability vendor's chart release cadence) | No TIDE-side code change to talk to Phoenix specifically — it's just another OTLP consumer. All the work is in *emitting* correctly-shaped spans (this document) and in the docs/chart-value plumbing to point at wherever Phoenix ends up running. |

### Internal Boundaries

| Boundary | Communication | Notes |
|----------|---------------|-------|
| Manager reconciler ↔ subagent dispatch Job | `TRACEPARENT` env var (new), one-way, dispatch-time only | Mirrors the existing `ANTHROPIC_BASE_URL`/credproxy env-injection pattern in `jobspec.go` — same file, same mechanism, just one more `corev1.EnvVar` |
| Manager reconciler ↔ reporter Job | `--traceparent` + `--emit-message-spans` CLI args (new), `OTEL_EXPORTER_OTLP_ENDPOINT`/sampler env vars (new) | Mirrors the existing `--parent-name`/`--parent-kind` arg pattern in `reporter_jobspec.go`; the OTLP env vars are a genuinely new category (today only the manager and dashboard Deployments carry them — this is the first Job-level OTel wiring) |
| Reporter Job ↔ PVC | Reads `events.jsonl` from the same directory it already reads `out.json` from — zero new mount/subPath work | Confirmed: both files live at `<workspace>/envelopes/<UID>/` |
| CRD `.status` ↔ next-level dispatch | New `Status.Trace.SpanID` field, read by `client.Get`-ing the parent (already happening for `parentRef` resolution in most dispatch sites) | Additive field on `MilestoneStatus`/`PhaseStatus`/`PlanStatus`/`ProjectStatus`; no version crank (v1alpha3 stays sole served+storage version) |

## Suggested Build Order

1. **`pkg/otelai/tracecontext.go`** — pure helpers (`TraceIDFromUID`, `FormatTraceparent`, `ExtractRemoteParent`). Zero K8s dependencies, fully unit-testable in isolation, and every other step depends on it. Lowest risk, do first.
2. **Manager: retroactive dispatch-span synthesis at the four existing planner-level completion handlers** (Milestone/Phase/Plan/Project). Pure composition of step 1 + `pkg/otelai`'s existing helpers + data (`completedJob.Status`, envelope `Usage`) the reconcilers already hold — no new I/O, no new Job spawns yet. Independently demoable: real spans appear in Phoenix (self-rooted, since propagation isn't wired yet) as soon as this lands. Add the `Status.Trace.SpanID` additive field here.
3. **Task-level parity** — extend the same synthesis to `TaskReconciler.handleJobCompletion` (net-new call site; no reporter-equivalent exists there today) and add the trace-only reporter spawn (Pattern 3). This is the step that closes the biggest current gap (executor level has zero PVC-side observability hook today).
4. **Traceparent propagation, parent → child** — thread `.status.trace.spanID` into `TRACEPARENT` on (a) the next level's dispatch Job env (`podjob.BuildJobSpec`) and (b) the reporter Job's args (`BuildReporterJob`/`ReporterOptions`). Touches the widest surface (every dispatch call site) but has no externally-visible behavior change until step 5 consumes it — safe to land as "plumbing only."
5. **Reporter: `events.jsonl` → LLM-kind message-array spans** (`internal/reporter/tracesynth.go` + `cmd/tide-reporter/main.go` gaining its first `otelinit.NewTracerProvider` call site + `OTEL_EXPORTER_OTLP_ENDPOINT` forwarding into `BuildReporterJob`). Depends on step 4 for correct parenting and step 1 for the extract helper. This is where the D-O5 redaction pass (Pattern 5) lands too.
6. **Self-instrumenting capability seam** (`pkg/dispatch/vendor_capabilities.go` + `--emit-message-spans` wiring at all six spawn sites). Ship last — it's pure forward-compatibility scaffolding with zero behavioral effect today (every current vendor returns `false`; nothing self-instruments yet), so sequencing it last avoids blocking the other steps on a capability table that has no real second entry until the LangGraph milestone.
7. **Self-hosted Phoenix chart/docs surface** (`INSTALL.md`/`docs/observability.md` expansion, Phoenix manifest recipe, NOTES.txt nudge). Independent of steps 1–6 — can proceed in parallel on a separate plan/track, gated only on step 2 *or* step 5 existing for the milestone's required "live run's trace tree visible and queryable in Phoenix" end-to-end proof.

## Sources

- `go.opentelemetry.io/otel/trace` — `WithTimestamp` dual start/end applicability: https://pkg.go.dev/go.opentelemetry.io/otel/trace (HIGH confidence, official)
- `go.opentelemetry.io/otel/sdk/trace` — `IDGenerator` interface, `WithIDGenerator` option: https://pkg.go.dev/go.opentelemetry.io/otel/sdk/trace and https://github.com/open-telemetry/opentelemetry-go/blob/main/sdk/trace/id_generator.go (HIGH confidence on the API surface; MEDIUM on the "context-stashed override" combinator pattern, which is community-sourced, not first-party — ultimately not needed per Pattern 2's reasoning, kept here only to document why it was considered and rejected)
- OTel Go custom TraceID/SpanID discussion: https://github.com/open-telemetry/opentelemetry-go/discussions/5029 (MEDIUM — "no consensus best practice" as of the discussion; corroborates that TIDE's dispatch-ordering-based avoidance of this problem is the right call, not a gap)
- Arize OpenInference semantic conventions spec: https://github.com/Arize-ai/openinference/blob/main/spec/semantic_conventions.md (HIGH — span kind vocabulary and flat-keyed message encoding verified directly; matches `pkg/otelai/attrs.go`'s existing implementation exactly)
- `openinference-instrumentation-langchain` package existence and general LangChain-callback mechanism: PyPI/GitHub search results (LOW-MEDIUM — package structure confirmed to exist and hook `langchain-core`; exact span-tree shape for LangGraph specifically NOT verified in this pass, flagged as an open question for the vNext LangGraph milestone)
- In-repo verified via direct source read (HIGH — not training-data speculation): `internal/otelinit/provider.go`, `pkg/otelai/{attrs,doc}.go`, `pkg/dispatch/envelope.go`, `pkg/dispatch/provider.go`, `pkg/dispatch/subagent.go`, `internal/subagent/anthropic/{subagent,stream_parser}.go`, `internal/reporter/materialize.go`, `cmd/tide-reporter/main.go`, `internal/controller/{reporter_jobspec,dispatch_helpers,milestone_controller,phase_controller,plan_controller,project_controller,task_controller}.go`, `internal/dispatch/podjob/{backend,jobspec}.go`, `internal/harness/redact/redact.go`, `internal/gitleaks/*.go`, `api/v1alpha3/task_types.go`, `charts/tide/{values.yaml,templates/deployment.yaml,templates/dashboard-deployment.yaml}`, `docs/observability.md`, `.planning/PROJECT.md`, `.planning/milestones/v1.0.0-phases/{03,04}-*/{03-RESEARCH,04-RESEARCH}.md`

---
*Architecture research for: OpenInference trace emission + self-hosted Arize Phoenix (v1.0.8 "Phoenix Rising")*
*Researched: 2026-07-15*

