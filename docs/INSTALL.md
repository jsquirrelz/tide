# TIDE Install Guide

**Audience:** Operators installing TIDE into a Kubernetes cluster — local kind/minikube for evaluation, or a real cluster (EKS / GKE / AKS / on-prem) for shared use.

**Status:** v1.0; ships the Helm chart pair (`tide` + `tide-crds`) via OCI registry (`ghcr.io/jsquirrelz/tide-charts/...`). The cloned-repo install path (`helm install ./charts/...`) is also documented for contributors and air-gapped users. Both surfaces ship the same chart bundle at the same version (`1.0.0`).

**Scope of this doc:**

- Prerequisites — versions and tooling
- Per-OS prerequisite install — macOS / Linux / Windows (WSL2)
- Install order (Pitfall 4 — CRDs first) — OCI install path + cloned-repo install path
- Provider Secret (`ANTHROPIC_API_KEY`)
- Git credentials Secret
- First Project apply (small sample, $0 stub-subagent)
- Is TIDE right for me? — 3 + 3 bullets to set expectation
- Next steps — forward links

## Prerequisites

| Tool      | Minimum version | Purpose                                                     |
| --------- | --------------- | ----------------------------------------------------------- |
| Docker    | 24.x or compatible (podman, nerdctl, colima) | Container runtime (kind requires it) |
| `kubectl` | 1.31+           | K8s API client (TIDE CRDs require ≥ 1.29 for CEL validation) |
| `helm`    | 3.16.x (≥ 3.8 for OCI install) | Package manager; OCI registry pull needs ≥ 3.8 |
| `kind`    | v0.31.0         | Local single-node K8s cluster (only required for local evaluation) |
| `go`      | 1.26+ (optional) | Only needed if you plan to build TIDE from source or contribute |

**API key requirement:** The README Quickstart small sample (`examples/projects/small/`) uses the **$0 stub-subagent** and needs **no** `ANTHROPIC_API_KEY`. The medium and large samples drive real Claude calls and require an API key. Wire the Secret only when you're ready for those samples — see [§ Provider Secret](#provider-secret-anthropic_api_key).

## Per-OS prerequisite install

### macOS (brew)

```bash
brew install kubectl helm kind
# Docker runtime: choose one
brew install --cask docker     # Docker Desktop (GUI)
# OR
brew install colima            # CLI-only alternative; lighter resource footprint
colima start                   # only if you chose colima
```

Verify:

```bash
docker version
kubectl version --client
helm version --short    # should report v3.16.x or newer
kind version            # should report v0.31.0
```

### Linux (curl / apt)

Versions pinned to match the dry-run baseline (D-D2 / RESEARCH §"P7.1") so your local install matches the install path CI exercises on every `v*-rc.*` release.

```bash
# kubectl 1.31.0
curl -LO "https://dl.k8s.io/release/v1.31.0/bin/linux/amd64/kubectl"
sudo install -m 0755 kubectl /usr/local/bin/kubectl

# helm 3.16.3
curl -fsSL https://raw.githubusercontent.com/helm/helm/main/scripts/get-helm-3 | bash -s -- --version v3.16.3

# kind v0.31.0
curl -Lo ./kind https://kind.sigs.k8s.io/dl/v0.31.0/kind-linux-amd64
chmod +x ./kind && sudo mv ./kind /usr/local/bin/kind
```

Docker runtime on Debian/Ubuntu:

```bash
sudo apt-get update
sudo apt-get install -y docker.io
sudo usermod -aG docker "$USER" && newgrp docker
```

On RHEL/Fedora, substitute `dnf install -y docker` (or `podman` — podman is a drop-in replacement for `docker` in kind v0.20+).

### Windows (WSL2)

TIDE installs only inside a Linux environment. On Windows, that means **WSL2 with Docker Desktop's WSL2 integration enabled** (or a native Linux VM). Native Windows containers are not supported.

