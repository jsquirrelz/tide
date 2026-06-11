# Roadmap: TIDE — Topologically-Indexed Dependency Execution

## Milestones

- ✅ **v1.0.0 — Self-Hosting MVP** (SHIPPED 2026-06-11) — Phases 1–11, 137 plans, 965 commits. Six CRDs + layered-Kahn waves + pluggable subagent dispatch + gates/observability/dashboard/CLI + Helm distribution; release published (binaries, 7 images, 2 OCI charts). Full archive: [milestones/v1.0.0-ROADMAP.md](milestones/v1.0.0-ROADMAP.md) · [milestones/v1.0.0-REQUIREMENTS.md](milestones/v1.0.0-REQUIREMENTS.md)

- 🚧 **v1.0.1 — Orchestrator Trustworthiness + Telemetry Completion** (Phases 12–16) — Fix dogfood run-1 findings; make the orchestrator trustworthy enough to gate run 2 on; complete the merged telemetry foundation.

## Phases

### v1.0.1 — Orchestrator Trustworthiness + Telemetry Completion

**Milestone Goal:** Fix the dogfood run-1 findings (gate semantics run-killer, reject/resume recovery, image dispatch chain, billing halt, budget UX, paper cuts) and complete the merged telemetry foundation. Every fix carries a regression test reproducing the run-1 symptom.

- [ ] **Phase 12: Gate Semantics + Reject/Resume** - Fix ConsumeApprove advancement bug + retry path + resume recovery (run-killers)
- [ ] **Phase 13: Dispatch Image Resolution + Provider Halt** - Implement image-resolution chain at all dispatch sites + billing-400 project-wide halt
- [ ] **Phase 14: Budget Enforcement + Pricing** - Current model IDs in pricing table + BudgetBlocked condition + in-flight overshoot bound
- [ ] **Phase 15: Paper Cuts** - Reporter CR labels, boundary push no-op, phase status flapping, artifact-get stub, dashboard chip + wave view, file-touch overlap
- [ ] **Phase 16: Telemetry Completion** - PROM_ENDPOINT wiring, TelemetryView tab, six locked metrics, PromQL name alignment, Makefile gate, proxy client timeout

## Phase Details

### Phase 12: Gate Semantics + Reject/Resume
**Goal**: Gate passage and reject/resume recovery are correct — approving a level does not advance it past its children, planner retries fire after approval, and `tide resume` recovers rejected children
**Depends on**: Phase 11 (v1.0.0)
**Requirements**: GATE-01, GATE-02, GATE-03, RESUME-01
**Success Criteria** (what must be TRUE):
  1. Approving a gated Milestone with 5 running Phases does not set the Milestone to Succeeded — it remains Running until all Phase children complete (regression test reproduces the run-1 finding-7 symptom using the kind cluster `tide` CRs)
  2. gates.md step 5 reads "approve records gate passage; the level reaches Succeeded only when its children complete" — the old "advances the level to Succeeded" text is gone
  3. Approving a level whose planner Job previously failed with `backoffLimit: 0` triggers a fresh planner Job dispatch — the level does not wedge at AwaitingApproval
  4. `tide resume` after `tide reject` resets fail-marked child Plans to Pending, fires reconciler re-dispatch, and records a `ResumedByUser` condition — matching the manual kubectl status-reset recipe
**Plans**: TBD

### Phase 13: Dispatch Image Resolution + Provider Halt
**Goal**: Subagent image resolves correctly at all four dispatch sites via the documented chain, and a provider billing-400 response halts the entire project instead of burning sessions one at a time
**Depends on**: Phase 12
**Requirements**: DISPATCH-01, DISPATCH-02, HALT-01
**Success Criteria** (what must be TRUE):
  1. A Project with `spec.levels.plan.image` set dispatches that image in the planner Job — `kubectl get job -o yaml` shows the correct image at each of the four reconciler dispatch sites
  2. A Project pinning a real image via `spec.subagent.image` in a released-chart install dispatches that image — no silent stub override; the chart's `--subagent-image` default posture is documented in values.yaml with an explicit comment
  3. A provider response with HTTP 400 "credit balance is too low" sets a `BillingHalt` condition on the Project and stops further Job dispatches — subsequent reconcile loops skip dispatch while the condition is present
**Plans**: TBD

