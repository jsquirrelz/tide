# vNext — Specialist Verify Tier + LangGraph Beachhead

**Status:** Scoped 2026-07-06 via /gsd:explore — architecture pillars locked; requirements and phases TBD at `/gsd:new-milestone`.
**Sequence:** Picks up after v1.0.7 "First-Run Paper Cuts" closes. Deliberately **before** dogfood run #2 — the verify tier is what makes a run's `Complete` stamp provable rather than hoped-for.
**Strategic frame:** First rung of the evidence-gated successor-runtime ladder — see [notes/langgraph-successor-runtime-strategy.md](../notes/langgraph-successor-runtime-strategy.md).

## Problem

TIDE's five compiled-in templates are all authoring/executing — nothing in-cluster checks, verifies, or reviews. The first external-repo run (2026-07-03) stamped `Complete` with a declared deliverable missing from the pushed branch, and the outcome prompt's pass criterion ("pytest green") was never executed by anything in-cluster; the only verifier in the loop was the human operator diffing `filesTouched` by hand. Phase 34 closes the mechanical, no-LLM degenerate case (git-verified merge completeness); this milestone ships the semantic tier. Full stage inventory and gap maps: [seeds/verify-level-subagent.md](../seeds/verify-level-subagent.md).

## Architecture Decision (locked)

**Pillar 1 — Verify tier only.** Three stages, one new template class (`verifier`):

- **plan-check** — post-plan-authoring, pre-task-dispatch: goal-backward "will these tasks achieve the level objective?" plus declared-vs-plausible file-touch sanity.
- **level-verify** — after a level's children succeed, before the level stamps Succeeded / boundary push: run the declared gate command in-worktree, confirm every declared deliverable exists on the run branch, confirm constraints (files NOT to touch) were honored.
- **integration-check** — at milestone/project boundaries: do sibling outputs actually compose; does the full run branch build/test as a whole.

Debug, code review, research, learnings, and tournament are explicitly deferred (the seed records their shapes and sequencing tiers).

**Pillar 2 — Read-only LangGraph specialist image.** The verifier class dispatches on a **new** Python/LangGraph container image behind the existing `pkg/dispatch.Subagent` + envelope contract — per-task Job, credproxy-gated, exactly like today's CLI image. Its surface is deliberately a small subset of the polyglot doc's full parity inventory:

- envelope in/out + TerminationStub (language-neutral per the polyglot conformance table)
- git **read** access to the run branch / worktree
- bash — to execute the declared gate command; incidental writes (test caches, build artifacts) are fine in its ephemeral workspace, but the image **never commits, never pushes, never authors run-branch artifacts**
- `with_structured_output` for the `gate_decision` verdict — findings carry severity + confidence tags
- NO file-edit tools, no worktree-commit machinery, no child-CRD authoring, no five-template parity port

The polyglot doc's contract-conformance table applies wholesale; the single Node-specific element (`NODE_EXTRA_CA_CERTS`) maps to `SSL_CERT_FILE`/`REQUESTS_CA_BUNDLE` (its assumption A1 — verify httpx honors it — is still open and lands here).

**Pillar 3 — Failure semantics.**

- **plan-check REJECT** → bounded re-plan loop: re-dispatch the planner with findings appended to the prompt, ≤ N attempts (config; default 1–2), then halt.
- **level-verify / integration-check BLOCKED** → halt immediately for a human — post-execution re-work discards paid work; that call is the operator's.
- The halt is `ConditionVerifyHalt`, a **new project-level halt class** mirroring the BillingHalt/FailureHalt pattern (including its resume/recovery discipline — note Phase 25's resume-ordering lessons). Wave-boundary failure semantics are untouched: a BLOCKED verify is a new halt class, not a reinterpretation of task failure.

**Pillar 4 — Dynamism placement.** The execution DAG stays static and derived, always. Dynamism lives (a) inside the pod as LangGraph conditional loops (plan → act → self-check → retry) and (b) at lifecycle seams via stage routing. Runtime DAG mutation was considered and rejected.

## Orchestrator surface (expected shape — not yet planned)

- Per-level stage-dispatch config, like gates and models today — gate policy stays in config, never baked into the controller. LLM stages cost money; default posture is an open question below.
- `gate_decision` enforcement in the reconcilers, the new halt condition, and its resume path.
- The verifier envelope carries: level objective, declared deliverables, the gate command, constraints, and pointers to level artifacts.
- Prompting: **coverage, not conservatism** — find everything with severity/confidence tags and filter downstream (CLAUDE.md's subagent-tuning note applies verbatim).

## Open Questions (defer to new-milestone / plan-phase)

1. N default for the re-plan loop (1 or 2), and whether the loop shares `maxAttemptsPerTask` machinery or gets its own counter.
2. `gate_decision` persistence: status condition + small in-CRD findings summary, full findings artifact where? The envelopes-as-artifacts size×locality rule applies — no blobs in etcd.
3. Verifier prompt source: rendered orchestrator-side from a sixth Go template and passed in the envelope (no Python template port; avoids the polyglot doc's Q2 drift problem) vs. in-image. Leaning orchestrator-side.
4. Model tier for verifiers (cheap-model verify vs. planner-tier) and the per-level override slot — interacts with `todos/pending/2026-07-03-project-level-subagent-override-slot.md`.
5. Is integration-check a distinct template, or level-verify parameterized at milestone/project levels?
6. Stage default posture: off / milestone+project only / all levels.
7. `langchain-anthropic` cache_control + params passthrough (research/questions.md) — matters for the ladder more than for this milestone, but cheapest to verify while building the image.

## Relationship to adjacent work

- **Phase 34 (v1.0.7):** the mechanical completeness gate lands there regardless — this milestone is its LLM-tier sibling, not a replacement.
- **Dogfood run #2:** sequenced after this milestone so the run executes with in-cluster verification watching it; its deliverable is retargeted at its own scoping (see the strategy note).
- **v1.x LangGraph Authoring Migration:** this image is its seed; parity grows role-by-role behind eval-harness evidence.

## Deferred

Debug / review / research / learnings / tournament stages; authoring parity (file tools, commits, child-CRD emission, template port); the CLI-deprecation decision; the OpenAI provider (credproxy routes + pricing rows); the run-#2 retarget.

**Research valid until:** LangGraph/langchain-anthropic pins re-verify at activation (1.x moves fast — polyglot doc's pinning rule applies).
