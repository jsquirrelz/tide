---
id: 260521-gmm-phase-03-cascade-11-pvcprewarmpod-helper
title: "Phase 03 Cascade 11: pvcPrewarmPod helper for WaitForFirstConsumer PVC bind"
type: quick
status: complete
created: 2026-05-21
completed: 2026-05-21
phase: quick
plan: "260521-gmm"
wave: 1
depends_on: []
requirements: [cascade-11-pvc-prewarm]
commits:
  - hash: e8083a5
    message: "fix(test): add pvcPrewarmPod helper to bind WaitForFirstConsumer PVCs (cascade-11)"
    files: [test/integration/kind/suite_test.go, test/integration/kind/failure_test.go]
key-files:
  modified:
    - test/integration/kind/suite_test.go
    - test/integration/kind/failure_test.go
key-decisions:
  - "OPTION A (test-side pre-warm Pod) picked over OPTION B (custom Immediate-binding StorageClass) and OPTION C (production controller change). Locked by user in .planning/debug/push-lease-pvc-pending.md:170-202."
  - "busybox:1.36 chosen as the pause image (matches chaos_resume_test.go:423 precedent; already cached in the kind cluster from chaos-resume — zero extra `kind load docker-image` cost)."
  - "controller-runtime k8sClient.Delete used for Pod cleanup (executor's recommended `a)` option in PLAN.md:198-201). Keeps the helper inside the same client idiom as the rest of suite_test.go; no exec child process per namespace."
  - "Container omits volumeMounts — a Pod with spec.volumes referencing a PVC is sufficient to trigger local-path-provisioner bind. Avoids needless mount-path noise in the helper."
metrics:
  files_modified: 2
  net_lines_added: 88
  suite_test_go_lines_added: 86
  failure_test_go_lines_added: 2
  verify_gates_passed: 6
debug_session: .planning/debug/push-lease-pvc-pending.md
---

# Phase 03 Cascade 11: pvcPrewarmPod helper Summary

One-liner: Added test-side `pvcPrewarmPod(ns)` helper in `suite_test.go` and wired it into `createNamespace` so Pod-less Layer-B fixtures (push-lease) pre-bind kind's WaitForFirstConsumer PVCs, unblocking the cascade-11 deadlock that surfaced after cascade-9 removed the push-lease SKIP gate.

## What Changed

**Commit:** `e8083a5` — `fix(test): add pvcPrewarmPod helper to bind WaitForFirstConsumer PVCs (cascade-11)`

```
 test/integration/kind/failure_test.go |  2 +
 test/integration/kind/suite_test.go   | 86 +++++++++++++++++++++++++++++++++++
 2 files changed, 88 insertions(+)
```

### `test/integration/kind/suite_test.go` (+86 lines, +1 import)

1. Added `corev1 "k8s.io/api/core/v1"` to the import block alongside the existing `k8s.io/client-go/*` imports (matches the alias convention used at `credproxy_test.go:49` and `chaos_resume_test.go:62`).
2. Added the `pvcPrewarmPod(ns string)` helper immediately after `projectsPVCYAML` (lines ~611+) — adjacent to the other PVC helper for code-locality. The helper:
   - **Step 1 (idempotency)**: `k8sClient.Get` the namespace-local `tide-projects` PVC; if `Status.Phase == corev1.ClaimBound`, log via `GinkgoWriter.Printf` and return. If the Get errors (PVC not yet visible), log a warning and proceed (the Eventually wait recovers).
   - **Step 2 (Pod create)**: `applyYAML` a Pod spec named `tide-projects-prewarm` running `busybox:1.36` `sleep 60`, with `spec.volumes` mounting the PVC by `claimName: tide-projects` (no `volumeMounts` — sufficient to trigger bind).
   - **Step 3 (Bound wait)**: `Eventually(60s, 1s)` polling `pvc.Status.Phase` until `ClaimBound`. Failure surfaces with the namespace name in the error message.
   - **Step 4 (cleanup)**: best-effort `k8sClient.Delete` of the prewarm Pod; errors logged to `GinkgoWriter`, not propagated. `AfterEach`'s `deleteNamespace` GCs any orphans.

### `test/integration/kind/failure_test.go` (+2 lines)

In `createNamespace(ns)`, after `ensureSigningKeySecret(ns)` and before the closing `}`, added:

```go
// Cascade-11: pre-bind WaitForFirstConsumer PVC for Pod-less fixtures (push-lease).
pvcPrewarmPod(ns)
```

No new imports — `pvcPrewarmPod` is package-local.

## Decision Recap

