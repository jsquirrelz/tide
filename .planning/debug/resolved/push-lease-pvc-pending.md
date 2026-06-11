---
slug: push-lease-pvc-pending
status: resolved
trigger: |
  User-supplied (post-cascade-7-runtime-gate): "Phase 03 cascade 11: push_lease ×4 failures at line 228 — 'tide-push-<project-uid> not found'". The 4 specs were SKIP-gated until quick task 260521-ccz removed the gate; this run is the first time they exercised end-to-end on main.
  
  Observed root cause shape: NOT the push Job dispatch itself. The ProjectReconciler never reaches Step 4 (push Job dispatch at internal/controller/project_controller.go:440) because Step 2 (shared-PVC Bound gate at line 246) requeues forever. Manager log shows ~30+ requeue events with `pvcName: tide-projects, pvcPhase: Pending` across all 4 push-lease test windows (15:27:49 → 15:34:48).
created: 2026-05-21
updated: 2026-05-21
phase_context: Phase 03 follow-up (surfaced post-cascade-9 removal of SKIP gate)
related_artifacts:
  - .planning/quick/260521-ccz-push-lease-cascade-9-recipe/260521-ccz-SUMMARY.md  # cascade-9 removed the SKIP gate
  - .planning/debug/chaos-resume-cascade-10.md  # cascade-10 sister investigation (same suite run pattern)
  - .planning/quick/260521-f8x-phase-03-cascade-7-gate-plan-planner-dis/260521-f8x-SUMMARY.md  # cascade-7 ran in same suite
cascade_classification: test-side-PVC-bind-deadlock (WaitForFirstConsumer + no-Pod consumer)
goal: find_and_fix
---

# Debug: push-lease PVC stuck Pending in push-lease-test namespace (cascade 11)

## Symptoms

**Expected behavior:** push-lease Layer B specs (4 specs at `test/integration/kind/push_lease_test.go:84/107/129/158`) should dispatch a `tide-push-<project-uid>` Job within 90s of `forcePushReady(project, "")` patching `Status.Phase=Complete`. ProjectReconciler at `internal/controller/project_controller.go:440` handles dispatch when:
```go
if project.Status.Phase == tideprojectv1alpha1.PhaseComplete &&
   project.Spec.Git != nil && project.Spec.Git.RepoURL != "" {
    // create push Job
}
```
The fixture has `spec.git.repoURL = "https://example.invalid/owner/push-lease-repo.git"` (set, non-empty); `forcePushReady` patches `Status.Phase=Complete`. The push Job should be created within ~1-2 reconciles.

**Actual behavior:** All 4 specs FAIL at `push_lease_test.go:228` (`waitForPushJob` Eventually 90s timeout) with the error: `jobs.batch "tide-push-<project-uid>" not found`. Suite ran 13/13 specs (no SKIPs because cascade-9 removed the gate) and 4 push-lease specs all failed identically. Wall-time impact: ~7 min × 4 = ~28 min added (each spec waits the full 90s + cleanup).

**Error messages (verbatim from /tmp/cascade-7-postfix.log Summarizing block):**
```
Summarizing 4 Failures:
  [FAIL] Push lease semantics (ART-06 / D-B5 / D-B6) [It] Test 1: ...  /push_lease_test.go:228
  [FAIL] Push lease semantics (ART-06 / D-B5 / D-B6) [It] Test 2: ...  /push_lease_test.go:228
  [FAIL] Push lease semantics (ART-06 / D-B5 / D-B6) [It] Test 3: ...  /push_lease_test.go:228
  [FAIL] Push lease semantics (ART-06 / D-B5 / D-B6) [It] Test 4: ...  /push_lease_test.go:228

Ran 13 of 13 Specs in 1063.060 seconds
FAIL! -- 9 Passed | 4 Failed | 0 Pending | 0 Skipped
```

The Eventually failure message (per `push_lease_test.go:228-229`):
```
push Job push-lease-test/tide-push-<project-uid> must be dispatched
```
Eventually wraps `k8sClient.Get(jobName)` → returns `NotFound: jobs.batch "..." not found` for the full 90s. The Job is never created.

