"""create_agent wiring for the tide-langgraph-verifier runtime (D-02).

Uses `langchain.agents.create_agent` — NOT the langgraph-prebuilt module's
older factory, which is formally deprecated at the pinned langgraph==1.2.9
(RESEARCH Pitfall B). No checkpointer is wired (D-02); this is a bare
seam-conformance shell (D-01), not a multi-turn/resumable agent.
"""

from __future__ import annotations

from typing import TYPE_CHECKING, Any

from langchain.agents import create_agent

from verifier.tools import git_read, run_gate_command

if TYPE_CHECKING:
    from langchain_core.language_models.chat_models import BaseChatModel

# EXPLICIT recursion cap. create_agent's compiled graph bakes in a default of
# 9999 (RESEARCH Pitfall C) — three orders of magnitude larger than what this
# bare shell needs (one gate command + one git-read + one model call). The
# K8s Job's activeDeadlineSeconds remains the ultimate backstop regardless;
# this cap bounds the LangGraph loop itself.
RECURSION_LIMIT = 10

TOOLS = [git_read, run_gate_command]


def build_agent(model: BaseChatModel, *, system_prompt: str | None = None) -> Any:
    """Build the read-only verifier's LangGraph agent: exactly the two tools
    in TOOLS, no checkpointer."""
    return create_agent(model, tools=TOOLS, system_prompt=system_prompt)


def run_agent(model: BaseChatModel, prompt: str) -> str:
    """Invoke the agent on prompt and return the final message's text.

    Passes `config={"recursion_limit": RECURSION_LIMIT}` EXPLICITLY at
    invoke time — never rely on create_agent's framework default.
    """
    compiled = build_agent(model, system_prompt=prompt)
    result = compiled.invoke(
        {"messages": [{"role": "user", "content": prompt}]},
        config={"recursion_limit": RECURSION_LIMIT},
    )
    content = result["messages"][-1].content
    return content if isinstance(content, str) else str(content)
