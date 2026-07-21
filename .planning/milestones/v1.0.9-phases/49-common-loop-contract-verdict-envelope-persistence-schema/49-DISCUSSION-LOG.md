# Phase 49: Common Loop Contract + Verdict/Envelope/Persistence Schema - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md — this log preserves the alternatives considered.

**Date:** 2026-07-18
**Phase:** 49-common-loop-contract-verdict-envelope-persistence-schema
**Mode:** `--auto` (fully autonomous — all gray areas auto-selected, each resolved to the recommended default; no interactive prompts)
**Areas discussed:** Type placement, Go↔Pydantic parity, VerifyContext shape, Fail-closed classifier, Findings persistence layout

---

## Type placement — where each contract lives

| Option | Description | Selected |
|--------|-------------|----------|
| Split by contract: `LoopPolicy`/`LoopStatus` in `api/v1alpha3`, `GateDecision`/`Finding` at the `pkg/dispatch` envelope seam | CRD-embedded types get deepcopy-gen + CRD embedding; verdict stays a wire-format doc adjacent to `EnvelopeOut` | ✓ |
| Everything in `api/v1alpha3` | Single home; but drags CRD machinery into the dispatch seam and misrepresents the wire verdict as a K8s object | |
| Everything in a new non-API Go package | Unified but loses CRD deepcopy generation for the loop types | |

**Choice:** Split by contract (recommended default). CRD-embedded loop types → `api/v1alpha3/loop_types.go`; envelope-seam verdict → `pkg/dispatch/verdict.go`. `LoopStatus.LastEvaluation` embeds a bounded verdict projection, never the full findings array.
**Notes:** `ConditionVerifyHalt` constant belongs in `shared_types.go` but is **Phase 50's** to add — this phase locks types only.

---

## Go ↔ Pydantic parity mechanism

| Option | Description | Selected |
|--------|-------------|----------|
| Hand-authored parallel pair + shared golden-JSON round-trip test | Mirrors the existing import-firewalled `verifier/envelope.py` re-impl; fixture is the proof | ✓ |
| Codegen from a single source of truth | One source, but new codegen infra and toolchain coupling for two small structs | |

**Choice:** Hand-authored pair + golden-JSON round-trip regression (recommended default). Go marshals canonical → fixture; Python validates + re-emits; Go unmarshals byte-equivalently.
**Notes:** The Python image is import-firewalled from the Go package (`make verify-dispatch-imports`) — a shared source is impossible; the fixture-backed parity is the established discipline.

---

## VerifyContext shape on EnvelopeIn

| Option | Description | Selected |
|--------|-------------|----------|
| Minimal `Verify *VerifyContext` pointer + 4th `Role="verifier"` | Mirrors `*DispatchMeta`/`*Dev`; carries pass-criterion cmd(s), requiredArtifacts, evaluatorRef, evidence-packet pointer; grows per consumer | ✓ |
| Carry the pre-2026-07-18 three-`Stage` config surface | Would bake the superseded per-stage framing into the locked schema | |
| A new parallel envelope kind for verify | Larger diff than a 4th Role; breaks the one-envelope-contract pattern | |

**Choice:** Minimal pointer struct, 4th Role (recommended default). Three-`Stage` field dropped under the single-loop reframe.
**Notes:** Matching Pydantic field added to `verifier/envelope.py`'s `EnvelopeIn`. ARCHITECTURE §Q4 gave the struct shape; the reframe supersedes its per-stage surface.

---

## Fail-closed verdict classification

| Option | Description | Selected |
|--------|-------------|----------|
| Shared classifier (Go + Python), empty/partial/malformed → `BLOCKED` | Never collapses to APPROVED; regression test per language covering empty-JSON / missing-verdict-field / malformed | ✓ |
| Classifier only in the Go controller | Leaves the Python verifier able to emit an unclassified verdict; parity gap | |
| Introduce a distinct `ESCALATE` terminal | Extra terminal; `BLOCKED` already IS the escalation/halt terminal Phase 50 consumes | |

**Choice:** Shared both-language classifier → `BLOCKED` (recommended default). Fail-open would reproduce the 2026-07-03 silent-`Complete` incident this milestone fixes.
**Notes:** Schema keeps machine verdict and judge verdict distinguishable so Phase 51 can enforce "deterministic failure dominates LLM approval"; the dominance *logic* is Phase 51, the *affordance* is here.

---

## Findings persistence layout (size×locality)

| Option | Description | Selected |
|--------|-------------|----------|
| Three tiers: `TerminationStub` verdict+counts · `LoopStatus.LastEvaluation` summary · full findings staged on run branch via `collectStageEnvelopes` | Reuses `NewTerminationStub` flatten + v1.0.7 git-artifact-store; no etcd blob, no new PVC path | ✓ |
| Full findings in `.status` | Violates LOOP-03 (etcd stays a state store, not an event DB); unbounded status growth | |
| A new dedicated PVC path for findings | Rejected by the project's own locality rule (`project_envelopes_as_artifacts.md`) | |

**Choice:** Three-tier size×locality split (recommended default). Extends `NewTerminationStub` (+ its `<4KB` test) and `collectStageEnvelopes`; a size test proves `LoopStatus` stays current-iteration-only (Success Criterion #5).
**Notes:** Subsumes ARCHITECTURE §Q5's ad-hoc `VerifyResult` struct into the shared `LoopStatus` contract per the reframe.

---

## Claude's Discretion

- Go struct field ordering, JSON tag spellings, kubebuilder validation markers on `LoopPolicy`/`LoopStatus`.
- One `pkg/dispatch/verdict.go` vs. split; golden-fixture path + test harness shape.
- Precise `VerifyContext` field names + evidence-packet pointer representation (within the minimal-fields decision).
- Fail-closed classifier as free function vs. method (within the both-languages + `BLOCKED`-terminal decision).

## Deferred Ideas

- `ConditionVerifyHalt` + `setVerifyHaltIfNeeded` + resume time-fence + dispatch-gate wiring → **Phase 50**.
- `TaskReconciler` verifier dispatch + concurrency-gate accounting + `LoopPolicy.BudgetCents` reservation + `onExhaustion` → **Phase 51**.
- `role="verifier"` Go prompt templates + coverage-not-conservatism split → **Phase 51** (EVAL-04).
- `"langgraph"` vendor sentinel + `SelfInstruments` + `EVALUATOR`-kind span → **Phase 51** (OBS-03).
- Reviewed-not-folded todos (2 dispatch-halt-gate findings → Phase 50; cache-f1 + GPG signing → deferred vNext) — see CONTEXT.md `<deferred>`.
