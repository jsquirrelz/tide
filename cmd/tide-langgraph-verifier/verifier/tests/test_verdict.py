"""Tests for verifier.verdict — the Pydantic GateDecision/Finding pair + the
fail-closed classify_verdict classifier (D-02/D-04), proven against the
shared Go golden fixture pkg/dispatch/testdata/gate_decision_golden.json.

Also covers the `verify` extraction on EnvelopeIn and the verdict-carrying
write_termination_stub extension (D-03/D-05a) — both live here rather than
test_envelope.py per this plan's files_modified scope.
"""

from __future__ import annotations

import json
import threading
from pathlib import Path

import pytest

from verifier import envelope, verdict


def test_golden_fixture_round_trip() -> None:
    """GateDecision.model_validate_json on the SAME
    pkg/dispatch/testdata/gate_decision_golden.json Plan 49-02 authored
    (D-02) — the cross-language parity proof. Value-equivalence, not a raw
    byte compare (key order differs across Go/Python serializers)."""
    golden_bytes = verdict.GOLDEN_FIXTURE.read_bytes()

    decoded = verdict.GateDecision.model_validate_json(golden_bytes)

    assert decoded.verdict == verdict.Verdict.REPAIRABLE
    assert decoded.summary
    assert len(decoded.findings) >= 1
    finding = decoded.findings[0]
    assert finding.dimension
    assert finding.severity
    assert finding.confidence
    assert finding.evidence
    assert finding.suggested_fix

    # Re-dump and re-validate to prove value-equivalence (NOT byte compare —
    # key order differs across Go/Python JSON serializers).
    redumped = decoded.model_dump_json(by_alias=True)
    reparsed = verdict.GateDecision.model_validate_json(redumped)
    assert reparsed == decoded


def test_gate_decision_dict_factory_produces_valid_decision(gate_decision_dict) -> None:
    payload = gate_decision_dict()

    decoded = verdict.GateDecision.model_validate(payload)

    assert decoded.verdict == verdict.Verdict.REPAIRABLE
    assert verdict.classify_verdict(json.dumps(payload)) == verdict.Verdict.REPAIRABLE


@pytest.mark.parametrize(
    "raw,want",
    [
        ("", verdict.Verdict.BLOCKED),
        ('{"summary":"looks fine","findings":[]}', verdict.Verdict.BLOCKED),
        ("{not valid json", verdict.Verdict.BLOCKED),
        ('{"verdict":"APPROVED","summary":"ok","findings":[]}', verdict.Verdict.APPROVED),
        # REPAIRABLE positive control — mirrors Go's ValidRepairable row (IN-03),
        # proving the classifier returns each terminal, not only APPROVED/BLOCKED.
        (
            '{"verdict":"REPAIRABLE","summary":"needs a fix","findings":[]}',
            verdict.Verdict.REPAIRABLE,
        ),
        # Recognized JSON, unknown verdict string → fails closed to BLOCKED via
        # the classifier's ValueError branch; mirrors Go's
        # TestClassifyVerdict_UnrecognizedVerdictField (IN-03).
        ('{"verdict":"REJECTED","summary":"stale vocabulary"}', verdict.Verdict.BLOCKED),
    ],
)
def test_classify_verdict_fails_closed(raw: str, want: verdict.Verdict) -> None:
    assert verdict.classify_verdict(raw) == want


def test_verify_extraction_round_trips(tmp_path: Path, envelope_in_dict) -> None:
    # gateCommand is a STRING on the Go wire contract (VerifyContext.GateCommand
    # is `string`, envelope.go:399; Go tests use "go test ./..."), not an argv
    # list — keep the Python fixture's encoded shape identical to Go (IN-02).
    payload = envelope_in_dict(verify={"gateCommand": "make test"})
    in_path = tmp_path / "in.json"
    in_path.write_text(json.dumps(payload))

    env = envelope.read_envelope_in(in_path)

    assert env.verify == {"gateCommand": "make test"}


def test_verify_missing_parses_fine(tmp_path: Path, envelope_in_dict) -> None:
    payload = envelope_in_dict()
    in_path = tmp_path / "in.json"
    in_path.write_text(json.dumps(payload))

    env = envelope.read_envelope_in(in_path)

    assert env.verify is None


