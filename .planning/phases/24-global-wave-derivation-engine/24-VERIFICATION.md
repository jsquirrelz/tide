---
phase: 24-global-wave-derivation-engine
verified: 2026-06-16T22:39:52Z
status: passed
score: 4/4 must-haves verified
overrides_applied: 0
---

# Phase 24: Global Wave Derivation Engine Verification Report

**Phase Goal:** Once project planning completes, the orchestrator assembles ONE global Execution DAG of every Task across all Milestones/Phases/Plans and derives a single monotonic wave schedule by layered Kahn — queryable both directions and re-derived cheaply with no cached schedule.
**Verified:** 2026-06-16T22:39:52Z
**Status:** passed
**Re-verification:** No — initial verification

## Goal Achievement

### Observable Truths

| #   | Truth (ROADMAP success criterion) | Status     | Evidence       |
| --- | --------------------------------- | ---------- | -------------- |
| 1 (EXEC-01) | A single global Execution DAG contains every Task across all Milestones/Phases/Plans, assembled once after planning before dispatch | ✓ VERIFIED | `ProjectReconciler.assembleProjectDepGraph` (project_controller.go:1470) lists ALL Tasks/Plans/Phases/Milestones in-namespace, builds resolution maps (`tasksByPlan`/`planToPhase`/`phaseToMS`), and fans out over all four `dependsOn` carriers (Task §6a, Plan §6b, Phase §6c, Milestone §6d) with de-duped edges (`edgeSet`). Called ONCE per reconcile (project_controller.go:264), result shared with cycle gate + derive. Envtest `GlobalDag` spec PASS. |
| 2 (EXEC-02) | Waves derived by layered Kahn carry global monotonic indices, NOT per-plan `tide-wave-<plan.UID>-<i>` | ✓ VERIFIED | `deriveGlobalWaves` (project_controller.go:1688) calls `pkg/dag.ComputeWaves` (via gate, threaded result) and creates Project-owned `tide-wave-<project>-<globalIndex>` CRs (line 1698). Per-plan writer GONE: zero `func materializeWaves`/`func stampTaskLabels` definitions in plan_controller.go, zero `tide-wave-` writers there (only doc comments), and `Owns(&Wave{})` removed from PlanReconciler.SetupWithManager (plan_controller.go:1352-1353 declares only Task+Job). Exactly ONE Wave writer. Envtest `GlobalWaveIndex` spec PASS. |
| 3 (EXEC-03) | Bidirectional index: given a Task resolve its global wave; given a wave list its Tasks (README:54 invariant Project-wide) | ✓ VERIFIED | task→wave: `stampGlobalTaskLabels` (project_controller.go:1783) writes `tideproject.k8s/wave-index` + `tideproject.k8s/project`; `taskToWaveMapper` (wave_controller.go:240) derives wave name O(1) from those labels, no List. wave→tasks: `reconcileObservational` (wave_controller.go:143) lists by `MatchingLabels{wave-index, project=ProjectRef}` — Project-scoped. Envtest `BidirectionalIndex` spec PASS. |
| 4 (EXEC-04) | Re-derivation O(V+E) on Task add/complete with no schedule cached in `.status` (PERSIST-03) | ✓ VERIFIED | Full assemble→ComputeWaves→derive runs every Project reconcile (re-enqueued via `taskToProject` on Task edits). No aggregate field on `api/v1alpha2` Project status (grep zero matches; `make verify-no-aggregates` OK). `ComputeWaves` runs exactly ONCE per reconcile (single call site project_controller.go:1850, result threaded to derive — WR-03 fixed). Envtest `WaveRederivation` + PERSIST-03 specs PASS. |

**Score:** 4/4 truths verified

### Required Artifacts

