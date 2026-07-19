# Phase 50: Execution-Loop Hardening + Loop-Native Observability - Context

**Gathered:** 2026-07-18
**Status:** Ready for planning

> **Mode:** `--auto` (fully autonomous). All 6 gray areas auto-selected; each
> resolved to the recommended default. Single-pass — CONTEXT.md written once.
> Decisions below are grounded in ROADMAP §Phase 50 (5 success criteria),
> REQUIREMENTS EXEC-01..04 + OBS-01/02, the v1.0.9 binding constraints
> (PROJECT.md / STATE.md / `f85ee3d`), the [five-loop model](../../notes/five-loop-model.md)
> §"Execution loop" + §"Observability", the run-evidence contract
> (`docs/templates/minimal-loop-project/evals/README.md`), and a live seam scout
> of the actual envelope / span-synth / metrics code.
>
> **⚠ Scope correction (load-bearing):** Phase 49's CONTEXT `<deferred>` predicted
> `ConditionVerifyHalt` + resume time-fence + dispatch-gate wiring would land in
> **Phase 50**. The authoritative ROADMAP §Phase 50 (criteria are entirely
> EXEC/OBS — no halt condition) and REQUIREMENTS traceability (ESC-02 → **Phase
> 51**) supersede that prediction. **Phase 50 adds NO halt class.** See
> `<domain>` "Deliberately NOT in this phase" and `<deferred>`.

<domain>
## Phase Boundary

Harden the **in-Job Execution loop** — which is a *pipeline stage today, not a
loop* (five-loop model §49) — so it produces **machine-checkable run evidence**
and emits the **loop-native trace/metric attributes** the Phase-51 Task loop will
consume. The harness (`internal/harness`, `internal/subagent/*`) *already* supplies
wall-clock/token/iteration caps, worktree isolation, output-path validation,
secret redaction, and structured envelopes. This phase **adds** on top of that
settled base:

1. **`loopRunID` + `attemptID`** — a stable run/attempt identity threaded through
   the envelope seam and onto spans (EXEC-01).
2. **Explicit terminal reason** — a closed enum `completed | cap_exceeded |
   blocked | tool_failure | invalid_output` on the result envelope, **never a
   silent default** (EXEC-02).
3. **Run-evidence contract on the envelope** — the canonical `evals/README.md`
   list (Task+Spec IDs + locking commit, commands + evaluator versions, test/eval
   results, changed-file manifest, runtime/model/prompt version, cost/duration,
   terminal reason, bounded feedback) — **referenced, not re-derived** (EXEC-03).
