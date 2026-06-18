---
phase: 28-plan-import-core
verified: 2026-06-18T00:00:00Z
status: passed
score: 5/5 must-haves verified
overrides_applied: 0
re_verification:
  previous_status: none
---

# Phase 28: Plan-Import Core Verification Report

**Phase Goal:** A fresh Project run adopts pre-authored planner envelopes and skips the planner for every level whose valid envelope already exists — resolving the UID-churn problem via a stable identity scheme, validating every envelope before adoption, running cycle detection before materializing any child CRDs, converting v1alpha1 schema, and never importing Wave CRs.
**Verified:** 2026-06-18
**Status:** passed
**Re-verification:** No — initial verification

## Goal Achievement

### Observable Truths (ROADMAP Success Criteria)

| # | Truth (Success Criterion) | Status | Evidence |
| --- | --- | --- | --- |
| 1 | Fresh apply adopts envelopes & skips planner — 5 dispatch sites PARK before pool-acquire when `importSource` set & `ImportComplete != True` | ✓ VERIFIED | All 5 controllers (project/milestone/phase/plan/task) carry the guard BEFORE `PlannerPool.Acquire`: project_controller.go:1074 (guard) precedes Acquire:1084; milestone:370 precedes :382; phase:368 precedes :380; plan:365 precedes :377; task:389 returns `taskGateResult{shouldHalt:true, RequeueAfter:5s}` before billing/pool holds. Every guard parks (RequeueAfter, never fail). `TestImportGuard_ParkOnPending_NoPoolAcquired` proves the check is pure in-memory (no slot leak). ImportController sets `ConditionImportComplete=True` via `succeedImport` (import_controller.go:670). |
| 2 | Envelope adopted only after completeness+schema check; incomplete rejected | ✓ VERIFIED | `isEnvelopeComplete` (cmd/tide-import/main.go:265): `ExitCode!=0 → false`, `ChildCount>0 && len(ChildCRDs)!=ChildCount → false`. Incomplete envelopes are `continue`d (skipped, `report.Incomplete++`), never copied. `convertSpecRaw` typed round-trip through MilestoneSpec/PhaseSpec/PlanSpec/TaskSpec; `childKindAllowlist` (Milestone/Phase/Plan/Task only) — non-allowlisted Kind → `exitInvariant`. `go test ./cmd/tide-import/...` passes. |
| 3 | UID-churn resolved by stable identity (object name + parent chain); no aliasing | ✓ VERIFIED | FQ-name rekey: seedEntry.FQName + parent refs (ProjectRef/MilestoneRef/PhaseRef) in import_controller.go; rekeyTable keyed by FQ-name (line 370). cmd/tide-import rekeyEntry carries FQName/OldUID/NewUID; copy is per old→new UID path. Tree validation against seed; no cross-object aliasing. |
| 4 | `dag.ComputeWaves` on SEED-DERIVED planning-DAG BEFORE any client.Create; cycle → CyclicPlanDetected, zero partial CRs; Wave CRs never imported | ✓ VERIFIED | import_controller.go:351 `dag.ComputeWaves(seedNodes, seedEdges)` where nodes = Milestone/Phase/Plan CR names and edges = their DependsOn — at line 304-349, BEFORE first `r.Create` (line 391). NOT buildGlobalEdges. Cycle → `ReasonCyclicPlanDetected` (line 358). `grep -c 'Wave{' import_controller.go` = **0**. Envtest "Test 2 (IMPORT-04)" asserts Plan-level cycle → `ReasonCyclicPlanDetected` + `Consistently` ZERO Milestone/Phase/Plan CRs (D-10 atomicity). cmd/tide-import excludes Wave from allowlist (D-09). |
| 5 | Operator-gated; import Job mounts ONLY this namespace's PVC at declared subPaths; budget rollup suppressed | ✓ VERIFIED | Operator-gated: `spec.importSource` (import_types.go ImportSourceRef). BuildImportJob (import_jobspec.go) mounts the `tide-projects` PVC at TWO subPaths only — `/old-workspace` subPath=OldSubPath ReadOnly, `/new-workspace` subPath=NewSubPath RW — never the PVC root. Hardened securityContext (RunAsNonRoot, drop ALL). Budget rollup suppressed: project_controller.go:1253 `if project.Spec.ImportSource != nil { skip rollup }` (D-11). |

