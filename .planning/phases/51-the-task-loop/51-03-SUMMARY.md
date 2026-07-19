---
phase: 51-the-task-loop
plan: 03
subsystem: verification-loop
tags: [prompt-template, go-embed, openinference, otel, evaluator-span, coverage-not-conservatism, gate-decision, sibling-span]

# Dependency graph
requires:
  - phase: 51-the-task-loop (plan 01)
    provides: VerificationSpec CRD schema (spec.verification), TaskStatus.LoopStatus
  - phase: 51-the-task-loop (plan 02)
    provides: SelfInstruments("langgraph")=true, EnvelopeOut.Verdict now a real Python-produced GateDecision
  - phase: 50 (execution-loop hardening + loop-native observability)
    provides: otelai.EvaluationAttributes/HumanIntervention (defined-but-empty), otelai.LoopAttributes/LoopKindExecution, synthesizePlannerSpan's trace spine (TraceIDFromUID + Remote SpanContext + otel.Tracer("tide.dispatch"))
provides:
  - "task_verifier.tmpl loadable via LoadPromptTemplate(\"verifier\",\"task\") — role=verifier coverage-not-conservatism content, PromptTemplateVersion co-bumped v1->v2"
  - "otelai.EvaluatorInvocation(system,name,role,level) — the EVALUATOR-kind sibling of AgentInvocation, routed through semconv.SpanKindEvaluator"
  - "otelai.LoopKindEvaluator — the loop.kind value for the Task loop's EVALUATOR span, distinct from LoopKindExecution"
  - "internal/controller.synthesizeEvaluatorSpan — standalone EVALUATOR-span emitter, sibling-parented to the checked level's AGENT span, populating evaluation.result/evaluation.version/human_intervention; no live call site yet"
affects: [51-06, 51-07]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Sibling-span parenting: synthesizeEvaluatorSpan is given the SAME parentSpanID the AGENT span's own synthesizePlannerSpan call received (never the AGENT span's own minted SpanID) — proven in tests by asserting both spans' Parent.SpanID() match and neither equals the other's own SpanContext.SpanID()"
    - "Coverage-not-conservatism prompt directive: the finding-coverage instruction is framed positively (report every deviation with severity+confidence; policy alone decides what blocks) rather than as a 'don't over-report' constraint, per the Opus-4.8 tuning note in CLAUDE.md"
    - "human_intervention gated on the escalation terminal only (VerdictBlocked) — never stamped for APPROVED, REPAIRABLE, or a nil (degraded) verdict"

key-files:
  created:
    - internal/subagent/common/templates/task_verifier.tmpl
  modified:
    - internal/subagent/common/prompt_templates.go
    - internal/subagent/common/prompt_templates_test.go
    - pkg/otelai/attrs.go
    - pkg/otelai/attrs_test.go
    - internal/controller/span_emission.go
    - internal/controller/span_emission_unit_test.go

key-decisions:
  - "human_intervention is stamped only when out.Verdict.Verdict == VerdictBlocked (the escalation terminal, ESC-02/ESC-03) — never for APPROVED, REPAIRABLE, or a nil Verdict on a degraded envelope. Not specified verbatim by the plan; this is the narrowest reading of the doc-comment population contract that avoids fabricating the marker ahead of a real escalation."
  - "The evaluator-span unit tests landed in span_emission_unit_test.go, not span_emission_test.go as the plan's files_modified listed — see Deviations."
  - "synthesizeEvaluatorSpan's span name is \"tide.dispatch.<level>.verify\" (not a reuse of the AGENT span's own \"tide.dispatch.<level>\" name), so the two sibling spans are trivially distinguishable by name alone in addition to openinference.span.kind."

patterns-established:
  - "EVALUATOR-span emission mirrors AGENT-span emission field-for-field (trace spine, degraded-envelope marker, FailureDetail-on-fail, tracing-dark not-emitted guard) but swaps AgentInvocation->EvaluatorInvocation and adds EvaluationAttributes/HumanIntervention — the pattern Plan 07's call site will reuse verbatim."

requirements-completed: [EVAL-04, OBS-03]

# Metrics
duration: 7min (first to last task commit, 08:42:55 -> 08:49:03 local)
completed: 2026-07-19
---

# Phase 51 Plan 03: Task-Loop Observability + Prompt Primitives Summary

