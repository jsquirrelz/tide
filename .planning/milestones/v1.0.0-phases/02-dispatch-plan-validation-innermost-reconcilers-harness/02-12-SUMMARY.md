---
phase: 2
plan: 12
subsystem: manager-wiring + helm-chart
tags: [manager, helm, signing-key, budget, pvc, credproxy, phase2-wiring]
dependency_graph:
  requires: ["02-03", "02-07", "02-09", "02-10", "02-11"]
  provides: ["phase2-wiring", "signing-secret-helm", "subagent-sa", "projects-pvc", "manager-phase2-flags"]
  affects: ["charts/tide", "charts/tide-crds", "cmd/manager", "hack/helm"]
tech_stack:
  added: []
  patterns:
    - "Helm signing-secret bootstrap via lookup + resource-policy:keep (D-C3)"
    - "Idempotent augment-script via cp source-of-truth + Python marker guards"
    - "ctrlmgr.RunnableFunc for budget.PreCharge at Manager startup"
    - "Single shared RWX PVC + subPath architecture (Blocker #2/#3)"
key_files:
  created:
    - hack/helm/signing-secret.yaml
    - hack/helm/serviceaccount-subagent.yaml
    - hack/helm/projects-pvc.yaml
    - charts/tide/templates/signing-secret.yaml
    - charts/tide/templates/serviceaccount-subagent.yaml
    - charts/tide/templates/projects-pvc.yaml
  modified:
    - cmd/manager/main.go
    - hack/helm/tide-values.yaml
    - hack/helm/augment-tide-chart.sh
    - charts/tide/values.yaml
    - charts/tide/templates/deployment.yaml
    - charts/tide-crds/templates/project-crd.yaml
    - charts/tide-crds/templates/task-crd.yaml
    - charts/tide-crds/templates/plan-crd.yaml
    - charts/tide/templates/manager-rbac.yaml
decisions:
  - "TIDE_SIGNING_KEY env var name matches Secret data key so envFrom:secretRef auto-populates it without valueFrom+secretKeyRef indirection (Blocker #1)"
  - "budget.PreCharge registered as ctrlmgr.RunnableFunc — runs after cache sync, best-effort non-fatal per Pitfall C (D-D1)"
  - "Single shared tide-projects RWX PVC with subPath per Project — RESEARCH.md OQ#2 RESOLVED; per-Project PVC deferred to Phase 3 ART-02"
  - "Python marker-guard idempotency in augment script (8 unique markers) rather than full deployment template replacement"
  - "WARNING #9 preserved: serviceaccount-subagent.yaml is a NEW template file; Phase 1 serviceaccount.yaml untouched"
metrics:
  duration: "~45 minutes"
  completed: "2026-05-13"
  tasks_completed: 2
  files_changed: 9
---

# Phase 2 Plan 12: Manager Wiring + Helm Chart Additions Summary

Wire Phase 2 components at the `cmd/manager` + Helm chart layers: signing key loading, budget store, image flags, PreCharge runnable, and three new Helm templates (signing-secret, subagent SA, projects PVC).

## Tasks Completed

