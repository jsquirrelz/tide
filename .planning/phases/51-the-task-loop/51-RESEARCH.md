# Phase 51: The Task Loop - Research

**Researched:** 2026-07-19
**Domain:** K8s controller-runtime reconciler design — verification-driven quality loop, CRD schema + CEL immutability, cross-language envelope seam, halt-condition mirroring, concurrency accounting, OpenInference span emission
**Confidence:** HIGH (every seam read at HEAD; all decisions pre-locked in CONTEXT — this is a HOW-to-implement study, not a WHAT-to-choose one)

<user_constraints>
## User Constraints (from 51-CONTEXT.md)

### Locked Decisions

- **D-01 (GateCommand schema location):** Add a `verification` block to **`TaskSpec` only** — `gateCommand` (→ resolves onto `pkg/dispatch/envelope.go` `VerifyContext.GateCommand`), plus `commands`, `requiredArtifacts`, `evaluator`, `maxIterations`, `onExhaustion`. Planner-authored, immutable once locked (Draft→Locked→Superseded + version). `git show <lockedSHA>` reproduces exactly what was dispatched. Plan/Project-level fields + Task>Plan>Project precedence are **Phase 52** — OUT OF SCOPE.
- **D-02 (LangGraph runtime):** Register `"langgraph"` as a new `ProviderSpec.Vendor` literal. `SelfInstruments("langgraph") → true`; verifier image refuses `Provider.Vendor != "langgraph"` at startup. `SelfInstruments(vendor string) bool` stays a pure predicate — no signature change. Model still resolves via `ResolveProvider` (Vendor=`langgraph`, Model=`claude-…`). Precedent: `"opencode"` is already a runtime-shaped vendor value.
- **D-03 (lifecycle/locking):** `VerificationPhase` enum (Draft/Locked/Superseded) + monotonic version + locking commit SHA. Enforce "immutable once Locked" with CEL `x-kubernetes-validations` (no admission webhook). A dispatched attempt records `lockedSHA` so `git show` reproduces it.
- **D-04 (compact evidence packet):** A REPAIRABLE verdict stages a **bounded** evidence packet (failures/diffs/test output from `RunEvidence` + verdict `findings[]`) to the PVC; passes its path via `EnvelopeIn.Verify.EvidencePacketPath` (envelope.go:453). Fresh attempt re-uses **original locked spec** + this packet, never the prior agent's full context. Keep reference-only/bounded (Phase-50 `RunEvidence.Bounded()` discipline).
- **D-05 (infra-retry ≠ quality-iteration):** Eviction/transient rerun (same `attemptID`, no evaluator feedback) is preserved as-is. Quality-iteration mints a **new attempt** (increments `Task.Status.Attempt` → new `attemptID`) seeded by D-04. The blind `maxAttemptsPerTask` quality-retry is **superseded** by evaluator-driven attempts (not the eviction path). Both must stay grep-distinguishable.
- **D-06 (deterministic dominance — structural):** Non-zero gate exit forces REPAIRABLE/BLOCKED **regardless** of any LLM-judge APPROVED, enforced in the evaluator's verdict assembly (`verifier/`) **and** re-checked controller-side when consuming `EnvelopeOut.Verdict` (defence in depth). Reuse Phase-49 fail-closed `ClassifyVerdict`.
- **D-08 (three-tier escalation + anti-gaming — enforced, not documented):** *fresh attempt* (REPAIRABLE → loop), *system escalation* (a fresh attempt whose `RunEvidence.ChangedFiles` **intersects evaluator/fixture/threshold paths** is flagged systemic, **never counted as a pass**), *human decision* (`onExhaustion: requireApproval`). A regression test proves an attempt that edits the evaluator to pass is flagged, not passed.
- **D-07 (bound + resume via `LoopStatus`, re-derived):** `maxIterations` bounds the loop; iteration/cost state lives in `LoopStatus` on `Task.Status` as **current-iteration summary + exit reason only** (LOOP-03 — no accumulating history), re-derivable across a restart from `Task.Status.Attempt` + the completed-set.
- **D-09 (`ConditionVerifyHalt` clones `failure_halt.go`, gates BOTH tiers, unifies dispatch-hold chains):** Add `verify_halt.go` mirroring `failure_halt.go` file-for-file (`checkVerifyHalt` ↔ `checkFailureHalt:56`, `setVerifyHaltIfNeeded` ↔ `setFailureHaltIfNeeded:79` **including Phase-25's resume time-fence**). Wire `checkVerifyHalt` into **`checkDispatchHolds`** (planner tier) **and** **`TaskReconciler.gateChecks`** (task tier). ESC-03 regression test: a VerifyHalt leaves phase/wave-siblings/conservative-profile propagation **untouched** — never a reinterpretation of `Failed` wave semantics. **Fold the two dispatch-gate todos here** (Option 1, gated behind a co-occurring-holds envtest): migrate `gateChecks` onto `checkDispatchHolds` (normalizing Task's Import-position divergence) AND add the missing `checkFailureHalt`/`checkVerifyHalt` gate to the **Project** planner chain.
- **D-10 (dedicated `verifierInFlightCount` + `LoopPolicy.BudgetCents`):** Add a NEW `verifierInFlightCount` (mirroring `gitWriterInFlightCount`/`plannerInFlightCount`) — verifier pods are a **distinct pool** — counted against the Phase-32 concurrency gate at the Task dispatch site. `LoopPolicy.BudgetCents` bounds evaluator cost through the existing `budget.ReservationStore`. Proven by a kind-cluster concurrent-dispatch test under the sized cap.
- **D-11 (`EVALUATOR`-kind span, sibling to the checked `AGENT` span):** With `SelfInstruments("langgraph")=true` the reporter skips synthesis; the evaluator emits a distinct OpenInference `EVALUATOR`-kind span parented as a **sibling** of the checked level's `AGENT` span (no double-emission). Populate `evaluation.result`/`evaluation.version`/`human_intervention` (defined-but-empty in Phase 50) — this is their first real consumer.
- **D-12 (`role="verifier"` compiled-in Go template, coverage-not-conservatism):** Add `templates/task_verifier.tmpl` behind `LoadPromptTemplate("verifier","task")` — **no Python port**. Prompt for **coverage** (emit a finding for every deviation with severity + confidence tags); config/policy alone decides what blocks — per the Opus-4.8 tuning note, do NOT prompt "be conservative / only high-severity".

### Claude's Discretion
- Exact Go field names / JSON tags / CEL rule spelling for `TaskSpec.verification` and the `VerificationPhase`/version/`lockedSHA` status fields — within D-01/D-03.
- Whether `verify_halt.go` shares helper code with `failure_halt.go` or stays a hand-synced clone (the metriccardinality precedent favors deliberate non-sharing of guard layers) — within D-09.
- The precise evidence-packet serialization + bounding thresholds — within D-04.
- The `EVALUATOR`-span attribute set beyond the OBS-01 loop/evaluation keys — within D-11.
- Exact `verifierInFlightCount` cap default (single-node-safe, Phase-32 shape) — within D-10.

### Deferred Ideas (OUT OF SCOPE)
- **Per-level verification at Plan/Phase/Milestone/Project** (same contract parameterized by `LoopPolicy`; `Plan.Spec`/`Project.Spec` fields; Task>Plan>Project precedence) → **Phase 52** (ESC-01).
- **Chart-first config surface** (evaluator image/model + per-level `LoopPolicy` defaults, `resolveImage` precedence, off-on-in-place-upgrade posture) → **Phase 53** (CFG-01/02).
- **Dashboard nested-provenance + `VerifyHalt` visual state** → **Phase 53** (OBS-04).
- **Composite evaluators** (schema/spec conformance, security, diff-scope beyond deterministic + single LLM judge) → named future arc.
- **Product / System / Oversight loops** → named future milestones.
</user_constraints>

<phase_requirements>
## Phase Requirements

