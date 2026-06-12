---
phase: 15-paper-cuts
verified: 2026-06-12T14:10:00Z
status: passed
score: 7/7 must-haves verified
overrides_applied: 0
gaps: []
---

# Phase 15: Paper Cuts Verification Report

**Phase Goal:** Seven run-1 correctness and UX regressions are closed — reporter CR labels, boundary push no-op, phase status flapping, artifact-get stub, dashboard status chip, cross-plan wave view, and file-touch overlap
**Verified:** 2026-06-12T14:10:00Z
**Status:** passed
**Re-verification:** No — initial verification

## Goal Achievement

### Observable Truths (ROADMAP Success Criteria)

| # | Truth | Status | Evidence |
| --- | ----- | ------ | -------- |
| 1 | Reporter Milestone/Phase CRs carry `tideproject.k8s/project`; `tide approve` discovers gated levels first call | ✓ VERIFIED | `owner.StampProjectLabel` create-site stamp (`materialize.go:257`) + D-03 backfill in both reconcilers (`milestone_controller.go:182-194`, `phase_controller.go:174-184`); backfill self-heals from `Spec.ProjectRef`/owner-chain BEFORE dispatch/parking (step 4b precedes step 5). Approve tests `TestApproveLabeledMilestoneDiscoveredFirstCall` + `TestApproveUnlabeledMilestoneNotDiscovered` pin the label-filter contract. Envtest backfill specs pass. **See WR-03 warning.** |
| 2 | `tide push` clean tree exits 0 with "nothing to push" — no `cannot create empty commit` | ✓ VERIFIED | `cmd/tide-push/main.go:488-510` `worktreeClean` guard skips empty commit, still pushes integrated branch with "clean working tree — nothing to commit" message. Regression `TestRunPushBoundaryCleanTreePushesIntegratedBranch` asserts no `cannot create empty commit` in stderr. Test passes. |
| 3 | Phase CRs do not oscillate AwaitingApproval↔Running; status converges | ✓ VERIFIED | `phase_controller.go:247` AwaitingApproval early-return (D-01 parity with milestone). Envtest spec "Phase stays AwaitingApproval through 3 reconciles; zero planner Jobs" passes. |
| 4 | `tide artifact-get` runs inspector pod and streams output — no dry-run print | ✓ VERIFIED | `artifact_get_run.go`: real `CoreV1().Pods.Create` (248), `GetLogs`+`io.Copy` `Follow:true` (267,285), deferred Delete (254), 5m `--timeout` flag (`artifact_get.go:55`). D-10 stdout=raw bytes / stderr=progress (line 28). Fake-seam tests pass. No dry-run print path. |
| 5 | Dashboard project-node chip shows "Complete" when CR status Complete (Pending mapping fixed) | ✓ VERIFIED | `StatusBadge.tsx:106-112` Complete row, success color, distinct CircleCheckBig icon. Both coerce sites import shared `KNOWN_STATUS_VALUES` (`PlanningDAGView.tsx:24`, `ProjectPicker.tsx:10`). 39 Vitest tests pass. |
| 6 | Dashboard aggregate view of all running waves; reads label-selector queries per derived-waves model | ✓ VERIFIED | Backend `computeRunningWaves` (`waves.go:94`) label-selects on `tideproject.k8s/project`+`wave-index`, emits `waves.snapshot` SSE on subscribe (`events_sse.go:212`) and Task informer change (`informer_bridge.go:164`); `waves: []` never null (line 161). Frontend `RunningWavesView` consumes it, App.tsx swaps right-pane default + "All waves" return; old empty state removed (pinned by `App.test.tsx:179`). Go + Vitest tests pass. |
| 7 | Two sibling Tasks in same wave sharing a file under strict mode rejected before any Job dispatch | ✓ VERIFIED | PlanReconciler dispatch gate `reconcileWaveMaterialization` step 2b (`plan_controller.go:992-1006`) runs after Tasks materialize, BEFORE wave derivation/dispatch; parks `ValidationState=FileTouchMismatch` (park-not-fail). Webhook D-08 resolves real Project mode via `resolveProjectForWebhook` (no more nil). Authoritative seat per 15-CONTEXT.md D-05. Envtest specs pass. |

