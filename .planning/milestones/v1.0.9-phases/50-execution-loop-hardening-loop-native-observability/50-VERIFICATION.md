---
phase: 50-execution-loop-hardening-loop-native-observability
verified: 2026-07-19T06:11:26Z
status: passed
score: 5/5 success criteria verified (6/6 requirements satisfied)
overrides_applied: 0
re_verification:
  previous_status: none
  note: initial verification
---

# Phase 50: Execution-Loop Hardening + Loop-Native Observability Verification Report

**Phase Goal:** The in-Job execution loop (a pipeline stage, not a loop) produces machine-checkable run evidence and emits the loop-native trace/metric attributes the Task loop will consume — before the Task loop is built on top of it.
**Verified:** 2026-07-19T06:11:26Z
**Status:** passed
**Re-verification:** No — initial verification

## Goal Achievement

### Observable Truths (ROADMAP Success Criteria)

| # | Truth | Status | Evidence |
|---|-------|--------|----------|
| 1 | Every attempt carries a stable `loopRunID` + `attemptID` and emits a span per tool/action iteration | ✓ VERIFIED | `buildEnvelopeIn` derives `LoopRunID=string(task.UID)`, `AttemptID=fmt.Sprintf("%s-%d", task.UID, attempt)` (task_controller.go:1866-1867) from the SAME `task.UID`+`task.Status.Attempt` tuple `podjob.JobName` uses (line 620) — never minted/persisted. Echo: `EnvelopeIn`+`EnvelopeOut` both carry the fields (envelope.go:116/125/226/230); executor echoes verbatim. Per-call LLM spans stamp `otelai.LoopRunID(attemptID)` + 1-indexed `otelai.LoopIteration(i+1)` in `EmitSpans` (tracesynth.go:683-686), conditional on non-empty attemptID. |
| 2 | The result envelope carries an explicit terminal reason (`completed\|cap_exceeded\|blocked\|tool_failure\|invalid_output`) — never a silent default | ✓ VERIFIED | `TerminalReason` typed enum + 5 consts + `Valid()` with no default-true case; zero value is invalid sentinel (terminal_reason.go). `writesite_guard_test.go` is an AST guard over the 3 real write sites (cmd/claude-subagent, internal/subagent/anthropic, cmd/stub-subagent) asserting every populated `EnvelopeOut{}` literal sets `TerminalReason`, plus an inventory-floor guard so the check can't be gamed by literal removal. Field carries NO omitempty (unset "" visible on wire). Live test: `go test ./pkg/dispatch/...` PASS. |
| 3 | The result envelope satisfies the run-evidence contract (evals/README.md), referenced not re-derived | ✓ VERIFIED | `RunEvidence` maps all 7 canonical contract items 1:1 (run_evidence.go): SpecID+LockingCommit (Task/Spec IDs+locking commit), Commands+EvaluatorVersions, ChangedFiles (git `--name-status` manifest via `harness.ChangedFileManifest`, not diffs), Model/PromptVersion/RuntimeVersion; the rest referenced from `EnvelopeOut` (TaskUID, Usage+CompletedAt, Git.HeadSHA, Result, TerminalReason) with an explicit "REFERENCED, NOT RE-DERIVED" doc contract. `Bounded()` is the DoS control. Populated at write site (anthropic subagent.go:369-372 echoes Provider.Model + compiled-in PromptTemplateVersion + `claude --version` probe; claude-subagent merges changed-file manifest). |
| 4 | The completion field reports only agent-believed-completion — no field/code path stamps Task correctness (negative EXEC-04) | ✓ VERIFIED | `TerminalReasonCompleted` doc-comment states it is agent BELIEF, NON-AUTHORITATIVE for Task correctness (terminal_reason.go:33-37). `TestEnvelopeOut_NoCorrectnessField` negative guard: exhaustive struct literal + runtime JSON-key-absence over `taskCorrect/correctness/verified/approved/passed` (envelope_test.go:1155). No correctness field exists in `EnvelopeOut` today; controller exit-0→Complete path unchanged (scope fence). |
| 5 | Spans carry the 9 `loop.*`/`evaluation.*`/`human_intervention` keys + cost/duration/token; run IDs NEVER in a Prometheus label (cardinality test) | ✓ VERIFIED | All 9 keys as `pkg/otelai` helper consts (attrs.go:417-425), deliberately non-`tide.`-prefixed (documented cross-vendor convention). AGENT span stamps `otelai.LoopAttributes(...)` conditionally (span_emission.go:242-251); `evaluation.*`/`human_intervention` correctly DEFINED-BUT-EMPTY (Phase 51 populates — expected). `metriccardinality` analyzer rejects 9-name run-ID-shaped forbidden set incl. `task` (analyzer.go:47-57), wired into `cmd/tide-lint` (main.go:56). `wave_label_test.go` greps registry.go against the same list. No new metric (registry.go untouched — OBS-02 Q3 guard-only resolution). |

**Score:** 5/5 truths verified

### Required Artifacts

