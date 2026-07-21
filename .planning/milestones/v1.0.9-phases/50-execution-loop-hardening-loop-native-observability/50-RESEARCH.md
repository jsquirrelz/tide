# Phase 50: Execution-Loop Hardening + Loop-Native Observability - Research

**Researched:** 2026-07-18
**Domain:** Go dispatch-envelope wire contract, Go↔Python envelope duality, OpenInference/OTel span synthesis (controller + in-namespace reporter), Prometheus cardinality guards
**Confidence:** HIGH (every claim below is grounded in a direct file:line read this session; no web research was needed — this phase is 100% internal-codebase mechanics)

<user_constraints>
## User Constraints (from CONTEXT.md)

### Locked Decisions

**`loopRunID` / `attemptID` identity — derive, don't mint (EXEC-01, GA1)**
- **D-01 (Deterministic derivation from existing identity):** Do **not** persist a new random run ID. Derive both from what already exists: `Task.Status.Attempt` (`api/v1alpha3/task_types.go:157`) + `TaskUID`, matching the existing per-attempt Job-name tuple `podjob.JobName(taskUID, attempt) → "tide-task-{taskUID}-{attempt}"` (`internal/dispatch/podjob/names.go:37`).
  - `attemptID` = the individual execution attempt = `{taskUID}-{attempt}` (the Job-name tuple). This is the Execution loop's `loop.run_id`.
  - `loopRunID` = the **outer Task-loop run**, stable across all repair attempts of one Task = `taskUID` (the run anchor). This is the Execution loop's `loop.parent_run_id`, and the natural seed for `LoopStatus.ParentRunID` (`loop_types.go:104`, already exists; `RunID` field does **not** exist yet).
  - The manager stamps both onto `EnvelopeIn` at dispatch; the executor echoes them onto `EnvelopeOut` so they round-trip into run evidence and onto spans.
- **D-01b:** "A span per tool/action iteration" (EXEC-01) reuses the existing per-call LLM-span synthesis (`internal/reporter/tracesynth.go:EmitSpans:594`) — the iteration spans **already exist**; Phase 50 stamps the correlating `loop.run_id`/`loop.iteration` subset on them (see D-05), it does not invent a new span emitter.

**Terminal reason — new typed enum field, never overload free-text `Reason` (EXEC-02, GA2)**
- **D-02 (Dedicated typed `TerminalReason` enum, fail-closed on the zero value):** Add a **new** defined string-enum type + field on `EnvelopeOut` (and mirror on `TerminationStub`), distinct from the existing free-text `EnvelopeOut.Reason` (`pkg/dispatch/envelope.go:196`). The enum is exactly `completed | cap_exceeded | blocked | tool_failure | invalid_output`.
  - `Reason` **stays** as the human/diagnostic detail string; `TerminalReason` is the machine enum. They are complementary, not a rename.
  - **"Never a silent default"** is enforced structurally: the zero value is an invalid/empty sentinel, every exit path sets `TerminalReason` explicitly, and a **test asserts no exit path emits an envelope with an unset terminal reason** (mirroring the Phase-49 fail-closed classifier discipline).
  - Exit-condition mapping (to be finalized at plan time): normal finish → `completed`; wall-clock/token/iteration cap → `cap_exceeded`; policy/output-path violation / gate block → `blocked`; tool subprocess/exec failure → `tool_failure`; unparseable or schema-invalid agent output → `invalid_output`.
  - Mirror the field + values in the Python verifier envelope (`cmd/tide-langgraph-verifier/verifier/envelope.py:132 write_envelope_out`) with matching tests (`envelope_test.go` + `verifier/tests/test_envelope.py`).
- **D-02b:** `TerminalReason` **is** the `loop.exit_reason` span attribute (OBS-01, D-05) — one source of truth for the exit disposition across envelope + trace.

**Run-evidence — a bounded `RunEvidence` struct that references, never re-derives (EXEC-03, GA3)**
- **D-03 (Dedicated `RunEvidence` sub-struct on `EnvelopeOut`, references-only):** Add one structured `RunEvidence` block mapping the canonical `evals/README.md` list 1:1.
  - **Already exist → reference:** Task ID (`TaskUID`), cost/duration/token (`Usage` `envelope.go:312` + `CompletedAt`/StartedAt), locking/head commit (`Git.HeadSHA` `envelope.go:282`), iteration count (`Usage.Iterations:343`), bounded feedback (`Result`).
  - **Genuinely missing → add:** (1) Spec ID + the Task-contract locking commit; (2) commands + evaluator versions executed; (3) changed-file manifest as a bounded path/name-status list (git `--name-status`), not diffs; (4) runtime/model/prompt version — the notable gap: the envelope "never carried a model field at any layer" (`span_emission.go:138`).
  - "Referenced, not re-derived" = `RunEvidence` holds IDs/versions/bounded pointers, never a re-computed duplicate. Stays small enough that the `<4KB` `TerminationStub` still carries the summary subset (`NewTerminationStub` `envelope.go:484`).
  - Schema parity both languages, full population on the Go executor path now; the Python verifier carries the field definitions (populated where trivially available). The verifier's *full* evidence population is Phase 51.

**Completion-is-belief — fold into `TerminalReason`, enforce with a negative guard (EXEC-04, GA4)**
- **D-04 (`TerminalReason == completed` is the sole "agent believes complete" signal + a structural non-authority guard):** Do **not** add a redundant `AgentReportedComplete` boolean.
  - A **doc-comment** on the completion field states it reports agent *belief* and is **non-authoritative** for Task correctness.
  - A **schema/guard test** asserts the executor envelope carries **no field that asserts Task-correctness**, and that the Task-success path does not exist inside the Execution loop. Phase 50 does **not** rewire today's controller exit-0→`Complete` behavior (Phase 51) — it only locks the envelope semantics + the guard.

**Loop-native span attributes — otelai helpers, stamped on AGENT span + LLM-call subset (OBS-01, GA5)**
- **D-05 (New `pkg/otelai` helpers for the 9 keys; primary home = the AGENT span):** Define `loop.*` / `evaluation.*` / `human_intervention` as **`pkg/otelai` attribute helpers** (constants + a helper fn, e.g. `LoopAttributes(...)` / `EvaluationAttributes(...)`), **not** hand-rolled string literals — required to pass the existing `TestKeysUseSemconvModule` grep guard (`pkg/otelai/attrs.go:89`).
  - **Stamp on the AGENT-kind span** (`internal/controller/span_emission.go:synthesizePlannerSpan:156` / `buildLevelEnrichment:317`) as the primary home.
  - **Stamp the correlating subset** (`loop.run_id`, `loop.iteration`) on the per-call **LLM spans** (`tracesynth.go:EmitSpans:626`).
  - **Populate what execution time knows now:** `loop.kind = "execution"`, `loop.run_id` (D-01 attemptID), `loop.parent_run_id` (D-01 loopRunID), `loop.iteration` (`Usage.Iterations`), `loop.candidate_version` (locking/head commit from D-03), `loop.exit_reason` (D-02 `TerminalReason`), plus cost/duration/token. **Define but leave empty until Phase 51:** `evaluation.result` / `evaluation.version` / `human_intervention`.
  - Watch the reporter export batch ceiling (`OTEL_BSP_MAX_EXPORT_BATCH_SIZE=6`, `reporter_jobspec.go:322`).

**Prometheus label cardinality — extend the static analyzer + a runtime guard (OBS-02, GA6)**
- **D-06 (Dual guard: extend `metriccardinality` analyzer + a runtime cardinality test; new loop metrics keep bounded labels):**
  - **Extend `tools/analyzers/metriccardinality/analyzer.go`** (rejects only `"task"` today, `:42-98`) to also reject run-ID-shaped label names: `run_id`, `loop_run_id`, `run`, `attempt`, `attempt_id`, `trace_id`, `task_uid`, `uid`.
  - **Add/extend a runtime label-cardinality test** (mirroring `internal/metrics/wave_label_test.go`) proving `loopRunID`/`attemptID`/`loop.run_id` never enter any `prometheus.New*Vec` label set.
  - **Loop-native run detail lives in TRACES, not metrics** (LOOP-03): any new metric Phase 50 adds carries **only bounded enum labels** (loop kind / exit reason / evaluator type / risk tier). Keep new metrics minimal — the per-run granularity is a trace/log concern.

### Claude's Discretion
- Exact Go struct field ordering, JSON tag spellings, the `RunEvidence` field names, and where `RunEvidence` lives (a sub-struct in `envelope.go` vs. its own file) — within D-03's bounded-references decision.
- The precise `TerminalReason` Go type name and whether it is its own file vs. added to `envelope.go` — within D-02's dedicated-typed-enum decision.
- The exact otelai helper signatures (`LoopAttributes` as one fn vs. split loop/evaluation helpers) and constant spellings — within D-05's semconv-guard-compliant decision.
- Whether the changed-file manifest is `git diff --name-status` output vs. a structured `[]ChangedFile{path, status}` — within D-03's bounded-list decision.
- Whether Phase 50 adds any new Prometheus metric at all, or only hardens the guard (the loop-outcome signal may be entirely trace-side per LOOP-03) — within D-06's bounded-labels-only decision.

