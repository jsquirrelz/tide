# v1alpha2 → v1alpha3 Migration Guide

**Breaking change** — Phase 40 introduces v1alpha3 as the sole served and storage version for
all six TIDE CRDs, and removes v1alpha1 and v1alpha2 entirely (Go packages, CRD version blocks,
chart copies, scheme registrations). v1alpha2 objects already in etcd are **not auto-migrated**;
they must be deleted and re-applied under v1alpha3. This guide covers what changed, why, and
exactly how to reinstall a running cluster.

---

## What changed and why

### `subagent.levels` semantic rename (D-02)

v1alpha2's `Levels.{milestone,phase,plan,task}` keys named the **CR issuing the dispatch**, not
the artifact the dispatch produces — an off-by-one that read backwards from what operators
expect. The MILESTONE.md-authoring dispatch, in particular, ran at level string `"project"`,
which matched **no** `Levels` key in `ResolveProvider`'s switch and silently fell back to
`Spec.Subagent.Model` with no error or event (first hit on the 2026-07-03 external-repo run).

v1alpha3 renames the semantics so each `levels.X` key means **"level X is planned by this
model"** — the reading operators already had. The Go field names and JSON tags are unchanged;
only the meaning attached to each key moved. Re-author existing overrides using this table:

| Key | Old meaning (v1alpha2) | New meaning (v1alpha3) |
| --- | --- | --- |
| `levels.milestone` | Model the Milestone CR used to author phase briefs | Model that authors MILESTONE.md |
| `levels.phase` | Model the Phase CR used to author PLAN.md | Model that authors phase briefs |
| `levels.plan` | Model the Plan CR used to author the task DAG | Model that authors PLAN.md **and** the task DAG (one slot, two dispatches — both are "planning the plan's content") |
| `levels.task` | Task execution (diffs) | Unchanged — task execution (diffs) |

**Fallback note:** under v1alpha2 the MILESTONE.md dispatch matched no `Levels` key and silently
used `spec.subagent.model`. v1alpha3 closes that hole — `levels.milestone` now resolves that
dispatch directly. If your v1alpha2 manifest relied on the fallback for the MILESTONE.md model,
set `levels.milestone.model` explicitly in the re-authored v1alpha3 Project; `subagent.model`
remains a pure fallback for any level without its own override.

