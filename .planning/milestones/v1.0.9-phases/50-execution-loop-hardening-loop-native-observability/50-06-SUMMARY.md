---
phase: 50-execution-loop-hardening-loop-native-observability
plan: 06
subsystem: observability
tags: [go, controller, opentelemetry, dispatch-envelope, fail-closed, tdd]

# Dependency graph
requires:
  - phase: 50-execution-loop-hardening-loop-native-observability
    plan: 01
    provides: "TerminalReason enum + LoopRunID/AttemptID fields on EnvelopeIn/EnvelopeOut this plan stamps and synthesizes"
  - phase: 50-execution-loop-hardening-loop-native-observability
    plan: 02
    provides: "otelai.LoopAttributes/LoopKindExecution helper this plan calls from synthesizePlannerSpan"
provides:
  - "buildEnvelopeIn stamps LoopRunID/AttemptID onto EnvelopeIn at Task dispatch (D-01), derived from task.UID + attempt — never minted or persisted"
  - "synthesizeNoEnvelopeOut(task, completedJob) — the controller-side cap_exceeded producer for wall-clock (ActiveDeadlineSeconds) Job kills that never wrote out.json, fail-closed on every other failure reason"
  - "synthesizePlannerSpan stamps the 6 loop.* attributes on the Task AGENT span, gated on out.AttemptID != \"\", evaluation.*/human_intervention left unset"
affects: [51-task-loop]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Fail-closed controller-side classification (mirrors ClassifyVerdict): synthesizeNoEnvelopeOut maps ONLY JobFailed reason DeadlineExceeded to cap_exceeded, every other reason stays unset rather than guessed"
    - "Absent-when-empty span-attribute gating: otelai.LoopAttributes is called only when out.AttemptID != \"\", so planner-level (unstamped) dispatches never fabricate empty loop.* values"

key-files:
  created: []
  modified:
    - internal/controller/task_controller.go
    - internal/controller/task_controller_extracted_test.go
    - internal/controller/span_emission.go
    - internal/controller/span_emission_unit_test.go

key-decisions:
  - "Test extensions landed in task_controller_extracted_test.go and span_emission_unit_test.go, not the plan's literally-named task_controller_test.go/span_emission_test.go — grep for the actual buildEnvelopeIn/synthesizePlannerSpan plain-testing.T idiom found these are the correct existing homes (span_emission_test.go is Ginkgo/envtest-only); this is a file-location clarification, not a scope or behavior change."
  - "The ConditionFailed condition's Reason stays exactly \"EnvelopeReadFailed\" on the wall-clock-kill path; only the Message gains the cap diagnostic prefix, per the plan's explicit scope fence (wave/failure semantics untouched)."
  - "synthesizeNoEnvelopeOut always stamps LoopRunID/AttemptID from task.UID + task.Status.Attempt regardless of classification outcome — span identity must survive envelope loss even when TerminalReason stays unset."

requirements-completed: [EXEC-01, EXEC-02, OBS-01]

# Metrics
duration: 10min
completed: 2026-07-19
---

# Phase 50 Plan 06: Controller-Side Loop Identity, cap_exceeded Synthesis, and AGENT-Span Stamping Summary

**Wires the controller half of the loop-identity + terminal-reason contract: `buildEnvelopeIn` stamps `LoopRunID`/`AttemptID` from `Task.Status.Attempt` + `TaskUID`, `handleJobCompletion`'s `EnvelopeReadFailed` branch synthesizes `cap_exceeded` for wall-clock-killed Jobs via a new `synthesizeNoEnvelopeOut`, and `synthesizePlannerSpan` stamps the 6 `loop.*` attributes on the Task AGENT span via `otelai.LoopAttributes`.**

## Performance

- **Duration:** 10 min (commit-to-commit)
- **Started:** 2026-07-19T01:11:19-04:00 (first task commit)
- **Completed:** 2026-07-19T01:20:54-04:00
- **Tasks:** 3 (all `tdd="true"`, each RED→GREEN)
- **Files modified:** 4

