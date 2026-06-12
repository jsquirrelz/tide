# Phase 15: Paper Cuts - Context

**Gathered:** 2026-06-12
**Status:** Ready for planning

<domain>
## Phase Boundary

Seven run-1 correctness and UX regressions are closed: reporter CR labels (CUTS-01), boundary push no-op (CUTS-02), phase status flapping (CUTS-03), artifact-get stub (CUTS-04), dashboard status chip (CUTS-05), cross-plan running-waves view (CUTS-06), and file-touch overlap enforcement (CUTS-07). Every fix carries a regression test reproducing the run-1 symptom. Scout finding that reshapes the work: CUTS-02 appears already fixed on main (8f0b99b, Phase 11) and CUTS-03 appears already fixed (abf177c, Phase 12-01 AwaitingApproval early-return) — for those two, the deliverable is verification + a regression test that pins the fix, not new behavior.

</domain>

<decisions>
## Implementation Decisions

### Label fix + approve discovery (CUTS-01)
- **D-01: Universal label stamping.** A shared helper stamps `tideproject.k8s/project` on every child CR at every create site — reporter (`internal/reporter/materialize.go`, which currently sets no labels) AND all reconciler create sites. The scout found the gap is systemic: `internal/gates/boundary.go` documents that only Plan→Task stamps the label today, which is why it falls back to OwnerRef walks. One seam, no future regressions.
- **D-02: `tide approve` discovery unchanged.** It keeps its existing list + label-filter shape (cmd/tide/approve.go:196-243). No OwnerRef fallback path — labels become reliable at creation; one source of truth.
- **D-03: Reconciler backfill for existing CRs.** Reconcilers patch the missing project label onto observed unlabeled CRs, deriving the project from the OwnerRef chain. Idempotent, self-healing for the live run-1 kind cluster and any v1.0.0 installs.
- **D-04: Project label only.** No broader role/level label parity for reporter-created CRs — other labels stay stamped by the components that need them.

### File-touch enforcement seat (CUTS-07)
- **Root cause (confirmed by scout):** `computeFileTouchMismatches` already exists and is correct, but lives only in the Plan admission webhook, which early-returns with a warning when zero Tasks are visible (plan_webhook.go:139-147, "Pitfall B"). In the reporter flow Tasks always materialize AFTER the Plan, so the check never ran in run 1. The check never re-runs.
- **D-05: Reconciler dispatch gate is the authoritative seat.** PlanReconciler re-runs the mismatch check once all Tasks are materialized, BEFORE wave derivation/dispatch. Catches the reporter flow and the kubectl flow. Sets the dormant `ValidationState=FileTouchMismatch` enum value (plan_types.go:45 — schema-present-but-never-set today).
- **D-06: Strict mismatch parks, never fails.** No Jobs dispatch; Plan parks with `ValidationState=FileTouchMismatch` + a condition naming both tasks and the shared path. Consistent with Phase 12 D-05 park-not-fail. Operator fixes the task specs (add dependsOn or split files); reconcile re-validates and lifts the park.
- **D-07: Enforce + prevent.** The planner prompt also gets a small patch: sibling tasks in a wave must not share files — declare dependsOn or split the work. Enforcement remains the guarantee; the prompt attacks the root cause (LLM emitted overlapping filesTouched without edges).
- **D-08: Webhook stays AND gets real mode resolution.** The admission check remains as the early layer for direct applies, and is upgraded to resolve the actual project file-touch mode instead of passing nil project and falling back to cluster default (plan_webhook.go:174; +3 Gets per validate accepted).

