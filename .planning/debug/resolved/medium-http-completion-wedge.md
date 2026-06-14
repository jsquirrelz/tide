---
slug: medium-http-completion-wedge
status: resolved
trigger: "nightly Layer B: medium HTTP stub Project never reaches Complete — wedges before dispatching any subagent Job"
created: 2026-06-14
updated: 2026-06-14
---

# Debug Session: medium-http-completion-wedge

## Symptoms

- **Expected:** `test/integration/kind/medium_http_test.go:360` "medium Project with stub-subagent reaches Complete over http://" — the stub-subagent project clones from git-http-server, returns canned envelopes ($0), pushes back, and reaches `Status.Phase=Complete` within 10 min.
- **Actual:** Project is created (admitted) but never reaches Complete. The `Eventually(..., 10*time.Minute)` at `medium_http_test.go:433` times out; the Ginkgo retry then blows the 20-min `go test` budget → `panic: test timed out after 20m0s`. nightly run 27503518297 (after the fixture-image fix `d45909b`).
- **Runtime evidence (kind-logs artifact, run 27503518297):**
  - `kubectl get pods -n medium-http-test` shows ONLY the fixtures: `demo-remote-init` (Completed), `git-http-server` (1/1 Running), `tide-projects-prewarm`. **No tide-owned Job pods** (no init/clone/planner/reporter/executor) were ever created for `medium-http-project-*`.
  - The tide controller-manager log (single container, no restart, INFO level) has exactly ONE line for the project: `"project cleanup"` at the post-timeout teardown. Dispatch decisions are DEBUG (suppressed), so the wedge reason is not in the log.
  - The project's actual stuck `Status.Phase`/condition did not print (the 20-min panic swallowed the Eventually failure message).
