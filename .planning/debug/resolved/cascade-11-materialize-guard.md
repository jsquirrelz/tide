---
slug: cascade-11-materialize-guard
status: resolved
trigger: "Phase 7 Layer B chaos_resume_test.go:233 Pillar 4 fails: 'exactly 3 Jobs must reach status.succeeded=1 post-release — got 4'. A spurious stub-phase-1→stub-plan-1→stub-task-1 subtree is authored in chaos-resume-test, producing a 4th executor Job. The cascade-10 spec-ref idempotency guard sits ONLY in the fresh-dispatch path; the handleJobCompletion→MaterializeChildCRDs path is unguarded."
created: 2026-05-31
updated: 2026-05-31
phase: 07-project-to-milestone-authoring-and-self-bootstrap
---

# Debug: cascade-11-materialize-guard

## Symptoms

- **Expected:** Full `make test-int` = Layer A 29/29 + Layer B 14/14. The chaos_resume spec (`test/integration/kind/chaos_resume_test.go`) pre-applies a full hierarchy (Project→Milestone→Phase→Plan→3 Tasks via `testdata/chaos-resume-three-task.yaml`), kills the controller mid-wave, releases β+γ, and asserts (Pillar 4, `chaos_resume_test.go:233`) that **exactly 3** Jobs reach `status.succeeded=1` (the 3 pre-applied tasks alpha/beta/gamma).
- **Actual:** `Eventually` times out after 120s — **4** Jobs succeeded, not 3. A spurious `stub-task-1` executor Job ran. `make test-int` = Layer A 29/29 PASS, **Layer B 13/14** (`Ran 14 of 14 ... 13 Passed | 1 Failed`), MAKE_EXIT=2. Run log: `/tmp/phase7-clean-run.log` (failure block ~line 1943-1968).
- **Error:** `Pillar 4: exactly 3 Jobs must reach status.succeeded=1 post-release / Expected <int>: 4 to equal <int>: 3` at `chaos_resume_test.go:233`.
- **Evidence (kind logs export `/tmp/claude-501/kind-logs-tide-test`):** The surviving (post-kill) controller pod log shows `stub-phase-1`, `stub-plan-1` (+ `created wave` for it), and `stub-task-1` already present at restart (20:38:07Z) in namespace `chaos-resume-test` — i.e. the ORIGINAL controller (killed pod, log not exported) authored the spurious subtree PRE-kill; the new pod inherited it and ran `stub-task-1` → 4th Job.
- **Timeline:** First observed on Layer B 2026-05-31 (Phase 7 env-gated verification). This is the recurring cascade-9/10 symptom (spurious 4th Job), but cascade-10's spec-ref guard was a PREDICTED fix never run on Layer B; it is insufficient.
- **Reproduction:** `KEEP_KIND_CLUSTER=true make test-int` (full), or focused: `KEEP_KIND_CLUSTER=true go test ./test/integration/kind/... -timeout=20m -ginkgo.v -ginkgo.focus="D-D4 four pillars" ` against the kept warm `tide-test` cluster.

## Current Focus

hypothesis: The cascade-10 idempotency guard (skip authoring when a child of the expected kind already exists by parent-specRef) lives ONLY in the fresh-dispatch path (`milestone_controller.go:reconcilePlannerDispatch` Step 2b, lines ~239-250; symmetric phase guard at `phase_controller.go:206`). It does NOT cover the materialization path `handleJobCompletion → MaterializeChildCRDs`. The pre-applied Milestone reconciled and dispatched its planner Job on its FIRST reconcile — before the sibling `chaos-resume-phase` was visible (multi-doc `kubectl apply -f` race) — so the dispatch-time guard saw 0 child Phases and let it dispatch. Milestone went `Running`; once Running, the planner Job's completion runs `handleJobCompletion` (a DIFFERENT reconcile branch, milestone_controller.go Step 2, line ~214-225) which calls `MaterializeChildCRDs` UNCONDITIONALLY → creates `stub-phase-1`. The stub subtree then cascades down (stub-plan-1 → stub-task-1 → 4th executor Job). Guarding *dispatch* is fundamentally racy against not-yet-applied siblings; the guard must move to the race-free *materialization* point.
next_action: Fix applied. Rebuild/reload images into kept tide-test cluster, rollout-restart controller, confirm pod rotation, then run focused chaos-resume + focused bare-Project + full regression sweep.

