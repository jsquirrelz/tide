# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this repository is

The seed of **TIDE** (Topologically-Indexed Dependency Execution) — a Kubernetes-native orchestrator for hierarchical agentic coding work that should eventually run against any project/codebase and be open-sourced for use in other clusters.

Right now the repo contains exactly one artifact: `README.md` — the design spec that the implementation will be built against (it doubles as the project's public-facing README, fitting the eventual OSS posture). As code lands (orchestrator, CRDs/operator, planner & executor subagent harnesses, persistence layer, dispatch logic, observability), update this file with the commands, layout, and architectural notes a future Claude needs.

## The spec is load-bearing

Everything downstream — schemas, APIs, controller logic, persistence — should trace back to the paradigm doc. When designing or implementing, preserve these distinctions; they exist for reasons the doc argues for explicitly:

- **Five-level hierarchy**: Milestone → Phase → Plan → Task → Wave. Each level has its own artifact (`MILESTONE.md`, phase brief, `PLAN.md`, diff, execution schedule) and dependency model. Don't collapse levels or invent new ones; if a real implementation pressure pushes back on the hierarchy, update the spec first.
- **Two distinct DAGs**: the *Planning DAG* (which artifacts must exist before another can be authored — shallow, fans out wide) and the *Execution DAG* (which code must exist before another can be written — deeper, fans out narrow). Same Kahn-layered algorithm runs on both. APIs and CRDs should keep these typed apart, not unified into one "DAG" abstraction.
- **Waves are derived, not declared**. They are the output of layered Kahn on the task DAG. The orchestrator never accepts a wave list as input — only a DAG. Re-deriving on every plan edit is intentional (the spec calls this out: O(V+E), cheap, no stale-schedule caching).
- **Cycles are bugs, not runtime conditions**. Cycle detection happens at plan-validation time and a cyclic DAG refuses to run. Don't add "cycle recovery" features; reject and surface.
- **Failure semantics at wave boundaries** (spec §"Failure handling at wave boundaries") are specific: failed task → siblings in same wave continue (they were declared independent), dependents in later waves never dispatch, non-dependents in later waves dispatch in strict-by-default but halt in conservative profile. Keep this contract intact when implementing the executor.
- **Resumption state is minimal**: indegree map + completed-task set. If the persistence layer starts wanting to store the full schedule, that's a smell — re-derive instead.

## Vocabulary conventions

The water/tide metaphor is intentional and consistent — use it in code names, CRD names, log lines, and docs:

- Rising tide = planning wave fanning out across subagents
- Slack tide = review checkpoint between waves
- Tidal lock = phase whose dependencies have all resolved
- Tidepool = sub-DAG developed in parallel isolation
- TIDE pod = deployment unit running a TIDE orchestrator (the K8s pun is intentional and load-bearing)

Prefer extending the metaphor naturally over coining unrelated terms. If a name doesn't fit, prefer plain prose.

## Implementation guidance (as code lands)

Until the first cut of code exists, treat these as defaults to be confirmed or overridden by the user before committing to them:

- **Open-source target**: design APIs, CRDs, and config to be portable across clusters from day one. Avoid hard-coding to a single LLM provider, a single git host, or a single auth model — abstract behind interfaces.
- **Subagent dispatch is pluggable**. The spec is model-agnostic ("Opus for milestone synthesis, Haiku for mechanical task execution"). The dispatch interface should accept a model/profile selector per level rather than baking in a vendor.
- **Two parallelism budgets**: planner and executor pools are separately sized. Don't unify them into one worker pool — the spec argues planning fans out wide and execution fans out narrow, and that's a deliberate capacity split.
- **Artifacts are the source of truth**, not in-memory state. Every level boundary produces a reviewable file (`MILESTONE.md`, phase brief, `PLAN.md`, diff). Resumption reads from artifacts; the orchestrator's database is a cache/index, not the truth.
- **Human gates are configurable per level**. Approve-every-milestone-but-auto-pass-plans should be as easy to express as fully-autonomous or fully-supervised. Don't bake gate policy into the controller.

## Structural conventions in the spec document

When editing `README.md` (the spec) itself:

- Mermaid diagrams: nested `subgraph` containment for the planning graph; flat wave subgraphs with cross-wave edges for the execution graph. Match the existing style.
- Pseudocode uses Python-ish syntax with numbered-step comments (`# 1.`, `# 2.`).
- Worked examples follow pseudocode; the Kahn example walks the indegree map iteration-by-iteration. New algorithms get the same treatment.
- "Alternatives considered and rejected" is part of the doc's argumentative shape — when proposing a design choice, include the rejected alternatives, not just the winner.
- Voice is tight, declarative, em-dash-heavy. Match it rather than reverting to hedged corporate prose.

## What this file should grow into

As real code arrives, add (and keep current):

- Build/test/lint commands, including how to run a single test
- The top-level layout (orchestrator vs CRDs vs subagent harnesses vs CLI) and the boundary between them
- Cluster-local dev loop (kind/minikube setup, how to deploy CRDs, how to tail orchestrator logs)
- Anything cross-cutting that requires reading multiple files to understand (controller reconcile flow, persistence schema, dispatch path)

Avoid re-stating things obvious from the code itself or from a typical Go/K8s project structure.
