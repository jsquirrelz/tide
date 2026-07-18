---
phase: 49-common-loop-contract-verdict-envelope-persistence-schema
verified: 2026-07-18T22:56:35Z
status: passed
score: 5/5 must-haves verified
overrides_applied: 0
re_verification:
  previous_status: none
  note: "Initial verification. 49-REVIEW.md (code review, not a verifier gate) found 0 critical / 2 warning / 5 info; the 2 warnings (WR-01 infinite-loop, WR-02 boundary parity) were fixed on main in dd71076b, IN-02/IN-03 test-parity in 8eb7bc7c — both confirmed present."
---

# Phase 49: Common Loop Contract + Verdict/Envelope/Persistence Schema Verification Report

**Phase Goal:** The `LoopPolicy`/`LoopStatus` shared API types, the `gate_decision` verdict schema, and the findings size×locality persistence contract are locked as shared, reusable primitives — before any halt-condition or reconciler logic is written on top of them.
**Verified:** 2026-07-18T22:56:35Z
**Status:** passed
**Re-verification:** No — initial verification

## Goal Achievement

This is a schema/contract-definition phase. All five ROADMAP success criteria were verified goal-backward against the actual source (not SUMMARY claims): the types/functions exist, are substantive, are wired (deepcopy generated, tests green), and the scope discipline (no Phase 50/51 reconciler/halt logic) holds. Both `make generate` and `make manifests` produce ZERO diff on a clean tree, and every Go + Python acceptance command was run and passed.

### Observable Truths

| # | Truth | Status | Evidence |
| --- | --- | --- | --- |
| 1 | LoopPolicy/LoopStatus exist as shared embeddable Go API types with five-element doc-comments; no generic `Loop` controller (SC1 / LOOP-01/02/03) | ✓ VERIFIED | `api/v1alpha3/loop_types.go`: `LoopPolicy`{MaxIterations,MaxDuration,BudgetCents,Autonomy,EvaluatorRef,EscalationPolicy} L37-73; `LoopStatus`{Iteration,ParentRunID,LastEvaluation,ExitReason,CostCents,Conditions} L92-127. Both type-godocs enumerate all five loop elements (goal/candidate/evaluator-feedback/repeat-policy/bounded-exit) L23-36, L75-91. `find internal/controller -iname '*loop*'` empty; no `LoopReconciler`/`Kind="Loop"`; no `kubebuilder:object:root` on the types. Not embedded into `TaskSpec`/`TaskStatus` (grep empty). `make generate` + `make manifests` → ZERO diff. |
| 2 | VerifyContext pointer on EnvelopeIn + matched Go+Pydantic GateDecision/Finding round-trip the shared golden fixture (SC2 / EVAL-03) | ✓ VERIFIED | `pkg/dispatch/verdict.go` `GateDecision`/`Finding`/`Verdict` (terminal set exactly APPROVED\|REPAIRABLE\|BLOCKED L31-42; Finding fields dimension/severity/confidence/evidence/suggestedFix L53-74; GateDecision.Summary L88). Python mirror `verifier/verdict.py` field-for-field with pydantic aliases. `EnvelopeIn.Verify *VerifyContext json:"verify,omitempty"` (envelope.go:158) + `VerifyContext` struct (:394-414). Python `EnvelopeIn.verify: dict\|None` (envelope.py:64). Golden fixture `pkg/dispatch/testdata/gate_decision_golden.json` (verdict=REPAIRABLE + full 5-field finding) read by Go `TestGateDecision_GoldenFixtureRoundTrip` (`os.ReadFile("testdata/...")`) AND Python `test_golden_fixture_round_trip` (`verdict.GOLDEN_FIXTURE.read_bytes()`, go.mod-anchored to the same file). |
| 3 | Empty / missing-verdict-field / malformed verdict classifies fail-closed to BLOCKED, never APPROVED, in both languages (SC3 / EVAL-03) | ✓ VERIFIED | Go `ClassifyVerdict` (verdict.go:102-118): 3 `return VerdictBlocked` branches (empty / malformed / default). Python `classify_verdict` (verdict.py:69-91): 3 `Verdict.BLOCKED` branches. Go `TestClassifyVerdict_FailsClosed` rows EmptyJSON/MissingVerdictField/Malformed + APPROVED control + `TestClassifyVerdict_UnrecognizedVerdictField`. Python `test_classify_verdict_fails_closed` mirrors row-for-row incl. REPAIRABLE + REJECTED controls (IN-03 fix). `go test ./pkg/dispatch/...` + `make test-langgraph-verifier` (65 passed) green. |
| 4 | Findings persist under size×locality: ≤4KB TerminationStub summary + small per-CRD status summary + task-findings staging plumbed (SC4 / EVAL-05) | ✓ VERIFIED | `TerminationStub` bounded fields GateDecision(enum string)/FindingsCount/HighSeverityCount (envelope.go:456-467), NO free-text Summary; `NewTerminationStub` nil-safe flatten `if out.Verdict != nil`. Go `TestNewTerminationStub_StaysSmall` populates 50 all-high-severity findings, asserts `< 4096` + FindingsCount==50. Python `write_termination_stub` strict `>= 4096` loop with terminating truncation (WR-01/WR-02 fix, dd71076b) + hang-proof + boundary regression tests. `LoopStatus.LastEvaluation` = bounded `EvaluationSummary` (decision+counts+completedAt), the small per-CRD status summary. `cmd/tide-push` `stageEnvelopeArtifacts` generalized via `strings.Cut(DestPrefix,"/")`: `task` kind stages `findings.json`-only (fail-closed if absent), else-branch (project/milestone/phase/plan) byte-identical. `collectStageEnvelopes` producer wiring deliberately NOT added — Phase 51 (documented scope; see Deferred). |
| 5 | A size test proves LoopStatus carries only current-iteration summary + exit reason, never accumulating history (SC5 / LOOP-03) | ✓ VERIFIED | `TestLoopStatus_NoForbiddenFields` (loop_types_test.go:136-157): compile-time `_ = LoopStatus{...}` literal naming every field (a history slice fails to compile) + runtime assertion that marshalled JSON contains no `previousEvaluations`/`evaluations`/`history` key. `LoopStatus.LastEvaluation` is a single `*EvaluationSummary` pointer, not a slice. `go test ./api/v1alpha3/...` green. |

