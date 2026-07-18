"""Tests for verifier.verdict — the Pydantic GateDecision/Finding pair + the
fail-closed classify_verdict classifier (D-02/D-04), proven against the
shared Go golden fixture pkg/dispatch/testdata/gate_decision_golden.json."""

from __future__ import annotations

import json

import pytest

from verifier import verdict


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
