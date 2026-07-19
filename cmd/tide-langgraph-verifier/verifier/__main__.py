"""Entrypoint: `python -m verifier`.

Resolves in.json, strict-validates it (D-03), constructs a plain
ChatAnthropic model (D-07 REVISED — no defensive factory, no pre-flight
probe, no subclass override; base URL and CA trust come purely from the pod
env), runs the agent, and writes EnvelopeOut + TerminationStub (D-01).
Fail-closed throughout: ANY failure exits nonzero with a structured stub
reason, never a silent success.

Phase 51 (D-06/T-51-02) extends this with deterministic gate-command
dominance: when `env.verify` is present, EACH resolved pass-criterion
command is executed out-of-band (independently of the LLM's own tool
narration), and the assembled verdict written to EnvelopeOut.Verdict can
NEVER be APPROVED if any command exited non-zero — regardless of what the
LLM judge itself returned. This is the milestone's raison d'être: a
deterministic failure always dominates a probabilistic judge's approval.
"""

from __future__ import annotations

import os
import subprocess
import sys
from pathlib import Path
from typing import Any, Callable

from verifier import envelope, tools, verdict
from verifier.agent import run_agent as _default_run_agent
from verifier.envelope import EnvelopeIn

ENVELOPE_PATH_ENV = "TIDE_ENVELOPE_PATH"
TASK_UID_ENV = "TIDE_TASK_UID"
TERMINATION_MESSAGE_PATH_ENV = "TIDE_TERMINATION_MESSAGE_PATH"
DEFAULT_TERMINATION_MESSAGE_PATH = "/dev/termination-log"

# D-02: this image now presents provider.vendor == "langgraph" only — the
# first (and so far only) self-instrumenting vendor (pkg/dispatch.
# SelfInstruments). "anthropic" (this phase's prior sentinel) is refused
# like every other vendor.
SUPPORTED_VENDOR = "langgraph"


def _default_envelope_path() -> str:
    """Mirrors cmd/claude-subagent/main.go:79-93's path resolution."""
    override = os.environ.get(ENVELOPE_PATH_ENV)
    if override:
        return override
    task_uid = os.environ.get(TASK_UID_ENV, "")
    return f"/workspace/envelopes/{task_uid}/in.json"


def _default_termination_message_path() -> str:
    return os.environ.get(TERMINATION_MESSAGE_PATH_ENV, DEFAULT_TERMINATION_MESSAGE_PATH)


def _out_path_for(in_path: str | os.PathLike[str]) -> Path:
    return Path(in_path).parent / "out.json"


def _one_line(text: str) -> str:
    """Collapse the agent's final message into a single-line D-01 result."""
    return " ".join(text.split())


def _build_default_model(env: EnvelopeIn) -> Any:
    """Plain ChatAnthropic construction (D-07 REVISED). No defensive
    factory, no pre-flight probe, no subclass — the pinned
    langchain-anthropic has no client-construction hook to attach one to
    (RESEARCH Pitfall A); the credproxy-TLS spike (plan 48-05) is the
    measurement, not this code. Base URL / CA trust come from the pod's own
    ANTHROPIC_BASE_URL / SSL_CERT_FILE env vars (already set by jobspec.go).
    """
    from langchain_anthropic import ChatAnthropic

    api_key = env.raw.get("signedToken") or os.environ.get("ANTHROPIC_API_KEY")
    return ChatAnthropic(model=env.provider_model, api_key=api_key)


def _run_commands_out_of_band(commands: list[str], worktree_dir: str) -> list[tuple[str, int]]:
    """Run EACH resolved pass-criterion command deterministically, pinned to
    the worktree, capturing its exit code independently of the LLM's own
    tool narration (T-51-02 — the milestone's raison d'être: a red gate can
    NEVER be overridden by an LLM APPROVED). Mirrors tools.py's
    _worktree_dir/timeout discipline rather than duplicating it; commands
    are the orchestrator-resolved, CEL-immutable-once-Locked
    VerificationSpec pass-criteria, never model-supplied.
    """
    results: list[tuple[str, int]] = []
    for command in commands:
        try:
            proc = subprocess.run(  # noqa: S602 — command is an orchestrator-resolved, CEL-immutable-once-Locked VerificationSpec pass-criterion, never a model-supplied value
                command,
                shell=True,
                cwd=worktree_dir,
                capture_output=True,
                text=True,
                timeout=tools.GATE_COMMAND_TIMEOUT_SECONDS,
                check=False,
            )
            results.append((command, proc.returncode))
        except subprocess.TimeoutExpired:
            # ME-03: a hung/deadlocked gate command (a flaky/deadlocked test —
            # common) is semantically a FAILING gate, not an unstructured crash.
            # The call site is OUTSIDE main()'s try/except, so an uncaught
            # TimeoutExpired would abort main() with a traceback before
            # write_envelope_out/write_termination_stub run — losing the
            # deterministic gate-command finding entirely. Record it as a
            # non-zero (124, the conventional timeout exit code) so
            # _assemble_verdict emits the gate-command blocker finding and forces
            # REPAIRABLE/BLOCKED, preserving the structured verdict + stub.
            results.append((command, 124))
    return results


