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
from verifier import verdict


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
