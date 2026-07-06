---
title: LangGraph successor-runtime strategy — evidence-gated migration ladder
date: 2026-07-06
context: /gsd:explore session (dynamic workflows, specialist agents, LangChain/LangGraph). Builds on seeds/verify-level-subagent.md, milestones/v1.x-polyglot-subagent-MILESTONE.md, and notes/adk-v2-subagent-evaluation.md.
---

# LangGraph Successor-Runtime Strategy (evidence-gated)

**Decision (2026-07-06):** LangGraph is not merely the second execution strategy — it is the **candidate successor runtime** for all subagent roles. It enters as a small read-only specialist image (the verify tier), and each subsequent migration rung is gated on eval-harness evidence. CLI deprecation is the ladder's *decision point*, not a day-one commitment.

## The ladder

1. **Beachhead (vNext):** verify tier — plan-check / level-verify / integration-check — ships on a read-only LangGraph specialist image. Framing: [milestones/vnext-specialist-verify-MILESTONE.md](../milestones/vnext-specialist-verify-MILESTONE.md).
2. **Authoring migration (v1.x, reframed):** planner roles migrate first (four templates; structured output is a native win), the task executor last — the hardest parity bet, since Claude Code's agent loop and file tools are battle-tested for coding work while hand-authored LangGraph tools start from zero. Every rung gates on the Phase-18 eval harness: a role migrates only when the LangGraph image matches or beats the CLI baseline on quality at comparable cost.
3. **Endgame decision:** with all roles proven, decide CLI deprecation. Multi-provider arrives via `init_chat_model` (single-string provider dispatch, already confirmed in the polyglot doc's LangGraph-fit section) — the standalone OpenAI backend **dissolves as a build effort**; its remnant is the credproxy route-allowlist extension for OpenAI paths plus pricing-table rows.

## What consolidates onto this one track

- **CACHE-F1** — the CLI's per-request-random `cch` billing nonce is unfixable on the CLI path; an in-image SDK owns the request body and can place `cache_control` breakpoints on the shared stable prefix. The LangGraph runtime likely supersedes the separate direct-SDK Go prototype (`todos/pending/cache-f1-direct-sdk-cross-pod-caching.md`). Open verification: `langchain-anthropic` cache_control passthrough (see research/questions.md).
- **The dead `Provider.Params` allowlist comes alive** — temperature / thinking_budget / top_p / top_k are validated at `subagent.go:68` but unreachable through the CLI (`--model`/`--effort` only, per CLAUDE.md). The SDK path exposes all of them.
- **Child-CRD emission** — `with_structured_output(PydanticModel)` replaces the sanitize-and-parse path (the Phase 10 cascade class) once authoring roles migrate.
- **OpenAI provider** — delivered by `init_chat_model`, not by a hand-built Go backend and not by a TIDE-built dogfood deliverable.

## What this changes elsewhere

| Artifact | Effect |
| --- | --- |
| vNext "OpenAI Backend + Dogfood Run #2" | Split. The OpenAI backend dissolves into the migration endgame; run #2 is retargeted at its own scoping (original deliverable moot) and stays gated on multi-node infrastructure |
| v1.x polyglot milestone doc | Roadmap entry reframed to "LangGraph Authoring Migration (evidence-gated)"; the doc's parity inventory and contract-conformance table remain the migration reference |
| CACHE-F1 todo | Stays open; the expected fix vehicle becomes the LangGraph runtime rather than a bespoke Go backend — re-point it when the beachhead ships |
| ADK-Go evaluation | Unchanged; its reopen criteria stand. This strategy strengthens its option (c) |
| Dogfood run #2 | Needs a new build target when scoped. Candidate: TIDE builds parts of the authoring-migration milestone itself (recursive, on-brand) — decide then. The archived Flood Tide phase details remain a starting point |

## Constraints honored (explicitly)

- **The execution DAG stays static and derived.** Dynamism lives (a) inside the pod — LangGraph conditional loops (plan → act → self-check → retry) — and (b) at lifecycle seams via stage routing. Runtime DAG mutation was considered and rejected; waves stay derived, cycles stay bugs.
- **The pluggable-runtime constraint survives CLI deprecation.** The seam is `pkg/dispatch.Subagent` + the envelope contract, not any particular image. Deprecating an image does not remove the seam — keep a contract-conformance test alive so a third image remains a documented, provable drop-in.
- **Wave-boundary failure semantics untouched.** A verify BLOCKED is a new halt class (`ConditionVerifyHalt`), not a reinterpretation of task failure.

## Risks

- **Executor parity is the hard bet** — mitigated by migrating it last, behind eval evidence.
- **The eval harness measures template-prompt output quality; agent-loop quality** (tool-use efficiency, edit correctness) **may need new eval dimensions before the executor rung.**
- **LangGraph 1.x velocity** — patch-pin discipline per the polyglot doc; re-verify pins at every rung.
- **Roadmap churn if the bet sours** — bounded by the evidence gates: any rung can stop the ladder with the CLI image still fully operational.
