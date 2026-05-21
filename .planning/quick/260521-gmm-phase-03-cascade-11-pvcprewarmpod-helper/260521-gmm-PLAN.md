---
id: 260521-gmm-phase-03-cascade-11-pvcprewarmpod-helper
title: "Phase 03 Cascade 11: pvcPrewarmPod helper for WaitForFirstConsumer PVC bind"
type: quick
status: planned
created: 2026-05-21
phase: quick
plan: "260521-gmm"
wave: 1
depends_on: []
files_modified:
  - test/integration/kind/suite_test.go
  - test/integration/kind/failure_test.go
autonomous: true
requirements: [cascade-11-pvc-prewarm]
must_haves:
  truths:
    - "A test-only helper pvcPrewarmPod(ns) exists in suite_test.go that creates a pause Pod consuming tide-projects PVC, waits for Bound, then deletes the Pod"
    - "pvcPrewarmPod is idempotent — calling it on a namespace where the PVC is already Bound returns immediately without creating the Pod"
    - "createNamespace(ns) in failure_test.go invokes pvcPrewarmPod(ns) AFTER ensureProjectsPVC(ns) AND AFTER ensureSigningKeySecret(ns) so all Layer-B namespaces get pre-bound PVCs"
    - "projects_pvc_test.go:assertProjectsPVCShape continues to PASS (pvc.Spec.StorageClassName == nil holds — the PVC YAML in projectsPVCYAML is NOT modified)"
    - "go vet ./test/integration/kind/... and go build ./test/integration/kind/... both pass clean (no new lint, no missing imports)"
    - "The push-lease Layer B fixture (push-lease-test namespace) PVC binds within seconds during a kind run instead of staying Pending until 90s timeout"
    - "chaos-resume Layer B spec continues to PASS without regression (the prewarm helper short-circuits idempotently because chaos-resume's tide-init Job triggers binding within the same window)"
  artifacts:
    - path: "test/integration/kind/suite_test.go"
      provides: "pvcPrewarmPod helper definition adjacent to ensureProjectsPVC"
      contains: "func pvcPrewarmPod"
    - path: "test/integration/kind/failure_test.go"
      provides: "createNamespace wires pvcPrewarmPod after ensureProjectsPVC + ensureSigningKeySecret"
      contains: "pvcPrewarmPod(ns)"
  key_links:
    - from: "test/integration/kind/failure_test.go createNamespace"
      to: "pvcPrewarmPod helper in suite_test.go"
      via: "direct function call after ensureSigningKeySecret(ns)"
      pattern: "pvcPrewarmPod\\(ns\\)"
    - from: "pvcPrewarmPod helper"
      to: "tide-projects PVC in given namespace"
      via: "pause Pod mounting persistentVolumeClaim.claimName=tide-projects + Eventually polling pvc.Status.Phase==ClaimBound"
      pattern: "claimName: tide-projects"
---

<objective>
Close Phase 03 Cascade 11 (WaitForFirstConsumer PVC bind deadlock) by adding a test-only `pvcPrewarmPod(ns)` helper in `test/integration/kind/suite_test.go` and wiring it into `createNamespace(ns)` in `test/integration/kind/failure_test.go`. Locked root cause + fix shape live in `.planning/debug/push-lease-pvc-pending.md` (status: root_cause_found, OPTION A picked by user over OPTION B custom StorageClass).

Purpose: kind v0.31 + kindest/node:v1.33.7 ship with the `standard` StorageClass backed by `rancher.io/local-path` (`volumeBindingMode: WaitForFirstConsumer`). PVCs only bind when a Pod consuming them is scheduled. Pod-having Layer-B fixtures (chaos-resume, etc.) trigger this naturally via Task `tide-init` Jobs. Pod-LESS fixtures (push-lease, which mocks Project.Status.Phase=Complete via direct status patch and never dispatches a Task) deadlock: the ProjectReconciler's Step 2 PVC gate at `internal/controller/project_controller.go:246` requeues forever waiting for a Bound PVC that will never bind. This helper schedules a one-shot pause Pod adjacent to the PVC, triggers the local-path provisioner to bind, then cleans up — entirely test-side, zero production change.

