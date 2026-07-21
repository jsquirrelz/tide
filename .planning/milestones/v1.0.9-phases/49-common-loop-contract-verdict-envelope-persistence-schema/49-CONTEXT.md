# Phase 49: Common Loop Contract + Verdict/Envelope/Persistence Schema - Context

**Gathered:** 2026-07-18
**Status:** Ready for planning

> **Mode:** `--auto` (fully autonomous). All 5 gray areas auto-selected; each
> resolved to the recommended default. Single-pass — CONTEXT.md written once.
> Decisions below are grounded in the ROADMAP §Phase 49 success criteria, the
> v1.0.9 binding constraints (PROJECT.md / STATE.md / `f85ee3d`), and the
> pre-decided type shapes in `research/ARCHITECTURE.md` Q4/Q5 — **with the
> Phase-48 caveat that ARCHITECTURE's pre-2026-07-18 "three verify stages"
> framing is superseded by the single `LoopPolicy`-parameterized loop reframe**
> (the struct *shapes* survive; the per-stage config surface does not).

<domain>
## Phase Boundary

Lock the **shared, reusable primitives** the entire Task loop (Phase 50/51) will
be built on top of — as **types + contracts only**, before any halt-condition
or reconciler logic exists:

1. **`LoopPolicy` / `LoopStatus`** — shared Go API types embeddable in any domain
   CRD (MaxIterations/MaxDuration/BudgetCents/Autonomy/EvaluatorRef/EscalationPolicy;
   Iteration/ParentRunID/LastEvaluation/ExitReason/CostCents/Conditions), carrying
   type-level doc-comments that apply the **five-element loop test**. **No generic
   `Loop` controller.**
2. **`gate_decision` verdict schema** — a matched **Go + Pydantic** `GateDecision` /
   `Finding` pair (`APPROVED | REPAIRABLE | BLOCKED` + `findings[]` with
   dimension/severity/confidence/evidence/suggested_fix + summary) that round-trips
   through the envelope seam and is classified **fail-closed**.
3. **`VerifyContext` pointer field on `EnvelopeIn`** — the verify-specific input
   payload (pointer + omitempty, a fourth `Role="verifier"`), mirroring the existing
   `*DispatchMeta` / `*Dev` pattern.
4. **Findings size×locality persistence contract** — a ≤4 KB verdict+counts summary
   on `TerminationStub`, a small per-CRD `LoopStatus` status summary, and the **full
   findings artifact staged on the run branch** via the extended `collectStageEnvelopes`
   — never an etcd blob, never a new PVC path.

**This phase is SCHEMA / CONTRACT DEFINITION ONLY.** The types exist, round-trip,
classify fail-closed, and pass size tests. Deliberately **NOT** in this phase:

- **`ConditionVerifyHalt` + `setVerifyHaltIfNeeded` + the resume time-fence + dispatch-gate wiring** → Phase 50 (the halt-condition + reconciler logic on top of these types).
- **`TaskReconciler` verifier dispatch, concurrency-gate accounting, `LoopPolicy.BudgetCents` reservation, `onExhaustion` escalation** → Phase 51.
- **`role="verifier"` orchestrator-side Go prompt templates + coverage-not-conservatism split** → Phase 51 (EVAL-04).
- **The `"langgraph"` vendor sentinel + `SelfInstruments` registration + `EVALUATOR`-kind span** → Phase 51 (OBS-03).

Success = the primitives are locked so the halt/dispatch phases build on a settled
contract, not a moving one.

</domain>

<decisions>
## Implementation Decisions

### Type placement — where each contract lives (GA1)
- **D-01 (Two homes for two contracts — recommended):** The **CRD-embedded** loop
  contract (`LoopPolicy` / `LoopStatus`) lands in `api/v1alpha3/` in a dedicated new
  file (`loop_types.go`) — the **sole served+storage version** (Phase 40), so
  kubebuilder `controller-gen` produces the DeepCopy methods and the types are
  embeddable in domain CRD `Spec`/`Status`. The **envelope-seam** verdict contract
  (`GateDecision` / `Finding`) lands in `pkg/dispatch/` alongside `EnvelopeOut` (a new
  `verdict.go`), because the verdict round-trips through the file-envelope seam the
  verifier image writes — it is **not** a CRD type. `LoopStatus.LastEvaluation` embeds
  a **bounded projection** of the verdict (decision + counts), never the full
  `Finding` array. Condition/reason vocabulary continues to live in
  `api/v1alpha3/shared_types.go` (its established home) — but the actual
  `ConditionVerifyHalt` constant is **Phase 50's** to add, not this phase's.
  *Rejected: putting `GateDecision` in `api/v1alpha3` — it would drag CRD-machinery
  imports into the dispatch seam and misrepresent a wire-format doc as a K8s object.*