reasoning_checkpoint:
  hypothesis: "The handleJobCompletion→MaterializeChildCRDs path is unguarded; a pre-applied Milestone that dispatched its planner Job before its sibling Phase was visible materializes a spurious stub-phase subtree on Job completion, yielding a 4th executor Job."
  confirming_evidence:
    - "cascade-10 spec-ref guard lives only in reconcilePlannerDispatch Step 2b (milestone_controller.go:239-250); the materialization call sites (milestone:404, phase:329, plan:389, project:806) had no guard."
    - "Surviving controller log shows stub-phase-1/stub-plan-1/stub-task-1 present at restart in chaos-resume-test ns — authored pre-kill via materialization path."
    - "All parent-refs (projectRef/milestoneRef/phaseRef/planRef) are present at kubectl-apply time per testdata/chaos-resume-three-task.yaml, so a specRef-keyed guard is race-free."
  falsification_test: "If the focused chaos-resume spec still produces 4 succeeded Jobs after the guard, the spurious authoring originates elsewhere (not the materialization path)."
  fix_rationale: "Shared childrenAlreadyMaterialized() helper lists children of the expected kind by parent-specRef (+IsControlledBy fallback) and skips MaterializeChildCRDs when one exists — moving the idempotency guard to the race-free materialization point. Genuine bootstrap is safe: 0 children at first materialize → guard does not fire."
  blind_spots: "Helper fails open on List error / unknown parent type (proceeds to materialize) to protect bare-Project; an unexpected List error during chaos-resume could theoretically let a spurious child through, but List errors against a healthy kept cluster are not expected."

## Evidence

- timestamp: 2026-05-31 — `testdata/chaos-resume-three-task.yaml` pre-applies Project(chaos-resume-project)→Milestone(chaos-resume-milestone, projectRef set)→Phase(chaos-resume-phase, milestoneRef set)→Plan(chaos-resume-plan, phaseRef set)→3 Tasks(planRef set). All parent-refs present at apply time, so a materialization-point guard keyed on specRef is race-free.
- timestamp: 2026-05-31 — Surviving controller log (xmw7t, post-kill) shows stub-phase-1/stub-plan-1/stub-task-1 already present at 20:38:07Z + `created wave` for stub-plan-1 → confirms the spurious subtree existed before the new pod started; created by the killed pod (fvnvx, log not exported).
- timestamp: 2026-05-31 — 4 MaterializeChildCRDs call sites: milestone_controller.go:404, phase_controller.go:329, plan_controller.go:389, project_controller.go:806. Existing dispatch-path guards: milestone_controller.go:245, phase_controller.go:206; plan uses taskPlanRefIndexKey field index; project uses an owned-Milestone list guard (728b60a).

## Eliminated

- hypothesis: "cascade-10 spec-ref guard fixes chaos-resume" — ELIMINATED: cascade-10 was never run on Layer B (env-deferred); the guard only covers the dispatch path, and the milestone dispatched before its sibling phase was visible, so the spurious subtree was still authored via the unguarded materialization path.
- hypothesis: "4th Job is a test-fixture artifact, not a prod bug" — ELIMINATED: re-applying a pre-authored hierarchy on resume is a real TIDE scenario (resumption = re-derive from artifacts); spurious authoring on resume is a genuine correctness defect that PERSIST-04/chaos_resume exists to catch.

## Resolution