**Score:** 7/7 truths verified

### Required Artifacts

| Artifact | Expected | Status | Details |
| -------- | -------- | ------ | ------- |
| `internal/owner/label.go` | StampProjectLabel + LabelProject | ✓ VERIFIED | Both exported, fail-open on empty string |
| `internal/reporter/materialize.go` | Create-site label stamp | ✓ VERIFIED | `owner.StampProjectLabel` at :257 (no-op for Project parent — WR-03) |
| `internal/controller/milestone_controller.go` | Label backfill | ✓ VERIFIED | Step 4b, idempotent, resolves via Spec.ProjectRef |
| `internal/controller/phase_controller.go` | Label backfill + AwaitingApproval early-return | ✓ VERIFIED | Backfill step 4b; early-return :247 |
| `internal/webhook/v1alpha1/plan_webhook.go` | ComputeFileTouchMismatches/SummariseMismatches + real mode | ✓ VERIFIED | Both exported; D-08 `resolveProjectForWebhook` |
| `internal/controller/plan_controller.go` | File-touch dispatch gate + park | ✓ VERIFIED | Gate step 2b; `patchPlanFileTouchMismatch` |
| `cmd/tide/artifact_get_run.go` | Real inspector pod | ✓ VERIFIED | Create/wait/stream/delete; tide-projects PVC mount |
| `cmd/tide/artifact_get.go` | --timeout flag | ✓ VERIFIED | DurationVar default 5m |
| `cmd/dashboard/api/waves.go` | computeRunningWaves + payload types | ✓ VERIFIED | Label-selector derivation, deterministic sort, [] not null |
| `dashboard/web/src/components/StatusBadge.tsx` | Complete row + KNOWN_STATUS_VALUES | ✓ VERIFIED | CircleCheckBig, success color, exported KNOWN list |
| `dashboard/web/src/components/RunningWavesView.tsx` | Aggregate wave-card view | ✓ VERIFIED | Consumes waves.snapshot, spinner/empty/card states |
| `internal/subagent/common/templates/plan_planner.tmpl` | D-07 file-touch prompt guidance | ✓ VERIFIED | "FILE-TOUCH RULE" lines 67-71 |

### Key Link Verification

| From | To | Via | Status |
| ---- | -- | --- | ------ |
| materialize.go | owner.label.go | StampProjectLabel before Create | ✓ WIRED |
| plan_controller.go | plan_webhook.go | ComputeFileTouchMismatches | ✓ WIRED |
| artifact_get_run.go | tide-projects PVC | volumeMount subPath = project UID | ✓ WIRED |
| informer_bridge.go | hub.Publish | waves.snapshot on Task event | ✓ WIRED |
| PlanningDAGView/ProjectPicker | StatusBadge | imported KNOWN_STATUS_VALUES | ✓ WIRED |
| sse.ts | waves.snapshot emission | SSE_PROJECT_EVENT_TYPES + addEventListener | ✓ WIRED |
| App.tsx | RunningWavesView | right-pane default when selectedPlan === null | ✓ WIRED |

### Behavioral Spot-Checks

| Behavior | Command | Result | Status |
| -------- | ------- | ------ | ------ |
| Go build all modified pkgs | `go build ./internal/... ./cmd/...` | exit 0 | ✓ PASS |
| Go unit tests (owner/reporter/webhook/tide/tide-push/dashboard) | `go test` | all `ok` | ✓ PASS |
| Frontend Vitest (StatusBadge/RunningWavesView/App/ProjectPicker) | `vitest run` | 39 passed | ✓ PASS |
| Controller envtest suite (incl. CUTS-01/03/07 specs) | `go test ./internal/controller/ -count=1` | ok, 61s, all 139 specs | ✓ PASS |
| Focused CUTS specs (backfill/oscillation/file-touch) | `go test -ginkgo.focus` | ok | ✓ PASS |

Note: envtest required `KUBEBUILDER_ASSETS` absolute path + sandbox-disabled exec (sandbox blocks fork/exec of etcd binary) — an environment constraint, not a code defect.

### Requirements Coverage

