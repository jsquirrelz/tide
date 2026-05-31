---
slug: planner-envelope-roundtrip
status: resolved
trigger: "Phase 7 cascade-8 — bare Project never authors its Milestone; bare_project_test.go Layer B cascade fails at first assertion (no Milestone owned by bare-project). The planner-level envelope round-trip never materializes child CRDs."
created: 2026-05-31
updated: 2026-05-31
phase: 07-project-to-milestone-authoring-and-self-bootstrap
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
