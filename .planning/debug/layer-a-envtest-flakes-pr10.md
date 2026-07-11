---
status: resolved
trigger: "PR #10 CI run 29161443508: boundary_push_test.go:113 'phase boundary dispatches tide: phase <name> authored' — Timed out after 5.001s (54 Passed | 1 Failed) on a .planning-only diff. Third distinct Layer A envtest flake in three consecutive CI rounds. Root-fix + bounded same-class sweep."
created: 2026-07-11T18:30:00Z
updated: 2026-07-11T20:30:00Z
---

## Current Focus

hypothesis: "RESOLVED — three root causes fixed (see Resolution). 5 consecutive full-suite runs green."
test: "Done — see Resolution.verification."
expecting: "n/a"
next_action: "None. Session resolved; commits on phase-34-verify-close (not pushed)."

reasoning_checkpoint:
  hypothesis: "Manager's push-incapable PhaseReconciler consumes the one-shot boundary-push transition and latches Phase=Succeeded, so the push-capable test-local reconciler can never fire the push."
  confirming_evidence:
    - "CI log 17:24:50.4669: INFO 'skipping boundary push: TidePushImage not configured' controller=phase phase=bp-it-ph project=bp-it-ph-proj — the manager-side reconciler reached the boundary trigger with empty image."
    - "Same reconcileID logged 'no env reader; skipping tiny-status read' — proves it was the suite-registered reconciler (EnvReader nil), not the test-local one (which has newMapEnvReader)."
    - "phase_controller.go:238 short-circuits terminal phases; boundary_push.go:96-108 skips silently on empty TidePushImage then handleJobCompletion falls to patchPhaseSucceeded (line 742)."
    - "boundary_push_test.go NOTE (line 177-188) claims suite Phase/Plan reconcilers 'have no Dispatcher configured' — stale: suite_test.go:287-309 registers both WITH stubDispatcher."
  falsification_test: "If the push Job existed but arrived late (>5s), the root cause would be slack, not a latch — refuted: Get returned NotFound for the entire window and the manager logged the skip + no 'triggered boundary push' line ever appeared."
  fix_rationale: "Setting TidePushImage on the suite-registered Milestone/Phase/Plan reconcilers makes both racers behave identically — the boundary push Job (deterministic name tide-push-<project.UID>) is created by whichever wins; triggerBoundaryPush is exists-check + AlreadyExists idempotent. This removes the race semantics instead of widening a timeout over a latched (unrecoverable) state."
  blind_spots: "Other specs whose Projects carry Spec.Git.RepoURL could now get push Jobs created by the manager (boundary or artifact triggers), affecting Job-list assertions or the D-02 gitWriterInFlight gate within the same project. Must bound before applying."

## Symptoms

expected: "Spec creates Project/Milestone/Phase, drives test-local PhaseReconciler, marks planner Job succeeded, drives 3 more times; push Job tide-push-<project-uid> appears with --commit-message='tide: phase bp-it-ph authored' within 5s."
actual: "Eventually at boundary_push_test.go:106-113 timed out after 5.001s — pushArgsForJob returned nil (Job never existed). 54 Passed | 1 Failed."
errors: "expected push Job tide-push-0cddee87-7902-497a-8796-96a1e6475208 to exist; Expected <[]string | len:0, cap:0>: nil not to be empty. Manager log in window: repeated ERROR 'create clone job: Job.batch tide-clone-0cddee87... is invalid: spec.template.spec.containers[0].image: Required value' from project controller (bp-it-ph-proj) in exponential backoff (17:24:50.39 → 50.70 → 51.03 → 51.67 → 52.95 → 55.51). Also early optimistic-lock conflict on the Project at create time."
reproduction: "Flaky on CI (ubuntu runner, make test-int-fast). Run 29161443508 on a .planning-only diff — definitively pre-existing. Not yet reproduced locally."
started: "First observed this spec failing in CI round 3 (PR #10). Prior rounds flaked in gates_test.go and global_wave_derivation_test.go (root-fixed on phase-38, now main). See .planning/debug/layer-a-envtest-flakes-pr9.md."

## Eliminated

## Evidence

