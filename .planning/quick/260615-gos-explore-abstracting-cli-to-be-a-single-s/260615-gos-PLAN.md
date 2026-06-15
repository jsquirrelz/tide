---
phase: quick-260615-gos
plan: 01
type: execute
wave: 1
depends_on: []
files_modified:
  - .planning/ROADMAP.md
  - .planning/milestones/v1.x-polyglot-subagent-MILESTONE.md
autonomous: true
requirements:
  - ARCH-POLYGLOT-01
must_haves:
  truths:
    - "A backlog milestone entry appears in ROADMAP.md under the Milestones list, sequenced after v1.0.2 and the OpenAI-backend milestone, with a clear not-yet-started marker."
    - "A framing doc exists at .planning/milestones/v1.x-polyglot-subagent-MILESTONE.md that locks the three architectural pillars, carries the parity inventory and contract-conformance requirements, states the provider-firewall gap and its resolution, includes Alternatives considered (and rejected), enumerates the open questions for future task breakdown, and notes the relationship to v1.0.2 and the OpenAI-backend milestone without absorbing their scope."
    - "No phase/task/requirements breakdown appears in either artifact — scope is locked, task authoring is explicitly deferred."
  artifacts:
    - path: ".planning/ROADMAP.md"
      provides: "Backlog milestone entry under ## Milestones, post-v1.0.2 and post-OpenAI-backend"
      contains: "v1.x — Polyglot Subagent Runtimes"
    - path: ".planning/milestones/v1.x-polyglot-subagent-MILESTONE.md"
      provides: "Semi-scoped milestone framing doc"
      contains: "Alternatives considered"
  key_links:
    - from: "ROADMAP.md ## Milestones list"
      to: ".planning/milestones/v1.x-polyglot-subagent-MILESTONE.md"
      via: "Markdown link in the backlog entry"
      pattern: "v1\\.x-polyglot-subagent-MILESTONE\\.md"
---

<objective>
Record the polyglot subagent runtimes architecture as a semi-scoped backlog milestone — locking the three architectural pillars decided in CONTEXT.md and grounding them in the parity inventory and contract-conformance analysis from RESEARCH.md — so the decision is durable and the future plan-phase has a clear starting frame.

Purpose: Prevent architectural drift and duplicated research; give the future plan-phase team a single authoritative doc covering what was decided, what was rejected, and what remains open.
Output: One backlog entry in ROADMAP.md and one framing doc in .planning/milestones/.
</objective>

<execution_context>
@$HOME/.claude/get-shit-done/workflows/execute-plan.md
@$HOME/.claude/get-shit-done/templates/summary.md
</execution_context>

<context>
@.planning/ROADMAP.md
@.planning/STATE.md
@.planning/quick/260615-gos-explore-abstracting-cli-to-be-a-single-s/260615-gos-CONTEXT.md
@.planning/quick/260615-gos-explore-abstracting-cli-to-be-a-single-s/260615-gos-RESEARCH.md
</context>

<tasks>

<task type="auto">
  <name>Task 1: Create the polyglot subagent milestone framing doc</name>
  <files>.planning/milestones/v1.x-polyglot-subagent-MILESTONE.md</files>
  <action>
Create .planning/milestones/v1.x-polyglot-subagent-MILESTONE.md as a self-contained framing document for the future "Polyglot Subagent Runtimes" milestone. This is a planning artifact, not a requirements spec — it locks architecture, frames scope, and explicitly defers task breakdown.

Voice: tight, declarative, em-dash-heavy; match CLAUDE.md / README.md style. Use the water/tide metaphor where natural. "Alternatives considered and rejected" is part of the argumentative shape — include it. Fenced code blocks are permitted here (this is a doc, not a task action).

The document must contain all of the following sections, in this order:

---

# v1.x — Polyglot Subagent Runtimes (LangGraph Strategy)

**Status:** Backlog — architecture locked, task breakdown deferred.
**Sequence:** After v1.0.2 "Ebb Tide" and the "OpenAI Backend + Dogfood Run #2" milestone. Do not pick up until both predecessors are shipped.
**Scope commitment:** Semi-scoped — the three architectural pillars are decided; phases, tasks, and requirements are not drafted here.

## Problem

[One tight paragraph: the `claude` CLI subagent is currently the only concrete `Subagent` implementation; this creates implicit single-provider lock-in at the *runtime* level even though the `Subagent` interface and the container-image contract are already provider-agnostic. A second, Python/LangGraph-hosted strategy would make multi-provider dispatch real and explorable without touching the orchestrator.]

## Architecture Decision (locked)

Three pillars, all from CONTEXT.md decisions D-01/D-02/D-03. State each as a one-sentence assertion:

