# Phase 50: Execution-Loop Hardening + Loop-Native Observability - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md — this log preserves the alternatives considered.

**Date:** 2026-07-18
**Phase:** 50-execution-loop-hardening-loop-native-observability
**Mode:** `--auto` (fully autonomous — all 6 gray areas auto-selected, each resolved to the recommended default; no interactive prompts)
**Areas discussed:** loopRunID/attemptID identity, Terminal reason field, Run-evidence envelope, Completion-is-belief, Loop-native span attributes, Prometheus label cardinality

---

## loopRunID / attemptID identity (EXEC-01)

| Option | Description | Selected |
|--------|-------------|----------|
| Deterministic derivation | Derive `attemptID`=`{taskUID}-{attempt}`, `loopRunID`=`taskUID` from existing `Status.Attempt` + `podjob.JobName` tuple; persist nothing new | ✓ |
| Mint + persist a fresh run ID | Mint a random `loopRunID` once and store it on `Task.Status` | |

**Auto-selected:** Deterministic derivation (recommended). Matches the project's "resumption = re-derive, never persist the schedule" principle; reuses the grep-unambiguous `JobName` tuple; keeps etcd a state store (LOOP-03). `LoopStatus.ParentRunID` already exists to seed the parent.
**Notes:** "A span per tool/action iteration" reuses the existing per-call LLM-span synthesis (`tracesynth.go:EmitSpans`) — the iteration spans already exist; Phase 50 only stamps the correlating subset.

---

## Terminal reason field (EXEC-02)

| Option | Description | Selected |
|--------|-------------|----------|
| New typed `TerminalReason` enum | Dedicated string-enum field distinct from free-text `Reason`; fail-closed on the zero value; a test asserts every exit path sets it | ✓ |
| Overload existing `Reason` | Reuse the free-text `Reason` field with the enum values | |

**Auto-selected:** New typed enum (recommended). The scout confirms `Reason` is a free-text diagnostic channel; a dedicated typed field makes "never a silent default" testable and mirrors the Phase-49 fail-closed classifier discipline. `TerminalReason` doubles as the `loop.exit_reason` span attribute.
**Notes:** Mirror the field + values in the Python verifier envelope (Go↔Python duality). Exit mapping: complete→`completed`, cap→`cap_exceeded`, policy/gate→`blocked`, tool exec→`tool_failure`, bad output→`invalid_output`.

---

## Run-evidence envelope (EXEC-03)

| Option | Description | Selected |
|--------|-------------|----------|
| `RunEvidence` sub-struct, references-only | One bounded struct mapping `evals/README.md` 1:1; references already-produced artifacts, adds only the genuinely-missing fields (SpecID/locking-commit, commands+evaluator versions, changed-file manifest, model/prompt version) | ✓ |
| Flat fields on `EnvelopeOut` | Sprinkle the missing evidence fields directly onto `EnvelopeOut` | |

**Auto-selected:** `RunEvidence` sub-struct (recommended). Maps the contract list legibly, keeps `EnvelopeOut` readable, and makes the "bounded/reference-only" invariant reviewable in one place. Schema parity both languages; full Go executor population now; the verifier's full population is Phase 51.
**Notes:** Changed-file manifest = bounded `git --name-status` list, not diffs. Model/prompt version is the notable gap (the envelope "never carried a model field at any layer").

---

## Completion-is-belief (EXEC-04)

| Option | Description | Selected |
|--------|-------------|----------|
| Fold into `TerminalReason==completed` + negative guard | No new field; `completed` = "agent believes complete"; deliver a belief-only doc-comment + a guard test asserting the envelope carries no Task-correctness field | ✓ |
| Add an `AgentReportedComplete` boolean | A separate belief flag alongside the terminal reason | |

