---
phase: 51-the-task-loop
plan: 01
subsystem: api
tags: [crd-schema, cel-validation, kubebuilder, controller-runtime, k8s-api, task-loop]

# Dependency graph
requires:
  - phase: 50-execution-loop-hardening-loop-native-observability
    provides: LoopStatus/LoopPolicy shared API types (api/v1alpha3/loop_types.go), TerminalReason, run-evidence
provides:
  - "VerificationSpec CRD type on TaskSpec: planner-authored, CEL-immutable-once-Locked pass-criteria contract (Phase/Version/GateCommand/Commands/RequiredArtifacts/Evaluator/MaxIterations/OnExhaustion)"
  - "TaskStatus.LoopStatus embed (current-iteration summary + exit reason only, LOOP-03) and TaskStatus.LockedSHA (runtime observation)"
  - "ConditionVerifyHalt / ReasonVerifyExhausted / AnnotationVerifyResumedAt vocabulary (third generation of the BillingHalt -> FailureHalt -> VerifyHalt mirror)"
  - "Regenerated CRD (config/crd/bases/tideproject.k8s_tasks.yaml + charts/tide-crds/) carrying the x-kubernetes-validations transition rule"
  - "envtest proof (verification_immutability_test.go) that the CEL rule enforces: Draft freely editable, Locked immutable except Locked->Superseded"
affects: [51-02, 51-03, 51-04, 51-05, 51-06, 51-07, 51-08, 52-plan-project-verification]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "CEL x-kubernetes-validations transition rule with oldSelf (net-new pattern in this repo — no prior in-repo oldSelf transition rule existed)"
    - "Governing lifecycle enum lives on spec (not status) specifically so a CEL transition rule can read oldSelf; only the runtime observation (lockedSHA) lives on status"

key-files:
  created:
    - internal/controller/verification_immutability_test.go
  modified:
    - api/v1alpha3/task_types.go
    - api/v1alpha3/shared_types.go
    - api/v1alpha3/zz_generated.deepcopy.go
    - config/crd/bases/tideproject.k8s_tasks.yaml
    - charts/tide-crds/templates/task-crd.yaml

key-decisions:
  - "Governing VerificationPhase/Version fields live on spec.verification, not TaskStatus — a CEL spec-subtree transition rule cannot reference status; only lockedSHA (an observation) lives on status (resolves RESEARCH Open Question 1 / Pitfall 2)"
  - "VerificationSpec is a standalone type (Gates/Caps precedent), not inline TaskSpec fields, so the identical shape generalizes to Plan.Spec/Project.Spec with Task > Plan > Project precedence in Phase 52"
  - "OnExhaustion's two enum values (escalate/requireApproval) resolve identically to ConditionVerifyHalt in Phase 51 — per-value differentiation is explicitly Phase 52 scope, documented in the field's doc-comment rather than left ambiguous"

patterns-established:
  - "Third-generation halt-condition block (BillingHalt -> FailureHalt -> VerifyHalt) in shared_types.go, doc-comment discipline: what SETS/READS/CLEARS it, explicit distinct-halt-class statement"

requirements-completed: [TASK-01, TASK-05, ESC-03]

# Metrics
duration: 15min
completed: 2026-07-19
---

# Phase 51 Plan 01: CRD Schema Foundation for the Task Loop Summary

**Standalone `VerificationSpec` CRD type with a CEL `oldSelf` transition-immutability rule (Draft freely editable, Locked immutable except Locked→Superseded), embedded on `TaskSpec`, plus `TaskStatus.LoopStatus`/`LockedSHA` and the `VerifyHalt` condition vocabulary — proven live against a real envtest API server.**

## Performance

- **Duration:** ~15 min (08:02 -> 08:17 local)
- **Started:** 2026-07-19T12:02:59Z
- **Completed:** 2026-07-19T12:17:05Z
- **Tasks:** 3/3 completed
- **Files modified:** 5 (1 created, 4 modified — including 2 regenerated-artifact pairs)

