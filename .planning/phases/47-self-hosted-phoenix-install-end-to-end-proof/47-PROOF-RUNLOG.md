# Phase 47 Plan 04: Live Proof Runlog

Real commands actually run to stand up `tide-phoenix-proof`, install self-hosted
Phoenix per `docs/INSTALL.md` § "Enable tracing (Phoenix)", and drive the
`examples/projects/medium` real-spend proof run. Secret names only — no
credential values appear anywhere below (T-47-05).

## Task 1: Offline pre-flight, fresh cluster, TIDE at local HEAD

### Offline pre-flight (BEFORE any cluster command — D-12)

**1. `make helm-assert` — green, all 9 render-gate permutations passed:**

```
$ make helm-assert
PASS: dashboard RBAC is read-only
PASS: PROM_ENDPOINT env var is absent from dashboard container (expected)
PASS: PROM_ENDPOINT='http://prom:9090' on dashboard container (expected)
PASS: PHOENIX_BASE_URL env var is absent from dashboard container (expected)
PASS: PHOENIX_BASE_URL='http://phoenix:6006' on dashboard container (expected)
PASS: OTEL_EXPORTER_OTLP_HEADERS env var is absent from manager container (expected)
PASS: OTEL_EXPORTER_OTLP_HEADERS env var is absent from dashboard container (expected)
PASS: OTEL_EXPORTER_OTLP_HEADERS on manager container is Secret-sourced via secretKeyRef(...) with no literal value (expected)
PASS: OTEL_EXPORTER_OTLP_HEADERS on dashboard container is Secret-sourced via secretKeyRef(...) with no literal value (expected)
--- Permutations A-I --- all PASS
PASS: all 9 permutations passed — EC-7 + OBS-01 + D-10 render gates satisfied
```

**2. TIDE chart rendered with the proof values (endpoint/headersSecretRef/phoenix.baseURL) — verbatim env assertions:**

```bash
helm template charts/tide \
  --set dashboard.enabled=true \
  --set otel.exporter.endpoint=tide-phoenix-svc.phoenix.svc.cluster.local:4317 \
  --set otel.exporter.headersSecretRef.name=tide-otlp-headers \
  --set phoenix.baseURL=http://localhost:6006 \
  > /tmp/47-04-proof/tide-render-proof-values.yaml
```

- Render exit 0.
- `OTEL_EXPORTER_OTLP_ENDPOINT` value on the manager container: `tide-phoenix-svc.phoenix.svc.cluster.local:4317` (bare `host:port`, verbatim, no scheme).
- `PHOENIX_BASE_URL` value: `http://localhost:6006`.
- `python3 hack/helm/assert-otlp-headers-env.py /tmp/47-04-proof/tide-render-proof-values.yaml --expect-secretref tide-otlp-headers OTEL_EXPORTER_OTLP_HEADERS` →
  ```
  PASS: OTEL_EXPORTER_OTLP_HEADERS on manager container is Secret-sourced via secretKeyRef(name='tide-otlp-headers', key='OTEL_EXPORTER_OTLP_HEADERS') with no literal value (expected)
  PASS: OTEL_EXPORTER_OTLP_HEADERS on dashboard container is Secret-sourced via secretKeyRef(name='tide-otlp-headers', key='OTEL_EXPORTER_OTLP_HEADERS') with no literal value (expected)
  ```
  Exit 0.

**3. Phoenix chart rendered offline with the docs' quickstart values:**

```bash
helm template tide-phoenix oci://registry-1.docker.io/arizephoenix/phoenix-helm \
    --version 10.0.1 \
    --namespace phoenix \
    --set persistence.enabled=true \
    --set persistence.size=2Gi \
    --set postgresql.enabled=false \
    --set database.defaultRetentionPolicyDays=7 \
    --set service.type=ClusterIP \
    --set ingress.enabled=false \
    --set auth.enableAuth=true
```