Re-author each `levels.X` entry by reading the **old meaning** column for what your existing
value actually controlled, then place that value under the **new meaning** column's key. A
v1alpha2 manifest that set a strong model on `levels.phase` (intending "make my phase planner
strong") was actually strengthening the PLAN.md-authoring dispatch — in v1alpha3 that same intent
is expressed as `levels.plan`.

### `spec.modelSelection` removed (D-10)

v1alpha2 carried a legacy `ModelSelection.{milestone,phase,plan,task}` field alongside
`subagent.levels`, documented as "kept for Phase 1 fixtures." It had zero controller readers
outside `api/` — a dead field that duplicated the wired `subagent.levels` config. v1alpha3 drops
it entirely. If your v1alpha2 Project set values under `modelSelection`, move them to the
corresponding `subagent.levels.*.model` key using the remap table above (the old
`modelSelection.X` slots shared the same off-by-one naming as `levels.X`).

### Envelope group decoupling (D-08)

The dispatch envelope contract used to travel under the CRD's own API group. v1alpha3 decouples
it: subagent-image authors now receive `apiVersion: dispatch.tideproject.k8s/v1alpha1` on every
`EnvelopeIn`/`EnvelopeOut` payload — a dedicated subdomain group for a document that is never a
served Kubernetes resource (the kubeadm pattern: `kubeadm.k8s.io/v1beta4` config files vs core
resource APIs). The version component stays `v1alpha1` — this is pure group decoupling, not a
stability claim on the envelope shape. Subagent images that hardcode the old combined
group+version string in their envelope parsing must be rebuilt against
`pkg/dispatch.APIVersionV1Alpha1`.

### v1alpha1 and v1alpha2 removed entirely

Both prior API versions are gone — packages, CRD version blocks, chart copies, scheme
registrations, and dead comments. v1alpha3 is the **sole served + storage version** for all six
CRDs (`Project`, `Milestone`, `Phase`, `Plan`, `Task`, `Wave`). The SchemaRevision guard now
requires `spec.schemaRevision: v1alpha3` on every Project — see [Fail-Closed Safety
Net](#fail-closed-safety-net) below.

---

## Reinstall Procedure

Consistent with the Phase 23 D-09 precedent (conversion webhook retired at v1alpha1→v1alpha2),
there is **no** conversion machinery for this crank either — no `storedVersions` pruning
recipe, no preflight automation, no conversion webhook. Migration is reinstall-only.

### 1. Export any in-flight state you need

```bash
kubectl get projects,milestones,phases,plans,tasks,waves -A -o yaml > tide-v1alpha2-backup.yaml
```

### 2. Delete existing Project objects (owner-ref cascade)

```bash
kubectl get projects --all-namespaces -o name | xargs -r kubectl delete
kubectl wait --for=delete milestones,phases,plans,tasks,waves --all \
  --all-namespaces --timeout=120s
```

### 3. Delete and reinstall the `tide-crds` chart

CRDs are single-version now — there is nothing to merge, only a clean replace:

```bash
helm uninstall tide-crds -n tide-system
helm install tide-crds oci://ghcr.io/jsquirrelz/tide-charts/tide-crds \
  --version <new-version> -n tide-system
```

Confirm v1alpha3 is the only served version:

```bash
kubectl get crd projects.tideproject.k8s -o jsonpath='{.spec.versions[*].name}={.spec.versions[*].served}'
# Expected: v1alpha3=true
```

### 4. Upgrade the `tide` chart

```bash
helm upgrade tide oci://ghcr.io/jsquirrelz/tide-charts/tide \
  --version <new-version> -n tide-system --reuse-values
```

### 5. Re-apply Projects under v1alpha3

Re-author each Project manifest per the levels-remap table above, then apply:

```yaml
apiVersion: tideproject.k8s/v1alpha3
kind: Project
metadata:
  name: my-project
  namespace: my-project-ns
spec:
  schemaRevision: v1alpha3
  targetRepo: https://github.com/org/repo
  # ... rest of spec, with subagent.levels re-authored per the remap table
```

```bash
kubectl apply -f my-project.yaml
```

### 6. Verify the controller picks up the new Project

```bash
kubectl get project my-project -n my-project-ns -o yaml | grep -E 'schemaRevision|phase|conditions'
```

The controller should transition the Project to `Initialized` (or `Running`) with no
`RequiresReinstall` condition.

---

## Fail-Closed Safety Net

Any Project object that reaches the controller **without** `spec.schemaRevision: v1alpha3` is
rejected with a `RequiresReinstall` status condition:

```yaml
status:
  conditions:
    - type: Ready
      status: "False"
      reason: RequiresReinstall
      message: >-
        Project was authored under a prior schema revision; reinstall required:
        kubectl delete project <name> && kubectl apply -f <project.yaml>
        (with schemaRevision: v1alpha3 set). See docs/migration/v1alpha2-to-v1alpha3.md.
```

The controller **never silently runs** on a stale-schema Project — reconciliation halts via a
`reconcile.TerminalError` until the Project is re-applied with the correct `schemaRevision`. This
is the same fail-closed guard from Phase 23 (`checkSchemaRevisionGuard`), generalized under D-04
so the expected revision and this doc's path are parameterized as two constants — the next
schema crank (v1alpha4) is a two-line change to `expectedSchemaRevision` and
`migrationGuideDocPath` in `internal/controller/project_controller.go`, nothing else in the
guard's logic.

---

## Dogfood Cluster Note (kind-tide-dogfood)

`kind-tide-dogfood` predates Spring Tide and still holds v1alpha1 objects from Phase 22 and
earlier. Per D-03, it is **rebuilt, not upgraded** for this crank — zero v1alpha1/v1alpha2
compatibility is built for that cluster. Stand up a fresh kind cluster, install the v1alpha3
chart pair, and re-apply Projects from scratch rather than attempting an in-place upgrade.

---

*Migration doc authored: Phase 40 (Deprecate v1alpha1 API), Plan 40-06.*