## Accomplishments
- `VerificationSpec` standalone type on `api/v1alpha3/task_types.go` with a CEL `x-kubernetes-validations` transition rule (`oldSelf.phase != 'Locked' || self == oldSelf || self.phase == 'Superseded'`) — the first `oldSelf`-based transition rule in this codebase (no prior precedent existed)
- The CEL immutability contract is proven against a **real API server** (envtest, not the fake client, since CEL admission doesn't run under it): 4 Ginkgo specs cover CREATE-with-Draft, Locked-field-mutation-rejected, Locked→Superseded-allowed, Draft-mutation-allowed — all green
- `TaskStatus` gains `LoopStatus` (current-iteration summary only, LOOP-03) and `LockedSHA` (the runtime observation, not the governing enum) — resolving RESEARCH Open Question 1 in favor of "governing phase/version on spec"
- `ConditionVerifyHalt`/`ReasonVerifyExhausted`/`AnnotationVerifyResumedAt` land as the third generation of the BillingHalt → FailureHalt → VerifyHalt halt-condition vocabulary, doc-commented as a distinct halt class (never a reinterpretation of Failed wave semantics)
- Full regeneration chain (`make generate`, `make manifests`, `make helm`) is idempotent (zero further diff on a second run) and the whole `internal/controller` envtest suite (242 specs) stays green after the schema change

## Task Commits

Each task was committed atomically:

1. **Task 1: VerificationSpec type + CEL immutability + TaskSpec/TaskStatus embeds** - `ad65607b` (feat)
2. **Task 2: VerifyHalt condition/reason/annotation vocabulary** - `a3f52e95` (feat)
3. **Task 3: Regenerate CRD/deepcopy + CEL immutability envtest** - `46208e6c` (test)

_No TDD RED/GREEN split was needed for Task 1 despite `tdd="true"` — the CEL admission behavior can only be exercised against a real API server, so its RED/GREEN cycle is folded into Task 3's envtest, which is the plan's own structure (Task 1 = schema, Task 3 = proof)._

## Files Created/Modified
- `api/v1alpha3/task_types.go` - `VerificationSpec` type (Phase/Version/GateCommand/Commands/RequiredArtifacts/Evaluator/MaxIterations/OnExhaustion) with the CEL transition marker; `TaskSpec.Verification` embed; `TaskStatus.LoopStatus`/`LockedSHA`
- `api/v1alpha3/shared_types.go` - `ConditionVerifyHalt`/`ReasonVerifyExhausted`/`AnnotationVerifyResumedAt` const block
- `api/v1alpha3/zz_generated.deepcopy.go` - regenerated (`VerificationSpec.DeepCopyInto/DeepCopy`, `TaskSpec`/`TaskStatus` DeepCopyInto updates)
- `config/crd/bases/tideproject.k8s_tasks.yaml` - regenerated; carries the `x-kubernetes-validations` rule under `spec.verification`, plus the `loopStatus`/`lockedSHA` status fields
- `charts/tide-crds/templates/task-crd.yaml` - regenerated via `make helm` (mirrors the CRD base; required by the `verify-chart-reproducible` pre-commit hook)
- `internal/controller/verification_immutability_test.go` (new) - 4-spec Ginkgo envtest proving the CEL transition rule against a real API server

## Decisions Made
- Governing `Phase`/`Version` live on `spec.verification` (not `TaskStatus`) — the only way a CEL spec-subtree transition rule can read `oldSelf`; `LockedSHA` on status stays a pure runtime observation. This is RESEARCH Open Question 1, resolved as planned.
- `VerificationSpec` is a standalone type (mirrors `Gates`/`Caps`), not inline `TaskSpec` fields, so Phase 52 can reuse the identical shape on `Plan.Spec`/`Project.Spec` with Task > Plan > Project precedence.
- `OnExhaustion`'s two enum values are declared now but resolve identically in Phase 51 (both halt via `ConditionVerifyHalt`); the doc-comment states this explicitly so a future Phase-52 reader doesn't assume differentiated behavior already exists.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] `charts/tide-crds/templates/task-crd.yaml` also required regeneration**
- **Found during:** Task 3 (Regenerate CRD/deepcopy + CEL immutability envtest) commit
- **Issue:** The plan's `files_modified` list didn't include the Helm-templated mirror of the CRD base (`charts/tide-crds/templates/task-crd.yaml`). The repo's `verify-chart-reproducible` pre-commit hook runs `make helm` and fails the commit if `charts/` drifts from a fresh regeneration — since `config/crd/bases/tideproject.k8s_tasks.yaml` changed, the chart template necessarily also needed to move.
- **Fix:** Ran `make helm` (which regenerates both `charts/tide/` and `charts/tide-crds/`) and staged the resulting `charts/tide-crds/templates/task-crd.yaml` diff alongside the CRD base/deepcopy regen in the same commit.
- **Files modified:** `charts/tide-crds/templates/task-crd.yaml`
- **Verification:** `make verify-chart-reproducible` passes after staging; pre-commit hook's chart-reproducibility check reports "Passed" on the retried commit.
- **Committed in:** `46208e6c` (Task 3 commit)

