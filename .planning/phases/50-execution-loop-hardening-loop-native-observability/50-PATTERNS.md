# Phase 50: Execution-Loop Hardening + Loop-Native Observability - Pattern Map

**Mapped:** 2026-07-18
**Files analyzed:** 13 (net-new + modified)
**Analogs found:** 13 / 13

> Correction inherited from RESEARCH.md (binding): `internal/harness/harness.go`
> is DEAD CODE (zero production call sites — `Harness.Run`/`buildEnvelopeOut`
> are orphaned scaffolding). The three REAL `TerminalReason`/`RunEvidence`
> write sites are `cmd/claude-subagent/main.go`, `internal/subagent/anthropic/subagent.go`,
> and `cmd/stub-subagent/main.go`. Do not target `internal/harness/harness.go`
> for D-02/D-03 write-site work.

## File Classification

| New/Modified File | Role | Data Flow | Closest Analog | Match Quality |
|--------------------|------|-----------|-----------------|----------------|
| `pkg/dispatch/envelope.go` (add `TerminalReason` enum + `RunEvidence` struct) | model (wire-contract type) | transform (fail-closed classify) | `pkg/dispatch/verdict.go` (`Verdict` enum + `ClassifyVerdict`) | exact |
| `pkg/dispatch/envelope_test.go` (extend) + new `pkg/dispatch/testdata/envelope_out_golden.json` | test | transform | `pkg/dispatch/verdict_test.go` + `pkg/dispatch/testdata/gate_decision_golden.json` | exact |
| `cmd/tide-langgraph-verifier/verifier/envelope.py` (`write_envelope_out`/`write_termination_stub` gain fields) | model (Python mirror) | file-I/O | Itself, prior shape (D-02/D-03 of Phase 49 already extended this exact file) | exact |
| `cmd/tide-langgraph-verifier/verifier/tests/test_envelope.py` / `test_verdict.py` | test | file-I/O | `cmd/tide-langgraph-verifier/verifier/tests/test_verdict.py` (golden-fixture parity + truncation-loop tests) | exact |
| `cmd/claude-subagent/main.go` (stamp `TerminalReason` at every exit path) | controller (thin executor shim) | request-response (batch process) | Itself, current shape (`failEnvelope`/`failOut`/`writeEnvelope`) | exact |
| `internal/subagent/anthropic/subagent.go` (`Run()` exit paths) | service (provider-firewalled executor) | request-response (batch process) | Itself, current shape (`Run()`'s `waitErr`/`readChildCRDs` branches) | exact |
| `cmd/stub-subagent/main.go` (all `EnvelopeOut{}` literal sites) | controller (test-fixture executor) | request-response (batch process) | Itself, current shape (`dispatchSuccess`/`dispatchFail`/`dispatchExceedOutputPaths`/`dispatchPlannerSuccess` internal-error branches) | exact |
| `pkg/otelai/attrs.go` (add `loop.*`/`evaluation.*`/`human_intervention` helpers) | utility (attribute-helper module) | transform | Itself, `AgentInvocation`/`FailureDetail`/const block (`:92-109`, `:240`, `:294`) | exact |
| `pkg/otelai/attrs_test.go` (add `TestLoopAttributes`/`TestEvaluationAttributes`) | test | transform | `TestSessionID`/`TestMetadata` (`:357-383`) + `TestKeysUseSemconvModule` guard (`:343`) | exact |
| `internal/reporter/tracesynth.go` (`EmitSpans` signature grows loop-identity params; loop uses index) | service (span synthesizer) | transform (events.jsonl → OTel spans) | Itself, current shape (`EmitSpans:594`, the `sessionID`/`metadataJSON`/`tags` conditional-enrichment triple) | exact |
| `internal/controller/span_emission.go` (`synthesizePlannerSpan` stamps `loop.*` on AGENT span) | controller (reconcile-time span synth) | event-driven (Job-completion reconcile) | Itself, current shape (`synthesizePlannerSpan:156`, `buildLevelEnrichment:317`) | exact |
| `internal/controller/reporter_jobspec.go` (`ReporterOptions` grows `AttemptID`/`LoopRunID` Args) | controller (Job-spec builder) | transform (opts → Job Args) | Itself, current shape (`SessionID`/`MetadataJSON`/`Tags` Args-threading, `:158-179`, `:292-306`) | exact |
| `tools/analyzers/metriccardinality/analyzer.go` (extend forbidden-label set) | utility (go/analysis static analyzer) | transform (AST → diagnostics) | Itself, current shape (`"task"`-literal rejection, `:38-98`) | exact |
| `internal/metrics/wave_label_test.go` (extend source-grep + arity checks) | test | transform (source-grep + runtime arity) | Itself, current shape (`TestWaveLabel`, the "registry.go carries no task label" subtest) | exact |

## Pattern Assignments

### `pkg/dispatch/envelope.go` — `TerminalReason` enum (model, transform)

**Analog:** `pkg/dispatch/verdict.go` (`Verdict` type + `ClassifyVerdict`)

**The exact fail-closed enum shape to mirror** (`pkg/dispatch/verdict.go:21-43`):
```go
// Verdict is the terminal classification of a verifier's gate_decision
// (EVAL-03). The set is exactly APPROVED | REPAIRABLE | BLOCKED — no other
// value is ever produced by [ClassifyVerdict].
type Verdict string

const (
	VerdictApproved   Verdict = "APPROVED"
	VerdictRepairable Verdict = "REPAIRABLE"
	VerdictBlocked    Verdict = "BLOCKED"
)
```

**The fail-closed bare-return classifier idiom** (`pkg/dispatch/verdict.go:95-118`) — note there is
NO `(T, error)` return; the caller cannot forget to map an error to the safe terminal because there
is no error to forget:
```go
func ClassifyVerdict(raw json.RawMessage) Verdict {
	if len(raw) == 0 {
		return VerdictBlocked // empty JSON
	}
	var parsed struct {
		Verdict string `json:"verdict"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return VerdictBlocked // malformed
	}
	switch Verdict(parsed.Verdict) {
	case VerdictApproved, VerdictRepairable, VerdictBlocked:
		return Verdict(parsed.Verdict)
	default:
		return VerdictBlocked // missing/unrecognized verdict field
	}
}
```

**`TerminalReason` differs in one respect** (per RESEARCH Pattern 1): there is no "parse a wire
document" step for the executor side — `TerminalReason` is *set* at each Go call site (in
`cmd/claude-subagent/main.go`, `internal/subagent/anthropic/subagent.go`, `cmd/stub-subagent/main.go`),
not classified from external input. The "never silent default" guarantee must instead be a **test
that enumerates every `EnvelopeOut{}` literal construction across the three real write sites** and
asserts each sets a non-empty `TerminalReason` — mirror the fail-closed *discipline*
(zero value = invalid sentinel, never collapses to `completed`), not the classifier function shape
verbatim.

**Where the field lands on `EnvelopeOut`** (`pkg/dispatch/envelope.go:194-196`, the field it sits
beside — `Reason` stays free-text, `TerminalReason` is the new machine enum):
```go
	// Reason carries a structured failure code when ExitCode != 0, e.g.
	// "forced-failure", "cap-hit", "output-path-violation", "token-expired".
	Reason string `json:"reason"`
	// TerminalReason (D-02) adds here — a NEW, additional field, never a
	// rename of Reason.
