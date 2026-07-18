"""Entrypoint: `python -m verifier`.

Resolves in.json, strict-validates it (D-03), constructs a plain
ChatAnthropic model (D-07 REVISED — no defensive factory, no pre-flight
probe, no subclass override; base URL and CA trust come purely from the pod
env), runs the agent, and writes the trivial EnvelopeOut + TerminationStub
(D-01). Fail-closed throughout: ANY failure exits nonzero with a structured
stub reason, never a silent success.
"""

from __future__ import annotations

import os
import sys
from pathlib import Path
from typing import Any, Callable

from verifier import envelope
from verifier.agent import run_agent as _default_run_agent
from verifier.envelope import EnvelopeIn

ENVELOPE_PATH_ENV = "TIDE_ENVELOPE_PATH"
TASK_UID_ENV = "TIDE_TASK_UID"
TERMINATION_MESSAGE_PATH_ENV = "TIDE_TERMINATION_MESSAGE_PATH"
DEFAULT_TERMINATION_MESSAGE_PATH = "/dev/termination-log"

# D-04: this phase's image presents provider.vendor == "anthropic" only. The
# "langgraph" sentinel question is deferred to Phase 51 — do not add a
# discriminator here.
SUPPORTED_VENDOR = "anthropic"


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

    try:
        model = (build_model or _build_default_model)(env)
        result_text = run_agent_fn(model, env.prompt)
    except Exception as exc:  # noqa: BLE001 — fail-closed: any agent-loop failure is a structured, nonzero exit
        return _fail(resolved_in_path, stub_path, f"agent-error: {exc}")

    envelope.write_envelope_out(
        _out_path_for(resolved_in_path), exit_code=0, result=_one_line(result_text)
    )
    envelope.write_termination_stub(stub_path, exit_code=0)
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