root_cause: The cascade-10 spec-ref idempotency guard covered ONLY the fresh-dispatch path (`reconcilePlannerDispatch` Step 2b). The `handleJobCompletion → MaterializeChildCRDs` path was UNGUARDED. In chaos-resume, the pre-applied Milestone reconciled and dispatched its planner Job on its FIRST reconcile — before the sibling `chaos-resume-phase` was visible (multi-doc `kubectl apply -f` race) — so the dispatch-time guard saw 0 child Phases and let it dispatch. The Milestone went Running; on planner-Job completion, `handleJobCompletion` called `MaterializeChildCRDs` UNCONDITIONALLY → authored `stub-phase-1`, which cascaded down (stub-plan-1 → stub-task-1 → a spurious 4th executor Job), failing chaos_resume_test.go:233 Pillar 4 ("exactly 3 Jobs … got 4"). Guarding *dispatch* is fundamentally racy against not-yet-applied siblings; the guard had to move to the race-free *materialization* point.

fix: Added a shared `childrenAlreadyMaterialized(ctx, c, parent)` helper in `internal/controller/dispatch_helpers.go` that lists children of the parent's expected child kind in the namespace and reports skip=true when ≥1 already matches the parent-specRef (Milestone.spec.projectRef / Phase.spec.milestoneRef / Plan.spec.phaseRef / Task.spec.planRef), with `metav1.IsControlledBy(child, parent)` as a belt-and-suspenders fallback (same pattern as the dispatch guards at milestone_controller.go:245 / phase_controller.go:206). Wired the guard into all four `MaterializeChildCRDs` call sites (milestone, phase, plan, project): when a child already exists, SKIP materialization but CONTINUE to the level's normal gate/boundary-push/ValidationState/complete handling — no early-return that skips the level's own completion transition. The helper fails open on List error / unknown parent type to protect bare-Project bootstrap. Genuine self-bootstrap is unaffected: at the first materialize the parent has 0 existing children → guard does not fire → genuine first child materialized once; a later reconcile finds it by specRef → idempotent skip.

verification:
  - Focused chaos-resume (`-ginkgo.focus="D-D4 four pillars"`): PASS — "all 5 pillars (D-D4 + algorithmic invariant) verified across leader-handoff"; `Ran 1 of 14 Specs … SUCCESS! -- 1 Passed | 0 Failed`. Pillar 4 (exactly 3 succeeded Jobs) holds — no spurious 4th Job. Verified against fresh binary digest sha256:ac45c344… (pod rotated; old xmw7t → new qz6bc/xcdpw).
  - Focused bare-Project (`-ginkgo.focus="bare Project self-bootstraps"`): PASS — full cascade Project→Milestone→Phase→Plan→Task→Wave→Project=Complete; each level's first child materialized exactly once; `Ran 1 of 14 Specs … SUCCESS! -- 1 Passed | 0 Failed`. v1.0 ship bar intact.
  - Full regression (`make test-int`): Layer A envtest `Ran 29 of 29 Specs … SUCCESS! -- 29 Passed | 0 Failed | 0 Skipped`; Layer B kind `Ran 14 of 14 Specs in 761.643 seconds … SUCCESS! -- 14 Passed | 0 Failed | 0 Skipped`.
  - The single non-Ginkgo failure `TestHelmDeploymentTemplateRendersManagerPodAnnotations` (projects_pvc_test.go:149) is a pre-existing chart-template unit-test failure unrelated to this fix — confirmed it FAILS identically on the unmodified tree (git stash run). No chart/test files were touched.

files_changed: internal/controller/dispatch_helpers.go, internal/controller/milestone_controller.go, internal/controller/phase_controller.go, internal/controller/plan_controller.go, internal/controller/project_controller.go

---
**Closed at v1.0.0 milestone completion (2026-06-11).** The defect class this
session tracked was fixed and validated before ship: full `make test-int`
green (Layer A 36/36 + Layer B), nightly-integration green, live medium DoD
on minikube (Project=Complete, BoundaryPushed=True), and the v1.0.0-rc dry-run
gate green end-to-end.
