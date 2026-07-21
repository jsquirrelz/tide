"""Tests for the findings.json artifact writer (Phase 53-11, T-53-28).

53-03-SUMMARY.md's "Next Phase Readiness" section surfaced a gap:
`verifier/__main__.py` never wrote `findings.json` — only `out.json` + the
termination stub — while `tide-push`'s fail-closed task-kind staging guard
(cmd/tide-push/main.go:1242-1252) hard-fails the ENTIRE cumulative push
whenever a task envelope directory that recorded `LastEvaluation` lacks
`findings.json`. This file pins the alignment invariant this plan closes:
findings.json presence on disk tracks the controller's `LastEvaluation`
predicate (`out.Verdict != nil`, i.e. `verdict_out is not None`) 1:1 — every
parseable verdict path writes it, every degraded/no-verdict path writes
nothing, and a write failure on the findings side never masks the out.json/
termination-stub relay.
"""

from __future__ import annotations

import json
from pathlib import Path

from verifier import __main__ as entrypoint
from verifier import envelope, verdict
from verifier.tests.conftest import GOLDEN_FIXTURE


def _approved_llm_result(summary: str = "LLM says fine") -> str:
    return json.dumps({"verdict": "APPROVED", "summary": summary, "findings": []})


# ---------------------------------------------------------------------------
# Task 1: verdict paths write findings.json beside out.json.
# ---------------------------------------------------------------------------


def test_verdict_path_writes_findings_beside_out(
    tmp_path: Path, monkeypatch, fixture_worktree: Path, envelope_in_dict
) -> None:
    """All-green commands + LLM APPROVED: findings.json exists in the SAME
    directory as out.json, parses, GateDecision.model_validate accepts it,
    and it equals out.json's "verdict" object exactly — no drift between the
    relayed verdict and the staged findings artifact."""
    monkeypatch.setenv("TIDE_WORKTREE_DIR", str(fixture_worktree))
    monkeypatch.setenv("TIDE_GATE_COMMAND", "pytest-leak-guard")
    in_path = tmp_path / "in.json"
    in_path.write_text(
        json.dumps(
            envelope_in_dict(
                provider={"vendor": "langgraph", "model": "claude-sonnet-4-6"},
                verify={"gateCommand": "true", "commands": ["true", "true"]},
            )
        )
    )
    stub_path = tmp_path / "termination-log"

    exit_code = entrypoint.main(
        in_path=str(in_path),
        termination_message_path=str(stub_path),
        build_model=lambda env: object(),
        run_agent_fn=lambda model, prompt: _approved_llm_result(),
    )

    assert exit_code == 0
    out = json.loads((tmp_path / "out.json").read_text())
    findings_path = tmp_path / "findings.json"
    assert findings_path.exists()
    findings_doc = json.loads(findings_path.read_text())
    verdict.GateDecision.model_validate(findings_doc)
    assert findings_doc == out["verdict"]


def test_dominance_rewrite_writes_findings_too(
    tmp_path: Path, monkeypatch, fixture_worktree: Path, envelope_in_dict
) -> None:
    """A red gate command among green ones + LLM APPROVED still forces a
    REPAIRABLE/BLOCKED verdict (T-51-02 dominance) — and findings.json still
    gets written, carrying the gate-command blocker finding with its
    exit_code, proving rewritten verdicts stage identically to clean ones."""
    monkeypatch.setenv("TIDE_WORKTREE_DIR", str(fixture_worktree))
    monkeypatch.setenv("TIDE_GATE_COMMAND", "pytest-leak-guard")
    in_path = tmp_path / "in.json"
    in_path.write_text(
        json.dumps(
            envelope_in_dict(
                provider={"vendor": "langgraph", "model": "claude-sonnet-4-6"},
                verify={"gateCommand": "true", "commands": ["true", "false", "true"]},
            )
        )
    )
    stub_path = tmp_path / "termination-log"

    exit_code = entrypoint.main(
        in_path=str(in_path),
        termination_message_path=str(stub_path),
        build_model=lambda env: object(),
        run_agent_fn=lambda model, prompt: _approved_llm_result(),
    )

    assert exit_code == 0
    findings_path = tmp_path / "findings.json"
    assert findings_path.exists()
    findings_doc = json.loads(findings_path.read_text())
    assert findings_doc["verdict"] in ("REPAIRABLE", "BLOCKED")
    gate_findings = [f for f in findings_doc["findings"] if f["dimension"] == "gate-command"]
    assert len(gate_findings) == 1
    assert "exit_code=1" in gate_findings[0]["evidence"]