**Landed the two independent primitives the Task loop's verify cycle consumes: `task_verifier.tmpl` (a coverage-not-conservatism Go prompt template, no Python port) and a standalone `synthesizeEvaluatorSpan` emitter that stamps an OpenInference `EVALUATOR`-kind span as a sibling of the checked level's `AGENT` span, first-populating the `evaluation.*`/`human_intervention` keys Phase 50 defined empty.**

## Performance

- **Duration:** ~7 min (08:42:55 -> 08:49:03 local, first to last task commit)
- **Tasks:** 3/3 completed
- **Files modified:** 7 (1 created, 6 modified)

## Accomplishments
- `internal/subagent/common/templates/task_verifier.tmpl` created — loads via the existing `LoadPromptTemplate("verifier","task")` convention with zero new loader machinery; instructs the evaluator to run the gate command for real (a non-zero exit dominates any self-reported verdict), confirm required artifacts, and report a finding for EVERY deviation with explicit severity + confidence — never "be conservative" or "only high-severity" (grep-verified absent)
- `PromptTemplateVersion` bumped `v1` -> `v2` in the same commit as the template addition (the file's own MAINTENANCE RULE), so `RunEvidence.PromptVersion` cross-attempt comparison never silently compares against a stale prompt
- `otelai.EvaluatorInvocation` added — the `EVALUATOR`-kind sibling of `AgentInvocation`, routed through the pinned `openinference-semantic-conventions` module's `semconv.SpanKindEvaluator` const (never a hand-rolled `"EVALUATOR"` literal — `TestKeysUseSemconvModule` stays green)
- `otelai.LoopKindEvaluator` added (`"evaluator"`), distinct from `LoopKindExecution`
- `internal/controller.synthesizeEvaluatorSpan` added — structured after `synthesizePlannerSpan` (identical trace spine: `otelai.TraceIDFromUID` + `trace.NewSpanContext{Remote:true}` + `otel.Tracer("tide.dispatch")`), but parented on the SAME `parentSpanID` the checked level's AGENT span received (a sibling, never a child), populating `evaluation.result`/`evaluation.version` (the first real consumer of `EvaluationAttributes` since Phase 50 defined it empty) and stamping `human_intervention` only on the `VerdictBlocked` escalation terminal
- 6 new unit tests in `span_emission_unit_test.go` genuinely proving: nil-job/nil-project skip posture, `EVALUATOR` kind + attribute population, sibling parenting against a real `AGENT` span from the same `parentSpanID` (with an explicit assertion that the EVALUATOR span is NOT parented on the AGENT span's own minted SpanID), `human_intervention` gating across all four verdict states, and degraded-envelope behavior

## Task Commits

Each task was committed atomically:

1. **Task 1: task_verifier.tmpl (coverage-not-conservatism) + PromptTemplateVersion bump** - `772d9c2d` (feat)
2. **Task 2: EvaluatorInvocation + LoopKindEvaluator (pkg/otelai)** - `f5ffe2f6` (feat)
3. **Task 3: synthesizeEvaluatorSpan emitter (sibling to the AGENT span)** - `72e5cfb1` (feat)

**Plan metadata:** commit pending (this SUMMARY + STATE/ROADMAP update)

## Files Created/Modified
- `internal/subagent/common/templates/task_verifier.tmpl` - New role=verifier prompt template, coverage-not-conservatism content
- `internal/subagent/common/prompt_templates.go` - `PromptTemplateVersion` v1->v2; `LoadPromptTemplate` doc comment gains the sixth (role,level) combo
- `internal/subagent/common/prompt_templates_test.go` - verifier/task added to the happy-path table; coverage-not-conservatism content assertion; version-bump regression guard
- `pkg/otelai/attrs.go` - `EvaluatorInvocation` helper; `LoopKindEvaluator` const
- `pkg/otelai/attrs_test.go` - `TestEvaluatorInvocation`, `TestLoopKindEvaluator`
- `internal/controller/span_emission.go` - `synthesizeEvaluatorSpan` emitter
- `internal/controller/span_emission_unit_test.go` - 6 new tests (see Deviations for why this file, not `span_emission_test.go`)

## Decisions Made
- `human_intervention` is stamped only when `out.Verdict.Verdict == VerdictBlocked` — the escalation terminal that hands the attempt to a human (ESC-02/ESC-03) — never for `APPROVED`, `REPAIRABLE`, or a nil `Verdict` (degraded envelope). The plan's must-haves said the emitter must populate `human_intervention` but did not specify the trigger condition; `VerdictBlocked`-only is the narrowest reading that avoids fabricating the marker ahead of a real escalation, consistent with the file's existing "absent when not applicable, never a fabricated empty" enrichment discipline (46 OBS-02/OBS-03).
- `synthesizeEvaluatorSpan`'s span name is `"tide.dispatch.<level>.verify"`, distinct from the AGENT span's `"tide.dispatch.<level>"` — the two sibling spans are then distinguishable by name alone in addition to `openinference.span.kind`, matching how Phoenix/LangSmith typically group same-trace siblings.
- One test deliberately drives `synthesizeEvaluatorSpan` with `level="plan"` (not `"task"`) and a non-zero `ProviderDefaults` literal, and asserts the real `sampled` return value — this both proves the emitter is level-agnostic (Phase 52 parameterizes the same loop contract per level) and satisfies `golangci-lint`'s `unparam` check, which otherwise flags `level`/`helmDefaults`/the second return value as apparently-constant given this plan's standalone (no production caller yet) state.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking issue] Evaluator-span unit tests placed in `span_emission_unit_test.go`, not `span_emission_test.go` as the plan's `files_modified` listed**
- **Found during:** Task 3, before writing the test
- **Issue:** The plan's acceptance criterion is `go test ./internal/controller/... -run 'EvaluatorSpan|SpanEmission' -count=1` exits 0 "(asserts EVALUATOR kind + sibling parent)." `internal/controller`'s only Ginkgo entry point is `func TestControllers(t *testing.T)` (confirmed via grep) — a `-run` pattern that doesn't match that literal name runs zero Ginkgo specs even if they exist in `span_emission_test.go` (the exact vacuous-pass trap 51-01-SUMMARY.md and this plan's own critical-reminders flag). `span_emission_unit_test.go` is the repo's own documented, pre-existing home for exactly this class of test — its header comment states plain `testing.T` functions on pure/K8s-object inputs "run without pulling in the package's Ginkgo BeforeSuite... via `go test ./internal/controller/ -run 'TestSpanEndTime|TestSynthesizePlannerSpan'`" — and `synthesizePlannerSpan`'s own analogous tests already live there, not in the envtest file.
- **Fix:** Added the 6 new `synthesizeEvaluatorSpan` tests as plain `testing.T` functions in `span_emission_unit_test.go`, following that file's exact structure (`setupSpanExporter`, `spanEmissionFixtureProject`, `attrValue`, `mustTime`). Confirmed the plan's literal acceptance command genuinely executes them (not vacuously): `go test ./internal/controller/... -run 'EvaluatorSpan|SpanEmission' -count=1` completes in <1s (no envtest API server spin-up) and `-v` output shows all 6 `RUN`/`PASS` lines.
- **Files modified:** `internal/controller/span_emission_unit_test.go` (instead of `internal/controller/span_emission_test.go`)
- **Verification:** `go test ./internal/controller/... -run 'EvaluatorSpan|SpanEmission' -count=1 -v` — 6/6 PASS, non-vacuous (confirmed via `-v` RUN lines and sub-second wall time). Full `go build ./...`, `go vet ./internal/controller/...`, and `./bin/golangci-lint run` (0 issues) also pass.
- **Committed in:** `72e5cfb1` (Task 3 commit)

