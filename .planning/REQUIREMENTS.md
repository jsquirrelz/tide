# Requirements: TIDE v1.0.3 — Planning Resumption & Cost Resilience

**Defined:** 2026-06-18
**Core Value:** The five-level paradigm (Milestone → Phase → Plan → Task → Wave) runs as a real K8s orchestrator that can drive its own next milestone end-to-end.

**Milestone goal:** Make interrupted or budget-halted TIDE runs cheaply resumable — a halt (budget, crash, bug) must never cost the already-authored plan. Motivated by TIDE-on-TIDE dogfood run #2 (2026-06-17/18), which budget-halted *during planning* (~$90 of LLM calls; 3 milestones / 15 phases / 42 plans; ZERO execution) and could not resume without re-paying all planning, because the planner always re-authors from the outcome prompt. The full plan tree survived on the PVC and was salvaged to `examples/projects/dogfood/salvage-20260618/` — the first real plan-import fixture. Predecessor: v1.0.2 Spring Tide (global Execution DAG) is COMPLETE; this milestone builds on it. (Prior milestone requirements archived under `milestones/`.)

**Binding constraints (apply to every requirement below):**
- Persistence stays CRD-`.status`-only — no external DB. Resumption state stays minimal and re-derivable (spec §resumption: indegree map + completed-set); extend that philosophy to planning artifacts (envelopes), never cache a schedule.
- The planner-skip / import path must NOT weaken cycle detection or the wave-boundary failure contract. `client.Create` bypasses the validating webhook, so any import must run cycle detection / `dag.ComputeWaves` explicitly before materializing children.
- Plan-import must be safe across CRD UID churn — no silent adoption of a stale, mismatched, partial-write, or wrong-schema envelope. Validate before skipping a planner; on any doubt, run the planner.
- Wave CRs are NEVER imported — always re-derived (PERSIST-03 / D-10). Import materializes Milestone/Phase/Plan/Task CRs only.
- Keep human-gate policy out of the controller — resume/import compose with the existing gate-as-hold model.
- This milestone de-risks the still-deferred OpenAI-backend + dogfood-run-#2-completion milestone; it does not itself add a provider.

## Milestone v1.0.3 Requirements

Requirements for this milestone. Each maps to exactly one roadmap phase.

### Budget-Bypass Resume Correctness (BYPASS)

- [ ] **BYPASS-01**: Clearing a budget halt (the `tideproject.k8s/bypass-budget` path) resumes the project at `Running`, not `Pending`, so no workspace re-init / re-clone fires. (root cause `project_controller.go:1257`)
- [ ] **BYPASS-02**: A resume never re-runs the init or clone Jobs when the workspace is already initialized — gated on a durable "already initialized" sentinel (`Status.Git.BranchName != ""` / a `CloneComplete` flag), not on Job/envelope presence.
- [ ] **BYPASS-03**: Planner-level Usage is rolled up exactly once across a halt→resume cycle — no double-count when the reporter Job has TTL-GC'd during the halt. Backed by a durable per-object rolled-up marker (e.g. `PlannerRolledUpUID`), not reporter-Job existence.
- [ ] **BYPASS-04**: Resuming a budget halt does not require manually raising both the absolute and rolling-window caps in lockstep — a single resume action (or raising the absolute cap) clears the halt without the rolling-window cap immediately re-halting dispatch.
- [ ] **BYPASS-05**: The project→milestone planner-completion handoff has regression coverage proving the reporter Job spawns AND planner cost rolls up while the planner Job still exists (locks in the ordering fix already landed in `2a5e0dc`, quick task 260617-qqh).

### Plan-Import / Envelope-Resumption (IMPORT)

- [x] **IMPORT-01**: A fresh Project run can adopt pre-authored planner envelopes and SKIP the planner for any level whose valid envelope already exists, proceeding straight to materialize → execution (no re-paid planning).
- [x] **IMPORT-02**: An envelope is validated before adoption — correct schema version (v1alpha2), complete (declared `childCount` present), and not a partial write — and any stale, mismatched, or partial envelope is rejected so the planner runs normally; an invalid envelope is never silently adopted.
- [x] **IMPORT-03**: Import resolves the UID-churn problem — envelopes authored under prior CR UIDs are matched to the new run's CRs by a stable identity (object name + parent chain), with no cross-object or cross-project aliasing. (resolves the Approach-A name-based-lookup vs Approach-B UID-rewrite design checkpoint)
- [ ] **IMPORT-04**: Import re-derives the global Execution DAG and runs cycle detection explicitly before materializing any child CRDs; cyclic or unresolved imported graphs are rejected, and Wave CRs are never imported (always re-derived).
- [x] **IMPORT-05**: Imported-envelope provenance is trust-bounded — import is operator-gated and verifies envelope origin before reading from the shared per-namespace PVC (no unverified third-party envelope is materialized into the CRD API channel).

### Operator Tooling + E2E (TOOL)

- [ ] **TOOL-01**: An operator CLI command exports a Project's planner envelopes to a portable bundle and imports a bundle into a new run, with a dry-run mode that reports what would be adopted vs re-planned.
- [ ] **TOOL-02**: A kind integration test proves end-to-end resumption against the real `examples/projects/dogfood/salvage-20260618` fixture: import the salvaged plan → planners are skipped → execution proceeds, with planning cost not re-paid.

## Future Requirements (deferred)

- Direct-SDK subagent backend for cross-pod prompt-cache benefit (CACHE-F1, carried from Ebb Tide) — would reduce the planning cost this milestone makes resumable, but is orthogonal.
- Partial / incremental re-planning (re-author only the changed sub-tree when the outcome prompt or a parent edits) — a richer evolution of plan-import; out of scope here, where the target is full-tree adoption.
- Automatic export-on-halt (snapshot envelopes to a durable bundle when a budget/failure halt fires) — convenience layer atop TOOL-01.

## Out of Scope (explicit exclusions)

- **Caching the wave schedule in `.status`** — violates the waves-derived-not-declared invariant; waves always re-derive in O(V+E).
- **Skipping or making cycle detection bypassable on import** — cycles are bugs, not a runtime condition; import must reject cyclic graphs, never adopt them.
- **In-memory envelope caching across reconciles / an external DB or object store** — persistence stays CRD-`.status`-only; envelopes stay on the per-namespace PVC.
- **Adding the OpenAI/Codex provider or completing dogfood run #2** — deferred to its own milestone; this milestone only makes that run cheaply resumable.
- **A new gate policy or schema-breaking CRD change** — resume/import compose with existing gates and the v1alpha2 schema; additive status fields only.

## Traceability

| Requirement | Phase | Status |
|-------------|-------|--------|
| BYPASS-01 | Phase 27 | Pending |
| BYPASS-02 | Phase 27 | Pending |
| BYPASS-03 | Phase 27 | Pending |
| BYPASS-04 | Phase 27 | Pending |
| BYPASS-05 | Phase 27 | Pending |
| IMPORT-01 | Phase 28 | Complete |
| IMPORT-02 | Phase 28 | Complete |
| IMPORT-03 | Phase 28 | Complete |
| IMPORT-04 | Phase 28 | Pending |
| IMPORT-05 | Phase 28 | Complete |
| TOOL-01 | Phase 29 | Pending |
| TOOL-02 | Phase 29 | Pending |