| Artifact | Expected | Status | Details |
| -------- | -------- | ------ | ------- |
| `internal/controller/project_controller.go` | Full fan-out assembler + deriveGlobalWaves + stampGlobalTaskLabels + cycle gate, Owns Wave | ✓ VERIFIED | `assembleProjectDepGraph` (1470), `deriveGlobalWaves` (1688), `stampGlobalTaskLabels` (1783), `checkGlobalCycleGate` (1841). Builds. |
| `internal/controller/plan_controller.go` | Per-plan materializeWaves/stampTaskLabels + Owns Wave removed | ✓ VERIFIED | Zero func defs for both; SetupWithManager Owns Task+Job only (1352-1353). |
| `internal/controller/wave_controller.go` | Four Phase-24 TODOs closed; O(1) global task→wave mapper | ✓ VERIFIED | Zero TODO/FIXME markers; `taskToWaveMapper` O(1) name derivation (251); ProjectRef-scoped roll-up (143-145). |
| `test/integration/envtest/global_wave_derivation_test.go` | EXEC-01..04 envtest contract + PruneShrink regression | ✓ VERIFIED | `TestGlobalWaveDerivation` Describe blocks GlobalDag/GlobalWaveIndex/BidirectionalIndex/WaveRederivation/PruneShrink + cross-phase/milestone fan-out. 45/45 specs pass. |

### Key Link Verification

| From | To | Via | Status | Details |
| ---- | -- | --- | ------ | ------- |
| ProjectReconciler.Reconcile | checkGlobalCycleGate | pre-assembled (nodes,edges) | ✓ WIRED | Single assemble at 264, passed to gate at 271 |
| ProjectReconciler.Reconcile | deriveGlobalWaves | called after gate passes, threading globalWaves+tasks | ✓ WIRED | Line 279 |
| deriveGlobalWaves | Wave CR tide-wave-<project>-<N> | Create + EnsureOwnerRef + WaveIndex; prune index>=len(waves) | ✓ WIRED | 1698-1768; prune lists by project label now stamped at create (CR-01 fix) |
| stampGlobalTaskLabels | Task wave-index label | client.MergeFrom + Patch | ✓ WIRED | 1809-1817 |
| WaveReconciler.taskToWaveMapper | tide-wave-<project>-<waveIndex> | O(1) name derivation from labels (no List) | ✓ WIRED | 251 |
| WaveReconciler.reconcileObservational | Tasks of a global wave | label selector wave-index + project | ✓ WIRED | 143-145 |

### Behavioral Spot-Checks

| Behavior | Command | Result | Status |
| -------- | ------- | ------ | ------ |
| Controllers compile | `go build ./internal/...` | exit 0 | ✓ PASS |
| Controller unit suite | `go test ./internal/controller/... -count=1` | `ok ... 62.957s` | ✓ PASS |
| Envtest suite (Global derivation incl PruneShrink) | `go test ./test/integration/envtest/... -count=1` | `Ran 45 of 45 Specs ... SUCCESS!` | ✓ PASS |
| No cached aggregate schedule (PERSIST-02) | `make verify-no-aggregates` | `OK: no aggregate schedule fields` exit 0 | ✓ PASS |
| pkg/dag import hygiene (DAG-05) | `make verify-dag-imports` | `OK: pkg/dag imports are clean` exit 0 | ✓ PASS |
| No DB driver dep (PERSIST-01) | `make verify-no-sqlite-dep` | `OK: no DB driver deps` exit 0 | ✓ PASS |

### Requirements Coverage

| Requirement | Source Plan | Description | Status | Evidence |
| ----------- | ----------- | ----------- | ------ | -------- |
| EXEC-01 | 24-01, 24-02 | ONE global DAG of all Tasks across all M/P/Plans before dispatch | ✓ SATISFIED | Truth 1; assembleProjectDepGraph full fan-out |
| EXEC-02 | 24-01, 24-03, 24-04 | Waves by layered Kahn over global DAG, global indices not per-plan | ✓ SATISFIED | Truth 2; single Wave writer, tide-wave-<project>-<N> |
| EXEC-03 | 24-01, 24-03, 24-04 | Global wave index queryable both directions | ✓ SATISFIED | Truth 3; label stamp + O(1) mapper + ProjectRef-scoped selector |
| EXEC-04 | 24-01, 24-03 | Re-derive O(V+E) on add/complete, no cached schedule | ✓ SATISFIED | Truth 4; verify-no-aggregates + PERSIST-03 spec |

