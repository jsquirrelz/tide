# ADK v2.0 — Subagent Execution-Strategy Evaluation

**Status:** Evaluation complete — recommendation is **(c): do not change the locked polyglot-subagent decision.** Research valid until ~2026-09-30; reopen criteria below.
**Date:** 2026-07-04
**Scope:** the subagent execution-image seam only (`pkg/dispatch.Subagent` + envelope contract, per-task Job). TIDE's orchestrator-level DAG engine (layered Kahn over native K8s Jobs) is explicitly OUT of scope — no candidate here replaces or absorbs it.

## Question

Google ADK v2.0 reached Go GA on 2026-06-30 with a graph-based execution engine (agents/tools/functions as workflow-graph nodes, conditional routing, dynamic branching). The locked v1.x polyglot-subagent milestone rejected a "Go-native SDK strategy" on the premise that "LangGraph in Python is more mature at exactly this than any Go agent framework in 2026" — research that predates ADK-Go 2.0. Should ADK v2.0: (a) replace the planned Python/LangGraph strategy image, (b) become a third strategy alongside the CLI and LangGraph images, or (c) leave the locked decision unchanged?

## Recommendation: (c) — no change

The milestone's rejection was re-verified against ADK-Go v2.0 rather than assumed, and it survives. Summary of why:

1. **Provider-agnosticism fails the hard rule at the implementation level.** CLAUDE.md forbids hard-coding one LLM provider anywhere in the stack. ADK-Go v2.0.0's only official model implementations are `model/gemini` and `model/apigee` — there is no official Anthropic or OpenAI package in `google/adk-go` (unlike Python ADK, which ships a first-party `AnthropicLlm`). Claude support in Go exists only via an unofficial community adapter (`Alcova-AI/adk-anthropic-go`) with unknown maintenance guarantees. Google's "model-agnostic" claim is true of the `model.LLM` interface, not of what ships in the Go box. Building TIDE's Claude execution path on an unofficial adapter — or hand-rolling one — is a materially worse provider story than LangGraph's officially maintained `langchain-anthropic`.
2. **Maturity: the rejection's premise still holds.** The ADK Go port is ~8 months old (preview Nov 2025, 1.0 GA 2026-03-31); the graph engine GA'd 2026-06-30 — days before this evaluation. The v1→v2 migration was breaking (new `/v2` module path, five new `Event` fields breaking rigid session schemas) and carries a silent-failure trap: legacy 1.x method overrides are "completely bypassed" by the graph engine and "silently ignored." LangGraph, by contrast, has been 1.0 GA since Oct 2025 with documented production use at NVIDIA scale (1000+ concurrent workers). (External research, 2026-07-04.)
3. **CACHE-F1 is not resolved by ADK.** See dedicated section below — the plausible "a Go-native strategy fixes CACHE-F1 and the firewall gap at once" argument turns out to point at a *direct-SDK* Go backend, not ADK.
4. **Graph-model overlap requires banning half the framework.** ADK 2.0's `workflow` package is a full second DAG engine with its own checkpointing, deterministic node IDs, and pause/resume across process restarts — conceptual duplication of TIDE's layered-Kahn orchestration, pod-scoped or not. Containment is achievable only by policy: use `LlmAgent` + tools and never touch `workflow`. Once you do that, ADK's residual value over raw `anthropic-sdk-go` for a Claude backend is approximately nil.
5. **No capability gap justifies a third image.** Dogfood run 2b's D3 (~60 concurrent subagent pods OOMing a single-node kind cluster) shows wave-scale dispatch is resource-sensitive; concurrency caps only landed in v1.0.6 (Phase 32). A third strategy triples the image/parity/conformance maintenance matrix and adds pressure toward heterogeneous-pool scheduling (CLAUDE.md's prescribed answer: "a wave-internal sub-scheduler behind Kahn") while delivering no capability the CLI and LangGraph images don't already cover between them.

License is not a factor either way: ADK-Go is Apache-2.0 (one internal test-helper exception, `internal/httprr`, not on any runtime path), fully standalone — Vertex AI Agent Engine, A2A, and Cloud-managed deployment are avoidable opt-ins, and telemetry is plain OpenTelemetry by default.

## Architecture Options

### Option A — Replace LangGraph with ADK-Go (rejected)
Gains language uniformity and same-module Go analyzability; loses the mature off-the-shelf agent loop (the entire reason the milestone chose LangGraph), swaps an official Anthropic integration for an unofficial one, and bets the second execution strategy on a graph engine with days of GA mileage. Does not resolve CACHE-F1.

### Option B — Add ADK-Go as a third strategy (rejected)
Everything under Option A, plus a 3× image maintenance/conformance matrix and heterogeneous-pool scheduling pressure, with no offsetting capability. The bar for a new execution strategy should be a capability gap the existing strategies can't close — a framework release is not that.

