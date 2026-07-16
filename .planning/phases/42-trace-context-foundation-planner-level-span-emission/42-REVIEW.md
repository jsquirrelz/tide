---
phase: 42-trace-context-foundation-planner-level-span-emission
reviewed: 2026-07-15T23:52:07Z
depth: standard
files_reviewed: 24
files_reviewed_list:
  - api/v1alpha3/milestone_types.go
  - api/v1alpha3/phase_types.go
  - api/v1alpha3/plan_types.go
  - api/v1alpha3/project_types.go
  - charts/tide-crds/templates/milestone-crd.yaml
  - charts/tide-crds/templates/phase-crd.yaml
  - charts/tide-crds/templates/plan-crd.yaml
  - charts/tide-crds/templates/project-crd.yaml
  - config/crd/bases/tideproject.k8s_milestones.yaml
  - config/crd/bases/tideproject.k8s_phases.yaml
  - config/crd/bases/tideproject.k8s_plans.yaml
  - config/crd/bases/tideproject.k8s_projects.yaml
  - internal/controller/milestone_controller.go
  - internal/controller/phase_controller.go
  - internal/controller/plan_controller.go
  - internal/controller/project_controller.go
  - internal/controller/span_emission_test.go
  - internal/controller/span_emission_unit_test.go
  - internal/controller/span_emission.go
  - pkg/otelai/attrs_test.go
  - pkg/otelai/attrs.go
  - pkg/otelai/doc.go
  - pkg/otelai/tracecontext_test.go
  - pkg/otelai/tracecontext.go
findings:
  critical: 0
  warning: 4
  info: 5
  total: 9
status: issues_found
---

# Phase 42: Code Review Report

**Reviewed:** 2026-07-15T23:52:07Z
**Depth:** standard
**Files Reviewed:** 24
**Status:** issues_found

## Summary

Reviewed the Phase 42 trace-context foundation: the pure `pkg/otelai` primitives (attribute helpers rebased onto the openinference-semantic-conventions module, deterministic TraceID derivation, W3C traceparent format/extract), the shared retroactive span-synthesis helper (`internal/controller/span_emission.go`), its wiring into all four planner-level completion handlers, the four new `*SpanEmittedUID` status marker fields, and the regenerated CRD schemas (both `config/crd/bases` and `charts/tide-crds` copies, verified in sync and correctly alphabetized — genuine controller-gen output).

Verification performed during review: `go build ./...` clean, `go vet` clean, `gofmt -l` clean, `go test ./pkg/otelai/` green, span unit tests green (`-run 'TestSpanEndTime|TestSynthesizePlannerSpan'`), and the four SpanEmission envtest Describes green against real envtest assets (28s). The suite manager registers no reconcilers, so the direct-handler-call test shape is race-free. Exit-code propagation was cross-checked (`cmd/claude-subagent/main.go:101` propagates the envelope exit code as the process exit code, so `isJobFailed` and envelope failure classification align in the normal path). Locked decisions (retroactive same-call synthesis, spans on succeeded AND failed Jobs, degraded spans, second `ResolveProvider` call, prompt-includes-cache-subsets token remap, `tide.*` namespace, exact v0.1.1 pin with no drift-guard, no `WithSampler`) were treated as ground truth and not flagged.

No Critical findings. Four Warnings — all cluster around the marker-gated emission contract: the implementation delivers at-least-once (not the "exactly one" its comments claim), gates by Job *name* where the phase decision (D-02 / "one per Job UID") requires per-attempt Job identity, couples telemetry bookkeeping failure to core pipeline progression, and has a test-fixture failure mode that can poison the global TracerProvider for subsequent specs.

## Narrative Findings (AI reviewer)

## Warnings

### WR-01: Span emission is at-least-once, not "exactly one" — duplicate spans double-count tokens/cost in Phoenix