### Deferred Ideas (OUT OF SCOPE)
- **`ConditionVerifyHalt` + `setVerifyHaltIfNeeded` + resume time-fence + dispatch-gate wiring (`checkDispatchHolds` / `TaskReconciler.gateChecks`)** → **Phase 51** (ESC-02/03). Corrects the Phase-49 CONTEXT prediction that placed this in Phase 50 — ROADMAP/REQUIREMENTS are authoritative and put it in Phase 51.
- **`TaskReconciler` verifier dispatch, evidence-packet-seeded fresh attempts, `maxIterations` Task loop, concurrency-gate accounting (`verifierInFlightCount`), `LoopPolicy.BudgetCents` reservation, `onExhaustion: requireApproval`** → **Phase 51** (TASK-*, ESC-04).
- **`"langgraph"` vendor `SelfInstruments` registration + the `EVALUATOR`-kind sibling span + populating `evaluation.*`/`human_intervention`** → **Phase 51** (OBS-03).
- **The controller rewire that consults the verifier before marking a Task correct** (replacing today's exit-0→`Complete`) → **Phase 51.**
- **Dashboard nested-provenance (Project run → Task iteration → Execution attempt/tool spans) + `VerifyHalt` visual state** → **Phase 53** (OBS-04).
- **Optional in-attempt checkpoints for long attempts** (five-loop model §50, "not one K8s object per action") → future / not required by any Phase-50 success criterion.
- Reviewed-but-not-folded todos: `project-dispatch-missing-failurehalt-gate` and `task-dispatch-gate-order-divergence` (both → Phase 51, halt-gate reconciler wiring); `signed-commits-verified-badge` (deferred since v1.0.7); `cache-f1-direct-sdk-cross-pod-caching` (deferred to vNext+).
</user_constraints>

<phase_requirements>
## Phase Requirements

| ID | Description | Research Support |
|----|-------------|--------------------|
| EXEC-01 | Every attempt carries a stable `loopRunID` + `attemptID` and emits a span per tool/action iteration | Identity derivation source confirmed (`Task.Status.Attempt` + `podjob.JobName` tuple); exact threading path traced EnvelopeIn (controller dispatch sites) → EnvelopeOut (3 real write sites, see Pitfall 1) → AGENT span (`synthesizePlannerSpan`) → LLM-span subset (`EmitSpans`, requires a signature change — Pitfall 5) |
| EXEC-02 | The result envelope carries an explicit terminal reason — `completed \| cap_exceeded \| blocked \| tool_failure \| invalid_output` | Fail-closed idiom sourced from `pkg/dispatch/verdict.go:ClassifyVerdict` (Pattern 1); all real exit-path write sites enumerated (Pitfall 1); the `cap_exceeded` producer gap for wall-clock kills identified as a genuine open question (Pitfall 2, Open Question 1); full Result/Reason-string → enum mapping table required (Pitfall 6) |
| EXEC-03 | The result envelope satisfies the run-evidence contract — referenced, not re-derived | Canonical contract list read verbatim from `evals/README.md`; field-by-field already-exists-vs-net-new split done against `envelope.go`'s actual fields; the model/prompt/runtime version gap traced to its root (no version marker anywhere) with concrete sourcing options (Pitfall 4, Open Question 2) |
| EXEC-04 | The Execution loop reports only that the agent believes the attempt is complete; never stamps Task correctness | Confirmed no existing "Task correct" field/path in `EnvelopeOut`; guard-test precedent identified (`TestTerminationStub_NoForbiddenFields`, Phase 49's `TestLoopStatus_NoForbiddenFields`) as the mechanical template for the negative-invariant test |
| OBS-01 | Spans carry `loop.kind`/`loop.run_id`/`loop.parent_run_id`/`loop.iteration`/`loop.candidate_version`/`loop.exit_reason`/`evaluation.result`/`evaluation.version`/`human_intervention` plus cost/duration/token | Exact `pkg/otelai/attrs.go` custom-key idiom captured (Pattern 3); `TestKeysUseSemconvModule`'s actual regex read and confirmed NOT to block `loop.`/`evaluation.` prefixes (Pitfall 3); both stamping sites (`synthesizePlannerSpan`, `EmitSpans`) traced with exact line numbers and the Reporter-Job Args-threading precedent (Pattern 2) |
| OBS-02 | Run IDs stay out of Prometheus labels; metrics use bounded labels | Exact `metriccardinality` analyzer logic and `wave_label_test.go` runtime guard read in full; confirmed no metric carries a run/task-UID label today; extension shape specified (forbidden-label-list growth, same two files) |

</phase_requirements>

## Summary

Phase 50 is pure plumbing on four already-settled seams: the `EnvelopeOut`/`TerminationStub` wire contract (`pkg/dispatch/envelope.go`), its Python mirror (`cmd/tide-langgraph-verifier/verifier/envelope.py`), the two span emitters (`internal/controller/span_emission.go`'s AGENT span + `internal/reporter/tracesynth.go`'s per-call LLM spans, invoked from the separate `cmd/tide-reporter` binary), and the dual Prometheus cardinality guard (`tools/analyzers/metriccardinality` + `internal/metrics/wave_label_test.go`). CONTEXT.md's 6 decisions (D-01..D-06) are locked; this research verifies the exact current shapes those decisions attach to and surfaces several load-bearing mechanics CONTEXT.md's scout did not drill into.

Three findings materially change how the planner should scope EXEC-02 (terminal reason) and EXEC-01 (span-per-iteration):

1. **`internal/harness.Harness.Run()` (the orchestrator CONTEXT.md cites as an executor write site, `harness.go:142`) is dead code in production** — zero call sites outside its own package. The REAL production terminal-reason producers are `cmd/claude-subagent/main.go`'s `run()`/`failEnvelope()` and `internal/subagent/anthropic/subagent.go`'s `Run()`; the REAL test-fixture producer is `cmd/stub-subagent/main.go`. The planner must target these three files, not `internal/harness/harness.go`.
2. **Only wall-clock caps are enforced today, and only at the Kubernetes Job level** (`ActiveDeadlineSeconds`, `internal/dispatch/podjob/jobspec.go:576`) — not in-process. `internal/harness.CheckCaps` (which *would* detect iteration/token-cap violations) is never called from any live write site. A wall-clock cap firing kills the Pod before any envelope is written at all; the controller then takes the `EnvelopeReadFailed` branch (`internal/controller/task_controller.go:1196-1222`) with a synthetic empty `EnvelopeOut{}` — there is no pod-side write site to stamp `TerminalReason: cap_exceeded` on in that case.
3. **The per-call LLM-span emitter (`EmitSpans`) has no iteration index or run-ID parameter today** — `CallSpan` carries no ordinal field, and `EmitSpans`'s signature is `(ctx, tracer, calls, artifactPath, sessionID, metadataJSON, tags)`. Threading `loop.run_id`/`loop.iteration` onto these spans requires a new parameter (mirroring exactly how `sessionID`/`metadataJSON`/`tags` were added in Phase 46/`46 D-05`) plus switching the `for _, call := range calls` loop to `for i, call := range calls`.

**Primary recommendation:** Extend `EnvelopeOut`+`TerminationStub` (Go) and `envelope.py` (Python) with `TerminalReason` (D-02) and `RunEvidence` (D-03) exactly as CONTEXT.md's decisions specify, using the `pkg/dispatch/verdict.go` `ClassifyVerdict`/golden-fixture pattern as the template for both the fail-closed idiom and the Go↔Python parity test. Wire `loopRunID`/`attemptID` (D-01) from `Task.Status.Attempt` through `EnvelopeIn` (stamped at the controller's existing dispatch-envelope-build sites) → `EnvelopeOut` (echoed at the three real write sites found above) → the AGENT span (`synthesizePlannerSpan`) and the LLM-span subset (extend `EmitSpans`'s signature + `BuildReporterJob`'s Args, mirroring the `--session-id`/`--metadata`/`--tags` precedent exactly). Add `loop.*`/`evaluation.*`/`human_intervention` as bare (non-`tide.`-prefixed) `pkg/otelai` helpers — the existing `TestKeysUseSemconvModule` guard does not block these prefixes, but note the deviation explicitly in the new consts' doc comment. Extend `metriccardinality`'s analyzer and `wave_label_test.go`'s source-grep for the run-ID-shaped label list; do not add any new Prometheus metric unless the plan finds a concrete bounded-label need (Claude's Discretion, D-06).