| Task | Name | Commit | Key Files |
|------|------|--------|-----------|
| 1 | cmd/manager wiring (Phase 2 fields injection + signing key + PreCharge) | 40956d2 | cmd/manager/main.go |
| 2 | hack/helm signing-secret template + tide-values.yaml Phase 2 additions + augment script extension | 8b33de8 | hack/helm/*, charts/tide/*, charts/tide-crds/* |

## Task 1: cmd/manager Phase 2 Wiring

### 5 new CLI flags

| Flag | Default | Source |
|------|---------|--------|
| `--subagent-image` | `""` | `.Values.images.stubSubagent.repository:tag` |
| `--credproxy-image` | `""` | `.Values.images.credProxy.repository:tag` |
| `--default-file-touch-mode` | `"warn"` | `.Values.planAdmission.fileTouchMode` |
| `--rate-limit-default-rpm` | `60` | `.Values.rateLimits.defaults.requestsPerMinute` |
| `--rate-limit-default-burst` | `10` | `.Values.rateLimits.defaults.burst` |

### TIDE_SIGNING_KEY env contract (Blocker #1 fix)

`decodeSigningKeyFromEnv()` reads `TIDE_SIGNING_KEY` from the environment, base64-decodes it, and verifies it is at least 32 bytes. The manager fails fast (`os.Exit(1)`) if the env var is missing, malformed, or too short — running without a signing key would silently break HMAC validation of per-Task tokens (HARN-03).

The Secret's data key is `TIDE_SIGNING_KEY` (env-friendly — no dashes). `envFrom: [{secretRef: {name: tide-signing-key}}]` on the Manager Deployment and credproxy sidecar auto-populates `TIDE_SIGNING_KEY` directly from the Secret data with no `valueFrom + secretKeyRef + key` indirection. This is the Blocker #1 fix.

### Phase 2 struct injection

```go
&controller.TaskReconciler{
    // Phase 1 fields preserved...
    Budget:         budgetStore,          // budget.NewStore()
    Defaults:       defaults,             // budget.Limits{RPM, Burst}
    SigningKey:      signingKey,           // base64-decoded from TIDE_SIGNING_KEY
    SubagentImage:  subagentImage,        // --subagent-image flag
    CredproxyImage: credproxyImage,       // --credproxy-image flag
    EnvReader:      envReader,            // FilesystemEnvelopeReader{"/workspaces"}
}
```

### PreCharge runnable

`budget.PreCharge` is registered as `ctrlmgr.RunnableFunc` on the Manager. It runs after the Manager starts (after leader election + cache sync), best-effort with a 60-second window. Errors are logged non-fatally per Pitfall C — timestamps are not persisted per-Job, so a slight count over/under is accepted for v1 (D-D1).

### defaultFileTouchMode

The Phase 1 hardcoded `const defaultFileTouchMode = "warn"` was converted to a `--default-file-touch-mode` flag (default `"warn"`). Helm chart operators set `planAdmission.fileTouchMode=strict` via `--set` to enforce strict file-touch validation cluster-wide.

## Task 2: Helm Chart Additions

### signing-secret.yaml (Blocker #1 / D-C3)

`charts/tide/templates/signing-secret.yaml` — idempotent via Helm's `lookup` + `if not` guard:
- First install: Helm generates a 64-char random alphanumeric key, base64-encodes it, writes as Secret data key `TIDE_SIGNING_KEY`
- `helm upgrade` / `helm uninstall`: `resource-policy: keep` annotation preserves the Secret — in-flight tokens validated against the key continue to verify (T-02-12-02 / T-02-12-03 mitigations)
- Secret name: `tide-signing-key` (from `signingKey.secretName` in values.yaml)

### serviceaccount-subagent.yaml (Warning #9 fix)

`charts/tide/templates/serviceaccount-subagent.yaml` — a NEW Helm template (not an edit to Phase 1's `serviceaccount.yaml`). Creates the `tide-subagent` ServiceAccount with no RoleBinding. Subagent pods have zero K8s API verbs (D-A4 / T-02-12-04 mitigation). Phase 1's `serviceaccount.yaml` is untouched.

### projects-pvc.yaml (Blocker #2/#3 / RESEARCH.md OQ#2 RESOLVED)

`charts/tide/templates/projects-pvc.yaml` — single shared `tide-projects` ReadWriteMany PVC:
- `helm.sh/resource-policy: keep` protects in-flight workspace state from accidental deletion on `helm uninstall` (T-02-12-07)
- `workspaces.pvc.storageClassName: ""` uses cluster-default StorageClass; operators set to a CSI RWX driver for production
- `workspaces.pvc.size: 10Gi` default; tunable via `--set workspaces.pvc.size=<N>`

**PVC architecture decision (RESEARCH.md OQ#2 RESOLVED):**
- ONE cluster-wide `tide-projects` PVC (ReadWriteMany), Helm-provisioned at install time
- Manager Pod mounts at `/workspaces` (no subPath) — reads `/workspaces/{project-uid}/workspace/envelopes/{task-uid}/out.json`
- Task Pods mount via `subPath: {project-uid}/workspace` → appearing as `/workspace` in-container
- Trade-off: single shared PVC means per-Project quota story does not fall out of CSI; quotas are Project.Spec.Budget-driven (FAIL-04), not storage-driven. Phase 3 ART-02 evaluates per-Project PVCs if/when needed.

### Phase 2 values keys

Added to `hack/helm/tide-values.yaml` (propagated to `charts/tide/values.yaml` via augment script):

| Key | Default | Purpose |
|-----|---------|---------|
| `planAdmission.fileTouchMode` | `warn` | Cluster-level file-touch validation mode |
| `rateLimits.defaults.requestsPerMinute` | `60` | Default RPM rate limit |
| `rateLimits.defaults.tokensPerMinute` | `100000` | Informational v1 |
| `rateLimits.defaults.burst` | `10` | Default burst size |
| `images.stubSubagent.{repository,tag}` | `ghcr.io/jsquirrelz/tide-stub-subagent:v0.1.0-dev` | Subagent image |
| `images.credProxy.{repository,tag}` | `ghcr.io/jsquirrelz/tide-credproxy:v0.1.0-dev` | Credproxy image |
| `images.busybox.{repository,tag}` | `busybox:1.36` | Init job image |
| `signingKey.secretName` | `tide-signing-key` | HMAC signing secret name |
| `budget.defaults.absoluteCapCents` | `10000` | $100.00 absolute cap |
| `budget.defaults.rollingWindowCapCents` | `5000` | $50.00/15-min window cap |
| `workspaces.pvc.{create,name,size,storageClassName}` | `true, tide-projects, 10Gi, ""` | Shared workspace PVC |

### augment script extension

`hack/helm/augment-tide-chart.sh` extended with:
1. **Step 5**: `cp hack/helm/signing-secret.yaml charts/tide/templates/signing-secret.yaml`
2. **Step 6**: `cp hack/helm/serviceaccount-subagent.yaml charts/tide/templates/serviceaccount-subagent.yaml`
3. **Step 7**: `cp hack/helm/projects-pvc.yaml charts/tide/templates/projects-pvc.yaml`
4. **Step 8**: Python-based idempotent deployment.yaml augmentation:
   - Adds `envFrom: [{secretRef: {name: tide-signing-key}}]` (Blocker #1)
   - Replaces `args: {{- toYaml ... }}` with expanded block including Phase 2 flags
   - Adds `/workspaces` volumeMount inside the container's volumeMounts list
   - Adds `tide-projects` PVC volume at the end of the pod volumes list

All four modifications use unique in-file markers (`# phase2-*-injected`) as idempotency guards — second invocation detects the markers and skips re-insertion.

### CRD subchart regeneration

`charts/tide-crds/templates/{project-crd,task-crd,plan-crd}.yaml` regenerated automatically by `make helm` from Plan 03's controller-gen output. Plan 03's schema additions (`providerSecretRef`, `maxAttemptsPerTask`, etc.) flow through the helmify pipeline.

### CI reproducibility gate

`make helm` is idempotent: running a second time after the first produces zero file checksums diff. The Phase 1 CI gate (`.github/workflows/ci.yaml:140-151` — `git diff --quiet charts/` after `make helm`) continues to be satisfied.

## Deviations from Plan

None — plan executed exactly as written. One implementation note:

**Python marker-based augment approach:** The plan's `<interfaces>` section described the deployment augmentation as "add envFrom/volume/volumeMount" without specifying the precise mechanism. The Phase 1 augment script used awk for the webhook-port dedup (a simple text transformation). For Phase 2's multi-field deployment modifications, the implementation uses an embedded Python script with unique in-file markers as idempotency guards. This preserves the helmify-driven architecture (single regenerate-then-augment pipeline) without requiring a hand-maintained deployment.yaml that drifts from helmify output. The marker approach is more explicit than awk for multi-field insertion across different parts of a YAML file.

## Threat Surface Scan

All new surfaces are covered by the plan's threat model:
- `charts/tide/templates/signing-secret.yaml` — T-02-12-01/T-02-12-02/T-02-12-03 all addressed
- `charts/tide/templates/serviceaccount-subagent.yaml` — T-02-12-04 addressed (zero RoleBindings)
- `charts/tide/templates/projects-pvc.yaml` — T-02-12-07 addressed (subPath isolation + resource-policy:keep)
- `charts/tide/templates/deployment.yaml` (envFrom) — T-02-12-06 accepted (same posture as ANTHROPIC_API_KEY)

No new trust boundaries introduced beyond what the plan's threat model covers.

## Self-Check: PASSED

- `cmd/manager/main.go` exists with `budget.NewStore`, `TIDE_SIGNING_KEY`, `SetupPlanWebhookWithManager.*defaultFileTouchMode`
- `charts/tide/templates/signing-secret.yaml` exists with `TIDE_SIGNING_KEY` data key
- `charts/tide/templates/serviceaccount-subagent.yaml` exists with `tide-subagent`
- `charts/tide/templates/projects-pvc.yaml` exists with `ReadWriteMany`
- `charts/tide/values.yaml` contains `planAdmission`, `rateLimits`, `signingKey`, `workspaces`
- `charts/tide-crds/templates/project-crd.yaml` contains `providerSecretRef`, `maxAttemptsPerTask`
- `charts/tide/templates/serviceaccount.yaml` diff empty (Phase 1 file untouched)
- `make helm` is idempotent (checksums stable across consecutive runs)
- `go build ./...` exits 0
- `make test` PASS (all packages)
- `make verify-no-blocking`, `make verify-rbac-marker-discipline`, `make verify-no-rbac-wildcards` all exit 0
- Commits 40956d2, 8b33de8 exist in git log