### Go ↔ Pydantic parity mechanism (GA2)
- **D-02 (Hand-authored pair + shared golden-JSON round-trip test — recommended):**
  The Go `GateDecision`/`Finding` (`pkg/dispatch`) and the Pydantic pair
  (`cmd/tide-langgraph-verifier/verifier/`) are **hand-authored in parallel** — no
  codegen — and kept honest by a **shared golden JSON fixture** exercised by a
  **cross-language round-trip regression test**: Go marshals the canonical verdict →
  committed fixture; Python validates the same fixture and re-emits it; Go unmarshals
  the Python output byte-equivalently. This mirrors the **already-established**
  pattern where `verifier/envelope.py` hand-re-implements the Go `EnvelopeIn`/`OUT`
  shapes under strict `apiVersion`/`kind` equality (the Python image is import-firewalled
  from the Go package — it cannot share types). Field JSON tags/aliases are the
  contract; the fixture is the proof.

### `VerifyContext` field shape on `EnvelopeIn` (GA3)
- **D-03 (Minimal pointer struct, 4th Role — recommended):** `Verify *VerifyContext
  \`json:"verify,omitempty"\`` on `EnvelopeIn`, mirroring `*DispatchMeta` / `*Dev`
  (`envelope.go:146,152`) — non-verify dispatches serialize nothing. A fourth
  `Role` value `"verifier"` joins `"planner"`/`"executor"` (per ARCHITECTURE §283:
  a smaller diff than a parallel envelope kind). `VerifyContext` carries only what
  the two v1.0.9 consumers need: the **planner-authored pass-criterion command(s)**
  (the explicit CRD field, per the `f85ee3d` scoping), **requiredArtifacts**, an
  **evaluatorRef**, and a pointer to the **compact evidence packet** a repair attempt
  receives (original spec + evidence, *not* the prior agent's full context). The
  matching Pydantic field is added to `verifier/envelope.py`'s `EnvelopeIn`.
  **Under the loop reframe the pre-2026-07-18 three-`Stage` field is dropped** — keep
  `VerifyContext` minimal and grow it per consumer; do not bake the superseded
  per-stage config surface into the locked schema.

### Fail-closed verdict classification (GA4)
- **D-04 (Shared classifier, unparseable/empty/partial → BLOCKED — recommended):** A
  **classifier helper mirrored in both languages** (Go func in `pkg/dispatch`, Python
  func in the verifier) maps any **empty / missing-`verdict`-field / malformed /
  partially-validated** `gate_decision` to **`BLOCKED`** — the escalation terminal —
  and **never** to `APPROVED`. Fail-open would reproduce the 2026-07-03 silent-`Complete`
  incident this milestone exists to fix. A **regression test in each language** covers
  the three named shapes: empty-JSON, missing-verdict-field, and malformed. `BLOCKED`
  is the terminal a later `ConditionVerifyHalt` gate consumes (Phase 50) — this phase
  only guarantees the *classification*, not the halt.
- **D-04b (Determinism dominates — captured, enforced downstream):** The contract
  encodes that **a deterministic (exit-code) failure dominates an LLM judge's
  approval** — the `GateDecision` schema keeps the machine verdict and the judge
  verdict distinguishable so Phase 51's handler can let the deterministic signal win.
  The *dominance logic* is Phase 51; the *schema affordance* is here.

### Findings persistence — size×locality (GA5, per ARCHITECTURE Q5 + the loop reframe)
- **D-05 (Three tiers, no etcd blob, no new PVC path — recommended):**
  - **(a) `TerminationStub` (≤4 KB hard cap, `envelope.go:394`):** add a bounded
    `GateDecision` (verdict string) + `FindingsCount` / `HighSeverityCount` counts,
    extending `NewTerminationStub` the same way it already flattens
    `ExitCode`/`Reason`/`Usage`/`ChildCount`. The `<4096`-byte invariant test
    (`TestNewTerminationStub_StaysSmall`, + the Python `write_termination_stub`
    truncation loop) is extended to cover the new fields.
  - **(b) per-CRD status:** `LoopStatus.LastEvaluation` carries only the
    **current-iteration** verdict summary (decision + counts + top-severity +
    completedAt) — **not** the `findings[]` array and **not** an accumulating
    iteration history. This subsumes ARCHITECTURE Q5's ad-hoc `VerifyResult` struct
    into the shared `LoopStatus` contract (the reframe). A **size test proves**
    `LoopStatus` on a consuming CRD stays current-iteration-only (Success Criterion #5).
  - **(c) full `findings[]` artifact:** written next to `out.json` on the per-Project
    PVC and **staged onto the run branch** via the **extended `collectStageEnvelopes`**
    (`<uid>:<destPrefix>` cumulative map, `push_helpers.go:81`) — the v1.0.7
    git-artifact-store. Never an etcd blob, never a new PVC path (the project's
    locality rule, `project_envelopes_as_artifacts.md`).

### Loop-contract doc-comment discipline (LOOP-02)
- **D-06 (Five-element test in the type doc-comments):** `LoopPolicy`/`LoopStatus`
  type-level godoc explicitly names the five elements (goal/spec · mutable candidate ·
  evaluator/environment feedback · repeat policy · bounded exit/escalation) so a future
  construct missing an element is provably a **pipeline stage, not a loop**. Fields are
  **minimal for the two v1.0.9 consumers** (Task loop + plan-check) — grow per loop,
  never a speculative superset. This is the LOOP-02 guardrail expressed as compiled-in
  documentation, not a runtime check.

### Claude's Discretion
- Exact Go struct field ordering, JSON tag spellings, and kubebuilder validation
  markers (`+kubebuilder:validation:*`) on `LoopPolicy`/`LoopStatus`.
- Whether `GateDecision`/`Finding` live in one `pkg/dispatch/verdict.go` or split;
  the exact golden-fixture path and test harness shape (Go table test vs. shared
  `testdata/`).
- The precise `VerifyContext` field names and the evidence-packet pointer
  representation (PVC-relative path vs. structured ref) — within the minimal-fields
  decision (D-03).
- Whether the fail-closed classifier is a free function or a method on a parsed
  wrapper — within D-04's both-languages + BLOCKED-terminal decision.

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### Requirements & roadmap (scope authority)
- `.planning/REQUIREMENTS.md` — **LOOP-01** (shared types, no generic Loop controller), **LOOP-02** (five-element test), **LOOP-03** (iteration history in traces/artifacts, `.status` = current-iteration summary only), **EVAL-03** (one `gate_decision` schema, matched Go+Pydantic pair, fail-closed, regression test), **EVAL-05** (size×locality findings persistence via `collectStageEnvelopes`).
- `.planning/ROADMAP.md` §"Phase 49" — the goal + 5 success criteria this CONTEXT locks against.

### Milestone framing & binding constraints (the reframe — load-bearing)
- `.planning/PROJECT.md` "Current Milestone: v1.0.9 — Slack Tide" — the Task-loop reframe (verification closes a loop, not a gate), the common-loop-contract feature, "five elements or it's a pipeline stage."
- `.planning/STATE.md` "Accumulated Context › Decisions" — the v1.0.9 binding constraints list (shared contract not generic controller; iteration history never in etcd; fail-closed; findings persistence = ≤4 KB status summary + full artifact on run branch).
- `.planning/notes/five-loop-model.md` — the organizing frame the Task loop + shared contract plug into (Execution/Task/Product/System/Oversight); the source of the five-element loop test.
- `.planning/notes/langgraph-successor-runtime-strategy.md` — the pluggable-runtime seam is `pkg/dispatch.Subagent` + envelope; verify BLOCKED = new halt class (Phase 50 context).

### Research — the pre-decided type shapes (with the reframe caveat)
- `.planning/research/ARCHITECTURE.md` **§Q4** (`VerifyContext` struct, pointer+omitempty, 4th `Role="verifier"`, lines 135–156) and **§Q5** (findings size×locality: `GateDecision`+counts on `TerminationStub`; full findings on run branch; small `.status` summary, lines 158–169; the file-layout at 242–243). **⚠ CAVEAT (Phase-48 CONTEXT):** ARCHITECTURE predates the 2026-07-18 single-loop reframe — its `Stage`/`VerifyResult`/three-template config surface is **superseded**; the *struct shapes* (VerifyContext pointer, GateDecision-on-stub, findings-on-branch) survive, the *per-stage framing* does not.
- `.planning/research/SUMMARY.md` — cross-cutting synthesis.
- `.planning/phases/48-langgraph-evaluator-image-credproxy-tls-spike/48-CONTEXT.md` — the Phase-48/49 hand-off boundary (D-03 envelope transport, the verifier's independent Pydantic envelope re-impl) and the explicit "→ Phase 49" deferrals (VerifyContext, gate_decision, findings persistence).

### The seams these types attach to (source of truth — read before coding)
- `pkg/dispatch/envelope.go` — `EnvelopeIn` (:45), the `*DispatchMeta`/`*Dev` pointer+omitempty pattern (`:146`, `:152`) `VerifyContext` mirrors; `EnvelopeOut` (:170) where `GateDecision` attaches; `TerminationStub` (:394, `<4KB` invariant) + `NewTerminationStub`; `APIVersionV1Alpha1` (:30); `ValidateAPIVersionKind` (:446).
- `api/v1alpha3/shared_types.go` — the condition/reason vocabulary idiom (`ConditionFailureHalt` :318, `FailureProfileType` :382) the future `ConditionVerifyHalt` follows; the home for shared CRD types.
- `api/v1alpha3/task_types.go` — `TaskSpec` (:72) / `TaskStatus` (:147): the Task loop's first `LoopPolicy`/`LoopStatus` consumer.
- `api/v1alpha3/project_types.go` — `ProjectSpec` (:365), `FailureProfile` (:450): the project-level halt-profile precedent `LoopPolicy.Autonomy`/escalation sits beside.
- `internal/controller/push_helpers.go` — `StageEnvelopes` cumulative `<uid>:<destPrefix>` map (:81–88), `--stage-envelopes=` render (:217), superset-guard (:145); `internal/controller/project_controller.go:834` (the `collectStageEnvelopes` call site EVAL-05 extends).
- `internal/controller/artifact_push_test.go` — the `collectStageEnvelopes` cumulative+deterministic test precedent (:67–109) the findings-staging test mirrors.
- `cmd/tide-langgraph-verifier/verifier/envelope.py` — the import-firewalled Pydantic re-impl (`EnvelopeIn` :106, `write_envelope_out` :117, `write_termination_stub` :150 with its truncation loop): where the Python `GateDecision`/`Finding` pair + the `VerifyContext` field + the fail-closed classifier land.
- `evals/README.md` — the per-evaluator-command evaluation contract (exit 0=pass / non-zero=fail; structured result when scores/confidence matter) EVAL-03 references.

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- **`NewTerminationStub` (`pkg/dispatch/envelope.go`)** — already flattens the tiny cross-namespace status subset (`ExitCode`/`Reason`/`Usage`/`HeadSHA`/`ChildCount`) under a `<4KB` test invariant; D-05(a) extends the *same* function + test to carry `GateDecision`+counts. No new mechanism.
- **`*DispatchMeta` / `*Dev` on `EnvelopeIn` (`:146`,`:152`)** — the exact pointer+omitempty idiom `Verify *VerifyContext` copies; grep-unambiguous, serializes nothing when nil.
- **`collectStageEnvelopes` + `StageEnvelopes` (`push_helpers.go`, `project_controller.go:834`)** — the v1.0.7 git-artifact-store staging path; D-05(c) adds the findings artifact to the cumulative `<uid>:<destPrefix>` map rather than inventing a persistence path.
- **`verifier/envelope.py`** — the established hand-re-implemented Pydantic mirror under strict version-equality; the Go+Pydantic `GateDecision`/`Finding` pair (D-02) extends this proven cross-language parity discipline.
- **`ConditionFailureHalt` + `FailureProfileType` vocabulary (`shared_types.go`)** — the naming/doc-comment template the loop-contract types and (Phase-50) `ConditionVerifyHalt` follow.

### Established Patterns
- **Strict `apiVersion`/`kind` equality first (`ValidateAPIVersionKind`)** — every envelope field addition (incl. `VerifyContext`) rides the existing version discriminator; a Go+Pydantic pair must both validate identically.
- **Size×locality rule (`project_envelopes_as_artifacts.md`, `envelope.go:379–393`):** tiny→termination message, small cross-ns→`.status`, small-medium same-ns→PVC/git-artifact-store; findings follow the exact split planning artifacts already use.
- **Import firewall (`pkg/dispatch/doc.go`, `make verify-dispatch-imports`)** — the Python image cannot import the Go types, which is *why* the schema is a matched hand-authored pair, not a shared source.
- **Sole served+storage version = `api/v1alpha3`** (Phase 40) — new CRD-embedded types go here; `controller-gen` deepcopy generation is the gate.

### Integration Points
- **`api/v1alpha3/` (new `loop_types.go`)** — `LoopPolicy`/`LoopStatus`; deepcopy regenerated via `make generate`.
- **`pkg/dispatch/` (new `verdict.go`)** — Go `GateDecision`/`Finding` + fail-closed classifier; `EnvelopeIn.Verify` field + `TerminationStub` verdict fields in `envelope.go`.
- **`cmd/tide-langgraph-verifier/verifier/`** — Pydantic `GateDecision`/`Finding`, the `VerifyContext` field on the Python `EnvelopeIn`, the mirrored fail-closed classifier + regression tests; the shared golden-JSON fixture.
- **`internal/controller/push_helpers.go` + `project_controller.go`** — the `collectStageEnvelopes` extension for the findings artifact (schema/plumbing only; the dispatch that *produces* findings is Phase 51).

</code_context>

<specifics>
## Specific Ideas

- **The verdict terminal set is exactly `APPROVED | REPAIRABLE | BLOCKED`** — `REPAIRABLE` is the signal that drives a *fresh repair attempt* (Task loop core); `BLOCKED` is the escalation/halt terminal and the fail-closed default; `APPROVED` is never reached by an unparseable verdict.
- **`Finding` fields are exactly `dimension / severity / confidence / evidence / suggested_fix`** (+ the top-level `summary`) — coverage-not-conservatism: every deviation is tagged, and *policy* (Phase 50/51) decides what blocks, not the finder.
- **Keep `LoopStatus` etcd-safe by construction** — the size test (Success Criterion #5) is the structural guarantee that iteration history never accumulates in `.status`; history lives in traces/artifacts (LOOP-03).

</specifics>

<deferred>
## Deferred Ideas

- **`ConditionVerifyHalt` + `setVerifyHaltIfNeeded` + resume time-fence + dispatch-gate wiring** → **Phase 50** (the halt-condition/reconciler logic built on these locked types).
- **`TaskReconciler` verifier dispatch, concurrency-gate accounting, `LoopPolicy.BudgetCents` reservation, `onExhaustion: requireApproval`** → **Phase 51**.
- **`role="verifier"` orchestrator-side Go prompt templates + coverage-not-conservatism prompt split** → **Phase 51** (EVAL-04).
- **`"langgraph"` vendor sentinel + `SelfInstruments` registration + `EVALUATOR`-kind span emission** → **Phase 51** (OBS-03).

### Reviewed Todos (not folded)
The `--auto` ≥0.4 auto-fold default was **overridden by the scope guardrail** — this
is a schema/contract-definition phase, and every match is a keyword false-positive
against reconciler/deferred work, not schema work:

- **`2026-07-12-project-dispatch-missing-failurehalt-gate`** (score 0.6) — `ProjectReconciler`'s planner-dispatch chain missing `checkFailureHalt`. This is **halt-gate reconciler wiring** (the same tier as `ConditionVerifyHalt`) → belongs with **Phase 50**, not schema definition.
- **`2026-07-12-task-dispatch-gate-order-divergence`** (score 0.6) — Task's dispatch-holds chain checks Import in a divergent position vs. the planner tier. **Dispatch-gate ordering** → **Phase 50/51** reconciler work, not this phase.
- **`cache-f1-direct-sdk-cross-pod-caching`** (score 0.6) — explicitly **deferred to vNext** (STATE.md Pending Todos); unrelated to the loop/verdict schema.
- **`2026-07-03-signed-commits-verified-badge`** (score 0.4) — GPG commit signing; **deferred by choice** since v1.0.7 (SIGN-02/03/04); no schema overlap.

</deferred>

---

*Phase: 49-common-loop-contract-verdict-envelope-persistence-schema*
*Context gathered: 2026-07-18*
