# v1alpha1 → v1alpha2 Migration Guide

**Breaking change** — v1.0.2 "Spring Tide" introduces v1alpha2 as the sole served and storage
version for all six TIDE CRDs. v1alpha1 objects already in etcd are **not auto-migrated**;
they must be deleted and re-applied under v1alpha2. This guide covers what changed, why, and
exactly how to reinstall a running cluster.

---

## What changed and why

### Wave ownership: Plan → Project (D-07, SCHEMA-01)

In v1alpha1, each `Wave` was owned by a single `Plan` (`WaveSpec.PlanRef`); wave indices were
per-plan counters. The "Topologically-Indexed" namesake requires a **global, monotonic wave
index** across the entire Project's task DAG — a per-plan index can only schedule within a
single plan and cannot express cross-plan or cross-phase dependencies.

`v1alpha2.WaveSpec` replaces `PlanRef` with `ProjectRef` and promotes `WaveIndex` to a
project-scoped global counter. One Wave CR covers one global wave position. The Phase 24
assembler derives these from the full task DAG (O(V+E) layered Kahn); the Phase 23 controller
stubs materialize per-plan Waves as an interim shape while the assembler is built.

### Cross-scope `dependsOn` (D-02, DEPS-01)

v1alpha1 `Task.Spec.DependsOn` was restricted to within-plan task names (D-F1 restriction).
v1alpha2 retires D-F1: `DependsOn` entries on every level (Task, Plan, Phase, Milestone) may
name any node in the Project — cross-plan, cross-phase, or cross-milestone. Coarse scope
references (naming a Plan rather than a specific Task) are fan-out expanded by the assembler
at execution time (DEPS-02). This is what enables the progressive-refinement dependency
model:

    MB requires MA  →  MB requires MA-P3  →  MB requires MA-P3-PB  →  MB requires MA-P3-PB-task-07

### `Plan.Spec.DependsOn` added (DEPS-02)

v1alpha1 `PlanSpec` had no `dependsOn` field. v1alpha2 adds it so planner-authored inter-plan
dependencies are first-class schema citizens, not workarounds.

### Conversion webhook retired (D-09)

v1alpha1 scaffolded a no-op conversion Hub (`api/v1alpha1/plan_conversion.go`). With the clean
break to v1alpha2 — and only dogfood clusters in existence — a full conversion webhook is
unnecessary overhead. It is removed. v1alpha1 is marked `served: false, storage: false` across
all six CRDs; the manager decodes any surviving v1alpha1 objects for the fail-closed guard but
never materializes new v1alpha1 objects.

### SchemaRevision discriminator (SCHEMA-03)

`v1alpha2.ProjectSpec` includes a `schemaRevision` field (string, default `"v1alpha2"`). The
controller reads this field on every reconcile; any Project missing `schemaRevision: v1alpha2`
triggers the `RequiresReinstall` status condition (fail-closed safety net, see below) and halts
dispatch until the field is set.

---

## Version bump

v1alpha1 → v1alpha2 is a **breaking CRD change**. Per the TIDE release recipe:

1. Bump `chart/Chart.yaml` `appVersion` **first** (before any image builds) so
   `make build` picks up the correct version string.
2. Build and push new controller images against the bumped appVersion.
3. Apply the new CRDs (see Reinstall Procedure below).
4. Re-apply Project manifests carrying `schemaRevision: v1alpha2`.

> `charts/tide/values.yaml` is the **fixed contract** — the binary catches up to the chart,
> never the reverse. Do not edit `values.yaml` to accommodate a mid-cycle schema change.

---

## Reinstall Procedure

Because v1alpha1 is unserved and there is no conversion webhook, in-flight v1alpha1 objects
must be removed and re-applied. The procedure below is the authoritative reinstall path for
v1.0.2 on any dogfood cluster (kind-tide-dogfood or equivalent).

### 1. Delete existing Project objects (owner-ref cascade)

```bash
# Owner-ref cascade removes all child Milestones, Phases, Plans, Tasks, and Waves
# owned by the Project. Confirm the namespace and project name before running.
kubectl delete project <project-name> -n <project-namespace>
```

If multiple Projects exist:

```bash
kubectl get projects --all-namespaces -o name | xargs kubectl delete
```

Wait for all child CRDs to disappear:

```bash
kubectl wait --for=delete milestones,phases,plans,tasks,waves --all \
  --all-namespaces --timeout=120s
```

### 2. Apply the new v1alpha2 CRDs

```bash
kubectl apply -f config/crd/bases/
```

Confirm v1alpha2 is served and v1alpha1 is not:

```bash
kubectl get crd tideproject.k8s_plans.yaml -o jsonpath='{.spec.versions[*].name}={.spec.versions[*].served}'
# Expected: v1alpha1=false v1alpha2=true
```

### 3. Re-apply the Project YAML with schemaRevision

v1alpha2 Project manifests **must** carry `spec.schemaRevision: v1alpha2`:

```yaml
apiVersion: tideproject.k8s/v1alpha2
kind: Project
metadata:
  name: my-project
  namespace: my-project-ns
spec:
  schemaRevision: v1alpha2
  targetRepo: https://github.com/org/repo
  # ... rest of spec
```

Apply:

```bash
kubectl apply -f my-project.yaml
```

### 4. Verify the controller picks up the new Project

```bash
kubectl get project my-project -n my-project-ns -o yaml | grep -E 'schemaRevision|phase|conditions'
```

The controller should transition the Project to `Initialized` (or `Running`) with no
`RequiresReinstall` condition.

---

## Fail-Closed Safety Net (SCHEMA-03)

Any Project object that reaches the controller **without** `spec.schemaRevision: v1alpha2` is
rejected with a `RequiresReinstall` status condition:

```yaml
status:
  conditions:
    - type: RequiresReinstall
      status: "True"
      reason: StaleSchemaVersion
      message: "Project carries schemaRevision '' or 'v1alpha1'; follow the v1alpha1→v1alpha2 migration guide to delete and re-apply"
```

The controller **never silently runs** on a stale-schema Project — dispatch is halted until the
Project is re-applied with the correct `schemaRevision`. This satisfies SCHEMA-03's
no-silent-corruption guarantee: an operator who forgets to reinstall sees a loud, actionable
status condition rather than undefined behavior.

The `RequiresReinstall` condition is wired by the Project reconciler (Plan 23-03). The guard
reads `spec.schemaRevision` on every reconcile; a Project that passes the guard and is later
downgraded (e.g., via `kubectl edit`) re-enters the guard on the next reconcile.

---

## Dogfood Cluster Note (kind-tide-dogfood)

`kind-tide-dogfood` holds v1alpha1 objects from Phase 22 and earlier runs. Follow the procedure
above on that cluster before deploying v1.0.2:

```bash
# 1. Delete existing Projects (cascade)
kubectl delete project --all --all-namespaces

# 2. Update the CRDs
kubectl apply -f config/crd/bases/

# 3. Re-deploy the controller (updated image with v1alpha2 scheme)
helm upgrade tide charts/tide/ --namespace tide-system --reuse-values

# 4. Re-apply your Project YAML with schemaRevision: v1alpha2
kubectl apply -f your-project.yaml
```

No automated data-migration tooling ships with v1.0.2. This is a dogfood-only cluster; the
clean delete+reapply avoids stranded etcd objects and sidesteps any conversion-strategy
ambiguity.

---

*Migration doc authored: Phase 23, Plan 23-02 (Spring Tide — Global Execution DAG)*