### Phase 14: Budget Enforcement + Pricing
**Goal**: The pricing table resolves current model IDs without warnings, budget-cap exhaustion is visible on the Project and dashboard, and in-flight overshoot past the cap is bounded
**Depends on**: Phase 12
**Requirements**: BUDGET-01, BUDGET-02, BUDGET-03
**Success Criteria** (what must be TRUE):
  1. Sessions using claude-opus-4-8, claude-fable-5, and other current model IDs log no `pricing: unknown model` lines — the pricing table covers all model IDs shipped with v1.0.1
  2. When the project budget cap is reached, a `BudgetBlocked` condition appears on the Project CR — visible via `kubectl get project -o yaml` and reflected on the dashboard project node
  3. In-flight overshoot past the budget cap is bounded to at most one wave's worth of already-dispatched sessions — no new Jobs are created after cap breach is detected
**Plans**: TBD

### Phase 15: Paper Cuts
**Goal**: Seven run-1 correctness and UX regressions are closed — reporter CR labels, boundary push no-op, phase status flapping, artifact-get stub, dashboard status chip, cross-plan wave view, and file-touch overlap
**Depends on**: Phase 12
**Requirements**: CUTS-01, CUTS-02, CUTS-03, CUTS-04, CUTS-05, CUTS-06, CUTS-07
**Success Criteria** (what must be TRUE):
  1. Reporter-created Milestone and Phase CRs carry the `tideproject.k8s/project` label — `tide approve` discovers gated levels on the first call instead of reporting "no level awaiting approval"
  2. `tide push` on a clean tree exits 0 with a "nothing to push" message — no `cannot create empty commit` error
  3. Phase CRs do not oscillate between AwaitingApproval and Running on successive reconcile loops — the status condition converges and stays stable
  4. `tide artifact-get` executes the inspector pod and streams its output — it no longer dry-run prints the pod spec
  5. The dashboard project-node status chip displays "Complete" when the Project CR status is `Complete` — the "Pending" mapping is corrected
  6. The dashboard offers an aggregate view of all currently-running waves across all Plans — the view reads from label-selector queries per the spec's derived-waves model
  7. Two sibling Tasks in the same wave that both declare the same file under `fileTouchMode: strict` are rejected at Plan admission time — the duplicate is surfaced before any Job dispatches
**Plans**: TBD
**UI hint**: yes

### Phase 16: Telemetry Completion
**Goal**: The merged telemetry foundation is functional end-to-end — PROM_ENDPOINT drives the PromQL proxy, TelemetryView is mounted in AppShell, the six locked metrics emit with correct labels, PromQL panel names match, the Makefile gate is wired, and the proxy client is hardened
**Depends on**: Phase 15
**Requirements**: TELEM-01, TELEM-02, TELEM-03, TELEM-04, TELEM-05, TELEM-06
**Success Criteria** (what must be TRUE):
  1. The dashboard reads `PROM_ENDPOINT` from the injected environment and passes it to the PromQL proxy — changing the helm value changes the endpoint the proxy queries without a code change
  2. AppShell renders a Telemetry tab that mounts TelemetryView; Vitest covers both degradation shapes (200 `unavailable` sentinel and 502 error)
  3. TaskReconciler terminal branches emit all six locked metrics (`tide_tokens_{input,output,cache_read,cache_creation}_total`, `tide_cost_cents_total`, `tide_task_duration_seconds`) with `{project, phase, wave}` labels matching the MILESTONE.md table (49e93cb)
  4. All four TelemetryView PromQL panels query the locked metric names — `tide_tasks_dispatched_total` and `tide_tokens_used_total{model}` are replaced with the correct names
  5. `make helm-rbac-assert` and the other telemetry gate scripts in `hack/helm` execute and pass on a running cluster — the Makefile targets are wired and documented
**Plans**: TBD
**UI hint**: yes

## Progress

**Execution Order:**
Phases execute in numeric order: 12 → 13 → 14 → 15 → 16
(14 and 15 depend only on Phase 12 and can be planned in parallel once Phase 12 is complete)

| Phase | Milestone | Plans Complete | Status | Completed |
|-------|-----------|----------------|--------|-----------|
| 12. Gate Semantics + Reject/Resume | v1.0.1 | 0/TBD | Not started | - |
| 13. Dispatch Image Resolution + Provider Halt | v1.0.1 | 0/TBD | Not started | - |
| 14. Budget Enforcement + Pricing | v1.0.1 | 0/TBD | Not started | - |
| 15. Paper Cuts | v1.0.1 | 0/TBD | Not started | - |
| 16. Telemetry Completion | v1.0.1 | 0/TBD | Not started | - |
