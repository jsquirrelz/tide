<<<<<<< HEAD
# Phase 5 — Deferred Items

Out-of-scope discoveries logged during plan execution. Address in a follow-up plan.

- 2026-05-22 (plan 05-01) — `cmd/dashboard/api/plans.go` and `cmd/dashboard/api/tasks.go` are not gofmt-clean. Detected when `make fmt` ran during plan 05-01 verification. Out of scope for DIST-03 (these files already carry the Apache-2.0 header, so the verify-license gate is unaffected). Defer to a follow-up plan or a `/gsd:quick`.
=======
# Phase 05 — Deferred Items

Items discovered during plan execution that are out of scope for the current plan but should be tracked.

## Discovered 2026-05-21 (during plan 05-05 execution)

### SOT drift in `hack/helm/tide-values.yaml` and `hack/helm/projects-pvc.yaml`

**Discovered by:** Plan 05-05 executor — running `bash hack/helm/augment-tide-chart.sh` produced unexpected modifications to `charts/tide/values.yaml` and `charts/tide/templates/projects-pvc.yaml` that have nothing to do with the chart version bump.

**Drift detected:**

1. **`charts/tide/values.yaml`** — SOT `hack/helm/tide-values.yaml` is missing two fields present in the generated chart:
   - `controllerManager.manager.podAnnotations: {}` (line 41)
   - `workspaces.pvc.accessModes: [ReadWriteMany]` (line 309)
   
   Running the augment script would drop these from `charts/tide/values.yaml`.

2. **`charts/tide/templates/projects-pvc.yaml`** — SOT `hack/helm/projects-pvc.yaml` uses a hardcoded `accessModes: [ReadWriteMany]` instead of the templated `{{- range (.Values.workspaces.pvc.accessModes | default (list "ReadWriteMany")) }}` loop that the generated chart now uses to support kind/minikube overrides.

**Why deferred:**

- Plan 05-05's scope is the lockstep version bump only (`files_modified: [hack/helm/tide-chart.yaml, hack/helm/tide-crds-chart.yaml]`).
- CLAUDE.md `Working Rules → 2. Execute, Don't Ask` lists "Edits to `charts/tide/values.yaml`" as an "always confirm or refuse" exception — chart is FIXED contract per Phase 02.2's anti-pattern.
- The augment scripts cleanly produced the desired `Chart.yaml` updates for both charts; the unrelated drift was reverted via `git checkout -- charts/tide/values.yaml charts/tide/templates/projects-pvc.yaml` to preserve plan scope.

**Remediation owner:** Future Phase 05 plan (likely a dedicated SOT-resync plan) should:

1. Promote the `podAnnotations`, `accessModes` template loop, and `accessModes` default into the SOT files at `hack/helm/tide-values.yaml` and `hack/helm/projects-pvc.yaml`.
2. Re-run `bash hack/helm/augment-tide-chart.sh` — expected diff should now be empty.
3. Add a CI guard so `make helm && git diff --exit-code charts/` fails fast on any future SOT drift (the ci.yaml `helm-lint` job already runs this — verify it catches future drift).

**Files affected:**
- `hack/helm/tide-values.yaml` (SOT — incomplete relative to chart-tree)
- `hack/helm/projects-pvc.yaml` (SOT — incomplete relative to chart-tree)
- `charts/tide/values.yaml` (chart-tree — has 2 fields not in SOT)
- `charts/tide/templates/projects-pvc.yaml` (chart-tree — uses templated accessModes; SOT hardcodes it)

**Severity:** Low — current chart-tree is correct and CI helm-lint passes. The drift only manifests if `augment-tide-chart.sh` is run alongside an unrelated bump, which is exactly what plan 05-05 did. Future plans that edit SOT files must reconcile this drift first or selectively `git checkout` the unrelated paths post-augment.
>>>>>>> worktree-agent-aa8905b2c77141ff0
