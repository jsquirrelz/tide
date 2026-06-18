---
phase: 28-plan-import-core
status: secured
asvs_level: 1
block_on: high
threats_total: 16
threats_closed: 16
threats_open: 0
audited: 2026-06-18
---

# SECURITY.md — Phase 28 (plan-import-core)

**Audit date:** 2026-06-18
**ASVS Level:** 1
**Block policy:** high
**Disposition:** SECURED — all 16 declared threats CLOSED (10 mitigate verified in code, 6 accept rationale confirmed)
**Threats open:** 0

Phase 28 implements envelope-import (Approach B, UID-rewrite): it materializes UNTRUSTED
foreign envelope data from a shared per-namespace PVC into the K8s CRD API channel. Each
PLAN.md carried its `<threat_model>` at plan time; this audit VERIFIES each declared
mitigation exists in the shipped implementation (no new-threat scan).

---

## Threat Verification — MITIGATE (code-verified)

| Threat ID | Category | Evidence (file:line) |
|-----------|----------|----------------------|
| T-28-02-01 | EoP | `api/v1alpha2/project_types.go:412` — `ImportSource *ImportSourceRef` optional `omitempty` pointer; operator-gated, RBAC trust anchor. Field merely declares intent; containment is enforced downstream. |
| T-28-02-02 | Tampering | `api/v1alpha2/import_types.go:25,30` MinLength=1 markers on both sub-fields → rendered as `minLength: 1` + `required` in `config/crd/bases/tideproject.k8s_projects.yaml:720,726,729-730` AND `charts/tide-crds/templates/project-crd.yaml:721,727,730-731`. Rejects empty seedManifestConfigMap / salvagedPVCSubPath. |
| T-28-03-01 | EoP (path traversal) | `cmd/tide-import/main.go:247-261` `containedJoin` (IsAbs reject + filepath.Clean + `..`-prefix reject + HasPrefix-base second-line). Called for both oldUID (`:140`) and newUID (`:145`) before any copy → exit 2, writes nothing. Tests `TestPathTraversal`, `TestPathTraversalNewUID`. |
| T-28-03-02 | Tampering | `cmd/tide-import/main.go:63-68` `childKindAllowlist {Milestone,Phase,Plan,Task}` (Wave excluded, D-09); checked at `:180` before conversion → exit 2; `convertSpecRaw` `:282-312` typed round-trip strips unknown fields, `default` case fails closed. Tests `TestKindAllowlistReject`, `TestConversionNoOp`. |
| T-28-03-03 | Tampering | `cmd/tide-import/main.go:265-275` `isEnvelopeComplete` (exitCode!=0 OR ChildCount>0 && len(ChildCRDs)!=ChildCount → incomplete, skipped at `:170-175`, recorded `Incomplete++`, NOT adopted). Atomic write+rename `:402-430`. Tests `TestCompletenessRejectExitCode`, `TestCompletenessRejectChildCountMismatch`. |
| T-28-03-04 | Spoofing | `cmd/tide-import/main.go:80-84` `rekeyEntry.FQName` (object name + parent chain) keys each entry; controller keys `rekeyTable` by fqName (`import_controller.go:404,452,499`). Test `TestFQNameNoAliasing`. |
| T-28-04-01 | DoS (cyclic seed) | `internal/controller/import_controller.go:351` `dag.ComputeWaves(seedNodes, seedEdges)` on SEED-DERIVED Milestone/Phase/Plan names+dependsOn (`:316-349`), runs BEFORE first `r.Create` (`:391`). CycleError→ReasonCyclicPlanDetected (`:357-359`), unknown-node→ReasonImportFailed (`:363-365`); both via `failImport` (status-patch only, ZERO CRs). Envtest Test 2 asserts zero CRs. NOT buildGlobalEdges. |
| T-28-04-02 | Tampering | `import_controller.go` materializes ONLY Milestone/Phase/Plan (no code path to other Kinds); FQ-name keyed rekeyTable; `owner.EnsureOwnerRef` same-namespace (`:388,437,484,525`). Binary re-enforces Kind allowlist (T-28-03-02). |
| T-28-04-03 | InfoDisclosure/EoP (PVC mount) | `internal/controller/import_jobspec.go:181-203` — single PVC volume, two subPath mounts: `/old-workspace` subPath=OldSubPath ReadOnly=true (`:185-189`), `/new-workspace` subPath=NewSubPath RW (`:190-196`); never root. Hardened SecurityContext RunAsNonRoot + AllowPrivilegeEscalation=false + drop ALL caps (`:117-123`). Owner-ref same-ns (`:217`). |
| T-28-04-04 | Spoofing (re-fire) | `import_controller.go:195-199` ConditionImportComplete=True → no-op return; `apierrors.IsAlreadyExists` = success on every Create (`:392,441,488,529,589`); deterministic names `tide-import-rekey-<UID>` (`:515`), `tide-import-<UID>` (`:553`). Envtest Test 4. |
| T-28-04-05 / T-28-05-03 | Repudiation (budget double-count) | `internal/controller/project_controller.go:1253-1254` — `handleProjectJobCompletion` skips `budget.RollUpUsage` when `project.Spec.ImportSource != nil` (D-11); ImportController itself never rolls up. |
| T-28-05-01 | DoS (slot leak) | Guard placed BEFORE `PlannerPool.Acquire` at all planner sites: milestone `:370`<`:382`, phase `:368`<`:380`, plan `:365`<`:377`, project `:1074`<`:1084`. Task site `:389` returns `shouldHalt` before billing-halt + pool acquisition. Test `TestImportGuard_ParkOnPending_NoPoolAcquired`. |
| T-28-05-02 | Tampering (premature dispatch) | All 5 sites hold (RequeueAfter, park) while `cond == nil OR cond.Status != ConditionTrue` (milestone `:372`, phase, plan, project `:1076`, task `:391`). Released only on ConditionImportComplete=True. Test `TestImportGuard_ClearOnComplete`. |
| T-28-05-04 | Spoofing (bleed into normal Projects) | Every guard gated on `Spec.ImportSource != nil`; non-import Projects unchanged. Tests `TestImportGuard_NoImportSource_NeverFires`, `TestImportGuard_NilProject_NeverFires`. |