**Score:** 5/5 truths verified

### Deferred Items

The ROADMAP SC4 wording references the full findings artifact staged "via the extended `collectStageEnvelopes`." This phase deliberately extended only the CONSUMER side (`tide-push` `stageEnvelopeArtifacts`); the PRODUCER side (`collectStageEnvelopes` gaining a Task entry) is scoped to Phase 51. This is documented in 49-04-PLAN.md scope-discipline, the SUMMARY, and the verification-focus brief ("deliberately NOT added (Phase 51) — this is correct scope, not a gap"). The persistence *contract* — the object of this phase — is locked; the producer wiring is a scheduled Phase-51 deliverable, not a Phase-49 gap.

| # | Item | Addressed In | Evidence |
|---|------|-------------|----------|
| 1 | `collectStageEnvelopes` producer entry for task/findings.json | Phase 51 | 49-04-PLAN.md "Scope discipline (LOCKED): Do NOT add a Task entry to collectStageEnvelopes … that is Phase 51's job"; grep of `internal/controller/artifact_push.go` confirms no task entry present |

### Required Artifacts

| Artifact | Expected | Status | Details |
| -------- | ----------- | ------ | ------- |
| `api/v1alpha3/loop_types.go` | LoopPolicy/LoopStatus/EvaluationSummary + 3 enums | ✓ VERIFIED | 224 lines, all types + five-element godocs present; deepcopy-generated (zero diff) |
| `api/v1alpha3/loop_types_test.go` | round-trip + LOOP-03 guard + embedder proof | ✓ VERIFIED | 205 lines; `TestLoopStatus_NoForbiddenFields`, `TestLoopContract_Embeddable`, round-trips green |
| `api/v1alpha3/zz_generated.deepcopy.go` | DeepCopy for the 3 new structs | ✓ VERIFIED | DeepCopy/DeepCopyInto for EvaluationSummary/LoopPolicy/LoopStatus present; `make generate` zero diff |
| `pkg/dispatch/verdict.go` | Verdict/Finding/GateDecision + fail-closed ClassifyVerdict | ✓ VERIFIED | 119 lines; imports only `encoding/json`; 3 BLOCKED branches |
| `pkg/dispatch/testdata/gate_decision_golden.json` | canonical cross-language fixture | ✓ VERIFIED | REPAIRABLE + 2 findings, all 5 fields populated on finding[0] |
| `pkg/dispatch/envelope.go` | VerifyContext + EnvelopeIn.Verify + EnvelopeOut.Verdict + bounded TerminationStub | ✓ VERIFIED | All present; TerminationStub has no free-text Summary; NewTerminationStub nil-safe flatten |
| `cmd/tide-push/main.go` | stageEnvelopeArtifacts task-kind generalization | ✓ VERIFIED | `strings.Cut(DestPrefix,"/")` kind branch; task=findings.json-only, else byte-identical |
| `verifier/verdict.py` | pydantic mirror + classify_verdict + go.mod-anchored golden path | ✓ VERIFIED | Field-for-field mirror; 3 BLOCKED branches; `_repo_root()` walks to go.mod |
| `verifier/envelope.py` | verify extraction + extended write_termination_stub | ✓ VERIFIED | Fail-closed isinstance guard on `verify`; strict `>=4096` truncation loop |
| `verifier/tests/test_verdict.py` | golden round-trip + fail-closed + WR regression | ✓ VERIFIED | Row-for-row parity with Go; hang-proof + boundary tests present |