### artifact-get execution (CUTS-04)
- **D-09: Bare inspector Pod + log stream.** CLI creates a short-lived inspector Pod mounting the per-project PVC, streams the container logs to retrieve artifact bytes (no meaningful size cap), and deletes the Pod itself. No Job/TTL indirection. (The dry-run sketch's `ttlSecondsAfterFinished` on a Pod was a Pod/Job conflation — Pods have no TTL controller.)
- **D-10: Raw bytes to stdout, status to stderr.** Pipeable: `tide artifact-get ns/proj/PLAN.md > plan.md`.
- **D-11: Wait for artifact READINESS — never partial reads.** User-stated constraint: "We never want to read a half-written artifact or close before an artifact has been created." If the path doesn't exist yet (or is mid-write) because the authoring session is still running, the CLI waits rather than erroring. Cleanup of the inspector pod happens only after the content is fully streamed. The completeness-detection mechanism (authoring Job status vs write-then-rename atomicity vs file-stability polling) is research/planner discretion, but it must be race-free against in-flight writers.
- **D-12: Plain error on genuinely missing path.** Non-zero exit relaying the failure — applied only AFTER the readiness wait window is exhausted. No directory-listing affordance.

### Cross-plan wave view (CUTS-06)
- **D-13: Right-pane default.** The aggregate all-running-waves view replaces the "Select a plan to view its execution DAG" empty state in the existing two-column layout — no new navigation; selecting a plan still swaps in its ExecutionDAGView.
- **D-14: Rich wave cards.** Per running wave: plan name, wave index, running/total count, and task chips reusing the existing StatusBadge primitive.
- **D-15: Server-side aggregate + SSE.** The manager API derives the running-waves aggregate via label-selector queries over Tasks (per the spec's derived-waves model — waves are never stored, always derived) and delivers it over the existing SSE channel. The client stays thin; no client-side wave re-derivation.
- **D-16: Click-through navigates.** Clicking a wave card selects that plan — right pane swaps to its ExecutionDAGView. The aggregate view doubles as a navigation surface.

### Already-fixed cuts (verify, don't rebuild)
- **CUTS-02:** `8f0b99b` (Phase 11) made the boundary push skip the empty commit on a clean tree, with tests in cmd/tide-push/main_test.go covering the exact run-1 error string. Deliverable: verify the run-1 symptom path is fully covered (including the `tide push`-labeled surface in the success criterion) and close the requirement with the regression evidence; add a test only if a gap is found.
- **CUTS-03:** `abf177c` (Phase 12-01) added the AwaitingApproval early-return whose comment explicitly claims it stops the finding-2 oscillation (phase_controller.go:197-206). Deliverable: verify convergence (no AwaitingApproval↔Running flapping across reconcile loops) and pin it with a regression test if not already covered by 12-01's specs.

### Claude's Discretion
- CUTS-05 chip fix shape: `StatusBadge.tsx` has a 10-value status union with no `Complete`; map Project `Complete` to the appropriate presentation (likely Succeeded-equivalent styling) per the UI-SPEC status vocabulary. Small, mechanical.
- Shared label-stamping helper placement (internal/owner is a candidate — near the existing ownership seam).
- artifact-get timeout flag default and RBAC for inspector-pod creation.
- Regression-test vehicle per cut (envtest vs kind Layer B vs Vitest) — follow the established split.
- Whether `inspect_wave` CLI machinery shares anything with the new running-waves aggregate endpoint.

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### Labels + approve discovery (CUTS-01)
- `internal/reporter/materialize.go` — the create site with zero labels today (MaterializeChildCRDs lifted from dispatch_helpers in plan 09-04).
- `internal/gates/boundary.go` (~:40-60) — documents the systemic label inconsistency and the OwnerRef-walk workaround; revisit its comment after universal stamping.
- `cmd/tide/approve.go` (:196-243, :301-310) — the label-filtered discovery that returns "no level awaiting approval" against unlabeled CRs.
- `internal/controller/plan_controller.go` :1017 / `internal/controller/task_controller.go` :318 — existing `ReasonNoProjectLabel` condition sites (the backfill interacts with these paths).

### File-touch enforcement (CUTS-07)
- `internal/webhook/v1alpha1/plan_webhook.go` — Pitfall B early-return (:139-147), nil-project mode resolution (:174), `computeFileTouchMismatches` (:247) — the reusable core the reconciler gate calls.
- `internal/webhook/v1alpha1/strict_mode.go` — `ResolveFileTouchMode` D-E3 precedence (annotation > resolved-cache > project.Spec > helm default).
- `api/v1alpha1/plan_types.go` :45 — `ValidationState` enum with the dormant `FileTouchMismatch` value D-05 brings alive.
- Planner prompt templates (compiled-in Go templates per the no-vendored-GSD constraint) — D-07's prompt patch lands there.

### artifact-get (CUTS-04)
- `cmd/tide/artifact_get_run.go` + `cmd/tide/artifact_get.go` — the dry-run stub and ref parsing; the dry-run text documents the intended inspector-pod design (busybox, PVC mount at /workspace, per-project claim).
- Memory file `project_envelopes_as_artifacts.md` — size×locality rules; the inspector reads large artifacts from the same-namespace PVC, which is exactly the PVC-for-large-same-ns lane.

### Dashboard (CUTS-05, CUTS-06)
- `dashboard/web/src/components/StatusBadge.tsx` — the 10-value status union missing `Complete`; UI-SPEC §Status Vocabulary is the contract it mirrors.
- `dashboard/web/src/App.tsx` (~:190-228) — the two-column grid and the empty-state right pane D-13 replaces.
- `dashboard/web/src/components/ExecutionDAGView.tsx`, `WaveBackground.tsx`, `TaskNode.tsx` — wave presentation primitives the wave cards draw from.
- `.planning/phases/14-budget-enforcement-pricing/14-UI-SPEC.md` — Phase 14's approved UI contract (ConditionBadge/TideNodeShell patterns the new view should stay consistent with).
- README.md spec §derived-waves — waves are derived via label-selector queries, never stored; D-15's server aggregate must follow this.

### Already-fixed verification (CUTS-02, CUTS-03)
- `cmd/tide-push/main.go` (:372, :486) + `cmd/tide-push/main_test.go` (:996-1045) — the Phase 11 empty-commit fix and its tests.
- `internal/controller/phase_controller.go` (:197-206) — the Phase 12-01 AwaitingApproval early-return that claims to stop the finding-2 oscillation.

### Background
- Memory file `project_dogfood_run1_findings.md` — findings 2, 3, 6, 8, 9b symptoms for regression assertions.
- `.planning/REQUIREMENTS.md` — CUTS-01..07.

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- `computeFileTouchMismatches` (plan_webhook.go) — correct pairwise overlap logic; the reconciler gate reuses it rather than reimplementing (export or relocate to a shared package).
- `ResolveFileTouchMode` precedence walk — usable from the reconciler with a real (non-nil) project.
- Phase 12's dispatch-entry hold pattern (`checkParentApproval` / CheckRejected) + Phase 13 BillingHalt hold + Phase 14 budget gate — the file-touch park is the next instance of the same dispatch-gate shape.
- `StatusBadge`, `ConditionBadge`, `TideNodeShell` (Phase 14) — dashboard primitives for the wave cards and the Complete chip.
- Existing SSE channel + projectSummary plumbing (Phase 14 added whitelisted blockingConditions) — the running-waves aggregate rides the same surfaces.
- `tideproject.k8s/wave-index` label (already stamped on Tasks) — the label-selector key for the server-side wave aggregate.
- `parseArtifactRef` (artifact_get_run.go) — ref parsing stays; only the execution half is new.

### Established Patterns
- Conditions via `Reason*` constants + MergeFrom status patches; NO new `Status.Phase` enum values (Phase 12 D-04) — the file-touch park uses ValidationState + conditions, not a new phase.
- Park-not-fail (Phase 12 D-05): holds park levels; `Failed` is reserved for real failures; `tide resume` family is the recovery verb.
- Chart is a FIXED contract — no chart changes expected this phase; if the webhook mode-resolution fix touches chart-fed defaults, document deliberately.
- CRD `.status` carries no derived aggregates (`make verify-no-aggregates`) — the running-waves view is derived server-side per request/stream, never cached in status.
- Regression tests reproduce the run-1 symptom (milestone-wide rule).

### Integration Points
- Five dispatch sites with existing gates (approval, rejection, billing, budget) — the Plan-level file-touch gate joins that stack; keep the gates composable and ordered deterministically.
- Manager API (chi router as manager.Runnable) — the running-waves aggregate endpoint lands beside the existing dashboard API + SSE handlers.
- Phase 16 (Telemetry) depends on Phase 15 for dashboard surface stability — AppShell/right-pane changes here should leave clean seams for the Telemetry tab (TELEM-02).
- `tide` CLI RBAC — inspector-pod creation needs create/get/delete Pod + pods/log verbs in the operator-facing role.

</code_context>

<specifics>
## Specific Ideas

- CUTS-01 regression: reporter materializes a gated Milestone/Phase → `tide approve <project>` discovers the parked level on the FIRST call (run-1 finding-6 symptom: "no level awaiting approval" despite a parked CR).
- CUTS-07 regression: a Plan whose reporter-created sibling Tasks share a file with no dependsOn edge, `fileTouchMode: strict` → zero task Jobs dispatch, Plan parks with FileTouchMismatch (run 1: two merge-conflict task branches shipped despite strict).
- CUTS-04 acceptance shape: `tide artifact-get <ns>/<proj>/MILESTONE.md` against a live cluster streams the real file bytes to stdout; while the authoring session is still running it waits instead of erroring or returning partial content.
- CUTS-06 reads from label-selector queries per the spec's derived-waves model — the requirement text itself pins the data path.

</specifics>

<deferred>
## Deferred Ideas

- Wave-view naming via the tide metaphor (e.g., "currents") — let ui-phase/planner pick; plain prose wins if nothing fits naturally.
- Artifact browsing/listing UX (`tide artifact-ls` or dashboard artifact browser) — new capability beyond CUTS-04's get-the-stub-working scope.
- OwnerRef fallback discovery in `tide approve` — rejected for v1.0.1 (D-02); labels are the single source of truth.

</deferred>

---

*Phase: 15-Paper Cuts*
*Context gathered: 2026-06-12*
