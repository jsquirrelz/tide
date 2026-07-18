"""Python re-implementation of the TIDE dispatch envelope wire contract.

Field-for-field port of pkg/dispatch/envelope.go's JSON shapes (D-03). The
Python image cannot import the Go package (import-firewalled, see
pkg/dispatch/doc.go), so this module independently re-implements the JSON
wire shapes this image reads (EnvelopeIn) and writes (EnvelopeOut,
TerminationStub) directly from the Go struct tags — the tags are the frozen
contract, not this module.
"""

from __future__ import annotations

import json
import os
from dataclasses import dataclass, field
from pathlib import Path
from typing import Any

# Wire-contract discriminators (pkg/dispatch/envelope.go:21-38). Consumers
# MUST reject any envelope whose apiVersion/kind does not match these exactly
# — the same invariant ValidateAPIVersionKind (:446) enforces on the Go side.
API_VERSION = "dispatch.tideproject.k8s/v1alpha1"
KIND_IN = "TaskEnvelopeIn"
KIND_OUT = "TaskEnvelopeOut"

# TerminationStub (pkg/dispatch/envelope.go:394) must stay under this many
# serialized bytes — it is written to the Job's 4 KB termination message.
TERMINATION_STUB_MAX_BYTES = 4096


class EnvelopeError(Exception):
    """Raised when in.json is malformed or fails strict apiVersion/kind
    validation. Never partially processed — a hard failure, mirroring
    harness.ReadEnvelopeIn's contract (D-A3)."""


class EnvelopeMissingError(EnvelopeError):
    """Raised when the envelope file at the given path does not exist."""


@dataclass
class EnvelopeIn:
    """Field-for-field port of pkg/dispatch/envelope.go's EnvelopeIn (:45).

    Only the fields this image's runtime consumes are typed explicitly.
    `raw` carries the full decoded document so unknown/future fields
    round-trip untouched (accept-and-ignore) instead of being rejected.

    `verify` mirrors the Go EnvelopeIn.Verify *VerifyContext pointer+omitempty
    field (D-03): None when absent, a plain dict when present. Kept as an
    untyped dict here — this phase only locks the fail-closed extraction
    guard, not a typed VerifyContext dataclass (Phase 51 consumes the
    concrete fields).
    """

    api_version: str
    kind: str
    task_uid: str
    role: str
    level: str
    prompt: str
    provider_vendor: str
    provider_model: str
    verify: dict[str, Any] | None = None
    raw: dict[str, Any] = field(default_factory=dict)


def read_envelope_in(path: str | os.PathLike[str]) -> EnvelopeIn:
    """Read and strictly validate a TaskEnvelopeIn document at path.

    Strict apiVersion/kind equality is the FIRST check performed, before any
    other field is read — mirrors ValidateAPIVersionKind
    (pkg/dispatch/envelope.go:446): a skewed apiVersion/kind or malformed
    JSON is a hard failure, never partial processing.

    Raises:
        EnvelopeMissingError: the file at path does not exist.
        EnvelopeError: the file is unreadable, not valid JSON, not a JSON
            object, or its apiVersion/kind does not match exactly.
    """
    try:
        with open(path, encoding="utf-8") as f:
            raw = json.load(f)
    except FileNotFoundError as exc:
        raise EnvelopeMissingError(f"envelope not found: {path!s}") from exc
    except (OSError, json.JSONDecodeError) as exc:
        raise EnvelopeError(f"read envelope {path!s}: {exc}") from exc

    if not isinstance(raw, dict):
        raise EnvelopeError(
            f"read envelope {path!s}: expected a JSON object, got {type(raw).__name__}"
        )

    got_api_version = raw.get("apiVersion")
    if got_api_version != API_VERSION:
        raise EnvelopeError(
            f"unrecognized apiVersion: expected {API_VERSION!r}, got {got_api_version!r}"
        )

    got_kind = raw.get("kind")
    if got_kind != KIND_IN:
        raise EnvelopeError(f"unrecognized kind: expected {KIND_IN!r}, got {got_kind!r}")

    provider = raw.get("provider")
    if provider is None:
        provider = {}
    elif not isinstance(provider, dict):
        raise EnvelopeError(
            f"read envelope {path!s}: 'provider' must be a JSON object, got {type(provider).__name__}"
        )

    verify = raw.get("verify")
    if verify is not None and not isinstance(verify, dict):
        raise EnvelopeError(
            f"read envelope {path!s}: 'verify' must be a JSON object, got {type(verify).__name__}"
        )

    return EnvelopeIn(
        api_version=got_api_version,
        kind=got_kind,
        task_uid=raw.get("taskUID", ""),
        role=raw.get("role", ""),
        level=raw.get("level", ""),
        prompt=raw.get("prompt", ""),
        provider_vendor=provider.get("vendor", ""),
        provider_model=provider.get("model", ""),
        verify=verify,
        raw=raw,
    )


def write_envelope_out(
    path: str | os.PathLike[str],
    *,
    exit_code: int,
    result: str,
    reason: str = "",
) -> None:
    """Write a trivial EnvelopeOut document to path (D-01 scope).

    Emits exactly apiVersion/kind/exitCode/result (+reason when exit_code is
    nonzero) — no `git`/`childCRDs` keys. Per
    pkg/dispatch/envelope.go IsEnvelopeComplete (:254), a complete envelope
    has ExitCode==0 AND len(ChildCRDs)==ChildCount; omitting both keys leaves
    ChildCount implicitly 0, which is complete for an executor-level
    dispatch. Written 0o644 (harness.WriteEnvelopeOut's own permission).
    """
    out: dict[str, Any] = {
        "apiVersion": API_VERSION,
        "kind": KIND_OUT,
        "exitCode": exit_code,
        "result": result,
    }
    if exit_code != 0:
        out["reason"] = reason

    target = Path(path)
    target.parent.mkdir(parents=True, exist_ok=True)
    target.write_bytes(json.dumps(out).encode("utf-8"))
    os.chmod(target, 0o644)


def write_termination_stub(
    path: str | os.PathLike[str],
    *,
    exit_code: int,
    reason: str = "",
    gate_decision: str = "",
    findings_count: int = 0,
    high_severity_count: int = 0,
) -> None:
    """Write a TerminationStub (pkg/dispatch/envelope.go:394) to path.

    Enforces the ≤4096-byte serialized-size invariant: if exitCode/reason
    together would exceed the cap, reason is progressively truncated until
    the document fits — this stub is written to the Job's termination
    message, which is hard-capped by Kubernetes at 4 KB.

    gate_decision/findings_count/high_severity_count (EVAL-05/D-05a) mirror
    TerminationStub's bounded verdict summary — an enum string + two ints,
    joined into the dict unconditionally since they are bounded by
    construction. Only `reason` is unbounded free text and needs the
    truncation loop.
    """
    stub: dict[str, Any] = {
        "exitCode": exit_code,
        "reason": reason,
        "gateDecision": gate_decision,
        "findingsCount": findings_count,
        "highSeverityCount": high_severity_count,
    }
    data = json.dumps(stub).encode("utf-8")

    while len(data) > TERMINATION_STUB_MAX_BYTES and reason:
        overflow = len(data) - TERMINATION_STUB_MAX_BYTES
        keep = max(0, len(reason) - overflow - len("...(truncated)"))
        reason = reason[:keep] + "...(truncated)"
        stub["reason"] = reason
        data = json.dumps(stub).encode("utf-8")

    target = Path(path)
    target.parent.mkdir(parents=True, exist_ok=True)
    target.write_bytes(data)