```

---

### `pkg/dispatch/envelope.go` — `RunEvidence` struct (model, transform, references-only)

**Analog:** the existing `EnvelopeOut`/`Usage`/`GitOutput` fields it references + `NewTerminationStub`'s
flatten-and-bound pattern (`pkg/dispatch/envelope.go:476-506`).

**Already-exist fields to reference (never re-derive)** — exact current shapes:
```go
// Usage (pkg/dispatch/envelope.go:312-359) — cost/duration/token/iteration source
type Usage struct {
	InputTokens          int64
	OutputTokens         int64
	EstimatedCostCents   int64
	CacheSavingsCents    int64  `json:"cacheSavingsCents,omitempty"`
	PricingFallbackModel string `json:"pricingFallbackModel,omitempty"`
	Iterations           int    // RunEvidence's iteration-count source
	CacheReadTokens      int64
	CacheCreationTokens  int64
}

// GitOutput (pkg/dispatch/envelope.go:282-287) — locking/head-commit source
type GitOutput struct {
	HeadSHA string `json:"headSHA"`
}
```

**The bounded-summary flattening pattern `RunEvidence`'s `TerminationStub` subset must mirror**
(`pkg/dispatch/envelope.go:484-506`, `NewTerminationStub`) — copy only bounded scalars/counts,
never the full array:
```go
func NewTerminationStub(out EnvelopeOut) TerminationStub {
	headSHA := ""
	if out.Git != nil {
		headSHA = out.Git.HeadSHA
	}
	stub := TerminationStub{
		ExitCode:   out.ExitCode,
		Reason:     out.Reason,
		Usage:      out.Usage,
		HeadSHA:    headSHA,
		ChildCount: len(out.ChildCRDs),
	}
	if out.Verdict != nil {
		stub.GateDecision = string(out.Verdict.Verdict)
		stub.FindingsCount = len(out.Verdict.Findings)
		for _, f := range out.Verdict.Findings {
			if f.Severity == highSeverityFindingToken {
				stub.HighSeverityCount++
			}
		}
	}
	return stub
}
```
Apply the identical discipline for the RunEvidence summary subset: counts/enum strings only
(e.g. `ChangedFileCount int`, not the full manifest) on `TerminationStub`; the full `RunEvidence`
struct (with the bounded changed-file list) lives only on the PVC `EnvelopeOut`.

**Model field gap** — `in.Provider.Model` already exists on `EnvelopeIn` and is in scope at every
write site; it was simply never echoed onto `EnvelopeOut` (`internal/controller/span_emission.go:135-138`
confirms: "the envelope never carried a model field at any layer"). Reference it, don't
re-derive it.

**The `<4KB` truncation test to extend** (`pkg/dispatch/envelope_test.go:788-828`,
`TestNewTerminationStub_StaysSmall`) — builds a deliberately oversized `EnvelopeOut` (50 ChildCRDs,
10KB Result, 50 high-severity findings) and asserts `json.Marshal(stub) < 4096` bytes:
```go
if len(data) >= 4096 {
	t.Errorf("TerminationStub JSON size = %d bytes, want < 4096 (termination-message budget)", len(data))
}
```
D-02/D-03 must add `TerminalReason` + the bounded RunEvidence summary to this SAME oversized
fixture (e.g. a maximally-long changed-file manifest input) and re-assert the same `< 4096` bound.

**The negative-invariant guard-test template (EXEC-04)** — mirror
`TestTerminationStub_NoForbiddenFields` (`pkg/dispatch/envelope_test.go:836-866`): a compile-time
struct-literal enumeration (adding a forbidden field breaks compilation) PLUS a runtime
JSON-key-absence check:
```go
func TestTerminationStub_NoForbiddenFields(t *testing.T) {
	_ = TerminationStub{
		ExitCode: 0, Reason: "", Usage: Usage{}, HeadSHA: "", ChildCount: 0,
		GateDecision: "", FindingsCount: 0, HighSeverityCount: 0,
		// adding a forbidden field here fails to compile
	}
	stub := NewTerminationStub(fullyPopulatedEnvelopeOut())
	data, _ := json.Marshal(stub)
	for _, forbidden := range []string{`"childCRDs"`, `"result"`, `"artifacts"`} {
		if strings.Contains(string(data), forbidden) {
			t.Errorf("TerminationStub JSON contains forbidden key %s", forbidden)
		}
	}
}
```
For EXEC-04, write the equivalent `TestEnvelopeOut_NoCorrectnessField` asserting no
`EnvelopeOut`/`TerminationStub` field name implies Task-correctness (e.g. no `taskCorrect`,
`verified`, `approved` boolean) — same compile-time-literal + runtime-JSON-key-absence shape.

---

### `pkg/dispatch/testdata/envelope_out_golden.json` + Go/Python parity tests

**Analog:** `pkg/dispatch/verdict_test.go` (`TestGateDecision_GoldenFixtureRoundTrip`) +
`cmd/tide-langgraph-verifier/verifier/tests/test_verdict.py` (`test_golden_fixture_round_trip`)

**Go side** (`pkg/dispatch/verdict_test.go:25-66`) — decode-and-assert-values, deliberately NOT a
byte compare (key order isn't guaranteed to match across Go/Python serializers):
```go
func TestGateDecision_GoldenFixtureRoundTrip(t *testing.T) {
	golden, err := os.ReadFile("testdata/gate_decision_golden.json")
	if err != nil {
		t.Fatalf("read golden fixture: %v", err)
	}
	var decoded GateDecision
	if err := json.Unmarshal(golden, &decoded); err != nil {
		t.Fatalf("unmarshal golden fixture: %v", err)
	}
	if decoded.Verdict != VerdictRepairable {
		t.Errorf("Verdict = %q, want %q", decoded.Verdict, VerdictRepairable)
	}
	// ... field-by-field non-empty assertions ...
}
```

**Python side** (`cmd/tide-langgraph-verifier/verifier/tests/test_verdict.py:21-44`) — reads the
SAME shared Go-authored fixture, value-equivalence not byte-compare:
```python
def test_golden_fixture_round_trip() -> None:
    golden_bytes = verdict.GOLDEN_FIXTURE.read_bytes()
    decoded = verdict.GateDecision.model_validate_json(golden_bytes)
    assert decoded.verdict == verdict.Verdict.REPAIRABLE
    assert decoded.summary
    # Re-dump and re-validate to prove value-equivalence (NOT byte compare).
    redumped = decoded.model_dump_json(by_alias=True)
    reparsed = verdict.GateDecision.model_validate_json(redumped)
    assert reparsed == decoded