### Key Link Verification

| From | To | Via | Status | Details |
| ---- | --- | --- | ------ | ------- |
| loop_types.go | controller-gen | package `+kubebuilder:object:generate=true` | ✓ WIRED | DeepCopyInto for LoopPolicy/LoopStatus/EvaluationSummary generated; `make generate` zero diff |
| verdict.go ClassifyVerdict | VerdictBlocked terminal | every non-exact branch returns BLOCKED | ✓ WIRED | 3 `return VerdictBlocked` (empty/malformed/default) |
| envelope.go NewTerminationStub | EnvelopeOut.Verdict | nil-safe pointer flatten | ✓ WIRED | `if out.Verdict != nil { … }` flattens enum + counts, never the array |
| test_verdict.py | pkg/dispatch/testdata/gate_decision_golden.json | go.mod-anchored path walk | ✓ WIRED | `verdict.GOLDEN_FIXTURE.read_bytes()`; same file Go reads |
| verdict.py classify_verdict | Verdict.BLOCKED | every non-exact branch returns BLOCKED (mirrors Go) | ✓ WIRED | 3 `return Verdict.BLOCKED` branches |
| tide-push stageEnvelopeArtifacts | DestPrefix first segment | `strings.Cut(es.DestPrefix, "/")` kind derivation | ✓ WIRED | task-kind branch + preserved else-branch |

### Data-Flow Trace (Level 4)

Not applicable in the runtime sense — this is a schema/contract phase with no dynamic-data-rendering artifacts. The equivalent trace (does real data flow through the shape?) is covered by the cross-language golden round-trip (a fully-populated verdict decodes identically in Go and Python) and the worst-case TerminationStub test (50 findings collapse to bounded counts under 4KB). Both flow real fixture data through the wiring and pass.

### Behavioral Spot-Checks

| Behavior | Command | Result | Status |
| -------- | ------- | ------ | ------ |
| Go loop/verdict/envelope/push tests | `go test ./api/v1alpha3/... ./pkg/dispatch/... ./cmd/tide-push/...` | 3 packages ok | ✓ PASS |
| Python verifier suite | `make test-langgraph-verifier` | 65 passed in 5.20s | ✓ PASS |
| DeepCopy in sync | `make generate && git diff --exit-code` | zero diff | ✓ PASS |
| Standalone types (no CRD embed) | `make manifests && git diff --exit-code -- config/crd` | zero CRD diff | ✓ PASS |
| Fail-closed empty/missing/malformed → BLOCKED (Go+Py) | test tables in both suites | all BLOCKED, only exact APPROVED reaches APPROVED | ✓ PASS |

