# Phase 49: Common Loop Contract + Verdict/Envelope/Persistence Schema - Research

**Researched:** 2026-07-18
**Domain:** Kubernetes CRD shared-type schema design (kubebuilder/controller-gen) + Go↔Pydantic wire-contract parity + git-artifact-store staging-pipeline plumbing
**Confidence:** HIGH — every recommendation below is grounded in a real file:line read from the current tree, not training-data conventions. The one genuine open design gap (findings-artifact staging glob) is flagged explicitly with a recommendation, not asserted as settled.

## Summary

Phase 49 is pure schema/contract definition: it adds a new `api/v1alpha3/loop_types.go` (LoopPolicy/LoopStatus, unreferenced by any Kind yet), a new `pkg/dispatch/verdict.go` (GateDecision/Finding/fail-closed classifier), a `VerifyContext` pointer field on `EnvelopeIn`, small bounded additions to `TerminationStub`, and a Go+Pydantic golden-fixture round-trip test. Every one of these lands on an **existing, well-established idiom** already used three-to-five times elsewhere in this codebase — pointer+omitempty envelope fields (`DispatchMeta`/`Dev`), package-level deepcopy generation (no per-type marker needed), tiny-cross-namespace-summary structs (`TerminationStub`), and hand-authored Go/Python parity under an import firewall (`verifier/envelope.py` already does this for `EnvelopeIn`/`EnvelopeOut`). There is no new library, no new codegen tool, and no new CI job to invent.

