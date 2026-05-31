---
slug: planner-envelope-roundtrip
status: investigating
trigger: "Phase 7 cascade-8 — bare Project never authors its Milestone; bare_project_test.go Layer B cascade fails at first assertion (no Milestone owned by bare-project). The planner-level envelope round-trip never materializes child CRDs."
created: 2026-05-31
updated: 2026-05-31
phase: 07-project-to-milestone-authoring-and-self-bootstrap
---

# Debug: planner-envelope-roundtrip (Phase 7 cascade-8 + cascade-9)

## REOPENED for cascade-9 (2026-05-31) — symmetric idempotency guards

cascade-8 is fixed and the bare-Project cascade PASSES (full tree → Project=Complete). But the full `make test-int` regression sweep (post-fix, warm cluster) is **13/14 Layer B** with **Layer A 29/29** and the bare-Project spec PASSING. The one failure is **chaos_resume_test.go:233** Pillar 4: "exactly 3 executor Jobs must reach status.succeeded=1 post-release — got 4."

**cascade-9 root cause (confirmed via manager log + fixture read):** The cascade-8 dual-label fix activated the planner envelope round-trip GLOBALLY (all levels). The cascade-8 session added an idempotency guard ONLY at the project level (`reconcileProjectPlannerDispatch`, commit 728b60a). The **milestone/phase/plan reconcilers author children UNCONDITIONALLY** — no idempotency guard. So `chaos-resume-milestone` (which already owns a pre-applied `chaos-resume-phase`) ALSO authors a spurious `stub-phase-1` → `stub-plan-1` → `stub-task-1` → a 4th executor Job. Manager log proof: both `chaos-resume-phase` AND `stub-phase-1` (+ `stub-plan-1`) reconcile in namespace `chaos-resume-test`. The chaos-resume fixture pre-applies the FULL hierarchy (Project→Milestone→Phase→Plan→3 Tasks), so the spurious stub subtree is purely the milestone re-authoring.

**APPROVED FIX (user-confirmed): symmetric idempotency guards.** Extend the project-level guard pattern (728b60a) to the **milestone, phase, and plan** authoring paths: skip planner dispatch / child materialization when the level already owns ≥1 child of the expected kind (Milestone→Phase, Phase→Plan, Plan→Task). Use `gates.BoundaryDetected`-style owned-children detection or a direct owned-child List + `metav1.IsControlledBy` filter (same as the project guard). 
- chaos-resume-milestone owns chaos-resume-phase → guard SKIPS authoring → no stub subtree → 3 executor Jobs → chaos-resume PASSES. No test/fixture edits needed.
- bare-Project flow UNAFFECTED: each level starts with 0 owned children, so the guard never blocks the genuine self-bootstrap.

**Verification bar:** rebuild controller:test (+ stub if touched), `kind load` into the warm `tide-test` cluster, re-run the FULL `make test-int` (KEEP_KIND_CLUSTER=true) — expect Layer A 29/29 + Layer B 14/14 (bare-Project still PASSES, chaos-resume now PASSES). Watch for any cascade-10. Then the orchestrator runs `make acceptance-v1-smoke` (REQ-6).

**Scope guard:** This expands down-stack edits to milestone_controller.go / phase_controller.go / plan_controller.go authoring paths — the user explicitly approved this expansion (it is the symmetric completion of the project-level guard). Do NOT touch charts/ or hack/scripts/acceptance-v1.sh. If a cascade-10 of a NEW class appears, checkpoint and surface.

## cascade-9 follow-up: the guard is RACY → cascade-10 (2026-05-31, SURFACED to user)

The cascade-9 idempotency guards (commit, milestone+phase) use `metav1.IsControlledBy` (ownerRef). But a **pre-applied** child Phase (chaos-resume-phase, declared with `spec.milestoneRef`) gets its ownerRef set **asynchronously** by the PhaseReconciler. At the milestone's FIRST reconcile, chaos-resume-phase is not yet owner-ref'd → the guard sees 0 owned Phases → the milestone authors a spurious `stub-phase-1`→`stub-plan-1`→stub-task → 4th executor Job. Manager-log proof (fresh-cluster run): `stub-phase-1` + `stub-plan-1` reconcile alongside `chaos-resume-phase` in `chaos-resume-test`. So cascade-9's guard does NOT fix chaos-resume; chaos still fails Pillar 4 (`:233` "exactly 3, got 4") when the cluster is healthy enough to reach that assertion.

