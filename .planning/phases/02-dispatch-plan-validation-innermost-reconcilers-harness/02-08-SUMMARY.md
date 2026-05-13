---
phase: 02-dispatch-plan-validation-innermost-reconcilers-harness
plan: 8
subsystem: dispatch
tags: [kubernetes, jobs, podjob, credproxy, sidecar, envelope, pvc, dispatch]

# Dependency graph
requires:
  - phase: 02-01
    provides: "pkg/dispatch envelope types (EnvelopeIn, EnvelopeOut)"
  - phase: 02-05
    provides: "internal/credproxy image and cert mount design"
  - phase: 02-06
    provides: "internal/harness envelope-out write contract"

provides:
  - "internal/dispatch.Dispatcher interface body (Run method replacing Phase 1 interface{})"
  - "internal/dispatch/podjob.JobName() deterministic dedup key (SUB-03)"
  - "internal/dispatch/podjob.BuildJobSpec() two-container Pod topology builder"
  - "internal/dispatch/podjob.PodJobBackend K8s Job dispatch backend"
  - "internal/dispatch/podjob.EnvelopeReader interface and FilesystemEnvelopeReader"
  - "Exported constants: ContainerNameEnvelopeWriter, ContainerNameCredproxy, ContainerNameSubagent, VolumeProjectWorkspace, VolumeCertShared, ServiceAccountSubagent"

affects:
  - phase: 02-09
    reason: "Plan 09 TaskReconciler calls BuildJobSpec + JobName directly; exports all needed constants"
  - phase: 02-12
    reason: "Plan 12 wires PodJobBackend as the concrete Dispatcher injected into reconcilers"
  - phase: 02-13
    reason: "Plan 13 integration tests use fakeEnvReader pattern and podjob constants"
  - phase: 02-07
    reason: "Plan 07 PreCharge depends on tideproject.k8s/provider-secret-uid label stamped by BuildJobSpec"

# Tech tracking
tech-stack:
  added:
    - "k8s.io/utils/ptr — ptr.To[T] helper for K8s pointer-typed fields (int32, int64, bool, ContainerRestartPolicy)"
    - "k8s.io/apimachinery/pkg/util/intstr — IntOrString for readiness probe port"
    - "k8s.io/apimachinery/pkg/util/wait — PollUntilContextTimeout for Job terminal state polling"
  patterns:
    - "K8s 1.33 native sidecar: initContainer with RestartPolicy: Always (ContainerRestartPolicyAlways)"
    - "Idempotent Job Create: apierrors.IsAlreadyExists treated as success (Pitfall F)"
    - "Single shared PVC with kubelet-enforced subPath per Project (Blocker #2/#3)"
    - "EnvelopeReader interface injection for testability vs FilesystemEnvelopeReader for production"
    - "TDD RED/GREEN pattern: tests written first, failing, then implementation added"

key-files:
  created:
    - "internal/dispatch/dispatcher.go — Dispatcher interface with Run method (replaces Phase 1 placeholder)"
    - "internal/dispatch/podjob/doc.go — package doc covering SUB-02, SUB-03, PVC architecture"
    - "internal/dispatch/podjob/names.go — JobName() deterministic dedup key helper"
    - "internal/dispatch/podjob/names_test.go — 5 table-driven TestJobName subtests"
    - "internal/dispatch/podjob/jobspec.go — BuildJobSpec() with full two-container topology"
    - "internal/dispatch/podjob/jobspec_test.go — 12 TestBuildJobSpec subtests (incl. D-C4 + sidecar gates)"
    - "internal/dispatch/podjob/backend.go — PodJobBackend, EnvelopeReader, FilesystemEnvelopeReader"
    - "internal/dispatch/podjob/backend_test.go — 4 TestPodJobBackend subtests with fake client"
  modified:
    - "internal/dispatch/doc.go — shrunk to one-line cross-reference (Phase 1 placeholder content moved)"