1. Install **WSL2** with an Ubuntu 22.04 or later distribution: `wsl --install -d Ubuntu-22.04`.
2. Install **Docker Desktop for Windows** and enable WSL2 integration in Settings → Resources → WSL Integration → enable for your distro.
3. Open a WSL2 shell (`wsl -d Ubuntu-22.04`) and follow the [Linux (curl / apt)](#linux-curl--apt) recipes above — all commands run inside WSL2, not PowerShell.

Verify Docker is reachable from inside WSL2:

```bash
docker version    # should connect to Docker Desktop's daemon
```

If `docker` reports no daemon, re-check the WSL2 integration toggle in Docker Desktop.

## Install order (Pitfall 4 — CRDs first)

**Install `tide-crds` BEFORE `tide`.** The main `tide` chart's webhook configurations (validating + mutating + conversion) reference the six TIDE CRDs (`Project`, `Milestone`, `Phase`, `Plan`, `Task`, `Wave`); if those CRDs aren't registered, the webhook config templates fail to apply and `helm install tide ...` hangs at "wait for webhook service" — a confusing error mode that's avoided entirely by installing the CRD subchart first.

The chart pair is intentionally **NOT** wired as a Helm dependency (per D-E1's upgrade safety rationale — splitting the CRDs into their own chart lets `helm upgrade tide ...` roll the controller without touching CRD storage). You must install both, in this exact order.

**Upgrades follow the same order — `tide-crds` before `tide`.** When a release adds new `Project.Spec` fields (e.g. `spec.git.baseRef`), those fields only exist in the cluster once the `tide-crds` chart carrying the newer CRD schema is applied. If you `helm upgrade tide ...` against a stale CRD schema, the API server **silently prunes** the unknown fields on admission — no error, no warning. A Project authored with the new field is accepted with that field dropped, and the run quietly bases from `HEAD` as if `baseRef` were never set. Always `helm upgrade tide-crds ...` first, confirm the new field is present (`kubectl get crd projects.tideproject.k8s -o yaml | grep <field>`), then upgrade `tide`.

### cert-manager prerequisite

The main `tide` chart's webhook and metrics Certificate resources — specifically `charts/tide/templates/serving-cert.yaml`, `charts/tide/templates/selfsigned-issuer.yaml`, and `charts/tide/templates/metrics-certs.yaml` — reference `cert-manager.io/v1` kinds. Helm refuses to apply custom resources whose CRDs are not registered in the cluster, so cert-manager **must be installed before the `tide` chart's helm-install step** below. The `tide-crds` subchart does **not** depend on cert-manager — only the main `tide` chart does — so cert-manager can install in either order relative to `tide-crds`, as long as it lands before `tide`.

Install cert-manager (pinned default `v1.20.2`):

```bash
kubectl apply -f https://github.com/cert-manager/cert-manager/releases/download/v1.20.2/cert-manager.yaml
```

Wait for all three cert-manager Deployments to roll out before proceeding:

```bash
kubectl -n cert-manager rollout status deployment/cert-manager --timeout=120s
kubectl -n cert-manager rollout status deployment/cert-manager-cainjector --timeout=120s
kubectl -n cert-manager rollout status deployment/cert-manager-webhook --timeout=120s
```

`v1.20.2` is the project's tested default — it's the same version pinned in `test/integration/kind/suite_test.go` (the Layer B integration harness) and `hack/scripts/acceptance-v1.sh` (the maintainer acceptance ritual), and it's compatible with the K8s 1.33 `kindest/node` pin used across both. Maintainers running the acceptance ritual can override via the `TIDE_CERT_MANAGER_VERSION` environment variable; the integration harness honors the same name, so the two recipes stay in lockstep.

cert-manager's broader role in the cluster — beyond the chart's own Certificate resources — is also touched on by [docs/observability.md](./observability.md) (metrics-server cert wiring) and [docs/rbac.md](./rbac.md) (RBAC for cert-manager's own service accounts). Refer to those docs for the full operational picture; this prerequisite section is scoped specifically to "what `helm install tide` needs to succeed."

### Quickstart (OCI registry — primary path)

```bash
kind create cluster --name tide
helm install tide-crds oci://ghcr.io/jsquirrelz/tide-charts/tide-crds \
    --version 1.0.0 -n tide-system --create-namespace
helm install tide oci://ghcr.io/jsquirrelz/tide-charts/tide \
    --version 1.0.0 -n tide-system
kubectl wait --for=condition=Available deploy/tide-controller-manager \
    -n tide-system --timeout=5m
```

Expected output of the last command:

```
deployment.apps/tide-controller-manager condition met
```

### Cloned-repo install path

For contributors or air-gapped environments that have already cloned this repo:

```bash
git clone https://github.com/jsquirrelz/tide && cd tide
kind create cluster --name tide
helm install tide-crds ./charts/tide-crds -n tide-system --create-namespace
helm install tide ./charts/tide -n tide-system
kubectl wait --for=condition=Available deploy/tide-controller-manager \
    -n tide-system --timeout=5m
```

Both paths ship identical chart bundles at the same `--version 1.0.0`; the OCI path is preferred for fresh installs because it avoids cloning the entire repo just to get the charts.

### Verifying the install

```bash
# CRDs registered (six of them)
kubectl get crd | grep tideproject.k8s

# Controller manager Ready
kubectl get deploy -n tide-system tide-controller-manager
kubectl logs -n tide-system deploy/tide-controller-manager --tail=20

# Dashboard reachable (port-forward; see docs/dashboard.md for ingress)
kubectl port-forward -n tide-system svc/tide-dashboard 8080:80
open http://localhost:8080
```

If any of these fail, see [docs/troubleshooting.md](troubleshooting.md) for canonical recipes (CRDs not registered → forgot the subchart; dashboard 404 → port-forward not running; `ImagePullBackoff` → image pull secret missing).

### Enable telemetry (Prometheus)

By default TIDE's run telemetry beyond the budget tally is **dark** — `prometheus.enabled` is `false`, no ServiceMonitor is shipped, and the dashboard's cost-over-time charts fall back to live-only CRD views. This step wires the full telemetry path: a Prometheus install, TIDE's scrape target, and the dashboard's PromQL proxy.

**1. Install kube-prometheus-stack** (skip if your cluster already runs a prometheus-operator — see the existing-Prometheus note below):

```bash
helm repo add prometheus-community https://prometheus-community.github.io/helm-charts
helm repo update
helm install kps prometheus-community/kube-prometheus-stack \
    -n monitoring --create-namespace
```

**2. Enable TIDE's telemetry surfaces.** Three values, three roles: `prometheus.enabled` declares telemetry wired (drives the NOTES.txt warning and the dashboard banner), `prometheus.serviceMonitor.enabled` ships the scrape target (default-off to avoid CRD-not-found on plain clusters), and `prometheus.endpoint` points the dashboard's PromQL proxy at your Prometheus:

```bash
helm upgrade tide oci://ghcr.io/jsquirrelz/tide-charts/tide \
    -n tide-system --reuse-values \
    --set prometheus.enabled=true \
    --set prometheus.serviceMonitor.enabled=true \
    --set prometheus.endpoint=http://prometheus-operated.monitoring:9090
```

**3. Label the ServiceMonitor.** kube-prometheus-stack's Prometheus selects only ServiceMonitors carrying `release: <its-release-name>`; TIDE's ServiceMonitor ships without that label, so a default kube-prometheus-stack install silently ignores it. Add the label (matching the `kps` release name from step 1):

```bash
kubectl -n tide-system label servicemonitor -l control-plane=controller-manager release=kps
```

Alternative: set `prometheus.prometheusSpec.serviceMonitorSelectorNilUsesHelmValues=false` on the kube-prometheus-stack install so it selects all ServiceMonitors regardless of label.

**4. Verify at the Targets page** — this is the done signal:

```bash
kubectl -n monitoring port-forward svc/prometheus-operated 9090:9090
open http://localhost:9090
```

Navigate to **Status → Targets** and confirm the `tide-...-metrics` endpoint shows **UP**. Once it's green, the dashboard's Telemetry tab serves token spend over time, dispatch counts, and per-level durations.

**Existing Prometheus (not kube-prometheus-stack):** your prometheus-operator must select the `tide-system` namespace and the ServiceMonitor's labels (`control-plane: controller-manager` plus the chart's standard labels) — or add whatever label your `serviceMonitorSelector` requires, as in step 3. Then set `prometheus.endpoint` to your Prometheus service URL instead of `http://prometheus-operated.monitoring:9090`.

## Provider Secret (`ANTHROPIC_API_KEY`)

The medium and large sample Projects drive real Claude calls and require an `ANTHROPIC_API_KEY`. The small sample uses the stub-subagent and skips this entirely.

Create the Secret in the **Project's namespace** (not `tide-system`). The convention used by the bundled samples is `tide-sample-<size>`:

```bash
export ANTHROPIC_API_KEY='sk-ant-...'   # set in your shell; never paste into YAML
kubectl create namespace tide-sample-medium
kubectl create secret generic tide-anthropic-creds \
    --namespace tide-sample-medium \
    --from-literal=ANTHROPIC_API_KEY="$ANTHROPIC_API_KEY"
```

The Project CRD references this Secret via the top-level `spec.providerSecretRef` (the convenience alias for `spec.secretRefs.anthropicAPIKey`) — NOT under `spec.subagent`. The controller pod itself **never sees the key** — it's mounted via `envFrom: secretRef:` directly into the subagent Job pod's container (the same pattern used for git PATs; see [docs/git-hosts.md](git-hosts.md) for the equivalent threat-model argument).

**Never paste the raw key into a YAML manifest** — using `--from-literal` keeps the key out of source control and out of `helm install --dry-run` output. See [docs/project-authoring.md](project-authoring.md) for which `Project.Spec` fields reference this Secret.

If the controller logs `401 unauthorized` from the Anthropic API, the key is wrong — see the [troubleshooting recipe](troubleshooting.md) for the "recreate secret + rollout restart" sequence.

## Git credentials Secret

TIDE's push Job clones, commits, and pushes via HTTPS using a Personal Access Token (PAT). The PAT is supplied as the environment variable `GIT_PAT` via `envFrom: secretRef:`, exactly the same pattern as the `ANTHROPIC_API_KEY` above.

```bash
export GIT_PAT='ghp_...'    # or glpat_..., or gitea token — host-agnostic
kubectl create secret generic tide-git-creds \
    --namespace tide-sample-medium \
    --from-literal=GIT_PAT="$GIT_PAT"
```

A **complete real-world Project** wires the provider key, the subagent image + model,
a real budget cap, and the git push target. Field names matter — copy this shape (see
[docs/project-authoring.md](project-authoring.md) for every field + the medium/large
samples):

```yaml
apiVersion: tideproject.k8s/v1alpha1
kind: Project
metadata:
  name: my-project
  namespace: tide-sample-medium
spec:
  outcomePrompt: "<what you want TIDE to build, in plain prose>"
  targetRepo: https://github.com/my-org/my-repo.git   # CEL: http(s):// or git@ only — file:// is rejected
  providerSecretRef: tide-anthropic-creds              # top-level; carries ANTHROPIC_API_KEY
  subagent:
    image: ghcr.io/jsquirrelz/tide-claude-subagent:1.0.0   # REAL Claude (omit → falls back to the chart's claude image, NOT the stub)
    model: claude-haiku-4-5                            # cheap default; override per level below
    levels:
      milestone: { model: claude-sonnet-4-6 }          # stronger model for planning bounds task count
      phase:     { model: claude-sonnet-4-6 }
      plan:      { model: claude-sonnet-4-6 }
      task:      { model: claude-haiku-4-5 }            # mechanical task execution — cheap
  budget:
    absoluteCapCents: 2500                             # $25 hard cap. NEVER 0 in production (0 = unlimited).
  git:
    repoURL: https://github.com/my-org/my-repo.git     # push target (same as targetRepo for a single-repo run)
    credsSecretRef: tide-git-creds                     # carries GIT_PAT (push-scoped)
```

Before applying a real Project, the Project's namespace must be bootstrapped with the
per-namespace resources (subagent ServiceAccount, RWX workspace PVC, signing key, push/
reporter RBAC) — see **[Bootstrapping a Project namespace](#bootstrapping-a-project-namespace)**
below. And read the **[Production checklist](production.md)** before pointing TIDE at a
repo that matters.

**Per-host PAT scope guidance** — minimally-scoped fine-grained tokens for GitHub, GitLab, and Gitea — lives in [docs/git-hosts.md](git-hosts.md). That doc also covers SSH (deferred to v1.x), `gitleaks` per-Project overrides, and the manual smoke recipe for verifying a `tide/run-*` branch landed on your remote.

## Bootstrapping a Project namespace

The Helm chart provisions the workspace PVC, the subagent/push/reporter ServiceAccounts,
and their RBAC **in `tide-system`**. A Project that runs in a *different* namespace (the
recommended pattern — one namespace per Project/tenant) needs those same resources
**mirrored into the Project's namespace**, or the clone/subagent/push/reporter Jobs fail
to schedule (e.g. `serviceaccount "tide-push" not found`, or the reporter can't create
child CRDs). The required set per namespace:

| Resource | Purpose |
|----------|---------|
| `tide-projects` PVC | Shared workspace for clone/subagent/push Jobs. **Production: `ReadWriteMany`** (multiple Jobs mount it concurrently) — needs an RWX StorageClass (see [docs/rwx-drivers.md](rwx-drivers.md)). On kind/minikube (RWO only) use `ReadWriteOnce`; wave sequencing keeps mounts serial. |
| `tide-subagent` SA | Identity the authoring/executor subagent pods run as. |
| `tide-push` SA + Role + RoleBinding | Identity for clone/push Jobs; Role grants `get` on Secrets (to read the git-creds Secret). |
| `tide-reporter` SA + Role + RoleBinding | Identity for the in-namespace reader Job; Role grants `create/get/list` on the 5 child CRD kinds + `get` on projects. |
| `tide-signing-key` Secret | Cluster-unique signing key — **copied from `tide-system`** (not generated per namespace). |

The bundled `examples/projects/medium/per-namespace-resources.yaml` is the canonical
template (and documents the RWX/signing-key notes inline). To prep your own namespace,
copy it and substitute the namespace, then copy the signing key from `tide-system`:

```bash
export NS=my-project-ns
kubectl create namespace "$NS"

# 1. PVC + SAs + RBAC (edit the namespace, or sed the sample template):
sed "s/tide-sample-medium/$NS/g" examples/projects/medium/per-namespace-resources.yaml \
  | kubectl apply -f -

# 2. Copy the cluster signing key from tide-system into the Project namespace:
kubectl get secret tide-signing-key -n tide-system -o yaml \
  | sed "s/namespace: tide-system/namespace: $NS/" \
  | kubectl apply -f -
```

> **RWX requirement:** the workspace PVC must be `ReadWriteMany` for a real run (concurrent
> wave tasks + clone/push mount it at once). If your cluster has no RWX StorageClass, see
> [docs/rwx-drivers.md](rwx-drivers.md) for supported drivers. RWO works only for serialized
> single-mount kind/minikube testing.

## First Project apply

The small sample uses the **$0 stub-subagent** — it exercises TIDE's full dispatch path (Project → Phase → Plan → Task → Wave reconciler) against a canned envelope, with no LLM cost and no API key required. It's the right starting point for a first install:

```bash
kubectl apply -f examples/projects/small/project.yaml

# Mirror the cluster-unique signing key into the sample namespace (the chart
# generates it in tide-system; dispatch Job pods read it from their own namespace):
SIGNING_KEY=$(kubectl get secret tide-signing-key -n tide-system -o jsonpath='{.data.TIDE_SIGNING_KEY}')
kubectl apply -f - <<EOF
apiVersion: v1
kind: Secret
metadata: { name: tide-signing-key, namespace: tide-sample-small }
type: Opaque
data: { TIDE_SIGNING_KEY: ${SIGNING_KEY} }
EOF

kubectl wait --for=jsonpath='{.status.phase}'=Complete project/small-project \
    -n tide-sample-small --timeout=10m
```

The sample's multi-doc YAML carries the rest of the per-namespace bootstrap
(`tide-projects` PVC, `tide-subagent`/`tide-reporter` ServiceAccounts + RBAC,
and a throwaway PVC-warmup pod for WaitForFirstConsumer storage classes) —
only the signing-key copy above is dynamic.

Expected output:

```
project.tideproject.k8s/small-project condition met
```

This is also the timer-stop signal for the `make dry-run-v1` CI gate (see [docs/live-e2e.md](live-e2e.md) for the gate posture). If it completes on your install, the dispatch path is wired correctly end-to-end.

For the medium and large samples — and the cost-spectrum walkthrough — see [docs/project-authoring.md](project-authoring.md).

## Is TIDE right for me?

TIDE is opinionated. It's a good fit for some teams and a poor fit for others. The blunt version, before you invest in installing it:

### Yes, if:

- **You run Kubernetes** and want agentic coding pipelines that compose with the K8s ecosystem (Helm, Prometheus, OTel, RBAC, namespaces-per-tenant). TIDE is K8s-native by design — no parallel control plane.
- **You coordinate LLM dispatch across multiple developers** or a small platform team, and need shared observability, shared budget caps, shared gate policy. Single-developer workflows on a laptop don't justify the K8s overhead.
- **Your org needs audited LLM cost caps** with hard halt-on-cap semantics (Phase 2 D-D2 + Phase 04.1 P4.1 rolling-window reset). TIDE's `Project.Spec.budget.absoluteCapCents` is a real circuit-breaker, not a soft warning. **Note:** the cap is only enforced when `> 0` — `absoluteCapCents: 0` means *unlimited*, not "no spend" (see [docs/project-authoring.md](project-authoring.md) and the production checklist below). Always set a real cap in production.

### No, if:

- **You're a solo developer with a non-K8s workflow.** Use Claude Code directly, or one of the lightweight wrappers. TIDE's value compounds with team size and dispatch volume.
- **Your application is latency-critical.** TIDE is for **batch agentic work** — planning waves, Phase → Plan → Task descent, multi-minute reconciler loops. It's not the right substrate for interactive chat or sub-second inference.
- **Your environment has no observability tolerance** (no Prometheus, no OTel collector, no log aggregation). TIDE assumes you can read `kubectl logs`, query Prometheus, and follow OTel traces. Blind operation is technically possible but defeats the design intent.

If you're still here, the [first Project apply](#first-project-apply) above is the cheapest way to feel TIDE end-to-end before you decide.

## Maintainer: image-publish pipeline

This section is for maintainers who are cutting releases or verifying the pre-publish local path. Operators installing from an already-published `v1.0.0` tag can skip to [Next steps](#next-steps).

### What publishes

The 6 TIDE component images are published to `ghcr.io/jsquirrelz/` as multi-arch (`linux/amd64` + `linux/arm64`) OCI images:

| Image name | Source Dockerfile |
|------------|-------------------|
| `ghcr.io/jsquirrelz/tide-controller` | `./Dockerfile` |
| `ghcr.io/jsquirrelz/tide-dashboard` | `./Dockerfile.dashboard` |
| `ghcr.io/jsquirrelz/tide-stub-subagent` | `images/stub-subagent/Dockerfile` |
| `ghcr.io/jsquirrelz/tide-credproxy` | `images/credproxy/Dockerfile` |
| `ghcr.io/jsquirrelz/tide-push` | `images/tide-push/Dockerfile` |
| `ghcr.io/jsquirrelz/tide-claude-subagent` | `images/claude-subagent/Dockerfile` |

### CI pipeline (IMG-01)

Images publish automatically via the `build-images` matrix job in `.github/workflows/release.yaml` when a `v*` tag is pushed. Release-candidate tags (`v*-rc.*`) do **not** trigger image publish — they only fire the chart-tree reproducibility gate (`helmify-verify`).

Run order for a full release tag (e.g. `v1.0.0`):

```
helmify-verify
    ├── build-images   (parallel with pre-flight — builds + pushes all 6 images)
    └── pre-flight
            └── release
                    └── chart-publish   (after both build-images and release succeed)
```

The chart is only published after all 6 `build-images` matrix legs succeed — images are always pushed before the chart (D-04).

**Image tag convention:** The `build-images` job strips the `v` prefix from the git tag via `${GITHUB_REF_NAME#v}`, so `v1.0.0` produces image tag `1.0.0`. This matches the Helm chart `appVersion` exactly — the chart's `| default .Chart.AppVersion` fallback and the published image tags resolve to the same string without any manual alignment.

### Local pre-publish path (IMG-LOAD-01)

Before cutting the `v1.0.0` tag, verify the full TIDE boot path at $0 cost using locally-built images:

```bash
make acceptance-v1-smoke
```

This runs `ACCEPTANCE_SAMPLE=small hack/scripts/acceptance-v1.sh`, which:

1. Calls `hack/scripts/load-images-if-needed.sh <cluster_name> 1.0.0` for each of the 6 images.
2. For each image, probes `docker manifest inspect ghcr.io/jsquirrelz/<name>:1.0.0`. If absent (pre-publish), builds the image locally via `docker build` and loads it into the kind cluster via `kind load docker-image ... --name <cluster>`.
3. Installs cert-manager, the `tide-crds` chart, and the `tide` chart against the locally-loaded images.
4. Applies `examples/projects/small/project.yaml` (the $0 stub-subagent sample) and waits up to 10 minutes for `status.phase=Complete`.

No `ANTHROPIC_API_KEY` is required for `acceptance-v1-smoke`. The stub-subagent exercises TIDE's full dispatch path (Project → Phase → Plan → Task → Wave reconciler) against a canned envelope at zero LLM cost.

### GHCR visibility (Pitfall 3 — act on first push)

The first push of each new package to `ghcr.io/jsquirrelz/` defaults to **private**. After the first successful `build-images` CI run, the maintainer must set each package to public:

1. Navigate to `https://github.com/users/jsquirrelz/packages/container/<name>/settings` for each of the 6 image names above.
2. Under "Danger Zone", set **Package visibility** to **Public**.

Until this is done, `docker manifest inspect` returns `401 Unauthorized` even though the image exists. Downstream `helm install` attempts will surface this as `ImagePullBackOff` (see [docs/troubleshooting.md](troubleshooting.md) for the recipe).

### Existence check

Verify a specific image is published and public:

```bash
docker manifest inspect ghcr.io/jsquirrelz/tide-controller:1.0.0
```

Exits `0` if the image is published and publicly accessible; non-zero if absent or private. Run for each of the 7 images before announcing a release.

## Uninstall

Two releases, two very different blast radii — order matters:

```bash
# 1. Remove the controller + dashboard. SAFE: your Project/Milestone/Phase/
#    Plan/Task/Wave CRs (and their status history) remain in etcd untouched.
helm uninstall tide -n tide-system

# 2. Remove the CRDs. ⚠️ DESTRUCTIVE: deleting a CustomResourceDefinition
#    makes Kubernetes garbage-collect EVERY custom resource of that Kind,
#    cluster-wide — all Projects in all namespaces, plus the entire child
#    hierarchy, irreversibly. Back up first if any run history matters:
kubectl get projects,milestones,phases,plans,tasks,waves -A -o yaml > tide-backup.yaml
helm uninstall tide-crds -n tide-system
```

Per-namespace resources you created while bootstrapping Project namespaces
(`tide-projects` PVC, ServiceAccounts, the `tide-signing-key` copy) are not
chart-managed — delete the namespaces or the resources directly.

## Next steps

- **[docs/production.md](production.md)** — READ BEFORE a real-Claude run against a repo you care about: repo-safety contract, budget safety, cluster sizing, gates, v1.0 limitations, pre-flight checklist.
- **[docs/project-authoring.md](project-authoring.md)** — author your first Project; walks the 3-sample cost spectrum (small / medium / large).
- **[docs/dashboard.md](dashboard.md)** — port-forward + ingress recipes; SSE behind reverse proxies.
- **[docs/cli.md](cli.md)** — `tide` CLI verbs reference (`tide approve`, `tide resume`, `tide cancel`).
- **[docs/gates.md](gates.md)** — per-level gate policy (auto-pass plans, approve milestones, etc.).
- **[docs/observability.md](observability.md)** — Prometheus metrics + OTel + OpenInference attribute names.
- **[docs/rbac.md](rbac.md)** — per-Kind verbs + namespace-scoped RoleBinding template for multi-tenant installs.
- **[docs/troubleshooting.md](troubleshooting.md)** — Symptom / Cause / Recipe table for the canonical install + first-apply + steady-state failure modes.
