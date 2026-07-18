"""Tests for verifier.envelope — the wire-shape re-implementation + strict
validation (D-03)."""

from __future__ import annotations

import json
import stat
from pathlib import Path

import pytest

from verifier import envelope


def test_read_envelope_in_valid_round_trip(tmp_path: Path, envelope_in_dict) -> None:
    payload = envelope_in_dict()
    in_path = tmp_path / "in.json"
    in_path.write_text(json.dumps(payload))

    env = envelope.read_envelope_in(in_path)

    assert env.task_uid == payload["taskUID"]
    assert env.role == payload["role"]
    assert env.level == payload["level"]
    assert env.prompt == payload["prompt"]
    assert env.provider_vendor == payload["provider"]["vendor"]
    assert env.provider_model == payload["provider"]["model"]


def test_read_envelope_in_rejects_skewed_api_version(tmp_path: Path, envelope_in_dict) -> None:
    payload = envelope_in_dict(apiVersion="dispatch.tideproject.k8s/v1alpha2")
    in_path = tmp_path / "in.json"
    in_path.write_text(json.dumps(payload))

    with pytest.raises(envelope.EnvelopeError, match="v1alpha1"):
        envelope.read_envelope_in(in_path)


def test_read_envelope_in_rejects_wrong_kind(tmp_path: Path, envelope_in_dict) -> None:
    payload = envelope_in_dict(kind="TaskEnvelopeOut")
    in_path = tmp_path / "in.json"
    in_path.write_text(json.dumps(payload))

    with pytest.raises(envelope.EnvelopeError, match="TaskEnvelopeIn"):
        envelope.read_envelope_in(in_path)


def test_read_envelope_in_rejects_malformed_json(tmp_path: Path) -> None:
    in_path = tmp_path / "in.json"
    in_path.write_text("{not valid json")

    with pytest.raises(envelope.EnvelopeError):
        envelope.read_envelope_in(in_path)


def test_read_envelope_in_missing_file_raises_missing_error(tmp_path: Path) -> None:
    missing_path = tmp_path / "does-not-exist.json"

    with pytest.raises(envelope.EnvelopeMissingError):
        envelope.read_envelope_in(missing_path)


def test_read_envelope_in_tolerates_unknown_fields(tmp_path: Path, envelope_in_dict) -> None:
    payload = envelope_in_dict(futureField="something-phase-49-adds")
    in_path = tmp_path / "in.json"
    in_path.write_text(json.dumps(payload))

    env = envelope.read_envelope_in(in_path)

    assert env.task_uid == payload["taskUID"]
    assert env.raw["futureField"] == "something-phase-49-adds"


def test_write_envelope_out_trivial_shape(tmp_path: Path) -> None:
    out_path = tmp_path / "out.json"

    envelope.write_envelope_out(out_path, exit_code=0, result="ran the gate command")

    data = json.loads(out_path.read_text())
    assert data == {
        "apiVersion": envelope.API_VERSION,
        "kind": envelope.KIND_OUT,
        "exitCode": 0,
        "result": "ran the gate command",
    }
    assert "git" not in data
    assert "childCRDs" not in data
    assert stat.S_IMODE(out_path.stat().st_mode) == 0o644


def test_write_envelope_out_includes_reason_when_nonzero(tmp_path: Path) -> None:
    out_path = tmp_path / "out.json"

    envelope.write_envelope_out(out_path, exit_code=1, result="agent failed", reason="tool-error")

    data = json.loads(out_path.read_text())
    assert data["reason"] == "tool-error"


def test_write_termination_stub_enforces_size_cap(tmp_path: Path) -> None:
    stub_path = tmp_path / "termination-log"

    envelope.write_termination_stub(stub_path, exit_code=1, reason="x" * 10_000)

    data = stub_path.read_bytes()
    assert len(data) <= envelope.TERMINATION_STUB_MAX_BYTES
    parsed = json.loads(data)
    assert parsed["exitCode"] == 1