```

**Do NOT overload `testdata/gate_decision_golden.json`** (scoped to the verdict sub-document
only) — add a NEW `pkg/dispatch/testdata/envelope_out_golden.json` covering `TerminalReason` +
`RunEvidence`, read by a new Go test and a new/extended Python test, same pattern.

**The Python fail-closed classifier parity table to mirror for terminal-reason mapping**
(`cmd/tide-langgraph-verifier/verifier/tests/test_verdict.py:56-76`, table-driven cases including
the malformed/unrecognized-value rows):
```python
@pytest.mark.parametrize(
    "raw,want",
    [
        ("", verdict.Verdict.BLOCKED),
        ('{"summary":"looks fine","findings":[]}', verdict.Verdict.BLOCKED),
        ("{not valid json", verdict.Verdict.BLOCKED),
        ('{"verdict":"APPROVED","summary":"ok","findings":[]}', verdict.Verdict.APPROVED),
        ('{"verdict":"REJECTED","summary":"stale vocabulary"}', verdict.Verdict.BLOCKED),
    ],
)
def test_classify_verdict_fails_closed(raw: str, want: verdict.Verdict) -> None:
    assert verdict.classify_verdict(raw) == want
```

---

### `cmd/tide-langgraph-verifier/verifier/envelope.py` — field-for-field Go↔Python mirror

**Analog:** the file's own current shape (already the Phase-49 hand-port precedent) — extend the
SAME two writer functions.

**`write_envelope_out` — current trivial shape to extend** (`envelope.py:132-160`):
```python
def write_envelope_out(
    path: str | os.PathLike[str],
    *,
    exit_code: int,
    result: str,
    reason: str = "",
) -> None:
    out: dict[str, Any] = {
        "apiVersion": API_VERSION,
        "kind": KIND_OUT,
        "exitCode": exit_code,
        "result": result,
    }
    if exit_code != 0:
        out["reason"] = reason
    target = Path(path)
    target.parent.mkdir(parents=True, exist_ok=True)
    target.write_bytes(json.dumps(out).encode("utf-8"))
    os.chmod(target, 0o644)
```
D-02/D-03 add `terminal_reason` (a plain str, defaulting to the Go zero-sentinel — never silently
defaulted to `"completed"`) and a `run_evidence` dict param, joined into `out` unconditionally
(bounded-by-construction fields never need the truncation loop below).

**`write_termination_stub` — the strict-`<4096`-bytes truncation loop to extend**
(`envelope.py:163-216`) — matches Go's `len(data) < 4096` (not `<=`); only `reason` is unbounded
free text and gets progressively truncated, all other fields are bounded-by-construction and
joined unconditionally:
```python
def write_termination_stub(
    path: str | os.PathLike[str],
    *,
    exit_code: int,
    reason: str = "",
    gate_decision: str = "",
    findings_count: int = 0,
    high_severity_count: int = 0,
) -> None:
    stub: dict[str, Any] = {
        "exitCode": exit_code,
        "reason": reason,
        "gateDecision": gate_decision,
        "findingsCount": findings_count,
        "highSeverityCount": high_severity_count,
    }
    data = json.dumps(stub).encode("utf-8")
    while len(data) >= TERMINATION_STUB_MAX_BYTES and reason:
        overflow = len(data) - TERMINATION_STUB_MAX_BYTES + 1
        keep = len(reason) - overflow - len("...(truncated)")
        if keep > 0:
            reason = reason[:keep] + "...(truncated)"
        else:
            reason = ""
        stub["reason"] = reason
        data = json.dumps(stub).encode("utf-8")
    target = Path(path)
    target.parent.mkdir(parents=True, exist_ok=True)
    target.write_bytes(data)
```
D-02/D-03: add `terminal_reason: str = ""` and the bounded RunEvidence-summary kwargs
(e.g. `changed_file_count: int = 0`) the SAME way `gate_decision`/`findings_count`/
`high_severity_count` were added — joined into the dict unconditionally, never subject to the
truncation loop (only `reason` is unbounded).

**The WR-01/WR-02 regression-test shapes to extend in `test_envelope.py`/`test_verdict.py`**
(non-hang + strictly-under-cap proofs, `test_verdict.py:128-217`):
```python
def test_write_termination_stub_with_verdict_fields_stays_small(tmp_path: Path) -> None:
    stub_path = tmp_path / "termination-log"
    envelope.write_termination_stub(
        stub_path, exit_code=0, gate_decision="REPAIRABLE",
        findings_count=3, high_severity_count=1,
    )
    data = stub_path.read_bytes()
    assert len(data) < envelope.TERMINATION_STUB_MAX_BYTES
    parsed = json.loads(data)
    assert parsed["gateDecision"] == "REPAIRABLE"
