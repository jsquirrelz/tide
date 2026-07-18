"""Sanity checks that the shared fixtures build correctly.

Ensures the suite collects >=1 test this wave (VALIDATION.md Wave 0
requirement) and gives every downstream plan a template for using
`fixture_worktree` / `envelope_in_dict`.
"""

from __future__ import annotations

import subprocess
from pathlib import Path
from typing import Any, Callable

from verifier.tests.conftest import API_VERSION, KIND_TASK_ENVELOPE_IN


def test_fixture_worktree_builds_a_committed_git_repo(fixture_worktree: Path) -> None:
    assert (fixture_worktree / ".git").is_dir()
    assert (fixture_worktree / "README.md").is_file()

    log = subprocess.run(
        ["git", "-C", str(fixture_worktree), "log", "--oneline"],
        capture_output=True,
        text=True,
        check=True,
    )
    assert log.stdout.strip() != ""


def test_envelope_in_dict_builds_a_valid_task_envelope_in(
    envelope_in_dict: Callable[..., dict[str, Any]],
) -> None:
    envelope = envelope_in_dict()

    assert envelope["apiVersion"] == API_VERSION
    assert envelope["kind"] == KIND_TASK_ENVELOPE_IN
    assert envelope["provider"]["vendor"] == "anthropic"


def test_envelope_in_dict_accepts_overrides(
    envelope_in_dict: Callable[..., dict[str, Any]],
) -> None:
    envelope = envelope_in_dict(role="planner", level="phase")

    assert envelope["role"] == "planner"
    assert envelope["level"] == "phase"
    # Overrides must not clobber the discriminator fields.
    assert envelope["apiVersion"] == API_VERSION