**Score:** 5/5 truths verified

### Required Artifacts

| Artifact | Expected | Status | Details |
| --- | --- | --- | --- |
| `api/v1alpha2/import_types.go` | ImportSourceRef struct | ✓ VERIFIED | `type ImportSourceRef struct {SeedManifestConfigMap, SalvagedPVCSubPath}` with MinLength validation |
| `api/v1alpha2/project_types.go` | ImportSource field | ✓ VERIFIED | `ImportSource *ImportSourceRef` at line 412 |
| `api/v1alpha2/shared_types.go` | ImportComplete vocab | ✓ VERIFIED | ConditionImportComplete, ReasonImportSucceeded/ImportFailed/CyclicPlanDetected, AnnotationRetryImport |
| `cmd/tide-import/main.go` | copy+rekey+convert+validate binary | ✓ VERIFIED | 459 lines; isEnvelopeComplete, convertSpecRaw, containedJoin traversal defense, allowlist; deps = api/v1alpha2 + pkg/dispatch only |
| `cmd/tide-import/main_test.go` | table-driven tests | ✓ VERIFIED | `go test ./cmd/tide-import/...` PASS |
| `images/tide-import/Dockerfile` | multi-stage distroless | ✓ VERIFIED | golang:1.26-alpine (digest-pinned) builder → distroless/static:nonroot, builds ./cmd/tide-import |
| `internal/controller/import_controller.go` | state machine + seed-DAG cycle check | ✓ VERIFIED | 699 lines; ImportReconciler, Pending→CreatingCRs→CopyingEnvelopes→Complete; ComputeWaves before Create |
| `internal/controller/import_jobspec.go` | two-subPath PVC mount | ✓ VERIFIED | BuildImportJob, dual subPath, no root mount, hardened SC |
| `internal/controller/import_controller_test.go` | envtest specs | ✓ VERIFIED | 4 It specs, Label("envtest","phase28"); skip-planner adoption, cycle-reject-zero-CRs, Kind allowlist, idempotent |
| `internal/controller/import_guard_test.go` | per-site guard tests | ✓ VERIFIED | 8 plain Go tests incl. slot-leak proof — all PASS |
| `cmd/manager/main.go` | ImportReconciler registration | ✓ VERIFIED | importImage from TIDE_IMPORT_IMAGE env (line 209); ImportReconciler registered (line 564) |
| `charts/tide/values.yaml` | images.tideImport block | ✓ VERIFIED | tideImport repository/tag/pullPolicy (line 199) |
| `charts/tide/templates/deployment.yaml` | TIDE_IMPORT_IMAGE env | ✓ VERIFIED | env injects `{repository}:{tag\|default AppVersion}` (line 101) |

Note: plan 28-02 frontmatter listed `charts/tide/crds/tideproject.k8s_projects.yaml` as the chart CRD; the actual chart CRD lives at `charts/tide-crds/templates/project-crd.yaml` and DOES carry `importSource` (regenerated). Path discrepancy in plan frontmatter only; the field round-trips correctly. Not a gap.

### Key Link Verification

| From | To | Via | Status |
| --- | --- | --- | --- |
| import_controller.go | pkg/dag.ComputeWaves | seed-derived planning DAG before client.Create | ✓ WIRED (line 351) |
| import_controller.go | ConditionImportComplete | meta.SetStatusCondition at Complete/Failed | ✓ WIRED |
| import_jobspec.go | per-namespace PVC | two VolumeMount subPaths, no root | ✓ WIRED |
| project_controller.go | ConditionImportComplete | guard reads condition before PlannerPool.Acquire | ✓ WIRED |
| cmd/manager/main.go | TIDE_IMPORT_IMAGE | envOrDefault → ImportReconciler.ImportImage | ✓ WIRED |
| cmd/tide-import | pkg/dispatch.EnvelopeOut | json.Unmarshal for TaskUID + ChildCount | ✓ WIRED |