**Critical observable from /var/folders/.../kind-logs-tide-test/tide-test-control-plane/pods/tide-system_tide-controller-manager-fc5b6df47-d4jjf_*/manager/0.log:**

The ProjectReconciler emits ~30+ identical log lines across the full 7-minute push-lease test window (15:27:49 → 15:34:48), all with the same shape:
```json
{
  "level":"info",
  "msg":"shared PVC not yet Bound; requeueing",
  "Project":{"name":"push-lease","namespace":"push-lease-test"},
  "pvcName":"tide-projects",
  "pvcPhase":"Pending"
}
```

This log line is emitted at `project_controller.go:247` (the gate at line 246: `if pvc.Status.Phase != corev1.ClaimBound`). The reconciler returns `ctrl.Result{RequeueAfter: initJobRequeueAfterNoPVC}, nil` and never proceeds to Step 3 (Init Job) or Step 4 (push Job dispatch). The 4 push-lease specs all hit the 90s timeout because the gate never opens.

**Comparison to chaos-resume (same suite run, same cluster, same PVC YAML):**

In the SAME suite run, chaos-resume's `chaos-resume-test` namespace PVC (identical YAML — same `accessModes`, same `storage: 1Gi`, no `storageClassName`) bound successfully and the test PASSED in 47.6s. Zero "shared PVC not yet Bound" log lines were emitted for the chaos-resume-test namespace. Both namespaces are created via `createNamespace(ns)` which calls `ensureProjectsPVC(ns)` (test/integration/kind/suite_test.go:592–608), and both fixtures also re-create the PVC inline via `applyFile`. Same identity, opposite outcome.

**Timeline (from /tmp/cascade-7-postfix.log + kind-logs export):**
- 15:27:00 — chaos-resume starts (chaos-resume-test ns created)
- 15:27:47.6 — chaos-resume PASSES (47.6s — its PVC bound somewhere in that window, no Pending log line)
- 15:27:48 — push-lease Test 1 starts (push-lease-test ns created by createNamespace + fixture apply)
- 15:27:49 — first "shared PVC not yet Bound" log; PVC is Pending
- 15:29:19 — Test 1 fails after 90s wait
- AfterEach deletes push-lease-test namespace
- 15:29:20 — Test 2 begins (recreates push-lease-test ns; the PVC is FRESH)
- 15:29:21 → 15:31:09 — Test 2 fails identically
- ... pattern repeats for Tests 3 + 4 ...
- 15:34:43 — Test 4 fails

Every single push-lease Test creates a FRESH `push-lease-test` namespace + fresh PVC. NONE bind. ~30 reconciles ×4 namespaces = 120+ Pending observations. Chaos-resume's PVC immediately preceded all 4 with the same YAML and bound fine.

**Timeline implies:** the PVC bind failure is NOT a cluster-state issue from a prior test (each push-lease test has a fresh ns/PVC). It is something structural about how `push-lease-test` namespace's PVC fails to bind in this kind cluster. Two leading sub-hypotheses (see Current Focus below).

**Reproduction:**

```bash
cd /Users/justinsearles/Projects/tide

# Full-suite repro (deterministic — matches /tmp/cascade-7-postfix.log):
make test-int 2>&1 | tee /tmp/cascade-11-fullsuite.log

# Isolation repro (recommended FIRST — confirms whether the bug needs prior-test cluster state):
make test-int GINKGO_FOCUS='Push lease semantics' 2>&1 | tee /tmp/cascade-11-isolation.log
# Note: GINKGO_LABEL_FILTER='kind && D-B5' won't work (D-B5 isn't a Ginkgo label — only 'kind' is registered).

# During the run, capture live PV/SC state from a second shell:
kubectl --context kind-tide-test get pvc -A -w &
kubectl --context kind-tide-test get pv -A -w &
kubectl --context kind-tide-test get storageclass -A
kubectl --context kind-tide-test describe pvc tide-projects -n push-lease-test
```

## Available Evidence