### Option B′ — Third strategy as a *thin direct-SDK Go backend* (not ADK) — noted, not recommended here
The strongest Go-native candidate surfaced by this evaluation is not ADK at all: a minimal Go binary implementing `Subagent` directly against `anthropic-sdk-go`, which is precisely the prototype CACHE-F1 already tracks ("a direct-SDK subagent backend that sets the system prompt explicitly… and places `cache_control` breakpoints on the shared stable prefix"). If that prototype graduates into a full execution strategy, it should do so through the GSD workflow on its own merits, under the CACHE-F1 / polyglot tracks — ADK adds nothing to it for a Claude backend.

### Option C — Keep the locked decision (adopted)
CLI image ships; Python/LangGraph remains the planned second strategy behind the unchanged `pkg/dispatch.Subagent` + envelope seam, per-task Job, credproxy-gated.

## Parity / Comparison Table

| Axis | `claude` CLI image (shipping) | LangGraph/Python image (locked plan) | ADK-Go 2.0 (candidate) |
|---|---|---|---|
| Agent loop maturity | First-party, battle-tested | LangGraph 1.0 GA Oct 2025; NVIDIA-scale production | Graph engine GA 2026-06-30 (~days); Go port ~8 months old |
| Anthropic/Claude support | Native | Official `langchain-anthropic` | **Unofficial third-party adapter only** |
| Multi-provider posture | Anthropic-only by nature (one strategy among several) | Broad official integrations | Official: Gemini + Apigee only; "agnostic" at interface level only |
| CACHE-F1 (`cache_control`) | **Broken** — CLI injects uncontrollable `cch` billing nonce | Possible via direct SDK use inside the image | **Not exposed** — `model.LLM` is Gemini-shaped `genai` types; would require bypassing ADK entirely |
| License | n/a (Anthropic tool) | MIT (LangGraph) | Apache-2.0; no GCP ToS coupling for OSS core |
| Per-pod footprint | node CLI + credproxy sidecar | Python container (heavier) | Go binary (lightest) — relevant post-D3, but accrues to any Go strategy |
| Build-time firewall reach | In-module; harness site carved out by design | **Unreachable** — Go analyzer can't see Python; runtime credproxy only | In-module `cmd/` binary would be *visible* to `./...` but *outside* `forbiddenScopes` by design (same treatment as `internal/subagent/anthropic`); `google.golang.org/adk`/`genai` not on the denylist today |
| Envelope/per-task-Job fit | Native | Designed for (milestone Pillar 2) | Fits — single `LlmAgent` process is cleanly poddable |
| Run-integrity contract (Phase 34) | Satisfies (commits to `EnvelopeIn.Branch`) | Satisfies — git worktree commit is pure shell-out, "trivially portable" | Satisfies, with one flag (below) |
| Orchestrator-overlap risk | None | Graph engine exists but is image-internal, per-task, stateless across Jobs | `workflow` pkg = second checkpoint/resume DAG engine; must be banned by policy |

## Provider-Firewall Analysis (build-time enforcement question)

The milestone correctly notes `tools/analyzers/providerfirewall` "cannot reach a Python container image," leaving LangGraph's neutrality runtime-enforced only (credproxy). The question was whether a Go ADK strategy could be brought under build-time enforcement. Reading the analyzer's actual source:

- **Mechanism:** a `golang.org/x/tools/go/analysis` pass (import-path prefix match per file) run via `go run ./cmd/tide-lint ./...` — scoped to the root module's package graph only. A separate-`go.mod` module (cf. `examples/tide-demo-fixture`) is invisible to it entirely.
- **Denylist** (`analyzer.go:53-58`): `github.com/anthropics/`, `github.com/openai/`, `github.com/sashabaranov/go-openai`, `github.com/google/generative-ai-go`. **No ADK/`genai` entries** — an ADK import would not trip it as written.
- **Scope** (`forbiddenScopes`): `pkg/controller`, `pkg/dispatch`, `pkg/dag`, `internal/controller`, `internal/webhook`, `internal/dispatch`. Subagent-implementation sites (`internal/subagent/*`, `cmd/credproxy`) are deliberately outside scope — provider SDKs are *supposed* to be importable there.

**Conclusion:** the "ADK gets us build-time enforcement" argument is weaker than it appears. A same-module `cmd/adk-subagent` binary would be no more firewalled than today's Anthropic harness adapter — the firewall's job is keeping SDKs out of the orchestrator core, and it does that identically in both worlds. The genuine, modest gain — whole-repo Go analyzability plus a two-line denylist addition (`google.golang.org/adk/`, `google.golang.org/genai`) to keep ADK/genai types out of controller/dispatch/dag — accrues to **any** same-module Go strategy and is not an argument for ADK specifically. If any Go-native strategy is ever adopted, add those denylist entries regardless.

## CACHE-F1 Interaction