def test_empty_commands_blocked_arm_writes_findings_too(
    tmp_path: Path, monkeypatch, fixture_worktree: Path, envelope_in_dict
) -> None:
    """env.verify present but commands == [] stays fail-closed BLOCKED — and
    still writes findings.json (verdict BLOCKED, findings []): a parseable
    verdict is a parseable verdict, however fail-closed."""
    monkeypatch.setenv("TIDE_WORKTREE_DIR", str(fixture_worktree))
    in_path = tmp_path / "in.json"
    in_path.write_text(
        json.dumps(
            envelope_in_dict(
                provider={"vendor": "langgraph", "model": "claude-sonnet-4-6"},
                verify={"gateCommand": "", "commands": []},
            )
        )
    )
    stub_path = tmp_path / "termination-log"

    exit_code = entrypoint.main(
        in_path=str(in_path),
        termination_message_path=str(stub_path),
        build_model=lambda env: object(),
        run_agent_fn=lambda model, prompt: _approved_llm_result("nothing to check"),
    )

    assert exit_code == 0
    findings_path = tmp_path / "findings.json"
    assert findings_path.exists()
    findings_doc = json.loads(findings_path.read_text())
    assert findings_doc["verdict"] == "BLOCKED"
    assert findings_doc["findings"] == []


# ---------------------------------------------------------------------------
# Task 2(a): no-verdict paths write NOTHING.
# ---------------------------------------------------------------------------


def test_no_verify_context_writes_no_findings(
    tmp_path: Path, monkeypatch, fixture_worktree: Path, envelope_in_dict
) -> None:
    """env.verify absent entirely: verdict_out stays None, main() returns 0,
    out.json has no "verdict" key. The controller's applyLoopStatus never
    records LastEvaluation for this envelope (out.Verdict == nil via the
    role-aware ReadVerifierOut relay), so taskFindingsStageable excludes the
    Task and tide-push never probes this dir — findings.json must not exist."""
    monkeypatch.setenv("TIDE_WORKTREE_DIR", str(fixture_worktree))
    in_path = tmp_path / "in.json"
    in_path.write_text(
        json.dumps(envelope_in_dict(provider={"vendor": "langgraph", "model": "claude-sonnet-4-6"}))
    )
    stub_path = tmp_path / "termination-log"

    exit_code = entrypoint.main(
        in_path=str(in_path),
        termination_message_path=str(stub_path),
        build_model=lambda env: object(),
        run_agent_fn=lambda model, prompt: "the gate command passed",
    )

    assert exit_code == 0
    out = json.loads((tmp_path / "out.json").read_text())
    assert "verdict" not in out
    assert not (tmp_path / "findings.json").exists()


def test_envelope_missing_writes_no_findings(tmp_path: Path) -> None:
    """_fail's envelope-missing arm (exit 1, no verdict ever assembled): the
    controller never records LastEvaluation for a task that never produced a
    readable envelope, so tide-push never probes for findings.json."""
    missing_in_path = tmp_path / "in.json"
    stub_path = tmp_path / "termination-log"

    exit_code = entrypoint.main(in_path=str(missing_in_path), termination_message_path=str(stub_path))

    assert exit_code != 0
    assert not (tmp_path / "findings.json").exists()


def test_unsupported_vendor_writes_no_findings(tmp_path: Path, envelope_in_dict) -> None:
    """_fail's unsupported-vendor arm (exit 1, no verdict ever assembled):
    same absence-is-correct reasoning as envelope-missing."""
    in_path = tmp_path / "in.json"
    in_path.write_text(json.dumps(envelope_in_dict(provider={"vendor": "openai", "model": "gpt-x"})))
    stub_path = tmp_path / "termination-log"

    exit_code = entrypoint.main(in_path=str(in_path), termination_message_path=str(stub_path))

    assert exit_code != 0
    assert not (tmp_path / "findings.json").exists()