**The one real trap in this phase is findings-artifact staging (Success Criterion #4).** `collectStageEnvelopes` (`internal/controller/artifact_push.go:84`) today lists only Milestone/Phase/Plan/Project — never Task — and the actual copy logic, `stageEnvelopeArtifacts` (`cmd/tide-push/main.go:1134`), **hard-fails if `glob(srcDir/*.md)` is empty** (`main.go:1175-1183`, `"a planner-completed level must have at least one [*.md]"`). A Task's envelope dir never contains a `*.md` — it contains `in.json`/`out.json` and (once this phase lands) a `findings.json`. Naively adding a Task entry to the cumulative map today would make `tide-push` fail loudly on every boundary push once ANY Task exists. Phase 49 must widen `stageEnvelopeArtifacts`'s glob to be **parameterizable per entry** (or add a second glob) — this is schema-adjacent plumbing the phase is explicitly scoped to do (CONTEXT.md code_context: "the `collectStageEnvelopes` extension for the findings artifact (schema/plumbing only; the dispatch that *produces* findings is Phase 51)"), but it is easy to under-scope because the actual Task-listing + dispatch-time findings.json production is correctly deferred to Phase 51.

**Primary recommendation:** Treat this phase as four independent, narrowly-scoped edits — (1) `api/v1alpha3/loop_types.go` standalone types, (2) `pkg/dispatch/verdict.go` + `EnvelopeIn.Verify` + `TerminationStub` additions, (3) the Go+Pydantic golden-fixture parity harness, (4) the `stageEnvelopeArtifacts` glob generalization — and do NOT embed `LoopPolicy`/`LoopStatus` into `TaskSpec`/`TaskStatus` this phase (that's Phase 51's TASK-01, confirmed by the roadmap's explicit phase split).

## Architectural Responsibility Map

TIDE is a K8s operator, not a web app — the five generic tiers don't map cleanly. Substituting TIDE's own dispatch-seam tiers:

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|-------------|----------------|-----------|
| `LoopPolicy`/`LoopStatus` shape (LOOP-01/02/03) | CRD Schema (`api/v1alpha3`) | — | Embeddable shared types belong where `controller-gen` produces DeepCopy + where domain CRDs (Task, later Plan/Phase/Milestone) will embed them; the schema has zero runtime behavior of its own |
| `gate_decision`/`Finding` verdict schema (EVAL-03) | Dispatch/Envelope Seam (`pkg/dispatch`) | Evaluator Runtime (`cmd/tide-langgraph-verifier`) | The verdict is a wire-format document that crosses the file-envelope seam between the K8s controller and an out-of-tree subagent image — not a K8s API object (D-01 explicitly rejects `api/v1alpha3` placement to avoid dragging CRD-machinery imports into the dispatch seam) |
| `VerifyContext` on `EnvelopeIn` (LOOP/EVAL) | Dispatch/Envelope Seam (`pkg/dispatch`) | Evaluator Runtime (Python re-impl) | Mirrors the existing `DispatchMeta`/`Dev` pointer-field pattern exactly; consumed by the (Phase 51) verifier image, produced by the (Phase 51) TaskReconciler — this phase only defines the shape |
| Fail-closed classifier (EVAL-03) | Dispatch/Envelope Seam (both languages) | — | Must exist identically in Go (`pkg/dispatch`) and Python (`verifier/`) since the two runtimes never share code across the import firewall |
| Findings size×locality persistence (EVAL-05) | Git Artifact Store (`internal/controller/artifact_push.go` + `cmd/tide-push`) | Dispatch/Envelope Seam (`TerminationStub`) | Three tiers per the project's own locality rule: tiny → `TerminationStub`; small → future `LoopStatus.LastEvaluation` (not wired to a CRD this phase); full → git-artifact-store via `collectStageEnvelopes`/`stageEnvelopeArtifacts` |

<user_constraints>
## User Constraints (from CONTEXT.md)

### Locked Decisions

- **D-01 (Two homes for two contracts):** `LoopPolicy`/`LoopStatus` → new `api/v1alpha3/loop_types.go`, sole served+storage version (`v1alpha3`, Phase 40), so `controller-gen` produces DeepCopy and the types are embeddable in domain CRD `Spec`/`Status`. `GateDecision`/`Finding` → `pkg/dispatch/verdict.go` (new file, alongside `EnvelopeOut`) — NOT a CRD type, because it round-trips through the file-envelope seam, not K8s object storage. `LoopStatus.LastEvaluation` embeds a **bounded projection** of the verdict (decision + counts), never the full `Finding` array. Condition/reason vocabulary stays in `api/v1alpha3/shared_types.go` (its established home) — the actual `ConditionVerifyHalt` constant is Phase 50's to add, not this phase's. *Rejected: putting `GateDecision` in `api/v1alpha3` — drags CRD-machinery imports into the dispatch seam.*
- **D-02 (Hand-authored pair + shared golden-JSON round-trip test):** Go `GateDecision`/`Finding` (`pkg/dispatch`) and Pydantic pair (`cmd/tide-langgraph-verifier/verifier/`) are hand-authored in parallel — no codegen — kept honest by a shared golden JSON fixture exercised by a cross-language round-trip regression test: Go marshals the canonical verdict → committed fixture; Python validates the same fixture and re-emits it; Go unmarshals the Python output byte-equivalently. Mirrors the already-established `verifier/envelope.py` pattern (Python image is import-firewalled from Go — cannot share types). Field JSON tags/aliases are the contract; the fixture is the proof.
- **D-03 (Minimal pointer struct, 4th Role):** `Verify *VerifyContext \`json:"verify,omitempty"\`` on `EnvelopeIn`, mirroring `*DispatchMeta`/`*Dev` (`envelope.go:146,152`) — non-verify dispatches serialize nothing. A fourth `Role` value `"verifier"` joins `"planner"`/`"executor"`. `VerifyContext` carries only: the planner-authored pass-criterion command(s), `requiredArtifacts`, an `evaluatorRef`, and a pointer to the compact evidence packet a repair attempt receives (original spec + evidence, NOT the prior agent's full context). **Under the loop reframe the pre-2026-07-18 three-`Stage` field is dropped** — keep `VerifyContext` minimal and grow it per consumer; do not bake the superseded per-stage config surface into the locked schema.
- **D-04 (Shared classifier, unparseable/empty/partial → BLOCKED):** A classifier helper mirrored in both languages maps any empty/missing-`verdict`-field/malformed/partially-validated `gate_decision` to `BLOCKED` — never `APPROVED`. Regression test in each language covers three named shapes: empty-JSON, missing-verdict-field, malformed.
- **D-04b (Determinism dominates — captured, enforced downstream):** The `GateDecision` schema keeps the machine verdict and the judge verdict distinguishable so Phase 51's handler can let the deterministic signal win. Dominance logic is Phase 51; the schema affordance is here.
- **D-05 (Three tiers, no etcd blob, no new PVC path):**
  - (a) `TerminationStub` (≤4KB hard cap, `envelope.go:394`): add a bounded `GateDecision` (verdict string) + `FindingsCount`/`HighSeverityCount` counts, extending `NewTerminationStub` the same way it already flattens `ExitCode`/`Reason`/`Usage`/`ChildCount`. `TestNewTerminationStub_StaysSmall` + the Python `write_termination_stub` truncation loop extended to cover the new fields.
  - (b) per-CRD status: `LoopStatus.LastEvaluation` carries only the current-iteration verdict summary (decision + counts + top-severity + completedAt) — not `findings[]`, not accumulating history. Subsumes ARCHITECTURE Q5's ad-hoc `VerifyResult` into the shared `LoopStatus` contract. Size test proves this (Success Criterion #5).
  - (c) full `findings[]` artifact: written next to `out.json` on the per-Project PVC, staged onto the run branch via the extended `collectStageEnvelopes` (`<uid>:<destPrefix>` cumulative map, `push_helpers.go:81`). Never an etcd blob, never a new PVC path.
- **D-06 (Five-element test in doc-comments):** `LoopPolicy`/`LoopStatus` type-level godoc names the five elements (goal/spec · mutable candidate · evaluator/environment feedback · repeat policy · bounded exit/escalation) so a future construct missing one is provably a pipeline stage, not a loop. Fields minimal for the two v1.0.9 consumers (Task loop + plan-check); grow per loop, never a speculative superset.

### Claude's Discretion

- Exact Go struct field ordering, JSON tag spellings, and `+kubebuilder:validation:*` markers on `LoopPolicy`/`LoopStatus`.
- Whether `GateDecision`/`Finding` live in one `pkg/dispatch/verdict.go` or split; the exact golden-fixture path and test harness shape (Go table test vs. shared `testdata/`).
- The precise `VerifyContext` field names and the evidence-packet pointer representation (PVC-relative path vs. structured ref) — within the minimal-fields decision (D-03).
- Whether the fail-closed classifier is a free function or a method on a parsed wrapper — within D-04's both-languages + BLOCKED-terminal decision.

### Deferred Ideas (OUT OF SCOPE)

- `ConditionVerifyHalt` + `setVerifyHaltIfNeeded` + resume time-fence + dispatch-gate wiring → **Phase 50**.
- `TaskReconciler` verifier dispatch, concurrency-gate accounting, `LoopPolicy.BudgetCents` reservation, `onExhaustion: requireApproval` → **Phase 51**.
- `role="verifier"` orchestrator-side Go prompt templates + coverage-not-conservatism prompt split → **Phase 51** (EVAL-04).
- The `"langgraph"` vendor sentinel + `SelfInstruments` registration + `EVALUATOR`-kind span → **Phase 51** (OBS-03).
- **Also confirmed out of scope by this research** (not itself listed in CONTEXT.md's deferred table, but implied by the phase boundary and the roadmap's TASK-01 assignment to Phase 51): embedding `LoopPolicy`/`LoopStatus` INTO `TaskSpec`/`TaskStatus`. Phase 49 defines the types standalone; Phase 51's TASK-01 is what adds a verification-contract field to `TaskSpec`. See Open Questions.
</user_constraints>

<phase_requirements>
## Phase Requirements

| ID | Description | Research Support |
|----|-------------|------------------|
| LOOP-01 | `LoopPolicy`/`LoopStatus` exist as shared API types embedded in domain CRDs; never a generic `Loop` controller | `api/v1alpha3/loop_types.go` design (Architecture Patterns §1); package-level `+kubebuilder:object:generate=true` marker means no per-type marker needed (Code Examples §1); field-type recommendations grounded in `Caps`/`BudgetConfig`/`GatePolicy` precedent |
| LOOP-02 | A construct is a loop only with all five elements; anything missing one is a pipeline stage | D-06 doc-comment discipline; exact five-element wording sourced from `notes/five-loop-model.md:43` |
| LOOP-03 | Iteration history lives in traces/artifacts; `.status` `LoopStatus` carries only current-iteration summary + exit reason | Size-test design (Common Pitfalls §"LoopStatus history creep"); `TestProjectSpecV1alpha3`-style negative-field-assertion precedent (`api/v1alpha3/schema_test.go:38`) |
| EVAL-03 | One `gate_decision` schema (`APPROVED\|REPAIRABLE\|BLOCKED` + `findings[]`), matched Go+Pydantic pair, fail-closed, regression test | `pkg/dispatch/verdict.go` design (Architecture Patterns §2); fail-closed classifier design (Architecture Patterns §4, Code Examples §4); golden-fixture parity harness (Architecture Patterns §3) |
| EVAL-05 | Findings persist under size×locality — ≤4KB `TerminationStub` summary, small per-CRD status summary, full findings artifact staged via `collectStageEnvelopes` | `TerminationStub` extension (Code Examples §2); the `stageEnvelopeArtifacts` glob-generalization trap (Summary, Common Pitfalls §1) — the single highest-risk item in this phase |
</phase_requirements>

## Project Constraints (from CLAUDE.md)

- **CRD `.status` only for v1** — no external DB, no SQLite, per-object size well under etcd's 1.5MiB limit. `LoopStatus` must obey this directly; it is the entire point of Success Criterion #5's size test.
- **Sole served+storage version is `api/v1alpha3`** (Phase 40 CRANK-01) — any new CRD-embeddable type goes here, not a new `v1alpha4` or a parallel package.
- **`make verify-dispatch-imports` (SUB-01/DAG-05 mirror)** — `pkg/dispatch` (where `verdict.go` lands) must not import `sigs.k8s.io/controller-runtime/*`, `github.com/anthropics/*`, or any `internal/*` package. It IS permitted `k8s.io/apimachinery/pkg/runtime` (already used by `ChildCRDSpec.Spec`). `GateDecision`/`Finding`/the classifier need only `encoding/json`-compatible struct tags and stdlib — this constraint is automatically satisfied by construction, not something to actively verify.
- **Vocabulary conventions (water/tide metaphor)** — "Prefer extending the metaphor naturally... If a name doesn't fit, prefer plain prose." `LoopPolicy`/`LoopStatus`/`GateDecision`/`Finding`/`VerifyContext` are correctly plain, domain-accurate prose (per `notes/five-loop-model.md`'s own naming) — do NOT rename these into tide-metaphor terms (no "Tidepool Verdict" or similar). This phase's naming is already CLAUDE.md-compliant; flag any drift toward forced-metaphor naming as a regression.
- **"Don't vendor GSD Markdown. Re-implement planner/executor prompts as compiled-in Go templates."** — not triggered by this phase (no prompt templates are authored here; that's Phase 51 EVAL-04), but the `VerifyContext.GateCommand`-equivalent field name should stay consistent with what Phase 51's template work will expect (see `research/ARCHITECTURE.md:146`'s `GateCommand` field name — carry that name forward unless a better one is found, to avoid Phase 51 churn).
- **GSD Workflow Enforcement** — this research and the phase it feeds route through `/gsd:plan-phase` as required; no direct edits happen from this research pass.

## Standard Stack

No new libraries. Every dependency this phase touches is already pinned:

### Core (already present, reused as-is)
| Library | Version | Purpose | Why Standard |
|---------|---------|---------|--------------|
| `k8s.io/apimachinery/pkg/apis/meta/v1` | (pinned via controller-runtime v0.24.x, per PROJECT.md stack table) | `metav1.Condition`, `metav1.Duration`, `metav1.Time` for `LoopStatus`/`LoopPolicy` fields | Already imported by every `api/v1alpha3/*_types.go` file (`project_types.go:20`, `task_types.go:20`) |
| `sigs.k8s.io/controller-tools` (`controller-gen`) | pinned via `Makefile`'s `CONTROLLER_GEN` var | DeepCopy generation for `loop_types.go` via `make generate` | Already the sole codegen tool for `api/v1alpha3`; package-level `+kubebuilder:object:generate=true` (`groupversion_info.go:18`) covers new files automatically — **no new marker or tool needed** [VERIFIED: read `api/v1alpha3/groupversion_info.go:18` and confirmed `Caps`/`Gates` get DeepCopy from the same package-level marker, `zz_generated.deepcopy.go:87-107`] |
| `pydantic` 2.13.4 (transitive, already pinned per `research/STACK.md:20`) | 2.13.4 | `GateDecision`/`Finding` Pydantic models | Already a transitive dependency of `langchain`/`langgraph` per Phase 48's pin table; this phase adds no new pin |
| stdlib `encoding/json`, `dataclasses` (Python) | n/a | Struct tags / dataclass fields are the wire contract | `pkg/dispatch` and `verifier/envelope.py` already use these exclusively — no serialization library needed |

### Package Legitimacy Audit

**No new external packages this phase.** `LoopPolicy`/`LoopStatus`/`GateDecision`/`Finding`/`VerifyContext` are hand-authored Go structs and (on the Python side) either stdlib `dataclasses` or `pydantic.BaseModel` — `pydantic` is already pinned and slopcheck-verifiable from Phase 48's own audit trail (Phase 48 STATE.md note: "pytest==9.1.1 slopchecked [OK]"). Since this phase installs nothing new, the Package Legitimacy Gate protocol is not triggered. If the plan discovers a need for a new package during implementation (e.g., a JSON-schema validator), re-run the gate at that point — do not skip it retroactively.

**Packages removed due to slopcheck [SLOP] verdict:** none (no packages evaluated — none proposed).
**Packages flagged as suspicious [SUS]:** none.

## Architecture Patterns

### System Architecture Diagram

```
                    ORCHESTRATOR (Go, in-cluster)                 EVALUATOR IMAGE (Python, out-of-tree)
                    ────────────────────────────                  ─────────────────────────────────────
api/v1alpha3/loop_types.go                                        (no Python analog — CRD types never
  LoopPolicy{MaxIterations,MaxDuration,                             cross the seam; only their bounded
    BudgetCents,Autonomy,EvaluatorRef,                              projection, EvaluationSummary, would
    EscalationPolicy}                                               ever be read by anything outside Go —
  LoopStatus{Iteration,ParentRunID,                                 and even that is Phase 51's job to wire)
    LastEvaluation *EvaluationSummary,
    ExitReason,CostCents,Conditions}
        │
        │ (NOT embedded into any Kind's Spec/Status
        │  this phase — Phase 51's TASK-01 does that)
        ▼
  [standalone, deepcopy-generated, unit-tested types
   sitting in the package, ready for Phase 51 to embed]


pkg/dispatch/envelope.go                                          verifier/envelope.py
  EnvelopeIn{                                                       EnvelopeIn (dataclass)
    ...,                                                              + Verify: VerifyContext | None
    Verify *VerifyContext `json:"verify,omitempty"` ──── file-envelope ──►  (typed nested field,
  }                                                          seam           mirrors provider_vendor/
                                                          (in.json)         provider_model extraction)
        │
        │ dispatch (Phase 51, NOT this phase)
        ▼
pkg/dispatch/verdict.go                                           verifier/verdict.py (NEW module)
  type Verdict string                                                class Finding(BaseModel): ...
    VerdictApproved / VerdictRepairable / VerdictBlocked              class GateDecision(BaseModel):
  type Finding struct{...}                                              verdict: Literal[...]
  type GateDecision struct{...}                 ◄──── file-envelope ──►    summary: str
  func ClassifyVerdict(raw []byte) Verdict           seam                  findings: list[Finding]
    (fail-closed: empty/malformed/missing → BLOCKED)  (out.json)         def classify_verdict(raw) -> Verdict
        │                                                                   (fail-closed, mirrors Go)
        │ bounded projection
        ▼
pkg/dispatch/envelope.go: TerminationStub
  + GateDecision string (verdict only)                            verifier/envelope.py: write_termination_stub
  + FindingsCount int                            ◄──── termination ──►    (extended signature: gate_decision,
  + HighSeverityCount int                             message              findings_count, high_severity_count)
  (≤4KB hard cap, unchanged mechanism)                (/dev/termination-log)


internal/controller/artifact_push.go                               cmd/tide-push/main.go
  collectStageEnvelopes()                                            stageEnvelopeArtifacts()
    lists Milestone/Phase/Plan/Project only        ── generalize ──►   glob(srcDir/*.md) hard-fails
    (Task NOT added this phase — Phase 51's job)        (this phase)   if empty — MUST become
                                                                        parameterizable per DestPrefix
                                                                        kind before ANY Task entry is
                                                                        ever added to the map (else
                                                                        Phase 51 ships a push-breaking
                                                                        regression on day one)
```

### Recommended Project Structure

```
api/v1alpha3/
├── loop_types.go                 # NEW — LoopPolicy, LoopStatus, EvaluationSummary,
│                                  #   AutonomyLevel/ExitReason/EscalationPolicy enums.
│                                  #   Standalone; NOT referenced by TaskSpec/TaskStatus yet.
├── loop_types_test.go             # NEW — five-element doc-comment presence isn't unit-
│                                  #   testable directly, but: JSON round-trip, size test,
│                                  #   negative "no history slice" field-absence test
│                                  #   (mirrors schema_test.go's TestProjectSpecV1alpha3 style)

pkg/dispatch/
├── envelope.go                   # + VerifyContext struct (mirrors DispatchMeta/Dev at
│                                  #   :146/:152) + Verify *VerifyContext field on EnvelopeIn
│                                  #   + GateDecision/FindingsCount/HighSeverityCount fields
│                                  #   on TerminationStub + NewTerminationStub extension
├── verdict.go                     # NEW — Verdict type + consts, Finding, GateDecision,
│                                  #   ClassifyVerdict (fail-closed classifier)
├── verdict_test.go                # NEW — golden-fixture round trip + 3 fail-closed
│                                  #   regression shapes (empty/missing-field/malformed)
├── testdata/
│   └── gate_decision_golden.json # NEW — single source of truth, read by BOTH Go
│                                  #   (os.ReadFile, relative to package dir) and Python
│                                  #   (repo-root-relative, walked up from __file__)

cmd/tide-langgraph-verifier/verifier/
├── verdict.py                     # NEW — Finding/GateDecision as pydantic.BaseModel
│                                  #   (NOT dataclasses — LangChain structured-output
│                                  #   compatibility, Phase 51's concern but shape it now),
│                                  #   classify_verdict() fail-closed classifier
├── envelope.py                    # + VerifyContext dataclass/pydantic field on EnvelopeIn
│                                  #   + write_termination_stub() gains gate_decision/
│                                  #   findings_count/high_severity_count params
├── tests/
│   ├── test_verdict.py           # NEW — golden-fixture round trip (reads the SAME
│   │                              #   testdata/gate_decision_golden.json Go wrote) +
│   │                              #   3 fail-closed regression shapes
│   └── conftest.py                # + a verify_context_dict / gate_decision_dict factory,
│                                  #   mirroring the existing envelope_in_dict fixture

cmd/tide-push/main.go
├── stageEnvelopeArtifacts()       # generalize the hard-coded `*.md` glob (main.go:1169)
│                                  #   to be parameterizable — see Common Pitfalls §1 for
│                                  #   the exact mechanism recommendation

internal/controller/
├── artifact_push.go               # collectStageEnvelopes() — NO Task-listing added this
│                                  #   phase (Phase 51's job once findings.json exists to
│                                  #   collect); the plumbing this phase touches is
│                                  #   stageEnvelopeArtifacts' glob generalization only
```

### Pattern 1: Package-level deepcopy generation — no per-type marker needed

**What:** `api/v1alpha3/groupversion_info.go:18` carries `// +kubebuilder:object:generate=true` at the PACKAGE level (`// +kubebuilder:object:generate=true` immediately above `package v1alpha3`). This means every exported struct anywhere in the package gets a `DeepCopy`/`DeepCopyInto` pair from `make generate`, whether or not any CRD `Kind` actually references it.

**When to use:** Always, for this phase — `LoopPolicy`/`LoopStatus`/`EvaluationSummary` need zero additional kubebuilder markers to get DeepCopy. Verified directly: `Caps` (`caps.go`-equivalent, actually declared in `task_types.go:31`) and `Gates` (`project_types.go:40`) both get `DeepCopyInto`/`DeepCopy` in `zz_generated.deepcopy.go:87-110` with no per-type `+kubebuilder:object:generate` marker on either struct.

**Example:**
```go
// Source: api/v1alpha3/groupversion_info.go:17-20 [VERIFIED: read directly]
// Package v1alpha3 contains API Schema definitions for the tideproject v1alpha3 API group.
// +kubebuilder:object:generate=true
// +groupName=tideproject.k8s
package v1alpha3
```
Because of this, `loop_types.go` needs no special generation marker — just declare the structs and run `make generate` (which runs `controller-gen object:headerFile=... paths="./api/..."`, Makefile:66-70).

**Important corollary — `make manifests` will NOT change this phase.** `make manifests` (Makefile:52-63) generates CRD YAML only for types reachable from a `+kubebuilder:object:root=true` Kind's `Spec`/`Status` fields (`config/crd/bases/*.yaml`). Since `LoopPolicy`/`LoopStatus` are not embedded into any Kind's Spec/Status this phase, `make manifests` produces **zero diff** — don't expect (or require) a CRD YAML change in this phase's plan. Only `make generate` (deepcopy) is exercised.

### Pattern 2: Pointer + omitempty envelope-seam fields (mirrors `DispatchMeta`/`Dev`)

**What:** Optional envelope payload structs are declared as `*T` with `json:"...,omitempty"` so dispatches that don't need them (the common case) serialize nothing.

**When to use:** `VerifyContext` on `EnvelopeIn`.

**Example:**
```go
// Source: pkg/dispatch/envelope.go:146-152 [VERIFIED: read directly] — the EXACT
// pattern VerifyContext copies.
Dispatch *DispatchMeta `json:"dispatch,omitempty"`

// Dev carries test-fixture-only metadata injected by integration tests. ...
// The field is omitted from JSON when nil so production envelopes are not
// polluted with "dev: null".
Dev *Dev `json:"dev,omitempty"`
```
Recommended `VerifyContext` addition (new field on `EnvelopeIn`, declared in `envelope.go` alongside `DispatchMeta`/`Dev` — NOT in the new `verdict.go`, which is reserved for the OUTPUT/verdict schema):
```go
// Verify carries verify-dispatch-specific input data. Populated only when
// Role=="verifier" (D-03); omitted from JSON otherwise, mirroring Dispatch/Dev.
Verify *VerifyContext `json:"verify,omitempty"`

// VerifyContext carries the minimal data a verifier dispatch needs (D-03 —
// the pre-2026-07-18 three-Stage framing is dropped; grow this per consumer,
// never a speculative superset).
type VerifyContext struct {
    // GateCommand is the planner-authored pass-criterion command(s) to run
    // for real (exit code parsed, never self-reported). Field name carried
    // forward from research/ARCHITECTURE.md:146 to avoid Phase 51 template churn.
    GateCommand string `json:"gateCommand,omitempty"`
    // RequiredArtifacts lists workspace-relative paths the verifier confirms exist.
    RequiredArtifacts []string `json:"requiredArtifacts,omitempty"`
    // EvaluatorRef names the evaluator config this dispatch resolves against
    // (plain string, same-namespace name-ref convention — see Pattern 4 below;
    // NOT corev1.LocalObjectReference).
    EvaluatorRef string `json:"evaluatorRef,omitempty"`
    // EvidencePacketPath is the PVC-relative path to the compact evidence
    // packet a repair attempt receives (original spec + evidence, never the
    // prior agent's full context — TASK-02). Empty on a fresh (non-repair) attempt.
    EvidencePacketPath string `json:"evidencePacketPath,omitempty"`
}
```
The Role godoc at `envelope.go:58` ("Role is the planner/executor selector") needs updating to mention the third value, since `Role` is a plain `string` field with no Go enum — [VERIFIED: `grep -n '"planner"\|"executor"'` across `pkg/dispatch/*.go` and `internal/controller/dispatch_helpers.go` shows every use is a bare string literal, never a typed constant]. No Go type change is needed to add `"verifier"` — only the doc comment and the dispatch call sites (Phase 51) need it.

### Pattern 3: Two homes, two types, translated at the boundary (NOT a shared Go import)

**What:** When a CRD-status field needs to summarize a `pkg/dispatch` wire-format value, the codebase's established answer is NOT "import `pkg/dispatch` types into `api/v1alpha3`" — it's "declare an independent, smaller type in `api/v1alpha3` with the same semantic shape, translated by a reconciler function at dispatch/read time."

**When to use:** `LoopStatus.LastEvaluation`'s bounded verdict projection (D-01: "embeds a bounded projection of the verdict (decision + counts), never the full Finding array").

**Example — the EXISTING precedent this mirrors:**
```go
// Source: api/v1alpha3/task_types.go:24-30 [VERIFIED: read directly]
// Design note: api/v1alpha3.Caps and pkg/dispatch.Caps are intentionally two
// separate types that serve different layers — this struct is CEL-validated at
// the CRD admission boundary, while pkg/dispatch.Caps is the Go-only public API
// used by the dispatcher. Plan 09's TaskReconciler.buildEnvelopeIn translates
// one to the other at dispatch time, keeping the CRD schema and the dispatch
// interface decoupled.
type Caps struct { ... }
```
Recommended: `api/v1alpha3/loop_types.go` declares its own `EvaluationSummary` struct with a **locally-scoped** `Decision string` field (optionally `+kubebuilder:validation:Enum=APPROVED;REPAIRABLE;BLOCKED` if CEL/OpenAPI enum validation is desired on the CRD side), NOT `pkg/dispatch.GateDecision` or `pkg/dispatch.Verdict` imported directly. This keeps the CRD schema decoupled from the wire-format package exactly like `Caps`/`Caps` are decoupled today. The value-string vocabulary (`APPROVED`/`REPAIRABLE`/`BLOCKED`) must be KEPT IN SYNC by convention (a translation function, written in Phase 50/51 when `LastEvaluation` actually gets populated) — recommend a same-phase unit test asserting the two vocabularies match (`reflect`-based or a literal string-slice equality check) so future drift is caught at compile/test time, not at runtime.

```go
// EvaluationSummary is the bounded, current-iteration-only projection of a
// verdict onto CRD status (D-01). Never the full findings[] array — LOOP-03.
type EvaluationSummary struct {
    Decision          string       `json:"decision,omitempty"`
    FindingsCount     int32        `json:"findingsCount,omitempty"`
    HighSeverityCount int32        `json:"highSeverityCount,omitempty"`
    CompletedAt       *metav1.Time `json:"completedAt,omitempty"`
}
```

### Pattern 4: Same-namespace name refs are plain strings, never `corev1.LocalObjectReference`

**What:** Every `*Ref` field in `api/v1alpha3` today is a plain `string` naming another same-namespace object — `PhaseRef` (`plan_types.go:27`), `PlanRef` (`task_types.go:76`), `ProjectRef` (`wave_types.go:30`, `milestone_types.go:27`), `MilestoneRef` (`phase_types.go:27`), `CredsSecretRef`/`LeaksConfigRef`/`ProviderSecretRef` (`project_types.go:224,230,406`). [VERIFIED: `grep -n "Ref string\|Ref \`json"` across `api/v1alpha3/*.go` returns 10 matches, zero of which use `corev1.LocalObjectReference` or `corev1.ObjectReference`; a separate grep for those two types across the same directory returns zero matches.]

**When to use:** `LoopPolicy.EvaluatorRef`. Do NOT introduce `corev1.LocalObjectReference` here — it would be the first of its kind in the package and breaks the established convention for zero benefit (nothing in this phase needs cross-Kind ambiguity resolution; that's what `EvaluatorConfig` chart/Project-level resolution, deferred to Phase 51/53, is for — mirrors `SubagentConfig`'s `Levels.{level}.Model` string-based resolution chain at `project_types.go:148-207`).

```go
// EvaluatorRef names the evaluator config this LoopPolicy resolves against
// (same-namespace name ref, mirroring PlanRef/PhaseRef/MilestoneRef — plain
// string, not corev1.LocalObjectReference; resolution chain is Phase 51/53's
// job, same shape as SubagentConfig.Levels).
EvaluatorRef string `json:"evaluatorRef,omitempty"`
```

### Pattern 5: `metav1.Duration`/int64-cents/int32-counter conventions (verified from every existing numeric field)

Verified field-type conventions to reuse verbatim, sourced from actual grep across `api/v1alpha3/*.go`:

| Semantic | Type | Precedent |
|----------|------|-----------|
| Cost in cents (Spec or Status) | `int64` | `BudgetConfig.AbsoluteCapCents` (`project_types.go:99`), `BudgetStatus.CostSpentCents` (`:317`), `pkg/dispatch.Usage.EstimatedCostCents` (`envelope.go:312`) — **every** cents field in the codebase is `int64`, none is `int32` |
| Bounded duration (optional cap) | `*metav1.Duration` | `BudgetConfig.RollingWindowDuration` (`project_types.go:114`) — pointer, not value, so `omitempty` fully elides it |
| Iteration/attempt counter (Spec side) | `int32` | `Caps.Iterations` (`task_types.go:40`), `MaxAttemptsPerTask` (`project_types.go:425`) |
| Iteration/attempt counter (Status side) | `int32` (dominant) — `BoundaryPushStatus.Attempts` (`project_types.go:350`), `GitStatus.LeaseFailureCount` (`:291`); **one outlier exists**: `TaskStatus.Attempt` is a bare `int` (`task_types.go:157`) — treat this as a pre-existing inconsistency, not a pattern to follow; use `int32` for `LoopStatus.Iteration` to match the DOMINANT convention |
| Enum-like policy string | `type X string` + `+kubebuilder:validation:Enum=a;b;c` | `FailureProfileType` (`shared_types.go:384`), `GatePolicy` (`project_types.go:37`) |

Recommended `LoopPolicy`/`LoopStatus` field types, applying the table above:
```go
type LoopPolicy struct {
    // +kubebuilder:validation:Minimum=0
    MaxIterations int32 `json:"maxIterations,omitempty"`
    // +optional
    MaxDuration *metav1.Duration `json:"maxDuration,omitempty"`
    // +kubebuilder:validation:Minimum=0
    BudgetCents int64 `json:"budgetCents,omitempty"`
    // +optional
    Autonomy AutonomyLevel `json:"autonomy,omitempty"`
    // +optional
    EvaluatorRef string `json:"evaluatorRef,omitempty"`
    // +optional
    EscalationPolicy EscalationPolicy `json:"escalationPolicy,omitempty"`
}

type LoopStatus struct {
    // +optional
    Iteration int32 `json:"iteration,omitempty"`
    // +optional
    ParentRunID string `json:"parentRunID,omitempty"`
    // +optional
    LastEvaluation *EvaluationSummary `json:"lastEvaluation,omitempty"`
    // +optional
    ExitReason ExitReason `json:"exitReason,omitempty"`
    // +optional
    CostCents int64 `json:"costCents,omitempty"`
    // +listType=map
    // +listMapKey=type
    // +optional
    Conditions []metav1.Condition `json:"conditions,omitempty"`
}
```

### Anti-Patterns to Avoid

- **Adding a `Summary string` free-text field to `TerminationStub`.** D-05(a) explicitly bounds the stub extension to a verdict ENUM string + two counts — no free text. `TerminationStub` has no truncation logic on the Go side today (`NewTerminationStub` copies `Reason` verbatim with no length cap — only the Python `write_termination_stub` truncates); adding unbounded text to the Go path would silently reintroduce the exact overflow risk the 4KB test guards against, without the truncation loop that protects `Reason`. Keep the new fields bounded-by-construction (enum + ints), not bounded-by-truncation.
- **Embedding `LoopPolicy`/`LoopStatus` into `TaskSpec`/`TaskStatus` this phase.** TASK-01 (Phase 51) owns that. Doing it early creates a live CRD schema (via `make manifests`) with no consumer yet, and risks colliding with however Phase 51 actually shapes `TaskSpec.verification` (which per `docs/templates/minimal-loop-project/tasks/TASK.template.md` and `five-loop-model.md:55-63` may be a DIFFERENT, task-specific struct that only PARTIALLY maps onto `LoopPolicy`, e.g. it has `commands`/`requiredArtifacts`/`evaluator`/`onExhaustion` directly rather than wrapping `LoopPolicy` verbatim).
- **Importing `pkg/dispatch.GateDecision` into `api/v1alpha3`.** Explicitly rejected by D-01. Would also technically work today (no import-firewall rule prevents `api/v1alpha3` → `pkg/dispatch`, only the reverse) but breaks the established `Caps`/`Caps` decoupling precedent for no benefit.
- **A byte-for-byte string comparison as the cross-language round-trip test's success criterion.** Go struct-tag field order and Python dict/Pydantic serialization order are not guaranteed to match key-for-key. D-02's "Go unmarshals the Python output byte-equivalently" means "decodes to an equivalent value," not "produces an identical byte string." Test via decoded-struct equality (`reflect.DeepEqual` / a hand-written field comparison in Go; `==`/`model_dump()` dict comparison in Python), not raw byte comparison.

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| DeepCopy for new CRD-embeddable structs | A hand-written `DeepCopyInto`/`DeepCopy` pair | `make generate` (controller-gen, package-level marker already present) | Every other type in the package gets this for free; hand-rolling risks a shallow-copy bug on the `[]metav1.Condition`/`*EvaluationSummary` pointer fields that controller-gen gets right automatically |
| Verdict wire-format parity between Go and Python | A schema-generation tool (e.g., generate Pydantic from Go structs, or vice versa) | The existing hand-authored-pair + golden-fixture-test discipline (D-02), identical to how `verifier/envelope.py` already re-implements `EnvelopeIn`/`EnvelopeOut` by hand | The import firewall (`pkg/dispatch/doc.go:44-55`) makes any codegen bridge either violate the firewall or require a THIRD build step (an IDL); the codebase has already decided (and proven, via `verifier/envelope.py`'s existing tests) that hand-authored-plus-fixture-test is sufficient at this schema's size |
| CRD-status size bounding | A generic "shrink this struct" reflection-based truncator | Structural discipline: `LastEvaluation` is a single bounded struct (not a slice), enforced by a compile-time struct-literal test mirroring `TestTerminationStub_NoForbiddenFields` (`envelope_test.go:698-716`) | The codebase's existing size-safety pattern is "make the overflow structurally impossible" (a single struct, not a list), not "measure and truncate at runtime" — reserve truncation for genuinely unbounded free text (`Reason`), which `LastEvaluation`/`TerminationStub`'s new fields deliberately are not |

**Key insight:** every "hand-roll" temptation in this phase (a schema generator, a runtime truncator, a shared cross-language base class) has already been explicitly rejected somewhere else in this codebase for a structurally identical problem. Reach for the existing precedent, not a general solution.

## Common Pitfalls

### Pitfall 1: `stageEnvelopeArtifacts`'s hard-coded `*.md` glob will silently break EVAL-05's own plumbing extension

**What goes wrong:** `cmd/tide-push/main.go:1169-1183` [VERIFIED: read directly] globs `srcDir/*.md` and returns exit code `exitGenericFail` with reason `"artifact-stage-failed"` if the glob is empty:
```go
mdMatches, gerr := filepath.Glob(filepath.Join(srcDir, "*.md"))
...
if len(mdMatches) == 0 {
    // A planner-completed level always emits at least one planning *.md;
    // an empty set means the envelope is incomplete — fail loudly (D-03).
    writePushEnvelope(cfg, "", exitGenericFail, "artifact-stage-failed", nil, 0, "")
    ...
    return exitGenericFail
}
```
A Task's envelope dir (`envelopes/<task-uid>/`) never contains a `*.md` — it contains `in.json`/`out.json` and (after this phase) a `findings.json`. If `collectStageEnvelopes` (or a future Phase 51 caller) ever adds a `<taskUID>:task/<name>` entry to the `--stage-envelopes` CSV without first generalizing this glob, EVERY subsequent boundary push fails loudly project-wide the moment any Task exists — a much worse failure than "findings don't stage," because it takes down planning-artifact staging too (the whole cumulative map fails atomically per-entry, `main.go:1141` loop).

**Why it happens:** `stageEnvelopeArtifacts` was written for exactly one artifact shape (planner `*.md` + `children/*.json`) because that's all `collectStageEnvelopes` has ever produced (Milestone/Phase/Plan/Project — all planner levels). EVAL-05's findings artifact is a structurally different shape (a single `findings.json`, no `*.md`, no `children/` subdir) attached to a non-planner level (Task).

**How to avoid:** This phase should generalize `stageEnvelopeArtifacts`'s glob to be parameterizable — the cleanest mechanism given `EnvelopeStage{UID, DestPrefix}` (`main.go:142-145`) already carries a `DestPrefix` whose FIRST path segment is the `kind` (`"milestone"`, `"phase"`, `"plan"`, `"project"` today). Recommend: derive the expected artifact glob from that first segment (`"task"` → `findings.json` only, no `*.md` requirement; everything else → unchanged `*.md`+`children/*.json` behavior), OR add an explicit `Glob string` field to `EnvelopeStage` set by the caller. Either approach keeps this phase's change additive and backward-compatible (existing Milestone/Phase/Plan/Project staging is byte-identical; only a NEW, unused-until-Phase-51 code path is added). **Do not add a Task entry to `collectStageEnvelopes` this phase** — that's still correctly Phase 51's job (nothing produces `findings.json` yet) — but DO make `stageEnvelopeArtifacts` capable of handling one when Phase 51 arrives, since "collectStageEnvelopes extended... schema/plumbing only" is explicitly this phase's stated scope (CONTEXT.md code_context).

**Warning signs:** A Phase 51 plan that adds Task entries to `collectStageEnvelopes` without touching `cmd/tide-push/main.go` at all — that plan will ship a `TestStageEnvelopesEmptyDirFailsLoud`-shaped regression (`cmd/tide-push/main_test.go:1377`) in production the first time a Task's verify iteration completes.

**Phase to address:** THIS phase for the glob-generalization plumbing (explicitly in scope); Phase 51 for actually calling it with real Task UIDs once `findings.json` exists.

### Pitfall 2: Fail-open collapse — the exact incident this milestone exists to prevent, now at schema level

**What goes wrong:** LangChain's `create_agent(..., response_format=GateDecision)` (Phase 51's mechanism, not this phase's) can return a verdict object that's empty, partially validated, or missing the `verdict` field under truncation/refusal/transient failure. If Phase 49's classifier's default branch (or its ABSENCE of a default branch) treats "no clear verdict" as anything other than `BLOCKED`, Phase 51 inherits a silent fail-open exactly like the 2026-07-03 incident this entire milestone exists to fix — arguably worse, since "verify ran and passed" is reported when verify didn't actually run correctly.

**Why it happens:** A classifier written as a simple "if verdict == 'APPROVED' then Approved elseif verdict == 'BLOCKED' then Blocked elseif verdict == 'REPAIRABLE' then Repairable" chain has no explicit `else` — Go's zero-value default for an unrecognized string falls through to whatever the CALLER's zero-value handling does, which is easy to get wrong at a call site far from the classifier itself.

**How to avoid:** Write `ClassifyVerdict` so `BLOCKED` is the return value for the missing/default case, not something the caller has to remember to check for:
```go
// Source: pattern mirrors pkg/dispatch/errors.go's typed-error discipline,
// applied to a fail-closed classifier instead of a validation error.
func ClassifyVerdict(raw json.RawMessage) Verdict {
    var parsed struct {
        Verdict string `json:"verdict"`
    }
    if len(raw) == 0 {
        return VerdictBlocked // empty JSON
    }
    if err := json.Unmarshal(raw, &parsed); err != nil {
        return VerdictBlocked // malformed
    }
    switch Verdict(parsed.Verdict) {
    case VerdictApproved, VerdictRepairable, VerdictBlocked:
        return Verdict(parsed.Verdict)
    default:
        return VerdictBlocked // missing/unrecognized verdict field
    }
}
```
The three regression-test shapes D-04 names map directly onto the three early-return branches above: empty-JSON (`len(raw)==0`), malformed (`json.Unmarshal` error), missing-verdict-field (`parsed.Verdict == ""` falls into `default`). Write the Python `classify_verdict` with the identical three-branch shape so the regression test names line up 1:1 across languages.

**Warning signs:** A classifier whose signature returns `(Verdict, error)` where the CALLER decides what to do with a non-nil error — that shape invites exactly the "did the caller remember to treat error as BLOCKED?" bug this pitfall describes. Prefer a signature that can't express "unknown" as anything other than the safe value.

**Phase to address:** This phase (the classifier's existence + its 3-shape regression test is EVAL-03's explicit deliverable). The classifier's CALL SITE (the reconciler deciding what to do with a `BLOCKED` result) is Phase 50/51.

### Pitfall 3: `LoopStatus` history creep via a well-intentioned "just add one more field" edit in a later phase

**What goes wrong:** LOOP-03 requires `LoopStatus` to carry ONLY the current-iteration summary. A later phase (50/51/52) under time pressure might add `PreviousEvaluations []EvaluationSummary` "just to show the last 3 attempts on the dashboard" — silently turning `.status` into the event database CLAUDE.md/REQUIREMENTS.md explicitly forbid (Out of Scope table: "Iteration histories in CRD `.status`... `LoopStatus` keeps only the current summary").

**Why it happens:** Dashboards want history; `.status` is the easiest place to reach for it because it's already being read by the dashboard's existing CRD-watch machinery, and reaching for the git-artifact-store (staged findings) or the trace tree (OBS-01's `loop.iteration` span attribute) requires wiring a NEW read path.

**How to avoid:** This phase's Success Criterion #5 size test should be written as a STRUCTURAL guard, not just a byte-count assertion — a compile-time struct literal (mirroring `TestTerminationStub_NoForbiddenFields`, `envelope_test.go:698-716`) that lists every `LoopStatus` field explicitly, so a later PR adding a slice/history field fails to COMPILE against the test, not just fails a size assertion that could be quietly raised.
```go
// Compile-time assertion mirroring envelope_test.go:698-716's pattern —
// if a history field is ever added, this literal fails to compile.
_ = v1alpha3.LoopStatus{
    Iteration:      0,
    ParentRunID:    "",
    LastEvaluation: nil,
    ExitReason:     "",
    CostCents:      0,
    Conditions:     nil,
}
```

**Warning signs:** A code review comment like "can we also show the last N attempts here" landing as a `.status` field addition instead of a dashboard query against the git-artifact-store or the trace tree.

**Phase to address:** The structural guard is this phase's job (Success Criterion #5). Enforcement against future violation is the test itself, running in CI forever after.

### Pitfall 4: Golden-fixture path resolution breaks across Go's package-relative and pytest's checkout-relative working directories

**What goes wrong:** `go test` always sets cwd to the package directory (so `pkg/dispatch/testdata/gate_decision_golden.json` is trivially reachable via `os.ReadFile("testdata/gate_decision_golden.json")`), but `make test-langgraph-verifier` (`Makefile:874-879`) `cd`s into `cmd/tide-langgraph-verifier` BEFORE invoking `pytest verifier/tests/` — so a Python test's `__file__`-relative path to the SAME fixture must walk up through `verifier/tests/ → verifier/ → tide-langgraph-verifier/ → cmd/ → <repo-root>` before descending into `pkg/dispatch/testdata/`. A fixed `Path(__file__).parents[3]` is fragile against any future directory rename.

**Why it happens:** The two test runners have structurally different cwd conventions (Go: always package-relative; pytest here: `Makefile`-driven, one level above the test file's natural rootdir), and there is no existing `testdata/` sharing precedent in this repo to copy (grep confirms `pkg/dispatch/` has no `testdata/` directory today — this is the first).

**How to avoid:** Don't hardcode a parent-count. Walk upward from `__file__` until a stable repo-root marker is found (e.g. `go.mod`, which is unambiguous and won't move):
```python
# Source: pattern recommendation — no existing precedent in this repo to cite,
# this is new plumbing this phase introduces.
def _repo_root() -> Path:
    """Walk up from this file until go.mod is found (repo root marker)."""
    p = Path(__file__).resolve()
    for parent in p.parents:
        if (parent / "go.mod").exists():
            return parent
    raise RuntimeError("could not locate repo root (no go.mod found above " + str(p) + ")")

GOLDEN_FIXTURE = _repo_root() / "pkg" / "dispatch" / "testdata" / "gate_decision_golden.json"
```

**Warning signs:** A pytest failure only in CI (different checkout depth/symlink structure than local dev) but not locally, or vice versa — a classic parent-count-fragility symptom.

**Phase to address:** This phase (the golden-fixture test IS this phase's D-02 deliverable).

## Code Examples

### 1. `TerminationStub` extension mirroring the existing `ExitCode`/`Reason`/`Usage`/`ChildCount` flattening

```go
// Source: pkg/dispatch/envelope.go:394-437 [VERIFIED: read directly] — extend
// the EXISTING struct and constructor, do not create a parallel type.
type TerminationStub struct {
    ExitCode int    `json:"exitCode"`
    Reason   string `json:"reason"`
    Usage    Usage  `json:"usage"`
    HeadSHA  string `json:"headSHA,omitempty"`
    ChildCount int  `json:"childCount"`

    // NEW (EVAL-05 / D-05a) — bounded verdict summary. GateDecision is the
    // enum string only (never a free-text summary — see Anti-Patterns).
    // Empty/zero on any non-verify dispatch (executor/planner), matching
    // ChildCount's zero-for-non-planner convention.
    GateDecision      string `json:"gateDecision,omitempty"`
    FindingsCount     int    `json:"findingsCount,omitempty"`
    HighSeverityCount int    `json:"highSeverityCount,omitempty"`
}

func NewTerminationStub(out EnvelopeOut) TerminationStub {
    headSHA := ""
    if out.Git != nil {
        headSHA = out.Git.HeadSHA
    }
    stub := TerminationStub{
        ExitCode:   out.ExitCode,
        Reason:     out.Reason,
        Usage:      out.Usage,
        HeadSHA:    headSHA,
        ChildCount: len(out.ChildCRDs),
    }
    if out.Verdict != nil { // out.Verdict is *GateDecision on EnvelopeOut — Phase 51 populates it
        stub.GateDecision = string(out.Verdict.Verdict)
        stub.FindingsCount = len(out.Verdict.Findings)
        for _, f := range out.Verdict.Findings {
            if f.Severity == "blocker" { // or whatever the locked severity vocabulary ends up being
                stub.HighSeverityCount++
            }
        }
    }
    return stub
}
```
Extend `TestNewTerminationStub_StaysSmall` (`envelope_test.go:674-696`) to also populate a worst-case `GateDecision` (e.g. 50 findings, all high-severity) and re-assert `< 4096` — this proves the COUNTS stay small even under a large findings list (which they must, since only the counts, not the list, are copied).

### 2. Fail-closed classifier regression test shape (both languages, matching 1:1)

```go
// Source: pattern mirrors pkg/dispatch table-test conventions already used
// throughout envelope_test.go (e.g. the assertRoundTripOut helper at :118).
func TestClassifyVerdict_FailsClosed(t *testing.T) {
    cases := []struct {
        name string
        raw  string
        want Verdict
    }{
        {"EmptyJSON", ``, VerdictBlocked},
        {"MissingVerdictField", `{"summary":"looks fine","findings":[]}`, VerdictBlocked},
        {"Malformed", `{not valid json`, VerdictBlocked},
        {"ValidApproved", `{"verdict":"APPROVED","summary":"ok","findings":[]}`, VerdictApproved},
    }
    for _, c := range cases {
        t.Run(c.name, func(t *testing.T) {
            if got := ClassifyVerdict([]byte(c.raw)); got != c.want {
                t.Errorf("ClassifyVerdict(%q) = %q, want %q", c.raw, got, c.want)
            }
        })
    }
}
```
```python
# Source: mirrors verifier/tests/test_envelope.py's existing parametrize
# convention (test_envelope.py:63-66's @pytest.mark.parametrize pattern).
import pytest
from verifier import verdict

@pytest.mark.parametrize(
    "raw,want",
    [
        ("", verdict.Verdict.BLOCKED),
        ('{"summary":"looks fine","findings":[]}', verdict.Verdict.BLOCKED),
        ("{not valid json", verdict.Verdict.BLOCKED),
        ('{"verdict":"APPROVED","summary":"ok","findings":[]}', verdict.Verdict.APPROVED),
    ],
)
def test_classify_verdict_fails_closed(raw, want):
    assert verdict.classify_verdict(raw) == want
```

### 3. Golden-fixture round-trip test (Go side)

```go
// Source: pattern — new (this phase's D-02 deliverable), no direct precedent,
// but the read/marshal shape mirrors envelope_test.go's existing round-trip
// helpers (assertRoundTripOut, :118-127).
func TestGateDecision_GoldenFixtureRoundTrip(t *testing.T) {
    golden, err := os.ReadFile("testdata/gate_decision_golden.json")
    if err != nil {
        t.Fatalf("read golden fixture: %v", err)
    }
    var decoded GateDecision
    if err := json.Unmarshal(golden, &decoded); err != nil {
        t.Fatalf("unmarshal golden fixture: %v", err)
    }
    // Assert against the CANONICAL values the fixture was authored with —
    // do NOT re-marshal and byte-compare (see Anti-Patterns: key order is
    // not guaranteed stable across Go/Python serializers).
    if decoded.Verdict != VerdictRepairable {
        t.Errorf("Verdict = %q, want %q", decoded.Verdict, VerdictRepairable)
    }
    if len(decoded.Findings) == 0 {
        t.Fatal("expected at least one finding in the golden fixture")
    }
}
```

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|-------|---------|---------------|
| A1 | `LoopPolicy.EscalationPolicy`'s exact value vocabulary (`escalate`/`requireApproval`) is inferred from `five-loop-model.md:62` (`onExhaustion: requireApproval`) and TASK-05's prose ("onExhaustion → escalate / requireApproval") — CONTEXT.md locks the FIELD NAME but not its enum values | Architecture Patterns §5 | Low — Claude's Discretion explicitly covers exact validation markers/enum values; if the plan picks different literals, only doc-comments and this table need updating, no structural rework |
| A2 | `LoopStatus.ExitReason`'s value vocabulary is NOT specified anywhere in CONTEXT.md/REQUIREMENTS.md (only the field NAME is required by LOOP-01) — this research does not propose a concrete enum, deliberately, since EXEC-02's terminal-reason vocabulary (`completed\|cap_exceeded\|blocked\|tool_failure\|invalid_output`, Phase 50) is a DIFFERENT loop layer (Execution, not the loop this ExitReason belongs to) and should not be copy-pasted without the planner deciding whether Task-loop exit reasons are the same set | Architecture Patterns §5 | Medium — if the plan doesn't explicitly decide this vocabulary, `ExitReason ExitReason` risks becoming a bare unvalidated string with no kubebuilder Enum marker, which is safe but loses CRD-level validation; flag as an explicit planning decision point, not a research gap |
| A3 | `stageEnvelopeArtifacts`'s glob generalization mechanism (derive glob from `DestPrefix`'s first path segment vs. an explicit `Glob` field on `EnvelopeStage`) is a RECOMMENDATION, not a locked design — CONTEXT.md says "extended `collectStageEnvelopes`" but does not specify tide-push's internal mechanism | Common Pitfalls §1 | High if skipped entirely — but the choice BETWEEN the two mechanisms is low-risk either way; the risk is in NOT generalizing the glob at all, not in which of the two mechanisms is chosen |
| A4 | `verifier/verdict.py`'s `GateDecision`/`Finding` should be `pydantic.BaseModel`, not stdlib `dataclass` (unlike the existing `envelope.py`'s `EnvelopeIn`), because Phase 51's `create_agent(response_format=GateDecision)` requires a Pydantic-compatible schema per `research/STACK.md:29-51` | Architecture Patterns (Recommended Project Structure) | Low-Medium — if built as a dataclass instead, Phase 51 would need to convert it to Pydantic at that point anyway (extra churn), but nothing in Phase 49 itself breaks either way since nothing consumes `response_format` yet |

**If this table is empty:** N/A — see rows above.

## Open Questions

1. **Does `LoopPolicy`/`LoopStatus` need ANY consumer this phase to make the size test and doc-comment discipline meaningful, or is a fully standalone (unembedded) type sufficient?**
   - What we know: The roadmap explicitly separates Phase 49 (schema) from Phase 51 (TASK-01, which adds the verification contract to `TaskSpec`). CONTEXT.md's Success Criterion #1 says "embeddable in any domain CRD" (capability), not "embedded in TaskSpec" (fact).
   - What's unclear: Whether the plan-checker/verifier will accept a standalone, zero-consumer type as satisfying LOOP-01, or whether it expects to SEE `LoopPolicy`/`LoopStatus` referenced somewhere (even in a test-only synthetic CRD) to prove the "embeddable" claim isn't aspirational.
   - Recommendation: Write the size test and round-trip test against bare `LoopPolicy{}`/`LoopStatus{}` values directly (no CRD needed — `json.Marshal`/`Unmarshal` don't require Kind registration). If the plan-checker wants stronger proof of embeddability, a throwaway test-only struct (`type testEmbedder struct { LoopPolicy \`json:",inline"\` }` in `_test.go`) proves embeddability without touching any real CRD — flag this as a fallback, not a requirement, unless the planner decides otherwise.

2. **Should `GateDecision`/`Finding`'s Go-side severity/dimension fields be typed enums (`+kubebuilder:validation:Enum`-style, even though `pkg/dispatch` isn't CRD-validated) or plain strings?**
   - What we know: `pkg/dispatch` carries no kubebuilder markers anywhere (confirmed by grep — zero `+kubebuilder:` comments in the package), so an "enum" there is Go-type-level only (a `type Severity string` with exported consts), not admission-enforced.
   - What's unclear: Whether locking a severity vocabulary now (e.g. `blocker`/`major`/`minor` per `research/STACK.md:39`, vs. `BLOCKER`/`WARNING`/`INFO` per `research/SUMMARY.md:38`) prematurely commits Phase 51's prompt-template work to specific casing/wording before EVAL-04 designs the actual rubric.
   - Recommendation: Leave `Finding.Dimension`/`Finding.Severity`/`Finding.Confidence` as plain `string` fields with a documentation comment listing the CURRENTLY-EXPECTED (not enforced) vocabulary, consistent with "coverage-not-conservatism: every deviation is tagged, and policy (Phase 50/51) decides what blocks, not the finder" (CONTEXT.md specifics). Locking a Go `const` enum this phase risks a rename churn in Phase 51 for zero present benefit.

## Validation Architecture

### Test Framework
| Property | Value |
|----------|-------|
| Go framework | stdlib `testing` (table-driven), no third-party assertion library — confirmed by every existing `pkg/dispatch/*_test.go` |
| Go config | none (no `.testrc`); driven by `go test` directly or via `make test`/`make test-only` |
| Python framework | `pytest` 9.1.1 (Phase 48 pin) |
| Python config | `cmd/tide-langgraph-verifier/pyproject.toml:1-2` — `testpaths = ["verifier/tests"]` |
| Quick run command (Go) | `go test ./pkg/dispatch/... ./api/v1alpha3/...` |
| Quick run command (Python) | `cd cmd/tide-langgraph-verifier && .venv/bin/python -m pytest verifier/tests/ -x -q` |
| Full suite command | `make test-only` (Go unit tier) + `make test-langgraph-verifier` (Python, idempotent venv setup + pytest) + `make generate` (deepcopy regen, no test but must run clean) |

### Phase Requirements → Test Map
| Req ID | Behavior | Test Type | Automated Command | File Exists? |
|--------|----------|-----------|-------------------|-------------|
| LOOP-01 | `LoopPolicy`/`LoopStatus` compile, deepcopy-generate, JSON round-trip | unit | `go test ./api/v1alpha3/... -run TestLoopPolicy` | ❌ Wave 0 |
| LOOP-02 | Five-element doc-comment present on both types (godoc content, not runtime-checkable) | manual/doc-review | N/A — verified by plan-checker reading the source, not a test | N/A |
| LOOP-03 | `LoopStatus` size/shape stays current-iteration-only | unit | `go test ./api/v1alpha3/... -run TestLoopStatus_NoForbiddenFields` | ❌ Wave 0 |
| EVAL-03 | `GateDecision`/`Finding` round-trip Go↔Python via golden fixture | unit (both languages) | `go test ./pkg/dispatch/... -run TestGateDecision_GoldenFixtureRoundTrip` + `pytest verifier/tests/test_verdict.py::test_golden_fixture_round_trip` | ❌ Wave 0 (both) |
| EVAL-03 | Fail-closed classifier: empty/missing-field/malformed → BLOCKED | unit (both languages) | `go test ./pkg/dispatch/... -run TestClassifyVerdict_FailsClosed` + `pytest verifier/tests/test_verdict.py::test_classify_verdict_fails_closed` | ❌ Wave 0 (both) |
| EVAL-05 | `TerminationStub` extension stays <4096 bytes under worst-case findings | unit | `go test ./pkg/dispatch/... -run TestNewTerminationStub_StaysSmall` (extended) | ✅ exists, needs extension |
| EVAL-05 | `stageEnvelopeArtifacts` glob generalization doesn't break existing Milestone/Phase/Plan/Project staging | unit (regression) | `go test ./cmd/tide-push/... -run TestStageEnvelopes` | ✅ exists (`main_test.go:1236+`), needs a NEW case added, not a rewrite |

### Sampling Rate
- **Per task commit:** the relevant quick-run command for whichever file(s) the task touched (Go OR Python, not both, unless the task spans the seam).
- **Per wave merge:** both full commands (`make test-only` + `make test-langgraph-verifier`) plus `make generate` (must produce zero diff after `git add`, proving deepcopy stayed in sync).
- **Phase gate:** full suite green before `/gsd:verify-work`, plus `make lint` (which includes `verify-dispatch-imports` — confirms `verdict.go` didn't accidentally pull in a forbidden import) and `make verify-no-aggregates` (confirms `LoopStatus` didn't trip the existing aggregate-schedule-field grep, which it won't by construction since it uses none of `Schedule|Waves\[\]|IndegreeMap|CachedDag|DerivedDag`).

### Wave 0 Gaps
- [ ] `api/v1alpha3/loop_types.go` — the types themselves don't exist yet.
- [ ] `api/v1alpha3/loop_types_test.go` — round-trip + structural size test.
- [ ] `pkg/dispatch/verdict.go` — `Verdict`/`Finding`/`GateDecision`/`ClassifyVerdict`.
- [ ] `pkg/dispatch/verdict_test.go` — golden-fixture + 3-shape fail-closed regression.
- [ ] `pkg/dispatch/testdata/gate_decision_golden.json` — the single-source fixture.
- [ ] `cmd/tide-langgraph-verifier/verifier/verdict.py` — Pydantic pair + classifier.
- [ ] `cmd/tide-langgraph-verifier/verifier/tests/test_verdict.py` — mirrored tests.
- [ ] `cmd/tide-push/main.go`'s `stageEnvelopeArtifacts` glob generalization + a new `main_test.go` case proving a non-`.md` `DestPrefix` kind (e.g. `task/...`) stages correctly without requiring a `*.md`.

*(No existing test infrastructure covers any of the above — this is all net-new schema, consistent with Phase 49 being the first phase in the v1.0.9 chain to touch these files.)*

## Security Domain

`security_enforcement` is absent from `.planning/config.json` → treated as enabled. This phase is schema-only (no new network surface, no new auth path, no new endpoint), so most ASVS categories don't apply — but the fail-closed classifier IS itself a security-relevant control (it's the mechanism that prevents a trust/authorization bypass, not just a data-quality nicety).

### Applicable ASVS Categories

| ASVS Category | Applies | Standard Control |
|---------------|---------|-----------------|
| V2 Authentication | No | This phase adds no auth surface — `EnvelopeIn.SignedToken`/credproxy auth are unchanged (Phase 3/48 scope) |
| V3 Session Management | No | No session concept in this phase's scope |
| V4 Access Control | No | No new access-control decision point — `ConditionVerifyHalt`'s gating (which IS an access-control-adjacent decision: "does new dispatch proceed") is explicitly Phase 50's scope, not this phase's |
| V5 Input Validation | **Yes** | The fail-closed classifier (`ClassifyVerdict`) is precisely an input-validation control: it must treat ANY malformed/adversarial verdict JSON as the safe (BLOCKED) case, mirroring `ValidateAPIVersionKind`'s existing strict-equality-first discipline (`envelope.go:446`) |
| V6 Cryptography | No | No cryptographic material touched by this phase |

### Known Threat Patterns for this stack

| Pattern | STRIDE | Standard Mitigation |
|---------|--------|---------------------|
| A malformed/truncated `gate_decision` JSON is misclassified as `APPROVED` | Tampering / Elevation of Privilege (an attacker or a flaky LLM call effectively "approves" work that was never actually verified) | Fail-closed classifier (D-04) — every non-exact-match path returns `BLOCKED`, never falls through to a default `APPROVED`; three regression-test shapes prove this (Pitfall 2 above) |
| Prompt-injection-shaped repo content influencing the verifier's eventual verdict | Tampering (of the evaluation process itself) | Explicitly OUT of this phase's scope — `research/PITFALLS.md:231,249` documents this as a Phase 51 concern (the verifier's PROMPT construction and its treatment of untrusted repo content); this phase only defines the WIRE SCHEMA the verdict travels in, which has no bearing on whether the verdict itself was honestly derived |
| A `findings.json` artifact staged onto the run branch containing something an attacker wants exfiltrated (e.g. secrets accidentally captured in evidence) | Information Disclosure | Out of scope for THIS phase (no findings.json is actually produced yet — Phase 51). Flag for Phase 51 research: the existing `tide-push` gitleaks scan (`push_helpers.go:247-264`, `LeaksConfigMap`) already runs on every commit including staged artifacts, so findings.json inherits that protection for free IF it's staged via the same `stageEnvelopeArtifacts`/commit path — worth confirming explicitly when Phase 51 wires the producer |

## Sources

### Primary (HIGH confidence — direct file reads this session)
- `pkg/dispatch/envelope.go` — `EnvelopeIn` (:45), `DispatchMeta`/`Dev` pointer pattern (:146,152), `EnvelopeOut` (:170), `TerminationStub` (:394), `NewTerminationStub` (:425), `ValidateAPIVersionKind` (:446)
- `pkg/dispatch/errors.go`, `pkg/dispatch/childcrd.go`, `pkg/dispatch/doc.go` — import-firewall contract, typed-error convention
- `pkg/dispatch/envelope_test.go` — round-trip test conventions (`assertRoundTripOut` :118), `TestNewTerminationStub_StaysSmall` (:674), `TestTerminationStub_NoForbiddenFields` (:698)
- `api/v1alpha3/shared_types.go` — condition/reason vocabulary idiom, `FailureProfileType` (:384), `ConditionFailureHalt` (:324)
- `api/v1alpha3/task_types.go` — `Caps`/`pkg/dispatch.Caps` decoupling doc-comment (:24-30), `TaskSpec` (:72), `TaskStatus` (:147)
- `api/v1alpha3/project_types.go` — `GatePolicy` (:37), `BudgetConfig` (:99-115), `SubagentConfig`/`LevelOverrides` resolution chain (:127-207), every `*Ref` field (grep-verified plain-string convention)
- `api/v1alpha3/groupversion_info.go` — package-level `+kubebuilder:object:generate=true` (:18)
- `api/v1alpha3/zz_generated.deepcopy.go` — proof that `Caps`/`Gates` get DeepCopy with no per-type marker
- `api/v1alpha3/schema_test.go` — external-test-package + reflect-based structural assertion convention
- `internal/controller/artifact_push.go` — `collectStageEnvelopes` (:84), `plannerMaterialized` (:55), `triggerArtifactPush` (:179)
- `internal/controller/artifact_push_test.go` — cumulative+deterministic staging test precedent (:69)
- `internal/controller/push_helpers.go` — `PushOptions.StageEnvelopes` doc (:81-88), `--stage-envelopes` render (:217-223), `stagedEnvelopesAnnotation` superset-guard doc (:142-151)
- `cmd/tide-push/main.go` — `EnvelopeStage` (:142), `parseStageEnvelopes` (:164), `stageEnvelopeArtifacts` (:1134), the `*.md` glob hard-fail (:1169-1183)
- `cmd/tide-langgraph-verifier/verifier/envelope.py` — `EnvelopeIn` dataclass (:42), `read_envelope_in` (:61), `write_envelope_out` (:118), `write_termination_stub` (:149)
- `cmd/tide-langgraph-verifier/verifier/__main__.py`, `agent.py`, `tests/conftest.py`, `tests/test_envelope.py` — Phase 48's established Python conventions, including a pre-existing `futureField` unknown-field-tolerance test (`test_envelope.py:78-86`) that anticipates this exact phase
- `cmd/tide-langgraph-verifier/pyproject.toml` — `testpaths` config
- `Makefile` — `generate`/`manifests` (:52-70), `verify-dispatch-imports` (:533), `test-langgraph-verifier` (:874), `verify-langgraph-pins` (:892), `verify-no-aggregates` (:573)
- `.github/workflows/ci.yaml` — `langgraph-verifier` job cwd/checkout convention (:234-259)

### Secondary (MEDIUM confidence — prior research docs in this repo, cross-checked against source)
- `.planning/research/ARCHITECTURE.md` §Q4 (:135-156), §Q5 (:158-169) — the pre-decided `VerifyContext`/findings-persistence struct shapes, explicitly caveated by 48-CONTEXT.md as superseded in FRAMING (three-Stage) but not in SHAPE
- `.planning/research/STACK.md` §2 (:27-55) — `GateDecision`/`Finding` Pydantic shape draft, `create_agent(response_format=...)` structured-output mechanism (informs the pydantic-vs-dataclass recommendation, Assumption A4)
- `.planning/research/SUMMARY.md` (:12-95) — cross-cutting synthesis, terminology note (`REJECT` in prior research docs → `REPAIRABLE` per this phase's CONTEXT.md locked decision — a genuine terminology shift, not a typo)
- `.planning/research/PITFALLS.md` Pitfall 7 (:124-137), Pitfall 9 (:161-173) — findings-persistence size×locality and fail-open pitfalls, both directly actionable this phase
- `.planning/notes/five-loop-model.md` — the `LoopPolicy`/`LoopStatus` field-name origin (:24-40), the five-element test wording (:43), the v1.0.9 cut boundary (:79-85)
- `.planning/notes/langgraph-successor-runtime-strategy.md` — confirms the pluggable-runtime seam is `pkg/dispatch.Subagent` + envelope, not any specific image

### Tertiary (LOW confidence — none used)
None — every claim in this document traces to either a direct file read this session or a committed research doc cross-checked against source.

## Metadata

**Confidence breakdown:**
- Standard stack: HIGH — zero new dependencies, every convention verified against real source
- Architecture: HIGH — every pattern recommendation cites an existing, working precedent in this exact codebase
- Pitfalls: HIGH for Pitfall 1 (`stageEnvelopeArtifacts` glob) — directly read the failing code path; MEDIUM for Pitfalls 2-4 — reasoned from established conventions plus prior research docs, not yet proven by a failing test in this session
- Findings persistence plumbing (EVAL-05): HIGH confidence on the PROBLEM (glob hard-fail is directly verified), MEDIUM confidence on the RECOMMENDED FIX mechanism (two viable options presented, Assumption A3) — flagged as a planning decision point, not asserted as the only correct approach

**Research date:** 2026-07-18
**Valid until:** 30 days (stable, low-churn schema domain — no external API/library surface that moves quickly; the ONE fast-moving dependency this phase's neighbors touch, `langgraph`/`langchain-anthropic`, is not itself used by this phase's files)