CACHE-F1's diagnosis: the `claude` CLI front-loads a per-request-random `cch=<hex>` billing nonce ahead of caller content, defeating cross-pod prefix caching, with no CLI suppression lever; the fix requires a backend with direct SDK access to place `cache_control` breakpoints explicitly. Checked whether ADK-Go resolves this: **it does not.** ADK-Go has no official Anthropic model implementation, its `model.LLM` contract is built around Gemini-shaped `genai` types, and no `cache_control` concept surfaces through the abstraction; whether the unofficial adapter passes it through is undocumented (external research, 2026-07-04). Satisfying CACHE-F1 through ADK would mean hand-rolling against raw `anthropic-sdk-go` *outside* ADK's abstraction — i.e., ADK contributes nothing to the fix. CACHE-F1's already-tracked direct-SDK prototype (behind the same `Subagent` interface, provider-neutral per CACHE-05) remains the correct vehicle, and remains a separate track from the polyglot milestone, linked only by the shared seam.

## Run-Integrity Constraint (Phase 34) — checked against both candidates

Locked constraint, not under evaluation: every Succeeded task's worktree branch must be provably merged into the run branch — the gate is `git merge-base --is-ancestor <task-branch> <runBranch>`, recomputed live from git inside the orchestrator-side `tide-push` Job, never cached in `.status`, never delegated to the subagent image. This is **orthogonal to execution strategy**: any backend satisfies it by committing its work to the envelope's declared `Branch` via plain `git` shell-out (already listed as "trivially portable" in the milestone's parity inventory). Neither LangGraph nor ADK makes the guarantee harder — with one flag for ADK: its workflow engine's built-in checkpoint/resume-across-restarts semantics must never be used to manage git state or re-execute committed work across pod restarts. Task resumption and integration verification belong to the orchestrator; a subagent image that "helpfully" resumes itself could recreate the D4 defect class (success signals inconsistent with actual produced work). Avoidable, but only by policy — one more reason the `workflow` half of ADK would have to be banned outright.

## Graph-Model Overlap and Anti-Pattern Bounding

ADK 2.0's execution model — `workflow.StringRoute`/`IntRoute`/`BoolRoute` conditional edges, `JoinNode` fan-in, loop edges, dynamic nodes via `RunNode(...)`, deterministic node IDs, durable checkpointing, human-in-the-loop pause/resume across process restarts — is a full scheduler with persistence opinions, not a thin routing shim. CLAUDE.md's anti-patterns ("layered Kahn in stdlib, not a graph library"; no Argo/Tekton; waves derived at runtime, never declared) apply by extension: TIDE should not carry a second resumption-capable DAG engine anywhere in the stack, even pod-scoped, when the orchestrator already owns cross-task topology via Kahn over native Jobs. A2A (cross-service agent delegation) and Vertex Agent Engine are opt-in and avoidable, but A2A in particular would directly compete with TIDE's dispatch model if ever enabled. Any hypothetical ADK adoption would need a hard usage boundary — `LlmAgent` + tool-calling only; no `workflow`, no A2A, no Agent Engine, no `VertexAiSessionService` — enforced by review convention only, since the firewall doesn't reach package-internal API usage.

## Alternatives Considered

- **Replace LangGraph with ADK-Go (Option A):** rejected — worse Anthropic story, days-old graph engine, no CACHE-F1 payoff, loses the mature off-the-shelf loop that motivated LangGraph.
- **ADK-Go as third strategy (Option B):** rejected — 3× conformance matrix, heterogeneous-pool scheduling pressure post-D3, no unique capability.
- **Thin direct-SDK Go backend (Option B′):** the real Go-native contender, already tracked as CACHE-F1's prototype; not an ADK question. Should proceed (or not) through GSD under its own track.
- **Reopen the locked milestone to re-run its alternatives analysis:** rejected — this evaluation *is* that re-run for the Go-native branch, and the conclusion is unchanged.

## Open Questions / Reopen Criteria

Reopen this evaluation only if **all** of the following hold:
1. An official (or credibly audited and actively maintained) Anthropic model package exists for ADK-Go, with verified `cache_control` passthrough.
2. ≥2 quarters of post-2.0 API stability without another breaking migration.
3. Verifiable production usage of ADK-**Go** specifically (not Python ADK; Google's named ADK users are not confirmed to run the Go SDK).

Even then, the comparison baseline should be the direct-SDK Go backend (CACHE-F1 track), which may already exist and would make ADK's remaining value proposition — a Gemini-shaped agent loop — hard to justify for a Claude strategy.

Unverified externals, flagged honestly: ADK-Go's transitive cgo-freedom (high confidence pure-Go, not exhaustively verified via `go list -deps`); the unofficial Anthropic adapter's `cache_control` support (no documentation either way).

## Process Note

This document is a recommendation for a human decision-maker, produced outside the GSD workflow. It recommends **not** reopening the locked polyglot-subagent milestone. If a future decision goes the other way (Option A, B, or B′-as-strategy), that decision must go through this project's GSD workflow — research, requirements, milestone — before any implementation begins. Nothing here authorizes code changes.