- timestamp: 2026-07-11T18:28:00Z
  checked: "gh run view 29161443508 --log-failed, full spec window"
  found: "During the whole 5s Eventually window the manager's project controller looped on 'create clone job ... containers[0].image: Required value' for bp-it-ph-proj with growing backoff. Push Job never created. The failing assertion is the NotTo(BeEmpty()) at line 108 — Job Get returned not-found for all 5s."
  implication: "The push Job creation never happened at all (not a late arrival). Whoever creates it never ran with satisfied preconditions during the window."

- timestamp: 2026-07-11T18:29:00Z
  checked: "boundary_push_test.go full read"
  found: "Test drives test-local PhaseReconciler exactly 3 times after markPlannerJobSucceeded (line 274), then polls 5s. markPlannerJobSucceeded writes Job status via mgrClient.Status().Update but there is NO wait for the informer cache to observe Succeeded before the 3 drives. The NOTE at line 177 says manager-registered Phase/Plan reconcilers have no Dispatcher — so only the test-local r can create the push Job. drive() retries only conflict-class errors."
  implication: "If the mgr cache is stale when the 3 drives run, boundary push never fires and nothing re-drives it during the Eventually. Matches the gates_test.go flake class (fixed-count drive vs drive-until-observable)."

- timestamp: 2026-07-11T18:45:00Z
  checked: "Blast radius of suite-level TidePushImage: grep RepoURL across envtest specs + read artifact_push.go guards + gates/planner_dispatch/leak_blocked completion paths"
  found: "Only 4 files have git-configured Projects. planner_dispatch_test and leak_blocked_test NEVER mark any Job terminal or child Succeeded (grep for Succeeded/terminal/JobComplete: zero hits) — handleJobCompletion never runs there. gates_test's only git project (gate12-proj) never completes a planner Job in its spec. triggerArtifactPush additionally guards on Status.Git.BranchName (only boundary_push fixtures set it) + non-empty stage-envelope map. gitWriterInFlightCount matches only role=git-writer labeled Jobs."
  implication: "Setting TidePushImage on suite-registered Phase/Plan/Milestone reconcilers changes behavior ONLY in the boundary_push specs — exactly where symmetric behavior is needed. No Job-count assertion elsewhere can observe a new push Job."

- timestamp: 2026-07-11T18:47:00Z
  checked: "Cache-visibility edges for the fix: waitITCacheSync (gates_test.go:827) + boundary_push_test.go:254-266"
  found: "The child Plan's existence AND Succeeded status are both confirmed visible in the mgr informer cache (mgrClient polls) BEFORE markPlannerJobSucceeded runs. Informer cache is monotonic, so any manager reconcile triggered by the job-terminal event sees the Succeeded child → BoundaryDetected=true → with TidePushImage set, creates the push Job before latching Succeeded."
  implication: "With config symmetry, every race interleaving produces the push Job (exists-check + AlreadyExists idempotency in triggerBoundaryPush). Deterministic."

## Eliminated

- hypothesis: "Cache-staleness on the fixed-count drive (test-local reconciler never observes the terminal Job within 3 drives) — pure drive-until-observable class like gates_test PR#9"
  evidence: "CI log shows the manager-side reconciler DID observe the terminal Job and ran the full completion path ('skipping boundary push: TidePushImage not configured' at 17:24:50.4669, then latched Succeeded). The push was skipped by config, not missed by staleness. Drive-until-observable alone cannot fix a latched terminal status (phase_controller.go:238 short-circuit)."
  timestamp: 2026-07-11T18:40:00Z

- hypothesis: "The clone-job error-backoff loop (create clone job: image Required) wedged the project controller and blocked the push"
  evidence: "The clone job is created by ProjectReconciler for git import; the push Job is created by Phase/Plan reconcilers via triggerBoundaryPush — independent controllers. gitWriterInFlightCount only counts role=git-writer labeled Jobs, and the clone job never existed (creation kept failing). The clone errors are pre-existing background noise in every run of these specs."
  timestamp: 2026-07-11T18:42:00Z

