# demo-calculator — ~$10 real Claude (web-calculator payoff)

**Audience:** Operators who want a visually satisfying real-Claude demo — at the
end of the run you clone a branch and open a working calculator in your browser.

**Status:** v1.0.9; in-cluster HTTP git remote served by the `git-http-server`
Deployment inside `tide-demo-calculator`. The calculator-remote-init Job seeds a
fresh bare repo on a PVC; the git-http server bridges that PVC to the HTTP
transport layer so TIDE's core images (which carry no git binary) can clone and
push over pure-Go HTTP.

## Overview

The demo-calculator sample drives TIDE to clone a freshly seeded repository
(one README, nothing else), plan a tightly scoped outcome — a single-page web
calculator in exactly three files (`index.html` + `style.css` + `app.js`) —
dispatch the resulting Task DAG via real Claude (`claude-sonnet-5`), and push
the artifacts back on a per-run branch.

The architecture is the same as the [medium sample](../medium/README.md): a
seed Job populates an in-cluster git remote, a git-http server serves it over
`http://`, and the controller's Jobs speak pure-Go go-git HTTP to it. The
difference is the seed: instead of the `tide-demo-init` Go-fixture image, the
init Job here runs the git-http-server image itself with an inline script that
creates a **fresh bare repo** with a single README commit — no embedded fixture,
zero external repo dependency.

**Cost:** ~$10 worst case. The hard cap
(`Project.Spec.budget.absoluteCapCents: 1000`) bounds it; the tight
three-file scope keeps the expected spend well under.

**Wall time:** roughly **10–20 minutes** from final `kubectl apply` to
`Status=Complete`, dominated by Claude round-trips.

## Architecture

Three components work in sequence:

### (a) calculator-remote-init seed Job

The `calculator-remote-init` Job (image:
`ghcr.io/jsquirrelz/tide-git-http-server:1.0.0` — reused as the seed image
because it already carries `sh` + git and runs as UID 1000) runs at setup time.
It creates a bare repo at `/workspace/calculator.git` on
`calculator-remote-pvc`, enables `http.receivepack`, and seeds one initial
README commit on `main` using a file-path push *internal to the seed pod*.
This is the only place file-path git is used. The Job is idempotent-by-refusal:
if `calculator.git` already exists it exits without touching it.

### (b) git-http-server Deployment

The `git-http-server` Deployment (image:
`ghcr.io/jsquirrelz/tide-git-http-server:1.0.0`) mounts `calculator-remote-pvc`
at `/srv/git/` and serves the bare repo over HTTP using `git-http-backend`
(CGI) + nginx + fcgiwrap.

The Service DNS name inside the cluster is:
```
http://git-http-server.tide-demo-calculator.svc.cluster.local/calculator.git
```

Or simply within the same namespace:
```
http://git-http-server/calculator.git
```

### (c) Controller Jobs (distroless — no git binary)

The controller's clone Job, Task executor Jobs, and push Job use **go-git
HTTP transport (pure-Go)**. They clone and push via the git-http-server
Service over `http://`. No system git binary is present in these images.

**The controller's clone and push Jobs do NOT mount calculator-remote-pvc —
they reach the repo via HTTP through the git-http-server Service.** The PVC is
accessed only by the seed Job (write once at setup) and the git-http-server
Deployment (read/write via the HTTP server).

## Prerequisites

- A Kubernetes cluster with TIDE installed (CRDs + controller chart).
  See [`docs/INSTALL.md`](../../../docs/INSTALL.md) for the full install
  recipe. cert-manager is required for TIDE's webhook TLS.
- An Anthropic API key in a local file (the secret-create step below reads
  and newline-strips it).
- A storage class that satisfies `ReadWriteOnce` for `calculator-remote-pvc`
  (default for most clusters; see
  [`docs/rwx-drivers.md`](../../../docs/rwx-drivers.md) for the matrix).
- `kubectl` configured against your target cluster.
- `docker` (for building the git-http-server fixture image below).

## Apply Sequence (10 steps — order matters)