**Pillar 1 — Seam:** Each execution strategy is a separate container image behind the existing `pkg/dispatch.Subagent` interface and the documented container-image envelope contract (envelope-in / envelope-out). The "CLI" becomes one named strategy/image; the Python/LangGraph runtime is a second image implementing the same contract. No new in-process LLM-call abstraction is introduced inside a shared Go agent loop — the seam stays at the image boundary.

**Pillar 2 — Python/LangGraph topology:** The SDK strategy is hosted as a Python container image, free to use LangGraph natively for its agent loop. TIDE stays Go; the second strategy is polyglot but isolated behind the language-neutral image contract. It is a per-task image (one K8s Job, one envelope-in, one envelope-out) — NOT a long-lived LangGraph HTTP service.

**Pillar 3 — Full parity:** The Python/LangGraph image targets full agent-loop parity with the CLI image: tool use (file ops + bash), MCP, streaming token/cost accounting, child-CRD JSON emission, and worktree commit. Any subagent role (planner, executor, reviewer) must be runnable on either strategy. Hooks and skills are intentionally N/A — TIDE runs hermetic (`--bare` equivalent) and never uses them.

## Parity Inventory

Reproduce the parity table from RESEARCH.md verbatim (the 9-row capability table with columns: CLI-bundled capability / TIDE consumer / Reproducible in Python/LangGraph? / Notes). Do not summarize — preserve the cell content since it is the measurable definition of "parity" for the future task team.

After the table, include the parity verdict paragraph from RESEARCH.md (starts "Parity verdict: Of the eight CLI-bundled capabilities...").

## Contract Conformance Requirements

Reproduce the contract-conformance table from RESEARCH.md verbatim (the 14-row table with columns: Contract element / Current mechanism / Python equivalent / CLI/Node-specific?). Do not summarize.

After the table, include the conformance summary sentence from RESEARCH.md (starts "Conformance summary: Exactly one contract element is genuinely Node/CLI-specific...").

## Provider-Firewall Gap (load-bearing)

This is the section the constraints call out explicitly. Cover:

1. What the firewall is: `tools/analyzers/providerfirewall` is a Go import analyzer enforcing that no `github.com/anthropics/*` or `github.com/openai/*` import leaks into `pkg/controller` or `pkg/dispatch`. It asserts the invariant at Go-build time via `make verify-dispatch-imports`.

2. Why a Python image sidesteps it: Python has no Go imports; the analyzer cannot reach it.

3. Why the invariant is still satisfied: (a) the Go orchestrator still imports no provider SDK — the Python image is a separate process behind the image contract, so `make verify-dispatch-imports` stays green by construction; (b) the credproxy signed-token flow and route allowlist still gate what the Python image can reach at runtime.

4. The enforcement shift: the provider boundary for the Python path is enforced at deploy time (which image is allowed in `values.yaml`) and at runtime (credproxy allowlist), not at Go-build time. No new Go analyzer can cover Python — document the boundary, don't try to extend the tool.

## LangGraph Fit (summary)

Brief section (not the full research) capturing: LangGraph 1.2.x is GA; `init_chat_model` handles multi-provider; `create_react_agent` or the functional `@entrypoint` API provides the agent loop; `with_structured_output(PydanticModel)` eliminates the child-CRD free-text-parse failure class; `langchain-mcp-adapters` 0.3.0 provides MCP tool loading; `deepagents` 0.6.10 is a candidate for built-in file/bash tooling (ASSUMED — verify at build time). Note the version-pinning discipline: pin `langgraph` to a patch (`langgraph==1.2.x`) mirroring the Anthropic-SDK pinning rule in STACK.md, since the 1.x line moves fast.

## Open Questions (defer to plan-phase)

Number these Q1–Q5, lifted from RESEARCH.md §"Open questions to enumerate":

Q1. Pricing/cost: compute estimated cost cents in-image (duplicate the Go pricing table in Python) vs emit raw tokens and let the controller price? In-image is current behavior; controller-side is cleaner (single pricing source) — cross-reference v1.0.2 Phase 21 (cost observability).

Q2. Templates: port the five `go:embed` prompt templates to Python copies (dual maintenance, drift risk) vs extract them to a language-neutral shared asset both images embed? Recommend the shared-asset route; note it touches v1.0.2 Phase 19 template work — sequence after Ebb Tide lands.

Q3. Observability: write an `events.jsonl` artifact in the same shape as the Go path (single downstream consumer) vs emit OTel spans directly via OpenInference Python auto-instrumentation (`openinference-instrumentation-langchain`)? Either is viable; the choice affects whether `pkg/otelai` needs a Python analog or a shared artifact consumer.

Q4. Agent loop harness: build from `create_react_agent` + custom `@tool` functions (maximum control, minimum surface area) vs adopt `deepagents` for built-in file/bash/planning tools (faster to parity, larger third-party dependency)? Confirm `deepagents` 0.6.10 tool surface at build time before committing.