### Behavioral Spot-Checks / Empirical Verification

| Behavior | Command | Result | Status |
| --- | --- | --- | --- |
| Full build | `go build ./...` | exit 0 | ✓ PASS |
| tide-import unit tests | `go test ./cmd/tide-import/... -count=1` | ok | ✓ PASS |
| phase28 envtests | `go test ./internal/controller/... --ginkgo.label-filter=phase28` (KUBEBUILDER_ASSETS=bin/k8s/1.36.0-darwin-amd64) | ok; JSON report: 6 passed, 0 failed, 147 skipped | ✓ PASS |
| guard plain tests | `go test -run TestImportGuard -v` | 8/8 PASS incl. NoPoolAcquired slot-leak proof | ✓ PASS |
| import firewall | `make verify-import-firewall` | exit 0 | ✓ PASS |
| dispatch firewall | `make verify-dispatch-imports` | OK, clean | ✓ PASS |
| tide-import deps | `go list -deps ./cmd/tide-import \| grep jsquirrelz` | only api/v1alpha2 + pkg/dispatch | ✓ PASS |
| Wave CR creation in import_controller | `grep -c 'Wave{'` | 0 | ✓ PASS |
| go vet | `go vet ./cmd/tide-import/... ./internal/controller/...` | exit 0 | ✓ PASS |

### Requirements Coverage

| Requirement | Source Plan(s) | Status | Evidence |
| --- | --- | --- | --- |
| IMPORT-01 | 28-01, 28-02, 28-04, 28-05 | ✓ SATISFIED | 5-site park guard; ImportReconciler; chart+manager wiring |
| IMPORT-02 | 28-03 | ✓ SATISFIED | isEnvelopeComplete + convertSpecRaw strict-validate |
| IMPORT-03 | 28-02, 28-03, 28-04 | ✓ SATISFIED | FQ-name rekey table; parent-chain identity |
| IMPORT-04 | 28-04 | ✓ SATISFIED | ComputeWaves on seed-DAG before Create; cycle→zero CRs envtest; Wave{ count=0 |
| IMPORT-05 | 28-03, 28-04 | ✓ SATISFIED | operator-gated; two-subPath PVC containment; budget suppression |

No orphaned requirements — all IMPORT-01..05 declared in plan frontmatter and marked Complete in REQUIREMENTS.md.

### Anti-Patterns Found

| File | Line | Pattern | Severity | Impact |
| --- | --- | --- | --- | --- |
| — | — | none | — | No TODO/FIXME/XXX/TBD/HACK/placeholder in phase-28 source files |

### Human Verification Required

None. All five success criteria are verifiable from source and pass empirically (build, unit tests, envtests under the phase28 label, firewall targets). The known pre-existing failures (api/v1alpha1 TestDogfoodManifests_* from commit dcd7069; test/integration/kind cluster-required tests) were confirmed out-of-scope for Phase 28 — git history shows zero Phase-28 changes to api/v1alpha1/ or test/integration/kind/.

### Gaps Summary

None. Phase 28 goal is achieved. The locked Approach B (UID-rewrite import) is fully implemented and wired: operator-gated import via spec.importSource, 5-site park guards before pool-acquire (no slot leak), completeness+schema validation in cmd/tide-import, FQ-name stable-identity rekey, cycle detection on the seed-derived planning DAG before any client.Create with zero partial CRs on a cycle, Wave CRs never imported (grep count 0), namespace-scoped PVC containment (two subPaths, no root mount), and budget-rollup suppression for imported projects. Build green, unit + phase28 envtests + guard tests green, both import firewalls green.

---

_Verified: 2026-06-18_
_Verifier: Claude (gsd-verifier)_
