---
gsd_state_version: 1.0
milestone: v1.0.10
milestone_name: King Tide — Five Loops, One Successor Runtime, Dynamic Workflows
status: planning
last_updated: "2026-07-21T14:02:25.263Z"
last_activity: 2026-07-21
progress:
  total_phases: 12
  completed_phases: 0
  total_plans: 0
  completed_plans: 0
  percent: 0
---

# Project State

## Project Reference

See: .planning/PROJECT.md (updated 2026-07-21)

**Core value:** The five-level paradigm (Milestone → Phase → Plan → Task → Wave) runs as a real K8s orchestrator that can drive its own next milestone end-to-end.
**Current focus:** v1.0.10 "King Tide" ROADMAPPED (2026-07-21) — 12 phases (54–65), 30/30 requirements mapped, awaiting `/gsd:plan-phase 54`

## Current Position

Phase: 54 of 65 (Runtime Selection Foundation + Observability Gap Closure) — 1st of 12 phases in v1.0.10
Plan: — (not yet planned)
Status: Ready to plan
Last activity: 2026-07-21 — ROADMAP.md created for v1.0.10 (Phases 54–65, 30/30 requirements mapped, 0 orphans)

Progress: [░░░░░░░░░░] 0%

## Performance Metrics

**Velocity (recent milestones):**

- v1.0.9: 46 plans across 6 phases in ~4 days (2026-07-18 → 2026-07-21) · 354 commits · +58.6k/−0.9k LOC
- v1.0.8: 32 plans across 6 phases in ~3 days (2026-07-15 → 2026-07-17) · 240 commits · +34.8k/−343 LOC
- v1.0.7: 51 plans across 8 phases in ~12 days (2026-07-03 → 2026-07-15)
- v1.0.6: 8 plans across 3 phases in ~2 days (2026-06-28 → 2026-06-29)
- v1.0.5: 3 plans, 1 phase (2026-06-27)
- Total plans completed v1.0.0–v1.0.9: ~430+

## Accumulated Context

### Decisions

Decisions are logged in PROJECT.md Key Decisions table.

**v1.0.9 binding constraints (Task loop + five-loop model — see [notes/five-loop-model.md](notes/five-loop-model.md), research committed `f85ee3d`):**

- **Verification closes a loop, not a gate.** One verification-driven loop parameterized per level by `LoopPolicy`: Task `maxIterations:N` (auto-repair, the core), Plan/plan-check `maxIterations:1` (re-plan), Phase/Milestone/Project `maxIterations:0` (escalate to `requireApproval`). Gate policy resolves from loop level + risk + confidence, not hierarchy position.
- **Shared loop contract, not a generic Loop controller.** `LoopPolicy`/`LoopStatus` are shared API types embedded in domain CRDs. Minimal fields for the two v1.0.9 consumers (Task loop + plan-check); grow per loop. Five elements (goal, candidate, evaluator feedback, repeat policy, bounded exit) or it is a pipeline stage, not a loop.
- **Iteration history lives in traces/artifacts, never CRD status** — etcd stays a state store, not an event DB. `LoopStatus` carries only the current-iteration summary + exit reason.
- **infra-retry ≠ quality-iteration.** Eviction/transient rerun of the same attempt is preserved; the blind `maxAttemptsPerTask` quality-retry is replaced by evaluator-driven fresh attempts that receive the original spec + a compact evidence packet, not the prior agent's full context.
- **The evaluator is logically independent** from the implementation agent (the read-only LangGraph image, a distinct runtime/process), and **a deterministic failure dominates an LLM judge's approval**. The Execution (in-Job) loop never stamps the Task correct.
- **Fail-closed verdict handling** — empty/partial/unparseable `gate_decision` routes to escalation, never collapses to APPROVED (fail-open would reproduce the 2026-07-03 silent-`Complete` incident this milestone exists to fix).
- **`ConditionVerifyHalt` mirrors `failure_halt.go` + Phase 25's resume time-fence, gates BOTH tiers** (a BLOCKED verify means the artifact tree is suspect), and is a **distinct halt class** — never a reinterpretation of `Failed` wave semantics.
- **Read-only enforced structurally** (ReadOnly mount + credential omission, no manager-side child-CRD consumption path), not by prompt. Verifier prompts render orchestrator-side (Go template, no Python port).
- **Cost/concurrency is the biggest multiplier yet** (attempts × evaluator × levels): `LoopPolicy.BudgetCents` + the reservation store + the Phase-32 concurrency gate (verifier pods MUST be counted, same phase as dispatch sites) bound it; `onExhaustion: requireApproval` is the human backstop.
- **A1 correction:** httpx honors `SSL_CERT_FILE` only (`REQUESTS_CA_BUNDLE` is dead); the credproxy-TLS path through `ChatAnthropic` is a genuine build spike (`langchain#35843`), scheduled first (Phase 48) with an `http_client=`/`anthropic_client=` fallback.
- **Named future arc:** Product / System / Oversight loops are later milestones; `internal/eval` seeds the System loop, the existing gates seed Oversight enforcement (resolve gate policy from loop level/risk/confidence/history).