Output: ≤ ~55 lines added to `suite_test.go` (helper + comment block + applyYAML template + Eventually wait + best-effort delete); 1 line added to `failure_test.go` (the wire-in call). One atomic commit: `fix(test): add pvcPrewarmPod helper to bind WaitForFirstConsumer PVCs (cascade-11)`. Code-shape correctness via `go vet` + `go build` is the planning-time bar; runtime gating via `make test-int GINKGO_FOCUS='Push lease semantics'` (5–7 min isolation) + full `make test-int` (~18 min) is the verification gate documented in the Verification section.
</objective>

<execution_context>
@$HOME/.claude/get-shit-done/workflows/execute-plan.md
@$HOME/.claude/get-shit-done/templates/summary.md
</execution_context>

<context>
@CLAUDE.md
@.planning/STATE.md
@.planning/debug/push-lease-pvc-pending.md
@test/integration/kind/suite_test.go
@test/integration/kind/failure_test.go
@test/integration/kind/projects_pvc_test.go
@test/integration/kind/chaos_resume_test.go
@test/integration/kind/testdata/push-lease-project.yaml

<interfaces>
<!-- Helpers/types the executor will call. All defined in test/integration/kind/ package-locally; no new module imports beyond what suite_test.go already pulls in. -->

From test/integration/kind/suite_test.go (existing — extend, do NOT modify):
```go
// applyYAML applies a YAML string to the kind cluster via kubectl stdin.
// Returns error if kubectl fails. Idempotent (kubectl apply tolerates re-creates).
func applyYAML(yaml string) error

// ensureProjectsPVC creates the namespace-local tide-projects PVC. Idempotent.
// Defined at suite_test.go:592–594. PVC has NO storageClassName (uses kind default).
func ensureProjectsPVC(ns string)

// projectsPVCYAML returns the PVC YAML for the given namespace.
// Defined at suite_test.go:596–608. Shape: accessModes=[RWO], storage=1Gi,
// no storageClassName. assertProjectsPVCShape in projects_pvc_test.go pins
// this exact shape — DO NOT modify projectsPVCYAML in this plan.
func projectsPVCYAML(ns string) string

// ensureSigningKeySecret mirrors the helm-created tide-signing-key Secret
// into a Task namespace. Returns silently if controllerSigningKeyData fails
// (CRDs-only mode). Defined at suite_test.go:619–626.
func ensureSigningKeySecret(ns string)

// Package-level globals used by the helper:
//   - ctx           context.Context (suite-scoped; suite_test.go:104)
//   - k8sClient     client.Client   (controller-runtime client; suite_test.go:106)
//   - kubeconfigPath string         (path to kind kubeconfig; suite_test.go ~105)
```

From test/integration/kind/failure_test.go (existing — minimal edit):
```go
// createNamespace creates the Namespace + Task Job dependencies for a Layer B
// test fixture. Existing body at failure_test.go:173–184:
//   1. applyYAML(nsYAML)         // create the Namespace
//   2. ensureSubagentSA(ns)      // mirror tide-subagent SA
//   3. ensureProjectsPVC(ns)     // create namespace-local tide-projects PVC
//   4. ensureSigningKeySecret(ns) // mirror tide-signing-key Secret
// Cascade-11 wire-in: ADD pvcPrewarmPod(ns) as step 5 AFTER step 4.
func createNamespace(ns string)
```

Imports already available in suite_test.go (do NOT re-add):
- `"context"`, `"fmt"`, `"time"`, `. "github.com/onsi/gomega"` (for Eventually/Expect)
- `"sigs.k8s.io/controller-runtime/pkg/client"` (for client.ObjectKey)

Import to ADD to suite_test.go's import block (verify via grep — credproxy_test.go:49 and
chaos_resume_test.go:62 already use this alias, so the path/alias is canonical):
- `corev1 "k8s.io/api/core/v1"` — needed for `corev1.PersistentVolumeClaim{}` and `corev1.ClaimBound` constant.

