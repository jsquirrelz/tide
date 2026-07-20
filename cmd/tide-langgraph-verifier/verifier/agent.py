"""create_agent wiring for the tide-langgraph-verifier runtime (D-02).

Uses `langchain.agents.create_agent` — NOT the langgraph-prebuilt module's
older factory, which is formally deprecated at the pinned langgraph==1.2.9
(RESEARCH Pitfall B). No checkpointer is wired (D-02); this is a bare
seam-conformance shell (D-01), not a multi-turn/resumable agent.
"""

from __future__ import annotations

from typing import TYPE_CHECKING, Any

from langchain.agents import create_agent
from langchain.agents.structured_output import ToolStrategy

from verifier.tools import git_read, run_gate_command
from verifier.verdict import GateDecision

if TYPE_CHECKING:
    from langchain_core.language_models.chat_models import BaseChatModel

# EXPLICIT recursion cap. create_agent's compiled graph bakes in a default of
# 9999 (RESEARCH Pitfall C) — orders of magnitude larger than needed. Sized
# for the Phase-51 controller-rendered task-verifier prompt, which instructs
# a multi-action pass (read the repo README, run the gate command, confirm
# required artifacts, read the candidate's diff and commits, emit the
# structured verdict): each tool round costs 2 graph steps, so 50 admits
# ~25 rounds — the original shell-sized cap of 10 (~4 rounds) starved the
# live agent mid-verification (proven on kind, 2026-07-19). The K8s Job's
# activeDeadlineSeconds remains the ultimate backstop regardless; this cap
# bounds the LangGraph loop itself.
RECURSION_LIMIT = 50

TOOLS = [git_read, run_gate_command]


def build_agent(model: BaseChatModel, *, system_prompt: str | None = None) -> Any:
    """Build the read-only verifier's LangGraph agent: exactly the two tools
    in TOOLS, no checkpointer.

    response_format=ToolStrategy(GateDecision) is the structured-output
    wiring verdict.py's docstring declares as the reason GateDecision is a
    pydantic.BaseModel: the model emits its verdict by calling a synthesized
    GateDecision tool, and the loop returns the validated instance as
    `structured_response`. Without it every final message is prose, and
    verdict.classify_verdict (D-04, strict json.loads) fail-closes to
    BLOCKED — making APPROVED/REPAIRABLE structurally unreachable (proven
    live on kind, 2026-07-19: a red gate escalated at attempt 1 instead of
    REPAIRABLE→repair because the LLM's prose verdict never parsed)."""
    return create_agent(
        model,
        tools=TOOLS,
        system_prompt=system_prompt,
        response_format=ToolStrategy(GateDecision),
    )


def run_agent(model: BaseChatModel, prompt: str) -> str:
    """Invoke the agent and return the verdict JSON, falling back to the
    final message's text.

    The happy path serializes `structured_response` (the ToolStrategy-
    validated GateDecision) with by_alias=True — byte-compatible with what
    verdict.classify_verdict parses. A run that ends without calling the
    GateDecision tool leaves structured_response unset (verified at the
    pinned langchain==1.3.14: no retry, no raise); the prose fallback then
    classifies fail-closed to BLOCKED downstream — never a silent APPROVED.

    Passes `config={"recursion_limit": RECURSION_LIMIT}` EXPLICITLY at
    invoke time — never rely on create_agent's framework default.
    """
    compiled = build_agent(model, system_prompt=prompt)
    result = compiled.invoke(
        {"messages": [{"role": "user", "content": prompt}]},
        config={"recursion_limit": RECURSION_LIMIT},
    )
    structured = result.get("structured_response")
    if isinstance(structured, GateDecision):
        return structured.model_dump_json(by_alias=True)
    content = result["messages"][-1].content
    return content if isinstance(content, str) else str(content)
