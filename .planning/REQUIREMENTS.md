# Requirements: TIDE v1.0.1 — Orchestrator Trustworthiness + Telemetry Completion

**Defined:** 2026-06-11
**Core Value:** The five-level paradigm (Milestone → Phase → Plan → Task → Wave) runs as a real K8s orchestrator that can drive its own next milestone end-to-end. v1.0.1's bar: trustworthy enough to gate dogfood run 2 on.

Every requirement below carries an implicit acceptance criterion: **a regression test that reproduces the dogfood run-1 symptom** (TDD where the surface already has tests). Findings reference the run-1 supervision log (memory: dogfood-run1-findings, v1-stub-image-bug).

## v1.0.1 Requirements

### Gate Semantics (GATE)

- [ ] **GATE-01**: Approving a gated level with incomplete children does not advance it to Succeeded — approval records gate passage; the level reaches Succeeded only when its children complete (finding 7, the run-killer: `ConsumeApprove` advanced a Milestone with 5 running Phases straight to Succeeded → Project `Complete`)
- [ ] **GATE-02**: gates.md step 5 documents approve-then-wait-for-children semantics (the current doc encodes the bug — "advances the level to Succeeded")
- [ ] **GATE-03**: Approval after a failed planner attempt triggers a retry dispatch instead of wedging the level (finding 5 — `backoffLimit: 0` planner failure during AwaitingApproval leaves no attempt-2 path)

### Reject/Resume Recovery (RESUME)

- [ ] **RESUME-01**: `tide resume` after `tide reject` recovers fail-marked children — status reset, reconciler re-dispatch, `ResumedByUser` condition — matching the manual kubectl recipe (finding 9a: reject patches children `Failed` via patchPlanFailed; reconcilers early-exit on Failed; resume only clears the annotation)

### Dispatch Image Resolution (DISPATCH)

- [ ] **DISPATCH-01**: Subagent image resolves via the documented chain (`Levels.<level>.Image` → `Spec.Subagent.Image` → flag/Helm default) at all four reconciler dispatch sites (v1-stub-image-bug: chain implemented for Model only in ResolveProvider; no code path consults `project.Spec.Subagent.Image`)
- [ ] **DISPATCH-02**: A released-chart install with a Project pinning a real image dispatches that image — no silent stub override; the chart's `--subagent-image=stub` default posture is explicitly decided and documented

### Provider Failure Halt (HALT)