```
Mirror `_write_stub_with_timeout` (daemon-thread + `.join(timeout)`, `test_verdict.py:147-164`)
as the direct WR-01 hang-regression proof if the new fields add any new bounded-vs-unbounded
ambiguity.

---

### `cmd/claude-subagent/main.go` — real production executor write sites (controller, request-response)

**Analog:** itself, current shape — every exit branch already constructs an `EnvelopeOut{}` literal;
D-02 adds `TerminalReason:` to each.

**All current `EnvelopeOut{}` construction sites** (`cmd/claude-subagent/main.go:107-175`) — the
exhaustive list a Wave-0 fail-closed test must cover:
```go
func run(ctx context.Context, envelopePath, workspaceRoot string, stdout, stderr io.Writer) int {
	outPath := filepath.Join(filepath.Dir(envelopePath), "out.json")
	env, err := harness.ReadEnvelopeIn(envelopePath)
	if err != nil {
		return failOut(stderr, outPath, "", err, 2, "invalid-envelope")   // TerminalReason: invalid_output
	}
	if err := ensureWorktreeFunc(env, workspaceRoot, env.Branch); err != nil {
		return failOut(stderr, outPath, env.TaskUID, err, 1, "worktree-setup-failed") // tool_failure
	}
	out, runErr := newSubagent("claude", workspaceRoot, pricingOverrides).Run(ctx, env)
	if runErr != nil {
		out = failEnvelope(env.TaskUID, runErr, 1, "subagent-error")      // tool_failure
	}
	if runErr == nil && env.Role == "executor" {
		worktreeDir := filepath.Join(workspaceRoot, "worktrees", env.TaskUID)
		hash, isEmpty, commitErr := commitWorktreeFunc(worktreeDir, env.TaskUID)
		if commitErr != nil {
			out = failEnvelope(env.TaskUID, commitErr, 1, "commit-failed")  // tool_failure
		} else if isEmpty {
			out.ExitCode = 1
			out.Result = "empty-diff"                                      // blocked (no changes produced)
			out.Reason = "executor produced no changes in worktree"
		} else {
			out.Git = &pkgdispatch.GitOutput{HeadSHA: hash.String()}
			// SUCCESS path — TerminalReason: completed
		}
	}
	if err := writeEnvelope(outPath, out); err != nil {
		return 2
	}
	return out.ExitCode
}

func failEnvelope(taskUID string, err error, exitCode int, result string) pkgdispatch.EnvelopeOut {
	return pkgdispatch.EnvelopeOut{
		APIVersion: pkgdispatch.APIVersionV1Alpha1, Kind: pkgdispatch.KindTaskEnvelopeOut,
		TaskUID: taskUID, ExitCode: exitCode, Result: result, Reason: err.Error(),
		CompletedAt: time.Now().UTC(),
		// D-02 adds: TerminalReason: <mapped from result>,
	}
}
```

**Where `writeEnvelope` builds the `TerminationStub`** (`cmd/claude-subagent/main.go:158-175`) —
D-02/D-03's Go executor RunEvidence-population point:
```go
func writeEnvelope(path string, out pkgdispatch.EnvelopeOut) error {
	if err := harness.WriteEnvelopeOut(path, out); err != nil {
		return err
	}
	stub := pkgdispatch.NewTerminationStub(out)
	data, _ := json.Marshal(stub)
	tp := os.Getenv("TIDE_TERMINATION_MESSAGE_PATH")
	if tp == "" {
		tp = "/dev/termination-log"
	}
	_ = os.WriteFile(tp, data, 0o644)
	return nil
}
```

**Result/Reason → `TerminalReason` mapping table to build (RESEARCH Pitfall 6)** — every distinct
`result` string this file's write sites produce, and its most-defensible enum bucket:

| `Result` string | Site | `TerminalReason` (recommended) |
|---|---|---|
| `invalid-envelope` | `ReadEnvelopeIn` failure | `invalid_output` |
| `worktree-setup-failed` | `ensureWorktreeFunc` failure | `tool_failure` |
| `subagent-error` | `anthropic.Run()` returned `err != nil` | `tool_failure` |
| `commit-failed` | `commitWorktreeFunc` failure | `tool_failure` |
| `empty-diff` | commit succeeded but no changes | `blocked` |
| (success, `out.ExitCode==0`) | normal finish | `completed` |

---

### `internal/subagent/anthropic/subagent.go` — the real production `Run()` exit paths

**Analog:** itself, current shape — `Run()` already branches on every terminal condition; D-02 adds
`TerminalReason:` at the single `out := pkgdispatch.EnvelopeOut{...}` construction plus its two
downstream mutation branches.

**The base envelope construction + both exit-path mutations** (`internal/subagent/anthropic/subagent.go:359-406`):
```go
out := pkgdispatch.EnvelopeOut{
	APIVersion:  pkgdispatch.APIVersionV1Alpha1,
	Kind:        pkgdispatch.KindTaskEnvelopeOut,
	TaskUID:     in.TaskUID,
	Result:      resultText,
	Usage:       usage,
	CompletedAt: time.Now().UTC(),
	// D-02: TerminalReason defaults to completed, downgraded below.
}

if waitErr != nil {
	// Surface task-level failure via ExitCode + Reason (pkg/dispatch
	// godoc contract). Do NOT return as a dispatch-level error.
	out.ExitCode = 1
	if exitErr, ok := waitErr.(*exec.ExitError); ok {
		out.ExitCode = exitErr.ExitCode()
	}
	out.Reason = fmt.Sprintf("claude exit %d: %s", out.ExitCode, truncate(stderrBuf.String(), 256))
	// TerminalReason: tool_failure
	return out, nil
}

if in.Role == "planner" {
	relPrefix := filepath.Join("envelopes", in.TaskUID, "children")
	children, readErr := readChildCRDs(filepath.Join(eventsDir, "children"), relPrefix)
	if readErr != nil {
		out.ExitCode = 1
		out.Reason = fmt.Sprintf("read child CRDs: %s", truncate(readErr.Error(), 256))
		// TerminalReason: invalid_output (malformed planner structural output)
		return out, nil
	}
	out.ChildCRDs = children
}

