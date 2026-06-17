---
phase: 25-global-dispatch-failure-semantics-gates-resumption
verified: 2026-06-17T01:20:00Z
status: passed
score: 4/4 must-haves verified
overrides_applied: 0
re_verification:
  previous_status: none
  previous_score: none
deferred:
  - truth: "The wave prune skips Waves that are not yet Succeeded (no deletion of in-flight Waves)"
    addressed_in: "Phase 26 (per code comment + 25-03 SUMMARY); NOT in Phase 26 roadmap success criteria — see note"
    evidence: "OQ-3 is an inherited Phase-24 TODO (project_controller.go:1553/1622), NOT one of the four Phase-25 requirements (DISP-01/02/03, RESUME-01), and is orthogonal to the dispatch/failure/gate/resumption contract — Wave CRs are display/aggregation artifacts, not read by computeGlobalIndegree (which reads Task.Status only)."
---

# Phase 25: Global Dispatch, Failure Semantics, Gates & Resumption Verification Report

**Phase Goal:** Execution dispatches off ONE global indegree map versus the completed-task set, the wave-boundary failure contract holds exactly at global scope, gates compose as holds over the global scheduler, and an orchestrator restart re-derives the entire schedule from minimal state.
**Verified:** 2026-06-17
**Status:** passed
**Re-verification:** No — initial verification

## Goal Achievement

### Observable Truths (ROADMAP Success Criteria + the four requirements)

| # | Truth | Status | Evidence |
| --- | ----- | ------ | -------- |
| 1 | **DISP-01** — A Task dispatches only when ALL its global dependencies are complete (global indegree 0 vs completed-task set), regardless of authoring Plan/Phase/Milestone | ✓ VERIFIED | `computeGlobalIndegree` (task_controller.go:1266-1302) reads `task.Status.Phase` of ALL project tasks via `listProjectTasks` (label `owner.LabelProject`, not plan-local `PlanRef`); resolves each `DependsOn` through the shared `buildScopeResolver` (depgraph.go:56). Coarse ref satisfied only when EVERY member Succeeded. Wired into dispatch at checkReadinessGates:472. DISP-01 envtest spec (cross-plan blocks until predecessor Succeeds) GREEN. |
| 2 | **DISP-02** — failed task's independent siblings (incl. same-global-wave cross-plan) continue; global dependents never dispatch; non-dependents dispatch in strict / halt in conservative | ✓ VERIFIED | Strict: falls out of indegree model (Failed never == Succeeded → dependents blocked; independents have own indegree). DISP-02 strict (a) later-wave non-dependent + (b) same-global-wave **cross-plan** independent sibling (global_dispatch_test.go:224, genuine two-plan test) both GREEN. Conservative: `setFailureHaltIfNeeded` fires on Job-failure path (task_controller.go:972) + terminal short-circuit (317); `checkFailureHalt` gates the FOUR execution sites; conservative envtest spec GREEN. |
| 3 | **DISP-03** — gate (M/P/P/task approve) withholds a globally-ready Task until approved without bypassing dependency readiness; gate-policy stays per-Project configurable | ✓ VERIFIED | Indegree check (472) precedes the gate check (510-522): gate composes as a hold OVER global readiness, never bypassing it. Task-level `Spec.Gates.Task` overrides project-level `Project.Spec.Gates` (controller reads policy, not baked in). M/P/P are planning-DAG holds with NO execution re-check (D-03 structural: un-approved scope ⇒ no authored tasks). DISP-03 envtest spec (gated task holds AwaitingApproval; non-dependent flows) GREEN. |
| 4 | **RESUME-01** — restart re-derives the entire schedule from global indegree map + completed-task set alone, no other persisted execution state | ✓ VERIFIED | Indegree re-derived every reconcile from Task `.status` + authored `dependsOn`; nothing cached. `make verify-no-aggregates` GREEN (no Schedule/Waves[]/IndegreeMap fields). RESUME-01 envtest spec (after A,B Succeeded in etcd, C dispatches w/o new persistence) GREEN. FailureHalt cleared by `tide resume --retry-failed` (resume.go:127-149, inside retryFailed branch only; bare resume leaves it). |