The RWO PVC (`calculator-remote-pvc`) can only be mounted by one pod at a
time on most single-node clusters. **The seed Job must complete and release
the PVC mount before the git-http server starts.** Follow the steps below in
order.

```bash
# 1. Build + load the git-http-server image. It is a demo/test fixture and
#    is NOT published to a public registry — build it locally and make it
#    available to your cluster.
docker build -t ghcr.io/jsquirrelz/tide-git-http-server:1.0.0 \
  -f images/tide-git-http-server/Dockerfile .
minikube image load ghcr.io/jsquirrelz/tide-git-http-server:1.0.0
#    NOTE: `minikube image load` is NOT idempotent — it does not overwrite
#    an existing tag. If you rebuild the image, force-remove the old one
#    inside minikube first:
#      minikube ssh -- docker rmi -f ghcr.io/jsquirrelz/tide-git-http-server:1.0.0
#    then reload. kind users substitute:
#      kind load docker-image ghcr.io/jsquirrelz/tide-git-http-server:1.0.0

# 2. Namespace.
kubectl apply -f examples/projects/demo-calculator/namespace.yaml

# 3. PVC for the git-http server bare repo.
#    (calculator-remote-pvc — shared between seed Job and git-http server,
#    but never mounted simultaneously — see step 6 note.)
kubectl apply -f examples/projects/demo-calculator/calculator-remote-pvc.yaml

# 4. Per-namespace resources:
#    - tide-projects PVC (controller workspace)
#    - tide-subagent / tide-push / tide-reporter SAs + RBAC
#    (The Helm chart provisions these in tide-system; the sample needs them
#    in tide-demo-calculator as well. tide-push is REQUIRED for this
#    git-enabled project — without it the clone/push Jobs fail FailedCreate.)
kubectl apply -f examples/projects/demo-calculator/per-namespace-resources.yaml

# 4b. kind / RWO-only provisioners ONLY — skip on RWX-capable clusters
#     (RWX binds fine on minikube).
#
#     WHY: per-namespace-resources.yaml ships tide-projects as ReadWriteMany
#     (the production default), but kind's rancher.io/local-path provisioner
#     is RWO-only — the RWX claim deadlocks at WaitForFirstConsumer and the
#     controller never reaches the PVC-Bound gate. accessModes are immutable,
#     so delete and recreate rather than patch (safe here: nothing has
#     mounted the claim yet).
kubectl delete pvc tide-projects -n tide-demo-calculator
kubectl apply -f - <<EOF
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: tide-projects
  namespace: tide-demo-calculator
  labels:
    app.kubernetes.io/name: tide
    app.kubernetes.io/managed-by: tide-sample
    tideproject.k8s/sample: demo-calculator
spec:
  accessModes: [ReadWriteOnce]
  resources:
    requests:
      storage: 1Gi
EOF

# 5. Mirror the signing-key Secret from tide-system.
#    The signing key is cluster-unique and is provisioned by the chart in
#    tide-system only. Extract and apply it to tide-demo-calculator.
SIGNING_KEY=$(kubectl get secret tide-signing-key -n tide-system \
  -o jsonpath='{.data.TIDE_SIGNING_KEY}')
kubectl apply -f - <<EOF
apiVersion: v1
kind: Secret
metadata:
  name: tide-signing-key
  namespace: tide-demo-calculator
type: Opaque
data:
  TIDE_SIGNING_KEY: ${SIGNING_KEY}
EOF

# 6. Seed Job — creates and seeds the bare calculator.git on the PVC.
#    Wait for Complete before proceeding (step 7).
#
#    WHY: calculator-remote-pvc is ReadWriteOnce. If the seed Job pod and
#    the git-http server pod try to mount the same RWO PVC at the same time,
#    the second pod stays Pending ("Unable to attach or mount volumes").
#    Waiting for Complete ensures the seed pod has terminated and released
#    the mount before the server starts.
kubectl apply -f examples/projects/demo-calculator/seed-remote-job.yaml
kubectl wait --for=condition=Complete job/calculator-remote-init \
  -n tide-demo-calculator --timeout=2m

# 7. git-http server — serves the bare repo over HTTP.
#    Only starts after step 6 ensures the PVC is free.
kubectl apply -f examples/projects/demo-calculator/git-http-server.yaml
kubectl wait --for=condition=Available deployment/git-http-server \
  -n tide-demo-calculator --timeout=2m

# 8. Secret carrying ANTHROPIC_API_KEY (for the planner/executor subagents)
#    and GIT_PAT (empty — anonymous in-cluster http:// push doesn't require
#    credentials; git-http-backend accepts push when http.receivepack=true).
#
#    The key is newline-stripped on the way in — a trailing newline in the
#    Secret value corrupts the auth header and every Claude call 401s.
kubectl create secret generic tide-secrets \
  --from-literal=ANTHROPIC_API_KEY="$(tr -d '\n\r' < /path/to/your/anthropic-key)" \
  --from-literal=GIT_PAT="" \
  -n tide-demo-calculator

# 9. Apply the Project CRD. TIDE picks it up; the controller starts the
#    clone Job (pure-Go HTTP clone from http://git-http-server/calculator.git),
#    then plans + dispatches the calculator outcome.
kubectl apply -f examples/projects/demo-calculator/project.yaml

# 10. Watch the run. The CLI streams phase/plan/task/wave transitions; the
#     dashboard shows the live planning + execution DAGs.
tide watch demo-calculator -n tide-demo-calculator
# In another terminal, for the dashboard:
kubectl port-forward svc/tide-dashboard 8080:80 -n tide-system
```

