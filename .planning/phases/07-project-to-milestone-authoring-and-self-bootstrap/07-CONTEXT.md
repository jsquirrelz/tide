# Phase 7: Project-to-Milestone Authoring and Self-Bootstrap - Context

**Gathered:** 2026-05-30
**Status:** Ready for planning

<domain>
## Phase Boundary

Wire the ProjectReconciler so a bare `Project` self-bootstraps the full five-level cascade (Project authors `Milestone` → existing down-stack drives `Phase → Plan → Task`) and reaches `Project status.phase=Complete` at `$0` (stub-driven). This is the **fifth** D-A2 dispatch site — the natural extension of Phase 3's up-stack authoring model one level up. Closes cascade-7, the v1.0 ship blocker.

</domain>

<spec_lock>
## Requirements (locked via SPEC.md)

**6 requirements are locked.** See `07-SPEC.md` for full requirements, boundaries, and acceptance criteria. Downstream agents MUST read `07-SPEC.md` before planning or implementing. Requirements are not duplicated here.

**In scope (from SPEC.md):**
- ProjectReconciler `Initialized → author-Milestone` planner dispatch (mirror `milestone_controller.go:reconcilePlannerDispatch` + `handleJobCompletion`)
- `Create(Milestone)` from `EnvelopeOut` via existing `MaterializeChildCRDs`
- Project `Complete`-detection from child Milestone status
- stub-subagent planner-mode multi-level canned `ChildCRDs` (project/milestone/phase/plan)
- New bare-Project Layer B integration test + fixture
- Smoke Project fixture carries `gates.milestone=auto`

**Out of scope (from SPEC.md):**
- Real Claude-backed authoring (live `acceptance-v1` `$25` path) — unchanged
- Changes to down-stack reconciler *logic* (reused as-is; only the stub now feeds them children)
- A new `project` gate level / change to `gates.Milestone` semantics
- Edits to `hack/scripts/acceptance-v1.sh`
- Multi-Milestone Projects / project-level `dependsOn`
- Push-result envelope schema / real `git push` semantics

</spec_lock>

<decisions>
## Implementation Decisions

### Scope & gate semantics (locked in spec-phase)
- **D-01 — Full `$0` self-bootstrap.** Acceptance bar is a real minimal `Milestone→Phase→Plan→Task` tree driving the Project to `Complete`, not a vacuous top-edge. (User chose this over "vacuous Milestone" and "authoring-only / defer Complete".)
- **D-02 — No new gate at the Project→Milestone step.** The Project authors the Milestone and proceeds; the existing `gates.Milestone` checkpoint stays at the milestone level (after Phases authored), unchanged. (User chose this over double-applying `gates.Milestone`.)

### Fixture & push interaction (resolved by reading `examples/projects/small/project.yaml`)
- **D-03 — No fixture edit needed for gates.** The `$0` smoke Project already sets `gates: {milestone: auto, phase: auto, plan: auto, task: auto}`. SPEC requirement "smoke fixture carries `gates.milestone=auto`" is **already satisfied** — do not duplicate.
- **D-04 — No git push on the `$0` path; keep it that way.** The smoke Project sets only `targetRepo: file:///tmp/no-such-repo` and **no `spec.git` block**. The clone/push Jobs are guarded on `project.Spec.Git != nil && project.Spec.Git.RepoURL != ""` (`project_controller.go:439,458`), so neither fires. `Project=Complete` is therefore clean and terminal at `$0`. **Do NOT add a `spec.git` target to the small fixture** — that would drag the unimplemented push-result path into the gate (out of scope).