Image to use: `busybox:1.36`. Already referenced at `chaos_resume_test.go:423` for the
release-writer Job; the kind cluster image cache already has it loaded for chaos-resume,
no extra `kind load docker-image` step required. (Spec allowed `registry.k8s.io/pause:3.9`
as an alternative, but busybox keeps the suite to a single non-stub external image and
matches existing precedent.) Container command: `["sleep", "60"]` — pause-equivalent
behavior; pod gets deleted as soon as PVC binds (typically within 1–3 seconds).
</interfaces>

**Cascade-11 root cause → fix shape (one-liner)**:
WaitForFirstConsumer + Pod-less Project fixture = unbreakable bind deadlock. Schedule a Pod consumer adjacent to the PVC so local-path-provisioner has a scheduling event to bind against. Helper is idempotent (no-op when PVC is already Bound) so chaos-resume/credproxy/output/up_stack — which naturally arrive at Bound via their Task Jobs — see at-worst-once redundant work (a fast PVC.Get, no Pod spin-up).

**Decision recap (locked by user — see push-lease-pvc-pending.md:170–202)**:
- OPTION A picked: test-side pre-warm Pod in suite_test.go + createNamespace wire-in. Smallest surface, zero production change, zero assertion drift.
- OPTION B rejected: custom Immediate-binding StorageClass would require updating `assertProjectsPVCShape` (which pins `pvc.Spec.StorageClassName == nil`) and touching `cluster.yaml`. Larger blast radius for the same outcome.
- OPTION C rejected: production change to controller's PVC gate is out of scope per CLAUDE.md anti-pattern (chart/controller are FIXED contracts; tests catch up to them, never reverse).

