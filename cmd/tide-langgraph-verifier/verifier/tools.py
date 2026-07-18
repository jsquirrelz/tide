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

# Option-injection / confinement-escape guard (ASVS V12): these options let
# an allowlisted subcommand escape the pinned worktree/repository entirely —
# reading arbitrary container files (--no-index), writing arbitrary
# container paths (--output), running arbitrary git config / external
# helpers (-c/--config, --upload-pack, --exec-path), or redirecting to a
# different repository (--git-dir/--work-tree). They must never reach
# subprocess, regardless of where they appear AFTER the subcommand.
#
# `-C` is deliberately NOT on this list. `tokens[0]` must already be an
# allowlisted subcommand (checked above; none of them start with "-"), so
# `-C` can only ever appear at index >= 1 — that is always git's per-
# subcommand copy-detection flag (e.g. "diff -C"), never the dangerous
# global "git -C <path> <subcommand>" repository redirect, which would
# require "-C" at tokens[0] and is already rejected by the subcommand check.
FORBIDDEN_GIT_OPTIONS = (
    "--no-index",
    "--output",
    "-c",
    "--config",
    "--git-dir",
    "--work-tree",
    "--upload-pack",
    "--exec-path",
)

GIT_READ_TIMEOUT_SECONDS = 30
GATE_COMMAND_TIMEOUT_SECONDS = 60

# CR-01: the ONLY source `run_gate_command` ever reads its command from.
# Never a model-supplied tool-call argument, never repo content.
GATE_COMMAND_ENV = "TIDE_GATE_COMMAND"


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

    # Only tokens[1:] are checked — tokens[0] is the already-validated
    # subcommand and never matches a forbidden option anyway.
    for token in tokens[1:]:
        if token in FORBIDDEN_GIT_OPTIONS or any(
            token.startswith(f"{opt}=") for opt in FORBIDDEN_GIT_OPTIONS
        ):
            return f"git_read: {token!r} is not allowed (confinement escape / arbitrary file access)"

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
def run_gate_command(command: str | None = None) -> str:
    """Run the orchestrator-declared gate command in the Task worktree and
    return its exit code plus combined stdout/stderr.

    SECURITY INVARIANT (T-48-05 / ASVS V5): the command actually executed
    is read ONLY from the orchestrator-set TIDE_GATE_COMMAND environment
    variable — never from a model tool-call argument or repo content. The
    model may decide WHEN to invoke this tool; it can never choose WHAT
    runs. `command` is accepted as a parameter for tool-call compatibility
    but is always discarded, never executed — this holds even under
    prompt injection from adversarial repo content, since there is no code
    path from a tool-call argument to subprocess.

    Forward-note (Phase 49 scope, not this phase's): the dispatch
    entrypoint will set TIDE_GATE_COMMAND from the envelope's
    (forthcoming) VerifyContext.GateCommand field before this process
    starts. This phase only consumes the env var; it does not implement
    that envelope field.

    Fails closed: if TIDE_GATE_COMMAND is unset or empty, no subprocess is
    spawned and an exit_code=1 result is returned.
    """
    del command  # SECURITY: model-supplied value, always discarded — never executed

    gate_command = os.environ.get(GATE_COMMAND_ENV, "")
    if not gate_command:
        return (
            "exit_code=1\nstdout:\nstderr:\n"
            f"{GATE_COMMAND_ENV} is unset or empty; refusing to run a gate command"
        )

    result = subprocess.run(  # noqa: S602 — gate_command is read from the orchestrator-set TIDE_GATE_COMMAND env var, never a model-supplied tool argument
        gate_command,
        shell=True,
        cwd=_worktree_dir(),
        capture_output=True,
        text=True,
        timeout=GATE_COMMAND_TIMEOUT_SECONDS,
        check=False,
    )
    return f"exit_code={result.returncode}\nstdout:\n{result.stdout}\nstderr:\n{result.stderr}"
