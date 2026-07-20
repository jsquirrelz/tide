"""Python re-implementation of the TIDE gate_decision verdict wire contract.

Field-for-field port of pkg/dispatch/verdict.go's JSON shapes (D-02). The
Python image cannot import the Go package (import-firewalled, see
pkg/dispatch/doc.go), so this module independently re-implements the verdict
schema — the Go struct's JSON tags are the frozen contract, and the shared
golden fixture (pkg/dispatch/testdata/gate_decision_golden.json) is the
cross-language proof that this re-implementation stays honest.

Declared as pydantic.BaseModel (NOT @dataclass, unlike envelope.EnvelopeIn):
Phase 51's create_agent(response_format=GateDecision) (LangChain structured
output) needs a Pydantic-compatible schema, so building this as a dataclass
now would only mean converting it later for zero present benefit.
"""

from __future__ import annotations

import json
from enum import Enum

import pydantic


class Verdict(str, Enum):
    """The terminal classification of a gate_decision (EVAL-03). Mirrors
    pkg/dispatch/verdict.go's Verdict type — the set is exactly
    APPROVED | REPAIRABLE | BLOCKED, no other value is ever produced by
    [classify_verdict]."""

    APPROVED = "APPROVED"
    REPAIRABLE = "REPAIRABLE"
    BLOCKED = "BLOCKED"


class Finding(pydantic.BaseModel):
    """A single deviation the verifier reported. Fields are FREE STRINGS
    this phase (coverage-not-conservatism: every deviation is tagged, and
    policy — not the finder — decides what blocks; typed enums are deferred
    to Phase 51 EVAL-04, mirroring pkg/dispatch.Finding's identical
    scope note). Field names/aliases match the Go JSON tags exactly;
    populate_by_name=True lets both the snake_case attribute and the
    camelCase wire alias round-trip.
    """

    model_config = pydantic.ConfigDict(populate_by_name=True)

    dimension: str = ""
    severity: str = ""
    confidence: str = ""
    evidence: str = ""
    suggested_fix: str = pydantic.Field(default="", alias="suggestedFix")


class GateDecision(pydantic.BaseModel):
    """The wire-format verdict document a verifier writes to out.json
    (EVAL-03). Mirrors pkg/dispatch.GateDecision field-for-field. This is
    intentionally NOT a CRD type — it round-trips through the file-envelope
    seam between the K8s controller and an out-of-tree evaluator image
    (D-01: putting this in a CRD-machinery module would misrepresent a
    wire-format doc as a K8s object).
    """

    verdict: Verdict
    summary: str = ""
    findings: list[Finding] = pydantic.Field(default_factory=list)


def classify_verdict(raw: str | bytes) -> Verdict:
    """Parse raw as a gate_decision JSON document and return its terminal
    Verdict, fail-closed by construction (D-04): empty input, malformed
    JSON, and a missing/unrecognized verdict field all return
    Verdict.BLOCKED — never Verdict.APPROVED. Mirrors
    pkg/dispatch.ClassifyVerdict's identical 3-branch shape (its bare-return,
    no-accompanying-error discipline) so test names line up 1:1 across
    languages — a caller cannot forget to map an error to the safe terminal,
    because there is no error to forget.
    """
    if not raw:
        return Verdict.BLOCKED  # empty JSON

    try:
        parsed = json.loads(raw)
    except json.JSONDecodeError:
        return Verdict.BLOCKED  # malformed

    verdict_str = parsed.get("verdict") if isinstance(parsed, dict) else None
    try:
        return Verdict(verdict_str)
    except ValueError:
        return Verdict.BLOCKED  # missing/unrecognized verdict field