key-decisions:
  - "K8s 1.33 native sidecar (initContainer + RestartPolicy: Always) confirmed as the credproxy sidecar pattern — no sidecar CRD or annotation needed; the RestartPolicy field is the K8s 1.33 marker"
  - "Single shared tide-projects RWX PVC with subPath {project-uid}/workspace per Project (Blocker #2/#3 resolution) — kubelet enforces isolation; no per-Project PVC provisioning"
  - "EnvelopeIn delivered via base64-encoded env var to envelope-writer init container — keeps Manager write-set narrow (Manager only reads out.json, never writes in.json)"
  - "PodJobBackend.Run is NOT the Phase 2 executor path — TaskReconciler (Plan 09) handles Job create + Owns-watch directly; Run is for test fixtures and Phase 3 planner dispatch"
  - "apierrors.IsAlreadyExists treated as success in Run (Pitfall F) — same idempotency contract TaskReconciler must maintain"
  - "busybox:stable image for envelope-writer init container — no additional image build needed; thin shell decode + write"
  - "subagent container has ReadOnlyRootFilesystem: false (subagent writes /workspace) while credproxy has ReadOnlyRootFilesystem: true"

patterns-established:
  - "Pattern: D-C4 secret isolation enforced at BuildJobSpec layer — subagent container has zero EnvFrom secretRefs; only credproxy gets the provider secret via envFrom + tide-signing-key"
  - "Pattern: Four labels stamped on every Job: task-uid, attempt, provider-secret-uid, role=executor — Plan 07 PreCharge depends on provider-secret-uid label"
  - "Pattern: EnsureOwnerRef called by PodJobBackend.Run AFTER BuildJobSpec returns — honors Phase 1 helper-package-usage rule from PATTERNS.md"
  - "Pattern: isJobTerminal checks JobComplete and JobFailed conditions — handles both success and failure in the poll loop"
  - "Pattern: No Apache headers on internal helper packages (dispatcher.go, podjob/*.go) — consistent with Phase 1 leaf-style convention"

requirements-completed: [SUB-02, SUB-03, ART-01]

# Metrics
duration: 35min
completed: 2026-05-13
---

# Phase 2 Plan 8: Dispatch Backend Summary

