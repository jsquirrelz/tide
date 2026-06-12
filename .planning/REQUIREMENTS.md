# Requirements: TIDE v1.0.1 — Orchestrator Trustworthiness + Telemetry Completion

**Defined:** 2026-06-11
**Core Value:** The five-level paradigm (Milestone → Phase → Plan → Task → Wave) runs as a real K8s orchestrator that can drive its own next milestone end-to-end. v1.0.1's bar: trustworthy enough to gate dogfood run 2 on.

Every requirement below carries an implicit acceptance criterion: **a regression test that reproduces the dogfood run-1 symptom** (TDD where the surface already has tests). Findings reference the run-1 supervision log (memory: dogfood-run1-findings, v1-stub-image-bug).

## v1.0.1 Requirements

### Gate Semantics (GATE)

- [x] **GATE-01**: Approving a gated level with incomplete children does not advance it to Succeeded — approval records gate passage; the level reaches Succeeded only when its children complete (finding 7, the run-killer: `ConsumeApprove` advanced a Milestone with 5 running Phases straight to Succeeded → Project `Complete`)
- [x] **GATE-02**: gates.md step 5 documents approve-then-wait-for-children semantics (the current doc encodes the bug — "advances the level to Succeeded")
- [x] **GATE-03**: A level whose planner Job failed is recoverable via `tide resume --retry-failed`, never wedged; `tide approve` against it gives an actionable error pointing at resume (finding 5 reworded per Phase 12 discussion D-07 — approval never doubles as a spend-retry)
- [x] **GATE-04**: A level parked at AwaitingApproval blocks child dispatch — children materialize (visible in dashboard/kubectl) but their reconcilers hold all Job dispatch until the parent is approved (finding 1, gate-before-descent: run 1 fired 5 × ~$0.64 phase planners one second after the milestone parked; folded into Phase 12 per discussion D-01/D-02)

### Reject/Resume Recovery (RESUME)

- [x] **RESUME-01**: `tide resume` after `tide reject` recovers fail-marked children — status reset, reconciler re-dispatch, `ResumedByUser` condition — matching the manual kubectl recipe (finding 9a: reject patches children `Failed` via patchPlanFailed; reconcilers early-exit on Failed; resume only clears the annotation)

### Dispatch Image Resolution (DISPATCH)

- [x] **DISPATCH-01**: Subagent image resolves via the documented chain (`Levels.<level>.Image` → `Spec.Subagent.Image` → flag/Helm default) at all four reconciler dispatch sites (v1-stub-image-bug: chain implemented for Model only in ResolveProvider; no code path consults `project.Spec.Subagent.Image`)
- [x] **DISPATCH-02**: A released-chart install with a Project pinning a real image dispatches that image — no silent stub override; the chart's `--subagent-image=stub` default posture is explicitly decided and documented

### Provider Failure Halt (HALT)