**Score:** 4/4 truths verified

### Deferred Items

| # | Item | Addressed In | Evidence |
|---|------|-------------|----------|
| 1 | Wave-prune in-flight guard (OQ-3) — prune skips not-yet-Succeeded Waves | Phase 26 (per comment/SUMMARY; not in P26 roadmap SCs) | Inherited Phase-24 TODO; NOT a Phase-25 requirement; orthogonal to the dispatch contract (Wave CRs are display artifacts, not read by dispatch). See "Known Deviation" below. |

### Required Artifacts

| Artifact | Expected | Status | Details |
| -------- | -------- | ------ | ------- |
| `internal/controller/depgraph.go` | shared coarse-ref fan-out resolver reused by ProjectReconciler + TaskReconciler | ✓ VERIFIED | 245 lines; `buildScopeResolver`/`resolveScope`/`ancestorScopeNames`/`buildGlobalEdges`. Used by project_controller.go:1501/1536 AND task_controller.go:1276/1523 — D-01 "never disagree" structurally enforced. |
| `internal/controller/task_controller.go` | global listProjectTasks + computeGlobalIndegree + globalDependentsMapper | ✓ VERIFIED | listProjectTasks:1244 (label-select), computeGlobalIndegree:1266 (coarse-aware), globalDependentsMapper:1488 (coarse-ref-aware re-enqueue). Old `listSiblingTasks`/`siblingsToTaskMapper`/`computeIndegree` func defs REMOVED (only appear in comments/tests). |
| `internal/controller/failure_halt.go` | checkFailureHalt + setFailureHaltIfNeeded mirroring billing_halt.go | ✓ VERIFIED | 97 lines; both functions present, nil-safe, idempotent, conservative-only stamp. |
| `api/v1alpha2/project_types.go` | FailureProfile field, CEL enum + default markers | ✓ VERIFIED | `FailureProfile FailureProfileType` (387) with `+kubebuilder:validation:Enum=strict;conservative` + `+kubebuilder:default=strict`. CRD regenerated (config/crd in sync, git clean). |
| `api/v1alpha2/shared_types.go` | FailureProfileType enum + FailureHalt vocabulary | ✓ VERIFIED | ConditionFailureHalt(243), ReasonTaskFailedHalt(247), AnnotationFailureResumedAt(253), FailureProfileStrict(264)/Conservative(269). |
| `cmd/tide/resume.go` | FailureHalt clear inside --retry-failed branch | ✓ VERIFIED | RemoveStatusCondition(ConditionFailureHalt) at resume.go:142, after the `!retryFailed { return nil }` guard at 127. |

### Key Link Verification

| From | To | Via | Status | Details |
| ---- | -- | --- | ------ | ------- |
| task_controller.go | depgraph.go | computeGlobalIndegree → buildScopeResolver/resolveScope | ✓ WIRED | task_controller.go:1276,1286 |
| project_controller.go | depgraph.go | assembleProjectDepGraph → buildScopeResolver/buildGlobalEdges | ✓ WIRED | project_controller.go:1501,1536 |
| task_controller.go (watch) | globalDependentsMapper | SetupWithManager EnqueueRequestsFromMapFunc | ✓ WIRED | task_controller.go:1686; mapper coarse-ref-aware (matchable includes plan/phase/ms names, 1530-1539) |
| task_controller.go | failure_halt.go | handleJobCompletion → setFailureHaltIfNeeded | ✓ WIRED | task_controller.go:972 (Job failure) + 317 (terminal short-circuit) |
| {task,plan,phase,milestone}_controller.go | checkFailureHalt(project) | dispatch gate after checkBillingHalt | ✓ WIRED | task:393, plan:350, phase:353, milestone:355 |
| project_controller.go (planner site) | checkFailureHalt | — must be ABSENT (D-03) | ✓ VERIFIED ABSENT | grep returns 0 in project_controller.go — execution-only halt confirmed |
| cmd/tide/resume.go | Project.Status.Conditions | RemoveStatusCondition(FailureHalt) in retryFailed branch | ✓ WIRED | resume.go:142 |