| Artifact | Expected | Status | Details |
|----------|----------|--------|---------|
| `pkg/dispatch/terminal_reason.go` | TerminalReason type + 5 consts + Valid() | ✓ VERIFIED | Fail-closed enum, zero value invalid |
| `pkg/dispatch/run_evidence.go` | RunEvidence + ChangedFile + bounds + Bounded() | ✓ VERIFIED | 1:1 contract map, references-only, DoS bound |
| `pkg/dispatch/envelope.go` | EnvelopeIn/Out/TerminationStub field extensions | ✓ VERIFIED | LoopRunID/AttemptID/TerminalReason/RunEvidence wired |
| `pkg/dispatch/testdata/envelope_out_golden.json` | shared Go+Python golden fixture | ✓ VERIFIED | Non-`completed` terminalReason + full RunEvidence + loop identity |
| `pkg/otelai/attrs.go` | 9 loop-native keys + helpers | ✓ VERIFIED | LoopAttributes/LoopRunID/LoopIteration/EvaluationAttributes/HumanIntervention |
| `tools/analyzers/metriccardinality/analyzer.go` | 9-name forbidden label set | ✓ VERIFIED | `task`+8 run-ID-shaped; wired into tide-lint |
| `internal/metrics/wave_label_test.go` | runtime source-grep guard | ✓ VERIFIED | Greps registry.go against full forbidden list |
| `internal/subagent/common/prompt_templates.go` | PromptTemplateVersion const | ✓ VERIFIED | `= "v1"` compiled-in |
| `internal/harness/commit.go` | ChangedFileManifest bounded helper | ✓ VERIFIED | `git show --name-status`, bounded by max |
| `pkg/dispatch/writesite_guard_test.go` | AST fail-closed guard over 3 write sites | ✓ VERIFIED | + inventory-floor anti-gaming |
| `internal/controller/task_controller.go` | buildEnvelopeIn identity + cap_exceeded synth | ✓ VERIFIED | synthesizeNoEnvelopeOut maps DeadlineExceeded→cap_exceeded only |
| `internal/controller/span_emission.go` | conditional LoopAttributes on AGENT span | ✓ VERIFIED | Gated on out.AttemptID; NO TokenCount (46 D-03 preserved) |
| `internal/controller/reporter_jobspec.go` | AttemptID/LoopRunID + Args threading | ✓ VERIFIED | Args-only (`--attempt-id=`), never Env — Phase 46 precedent |
| `cmd/tide-reporter/main.go` | --attempt-id/--loop-run-id flags → EmitSpans | ✓ VERIFIED | parseFlags → reporterConfig → EmitSpans |
| `internal/reporter/tracesynth.go` | EmitSpans loop identity + indexed iteration | ✓ VERIFIED | `for i, call := range calls`, no redundant CallSpan field |
| `cmd/tide-langgraph-verifier/verifier/envelope.py` | Python field mirror + golden constant | ✓ VERIFIED | terminal_reason/loop_run_id/attempt_id/run_evidence + changed_file_count; strictly <4096 |
| `cmd/tide-langgraph-verifier/verifier/tests/test_envelope.py` | golden parity + truncation + no-silent-default | ✓ VERIFIED | 16 tests PASS live (value-equivalence vs shared fixture) |

### Key Link Verification

| From | To | Via | Status | Details |
|------|----|----|--------|---------|
| envelope.go | terminal_reason.go | `TerminalReason TerminalReason` field | ✓ WIRED | envelope.go:221 (SDK regex false-negative on tab; manually confirmed) |
| envelope.go | run_evidence.go | `RunEvidence *RunEvidence` field | ✓ WIRED | envelope.go:289 (manually confirmed) |
| task_controller.go | envelope.go | LoopRunID/AttemptID in buildEnvelopeIn | ✓ WIRED | :1866-1867 |
| span_emission.go | attrs.go | `otelai.LoopAttributes` on AGENT span | ✓ WIRED | :247 (SDK false-negative; manually confirmed) |
| tracesynth.go | attrs.go | `otelai.LoopRunID`/`LoopIteration` on LLM spans | ✓ WIRED | :684-685 (SDK false-negative; manually confirmed) |
| subagent.go | harness/caps.go | `harness.CheckCaps` post-usage | ✓ WIRED | :415 → cap_exceeded |
| claude-subagent/main.go | harness/commit.go | `ChangedFileManifest` after commit | ✓ WIRED | :156/172 |
| reporter_jobspec.go | tide-reporter/main.go | `--attempt-id=`/`--loop-run-id=` Args | ✓ WIRED | :324-328 → parseFlags |
| test_envelope.py | golden JSON | ENVELOPE_OUT_GOLDEN_FIXTURE repo-root path | ✓ WIRED | envelope.py:270 |

*Note: 4 links flagged `verified:false` by `gsd-sdk query verify.key-links` were SDK regex-escaping false-negatives (`\s+` vs a single tab / dotted-call escaping). All 4 patterns confirmed present by direct grep against the real source.*

### Behavioral Spot-Checks (live test execution)