## Payoff

When the Project reaches `Complete`, pull the calculator out of the in-cluster
remote and open it:

```bash
# Port-forward the git-http server to your machine.
kubectl port-forward svc/git-http-server 8081:80 -n tide-demo-calculator

# Clone the repo and check out the per-run branch TIDE pushed.
git clone http://localhost:8081/calculator.git
cd calculator
git checkout "$(git branch -r | grep 'tide/run-demo-calculator-' | head -1 | sed 's|.*origin/||')"

# Open the calculator.
open index.html   # macOS; xdg-open on Linux
```

A working calculator — digits, add/subtract/multiply/divide, clear, equals,
keyboard input — built end-to-end by real Claude through TIDE's clone → plan →
execute → push loop.

## Budget cap behavior

`Project.Spec.budget.absoluteCapCents: 1000` ($10). If TIDE exceeds the cap
during the run, dispatch halts and `Project.Status.phase=BudgetExceeded`
fires (Phase 2 D-D2 + Phase 04.1 P4.1 rolling-window infra). The cap firing
is a LEGITIMATE outcome — it means the outcome prompt was too ambitious for
the budget; the recovery is to either tighten the prompt or raise the cap.

`tide approve --bypass-budget` exists as a manual-recovery affordance.
For details and the full set of recovery paths see
[`docs/troubleshooting.md`](../../../docs/troubleshooting.md).

## Cleanup

```bash
kubectl delete namespace tide-demo-calculator
```

The namespace deletion cascades to the Project, all child CRDs (Phase, Plan,
Task, Wave), the calculator-remote-init Job, the git-http-server Deployment,
the calculator-remote-pvc PVC, and the tide-secrets Secret via owner-refs
(where applicable) and namespace-scoped lifecycle.

## Related

- [`examples/projects/README.md`](../README.md) — cost-spectrum overview of the samples
- [`examples/projects/medium/README.md`](../medium/README.md) — the $5 sample this one is modeled on (Go fixture repo, Haiku)
- [`images/tide-git-http-server/`](../../../images/tide-git-http-server/) — the in-cluster HTTP git server image (doubles as the seed image here)
- [`docs/project-authoring.md`](../../../docs/project-authoring.md) — Project.Spec field reference (subagent image pin: `ghcr.io/jsquirrelz/tide-claude-subagent:1.0.9` must equal the chart appVersion)