### Probe Execution

Not applicable — this phase declares no `scripts/*/tests/probe-*.sh` and is not a migration/tooling phase. Verification used the phase's own `go test` / `make test-langgraph-verifier` / `make generate` / `make manifests` acceptance commands directly (all run in-process, results above).

### Requirements Coverage

| Requirement | Source Plan | Description | Status | Evidence |
| ----------- | ---------- | ----------- | ------ | -------- |
| LOOP-01 | 49-01 | LoopPolicy/LoopStatus as shared embedded API types, no generic Loop controller | ✓ SATISFIED | Truth 1; no Loop controller/Kind; embeddability proof green |
| LOOP-02 | 49-01 | Five-element loop test in type doc-comments | ✓ SATISFIED | Truth 1; both godocs enumerate all five elements |
| LOOP-03 | 49-01 | .status carries only current-iteration summary + exit reason, no history | ✓ SATISFIED | Truth 5; compile-time NoForbiddenFields guard + LastEvaluation is a pointer not slice |
| EVAL-03 | 49-02, 49-03 | Matched Go+Pydantic gate_decision verdict schema, fail-closed | ✓ SATISFIED | Truths 2+3; shared golden round-trips both langs; 3-shape fail-closed regression both langs |
| EVAL-05 | 49-02, 49-04 | Findings size×locality: ≤4KB TerminationStub + per-CRD summary + run-branch staging | ✓ SATISFIED | Truth 4; bounded TerminationStub (Go+Py, <4096); LastEvaluation summary; task-findings staging plumbed (producer = Phase 51, deferred) |

All 5 declared requirement IDs are accounted for and marked `Complete` in REQUIREMENTS.md (lines 103-120). No orphaned requirements: REQUIREMENTS.md maps exactly LOOP-01/02/03, EVAL-03, EVAL-05 to Phase 49, all claimed by the phase plans.

### Anti-Patterns Found

| File | Line | Pattern | Severity | Impact |
| ---- | ---- | ------- | -------- | ------ |
| — | — | none | — | No unreferenced debt markers (TBD/FIXME/XXX/HACK/PLACEHOLDER) and no TODO markers in any phase-modified file. No stub returns feeding user-visible output. |

**Scope-discipline check (verified NOT violated):** No Phase 50/51 logic present — `ConditionVerifyHalt`, `setVerifyHaltIfNeeded`, `VerifyHalt`, a TaskReconciler verifier-dispatch path, and a `collectStageEnvelopes` task entry all absent. Their absence is correct scope, not a gap.

### Human Verification Required

None. This is a pure Go/Python schema + wire-contract phase with no visual, real-time, external-service, or UX surface. Every success criterion is programmatically verifiable and was verified by running the phase's own acceptance commands (all green). No `<verify><human-check>` blocks were deferred by the planner.

### Gaps Summary

No gaps. All five ROADMAP success criteria are observably true in the codebase:
- Shared LoopPolicy/LoopStatus embeddable types with five-element doc-comments and no generic Loop controller (SC1).
- Matched Go+Pydantic GateDecision/Finding + VerifyContext round-tripping a single shared golden fixture across both languages (SC2).
- Fail-closed classification (empty/missing/malformed → BLOCKED, never APPROVED) with mirrored regression coverage in Go and Python (SC3).
- Findings size×locality: bounded ≤4KB TerminationStub (both languages, strictly <4096 after the WR-01/WR-02 fix), small per-CRD `EvaluationSummary`, and consumer-side task-findings staging plumbed (SC4; producer wiring is a documented Phase 51 deliverable).
- LoopStatus proven history-free by a compile-time structural guard (SC5).

`make generate` and `make manifests` both produce zero diff on a clean tree, confirming deepcopy sync and that the types are standalone (embedded in no Kind). The two code-review warnings (WR-01 infinite-loop, WR-02 boundary parity) and the two test-parity infos (IN-02/IN-03) were fixed on main (dd71076b, 8eb7bc7c) — both confirmed present in source and covered by regression tests. All 12 phase commits (task + review-fix) are on main.

---

_Verified: 2026-07-18T22:56:35Z_
_Verifier: Claude (gsd-verifier)_