**File:** `internal/controller/milestone_controller.go:553-577` (same pattern at `phase_controller.go:494-518`, `plan_controller.go:538-562`, `project_controller.go:1803-1833`; helper at `internal/controller/span_emission.go:69-74`)
**Issue:** The span is emitted *before* the marker is durably stamped, and the entry gate reads the marker off the informer-cache object (`ms.Status.MilestoneSpanEmittedUID != completedJob.Name`). Two duplicate windows exist:

1. **Stale-cache entry check.** A reconcile that runs after the marker patch landed on the API server but before the cache reflects it sees an empty marker and re-emits the span. The in-closure re-check of `latest` (line 567) only prevents a redundant *patch* — the duplicate *span* has already been exported by then.
2. **Marker-patch failure loop.** If `RetryOnConflict` exhausts (`RetryOnConflict` only retries Conflict errors; a Get from a stale cache can return the same stale ResourceVersion each attempt, and any non-conflict API error exits immediately), the handler returns an error, the reconcile requeues, and the whole block re-runs — emitting one more duplicate span per attempt, unbounded until the patch lands.

This is inherent to emit-then-mark (no transaction spans the exporter and etcd), but the comments claim "synthesize exactly one retroactive AGENT span" — an overstatement. The consequence is not cosmetic: Phoenix sums `llm.token_count.*` across spans for cost views, so each duplicate double-counts that dispatch's tokens/cost — the exact metric this phase exists to make trustworthy.
**Fix:** Narrow window 1 and correct the contract documentation:
```go
// Before emitting, re-check the marker on an API-fresh read inside the same
// guarded path (one extra GET per completion, only until the marker lands):
latest := &tideprojectv1alpha3.Milestone{}
if err := r.Get(ctx, client.ObjectKeyFromObject(ms), latest); err == nil &&
    latest.Status.MilestoneSpanEmittedUID == completedJob.Name {
    // marker already durable — skip emission entirely
} else if synthesizePlannerSpan(...) { ... }
```
(Reads via `r.Client` are still cache-backed; for a truly fresh check use the APIReader, or accept the narrowed window.) Independently, change the comments from "exactly one" to "at-least-once, marker-gated" so downstream consumers (and future reviewers) don't build on a guarantee the code cannot provide.

### WR-02: Marker gates by Job name, not Job UID — violates D-02's "each retry attempt produces its own span" and the field's own name

**File:** `internal/controller/milestone_controller.go:560,571` (same at `phase_controller.go:501,512`, `plan_controller.go:545,556`, `project_controller.go:1816,1827`; field defs `api/v1alpha3/milestone_types.go:69-75`, `phase_types.go:65-71`, `plan_types.go:121-128`, `project_types.go:518-526`)
**Issue:** The durable marker stores `completedJob.Name` and the emission gate compares names. But 42-CONTEXT.md D-02 is explicit: "**One span per Job attempt** … Retries (reject/re-plan, `resume --retry-failed`) each produce their own span," and the phase decision record frames the gate as "one per Job UID." Planner Job names are deterministic (`tide-<level>-<parentUID>-<attempt>`) and **attempt is hardcoded to `1` at all four dispatch sites** (`milestone_controller.go:396`, `phase_controller.go:366`, `plan_controller.go:386`, `project_controller.go:1688`). Any path that deletes and recreates a planner Job therefore reuses the exact same name with a new Job UID — and its completion span is silently suppressed forever, because the marker still matches. Today no in-controller path recreates a planner Job (the milestone dispatch site tags retry semantics as future work, "CR-NN for retry semantics"), so the defect is latent — but it detonates precisely when the retry semantics D-02 anticipates are implemented, and nothing will fail loudly when it does. The field name (`*SpanEmittedUID`) says UID; the stored value is a name; the CRD description says "is the name of the planner Job" — three artifacts, two contradicting the decision record.
**Fix:** Store the Job's actual UID — one-line change per level, honors D-02, and makes the field name truthful:
```go
if completedJob != nil && ms.Status.MilestoneSpanEmittedUID != string(completedJob.UID) {
    ...
    latest.Status.MilestoneSpanEmittedUID = string(completedJob.UID)
```
Update the four field doc comments + regenerated CRD descriptions to match. (The `*RolledUpUID` precedent also stores a name, but that marker deliberately hardcodes `-1` and guards a different, single-shot contract — it is not a reason to repeat the misnomer on a field whose decision record specifies per-attempt identity.)

