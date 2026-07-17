# medium — ~$5 real Claude (in-cluster HTTP git remote)

**Audience:** Operators trying TIDE with real LLM dispatch at ~$5 cost, with no
external repo dependency.

**Status:** v1.0; in-cluster HTTP git remote served by the `git-http-server`
Deployment inside `tide-sample-medium`. The demo-remote-init Job populates the
bare repo on a PVC; the git-http server bridges that PVC to the HTTP transport
layer so TIDE's core images (which carry no git binary) can clone and push
over pure-Go HTTP. Phase 5 D-B3 / RESEARCH §"Topic 4 Option b" decision
preserved: no external public fixture repo for v1.

## Overview

The medium sample drives TIDE to clone a fixture repository, plan a small
bounded outcome (add `FormattedNow() string` alongside the existing
`Greeting(name)` function), dispatch the resulting Task DAG via real Claude
(`claude-haiku-4-5`), and push the artifacts back on a per-run branch.

Unlike the small ($0 stub) sample, this sample uses real LLM calls and a
real git round-trip — but the entire git transport stays inside the cluster.
The git remote is an in-cluster HTTP server (`git-http-server`) that serves
the fixture repo over `http://`, exercising the same pure-Go HTTP transport
that production HTTPS uses. Zero external dependencies.

**Cost:** ~$5. The hard `$5` cap (`Project.Spec.budget.absoluteCapCents: 500`)
bounds the worst case.

**Wall time:** roughly **5–10 minutes** from final `kubectl apply` to
`Status=Complete`, dominated by Claude round-trips.

## Architecture

Three components work in sequence:

### (a) demo-remote-init Job

The `demo-remote-init` Job (image: `ghcr.io/jsquirrelz/tide-demo-init:1.0.0`)
runs at setup time. It populates a bare git repository on `demo-remote-pvc`
using a `file://` push *internal to the init pod*. This is the only place
`file://` is used — and the only image that carries a system git binary for
that reason.

### (b) git-http-server Deployment

The `git-http-server` Deployment (image:
`ghcr.io/jsquirrelz/tide-git-http-server:1.0.0`) mounts `demo-remote-pvc`
at `/srv/git/` and serves the bare repo over HTTP using `git-http-backend`
(CGI) + nginx + fcgiwrap.

The Service DNS name inside the cluster is:
```
http://git-http-server.tide-sample-medium.svc.cluster.local/demo-remote.git
```

Or simply within the same namespace:
```
http://git-http-server/demo-remote.git
```

### (c) Controller Jobs (distroless — no git binary)

The controller's clone Job, Task executor Jobs, and push Job use **go-git
HTTP transport (pure-Go)**. They clone and push via the git-http-server
Service over `http://`. No system git binary is present in these images.

**The controller's clone and push Jobs do NOT mount demo-remote-pvc — they
reach the repo via HTTP through the git-http-server Service.** The PVC is
accessed only by the init Job (write once at setup) and the git-http-server
Deployment (read/write via the HTTP server).

## Prerequisites

- A Kubernetes cluster with TIDE installed (CRDs + controller chart).
  See [`docs/INSTALL.md`](../../../docs/INSTALL.md) for the full install
  recipe. cert-manager is required for TIDE's webhook TLS.
- `ANTHROPIC_API_KEY` exported in your shell (the secret-create step
  below reads it from the environment).
- A storage class that satisfies `ReadWriteOnce` for `demo-remote-pvc`
  (default for most clusters; see
  [`docs/rwx-drivers.md`](../../../docs/rwx-drivers.md) for the matrix).
- `kubectl` configured against your target cluster.
- `docker` (for building the two demo fixture images below).
- The two demo fixture images below. **These are demo/test fixtures and are
  NOT published to a public registry** — build them locally and make them
  available to your cluster (for kind, `kind load`; for other clusters, push
  to a registry your nodes can pull and update the image refs in
  `demo-remote-init-job.yaml` + `git-http-server-deployment.yaml`):

  ```bash
  docker build -t ghcr.io/jsquirrelz/tide-demo-init:1.0.0 \
    -f images/tide-demo-init/Dockerfile .
  docker build -t ghcr.io/jsquirrelz/tide-git-http-server:1.0.0 \
    -f images/tide-git-http-server/Dockerfile .

  # kind clusters: load the freshly built images so IfNotPresent resolves them
  kind load docker-image ghcr.io/jsquirrelz/tide-demo-init:1.0.0
  kind load docker-image ghcr.io/jsquirrelz/tide-git-http-server:1.0.0
  ```

## Apply Sequence (9 steps — order matters)

The RWO PVC (`demo-remote-pvc`) can only be mounted by one pod at a time
on most single-node clusters. **The init Job must complete and release the
PVC mount before the git-http server starts.** Follow the steps below in
order.