## Threat Verification — ACCEPT (accepted risk, rationale confirmed)

| Threat ID | Category | Accepted-risk rationale (confirmed sound) |
|-----------|----------|-------------------------------------------|
| T-28-01-01 | Tampering | `images.tideImport.repository/tag` is operator-controlled at install (`charts/tide/values.yaml:199-202`). Same trust model as existing `tideReporter`/`tidePush` image blocks; only Helm/RBAC-write operators set it. No new exposure beyond the existing image-value surface. Production override should pin `@sha256:` per CLAUDE.md. ACCEPTED. |
| T-28-0X-SC (T-28-01-SC, -02-SC, -03-SC, -04-SC, -05-SC) | Tampering | No package-manager installs; zero new `go.mod`/`go.sum` entries across all Phase-28 commits (verified: no phase-28 commit touches go.mod/go.sum). Pure Go + chart YAML + existing deps (controller-runtime, pkg/dag, api/v1alpha2, pkg/dispatch). tide-import binary firewall-clean (no internal/controller-runtime imports). ACCEPTED. |

---

## Unregistered Flags

None. SUMMARY `## Threat Flags` sections (28-01, 28-05) map to declared threats
(T-28-01-01 operator-controlled-image; guard = read-only condition check, no new surface).
28-02/03/04 declared no new threat surface. No new attack surface appeared without a
threat mapping.

## Auditor Notes (informational — not gaps)

- **T-28-03-03 ChildCount==0 edge:** `isEnvelopeComplete` flags an undercount only when
  `ChildCount > 0`. A `ChildCount==0` envelope with extra ChildCRDs is not flagged
  incomplete, but every extra child is still Kind-checked + typed-converted (`main.go:179-191`),
  so the central CRD-API surface is not bypassed. Mitigation intent holds.
- **T-28-04-02 controller Kind enforcement is structural:** the controller has no code path
  to create any Kind outside {Milestone,Phase,Plan}; plan-04 envtest Test 3 ships as
  "empty-seed succeeds" rather than a Kind-reject branch. The Kind *reject* is enforced and
  tested in the binary (`TestKindAllowlistReject`). No gap.
- **CRD chart-path drift:** plan-02 frontmatter cited `charts/tide/crds/...` but the chart
  CRD actually lives at `charts/tide-crds/templates/project-crd.yaml` (verified present with
  minLength+required). Documentation path stale; implementation correct.

## Disposition

All 10 mitigate threats are present in code at the cited locations and exercised by named
tests; all 6 accept threats have sound, confirmed rationale. With `block_on: high` and zero
open high-severity threats (path-traversal, PVC-mount containment, cycle-before-create, and
slot-leak guard placement all CLOSED), Phase 28 is cleared to ship.
