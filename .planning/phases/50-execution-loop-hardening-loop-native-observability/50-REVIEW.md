---
phase: 50-execution-loop-hardening-loop-native-observability
reviewed: 2026-07-19T00:00:00Z
depth: deep
files_reviewed: 33
files_reviewed_list:
  - pkg/dispatch/envelope.go
  - pkg/dispatch/terminal_reason.go
  - pkg/dispatch/run_evidence.go
  - pkg/dispatch/writesite_guard_test.go
  - pkg/dispatch/terminal_reason_test.go
  - pkg/dispatch/envelope_test.go
  - pkg/dispatch/testdata/envelope_out_golden.json
  - cmd/claude-subagent/main.go
  - cmd/claude-subagent/main_test.go
  - cmd/stub-subagent/main.go
  - internal/subagent/anthropic/subagent.go
  - internal/subagent/anthropic/subagent_test.go
  - internal/subagent/common/prompt_templates.go
  - internal/harness/commit.go
  - internal/harness/commit_test.go
  - internal/controller/task_controller.go
  - internal/controller/span_emission.go
  - internal/controller/span_emission_unit_test.go
  - internal/controller/reporter_jobspec.go
  - internal/controller/reporter_jobspec_test.go
  - internal/reporter/tracesynth.go
  - internal/reporter/tracesynth_test.go
  - cmd/tide-reporter/main.go
  - cmd/tide-reporter/main_test.go
  - pkg/otelai/attrs.go
  - pkg/otelai/attrs_test.go
  - tools/analyzers/metriccardinality/analyzer.go
  - tools/analyzers/metriccardinality/analyzer_test.go
  - tools/analyzers/metriccardinality/testdata/src/badlabels/registry.go
  - tools/analyzers/metriccardinality/testdata/src/goodlabels/registry.go
  - internal/metrics/wave_label_test.go
  - cmd/tide-langgraph-verifier/verifier/envelope.py
  - cmd/tide-langgraph-verifier/verifier/tests/test_envelope.py
findings:
  high: 1
  medium: 1
  low: 2
  info: 2
  total: 6
status: resolved
resolution:
  high_fixed: 1
  medium_fixed: 1
  low_accepted: 2
  info_accepted: 2
---

# Phase 50: Code Review Report

## Resolution (2026-07-19)

- **HIGH-01 — FIXED** at wave-close (commit follows this doc). `cmd/claude-subagent/main.go`'s executor commit block is now gated on `out.TerminalReason == TerminalReasonCompleted`, so a `cap_exceeded`/`tool_failure` run anthropic.Run() returned as `(out, nil)` is written verbatim instead of being downgraded to `blocked`/`tool_failure`. Regression test `TestClaudeSubagentMain_NonCompletedRunBypassesCommit` (cap_exceeded + tool_failure sub-cases) added and **verified to fail without the gate**.
- **MED-01 — FIXED** same commit. The `commit-failed` branch now preserves the RunEvidence anthropic.Run() assembled (`failed.RunEvidence = out.RunEvidence`), proven by `TestClaudeSubagentMain_CommitFailedPreservesRunEvidence`.
- **LOW-01 — accepted with rationale.** The write-site AST guard checks `TerminalReason` key presence, not value validity. No live violation (all sites pass typed consts), and the runtime `TerminalReason.Valid()` method is a second structural layer. Left as-is; a future hardening may reject empty-string literals.
- **LOW-02 — accepted (documented Phase-51 boundary).** Python mirrors `run_evidence` as an opaque dict; the shared golden fixture is the sole sub-field parity anchor until Phase 51 populates the verifier's full RunEvidence (per D-03).
- **INFO-01 / INFO-02 — accepted.** `loop.iteration` dual-meaning (total on AGENT span, ordinal on LLM span) is per-spec (D-05 + RESEARCH Pitfall 5); the TerminationStub Go-`omitempty`/Python-unconditional divergence decodes byte-identically into the Go struct (mirrors the pre-existing Phase-49 stub pattern).