def test_agent_error_writes_no_findings(tmp_path: Path, envelope_in_dict) -> None:
    """_fail's agent-error arm (exit 1, no verdict ever assembled): the agent
    raised before result_text existed, so verdict_out stays None and the
    findings-write gate is never reached — same absence-is-correct
    reasoning as envelope-missing/unsupported-vendor."""
    in_path = tmp_path / "in.json"
    in_path.write_text(
        json.dumps(envelope_in_dict(provider={"vendor": "langgraph", "model": "claude-sonnet-4-6"}))
    )
    stub_path = tmp_path / "termination-log"

    def _raising_run_agent(model, prompt):  # noqa: ARG001
        raise RuntimeError("boom")

    exit_code = entrypoint.main(
        in_path=str(in_path),
        termination_message_path=str(stub_path),
        build_model=lambda env: object(),
        run_agent_fn=_raising_run_agent,
    )

    assert exit_code != 0
    assert not (tmp_path / "findings.json").exists()


# ---------------------------------------------------------------------------
# Task 2(b): a findings.json OSError never masks the out.json/stub relay.
# ---------------------------------------------------------------------------


def test_findings_write_oserror_never_masks_the_relay(
    tmp_path: Path, monkeypatch, fixture_worktree: Path, envelope_in_dict
) -> None:
    """A raising envelope.write_findings on an otherwise-green verdict path
    must not affect main()'s return code or the out.json/termination-stub
    relay: main() still returns 0, out.json still carries the full verdict
    object, and the stub still carries gateDecision/findingsCount. The
    resulting findings.json absence is the divergence tide-push's
    fail-closed guard (cmd/tide-push/main.go:1242-1252) exists to catch —
    deliberately NOT softened here."""
    monkeypatch.setenv("TIDE_WORKTREE_DIR", str(fixture_worktree))
    monkeypatch.setenv("TIDE_GATE_COMMAND", "pytest-leak-guard")

    def _raising_write_findings(path, *, verdict):  # noqa: ARG001
        raise OSError("disk full")

    monkeypatch.setattr(entrypoint.envelope, "write_findings", _raising_write_findings)

    in_path = tmp_path / "in.json"
    in_path.write_text(
        json.dumps(
            envelope_in_dict(
                provider={"vendor": "langgraph", "model": "claude-sonnet-4-6"},
                verify={"gateCommand": "true", "commands": ["true", "true"]},
            )
        )
    )
    stub_path = tmp_path / "termination-log"

    exit_code = entrypoint.main(
        in_path=str(in_path),
        termination_message_path=str(stub_path),
        build_model=lambda env: object(),
        run_agent_fn=lambda model, prompt: _approved_llm_result(),
    )

    assert exit_code == 0
    out = json.loads((tmp_path / "out.json").read_text())
    assert out["verdict"]["verdict"] == "APPROVED"
    stub = json.loads(stub_path.read_text())
    assert stub["gateDecision"] == "APPROVED"
    assert stub["findingsCount"] == 0
    assert not (tmp_path / "findings.json").exists()


# ---------------------------------------------------------------------------
# Task 2(c): golden round-trip — write_findings neither adds nor drops
# semantic content.
# ---------------------------------------------------------------------------


def test_write_findings_golden_round_trip(tmp_path: Path) -> None:
    """write_findings writes the shared gate_decision_golden.json (the same
    fixture both languages prove against) field-for-field unaltered, and the
    re-read document still validates as a GateDecision."""
    golden_dict = json.loads(GOLDEN_FIXTURE.read_text())
    findings_path = tmp_path / "findings.json"

    envelope.write_findings(findings_path, verdict=golden_dict)

    re_read = json.loads(findings_path.read_text())
    assert re_read == golden_dict
    verdict.GateDecision.model_validate(re_read)