**cascade-10 fix (NOT yet applied — surfaced for decision):** make the guard race-free by counting children via the **spec parent-ref** (Phase.spec.milestoneRef == ms.Name, Plan.spec.phaseRef == ph.Name) — set at apply time, before ownerRef — OR by both ownerRef AND specRef. Apply symmetrically at milestone/phase (and verify plan). Then re-verify chaos-resume + three-task on a NON-resource-constrained cluster.

**Environmental compounding factor:** the test host's Docker VM has only **7.65 GiB**. The cascade-8 activation makes the full Layer B suite materially heavier (active authoring cascades on every milestone-bearing fixture + the bare-Project 5-level cascade). The single-node kind cluster is overwhelmed mid-run → controller-manager goes unready → credproxy HARN-03 + chaos-resume fail and wave_test specs skip ("controller not ready") + the go-test 20m budget (KIND_GO_TEST_TIMEOUT) is exceeded (only ~10/14 specs run). Multiple full `make test-int` runs over a ~3h session also OOM-killed (exit 137) the kept cluster. A clean, adequately-resourced environment is needed for a reliable full-suite signal.

## What IS proven (robust across 4+ runs)

- **Layer A: 29/29** (clean in isolation and on fresh clusters; the budget/indegree failures seen only under full-suite CPU contention are flakes).
- **bare-Project cascade PASSES**: bare Project → stub-milestone-1 (Succeeded) → stub-phase-1 → stub-plan-1 (ValidationState=Validated) → stub-task (Succeeded) → Wave → **Project=Complete**. This is cascade-7's closure / the v1.0 TIDE-on-TIDE self-bootstrap proof. Verified in focused runs AND full-suite runs.
- cascade-8 fixed (planner envelope round-trip: dual-label + plan Job-watch + provider-secret + project idempotency guard) — commits 728b60a, 3ea86e5.
- cascade-9 boundary-push placement corrected (guards gate only fresh dispatch) — commit fix(07-09); Layer A boundary-push green. (But the guard is racy for pre-applied children → cascade-10.)

## Outstanding (for the decision)

- cascade-10: race-free idempotency guard (spec-ref based) so existing pre-applied-Milestone fixtures (chaos-resume, three-task) don't author spurious subtrees.
- A clean full `make test-int` (14/14) on an adequately-resourced cluster.
- `make acceptance-v1-smoke` ($0 BOOT-04 ship gate, REQ-6) → Project=Complete — not yet run.

---

# Debug: planner-envelope-roundtrip (Phase 7 cascade-8)

## Symptoms

- **Expected:** A bare `Project` (no pre-applied Milestone) self-bootstraps the full cascade: ProjectReconciler dispatches a project-level planner Job → stub emits `EnvelopeOut.ChildCRDs` containing a Milestone → `MaterializeChildCRDs` creates the Milestone CR → cascade continues Milestone→Phase→Plan→Task → all reach `Succeeded` → `Project.Status.Phase=Complete`.
- **Actual:** No Milestone is ever created. `bare_project_test.go:123` times out after 180s: `no Milestone owned by bare-project found yet (total in ns: 0)`. Same root failure for EVERY Project (chaos-resume, push-lease, wave-test), not just bare-project.
- **Error (manager log, tide-system/tide-controller-manager):**
  `"project planner envelope read failed; proceeding without ChildCRDs" ... error: read envelope out "/workspaces/<uid>/workspace/envelopes/<uid>/out.json": no such file or directory`
  at `handleProjectJobCompletion (project_controller.go:778)` ← `reconcileProjectPlannerDispatch (666)` ← `reconcilePhase3Lifecycle (402)`.