### WR-03: Telemetry marker-patch failure blocks the core planning pipeline

**File:** `internal/controller/milestone_controller.go:573-575` (same at `phase_controller.go:514-516`, `plan_controller.go:558-560`, `project_controller.go:1829-1831`)
**Issue:** The span block sits *before* the reporter-Job spawn, budget rollup, planner-failure classification, and gate hooks. When the marker patch fails after retry exhaustion, the handler returns an error — so a failure in pure telemetry bookkeeping halts child materialization and gate evaluation for that reconcile. Transient failures self-heal on requeue, but a persistent status-patch failure (e.g. a webhook or RBAC regression on the status subresource) now wedges planning progression *and* emits one duplicate span per requeue attempt (WR-01 window 2) — the worst of both outcomes. The rollup marker's identical error-return (Phase 25 WR-03) was justified because that marker guards money (double-counted budget); this marker guards a duplicate telemetry span. The tradeoff was inherited with the skeleton rather than re-derived for the lower-stakes payload.
**Fix:** Either (a) move the span block after the reporter spawn so the pipeline's first-completion action always fires before telemetry bookkeeping can error out, or (b) keep placement but make the failure mode explicit in the comment: "marker-patch failure blocks this reconcile by design; duplicate span per retry is accepted." If duplicates are judged worse than blocking (per WR-01's cost-integrity argument), (b) is defensible — but it should be a recorded decision, not an artifact of copying the rollup skeleton.

### WR-04: Test fixture failure poisons the global TracerProvider — cascading panics across unrelated specs

**File:** `internal/controller/span_emission_test.go:131-141` (same pattern at 357-373, 545-567, 752-762)
**Issue:** `BeforeEach` calls `spanEmissionProject(...)` (which can fail its `Expect`) *before* capturing `prevTP = otel.GetTracerProvider()`. If the fixture fails, Ginkgo still runs `AfterEach`, which executes `otel.SetTracerProvider(prevTP)` with `prevTP` == nil. The otel global (`otel@v1.43.0/internal/global/state.go:70`) has no nil guard — it stores nil, and every subsequent `otel.Tracer(...)` call in the process panics on a nil-interface method call. Because `synthesizePlannerSpan` now runs inside every planner completion handler, *other* suites in this package that drive the same handlers (e.g. the child-rollup idempotency specs) would panic, burying the original fixture failure under a cascade of unrelated nil-pointer panics.
**Fix:** Capture and swap the provider as the *first* statements of `BeforeEach` (before any `Expect`), or nil-guard the restore:
```go
AfterEach(func() {
    if prevTP != nil {
        otel.SetTracerProvider(prevTP)
    }
    ...
})
```
The unit-test helper `setupSpanExporter` (`span_emission_unit_test.go:48-55`) already gets this ordering right — mirror it.

## Info

### IN-01: Plan level skips span emission entirely on the nil-EnvReader path — cross-level behavioral drift

**File:** `internal/controller/plan_controller.go:516-524`
**Issue:** The plan handler returns early when `r.Deps.EnvReader == nil`, before the span block. Milestone (`milestone_controller.go:549-551`), phase (`phase_controller.go:490-492`), and project (`project_controller.go:1799-1801`) log and fall through, emitting a degraded span. The nil-EnvReader path is unit-test-only today, so production behavior is unaffected, but the four "identical pattern" ports are not actually identical — a future consolidation or test comparing levels will trip on this.
**Fix:** Either hoist the plan-level span block above the nil-EnvReader early return, or note the deliberate divergence in the plan-level comment.

### IN-02: Reject-park before span emission can permanently lose a span via TTL-GC