@pytest.mark.parametrize("bad_verify", ["not-an-object", 42, ["verify"]])
def test_verify_rejects_non_object(tmp_path: Path, envelope_in_dict, bad_verify) -> None:
    """WR-01 fail-closed precedent applied to `verify`: a non-object value
    must raise EnvelopeError, never an uncaught AttributeError."""
    payload = envelope_in_dict(verify=bad_verify)
    in_path = tmp_path / "in.json"
    in_path.write_text(json.dumps(payload))

    with pytest.raises(envelope.EnvelopeError, match="verify"):
        envelope.read_envelope_in(in_path)


def test_read_envelope_in_still_tolerates_unknown_fields_with_verify(
    tmp_path: Path, envelope_in_dict
) -> None:
    """Adding `verify` must not weaken the accept-and-ignore contract for
    OTHER unknown top-level keys (test_envelope.py:78-86 stays green)."""
    payload = envelope_in_dict(futureField="something-phase-49-adds")
    in_path = tmp_path / "in.json"
    in_path.write_text(json.dumps(payload))

    env = envelope.read_envelope_in(in_path)

    assert env.raw["futureField"] == "something-phase-49-adds"


def test_write_termination_stub_with_verdict_fields_stays_small(tmp_path: Path) -> None:
    stub_path = tmp_path / "termination-log"

    envelope.write_termination_stub(
        stub_path,
        exit_code=0,
        gate_decision="REPAIRABLE",
        findings_count=3,
        high_severity_count=1,
    )

    data = stub_path.read_bytes()
    assert len(data) < envelope.TERMINATION_STUB_MAX_BYTES
    parsed = json.loads(data)
    assert parsed["gateDecision"] == "REPAIRABLE"
    assert parsed["findingsCount"] == 3
    assert parsed["highSeverityCount"] == 1


def _write_stub_with_timeout(timeout: float = 8.0, **kwargs) -> bool:
    """Run write_termination_stub on a daemon thread and join with a timeout.

    Returns True if it returned, False if it was still running at `timeout` —
    the direct regression proof for WR-01 (a spinning truncation loop never
    joins). No pytest-timeout plugin is in the lockfiles, so a bare hang would
    stall the whole suite; the daemon thread lets a regression fail fast
    instead (the abandoned thread dies with the interpreter)."""
    done = threading.Event()

    def _run() -> None:
        envelope.write_termination_stub(**kwargs)
        done.set()

    worker = threading.Thread(target=_run, daemon=True)
    worker.start()
    worker.join(timeout)
    return done.is_set()


def test_write_termination_stub_does_not_hang_on_oversized_bounded_field(
    tmp_path: Path,
) -> None:
    """WR-01 regression: when a NON-reason field (gateDecision) alone exceeds
    the 4 KB cap, the truncation loop must not spin forever. reason is dropped
    entirely once the "...(truncated)" marker can no longer fit, which trips
    the `and reason` guard so the finalizer returns — a hung finalizer never
    writes the termination message. The doc itself can still exceed the cap
    here (gateDecision alone overflows — an upstream-misuse case K8s
    truncates), so this asserts graceful termination + reason-drop, not the
    <4096 bound."""
    stub_path = tmp_path / "termination-log"

    returned = _write_stub_with_timeout(
        path=stub_path,
        exit_code=1,
        reason="short",
        gate_decision="Z" * 5000,  # bounded enum by contract; oversized here to prove the guard
        findings_count=1,
        high_severity_count=1,
    )

    assert returned, "write_termination_stub hung (WR-01 infinite truncation loop)"
    parsed = json.loads(stub_path.read_bytes())
    assert parsed["reason"] == ""  # dropped entirely once the marker no longer fits


def test_write_termination_stub_truncates_huge_reason_strictly_under_cap(
    tmp_path: Path,
) -> None:
    """WR-02 parity + boundary: a huge reason with a bounded gateDecision is
    truncated until the doc is STRICTLY < 4096 bytes — matching Go's
    TestNewTerminationStub_StaysSmall (`len(data) < 4096`), never the exact
    4096 the old `> 4096` condition permitted."""
    stub_path = tmp_path / "termination-log"

    returned = _write_stub_with_timeout(
        path=stub_path,
        exit_code=1,
        reason="X" * 20_000,
        gate_decision="REPAIRABLE",
        findings_count=3,
        high_severity_count=1,
    )

    assert returned, "write_termination_stub hung (WR-01/WR-02 truncation loop)"
    data = stub_path.read_bytes()
    # Strictly under, not <= : the one-byte parity fix with Go's < 4096.
    assert len(data) < envelope.TERMINATION_STUB_MAX_BYTES
    assert len(data) != envelope.TERMINATION_STUB_MAX_BYTES
