# Quick Task 260615-gos: Explore abstracting CLI to be a single strategy for executing LLM calls and implementing another strategy using SDKs — Context

**Gathered:** 2026-06-15
**Status:** Ready for planning

<domain>
## Task Boundary

Explore abstracting the CLI subagent runtime so it becomes *one* strategy for
executing LLM calls, alongside a *second* strategy built on SDKs (directly, or
indirectly via LangGraph). The deliverable of this quick task is **not code** —
it is a **semi-scoped milestone document added to the backlog** for a future
milestone cycle.

Grounding observation (current code): the seam already exists.
`pkg/dispatch.Subagent` is the interface (`Run(ctx, EnvelopeIn) (EnvelopeOut,
error)`); `internal/subagent/anthropic/` implements it by shelling out to the
`claude` CLI behind a build-time provider firewall
(`make verify-dispatch-imports` / `tools/analyzers/providerfirewall`). The CLI
deliberately bundles the agent loop, hooks, MCP, skills, and bash/file tools
(HARN-06). Pluggability "via a documented container image contract" is an
existing project constraint, not a new invention.

</domain>

<decisions>
## Implementation Decisions

These are **locked** for the milestone draft. The planner/executor must treat
them as settled and write the backlog milestone around them.

### Strategy seam location — Reuse the `Subagent` contract
Each execution strategy is a **separate container image** behind the existing
`pkg/dispatch.Subagent` interface and the documented container-image contract
(envelope-in / envelope-out). The "CLI" becomes one named strategy/image; the
SDK-based runtime is a second image implementing the same contract. No new
in-process LLM-call abstraction is introduced inside a shared Go agent loop —
the seam stays at the image boundary, which is the lowest-blast-radius option
and honors the "pluggable subagent runtime via a documented container image
contract" constraint already in CLAUDE.md and the provider firewall.

### LangGraph topology — Python sidecar/image
The SDK strategy is hosted as a **Python container image** that implements the
documented envelope contract, free to use LangGraph natively for its agent
loop. TIDE stays Go; the second strategy is polyglot but isolated behind the
same image contract — the contract is the language-neutral seam. This is a
per-task image (consistent with the current Job-per-task dispatch model), NOT a
long-lived LangGraph service the controller calls over HTTP.

### SDK strategy feature scope — Full parity
The SDK/LangGraph image targets **full agent-loop parity** with the CLI image:
tool use, file operations, and MCP. Any subagent role must be runnable on
either strategy. LangGraph supplies the agent-loop machinery that makes full
parity tractable in Python. (Note for the milestone: enumerate exactly which
CLI-bundled capabilities — hooks, skills, MCP, bash/file tools, streaming token
accounting, child-CRD JSON emission — must be reproduced to claim "parity," and
flag any that are CLI-specific and therefore N/A.)

### Milestone commitment level — Lock architecture, defer task breakdown
The backlog artifact locks the architectural choices above (seam, topology,
scope) and frames the problem, constraints, and success criteria. It does NOT
draft phases/tasks/requirements — that is deferred to a later `/gsd:plan-phase`
(or new-milestone) cycle. "Semi-scoped" = decided architecture + framed scope,
open task breakdown.

### Claude's Discretion
- Exact milestone numbering / placement in the backlog (after the current
  v1.0.2 "Ebb Tide" and the next OpenAI-backend + run-#2 milestone, per project
  memory) — pick a sensible slot and label it clearly as backlog/future.
- The precise success-criteria phrasing and which open questions to enumerate.
- Whether to capture the rejected alternatives (in-process seam, Go-native SDK,
  LangGraph-as-a-service) as an "Alternatives considered" section — recommended,
  since the spec's argumentative style favors it.

</decisions>

<specifics>
## Specific Ideas

- The deliverable is a milestone/backlog markdown document. Likely homes:
  `.planning/backlog/` or a `BACKLOG.md` entry — the planner should detect the
  project's existing backlog convention (memory references a backlog and
  `/gsd:review-backlog`) and follow it rather than inventing a new location.
- Tie-in: project memory notes the *next* milestone is "OpenAI backend + run
  #2," and v1.0.2 "Ebb Tide" (token/cost optimization) is in flight. A
  second, SDK/LangGraph strategy is a natural vehicle for non-Anthropic
  providers and for cost/observability experiments — the milestone should note
  the relationship without absorbing those milestones' scope.
- Provider firewall + envelope contract are the load-bearing existing
  artifacts; the milestone must explicitly preserve them.

</specifics>

<canonical_refs>
## Canonical References

- `pkg/dispatch/subagent.go` — the `Subagent` interface (the seam being reused)
- `pkg/dispatch/envelope.go` — `EnvelopeIn`/`EnvelopeOut` (the image contract)
- `internal/subagent/anthropic/subagent.go` — current CLI strategy, HARN-06
  rationale for shelling out instead of embedding the Go SDK
- `tools/analyzers/providerfirewall` + `make verify-dispatch-imports` — the
  build-time firewall any new strategy must respect
- `CLAUDE.md` — "pluggable subagent runtime via a documented container image
  contract"; anti-patterns (no host `~/.claude/` mount, no headless OAuth, all
  Anthropic-specific code behind the interface)
- Project memory: v1.0.2 "Ebb Tide" cost-optimization milestone; next milestone
  = OpenAI backend + dogfood run #2

</canonical_refs>