- timestamp: 2026-07-11T19:20:00Z
  checked: "Post-fix full-suite runs (3x local) — new failure surfaced: gates_test.go TestGateApproveFlow 'approve-milestone annotation' timed out (15s) at the 6a lift assert; milestone stayed AwaitingApproval; manager milestone controller logged NOTHING suite-wide in the window."
  found: "Differential test on BASE (git stash, no changes): base run 1 FAILED on the same spec at the same line (Timed out after 5.002s); base run 2 passed. Definitively pre-existing — the 4th flake in the series, not introduced by this session's changes."
  implication: "The spec applies the approve annotation, blind-drives the local reconciler 5x, then polls without driving. If the drives all run before the annotation converges in the manager cache AND the manager's own annotation-event reconcile misses it (stale read → parked arm → RequeueAfter 30s, silent — the parked arm has zero log statements and the artifact-push trigger exits at the git-less RepoURL check before its Info log), NOTHING lifts the park within any poll window. Same starvation class as the PR#9 gates fix — fixed-count blind drives + poll-only Eventually."

- timestamp: 2026-07-11T19:30:00Z
  checked: "gates_test.go sibling specs for the identical blind-drive-then-poll shape"
  found: "7 mechanically-identical sites: TestGateApproveFlow park assert / 6a lift / 6b Succeeded, TestRejectHalts park assert, RESUME-01 exit-Failed assert, TestWavePauseBetweenWaves boundary-pause assert + approve-wave consumption assert."
  implication: "All converted to drive-inside-Eventually (1 drive per poll), mirroring the existing house pattern at the planner-Job wait (gates_test.go:309-314, from the PR#9 fix)."

## Resolution