- **Timeline:** Surfaced 2026-05-31 by the new Phase 7 `bare_project_test.go` — the FIRST test to depend on the planner envelope round-trip. Never worked before because no test exercised it (`up-stack-project.yaml` comment lines 13-17 explicitly defers it as "separate follow-up work").
- **Reproduction:** `KEEP_KIND_CLUSTER=true go test ./test/integration/kind/... -timeout=20m -ginkgo.v -ginkgo.focus="bare Project self-bootstraps"` against the warm `tide-test` kind cluster (KEPT, Phase 7 images loaded).

## Eliminated

- hypothesis: "ProjectReconciler not wired / SigningKey missing" — ELIMINATED: the project planner Job IS dispatched (log shows the dispatch + the subsequent envelope-read error), so the 5 fields + SigningKey are live in the deployed manager.
- hypothesis: "checkProjectComplete prematurely completes / BoundaryDetected vacuous-true on zero children" — ELIMINATED: `BoundaryDetected` returns `matched > 0` (false for zero children); verified in `internal/gates/boundary.go`.
- hypothesis: "test-harness namespace/PVC bug" — ELIMINATED: fixed separately (commit 2b02633 — bare_project_test BeforeEach now uses createNamespace); the spec body now runs and reaches the Milestone assertion.
- hypothesis: "budget/indegree Layer A failures are regressions" — ELIMINATED: Layer A is 29/29 in isolation (`make test-int-fast`); those are CPU-contention Eventually flakes under full `make test-int`.
- hypothesis: "backend.go RBAC issue" — ELIMINATED: `kubectl describe clusterrole tide-manager-role` shows `pods: [get list watch]` cluster-wide.
- hypothesis: "pod TTL causing cleanup before envelope read" — ELIMINATED: TTL=600s, envelope read happens within seconds of Job completion.

## Resolution

- root_cause: Four compounding bugs:
  1. (Primary) `jobspec.go`: planner pods labeled `tideproject.k8s/<level>-uid` only, but `PodStatusEnvelopeReader.ReadOut` queries `tideproject.k8s/task-uid` → 0 pods found → filesystem fallback fails → `no such file or directory` → `EnvelopeOut.ChildCRDs=[]` → no Milestone created. Affected ALL planner levels (project/milestone/phase/plan).
  2. (Primary) `project_controller.go`: `reconcileProjectPlannerDispatch` had no idempotency guard — Projects that already had owned Milestones (push-lease/chaos-resume/wave-test) would author spurious stub-milestones once the label fix made the round-trip succeed.
  3. (Follow-on, found during verification) `plan_controller.go`: missing `Owns(&batchv1.Job{})` in `SetupWithManager`. When the plan planner job completed, no watch event re-enqueued the plan reconciler → plan stuck in `Running` forever (never called `handlePlannerJobCompletion`, never stamped `ValidationState=Validated`, never created Task children).
  4. (Follow-on, found during verification) `bare-project.yaml`: test fixture had no `providerSecretRef` and no provider Secret → credproxy init container crashed on startup (`requireEnv("ANTHROPIC_API_KEY")` exits 1) → planner pod `CrashLoopBackOff` → job never completed.
- fix:
  1. `jobspec.go`: planner pods now carry BOTH `tideproject.k8s/<level>-uid` AND `tideproject.k8s/task-uid` (= parentUID) so the shared reader finds them.
  2. `project_controller.go`: added idempotency guard (list owned Milestones, return early if any exist).
  3. `plan_controller.go`: added `Owns(&batchv1.Job{})` to `SetupWithManager`.
  4. `bare-project.yaml`: added `tide-provider-secret` Secret + `providerSecretRef` to the Project fixture.
  5. `jobspec_test.go`: updated assertion from "task-uid ABSENT" to "task-uid = parentUID".
- verification: PASSED. `bare Project self-bootstraps full cascade to Project=Complete (REQ-1..5 + REQ-7a/b)` — 1 Passed | 0 Failed in 64.888s spec body. Full tree materialized: Project→Milestone→Phase→Plan→Task→Wave, all Succeeded, Project=Complete. Commits: 728b60a, 3ea86e5.
- files_changed: internal/dispatch/podjob/jobspec.go, internal/dispatch/podjob/jobspec_test.go, internal/controller/project_controller.go, internal/controller/plan_controller.go, test/integration/kind/testdata/bare-project.yaml, cmd/manager/main.go (whitespace)