- **Timeline:** Last GREEN nightly was 2026-06-10; first RED 2026-06-11 (then masked by the ImagePullBackOff bug #1 until that was fixed today). So this is almost certainly a **v1.0.1-era regression** (Phases 12–17) in the early Project→dispatch path — it ran green on Jun 10.

## Refuted hypotheses

- **Budget zero-cap wedge:** the test sets `Budget.AbsoluteCapCents: 0`. REFUTED — `internal/budget/cap.go:49` gates the cap check on `AbsoluteCapCents > 0`, and `reservation.go:124` treats `AbsoluteCapCents <= 0 && RollingWindowCapCents <= 0` as "no cap configured." Cap 0 = unlimited (backward-compatible, cap_test.go:75). Budget is not blocking.

## Leading hypotheses (verify, don't assume)

1. **Phase 13 image-resolution regression at the init/clone dispatch site.** Phase 13 (DISPATCH-01/02) rewired image resolution at "all six dispatch sites." The test project sets `Subagent.Model: "stub"` with NO explicit subagent image. If the early init/clone Job's image now fails to resolve (or resolves to something uncreatable) the Job may never be created → wedge before dispatch. Check the Project reconciler's init/clone Job creation path and `resolveImage` for the stub/no-image case.
2. **Phase 12 gate-at-descent / dispatch-hold regression.** Even with all gates `auto`, Phase 12 added "children materialize but dispatch holds until approval" + approve-at-descent routing. If `auto` gates are mishandled in the new descent-hold code, the project could park at AwaitingApproval and never dispatch. Check the auto-gate path through the new descent logic.
3. **Project never leaves an early phase** (Pending/Initialized) — the init/clone Job isn't created at all. Determine which.

## Reproduction recipe (kind required — Docker + kind per CLAUDE.md constrained-VM notes)

Run ONLY this spec to keep it cheap:
```
make test-int-kind-prep   # builds+loads stub-subagent/credproxy/manager etc., creates kind cluster, helm installs TIDE
# build+load the two http fixtures (private ghcr packages):
docker build -t ghcr.io/jsquirrelz/tide-demo-init:1.0.0 -f images/tide-demo-init/Dockerfile .
docker build -t ghcr.io/jsquirrelz/tide-git-http-server:1.0.0 -f images/tide-git-http-server/Dockerfile .
kind load docker-image ghcr.io/jsquirrelz/tide-demo-init:1.0.0 --name <cluster>
kind load docker-image ghcr.io/jsquirrelz/tide-git-http-server:1.0.0 --name <cluster>
# focus just the wedging spec:
cd test/integration/kind && go run github.com/onsi/ginkgo/v2/ginkgo --focus="medium Project with stub-subagent reaches Complete" .
```
While it runs, watch the live truth the CI log lacked:
```
kubectl get project,milestone,phase,plan,task,jobs -n medium-http-test -o wide --watch
kubectl get project medium-http-project-* -n medium-http-test -o yaml   # Status.Phase + .Status.Conditions = the wedge reason
kubectl logs -n tide-system deploy/tide-controller-manager -f         # DEBUG dispatch decisions
```
First thing to capture: the project's `Status.Phase` and last condition message — that names the wedge. Then bisect against the last-green commit (Jun 10) for the medium_http path if the phase doesn't immediately implicate Phase 12/13/14 code.

## Constraints

- Fixture-image bug #1 (ImagePullBackOff) is ALREADY fixed (commit d45909b) — do not re-litigate it; this is the next layer.
- The fix must keep the stub flow $0 (no real LLM calls).
- If the root cause is a product regression (not a test-only issue), fix the product; if the test's project spec is genuinely invalid under v1.0.1 semantics, fix the test — but justify which, with evidence (a real operator with the same spec shape must still work).

## Evidence

- timestamp: 2026-06-14 — nightly run 27503518297, medium_http_test.go:433 Eventually(10m) timeout → 20m suite panic. Namespace pod dump: zero tide Job pods for medium-http-project. Controller log: only "project cleanup".
- timestamp: 2026-06-14 — budget zero-cap refuted via cap.go:49 / reservation.go:124.
- timestamp: 2026-06-14 (CODE ANALYSIS) — **The "no tide Job pods" evidence is collected POST-teardown.** nightly-integration.yml:152 `kind export logs` runs in the workflow's post-failure step, AFTER the 20m panic AND after medium_http_test.go:415 `defer k8sClient.Delete(ctx, proj)`. Deleting the Project triggers owner-ref GC of every tide Job it owns (init/clone/planner are all owner-ref'd to the Project via owner.EnsureOwnerRef). So "zero tide Job pods" is consistent with Jobs having dispatched and been GC'd. It does NOT prove a pre-dispatch wedge. → REFRAMES the "wedge before dispatch" assumption.
- timestamp: 2026-06-14 (CODE ANALYSIS) — **Hypothesis 1 (Phase 13 image resolution) substantially weakened.** (a) init Job uses hardcoded busybox:1.36 (project_controller.go:1291), no resolveImage. (b) clone Job uses r.TidePushImage (CloneOptions), no resolveImage. (c) only the project-level PLANNER Job uses resolveImage (project_controller.go:1017). For Model="stub"+no image, resolveImage falls through to helmDefaults.Image. The kind harness sets `--set subagent.defaults.image=ghcr.io/jsquirrelz/tide-stub-subagent:test` (suite_test.go:489), chart maps it subagent.defaults.image → CLAUDE_SUBAGENT_IMAGE env → helmProviderDefaults.Image (deployment.yaml:63-64). So the planner resolves to the stub image. The Phase-13 `--subagent-image` compatibility shim (main.go:233) is moot because the chart dropped that flag, but the defaults.image path replaces it cleanly. Image resolution is sound for this spec.
- timestamp: 2026-06-14 (CODE ANALYSIS) — **Hypothesis 2 (Phase 12/13/14 dispatch-entry holds) not triggered initially.** checkBillingHalt / checkBudgetBlocked (project_controller.go:953-966) only return RequeueAfter when those conditions are already True; a fresh stub project has neither. BUT: the holds sit inside reconcileProjectPlannerDispatch, and reconcilePhase3Lifecycle Step 0b (line 449) returns EARLY whenever that function returns Requeue||RequeueAfter>0 — before clone dispatch (Step 3). So IF a hold (or any early requeue in planner dispatch) ever fires, clone never happens. Keep as a live mechanism but no initial trigger found.
- timestamp: 2026-06-14 (CODE ANALYSIS) — **Dispatch ordering clarified.** First tide Job is the project-level PLANNER `tide-project-<UID>-1` (reconcilePhase3Lifecycle Step 0b runs reconcileProjectPlannerDispatch BEFORE clone Step 3). Planner sets Phase=Running, returns Requeue → early-return. Clone only dispatches after the planner's Milestone is owned (idempotency guard line 914-925 returns no-requeue, falling through to Step 3). So the real succession is: init → planner(project) → Milestone materialized → … → clone → push → Complete. The wedge is somewhere in THIS chain, not strictly "before dispatch."

## Eliminated