Q5. Credproxy (Anthropic path): does `SSL_CERT_FILE`/`REQUESTS_CA_BUNDLE` satisfy the CA-trust requirement for httpx-based Anthropic + OpenAI Python SDKs, or does the sidecar spec need adjustment? (Expected: yes, `SSL_CERT_FILE` works; verify at build time. OpenAI route allowlist is out of scope — deferred to the OpenAI-backend milestone.)

## Alternatives Considered (and Rejected)

Three alternatives, matching the framing in RESEARCH.md §"Alternatives Considered":

**In-process Go LLM seam** — a shared Go agent loop with providers as in-process Go strategies behind one interface. Rejected: would re-implement the agent loop, tool dispatch, MCP, and file tools in Go — exactly the work the team deliberately avoided by shelling out to the CLI (HARN-06). Drags every provider SDK into the Go module, directly contradicting the firewall's reason to exist, and raises blast radius from "a new image" to "a new core abstraction touched by the controller."

**Go-native SDK strategy** — embed the Anthropic/OpenAI Go SDK in a Go subagent image (no CLI, no Python). Rejected: gains language uniformity but loses the off-the-shelf agent loop — tool-calling, file ops, and structured retries must be hand-built in Go. LangGraph in Python is more mature at exactly this than any Go agent framework in 2026. The provider firewall would have to relax to admit the SDK into a Go in-module binary.

**LangGraph-as-a-service** — a long-lived LangGraph HTTP server the controller calls per task. Rejected: breaks the Job-per-task dispatch model, introduces a stateful long-running component into a system designed around re-derivable state at every level boundary, adds a network hop + auth surface + scaling concern, and makes the per-pod credproxy sidecar model awkward. The per-task image keeps the contract identical to today.

## Relationship to Adjacent Milestones

Short section, three paragraphs:

1. **v1.0.2 "Ebb Tide" (Phases 18–21, current):** Tunes the five prompt templates (Phase 19) and adds cost/cache observability (Phase 21). The polyglot milestone's template-portability decision (Q2) and cost-mapping decision (Q1) depend on Ebb Tide's outcomes — pick this milestone up after v1.0.2 ships to avoid branching on unsettled template structure.

2. **"OpenAI Backend + Dogfood Run #2" (next planned milestone):** Extends the credproxy's route allowlist for OpenAI's paths and wires an OpenAI provider into the dispatch chain. A LangGraph image with `init_chat_model` is a natural vehicle for OpenAI — but the OpenAI-backend milestone may land OpenAI on the CLI/Go path first. These two milestones are independently shippable: this one delivers the second *runtime*; the OpenAI-backend milestone delivers a second *provider*. They compose but neither requires the other.

3. **TIDE-on-TIDE:** The ultimate goal — TIDE driving its own next milestone — benefits from cost reduction (Ebb Tide) and provider diversity (OpenAI + LangGraph runtimes), but does not depend on either being complete first. The polyglot runtime is additive.

## Deferred

Explicit list of what is NOT in this milestone:
- Phase/task/requirements breakdown (deferred to `/gsd:plan-phase` or `/gsd:new-milestone` when picked up).
- OpenAI credproxy route extension (owned by the OpenAI-backend milestone).
- Dogfood run #2 (owned by the OpenAI-backend milestone).
- Ebb Tide template tuning (owned by v1.0.2).

## Assumptions Log