## Accomplishments
- `buildEnvelopeIn`'s previously-discarded `attempt int` parameter is now used: `EnvelopeIn.LoopRunID = string(task.UID)`, `EnvelopeIn.AttemptID = fmt.Sprintf("%s-%d", task.UID, attempt)` — the same tuple `podjob.JobName` derives the per-attempt Job name from, re-derived not minted or persisted (D-01).
- `synthesizeNoEnvelopeOut(task, completedJob) pkgdispatch.EnvelopeOut` — a new pure function covering the controller-half of RESEARCH Open Question 1: the ONLY place a wall-clock `ActiveDeadlineSeconds` Job kill can ever be classified as `cap_exceeded`, since the SIGKILLed pod never wrote `out.json`. Maps exactly `JobFailed` condition `Reason == "DeadlineExceeded"` to `TerminalReasonCapExceeded`; every other failure reason leaves `TerminalReason` unset (fail-closed, never guessed). Always stamps `LoopRunID`/`AttemptID` from `task.UID`/`task.Status.Attempt` regardless of classification outcome, so span identity survives envelope loss.
- `handleJobCompletion`'s `EnvelopeReadFailed` branch now passes the synthesized envelope into `emitTaskSpanOnce` instead of a bare `pkgdispatch.EnvelopeOut{}`; the `ConditionFailed` condition's `Reason` stays exactly `"EnvelopeReadFailed"` (wave semantics untouched) — only the `Message` gains the cap diagnostic prefix when classified.
- `synthesizePlannerSpan` stamps `otelai.LoopAttributes(...)` on the AGENT span, gated on `out.AttemptID != ""` — planner-level dispatches (never stamped this phase, D-01) correctly carry zero `loop.*` attributes rather than fabricated empties. `loop.candidate_version` sources from `out.Git.HeadSHA` (empty when `out.Git` is nil); `loop.exit_reason` is `out.TerminalReason` verbatim (D-02b, one source of truth). `evaluation.*`/`human_intervention` remain unstamped (Phase 51's domain) and no `otelai.TokenCount` call was added anywhere (46 D-03 preserved).

## Task Commits

Each task followed the RED/GREEN TDD cycle:

1. **Task 1 (RED): buildEnvelopeIn D-01 identity stamping test** - `be791c9a` (test)
2. **Task 1 (GREEN): stamp LoopRunID/AttemptID on EnvelopeIn** - `db7e4782` (feat)
3. **Task 2 (RED): synthesizeNoEnvelopeOut cap_exceeded synthesis test** - `c7b204b2` (test)
4. **Task 2 (GREEN): synthesize cap_exceeded for wall-clock-killed envelope-less Jobs** - `c08af5dc` (feat)
5. **Task 3 (RED): AGENT-span loop.* stamping test** - `5af0041b` (test)
6. **Task 3 (GREEN): stamp loop.* on the Task AGENT span** - `ad02571f` (feat)
7. **Style fix: wrap a >120-char test assertion line** - `041555c7` (style)

**Plan metadata:** pending (this SUMMARY's own commit)

## Files Created/Modified
- `internal/controller/task_controller.go` - `buildEnvelopeIn` uses `attempt` to stamp `LoopRunID`/`AttemptID`; new `synthesizeNoEnvelopeOut` function; `handleJobCompletion`'s `EnvelopeReadFailed` branch wires the synthesized envelope through `emitTaskSpanOnce` and prefixes the condition Message on cap classification
- `internal/controller/task_controller_extracted_test.go` - extended `TestBuildEnvelopeIn_PromptPath` with 2 D-01 identity subtests; new `TestSynthesizeNoEnvelopeOut` (DeadlineExceeded → cap_exceeded, BackoffLimitExceeded → unset)
- `internal/controller/span_emission.go` - `synthesizePlannerSpan` calls `otelai.LoopAttributes` gated on `out.AttemptID != ""`
- `internal/controller/span_emission_unit_test.go` - 3 new tests: full loop.* population, absent-when-empty, token-count omission preserved

## Decisions Made
- Test file targets deviate from the plan's literal `task_controller_test.go`/`span_emission_test.go` filenames — the actual `buildEnvelopeIn`/`synthesizePlannerSpan` plain-`testing.T` idiom (no envtest cluster needed) lives in `task_controller_extracted_test.go` and `span_emission_unit_test.go`; `span_emission_test.go` is exclusively Ginkgo/envtest specs requiring a live k8sClient. Confirmed via grep before writing tests, matching the plan's own `read_first` instruction to "locate via grep first."
- `synthesizeNoEnvelopeOut`'s doc comment states explicitly it is the sole `cap_exceeded` producer for wall-clock kills, and that `harness.CheckCaps` (Plan 50-04) is the separate in-pod producer for iteration/token caps — kept the two producers' responsibility boundary documented at the code site, not just in RESEARCH.md.
- One >120-char test assertion line was wrapped in a small follow-up `style` commit rather than folded into the Task 2 GREEN commit, keeping each task's diff minimal and reviewable.

## Deviations from Plan

### Auto-fixed Issues

None functionally — all three tasks executed exactly to the plan's `<action>` specification. The test-file-location adjustment (documented above under "Decisions Made") is a Rule 3-style clarification (the plan's own `read_first` instructed locating the correct file via grep, and grep confirmed a different existing file than the plan's `<files>` list named), not a scope or behavior deviation — no functionality changed as a result.

## Issues Encountered
None.

## User Setup Required
None - no external service configuration required.

## Next Phase Readiness
- Dispatch-time identity stamping, cap_exceeded synthesis, and AGENT-span loop attributes are all locked and envtest-green — Plan 50-07 (LLM-span subset stamping via `EmitSpans`) can proceed independently.
- Scope fence confirmed intact: `grep -rn "VerifyHalt" --include="*.go" .` returns 0 hits; `git diff --stat internal/controller/failure_halt.go` shows no changes; `grep -n '"EnvelopeReadFailed"' internal/controller/task_controller.go` still shows the condition Reason unchanged.
- `go build ./...`, `go vet ./...`, `go test ./internal/controller/...` (full envtest package, 126s), and `make lint` (golangci-lint + import firewalls) all verified green after every task and again at plan close.
- Phase 51's `ConditionVerifyHalt`/verifier dispatch/EVALUATOR span work builds on a settled envelope+span contract: `loop.kind`/`loop.run_id`/`loop.parent_run_id`/`loop.iteration`/`loop.candidate_version`/`loop.exit_reason` are populated end-to-end from dispatch through Job completion; `evaluation.*`/`human_intervention` are defined (Plan 50-02) but deliberately still unpopulated, ready for Phase 51's evaluator to stamp.

---
*Phase: 50-execution-loop-hardening-loop-native-observability*
*Completed: 2026-07-19*

## Self-Check: PASSED

All modified files and task commit hashes verified present on disk / in `git log --oneline --all`.