**File:** `internal/controller/milestone_controller.go:521-523` (same at `phase_controller.go:466-468`, `plan_controller.go:500-502`)
**Issue:** The reject short-circuit returns before the span block. If a project stays rejected past the completed planner Job's TTL window, resume reaches `handleJobCompletion(ctx, ms, nil)` — nil job, no span, ever. This is consistent with Pattern 3 (never fabricate timestamps), but the span-block comment documents the TTL-GC no-op path without mentioning the reject-park interaction that makes it reachable for a Job that *was* observable at completion time.
**Fix:** One sentence in the span-block comment: "a reject-park that outlives the Job's TTL loses this span permanently (Pattern 3: no fabricated timestamps)."

### IN-03: Four near-identical 20-line marker-patch blocks — extract a shared helper

**File:** `internal/controller/milestone_controller.go:560-577`, `phase_controller.go:501-518`, `plan_controller.go:545-562`, `project_controller.go:1816-1833`
**Issue:** The gate + emit + RetryOnConflict + re-fetch + optimistic-lock patch block is copy-pasted per level with only the type and field varying. `synthesizePlannerSpan` was correctly shared; the marker stamp was not. Any fix to WR-01/WR-02/WR-03 must now be applied four times, which is exactly how per-level drift (IN-01) happens.
**Fix:** A shared `stampSpanEmittedMarker(ctx, c, obj, getMarker func() string, setMarker func(string), jobKey string) error` alongside `synthesizePlannerSpan` in span_emission.go collapses the four blocks to one call each.

### IN-04: stripGoComments treats `//` inside string literals as a comment — latent blind spot in the ATTR-03 guard

**File:** `pkg/otelai/attrs_test.go:264-291`
**Issue:** The helper starts a line-comment at any `//`, including inside a double-quoted string. If attrs.go ever gains a line where a string containing `//` (e.g. a URL constant) precedes a hand-rolled spec-family literal, everything after the `//` on that line is stripped and `TestKeysUseSemconvModule` silently misses the violation. The doc comment acknowledges the backtick-string limitation but not this one.
**Fix:** Extend the caveat comment to cover `//` inside quoted strings, or skip over double-quoted string literals in the scanner loop (a ~6-line addition matching the existing style).

### IN-05: Handler-level degraded-envelope coverage exists only at the milestone level

**File:** `internal/controller/span_emission_test.go:302-337`
**Issue:** The envtest "degraded envelope still emits" spec exists for milestone only; phase, plan, and project rely on the shared helper's unit tests. The helper is shared, but the *wiring* (envelope read → `envReadOK` → span block) is quadruplicated inline per controller — and IN-01 proves the wirings already diverge. Per-level degraded specs (or fixing IN-03 so a single wiring exists) would catch that class of drift.
**Fix:** Add the ~30-line degraded spec to the phase/plan/project Describes, or deduplicate the wiring per IN-03 and keep single-level coverage.

---

_Reviewed: 2026-07-15T23:52:07Z_
_Reviewer: Claude (gsd-code-reviewer)_
_Depth: standard_

---

## Fix Disposition (2026-07-16, post-review --fix pass)

All 4 WARNING findings fixed and verified on `main` (13/13 SpanEmission envtest specs + RollupIdempotency regression guard green):

| Finding | Outcome | Commit |
|---------|---------|--------|
| WR-01 (at-least-once duplicate emission) | fixed — mark-then-emit ordering, `plannerSpanResolvable` predicate; duplicates impossible, at-most-once documented | `c936762` + `9cae6bb` |
| WR-02 (marker stored Job name, not UID) | fixed — `string(completedJob.UID)` at all four levels; CRDs regenerated | `4cc9f68` |
| WR-03 (marker-patch failure blocked pipeline) | fixed — log-and-continue degrade; reporter/rollup/gates always proceed | `9b9b396` |
| WR-04 (nil TracerProvider restore in tests) | fixed — capture/swap before failable fixture steps | `b4b15f2` |

The 5 INFO findings remain open (documented above) — candidates for a later cleanup pass.