**K8s Job dispatch backend with native sidecar topology — deterministic Job names (SUB-03), D-C4 secret isolation, and single shared PVC with per-Project subPath (Blocker #2/#3 resolved)**

## Performance

- **Duration:** ~35 min
- **Started:** 2026-05-13T03:00Z (approx)
- **Completed:** 2026-05-13T03:38Z
- **Tasks:** 3/3 completed
- **Files modified/created:** 9

## Accomplishments

- Replaced Phase 1's `type Dispatcher interface{}` placeholder with the real `Run(ctx, EnvelopeIn) (EnvelopeOut, error)` body, preserving all Phase 1 reconciler struct field types unchanged
- Built `BuildJobSpec` — the two-container Pod topology (envelope-writer init + credproxy native sidecar + subagent main) with all security contracts: D-C4 isolation, D-G3 UIDs, D-B5 deterministic names, Blocker #2/#3 shared PVC + subPath architecture
- Built `PodJobBackend.Run` satisfying `dispatch.Dispatcher` with idempotent Job create (AlreadyExists = success per Pitfall F / SUB-03), polling watch loop, and injectable `EnvelopeReader` for testability

## The Four Job Labels

Every Job stamped with:
- `tideproject.k8s/task-uid` — correlates Job to Task across reconciler watch events
- `tideproject.k8s/attempt` — dedup key component (paired with JobName)
- `tideproject.k8s/provider-secret-uid` — Plan 07 PreCharge uses this to find active Jobs at Manager restart
- `tideproject.k8s/role=executor` — selector for executor-pool label queries

## Envelope-Writer Init Container Approach

EnvelopeIn JSON is delivered to the Task pod via a `busybox:stable` init container (not by the Manager writing directly to the PVC). The Manager base64-encodes the JSON into an env var; the init container decodes and writes to `/workspace/envelopes/{taskUID}/in.json`.

This keeps the Manager write-set narrow: the Manager ONLY reads `out.json` from its `/workspaces/{project-uid}/workspace/envelopes/{taskUID}/out.json` path. The Manager never writes to the PVC. The init container writes `in.json` using the same shared PVC, just from the subagent pod side via subPath mount.

## Shared PVC + subPath Architecture (Blocker #2/#3)

- **PVC name:** `tide-projects` — single chart-provisioned RWX PVC, shared across all Projects
- **Task pod mount:** `subPath: {project-uid}/workspace` at `/workspace` — kubelet enforces per-Project isolation
- **Manager pod mount:** no subPath at `/workspaces` — reads `/workspaces/{project-uid}/workspace/envelopes/{taskUID}/out.json`
- **envelope-writer subPath:** same `{project-uid}/workspace` so init container writes to the same per-Project slice

## Native Sidecar Wiring (K8s 1.33)

`tide-credproxy` init container has:
```go
RestartPolicy: ptr.To(corev1.ContainerRestartPolicyAlways)
```

This single field is the K8s 1.33 native-sidecar marker. The Job completes when the main `subagent` container exits; kubelet then terminates `tide-credproxy` in reverse spec order. No CRD, no annotation, no special scheduler needed.

## D-C4 Secret Isolation Contract

The load-bearing security gate `TestBuildJobSpec_SubagentDoesNotReceiveProviderSecret_envFrom` verifies that the subagent container's `EnvFrom` does NOT contain the Project's `providerSecretRef`. Only `tide-credproxy` gets `envFrom: [{secretRef: tide-signing-key}, {secretRef: <providerSecretRef>}]`. The subagent receives only the HMAC-signed token as `ANTHROPIC_API_KEY` and `ANTHROPIC_AUTH_TOKEN`.

## Exported Constants

Plan 09 and Plan 13 reference these by name (not string literals):

| Constant | Value |
|----------|-------|
| `ContainerNameEnvelopeWriter` | `"envelope-writer"` |
| `ContainerNameCredproxy` | `"tide-credproxy"` |
| `ContainerNameSubagent` | `"subagent"` |
| `VolumeProjectWorkspace` | `"project-workspace"` |
| `VolumeCertShared` | `"cert-shared"` |
| `ServiceAccountSubagent` | `"tide-subagent"` |
| `DefaultWallClockGraceSeconds` | `60` |
| `DefaultTTLSecondsAfterFinished` | `600` |

## Run Method Role

`PodJobBackend.Run` is NOT the Phase 2 executor path. The TaskReconciler (Plan 09):
1. Calls `BuildJobSpec` + `client.Create` directly inside `ensureJob` (sync, fast, from Reconcile)
2. Receives Owns-watch events when the Job reaches terminal state via `handleJobCompletion`

`Run` is exposed for:
- Unit/integration test fixtures (simpler single-call API than split reconciler flow)
- Phase 3+ planner-dispatch callers running in goroutines outside Reconcile

## Task Commits

1. **Task 1: Dispatcher interface body + JobName helper** - `43b77de` (feat)
2. **Task 2: BuildJobSpec two-container topology** - `3038a98` (feat)
3. **Task 3: PodJobBackend.Run** - `f15fd95` (feat)

## Files Created/Modified

- `internal/dispatch/doc.go` — shrunk to one-line cross-reference
- `internal/dispatch/dispatcher.go` — real Dispatcher interface body
- `internal/dispatch/podjob/doc.go` — package doc (SUB-02, SUB-03, PVC architecture)
- `internal/dispatch/podjob/names.go` — JobName() helper
- `internal/dispatch/podjob/names_test.go` — 5 TestJobName subtests
- `internal/dispatch/podjob/jobspec.go` — BuildJobSpec() with two-container topology
- `internal/dispatch/podjob/jobspec_test.go` — 12 TestBuildJobSpec subtests
- `internal/dispatch/podjob/backend.go` — PodJobBackend, EnvelopeReader, FilesystemEnvelopeReader
- `internal/dispatch/podjob/backend_test.go` — 4 TestPodJobBackend subtests

## Deviations from Plan

None — plan executed exactly as written.

## TDD Gate Compliance

All three tasks followed RED/GREEN pattern:
- Task 1: `names_test.go` written first (failed: undefined JobName), then `names.go` added (passed)
- Task 2: `jobspec_test.go` written first (failed: undefined BuildOptions/BuildJobSpec/constants), then `jobspec.go` added (all 12 passed)
- Task 3: `backend_test.go` written first (failed: undefined PodJobBackend), then `backend.go` added (all 4 passed)

## Self-Check: PASSED
