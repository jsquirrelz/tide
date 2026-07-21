# Phase 51: The Task Loop - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md — this log preserves the alternatives considered.

**Date:** 2026-07-19
**Phase:** 51-the-task-loop
**Areas discussed:** GateCommand schema location, LangGraph vendor sentinel (the two ROADMAP-flagged open calls; all other decisions auto-resolved under `--auto`)

---

## GateCommand schema location (D-01)

The per-level pass-criterion gate command (deterministic, exit-code-parsed) that resolves onto the existing `VerifyContext.GateCommand` wire field — where is it declared in the CRD schema?

| Option | Description | Selected |
|--------|-------------|----------|
| Explicit field, Task-scoped now | `verification` block on `TaskSpec` only in Phase 51; same shape generalizes to Plan/Project in Phase 52. Planner-authored, immutable-locked, git-reproducible. | ✓ |
| Explicit field, all levels now | Add `verification` to Task + Plan + Project specs now with a resolution precedence; front-loads Phase-52 schema. | |
| Convention-based lookup | Gate command discovered from a repo convention (`.tide/gate.sh` / `make verify-<level>`); no authored CRD field. Cannot satisfy TASK-01 immutability/reproducibility. | |

**User's choice:** Explicit field, Task-scoped now.
**Notes:** The only option consistent with TASK-01's "immutable once locked (Draft→Locked→Superseded), `git show <sha>` reproduces exactly what was dispatched" — a repo convention can drift between lock and dispatch and is not planner-authored. Plan/Project-level fields + Task > Plan > Project precedence deferred to Phase 52 per the roadmap phase split (keeps this Task-focused phase's schema surface minimal).

---

## LangGraph vendor sentinel (D-02)

The Phase-48 read-only LangGraph evaluator image self-instruments (native OpenInference spans), so `SelfInstruments(vendor)` must return true for it — but it calls `ChatAnthropic` underneath. How is its runtime identified vs the anthropic-CLI executor?

| Option | Description | Selected |
|--------|-------------|----------|
| New `"langgraph"` sentinel | Register `"langgraph"` as a `ProviderSpec.Vendor` literal; `SelfInstruments("langgraph")→true`; verifier image refuses other vendors at startup. Keeps `SelfInstruments` a pure vendor predicate; precedent = `"opencode"` already a runtime-shaped vendor. | ✓ |
| Reuse `"anthropic"` + discriminator | Keep `Vendor="anthropic"`; add a runtime discriminator (Runtime field / key off Role). Forces a `SelfInstruments` signature change (ADAPT-01 seam churn) + ambiguous startup sentinel. | |

**User's choice:** New `"langgraph"` sentinel.
**Notes:** Preserves the Phase-45 ADAPT-01 seam signature (`SelfInstruments(vendor string) bool` stays a pure predicate; reporter keeps trusting the manager-computed boolean on the Job). Matches the existing precedent that `"opencode"` — a runtime/wrapper — is already a Vendor value, not a pure LLM vendor. Model still resolves normally via `ResolveProvider` (Vendor=`langgraph`, Model=`claude-…`).

---

## Claude's Discretion

Auto-resolved to recommended defaults under `--auto` (grounded in the requirements + prior-phase patterns), captured as D-03…D-12 in CONTEXT.md:
- Verification lifecycle/locking mechanism (Draft→Locked→Superseded + version + `lockedSHA`, CEL-enforced) — D-03
- Compact evidence packet via existing `VerifyContext.EvidencePacketPath`, bounded — D-04
- infra-retry vs quality-iteration path distinction — D-05
- Deterministic-dominates-judge enforcement (verifier + controller-side) — D-06
- maxIterations bound + resumable `LoopStatus` (re-derived, no history) — D-07
- Three-tier escalation + structural anti-gaming detector (changed-file intersect) — D-08
- `ConditionVerifyHalt` clone of `failure_halt.go`, gates both tiers + unifies the dispatch-hold chains — D-09
- Dedicated `verifierInFlightCount` + `LoopPolicy.BudgetCents` via `ReservationStore` — D-10
- `EVALUATOR`-kind sibling span + `evaluation.*` population — D-11
- `role="verifier"` compiled-in Go template, coverage-not-conservatism — D-12
- Field names / JSON tags / CEL spelling / cap defaults — within the above decisions

## Deferred Ideas

- Per-level verification (Plan/Phase/Milestone/Project) + resolution precedence → Phase 52 (ESC-01)
- Chart-first config surface + default posture → Phase 53 (CFG-01/02)
- Dashboard nested-provenance + `VerifyHalt` visual state → Phase 53 (OBS-04)
- Composite evaluators; Product/System/Oversight loops → named future arc

**Folded todos:** `2026-07-12-project-dispatch-missing-failurehalt-gate` + `2026-07-12-task-dispatch-gate-order-divergence` — folded into D-09 (this phase edits the exact dispatch-hold chains; VerifyHalt wiring unifies all five chains + closes the pre-existing FailureHalt gaps, gated behind a co-occurring-holds envtest).
**Reviewed, not folded:** `2026-07-03-signed-commits-verified-badge` (GPG, deferred by choice), `cache-f1-direct-sdk-cross-pod-caching` (vNext+).