**Out of scope** (do NOT touch in this quick task):
- `projects_pvc_test.go:assertProjectsPVCShape` — the PVC YAML stays exactly as it is (no `storageClassName` added). The assertion at lines 200–210 continues to PASS unchanged.
- `internal/controller/project_controller.go` — Step 2 PVC gate is correct production behavior (Pitfall #1). Test fixture catches up.
- `charts/tide/values.yaml` — chart is FIXED contract per CLAUDE.md.
- `test/integration/kind/cluster.yaml` — leaving kind defaults intact (no custom StorageClass).
- `test/integration/kind/testdata/push-lease-project.yaml` — fixture is intentionally Pod-less to test push-Job state machine in isolation; the helper compensates structurally for that intent without modifying the fixture.
- Cascade 10 (chaos-resume second-stage failure at chaos_resume_test.go:230) — separate Phase 03 follow-up, not surfaced here.
- Running full `make test-int` during plan-time confirmation — verification is the executor's gate, isolation `GINKGO_FOCUS='Push lease semantics'` (5–7 min) FIRST, then full suite (~18 min).
</context>

<tasks>

<task type="auto">
  <name>Task 1: Add pvcPrewarmPod helper + wire into createNamespace</name>
  <files>test/integration/kind/suite_test.go, test/integration/kind/failure_test.go</files>
  <action>
This is a single atomic edit across two adjacent files. Keep it one commit (`fix(test): add pvcPrewarmPod helper to bind WaitForFirstConsumer PVCs (cascade-11)`).

**Part A — `test/integration/kind/suite_test.go`:**

1. **Import addition**: in the existing import block (top of file, lines ~33–54), ADD the line `corev1 "k8s.io/api/core/v1"` alongside the other `k8s.io/*` imports. Reference the same import alias used at credproxy_test.go:49 and chaos_resume_test.go:62 — `corev1` (lowercase, no underscore). If the import block uses goimports grouping, place it with the other `k8s.io/*` lines.

2. **Helper definition**: add `pvcPrewarmPod` IMMEDIATELY AFTER the `projectsPVCYAML` function (after line 608 — i.e. between `projectsPVCYAML`'s closing brace and `ensureSigningKeySecret`'s leading comment at line 611). The helper goes adjacent to the other PVC helper for code-locality reasons.

   Helper requirements (do NOT inline implementation code in this plan — author it in the executor pass; this list is the contract):
   - **Doc-comment**: 7–10-line `//` comment block ABOVE the function. Mirror the tone of `ensureProjectsPVC` and `ensureSigningKeySecret` doc-comments: state what the helper does (one-shot pause Pod mounting tide-projects PVC, wait for Bound, delete Pod), WHY it's needed (kind's WaitForFirstConsumer default + Pod-less push-lease fixture), idempotency contract (no-op if already Bound), and a pointer to `.planning/debug/push-lease-pvc-pending.md` for the full root-cause analysis (cascade-11). Use `GinkgoWriter.Printf` (existing precedent at line 622) for any informational logging, NOT `fmt.Printf`.
   - **Signature**: `func pvcPrewarmPod(ns string)` — no return value, no parameters beyond the namespace. Match the shape of `ensureProjectsPVC` / `ensureSubagentSA` / `ensureSigningKeySecret`.
   - **Step 1 — idempotency check**: use the package-level `k8sClient` (suite_test.go:106) to `Get` the `tide-projects` PVC in the namespace. Decode into `corev1.PersistentVolumeClaim`. If `pvc.Status.Phase == corev1.ClaimBound`, log a one-line `GinkgoWriter.Printf("pvcPrewarmPod: tide-projects PVC in namespace %q already Bound; skipping prewarm\n", ns)` and `return`. If the Get returns an error (PVC not yet visible in cache), do NOT fail the helper — log a `GinkgoWriter.Printf` warning and proceed to step 2 (the Pod create + Eventually will recover).
   - **Step 2 — Pod create via applyYAML**: build a Pod YAML via `fmt.Sprintf(`...`, ns)` following the `projectsPVCYAML` template precedent at suite_test.go:596–608. Pod shape:
     ```
     apiVersion: v1
     kind: Pod
     metadata:
       name: tide-projects-prewarm
       namespace: <ns>
     spec:
       restartPolicy: Never
       containers:
         - name: pause
           image: busybox:1.36
           command: ["sleep", "60"]
       volumes:
         - name: workspace
           persistentVolumeClaim:
             claimName: tide-projects
     ```
     Note the container omits `volumeMounts` — a Pod with `spec.volumes` referencing a PVC is sufficient to trigger the local-path-provisioner to bind. (Mounting the volume in the container is unnecessary for the bind side-effect and adds noise.) However, if a future K8s admission policy requires the volume to be mounted, the executor MAY add a `volumeMounts: [{name: workspace, mountPath: /workspace}]` block — this is implementation discretion within the helper, not a contract change.
     Apply via `_ = applyYAML(podYAML)` matching the existing `ensureProjectsPVC` pattern (suite_test.go:593). Discard the error: applyYAML's error path is exercised by the Eventually wait that follows.

   - **Step 3 — Eventually wait for Bound**: poll the PVC status using Gomega's `Eventually` matcher (already imported as a dot-import at suite_test.go:44 via `. "github.com/onsi/gomega"`). Pattern (match existing Eventually shape at chaos_resume_test.go:260–266, but for PVC.Status.Phase):
     ```
     Eventually(func() corev1.PersistentVolumeClaimPhase {
         pvc := &corev1.PersistentVolumeClaim{}
         if err := k8sClient.Get(ctx, client.ObjectKey{
             Name:      "tide-projects",
             Namespace: ns,
         }, pvc); err != nil {
             return ""
         }
         return pvc.Status.Phase
     }, 60*time.Second, time.Second).Should(Equal(corev1.ClaimBound),
         "tide-projects PVC in namespace %q must reach Bound after pause-Pod scheduled", ns)
     ```
     60s timeout matches the spec's recommended bound (push-lease-pvc-pending.md:181 "Risks: low" + the existing 90s `waitForPushJob` timeout in push_lease_test.go gives headroom). 1s poll interval matches existing precedents.

   - **Step 4 — Pod cleanup (best-effort)**: delete the pause Pod once PVC is Bound. Two equally acceptable shapes (executor's choice):
     - **a) controller-runtime client**: `pod := &corev1.Pod{}; pod.Name = "tide-projects-prewarm"; pod.Namespace = ns; _ = k8sClient.Delete(ctx, pod)` — log warning on err but don't fail.
     - **b) kubectl shell-out**: `cmd := exec.CommandContext(ctx, "kubectl", "--kubeconfig", kubeconfigPath, "delete", "pod", "tide-projects-prewarm", "-n", ns, "--grace-period=0", "--force"); _ = cmd.Run()` — match the existing `controllerSigningKeyData` kubectl pattern at suite_test.go:629–632.

     Recommended: **a)** — keeps the helper in controller-runtime idiom and avoids exec'ing a child process per namespace. Either way, errors are LOGGED to GinkgoWriter and NOT propagated; the AfterEach deleteNamespace will clean up any orphans. The pause Pod's `sleep 60` ensures it exits on its own even if our explicit delete is skipped or fails — the namespace's GC handles the rest at AfterEach.