---

**Total deviations:** 1 auto-fixed (1 blocking)
**Impact on plan:** Necessary for the commit to land under this repo's chart-source-reproducibility invariant; no scope creep — the chart template is a pure derivative of the CRD base this task already regenerated.

## Issues Encountered

**Plan's literal `<verify>` command is a silent no-op for Ginkgo specs — required substitute verification.** The plan's automated verify step for Task 3 (and the overall `<verification>` block) specifies `go test ./internal/controller/... -run VerificationImmutab -count=1`. `internal/controller` runs its entire envtest suite through one Ginkgo entry point, `TestControllers` (the sole `func Test*` in the package); Go's `-run` flag only matches top-level test function names, not Ginkgo `Describe`/`It` text. Running the plan's literal command produces `ok ... [no tests to run]` in ~1s — a vacuous pass that never boots envtest or exercises the CEL rule at all (confirmed empirically: envtest boot alone takes several seconds, and `-v` output showed zero specs ran). This is a pre-existing pattern used across many Phase 50/51 plan verify-blocks in this codebase (`-run 'Span|Loop|...'` etc.), so the false-pass risk is systemic, not specific to this plan.

**Resolution:** Verified genuinely via `go test ./internal/controller/... -run TestControllers -v -args --ginkgo.focus="VerificationSpec"`, which reports `Will run 4 of 242 Specs` and `4 Passed | 0 Failed`. Also ran the full unfiltered suite (`go test ./internal/controller/... -count=1`) — all 242 specs green (119.9s). The CEL rule is genuinely proven; only the plan's suggested `-run` invocation was vacuous. Flagging this here per "Verify Before Claiming" rather than silently substituting — future Phase 51 plans (02-08) reusing the same `-run VerbatimName` pattern against this shared-suite package should either use `--ginkgo.focus` or accept that their literal verify command will pass trivially even if the spec is broken or never runs.

## User Setup Required

None - no external service configuration required.

## Next Phase Readiness
- `VerificationSpec`, `LoopStatus`/`LockedSHA` on `TaskStatus`, and the `VerifyHalt` vocabulary are now available for Plan 51-02 onward (the verifier dispatch/consume sub-state-machine, `verify_halt.go`, `verifierInFlightCount`, the `EVALUATOR` span, and `task_verifier.tmpl`).
- No blockers. The CEL rule is proven at the admission layer only — no reconciler logic reads/writes `spec.verification` yet (correctly out of scope for this plan; Plan 02+ wires the dispatch/consume state machine).
- Downstream plans should be aware the `-run <exact-name>` verify-command pattern against `internal/controller`'s shared Ginkgo suite needs `--ginkgo.focus=` (or a full suite run) to be a genuine (non-vacuous) check — see Issues Encountered above.

---
*Phase: 51-the-task-loop*
*Completed: 2026-07-19*

## Self-Check: PASSED

All 6 created/modified files confirmed present on disk; all 3 task commits (`ad65607b`, `a3f52e95`, `46208e6c`) plus the summary commit confirmed in `git log`.