4. **Completion = belief only** — the envelope reports only that the *agent
   believes* the attempt is complete; **no field or code path lets the Execution
   loop stamp Task correctness** (correctness is exclusively the Phase-51 Task
   loop's call) (EXEC-04).
5. **Loop-native span attributes** — `loop.kind` / `loop.run_id` /
   `loop.parent_run_id` / `loop.iteration` / `loop.candidate_version` /
   `loop.exit_reason` / `evaluation.result` / `evaluation.version` /
   `human_intervention` + cost/duration/token usage (OBS-01).
6. **Run IDs out of Prometheus labels** — proven by a **label-cardinality test**;
   metrics keep bounded labels only (loop kind, exit reason, evaluator type, risk
   tier) (OBS-02).

**This phase is EXECUTION-LOOP HARDENING + OBSERVABILITY PLUMBING.** The evidence
envelope, terminal reasons, run IDs, and loop-native span/metric attributes exist,
round-trip, and pass their guards. Deliberately **NOT** in this phase:

- **`ConditionVerifyHalt` + `setVerifyHaltIfNeeded` + resume time-fence + dispatch-gate
  wiring** → **Phase 51** (ESC-02/03). Phase 50 adds no halt class; `failure_halt.go`
  is untouched (it is the *template* Phase 51 clones). `grep VerifyHalt` = 0 hits today
  and stays 0 after Phase 50.
- **`TaskReconciler` verifier dispatch, evidence-packet-seeded fresh attempts,
  `maxIterations` loop, concurrency-gate accounting, `LoopPolicy.BudgetCents`
  reservation, `onExhaustion` escalation** → **Phase 51** (TASK-*, ESC-04).
- **The `"langgraph"` vendor `SelfInstruments` registration + the `EVALUATOR`-kind
  sibling span** → **Phase 51** (OBS-03). Phase 50 *defines* the `evaluation.*`
  attribute keys but the EVALUATOR span that meaningfully populates them is Phase 51.
- **The controller rewire that routes exit-0 through the verifier before marking a
  Task correct** → **Phase 51.** Phase 50 only makes the envelope's completion field
  *semantically* belief-only + adds the negative guard; it does not change today's
  exit-0→Complete controller path.
- **Dashboard nested-provenance + `VerifyHalt` visual state** → **Phase 53** (OBS-04).

Success = the Execution loop's evidence + observability contract is locked and
guarded, so the Task loop (Phase 51) builds on a settled envelope + span shape, not
a moving one.

</domain>

<decisions>
## Implementation Decisions

### `loopRunID` / `attemptID` identity — derive, don't mint (EXEC-01, GA1)
- **D-01 (Deterministic derivation from existing identity — recommended):**
  Do **not** persist a new random run ID. Derive both from what already exists:
  `Task.Status.Attempt` (`api/v1alpha3/task_types.go:157`) + `TaskUID`, matching
  the existing per-attempt Job-name tuple `podjob.JobName(taskUID, attempt) →
  "tide-task-{taskUID}-{attempt}"` (`internal/dispatch/podjob/names.go:37`).
  - `attemptID` = the individual execution attempt = `{taskUID}-{attempt}` (the
    Job-name tuple). This is the Execution loop's `loop.run_id`.
  - `loopRunID` = the **outer Task-loop run**, stable across all repair attempts of
    one Task = `taskUID` (the run anchor). This is the Execution loop's
    `loop.parent_run_id`, and the natural seed for `LoopStatus.ParentRunID`
    (`loop_types.go:104`, already exists; `RunID` field does **not** exist yet).
  - The manager stamps both onto `EnvelopeIn` at dispatch; the executor echoes them
    onto `EnvelopeOut` so they round-trip into run evidence and onto spans.
  *Rationale: matches the project's "resumption = re-derive, never persist the
  schedule" principle (CLAUDE.md / spec), reuses the grep-unambiguous `JobName`
  tuple, and keeps etcd a state store (LOOP-03). Rejected: minting + persisting a
  fresh `loopRunID` on `Task.Status` — a new stored identifier that adds a resume
  reconstruction burden for zero benefit over the deterministic tuple.*
- **D-01b:** "A span per tool/action iteration" (EXEC-01) reuses the existing
  per-call LLM-span synthesis (`internal/reporter/tracesynth.go:EmitSpans:594`,
  one LLM span per `message_start..message_stop`) — the iteration spans **already
  exist**; Phase 50 stamps the correlating `loop.run_id`/`loop.iteration` subset on
  them (see D-05), it does not invent a new span emitter.

### Terminal reason — new typed enum field, never overload free-text `Reason` (EXEC-02, GA2)
- **D-02 (Dedicated typed `TerminalReason` enum, fail-closed on the zero value —
  recommended):** Add a **new** defined string-enum type + field on `EnvelopeOut`
  (and mirror on `TerminationStub`), distinct from the existing free-text
  `EnvelopeOut.Reason` (`pkg/dispatch/envelope.go:196`, which carries diagnostic
  detail like `forced-failure`/`cap-hit`/`output-path-violation`). The enum is
  exactly `completed | cap_exceeded | blocked | tool_failure | invalid_output`.
  - `Reason` **stays** as the human/diagnostic detail string; `TerminalReason` is
    the machine enum. They are complementary, not a rename.
  - **"Never a silent default"** is enforced structurally: the zero value is an
    invalid/empty sentinel, every exit path sets `TerminalReason` explicitly, and a
    **test asserts no exit path emits an envelope with an unset terminal reason**
    (mirroring the Phase-49 fail-closed classifier discipline — an unclassified
    verdict never collapses to APPROVED; here an unset reason never collapses to
    `completed`).
  - Exit-condition mapping (to be finalized at plan time): normal finish →
    `completed`; wall-clock/token/iteration cap → `cap_exceeded`; policy/output-path
    violation / gate block → `blocked`; tool subprocess/exec failure →
    `tool_failure`; unparseable or schema-invalid agent output → `invalid_output`.
  - Mirror the field + values in the Python verifier envelope
    (`cmd/tide-langgraph-verifier/verifier/envelope.py:132 write_envelope_out`) —
    the Go↔Python envelope duality means every new field lands in both, with
    matching tests (`envelope_test.go` + `verifier/tests/test_envelope.py`).
  *Rejected: overloading `Reason` with the enum values — the scout confirms `Reason`
  is already a free-text diagnostic channel; conflating the two loses the diagnostic
  detail and makes "never a silent default" un-testable.*
- **D-02b:** `TerminalReason` **is** the `loop.exit_reason` span attribute (OBS-01,
  D-05) — one source of truth for the exit disposition across envelope + trace.

### Run-evidence — a bounded `RunEvidence` struct that references, never re-derives (EXEC-03, GA3)
- **D-03 (Dedicated `RunEvidence` sub-struct on `EnvelopeOut`, references-only —
  recommended):** Add one structured `RunEvidence` block mapping the canonical
  `evals/README.md` list 1:1, populated from already-produced sources rather than
  re-collecting heavy data:
  - **Already exist → reference:** Task ID (`TaskUID`), cost/duration/token
    (`Usage` `envelope.go:312` + `CompletedAt`/StartedAt), locking/head commit
    (`Git.HeadSHA` `envelope.go:282`), iteration count (`Usage.Iterations:343`),
    bounded feedback (`Result`).
  - **Genuinely missing → add:** (1) **Spec ID + the Task-contract locking commit**
    (only `TaskUID` + push-time SHA exist today — no spec-ref/base-commit); (2)
    **commands + evaluator versions executed** (the recorded command strings +
    version strings); (3) **changed-file manifest** as a **bounded path/name-status
    list** (git `--name-status`), **not** the diffs — `EnvelopeOut.Artifacts:205`
    is declared-write confirmation, not a change manifest; (4) **runtime/model/prompt
    version** — the notable gap: the envelope "never carried a model field at any
    layer" (`span_emission.go:138`); add model name + prompt-template version +
    runtime id.
  - **"Referenced, not re-derived"** = `RunEvidence` holds IDs / versions / bounded
    pointers to artifacts the run already produced (staged findings/diff on the run
    branch, the per-call spans), never a re-computed duplicate of that data. It stays
    small enough that the `<4KB` `TerminationStub` still carries the summary subset
    (`NewTerminationStub` `envelope.go:484`).
  - **Schema parity both languages, full population on the Go executor path now;**
    the Python verifier carries the field definitions (populated where trivially
    available) so the schema round-trips — matching the Phase-49 discipline where the
    Python side carried fields ahead of its consumer. The verifier's *full* evidence
    population is Phase 51.
  *Rejected: flat fields sprinkled directly onto `EnvelopeOut` — a dedicated struct
  maps the contract list legibly, keeps `EnvelopeOut` readable, and makes the
  "bounded/reference-only" invariant reviewable in one place.*

### Completion-is-belief — fold into `TerminalReason`, enforce with a negative guard (EXEC-04, GA4)
- **D-04 (`TerminalReason == completed` is the sole "agent believes complete"
  signal + a structural non-authority guard — recommended):** Do **not** add a
  redundant `AgentReportedComplete` boolean. `TerminalReason == completed` already
  means exactly "the agent believes this attempt is complete." Deliver the EXEC-04
  guarantee as the **negative invariant**, not a new field:
  - A **doc-comment** on the completion field states it reports agent *belief* and is
    **non-authoritative** for Task correctness.
  - A **schema/guard test** asserts the executor envelope carries **no field that
    asserts Task-correctness**, and that the Task-success path does not exist inside
    the Execution loop (correctness is the Phase-51 verifier's call). Scope-checked:
    Phase 50 does **not** rewire today's controller exit-0→`Complete` behavior (that
    re-route through the verifier is Phase 51) — it only locks the envelope semantics
    + the guard so the Phase-51 insertion has a clean, documented seam.
  *Rejected: a separate belief boolean — redundant with `completed`, and a second
  completion signal invites exactly the "which one is authoritative?" ambiguity
  EXEC-04 exists to kill.*

### Loop-native span attributes — otelai helpers, stamped on AGENT span + LLM-call subset (OBS-01, GA5)
- **D-05 (New `pkg/otelai` helpers for the 9 keys; primary home = the AGENT span —
  recommended):** Define `loop.*` / `evaluation.*` / `human_intervention` as
  **`pkg/otelai` attribute helpers** (constants + a helper fn, e.g.
  `LoopAttributes(...)` / `EvaluationAttributes(...)`), **not** hand-rolled string
  literals — required to pass the existing `TestKeysUseSemconvModule` grep guard
  (`pkg/otelai/attrs.go:89`) and consistent with the custom-key idiom at `attrs.go:92`.
  - **Stamp on the AGENT-kind span** (`internal/controller/span_emission.go:synthesizePlannerSpan:156`
    / `buildLevelEnrichment:317`) as the primary home — loop identity is
    per-attempt/level. This is where `level`/`wave_index`/`failure_profile` metadata
    already lives, so the loop metadata slots in beside it.
  - **Stamp the correlating subset** (`loop.run_id`, `loop.iteration`) on the per-call
    **LLM spans** (`tracesynth.go:EmitSpans:626`) so Phoenix groups each tool/action
    iteration under its attempt.
  - **Populate what execution time knows now:** `loop.kind = "execution"`,
    `loop.run_id` (D-01 attemptID), `loop.parent_run_id` (D-01 loopRunID),
    `loop.iteration` (`Usage.Iterations`), `loop.candidate_version` (the attempt's
    candidate = the locking/head commit from D-03), `loop.exit_reason` (D-02
    `TerminalReason`), plus cost/duration/token (already emitted via
    `otelai.TokenCount`). **Define but leave empty until Phase 51:**
    `evaluation.result` / `evaluation.version` / `human_intervention` — these are
    populated by the verifier/Task loop (OBS-03/Phase 51). Defining the keys now
    keeps the trace schema stable ahead of the consumer.
  - Watch the reporter export batch ceiling (`OTEL_BSP_MAX_EXPORT_BATCH_SIZE=6`,
    `reporter_jobspec.go:322`) — the added per-span attributes are small, but keep an
    eye on per-span payload growth.
  *Rejected: a separate loop span — the v1.0.8 AGENT span already models the attempt;
  loop attributes belong on it, not on a parallel span (that would double-emit into
  the trace tree, the exact OBS-03 anti-pattern).*

### Prometheus label cardinality — extend the static analyzer + a runtime guard (OBS-02, GA6)
- **D-06 (Dual guard: extend `metriccardinality` analyzer + a runtime cardinality
  test; new loop metrics keep bounded labels — recommended):** Match the existing
  two-layer guard (static analyzer + `wave_label_test.go` grep).
  - **Extend `tools/analyzers/metriccardinality/analyzer.go`** (which today rejects
    only the literal `"task"` label, `:42-98`) to also reject run-ID-shaped label
    names: `run_id`, `loop_run_id`, `run`, `attempt`, `attempt_id`, `trace_id`,
    `task_uid`, `uid`.
  - **Add/extend a runtime label-cardinality test** (mirroring
    `internal/metrics/wave_label_test.go` arity-lock + source-grep) proving
    `loopRunID`/`attemptID`/`loop.run_id` never enter any `prometheus.New*Vec` label
    set — the ROADMAP criterion #5 proof.
  - **Loop-native run detail lives in TRACES, not metrics** (LOOP-03 + five-loop
    model §77): any new metric Phase 50 adds carries **only bounded enum labels**
    (loop kind / exit reason / evaluator type / risk tier), following the existing
    `reason`/`outcome`/`level` enum pattern in `internal/metrics/registry.go`. Keep
    new metrics minimal — the per-run granularity is a trace/log concern; metrics
    stay aggregate.
  *Rejected: a runtime-test-only guard — the project already trusts the static
  analyzer for `"task"`; a run-ID label should fail at `go vet` time, not only in a
  unit test.*

### Claude's Discretion
- Exact Go struct field ordering, JSON tag spellings, the `RunEvidence` field names,
  and where `RunEvidence` lives (a sub-struct in `envelope.go` vs. its own file) —
  within D-03's bounded-references decision.
- The precise `TerminalReason` Go type name and whether it is its own file vs. added
  to `envelope.go` — within D-02's dedicated-typed-enum decision.
- The exact otelai helper signatures (`LoopAttributes` as one fn vs. split
  loop/evaluation helpers) and constant spellings — within D-05's semconv-guard-compliant
  decision.
- Whether the changed-file manifest is `git diff --name-status` output vs. a
  structured `[]ChangedFile{path, status}` — within D-03's bounded-list decision.
- Whether Phase 50 adds any new Prometheus metric at all, or only hardens the guard
  (the loop-outcome signal may be entirely trace-side per LOOP-03) — within D-06's
  bounded-labels-only decision.

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### Requirements & roadmap (scope authority)
- `.planning/REQUIREMENTS.md` — **EXEC-01** (`loopRunID`+`attemptID`, span per
  tool/action iteration), **EXEC-02** (terminal-reason enum), **EXEC-03** (run-evidence
  contract, referenced-not-re-derived), **EXEC-04** (belief-only, never stamps
  correctness), **OBS-01** (loop.*/evaluation.* span keys + cost/duration/token),
  **OBS-02** (run IDs out of Prometheus labels, metrics bounded).
- `.planning/ROADMAP.md` §"Phase 50" — the goal + 5 success criteria this CONTEXT
  locks against; §"Phase 51" — confirms `ConditionVerifyHalt` / verifier dispatch /
  OBS-03 EVALUATOR span belong to Phase 51 (the scope-correction authority).

### The canonical run-evidence contract (EXEC-03 — do not re-derive)
- `docs/templates/minimal-loop-project/evals/README.md` §"Run evidence contract"
  (lines 28-39) — the canonical list Task contracts reference: Task+Spec IDs +
  locking commit, commands + evaluator versions, test/eval results, changed-file
  manifest, runtime/model/prompt version, cost/duration/resource, terminal reason +
  bounded feedback. §"Integrity rules" (41-46) — deterministic failure dominates the
  judge; don't weaken an evaluator to pass.

### Milestone framing & binding constraints (the reframe — load-bearing)
- `.planning/notes/five-loop-model.md` §"Execution loop — inside the Job" (line 49-50,
  the exact EXEC-01..04 add-list + "never stamps correctness") and §"Observability"
  (line 77, the exact OBS-01/02 key list + "run IDs stay out of Prometheus labels").
- `.planning/PROJECT.md` "Current Milestone: v1.0.9 — Slack Tide" — the Task-loop
  reframe (verification closes a loop, not a gate); the Execution loop never stamps
  correctness.
- `.planning/STATE.md` "Accumulated Context › Decisions" — the v1.0.9 binding
  constraints (iteration history never in etcd; fail-closed; the Execution loop never
  stamps Task correct; run-IDs-out-of-metrics).
- `.planning/phases/49-common-loop-contract-verdict-envelope-persistence-schema/49-CONTEXT.md`
  — the Phase-49 hand-off: the `LoopStatus`/`LoopPolicy` types (`loop_types.go`), the
  `GateDecision` verdict schema, `VerifyContext` on `EnvelopeIn`, and the `TerminationStub`
  size×locality contract Phase 50's terminal-reason/run-evidence fields extend. **⚠
  Its `<deferred>` "`ConditionVerifyHalt` → Phase 50" is superseded — ROADMAP places it
  in Phase 51.**

### The seams these fields/spans/metrics attach to (source of truth — read before coding)
- `pkg/dispatch/envelope.go` — `EnvelopeOut` (:176-250: `TaskUID`:185, `ExitCode`:188,
  `Result`:192, `Reason`:196 [free-text — NOT the enum], `Usage`:200, `Artifacts`:205,
  `CompletedAt`:208, `Git`:226→`GitOutput.HeadSHA`:282, `Verdict`:249); `Usage`
  (:312-359: tokens/cost/`Iterations`:343); `TerminationStub` (:431-468) +
  `NewTerminationStub` (:484-506, `<4KB` invariant); `IsEnvelopeComplete` (:266-274).
  Where D-02 `TerminalReason`, D-03 `RunEvidence`, and the `<4KB` summary land.
- `cmd/tide-langgraph-verifier/verifier/envelope.py` — `write_envelope_out` (:132,
  the trivial 4-key writer) + `write_termination_stub` (:163); the import-firewalled
  Pydantic mirror **every** new Go envelope field (D-02/D-03) must be hand-ported into,
  with a matching `verifier/tests/test_envelope.py`.
- `internal/harness/harness.go:142` (`EnvelopeOut{...}` literal) + `internal/harness/envelope_io.go:117`
  (`WriteEnvelopeOut`) and `internal/subagent/anthropic/subagent.go:359` — the Go
  executor write sites D-02/D-03 populate.
- `api/v1alpha3/task_types.go` — `Task.Status.Attempt` (:157, the attempt number
  D-01 derives from); `TaskTraceReporterSpawnedUID` (:183-193). `internal/controller/task_controller.go`
  — `nextAttempt`/`prepareDispatch` (:723-776); `emitTaskMetrics` (:1491-1544, the
  bounded `{project,phase,plan,wave}` label resolution — never `task.UID`/`Attempt`).
- `internal/dispatch/podjob/names.go:37` — `JobName(taskUID, attempt) → "tide-task-{taskUID}-{attempt}"`,
  the identity tuple D-01 reuses.
- `api/v1alpha3/loop_types.go` — `LoopStatus.Iteration`:97, `LoopStatus.ParentRunID`:104
  (exists; no `RunID` field yet — D-01 note). The Phase-49 shared types Phase 50's
  identity plumbing rides beside.
- `internal/reporter/tracesynth.go` — `ReconstructConversation`:325, `EmitSpans`:594
  (LLM-kind span per call, attributes :626-659). Where D-05 stamps the
  `loop.run_id`/`loop.iteration` LLM-span subset.
- `internal/controller/span_emission.go` — `synthesizePlannerSpan`:156-267 (the
  AGENT-kind span, `otelai.AgentInvocation`:210, TraceID `otelai.TraceIDFromUID(project.UID)`:180)
  + `buildLevelEnrichment`:317-371 (the metadata/tags pair). The primary home for D-05
  loop attributes. `traceparentForLevel`:404-413.
- `pkg/otelai/attrs.go` — the attribute-helper module (custom TIDE keys :92-109; the
  `TestKeysUseSemconvModule` grep guard :89). Where D-05 adds the `loop.*`/`evaluation.*`
  helpers. `pkg/otelai/tracecontext.go` — `TraceIDFromUID`:53, `FormatTraceparent`:69.
- `pkg/dispatch/vendor_capabilities.go:38` — `SelfInstruments(vendor)` (a pure
  predicate; all vendors currently → false). **Phase 50 does NOT touch it** (the
  `"langgraph"` registration is OBS-03/Phase 51) — listed for boundary awareness.
- `internal/metrics/registry.go` — all metric registrations (`init()` :148-303) + their
  bounded label sets (`TasksFailedTotal{...,reason}`:165, `DispatchLatency{level}`:173,
  `PushJobsTotal{...,outcome}`:190, the six TELEM-03 token/cost metrics `{project,phase,plan,wave}`).
  Where D-06's bounded loop labels (if any new metric) follow the enum idiom.
- `tools/analyzers/metriccardinality/analyzer.go:42-98` (rejects `"task"` label) +
  `internal/metrics/wave_label_test.go` (arity-lock + source-grep guard) — the dual
  cardinality guard D-06 extends for run-ID-shaped labels.
- `internal/controller/failure_halt.go` — `checkFailureHalt`:56, `setFailureHaltIfNeeded`:79-114
  (the `ConditionFailureHalt` mirror + Phase-25 resume time-fence). **Read for
  boundary confirmation only — Phase 50 does NOT touch it;** it is the template
  Phase 51 clones for `ConditionVerifyHalt`.

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- **`EnvelopeOut` + `Usage` + `GitOutput` (`pkg/dispatch/envelope.go`)** — already
  carry Task ID, tokens/cost, `Iterations`, `CompletedAt`, and `Git.HeadSHA`. D-03's
  `RunEvidence` *references* these rather than re-collecting; only Spec-ID/locking-commit,
  commands+evaluator-versions, changed-file manifest, and model/prompt version are net-new.
- **`NewTerminationStub` + the `<4KB` invariant test (`envelope.go:484`)** — the same
  function that flattens `ExitCode`/`Reason`/`Usage`/`GateDecision`+counts; D-02's
  `TerminalReason` and D-03's evidence summary extend it the same way, and the existing
  `TestNewTerminationStub_StaysSmall` (+ the Python truncation loop) is extended to cover them.
- **`Task.Status.Attempt` + `podjob.JobName` tuple** — the deterministic per-attempt
  identity D-01 derives `loopRunID`/`attemptID` from; no new persisted field needed.
- **`internal/reporter/tracesynth.go:EmitSpans` (LLM span per message pair) +
  `span_emission.go:synthesizePlannerSpan` (AGENT span)** — the two existing span
  emitters D-05 stamps loop attributes onto; the "span per tool/action iteration"
  (EXEC-01) already exists as the per-call LLM span.
- **`pkg/otelai/attrs.go` custom-key idiom (:92) + `AgentInvocation`/`buildLevelEnrichment`
  metadata pair** — the exact pattern D-05's `loop.*` helpers + metadata slot into.
- **`metriccardinality` analyzer + `wave_label_test.go`** — the established dual
  cardinality guard D-06 extends; the run-ID-out-of-labels invariant is *already*
  actively enforced for `"task"` (no metric carries `task.UID`/`Attempt` today).

### Established Patterns
- **Go↔Python envelope duality (import firewall, `pkg/dispatch/doc.go` +
  `make verify-dispatch-imports`)** — every new `EnvelopeOut`/`TerminationStub` field
  (D-02, D-03) is hand-ported into `verifier/envelope.py` with matching tests; the
  Python image cannot import the Go types (which is *why* it's a hand-authored pair).
- **Attribute helpers via the semconv module, never string literals
  (`TestKeysUseSemconvModule`, `attrs.go:89`)** — D-05's loop keys must be otelai
  helpers to pass the grep guard.
- **Bounded enum labels only (`reason`/`outcome`/`level`/`wave` in `registry.go`)** —
  D-06's new loop metrics (if any) follow this; run-scoped detail goes to traces (LOOP-03).
- **Fail-closed on the zero value (Phase-49 `ClassifyVerdict` → BLOCKED, never
  APPROVED)** — D-02's "never a silent default" mirrors this: an unset `TerminalReason`
  is invalid, never `completed`.
- **Deterministic TraceID from Project.UID (`TraceIDFromUID`) + W3C traceparent
  threading** — the v1.0.8 trace-tree spine D-05's loop attributes ride on; no new
  trace plumbing, only new attributes.

### Integration Points
- **`pkg/dispatch/envelope.go` (+ `verifier/envelope.py`)** — `TerminalReason` field
  + `RunEvidence` struct + the `TerminationStub` summary extension (Go + Python mirror).
- **`internal/harness/*` + `internal/subagent/anthropic/subagent.go`** — the Go executor
  write sites that populate `TerminalReason` at every exit path + `RunEvidence`.
- **`internal/controller/span_emission.go` + `internal/reporter/tracesynth.go` +
  `pkg/otelai/attrs.go`** — the loop-native span attributes (helpers + AGENT-span +
  LLM-span-subset stamping).
- **`tools/analyzers/metriccardinality/` + `internal/metrics/`** — the extended
  cardinality guard + any bounded new loop metric.

</code_context>

<specifics>
## Specific Ideas

- **The terminal-reason set is exactly `completed | cap_exceeded | blocked |
  tool_failure | invalid_output`** (ROADMAP #2 / five-loop model §50) — a closed enum,
  never a silent default; `TerminalReason` is one source of truth shared by the
  envelope and the `loop.exit_reason` span attribute.
- **`loop.run_id` = attemptID (`{taskUID}-{attempt}`), `loop.parent_run_id` =
  loopRunID (`taskUID`)** — the Execution loop's run is the attempt; its parent is the
  Task-loop run. `loop.candidate_version` = the attempt's candidate = the locking/head
  commit.
- **`evaluation.result` / `evaluation.version` / `human_intervention` are DEFINED but
  EMPTY in Phase 50** — the keys exist so the trace schema is stable, but the verifier
  that populates them is Phase 51 (OBS-03). Do not fake-populate them.
- **The Execution loop never stamps correctness** (five-loop model §50/§85, EXEC-04) —
  the deliverable is the *negative* guarantee (belief-only doc + guard test), not a
  new authority field.
- **Run-native detail lives in traces/logs, aggregate lives in metrics** (LOOP-03,
  five-loop §77) — run IDs never touch a Prometheus label; this is the guard, not a
  suggestion.

</specifics>

<deferred>
## Deferred Ideas

- **`ConditionVerifyHalt` + `setVerifyHaltIfNeeded` + resume time-fence + dispatch-gate
  wiring (`checkDispatchHolds` / `TaskReconciler.gateChecks`)** → **Phase 51** (ESC-02/03).
  *Corrects the Phase-49 CONTEXT prediction that placed this in Phase 50 — ROADMAP/REQUIREMENTS
  are authoritative and put it in Phase 51.*
- **`TaskReconciler` verifier dispatch, evidence-packet-seeded fresh attempts,
  `maxIterations` Task loop, concurrency-gate accounting (`verifierInFlightCount`),
  `LoopPolicy.BudgetCents` reservation, `onExhaustion: requireApproval`** → **Phase 51**
  (TASK-*, ESC-04).
- **`"langgraph"` vendor `SelfInstruments` registration + the `EVALUATOR`-kind sibling
  span + populating `evaluation.*`/`human_intervention`** → **Phase 51** (OBS-03).
- **The controller rewire that consults the verifier before marking a Task correct**
  (replacing today's exit-0→`Complete`) → **Phase 51.**
- **Dashboard nested-provenance (Project run → Task iteration → Execution attempt/tool
  spans) + `VerifyHalt` visual state** → **Phase 53** (OBS-04).
- **Optional in-attempt checkpoints for long attempts** (five-loop model §50, "not one
  K8s object per action") → future / not required by any Phase-50 success criterion.

### Reviewed Todos (not folded)
The `--auto` ≥0.4 auto-fold default was **overridden by the scope guardrail** — Phase 50
is Execution-loop hardening + observability plumbing, and every match is a keyword
false-positive against reconciler/halt-gate/deferred work, not evidence-envelope or
span/metric work:

- **`2026-07-12-project-dispatch-missing-failurehalt-gate`** (score 0.6) —
  `ProjectReconciler`'s planner-dispatch chain missing `checkFailureHalt`. **Halt-gate
  reconciler wiring** (the same tier as `ConditionVerifyHalt`) → belongs with **Phase 51**,
  not the execution-loop evidence/observability work. (STATE.md flags this exact todo as
  "relevant to v1.0.9's `ConditionVerifyHalt` gate-order work, Phase 51".)
- **`2026-07-12-task-dispatch-gate-order-divergence`** (score 0.6) — Task's dispatch-holds
  chain checks Import in a divergent position vs. the planner tier. **Dispatch-gate
  ordering** → **Phase 51** reconciler work, not this phase.
- **`2026-07-03-signed-commits-verified-badge`** (score 0.4) — GPG commit signing;
  **deferred by choice** since v1.0.7 (SIGN-02/03/04); no envelope/span/metric overlap.
- **`cache-f1-direct-sdk-cross-pod-caching`** (score 0.4) — explicitly **deferred to
  vNext** (STATE.md Pending Todos); unrelated to execution-loop hardening.

</deferred>

---

*Phase: 50-execution-loop-hardening-loop-native-observability*
*Context gathered: 2026-07-18*