- [x] **HALT-01**: A provider billing 400 ("credit balance is too low") halts further dispatch project-wide and surfaces a condition on the Project, instead of crashing the fan-out one session at a time (finding 9a tail: ~$80 of run 1's $140.64 burned by dying sessions across two credit dry-outs)

### Budget & Pricing (BUDGET)

- [x] **BUDGET-01**: Pricing table resolves current model IDs (claude-opus-4-8, claude-fable-5, …) without falling to the conservative default (finding 4 reduced scope: the fallback was near-accurate in aggregate but logs `pricing: unknown model` per session)
- [x] **BUDGET-02**: Budget-cap enforcement surfaces `BudgetBlocked` on the Project status, visible to kubectl and the dashboard (finding 12: cap works at task dispatch but is silent on the Project)
- [x] **BUDGET-03**: In-flight overshoot past the budget cap is bounded (run 1 overshot ~$40 past the $100 cap from already-dispatched sessions)

### Paper Cuts (CUTS)

- [x] **CUTS-01**: Reporter-created Milestone/Phase CRs carry the `tideproject.k8s/project` label so `tide approve` discovers gated levels (finding 6: zero labels → "no level awaiting approval" despite a parked CR)
- [x] **CUTS-02**: Boundary push no-ops cleanly on a clean tree instead of erroring/retrying on `cannot create empty commit` (finding 8)
- [x] **CUTS-03**: Phase CRs stop oscillating AwaitingApproval↔Running (finding 2: cosmetic requeue loop, noisy for watchers)
- [x] **CUTS-04**: `tide artifact-get` runs the inspector pod for real instead of dry-run printing its spec (finding 3)
- [x] **CUTS-05**: Dashboard project-node status chip maps CR status `Complete` correctly (finding 9b: showed "Pending")
- [x] **CUTS-06**: Dashboard offers a cross-plan "all running waves right now" view (run-1 user feature 2; waves stay per-plan derivations per spec — this is an aggregate read surface)
- [x] **CUTS-07**: Sibling tasks in one wave cannot both declare the same file under strict fileTouchMode (root cause of run-1's two merge-conflict task branches despite `fileTouchMode: strict`)

### Telemetry Completion (TELEM)

- [x] **TELEM-01**: The dashboard reads `PROM_ENDPOINT` into `Dependencies.PrometheusEndpoint` — the helm-injected env actually drives the PromQL proxy (dead config today: helm injects, nothing reads)
- [x] **TELEM-02**: TelemetryView is mounted as a Telemetry tab in AppShell with Vitest coverage of both degradation shapes (200 `unavailable` sentinel and 502 error)
- [x] **TELEM-03**: The six locked metrics (`tide_tokens_{input,output,cache_read,cache_creation}_total`, `tide_cost_cents_total`, `tide_task_duration_seconds`) are emitted from the TaskReconciler terminal branch with `{project, phase, wave}` labels per the merged MILESTONE.md table
- [x] **TELEM-04**: TelemetryView PromQL queries use the locked metric names (two of four panels query nonexistent names: `tide_tasks_dispatched_total`, `tide_tokens_used_total{model}`)
- [x] **TELEM-05**: The `hack/helm` telemetry gate scripts are wired into the Makefile (docstrings claim `make helm-rbac-assert` drives them; nothing does)
- [x] **TELEM-06**: PrometheusHandler uses a bounded HTTP client (timeout + request-context propagation) and preserves base paths in the configured endpoint URL

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
| GATE-01 | Phase 12 | Complete |
| GATE-02 | Phase 12 | Complete |
| GATE-03 | Phase 12 | Complete |
| GATE-04 | Phase 12 | Complete |
| RESUME-01 | Phase 12 | Complete |
| DISPATCH-01 | Phase 13 | Complete |
| DISPATCH-02 | Phase 13 | Complete |
| HALT-01 | Phase 13 | Complete |
| BUDGET-01 | Phase 14 | Complete |
| BUDGET-02 | Phase 14 | Complete |
| BUDGET-03 | Phase 14 | Complete |
| CUTS-01 | Phase 15 | Complete |
| CUTS-02 | Phase 15 | Complete |
| CUTS-03 | Phase 15 | Complete |
| CUTS-04 | Phase 15 | Complete |
| CUTS-05 | Phase 15 | Complete |
| CUTS-06 | Phase 15 | Complete |
| CUTS-07 | Phase 15 | Complete |
| TELEM-01 | Phase 16 | Complete |
| TELEM-02 | Phase 16 | Complete |
| TELEM-03 | Phase 16 | Complete |
| TELEM-04 | Phase 16 | Complete |
| TELEM-05 | Phase 16 | Complete |
| TELEM-06 | Phase 16 | Complete |

**Coverage:**
- v1.0.1 requirements: 24 total
- Mapped to phases: 24
- Unmapped: 0 ✓

---
*Requirements defined: 2026-06-11*
*Last updated: 2026-06-11 — traceability populated by roadmapper*
