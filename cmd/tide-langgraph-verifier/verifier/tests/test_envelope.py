"""Tests for verifier.envelope — the wire-shape re-implementation + strict
validation (D-03)."""

from __future__ import annotations

import json
import stat
import threading
from pathlib import Path

import pytest

from verifier import envelope


def _write_stub_with_timeout(timeout: float = 8.0, **kwargs) -> bool:
    """Run write_termination_stub on a daemon thread and join with a timeout.

    Returns True if it returned, False if it was still running at `timeout` —
    the direct regression proof for WR-01 (a spinning truncation loop never
    joins). Duplicated from test_verdict.py's identical helper — no
    cross-test-module imports exist in this package today, so this module
    carries its own copy rather than establishing a new import edge."""
    done = threading.Event()

    def _run() -> None:
        envelope.write_termination_stub(**kwargs)
        done.set()

    worker = threading.Thread(target=_run, daemon=True)
    worker.start()
    worker.join(timeout)
    return done.is_set()


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


@pytest.mark.parametrize("bad_provider", ["anthropic", 42, ["anthropic"]])
def test_read_envelope_in_rejects_non_object_provider(
    tmp_path: Path, envelope_in_dict, bad_provider
) -> None:
    """WR-01 regression: a non-object `provider` (string/number/array) must
    raise EnvelopeError (the fail-closed path), never an uncaught
    AttributeError that skips the structured termination stub."""
    payload = envelope_in_dict(provider=bad_provider)
    in_path = tmp_path / "in.json"
    in_path.write_text(json.dumps(payload))

    with pytest.raises(envelope.EnvelopeError, match="provider"):
        envelope.read_envelope_in(in_path)


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
        "terminalReason": "",
    }
    assert "git" not in data
    assert "childCRDs" not in data
    assert "loopRunID" not in data
    assert "attemptID" not in data
    assert "runEvidence" not in data
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


def test_write_envelope_out_mirrors_go_camelcase_keys(tmp_path: Path) -> None:
    """Phase 50 D-02/D-03: terminal_reason/loop_run_id/attempt_id/run_evidence
    join the JSON document under the exact Go camelCase spellings —
    terminalReason/loopRunID/attemptID/runEvidence."""
    out_path = tmp_path / "out.json"

    envelope.write_envelope_out(
        out_path,
        exit_code=1,
        result="cap-exceeded during agent loop",
        terminal_reason="blocked",
        loop_run_id="uid-0001",
        attempt_id="uid-0001-2",
        run_evidence={"specID": "spec-001", "commands": ["go test ./..."]},
    )

    data = json.loads(out_path.read_text())
    assert data["terminalReason"] == "blocked"
    assert data["loopRunID"] == "uid-0001"
    assert data["attemptID"] == "uid-0001-2"
    assert data["runEvidence"] == {"specID": "spec-001", "commands": ["go test ./..."]}


def test_write_envelope_out_terminal_reason_never_defaults(tmp_path: Path) -> None:
    """Phase 50 D-02: an unset terminal_reason writes as "" — NEVER silently
    defaults to "completed" (fail-closed, mirroring verdict.classify_verdict)."""
    out_path = tmp_path / "out.json"

    envelope.write_envelope_out(out_path, exit_code=0, result="ran the gate command")

    data = json.loads(out_path.read_text())
    assert data["terminalReason"] == ""
    assert data["terminalReason"] != "completed"


def test_write_termination_stub_with_loop_fields_stays_small(tmp_path: Path) -> None:
    """Phase 50 D-02/D-03 + WR-01/WR-02 shape: terminal_reason +
    changed_file_count join the stub unconditionally and survive truncation
    of a 10KB reason, staying strictly under the 4096-byte termination-
    message budget. Uses the daemon-thread timeout guard so a regression to
    the WR-01 infinite-truncation-loop bug fails fast instead of hanging the
    whole suite."""
    stub_path = tmp_path / "termination-log"

    returned = _write_stub_with_timeout(
        path=stub_path,
        exit_code=1,
        reason="x" * 10_000,
        terminal_reason="cap_exceeded",
        changed_file_count=500,
    )

    assert returned, "write_termination_stub hung (WR-01 infinite truncation loop)"
    data = stub_path.read_bytes()
    assert len(data) < envelope.TERMINATION_STUB_MAX_BYTES
    parsed = json.loads(data)
    assert parsed["terminalReason"] == "cap_exceeded"
    assert parsed["changedFileCount"] == 500
    # The 10KB reason must have actually been truncated (never left intact).
    assert len(parsed["reason"]) < 10_000


def test_envelope_out_golden_fixture_parity(tmp_path: Path) -> None:
    """Phase 50 D-02/D-03: read the SAME Go-authored
    pkg/dispatch/testdata/envelope_out_golden.json Plan 50-01 wrote — the
    cross-language parity proof. The fixture deliberately pins a
    non-"completed" terminalReason so a silent-default bug in either
    language surfaces as a value mismatch here."""
    golden = json.loads(envelope.ENVELOPE_OUT_GOLDEN_FIXTURE.read_bytes())

    assert golden["terminalReason"] == "cap_exceeded"
    assert golden["loopRunID"]
    assert golden["attemptID"]
    assert golden["attemptID"].rsplit("-", 1)[-1].isdigit(), (
        "attemptID must end in a '-<digit>' attempt suffix"
    )

    run_evidence = golden["runEvidence"]
    for key in (
        "specID",
        "lockingCommit",
        "commands",
        "evaluatorVersions",
        "changedFiles",
        "changedFileTotal",
        "model",
        "promptVersion",
        "runtimeVersion",
    ):
        assert run_evidence.get(key), f"runEvidence.{key} must be non-empty/non-zero"

    first_changed_file = run_evidence["changedFiles"][0]
    assert "path" in first_changed_file
    assert "status" in first_changed_file

    # Round-trip: pass the fixture's own values through write_envelope_out
    # and assert the re-read JSON is value-equivalent for every mirrored key
    # — never a byte compare, since key order differs across serializers.
    out_path = tmp_path / "out.json"
    envelope.write_envelope_out(
        out_path,
        exit_code=golden["exitCode"],
        result=golden["result"],
        reason=golden.get("reason", ""),
        terminal_reason=golden["terminalReason"],
        loop_run_id=golden["loopRunID"],
        attempt_id=golden["attemptID"],
        run_evidence=run_evidence,
    )
    reread = json.loads(out_path.read_text())
    assert reread["terminalReason"] == golden["terminalReason"]
    assert reread["loopRunID"] == golden["loopRunID"]
    assert reread["attemptID"] == golden["attemptID"]
    assert reread["runEvidence"] == golden["runEvidence"]
