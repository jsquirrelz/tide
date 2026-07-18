"""Tests for verifier.verdict — the Pydantic GateDecision/Finding pair + the
fail-closed classify_verdict classifier (D-02/D-04), proven against the
shared Go golden fixture pkg/dispatch/testdata/gate_decision_golden.json.

Also covers the `verify` extraction on EnvelopeIn and the verdict-carrying
write_termination_stub extension (D-03/D-05a) — both live here rather than
test_envelope.py per this plan's files_modified scope.
"""

from __future__ import annotations

import json
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
    ],
)
def test_classify_verdict_fails_closed(raw: str, want: verdict.Verdict) -> None:
    assert verdict.classify_verdict(raw) == want


def test_verify_extraction_round_trips(tmp_path: Path, envelope_in_dict) -> None:
    payload = envelope_in_dict(verify={"gateCommand": ["make", "test"]})
    in_path = tmp_path / "in.json"
    in_path.write_text(json.dumps(payload))

    env = envelope.read_envelope_in(in_path)

    assert env.verify == {"gateCommand": ["make", "test"]}


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
