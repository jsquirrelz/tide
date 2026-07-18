"""Tests for verifier.tools — git_read + run_gate_command, the only two
tools this image ships (D-02)."""

from __future__ import annotations

import subprocess
from pathlib import Path

import pytest

from verifier import tools


@pytest.fixture(autouse=True)
def _pin_worktree(monkeypatch, fixture_worktree: Path) -> None:
    monkeypatch.setenv("TIDE_WORKTREE_DIR", str(fixture_worktree))


def test_git_read_log_oneline(fixture_worktree: Path) -> None:
    output = tools.git_read.invoke("log --oneline -1")

    assert "initial commit" in output


def test_git_read_show_tracked_file(fixture_worktree: Path) -> None:
    output = tools.git_read.invoke("show HEAD:README.md")

    assert "fixture worktree for tide-langgraph-verifier tests" in output


@pytest.mark.parametrize(
    "git_args",
    [
        "commit -m evil",
        "push origin main",
        "worktree add ../evil",
        "config user.email evil@example.com",
        "fetch origin",
    ],
)
def test_git_read_rejects_mutating_subcommands(monkeypatch, git_args: str) -> None:
    def _boom(*args, **kwargs):
        raise AssertionError("subprocess must never be spawned for a rejected subcommand")

    monkeypatch.setattr(tools.subprocess, "run", _boom)

    output = tools.git_read.invoke(git_args)

    assert "not allowed" in output


@pytest.mark.parametrize(
    "git_args",
    [
        "-C /etc show HEAD",
        "--git-dir=/etc/passwd log",
        "--work-tree=/tmp show HEAD",
    ],
)
def test_git_read_rejects_option_injection(monkeypatch, git_args: str) -> None:
    def _boom(*args, **kwargs):
        raise AssertionError("subprocess must never be spawned for an injected option")

    monkeypatch.setattr(tools.subprocess, "run", _boom)

    output = tools.git_read.invoke(git_args)

    assert "not allowed" in output


def test_run_gate_command_success(monkeypatch) -> None:
    monkeypatch.setenv("TIDE_GATE_COMMAND", "true")

    output = tools.run_gate_command.invoke({})

    assert "exit_code=0" in output


def test_run_gate_command_failure(monkeypatch) -> None:
    monkeypatch.setenv("TIDE_GATE_COMMAND", "false")

    output = tools.run_gate_command.invoke({})

    assert "exit_code=1" in output


def test_run_gate_command_fails_closed_when_env_unset(monkeypatch) -> None:
    """CR-01: with no orchestrator-set TIDE_GATE_COMMAND, no subprocess is
    ever spawned — the tool fails closed instead."""
    monkeypatch.delenv("TIDE_GATE_COMMAND", raising=False)

    def _boom(*args, **kwargs):
        raise AssertionError("subprocess must never be spawned when TIDE_GATE_COMMAND is unset")

    monkeypatch.setattr(tools.subprocess, "run", _boom)

    output = tools.run_gate_command.invoke({})

    assert "exit_code=1" in output


def test_run_gate_command_ignores_model_supplied_command(monkeypatch) -> None:
    """CR-01 regression: the model can call this tool (decide WHEN) but
    cannot choose WHAT runs — a model-emitted `command` tool-call argument
    (e.g. a prompt-injection attempt to exfiltrate the signed token) is
    always discarded; only TIDE_GATE_COMMAND ever reaches subprocess."""
    monkeypatch.setenv("TIDE_GATE_COMMAND", "true")
    captured: dict = {}

    def _fake_run(command, **kwargs):
        captured["command"] = command
        return subprocess.CompletedProcess(command, 0, stdout="", stderr="")

    monkeypatch.setattr(tools.subprocess, "run", _fake_run)

    tools.run_gate_command.invoke({"command": "printenv ANTHROPIC_API_KEY"})

    assert captured["command"] == "true"
    assert captured["command"] != "printenv ANTHROPIC_API_KEY"


def test_git_read_pins_cwd_and_timeout(monkeypatch, fixture_worktree: Path) -> None:
    captured: dict = {}

    def _fake_run(args, **kwargs):
        captured["args"] = args
        captured["kwargs"] = kwargs
        return subprocess.CompletedProcess(args, 0, stdout="ok", stderr="")

    monkeypatch.setattr(tools.subprocess, "run", _fake_run)

    tools.git_read.invoke("status")

    assert captured["kwargs"]["cwd"] == str(fixture_worktree)
    assert captured["kwargs"]["timeout"] == tools.GIT_READ_TIMEOUT_SECONDS


def test_run_gate_command_pins_cwd_and_timeout(monkeypatch, fixture_worktree: Path) -> None:
    monkeypatch.setenv("TIDE_GATE_COMMAND", "true")
    captured: dict = {}

    def _fake_run(command, **kwargs):
        captured["command"] = command
        captured["kwargs"] = kwargs
        return subprocess.CompletedProcess(command, 0, stdout="", stderr="")

    monkeypatch.setattr(tools.subprocess, "run", _fake_run)

    tools.run_gate_command.invoke({})

    assert captured["command"] == "true"
    assert captured["kwargs"]["cwd"] == str(fixture_worktree)
    assert captured["kwargs"]["timeout"] == tools.GATE_COMMAND_TIMEOUT_SECONDS
    assert captured["kwargs"]["shell"] is True