def _assemble_verdict(
    llm_result_text: str, command_results: list[tuple[str, int]]
) -> verdict.GateDecision:
    """Assemble the final GateDecision (D-06/T-51-02).

    The LLM's own gate_decision text (parsed fail-closed via
    verdict.classify_verdict, D-04) is the baseline verdict. ANY out-of-band
    command exiting non-zero structurally FORCES the verdict down to
    REPAIRABLE (or BLOCKED if the LLM itself already said BLOCKED) —
    dominance only ever pulls a verdict DOWN, never up, and a red gate-command
    Finding (dimension="gate-command", severity="blocker") carries exit_code=N
    for each failing command. An empty command_results list (no authored
    pass-criteria at all — nothing ran) also stays fail-closed: never
    APPROVED, mirroring tools.py's fail-closed-if-empty discipline.
    """
    llm_verdict = verdict.classify_verdict(llm_result_text)

    gate_findings = [
        verdict.Finding(
            dimension="gate-command",
            severity="blocker",
            evidence=f"command {command!r} exited with exit_code={exit_code}",
        )
        for command, exit_code in command_results
        if exit_code != 0
    ]

    if not command_results:
        final_verdict = verdict.Verdict.BLOCKED
        summary = "no gate command ran: verification is not authored or unresolved"
    elif gate_findings:
        final_verdict = (
            verdict.Verdict.BLOCKED if llm_verdict == verdict.Verdict.BLOCKED else verdict.Verdict.REPAIRABLE
        )
        summary = f"{len(gate_findings)} of {len(command_results)} gate command(s) failed"
    else:
        final_verdict = llm_verdict
        summary = _one_line(llm_result_text)

    return verdict.GateDecision(verdict=final_verdict, summary=summary, findings=gate_findings)


def main(
    argv: list[str] | None = None,
    *,
    in_path: str | None = None,
    termination_message_path: str | None = None,
    build_model: Callable[[EnvelopeIn], Any] | None = None,
    run_agent_fn: Callable[[Any, str], str] = _default_run_agent,
) -> int:
    """Entry point. `in_path`/`termination_message_path`/`build_model` are
    injectable seams so tests can drive the full flow offline with a fake
    chat model and tmp-path envelope dirs; production callers pass none of
    them and the paths resolve from the pod's env (D-03)."""
    del argv  # no CLI flags this phase; the entrypoint is entirely env/file-driven

    resolved_in_path = in_path or _default_envelope_path()
    stub_path = termination_message_path or _default_termination_message_path()

    try:
        env = envelope.read_envelope_in(resolved_in_path)
    except envelope.EnvelopeMissingError:
        return _fail(resolved_in_path, stub_path, "envelope-missing")
    except envelope.EnvelopeError as exc:
        return _fail(resolved_in_path, stub_path, f"envelope-invalid: {exc}")

    if env.provider_vendor != SUPPORTED_VENDOR:
        return _fail(
            resolved_in_path,
            stub_path,
            f"unsupported-vendor: expected {SUPPORTED_VENDOR!r}, got {env.provider_vendor!r}",
        )

    # D-06: TIDE_GATE_COMMAND is set from the canonical gateCommand BEFORE
    # the agent runs, so the LLM's run_gate_command tool still functions —
    # advisory narration only, never the authoritative result (that's the
    # out-of-band capture below).
    command_results: list[tuple[str, int]] | None = None
    if env.verify is not None:
        gate_command = env.verify.get("gateCommand", "")
        if gate_command:
            os.environ[tools.GATE_COMMAND_ENV] = gate_command
        commands = env.verify.get("commands") or []
        command_results = _run_commands_out_of_band(commands, tools._worktree_dir())

    try:
        model = (build_model or _build_default_model)(env)
        result_text = run_agent_fn(model, env.prompt)
    except Exception as exc:  # noqa: BLE001 — fail-closed: any agent-loop failure is a structured, nonzero exit
        return _fail(resolved_in_path, stub_path, f"agent-error: {exc}")

    verdict_out: dict[str, Any] | None = None
    if command_results is not None:
        assembled = _assemble_verdict(result_text, command_results)
        verdict_out = assembled.model_dump(by_alias=True)

    envelope.write_envelope_out(
        _out_path_for(resolved_in_path),
        exit_code=0,
        result=_one_line(result_text),
        verdict=verdict_out,
    )
    envelope.write_termination_stub(
        stub_path,
        exit_code=0,
        gate_decision=verdict_out["verdict"] if verdict_out else "",
        findings_count=len(verdict_out["findings"]) if verdict_out else 0,
        high_severity_count=(
            sum(1 for f in verdict_out["findings"] if f["severity"] == "blocker") if verdict_out else 0
        ),
    )
    return 0


def _fail(in_path: str, stub_path: str, reason: str) -> int:
    """Write a structured failure TerminationStub and (best-effort)
    EnvelopeOut, then return exit code 1 — fail-closed, never a silent
    success."""
    envelope.write_termination_stub(stub_path, exit_code=1, reason=reason)
    try:
        envelope.write_envelope_out(
            _out_path_for(in_path), exit_code=1, result="", reason=reason
        )
    except OSError:
        pass  # in.json's own directory may not exist at all (envelope-missing)
    return 1


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