| ID | Description | Research Support |
|----|-------------|------------------|
| TASK-01 | Planner-authored, immutable-once-locked verification contract on `TaskSpec` | `TaskSpec.verification` block + CEL transition rule (§CEL Immutability); `VerifyContext.GateCommand` wire field already exists (envelope.go:439) |
| TASK-02 | REPAIRABLE → fresh attempt + compact evidence packet | `EvidencePacketPath` wire field exists (envelope.go:453); `RunEvidence`/`findings[]` are the packet source; new attempt = `Task.Status.Attempt`++ |
| TASK-03 | Infra-retry ≠ quality-iteration | Two distinct paths: eviction rerun (same `attemptID`) preserved; quality-iteration mints new `attemptID`; blind `maxAttemptsPerTask` (task_controller.go:729) superseded |
| TASK-04 | Independent evaluator + deterministic dominates judge | Verifier image already independent process (buildable, never dispatched today); `ClassifyVerdict` fail-closed + out-of-band gate-exit capture (§Pitfall 1) |
| TASK-05 | `maxIterations` bound + resumable across restart | `LoopStatus` on `Task.Status` (current-iteration only); re-derive from `Task.Status.Attempt` + completed-set — mirrors indegree re-derive principle |
| TASK-06 | Three-tier escalation + anti-gaming enforced structurally | Intersect `RunEvidence.ChangedFiles` (run_evidence.go:70) with a protected-path set; `onExhaustion: requireApproval` |
| EVAL-04 | Verifier Go template, coverage-not-conservatism | `LoadPromptTemplate("verifier","task")` + `templates/task_verifier.tmpl` (loader already keys off `<level>_<role>.tmpl`, prompt_templates.go:81) |
| ESC-02 | `ConditionVerifyHalt` mirrors `failure_halt.go`, gates both tiers | Clone `failure_halt.go` → `verify_halt.go`; wire into `checkDispatchHolds:580` + `gateChecks:334` |
| ESC-03 | Distinct halt class, never `Failed` wave reinterpretation | New `ConditionVerifyHalt` constant in shared_types.go alongside `ConditionFailureHalt:324`; regression test |
| ESC-04 | Verifier counted against concurrency gate + `BudgetCents` | New `verifierInFlightCount` (mirror `plannerInFlightCount:490`); `LoopPolicy.BudgetCents` via `budget.ReservationStore` (task_controller.go:109) |
| OBS-03 | `SelfInstruments` registration + `EVALUATOR` sibling span | `vendor_capabilities.go:38` add `"langgraph"→true`; `semconv.SpanKindEvaluator` exists in pinned module; `EvaluationAttributes`/`HumanIntervention` helpers exist (attrs.go:480,490) |
</phase_requirements>

## Summary

Phase 51 is the highest-value addition of v1.0.9: it turns `TaskReconciler` from an "exit-0 → Succeeded" pipeline stage into a real verification-driven quality loop. Every downstream primitive already exists — Phase 48 built the read-only LangGraph verifier image, Phase 49 locked the `LoopPolicy`/`LoopStatus`/`GateDecision`/`VerifyContext` schema, and Phase 50 hardened the run-evidence envelope and defined (but left empty) the `evaluation.*` span keys. **This phase is the wiring that connects them, plus the halt/concurrency/anti-gaming safety that lands with the dispatch sites (not deferred).**

The single most important structural finding: **the verifier is buildable but is dispatched nowhere today.** `podjob.BuildJobSpec` has a `ReadOnly` variant (jobspec.go:188), the verifier reads `TIDE_GATE_COMMAND` (tools.py:56), and `Role="verifier"` is a valid envelope value — but no controller code creates a verifier Job, sets `TIDE_GATE_COMMAND`, or reads back `EnvelopeOut.Verdict`. Phase 51 must build the entire verifier dispatch: a new Task sub-state (executor-complete → verify-dispatch → verify-complete → decide), a verifier `JobKind`/job-name/image resolution, the `TIDE_GATE_COMMAND` env injection from `VerifyContext.GateCommand`, and the verdict-consumption decision tree.

