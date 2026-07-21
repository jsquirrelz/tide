# Phase 51: The Task Loop - Context

**Gathered:** 2026-07-19
**Status:** Ready for planning

> **Mode:** `--auto` (auto-advance), with a deliberate exception. The two
> **genuinely-open architectural calls the ROADMAP itself flagged** ("must be
> decided during `/gsd:plan-phase 51`, not discovered mid-execution" — STATE.md
> Blockers/Concerns) were **asked interactively** — they are Option-A-vs-B
> scope-defining forks with "no existing source," exactly the class the project
> CLAUDE.md reserves for a human call even under auto-advance. Both resolved to
> the recommended option (D-01, D-02). Every other decision (D-03…D-12) is
> auto-resolved to its recommended default, grounded in ROADMAP §Phase 51 (5
> success criteria), REQUIREMENTS TASK-01..06 / EVAL-04 / ESC-02/03/04 / OBS-03,
> the v1.0.9 binding constraints (PROJECT.md / STATE.md / `f85ee3d`), the
> [five-loop model](../../notes/five-loop-model.md) §"Task loop", the committed
> TASK-template contract, and a live seam scout of the actual verdict / envelope /
> halt-gate / concurrency-gate / vendor-capabilities / prompt-template code.

<domain>
## Phase Boundary

Build **the Task loop** — the highest-value addition of v1.0.9 and the first real
feedback loop TIDE closes. `TaskReconciler` (`internal/controller/task_controller.go`)
gains a verification-driven quality loop:

1. A **planner-authored, immutable-once-locked verification contract** on
   `TaskSpec.verification` (`gateCommand`, `commands`, `requiredArtifacts`,
   `evaluator`, `maxIterations`, `onExhaustion`) — `git show <locking-sha>`
   reproduces exactly what was dispatched (TASK-01).
2. An **independent** read-only LangGraph evaluator (the Phase-48 image) is
   dispatched against the **real gate command** — exit code parsed, never
   self-reported — and returns a `gate_decision` verdict (Phase-49 schema).
3. A **REPAIRABLE** verdict creates a **fresh attempt** seeded with the original
   locked spec + a **compact evidence packet** (failures/diffs/test output), never
   the prior agent's full context — while **infra-retry** (eviction/transient
   rerun of the *same* attempt) stays a distinct, preserved path (TASK-02/03).
4. A **deterministic gate-command failure dominates** any LLM-judge APPROVED — a
   Task can never pass on a judge's word over a red command (TASK-04).
5. The loop is **bounded by `maxIterations`**; `onExhaustion` routes to a new
   **`ConditionVerifyHalt`** (mirroring `failure_halt.go` + Phase-25's resume
   time-fence), a **distinct halt class** from `Failed` wave semantics, gating
   **both** the planner tier (`checkDispatchHolds`) and the task tier
   (`gateChecks`); state is **resumable across a controller restart** (TASK-05,
   ESC-02/03).
6. A fresh attempt that **edits fixtures/thresholds/the evaluator itself** is
   flagged as a **system escalation, never counted as a pass** — the anti-gaming
   invariant is **enforced structurally**, not documented (TASK-06).