| Requirement | Source Plan | Description | Status |
| ----------- | ----------- | ----------- | ------ |
| CUTS-01 | 15-01 | Reporter CR project labels → approve discovery | ✓ SATISFIED |
| CUTS-02 | 15-04 | Boundary push clean-tree no-op | ✓ SATISFIED |
| CUTS-03 | 15-04 | Phase AwaitingApproval↔Running convergence | ✓ SATISFIED |
| CUTS-04 | 15-03 | artifact-get real inspector pod | ✓ SATISFIED |
| CUTS-05 | 15-05 | Dashboard Complete status chip | ✓ SATISFIED |
| CUTS-06 | 15-06 + 15-07 | Cross-plan running-waves aggregate | ✓ SATISFIED |
| CUTS-07 | 15-02 | Strict file-touch sibling-overlap rejection | ✓ SATISFIED |

All 7 CUTS IDs claimed across plans; zero orphaned requirements vs REQUIREMENTS.md Phase-15 mapping.

### Anti-Patterns Found

| File | Pattern | Severity | Impact |
| ---- | ------- | -------- | ------ |
| (none) | No unreferenced TBD/FIXME/XXX debt markers in phase-modified files | — | — |

### Code-Review Findings Assessment (15-REVIEW.md)

| Finding | Phase-15-introduced? | Disposition |
| ------- | -------------------- | ----------- |
| **CR-01** (plan_controller.go envelope-read transient → terminal Failed) | **NO — pre-existing** | `EnvelopeReadFailed` block introduced in commit `16baadb` (phase 03-08), last touched `c6d26c3` (phase 09-08). Phase 15's only commit to plan_controller.go (`aef8316`, the file-touch gate) did not touch lines 488-504. Per goal-backward scope: noted, does NOT fail Phase 15. **Recommend tracking as a follow-up parity-fix issue** (mirror milestone/phase Pitfall-1 non-fatal requeue). |
| **WR-03** (D-01 project-label stamp no-op for Project→Milestone edge) | YES (this is a known shape of the create-site stamp) | **WARNING, not blocker.** The create-site stamp is genuinely a no-op for the Project→Milestone reporter edge (a Project CR carries no `tideproject.k8s/project` label — verified: no stamp site in project_controller.go). BUT the D-03 MilestoneReconciler backfill (step 4b, envtest-covered) heals the label from `Spec.ProjectRef` BEFORE the level parks at AwaitingApproval (step 5). The finding-6 symptom (permanent unlabeled → "no level awaiting approval") is closed; the residual window is sub-second pre-first-reconcile during which the level is not yet parked. SC1's observable outcome holds. Gap: the Project-parent create-site path is untested (`TestMaterializeChildCRDsStampsProjectLabel` only exercises a Milestone parent) and the stamp is dead at that edge. |
| WR-01/02/04/05/06/07, IN-01..07 | mixed | Quality/robustness warnings (hot poll loop, pre-charge timing, condition polarity, SSE namespace scoping, label-literal drift). None block the seven success criteria. Recommend folding into a hardening pass. |

### Human Verification Required

None required. All seven success criteria are observable via code inspection + automated tests (Go unit, controller envtest, frontend Vitest) — all green. Dashboard visual behavior (CUTS-05 chip render, CUTS-06 aggregate pane) is covered by Vitest assertions on the rendered output.

### Gaps Summary

No goal-blocking gaps. All 7 ROADMAP success criteria are achieved with passing regression tests. Two review findings were weighed:

- **CR-01 is pre-existing** (git-confirmed phase 03-08 origin, untouched by Phase 15) — out of scope for failing this phase; recommend a follow-up issue to bring plan_controller envelope-read handling to milestone/phase Pitfall-1 parity.
- **WR-03 is a real but non-blocking warning** — the create-site stamp is dead for the Project→Milestone edge, but the D-03 reconciler backfill closes the finding-6 symptom before any level parks for approval. Recommend the one-line `*Project` special-case + a Project-parent test row to make the create-site stamp authoritative and remove the backfill dependency.

---

_Verified: 2026-06-12T14:10:00Z_
_Verifier: Claude (gsd-verifier)_
