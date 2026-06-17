---
phase: 25-global-dispatch-failure-semantics-gates-resumption
plan: 01
completed: 2026-06-17
status: complete
self_check: passed
---

# Plan 25-01 Summary — API Vocabulary + RED Scaffolds (Nyquist Wave 0)

## What was built

- **Task 1 (`bfa6c7a`):** Added the Phase 25 API surface to `api/v1alpha2`:
  - `FailureProfileType string` enum with `+kubebuilder:validation:Enum=strict;conservative`, constants `FailureProfileStrict`/`FailureProfileConservative`.
  - `ProjectSpec.FailureProfile` field (`+kubebuilder:default=strict`, `+optional`, json `failureProfile,omitempty`), inserted after `Git *GitConfig`.
  - Condition vocabulary mirroring BillingHalt: `ConditionFailureHalt = "FailureHalt"`, `ReasonTaskFailedHalt = "TaskFailedHalt"`, `AnnotationFailureResumedAt = "tideproject.k8s/failure-resumed-at"`.
  - `make manifests generate` regenerated the CRD — `config/crd/bases/tideproject.k8s_projects.yaml:616` now carries the `failureProfile` property with the enum + default; deepcopy regenerated.
- **Task 2 (`1699dcd`):** Authored the three RED test scaffolds (Nyquist Wave 0):
  - `internal/controller/failure_halt_test.go` — 7 unit cases for `checkFailureHalt`/`setFailureHaltIfNeeded` (RED: symbols undefined until 25-03).
  - `test/integration/envtest/global_dispatch_test.go` — `Label("envtest","phase25")` Ginkgo suite with specs tagged DISP-01, DISP-02 (strict — BOTH a later-wave non-dependent AND a same-global-wave **cross-plan** independent sibling; conservative halt), DISP-03 (task-gate hold), RESUME-01 (cross-plan A→B→C restart re-derive).
  - `cmd/tide/resume_failure_test.go` — `TestResumeRunClearsFailureHalt` + `TestResumeWithoutRetryFailedLeavesFailureHalt`.
  - VALIDATION.md marked `wave_0_complete: true`, `nyquist_compliant: true`.

## A1 coarse-ref finding (steers 25-02 shared resolver)

**Coarse-ref `DependsOn` is PRESENT in authored fixtures.** Evidence:
- `test/integration/envtest/global_wave_derivation_test.go:478` — `DependsOn: []string{"cm-plan-a"}` with the inline comment `// coarse Plan-level dep` (a Plan name, not a Task name).
- Numerous Task-name deps throughout (`admission_test.go`, `indegree_test.go`, `task_controller_test.go`, YAML samples), plus coarse refs in the Phase 24 wave-derivation fixtures.

**Implication for 25-02:** the shared `depgraph` fan-out resolver is **load-bearing for existing fixtures**, not merely a forward-looking contract — at least one envtest fixture already exercises a Plan-level coarse ref. Both `computeGlobalIndegree` and the `globalDependentsMapper` must resolve through it (per BLOCKER-1 fix). Regardless of the grep, 25-02 builds the resolver anyway because Plan/Phase/Milestone-level `DependsOn` carriers are part of the global contract.

## Verification observed

- `go build ./api/...` exits 0; CRD enum/default present (`failureProfile` @ line 616).
- Scaffolds are correctly RED: `go vet ./internal/controller/...` reports `undefined: checkFailureHalt`/`setFailureHaltIfNeeded` (expected — turned GREEN by 25-03).
- Guards green: `make verify-no-aggregates` (OK: no aggregate schedule fields), `verify-dag-imports` (OK: pkg/dag clean), `verify-no-sqlite-dep` (OK: no DB driver deps).

## Self-Check: PASSED

All Task 1 + Task 2 acceptance criteria verified on disk. Both tasks committed atomically. RED scaffolds in place for downstream waves.

## Notes / deviations

- This plan was completed across two executor dispatches: the first executor committed Task 1 and authored Task 2's files but was interrupted by a transient API 529 before committing; the orchestrator verified the on-disk scaffolds against the plan's Task 2 acceptance criteria, ran the A1 grep, flipped VALIDATION's Wave-0 flag, and committed Task 2 inline. No work was duplicated or lost.