**Auto-selected:** Fold into `TerminalReason` (recommended). A second completion signal invites the "which is authoritative?" ambiguity EXEC-04 exists to kill. The deliverable is the negative invariant, not a new field.
**Notes:** Phase 50 does NOT rewire today's controller exit-0→`Complete` behavior — that re-route through the verifier is Phase 51; Phase 50 only locks the envelope semantics + guard.

---

## Loop-native span attributes (OBS-01)

| Option | Description | Selected |
|--------|-------------|----------|
| otelai helpers; AGENT span primary + LLM-span subset | Define `loop.*`/`evaluation.*`/`human_intervention` as `pkg/otelai` helpers (semconv-guard); stamp loop identity on the AGENT span, correlating subset on per-call LLM spans; populate execution-known keys, leave `evaluation.*` defined-but-empty until Phase 51 | ✓ |
| A separate loop span | Emit a new parallel span carrying the loop attributes | |

**Auto-selected:** otelai helpers on the existing spans (recommended). The v1.0.8 AGENT span already models the attempt; a parallel span would double-emit (the OBS-03 anti-pattern). Keys defined via the semconv module to pass `TestKeysUseSemconvModule`.
**Notes:** `evaluation.result`/`evaluation.version`/`human_intervention` are defined but empty in Phase 50 — the verifier that populates them is Phase 51 (OBS-03). `loop.candidate_version` = the attempt's locking/head commit.

---

## Prometheus label cardinality (OBS-02)

| Option | Description | Selected |
|--------|-------------|----------|
| Dual guard: extend static analyzer + runtime test | Extend `metriccardinality` to reject run-ID-shaped labels + add a runtime cardinality test; new loop metrics keep bounded enum labels only | ✓ |
| Runtime test only | A single unit test asserting no run-ID label | |

**Auto-selected:** Dual guard (recommended). The project already trusts the static analyzer for `"task"`; a run-ID label should fail at `go vet` time, not only in a unit test. Run-scoped detail stays in traces (LOOP-03); metrics stay aggregate/bounded.
**Notes:** Whether Phase 50 adds any new metric at all (vs. only hardening the guard) is Claude's discretion — the loop-outcome signal may be entirely trace-side.

---

## Claude's Discretion

- Go struct field ordering / JSON tag spellings / `RunEvidence` field names + file placement.
- The `TerminalReason` Go type name + file placement.
- otelai helper signatures (one `LoopAttributes` fn vs. split loop/evaluation helpers) + constant spellings.
- Changed-file manifest representation (`git diff --name-status` output vs. structured `[]ChangedFile`).
- Whether Phase 50 adds any new Prometheus metric, or only hardens the cardinality guard.

## Deferred Ideas

- `ConditionVerifyHalt` + resume time-fence + dispatch-gate wiring → **Phase 51** (ESC-02/03) — *corrects the Phase-49 CONTEXT prediction that placed it in Phase 50*.
- `TaskReconciler` verifier dispatch, evidence-packet-seeded fresh attempts, `maxIterations` loop, concurrency accounting, `BudgetCents` reservation, `onExhaustion` → **Phase 51** (TASK-*, ESC-04).
- `"langgraph"` `SelfInstruments` registration + `EVALUATOR` sibling span + populating `evaluation.*` → **Phase 51** (OBS-03).
- Controller rewire consulting the verifier before marking a Task correct → **Phase 51**.
- Dashboard nested-provenance + `VerifyHalt` visual state → **Phase 53** (OBS-04).
- Optional in-attempt checkpoints for long attempts → future (not required by any Phase-50 criterion).

### Reviewed Todos (not folded)
- `2026-07-12-project-dispatch-missing-failurehalt-gate` (0.6) — halt-gate reconciler wiring → Phase 51.
- `2026-07-12-task-dispatch-gate-order-divergence` (0.6) — dispatch-gate ordering → Phase 51.
- `2026-07-03-signed-commits-verified-badge` (0.4) — GPG signing, deferred by choice since v1.0.7.
- `cache-f1-direct-sdk-cross-pod-caching` (0.4) — deferred to vNext.