**Reviewed:** 2026-07-19
**Depth:** deep (cross-file: envelope wire contract, Go↔Python duality, span synth, cardinality guards)
**Files Reviewed:** 33 (`git diff a42634b9..HEAD -- '*.go' '*.py'`, +2656/−152)
**Status:** issues_found

## Summary

Phase 50 wires four settled seams — the `EnvelopeOut`/`TerminationStub` wire
contract, its Python mirror, the two span emitters, and the dual Prometheus
cardinality guard — cleanly and, in most places, with unusual care. The
invariants that are structurally decidable hold up under adversarial reading:

- **D-01 identity is consistent, no off-by-one.** `buildEnvelopeIn` now takes
  the same `attempt` that `nextAttempt()` computed and that `createDispatchJob`
  patches into `Task.Status.Attempt` before Job creation (`task_controller.go:814`),
  so `EnvelopeIn.AttemptID` (`{uid}-{attempt}`), the Job name tuple, the AGENT
  span (`out.AttemptID`), and the reporter's `--attempt-id`
  (`{uid}-{Status.Attempt}`) all resolve to the same string. The LLM-span
  `loop.iteration` is correctly `i+1` (1-indexed).
- **cap_exceeded synthesis is fail-closed.** `synthesizeNoEnvelopeOut` maps
  *only* a `JobFailed`/`DeadlineExceeded` condition to `cap_exceeded`; every
  other failure reason leaves `TerminalReason` unset — no path mis-classifies a
  normal failure as `cap_exceeded`.
- **RunEvidence bounds hold.** `Bounded()` truncates every agent-influenced
  field; only the count (`ChangedFileCount`) reaches the 4 KB stub; the argv in
  `Commands` carries no secret (key rides env, prompt rides stdin, verified at
  `subagent.go:287-307`); paths come from `git --name-status` with no shell.
- **OBS-02 guard is airtight and well-tested.** The analyzer + runtime grep
  reject the full run-ID-shaped label set across all four `*Vec` constructors,
  with bounded-enum positive controls.
- **Scope fence holds.** Zero `VerifyHalt` hits; `failure_halt.go` /
  `vendor_capabilities.go` untouched; `EvaluationAttributes`/`HumanIntervention`
  are defined with no non-test caller; no `EVALUATOR` span; no verifier dispatch.
- **Go↔Python parity round-trips.** Both languages read the same
  `envelope_out_golden.json` and assert value-equivalence on all mirrored
  top-level fields.

The one real correctness defect is in the executor commit path of
`cmd/claude-subagent`: it unconditionally re-classifies the envelope for any
`runErr == nil` executor outcome, silently overwriting the `TerminalReason`
(and RunEvidence) that `anthropic.Run()` already set for `cap_exceeded` /
`tool_failure` / `invalid_output` runs. This directly defeats the phase's
headline EXEC-02/EXEC-03 deliverables for the executor — the exact classification
Phase 51 will build on — and is not exercised by any test.

## High

### HIGH-01: Executor commit block silently overwrites anthropic.Run's non-`completed` TerminalReason

**File:** `cmd/claude-subagent/main.go:139-158`
**Requirement undermined:** EXEC-02 ("explicit terminal reason", "never a silent default" — here a *silently wrong* default) for the executor path.

`anthropic.Run()` now returns `(out, nil)` — i.e. `runErr == nil` — for several
non-success outcomes, each stamping a specific `TerminalReason`:
`tool_failure` (claude exited non-zero, `subagent.go:396-402`), `cap_exceeded`
(in-pod cap fired, `subagent.go:407-419`), and `invalid_output` (child-CRD parse
error, `subagent.go:444`). But the outer `run()` gates its commit block only on
`runErr == nil && env.Role == "executor"` (line 139) and then re-writes the
disposition:

- **empty diff (line 144-148):** `out.TerminalReason = TerminalReasonBlocked` —
  unconditionally. A run that already terminated `cap_exceeded` or `tool_failure`
  but happened to leave no committable diff is reported as `blocked`.
- **commit error (line 142-143):** `out = failEnvelope(env, commitErr, 1,
  "commit-failed", TerminalReasonToolFailure)` — a `cap_exceeded` run becomes
  `tool_failure`, and the whole envelope (including RunEvidence) is rebuilt.
