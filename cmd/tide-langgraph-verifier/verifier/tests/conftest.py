"""Shared pytest fixtures for the tide-langgraph-verifier test suite.

Both fixtures below exist to keep Wave 2/3 plans from re-deriving this
scaffolding: every test that needs a git worktree or a wire-shape envelope
should consume `fixture_worktree` / `envelope_in_dict` rather than building
its own.
"""

from __future__ import annotations

import subprocess
from pathlib import Path
from typing import Any, Callable

import pytest

# Wire-contract constants (48-RESEARCH.md <interfaces>; pkg/dispatch/envelope.go).
# The verbatim discriminator values a TaskEnvelopeIn document must carry.
API_VERSION = "dispatch.tideproject.k8s/v1alpha1"
KIND_TASK_ENVELOPE_IN = "TaskEnvelopeIn"


def _run_git(args: list[str], cwd: Path) -> None:
    subprocess.run(
        ["git", "-C", str(cwd)] + args,
        capture_output=True,
        text=True,
        check=True,
        timeout=30,
    )


@pytest.fixture
def fixture_worktree(tmp_path: Path) -> Path:
    """Build a real git repository representing an ALREADY-MATERIALIZED Task
    worktree (RESEARCH.md Pitfall D): git init, local (not global)
    user.name/user.email, one committed tracked file.

    Downstream tests must treat this directory the way the verifier's
    git_read tool treats `/workspace/worktrees/{task-uid}/` in production —
    read-only, already checked out by the executor. Tests must never call
    `git worktree add` against it; that mutates `.git` admin state and would
    fail under the real ReadOnly mount this fixture stands in for.
    """
    worktree_dir = tmp_path / "worktree"
    worktree_dir.mkdir()

    _run_git(["init"], worktree_dir)
    _run_git(["config", "user.name", "tide-langgraph-verifier-test"], worktree_dir)
    _run_git(["config", "user.email", "verifier-test@example.invalid"], worktree_dir)

    tracked_file = worktree_dir / "README.md"
    tracked_file.write_text("fixture worktree for tide-langgraph-verifier tests\n")

    _run_git(["add", "README.md"], worktree_dir)
    _run_git(["commit", "-m", "initial commit"], worktree_dir)

    return worktree_dir


@pytest.fixture
def envelope_in_dict() -> Callable[..., dict[str, Any]]:
    """Factory returning a valid TaskEnvelopeIn dict using the verbatim field
    names and discriminator values pkg/dispatch/envelope.go defines (D-03 —
    the Python image re-implements this JSON shape independently since it
    cannot import the Go package).

    Call with no arguments for a minimal-but-valid executor-level envelope,
    or pass keyword overrides to customize individual top-level fields.
    """

    def _build(**overrides: Any) -> dict[str, Any]:
        envelope: dict[str, Any] = {
            "apiVersion": API_VERSION,
            "kind": KIND_TASK_ENVELOPE_IN,
            "taskUID": "00000000-0000-0000-0000-000000000001",
            "role": "executor",
            "level": "task",
            "prompt": "Run the declared read-only gate command and report the result.",
            "filesTouched": [],
            "declaredOutputPaths": [],
            "caps": {
                "wallClockSeconds": 60,
                "iterations": 10,
                "inputTokens": 100_000,
                "outputTokens": 20_000,
            },
            "proxyEndpoint": "https://127.0.0.1:8443",
            "signedToken": "test-signed-token",
            "provider": {
                "vendor": "anthropic",
                "model": "claude-sonnet-4-6",
            },
        }
        envelope.update(overrides)
        return envelope

    return _build