| Artifact | Path | Use |
|----------|------|-----|
| Full-suite test log | `/tmp/cascade-7-postfix.log` | Failure context; lines 1922–2095 contain all 4 push-lease failures with full Eventually-error stacks |
| Latest manager log | `/var/folders/51/h7gq6p5x3592gvrbhrd985q80000gn/T/kind-logs-tide-test/tide-test-control-plane/pods/tide-system_tide-controller-manager-fc5b6df47-d4jjf_*/manager/0.log` | ~30 "shared PVC not yet Bound" entries; project cleanup events; no push-Job-dispatch attempts |
| Push-lease fixture | `test/integration/kind/testdata/push-lease-project.yaml` | PVC inline (lines 22-30), Project spec with git block (lines 60-70) |
| Push-lease test spec | `test/integration/kind/push_lease_test.go` | BeforeEach lines 57-61 (with createNamespace post-cascade-9); 4 It blocks at 84/107/129/158; waitForPushJob at 224-230 |
| createNamespace + ensureProjectsPVC | `test/integration/kind/suite_test.go:592-608` + `test/integration/kind/failure_test.go:173-184` | PVC YAML the test creates programmatically; identical shape to fixture PVC |
| ProjectReconciler PVC gate | `internal/controller/project_controller.go:240-249` (gate); `:430-490` (push dispatch — never reached) | Where the reconciler short-circuits |
| Chaos-resume passing run | Same `/tmp/cascade-7-postfix.log` lines 1899-1920 | Confirms PVC YAML works in some namespaces; same kind cluster, same provisioner |
| Kind cluster | DELETED at AfterSuite (`suite_test.go:184` calls `kubectl delete cluster tide-test`). | No live state — must rerun to capture `kubectl describe pvc/pv/sc` |

## Current Focus