**v1.0.10 "King Tide" binding constraints (research committed 2026-07-21, confidence MEDIUM-HIGH):**

- **Nothing new needed at the dispatch spine** — every King Tide capability rides the seam the read-only verifier already proved: `EnvelopeIn`/`EnvelopeOut`, `pkg/dispatch.Subagent`, one Job per dispatch, and three precedence-chain resolvers (`ResolveProvider`, `resolveImage`, `ResolveLoopPolicy`) as the only config chokepoints.
- **The fan-out + reduce primitive is the one genuinely new, highest-leverage build item** — shadow-pair runtime comparison and all three dynamic-workflow patterns (judge panel, generate-and-filter, tournament) are the SAME mechanism with different N and reduce strategy. Its cost/OOM rails (`maxShape`, per-wave aggregate cap) MUST land in the SAME phase as the first fan-out shape (Phase 57), never retrofitted — direct lesson from the dogfood run-2b OOM incident.
- **"Wired but never ran the shipped path" is this project's own recurring defect class** (Phase 22 stale embed, Phase 51 nil-verdict relay, Phase 52 DEFECT-B/C). Every phase introducing a NEW loop-closing path (Product re-plan, System promotion, Oversight escalation, a fan-out reduce step) requires a live billable proof run attached to its verification record, not deferred to milestone close.
- **`SelfInstruments("langgraph")` already returns true with no instrumentation behind it** — every live LangGraph dispatch today produces zero trace spans. Closed in Phase 54, first, not inherited silently into the write-capable authoring image.
- **System loop precedes the rungs that consume it** (Phase 56 before Phase 58) — the migration ladder's evidence-gate requirement makes System loop's core contract a hard dependency, not a parallel track. SYS-01 requires recorded candidate/experiment artifacts from day one (no separate later "persist it" phase).
- **Oversight's classifier feature schema (OVR-05/06) lands EARLY** (Phase 55, second phase) specifically so every subsequent loop (System/Product/Oversight itself, every fan-out shape) generates labeled training data from its first iteration onward — not bolted on after the loops exist.
- **Product/Oversight loops are sequenced last among the loop-closing phases** because each needs real signal (promotion history, verdict/track-record data) to be meaningful; building them earlier would produce loops with nothing real to compute over.
- **Multi-provider + CLI-deprecation (Phase 65) is explicitly the closing move** — gated on the accumulated evidence from the full ladder (Phase 58 planner rungs + Phase 61 executor rung), not pulled forward just because `init_chat_model` is mechanically simple.
- **Gemini is out of scope this milestone** (Future Requirements) — its dual CA-trust path (`SSL_CERT_FILE` + `REQUESTS_CA_BUNDLE`) is unverified and needs its own build spike; only Anthropic + OpenAI ship as PROV-01..04.
- **`sounding-dynamic-orchestration-design.md` is a precedent, not a locked design** — re-confirm the fan-out primitive's actual scope against Phase 57's real requirements at `/gsd:plan-phase 57`, don't assume its full apparatus (judge subagent escalation, ML classifier tiers) is in scope.

### Pending Todos

- CACHE-F1 direct-SDK cross-pod caching backend — `.planning/todos/pending/cache-f1-direct-sdk-cross-pod-caching.md` (deferred; carried forward, no v1.0.10 requirement touches it).
- `subagent.levels` semantic rename — CLOSED, folded into v1.0.7 Phase 40 (CRANK-04).

### Blockers/Concerns

- None currently blocking — v1.0.10 roadmap just created; no phase execution has started.
- **Carried lesson from v1.0.8/v1.0.9 release cascades:** GSD per-phase verification never runs the `ci.yaml`-only gates (`make lint`, `verify-dashboard-freshness`, kind image-pin tests) — wire these into each v1.0.10 phase's verification, don't wait for release pre-flight to catch them.
- **Cross-pod clock skew (Pitfall 5, v1.0.8) remains unverified** — single-node kind can't surface child-span-outside-parent-window rendering; relevant again for OBS-06's fan-out-sibling-group Phoenix queries in Phase 64.