- **non-empty diff (line 149-156):** keeps the failure `TerminalReason` but sets
  `out.Git` and completes RunEvidence as if the attempt succeeded — committing a
  capped run's "suspect" partial work (`subagent.go` cap comment) as a normal head commit.

The code comment at line 154 ("out.TerminalReason arrives as 'completed' from
anthropic.Run()'s base literal — not re-set here") documents the incorrect
assumption at the root of the bug: it assumes a `runErr == nil` executor run is
always `completed`, which the new cap/tool_failure branches violate.

**Concrete failure scenario:** an executor Task with `Caps.Iterations` set burns
its iteration budget reading files without producing a diff. `anthropic.Run()`
returns `cap_exceeded`, `ExitCode=1`. `run()` enters the commit block, `isEmpty`
is true, and the envelope ships with `TerminalReason: blocked`. The Phase-51 Task
loop (which keys repair strategy off `TerminalReason`) sees a policy-block, not a
budget exhaustion, and mis-routes the retry.

**No test covers this:** `TestClaudeSubagentMain_TerminalReasonMapping`
(`main_test.go:391`) exercises empty-diff→`blocked` and commit-failed→`tool_failure`
only from a *successful* fake-exec anthropic run — never from a `cap_exceeded` /
`tool_failure` one.

**Fix:** the commit block must not downgrade an already-classified failure. Gate
the whole block (or at least the reclassifying branches) on the run having
actually succeeded:

```go
if runErr == nil && env.Role == "executor" && out.TerminalReason == pkgdispatch.TerminalReasonCompleted {
    hash, isEmpty, commitErr := commitWorktreeFunc(worktreeDir, env.TaskUID)
    // ... existing empty-diff / commit-error / success branches ...
}
```

A capped or tool-failed executor run then writes its `out.json` verbatim
(carrying `cap_exceeded`/`tool_failure` + the RunEvidence the anthropic layer
assembled) without the commit path clobbering it. Add a
`TerminalReasonMapping` sub-case that feeds a `cap_exceeded` anthropic result
through the executor path and asserts the reason survives.

## Medium

### MED-01: `commit-failed` executor path discards the RunEvidence anthropic.Run assembled (EXEC-03 gap)

**File:** `cmd/claude-subagent/main.go:143` (`failEnvelope` rebuild)
**Requirement undermined:** EXEC-03 ("the result envelope satisfies the run-evidence contract").

Same root cause as HIGH-01 but a distinct, more common blast radius: even for a
*fully successful* `completed` executor run, if `commitWorktreeFunc` returns an
error, `out = failEnvelope(...)` builds a brand-new `EnvelopeOut` from scratch.
`anthropic.Run()` had already populated `RunEvidence{Model, PromptVersion,
RuntimeVersion, Commands}` (`subagent.go:369-393`); `failEnvelope`
(`main.go:180-196`) sets no `RunEvidence`, so the shipped `commit-failed`
envelope carries none of it. Commit failures are a routine executor failure
mode, and the evidence contract exists precisely so a failed attempt is
debuggable/comparable.

**Concrete failure scenario:** an executor attempt runs, produces changes, but
the commit fails (e.g. transient go-git lock). The resulting envelope on the PVC
has `terminalReason: tool_failure` and an empty `runEvidence` — the operator
loses model/prompt/runtime/argv provenance for the very attempt that failed.

**Fix:** preserve the already-assembled evidence when downgrading to
`commit-failed`, e.g. carry `out.RunEvidence` forward instead of discarding it:

```go
if commitErr != nil {
    failed := failEnvelope(env, commitErr, 1, "commit-failed", pkgdispatch.TerminalReasonToolFailure)
    failed.RunEvidence = out.RunEvidence // preserve model/prompt/runtime/argv provenance
    out = failed
}
```

(If HIGH-01 is fixed by gating the whole block on `completed`, this branch only
ever handles genuine commit failures of otherwise-successful runs, and the same
evidence-preservation applies.)

## Low

### LOW-01: The write-site AST guard asserts `TerminalReason` key *presence*, not value validity

**File:** `pkg/dispatch/writesite_guard_test.go:96-106`
**Requirement partially covered:** EXEC-02 "never a silent default."

`TestEnvelopeOutWriteSites_AlwaysSetTerminalReason` walks every populated
`pkgdispatch.EnvelopeOut{...}` literal and requires a `TerminalReason` *key*. It
does not inspect the value. A literal `EnvelopeOut{TerminalReason: ""}`, or
`EnvelopeOut{TerminalReason: someVar}` where `someVar` defaults to the invalid
zero value, satisfies the guard while violating the "never unset" invariant the
guard claims to enforce. Today every write site passes a typed constant, so
there is no live violation — but the guard is weaker than its doc comment
asserts, and the invariant it is the sole structural enforcer of.

**Fix:** additionally reject a `TerminalReason` value that is an empty string
literal (`""`), and optionally require the value to be an `*ast.SelectorExpr`
resolving to a `TerminalReason*` const, so a future non-const/empty assignment
fails the guard rather than passing quietly.

### LOW-02: Python `write_envelope_out` treats `run_evidence` as an opaque dict — RunEvidence sub-fields are not independently mirrored

**File:** `cmd/tide-langgraph-verifier/verifier/envelope.py:147,190-193`
**Requirement partially covered:** Go↔Python envelope duality (D-03).

The nine typed `RunEvidence` Go fields (`SpecID`, `LockingCommit`, `Commands`,
`EvaluatorVersions`, `ChangedFiles`, `ChangedFileTotal`, `Model`,
`PromptVersion`, `RuntimeVersion`) are mirrored on the Python side only as an
opaque `dict[str, Any]` passthrough, not as typed fields the way the top-level
`terminal_reason`/`loop_run_id`/`attempt_id` are. A Go-side RunEvidence field
rename or JSON-tag change would therefore be caught only by the shared golden
fixture test — not by any Python-side schema — quietly breaking the
"hand-port field-for-field" discipline the phase relies on. This is acceptable
for Phase 50 (the verifier's full RunEvidence population is Phase 51, per D-03),
and the golden test does exercise every key name, so flagged LOW.

**Fix (or accept):** either add a typed `RunEvidence` dataclass mirror on the
Python side, or add an inline comment at `envelope.py:190` noting the opaque-dict
passthrough is deliberate and the golden fixture is the sole parity anchor for
the sub-fields until Phase 51.

## Info

### INFO-01: `loop.iteration` carries two different meanings across span kinds

**File:** `internal/controller/span_emission.go:249` (AGENT span) vs `internal/reporter/tracesynth.go:685` (LLM span)

On the AGENT span, `loop.iteration = out.Usage.Iterations` — the *total*
iteration count for the attempt. On the per-call LLM spans,
`loop.iteration = i+1` — the *ordinal index* of that call. Both are per-spec
(CONTEXT D-05 + RESEARCH Pitfall 5), so this is not a deviation, but a
downstream consumer (Phoenix) grouping or aggregating on a single
`loop.iteration` attribute across both span kinds gets inconsistent semantics.
Worth a one-line note near one of the two call sites so a future observability
consumer isn't surprised.

### INFO-02: TerminationStub Go(`omitempty`)/Python(unconditional) serialization divergence on empty bounded fields

**File:** `cmd/tide-langgraph-verifier/verifier/envelope.py:236-243`

Go's `TerminationStub` tags `terminalReason` and `changedFileCount` (and the
Phase-49 `gateDecision`/`findingsCount`/`highSeverityCount`) `omitempty`, so an
empty/zero value is omitted; Python's `write_termination_stub` joins them
unconditionally, writing `"terminalReason": ""` / `"changedFileCount": 0`. The
two stub producers thus emit byte-divergent JSON for empty values. Cosmetic —
any consumer deserializing into the Go struct decodes both forms to the same
zero value, and this mirrors the pre-existing Phase-49 pattern (the stub is not
covered by the golden fixture). Noted for awareness only.

---

_Reviewed: 2026-07-19_
_Reviewer: Claude (gsd-code-reviewer)_
_Depth: deep_