- `Pulled: registry-1.docker.io/arizephoenix/phoenix-helm:10.0.1`, `Digest: sha256:2761ea88bf7edae122e1c28b255c54c56d77c66eb9827a87df5f39f2ab184784`.
- Render exit 0 — `phoenix.validatePersistence` passed (SQLite-on-PVC / Postgres-off combination is legal).
- Rendered `Service` name: **`tide-phoenix-svc`** in namespace `phoenix`, ports `4317` (grpc), `6006` (app/UI), `9090` (metrics) — matches every doc example verbatim. No doc divergence found; zero-divergence is a valid, recorded outcome for this render.
- Chart-pin freshness re-check: `docker manifest inspect registry-1.docker.io/arizephoenix/phoenix-helm:10.0.1` → exit 0, `org.opencontainers.image.version: "10.0.1"` — the pin recorded by Plan 47-03 (fetched same day) is still current; no drift in the hours since.

All three pre-flight checks green BEFORE any cluster command. Zero API spend so far.

### Cluster bring-up

**4. `kind get clusters` before teardown:** `tide-dogfood` (stale, pre-v1alpha3, confirmed by the orchestrator's machine facts).

`kind delete cluster --name tide-dogfood` initially failed with a Docker daemon
error (`could not kill container: tried to kill container, but did not receive
an exit event`) — transient Docker Desktop hang, not a TIDE issue. Recovered
with `docker rm -f -v tide-dogfood-control-plane` (exit 0), then `kind delete
cluster --name tide-dogfood` succeeded (exit 0). `kind get clusters` afterward:
`No kind clusters found.` — zero clusters remained, confirming the full 2.65 GiB
budget was freed before creating the new cluster (D-11).

**5. `kind create cluster --name tide-phoenix-proof`** — succeeded, control-plane
Ready, CNI + StorageClass installed, context set to `kind-tide-phoenix-proof`.

**6. cert-manager v1.20.2 installed per docs/INSTALL.md:**

```bash
kubectl apply -f https://github.com/cert-manager/cert-manager/releases/download/v1.20.2/cert-manager.yaml
kubectl -n cert-manager rollout status deployment/cert-manager --timeout=120s
kubectl -n cert-manager rollout status deployment/cert-manager-cainjector --timeout=120s
kubectl -n cert-manager rollout status deployment/cert-manager-webhook --timeout=120s
```

All three Deployments rolled out successfully.

**7. Local HEAD images built + kind-loaded** (honest substitution note: the
published OCI images predate this phase's OTLP-headers wiring, so the proof
builds and loads local HEAD images tagged to match the chart's default
`appVersion` (`1.0.7`) so no image-tag override is needed at install time).
Image set mirrors `hack/scripts/acceptance-v1.sh` / `load-images-if-needed.sh`'s
7-image inventory (minus `tide-stub-subagent`, not needed for a real-Claude run)
plus the two medium-sample-only fixture images:

| Image | Tag | Dockerfile |
|---|---|---|
| `ghcr.io/jsquirrelz/tide-controller` | `1.0.7` | `./Dockerfile` |
| `ghcr.io/jsquirrelz/tide-dashboard` | `1.0.7` | `./Dockerfile.dashboard` |
| `ghcr.io/jsquirrelz/tide-credproxy` | `1.0.7` | `images/credproxy/Dockerfile` |
| `ghcr.io/jsquirrelz/tide-push` | `1.0.7` | `images/tide-push/Dockerfile` |
| `ghcr.io/jsquirrelz/tide-claude-subagent` | `1.0.7` | `images/claude-subagent/Dockerfile` |
| `ghcr.io/jsquirrelz/tide-reporter` | `1.0.7` | `images/tide-reporter/Dockerfile` |
| `ghcr.io/jsquirrelz/tide-demo-init` | `1.0.0` | `images/tide-demo-init/Dockerfile` |
| `ghcr.io/jsquirrelz/tide-git-http-server` | `1.0.0` | `images/tide-git-http-server/Dockerfile` |

**Doc/code divergence found + root-fixed (Rule 1/3 auto-fix, not a doc bug):**
`images/tide-reporter/Dockerfile`'s `COPY` list only carried
`internal/reporter/`, `internal/owner/`, `api/`, `pkg/dispatch/`,
`cmd/tide-reporter/` — a comment dated to an earlier phase. Phases 44-46 added
new imports to `cmd/tide-reporter/main.go` and `internal/reporter/tracesynth.go`
(`internal/otelinit`, `internal/harness/redact`, `internal/subagent/common`,
`pkg/otelai`) without updating the Dockerfile's COPY list, so the real
production build (not just this proof) was broken: `go build` failed with
`no required module provides package ...` for all four. Verified each of the
four has no further local transitive deps beyond what the Dockerfile already
copies (`grep -rn jsquirrelz/tide` on each package returned nothing beyond the
K8s/OTel SDKs). Fixed the Dockerfile's COPY list to add all four directories
and updated the stale comment; rebuild succeeded (`docker build` exit 0).

All 8 images kind-loaded into `tide-phoenix-proof` via
`kind load docker-image <img> --name tide-phoenix-proof` — all reported "not
yet present ... loading" (fresh cluster, first load).

**8. `tide-crds` then `tide` chart installed** (tracing values NOT yet set —
Task 2 wires them after Phoenix exists):

```bash
helm install tide-crds ./charts/tide-crds -n tide-system --create-namespace
helm install tide ./charts/tide -n tide-system \
  --set 'workspaces.pvc.accessModes={ReadWriteOnce}' \
  --set dashboard.enabled=true
kubectl wait --for=condition=Available deploy/tide-controller-manager -n tide-system --timeout=5m
```

**Live D-10 tracing-dark NOTES.txt evidence** (captured verbatim from the
`helm install tide` output, tracing values not yet set):

```
NOTES:
TIDE 1.0.7 installed in tide-system.

Dashboard:  kubectl -n tide-system port-forward svc/tide-dashboard 8080:80
Docs:       https://github.com/jsquirrelz/tide/blob/main/docs/INSTALL.md

WARNING: run telemetry beyond the budget tally is unavailable —
prometheus.enabled is false.
Token spend over time, dispatch counts, and per-level durations will be dark.
Enable: see the "Enable telemetry" step in docs/INSTALL.md.

WARNING: tracing is dark — otel.exporter.endpoint is empty.
Run trace trees (five levels, plus redacted LLM message spans) are not
exported anywhere, and the dashboard's Phoenix deep links stay hidden.
Enable: see the "Enable tracing" step in docs/INSTALL.md.
```

`deployment.apps/tide-controller-manager condition met`.

### Task 1 verification

```
$ kind get clusters
tide-phoenix-proof

$ kubectl --context kind-tide-phoenix-proof get pods -n tide-system --no-headers | grep -c Running
2

$ kubectl --context kind-tide-phoenix-proof get crd projects.tideproject.k8s -o jsonpath='{.spec.versions[*].name}'
v1alpha3
```

Manager + dashboard pods Running; controller logs show all 6 reconcilers
(project, milestone, phase, plan, task, wave) started cleanly with the
reservation store rederived at `totalReservedCents: 0` — no errors.

**Zero API spend through end of Task 1.**

## Task 2: Phoenix install, API key + headers Secret, tracing upgrade, no-spend checks

### 1. Phoenix install (docs' pinned version + quickstart values, namespace `phoenix`)

```bash
helm install tide-phoenix oci://registry-1.docker.io/arizephoenix/phoenix-helm \
    --version 10.0.1 \
    --namespace phoenix --create-namespace \
    --set persistence.enabled=true \
    --set persistence.size=2Gi \
    --set postgresql.enabled=false \
    --set database.defaultRetentionPolicyDays=7 \
    --set service.type=ClusterIP \
    --set ingress.enabled=false \
    --set auth.enableAuth=true
kubectl -n phoenix rollout status deploy --timeout=5m
```

- `Pulled: registry-1.docker.io/arizephoenix/phoenix-helm:10.0.1` — same pin, same digest as the offline pre-flight render; no drift.
- Chart NOTES printed a generic `WARNING: PostgreSQL is disabled but no external database is configured!` — this is the chart's own boilerplate warning that fires whenever `postgresql.enabled=false` regardless of the `persistence.enabled=true` SQLite-on-PVC alternative being correctly configured; not a doc bug (the SQLite path is documented by Arize as the deliberate alternative to the Postgres default, and the render's own `phoenix.validatePersistence` guard — which DOES understand the exclusivity — passed both offline and live). Not a divergence worth a doc edit; noting it here for completeness.
- `deployment "tide-phoenix" successfully rolled out`.
- `kubectl -n phoenix get pvc`: `tide-phoenix-data-pvc` **Bound**, capacity **2Gi**, RWO.
- `helm list -n phoenix`: `tide-phoenix` / `phoenix-helm-10.0.1` / APP VERSION `18.1.0` — matches doc pin exactly.
- `kubectl -n phoenix get svc`: **`tide-phoenix-svc`**, ClusterIP, ports `4317/TCP,6006/TCP,9090/TCP` — matches the offline render and every doc example verbatim.