```bash
# 1. Namespace.
kubectl apply -f examples/projects/medium/namespace.yaml

# 2. PVC for the git-http server bare repo.
#    (demo-remote-pvc — shared between init Job and git-http server,
#    but never mounted simultaneously — see step 5 note.)
kubectl apply -f examples/projects/medium/demo-remote-pvc.yaml

# 3. Per-namespace resources:
#    - tide-projects PVC (controller workspace)
#    - tide-subagent ServiceAccount
#    (The Helm chart provisions these in tide-system; the medium sample
#    needs them in tide-sample-medium as well.)
kubectl apply -f examples/projects/medium/per-namespace-resources.yaml

# 3b. kind / RWO-only provisioners ONLY — skip on RWX-capable clusters.
#
#     WHY: per-namespace-resources.yaml ships tide-projects as ReadWriteMany
#     (the production default), but kind's rancher.io/local-path provisioner
#     is RWO-only — the RWX claim deadlocks at WaitForFirstConsumer and the
#     controller never reaches the PVC-Bound gate. accessModes are immutable,
#     so delete and recreate rather than patch (safe here: nothing has
#     mounted the claim yet). RWO is correct on a single node — same-node
#     pods share the mount, and wave sequencing keeps mounts serialized
#     (mirrors hack/scripts/acceptance-v1.sh's small-sample override).
kubectl delete pvc tide-projects -n tide-sample-medium
kubectl apply -f - <<EOF
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: tide-projects
  namespace: tide-sample-medium
  labels:
    app.kubernetes.io/name: tide
    app.kubernetes.io/managed-by: tide-sample
    tideproject.k8s/sample: medium
spec:
  accessModes: [ReadWriteOnce]
  resources:
    requests:
      storage: 1Gi
EOF

# 4. Mirror the signing-key Secret from tide-system.
#    The signing key is cluster-unique and is provisioned by the chart in
#    tide-system only. Extract and apply it to tide-sample-medium.
SIGNING_KEY=$(kubectl get secret tide-signing-key -n tide-system \
  -o jsonpath='{.data.TIDE_SIGNING_KEY}')
kubectl apply -f - <<EOF
apiVersion: v1
kind: Secret
metadata:
  name: tide-signing-key
  namespace: tide-sample-medium
type: Opaque
data:
  TIDE_SIGNING_KEY: ${SIGNING_KEY}
EOF

# 5. Init Job — populates the bare repo on demo-remote-pvc.
#    Wait for Complete before proceeding (step 6).
#
#    WHY: demo-remote-pvc is ReadWriteOnce. If the init Job pod and the
#    git-http server pod try to mount the same RWO PVC at the same time,
#    the second pod stays Pending ("Unable to attach or mount volumes").
#    Waiting for Complete ensures the init pod has terminated and released
#    the mount before the server starts.
kubectl apply -f examples/projects/medium/demo-remote-init-job.yaml
kubectl wait --for=condition=Complete job/demo-remote-init \
  -n tide-sample-medium --timeout=2m

# 6. git-http server — serves the bare repo over HTTP.
#    Only starts after step 5 ensures the PVC is free.
kubectl apply -f examples/projects/medium/git-http-server-deployment.yaml
kubectl wait --for=condition=Available deployment/git-http-server \
  -n tide-sample-medium --timeout=2m

# 7. Secret carrying ANTHROPIC_API_KEY (for the planner/executor subagents)
#    and GIT_PAT (empty — anonymous in-cluster http:// push doesn't require
#    credentials; git-http-backend accepts push when http.receivepack=true).
kubectl create secret generic tide-secrets \
  --from-literal=ANTHROPIC_API_KEY="$ANTHROPIC_API_KEY" \
  --from-literal=GIT_PAT="" \
  -n tide-sample-medium

# 8. Apply the Project CRD. TIDE picks it up; the controller starts the
#    clone Job (pure-Go HTTP clone from http://git-http-server/demo-remote.git),
#    then plans + dispatches the function-addition outcome.
kubectl apply -f examples/projects/medium/project.yaml

# 9. Wait for completion (bounded by ~15 min — well under the $5 cap's
#    natural duration). Watch via `kubectl get project -w` in another
#    terminal if you prefer.
kubectl wait --for=jsonpath='{.status.phase}'=Complete \
  project/medium-project -n tide-sample-medium --timeout=30m
```

## Minikube-Specific Notes

### Stale image tag problem (minikube image load is NOT idempotent)

`minikube image load` does **not** overwrite an existing image tag. If you
rebuild an image, the old version remains in minikube's docker daemon under
the same tag until you explicitly remove it first:

```bash
# Force-remove both tag forms before reloading:
minikube ssh -- docker rmi -f ghcr.io/jsquirrelz/tide-git-http-server:1.0.0
# Then reload the new build:
minikube image load ghcr.io/jsquirrelz/tide-git-http-server:1.0.0
```

Apply the same pattern for `tide-push`, `tide-demo-init`, and
`tide-claude-subagent` if you rebuild those images.

### tide-projects PVC prewarm on kind (local-path provisioner)

kind's `rancher.io/local-path` storage class uses `WaitForFirstConsumer` —
the PVC won't bind until a Pod is actually scheduled. If your `kubectl wait`
for the init Job shows the pod stuck Pending, prewarm the `tide-projects`
PVC with a temporary busybox pod:

```bash
kubectl run prewarm --image=busybox:1.36 --restart=Never \
  --overrides='{"spec":{"volumes":[{"name":"v","persistentVolumeClaim":{"claimName":"tide-projects"}}],"containers":[{"name":"c","image":"busybox:1.36","command":["sh","-c","exit 0"],"volumeMounts":[{"name":"v","mountPath":"/data"}]}]}}' \
  -n tide-sample-medium
kubectl wait --for=condition=Succeeded pod/prewarm -n tide-sample-medium --timeout=60s
kubectl delete pod prewarm -n tide-sample-medium
```

## Verification

After step 9 completes, inspect what TIDE wrote:

```bash
# Project status
kubectl describe project/medium-project -n tide-sample-medium

# Budget spend (should be well under 500 cents)
kubectl get project medium-project -n tide-sample-medium \
  -o jsonpath='{.status.budget.costSpentCents}'

# Jobs dispatched
kubectl get jobs -n tide-sample-medium

# Init job logs (should show successful repo population)
kubectl logs job/demo-remote-init -n tide-sample-medium

# git-http server logs (shows HTTP clone/push requests from the controller)
kubectl logs deployment/git-http-server -n tide-sample-medium
```

To inspect the artifacts TIDE wrote, exec into a pod that has git installed
(or use `kubectl debug` against a temporary pod that mounts `demo-remote-pvc`)
and run `git log tide/run-medium-project-*` to see the planner's `PLAN.md`
and Task diffs on the per-run branch.

## What you'll observe

- `kubectl get pods -n tide-sample-medium` shows the controller's clone
  Job pod transition Pending → Running → Completed (~30s).
- A planner Job pod follows: `kubectl get jobs -n tide-sample-medium`.
  The planner authors the phase brief + PLAN.md and pushes a boundary
  commit to `tide/run-medium-project-<unix-ts>` on the in-cluster remote.
- Per-Task executor Job pods run the function-addition tasks in waves
  (typically 1–2 waves for a 3–5 task plan).
- A push Job stages the final commit (`tide: project complete`) and
  pushes back to the same per-run branch via the git-http server.
- `kubectl get project medium-project -n tide-sample-medium -o jsonpath='{.status.budget.costSpentCents}'`
  shows the running spend (well under 500).

## Budget cap behavior

`Project.Spec.budget.absoluteCapCents: 500` ($5). If TIDE exceeds the cap
during the run, dispatch halts and `Project.Status.phase=BudgetExceeded`
fires (Phase 2 D-D2 + Phase 04.1 P4.1 rolling-window infra). The cap
firing is a LEGITIMATE outcome — it means the outcome prompt was too
ambitious for the budget; the recovery is to either tighten the prompt
or raise the cap.

`tide approve --bypass-budget` exists as a manual-recovery affordance.
For details and the full set of recovery paths see
[`docs/troubleshooting.md`](../../../docs/troubleshooting.md).

## Cleanup

```bash
kubectl delete namespace tide-sample-medium
```

The namespace deletion cascades to the Project, all child CRDs (Phase,
Plan, Task, Wave), the demo-remote-init Job, the git-http-server Deployment,
the demo-remote-pvc PVC, and the tide-secrets Secret via owner-refs (where
applicable) and namespace-scoped lifecycle.

## Pitfall 9 reminder

`tide-sample-medium` is distinct from Phase 1's `tide-samples` (plural)
kubebuilder fixture namespace. The two paths serve different audiences
and never share resources. The `tide-sample-` prefix is the Phase 5
sample convention (`tide-sample-small`, `tide-sample-medium`,
`tide-sample-large`).

## Schema-gap notes (v1.0 → v1.x)

One v1.0 schema gap to be aware of (documented in
`.planning/phases/05-distribution-self-hosting-acceptance/deferred-items.md`
and carried forward from plan 05-11's large-sample experience):

- **`outcomePrompt`** is carried as the `tideproject.k8s/outcome-prompt`
  annotation. The planner subagent reads the annotation off the Project
  CRD. v1.x promotes this to a first-class `Project.Spec.OutcomePrompt`
  field; both surfaces will coexist for one minor version.

## Related

- [`examples/projects/README.md`](../README.md) — cost-spectrum overview of all 3 samples
- [`examples/projects/small/README.md`](../small/README.md) — $0 stub-subagent smoke test
- [`examples/projects/large/README.md`](../large/README.md) — $25 v1 acceptance ritual
- [`cmd/tide-demo-init/README.md`](../../../cmd/tide-demo-init/README.md) — the bootstrap binary
- [`images/tide-git-http-server/`](../../../images/tide-git-http-server/) — the in-cluster HTTP git server image
- [`examples/tide-demo-fixture/`](../../tide-demo-fixture/) — source-of-truth seed content (MIT-licensed)
- [`docs/project-authoring.md`](../../../docs/project-authoring.md) — Project.Spec field reference
- [Phase 5 CONTEXT.md](../../../.planning/phases/05-distribution-self-hosting-acceptance/05-CONTEXT.md) — D-B1..D-B3 sample decisions