- [ ] **HALT-01**: A provider billing 400 ("credit balance is too low") halts further dispatch project-wide and surfaces a condition on the Project, instead of crashing the fan-out one session at a time (finding 9a tail: ~$80 of run 1's $140.64 burned by dying sessions across two credit dry-outs)

### Budget & Pricing (BUDGET)

- [ ] **BUDGET-01**: Pricing table resolves current model IDs (claude-opus-4-8, claude-fable-5, …) without falling to the conservative default (finding 4 reduced scope: the fallback was near-accurate in aggregate but logs `pricing: unknown model` per session)
- [ ] **BUDGET-02**: Budget-cap enforcement surfaces `BudgetBlocked` on the Project status, visible to kubectl and the dashboard (finding 12: cap works at task dispatch but is silent on the Project)
- [ ] **BUDGET-03**: In-flight overshoot past the budget cap is bounded (run 1 overshot ~$40 past the $100 cap from already-dispatched sessions)

### Paper Cuts (CUTS)

- [ ] **CUTS-01**: Reporter-created Milestone/Phase CRs carry the `tideproject.k8s/project` label so `tide approve` discovers gated levels (finding 6: zero labels → "no level awaiting approval" despite a parked CR)
- [ ] **CUTS-02**: Boundary push no-ops cleanly on a clean tree instead of erroring/retrying on `cannot create empty commit` (finding 8)
- [ ] **CUTS-03**: Phase CRs stop oscillating AwaitingApproval↔Running (finding 2: cosmetic requeue loop, noisy for watchers)
- [ ] **CUTS-04**: `tide artifact-get` runs the inspector pod for real instead of dry-run printing its spec (finding 3)
- [ ] **CUTS-05**: Dashboard project-node status chip maps CR status `Complete` correctly (finding 9b: showed "Pending")
- [ ] **CUTS-06**: Dashboard offers a cross-plan "all running waves right now" view (run-1 user feature 2; waves stay per-plan derivations per spec — this is an aggregate read surface)
- [ ] **CUTS-07**: Sibling tasks in one wave cannot both declare the same file under strict fileTouchMode (root cause of run-1's two merge-conflict task branches despite `fileTouchMode: strict`)

### Telemetry Completion (TELEM)

- [ ] **TELEM-01**: The dashboard reads `PROM_ENDPOINT` into `Dependencies.PrometheusEndpoint` — the helm-injected env actually drives the PromQL proxy (dead config today: helm injects, nothing reads)
- [ ] **TELEM-02**: TelemetryView is mounted as a Telemetry tab in AppShell with Vitest coverage of both degradation shapes (200 `unavailable` sentinel and 502 error)
- [ ] **TELEM-03**: The six locked metrics (`tide_tokens_{input,output,cache_read,cache_creation}_total`, `tide_cost_cents_total`, `tide_task_duration_seconds`) are emitted from the TaskReconciler terminal branch with `{project, phase, wave}` labels per the merged MILESTONE.md table
- [ ] **TELEM-04**: TelemetryView PromQL queries use the locked metric names (two of four panels query nonexistent names: `tide_tasks_dispatched_total`, `tide_tokens_used_total{model}`)
- [ ] **TELEM-05**: The `hack/helm` telemetry gate scripts are wired into the Makefile (docstrings claim `make helm-rbac-assert` drives them; nothing does)
- [ ] **TELEM-06**: PrometheusHandler uses a bounded HTTP client (timeout + request-context propagation) and preserves base paths in the configured endpoint URL

## Future Requirements

Deferred — tracked but not in this roadmap.

### Cost Engineering

- **COST-01**: Orchestrator-level prompt-caching strategy — stable shared prompt prefixes across planner levels, staggered dispatch (first planner warms cache, siblings read), cache-aware cost accounting (run-1 user feature 3)
- **COST-02**: Provider-key budget on dashboard — surface Anthropic org credit balance so billing failures are visible before they kill a wave (run-1 user feature 1)

## Out of Scope

| Feature | Reason |
|---------|--------|
| Dogfood run 2 (02-codex-runtime) | Gated ON this milestone, not part of it — explicitly excluded this session |
| Full TIDE-on-TIDE headline | Next major milestone; v1.0.1 is the trustworthiness prerequisite |
| docs/audit/ 27-item hardening backlog | Separate hardening milestone; none of it blocks run 2 |
| Schema-constrained MCP `emit_child` | Blocked on `claude --bare` upstream (project memory: child-crd-json-mandated) |
| Caps floor bump (finding 11) | Already DONE on main (47a9aa9) |

## Traceability

Which phases cover which requirements.

| Requirement | Phase | Status |
|-------------|-------|--------|
| GATE-01 | Phase 12 | Pending |
| GATE-02 | Phase 12 | Pending |
| GATE-03 | Phase 12 | Pending |
| RESUME-01 | Phase 12 | Pending |
| DISPATCH-01 | Phase 13 | Pending |
| DISPATCH-02 | Phase 13 | Pending |
| HALT-01 | Phase 13 | Pending |
| BUDGET-01 | Phase 14 | Pending |
| BUDGET-02 | Phase 14 | Pending |
| BUDGET-03 | Phase 14 | Pending |
| CUTS-01 | Phase 15 | Pending |
| CUTS-02 | Phase 15 | Pending |
| CUTS-03 | Phase 15 | Pending |
| CUTS-04 | Phase 15 | Pending |
| CUTS-05 | Phase 15 | Pending |
| CUTS-06 | Phase 15 | Pending |
| CUTS-07 | Phase 15 | Pending |
| TELEM-01 | Phase 16 | Pending |
| TELEM-02 | Phase 16 | Pending |
| TELEM-03 | Phase 16 | Pending |
| TELEM-04 | Phase 16 | Pending |
| TELEM-05 | Phase 16 | Pending |
| TELEM-06 | Phase 16 | Pending |

**Coverage:**
- v1.0.1 requirements: 23 total
- Mapped to phases: 23
- Unmapped: 0 ✓

---
*Requirements defined: 2026-06-11*
*Last updated: 2026-06-11 — traceability populated by roadmapper*
