"""The only two tools this image ships (D-02): git_read + run_gate_command.

No file-edit/commit/push tools, no deepagents, no checkpointer. Both tools
run pinned to the already-materialized Task worktree
(/workspace/worktrees/{task-uid}/, pkg/git/worktree.go:50-58) and enforce a
timeout so a hung subprocess cannot stall the agent loop indefinitely
(T-48-08 — the K8s Job's activeDeadlineSeconds remains the ultimate
backstop).
"""

from __future__ import annotations

import os
import shlex
import subprocess

from langchain_core.tools import tool

# Read-only git subcommand allowlist. Anything not in this set — including
# "commit", "push", "worktree" (never "worktree add"; RESEARCH Pitfall D),
# "fetch", and "config" — is rejected before subprocess is ever invoked.
ALLOWED_GIT_SUBCOMMANDS = frozenset(
    {"show", "diff", "log", "ls-tree", "cat-file", "rev-parse", "status", "ls-files"}
)

# Option-injection / path-traversal guard (ASVS V12): these tokens redirect
# git to operate on a different repository/worktree than the pinned one and
# must never reach subprocess, regardless of where they appear in the
# argument list.
FORBIDDEN_GIT_OPTIONS = ("-C", "--git-dir", "--work-tree")

GIT_READ_TIMEOUT_SECONDS = 30
GATE_COMMAND_TIMEOUT_SECONDS = 60


def _worktree_dir() -> str:
    """Resolve the already-materialized Task worktree directory.

    Mirrors the layout pkg/git/worktree.go:50-58 materializes:
    $TIDE_WORKTREE_DIR if set (test/dev override), else
    /workspace/worktrees/$TIDE_TASK_UID (production default).
    """
    override = os.environ.get("TIDE_WORKTREE_DIR")
    if override:
        return override
    return f"/workspace/worktrees/{os.environ.get('TIDE_TASK_UID', '')}"


@tool
def git_read(git_args: str) -> str:
    """Run a read-only git command against the ALREADY-MATERIALIZED Task
    worktree and return its output.

    `git_args` is a space-separated argument string for `git`, e.g.
    "log --oneline -1" or "show HEAD:README.md". Only a fixed allowlist of
    read-only subcommands is permitted: show, diff, log, ls-tree, cat-file,
    rev-parse, status, ls-files.

    This tool reads the worktree the executor already checked out at
    /workspace/worktrees/{task-uid}/ — it NEVER calls `git worktree add`,
    `commit`, `push`, or `config`. Those subcommands write .git admin state
    and hard-fail under the D-08 ReadOnly mount (RESEARCH Pitfall D).

    This tool-layer allowlist is defense-in-depth ON TOP of the structural
    ReadOnly-mount layer (48-02/48-04) — both layers stay load-bearing; do
    not remove one because the other exists.
    """
    try:
        tokens = shlex.split(git_args)
    except ValueError as exc:
        return f"git_read: could not parse arguments: {exc}"

    if not tokens:
        return "git_read: no git subcommand given"

    subcommand = tokens[0]
    if subcommand not in ALLOWED_GIT_SUBCOMMANDS:
        return (
            f"git_read: subcommand {subcommand!r} is not allowed; only "
            f"{sorted(ALLOWED_GIT_SUBCOMMANDS)} may be run"
        )

    for token in tokens:
        if token in FORBIDDEN_GIT_OPTIONS or any(
            token.startswith(f"{opt}=") for opt in FORBIDDEN_GIT_OPTIONS
        ):
            return f"git_read: {token!r} is not allowed (repository/worktree override)"

    result = subprocess.run(  # noqa: S603 — fixed argv, no shell, allowlisted subcommand
        ["git", *tokens],
        cwd=_worktree_dir(),
        capture_output=True,
        text=True,
        timeout=GIT_READ_TIMEOUT_SECONDS,
        check=False,
    )
    return result.stdout or result.stderr


@tool
def run_gate_command(command: str) -> str:
    """Execute the declared gate command in the Task worktree and return its
    exit code plus combined stdout/stderr.

    SECURITY INVARIANT (T-48-05 / ASVS V5): `command` must originate ONLY
    from the orchestrator-authored envelope — never from repo content or
    model-generated text. The entrypoint (__main__.py) never routes model
    output into this parameter.
    """
    result = subprocess.run(  # noqa: S602 — command is orchestrator-authored, never model output
        command,
        shell=True,
        cwd=_worktree_dir(),
        capture_output=True,
        text=True,
        timeout=GATE_COMMAND_TIMEOUT_SECONDS,
        check=False,
    )
    return f"exit_code={result.returncode}\nstdout:\n{result.stdout}\nstderr:\n{result.stderr}"