3. **No other changes to suite_test.go**. Do NOT modify `projectsPVCYAML` (the PVC stays storageClassName-less). Do NOT modify `ensureProjectsPVC`, `ensureSigningKeySecret`, or `ensureSubagentSA`. Do NOT touch the kindTestTimeout block (Cascade 9's quick task already scrubbed the stale SKIP env-var reference).

**Part B — `test/integration/kind/failure_test.go`:**

4. **Wire-in**: in `createNamespace` (lines 173–184), ADD `pvcPrewarmPod(ns)` as the LAST line of the function body — AFTER `ensureSigningKeySecret(ns)` (line 183) and BEFORE the closing `}` (line 184). Final body shape (5 lines instead of 4):
   ```
   _ = applyYAML(nsYAML)
   ensureSubagentSA(ns)
   ensureProjectsPVC(ns)
   ensureSigningKeySecret(ns)
   pvcPrewarmPod(ns)
   ```
   No new imports needed in failure_test.go — `pvcPrewarmPod` is package-local.

5. **Optional: append a 1-line comment** at the call site: `// Cascade-11: pre-bind WaitForFirstConsumer PVC for Pod-less fixtures (push-lease)`. Single line, matching the existing comment terseness in failure_test.go's helpers. NOT required; executor discretion.

6. **No other changes to failure_test.go**. Do NOT modify `makeKindTask` or any other helper.

**Anti-pattern guard rails (read this before editing)**:
- Do NOT add `storageClassName` to `projectsPVCYAML` — `projects_pvc_test.go:assertProjectsPVCShape:200–210` will fail if you do. Verify via `grep -c 'storageClassName' test/integration/kind/suite_test.go` returning `0` post-edit.
- Do NOT inline the Pod creation in createNamespace itself — keep the helper in suite_test.go so other test files (push_lease_test.go BeforeEach, if a future cascade ever needs it directly) can call it independently. The wire-in via `createNamespace` is the primary path; the helper as a discrete function is the contract.
- Do NOT touch `internal/controller/project_controller.go`. Per CLAUDE.md Working Rule #1 (Observe First) and the locked diagnosis in push-lease-pvc-pending.md, the production gate at line 246 is correct — Pitfall #1 says don't dispatch Pods without a Bound PVC. The test fixture is what's out of sync, and this plan brings it back into sync.
- Do NOT add `corev1` import to failure_test.go — the wire-in is a parameterless function call from existing package-local code.
- Do NOT remove or modify the existing `_ = applyYAML(nsYAML)` discard pattern in createNamespace — failure_test.go intentionally discards applyYAML errors at namespace-create time (idempotent, AfterEach handles cleanup).

Commit message stub: `fix(test): add pvcPrewarmPod helper to bind WaitForFirstConsumer PVCs (cascade-11)`.
  </action>
  <verify>
    <automated>cd /Users/justinsearles/Projects/tide && go vet ./test/integration/kind/... && go build ./test/integration/kind/... && [ "$(grep -c 'func pvcPrewarmPod' test/integration/kind/suite_test.go)" = "1" ] && [ "$(grep -c 'pvcPrewarmPod(ns)' test/integration/kind/failure_test.go)" = "1" ] && [ "$(grep -v '^#' test/integration/kind/suite_test.go | grep -c storageClassName)" = "0" ] && grep -qE '^\s*corev1 "k8s.io/api/core/v1"' test/integration/kind/suite_test.go</automated>
  </verify>
  <done>
- `go vet ./test/integration/kind/...` passes (exit 0).
- `go build ./test/integration/kind/...` passes (exit 0).
- `grep -c 'func pvcPrewarmPod' test/integration/kind/suite_test.go` returns exactly `1`.
- `grep -c 'pvcPrewarmPod(ns)' test/integration/kind/failure_test.go` returns exactly `1` (the call site in createNamespace).
- `grep -v '^#' test/integration/kind/suite_test.go | grep -c storageClassName` returns `0` (PVC YAML in projectsPVCYAML is unchanged — confirms OPTION B not accidentally implemented).
- `grep -qE '^\s*corev1 "k8s.io/api/core/v1"' test/integration/kind/suite_test.go` succeeds (the new import is wired).
- Diff scope: ONLY `test/integration/kind/suite_test.go` (~50 net lines added: 1 import + 7–10-line doc comment + ~35–40-line helper body) and `test/integration/kind/failure_test.go` (1 net line added: the wire-in call, plus optionally 1 comment line).
- One atomic commit on `main` (or the active worktree branch if the executor is in a worktree): `fix(test): add pvcPrewarmPod helper to bind WaitForFirstConsumer PVCs (cascade-11)`.
  </done>
</task>

</tasks>

<verification>
**Runtime gate** (executor runs this AFTER the code-shape commit lands and `go vet` + `go build` are green — per CLAUDE.md Working Rule #3 "Verify Before Claiming"):

**Stage 1 — Isolation run (~5–7 min, PRIMARY gate)**:
```bash
cd /Users/justinsearles/Projects/tide
make test-int GINKGO_FOCUS='Push lease semantics' 2>&1 | tee /tmp/cascade-11-isolation.log
```
Expected post-fix shape:
- All 4 push-lease specs PASS.
- Manager log shows ZERO `"shared PVC not yet Bound; requeueing"` lines for `push-lease-test` namespace across the entire run (the prewarm Pod schedules first → local-path-provisioner binds the PVC → Step 2 gate opens immediately).
- The `tide-projects-prewarm` Pod is visible in `kubectl get pods -n push-lease-test` briefly (~1–3s) during each test's namespace-create phase, then disappears after the helper's explicit Delete (or the AfterEach namespace deletion).
- Total isolation wall-time: ≤ 7 min (vs. 4×90s = 360s timeouts + cleanup = ~7 min wasted in cascade-11 reproduction).

Grep gates on the isolation log (Working Rule #3 protocol):
```bash
grep -cE '\[FAIL\].*Push lease semantics' /tmp/cascade-11-isolation.log  # must be 0
grep -cE '"shared PVC not yet Bound".*"push-lease-test"' /tmp/cascade-11-isolation.log  # must be 0
grep -cE 'Ran .* of .* Specs' /tmp/cascade-11-isolation.log  # must be 1 (Ginkgo summary line present)
grep -E 'Passed|Failed|Skipped' /tmp/cascade-11-isolation.log | tail -1  # expect "Passed | 0 Failed" pattern
```

**Stage 2 — Full suite run (~18 min, REGRESSION gate, only if Stage 1 passes)**:
```bash
make test-int 2>&1 | tee /tmp/cascade-11-fullsuite.log
```
Expected post-fix shape:
- All 13 specs PASS (no SKIPs because cascade-9 removed the push-lease gate; no FAILs).
- chaos-resume still PASSES (no regression — the prewarm helper is a no-op there because chaos-resume's tide-init Job arrives within the same ~1s window as the prewarm Pod and the idempotency check skips the second-place arrival).
- Full-suite wall-time: ≤ 18 min (matches the iter-5 kindTestTimeout budget).

Grep gates on the full-suite log:
```bash
grep -cE '\[FAIL\]' /tmp/cascade-11-fullsuite.log  # must be 0
grep -E 'Passed|Failed|Skipped' /tmp/cascade-11-fullsuite.log | tail -1  # expect 13 Passed | 0 Failed | 0 Skipped
grep -cE '"shared PVC not yet Bound"' /tmp/cascade-11-fullsuite.log  # must be 0 across all test namespaces
```

**If Stage 1 PASSES but Stage 2 FAILS** with a NEW cascade class: the cascade-11 fix is correct (push-lease binds), but a new pre-existing latent issue surfaced. Open a new debug session per CLAUDE.md Working Rule #1 (Observe First → read manager log + VERIFICATION frontmatter BEFORE hypothesizing). Do NOT claim cascade-11 "still broken" — verify which class of failure is new.

**If Stage 1 FAILS** (push-lease specs still timeout): the prewarm Pod isn't reaching scheduled state, OR the Eventually wait is firing before the PVC actually binds. Capture:
```bash
kubectl --context kind-tide-test describe pvc tide-projects -n push-lease-test
kubectl --context kind-tide-test describe pod tide-projects-prewarm -n push-lease-test
kubectl --context kind-tide-test get events -n push-lease-test --sort-by='.lastTimestamp'
```
Then route back to `.planning/debug/push-lease-pvc-pending.md` to extend the Eliminated Hypotheses list with the new observation.

**Cluster-state captures during the run (optional debugging aid, from a second shell)**:
```bash
kubectl --context kind-tide-test get pvc -A -w &
kubectl --context kind-tide-test get pv -A -w &
kubectl --context kind-tide-test get pods -A -w | grep prewarm &
```
</verification>

<success_criteria>
1. `go vet ./test/integration/kind/...` and `go build ./test/integration/kind/...` both exit 0 immediately after the edits land.
2. `grep -c 'func pvcPrewarmPod' test/integration/kind/suite_test.go` → exactly `1`.
3. `grep -c 'pvcPrewarmPod(ns)' test/integration/kind/failure_test.go` → exactly `1`.
4. `grep -v '^#' test/integration/kind/suite_test.go | grep -c storageClassName` → exactly `0` (no OPTION-B drift).
5. `grep -qE '^\s*corev1 "k8s.io/api/core/v1"' test/integration/kind/suite_test.go` succeeds (import added).
6. `projects_pvc_test.go` continues to PASS — verify via `go test ./test/integration/kind/ -run TestAssertProjectsPVCShape` (table-driven Go test, fast, no kind cluster needed).
7. Runtime: `make test-int GINKGO_FOCUS='Push lease semantics'` (Stage 1) shows 4/4 push-lease specs PASS, zero `"shared PVC not yet Bound"` log lines in push-lease-test namespace.
8. Runtime: `make test-int` (Stage 2) shows 13/13 specs PASS, including chaos-resume (no regression).
9. Diff scope: ONLY the two named files modified. No production code (`internal/controller/`), no chart files (`charts/`), no other test files (`test/integration/kind/projects_pvc_test.go`, `push_lease_test.go`, etc.) touched.
10. Single atomic commit on the active branch: `fix(test): add pvcPrewarmPod helper to bind WaitForFirstConsumer PVCs (cascade-11)`.
</success_criteria>

<output>
After execution, create `.planning/quick/260521-gmm-phase-03-cascade-11-pvcprewarmpod-helper/260521-gmm-SUMMARY.md` capturing:

- **Decision**: OPTION A (test-side pre-warm Pod) over OPTION B (custom Immediate-binding StorageClass) — link to `.planning/debug/push-lease-pvc-pending.md` Resolution section.
- **Diff stat**: actual net line counts in suite_test.go and failure_test.go (expect ~50 + ~1).
- **Image choice**: `busybox:1.36` (matches chaos_resume_test.go:423 precedent; already in kind cluster cache; no extra `kind load docker-image` step).
- **Cleanup model**: controller-runtime `k8sClient.Delete` (recommended) vs kubectl shell-out (alternate) — note which the executor used.
- **Stage 1 isolation result**: full Ginkgo summary line (`Ran N of N Specs in Xs`), zero "shared PVC not yet Bound" log lines for push-lease-test, wall-time.
- **Stage 2 full-suite result**: full Ginkgo summary line, regression status for chaos-resume + the other 8 Layer-B specs, wall-time.
- **Cascade-11 closure check** (per CLAUDE.md Working Rule #3): the specific manager-log error that defined cascade-11 (`"shared PVC not yet Bound; requeueing"` for `push-lease-test` namespace) returns 0 in the new run's log.
- **Any new cascade classes surfaced** in Stage 2 (if Stage 1 passed but Stage 2 found a NEW failure shape, document it here and open a follow-up debug doc — do NOT close cascade-11 prematurely).
- **Outstanding follow-ups** (none expected from this plan; cascade-10 chaos-resume second-stage remains in its own debug doc, NOT this plan's surface area).
</output>
