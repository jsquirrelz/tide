# TIDE Dashboard

Phase 4 stub — Phase 5 expands with screenshots, full deployment matrices,
and a troubleshooting recipe set. Treat this as the minimum-install +
gotcha guide for v0.1.x.

## Install

The dashboard ships in the same Helm chart as the controller. Default values
enable it:

```bash
helm install tide ./charts/tide -n tide-system --create-namespace
```

That installs:

- `Deployment/tide-dashboard` — stateless read-only Go binary, single replica
- `Service/tide-dashboard` — `ClusterIP`, port `80` → `targetPort 8080`
- `ServiceAccount/tide-dashboard` + read-only `ClusterRole`
  + `ClusterRoleBinding` (RBAC scope: `{get, list, watch}` on the six TIDE CRDs
  + `pods`, plus `get` on `pods/log` — enforced read-only by
  `make helm-rbac-assert`)

Disable explicitly with `--set dashboard.enabled=false` for controller-only
installs.

## Accessing the UI

The chart ships `Service` only — no `Ingress`. Front it however your cluster
prefers. The simplest local-dev shape is `kubectl port-forward`:

```bash
kubectl port-forward -n tide-system svc/tide-dashboard 8080:80
open http://localhost:8080
```

For a real install, put any ingress controller in front of the Service. If
you use nginx-ingress, **Pitfall 23** (Server-Sent Events through reverse
proxies) requires a few annotations so SSE streams aren't buffered:

```yaml
metadata:
  annotations:
    nginx.ingress.kubernetes.io/proxy-buffering: "off"
    nginx.ingress.kubernetes.io/proxy-read-timeout: "3600"
    nginx.ingress.kubernetes.io/proxy-send-timeout: "3600"
```

Equivalent settings exist for HAProxy, Traefik, and Envoy — see Phase 5
docs for the full matrix.

## Auth

The dashboard is **read-only end-to-end**. Mutating actions (approve,
reject, resume, cancel) appear in the UI as copy-to-clipboard CLI commands
that the operator runs via `tide approve …` (or `kubectl annotate …`)
against the cluster. No tokens in the browser; no apiserver proxy with
write verbs; no per-user auth surface on the dashboard itself.

This design avoids needing dashboard SA RBAC beyond `{get, list, watch}`
(DASH-05). Phase 5 docs cover delegation patterns (OIDC in front of the
ingress) for multi-tenant clusters.

## Health endpoints

| Port | Path       | Purpose                                       |
| ---- | ---------- | --------------------------------------------- |
| 8080 | /healthz   | bare process liveness (no apiserver gate)     |
| 8080 | /api/v1/*  | read-only REST API (DASH-05 enforced at build)|
| 8081 | /healthz   | gated on informer-cache sync (`WaitForCacheSync`)|
| 8081 | /readyz    | gated on informer-cache sync                  |

The Helm `readinessProbe` targets `:8081` so kubelet only routes traffic
to a Pod once the informer cache is hot — a `kubectl rollout status` of
the dashboard Deployment is therefore an honest readiness signal.

## Configuration

| Helm value                          | Default                                  | Purpose                              |
| ----------------------------------- | ---------------------------------------- | ------------------------------------ |
| `dashboard.enabled`                 | `true`                                   | Toggle dashboard install             |
| `dashboard.image.repository`        | `ghcr.io/jsquirrelz/tide-dashboard`      | Container image                      |
| `dashboard.image.tag`               | `""` (→ `.Chart.AppVersion`)             | Image tag                            |
| `dashboard.replicas`                | `1`                                      | Replica count (stateless — safe >1)  |
| `dashboard.resources`               | `50m/64Mi req`, `200m/256Mi limit`       | Resource budget                      |
| `dashboard.service.{type,port,targetPort}` | `ClusterIP`, `80`, `8080`         | Service shape                        |

See `charts/tide/values.yaml` for the full inline-documented value block.

## What's coming

Phase 5 expands this doc with:

- Screenshots of the side-by-side Planning/Execution DAG view
- TaskDetailDrawer + log-stream walkthrough
- Multi-cluster ingress patterns
- Auth delegation recipes (OIDC, OAuth2 Proxy, dex)
- Operator runbook for SSE reconnect storms + log-stream cleanup