### Wiring (mirror the proven milestone pattern — Phase 3 D-A2/D-A4)
- **D-05 — Project is the 5th D-A2 dispatch site.** Add a `reconcileProjectPlannerDispatch` + project-level `handleJobCompletion` that mirror `milestone_controller.go:184-432` one level up. Reuse: `BuildPlannerEnvelope("project", project, ...)`, `podjob.BuildJobSpec(JobKindPlanner)`, `MaterializeChildCRDs` (the `Milestone` Kind is **already** in the allowlist — `dispatch_helpers.go:80,209`), `internal/gates`, `credproxy.Sign`, `PodStatusEnvelopeReader`. Planner Job name `tide-project-<uid>-1`. Acquire `PlannerPool` at the reconciler (D-A4). Patch `Status.Phase=Running` + Condition `AuthoringPlanner=True` on dispatch.
- **D-06 — ProjectReconciler struct + manager wiring must gain the dispatch deps it lacks.** `MilestoneReconciler` uses `r.EnvReader`, `r.SigningKey`, `r.SubagentImage`, `r.CredproxyImage`, `r.HelmProviderDefaults`; `ProjectReconciler` today has only `PlannerPool` + `Dispatcher`. Phase 7 adds the missing fields to the struct AND injects them in `cmd/*/main.go` (manager wiring) — same values already wired for the MilestoneReconciler.
- **D-07 — Project `Complete`-detection.** Add a branch that patches `Status.Phase=Complete` when ≥1 owned Milestone exists and **all** owned Milestones report `Status.Phase=Succeeded`. Use the existing `Owns(&Milestone{})` watch (re-enqueues on child status change) + a `gates.BoundaryDetected(..., "Milestone")`-style check. Coexists with the existing `reconcilePhase3Lifecycle` (branch-name init still runs; clone/push stay no-ops because `spec.git` is unset).
- **D-08 — Stub planner-mode canned tree.** In `cmd/stub-subagent` `success` mode, when `env.Role=="planner"`, switch on `env.Level` and emit exactly one `EnvelopeOut.ChildCRDs` entry: `project→Milestone`, `milestone→Phase`, `phase→Plan`, `plan→Task`; `task`→leaf (no children, today's behavior). Child Specs must be **minimal-but-CRD-valid**: the canned `Task` MUST set `FilesTouched` non-empty (Phase 3 D-F2) and each child carries its required parent `*Ref`. Deterministic child names. Keep payload small (single child per level) so it fits the Pod termination-message envelope channel (`PodStatusEnvelopeReader`).

### Claude's Discretion
- Exact deterministic child names; placeholder Markdown artifact content (only the structured `ChildCRDs` is load-bearing at `$0` — Phase 3 D-A1's "two parallel outputs"; the Markdown surface can be a stub).
- Where `Complete`-detection slots into the Reconcile ordering relative to `reconcilePhase3Lifecycle`.
- Whether the project-level `handleJobCompletion` factors shared logic with milestone's via a small helper or stays a parallel method (lean toward minimal duplication without over-generalizing — Phase 3 deliberately kept four symmetric dispatch sites rather than one generic dispatcher).

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### Locked requirements & scope-of-record
- `.planning/phases/07-project-to-milestone-authoring-and-self-bootstrap/07-SPEC.md` — locked requirements/boundaries/acceptance — MUST read before planning
- `.planning/phases/06-v1-image-publish-and-ship-readiness-revalidation/06-ACCEPTANCE-FINDINGS.md` — cascade-7 evidence + recommendation (scope-of-record)

### The pattern to mirror (proven analog, one level down)
- `internal/controller/milestone_controller.go` §`reconcilePlannerDispatch` (184-337) + §`handleJobCompletion` (339-432) — the exact shape to lift to the Project level
- `internal/controller/dispatch_helpers.go` — `BuildPlannerEnvelope` (149-177), `MaterializeChildCRDs` (179-258, `Milestone` already allowlisted), `ResolveProvider`
- `internal/controller/project_controller.go` — `Reconcile` (271-352, where the new dispatch slots in), `reconcilePhase3Lifecycle` (381+, git lifecycle that coexists), struct def (missing dispatch deps)

### Authoring-cascade design (Phase 3 — this phase extends it upward)
- `.planning/phases/03-up-stack-reconcilers-git-integration-real-subagent-resumptio/03-CONTEXT.md` — **D-A1** (planner emits Markdown + structured `ChildCRDs`; envelope is authoritative), **D-A2** (each up-stack reconciler is the sole dispatch site for its level — Project becomes the 5th), **D-A4** (pool acquisition at the calling reconciler), **D-F1/F2** (`Task.Spec.DependsOn` sibling names; `Task.Spec.FilesTouched` required non-empty), **D-B1..B3** (PlanReconciler derives/materializes Waves from Task `ChildCRDs` — stub emits Tasks, not Waves)
- `README.md` §"five-level hierarchy" + §"a human applies a Project; TIDE authors MILESTONE.md by dispatching a planner" — the Core Value this phase proves

### Stub & fixtures
- `cmd/stub-subagent/main.go` §`dispatchSuccess` (204-244) — where planner-mode `ChildCRDs` emission lands; `pkg/dispatch` `EnvelopeIn.{Role,Level}` + `EnvelopeOut.ChildCRDs` + `ChildCRDSpec{Kind,Name,Spec}`
- `examples/projects/small/project.yaml` — the `$0` smoke fixture (gates already auto; no `spec.git`; `absoluteCapCents: 0`)
- `test/integration/kind/testdata/up-stack-project.yaml` (+ comment lines 13-17 documenting the never-implemented round-trip) and `test/integration/kind/suite_test.go` (ensureProjectsPVC / SubagentSA / SigningKeySecret / pvcPrewarmPod helpers, RWO override) — the Layer B harness the bare-Project test extends

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- `MaterializeChildCRDs` — already creates a `Milestone` from a `ChildCRDSpec{Kind:"Milestone"}` with Project owner-ref; idempotent. Project path calls it unchanged.
- `BuildPlannerEnvelope(level, parent, project, ...)` — level-parameterized; pass `"project"`.
- `podjob.BuildJobSpec(JobKindPlanner)` — full planner Job (PVC subPath, envelope-writer init container, credproxy sidecar, signed token, 600s planner Caps floor, 20-iter default). Reuse verbatim.
- `internal/gates` — `EvaluatePolicy`, `CheckApprove/Rejected`, `BoundaryDetected` (parameterize childKind="Milestone" for Complete-detection).
- `credproxy.Sign(SigningKey, uid, ttl)` — token minting (Project must gain `SigningKey`).

### Established Patterns
- **D-A2 dispatch-site symmetry** — each up-stack reconciler is the sole Job creator for its level. Add the Project site without generalizing into one dispatcher.
- **D-A4 pool acquisition at the reconciler** — `PlannerPool.Acquire/Release` around Job creation.
- **PodStatusEnvelopeReader** (Phase 02.2 cascade-10) — manager reads `EnvelopeOut` from the subagent container's termination message, not a cross-namespace PVC. Keeps the canned tree small.
- **Idempotency guards** — milestone uses `Status.Phase` short-circuits (terminal, AwaitingApproval, Running) to avoid re-dispatch; Project's `handleInitJobCompletion` already shows the cascade-13 idempotency pattern. Mirror it for the new Running/Complete transitions.

### Integration Points
- `ProjectReconciler` struct (`internal/controller/project_controller.go`) — **add** `EnvReader`, `SigningKey`, `SubagentImage`, `CredproxyImage`, `HelmProviderDefaults`.
- `cmd/*/main.go` manager wiring — inject the new ProjectReconciler fields (values already computed for MilestoneReconciler).
- `cmd/stub-subagent/main.go` `dispatchSuccess` — branch on `env.Role=="planner"` + `env.Level`.
- `examples/projects/small/project.yaml` — no change required (gates already auto, no push target).
- `test/integration/kind/` — new `bare-project.yaml` fixture (Project only) + new spec asserting Milestone→Phase→Plan→Task materialization and Project=Complete; reuse existing per-namespace setup helpers.

### Research / risk directives (for gsd-phase-researcher + gsd-planner)
- **Map the down-stack "Succeeded" model before planning.** In `milestone_controller.go:handleJobCompletion`, a level patches `Succeeded` when **its own planner Job** completes (children materialized but not necessarily completed). Confirm what `Project=Complete` actually requires end-to-end (does the leaf `Task` executor have to run, or does each level Succeed on author?). **This is where cascades 8/9 would surface** (CLAUDE.md warns of piecemeal down-stack cascades) — verify the Phase/Plan/Task reconcilers actually drive a 1-1-1-1 tree to all-Succeeded with the stub feeding children, against the live reconcilers, not assumptions (Observe First).
- Confirm the canned `Task` child spec satisfies CRD admission (CEL validation): `FilesTouched` non-empty (D-F2), valid `planRef`/parent refs, `dependsOn` empty (single-task wave).
- Confirm `PlanReconciler` derives the `Wave` from the emitted `Task` (stub should NOT emit a `Wave` — waves are derived, per CLAUDE.md "Waves are derived, not declared").

</code_context>

<specifics>
## Specific Ideas

- The proof bar is TIDE-on-TIDE: a bare `Project` → `Complete` at `$0` with no API key. The run log should show the `Milestone→Phase→Plan→Task` tree materialized. This is the v1.0 acceptance bar — treat it as load-bearing.
- The leaf `Task` should be real enough to exercise the executor Job path at `$0` (stub `testMode=success`), so the proof covers dispatch end-to-end, not just CRD creation — pending the research finding on whether Complete strictly requires it.

</specifics>

<deferred>
## Deferred Ideas

- **Multi-Milestone Projects** + project-level `dependsOn` ordering — single-Milestone bootstrap proves the edge; multi-milestone authoring is a future phase.
- **Real `git push` at level boundaries** on the `$0` path — requires the push-result envelope schema (Phase 3 follow-up); the small fixture deliberately has no push target.
- **Real Claude-backed authoring** (`acceptance-v1` `$25` path) — the live planner emitting genuine `MILESTONE.md` + `ChildCRDs`; out of scope for the `$0` proof.

None of these block the v1.0 ship gate.

</deferred>

---

*Phase: 07-project-to-milestone-authoring-and-self-bootstrap*
*Context gathered: 2026-05-30*