### Data-Flow Trace (Level 4)

| Artifact | Data Variable | Source | Produces Real Data | Status |
| -------- | ------------- | ------ | ------------------ | ------ |
| computeGlobalIndegree | statusByName[member] | live Task `.status.Phase` from listProjectTasks (etcd) | Yes — reads real CRD status, not static | ✓ FLOWING |
| checkFailureHalt | project.Status.Conditions | live Project condition stamped by setFailureHaltIfNeeded on real failure | Yes | ✓ FLOWING |

### Behavioral Spot-Checks

| Behavior | Command | Result | Status |
| -------- | ------- | ------ | ------ |
| Module builds | `go build ./...` | exit 0 | ✓ PASS |
| go vet clean | `go vet ./internal/controller ./cmd/tide ./api/v1alpha2` | exit 0 | ✓ PASS |
| Phase-25 envtest specs (DISP-01/02/03, RESUME-01) | `go test ./test/integration/envtest --ginkgo.label-filter=phase25` | Ran 6 of 51, 6 Passed 0 Failed | ✓ PASS |
| Full envtest layer (regression) | `go test ./test/integration/envtest -v` | Ran 51 of 51, 51 Passed 0 Failed, `ok` exit 0 | ✓ PASS |
| failure_halt + depgraph unit tests | `go test ./internal/controller -run 'FailureHalt|ScopeResolver|GlobalIndegree|...'` | ok | ✓ PASS |
| resume FailureHalt clear/leave units | `go test ./cmd/tide -run 'ResumeRunClearsFailureHalt|ResumeWithoutRetryFailedLeavesFailureHalt'` | both PASS (were RED in 25-02) | ✓ PASS |
| Full controller+cmd unit (-short) | `go test ./internal/controller ./cmd/tide -short` | both ok | ✓ PASS |

### Probe Execution / Guards

| Guard | Command | Result | Status |
| ----- | ------- | ------ | ------ |
| No aggregate schedule fields (PERSIST-02/RESUME-01) | `make verify-no-aggregates` | "OK: no aggregate schedule fields" exit 0 | ✓ PASS |
| pkg/dag clean imports (DAG-05) | `make verify-dag-imports` | "OK: pkg/dag imports are clean" exit 0 | ✓ PASS |
| No DB driver deps (PERSIST-01) | `make verify-no-sqlite-dep` | "OK: no DB driver deps" exit 0 | ✓ PASS |
| Metric label set {project,phase,plan,wave}, no `task` | grep registry.go | 7 vecs all `{"project","phase","plan","wave"}`; no `task` label; last touched Phase 21 | ✓ PASS |

### Requirements Coverage

| Requirement | Source Plan | Description | Status | Evidence |
| ----------- | ----------- | ----------- | ------ | -------- |
| DISP-01 | 25-01, 25-02 | Global indegree dispatch regardless of authoring scope | ✓ SATISFIED | Truth 1; depgraph.go + computeGlobalIndegree + globalDependentsMapper |
| DISP-02 | 25-01, 25-03 | Wave-boundary failure contract at global scope (strict/conservative) | ✓ SATISFIED | Truth 2; failure_halt.go + 4 gate sites + setFailureHaltIfNeeded |
| DISP-03 | 25-01, 25-02 | Gates compose as holds over global scheduler | ✓ SATISFIED | Truth 3; indegree-before-gate ordering + per-Project/per-task policy |
| RESUME-01 | 25-01, 25-02, 25-03 | Restart re-derives schedule from minimal state | ✓ SATISFIED | Truth 4; verify-no-aggregates green + re-derive each reconcile + resume clear |

