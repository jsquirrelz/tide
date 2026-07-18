---
phase: 49-common-loop-contract-verdict-envelope-persistence-schema
reviewed: 2026-07-18T22:32:27Z
depth: standard
files_reviewed: 13
files_reviewed_list:
  - api/v1alpha3/loop_types.go
  - api/v1alpha3/loop_types_test.go
  - pkg/dispatch/verdict.go
  - pkg/dispatch/verdict_test.go
  - pkg/dispatch/envelope.go
  - pkg/dispatch/envelope_test.go
  - pkg/dispatch/testdata/gate_decision_golden.json
  - cmd/tide-push/main.go
  - cmd/tide-push/main_test.go
  - cmd/tide-langgraph-verifier/verifier/verdict.py
  - cmd/tide-langgraph-verifier/verifier/envelope.py
  - cmd/tide-langgraph-verifier/verifier/tests/test_verdict.py
  - cmd/tide-langgraph-verifier/verifier/tests/conftest.py
findings:
  critical: 0
  warning: 2
  info: 5
  total: 7
status: issues_found
---

# Phase 49: Code Review Report

**Reviewed:** 2026-07-18T22:32:27Z
**Depth:** standard
**Files Reviewed:** 13
**Status:** issues_found

## Summary

This is a schema/contract-definition phase (shared loop contract + verdict/envelope wire types across a Go K8s operator and a Python LangGraph verifier). The highest-priority invariant — **fail-closed verdict classification** — is solid on both sides. I traced and empirically exercised `ClassifyVerdict` (Go) and `classify_verdict` (Python) against every degenerate input class (empty, whitespace, malformed, `null` verdict, lowercase, unrecognized string, bare JSON string, JSON array, bool verdict, missing field). Both map all of them to `BLOCKED` and only ever return `APPROVED` for an exact well-formed `"APPROVED"`. The bare-return signatures (no error to forget) make "unknown → APPROVED" structurally inexpressible. No regression of the silent-`Complete` incident this milestone exists to prevent.

Verified clean:
- **Import firewall** holds — `api/v1alpha3` does not import `pkg/dispatch` (only doc comments reference it); `EvaluationSummary.Decision` is a locally-scoped `string`, not `pkgdispatch.GateDecision`. `pkg/dispatch/verdict.go` imports only `encoding/json`.
- **LOOP-03 etcd-safety** — `LoopStatus` carries only current-iteration summary (single `LastEvaluation` pointer, not a slice) + a bounded map-list of `Conditions`; the compile-time `TestLoopStatus_NoForbiddenFields` guard pins it. Generated `DeepCopyInto` correctly deep-copies the `*EvaluationSummary`, its `*metav1.Time`, and the `Conditions` slice.
- **`stageEnvelopeArtifacts` glob generalization** — confirmed via diff that the Milestone/Phase/Plan/Project (`else`) path is byte-identical to before (the `*.md`/`children/*.json` logic is unchanged, only relocated into the `else` branch); the empty-`*.md` guard is preserved (`TestStageEnvelopesEmptyDirFailsLoud`); only the new `task`-kind branch stages `findings.json`-only, fail-closed on an absent `findings.json`. `kind` derivation (`strings.Cut(DestPrefix, "/")`) is safe — `DestPrefix` is DNS-pattern + traversal-containment validated in `parseStageEnvelopes` before it reaches the split.
- Go tests (`pkg/dispatch`, `api/v1alpha3`, `cmd/tide-push` staging suite) pass; `go vet` clean; Python classifier smoke-tested against Go for parity.

The two Warnings are both in the Python `write_termination_stub` size-guard — the exact "size-boundary" surface this phase is named for — and both are empirically reproduced.

## Warnings

### WR-01: `write_termination_stub` truncation loop can spin forever when a non-`reason` field exceeds the 4 KB cap