## Architectural Responsibility Map

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|-------------|----------------|-----------|
| `loopRunID`/`attemptID` derivation | API/Backend (controller, dispatch-time) | — | `Task.Status.Attempt` + `TaskUID` are controller-owned state; derived once at dispatch, never minted in-pod |
| `loopRunID`/`attemptID` echo onto envelope | API/Backend (in-Job executor process) | — | The executor (`cmd/claude-subagent`, `cmd/stub-subagent`) is a short-lived batch process, architecturally "backend," not "client" |
| `TerminalReason` classification | API/Backend (in-Job executor + controller synth for pod-less deaths) | — | Set at each executor exit path; controller-side synthesis needed only for the `EnvelopeReadFailed`/no-envelope case (see Pitfall 2 below) |
| `RunEvidence` assembly | API/Backend (in-Job executor) | — | References already-produced Usage/Git/Result fields in-process; no new service boundary |
| AGENT-span loop attributes | API/Backend (controller, at Job-completion reconcile) | — | `synthesizePlannerSpan` runs inside `TaskReconciler`'s reconcile loop |
| LLM-span loop attributes | API/Backend (`cmd/tide-reporter`, a separate in-namespace Job) | — | `EmitSpans` runs in its own spawned Job process, reading the same PVC envelopes |
| Prometheus cardinality guard | Dev tooling (static analyzer, `go/analysis`) + API/Backend (runtime source-grep test) | — | Compile-time (`go vet`-style) + test-time double guard, no runtime service component |

No Browser/Client, CDN/Static, or Database/Storage tier involvement — this phase is entirely wire-contract + in-process observability plumbing inside the Go operator and its spawned Job binaries.

## Project Constraints (from CLAUDE.md)

- **GSD Workflow Enforcement**: all edits must route through the active GSD phase-execution flow (already satisfied — this research feeds `/gsd:plan-phase 50`).
- **`charts/tide/values.yaml` is a FIXED contract** — Phase 50 touches no chart values (no new config surface; CFG-01/02 are Phase 53). Confirm no plan task edits `values.yaml`.
- **Verify Before Claiming / `make test-int` MAKE_EXIT nuance** — any Layer B (kind) work this phase touches must check `MAKE_EXIT` and grep `^--- FAIL|^FAIL\s`, not just the Ginkgo summary line. Phase 50 is Layer A-heavy (envtest/unit), but if a plan adds any kind-cluster verification, apply this discipline.
- **"Don't predict chain terminator"** — Phase 50 is one link in a locked 48→49→50→51→52→53 chain; frame plans as this phase's iteration only.
- **Subagent effort tuning note (Opus 4.x)** — not directly applicable to Phase 50's content (no subagent template changes), but the "state instruction scope explicitly" guidance applies to any new otelai/envelope doc comments the plan authors (Opus reads literally; scope loop.*/evaluation.* population rules explicitly per key, as CONTEXT.md's D-05 already does).
- **Observe First** — this research already applied that discipline: every claim below was grepped/read directly rather than inferred from CONTEXT.md's summary.

## Standard Stack

No new external libraries. This phase extends existing internal packages only:

