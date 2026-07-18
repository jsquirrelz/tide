# Phase 43: Task-Level Parity + Trace-Context Propagation - Context

**Gathered:** 2026-07-16
**Status:** Ready for planning

<domain>
## Phase Boundary

Thread the five dispatch levels (Project→Milestone→Phase→Plan→Task) into ONE connected OpenInference trace tree. Concretely:

1. **Task-level span parity (TRACE-01):** `TaskReconciler.handleJobCompletion` (`internal/controller/task_controller.go:924`) gains the same retroactive AGENT-span synthesis the four planner levels already have — currently **zero** span/otel code exists in this file.
2. **Real parent-child linking, not just Task addition (TRACE-02):** Phase 42 shipped all four existing planner-level spans as **independent roots** ("Option A" — see `internal/controller/span_emission.go:20-23`: *"no remote SpanContext injection, no TraceIDFromUID call. Phase 43 threads parenting."*). This phase must **retrofit** `synthesizePlannerSpan` (or add a parenting-aware variant) so Milestone/Phase/Plan/Project spans — AND the new Task span — share one deterministic TraceID (`otelai.TraceIDFromUID(string(project.UID))`) and each child's span parents under its immediate parent's real span, not just live alongside it.
3. **Durable per-level span-ID carrier (PROP-02):** add a new additive `.status` field (name/shape at planner's discretion) to all FIVE CRDs carrying that level's own synthesized span ID — this field does **not exist today** (confirmed absent from all five `*_types.go` Status structs) and is separate from the existing `{Level}SpanEmittedUID` idempotency markers (which gate "did I already emit for this Job UID," not "what is my span's identity").
4. **Traceparent injection at both pod hops (PROP-01):** inject `TRACEPARENT` into every level's subagent dispatch Job env (`internal/dispatch/podjob/jobspec.go` `BuildJobSpec`) and into the four *existing* reporter Jobs' env/args (`internal/controller/reporter_jobspec.go` `BuildReporterJob` / `ReporterOptions`, threaded through `dispatch_helpers.go`'s `spawnReporterIfNeeded`). Task has no reporter Job in this phase (that lands in Phase 44's MSG-01) — only Task's own subagent dispatch Job needs `TRACEPARENT`.

**Explicitly NOT this phase:** LLM message-array spans / `events.jsonl` parsing / Task's own trace-only reporter Job (Phase 44, MSG-01..03 + TRACE-03), the self-instrumenting adapter seam (Phase 45, ADAPT-01), sampler/session.id/metadata enrichment and the dashboard deep-link (Phase 46, OBS-01..04), the self-hosted Phoenix install + live proof (Phase 47, PHX/PROOF).

**Requirements:** TRACE-01, TRACE-02, PROP-01, PROP-02 (ROADMAP.md Phase 43 section, 4 success criteria).

</domain>

<decisions>
## Implementation Decisions

### Scope correction (not a preference — a verified fact downstream agents must not miss)
- **D-00 [informational]:** ARCHITECTURE.md's Suggested Build Order (steps 2–4) reads as if Phase 42 already added `Status.Trace.SpanID` and Phase 43 only needs to *extend* propagation to Task. **This is drift — direct code inspection confirms Phase 42 added none of it.** Phase 43 must MODIFY all four existing `synthesizePlannerSpan` call sites for real parenting, in addition to adding Task's. Treat ARCHITECTURE.md Patterns 1–3 and their code sketches as directionally correct but verify every signature against actual code (see D-08 below for one concrete signature mismatch already caught).

### Parent-child linking mechanism
- **D-01:** Follow ARCHITECTURE.md Pattern 2 exactly: a child level's dispatch/completion handler reads its **parent's** persisted span-ID field (a `client.Get` most handlers already perform for parent-ref resolution), builds a remote `trace.SpanContext` via `trace.NewSpanContext(...)` + `trace.ContextWithSpanContext`, and passes that context into `tracer.Start()` instead of a bare context. TraceID for every level (including Project, the trace root) is `otelai.TraceIDFromUID(string(project.UID))` — replacing whatever implicit random-root TraceID today's independent-root spans currently get.
- **D-02:** Project's span has no parent above it (Project is the run root) — it stays a root span, but now explicitly anchored to the deterministic TraceID rather than an arbitrary one, so it's part of the same trace Phoenix groups by `trace_id`.

### Durable span-ID field — shape and level coverage
- **D-03:** The new field is additive on **all five** CRDs' `.status`, including Task. Task is a "leaf" only w.r.t. having no CRD children to hand a parent span to — but Phase 46's dashboard deep-link (OBS-04) needs every level's own span ID including Task's, so skipping Task here would just move this exact field-addition work into Phase 46 instead. Add it now, for all five.
- **D-04:** The new field is **separate from** the existing `{Level}SpanEmittedUID` markers (`MilestoneSpanEmittedUID`, `PhaseSpanEmittedUID`, `PlanSpanEmittedUID`, `PlannerSpanEmittedUID`, and Task needs its own new `TaskSpanEmittedUID` mirroring the other four) — do not fold span-identity storage into the idempotency-marker field or vice versa; they answer different questions.
- **Claude's Discretion:** exact field name/shape. The existing four CRDs use flat string fields (`MilestoneSpanEmittedUID string`) rather than nested structs — for house-style consistency, prefer a flat `{Level}TraceSpanID string`-shaped field over ARCHITECTURE.md's suggested nested `Status.Trace *TraceStatus{SpanID string}`, but either is acceptable; match whatever the planner judges reads best alongside the existing marker fields in each `*_types.go`.

### Traceparent injection scope
- **D-05:** `TRACEPARENT` env injection applies to **all five** levels' subagent dispatch Jobs (Project/Milestone/Phase/Plan/Task), plus the **four existing** reporter Jobs (Milestone/Phase/Plan/Project — Task has no reporter Job yet). This matches PROP-01's literal, level-unscoped requirement text and keeps the contract uniform for a future self-instrumenting runtime at any level, not just Task.
- **D-06:** Mirror the existing conditional-env-append pattern already used for credproxy vars in `jobspec.go:405-412` (`ANTHROPIC_BASE_URL`/`SSL_CERT_FILE`/`NODE_EXTRA_CA_CERTS`) for the new `TRACEPARENT` var — same file, same mechanism, one more `corev1.EnvVar`.

### Task-level failure/retry parity
- **D-07:** Task-level span emission inherits Phase 42's D-01..D-04 policies verbatim, no Task-specific deviation: spans emit for succeeded AND failed Task Jobs; one span per Job attempt (each `resume --retry-failed` retry gets its own span, gated by the new `TaskSpanEmittedUID` marker keyed by Job UID exactly like the other four); failure detail rides as span status `Error` + `otelai.FailureDetail` attributes when the envelope is readable; a degraded envelope (`envReadOK=false`) still emits a span with usage attributes absent plus the `tide.envelope.degraded` marker.

### Known signature drift to correct against ARCHITECTURE.md
- **D-08 [informational]:** `otelai.TraceIDFromUID` is **not** `TraceIDFromUID(uid types.UID) trace.TraceID` as ARCHITECTURE.md's code sketch shows. The actual, already-implemented signature (`pkg/otelai/tracecontext.go`) is `TraceIDFromUID(uid string) (trace.TraceID, error)` — deliberately K8s-import-free (callers pass `string(project.UID)` and must handle the returned error, even though K8s UIDs are always valid UUIDs in practice).

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### Milestone research (v1.0.8 Phoenix Rising)
- `.planning/research/ARCHITECTURE.md` — Pattern 1 (retroactive synthesis, already implemented in `span_emission.go`), **Pattern 2 (deterministic TraceID + parent SpanID threading — the core mechanism this phase implements, D-01)**, Pattern 3 (trace-only reporter — NOTE this is Phase 44's MSG-01, not this phase's, despite appearing in "Suggested Build Order" step 3), Anti-Patterns 1–4, Suggested Build Order steps 3–4 (directionally correct, verify signatures per D-00/D-08)
- `.planning/research/PITFALLS.md` — Pitfall 3 (span-creation idempotency; Task needs its own `TaskSpanEmittedUID` marker mirroring the pattern in `span_emission.go`/the four controllers); Pitfall 5 (cross-pod clock skew — informational for this phase, verified at Phase 47)
- `.planning/research/SUMMARY.md` — confirms Phase 43 ("Phase 2: Task parity + propagation") is HIGH confidence / standard-pattern, skip a dedicated research pass
- `.planning/research/STACK.md` — `openinference-semantic-conventions` v0.1.1 pin (unchanged this phase)

### Requirements and constraints
- `.planning/REQUIREMENTS.md` §Trace-Context Propagation (PROP) and §Dispatch-Chain Span Emission (TRACE) — exact TRACE-01/02, PROP-01/02 text
- `.planning/ROADMAP.md` §"Phase 43: Task-Level Parity + Trace-Context Propagation" — goal, depends-on (Phase 42), 4 success criteria
- `.planning/PROJECT.md` §"Runtime-neutrality constraints" — traceparent contract is the durable seam for the future LangGraph runtime
- `.planning/STATE.md` §"v1.0.8 binding constraints" — span creation has no natural idempotency (must gate on state-transition edges, same as Job-creation idempotency), deterministic TraceID from Project.UID, no custom IDGenerator

### Phase 42 (prerequisite — decisions this phase inherits verbatim)
- `.planning/phases/42-trace-context-foundation-planner-level-span-emission/42-CONTEXT.md` — D-01..D-08: failure-path span coverage (D-01/D-02/D-03/D-04, inherited as D-07 above), ATTR-03 custom-key policy, attribute value semantics
- `.planning/phases/42-trace-context-foundation-planner-level-span-emission/` PLAN/PATTERNS files (if present) — the "42-PATTERNS.md" deviations from the `*RolledUpUID` skeleton referenced in `span_emission.go`'s comments

### Existing code (the surfaces this phase modifies or extends)
- `pkg/otelai/tracecontext.go` — `TraceIDFromUID(uid string) (trace.TraceID, error)`, `FormatTraceparent(traceID, spanID, sampled) string`, `ExtractRemoteParent(ctx, traceparent) context.Context` — all exist from Phase 42, **zero production call sites today**; this phase is where they get their first real callers
- `internal/controller/span_emission.go` — `synthesizePlannerSpan` (lines 115-164), `spanEndTime`, `plannerSpanResolvable` — the shared helper all four planner levels call; needs a parenting-aware parameter (parent SpanContext / parent span-ID) threaded in
- `internal/controller/{milestone,phase,plan,project}_controller.go` — existing marker-gated call sites: `milestone_controller.go:553-597`, `phase_controller.go:495-536`, `plan_controller.go:539-580` (`handlePlannerJobCompletion`), `project_controller.go:1804-1851` (`handleProjectJobCompletion`) — each needs its `synthesizePlannerSpan` call updated to pass parent context, plus a new `.status` patch for the durable span-ID field
- `internal/controller/task_controller.go` — `handleJobCompletion` (lines 924-1122, called from `checkRunningState` at line ~609-621 with `project` already resolved via `resolveProject`, no new plumbing needed for `project.UID` access); needs a net-new span-emission block mirroring the four existing ones, gated by a new `TaskSpanEmittedUID` marker
- `api/v1alpha3/{milestone,phase,plan,project,task}_types.go` — the four existing `{Level}SpanEmittedUID` fields (`milestone_types.go:78`, `phase_types.go:74`, `plan_types.go:130`, `project_types.go:529`); `task_types.go` `TaskStatus` (lines 147-170) has neither a `SpanEmittedUID` nor a `Trace` field — both are net-new for Task
- `internal/controller/dispatch_helpers.go` — `spawnReporterIfNeeded` (lines 93-133) — needs new `traceParent` (and later, Phase 44's `emitMessageSpans`) parameter threaded through to `BuildReporterJob`
- `internal/controller/reporter_jobspec.go` — `ReporterOptions` (line 74, currently only `ReporterImage string`) and `BuildReporterJob` (line 121) — need new OTEL/traceparent fields and CLI args
- `internal/dispatch/podjob/jobspec.go` — `BuildJobSpec` (line 191); mirror the conditional credproxy env-append pattern at lines 374-412 for the new `TRACEPARENT` var
- `internal/controller/span_emission_test.go` / `span_emission_unit_test.go` / `pkg/otelai/tracecontext_test.go` — established test conventions: Ginkgo/Gomega envtest + `tracetest.SpanRecorder` calling the handler method directly (not full `Reconcile()`) with a synthetic in-memory `*batchv1.Job` for handler-level tests; plain Go `func Test...` with no K8s client for pure-function tests. Mirror both tiers for Task-level and parenting work.

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- `pkg/otelai/tracecontext.go` primitives — fully implemented, unit-tested (`tracecontext_test.go`), zero K8s deps, ready to call as-is (mind the D-08 signature: `string` in, `(trace.TraceID, error)` out)
- `synthesizePlannerSpan` / `spanEndTime` / `plannerSpanResolvable` in `span_emission.go` — the D-01..D-04 policy set (succeeded+failed coverage, one-span-per-attempt, failure detail, degraded-envelope handling) is already correctly implemented and directly reusable/extensible for Task
- The marker-gated at-most-once emission pattern (`retry.RetryOnConflict` + `client.MergeFromWithOptimisticLock` + stamp-then-emit ordering) is proven across all four existing controllers — copy verbatim for Task's new `TaskSpanEmittedUID`

### Established Patterns
- **Mark-then-emit ordering** (42-REVIEW WR-01 fix): the idempotency marker is stamped BEFORE span emission, never after — a crash between stamp and emission loses that attempt's span (acceptable: span loss preferred over double-counted tokens in Phoenix cost views). Apply identically for Task.
- **`project` resolution already happens before `handleJobCompletion`** for all levels, including Task (`resolveProject` at `task_controller.go:1131`, via label fast-path or bounded-depth owner-chain walk) — `project.UID` is free to use, no new `client.Get` needed.
- **Conditional env-var append pattern** in `jobspec.go` (credproxy vars) is the exact template for injecting `TRACEPARENT`.

### Integration Points
- Task's `handleJobCompletion` is called from `checkRunningState` once a Job is observed terminal — the same call site that already resolves `project`, so the new span-emission block slots in alongside the existing envelope-read / output-path-validation logic (task_controller.go:924-1122) without new signature changes to the function itself.
- The four existing reporter-spawn call sites (inside each planner level's own `handleJobCompletion`-equivalent) are where the newly-synthesized span's W3C string gets threaded to `spawnReporterIfNeeded` → `BuildReporterJob` for that level's reporter Job.

</code_context>

<specifics>
## Specific Ideas

No user-specific vision requests for this phase — it's pure trace-plumbing correctness work fully specified by prior research and the Phase 42 precedent. The one substantive judgment call (D-00/D-01: retrofitting all four existing spans for real parenting, not just adding Task) was resolved by direct code verification rather than a stylistic preference, and is treated as locked fact, not an open question.

</specifics>

<deferred>
## Deferred Ideas

### Reviewed Todos (not folded)
- `2026-07-12-project-dispatch-missing-failurehalt-gate.md` (W-2 candidate) — dispatch-gate ordering concern touches the same controllers this phase modifies, but is a distinct concern (gate ordering, not span emission); stays a next-milestone candidate, not folded here.
- `2026-07-12-task-dispatch-gate-order-divergence.md` (W-2 sibling) — same disposition.
- `cache-f1-direct-sdk-cross-pod-caching.md` (CACHE-F1) — no overlap, deferred vNext+.
- `2026-07-03-signed-commits-verified-badge.md` (GPG/SIGN-02..04) — keyword false-positive, no overlap.

None of the above were folded — all four are either out of this phase's domain or already tracked as next-milestone candidates.

</deferred>

---

*Phase: 43-Task-Level Parity + Trace-Context Propagation*
*Context gathered: 2026-07-16*