```yaml
hypothesis: |
  ROOT CAUSE LOCKED — WaitForFirstConsumer deadlock specific to push-lease test shape.

  The kind cluster's default StorageClass is `standard` backed by `rancher.io/local-path`,
  which has `volumeBindingMode: WaitForFirstConsumer`. A PVC with this binding mode stays
  in `Pending` until the first Pod that mounts it is scheduled — only then does the
  provisioner create a PV and bind the PVC.

  - chaos-resume passes because its workflow creates Tasks (alpha/beta/gamma). Once a
    Task is in Wave 0 the ProjectReconciler dispatches `tide-init-<UID>` Job (which mounts
    `tide-projects` PVC). That Pod schedule triggers the local-path provisioner →
    PVC binds → reconciler proceeds → init Job completes → Task Jobs run. All of this
    inside the chaos-resume 47.6s window.

  - push-lease fails because the spec exists to test the push-Job state machine in
    isolation — it has NO Tasks. The ProjectReconciler's Step 2 (line 246) gates on
    `pvc.Status.Phase == corev1.ClaimBound` BEFORE Step 3 (Init Job creation at line 252).
    With no other consumer Pod ever scheduled, the PVC never gets a first consumer, never
    binds, the reconciler requeues forever. Step 4 (push-Job dispatch at line 440) is
    structurally unreachable. Net result: 4×90s Eventually-timeouts.

  This is a TEST-SIDE problem (the push-lease spec's shape interacts badly with kind's
  WaitForFirstConsumer default) and NOT a production-controller bug — the controller's
  Pitfall #1 gate is correct (don't dispatch with an unbound PVC). The test fixture just
  needs to ensure a first-consumer Pod arrives so the PVC binds.

test: |
  Verified via reasoning over committed evidence (cluster currently deleted by
  AfterSuite; no live state to inspect, but the chain of evidence is complete):
  
  (1) kind v0.31 + kindest/node:v1.33.7 ships with `standard` StorageClass backed by
      `rancher.io/local-path-provisioner` — this is kind's default and documented.
      The provisioner uses `volumeBindingMode: WaitForFirstConsumer` by default
      (see kind release notes + local-path-provisioner manifest); this is the well-known
      kind behavior and matches the suite_test.go:475 comment block that calls out the
      single-node RWO override.
  
  (2) /tmp/cascade-7-postfix.log + manager log: zero `tide-init` Job creations occur in
      `push-lease-test` namespace. Pre-Step-3 short-circuit confirmed.
  
  (3) Chaos-resume's same suite run binds the IDENTICAL PVC YAML because it dispatches
      Tasks → init Job → first consumer scheduled → PVC binds. The DIFFERENCE between
      the two specs is presence of Pod consumers, not PVC shape.
  
  (4) push_lease_test.go is intentionally Pod-free — `forcePushReady` mocks
      Status.Phase=Complete via direct status patch, NEVER dispatches a Task. The spec
      is a state-machine isolation test, not a Pod orchestration test.

expecting: |
  Fix paths (ranked):
  
  OPTION A — TEST-SIDE PRE-WARM POD (recommended, smallest surface):
    Add a helper `ensurePVCBound(ns)` in suite_test.go that creates a one-shot pause Pod
    mounting `tide-projects`, waits for the PVC to reach Bound, then deletes the Pod.
    Wire it into `createNamespace(ns)` AFTER `ensureProjectsPVC` so every Layer-B
    namespace's PVC is pre-bound before the controller sees it. Chaos-resume already
    binds naturally (Tasks pre-warm); this helper is a no-op for Pod-bearing specs
    because the PVC is already bound when chaos-resume's tide-init arrives — but it
    is harmless redundancy.
    Risks: low. The pause Pod uses an existing image (e.g. busybox or registry.k8s.io/pause:3.9).
    Scope: ~25 lines suite_test.go. Quick-task.
  
  OPTION B — STORAGECLASS WITH IMMEDIATE BINDING:
    Define a custom test StorageClass with `volumeBindingMode: Immediate` in the
    cluster.yaml or as a kubectl apply at AfterSuite cluster-create. Reference it from
    `projectsPVCYAML(ns)` via `storageClassName: tide-test-immediate`.
    Risks: medium. Requires updating projects_pvc_test.go assertProjectsPVCShape
    assertion (currently REQUIRES storageClassName: nil — line 202-204). Also touches
    the kind setup path which is shared across all Layer-B specs.
    Scope: ~40 lines spread across 3 files. Slightly larger quick-task.
  
  OPTION C — DROP THE PVC GATE FOR push-LIFECYCLE-ONLY PROJECTS (PRODUCTION CHANGE):
    Allow `reconcileProjectPhase2` to proceed to push-Job dispatch when the Project has
    no init-Job lifecycle (e.g., Phase=Complete forced via status patch). Or invert the
    Step 2/Step 3 order so the init Job is dispatched first (the Job creation in K8s
    api-server proceeds even without a bound PVC — only Pod scheduling waits for the PVC
    consumer chain).
    Risks: high. Changes production controller semantics that exist for Pitfall #1 + the
    chart's single-shared-RWX-PVC architecture. Out of scope for this debug session per
    CLAUDE.md anti-pattern; this is a Phase 03+ follow-up if at all.

observed: |
  (1) Codebase grep confirms zero `WaitForFirstConsumer` / `volumeBindingMode` references
      in repo (tests don't override it). Means the test relies on whatever the kind default
      is, which for rancher.io/local-path is WaitForFirstConsumer.
  
  (2) cluster.yaml at test/integration/kind/cluster.yaml: plain kind config, no
      featureGates, no storageClass override. Cluster uses kind's built-in `standard` SC.
  
  (3) suite_test.go:474-479 comment explicitly calls out rancher.io/local-path as the
      single-node provisioner; chart's RWX accessModes are overridden to RWO at helm
      install for kind compatibility. This is well-known to the codebase.
  
  (4) push-lease's spec applies fixture YAML that creates a Project with `spec.git.repoURL`
      set BUT no Milestone/Phase/Plan/Task hierarchy. Chaos-resume's fixture creates the
      FULL hierarchy including Tasks (line 95-160 of chaos-resume-three-task.yaml). The
      structural difference is the load-bearing one for "does anything ever try to mount
      this PVC?"
  
  (5) ProjectReconciler.reconcileProjectPhase2 line 246-249: the gate is a hard
      short-circuit before any Pod is ever requested. The init Job (Step 3) at line 252+
      WOULD eventually create a Pod that mounts the PVC, but Step 2 blocks Step 3 from
      running. Classic chicken-and-egg with WaitForFirstConsumer.
  
  (6) projects_pvc_test.go:202-204 asserts `pvc.Spec.StorageClassName == nil` —
      means OPTION B (explicit storageClassName) requires updating this assertion.

next_action: |
  Surface root cause + 3 fix options to user. Default recommendation: OPTION A
  (test-side pre-warm Pod in `createNamespace`). Smallest scope, no production
  changes, no assertion drift, idempotent for Pod-bearing specs (chaos-resume keeps
  binding naturally via tide-init Job — pre-warm Pod just arrives first).

reasoning_checkpoint: "WaitForFirstConsumer + Pod-free Project = unbreakable deadlock"
tdd_checkpoint: ""
specialist_hint: "go"
```

