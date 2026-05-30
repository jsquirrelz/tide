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

The Project CRD references this Secret via `spec.subagent.credsSecretRef`. The controller pod itself **never sees the key** — it's mounted via `envFrom: secretRef:` directly into the subagent Job pod's container (the same pattern used for git PATs; see [docs/git-hosts.md](git-hosts.md) for the equivalent threat-model argument).

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

Reference from the Project CRD:

```yaml
apiVersion: tideproject.k8s/v1alpha1
kind: Project
metadata:
  name: my-project
  namespace: tide-sample-medium
spec:
  git:
    repoURL: https://github.com/my-org/my-repo.git
    credsSecretRef: tide-git-creds
```

**Per-host PAT scope guidance** — minimally-scoped fine-grained tokens for GitHub, GitLab, and Gitea — lives in [docs/git-hosts.md](git-hosts.md). That doc also covers SSH (deferred to v1.x), `gitleaks` per-Project overrides, and the manual smoke recipe for verifying a `tide/run-*` branch landed on your remote.

## First Project apply

The small sample uses the **$0 stub-subagent** — it exercises TIDE's full dispatch path (Project → Phase → Plan → Task → Wave reconciler) against a canned envelope, with no LLM cost and no API key required. It's the right starting point for a first install:

```bash
kubectl apply -f examples/projects/small/project.yaml
kubectl wait --for=jsonpath='{.status.phase}'=Complete project/small \
    --timeout=10m
```

Expected output:

```
project.tideproject.k8s/small condition met
```

This is also the timer-stop signal for the `make dry-run-v1` CI gate (see [docs/live-e2e.md](live-e2e.md) for the gate posture). If it completes on your install, the dispatch path is wired correctly end-to-end.

For the medium and large samples — and the cost-spectrum walkthrough — see [docs/project-authoring.md](project-authoring.md).

## Is TIDE right for me?

TIDE is opinionated. It's a good fit for some teams and a poor fit for others. The blunt version, before you invest in installing it:

### Yes, if:

- **You run Kubernetes** and want agentic coding pipelines that compose with the K8s ecosystem (Helm, Prometheus, OTel, RBAC, namespaces-per-tenant). TIDE is K8s-native by design — no parallel control plane.
- **You coordinate LLM dispatch across multiple developers** or a small platform team, and need shared observability, shared budget caps, shared gate policy. Single-developer workflows on a laptop don't justify the K8s overhead.
- **Your org needs audited LLM cost caps** with hard halt-on-cap semantics (Phase 2 D-D2 + Phase 04.1 P4.1 rolling-window reset). TIDE's `Project.Spec.budget.costCeilingCents` is a real circuit-breaker, not a soft warning.

### No, if:

- **You're a solo developer with a non-K8s workflow.** Use Claude Code directly, or one of the lightweight wrappers. TIDE's value compounds with team size and dispatch volume.
- **Your application is latency-critical.** TIDE is for **batch agentic work** — planning waves, Phase → Plan → Task descent, multi-minute reconciler loops. It's not the right substrate for interactive chat or sub-second inference.
- **Your environment has no observability tolerance** (no Prometheus, no OTel collector, no log aggregation). TIDE assumes you can read `kubectl logs`, query Prometheus, and follow OTel traces. Blind operation is technically possible but defeats the design intent.

If you're still here, the [first Project apply](#first-project-apply) above is the cheapest way to feel TIDE end-to-end before you decide.

## Next steps

- **[docs/project-authoring.md](project-authoring.md)** — author your first Project; walks the 3-sample cost spectrum (small / medium / large).
- **[docs/dashboard.md](dashboard.md)** — port-forward + ingress recipes; SSE behind reverse proxies.
- **[docs/cli.md](cli.md)** — `tide` CLI verbs reference (`tide approve`, `tide resume`, `tide cancel`).
- **[docs/gates.md](gates.md)** — per-level gate policy (auto-pass plans, approve milestones, etc.).
- **[docs/observability.md](observability.md)** — Prometheus metrics + OTel + OpenInference attribute names.
- **[docs/rbac.md](rbac.md)** — per-Kind verbs + namespace-scoped RoleBinding template for multi-tenant installs.
- **[docs/troubleshooting.md](troubleshooting.md)** — Symptom / Cause / Recipe table for the canonical install + first-apply + steady-state failure modes.