- Bug #1 (git-http-server ImagePullBackOff) — fixed in d45909b; the Deployment now reaches Available and receive-pack advertises (both green in run 27503518297).
- Budget zero-cap enforcement (see Refuted hypotheses).
- Hypothesis 1 (Phase 13 image resolution): evidence 2026-06-14. init Job=hardcoded busybox; clone Job=TidePushImage; only project-planner uses resolveImage, which resolves the stub via subagent.defaults.image→CLAUDE_SUBAGENT_IMAGE→helmDefaults.Image (chart deployment.yaml:63-64, suite_test.go:489). Sound.
- Budget HasHeadroom gate (NEW Phase 14-03 gate, task_controller.go:402-411): reservation.go:127 HasHeadroom returns true via the `default:` branch when both AbsoluteCapCents==0 and RollingWindowCapCents==0. cap=0 → unlimited headroom → not blocked. Extends the original budget refutation to cover the headroom path too.
- BillingHalt on stub success: setBillingHaltIfNeeded is gated on out.ExitCode != 0 (project_controller.go:1133); stub success exits 0, so no halt stamped.

## Current Focus

reasoning_checkpoint:
  hypothesis: "ProjectReconciler.Reconcile returns ctrl.Result{}, nil after adding the finalizer (an r.Update), but the For()-level predicate is predicate.Or(GenerationChangedPredicate, AnnotationChangedPredicate). Adding a finalizer changes neither generation nor annotations, so the resulting Update event is filtered out. With no self-requeue and no other event, the Project parks at empty Status.Phase until the 10h default resync, never reaching the init-Job path."
  confirming_evidence:
    - "Live kind repro: Project sat at empty phase + ZERO jobs (no init Job) for 2+ min."
    - "Adding an unrelated annotation (debug/kick) fired AnnotationChangedPredicate → Project IMMEDIATELY progressed: init created+completed, planner dispatched, clone created, Phase=Running in 8s."
    - "Project YAML: finalizer present, generation=1, status section empty — reconcile #1 added finalizer and stopped, never re-triggered."
    - "Sibling reconcilers use builder.WithPredicates(annotationOnly); task_controller.go:1518-1527 documents this exact post-finalizer-Update filtering trap. Project is the lone outlier with the Generation-OR predicate (introduced Phase 02-10 6d13089, May 13 — latent ~1 month)."
  falsification_test: "After ctrl.Result{Requeue:true} in the finalizer-add block, a freshly created Project still parks at empty phase with no init Job."
  fix_rationale: "Self-requeue after the finalizer Update so the reconciler deterministically re-runs into reconcileProjectPhase2 — independent of any predicate-passing external event. Standard kubebuilder finalizer pattern; removes the race entirely. Root cause, not symptom."
  blind_spots: "Exact v1.0.1 commit that shifted timing not isolated (latent race since May 13; fix is correct regardless). Full Layer B suite not yet re-run against the fixed image."

next_action: human-verify checkpoint — present root cause + minimal fix + RED/GREEN regression proof before the formal release re-run deploys the fixed controller image to kind for an unkicked end-to-end medium_http Complete.

## Resolution

root_cause: |
  ProjectReconciler.Reconcile adds the project finalizer via r.Update and returns
  ctrl.Result{}, nil. The controller's For()-level watch predicate is
  predicate.Or(GenerationChangedPredicate, AnnotationChangedPredicate). A finalizer
  change bumps neither metadata.generation (CRD generation tracks spec only) nor
  annotations, so the resulting Update event is filtered out. Nothing else enqueues
  the Project, so it parks at empty Status.Phase until the controller-runtime default
  10h resync — the init Job (and therefore all downstream dispatch and the path to
  Complete) is never created. The medium_http nightly was passing by luck of an
  incidental re-enqueue; a v1.0.1 timing shift (Phases 12–17 perturbed reconcile
  ordering) exposed this latent missing-requeue that has existed since Phase 02-10
  (6d13089, 2026-05-13). NOT a budget/image/gate regression — those were all verified
  inert for the all-auto cap=0 stub spec.
fix: |
  Return ctrl.Result{Requeue: true} (instead of ctrl.Result{}) after the finalizer-add
  Update in ProjectReconciler.Reconcile, so the reconciler deterministically re-runs
  into reconcileProjectPhase2 on the next pass regardless of any external event passing
  the predicate. Standard kubebuilder finalizer pattern; removes the race entirely.
verification: |
  - Live kind repro reproduced the wedge (empty phase, zero jobs, 2+ min) and the
    annotation-kick unwedge (init→planner→clone→Running in 8s) — pinned the mechanism.
  - Regression test added (project_controller_test.go): asserts the finalizer-add
    reconcile returns res.Requeue==true. Proven RED without the production fix
    (1 Failed) and GREEN with it (143 Passed). go build ./... + go vet clean, gofmt clean.
  - PENDING human-verify: deploy fixed controller image to kind and confirm a fresh
    medium_http Project reaches Status.Phase=Complete WITHOUT any manual kick.
files_changed:
  - internal/controller/project_controller.go
  - internal/controller/project_controller_test.go