## Evidence

- timestamp: 2026-05-21 (Wave 1, root-cause investigation via reasoning + grep)
  source: `/Users/justinsearles/Projects/tide/internal/controller/project_controller.go:236-249`
  observation: |
    Step 2 PVC bind gate is a hard short-circuit:
    ```go
    if pvc.Status.Phase != corev1.ClaimBound {
        logger.Info("shared PVC not yet Bound; requeueing", "pvcName", pvcName, "pvcPhase", pvc.Status.Phase)
        return ctrl.Result{RequeueAfter: initJobRequeueAfterNoPVC}, nil
    }
    ```
    The reconciler returns early — Step 3 (init Job creation, line 252+) NEVER executes
    until pvc.Status.Phase == ClaimBound. This is the published Pitfall #1 behavior.

- timestamp: 2026-05-21
  source: `/Users/justinsearles/Projects/tide/test/integration/kind/cluster.yaml`
  observation: |
    Plain 4-line kind config — no featureGates, no storageClass override, no
    extraMounts. The kind cluster uses the default `standard` StorageClass backed by
    `rancher.io/local-path-provisioner`. This provisioner ships with
    `volumeBindingMode: WaitForFirstConsumer` (kind v0.31 default; documented in
    kind release notes + local-path-provisioner manifest).

- timestamp: 2026-05-21
  source: `/Users/justinsearles/Projects/tide/test/integration/kind/suite_test.go:474-479`
  observation: |
    Code comment explicitly acknowledges the kind provisioner:
    ```go
    // Override the chart's default accessModes [ReadWriteMany] to [ReadWriteOnce]
    // because kind's default rancher.io/local-path provisioner only supports
    // RWO/RWOPod.
    ```
    Confirms the codebase knows the cluster uses rancher.io/local-path. The team
    overrode accessModes to RWO but did NOT override volumeBindingMode — leaving the
    WaitForFirstConsumer default in place. Works for tests with Pods; breaks for
    push-lease which has no Pods.

- timestamp: 2026-05-21
  source: `/tmp/cascade-7-postfix.log:1899-1920` (chaos-resume) vs `:1922-2095` (push-lease)
  observation: |
    Chaos-resume PASSES at 47.6s with Tasks (alpha-chaos / beta-chaos / gamma-chaos)
    reaching Running by 11:27:01.106 (within ~1s of namespace create at 11:27:00.045).
    The presence of Task Jobs forces tide-init dispatch → PVC consumer → bind → success.
    
    Push-lease FAILS 4×90s timeout. Fixture has NO Tasks (only Project + Secrets).
    Reconciler gates at Step 2 with no consumer ever arriving. ~30 requeue logs across
    each failed test span.

- timestamp: 2026-05-21
  source: `test/integration/kind/testdata/chaos-resume-three-task.yaml:95-160` vs
          `test/integration/kind/testdata/push-lease-project.yaml:53-66`
  observation: |
    Chaos-resume fixture defines full hierarchy: Project + Milestone + Phase + Plan + 3
    Tasks. Push-lease fixture stops at Project (no Milestone/Phase/Plan/Task). This is
    INTENTIONAL — push-lease tests the push-Job state machine via direct status patch
    (`forcePushReady` patches Status.Phase=Complete), bypassing the normal lifecycle.
    But this intentional Pod-less design collides with WaitForFirstConsumer.

- timestamp: 2026-05-21
  source: `test/integration/kind/projects_pvc_test.go:202-204`
  observation: |
    Existing assertion `assertProjectsPVCShape` enforces `pvc.Spec.StorageClassName == nil`:
    ```go
    if pvc.Spec.StorageClassName != nil {
        t.Fatalf("PVC storageClassName = %q, want it omitted", *pvc.Spec.StorageClassName)
    }
    ```
    Means any fix that adds `storageClassName` to `projectsPVCYAML` must update this
    assertion. Influences Option B sizing.