All four declared requirements are mapped to Phase 25 in REQUIREMENTS.md (lines 35-37, 47) and to no other phase. No orphaned requirements.

### Anti-Patterns Found

| File | Line | Pattern | Severity | Impact |
| ---- | ---- | ------- | -------- | ------ |
| (none) | — | No TBD/FIXME/XXX/TODO/HACK/PLACEHOLDER in any phase-25 modified file | — | The OQ-3 deferral uses a descriptive "NOTE (Phase 25 OQ-3 deferred)" comment with full root-cause — not a debt-marker class blocker. |

### Known Deviation Assessment — Wave-prune in-flight guard (OQ-3)

**Verdict: legitimately-tracked INHERITED debt; does NOT compromise the phase goal or any of the four requirements.**

Reasoning:
1. **Not a Phase-25 requirement.** OQ-3 is an inherited Phase-24 TODO (`project_controller.go:1553/1622` say "Phase 25 *should* gate prune..."). It is absent from all four CONTEXT decisions (D-01..D-04) and from the four ROADMAP success criteria. The 25-03 plan opportunistically listed it as a fifth must-have truth, then correctly reverted it (2a97a7a→e7c14f7) when it broke the pre-existing CR-01 `PruneShrink` regression.
2. **Orthogonal to the dispatch/failure/gate/resumption contract.** The global dispatch decision (`computeGlobalIndegree`) reads Task `.status.Phase` only — it does NOT consult Wave CRs. Wave CRs are display/aggregation artifacts. Pruning a stale Wave CR therefore cannot affect indegree, dispatch readiness, the failure contract, gate holds, or restart re-derivation (RESUME-01 re-derives from Task status, not Wave CRs). The CR-01 prune regression (`PruneShrink`) is GREEN in the 51/51 run.
3. **Correct engineering call.** A naive `Phase != "Succeeded"` guard was reverted because the wave aggregator stamps `Phase="Running"` on zero-member waves, which would block pruning legitimately-stale empty waves. The real fix needs a WaveController refactor (distinguish "no tasks" from "tasks in-flight") — genuinely out of Phase-25 scope. The residual risk (T-25-03-04: in-flight Wave CR deletion) is a documented display/DoS concern, not a contract violation.

**Caveat (informational, non-blocking):** The deferral target "Phase 26" is asserted in code comments and the 25-03 SUMMARY but is NOT explicitly listed in the Phase 26 roadmap success criteria. Recommend adding it to the Phase 26 plan backlog so the inherited debt does not fall through. This does not block Phase 25 completion.

### Human Verification Required

None. All four requirements verified programmatically via envtest (real K8s API server), unit tests, build, vet, and the three invariant guards. No PLAN `<human-check>` blocks were present.

### Gaps Summary

No gaps. All four ROADMAP success criteria and all four declared requirements (DISP-01, DISP-02, DISP-03, RESUME-01) are observably true in the codebase:
- The shared `depgraph.go` resolver guarantees TaskReconciler dispatch and ProjectReconciler wave derivation can never disagree (D-01).
- Global indegree dispatches off Task `.status` + re-derived edges; old plan-local helpers are removed.
- Strict failure semantics fall out of the indegree model; conservative adds the `FailureHalt` rail at exactly the four execution dispatch sites (absent from the planner site per D-03).
- Gate composition is ordered indegree-before-gate (hold over readiness, never bypass); M/P/P are structural planning holds.
- Resumption is re-derive-only; `verify-no-aggregates` confirms no cached schedule.

The one deferred item (OQ-3 wave-prune in-flight guard) is inherited Phase-24 debt, orthogonal to the phase contract, and properly tracked.

---

_Verified: 2026-06-17T01:20:00Z_
_Verifier: Claude (gsd-verifier)_