root_cause: "THREE distinct root causes found and fixed in this session.

  (1) boundary_push (the PR #10 CI failure): config-asymmetry race. suite_test.go registers PhaseReconciler/PlanReconciler WITH Dispatcher (stubDispatcher) but WITHOUT TidePushImage. When the manager's Owns(Job)/Owns(Plan) watch wins the race to handleJobCompletion at the one-shot boundary transition (job terminal + all child plans Succeeded), it takes the nil-EnvReader fallback, BoundaryDetected=true, triggerBoundaryPush SKIPS on empty TidePushImage (boundary_push.go:96), and patchPhaseSucceeded latches the level terminal. The test-local push-capable reconciler then short-circuits forever (phase_controller.go:238); the push Job is never created. The test file's NOTE claiming suite Phase/Plan reconcilers 'have no Dispatcher configured' was stale.

  (2) gates TestGateApproveFlow (4th flake, surfaced during verification, PROVEN pre-existing via base-stash differential — base run failed 5.002s at the same line): stale-snapshot re-park clobber, a CONTROLLER bug. patchMilestone/Phase/PlanAwaitingApproval used plain MergeFrom (no optimistic lock). A reconciler holding a stale pre-park Running snapshot that walks handleJobCompletion → gate hook (!alreadyApproved && !CheckApprove on the stale copy) blind-merges the park OVER a concurrent approve's Running+ApprovedByUser write. Because SetStatusCondition replaces ConditionWaveOrLevelPaused — the very condition whose False/ApprovedByUser value is the only re-park guard — and the one-shot approve annotation is already deleted, the level wedges at AwaitingApproval permanently. Proven by refutation: with drive-inside-Eventually re-driving every 100ms for 15s the level STAYED parked (starvation alone was falsified).

  (3) boundary_push markPlannerJobSucceeded 'Job not found' (surfaced in verification round 2, run started with this spec first while manager caches were syncing): fixed-count blind drives can abort before creating the planner Job; the test then hard-fails marking a nonexistent Job terminal. Identical to the PR#9 gates flake ('planner Job NotFound at the terminal patch') whose drive-until-observable fix never reached boundary_push."
fix: "
  (1) Config symmetry: suite_test.go adds testTidePushImage constant + TidePushImage on the suite-registered PhaseReconciler and PlanReconciler (NOT Milestone/Project — no spec asserts their pushes; ProjectReconciler's bounded-retry push replacement would interfere with leak_blocked's hand-built push Jobs). Race now outcome-invariant: whoever wins creates the same deterministic tide-push-<project-uid> Job; loser hits exists-check/AlreadyExists idempotency. Blast radius verified nil: no other spec reaches a boundary transition on a git-configured project.
  (2) Optimistic lock on all three level park patches (milestone/phase/plan patch*AwaitingApproval): MergeFromWithOptimisticLock — a stale re-parker now 409s and the requeued fresh read sees alreadyApproved. Plus drive-until-observable conversions at 7 blind-drive-then-poll sites in gates_test.go.
  (3) Drive-until-planner-Job-observable Eventually before markPlannerJobSucceeded in both boundary_push specs (house pattern from gates_test.go:309).
  Sweep fixes: (a) all ≤5s Eventually windows on manager/controller-driven state widened to 15s (intervals kept) in gates/planner_dispatch/leak_blocked/annotation_patch/boundary_push; (b) ensureLiveProject shared create-or-wait helper (helpers_test.go) extracted from global_wave_derivation's PR#9 fix and applied to the 4 mechanically-identical Create+IgnoreAlreadyExists-racing-terminating-Project sites (global_dispatch, indegree, spec_conformance, parent_unresolved); (c) RetryOnConflict on boundary_push markPlannerJobSucceeded (mgrClient cached-read Job status helper, mirroring gates makeFakeJobTerminalGates)."
verification: "Full Layer A suite (go test ./test/integration/envtest/... -ginkgo.label-filter='envtest') 5 CONSECUTIVE runs green after all fixes: exit codes 0,0,0,0,0 — 55 Passed | 0 Failed each, zero '--- FAIL' lines. Focused primary spec 5x green before that; gates gate-flow specs focused 5x green. internal/controller unit suite green after the optimistic-lock change (go test -short: ok, 54.6s). gofmt clean, go vet clean, golangci-lint 0 issues on both touched packages."
files_changed:
  - internal/controller/milestone_controller.go (optimistic-lock park patch)
  - internal/controller/phase_controller.go (optimistic-lock park patch)
  - internal/controller/plan_controller.go (optimistic-lock park patch)
  - test/integration/envtest/suite_test.go (testTidePushImage + Phase/Plan reconciler symmetry)
  - test/integration/envtest/boundary_push_test.go (drive-until-Job-observable, RetryOnConflict helper, 15s windows, stale NOTE rewritten, shared image constant)
  - test/integration/envtest/gates_test.go (7 drive-inside-Eventually conversions, 15s windows)
  - test/integration/envtest/helpers_test.go (NEW — ensureLiveProject shared helper)
  - test/integration/envtest/global_wave_derivation_test.go (refactored to ensureLiveProject)
  - test/integration/envtest/global_dispatch_test.go (ensureLiveProject)
  - test/integration/envtest/indegree_test.go (ensureLiveProject)
  - test/integration/envtest/spec_conformance_test.go (ensureLiveProject)
  - test/integration/envtest/parent_unresolved_test.go (ensureLiveProject)
  - test/integration/envtest/planner_dispatch_test.go (15s windows)
  - test/integration/envtest/leak_blocked_test.go (15s windows)
  - test/integration/envtest/annotation_patch_test.go (15s window)

## Seeds (suspect patterns NOT fixed — not mechanically same-class)

- leak_blocked_test.go markPushJobFailed (:107) and markPodTerminated: bare direct-client (k8sClient) Get→mutate→Status().Update on Jobs/Pods the test itself created. Not same-class: the proven conflict mechanism was the CACHED client serving a stale RV; direct-client reads return the latest RV, and no controller writes Job/Pod status in envtest (no kube-controller-manager). Revisit only if a 409 ever appears here.
- leak_blocked_test.go bare PVC Status().Update (:180): same rationale — no concurrent PVC status writer exists in envtest.
- budget_test.go makeBoundPVC Create+IgnoreAlreadyExists on PVCs (:213): PVC deletion in envtest hangs Terminating forever (pvc-protection finalizer, no controller to clear it), so the terminating object remains USABLE — the vanish-mid-spec failure mode of the Project variant cannot occur. Benign.
- boundary_push_test.go childPlan OwnerReference uses APIVersion 'tideproject.k8s/v1alpha1' (real API is v1alpha2) — causes noisy 'Could not retrieve rest mapping' enqueueRequestForOwner ERRORs in the manager log and means the manager's Owns(Plan) watch does NOT map this child to its Phase. Currently harmless to the spec (the direct Owns(Job) path drives completion) but worth correcting in a future pass.
- Milestone/ProjectReconciler remain push-incapable in the suite (deliberate, see suite_test.go comment). If a milestone- or project-boundary push integration spec is ever added to Layer A, the same config-symmetry latch race will fire there.