| Component | Current version/location | Purpose | Why no new dependency |
|-----------|---------------------------|---------|------------------------|
| `go.opentelemetry.io/otel` | v1.43 (pinned, per STACK.md) | Span/attribute API | Already used by `pkg/otelai`, `span_emission.go`, `tracesynth.go` |
| `github.com/Arize-ai/openinference/go/openinference-semantic-conventions` | vendored, pinned (per `pkg/otelai/attrs.go:23`) | Spec-backed semconv constants | `loop.*`/`evaluation.*` are explicitly NOT in this module (D-05) — they are new `pkg/otelai` custom consts, same idiom as the existing `tide.*` keys |
| `github.com/prometheus/client_golang` | v1.23 (pinned) | Metrics | No new metric is mandated (Claude's Discretion, D-06) |
| Python `pydantic`/`dataclasses` | pinned patch-exact in `cmd/tide-langgraph-verifier/requirements*.in` | Envelope mirror | `envelope.py`'s `EnvelopeIn`/write functions are hand-authored dataclasses/dict builders, not pydantic models today — extend the same style |

**Version verification:** N/A — no new package installs this phase.

## Package Legitimacy Audit

**Not applicable.** Phase 50 installs zero new external packages (Go or Python). All work extends existing, already-vetted dependencies (`go.opentelemetry.io/otel`, the vendored OpenInference semconv module, `prometheus/client_golang`, and the Python stdlib `dataclasses`/`json`). No `slopcheck`/registry verification is required.

## Architecture Patterns

### System Architecture Diagram

```
                    ┌─────────────────────────────────────────────────────┐
                    │  TaskReconciler (controller, manager pod)            │
                    │                                                       │
  Task dispatch ──▶ │  1. Derive attemptID = "{taskUID}-{attempt}"         │
  (prepareDispatch)  │     loopRunID  = taskUID   (D-01, from              │
                    │     task.Status.Attempt + podjob.JobName tuple)      │
                    │  2. Stamp both onto EnvelopeIn (new fields)          │
                    └───────────────────┬───────────────────────────────────┘
                                         │ writes in.json to PVC
                                         ▼
                    ┌─────────────────────────────────────────────────────┐
                    │  Executor Pod (Job "tide-task-{uid}-{attempt}")      │
                    │  cmd/claude-subagent/main.go  (prod)                 │
                    │  cmd/stub-subagent/main.go    (test fixtures)        │
                    │                                                       │
                    │  3. Echo attemptID/loopRunID onto EnvelopeOut        │
                    │  4. Set TerminalReason at EVERY exit path (D-02)     │
                    │     — invalid-envelope / worktree-setup-failed /     │
                    │       subagent-error / commit-failed / empty-diff /  │
                    │       claude-exit-N / success                       │
                    │  5. Assemble RunEvidence (D-03) — reference          │
                    │     Usage/Git.HeadSHA/in.Provider.Model, ADD         │
                    │     spec-ID/locking-commit/changed-files/prompt-ver  │
                    │  6. Write out.json (full) + TerminationStub          │
                    │     (<4KB, NewTerminationStub extended)              │
                    └───────────────────┬───────────────────────────────────┘
                                         │ Job terminates; controller reads
                                         │ EnvelopeOut off PVC (or reads
                                         │ nothing → EnvelopeReadFailed)
                                         ▼
                    ┌─────────────────────────────────────────────────────┐
                    │  TaskReconciler.handleJobCompletion (same manager)   │
                    │                                                       │
                    │  7. synthesizePlannerSpan → AGENT span               │
                    │     stamp loop.kind/run_id/parent_run_id/iteration/  │
                    │     candidate_version/exit_reason (D-05)             │
                    │  8. spawnTaskTraceReporterIfNeeded → spawns          │
                    │     cmd/tide-reporter Job, passing attemptID/        │
                    │     loopRunID as new BuildReporterJob Args           │
                    └───────────────────┬───────────────────────────────────┘
                                         │ separate Job, same PVC (read-only
                                         │ events.jsonl + in.json)
                                         ▼
                    ┌─────────────────────────────────────────────────────┐
                    │  cmd/tide-reporter (in-namespace reporter Job)       │
                    │  internal/reporter/tracesynth.go: EmitSpans          │
                    │                                                       │
                    │  9. ReconstructConversation → []CallSpan             │
                    │  10. for i, call := range calls: stamp               │
                    │      loop.run_id=attemptID, loop.iteration=i+1       │
                    │      on each per-call LLM span                       │
                    └───────────────────┬───────────────────────────────────┘
                                         │ OTLP export (batch ceiling 6)
                                         ▼
                              OTel Collector → Phoenix

  Prometheus side (parallel, never touched by the above):
  tools/analyzers/metriccardinality (go/analysis, compile-time)  ──┐
  internal/metrics/wave_label_test.go (source-grep, test-time)   ──┴──▶ guards
  internal/metrics/registry.go (bounded enum labels only — no run-ID label ever)
```

### Recommended Project Structure

No new files are structurally required — Phase 50 extends existing files. If the planner splits `RunEvidence`/`TerminalReason` into their own files (Claude's Discretion), the natural split mirrors the existing `pkg/dispatch/verdict.go` precedent (Phase 49 kept `GateDecision`/`Finding`/`ClassifyVerdict` in one new file, not bolted onto `envelope.go`):

```
pkg/dispatch/
├── envelope.go            # EnvelopeOut/TerminationStub gain TerminalReason + RunEvidence fields
├── envelope_test.go        # extend TestNewTerminationStub_StaysSmall, add TestEnvelopeOut_TerminalReason_*
├── terminal_reason.go      # NEW (optional, Claude's Discretion) — TerminalReason type + consts, mirrors verdict.go's shape
├── run_evidence.go         # NEW (optional, Claude's Discretion) — RunEvidence struct
└── testdata/
    └── envelope_out_golden.json  # NEW — shared Go+Python golden fixture, mirrors testdata/gate_decision_golden.json

cmd/tide-langgraph-verifier/verifier/
├── envelope.py             # write_envelope_out/write_termination_stub gain terminal_reason/run_evidence params
└── tests/test_envelope.py  # extend to read the SAME golden fixture

pkg/otelai/
├── attrs.go                 # add loop.* / evaluation.* / human_intervention consts + LoopAttributes()/EvaluationAttributes() helpers
└── attrs_test.go            # TestKeysUseSemconvModule already covers new consts (no change needed); add TestLoopAttributes/TestEvaluationAttributes

internal/reporter/
└── tracesynth.go            # EmitSpans signature grows a loop-identity param; CallSpan loop unchanged (use range index)

internal/controller/
├── span_emission.go         # synthesizePlannerSpan stamps loop.* on the AGENT span
├── reporter_jobspec.go      # ReporterOptions/BuildReporterJob grow LoopRunID/AttemptID/Iteration Args
└── task_controller.go       # spawnTaskTraceReporterIfNeeded threads task.Status.Attempt through

tools/analyzers/metriccardinality/
└── analyzer.go              # forbidden-label set grows from {"task"} to {"task","run_id","loop_run_id","run","attempt","attempt_id","trace_id","task_uid","uid"}

internal/metrics/
└── wave_label_test.go       # extend source-grep list alongside registry.go
```

### Pattern 1: Fail-closed bare-return classifier (mirror Phase 49's `ClassifyVerdict`)

**What:** A function returning a bare enum type (never `(T, error)`) so a forgetful caller cannot accidentally treat an unclassified/malformed input as the "good" value.
**When to use:** `TerminalReason` needs the identical discipline — "never a silent default" (D-02) means the zero value must be structurally impossible to read as `completed`.
**Example:**
```go
// Source: pkg/dispatch/verdict.go:102 (Phase 49, ClassifyVerdict)
func ClassifyVerdict(raw json.RawMessage) Verdict {
	if len(raw) == 0 {
		return VerdictBlocked // empty JSON
	}
	var parsed struct {
		Verdict string `json:"verdict"`
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
`TerminalReason` differs in one respect: there is no "parse a wire document" step — it is *set* at each Go exit-path call site, not classified from external input. The "never silent default" guarantee for `TerminalReason` must instead be a **test that enumerates every `EnvelopeOut{}` literal construction across the three real write sites** (`cmd/claude-subagent/main.go`, `internal/subagent/anthropic/subagent.go`, `cmd/stub-subagent/main.go`) and asserts each sets a non-empty `TerminalReason`. A source-grep test (mirroring `TestKeysUseSemconvModule`'s comment-stripped-regex idiom) is the cheapest reliable mechanism — Go's type system cannot make a struct field non-optional.

### Pattern 2: Reporter-Job Args threading (mirror Phase 46's session/metadata/tags precedent)

**What:** New per-span identity/enrichment data flows from the controller (which owns Task/Project state) to the separately-spawned `cmd/tide-reporter` process via `ReporterOptions` → `BuildReporterJob`'s `args []string` → `reporterConfig` CLI flags → `EmitSpans` parameters. Never via Env (file convention: "100% Args-based," `reporter_jobspec.go:162`).
**When to use:** Exactly the shape `loopRunID`/`attemptID`/`loop.iteration` need for the LLM-span subset (D-05's "stamp the correlating subset on per-call LLM spans").
**Example:**
```go
// Source: internal/controller/reporter_jobspec.go:292-306 (the SessionID/MetadataJSON/Tags precedent)
if opts.SessionID != "" {
    args = append(args, "--session-id="+opts.SessionID)
}
if opts.MetadataJSON != "" {
    args = append(args, "--metadata="+opts.MetadataJSON)
}
if len(opts.Tags) > 0 {
    args = append(args, "--tags="+strings.Join(opts.Tags, ","))
}
// D-01/D-05 extension follows the identical shape:
// if opts.AttemptID != "" { args = append(args, "--attempt-id="+opts.AttemptID) }
// if opts.LoopRunID != "" { args = append(args, "--loop-run-id="+opts.LoopRunID) }
```
```go
// Source: cmd/tide-reporter/main.go:594 (EmitSpans call site) — new params thread
// straight through parseFlags → reporterConfig → synthesizeSpans → EmitSpans,
// exactly like sessionID/metadataJSON/tags did in Phase 46.
```

### Pattern 3: TIDE-custom otelai attribute helper (constants + typed helper fn)

**What:** Every custom (non-spec) attribute key is a package-level `const` string, exposed through a small typed helper returning `[]attribute.KeyValue` or a single `attribute.KeyValue` — never a hand-rolled `attribute.String("loop.kind", ...)` at the call site.
**When to use:** D-05's `LoopAttributes(...)`/`EvaluationAttributes(...)` helpers.
**Example:**
```go
// Source: pkg/otelai/attrs.go:92-109 (the existing tide.* const block) — new
// consts follow the SAME block shape but WITHOUT the tide. prefix (see Pitfall
// 3 below for why that's a deliberate, not accidental, deviation):
const (
	keyLoopKind            = "loop.kind"
	keyLoopRunID           = "loop.run_id"
	keyLoopParentRunID     = "loop.parent_run_id"
	keyLoopIteration       = "loop.iteration"
	keyLoopCandidateVer    = "loop.candidate_version"
	keyLoopExitReason      = "loop.exit_reason"
	keyEvaluationResult    = "evaluation.result"
	keyEvaluationVersion   = "evaluation.version"
	keyHumanIntervention   = "human_intervention"
)

// AgentInvocation (attrs.go:240) is the closest existing shape to model
// LoopAttributes after — a plain func(...) []attribute.KeyValue with every
// field required as a positional arg (no optional-attribute pattern exists
// in this file today).
```

### Anti-Patterns to Avoid

- **Reusing `EnvelopeOut.Reason` for the terminal-reason enum.** `Reason` is an established free-text diagnostic channel (`"forced-failure"`, `"cap-hit"`, `"output-paths-violation"`, `"claude exit N: ..."`) consumed by `otelai.FailureDetail` (`span_emission.go:238`) and `TerminationStub.Reason`. D-02 is explicit that `TerminalReason` is a **new, additional** field — conflating them breaks both the existing Reason consumers and the "never silent default" testability.
- **Reusing `api/v1alpha3.ExitReason` (loop_types.go) for the Execution-loop terminal reason.** That type is explicitly documented (`loop_types.go:197-200`) as the loop-level exit vocabulary (`approved|iterationsExhausted|durationExhausted|budgetExhausted|escalated`) and is NOT the Phase 50 in-Job terminal-reason set. Do not merge the two enums.
- **Emitting a second/parallel span for loop identity.** D-05 explicitly rejects this (would double-emit into the v1.0.8 trace tree, the OBS-03 anti-pattern) — stamp attributes on the existing AGENT span + LLM-span subset only.
- **Assuming `internal/harness.Harness.Run()`/`CheckCaps` are live.** Confirmed zero production call sites (see Summary finding 1/2). A plan task that "extends `Harness.Run`'s `buildEnvelopeOut`" alone would ship dead code — the real write sites are `cmd/claude-subagent/main.go` and `internal/subagent/anthropic/subagent.go`.

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|--------------|-----|
| Fail-closed enum classification | A new ad-hoc `switch`/`if` chain with an implicit default | Mirror `pkg/dispatch/verdict.go:ClassifyVerdict`'s bare-return + explicit-default-collapse shape | Already reviewed, already has a golden-fixture test precedent, already understood by the team (Phase 49 shipped it) |
| Go↔Python wire-shape parity | A schema-generation tool or shared IDL | Hand-port field-for-field into `envelope.py`, matched by a shared JSON golden fixture (`pkg/dispatch/testdata/gate_decision_golden.json` precedent) | `pkg/dispatch/doc.go`'s import firewall makes codegen pointless — the Python image cannot import the Go package by design; hand-porting is the established, intentional pattern |
| Cross-process span-attribute threading | A new IPC/shared-state mechanism between the controller and the reporter Job | CLI Args on the spawned Job (`ReporterOptions` → `BuildReporterJob` args → `reporterConfig` flags), exactly like `--session-id`/`--metadata`/`--tags` | The file is documented "100% Args-based" (`reporter_jobspec.go:162`) — Env was deliberately rejected for this exact class of per-span identity value |
| Prometheus label cardinality prevention | A runtime cardinality-limiter/relabeling config | The existing dual guard: `go/analysis` static analyzer (compile-time) + source-grep test (test-time) | Already proven for `"task"`; extending the same two files is strictly cheaper and catches violations earlier (`go vet` time) than any runtime limiter |

**Key insight:** Every one of Phase 50's six decisions has a *direct* Phase-48/49/46 precedent already shipped in this exact codebase. This is not a "find the right library" phase — it is a "match the established local idiom" phase. Deviating from any of the four patterns above (bare-return classifier, Args-threading, otelai-const-helper, dual cardinality guard) would be inconsistent with code the reviewer/verifier will directly compare against.

## Common Pitfalls

### Pitfall 1: `internal/harness.Harness.Run()`/`CheckCaps` look like the write site but are dead code
**What goes wrong:** A plan task instructs "extend `Harness.Run`'s `buildEnvelopeOut` to set `TerminalReason`" — this compiles, tests pass (its own `harness_test.go` exercises it), but ships no behavior change in production, because nothing calls `harness.Harness{}` outside its own package (confirmed via `grep -rn "harness\.Harness\b"` across the whole repo — zero hits).
**Why it happens:** CONTEXT.md's canonical_refs cites `internal/harness/harness.go:142` as an executor write site; it IS part of the reusable "settled base" (worktree/redact/output-path helpers ARE live, imported piecemeal), but the specific `Harness` struct + `Run()`/`buildEnvelopeOut` orchestrator is orphaned scaffolding from an earlier phase.
**How to avoid:** Target the three REAL write sites: `cmd/claude-subagent/main.go` (`failEnvelope`, the `run()` function's branch points, and the inline `empty-diff` branch), `internal/subagent/anthropic/subagent.go` (`Run()`'s `waitErr != nil` branch and the `readChildCRDs` error branch), and `cmd/stub-subagent/main.go` (all `writeEnvelope(outPath, pkgdispatch.EnvelopeOut{...})` call sites — `dispatchSuccess`, `dispatchFail`, `dispatchExceedOutputPaths`, `ensureExecutorWorktree`, `commitExecutorWorktree` failure branch, `run()`'s two error branches, and every `dispatchPlannerSuccess` error branch).
**Warning signs:** A grep for `pkgdispatch.EnvelopeOut{` across `cmd/` and `internal/subagent/` turns up ~15 literal construction sites across the three real files — if a plan's task list has fewer than that, some exit paths are being missed.

### Pitfall 2: Wall-clock cap violations never reach the envelope — no in-pod write site exists to set `TerminalReason: cap_exceeded`
**What goes wrong:** EXEC-02 names `cap_exceeded` as one of five terminal reasons, implying the harness detects and reports it. In production, the ONLY live cap enforcement is `ActiveDeadlineSeconds` on the K8s Job (`internal/dispatch/podjob/jobspec.go:576`), which SIGKILLs the pod externally — the process never gets a chance to write `out.json`. The controller then takes the `EnvelopeReadFailed` branch (`task_controller.go:1196-1222`) with `out = pkgdispatch.EnvelopeOut{}` (zero value) — there is no envelope to stamp `TerminalReason` onto.
**Why it happens:** `internal/harness.CheckCaps` (iteration/input-token/output-token caps) exists and is unit-tested but is never invoked from `internal/subagent/anthropic/subagent.go` or `cmd/claude-subagent/main.go` — no in-process cap enforcement of any kind runs in the real dispatch path today. Only the K8s-level wall-clock deadline is live.
**How to avoid:** The plan must explicitly decide (this is a genuine open question, not resolved by CONTEXT.md's decisions): (a) does Phase 50 wire `CheckCaps` into the live `anthropic.Run()` loop so at least token/iteration caps produce a real in-pod `TerminalReason: cap_exceeded` envelope, and/or (b) does the controller synthesize `TerminalReason: cap_exceeded` itself when it observes a Job's `JobFailed` condition with reason `DeadlineExceeded` and no `EnvelopeOut` was ever written? Option (b) touches `task_controller.go`'s `EnvelopeReadFailed` branch, which is reconciler logic — CONTEXT.md's scope guardrail excludes reconciler/halt-gate work from Phase 50 in general (that's Phase 51's territory), but this specific case is NOT `ConditionVerifyHalt`/gate wiring — it's the only place `cap_exceeded` can ever be produced for a wall-clock kill. Flag this explicitly for the planner to scope in or defer with a documented reason; do not let it silently fall through the cracks as "someone else's phase."
**Warning signs:** If the plan's fail-closed test only exercises in-pod exit paths (never an `ActiveDeadlineSeconds`-killed Job), it will pass while `cap_exceeded` remains structurally unreachable for the most common real-world cap violation (wall-clock, not token/iteration).

### Pitfall 3: `loop.*`/`evaluation.*` keys deviate from the file's own documented "only tide.* may be hand-rolled" convention — but the guard test does NOT block them
**What goes wrong:** `pkg/otelai/attrs.go:85-91`'s doc comment says "Only `tide.*` literals may remain hand-rolled (D-05)" [that file's own D-05, unrelated to this phase's D-05], and `TestKeysUseSemconvModule`'s failure message repeats "only tide.* keys may be hand-rolled." A naive reading suggests `loop.kind`/`evaluation.result` must be renamed `tide.loop.kind` to comply. They must NOT — REQUIREMENTS.md's OBS-01 and CONTEXT.md's D-05 lock the exact literal strings `loop.kind`, `loop.run_id`, etc. (no `tide.` prefix), because Phase 51's LangGraph evaluator (a non-TIDE-branded process) will emit spans using the SAME keys — a `tide.` prefix would be semantically wrong for a cross-implementation loop-native convention.
**Why it happens:** `TestKeysUseSemconvModule`'s regex (`attrs_test.go:351`) only forbids literals beginning with `llm.`, `openinference.`, `gen_ai.`, or `agent.` — `loop.` and `evaluation.` are NOT in that forbidden list, so the guard passes either way. The doc comment is aspirational/historical, not mechanically enforced beyond those four prefixes.
**How to avoid:** Add the new `loop.*`/`evaluation.*`/`human_intervention` consts to `attrs.go` with a doc comment explicitly noting they are intentionally NOT `tide.`-prefixed (loop-native, cross-vendor convention — Phase 51's LangGraph evaluator spans reuse the same keys), so a future reader doesn't "fix" them into the `tide.` namespace. Confirm `TestKeysUseSemconvModule` still passes after the addition (it will, since neither prefix is forbidden) but do not treat that pass as validating the naming choice — it wasn't designed to.
**Warning signs:** A PR review comment asking "why don't these have the tide. prefix like everything else in this file?" — the answer needs to already be in the code comment, not re-derived from CONTEXT.md.

### Pitfall 4: `RunEvidence`'s "model/prompt/runtime version" field has no existing source anywhere in the codebase
**What goes wrong:** D-03 names this the "notable gap" — `span_emission.go:135-138`'s comment confirms "the envelope never carried a model field at any layer" — but research found there is ALSO no prompt-template version marker (`internal/subagent/common/prompt_templates.go`'s `LoadPromptTemplate` has no version const/hash) and no runtime `claude --version` capture anywhere in the live path (`pricing.go:62` mentions a manually-recorded probe date in a comment only, not a programmatic capture).
**Why it happens:** These three sub-fields were never needed before because nothing consumed them; Phase 50 is the first consumer.
**How to avoid:** Model name is a trivial reference-only win — `in.Provider.Model` is already on `EnvelopeIn` (`pkg/dispatch/provider.go:42-45`), just never echoed onto `EnvelopeOut`; the executor already has `in` in scope at every write site. Prompt-template version and runtime (CLI) version are genuinely NEW data the plan must source: either (a) exec `claude --version` once at startup and capture stdout (small proc-exec cost, always-current), or (b) a compiled-in constant matching the pinned CLI version (`STACK.md`: "Claude Code CLI ≥ v2.1.139") that drifts silently if the pin bumps without updating the constant. Flag this as a plan-time decision, not a research-resolved fact — CONTEXT.md's Claude's Discretion list does not cover it explicitly.
**Warning signs:** A `RunEvidence.PromptVersion` field that's always empty string, or hardcoded to a value that never gets updated when the CLI pin changes.

### Pitfall 5: `EmitSpans`/`CallSpan` have no ordinal — `loop.iteration` on LLM spans requires a signature change, not just a new attribute call
**What goes wrong:** A plan task that says "stamp `loop.iteration` on the LLM spans" without also changing `EmitSpans`'s loop from `for _, call := range calls` to `for i, call := range calls` (or adding an ordinal to `CallSpan`) has no iteration number to stamp — `CallSpan` (`tracesynth.go:91-99`) carries `Model`/`InputMessages`/`OutputMessages`/`Usage`/`StartTime`/`EndTime`/`Degraded`/`TimingSynthetic` only.
**Why it happens:** CallSpan reconstruction (`ReconstructConversation`) was designed before any per-call ordinal was needed; the slice order IS the iteration order, it's just never been surfaced as an attribute.
**How to avoid:** Use the range index (`i+1`, 1-indexed to match `LoopStatus.Iteration`'s documented "1-indexed once dispatched" convention, `loop_types.go:93-94`) — no new `CallSpan` field is needed, just a signature change to `EmitSpans` to accept `attemptID`/`loopRunID` and derive `loop.iteration` from the loop index.
**Warning signs:** A `CallSpan.Iteration` field added redundantly when the range index already provides it.

### Pitfall 6: The stub-subagent's `dispatchFail`/`forced-failure` result has no clean 1:1 terminal-reason mapping
**What goes wrong:** `cmd/stub-subagent/main.go:dispatchFail` writes `Result: "forced-failure"` — this is the ONLY stub testMode that simulates a generic task failure, and none of the five enum values (`completed|cap_exceeded|blocked|tool_failure|invalid_output`) is an obvious fit (it's not a cap, not an output-path violation, not literally a tool-exec failure, not malformed output — it's a deliberately-injected generic failure for test fixtures).
**Why it happens:** The stub's test-mode vocabulary predates the terminal-reason enum by many phases and was never designed against it.
**How to avoid:** The plan must pick one (most defensible: `tool_failure`, treating "forced-failure" as a generic non-agent-output failure bucket) and document the mapping table exhaustively — CONTEXT.md explicitly defers this ("Exit-condition mapping (to be finalized at plan time)"). Build the mapping as a literal table in the plan covering ALL of: stub-subagent's 6 result strings (`invalid-envelope`, `worktree-setup-failed`, `success`, `forced-failure`, `output-paths-violation`, `commit-failed`, `internal-error`) AND claude-subagent's 6 result strings (`invalid-envelope`, `worktree-setup-failed`, `subagent-error`, `commit-failed`, `empty-diff`, plus the anthropic-package's inline `"claude exit N: ..."` Reason and the planner-only `"read child CRDs: ..."` Reason) — 12+ distinct current failure classes collapsing into 4 non-`completed` enum values.
**Warning signs:** A mapping table that only covers the 5 example conditions CONTEXT.md sketches ("normal finish", "cap hit", "policy/output-path violation", "tool subprocess/exec failure", "unparseable output") without cross-referencing the actual `Result`/`Reason` string literals found in this research.

## Code Examples

### The exact current EnvelopeOut/TerminationStub shape D-02/D-03 extend

```go
// Source: pkg/dispatch/envelope.go:176-250, :416-468 (verified this session)
type EnvelopeOut struct {
	APIVersion  string          `json:"apiVersion"`
	Kind        string          `json:"kind"`
	TaskUID     string          `json:"taskUID"`
	ExitCode    int             `json:"exitCode"`
	Result      string          `json:"result"`
	Reason      string          `json:"reason"`       // free-text diagnostic — NOT the new enum
	Usage       Usage           `json:"usage"`
	Artifacts   []string        `json:"artifacts"`
	CompletedAt time.Time       `json:"completedAt"`
	ChildCRDs   []ChildCRDSpec  `json:"childCRDs,omitempty"`
	Git         *GitOutput      `json:"git,omitempty"`
	ChildCount  int             `json:"childCount,omitempty"`
	SharedContext string        `json:"sharedContext,omitempty"`
	Verdict     *GateDecision   `json:"verdict,omitempty"`
	// D-02 adds: TerminalReason TerminalReason `json:"terminalReason"`
	// D-03 adds: RunEvidence    *RunEvidence    `json:"runEvidence,omitempty"`
}

type TerminationStub struct {
	ExitCode          int    `json:"exitCode"`
	Reason            string `json:"reason"`
	Usage             Usage  `json:"usage"`
	HeadSHA           string `json:"headSHA,omitempty"`
	ChildCount        int    `json:"childCount"`
	GateDecision      string `json:"gateDecision,omitempty"`
	FindingsCount     int    `json:"findingsCount,omitempty"`
	HighSeverityCount int    `json:"highSeverityCount,omitempty"`
	// D-02 adds: TerminalReason string `json:"terminalReason,omitempty"`
	// D-03 adds: a bounded RunEvidence summary subset (NOT the full struct —
	// mirror how GateDecision→{gateDecision,findingsCount,highSeverityCount}
	// was flattened, per NewTerminationStub:484-506)
}
```

### The `<4KB` truncation test D-02/D-03 must extend

```go
// Source: pkg/dispatch/envelope_test.go:788-828 (TestNewTerminationStub_StaysSmall)
// Builds a deliberately oversized EnvelopeOut (50 ChildCRDs, 10KB Result, 50
// high-severity findings) and asserts json.Marshal(stub) < 4096 bytes. D-02/D-03
// must add TerminalReason + the bounded RunEvidence summary to this same
// "deliberately oversized" fixture (e.g. a maximally-long changed-file manifest)
// and re-assert the same < 4096 bound.
if len(data) >= 4096 {
	t.Errorf("TerminationStub JSON size = %d bytes, want < 4096 (termination-message budget)", len(data))
}
```

### The Python truncation loop D-02/D-03's new fields must survive

```python
# Source: cmd/tide-langgraph-verifier/verifier/envelope.py:163-215
# write_termination_stub progressively truncates `reason` (the only unbounded
# field) until the doc is strictly under TERMINATION_STUB_MAX_BYTES (4096),
# matching Go's `< 4096` (not `<= 4096`). New bounded fields (terminal_reason,
# a run-evidence summary) must be added the SAME way gate_decision/
# findings_count/high_severity_count were (Phase 49): joined into the dict
# unconditionally since they are bounded-by-construction, never subject to the
# truncation loop (only `reason` is unbounded free text).
```

### The golden-fixture Go↔Python parity pattern to replicate

```go
// Source: pkg/dispatch/verdict_test.go:25-39 (TestGateDecision_GoldenFixtureRoundTrip)
func TestGateDecision_GoldenFixtureRoundTrip(t *testing.T) {
	golden, err := os.ReadFile("testdata/gate_decision_golden.json")
	// ... unmarshal, assert shape ...
}
```
```python
# Source: cmd/tide-langgraph-verifier/verifier/tests/test_verdict.py:21-28
def test_golden_fixture_round_trip() -> None:
    """Reads the shared Go golden fixture pkg/dispatch/testdata/gate_decision_golden.json."""
    golden_bytes = verdict.GOLDEN_FIXTURE.read_bytes()
    decoded = verdict.GateDecision.model_validate_json(golden_bytes)
```
D-02/D-03 should add a NEW shared golden fixture (e.g. `pkg/dispatch/testdata/envelope_out_golden.json`) covering `TerminalReason` + `RunEvidence`, read by both a new Go test and a new/extended Python test — do not overload `gate_decision_golden.json`, which is scoped to the verdict sub-document only.

### `Task.Status.Attempt` + the `JobName` identity tuple D-01 derives from

```go
// Source: api/v1alpha3/task_types.go:157
Attempt int `json:"attempt,omitempty"`

// Source: internal/dispatch/podjob/names.go:37
func JobName(taskUID types.UID, attempt int) string {
	return fmt.Sprintf("tide-task-%s-%d", taskUID, attempt)
}
// D-01: attemptID = "{taskUID}-{attempt}" (the same tuple, not the "tide-task-" prefix)
//       loopRunID = taskUID alone
```

## State of the Art

| Old Approach | Current Approach | When Changed | Impact |
|--------------|-------------------|---------------|--------|
| No terminal-reason enum — only free-text `Reason` | D-02 adds a closed, fail-closed `TerminalReason` enum | This phase | Machine-checkable exit classification for the first time |
| Envelope has no model/prompt-version field | D-03 adds `RunEvidence` referencing `in.Provider.Model` + new prompt/runtime version capture | This phase | Closes the "notable gap" `span_emission.go:138` documents |
| AGENT span carries level/role/session metadata only | D-05 adds loop-identity attributes (`loop.*`) alongside | This phase | Prepares the trace schema for Phase 51's Task loop before that loop exists (defined-but-empty `evaluation.*` keys) |
| `"task"` is the only Prometheus label the cardinality guard rejects | D-06 extends the forbidden-label set to run/attempt/trace-ID-shaped names | This phase | Closes the same class of unbounded-cardinality risk one level down (per-attempt, not just per-task) |

**Deprecated/outdated:** Nothing in this phase deprecates prior work — Phase 49's `LoopPolicy`/`LoopStatus`/`GateDecision` and Phase 46/47's span-enrichment triple (`session.id`/`metadata`/`tags`) are extended, not replaced.

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|-------|---------|-----------------|
| A1 | `internal/harness.CheckCaps` was never wired into a production call site (only iteration/token caps are unenforced; wall-clock IS enforced, at the K8s Job level) | Summary finding 2, Pitfall 2 | Verified via exhaustive grep (`Caps\.` / `CheckCaps` / `MaxIterations` across `internal/subagent/anthropic/`) — HIGH confidence, not flagging as assumed, but noting the grep could theoretically miss a reflection-based or generated call site (none found in this codebase's style) |
| A2 | The `tool_failure` mapping for stub-subagent's `forced-failure`/claude-subagent's generic exit codes is the best-fit enum value | Pitfall 6 | If the planner instead splits these into `blocked` or a 6th unlisted category, downstream Phase 51 evaluator-feedback logic that keys off `TerminalReason` may need re-tuning; LOW risk since CONTEXT.md already defers this exact mapping to plan time |
| A3 | `loop.*`/`evaluation.*` keys deliberately should NOT get a `tide.` prefix (cross-vendor Phase 51 LangGraph reuse) | Pitfall 3 | This is REQUIREMENTS.md/CONTEXT.md's own explicit literal-key naming (not this research's invention) — cross-checked against the file's own guard test regex; LOW risk, high confidence |

**All three items above are grounded in direct repo reads this session (not training-data assumptions) — tagged here per the "risk if wrong" framing the planner should sanity-check, not because the underlying claims are unverified.**

## Open Questions (RESOLVED at planning — see Plans 50-03/50-04/50-06)

1. **Does Phase 50 wire `CheckCaps` (iteration/token) into the live `anthropic.Run()` path, and/or does the controller synthesize `cap_exceeded` for `ActiveDeadlineSeconds`-killed Jobs?**
   - **RESOLVED — BOTH halves (Plan 50-04 + Plan 50-06).** In-pod: `harness.CheckCaps` is wired into `anthropic.Run()`'s post-`ParseStream` step so an iteration/token cap violation produces a real `cap_exceeded` envelope (50-04). Controller-side: the `EnvelopeReadFailed`/no-envelope branch synthesizes `TerminalReason: cap_exceeded` for `DeadlineExceeded`-killed Jobs (the only place a wall-clock SIGKILL can be classified, since the pod never writes `out.json`) via `synthesizeNoEnvelopeOut`, fail-closed on any other Job-failure reason (50-06). `cap_exceeded` is therefore never a theoretically-reachable-but-dead value.
   - What we know: neither path exists today; `CheckCaps` is unit-tested but orphaned; wall-clock kills bypass envelope-writing entirely.
   - What's unclear: whether this is in Phase 50's "hardening" scope or is implicitly deferred (it's not named in CONTEXT.md's "Deliberately NOT in this phase" list, but it also isn't explicitly claimed).
   - Recommendation: the plan should explicitly scope this in or out with a one-line rationale, rather than silently shipping `cap_exceeded` as a theoretically-reachable-but-practically-dead enum value. At minimum, wiring `CheckCaps` into `anthropic.Run()`'s post-`ParseStream` step (comparing `usage.Iterations`/`usage.InputTokens`/`usage.OutputTokens` against `in.Caps`) is a small, self-contained addition with an existing, tested helper (`harness.CheckCaps`) ready to reuse — likely the highest-value, lowest-risk EXEC-02 completion.

2. **Where does the prompt-template version number come from?**
   - **RESOLVED (Plan 50-04).** A compiled-in package-level `PromptTemplateVersion` const (bumped by hand alongside template content) + the CLI runtime version from a `claude --version` probe — the recommendation below, adopted as-is.
   - What we know: no version marker exists on the compiled-in Go templates (`internal/subagent/common/prompt_templates.go`) today.
   - What's unclear: whether the plan adds a version const per template family, a content-hash, or a single package-level version bumped manually.
   - Recommendation: a simple package-level `const PromptTemplateVersion = "v1"` (bumped by hand alongside template content changes) is the lowest-friction option and matches this repo's general preference for explicit compiled-in constants over auto-derived hashes (e.g., `highSeverityFindingToken` in `envelope.go:474`).

3. **Does the plan add any new Prometheus metric at all (D-06's open discretion)?**
   - **RESOLVED — guard-only, no new metric (Plan 50-03).** The minimal-scope reading below was adopted: harden the dual cardinality guard (analyzer + runtime grep) only; the first loop-scoped metric waits for Phase 51/52's real loop-outcome consumer.
   - What we know: LOOP-03/five-loop-model explicitly says run-native detail belongs in traces, not metrics; the existing 7 TELEM-03 metrics already carry `{project,phase,plan,wave}` — no task/run-scoped metric exists today.
   - What's unclear: whether OBS-02's "metrics use bounded labels (loop kind, exit reason, evaluator type, risk tier)" implies a NEW metric (e.g. a loop-outcome counter) is expected this phase, or whether it's purely a guard against future violations.
   - Recommendation: given ROADMAP's 5 success criteria for Phase 50 mention "proven by a label-cardinality test" (criterion 5) without naming a new metric, the minimal-scope reading is: harden the guard only, add no new metric, and let Phase 51/52 (which will have real loop-outcome data — `EvaluationSummary.Decision`, `LoopStatus.ExitReason`) add the first loop-scoped metric when there's a real consumer. This matches this repo's repeated "never ship a speculative superset ahead of a real consumer" principle (`loop_types.go:35-36`, `:90-91`).

## Environment Availability

Not applicable — Phase 50 has no external tool/service dependencies beyond the already-provisioned Go toolchain, Python `uv`/pytest environment (`make test-langgraph-verifier`), and the existing envtest/kind infrastructure, all confirmed present per STATE.md's "Local toolchain facts" note (golangci-lint v2.11.4 + envtest run fine locally).

## Validation Architecture

### Test Framework
| Property | Value |
|----------|-------|
| Framework (Go) | stdlib `testing` + Ginkgo v2.28/Gomega (envtest, Layer A) |
| Framework (Python) | pytest (`cmd/tide-langgraph-verifier/verifier/tests/`) |
| Config file | none dedicated — `go test ./...` + `make test-langgraph-verifier` |
| Quick run command | `go test ./pkg/dispatch/... ./pkg/otelai/... ./internal/reporter/... ./internal/controller/... ./tools/analyzers/metriccardinality/... ./internal/metrics/...` |
| Full suite command | `make test` (unit) + `make test-int-fast` (Layer A envtest) + `make test-langgraph-verifier` (Python) + `make lint` (golangci-lint + import firewalls + `tide-lint` custom analyzers, includes `metriccardinality`) |

### Phase Requirements → Test Map
| Req ID | Behavior | Test Type | Automated Command | File Exists? |
|--------|----------|-----------|---------------------|--------------|
| EXEC-01 | `loopRunID`/`attemptID` round-trip EnvelopeIn→EnvelopeOut→spans; span-per-iteration | unit + envtest | `go test ./pkg/dispatch/... ./internal/reporter/... ./internal/controller/... -run TestEnvelope.*RunID\|TestEmitSpans` | Partial — `envelope_test.go`/`tracesynth_test.go` exist, new subtests needed, Wave 0 |
| EXEC-02 | `TerminalReason` set at every exit path, fail-closed, never silent default | unit | `go test ./pkg/dispatch/... ./cmd/claude-subagent/... ./cmd/stub-subagent/... ./internal/subagent/anthropic/... -run TestTerminalReason` | ❌ Wave 0 — new test file(s) needed per the 3 real write sites |
| EXEC-03 | `RunEvidence` populates referenced fields, stays bounded (<4KB in stub) | unit | `go test ./pkg/dispatch/... -run TestRunEvidence\|TestNewTerminationStub_StaysSmall` | Partial — extend existing `TestNewTerminationStub_StaysSmall` |
| EXEC-04 | No field/path asserts Task correctness; doc-comment + guard test | unit (static) | `go test ./pkg/dispatch/... -run TestEnvelopeOut_NoCorrectnessField` | ❌ Wave 0 — mirrors `TestTerminationStub_NoForbiddenFields` (envelope_test.go:841) and `TestLoopStatus_NoForbiddenFields` (Phase 49 precedent) |
| OBS-01 | 9 span keys emitted (or defined-but-empty) on AGENT + LLM spans | unit | `go test ./pkg/otelai/... ./internal/controller/... ./internal/reporter/... -run TestLoopAttributes\|TestEvaluationAttributes\|TestSynthesizePlannerSpan_Loop\|TestEmitSpans_Loop` | ❌ Wave 0 — new otelai helpers + their tests |
| OBS-02 | Run IDs never enter a Prometheus label; static analyzer + runtime guard | static analysis + unit | `go test ./tools/analyzers/metriccardinality/... ./internal/metrics/... -run TestWaveLabel\|TestAnalyzer` | Partial — extend `analyzer_test.go`'s testdata `badlabels`/`goodlabels` fixtures + `wave_label_test.go` |

Go↔Python envelope parity (D-02/D-03) is validated by a NEW shared golden fixture test pair (not in the table above since it's cross-cutting, not tied to one requirement): `go test ./pkg/dispatch/... -run TestEnvelopeOut_GoldenFixtureRoundTrip` + `make test-langgraph-verifier` (extend `test_envelope.py`).

### Sampling Rate
- **Per task commit:** the relevant package's `go test ./<pkg>/...` (few seconds) + `make test-langgraph-verifier` when Python files change
- **Per wave merge:** `go test ./...` + `make test-int-fast` + `make lint`
- **Phase gate:** `make lint` (golangci-lint, import firewalls, `tide-lint` custom analyzers incl. `metriccardinality`) + `go vet ./...` + full unit + Layer A envtest green before `/gsd:verify-work` — per STATE.md's "GSD phase verification never runs the ci.yaml-only gates" lesson, `make lint` must be explicitly run during phase verification, not deferred to release pre-flight.

### Wave 0 Gaps
- [ ] `pkg/dispatch/terminal_reason_test.go` (or added to `envelope_test.go`) — covers EXEC-02 fail-closed enumeration across all real write sites
- [ ] `pkg/dispatch/testdata/envelope_out_golden.json` — new shared Go+Python golden fixture for `TerminalReason`+`RunEvidence`
- [ ] `pkg/otelai/attrs_test.go` additions — `TestLoopAttributes`/`TestEvaluationAttributes` for the new helpers
- [ ] `tools/analyzers/metriccardinality/testdata/src/badlabels/registry.go` — add fixtures for each new forbidden label (`run_id`, `loop_run_id`, `attempt`, etc.)
- [ ] Framework install: none — all frameworks already present and exercised by existing tests in the touched packages

## Security Domain

`security_enforcement` is absent from `.planning/config.json` (treated as enabled per protocol), but this phase's blast radius is internal wire-contract + observability plumbing with no new trust boundary, no new user input, and no new auth surface.

### Applicable ASVS Categories

| ASVS Category | Applies | Standard Control |
|----------------|---------|--------------------|
| V2 Authentication | No | No auth surface touched |
| V3 Session Management | No | N/A |
| V4 Access Control | No | No RBAC/permission surface touched |
| V5 Input Validation | Partial | `TerminalReason`/`RunEvidence` fields are populated from already-trusted in-process data (own process's `Usage`, `Git.HeadSHA`, `Provider.Model`) or bounded git output (`git --name-status`) — no new externally-supplied string is written verbatim to a trace/log without the existing redact pipeline (`internal/harness/redact.String`, already applied to span content upstream of `otelai.LLMInputMessages`/`LLMOutputMessages`). The changed-file manifest and prompt/runtime version strings should go through the same bounded-length discipline `TerminationStub`'s existing truncation loop already enforces — no new unbounded free-text field should be added without a truncation bound. |
| V6 Cryptography | No | No new crypto surface |

### Known Threat Patterns for this stack

| Pattern | STRIDE | Standard Mitigation |
|---------|--------|-----------------------|
| Unbounded `RunEvidence` field (e.g. an unbounded changed-file list from a pathological worktree) blowing the 4KB `TerminationStub` budget or the reporter's OTLP batch ceiling | Denial of Service | Bound every new field the same way `NewTerminationStub` already bounds `Reason` (progressive truncation) and `Verdict.Findings` (counts only, never the array) — apply the identical discipline to any new `RunEvidence` summary subset placed on `TerminationStub` |
| A span attribute payload growth silently exceeding the reporter's OTLP export batch ceiling (`OTEL_BSP_MAX_EXPORT_BATCH_SIZE=6`, `reporter_jobspec.go:322`, sized for a ~512KiB whole-span cap × 6 = 3MiB, ~25% under the 4MiB gRPC ceiling) | Denial of Service (export failure) | The 9 new `loop.*`/`evaluation.*` attributes are small scalars (strings/ints/bools) — negligible individual growth, but the plan should note the new total per-span attribute count in a comment near `OTEL_BSP_MAX_EXPORT_BATCH_SIZE` so a future reviewer re-checks the size budget, not silently assume it still holds |
| A malformed/adversarial `TerminalReason` string reaching Prometheus as a label if a future metric mistakenly keys off it directly | Information Disclosure / cardinality DoS | Exactly what D-06's dual guard (analyzer + runtime test) exists to prevent — `TerminalReason` (bounded 5-value enum) is actually SAFE as a metric label by construction (it's not run-ID-shaped), unlike `loopRunID`/`attemptID` — the guard's forbidden-list should target the latter, not the former; do not over-broadly forbid `TerminalReason` from ever appearing in a label if a future phase wants a `terminal_reason` bounded-enum metric label |

## Sources

### Primary (HIGH confidence — direct file reads this session)
- `pkg/dispatch/envelope.go` (full read) — EnvelopeOut/TerminationStub/Usage/GitOutput/VerifyContext/NewTerminationStub shapes
- `pkg/dispatch/envelope_test.go` (test list + `TestNewTerminationStub_StaysSmall` full read) — the `<4KB` invariant test
- `pkg/dispatch/verdict.go` (`ClassifyVerdict` full read) — fail-closed bare-return idiom
- `pkg/dispatch/verdict_test.go` / `cmd/tide-langgraph-verifier/verifier/tests/test_verdict.py` (grep) — golden fixture parity pattern
- `cmd/tide-langgraph-verifier/verifier/envelope.py` (full read) — `write_envelope_out`/`write_termination_stub`/truncation loop
- `internal/harness/harness.go`, `internal/harness/envelope_io.go`, `internal/harness/commit.go` (full reads) — confirmed `Harness.Run()` is orphaned; `CommitWorktree`'s git-diff idiom
- `internal/subagent/anthropic/subagent.go` (lines 180-440 read) — the real production `Run()` exit paths
- `cmd/claude-subagent/main.go` (full read) — the real production `run()`/`failEnvelope()` write sites
- `cmd/stub-subagent/main.go` (full read) — the real test-fixture write sites and their Result/Reason vocabulary
- `internal/controller/span_emission.go` (full read) — `synthesizePlannerSpan`/`buildLevelEnrichment`, the "model field never carried" comment
- `internal/reporter/tracesynth.go` (lines 85-130, 550-665 read) — `CallSpan`, `EmitSpans` signature and loop body
- `cmd/tide-reporter/main.go` (lines 60-220, 328-430 read) — `reporterConfig`, `synthesizeSpans`, the Args-threading precedent
- `internal/controller/reporter_jobspec.go` (lines 140-330 read) — `ReporterOptions`, `BuildReporterJob`, the OTLP batch ceiling comment
- `internal/controller/task_controller.go` (lines 1082, 1176-1300, and grep for `EnvelopeReadFailed`/`DeadlineExceeded` across the file) — `handleJobCompletion`, `spawnTaskTraceReporterIfNeeded`, the no-envelope controller-side branch
- `pkg/otelai/attrs.go` (full read) + `pkg/otelai/attrs_test.go` (`TestKeysUseSemconvModule` + surrounding tests read) — the custom-key idiom and the exact guard regex
- `tools/analyzers/metriccardinality/analyzer.go` (full read) — the exact `"task"`-literal rejection logic
- `internal/metrics/wave_label_test.go` (full read) — the runtime source-grep guard
- `internal/metrics/registry.go` (grep for label sets) — the bounded-enum-label idiom
- `api/v1alpha3/loop_types.go` (full read) — `LoopPolicy`/`LoopStatus`/`ExitReason`, confirming `ExitReason` ≠ Phase 50's `TerminalReason`
- `api/v1alpha3/task_types.go` (grep) — `Task.Status.Attempt`
- `internal/dispatch/podjob/names.go` (full read) — `JobName` identity tuple
- `internal/dispatch/podjob/jobspec.go` (grep) — `ActiveDeadlineSeconds` wall-clock enforcement
- `docs/templates/minimal-loop-project/evals/README.md` (full read) — the canonical run-evidence contract list
- `Makefile` (grep) — `lint`/`test-langgraph-verifier`/`verify-dispatch-imports`/`verify-langgraph-pins` targets
- `.github/workflows/ci.yaml` (grep) — confirmed CI job structure; `examples_image_pin_test.go` and `verify-dashboard-freshness` NOT applicable to this phase's file surface
- `.planning/phases/50-execution-loop-hardening-loop-native-observability/50-CONTEXT.md`, `.planning/REQUIREMENTS.md`, `.planning/STATE.md`, `.planning/ROADMAP.md` §Phase 49/50/51 — upstream scope/decisions

### Secondary (MEDIUM confidence)
None — this phase required no external/web research; all findings are direct repo reads.

### Tertiary (LOW confidence)
None.

## Metadata

**Confidence breakdown:**
- Standard stack: HIGH — no new dependencies, all extensions of pinned/vendored existing packages
- Architecture: HIGH — every seam traced to exact file:line, including two load-bearing corrections to CONTEXT.md's scout (dead `Harness.Run`, unwired `CheckCaps`)
- Pitfalls: HIGH — all 6 pitfalls are grounded in direct reads (not inferred), including the wall-clock-cap gap which is a genuine, previously-undocumented architectural finding

**Research date:** 2026-07-18
**Valid until:** 30 days (stable internal codebase mechanics; re-verify if Phase 49's envelope/verdict shapes or Phase 46-48's span/reporter plumbing change before Phase 50 planning executes)