---

**Total deviations:** 1 auto-fixed (Rule 3 — a blocking mismatch between the plan's literal file target and what its own acceptance command actually requires to run genuinely)
**Impact on plan:** No scope creep — the fix stays entirely within the "add a unit test for `synthesizeEvaluatorSpan`" task boundary; it only changes which of the package's two existing test files the new tests live in, matching an established, documented repo convention.

## Issues Encountered

None beyond the deviation above.

## User Setup Required

None - no external service configuration required.

## Next Phase Readiness
- `task_verifier.tmpl` is ready for Plan 06 (rendering it at verifier-dispatch time with a populated `EnvelopeIn.Verify`).
- `synthesizeEvaluatorSpan` is ready for Plan 07's verifier-completion call site — it is fully unit-tested but has zero production callers today, exactly as this plan's objective specified.
- No blockers. `go test ./internal/subagent/common/... ./pkg/otelai/... ./internal/controller/... -run 'Template|Verifier|Evaluator|SpanEmission|SelfInstruments' -count=1` and `make lint` both pass clean.

---
*Phase: 51-the-task-loop*
*Completed: 2026-07-19*

## Self-Check: PASSED

All 7 created/modified files confirmed present on disk; all 3 task commits (`772d9c2d`, `f5ffe2f6`, `72e5cfb1`) confirmed in `git log`.