| Suite | Command | Result | Status |
|-------|---------|--------|--------|
| Envelope/terminal-reason/run-evidence/writesite guard | `go test ./pkg/dispatch/...` | ok | ✓ PASS |
| otelai loop helpers + semconv guard | `go test ./pkg/otelai/...` | ok | ✓ PASS |
| metriccardinality analyzer | `go test ./tools/analyzers/metriccardinality/...` | ok | ✓ PASS |
| wave_label cardinality guard | `go test ./internal/metrics/...` | ok | ✓ PASS |
| tracesynth loop-identity | `go test ./internal/reporter/...` | ok | ✓ PASS |
| ChangedFileManifest | `go test ./internal/harness/...` | ok | ✓ PASS |
| tide-reporter flags | `go test ./cmd/tide-reporter/...` | ok | ✓ PASS |
| claude-subagent write site | `go test ./cmd/claude-subagent/...` | ok | ✓ PASS |
| anthropic terminal reasons + CheckCaps | `go test ./internal/subagent/anthropic/...` | ok | ✓ PASS |
| controller span/identity/cap_exceeded | `go test ./internal/controller/... -run Span\|Loop\|EnvelopeIn\|CapExceeded\|Attempt` | ok 1.085s | ✓ PASS |
| Python envelope parity (Go↔Python duality) | `pytest verifier/tests/test_envelope.py` | 16 passed in 0.41s | ✓ PASS |

### Scope-Fence Verification (all correctly ABSENT — Phases 51/53)

| Fence | Check | Result | Status |
|-------|-------|--------|--------|
| No halt class | `grep -rn VerifyHalt --include='*.go'` | 0 hits | ✓ HELD |
| failure_halt.go untouched | not in any Phase-50 files_modified | confirmed | ✓ HELD |
| No langgraph SelfInstruments registration | vendor_capabilities.go | no langgraph, predicate untouched | ✓ HELD |
| No EVALUATOR-kind span | grep EVALUATOR internal/ pkg/ | comments-only (referencing Phase 51) | ✓ HELD |
| No TaskReconciler verifier dispatch | controller exit-0→Complete unchanged | confirmed | ✓ HELD |

### Requirements Coverage

| Requirement | Source Plan(s) | Description | Status | Evidence |
|-------------|----------------|-------------|--------|----------|
| EXEC-01 | 50-01, 50-04, 50-06, 50-07 | loopRunID+attemptID, span per iteration | ✓ SATISFIED | Truth 1; derived identity + per-call LLM span stamping |
| EXEC-02 | 50-01, 50-04, 50-05 | terminal-reason enum, never silent default | ✓ SATISFIED | Truth 2; enum + AST write-site guard |
| EXEC-03 | 50-01, 50-04, 50-05 | run-evidence contract, referenced-not-re-derived | ✓ SATISFIED | Truth 3; RunEvidence 1:1 map + Bounded() |
| EXEC-04 | 50-01 | belief-only, never stamps correctness | ✓ SATISFIED | Truth 4; negative guard test |
| OBS-01 | 50-02, 50-06, 50-07 | 9 loop.*/evaluation.* span keys + cost/duration/token | ✓ SATISFIED | Truth 5; otelai helpers + AGENT/LLM span stamping |
| OBS-02 | 50-03 | run IDs out of Prometheus labels, bounded labels | ✓ SATISFIED | Truth 5; analyzer (9-name) + wave_label guard, no new metric |

All 6 requirement IDs are mapped to Phase 50 in REQUIREMENTS.md and marked **Complete** in the traceability table (lines 106-109, 125-126). No orphaned requirements.

### Anti-Patterns Found

| File | Line | Pattern | Severity | Impact |
|------|------|---------|----------|--------|
| (none) | — | Zero TBD/FIXME/XXX/TODO/HACK/PLACEHOLDER across all 18 modified files | ✓ clean | — |
| pkg/dispatch/envelope_test.go | 1156-1158 | EXEC-04 guard's "compile-time exhaustive literal" comment over-claims: a Go keyed struct literal permits field omission, so adding a new field would NOT fail compilation as the comment asserts | ℹ️ Info | Test-comment nit only. The runtime JSON-key-absence layer IS the real enforcement, and no correctness field exists today — the EXEC-04 negative guarantee holds structurally. Not a phase-goal gap. |

### Human Verification Required

None. This phase is evidence-envelope + trace/metric plumbing — every deliverable is verifiable programmatically via code inspection and the test tiers (all run live and passing here, including the Python Go↔Python parity suite). The `evaluation.*`/`human_intervention` keys being defined-but-empty is the EXPECTED, documented Phase-51 boundary, not an untested behavior.

### Gaps Summary

No gaps. All 5 ROADMAP success criteria are observably true in the codebase, backed by substantive implementations (not stubs), real wiring (not orphaned artifacts), live data flow (not hollow), and passing test guards. All 6 requirements are satisfied and marked Complete. The scope fence holds on all five fronts (the deferred Phase-51/53 work is correctly absent). Zero debt markers. The single informational finding is a test-comment over-claim that does not affect the delivered EXEC-04 guarantee.

The Execution loop's evidence + observability contract is locked and guarded — the Task loop (Phase 51) can build on a settled envelope + span shape.

---

_Verified: 2026-07-19T06:11:26Z_
_Verifier: Claude (gsd-verifier)_