return out, nil // TerminalReason: completed
```

**Dispatch-level errors return `(EnvelopeOut{}, err)`** (e.g. vendor mismatch line 219, params
allow-list line 225, prompt-template load line 250) — these never reach a `writeEnvelope` call at
all (the caller in `cmd/claude-subagent/main.go` catches `runErr != nil` and builds its OWN
`failEnvelope` with Result `"subagent-error"`), so `TerminalReason` for these is set by the CALLER
(`cmd/claude-subagent/main.go`'s `failEnvelope`), not inside `subagent.go`. Don't add
`TerminalReason` to the dispatch-level-error early returns in `subagent.go` — they never construct
a populated `EnvelopeOut`.

**Reference-only `RunEvidence` sourcing already in scope at this write site** — `in.Provider.Model`
(the "notable gap" field, `pkg/dispatch/provider.go:42-45`), `usage.Iterations`, and the SHA from
`out.Git` (set by the caller after `Run()` returns) are all already local variables here; `RunEvidence`
assembly should read them, not re-derive.

---

### `cmd/stub-subagent/main.go` — test-fixture write sites (controller, request-response)

**Analog:** itself, current shape — every `writeEnvelope(outPath, pkgdispatch.EnvelopeOut{...})`
call site needs `TerminalReason:` added.

**The ~15 literal-construction call sites (RESEARCH Pitfall 1 warning-sign count)** grouped by
function, with recommended mapping:

| Function | `Result` | `TerminalReason` |
|---|---|---|
| `run()` unknown-testMode branch (`:172`) | `invalid-envelope` | `invalid_output` |
| `run()` `loadEnvelope` failure (`:122`) | `invalid-envelope` | `invalid_output` |
| `ensureExecutorWorktree` failure (`:229`) | `worktree-setup-failed` | `tool_failure` |
| `dispatchSuccess` commit failure (`:611`) | `commit-failed` | `tool_failure` |
| `dispatchFail` (`:630-650`) | `forced-failure` | `tool_failure` (RESEARCH A2: most defensible bucket — deliberately-injected generic failure, not a cap/output-path/parse case) |
| `dispatchExceedOutputPaths` (`:670-696`) | `output-paths-violation` | `blocked` |
| `dispatchPlannerSuccess` marshal/mkdir/write internal-error branches (5 sites, `:343-500`) | `internal-error` | `invalid_output` |
| `dispatchSuccess`/`dispatchPlannerSuccess` clean finish | `success` | `completed` |

**One representative success-path construction** (`cmd/stub-subagent/main.go:585-600`):
```go
out := pkgdispatch.EnvelopeOut{
	APIVersion: pkgdispatch.APIVersionV1Alpha1,
	Kind:       pkgdispatch.KindTaskEnvelopeOut,
	TaskUID:    env.TaskUID,
	ExitCode:   0,
	Result:     "success",
	Reason:     "stub testMode=success",
	Usage: pkgdispatch.Usage{
		InputTokens: 100, OutputTokens: 200, EstimatedCostCents: 1, Iterations: 1,
	},
	Artifacts:   artifacts,
	CompletedAt: time.Now().UTC(),
	// D-02 adds: TerminalReason: pkgdispatch.TerminalReasonCompleted,
}
```

**One representative forced-failure construction** (`cmd/stub-subagent/main.go:630-645`):
```go
func dispatchFail(env pkgdispatch.EnvelopeIn, outPath string, stderr io.Writer) int {
	out := pkgdispatch.EnvelopeOut{
		APIVersion: pkgdispatch.APIVersionV1Alpha1,
		Kind:       pkgdispatch.KindTaskEnvelopeOut,
		TaskUID:    env.TaskUID,
		ExitCode:   1,
		Result:     "forced-failure",
		Reason:     "stub testMode=fail-exit-1",
		Usage:      pkgdispatch.Usage{},
		CompletedAt: time.Now().UTC(),
		// D-02 adds: TerminalReason: pkgdispatch.TerminalReasonToolFailure,
	}
	if err := writeEnvelope(outPath, out); err != nil {
		fmt.Fprintf(stderr, "stub-subagent: write out.json: %v\n", err)
	}
	return 1
}
```

**`writeEnvelope`/`writeTerminationMessage`** (`cmd/stub-subagent/main.go:290-315`) — same
`NewTerminationStub` call site pattern as `claude-subagent`'s `writeEnvelope`, confirming both
production and test-fixture executors share this exact call:
```go
func writeEnvelope(path string, out pkgdispatch.EnvelopeOut) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", filepath.Dir(path), err)
	}
	data, err := json.Marshal(out)
	if err != nil {
		return fmt.Errorf("marshal out.json: %w", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	writeTerminationMessage(pkgdispatch.NewTerminationStub(out))
	return nil
}
```

---

### `pkg/otelai/attrs.go` — `loop.*`/`evaluation.*`/`human_intervention` helpers

**Analog:** the existing TIDE-custom const block + `AgentInvocation`/`FailureDetail` helper shape.

**The exact const-block idiom to extend** (`pkg/otelai/attrs.go:85-109`) — note the doc comment
explicitly says "Only `tide.*` literals may remain hand-rolled" for THIS existing block; the new
`loop.*`/`evaluation.*` keys are a DELIBERATE, DOCUMENTED deviation (see Pitfall note below), not
an extension of the `tide.*` bucket:
```go
// TIDE-custom keys with no counterpart in the official
// openinference-semantic-conventions Go module (D-05 rename bucket ...).
// Every other attribute key emitted by this package resolves from the
// semconv.* constants below — see TestKeysUseSemconvModule (attrs_test.go)
// for the source-grep guard that enforces this split at PR-review time (ATTR-03).
const (
	keyAgentRole            = "tide.role"
	keyAgentInvocationLevel = "tide.invocation.level"
	keyArtifactPath         = "tide.artifact_path"
	keyExitCode             = "tide.exit_code"
	keyReason               = "tide.reason"
	keyEnvelopeDegraded     = "tide.envelope.degraded"
	keyTimingSynthetic      = "tide.trace.timing_synthetic"
	keyParseDegraded        = "tide.trace.parse_degraded"
)
```

**The positional-args helper-fn shape to mirror** (`pkg/otelai/attrs.go:240-248`, `AgentInvocation`
— every field is a required positional arg, no optional-attribute builder pattern exists in this
file today):
```go
func AgentInvocation(system, name, role, level string) []attribute.KeyValue {
	return []attribute.KeyValue{
		attribute.String(semconv.OpenInferenceSpanKind, semconv.SpanKindAgent),
		attribute.String(semconv.LLMSystem, system),
		attribute.String(semconv.AgentName, name),
		attribute.String(keyAgentRole, role),
		attribute.String(keyAgentInvocationLevel, level),
	}
}
```

**The two-attribute failure-classification helper to mirror for `EvaluationAttributes`
(int + string sibling)** (`pkg/otelai/attrs.go:294-299`, `FailureDetail`):
```go
func FailureDetail(exitCode int, reason string) []attribute.KeyValue {
	return []attribute.KeyValue{
		attribute.Int(keyExitCode, exitCode),
		attribute.String(keyReason, reason),
	}
}
```

**The single-attribute marker-value helper to mirror for `HumanIntervention` (a bare-bool helper)**
(`pkg/otelai/attrs.go:308-310`, `EnvelopeDegraded`):
```go
func EnvelopeDegraded() attribute.KeyValue {
	return attribute.Bool(keyEnvelopeDegraded, true)
}
```

**D-05 required new consts (per CONTEXT/RESEARCH — intentionally NOT `tide.`-prefixed, cross-vendor
loop-native convention Phase 51's LangGraph evaluator will reuse)**:
```go
const (
	keyLoopKind          = "loop.kind"
	keyLoopRunID         = "loop.run_id"
	keyLoopParentRunID   = "loop.parent_run_id"
	keyLoopIteration     = "loop.iteration"
	keyLoopCandidateVer  = "loop.candidate_version"
	keyLoopExitReason    = "loop.exit_reason"
	keyEvaluationResult  = "evaluation.result"
	keyEvaluationVersion = "evaluation.version"
	keyHumanIntervention = "human_intervention"
)
```
Add a doc comment on this new block explicitly stating these are NOT `tide.`-prefixed by design
(loop-native, cross-vendor keys — Phase 51's LangGraph evaluator spans reuse the same literal
strings) so a future reviewer doesn't "fix" them into the `tide.` namespace.

**The guard test these new consts must pass unmodified** (`pkg/otelai/attrs_test.go:343-355`,
`TestKeysUseSemconvModule`) — confirmed the forbidden-prefix regex does NOT block `loop.`/`evaluation.`:
```go
func TestKeysUseSemconvModule(t *testing.T) {
	root := findRepoRoot(t)
	data, err := os.ReadFile(filepath.Join(root, "pkg", "otelai", "attrs.go"))
	stripped := stripGoComments(string(data))
	forbidden := regexp.MustCompile(`"(llm\.|openinference\.|gen_ai\.|agent\.)`)
	if m := forbidden.FindString(stripped); m != "" {
		t.Errorf("ATTR-03 violation: ...")
	}
}
```

**The sibling test shape for the new helpers** (`pkg/otelai/attrs_test.go:357-365`, `TestSessionID`
— exact-equality assertion against `attribute.String(...)`):
```go
func TestSessionID(t *testing.T) {
	got := SessionID("uid-123")
	want := attribute.String("session.id", "uid-123")
	if !reflect.DeepEqual(got, want) {
		t.Errorf("SessionID(\"uid-123\") = %v, want %v", got, want)
	}
}
```

---

### `internal/reporter/tracesynth.go` — `EmitSpans` signature grows a loop-identity param

**Analog:** itself, current shape — the exact precedent for HOW a new per-span identity param was
added is the `sessionID`/`metadataJSON`/`tags` triple already on this signature.

**Current `CallSpan` shape** (`internal/reporter/tracesynth.go:91-100`) — NO ordinal field; the
slice order IS the iteration order (RESEARCH Pitfall 5) — use the range index, don't add a field:
```go
type CallSpan struct {
	Model           string
	InputMessages   []otelai.Message
	OutputMessages  []otelai.Message
	Usage           Usage
	StartTime       time.Time
	EndTime         time.Time
	Degraded        bool
	TimingSynthetic bool
}
```

**Current `EmitSpans` signature + the conditional-enrichment-triple idiom to extend**
(`internal/reporter/tracesynth.go:594-664`):
```go
func EmitSpans(ctx context.Context, tracer trace.Tracer, calls []CallSpan, artifactPath string, sessionID string, metadataJSON string, tags []string) error {
	for _, call := range calls {   // D-01/D-05: change to `for i, call := range calls`
		spanName := call.Model
		if spanName == "" {
			spanName = "llm"
		}
		// ... timing fallback logic unchanged ...
		_, span := tracer.Start(ctx, spanName, trace.WithTimestamp(startTime))

		span.SetAttributes(otelai.LLMSpanKind())
		span.SetAttributes(otelai.LLMIdentity("anthropic", call.Model)...)
		span.SetAttributes(inputAttrs...)
		span.SetAttributes(outputAttrs...)
		span.SetAttributes(otelai.TokenCount(...)...)
		span.SetAttributes(otelai.ArtifactPath(artifactPath))
		span.SetAttributes(otelai.TimingSynthetic())
		if call.Degraded || inputDegraded || outputDegraded {
			span.SetAttributes(otelai.ParseDegraded())
		}
		// 46 OBS-02/OBS-03: conditional enrichment triple — absent when
		// empty, never a fabricated empty value.
		if sessionID != "" {
			span.SetAttributes(otelai.SessionID(sessionID))
		}
		if metadataJSON != "" {
			span.SetAttributes(otelai.MetadataJSON(metadataJSON))
		}
		if len(tags) > 0 {
			span.SetAttributes(otelai.Tags(tags...))
		}
		span.SetStatus(codes.Ok, "")
		span.End(trace.WithTimestamp(endTime))
	}
	return nil
}
```
D-05 extension: add `attemptID`/`loopRunID string` params after `tags`, and inside the loop
(now `for i, call := range calls`) stamp
`span.SetAttributes(otelai.LoopAttributes("execution", attemptID, loopRunID, i+1, ...)...)`
following the exact SAME "conditional, absent when empty" pattern the sessionID/metadataJSON/tags
triple already uses. 1-indexed (`i+1`) to match `LoopStatus.Iteration`'s documented "1-indexed once
dispatched" convention (`api/v1alpha3/loop_types.go:93-94`).

---

### `internal/controller/span_emission.go` — `synthesizePlannerSpan` stamps `loop.*` on AGENT span

**Analog:** itself, current shape — the exact insertion point is beside the existing
`SessionID`/`buildLevelEnrichment` stamping block.

**Current stamping sequence to extend** (`internal/controller/span_emission.go:210-233`):
```go
span.SetAttributes(otelai.AgentInvocation(provider.Vendor, spanName, role, level)...)
span.SetAttributes(otelai.LLMIdentity(provider.Vendor, provider.Model)...)

// 46 D-05/OBS-02/OBS-03: session.id + metadata/tags, computed from the
// SAME buildLevelEnrichment inputs the level's reporter spawn uses (Task
// 2) so a Task's AGENT span and its reporter's LLM spans carry
// byte-identical values.
span.SetAttributes(otelai.SessionID(string(project.UID)))
md, tags := buildLevelEnrichment(project, level, levelName, waveIndex)
if md != "" {
	span.SetAttributes(otelai.MetadataJSON(md))
}
if len(tags) > 0 {
	span.SetAttributes(otelai.Tags(tags...))
}

// D-05 insertion point: span.SetAttributes(otelai.LoopAttributes(
//     "execution", attemptID, loopRunID, usage.Iterations,
//     candidateVersion, string(terminalReason))...)
// where attemptID/loopRunID derive from Task.Status.Attempt + TaskUID
// (D-01 — see podjob.JobName below), candidateVersion is out.Git.HeadSHA
// (D-03), terminalReason is out.TerminalReason (D-02).

if !envReadOK {
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
```

**`buildLevelEnrichment`'s metadata-map-building shape** (`internal/controller/span_emission.go:317-354`)
is the reference for HOW conditional keys are added to a shared metadata map (only if the plan
threads loop identity through the metadata JSON instead of/in addition to direct span attributes):
```go
md := map[string]string{
	"level": level,
	"name":  levelName,
}
if waveIndex != "" {
	md["wave_index"] = waveIndex
}
// ... more conditional key additions ...
encoded, err := json.Marshal(md)
```

---

### `internal/controller/reporter_jobspec.go` — `ReporterOptions` grows `AttemptID`/`LoopRunID` Args

**Analog:** itself, current shape — the EXACT precedent (Phase 46's `SessionID`/`MetadataJSON`/`Tags`)
for threading new per-span identity data to the separately-spawned `cmd/tide-reporter` process via
Args, never Env.

**The doc-comment discipline to match** (`internal/controller/reporter_jobspec.go:158-179`):
```go
// SessionID (46 D-05/OBS-02) is TIDE's own run identity — the Project
// UID — stamped on every reporter-emitted LLM span via otelai.SessionID.
// ... Carried as an Arg (--session-id=), not Env, matching TraceParent's
// precedent (Pitfall 3 — this file is 100% Args-based).
SessionID string

MetadataJSON string
Tags []string
```

**The exact Args-append idiom to extend** (`internal/controller/reporter_jobspec.go:292-306`):
```go
// 46 D-05: session/metadata/tags ride the same uniform-across-both-shapes
// placement as SkipMessageSpans above. All three are per-span attribute
// values, so they follow TraceParent's Args precedent, never Env.
if opts.SessionID != "" {
	args = append(args, "--session-id="+opts.SessionID)
}
if opts.MetadataJSON != "" {
	args = append(args, "--metadata="+opts.MetadataJSON)
}
if len(opts.Tags) > 0 {
	args = append(args, "--tags="+strings.Join(opts.Tags, ","))
}
// D-01/D-05 extension follows the identical shape:
// if opts.AttemptID != "" { args = append(args, "--attempt-id="+opts.AttemptID) }
// if opts.LoopRunID != "" { args = append(args, "--loop-run-id="+opts.LoopRunID) }
```
Both new params thread straight through `cmd/tide-reporter/main.go`'s `parseFlags` →
`reporterConfig` → `synthesizeSpans` → `EmitSpans` call site, exactly like `sessionID`/
`metadataJSON`/`tags` did in Phase 46.

---

### `tools/analyzers/metriccardinality/analyzer.go` — extend forbidden-label set

**Analog:** itself, current shape — the exact mechanical change is growing a `map[string]struct{}`
membership set (today implicit as a single string comparison) to a set-membership check.

**Current single-literal rejection to generalize** (`tools/analyzers/metriccardinality/analyzer.go:38-98`):
```go
var Analyzer = &analysis.Analyzer{
	Name: "metriccardinality",
	Doc:  `rejects "task" label literal in prometheus.New*Vec calls (OBS-02 / Pitfall 17 / D-X4)`,
	Run:  run,
}

func run(pass *analysis.Pass) (any, error) {
	for _, f := range pass.Files {
		ast.Inspect(f, func(n ast.Node) bool {
			// ... call/selector/pkgIdent walk to find prometheus.New*Vec ...
			for _, arg := range call.Args {
				lit, ok := arg.(*ast.CompositeLit)
				if !ok || !isStringSliceType(lit.Type) {
					continue
				}
				for _, elt := range lit.Elts {
					bl, ok := elt.(*ast.BasicLit)
					if !ok || bl.Kind != token.STRING {
						continue
					}
					unquoted, err := strconv.Unquote(bl.Value)
					if err != nil {
						continue
					}
					if unquoted == "task" {
						pass.Reportf(bl.Pos(), "metriccardinality: %q label forbidden in prometheus.%s(...) — adds unbounded task-axis cardinality (Pitfall 17 / D-X4)", "task", sel.Sel.Name)
					}
				}
			}
			return true
		})
	}
	return nil, nil
}
```
D-06 extension: replace the `unquoted == "task"` single comparison with a
`forbiddenLabels := map[string]struct{}{"task": {}, "run_id": {}, "loop_run_id": {}, "run": {}, "attempt": {}, "attempt_id": {}, "trace_id": {}, "task_uid": {}, "uid": {}}`
membership check, generalizing the diagnostic message to report whichever label matched. The
`vecConstructors`/`isStringSliceType` scaffolding is unchanged.

**`vecConstructors` (unchanged, the scope-limiter this analyzer already applies)**:
```go
var vecConstructors = map[string]struct{}{
	"NewCounterVec": {}, "NewHistogramVec": {}, "NewGaugeVec": {}, "NewSummaryVec": {},
}
```

**The existing `testdata/src/badlabels`/`goodlabels` fixture convention** (referenced by RESEARCH's
Wave-0 gap list) is where the new forbidden labels each need a positive-control fixture entry —
locate via `tools/analyzers/metriccardinality/analyzer_test.go` and its `testdata/src/` tree.

---

### `internal/metrics/wave_label_test.go` — extend source-grep + arity guard

**Analog:** itself, current shape — the `registry.go carries no task label` subtest is the exact
template to duplicate for the run-ID-shaped label list.

**The subtest shape to mirror** (`internal/metrics/wave_label_test.go:120-134`):
```go
t.Run("registry.go carries no task label (Pitfall 17)", func(t *testing.T) {
	root := findMetricsRepoRoot(t)
	data, err := os.ReadFile(filepath.Join(root, "internal", "metrics", "registry.go"))
	if err != nil {
		t.Fatalf("read registry.go: %v", err)
	}
	src := string(data)
	if strings.Contains(src, `"task"`) {
		t.Errorf(`registry.go contains literal "task" — Pitfall 17 violation; no task-scoped metric label permitted`)
	}
})
```
D-06 extension: add sibling subtests (or loop over the same forbidden-label list the analyzer now
uses) asserting `registry.go` contains none of `"run_id"`, `"loop_run_id"`, `"run"`, `"attempt"`,
`"attempt_id"`, `"trace_id"`, `"task_uid"`, `"uid"` as a quoted label literal.

**The arity-lock table-driven shape** (`internal/metrics/wave_label_test.go:49-98`) — if D-06 adds
any new bounded-label metric, register it here with a `WithLabelValues(...)` seed call (arity
mismatch panics at test time, which IS the test):
```go
var telem03labelChecks = []struct {
	name   string
	seedFn func()
}{
	{"tide_tokens_input_total", func() {
		tidemetrics.TokensInputTotal.WithLabelValues("p", "ph", "pl", "w").Add(0)
	}},
	// ... 6 more entries, all {project, phase, plan, wave} arity ...
}
```
Per RESEARCH Open Question 3, the minimal-scope reading is: harden the guard, add NO new metric
this phase (loop-outcome metrics wait for Phase 51's real consumer) — this table stays unchanged
unless the plan finds a concrete need.

---

## Shared Patterns

### Fail-closed on the zero value (D-02, the terminal-reason discipline)
**Source:** `pkg/dispatch/verdict.go:ClassifyVerdict` (bare-return, no error to forget) +
`pkg/dispatch/envelope_test.go:TestTerminationStub_NoForbiddenFields` (compile-time struct-literal
enumeration + runtime JSON-key check).
**Apply to:** `pkg/dispatch/envelope.go` (`TerminalReason` type + zero-value sentinel), the new
`TestEnvelopeOut_TerminalReasonNeverSilent` test enumerating all real write sites across
`cmd/claude-subagent/main.go` + `internal/subagent/anthropic/subagent.go` + `cmd/stub-subagent/main.go`.

### Go↔Python envelope duality — hand-port, never codegen (D-02/D-03)
**Source:** `cmd/tide-langgraph-verifier/verifier/envelope.py` (existing `write_envelope_out`/
`write_termination_stub`) + the import-firewall doc at `pkg/dispatch/doc.go`.
**Apply to:** every new `EnvelopeOut`/`TerminationStub` field — hand-ported field-for-field into
`envelope.py`, proven via a NEW shared golden fixture (`pkg/dispatch/testdata/envelope_out_golden.json`)
read by both a Go test and a Python test (mirroring `gate_decision_golden.json`'s pattern exactly).

### Attribute helpers via the semconv module / TIDE-custom const block (D-05)
**Source:** `pkg/otelai/attrs.go:85-109` (const block) + `:240` (`AgentInvocation`, positional-args
helper shape) + `attrs_test.go:343` (`TestKeysUseSemconvModule` guard — confirmed to NOT block
`loop.`/`evaluation.` prefixes).
**Apply to:** all `loop.*`/`evaluation.*`/`human_intervention` helper additions in `pkg/otelai/attrs.go`,
stamped from `internal/controller/span_emission.go` (AGENT span) and `internal/reporter/tracesynth.go`
(LLM-span subset).

### Args-only cross-process threading, never Env (D-01/D-05)
**Source:** `internal/controller/reporter_jobspec.go:158-179` (doc comment: "this file is
100% Args-based") + `:292-306` (the `SessionID`/`MetadataJSON`/`Tags` append idiom).
**Apply to:** `AttemptID`/`LoopRunID` on `ReporterOptions` → `BuildReporterJob` Args → `cmd/tide-reporter`
CLI flags → `EmitSpans` params.

### Bounded enum labels only, dual static+runtime cardinality guard (D-06)
**Source:** `tools/analyzers/metriccardinality/analyzer.go` (compile-time `go vet`-style guard) +
`internal/metrics/wave_label_test.go` (runtime source-grep + arity lock).
**Apply to:** any new Prometheus metric this phase adds (default: none, per RESEARCH Open Question 3)
— run IDs/attempt IDs must never enter a `prometheus.New*Vec` label slice; enforced at BOTH layers,
matching the existing `"task"`-label precedent exactly.

## No Analog Found

None. All 13 files/surfaces in Phase 50's scope have a direct, currently-shipping analog in this
exact codebase (verified per RESEARCH.md: "Every one of Phase 50's six decisions has a *direct*
Phase-48/49/46 precedent already shipped in this exact codebase").

## Metadata

**Analog search scope:** `pkg/dispatch/`, `cmd/tide-langgraph-verifier/verifier/`,
`cmd/claude-subagent/`, `cmd/stub-subagent/`, `internal/subagent/anthropic/`, `pkg/otelai/`,
`internal/reporter/`, `internal/controller/` (`span_emission.go`, `reporter_jobspec.go`),
`tools/analyzers/metriccardinality/`, `internal/metrics/`, `api/v1alpha3/` (`task_types.go`,
`loop_types.go`), `internal/dispatch/podjob/`.
**Files scanned:** ~20 direct reads this session (envelope.go, verdict.go, verdict_test.go,
envelope.py, test_verdict.py, claude-subagent/main.go, stub-subagent/main.go,
anthropic/subagent.go, attrs.go, attrs_test.go, tracesynth.go, span_emission.go,
reporter_jobspec.go, metriccardinality/analyzer.go, wave_label_test.go, podjob/names.go,
task_types.go, loop_types.go, envelope_test.go).
**Pattern extraction date:** 2026-07-18