### 2. Admin password rotation + System API key mint (headless equivalent of the doc's UI click-path)

`kubectl -n phoenix port-forward svc/tide-phoenix-svc 6006:6006` backgrounded; `curl http://localhost:6006/` → `200`.

**Doc-vs-execution note (not a doc divergence — an execution-technique substitution):**
`docs/INSTALL.md`'s documented flow (port-forward → log in via the UI at
`admin@localhost` → change password at first login → **Settings → API Keys**
→ create a System API key) is the correct guidance for a human operator and
was independently confirmed accurate against Arize's own current
authentication docs (Settings → API Keys is exactly right). This proof has no
browser, so it drove the identical outcome through Phoenix's REST/GraphQL
API — the officially documented non-interactive equivalent:

1. Read `PHOENIX_DEFAULT_ADMIN_INITIAL_PASSWORD` from the chart-created
   `phoenix-secret` Secret → decoded to the literal `admin` (the chart's weak
   fallback default, exactly as the doc warns).
2. `POST /auth/login` with `{"email":"admin@localhost","password":"admin"}` →
   **204 No Content**, `Set-Cookie: phoenix-access-token=<JWT>` (also proves
   the doc's stated login credentials are correct).
3. `POST /graphql` `patchViewer(input: {currentPassword, newPassword})` using
   the access token as `Authorization: Bearer` → succeeded
   (`{"data":{"patchViewer":{"__typename":"UserMutationPayload"}}}`). Verified
   the rotation took effect: a fresh `/auth/login` with the OLD password now
   returns **401**; the NEW (locally-generated random 40-char, never
   committed/logged) password returns **204**.
4. Re-authenticated with the new password, then `POST /v1/system/api_keys`
   `{"data":{"name":"tide-otlp-ingest","description":"TIDE manager/dashboard/reporter OTLP exporter auth (Phase 47-04 live proof)"}}`
   using the fresh access token → **201**, returned a `CreatedApiKey` with
   `id: U3lzdGVtQXBpS2V5OjE=`, `name: tide-otlp-ingest`,
   `created_at: 2026-07-17T13:59:32Z`. The `key` field (105 chars) was written
   directly to a `chmod 600` local temp file, never echoed to any log or
   committed artifact, and used immediately below.

Zero doc divergence found on this step — the documented Settings → API Keys
UI path and the REST equivalent used here mint the identical
admin-scoped-creation, system-owned API key artifact.

### 3. `tide-otlp-headers` Secret created (`--from-literal`, key value never lands in any file or runlog)

```bash
kubectl create secret generic tide-otlp-headers -n tide-system \
    --from-literal=OTEL_EXPORTER_OTLP_HEADERS="Authorization=Bearer ${PHOENIX_API_KEY}"
```

`secret/tide-otlp-headers created`.

### 4. `helm upgrade tide` with the tracing flags — local chart path substitution

```bash
helm upgrade tide ./charts/tide -n tide-system --reuse-values \
  --set otel.exporter.endpoint=tide-phoenix-svc.phoenix.svc.cluster.local:4317 \
  --set otel.exporter.headersSecretRef.name=tide-otlp-headers \
  --set phoenix.baseURL=http://localhost:6006
kubectl -n tide-system rollout status deploy/tide-controller-manager --timeout=3m
kubectl -n tide-system rollout status deploy/tide-dashboard --timeout=3m
```

**Live D-10 both-ways NOTES evidence — the tracing-dark warning is now
ABSENT** (compare against Task 1's captured output, which carried both
warnings):

```
NOTES:
TIDE 1.0.7 installed in tide-system.

Dashboard:  kubectl -n tide-system port-forward svc/tide-dashboard 8080:80
Docs:       https://github.com/jsquirrelz/tide/blob/main/docs/INSTALL.md

WARNING: run telemetry beyond the budget tally is unavailable —
prometheus.enabled is false.
Token spend over time, dispatch counts, and per-level durations will be dark.
Enable: see the "Enable telemetry" step in docs/INSTALL.md.
```

(Prometheus warning still present — expected, untouched by this task. The
tracing warning block is gone.) Both Deployments rolled out successfully.

### 5. No-spend connectivity checks (all before the first real-API dispatch)

**(a) nc probe to Phoenix's OTLP gRPC port:**

```bash
kubectl --context kind-tide-phoenix-proof run otlp-nc -n tide-system --rm -i --restart=Never \
  --image=busybox:1.36 -- nc -z -w5 tide-phoenix-svc.phoenix.svc.cluster.local 4317
```

Exit 0 — `nc -z` succeeded, `pod "otlp-nc" deleted from tide-system namespace`.

**(b) Manager Deployment env shows the headers entry as `secretKeyRef`, zero literal (T-47-01 live check):**

```
$ kubectl get deploy -n tide-system -o yaml | grep -A4 OTEL_EXPORTER_OTLP_HEADERS
          - name: OTEL_EXPORTER_OTLP_HEADERS
            valueFrom:
              secretKeyRef:
                key: OTEL_EXPORTER_OTLP_HEADERS
                name: tide-otlp-headers
...
          - name: OTEL_EXPORTER_OTLP_HEADERS
            valueFrom:
              secretKeyRef:
                key: OTEL_EXPORTER_OTLP_HEADERS
                name: tide-otlp-headers
```

Both the manager AND dashboard Deployments carry the entry, both Secret-sourced, zero literal `value:` field.

**(c) Manager logs — zero OTLP export errors:**

```
$ kubectl logs -n tide-system deploy/tide-controller-manager | grep -ci 'export.*error\|otlp.*error'
0
```

Manager log tail shows all 6 reconcilers restarted cleanly post-rollout, reservation store rederived at `totalReservedCents: 0`.

### Task 2 verification

```
$ kubectl --context kind-tide-phoenix-proof -n phoenix get pods --no-headers | grep -c Running
1
$ kubectl --context kind-tide-phoenix-proof get deploy -n tide-system -o yaml | grep -c "OTEL_EXPORTER_OTLP_HEADERS"
4
```

(4 = 2 env-name occurrences × 2 Deployments, i.e. both manager and dashboard wired.)

**Zero divergences found in docs/INSTALL.md or docs/observability.md this task — no doc edit was needed.** The pipe is proven end-to-end (nc reachability + secretKeyRef shape + zero OTLP errors) without spending a cent.