Reproduce the 6-row assumptions table from RESEARCH.md verbatim (columns: # / Claim / Section / Risk if Wrong). These must be verified at the build phase when the milestone is activated; they are not resolved here.

**Research valid until:** ~2026-07-15 (LangGraph 1.x moves fast; re-pin versions when this milestone is activated).
  </action>
  <verify>
    <automated>ls /Users/justinsearles/Projects/tide/.planning/milestones/v1.x-polyglot-subagent-MILESTONE.md && grep -c "Alternatives considered" /Users/justinsearles/Projects/tide/.planning/milestones/v1.x-polyglot-subagent-MILESTONE.md</automated>
  </verify>
  <done>File exists, contains "Alternatives considered" section, parity inventory table, contract-conformance table, provider-firewall gap section, five numbered open questions, and deferred section. No phase/task/requirements content present.</done>
</task>

<task type="auto">
  <name>Task 2: Add backlog milestone entry to ROADMAP.md</name>
  <files>.planning/ROADMAP.md</files>
  <action>
Edit .planning/ROADMAP.md to add the "Polyglot Subagent Runtimes" milestone as a backlog/future entry. Make three targeted edits:

**Edit 1 — Add to ## Milestones list** (after the v1.0.2 line):

Add two new lines after `🚧 **v1.0.2 — Ebb Tide: Token & Cost Optimization**`:

```
- 📋 **vNext — OpenAI Backend + Dogfood Run #2** — (planned; phases TBD)
- 📋 **v1.x — Polyglot Subagent Runtimes: LangGraph Strategy** — (backlog; architecture locked, phases TBD) — [framing doc](milestones/v1.x-polyglot-subagent-MILESTONE.md)
```

Use the 📋 marker (distinct from ✅ shipped and 🚧 in-progress) to signal "planned/backlog, not started."

Note: the OpenAI-backend milestone entry is being added here for completeness of the sequence per CONTEXT.md and project memory; it is NOT being scoped or framed in this task — one line only.

**Edit 2 — Add a collapsed details block under ## Phases** (after the v1.0.2 expanded section, i.e., after the Phase 21 details block):

```markdown
<details>
<summary>📋 vNext — OpenAI Backend + Dogfood Run #2 (Planned)</summary>

Scope TBD. Extends credproxy route allowlist for OpenAI paths, wires an OpenAI provider into the dispatch chain, and runs dogfood run #2. Sequenced after v1.0.2 "Ebb Tide."

</details>

<details>
<summary>📋 v1.x — Polyglot Subagent Runtimes: LangGraph Strategy (Backlog)</summary>

Architecture locked; task breakdown deferred. The `claude` CLI subagent becomes one named strategy behind the existing `pkg/dispatch.Subagent` image contract; a second Python/LangGraph container image implements the same envelope contract for full agent-loop parity. Sequenced after v1.0.2 "Ebb Tide" and after the OpenAI-backend milestone.

See [milestones/v1.x-polyglot-subagent-MILESTONE.md](milestones/v1.x-polyglot-subagent-MILESTONE.md) for the full framing: parity inventory, contract-conformance table, provider-firewall gap analysis, alternatives considered, and open questions.

</details>
```

**Edit 3 — No change to the Progress table.** The backlog milestones are not tracked in the progress table (that table covers phases with assigned numbers and plans). Leave the table as-is.

Do not modify any existing content in the file. Insert only; preserve all formatting.
  </action>
  <verify>
    <automated>grep -c "Polyglot Subagent Runtimes" /Users/justinsearles/Projects/tide/.planning/ROADMAP.md</automated>
  </verify>
  <done>ROADMAP.md contains the backlog entry in the ## Milestones list (with 📋 marker and link to the framing doc) and a collapsed details block under ## Phases. The v1.0.2 and prior entries are unchanged.</done>
</task>

</tasks>

<threat_model>
## Trust Boundaries

| Boundary | Description |
|----------|-------------|
| filesystem → planning artifacts | Writes land in .planning/; no code, secrets, or network surface involved |

## STRIDE Threat Register

| Threat ID | Category | Component | Disposition | Mitigation Plan |
|-----------|----------|-----------|-------------|-----------------|
| T-quick-01 | Tampering | ROADMAP.md edits | accept | Low-value target; doc artifact under git; any corruption is immediately visible in `git diff` |
</threat_model>

<verification>
1. `ls .planning/milestones/v1.x-polyglot-subagent-MILESTONE.md` returns the file.
2. `grep "Polyglot Subagent Runtimes" .planning/ROADMAP.md` returns at least 2 matches (Milestones list + details block).
3. `grep "Alternatives considered" .planning/milestones/v1.x-polyglot-subagent-MILESTONE.md` returns 1 match.
4. `grep "phases TBD\|task breakdown deferred\|Deferred" .planning/milestones/v1.x-polyglot-subagent-MILESTONE.md | wc -l` confirms the deferred section exists.
5. `grep -c "Requirements\|Phase [0-9]" .planning/milestones/v1.x-polyglot-subagent-MILESTONE.md` returns 0 (no task breakdown crept in; "Requirements" here would appear only in the contract section, so manual review applies).
</verification>

<success_criteria>
- .planning/milestones/v1.x-polyglot-subagent-MILESTONE.md exists with all required sections (three locked pillars, parity inventory table, contract-conformance table, provider-firewall gap, LangGraph fit summary, five numbered open questions, three alternatives considered, adjacent-milestone relationship section, deferred list, assumptions log).
- ROADMAP.md ## Milestones list contains a 📋 backlog entry for v1.x Polyglot Subagent Runtimes, sequenced after v1.0.2 and after the OpenAI-backend entry, with a link to the framing doc.
- No phase/task/requirements breakdown appears in either artifact.
- The voice matches the spec style (tight, declarative, em-dash-heavy, alternatives-considered convention).
</success_criteria>

<output>
Create `.planning/quick/260615-gos-explore-abstracting-cli-to-be-a-single-s/260615-gos-SUMMARY.md` when done.
</output>