**Cross-cutting safety lands in THIS phase, with the dispatch sites** (the
research's most-repeated instruction — do not defer to a follow-up):

- **Concurrency accounting** (ESC-04): evaluator dispatches count against the
  Phase-32 concurrency gate, and `LoopPolicy.BudgetCents` bounds cost via the
  existing `ReservationStore`.
- **`SelfInstruments` registration + `EVALUATOR` span** (OBS-03): the new
  `"langgraph"` vendor registers so the reporter skips `events.jsonl` synthesis,
  and the evaluator emits a distinct `EVALUATOR`-kind span **sibling** to the
  checked `AGENT` span — no double-emission.
- **Verifier prompt template** (EVAL-04): a `role="verifier"` orchestrator-side Go
  template (no Python port), **coverage-not-conservatism**.

**Deliberately NOT in this phase:**

- **Per-level verification at Plan / Phase / Milestone / Project** (the same
  contract parameterized by `LoopPolicy` — plan-check re-plan `maxIterations:1`,
  Phase/Milestone/Project escalate `maxIterations:0`) → **Phase 52** (ESC-01). This
  phase adds `verification` to **`TaskSpec` only**; the `Plan.Spec`/`Project.Spec`
  fields + the resolution precedence are Phase 52 (D-01).
- **Chart-first config surface + default posture** (evaluator image/model + per-level
  `LoopPolicy` defaults, off-on-upgrade) → **Phase 53** (CFG-01/02).
- **Dashboard nested-provenance + `VerifyHalt` visual state** → **Phase 53** (OBS-04).

Success = a Task whose locked gate command fails is auto-repaired by an independent
evaluator up to `maxIterations`, then halts on `ConditionVerifyHalt` — proven live
on a kind cluster, with the evaluator counted against the concurrency cap and its
cost bounded by the reservation store.

</domain>

<decisions>
## Implementation Decisions

### The two flagged open calls — LOCKED (asked interactively)

- **D-01 (GateCommand schema location = explicit planner-authored field on
  `TaskSpec.verification`, Task-scoped now — LOCKED):** Add a `verification` block
  to **`TaskSpec` only** in Phase 51 — `gateCommand` (→ resolves onto the existing
  `pkg/dispatch/envelope.go` `VerifyContext.GateCommand` wire field), plus
  `commands`, `requiredArtifacts`, `evaluator`, `maxIterations`, `onExhaustion`.
  It is **planner-authored data**, **immutable once locked** (Draft→Locked→Superseded
  + version), and `git show <lockedSHA>` reproduces exactly what was dispatched.
  The identical shape **generalizes to `Plan.Spec`/`Project.Spec` in Phase 52** with
  a Task > Plan > Project resolution precedence (mirroring `ResolveProvider` / the
  existing `Gates` precedence). *Rejected: (a) declaring the field at all levels
  now — front-loads Phase-52 schema churn into a Task-focused phase; (b) a
  convention-based repo lookup (`.tide/gate.sh` / `make verify-<level>`) — it
  cannot satisfy TASK-01's "immutable, locked, git-show-reproduces-what-was-
  dispatched" because a repo convention target can drift between lock and dispatch,
  and it is not planner-authored.*

- **D-02 (LangGraph runtime = a NEW `"langgraph"` vendor sentinel — LOCKED):**
  Register `"langgraph"` as a new `ProviderSpec.Vendor` literal.
  `SelfInstruments("langgraph") → true` (the Phase-48 image self-instruments via
  `openinference-instrumentation-langchain`, so the reporter **skips**
  `events.jsonl` synthesis for it); the verifier image refuses `Provider.Vendor !=
  "langgraph"` at startup. This keeps `SelfInstruments(vendor string) bool` a **pure
  vendor predicate** — no signature change to the Phase-45 ADAPT-01 seam (whose D-02
  had the reporter trust the manager-computed boolean carried on the Job; a new
  vendor value flows through cleanly). It matches **existing precedent**: `"opencode"`
  is already a runtime/wrapper Vendor value, not a pure LLM vendor. Model still
  resolves via `ResolveProvider` (Vendor=`langgraph`, Model=`claude-…` as a normal
  per-level config). *Rejected: reusing `"anthropic"` + a runtime discriminator — it
  forces `SelfInstruments` to take an extra arg (ADAPT-01 seam churn) and makes the
  image-startup refusal sentinel ambiguous (two images would accept `Vendor=anthropic`).*

### Verification contract lifecycle & locking (TASK-01)

- **D-03 (Draft→Locked→Superseded + version + `lockedSHA` on `Task.Status`):**
  Model the immutability as a `VerificationPhase` enum (Draft / Locked / Superseded)
  + a monotonic version + the locking commit SHA, recorded on `Task.Status`. Enforce
  "immutable once locked" with a CEL `x-kubernetes-validations` rule (the project's
  established validation mechanism — no admission webhook) so a Locked
  `spec.verification` cannot mutate; a spec change mints a Superseded→new-version
  transition. A dispatched attempt records the `lockedSHA` so `git show` reproduces
  it — reusing the run-branch/git-artifact-store already staging findings (D-04).

### Fresh attempt & the compact evidence packet (TASK-02/03)

- **D-04 (compact evidence packet via the existing `VerifyContext.EvidencePacketPath`):**
  A REPAIRABLE verdict stages a **bounded** evidence packet (relevant failures/diffs/
  test output from `RunEvidence` + the verdict `findings[]`) to the PVC and passes its
  path via the already-present `EnvelopeIn.Verify.EvidencePacketPath` (envelope.go:453);
  the fresh attempt re-uses the **original locked spec** + this packet, never the prior
  agent's full context. Keep the packet reference-only/bounded, consistent with the
  Phase-50 `RunEvidence.Bounded()` discipline.
- **D-05 (infra-retry ≠ quality-iteration — two distinct paths):** The eviction/
  transient rerun path (same `attemptID`, no evaluator feedback) is **preserved
  as-is**; **quality-iteration** mints a **new attempt** (increments
  `Task.Status.Attempt` → new `attemptID` tuple) seeded by D-04. The blind
  `maxAttemptsPerTask` quality-retry is **superseded** by evaluator-driven attempts
  (not the eviction path). The two must remain grep-distinguishable in the controller.

### Deterministic dominance & anti-gaming (TASK-04/06)

- **D-06 (deterministic gate-command exit dominates the judge — structural):** The
  gate command's real exit code is authoritative: a non-zero gate exit forces
  REPAIRABLE/BLOCKED **regardless** of any LLM-judge APPROVED, enforced in the
  evaluator's verdict assembly (`verifier/`) **and** re-checked controller-side when
  consuming `EnvelopeOut.Verdict` (defence in depth). Reuse the Phase-49 fail-closed
  `ClassifyVerdict` (`pkg/dispatch/verdict.go`) — an unparseable/empty verdict already
  routes to BLOCKED, never APPROVED.
- **D-08 (three-tier escalation + anti-gaming invariant — enforced, not documented):**
  *fresh attempt* (REPAIRABLE → back to the loop), *system escalation* (a fresh
  attempt whose **changed-file manifest** — the Phase-50 `RunEvidence.ChangedFiles` —
  **intersects evaluator/fixture/threshold paths** is flagged systemic, **never
  counted as a pass**), *human decision* (`onExhaustion: requireApproval`). The
  fixture/evaluator/threshold path set is the structural detector; a regression test
  proves an attempt that edits the evaluator to pass is flagged, not passed.

### Bounding, halting & resumability (TASK-05, ESC-02/03)

- **D-07 (bound + resume via `LoopStatus`, re-derived not persisted-as-history):**
  `maxIterations` bounds the loop; iteration/cost state lives in `LoopStatus` on
  `Task.Status` as the **current-iteration summary + exit reason only** (LOOP-03 — no
  accumulating history), re-derivable across a controller restart from
  `Task.Status.Attempt` + the completed-set, matching the project's "resumption =
  re-derive, never persist the schedule" principle.
- **D-09 (`ConditionVerifyHalt` clones `failure_halt.go`, gates BOTH tiers, distinct
  class — and unifies the dispatch-hold chains):** Add `verify_halt.go` mirroring
  `failure_halt.go` **file-for-file** (`checkVerifyHalt` ↔ `checkFailureHalt:56`,
  `setVerifyHaltIfNeeded` ↔ `setFailureHaltIfNeeded:79` **including Phase-25's resume
  time-fence**). Wire `checkVerifyHalt` into **`checkDispatchHolds`
  (dispatch_helpers.go:580)** (planner tier) **and** **`TaskReconciler.gateChecks`
  (task_controller.go:334)** (task tier). ESC-03: a regression test asserts a
  VerifyHalt leaves the checked level's phase, wave siblings, and conservative-profile
  propagation **untouched** — it is never a reinterpretation of `Failed` wave
  semantics. **Fold the two dispatch-gate todos here** (see Folded Todos): since this
  phase edits exactly these chains, migrate `gateChecks` onto `checkDispatchHolds`
  (normalizing Task's Import-position divergence) **and** add the missing
  `checkFailureHalt`/`checkVerifyHalt` gate to the **Project** planner chain, so all
  five dispatch chains carry a **uniform** hold order — a structural fix (shared chain
  > per-site divergence), gated behind a co-occurring-holds envtest per the todos'
  Option 1 (a deliberate, tested behavior change, not a silent shift).

### Concurrency & cost (ESC-04)

- **D-10 (dedicated `verifierInFlightCount` + `LoopPolicy.BudgetCents` via
  `ReservationStore`):** Add a **new** `verifierInFlightCount` (mirroring
  `gitWriterInFlightCount`/`plannerInFlightCount` shape, `git_writer.go:100` /
  `dispatch_helpers.go:490`) rather than overloading `plannerInFlightCount` — verifier
  pods are a **distinct pool** (the spec sizes planner/executor/verifier pools
  separately) and count against the Phase-32 concurrency gate at the Task dispatch
  site. `LoopPolicy.BudgetCents` bounds evaluator cost through the existing
  `budget.ReservationStore` (task_controller.go:109). Proven by a **kind-cluster
  concurrent-dispatch test** that stays under the sized cap (guards the run-2b D3
  single-node OOM).

### Loop-native observability for the evaluator (OBS-03)

- **D-11 (`EVALUATOR`-kind span, sibling to the checked `AGENT` span):** With
  `SelfInstruments("langgraph")=true` (D-02) the reporter skips synthesis; the
  evaluator emits a distinct OpenInference `EVALUATOR`-kind span parented as a
  **sibling** of the checked level's `AGENT` span (no double-emission into the v1.0.8
  trace tree). Populate the `evaluation.result` / `evaluation.version` /
  `human_intervention` keys **defined-but-empty in Phase 50** (`pkg/otelai`) — this is
  their first real consumer.

### Verifier prompt (EVAL-04)

- **D-12 (`role="verifier"` compiled-in Go template, coverage-not-conservatism):**
  Add a `templates/task_verifier.tmpl` to the compiled-in family
  (`internal/subagent/common/prompt_templates.go:80` `LoadPromptTemplate(role,
  level)`; the `<level>_<role>.tmpl` convention) — **no Python port** (prompts render
  orchestrator-side). Prompt for **coverage** (emit a finding for *every* deviation
  with severity + confidence tags); **config/policy alone decides what blocks** — per
  the Opus-4.8 tuning note, do NOT prompt "be conservative / only high-severity"
  (it drops real low-severity findings).

### Claude's Discretion
- Exact Go field names / JSON tags / CEL rule spelling for `TaskSpec.verification`
  and the `VerificationPhase`/version/`lockedSHA` status fields — within D-01/D-03.
- Whether `verify_halt.go` shares helper code with `failure_halt.go` or stays a
  hand-synced clone (the metriccardinality precedent favors deliberate non-sharing of
  guard layers) — within D-09.
- The precise evidence-packet serialization + bounding thresholds — within D-04's
  bounded/reference-only decision.
- The `EVALUATOR`-span attribute set beyond the OBS-01 loop/evaluation keys — within
  D-11.
- Exact `verifierInFlightCount` cap default (single-node-safe, Phase-32 shape) —
  within D-10.

### Folded Todos
Both fold **because this phase edits exactly the dispatch-hold chains they concern**
(ESC-02 wires `ConditionVerifyHalt` into `checkDispatchHolds` + `gateChecks`) — STATE.md
flags both as "relevant to v1.0.9's `ConditionVerifyHalt` gate-order work, Phase 51":

- **`2026-07-12-project-dispatch-missing-failurehalt-gate`** (score 0.6) —
  `ProjectReconciler`'s planner-dispatch chain has **no `checkFailureHalt`** gate, so a
  conservative-profile project-wide halt freezes Milestone/Phase/Plan/Task but the
  Project-level planner still dispatches (spends). **Fold via D-09 Option 1:** add the
  gate (and `checkVerifyHalt`) to the Project chain while wiring VerifyHalt, with a
  conservative-profile envtest.
- **`2026-07-12-task-dispatch-gate-order-divergence`** (score 0.6) — `gateChecks`
  checks Import **second** while Milestone/Phase/Plan (via `checkDispatchHolds`) check
  it **last**, so the hold that fires under co-occurring holds differs by level.
  **Fold via D-09 Option 1:** migrate `gateChecks` onto `checkDispatchHolds`, adding
  the new VerifyHalt in one uniform order, gated behind a co-occurring-holds envtest.

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### Requirements & roadmap (scope authority)
- `.planning/REQUIREMENTS.md` — **TASK-01** (verification contract, planner-authored,
  immutable-locked), **TASK-02** (REPAIRABLE → fresh attempt + compact evidence packet),
  **TASK-03** (infra-retry ≠ quality-iteration), **TASK-04** (deterministic dominates
  judge), **TASK-05** (maxIterations bound + resumable), **TASK-06** (three-tier
  escalation + anti-gaming), **EVAL-04** (verifier Go template, coverage-not-
  conservatism), **ESC-02** (`ConditionVerifyHalt` mirrors failure_halt.go, gates both
  tiers), **ESC-03** (distinct halt class), **ESC-04** (concurrency gate + BudgetCents),
  **OBS-03** (SelfInstruments registration + EVALUATOR sibling span).
- `.planning/ROADMAP.md` §"Phase 51" — goal + 5 success criteria this CONTEXT locks
  against; the **Research flag** naming the two open calls (D-01/D-02). §"Phase 52"
  confirms per-level (Plan/Phase/Milestone/Project) verification is Phase 52, not here.

### Milestone framing & binding constraints (the reframe — load-bearing)
- `.planning/notes/five-loop-model.md` §"Task loop — `task_controller.go`" (lines 52-65,
  the exact create-attempt→verify→repairable/complete/escalate shape + "deterministic
  failure dominates the LLM judge" + the `verification:` YAML) and §"Load-bearing rules"
  (line 85). §"Observability" (line 76-77) for the EVALUATOR-span/OBS-03 shape.
- `.planning/PROJECT.md` "Current Milestone: v1.0.9 — Slack Tide" (Task-loop target
  features) + "Key Decisions" (the failure-halt/gate/reservation precedents this phase
  mirrors).
- `.planning/STATE.md` "Accumulated Context › Decisions" — the v1.0.9 binding
  constraints (fail-closed verdict; deterministic dominates judge; `ConditionVerifyHalt`
  mirrors `failure_halt.go` + Phase-25 time-fence, gates both tiers, distinct class;
  read-only enforced structurally; cost/concurrency is the biggest multiplier) — and
  Blockers/Concerns (the two open calls, now resolved D-01/D-02).

### The committed contract this CRD embodies
- `docs/templates/minimal-loop-project/tasks/TASK.template.md` — the immutable
  Draft→Locked→Superseded contract, acceptance signals, prohibited-changes, three-tier
  escalation that `TaskSpec.verification` (D-01) + the anti-gaming detector (D-08) embody.
- `docs/templates/minimal-loop-project/evals/README.md` §"Run evidence contract" +
  §"Integrity rules" — deterministic failure dominates the judge; **do not weaken/delete
  an evaluator to make a Task pass** (the D-08 anti-gaming invariant's source).

### Prior-phase hand-offs (the seams this phase builds on)
- `.planning/phases/49-common-loop-contract-verdict-envelope-persistence-schema/49-CONTEXT.md`
  — `LoopPolicy`/`LoopStatus` (`api/v1alpha3/loop_types.go`), the `GateDecision`/`Finding`
  verdict schema + fail-closed `ClassifyVerdict` (`pkg/dispatch/verdict.go` ↔
  `verifier/verdict.py`), `VerifyContext` on `EnvelopeIn` (the `GateCommand`/
  `RequiredArtifacts`/`EvaluatorRef`/`EvidencePacketPath` fields D-01/D-04 populate).
- `.planning/phases/50-execution-loop-hardening-loop-native-observability/50-CONTEXT.md`
  — `TerminalReason` enum, `RunEvidence` (the `ChangedFiles` manifest D-08 keys off),
  derived `loopRunID`/`attemptID`, the `loop.*`/`evaluation.*` otelai keys (defined-empty
  → D-11 first-populates), the cardinality guard. **Its `<deferred>` correctly routes
  `ConditionVerifyHalt` / verifier dispatch / OBS-03 to THIS phase.**

### The seams these fields/gates/spans attach to (source of truth — read before coding)
- `api/v1alpha3/task_types.go` — `TaskSpec` (D-01 adds `verification`; note existing
  `Gates` precedence pattern to mirror), `Task.Status.Attempt` (D-05/D-07 identity).
- `pkg/dispatch/envelope.go:431-454` — `VerifyContext` (`GateCommand:439`,
  `RequiredArtifacts`, `EvaluatorRef`, `EvidencePacketPath:453`) that D-01/D-04 resolve
  onto; `EnvelopeOut.Verdict` + `RunEvidence` (D-06/D-08).
- `pkg/dispatch/verdict.go` — `Verdict` (APPROVED/REPAIRABLE/BLOCKED), `GateDecision`,
  `Finding`, fail-closed `ClassifyVerdict`, `highSeverityFindingToken` (D-06/D-08 reuse).
- `pkg/dispatch/provider.go:36-40` — `ProviderSpec.Vendor` (D-02 adds `"langgraph"` to
  the canonical set); `pkg/dispatch/vendor_capabilities.go:38` — `SelfInstruments`
  (D-02: add the `"langgraph"→true` case).
- `internal/controller/failure_halt.go:56,79` — `checkFailureHalt` /
  `setFailureHaltIfNeeded` (+ Phase-25 resume time-fence) — the **template** D-09 clones
  into `verify_halt.go`.
- `internal/controller/dispatch_helpers.go:490,561,580` — `plannerInFlightCount` (D-10
  mirror) + `checkDispatchHolds` (D-09 wires `checkVerifyHalt` in); `internal/controller/git_writer.go:100`
  — `gitWriterInFlightCount` (the exact shape D-10's `verifierInFlightCount` copies).
- `internal/controller/task_controller.go:334,391-395` — `gateChecks` (D-09 wires
  `checkVerifyHalt`; the `:391-395` comment documents the Import-order divergence D-09
  normalizes); `:107-109` `ReservationStore` (D-10 BudgetCents); `:1117` the existing
  `SelfInstruments(ResolveProvider(...).Vendor)` call site (D-02/D-11 flows through it).
- `internal/subagent/common/prompt_templates.go:51-81` + `templates/` — the five
  compiled-in `<level>_<role>.tmpl` templates; D-12 adds `task_verifier.tmpl` behind
  `LoadPromptTemplate("verifier","task")`.
- `internal/controller/span_emission.go:198-211` — `synthesizePlannerSpan` /
  `AgentInvocation` / `LLMIdentity` (the AGENT-span emitter D-11's EVALUATOR sibling
  span parallels); `pkg/otelai/attrs.go` — the `evaluation.*` keys D-11 populates.
- `cmd/tide-langgraph-verifier/verifier/` — the Phase-48 read-only image (`tools.py:141`
  + `test_verdict.py:80` already anticipate consuming `VerifyContext.GateCommand`);
  D-06's deterministic-dominance is enforced here + controller-side.

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- **`pkg/dispatch/verdict.go` (Phase 49)** — `GateDecision`/`Finding` + fail-closed
  `ClassifyVerdict` already exist; D-06 reuses them (an empty/unparseable verdict is
  already BLOCKED, never APPROVED). `highSeverityFindingToken` is a package const so
  D-08's severity rubric retunes in one place.
- **`VerifyContext` on `EnvelopeIn` (Phase 49, envelope.go:431)** — `GateCommand`,
  `RequiredArtifacts`, `EvaluatorRef`, `EvidencePacketPath` are **already on the wire**;
  D-01/D-04 only add the CRD-schema authoring surface + resolution that populates them.
- **`RunEvidence.ChangedFiles` (Phase 50)** — the bounded changed-file manifest D-08's
  anti-gaming detector intersects against the evaluator/fixture/threshold path set.
- **`failure_halt.go` + `ConditionFailureHalt` + Phase-25 resume time-fence** — the
  file-for-file template D-09 clones for `verify_halt.go`; the mirror pattern
  (BillingHalt→FailureHalt→VerifyHalt) is well-worn.
- **`gitWriterInFlightCount` / `plannerInFlightCount`** — the exact in-flight-count
  shape (label-selected non-terminal Jobs) D-10's `verifierInFlightCount` copies; the
  Phase-32 concurrency-gate pattern is proven against the run-2b single-node OOM.
- **`LoadPromptTemplate(role, level)` + the `<level>_<role>.tmpl` family** — D-12 adds
  `task_verifier.tmpl` with zero new loader machinery.
- **`budget.ReservationStore` (rederivable-on-restart)** — D-10's `LoopPolicy.BudgetCents`
  rides the existing reserve/settle accounting.

### Established Patterns
- **CEL `x-kubernetes-validations`, not admission webhooks** — D-03's "immutable once
  Locked" is a CEL rule on `TaskSpec.verification`, matching the project's validation idiom.
- **`Gates` precedence (Task-level overrides Project-level)** — the exact precedent
  D-01's Phase-52 Task > Plan > Project `verification` resolution follows.
- **Go↔Python envelope duality (import firewall)** — any wire field D-01/D-04 add lands
  in both `pkg/dispatch` and `verifier/` with matching tests; but **prompts stay
  Go-only** (D-12, EVAL-04 — no Python port).
- **`SelfInstruments(vendor)` as a pure predicate + manager-carried boolean (Phase-45
  ADAPT-01 D-02)** — D-02's new `"langgraph"` case preserves the signature; the reporter
  keeps trusting the manager-computed flag on the Job.
- **Deterministic TraceID + W3C traceparent + AGENT-span emitter** — D-11's EVALUATOR
  sibling span rides the v1.0.8 trace spine; no new trace plumbing, a new span kind +
  the `evaluation.*` attributes.
- **Resumption = re-derive, never persist the schedule (LOOP-03)** — D-07's `LoopStatus`
  carries only the current-iteration summary + exit reason.

### Integration Points
- **`api/v1alpha3/task_types.go` (+ `verifier/` mirror where wire-facing)** — the
  `verification` block, `VerificationPhase`/version/`lockedSHA` status (D-01/D-03).
- **`internal/controller/task_controller.go`** — the loop itself: verdict consumption
  (D-06), fresh-attempt-vs-infra-retry (D-05), evidence packet (D-04), `gateChecks`
  VerifyHalt wiring + chain normalization (D-09), `verifierInFlightCount` + BudgetCents
  (D-10), anti-gaming detector (D-08).
- **`internal/controller/verify_halt.go` (new) + `dispatch_helpers.go` +
  `project_controller.go`** — `ConditionVerifyHalt` mirror + the unified dispatch-hold
  chain across all five levels (D-09, folds the two todos).
- **`pkg/dispatch/vendor_capabilities.go` + `provider.go`** — the `"langgraph"` sentinel
  (D-02).
- **`internal/subagent/common/templates/task_verifier.tmpl`** — the verifier prompt (D-12).
- **`internal/controller/span_emission.go` + `pkg/otelai`** — the EVALUATOR span +
  `evaluation.*` population (D-11).

</code_context>

<specifics>
## Specific Ideas

- **`TaskSpec.verification` shape (D-01, locked):** `gateCommand` (single canonical
  pass-criterion → `VerifyContext.GateCommand`), `commands` (list), `requiredArtifacts`,
  `evaluator`, `maxIterations`, `onExhaustion` — planner-authored, immutable once Locked,
  `git show <lockedSHA>` reproduces it. Plan/Project-level fields → Phase 52.
- **`"langgraph"` is a new `Vendor` sentinel (D-02, locked)** — `SelfInstruments`
  returns `true` for it (skip synthesis), false for everything else; the verifier image
  refuses any other vendor at startup. Precedent: `"opencode"` is already a
  runtime-shaped vendor value.
- **A deterministic gate-command failure ALWAYS dominates an LLM-judge APPROVED (D-06)**
  — enforced in `verifier/` and re-checked controller-side; there is no code path where
  a judge's APPROVED overrides a red gate exit.
- **The anti-gaming detector is structural (D-08):** a fresh attempt whose
  `RunEvidence.ChangedFiles` touches evaluator/fixture/threshold paths is a **system
  escalation, never a pass** — proven by a regression test, not a doc note.
- **`ConditionVerifyHalt` is a DISTINCT halt class (D-09)** — never a reinterpretation
  of `Failed` wave semantics; a VerifyHalt leaves phase/wave-siblings/conservative-profile
  propagation untouched (ESC-03 regression test).
- **Verifier pods count against the concurrency gate IN THIS PHASE (D-10)** — a new
  `verifierInFlightCount`, proven by a kind concurrent-dispatch test under the cap.

</specifics>

<deferred>
## Deferred Ideas

- **Per-level verification at Plan / Phase / Milestone / Project** — the same contract
  parameterized by `LoopPolicy` (plan-check re-plan `maxIterations:1`; Phase/Milestone/
  Project escalate `maxIterations:0` → `requireApproval`), the `Plan.Spec`/`Project.Spec`
  `verification` fields, and the Task > Plan > Project resolution precedence → **Phase 52**
  (ESC-01). This phase adds `verification` to `TaskSpec` only (D-01).
- **Chart-first config surface** (evaluator image/model + per-level `LoopPolicy` defaults,
  `subagent.levels`/`resolveImage` precedence, off-on-in-place-upgrade posture) →
  **Phase 53** (CFG-01/02).
- **Dashboard nested-provenance + `VerifyHalt` visual state** (distinct from `Failed`,
  staged findings via existing gitfetch/artifacts API) → **Phase 53** (OBS-04).
- **Composite evaluators** (schema/spec conformance, security, diff-scope/file-touch
  beyond deterministic + single LLM judge) → named future arc (REQUIREMENTS Future).
- **Product / System / Oversight loops** → named future milestones (five-loop model).

### Reviewed Todos (not folded)
- **`2026-07-03-signed-commits-verified-badge`** (score 0.9 — keyword false-positive on
  the "git" area) — GPG commit signing (SIGN-02/03/04), **deferred by choice** since
  v1.0.7; no verification/loop overlap. Same disposition as Phase 50.
- **`cache-f1-direct-sdk-cross-pod-caching`** (score 0.6) — direct-SDK cross-pod caching,
  **explicitly deferred to vNext+** (STATE.md Pending Todos); unrelated to the Task loop.

</deferred>

---

*Phase: 51-the-task-loop*
*Context gathered: 2026-07-19*