**File:** `cmd/tide-langgraph-verifier/verifier/envelope.py:194-199`
**Issue:** The size-enforcement loop only ever shrinks `reason`:
```python
while len(data) > TERMINATION_STUB_MAX_BYTES and reason:
    overflow = len(data) - TERMINATION_STUB_MAX_BYTES
    keep = max(0, len(reason) - overflow - len("...(truncated)"))
    reason = reason[:keep] + "...(truncated)"
    stub["reason"] = reason
    data = json.dumps(stub).encode("utf-8")
```
`gate_decision` is added unconditionally and is never truncated. The docstring asserts it is "bounded by construction," but the function signature (`gate_decision: str`) enforces nothing. Once `reason` reaches its `"...(truncated)"` floor (14 chars) while `data` is still `> 4096` (because `gate_decision` alone overflows the cap), `keep` clamps to `0`, `reason` stays `"...(truncated)"`, `data` stops changing, and — because `reason` is still truthy — the `while` condition never goes false. **Empirically confirmed:** calling `write_termination_stub(..., reason="short", gate_decision="Z"*5000)` hangs (killed by an 8 s test timeout). A hung verifier finalizer never writes the termination message, so the Manager cannot read the Task outcome until the wall-clock cap (HARN-02 SIGTERM) kills the Job. The intended Phase 51 caller passes the bounded `Verdict` enum, so this needs upstream misuse/regression to trigger — but a size guard that infinite-loops instead of failing safe is the wrong shape for a fail-closed contract.
**Fix:** Break when `reason` can no longer absorb the overflow, so oversized bounded fields degrade instead of hanging:
```python
while len(data) > TERMINATION_STUB_MAX_BYTES and reason:
    overflow = len(data) - TERMINATION_STUB_MAX_BYTES
    keep = max(0, len(reason) - overflow - len("...(truncated)"))
    new_reason = reason[:keep] + "...(truncated)"
    if new_reason == reason:  # reason already minimal; overflow is elsewhere
        break
    reason = new_reason
    stub["reason"] = reason
    data = json.dumps(stub).encode("utf-8")
```
(Optionally also cap `gate_decision`/`reason` inputs so a mis-sized bounded field can't silently defeat the guard.)

### WR-02: Size-boundary parity drift — Python permits a 4096-byte stub; Go's invariant is strictly `< 4096`

**File:** `cmd/tide-langgraph-verifier/verifier/envelope.py:194` (vs `pkg/dispatch/envelope_test.go:825`)
**Issue:** The Python loop condition is `while len(data) > TERMINATION_STUB_MAX_BYTES` (`> 4096`), so it accepts a serialized stub of **exactly 4096 bytes**. Go's documented/tested invariant is strictly less than 4096 — `TestNewTerminationStub_StaysSmall` fails on `len(data) >= 4096`. **Empirically confirmed:** `write_termination_stub(..., reason="X"*20000, gate_decision="BLOCKED", ...)` terminates at exactly 4096 bytes and Python accepts it — a stub that Go's own guard would reject. For a phase whose scope explicitly names the "size boundary," the two implementations disagree by one byte at that boundary. Practical K8s impact is marginal (kubelet reads up to 4096 bytes, so exactly 4096 is still fully readable), but the shared 4 KB termination-message contract should be enforced identically on both sides.
**Fix:** Align Python with the Go `< 4096` invariant:
```python
while len(data) >= TERMINATION_STUB_MAX_BYTES and reason:
```

## Info

### IN-01: TerminationStub JSON-shape drift — Python always emits verdict keys, Go omits them

**File:** `cmd/tide-langgraph-verifier/verifier/envelope.py:185-191` (vs `pkg/dispatch/envelope.go:456-467`)
**Issue:** Python's `write_termination_stub` writes `gateDecision`/`findingsCount`/`highSeverityCount` unconditionally, while the Go `TerminationStub` tags them `omitempty` (so a non-verifier dispatch's stub omits all three). The shapes are functionally compatible — Go unmarshal tolerates zero-value keys, and the extra keys add ~60 bytes (negligible vs the cap) — but the same logical stub serializes differently across languages on the shared termination-message wire. If the verifier image ever writes a non-verdict stub, the drift is observable.
**Fix:** Either omit the three keys when they are empty/zero (to match Go's `omitempty`), or add a note that the drift is intentional and Go's tolerant reader is the contract.

### IN-02: `verify.gateCommand` cross-language shape contradiction (string vs list)

**File:** `cmd/tide-langgraph-verifier/verifier/tests/test_verdict.py:69` / `conftest.py` (vs `pkg/dispatch/envelope.go:397-399`)
**Issue:** Go's `VerifyContext.GateCommand` is a `string` (`json:"gateCommand"`; Go tests use `"go test ./..."` / `"make test-int"`). The Python `test_verify_extraction_round_trips` fixture passes a **list**: `verify={"gateCommand": ["make", "test"]}`. It passes today only because Python treats `verify` as an opaque dict this phase (accept-and-ignore), but the encoded shape contradicts the Go wire contract for the same field. When Phase 51 gives Python a typed `VerifyContext`, string-vs-list will collide.
**Fix:** Make the Python fixture use a string (`"gateCommand": "make test"`) to match the Go contract, or resolve now whether `gateCommand` is a scalar shell string or an argv list and align both sides.

### IN-03: Python `classify_verdict` test table missing positive controls Go has

**File:** `cmd/tide-langgraph-verifier/verifier/tests/test_verdict.py:55-65`
**Issue:** `test_classify_verdict_fails_closed` covers empty/missing/malformed/`APPROVED`, but omits (a) a `REPAIRABLE` positive control and (b) an explicit recognized-JSON-but-unknown-verdict-string case (`{"verdict":"REJECTED"}`) — both of which the Go suite pins (`TestClassifyVerdict_FailsClosed`'s `ValidRepairable` row and `TestClassifyVerdict_UnrecognizedVerdictField`). The classifier itself is correct (verified empirically), but the Python table proves less than its Go twin, weakening the "1:1 across languages" claim the module docstring makes.
**Fix:** Add the two rows so the cross-language parametrized tables match row-for-row.

### IN-04: `--stage-envelopes` UID has no traversal guard while `destPrefix` does

**File:** `cmd/tide-push/main.go:183-201` (consumed at `stageEnvelopeArtifacts`, main.go:1145)
**Issue:** `parseStageEnvelopes` validates `destPrefix` with a DNS-1123-style regex plus a `filepath.Clean` containment check against `.tide/planning/`, but validates the `uid` only for non-emptiness. Both feed filesystem paths — `srcDir := filepath.Join(cfg.Workspace, "envelopes", es.UID)` is read (stat/glob/copy) by the new `task` branch and the pre-existing planner branch alike. A `../`-laden UID would let staging read outside `envelopes/`. Not exploitable in production — `collectStageEnvelopes` sources UIDs from `string(obj.GetUID())` (server-generated K8s UUIDs), and the flag is controller-supplied, not user-facing — and it is pre-existing, but the new `task` path inherits the asymmetry and the inconsistent validation is a defense-in-depth gap.
**Fix:** Apply the same "no `\\`, no `..` after `filepath.Clean`, must stay under `envelopes/`" containment check to `uid` that `destPrefix` already gets.

### IN-05: Go `NewTerminationStub` performs no runtime size truncation (asymmetric with Python)

**File:** `pkg/dispatch/envelope.go:484-506`
**Issue:** Unlike the Python helper, the Go `NewTerminationStub` copies `out.Reason` verbatim with no truncation and no size enforcement — the `< 4096` invariant is only asserted by a test that uses an empty `Reason`. A pathologically long `EnvelopeOut.Reason` would produce an oversized stub that K8s truncates to 4096 bytes, corrupting the JSON the Manager parses. `Reason` is a bounded structured code by convention (`"forced-failure"`, `"cap-hit"`), so this is latent and pre-existing, but it is the mirror image of WR-01/WR-02: Python truncates (and can hang), Go never truncates (and can overflow). Worth reconciling the two size-safety strategies when Phase 51 wires the verdict path end-to-end.
**Fix:** If runtime overflow is a real concern for verifier dispatches, add a symmetric bounded-truncation step (or an assertion) to the Go path so both languages guarantee the same ≤4 KB result.

---

_Reviewed: 2026-07-18T22:32:27Z_
_Reviewer: Claude (gsd-code-reviewer)_
_Depth: standard_