No orphaned requirements: REQUIREMENTS.md maps exactly EXEC-01..04 to Phase 24, all claimed across the plans.

### Code Review Fix Verification (24-REVIEW.md)

| Finding | Severity | Resolution | Verified |
| ------- | -------- | ---------- | -------- |
| CR-01: stale-Wave prune dead code (Wave leak) | BLOCKER | Fixed f3d45f8 — Wave CR created WITH `owner.LabelProject` (1706); prune lists by that label (1754-1756); `PruneShrink` regression test (test line 413) asserts `tide-wave-<project>-1` deleted after shrink | ✓ Code + test confirm; 45/45 pass |
| WR-01: CycleDetected never cleared | Warning | Fixed — gate sets `CycleDetected=False/NoCycle` on pass path (1876-1887) | ✓ Code confirms |
| WR-02: unconditional owner-ref Update churn | Warning | Fixed — gated on `!metav1.IsControlledBy` (1738) | ✓ Code confirms |
| WR-03: ComputeWaves run twice | Warning | Fixed — single call site (1850), result threaded to derive | ✓ grep: one ComputeWaves call |
| IN-02: redundant Task re-List in derive | Info | Fixed — assembler task slice threaded to deriveGlobalWaves (264→279) | ✓ Code confirms |
| WR-04: unlabeled Tasks excluded | Warning (deferred) | `NOTE(phase-25+)` + warning-log mitigation (1559-1581) | ✓ Documented follow-up, covered by Phase 25 goal |
| WR-05: taskToWaveMapper drops stale-label events | Warning (deferred) | `NOTE(phase-25+)` + 30s safety resync mitigation (wave_controller.go:215-224) | ✓ Documented follow-up, covered by Phase 25 goal |

### Anti-Patterns Found

None. Zero debt markers (TODO/FIXME/XXX/TBD) in the three modified controller files. No stub returns, no hardcoded-empty render paths. WR-04/WR-05 deferrals carry active mitigations (warning log + safety resync) and `NOTE(phase-25+)` references whose authoritative fix is explicitly in Phase 25's goal/success-criteria — documented follow-ups, not silent gaps.

### Deferred Items

| # | Item | Addressed In | Evidence |
|---|------|-------------|----------|
| 1 | WR-04 full coverage (field-index listing / admission defaulting so directly-applied unlabeled Tasks participate) | Phase 25 | Phase 25 goal: "Execution dispatches off ONE global indegree map versus the completed-task set ... orchestrator restart re-derives the entire schedule" — the ProjectReconciler-owned authoritative assignment seam the NOTE targets |
| 2 | WR-05 authoritative ProjectReconciler-owned Task→Wave fan-out (replacing label-derived mapper) | Phase 25 | `NOTE(phase-25+)` at wave_controller.go:220 names "ProjectReconciler-owned Task→Wave fan-out"; Phase 25 SC covers dispatch off global indegree map |

Deferred items are informational and do not affect status.

### Human Verification Required

None. All criteria are programmatically verifiable and were verified by reading the code and running the controller suite (62s), envtest suite (45/45, incl PruneShrink), and all three verify guards. The pre-existing kind Layer-B `medium_http_test` failure is unrelated to Phase 24 (it touched no demo-init/git-http code) and is excluded per phase scope.

### Gaps Summary

No gaps. The phase goal is achieved in the codebase: a single global Execution DAG is assembled once per reconcile with full fan-out over all four `dependsOn` carriers (EXEC-01); waves derive by layered Kahn into Project-owned `tide-wave-<project>-<N>` CRs with the per-plan path fully removed leaving exactly one Wave writer (EXEC-02); the global wave index is bidirectional and Project-scoped (EXEC-03); and re-derivation is O(V+E) with no schedule cached in `.status`, guarded by `make verify-no-aggregates` (EXEC-04). The CR-01 BLOCKER is genuinely fixed — Waves carry the project label and the `PruneShrink` regression test exercises real deletion of stale high-index Waves. WR-04/WR-05 are documented Phase-25 follow-ups with live mitigations.

---

_Verified: 2026-06-16T22:39:52Z_
_Verifier: Claude (gsd-verifier)_