### Roadmap Evolution

- **v1.0.10 roadmap defined 2026-07-21:** Phases 54–65 (12 phases), 30 requirements (MIG-01..06, PROV-01..04, PROD-01..03, SYS-01..04, OVR-01..06, FAN-01..05, OBS-05..06), 100% mapped, 0 orphans. Phase numbering continues from v1.0.9 (Phase 53 was the last phase); Phase 54 is the first v1.0.10 phase.
- Sequencing deviates from research's suggested 12-phase order in one deliberate way: the System loop's controller-CRD persistence stage (research's separate Phase 10) is merged into Phase 56 up front, because SYS-01 requires recorded candidate/experiment artifacts from day one and MIG-04 (Phase 58) has a hard dependency on SYS-02 existing before any rung promotes — matches the milestone brief's explicit sequencing instruction over research's later-persistence suggestion.
- Dependency chain: 54 (foundation) and 55 (Oversight schema, sequenced early per instruction) have no hard technical dependency on each other; 56 (System loop) and 57 (fan-out primitive) are each independent and both precede 58 (planner rung migration, consumes both); 59/60 (judge panel / generate-filter) each depend only on 57; 61 (executor rung) depends on 58; 62 (tournament) depends on 57+59+60; 63 (Product loop) depends on 56; 64 (Oversight loop + full observability closure) depends on 63+56; 65 (multi-provider + CLI-deprecation) depends on 61+58 as the milestone's closing move.
- Research-flagged phases (`research: true`): Phase 57 (fan-out + reduce primitive — no existing code precedent beyond `ChildCount`), Phase 61 (executor rung — agent-loop eval dimensions don't exist in any form today), Phase 62 (tournament — budget pre-flight math + tie-break rules are novel), Phase 64 (Oversight — autonomy-resolver heuristic formula is from-scratch design).
- v1.0.9 roadmap (for reference): Phases 48–53, 28 requirements, 100% mapped, strict chain 48→49→50→51→52→53.
- v1.0.8 roadmap (for reference): Phases 42–47, 19 requirements, 100% mapped, strict chain 42→43→44→45→46→47.

## Deferred Items

Items acknowledged and deferred at v1.0.9 close (2026-07-21) — 31 open artifacts, none blocking, carried forward unchanged into v1.0.10.

| Category | Count | Notes |
|----------|-------|-------|
| quick_tasks | 24 | SUMMARY frontmatter `status:` field missing/unknown — audit-scanner bookkeeping only; the work itself shipped (same class carried since v1.0.7) |
| todos | 2 | signed-commits-verified-badge (GPG scope, Future Requirements) · cache-f1-direct-sdk-cross-pod-caching (vNext+). The two W-2 dispatch-gate todos were FOLDED and closed in Phase 51 (D-09). |
| debug_sessions | 2 | knowledge-base.md (a KB file, not a session) · layer-a-envtest-flakes-pr9 [investigating] — CI-side Layer A envtest flakes; local envtest runs green |
| uat_gaps | 2 | 51-HUMAN-UAT.md + 53-HUMAN-UAT.md — both phases' live proofs were operator-approved; files stay `partial` until `/gsd:verify-work` formally records results |
| verification_gaps | 1 | 53-VERIFICATION.md `human_needed` — the approved live-render item's bookkeeping trail |

Tech-debt still carried forward: `LoopPolicy.Autonomy` unconsumed until Phase 64 (Oversight loop — this is the v1.0.10 phase that finally consumes it), `level_verify_dispatch_test.go:682` Eventually-wrap advisory, no SECURITY.md for Phases 52/53 (`/gsd:secure-phase` recommended), W-4 agentName/agentEmail CRD pattern locks not re-established post-crank, Phase 36 residual 'bot' vocabulary (7 comment/fixture refs), 37-REVIEW advisory warnings (secrets RBAC blast radius, gitfetch timeouts, settings-match determinism, Job-name coupling) + GIT_PAT fetch-path allowance.

## Session Continuity

Last session: 2026-07-21T14:02:25.263Z
Stopped at: v1.0.10 ROADMAP.md + REQUIREMENTS.md traceability created (Phases 54–65, 30/30 requirements mapped) — awaiting roadmap approval
Resume file: None

## Operator Next Steps

- Review and approve the v1.0.10 roadmap, then start `/gsd:plan-phase 54`
