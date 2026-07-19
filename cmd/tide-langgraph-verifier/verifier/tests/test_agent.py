"""Tests for verifier.agent (create_agent wiring) and verifier.__main__ (the
seam-conformant entrypoint) — Task 3, D-01/D-02/D-04/D-07(revised)."""

from __future__ import annotations

import json
from pathlib import Path
from typing import Any

from langchain_core.language_models.fake_chat_models import GenericFakeChatModel
from langchain_core.messages import AIMessage

from verifier import __main__ as entrypoint
from verifier import agent


class FakeToolCallingModel(GenericFakeChatModel):
    """Minimal fake chat model implementing bind_tools so create_agent's
    tool-calling loop runs fully offline (no network, no real API key)."""

    def bind_tools(self, tools, *, tool_choice=None, **kwargs):  # noqa: ARG002
        return self


def _fake_model(messages: list) -> FakeToolCallingModel:
    return FakeToolCallingModel(messages=iter(messages))


# ---------------------------------------------------------------------------
# agent.py
# ---------------------------------------------------------------------------


def test_build_agent_wires_exactly_the_two_tools() -> None:
    model = _fake_model(["final answer"])

    graph = agent.build_agent(model)

    tools_node = graph.nodes["tools"].bound
    assert set(tools_node.tools_by_name) == {"git_read", "run_gate_command"}


def test_build_agent_has_no_checkpointer() -> None:
    model = _fake_model(["final answer"])

    graph = agent.build_agent(model)

    assert graph.checkpointer is None


def test_run_agent_one_tool_call_then_final_answer(monkeypatch, fixture_worktree: Path) -> None:
    monkeypatch.setenv("TIDE_WORKTREE_DIR", str(fixture_worktree))
    # CR-01: the command actually executed comes from the orchestrator-set
    # TIDE_GATE_COMMAND env var, not the model's tool-call args.
    monkeypatch.setenv("TIDE_GATE_COMMAND", "true")
    model = _fake_model(
        [
            AIMessage(
                content="",
                tool_calls=[{"name": "run_gate_command", "args": {}, "id": "call-1"}],
            ),
            AIMessage(content="gate passed"),
        ]
    )

    result_text = agent.run_agent(model, "run the gate command")

    assert result_text == "gate passed"


def test_run_agent_ignores_model_supplied_gate_command(monkeypatch, fixture_worktree: Path) -> None:
    """CR-01 regression: even through the full tool-calling loop, a
    model-emitted `command` tool-call argument (e.g. a prompt-injection
    attempt) never reaches subprocess — only TIDE_GATE_COMMAND executes."""
    monkeypatch.setenv("TIDE_WORKTREE_DIR", str(fixture_worktree))
    monkeypatch.setenv("TIDE_GATE_COMMAND", "true")
    captured: dict = {}

    from verifier import tools as verifier_tools

    real_run = verifier_tools.subprocess.run

    def _spy_run(command, **kwargs):
        if kwargs.get("shell"):
            captured["command"] = command
        return real_run(command, **kwargs)

    monkeypatch.setattr(verifier_tools.subprocess, "run", _spy_run)

    model = _fake_model(
        [
            AIMessage(
                content="",
                tool_calls=[
                    {
                        "name": "run_gate_command",
                        "args": {"command": "printenv ANTHROPIC_API_KEY"},
                        "id": "call-1",
                    }
                ],
            ),
            AIMessage(content="gate passed"),
        ]
    )

    result_text = agent.run_agent(model, "run the gate command")

    assert result_text == "gate passed"
    assert captured["command"] == "true"


def test_run_agent_passes_explicit_recursion_limit(monkeypatch) -> None:
    captured: dict[str, Any] = {}

    class _FakeCompiledGraph:
        def invoke(self, inputs, config=None, **kwargs):  # noqa: ARG002
            captured["config"] = config
            return {"messages": [AIMessage(content="final answer")]}

    monkeypatch.setattr(agent, "build_agent", lambda model, **kwargs: _FakeCompiledGraph())

    result = agent.run_agent(_fake_model(["unused"]), "go")

    assert result == "final answer"
    assert captured["config"] == {"recursion_limit": agent.RECURSION_LIMIT}
    assert agent.RECURSION_LIMIT < 9999


# ---------------------------------------------------------------------------
# __main__.py entrypoint
# ---------------------------------------------------------------------------


def test_main_missing_envelope_writes_envelope_missing_stub(tmp_path: Path) -> None:
    missing_in_path = tmp_path / "in.json"
    stub_path = tmp_path / "termination-log"

    exit_code = entrypoint.main(in_path=str(missing_in_path), termination_message_path=str(stub_path))

    assert exit_code != 0
    stub = json.loads(stub_path.read_text())
    assert stub["reason"] == "envelope-missing"
    assert stub["exitCode"] != 0


def test_main_rejects_wrong_vendor(tmp_path: Path, envelope_in_dict) -> None:
    """D-02 (Phase 51): the image now presents provider.vendor=="langgraph"
    only — every other vendor, "openai" here, is refused at startup."""
    in_path = tmp_path / "in.json"
    in_path.write_text(
        json.dumps(envelope_in_dict(provider={"vendor": "openai", "model": "gpt-x"}))
    )
    stub_path = tmp_path / "termination-log"

    exit_code = entrypoint.main(in_path=str(in_path), termination_message_path=str(stub_path))

    assert exit_code != 0
    stub = json.loads(stub_path.read_text())
    assert "vendor" in stub["reason"]


def test_main_happy_path_writes_trivial_envelope_out(
    tmp_path: Path, monkeypatch, fixture_worktree: Path, envelope_in_dict
) -> None:
    monkeypatch.setenv("TIDE_WORKTREE_DIR", str(fixture_worktree))
    in_path = tmp_path / "in.json"
    in_path.write_text(
        json.dumps(envelope_in_dict(provider={"vendor": "langgraph", "model": "claude-sonnet-4-6"}))
    )
    stub_path = tmp_path / "termination-log"

    fake_model = _fake_model(["the gate command passed"])

    exit_code = entrypoint.main(
        in_path=str(in_path),
        termination_message_path=str(stub_path),
        build_model=lambda env: fake_model,
    )

    assert exit_code == 0
    out = json.loads((tmp_path / "out.json").read_text())
    assert out["exitCode"] == 0
    assert out["result"] == "the gate command passed"
    assert "git" not in out
    assert "childCRDs" not in out
    # env.verify is absent on this envelope (D-06): no gate ran, so no
    # verdict is assembled at all — distinct from the fail-closed BLOCKED
    # case where verify IS present but commands is empty.
    assert "verdict" not in out

    stub = json.loads(stub_path.read_text())
    assert stub["exitCode"] == 0


def test_main_agent_failure_is_fail_closed(tmp_path: Path, envelope_in_dict) -> None:
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
    stub = json.loads(stub_path.read_text())
    assert "agent-error" in stub["reason"]