The second structural finding: **`ConditionVerifyHalt` follows an exceptionally well-worn path.** `failure_halt.go` is a 115-line, self-contained, two-function file (`checkFailureHalt`/`setFailureHaltIfNeeded`) with the Phase-25 resume time-fence baked in — it clones almost verbatim into `verify_halt.go`. The two folded dispatch-gate todos (Project chain missing `checkFailureHalt`; Task chain's divergent Import position) are real and confirmed at HEAD, and this phase edits exactly those chains, so folding them is a structural improvement, not scope creep.

**Primary recommendation:** Structure the phase around four load-bearing seams in dependency order: (1) `TaskSpec.verification` schema + CEL immutability + `LoopStatus` embedding; (2) the verifier dispatch/consume sub-state-machine in `task_controller.go` (the new Task phase, the `VerifyContext`-populated envelope, the `verifierInFlightCount` gate, the Go+Python verdict-assembly with out-of-band deterministic capture); (3) `verify_halt.go` + the dispatch-chain unification (folds both todos); (4) the `"langgraph"` vendor + `EVALUATOR` span + `task_verifier.tmpl`. Prove it live on a kind cluster (Success Criterion) with the concurrent-dispatch cap test.

## Architectural Responsibility Map

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|-------------|----------------|-----------|
| Verification contract schema + immutability | K8s API (CRD + CEL admission) | — | `TaskSpec.verification` is planner-authored data; CEL `x-kubernetes-validations` is the project's established validation idiom (no webhook) |
| Verifier dispatch + verdict consumption | API / Controller (`TaskReconciler`) | — | The Task loop is `TaskReconciler`'s job (five-loop model §52); a held task blocks dependents free via indegree |
| Real gate command execution + verdict assembly | Verifier pod (`cmd/tide-langgraph-verifier`) | Controller (D-06 re-check) | The evaluator is a **logically independent process** (TASK-04); exit code parsed in-pod, never self-reported |
| Deterministic-dominates-judge | Verifier pod (out-of-band capture) | Controller (defence in depth) | The LLM must not be trusted to report its own gate exit (§Pitfall 1) — dominance enforced structurally in both |
| Halt on exhaustion (`ConditionVerifyHalt`) | Controller (`verify_halt.go` + dispatch chains) | — | Halt is a project-scoped dispatch gate; mirrors `FailureHalt`/`BillingHalt` (execution-only + planner-tier per D-09) |
| Concurrency + cost accounting | Controller (`verifierInFlightCount` + `ReservationStore`) | — | Verifier pods are a distinct pool the spec sizes separately; the cap is the run-2b single-node-OOM guard |
| Anti-gaming (protected-path intersection) | Controller (`RunEvidence.ChangedFiles` ∩ protected set) | — | The changed-file manifest is a controller-side observation of the attempt's diff (run_evidence.go) |
| Loop-native observability (`EVALUATOR` span) | Controller (`span_emission.go`) | `pkg/otelai` helpers | Span synthesis is manager-side; `SelfInstruments("langgraph")` makes the reporter skip events.jsonl synthesis |
| Verifier prompt rendering | Controller / orchestrator-side (Go template) | — | D-12/EVAL-04: prompts render Go-side, no Python authoring surface |

## Standard Stack

This phase adds **no new external dependencies.** Every capability rides an existing in-repo primitive. The "stack" here is the set of internal seams to reuse.

### Core (existing seams — reuse, do not re-invent)

| Seam | Location | Purpose | Why Standard |
|------|----------|---------|--------------|
| `VerifyContext` | pkg/dispatch/envelope.go:434 | Wire fields `GateCommand`/`RequiredArtifacts`/`EvaluatorRef`/`EvidencePacketPath` | Already on `EnvelopeIn.Verify` (:174); D-01/D-04 only add the CRD authoring surface that populates it |
| `GateDecision`/`Finding`/`ClassifyVerdict` | pkg/dispatch/verdict.go | Verdict schema, fail-closed classifier | Phase 49; empty/malformed → BLOCKED, never APPROVED. `highSeverityFindingToken="blocker"` is a package const to retune (D-06/D-08) |
| `LoopPolicy`/`LoopStatus`/`EvaluationSummary` | api/v1alpha3/loop_types.go | CRD-embeddable loop contract | Phase 49; `LoopStatus.LastEvaluation` is the bounded verdict projection; `ExitReason` enum (`iterationsExhausted`/`escalated`/…) already defined |
| `RunEvidence` + `ChangedFiles` + `.Bounded()` | pkg/dispatch/run_evidence.go | Bounded run-evidence + changed-file manifest | Phase 50; `ChangedFiles []ChangedFile{Path,Status}` is the D-08 anti-gaming key; `MaxRunEvidenceChangedFiles=100` |
| `failure_halt.go` (`checkFailureHalt`/`setFailureHaltIfNeeded`) | internal/controller/failure_halt.go:56,79 | The clone template for `verify_halt.go` | Self-contained 2-func file with Phase-25 resume time-fence baked in |
| `checkDispatchHolds` | internal/controller/dispatch_helpers.go:580 | Planner-tier shared hold chain (Billing→Failure→Budget→Import) | Called by Milestone/Phase/Plan; D-09 wires `checkVerifyHalt` in |
| `plannerInFlightCount` / `gitWriterInFlightCount` | dispatch_helpers.go:490 / git_writer.go:100 | Label-selected non-terminal-Job in-flight count | The exact shape `verifierInFlightCount` copies (D-10) |
| `budget.ReservationStore` | task_controller.go:109 | Reserve/settle pre-charge accounting, rederivable-on-restart | `LoopPolicy.BudgetCents` rides this (D-10) |
| `LoadPromptTemplate(role,level)` | internal/subagent/common/prompt_templates.go:80 | `<level>_<role>.tmpl` compiled-in loader | Zero new loader machinery for `task_verifier.tmpl` (D-12) |
| `SelfInstruments(vendor)` | pkg/dispatch/vendor_capabilities.go:38 | Pure vendor predicate | D-02 adds `case "langgraph": return true` (currently every case returns false) |
| `otelai.EvaluationAttributes` / `HumanIntervention` / `LoopAttributes` | pkg/otelai/attrs.go:480,490,441 | Loop/evaluation span attribute helpers | Phase 50 defined them empty; Phase 51 is first consumer. `semconv.SpanKindEvaluator="EVALUATOR"` exists in pinned module |
| `podjob.BuildJobSpec` `ReadOnly` variant | internal/dispatch/podjob/jobspec.go:188 | Read-only verifier Job (RO worktree + git-cred omission + verifier-scratch) | Phase 48 D-08; the build path exists — Phase 51 supplies the dispatch caller |

### Supporting (verifier image — Python, extend in place)

| File | Location | What Phase 51 adds |
|------|----------|--------------------|
| `__main__.py` | cmd/tide-langgraph-verifier/verifier/ | Flip `SUPPORTED_VENDOR="anthropic"` → `"langgraph"` (D-02); extract `env.verify.gateCommand` → set `TIDE_GATE_COMMAND`; assemble `GateDecision` verdict with deterministic dominance; write into `EnvelopeOut.Verdict` |
| `tools.py` | " | `run_gate_command` already reads `TIDE_GATE_COMMAND` (:150), fail-closed if empty (:151) — unchanged; entrypoint must set the env |
| `verdict.py` | " | `GateDecision`/`classify_verdict` already ported (Phase 49) — reuse for the assembled verdict |
| `agent.py` | " | Prompt already passed via `system_prompt`/`env.prompt` (D-12: Go-rendered, no Python port); no template logic added |

### Alternatives Considered
| Instead of | Could Use | Tradeoff |
|------------|-----------|----------|
| New `JobKindVerifier` | Reuse `ReadOnly bool` on `JobKindExecutor` | `ReadOnly` already exists (jobspec.go:188); but caps floor differs (executor 1200s vs a shorter verify floor) and the wall-clock/token derivation (caps.go) keys off JobKind — a dedicated kind keeps the verify caps + job-name tuple grep-distinct. **Recommend a dedicated kind** for clean concurrency-label selection (`role=verifier`). |
| `verifierInFlightCount` (new) | Overload `plannerInFlightCount` | CONTEXT D-10 locks the new dedicated count — verifier is a distinct pool; overloading would let a burst of verifiers starve planners and vice-versa |

**Installation:** None — no `go get`, no `pip install`. All Go seams are in-tree; the Python image already pins its deps (Phase 48, patch-exact, CI-gated).

## Package Legitimacy Audit

**No external packages are installed in this phase.** All work is on in-repo Go packages (`api/v1alpha3`, `pkg/dispatch`, `pkg/otelai`, `internal/controller`, `internal/dispatch/podjob`, `internal/subagent/common`) and the already-pinned Python verifier image (`cmd/tide-langgraph-verifier`). The slopcheck gate is therefore N/A for this phase.

*If the planner discovers a genuinely new dependency is needed (not anticipated by this research), gate it behind a `checkpoint:human-verify` task and run the Package Legitimacy Gate before install.*

## Architecture Patterns

### System Architecture Diagram — the Task loop (new control flow)

```
                          ┌──────────────────────────────────────────────┐
   Task Pending ─────────▶│ gateChecks (task_controller.go:334)          │
                          │  Reject → ParentApproval → Import → Billing → │
                          │  Failure → [NEW: VerifyHalt] → Budget → Rsv   │
                          └───────────────┬──────────────────────────────┘
                                          │ all clear
                                          ▼
                     ┌────────────────────────────────────────┐
                     │ dispatch EXECUTOR Job (Role="executor") │  ← existing path
                     │  attemptID = {taskUID}-{attempt}        │
                     └───────────────┬────────────────────────┘
                                     │ Job terminal, exit 0 (belief-complete, EXEC-04)
                                     ▼
        ┌────────────────────────────────────────────────────────────────┐
        │  NEW: instead of stamping Succeeded (handleJobCompletion:1424),  │
        │  transition Task → "Verifying" and dispatch VERIFIER Job:        │
        │   • verifierInFlightCount cap gate (D-10) + BudgetCents reserve  │
        │   • EnvelopeIn{ Role="verifier", Verify=VerifyContext{           │
        │        GateCommand ← spec.verification (locked), EvidencePacket… │
        │     }, Provider.Vendor="langgraph" }                             │
        │   • TIDE_GATE_COMMAND env ← Verify.GateCommand                   │
        │   • BuildJobSpec ReadOnly=true, verifier image                   │
        └───────────────────────────┬────────────────────────────────────┘
                                     │ verifier Job terminal → read EnvelopeOut.Verdict
                                     ▼
     ┌───────────────────────────────────────────────────────────────────────┐
     │  ClassifyVerdict(out.Verdict) + D-06 controller-side re-check           │
     └───────┬───────────────────────┬───────────────────────────┬───────────┘
             │ APPROVED              │ REPAIRABLE                 │ BLOCKED / exhausted
             ▼                       ▼                            ▼
     ┌───────────────┐   ┌───────────────────────────┐   ┌────────────────────────┐
     │ Succeeded     │   │ D-08 anti-gaming check:    │   │ setVerifyHaltIfNeeded   │
     │ (JobComplete) │   │ ChangedFiles ∩ protected?  │   │  → ConditionVerifyHalt  │
     └───────────────┘   │  yes → system escalation   │   │  (onExhaustion:         │
                         │       (never a pass)       │   │   requireApproval)      │
                         │  no  → Attempt++ (D-05),   │   │  parks both tiers        │
                         │        stage evidence      │   └────────────────────────┘
                         │        packet (D-04),      │
                         │        re-dispatch executor│
                         │        if Attempt ≤ maxIter│
                         └───────────────────────────┘
```

Data-flow trace of the primary use case (a Task whose gate fails then repairs): planner authors `spec.verification` (Locked); executor runs, believes complete; controller dispatches the verifier against the real `gateCommand`; verifier's out-of-band gate exit is non-zero → `GateDecision.Verdict=REPAIRABLE` with a `blocker` finding; controller stages a bounded evidence packet, increments `Attempt`, re-dispatches the executor seeded with the locked spec + packet; second attempt passes the gate → verifier returns APPROVED → Task Succeeded. If `Attempt` reaches `maxIterations` without APPROVED → `ConditionVerifyHalt` parks dispatch until `tide resume`/approval.

### Recommended change map (files touched)

```
api/v1alpha3/
├── task_types.go            # + VerificationSpec on TaskSpec; + LoopStatus/VerificationPhase/version/lockedSHA on TaskStatus; CEL immutability marker
├── shared_types.go          # + ConditionVerifyHalt + ReasonVerifyExhausted + AnnotationVerifyResumedAt (mirror :318-335 block)
└── zz_generated.deepcopy.go # regenerated via `make generate`
pkg/dispatch/
└── provider.go              # + "langgraph" to the ProviderSpec.Vendor doc-comment canonical set (:38-40)
pkg/dispatch/vendor_capabilities.go  # + case "langgraph": return true
pkg/otelai/attrs.go          # + EvaluatorInvocation helper (SpanKindEvaluator) — parallels AgentInvocation
internal/controller/
├── verify_halt.go           # NEW — clone of failure_halt.go (checkVerifyHalt/setVerifyHaltIfNeeded + resume time-fence)
├── task_controller.go       # the verifier dispatch/consume sub-state-machine; VerifyContext population; verifierInFlightCount; anti-gaming; VerifyHalt in gateChecks (via checkDispatchHolds migration)
├── dispatch_helpers.go      # + verifierInFlightCount; + checkVerifyHalt into checkDispatchHolds
├── project_controller.go    # + checkFailureHalt + checkVerifyHalt into the planner-dispatch block (:1540) — folds todo #1
└── span_emission.go         # EVALUATOR sibling span emission
internal/dispatch/podjob/
├── caps.go                  # + JobKindVerifier (+ verify caps floor)
├── jobspec.go               # verifier Job build wiring (image, TIDE_GATE_COMMAND env, ReadOnly already present)
└── names.go                 # + verifier job-name helper (mirror JobName tuple)
internal/subagent/common/templates/
└── task_verifier.tmpl       # NEW — role="verifier" coverage-not-conservatism prompt
cmd/tide-langgraph-verifier/verifier/
├── __main__.py              # vendor sentinel → langgraph; TIDE_GATE_COMMAND from Verify; verdict assembly + deterministic dominance
└── (verdict.py/tools.py reused)
```

### Pattern 1: Clone the halt-condition file (D-09)
**What:** `verify_halt.go` is a near-verbatim copy of `failure_halt.go`, swapping `Failure`→`Verify`, `conservative FailureProfile`→`onExhaustion`, and the resume annotation.
**When to use:** For `ConditionVerifyHalt` — the BillingHalt→FailureHalt→VerifyHalt mirror is a proven three-generation pattern.
**Example (the template being cloned):**
```go
// Source: internal/controller/failure_halt.go:79 (VERIFIED: read at HEAD)
func setFailureHaltIfNeeded(ctx context.Context, c client.Client, project *tideprojectv1alpha3.Project, taskCompletedAt time.Time) error {
    if project == nil { return nil }
    if project.Spec.FailureProfile != tideprojectv1alpha3.FailureProfileConservative { return nil }
    if meta.IsStatusConditionTrue(project.Status.Conditions, tideprojectv1alpha3.ConditionFailureHalt) { return nil }
    // Phase-25 resume time-fence — refuse to re-stamp for a failure predating `tide resume`:
    if !taskCompletedAt.IsZero() {
        if resumeVal, ok := project.Annotations[tideprojectv1alpha3.AnnotationFailureResumedAt]; ok {
            if resumedAt, err := time.Parse(time.RFC3339, resumeVal); err == nil {
                if taskCompletedAt.Before(resumedAt) { return nil }
            }
        }
    }
    patch := client.MergeFrom(project.DeepCopy())
    meta.SetStatusCondition(&project.Status.Conditions, metav1.Condition{
        Type: tideprojectv1alpha3.ConditionFailureHalt, Status: metav1.ConditionTrue,
        Reason: tideprojectv1alpha3.ReasonTaskFailedHalt, LastTransitionTime: metav1.Now(),
    })
    return c.Status().Patch(ctx, project, patch)
}
```
`setVerifyHaltIfNeeded` differs only in the trigger condition (loop exhausted vs. conservative-failure) and vocabulary. **Keep the time-fence** — a Verifying Task can reconcile between a `tide resume` clear and its own reset, re-freezing the project exactly as CR-02 (Phase 25) documents.

### Pattern 2: CEL conditional immutability (D-03, TASK-01)
**What:** "Immutable once Locked" is a CEL transition rule, not unconditional immutability.
**Critical constraint (KEY FINDING):** A CEL `x-kubernetes-validations` transition rule using `oldSelf` on `spec.verification` **cannot reference `status`** — spec and status are separate subresources, and a spec transition rule only sees `oldSelf` of that same spec subtree. Therefore **the phase/version fields that GOVERN immutability must live on `spec.verification` itself** (so the rule can read `oldSelf.phase`), while `lockedSHA` — a runtime observation recorded at dispatch — lives on `Task.Status`. This is a refinement of D-03's "recorded on Task.Status": *the governing enum lives on spec; only the observed lockedSHA is status.* See Open Question 1.
**Example:**
```go
// Source: kubebuilder crd-validation docs (CITED: book.kubebuilder.io/reference/markers/crd-validation.html)
// On the VerificationSpec type — conditional immutability + a Superseded escape:
// +kubebuilder:validation:XValidation:rule="oldSelf.phase != 'Locked' || self == oldSelf || self.phase == 'Superseded'",message="verification is immutable once Locked; supersede to a new version to change it"
type VerificationSpec struct {
    // +kubebuilder:validation:Enum=Draft;Locked;Superseded
    Phase string `json:"phase,omitempty"`
    // +kubebuilder:validation:Minimum=1
    Version int32 `json:"version,omitempty"`
    GateCommand string `json:"gateCommand,omitempty"`
    Commands []string `json:"commands,omitempty"`
    RequiredArtifacts []string `json:"requiredArtifacts,omitempty"`
    Evaluator string `json:"evaluator,omitempty"`
    // +kubebuilder:validation:Minimum=0
    MaxIterations int32 `json:"maxIterations,omitempty"`
    // +kubebuilder:validation:Enum=escalate;requireApproval
    OnExhaustion string `json:"onExhaustion,omitempty"`
}
```
Transition rules are **skipped on CREATE** (no `oldSelf`) — so a Draft-at-create is unconstrained; the rule fires only on UPDATE. `self == oldSelf` on a small struct is well within the CEL cost budget.

### Pattern 3: In-flight concurrency cap gate (D-10, ESC-04)
**What:** Count non-terminal verifier Jobs; defer dispatch (requeue, never error) when the count ≥ cap. **Note:** the executor/task path has NO D3 in-flight cap today — only planners do (milestone_controller.go:344). So the pattern to mirror is the *planner-tier* gate, applied at the new verifier dispatch site.
**Example:**
```go
// Source: internal/controller/milestone_controller.go:344 (VERIFIED: read at HEAD) — the shape to mirror
if r.Deps.VerifierPool != nil {
    inFlight, err := verifierInFlightCount(ctx, r.Client, r.Deps.WatchNamespace)
    if err != nil { return ctrl.Result{}, fmt.Errorf("verifier in-flight count: %w", err) }
    if inFlight >= r.Deps.VerifierPool.Capacity() {
        return ctrl.Result{RequeueAfter: 10 * time.Second}, nil  // defer, no slot leak
    }
}
// verifierInFlightCount mirrors plannerInFlightCount (dispatch_helpers.go:490):
//   List Jobs matching {tideproject.k8s/role: "verifier"}; skip DeletionTimestamp!=nil; count !isJobTerminal.
```

### Pattern 4: Out-of-band deterministic capture (D-06, TASK-04)
**What:** The verifier must capture the gate command's exit code **independent of the LLM's tool-call narration**, because a probabilistic judge could misreport a red gate as green (the exact 2026-07-03 failure this milestone exists to fix). The `run_gate_command` tool (tools.py:126) returns `exit_code=N\n...`, but the *entrypoint* must not trust the LLM to relay it. See §Pitfall 1 for the two viable enforcement points.

### Anti-Patterns to Avoid
- **Trusting the LLM's reading of the gate exit.** The judge's APPROVED must never override a captured non-zero exit (Out of Scope table, REQUIREMENTS.md:87). Capture the exit deterministically.
- **Stamping Task correctness from the Execution loop.** EXEC-04 is locked: the executor envelope reports belief only (`TerminalReason==completed`). Correctness is exclusively the verifier's call. Do not shortcut exit-0 → Succeeded (that is precisely the path being replaced).
- **Accumulating iteration history in `.status`.** LOOP-03: `LoopStatus` carries current-iteration summary only. `TestLoopStatus_NoForbiddenFields` (Phase 49) guards this — history goes to traces/artifacts.
- **Fake-populating a verdict on envelope loss.** `ClassifyVerdict` fail-closed → BLOCKED. A missing/unreadable verifier envelope is BLOCKED, never APPROVED (mirrors the EnvelopeReadFailed path at task_controller.go:1251).
- **Overloading `plannerInFlightCount` for verifiers.** D-10 locks a distinct count/pool — verifier pods are a separately-sized pool.
- **A parallel loop span.** D-11: `EVALUATOR` is a **sibling** of the AGENT span, not a child, and the reporter must skip events.jsonl synthesis for `langgraph` (via `SelfInstruments`) so there's no double-emission.

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| Verdict classification | A custom "is this APPROVED?" parser | `pkg/dispatch.ClassifyVerdict` | Already fail-closed by construction; bare `Verdict` return makes "unknown" inexpressible as anything but BLOCKED (verdict.go:99) |
| Halt condition | A bespoke verify-halt gate | Clone `failure_halt.go` | The resume time-fence (CR-02) is subtle and already solved; a fresh implementation would re-introduce the pre-resume-straggler re-freeze bug |
| Changed-file manifest | Re-run `git diff` in the controller | `EnvelopeOut.RunEvidence.ChangedFiles` | Phase 50 already produces the bounded `--name-status` manifest; re-deriving duplicates it and risks unbounded growth |
| Loop/evaluation span attributes | Hand-typed `"evaluation.result"` string literals | `otelai.EvaluationAttributes`/`HumanIntervention` | `TestKeysUseSemconvModule` grep guard rejects raw literals; helpers already exist empty (attrs.go:480) |
| EVALUATOR span kind | Hardcoded `"EVALUATOR"` | `semconv.SpanKindEvaluator` (module const, VERIFIED present in v0.1.1) | Same grep guard; keeps the span kind spec-tracked |
| In-flight counting | A new label scheme | Mirror `plannerInFlightCount`/`gitWriterInFlightCount` | Proven DeletionTimestamp + isJobTerminal exclusions; the run-2b OOM guard is this exact pattern |
| Cost bounding | A new budget ledger | `budget.ReservationStore` reserve/settle | Rederivable-on-restart; `BudgetCents` rides the existing accounting (task_controller.go:889,1247) |
| Evidence packet transport | A new PVC path or etcd blob | `VerifyContext.EvidencePacketPath` + staged on run branch | envelope.go:453 wire field exists; size×locality rule forbids new paths (Phase 49 D-05) |
| CRD immutability | An admission webhook | CEL `x-kubernetes-validations` | Project idiom (CONTEXT Established Patterns); no webhook infra to add |

**Key insight:** Phase 51 is ~90% wiring of primitives Phases 48–50 already built and tested. The genuinely-new *logic* is narrow: (a) the verifier dispatch/consume sub-state-machine in `task_controller.go`, (b) the out-of-band deterministic capture in the Python entrypoint, (c) the anti-gaming path intersection. Everything else is clone-and-wire.

## Runtime State Inventory

> This phase is an additive schema + reconciler-logic change (not a rename), but it changes the Task state machine and adds CRD fields — so the runtime-state questions matter.

| Category | Items Found | Action Required |
|----------|-------------|------------------|
| Stored data (CRD) | Existing `Task` objects have no `spec.verification` and no `LoopStatus`. The fields are `+optional` — old Tasks deserialize with zero values. A Task with an empty/absent `verification` must degrade gracefully (no gate command → skip verify, preserve today's exit-0→Succeeded, OR fail-closed to require a contract). **Decide the empty-verification posture** (see Open Question 2). | Controller nil-guard + a decision on default posture. No data migration (additive, v1alpha3 is sole version). |
| Live service config | None. No external service stores the Task-loop state — it re-derives from `Task.Status.Attempt` + the completed-set each reconcile (D-07). | None — verified by D-07's re-derive model. |
| OS-registered state | None — no OS-level registration involved. | None. |
| Secrets / env vars | The verifier pod needs `TIDE_GATE_COMMAND` (orchestrator-set, NOT model-supplied — a real threat-model boundary, §Security). The signed-token/credproxy path is unchanged (verifier reuses the executor's credproxy sidecar per Phase 48). No new secret keys. | Set `TIDE_GATE_COMMAND` in the verifier Job env from `VerifyContext.GateCommand`. |
| Build artifacts / images | The `tide-langgraph-verifier` image (Phase 48) exists but its entrypoint changes (vendor sentinel, verdict assembly). CI rebuilds it; the chart's verifier-image ref is a **Phase 53** concern (CFG-01) — for Phase 51 the manager needs a way to know the verifier image (a new `Deps` field / flag). | Add a verifier-image `Deps` field (dev-head default); the chart-first surface is Phase 53. |

**Controller-restart resumption (LOOP-03/TASK-05):** After a manager restart mid-verify, the Task is at phase `Verifying` with `Attempt=N`; the reconciler re-observes the (deterministic-named) verifier Job, reads its verdict if terminal, or waits. No loop history is persisted — `LoopStatus` current-iteration summary + `Task.Status.Attempt` are sufficient to re-derive, exactly as the indegree model re-derives waves. **Verified:** the same deterministic-Job-name + re-read pattern already resumes executor Jobs (checkRunningState:620).

## Common Pitfalls

### Pitfall 1: The LLM judge silently overriding a red gate (the milestone's raison d'être)
**What goes wrong:** The verifier's LLM sees `run_gate_command` return `exit_code=1` but its final message says "tests pass, APPROVED"; the verdict is assembled from the LLM text → a red Task passes. This is the exact 2026-07-03 silent-`Complete` incident (verdict.go:41, REQUIREMENTS.md:90).
**Why it happens:** `run_gate_command` returns the exit code *as text to the model* (tools.py:166); nothing forces the assembled verdict to honor it.
**How to avoid (D-06, two enforcement points — recommend BOTH):**
1. **Verifier-side (primary):** The entrypoint captures the gate exit **out-of-band** — either by running the gate command itself once deterministically and stamping the result, or by post-processing the tool's structured return — and forces `Verdict ∈ {REPAIRABLE, BLOCKED}` (never APPROVED) when the exit is non-zero, emitting a `blocker`-severity `Finding` with `dimension="gate-command"` and `evidence="exit_code=N"`.
2. **Controller-side (defence in depth):** When consuming `EnvelopeOut.Verdict`, `ClassifyVerdict` handles empty/malformed; additionally, treat any `Finding{Severity: "blocker", Dimension: "gate-command"}` (or a dedicated deterministic-failure signal on the verdict) as forcing non-APPROVED regardless of the top-level `verdict` string.
**Warning signs:** A verifier envelope with `verdict=APPROVED` but a non-zero gate finding — the regression test must assert this cannot pass.

### Pitfall 2: CEL immutability rule can't see status
**What goes wrong:** Planner puts `VerificationPhase` on `Task.Status` (literal D-03 reading), then writes a CEL rule on `spec.verification` referencing `status.verificationPhase` — which is inexpressible (spec transition rules see only `oldSelf` of spec).
**Why it happens:** Spec and status are separate subresources.
**How to avoid:** Put the governing `phase`/`version` on `spec.verification` (Pattern 2); keep only the observed `lockedSHA` on status. Confirm at `make manifests` time that the generated CRD carries the `x-kubernetes-validations` rule.
**Warning signs:** `controller-gen` emits no validation rule, or admission accepts a Locked-spec mutation in an envtest.

### Pitfall 3: The two dispatch-gate divergences bite under co-occurring holds
**What goes wrong:** Wiring `checkVerifyHalt` into `gateChecks` inline (Task's divergent order — Import checked SECOND, task_controller.go:391-402) vs. into `checkDispatchHolds` (planner order — Import LAST) means a Task under simultaneous VerifyHalt + Import-pending fires a different hold than a Plan would, silently diverging behavior.
**Why it happens:** `gateChecks` is intentionally NOT a `checkDispatchHolds` caller today (documented divergence at :390-402); the two folded todos are exactly this.
**How to avoid (D-09 Option 1):** Migrate `gateChecks` onto `checkDispatchHolds` in the same change, normalizing the order, gated behind a **co-occurring-holds envtest** that pins which hold fires. This is a deliberate, tested behavior change — not a silent shift. Also add `checkFailureHalt`+`checkVerifyHalt` to the Project planner block (project_controller.go:1540, which has neither today — folds todo #1).
**Warning signs:** The envtest for "Import-pending AND VerifyHalt" asserts a requeue interval/message that differs by level.

### Pitfall 4: Infra-retry and quality-iteration collapse into one path
**What goes wrong:** Reusing the `maxAttemptsPerTask` counter (task_controller.go:729) for evaluator-driven attempts conflates a transient eviction rerun (same attempt) with a quality repair (new attempt), breaking the D-05 distinction and mis-counting against `maxIterations`.
**Why it happens:** Both increment attempt-like counters.
**How to avoid:** Keep the eviction path (same `attemptID`) untouched; quality-iteration increments `Task.Status.Attempt` → new `attemptID` tuple, seeded by the evidence packet. The blind `maxAttemptsPerTask` quality-retry is superseded (not the eviction path). Keep them grep-distinguishable in the controller (distinct code paths + comments).
**Warning signs:** A test where an evicted-then-rerun attempt consumes a `maxIterations` slot.

### Pitfall 5: Anti-gaming check false-negatives on legitimate test edits
**What goes wrong:** The protected-path set is too broad (e.g. all `*_test.go`) → a legitimate repair that adds a test is flagged as gaming; or too narrow → an attempt that weakens the evaluator slips through.
**Why it happens:** "Fixtures/thresholds/the evaluator itself" (TASK-06) is a policy-defined set, not a syntactic one.
**How to avoid:** Intersect `RunEvidence.ChangedFiles` (path + status) with a **planner-declared/config protected-path set** scoped to the *evaluator and fixtures the verification contract depends on* — not all tests. The regression test (D-08) proves an attempt editing the evaluator to pass is flagged as system escalation, never a pass. Keep the set explicit and reviewable.
**Warning signs:** The detector has no test proving both a true-positive (evaluator edit) and a true-negative (ordinary code+test repair).

### Pitfall 6: Verifier dispatch leaks a reservation slot or a concurrency slot
**What goes wrong:** Parking the verifier dispatch after acquiring a pool slot / reserving budget leaks it (the exact Pitfall-2 pattern the planner-tier gates avoid by ordering the cap check BEFORE acquire).
**How to avoid:** Order the `verifierInFlightCount` cap check BEFORE any pool acquire and BEFORE the reservation `Reserve`, mirroring milestone_controller.go:344 (cap gate before acquire) and the `committed=false` deferred-release pattern (task_controller.go:301-306). Settle/Release the reservation when the verifier Job settles.
**Warning signs:** A concurrent-dispatch kind test where the in-flight count drifts above the cap or reservations never settle.

## Code Examples

### Consuming the verifier verdict (controller-side decision tree)
```go
// Source: pattern grounded in task_controller.go:1249-1471 (handleJobCompletion) + verdict.go (VERIFIED)
// After the VERIFIER Job (not the executor) reaches terminal:
out, err := r.Deps.EnvReader.ReadOut(ctx, string(project.UID), string(task.UID)) // verifier out.json
if err != nil || out.Verdict == nil {
    // Fail-closed: an unreadable/absent verifier envelope is BLOCKED, never APPROVED (verdict.go:41).
    return r.haltVerify(ctx, task, project, "verifier envelope unreadable")
}
raw, _ := json.Marshal(out.Verdict)
switch dispatch.ClassifyVerdict(raw) {
case dispatch.VerdictApproved:
    // D-06 defence-in-depth: even APPROVED loses to a deterministic gate-failure finding.
    if hasDeterministicFailure(out.Verdict) {
        return r.repairOrHalt(ctx, task, project, out) // treat as REPAIRABLE
    }
    return r.markSucceeded(ctx, task) // JobComplete (existing path, :1424)
case dispatch.VerdictRepairable:
    return r.repairOrHalt(ctx, task, project, out)
default: // VerdictBlocked
    return r.haltVerify(ctx, task, project, out.Verdict.Summary)
}
```

### The fresh attempt + anti-gaming gate (D-05 + D-08)
```go
// Source: grounded in run_evidence.go:70 (ChangedFiles) + task_controller.go:725 (nextAttempt) (VERIFIED)
func (r *TaskReconciler) repairOrHalt(ctx context.Context, task *T, project *P, out dispatch.EnvelopeOut) (ctrl.Result, error) {
    // D-08 anti-gaming: a fresh attempt that touched evaluator/fixture/threshold paths is a
    // SYSTEM ESCALATION, never a pass and never a plain repair.
    if out.RunEvidence != nil && intersectsProtected(out.RunEvidence.ChangedFiles, protectedPathsFor(task)) {
        return r.escalateSystem(ctx, task, project, "attempt modified evaluator/fixtures")
    }
    if int32(task.Status.Attempt) >= task.Spec.Verification.MaxIterations {
        return r.haltVerify(ctx, task, project, "maxIterations exhausted") // → ConditionVerifyHalt
    }
    stageEvidencePacket(ctx, out) // D-04: bounded failures/diffs/findings → PVC; path → EvidencePacketPath
    // D-05: quality-iteration mints a NEW attempt (distinct from the eviction rerun path).
    return r.redispatchExecutor(ctx, task, project) // Attempt++, locked spec + packet
}
```

### Populating VerifyContext at verifier dispatch (D-01/D-04)
```go
// Source: grounded in envelope.go:434 (VerifyContext) + buildEnvelopeIn shape task_controller.go:1831 (VERIFIED)
envIn := dispatch.EnvelopeIn{
    APIVersion: dispatch.APIVersionV1Alpha1, Kind: dispatch.KindTaskEnvelopeIn,
    TaskUID: string(task.UID), Role: "verifier", Level: "task",
    LoopRunID: string(task.UID),                                   // outer loop = taskUID (Phase 50 D-01)
    AttemptID: fmt.Sprintf("%s-%d", task.UID, task.Status.Attempt),
    Provider:  dispatch.ProviderSpec{Vendor: "langgraph", Model: ResolveProvider(project, "task", defs).Model},
    Verify: &dispatch.VerifyContext{
        GateCommand:        task.Spec.Verification.GateCommand,        // the LOCKED command (git-show-reproducible)
        RequiredArtifacts:  task.Spec.Verification.RequiredArtifacts,
        EvaluatorRef:       task.Spec.Verification.Evaluator,
        EvidencePacketPath: evidencePacketPath,                        // "" on first (non-repair) verify
    },
}
```

### Registering the langgraph vendor (D-02, OBS-03)
```go
// Source: pkg/dispatch/vendor_capabilities.go:38 (VERIFIED — every case returns false today)
func SelfInstruments(vendor string) bool {
    switch vendor {
    case "langgraph":
        return true // self-instruments via openinference-instrumentation-langchain; reporter skips events.jsonl synthesis
    case "anthropic", "openai", "google", "xai", "opencode":
        return false
    default:
        return false // fail-closed
    }
}
```

## State of the Art

| Old Approach (current HEAD) | Phase 51 Approach | Impact |
|-----------------------------|-------------------|--------|
| Executor exit-0 → Task `Succeeded` directly (handleJobCompletion:1424) | Executor exit-0 → dispatch independent verifier → verdict decides | The Execution loop stops stamping correctness (EXEC-04 realized) |
| Blind `maxAttemptsPerTask` quality retry (:729) | Evaluator-driven fresh attempts bounded by `maxIterations`, eviction-retry preserved | Retries carry evidence, not blind reruns (D-05) |
| Verifier image buildable but dispatched nowhere; `SUPPORTED_VENDOR="anthropic"` | Verifier dispatched by the Task loop; vendor sentinel `"langgraph"` | The Phase-48 image finally runs in-loop |
| `evaluation.*`/`human_intervention` span keys defined-but-empty (attrs.go:413) | First real population via the EVALUATOR span | The trace tree gains loop-native evaluator provenance |
| Project planner dispatch has no `checkFailureHalt` (project_controller.go:1540); Task Import-order divergence | Unified hold chain across all five levels | Folds two carried-forward W-2 dispatch-gate todos |

**Deprecated/outdated:**
- The pre-2026-07-18 "three verify stages" framing is superseded by the single `LoopPolicy`-parameterized loop (Phase 49 CONTEXT). The `VerifyContext` three-`Stage` field was already dropped; do not reintroduce per-stage config.

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|-------|---------|---------------|
| A1 | A dedicated `JobKindVerifier` (vs. reusing `ReadOnly` on `JobKindExecutor`) is the cleaner path for verify caps + `role=verifier` concurrency labeling | Standard Stack / Alternatives | Low — either works; a dedicated kind is more grep-distinct. Planner may choose the `ReadOnly`-flag route if verify caps == executor caps |
| A2 | The governing `VerificationPhase`/`version` should live on `spec.verification` (not status) so the CEL immutability rule is expressible | Pattern 2 / Pitfall 2 / OQ1 | Medium — if the planner insists on status-only per a literal D-03 reading, the CEL immutability becomes inexpressible and needs a webhook (which the project forbids). Resolve at plan time |
| A3 | The verifier reuses the executor's credproxy sidecar + signed-token path unchanged (Phase 48 posture) | Runtime State Inventory | Low — Phase 48 built the RO verifier Job with the same credproxy pattern; confirm the token is minted for the verifier dispatch too |
| A4 | The Task gains a new intermediate phase (e.g. `Verifying`) between executor-complete and terminal | Architecture Diagram | Medium — introduces a new `LevelPhase` value or a Task-local sub-state; the planner must decide whether to reuse `Running` with a condition or add a phase. Affects `gateChecks` terminal short-circuit (:336) |
| A5 | The verifier image ref reaches the manager via a new `Deps` field/flag in Phase 51 (chart surface is Phase 53) | Runtime State Inventory | Low — a dev-head default flag suffices for the kind proof; CFG-01 formalizes it in Phase 53 |
| A6 | The empty/absent `verification` posture defaults to "skip verify, preserve exit-0→Succeeded" for backward-compat unless a contract is present | Runtime State / OQ2 | Medium — the alternative (fail-closed require-a-contract) changes behavior for every existing Task. Resolve at plan time |

## Open Questions

1. **Where does `VerificationPhase`/`version` live — spec or status?**
   - What we know: D-03 says "recorded on Task.Status"; CEL immutability needs the governing enum readable by a spec transition rule (Pitfall 2).
   - What's unclear: whether to honor the literal status placement (then immutability needs a different mechanism) or move the governing enum to `spec.verification` (recommended) and keep only `lockedSHA` on status.
   - Recommendation: governing `phase`/`version` on `spec.verification`; `lockedSHA` (a runtime observation) on `Task.Status`. Flag to the planner as an explicit fork; it is within D-03's "exact CEL rule spelling is Claude's discretion".

2. **Empty/absent-`verification` posture for existing Tasks.**
   - What we know: fields are `+optional`; old Tasks have none; the milestone default posture is "off on in-place upgrade" (CFG-02, Phase 53).
   - What's unclear: whether Phase 51's controller skips verify (preserve exit-0→Succeeded) or fail-closes when no contract is present.
   - Recommendation: skip verify when `verification` is absent (backward-compatible; CFG-02's off-on-upgrade posture lands in Phase 53). A Task WITH a contract always verifies.

3. **How does the verifier capture the deterministic gate exit out-of-band (Pitfall 1)?**
   - What we know: `run_gate_command` returns the exit as tool text; D-06 requires the exit to dominate regardless of the LLM.
   - What's unclear: run-the-gate-in-the-entrypoint-once vs. structured tool-return interception.
   - Recommendation: entrypoint runs the gate command deterministically (it is the same `TIDE_GATE_COMMAND`), captures returncode, and the verdict assembly forces non-APPROVED on non-zero — the LLM's verdict is advisory over the machine result. Emit a `blocker`/`gate-command` finding carrying `exit_code=N` so the controller-side re-check (D-06) has a structural signal.

4. **`verifierInFlightCount` cap default (single-node-safe).**
   - What we know: run-2b OOMed a single-node cluster; planner cap default is 4 (configmap_planner_concurrency_test.go).
   - Recommendation: a small default (e.g. 2–4, Claude's discretion per D-10) that, combined with the executor cap, stays within a single kind node's memory. The kind concurrent-dispatch test pins it.

## Environment Availability

| Dependency | Required By | Available | Version | Fallback |
|------------|------------|-----------|---------|----------|
| Go toolchain | All Go changes | ✓ | 1.26 (STACK.md) | — |
| controller-gen (`make manifests`/`generate`) | CRD + deepcopy regen | ✓ | kubebuilder v4.14.0 bundled | — |
| envtest | Layer A reconciler tests | ✓ (local green per MEMORY) | setup-envtest | — |
| kind | ESC-04 concurrent-dispatch proof + live Success Criterion | ✓ | v0.31 | — (single-node OOM discipline: delete→recreate→prewarm per heavy run, CLAUDE.md) |
| helm | chart render tests (image-pin/concurrency) | ✓ | Helm 3 | — |
| Python + uv (verifier image tests) | verdict-assembly + parity tests | ✓ | astral-sh/setup-uv (Phase 48 recipe) | golang:1.26.3 Linux container for live make eval (macOS SSL_CERT_FILE gap, MEMORY) |
| `tide-langgraph-verifier` image | live verify on kind | Build path exists | Phase 48 dev-head | rebuild via CI; kind-load for local |
| Real Anthropic key | live end-to-end verify on kind (billable) | Operator-supplied | `~/.tide/anthropic.key` (durable, outside repo) | envtest + stub-model for CI; live run is a human checkpoint |

**Missing dependencies with no fallback:** None — every tool is present locally.
**Missing dependencies with fallback:** The live billable verify (real key) is a human-checkpoint step; automated coverage uses envtest + a fake chat model (the verifier's `run_agent_fn`/`build_model` injectable seams, __main__.py:74-75) for offline flow tests.

## Validation Architecture

> `workflow.nyquist_validation: true` — this section is required.

### Test Framework
| Property | Value |
|----------|-------|
| Framework (Go) | Ginkgo v2.28 + Gomega (envtest, Layer A); plain go-test for helm-render + pkg unit tests |
| Framework (Python) | pytest (verifier image; Phase 48 recipe via uv) |
| Config file | `test/integration/kind/` (kind Layer B); envtest suite in `internal/controller/` |
| Quick run command | `go test ./internal/controller/... ./pkg/dispatch/... ./api/...` (unit + envtest) |
| Full suite command | `make test-int` (Layer A envtest + Layer B kind + helm-render contract tests) |

### Phase Requirements → Test Map
| Req ID | Behavior | Test Type | Automated Command | File Exists? |
|--------|----------|-----------|-------------------|-------------|
| TASK-01 | Locked `spec.verification` is immutable; Draft mutable; Superseded escape | envtest (admission) | `go test ./internal/controller/... -run VerificationImmutab` | ❌ Wave 0 |
| TASK-01 | `git show <lockedSHA>` reproduces dispatched contract | envtest + unit | verify `lockedSHA` stamped on status at dispatch | ❌ Wave 0 |
| TASK-02 | REPAIRABLE → fresh attempt seeded with locked spec + evidence packet | envtest | `go test ./internal/controller/... -run RepairableFreshAttempt` | ❌ Wave 0 |
| TASK-03 | eviction rerun (same attemptID) ≠ quality-iteration (new attemptID) | envtest | `-run InfraRetryVsQualityIteration` | ❌ Wave 0 |
| TASK-04 | deterministic non-zero gate dominates LLM APPROVED | unit (Go verdict re-check) + pytest (verifier assembly) | `go test ./pkg/dispatch/...`; `pytest .../tests/test_verdict.py` | Partial (verdict tests exist; add dominance case) |
| TASK-05 | `maxIterations` bound + resume across restart re-derives | envtest | `-run VerifyLoopResumeAfterRestart` | ❌ Wave 0 |
| TASK-06 | attempt editing evaluator/fixtures → system escalation, never a pass | envtest | `-run AntiGamingProtectedPathEscalation` | ❌ Wave 0 |
| EVAL-04 | `task_verifier.tmpl` loads via `LoadPromptTemplate("verifier","task")`; coverage-not-conservatism content | unit | `go test ./internal/subagent/common/... -run VerifierTemplate` | ❌ Wave 0 |
| ESC-02 | `ConditionVerifyHalt` set on exhaustion; gates planner + task tiers | envtest | `-run VerifyHaltGatesBothTiers` | ❌ Wave 0 |
| ESC-03 | VerifyHalt leaves phase/wave-siblings/conservative-profile untouched | envtest | `-run VerifyHaltDistinctFromFailed` | ❌ Wave 0 |
| ESC-04 | verifier counted vs. concurrency cap; BudgetCents bounds cost | kind (Layer B) | `make test-int` (concurrent-dispatch spec under cap) | ❌ Wave 0 (mirror configmap_planner_concurrency_test.go + a live Ginkgo concurrency spec) |
| OBS-03 | `SelfInstruments("langgraph")=true`; EVALUATOR sibling span, no double-emit | unit + envtest | `go test ./pkg/dispatch/... -run SelfInstruments`; span_emission test asserts `EVALUATOR` kind + sibling parent | Partial (span_emission_test.go exists; add EVALUATOR case) |
| D-09 (fold) | co-occurring holds fire uniform hold across levels | envtest | `-run CoOccurringDispatchHolds` | ❌ Wave 0 |

### Sampling Rate
- **Per task commit:** `go test ./internal/controller/... ./pkg/dispatch/... ./api/...` (fast unit + envtest for the touched package).
- **Per wave merge:** `make test-int` (Layer A + Layer B + helm render) — remember `MAKE_EXIT` ≠ Ginkgo-green (CLAUDE.md: plain go-tests bundled in the kind package can fail the package while Ginkgo prints SUCCESS).
- **Phase gate:** Full suite green + the live kind verify (real key, human checkpoint) before `/gsd:verify-work`.

### Wave 0 Gaps
- [ ] `internal/controller/verification_immutability_test.go` — TASK-01 CEL admission (envtest against a real API server; CEL runs only there, not in fake-client unit tests).
- [ ] `internal/controller/task_verify_loop_test.go` — TASK-02/03/05/06 the verify sub-state-machine (fresh attempt, infra-vs-quality, resume, anti-gaming).
- [ ] `internal/controller/verify_halt_test.go` — ESC-02/03 mirror of the failure_halt tests.
- [ ] `internal/controller/co_occurring_holds_test.go` — the D-09 gate-order-unification proof (folds both todos).
- [ ] `test/integration/kind/verifier_concurrency_test.go` — ESC-04 live concurrent-dispatch under cap (Ginkgo, mirrors chaos_resume_test.go's real-Job shape).
- [ ] `pkg/dispatch/` + `cmd/tide-langgraph-verifier/verifier/tests/` — extend verdict tests with the D-06 deterministic-dominance case (Go re-check + Python assembly).
- [ ] Framework install: none — Ginkgo/Gomega/envtest/pytest all present.

## Security Domain

> `security_enforcement` is absent from config → treated as enabled. This phase adds a shell-command execution surface (the gate command) and an anti-gaming trust boundary — real security relevance.

### Applicable ASVS Categories

| ASVS Category | Applies | Standard Control |
|---------------|---------|-----------------|
| V1 Architecture | yes | The evaluator is a **logically independent, read-only process** (RO worktree mount + git-cred omission, Phase 48 D-08) — enforced structurally, not by prompt |
| V5 Input Validation | yes | CEL `x-kubernetes-validations` on `spec.verification` (immutability + enum constraints); `ClassifyVerdict` fail-closed on malformed verdict |
| V6 Cryptography | no (reuse) | Verifier reuses the existing credproxy signed-token (HMAC-SHA256) path — no new crypto |
| V10 Malicious Code / Integrity | yes | Anti-gaming: `RunEvidence.ChangedFiles` ∩ protected-path set flags an attempt that weakens the evaluator; `TIDE_GATE_COMMAND` is orchestrator-set, never model-supplied |
| V12 Business Logic | yes | Deterministic-dominates-judge is a business-logic integrity control (a red gate can never be approved by a probabilistic judge) |

### Known Threat Patterns for this stack

| Pattern | STRIDE | Standard Mitigation |
|---------|--------|---------------------|
| LLM judge overrides a red gate (silent-Complete) | Tampering / Repudiation | Out-of-band deterministic capture + controller-side re-check (D-06, §Pitfall 1) — the core threat this milestone closes |
| Agent edits the evaluator/fixtures to force a pass | Tampering | `ChangedFiles` ∩ protected paths → system escalation, never a pass (D-08); regression test proves it |
| Command injection via `gateCommand` | Elevation / Tampering | `TIDE_GATE_COMMAND` is orchestrator-set from the **locked, planner-authored** contract (immutable once Locked), never a model-supplied tool argument (tools.py:157 `# noqa: S602` is justified by exactly this invariant). The gate runs in the RO verifier pod, not the manager |
| Verifier gains write/push capability | Elevation | RO worktree mount + no git-write/push creds in env (Phase 48 EVAL-01, adversarial commit/push fixture test) — Phase 51 must not add write tools |
| Cost/concurrency DoS (attempts × evaluator × levels) | DoS | `verifierInFlightCount` cap + `LoopPolicy.BudgetCents` reservation + `maxIterations` bound → `ConditionVerifyHalt` (ESC-04, the run-2b OOM guard) |
| Evidence packet / verdict blowing etcd | DoS | `RunEvidence.Bounded()` + `LoopStatus` current-iteration-only + findings on run branch, never etcd blob (Phase 49/50 discipline) |

## Sources

### Primary (HIGH confidence — read at HEAD this session)
- `internal/controller/failure_halt.go` (:56,:79) — the `verify_halt.go` clone template + Phase-25 resume time-fence
- `internal/controller/task_controller.go` (:283 reconcileDispatch, :334 gateChecks, :391-402 divergence comment, :620 checkRunningState, :723 prepareDispatch/:725 nextAttempt, :1236-1472 handleJobCompletion incl. :1424 exit-0→Succeeded, :1117 SelfInstruments call site, :107-109 Reservations)
- `internal/controller/dispatch_helpers.go` (:282 ResolveProvider, :418 resolveImage, :490 plannerInFlightCount, :580 checkDispatchHolds incl. checkFailureHalt at :596)
- `internal/controller/git_writer.go` (:100 gitWriterInFlightCount)
- `internal/controller/project_controller.go` (:1540 planner dispatch block — no checkFailureHalt, folds todo #1)
- `internal/controller/milestone_controller.go` (:344 D3 in-flight cap gate shape)
- `internal/controller/span_emission.go` (:156 synthesizePlannerSpan AGENT span, :242 LoopAttributes gating)
- `pkg/dispatch/verdict.go` (Verdict/GateDecision/Finding/ClassifyVerdict fail-closed, `highSeverityFindingToken="blocker"`)
- `pkg/dispatch/envelope.go` (:60 Role, :174 Verify, :283 Verdict, :289 RunEvidence, :434-454 VerifyContext, :471-561 TerminationStub/NewTerminationStub)
- `pkg/dispatch/run_evidence.go` (RunEvidence/ChangedFiles/Bounded, MaxRunEvidenceChangedFiles=100)
- `pkg/dispatch/provider.go` (:36-40 ProviderSpec.Vendor canonical set)
- `pkg/dispatch/vendor_capabilities.go` (:38 SelfInstruments — all cases false today)
- `pkg/otelai/attrs.go` (:240 AgentInvocation, :320 LLMSpanKind, :441 LoopAttributes, :480 EvaluationAttributes, :490 HumanIntervention, :92 tide.* key guard)
- `api/v1alpha3/task_types.go` (TaskSpec, TaskStatus.Attempt:157, Gates precedent :122)
- `api/v1alpha3/loop_types.go` (LoopPolicy/LoopStatus/EvaluationSummary/ExitReason/EscalationPolicy)
- `api/v1alpha3/shared_types.go` (:264-335 BillingHalt/FailureHalt condition+reason+annotation vocabulary; :382 FailureProfileType; :439 LevelPhase constants)
- `api/v1alpha3/project_types.go` (:37-52 Gates/GatePolicy precedence precedent)
- `internal/subagent/common/prompt_templates.go` (:80 LoadPromptTemplate + `<level>_<role>.tmpl`; templates/ has 5, no verifier yet)
- `internal/subagent/anthropic/subagent.go` (:62 vendorSentinel + :219 fail-fast refusal — the D-02 image-refusal precedent)
- `internal/dispatch/podjob/jobspec.go` (:188 ReadOnly verifier variant), `caps.go` (:33 JobKind), `names.go` (:37 JobName tuple)
- `cmd/tide-langgraph-verifier/verifier/` — `__main__.py` (SUPPORTED_VENDOR="anthropic":30, injectable seams:74, writes exit_code only — no verdict yet), `tools.py` (:56 TIDE_GATE_COMMAND, :126 run_gate_command fail-closed), `agent.py` (bare shell, system_prompt passthrough), `verdict.py` (GateDecision/classify_verdict ported)
- OpenInference Go semconv module `@v0.1.1/enums.go` — `SpanKindEvaluator = "EVALUATOR"` (VERIFIED present)
- `.planning/{REQUIREMENTS.md, STATE.md, ROADMAP.md §Phase 51, notes/five-loop-model.md}`, `51-CONTEXT.md`, `49-CONTEXT.md`, `50-CONTEXT.md`

### Secondary (MEDIUM confidence — verified with official docs)
- Kubebuilder CRD-validation markers — CEL `XValidation` immutability (`self == oldSelf`) and transition-rule semantics (Context7 `/websites/book_kubebuilder_io`)

### Tertiary (LOW confidence, flagged for validation)
- None — all claims are grounded in read-at-HEAD source or official docs.

## Metadata

**Confidence breakdown:**
- Standard stack (in-repo seams): HIGH — every seam read at HEAD with file:line
- Architecture (verifier dispatch sub-state-machine): HIGH for the seams; MEDIUM for the exact new-phase shape (A4/OQ — a genuine design choice the planner makes)
- CEL immutability: HIGH on the mechanism, MEDIUM on the spec-vs-status placement (OQ1 — needs a plan-time decision, flagged)
- Deterministic dominance enforcement point: MEDIUM — two viable approaches (OQ3); recommendation given
- Pitfalls: HIGH — grounded in the documented divergence comments + the milestone's stated failure mode

**Research date:** 2026-07-19
**Valid until:** 2026-08-18 (30 days — stable internal codebase; re-verify HEAD line numbers if the branch advances significantly)