- timestamp: 2026-05-21
  source: grep on /tmp/cascade-7-postfix.log
  observation: |
    Zero `tide-init` Job creations referencing `push-lease-test` namespace in either
    the test output or the manager log. Confirms Step 3 (init Job) never fires; the
    PVC gate at Step 2 blocks it. Aligned with the manager-log evidence in Symptoms.

## Eliminated Hypotheses

- **H1(a) "local-path-provisioner Pod crashed"** — Unlikely; chaos-resume's PVC bound in
  the same suite run with the same provisioner Pod. Crashed provisioner would have
  failed both namespaces' PVCs.

- **H1(b) "kind node disk full or quota hit"** — Unlikely; chaos-resume's PV is 1Gi and
  the kind node has the host's full disk. Even after chaos-resume consumes 1Gi, ample
  capacity remains. Also each push-lease test creates a fresh PVC in a fresh namespace
  — disk pressure would affect Test 1's PVC most and improve afterward, but all 4
  Tests fail identically with the same Pending phase.

- **H1(c) "provisioner serialization point"** — Unlikely; local-path-provisioner
  handles concurrent PVCs without serialization. Even if it serialized, the next PVC
  would eventually bind after ~seconds, not 90s+.

- **H2 "ordering-dependent state from chaos-resume namespace deletion"** — Eliminated.
  Each push-lease test creates its own fresh namespace AFTER chaos-resume's deleteNamespace
  completes. If suite-state-pollution from chaos-resume were the cause, Test 1 might fail
  but Tests 2/3/4 (which see only push-lease-test namespace deletions, not
  chaos-resume-test) would succeed. They all fail identically → H2 refuted.

- **H3 "PVC double-create race"** — Unlikely. Both `createNamespace.ensureProjectsPVC`
  and `applyFile(testdata/push-lease-project.yaml)` create the same-named PVC; kubectl
  apply is idempotent. Also chaos-resume has the exact same double-create pattern
  (createNamespace + fixture YAML both define the PVC) and it works. The double-create
  is not the differentiator between the two tests.

The REMAINING hypothesis that survives all elimination:

- **H4 "WaitForFirstConsumer deadlock"** — Confirmed by mechanism. local-path-provisioner
  binds PVCs only when a Pod consumer is scheduled. Push-lease has no Pods. PVC never
  binds. ProjectReconciler gates at Step 2 on PVC.Status.Phase == ClaimBound. Deadlock.

## Out of Scope (for this session)

- Cascade 7-bis (phase_controller.go symmetric nil-Project race) — separate follow-up from cascade-7's SUMMARY footer.
- Cascade 7-ter (milestone_controller.go latent nil-deref) — separate follow-up.
- Removing the nil-Project guard at `internal/dispatch/podjob/jobspec.go:266-272` — defense-in-depth follow-up only safe AFTER 7-bis + 7-ter land.
- Refactoring `resolveProjectForPlan` signature to return errors (4 call sites — over-scoped per cascade-7 plan).
- Item 2 Layer B 429 storm spec authoring (Phase 02 UAT closeout #4) — separate follow-up.
- Modifying `charts/tide/values.yaml` — chart is FIXED contract per CLAUDE.md anti-pattern; binary catches up to chart, never reverse.
- Re-running full `make test-int` end-to-end as a verification step (use isolation `GINKGO_FOCUS='Push lease semantics'` instead — 5-7 min instead of 18 min).
- OPTION C above (production controller change to invert Step 2/Step 3 order) — Phase 03+ follow-up; out of scope for this debug session.

## Resolution

(populated when fix lands)

---
**Closed at v1.0.0 milestone completion (2026-06-11).** The defect class this
session tracked was fixed and validated before ship: full `make test-int`
green (Layer A 36/36 + Layer B), nightly-integration green, live medium DoD
on minikube (Project=Complete, BoundaryPushed=True), and the v1.0.0-rc dry-run
gate green end-to-end.