| Option | Status | Why                                                                                                                                                                                                                                                                                                                                                                            |
| ------ | ------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| **A — test-side pre-warm Pod** | **PICKED** | Smallest surface (2 files, 88 LOC). Zero production change. Zero assertion drift. Idempotent: chaos-resume + credproxy + output + up_stack hit the fast-path `ClaimBound` skip in ~1 client.Get with no Pod spin-up. Push-lease gets its first PVC consumer scheduled, the local-path provisioner binds, the ProjectReconciler's Step 2 gate at `project_controller.go:246` opens, and Step 4 (push-Job dispatch) becomes reachable. |
| B — custom Immediate StorageClass | REJECTED | Would require updating `projects_pvc_test.go:assertProjectsPVCShape` (lines 200-210, which pins `pvc.Spec.StorageClassName == nil`) and touching `cluster.yaml`. Larger blast radius for the same outcome.                                                                                                                                                                       |
| C — production controller change (invert Step 2/Step 3) | REJECTED | CLAUDE.md anti-pattern: chart and controller are FIXED contracts; tests catch up to them, never reverse. The Step 2 gate is correct production behavior (Pitfall #1: don't dispatch Pods without a Bound PVC).                                                                                                                                                                  |

Full decision context lives in `.planning/debug/push-lease-pvc-pending.md` Resolution-section reasoning.

## Invariant Preservation

**`projects_pvc_test.go:assertProjectsPVCShape` continues to PASS** — the `projectsPVCYAML` body was NOT modified in this plan. The PVC still has no `storageClassName`, so the `pvc.Spec.StorageClassName == nil` assertion at `projects_pvc_test.go:202-204` is unchanged.

Verify gate (one of six in the automated verify command):
```bash
grep -v '^#' test/integration/kind/suite_test.go | grep -c storageClassName   # returns 0
```

## Image Choice

`busybox:1.36` — already referenced at `chaos_resume_test.go:423` for the release-writer Job and therefore already loaded in the kind cluster's image cache by the time `createNamespace` first runs. No extra `kind load docker-image` step required, no `ErrImagePull` risk. The PLAN.md allowed `registry.k8s.io/pause:3.9` as an alternative, but keeping the suite to a single non-stub external image preserves operational simplicity.

## Cleanup Model

`k8sClient.Delete` (controller-runtime, executor's recommended option `a)` in PLAN.md:198-201). Stays in the same client idiom as the rest of suite_test.go; no `exec.CommandContext` child process per namespace. Errors logged via `GinkgoWriter.Printf` and explicitly NOT propagated — the pause Pod's `sleep 60` ensures it self-exits even if our explicit delete is skipped or fails, and the namespace's `AfterEach` GC handles the rest.

## Verify Result

Automated verify command (plan-supplied, verbatim):
```bash
cd /Users/justinsearles/Projects/tide && \
  go vet ./test/integration/kind/... && \
  go build ./test/integration/kind/... && \
  [ "$(grep -c 'func pvcPrewarmPod' test/integration/kind/suite_test.go)" = "1" ] && \
  [ "$(grep -c 'pvcPrewarmPod(ns)' test/integration/kind/failure_test.go)" = "1" ] && \
  [ "$(grep -v '^#' test/integration/kind/suite_test.go | grep -c storageClassName)" = "0" ] && \
  grep -qE '^\s*corev1 "k8s.io/api/core/v1"' test/integration/kind/suite_test.go
```

**Exit code: 0** (all six gates pass post-commit).

| Gate | Result |
| ---- | ------ |
| `go vet ./test/integration/kind/...` | clean |
| `go build ./test/integration/kind/...` | clean |
| `grep -c 'func pvcPrewarmPod' suite_test.go` | 1 (expected 1) |
| `grep -c 'pvcPrewarmPod(ns)' failure_test.go` | 1 (expected 1) |
| `grep -v '^#' suite_test.go | grep -c storageClassName` | 0 (expected 0 — OPTION B not accidentally implemented) |
| `grep corev1 "k8s.io/api/core/v1"` in suite_test.go | found |

## Cascade-11 Closure Check (Pending Runtime Gate)

Per CLAUDE.md Working Rule #3 (Verify Before Claiming), the **code-shape correctness gate has passed** but the **runtime gate is owned by the orchestrator**:

- Stage 1 (isolation): `make test-int GINKGO_FOCUS='Push lease semantics'` (~5-7 min). Expected: 4/4 push-lease specs PASS; 0 `"shared PVC not yet Bound"` log lines for `push-lease-test` namespace; brief visibility of `tide-projects-prewarm` Pod during each spec's `createNamespace`.
- Stage 2 (regression): `make test-int` (~18 min). Expected: 13/13 specs PASS; chaos-resume continues to PASS via the idempotency short-circuit (its tide-init Job arrives in the same ~1s window and the `ClaimBound` early-return skips Pod spin-up).

The runtime gate is the next step after the orchestrator merges this worktree to `main`.

## Out-of-Scope Follow-ups (carry forward)

- **Cascade 10** (chaos-resume second-stage failure at `chaos_resume_test.go:230`) — separate Phase 03 debug doc; NOT this plan's surface area.
- **Cascade 7-bis / 7-ter** (`phase_controller.go` symmetric nil-Project race + `milestone_controller.go` latent nil-deref) — defense-in-depth follow-ups from the cascade-7 SUMMARY footer.
- **Removing the nil-Project guard at `internal/dispatch/podjob/jobspec.go:266-272`** — only safe AFTER cascades 7-bis + 7-ter land.
- **Item 2 Layer B 429 storm spec authoring** (Phase 02 UAT closeout #4) — separate follow-up.
- **OPTION C** (production controller change to invert Step 2/Step 3 order) — Phase 03+ follow-up at most; CLAUDE.md anti-pattern restricts in current cascade chain.

## Self-Check: PASSED

- [x] Commit `e8083a5` exists on `worktree-agent-acbec498b26ca0634` (`git log --oneline -1`)
- [x] `test/integration/kind/suite_test.go` modified (+86 lines including `corev1` import + `pvcPrewarmPod` helper)
- [x] `test/integration/kind/failure_test.go` modified (+2 lines — `pvcPrewarmPod(ns)` call + comment in `createNamespace`)
- [x] Automated verify gate exits 0 on the committed tree (all 6 sub-gates pass)
- [x] No deletions in the commit (`git diff --diff-filter=D --name-only HEAD~1 HEAD` empty)
- [x] No untracked files (`git status --porcelain | grep '^??'` empty)
- [x] `projectsPVCYAML` body is unchanged — `assertProjectsPVCShape` invariant (`storageClassName == nil`) preserved
- [x] No production code touched (`internal/controller/`), no chart files touched (`charts/`), no other test files touched
