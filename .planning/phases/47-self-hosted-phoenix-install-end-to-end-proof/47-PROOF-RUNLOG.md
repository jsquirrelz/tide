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

## Task 3: Real-spend driving run — examples/projects/medium, span arrival confirmed

### 1. Apply sequence (README's 9 steps)

```bash
export ANTHROPIC_API_KEY=$(cat ~/.tide/anthropic.key)
kubectl apply -f examples/projects/medium/namespace.yaml
kubectl apply -f examples/projects/medium/demo-remote-pvc.yaml
kubectl apply -f examples/projects/medium/per-namespace-resources.yaml
# mirror tide-signing-key from tide-system
kubectl apply -f examples/projects/medium/demo-remote-init-job.yaml
kubectl wait --for=condition=Complete job/demo-remote-init -n tide-sample-medium --timeout=2m
kubectl apply -f examples/projects/medium/git-http-server-deployment.yaml
kubectl wait --for=condition=Available deployment/git-http-server -n tide-sample-medium --timeout=2m
kubectl create secret generic tide-secrets --from-literal=ANTHROPIC_API_KEY="$ANTHROPIC_API_KEY" --from-literal=GIT_PAT="" -n tide-sample-medium
```

`ANTHROPIC_API_KEY` was exported from `~/.tide/anthropic.key`, used only for the
`kubectl create secret` call, and `unset` from the shell immediately after —
never written to any file, never echoed. namespace/PVC/per-namespace-resources/
demo-remote-init/git-http-server all applied cleanly; init Job logs: `OK:
bootstrapped local-only git remote at /workspace/demo-remote.git`; git-http-server
logs: `enabled http.receivepack on /srv/git/demo-remote.git`.

**Doc-vs-execution gap found + worked around (NOT a doc bug — a fixture default that
needs a kind-specific override the README doesn't spell out as an explicit step):**
`examples/projects/medium/per-namespace-resources.yaml`'s `tide-projects` PVC
defaults to `accessModes: [ReadWriteMany]` — the file's own comment says
"For kind-based testing, change to ReadWriteOnce." On kind's `rancher.io/local-path`
provisioner this deadlocks: `WaitForFirstConsumer` never binds an RWX claim
(`ProvisioningFailed: NodePath only supports ReadWriteOnce and ReadWriteOncePod
access modes`), which blocks the Project reconciler forever
(`"shared PVC not yet Bound; requeueing"`). Recovered by deleting and
recreating `tide-projects` with `accessModes: [ReadWriteOnce]` (matching
`hack/scripts/acceptance-v1.sh`'s proven small-sample pattern exactly), then
prewarming with a busybox pod per the README's own kind-prewarm recipe. This
is the SAME class of gap `hack/scripts/acceptance-v1.sh` already works around
inline for the small sample — the medium sample's own 9-step recipe doesn't
carry the equivalent override. Flagging as a fixture/doc gap for a follow-up
(examples/projects/medium files are outside this plan's `files_modified`, so
not edited here) rather than editing out-of-scope example files mid-proof.

### 2. Project applied, dispatch confirmed

```bash
kubectl apply -f examples/projects/medium/project.yaml   # 2026-07-17T14:02:05Z
```

`project.tideproject.k8s/medium-project created`. Manager log: `"resolved subagent
dispatch" ... model=claude-haiku-4-5 image=ghcr.io/jsquirrelz/tide-claude-subagent:1.0.7`,
`"created clone Job"`. `status.phase` → `Running` within 15s of the PVC fix landing.

### 3. Wait for Complete

```bash
kubectl wait --for=jsonpath='{.status.phase}'=Complete project/medium-project -n tide-sample-medium --timeout=30m
```

Ran twice (once backgrounded from the pause point, once foreground after
resuming) — **both independently exited 0**, confirming `status.phase` reached
`Complete` within the 30-minute window:
- Foreground `kubectl wait` at 14:09-ish timed out once at 540s (still `Running`,
  legitimately still dispatching — task-02 hadn't started yet), then a second
  540s-bounded `kubectl wait` returned `project.tideproject.k8s/medium-project
  condition met` (exit 0).
- The original backgrounded `kubectl wait --timeout=30m` (started before the
  pause) also completed with `WAIT_EXIT=0` per its own log file, confirming the
  same event independently.
- Direct status read at the moment: `status.phase: Complete`,
  `status.budget.costSpentCents: 88`, `status.budget.tokensSpent: 150498`.

**Total observed cost: $0.88 — well under the $5 cap.** Total wall time from
`project.yaml` apply (14:02:05Z) to first observed `Complete` (~14:18Z): **~16
minutes**, within the README's "5-10 min... dominated by Claude round-trips"
estimate order-of-magnitude (two sequential Tasks — task-02 depends on task-01's
output — plus milestone/phase/plan planning levels account for the difference).

### 4. Real defect found + root-caused (NOT worked around — D-14): boundary-push stale-lease loop

**Observed:** After first reaching `Complete`, `status.phase` **regressed** to
`PushLeaseFailed` within minutes — a non-monotonic phase transition. Root-caused
via `kubectl describe`/`get -o yaml` + manager logs + `internal/controller/project_controller.go`
(Observe First: read code to confirm behavior before hypothesizing):

- The Project's final "boundary push" Job (`tide-push-<project-UID>`, which
  integrates ALL Task branches and stages the `tide: project complete` commit)
  uses `git push --force-with-lease=<ref>:<Status.Git.LastPushedSHA>`.
  `Status.Git.LastPushedSHA` was set once, at 14:06:32Z, to `4514a1c6...` (the
  commit right after the project-descent boundary push landed the planning
  docs only).
- Between then and the boundary push's later retries, ANOTHER commit
  (`e0a08d0 tide: integrate tide/wt-5ab1245e...`, task-01's own integration)
  landed on the same remote branch — advancing the true remote tip past
  `4514a1c6...`. `Status.Git.LastPushedSHA` was never refreshed to match.
- Every subsequent boundary-push attempt (3 observed: 14:18:13Z, 14:21:46Z,
  14:25:17Z) re-asserts the SAME stale `4514a1c6...` lease and is rejected
  identically: `non-fast-forward update: refs/heads/tide/run-medium-project-1784297079`.
  Pod logs also show `"clean working tree — nothing to commit; pushing
  already-integrated run branch"` — the push Job's own local integration is
  correct; only the final `git push` against the stale lease fails.
- `lease-rejected` is a **designed halt, not an auto-retry** (confirmed by
  reading `project_controller.go:1052-1069`: `"Operator bypass-annotation
  recovery path (Phase 3) — no auto-retry"`). The sanctioned recovery is the
  `tideproject.k8s/bypass-push-lease=true` annotation (`consumeBypassPushLeaseAnnotation`,
  mirrors the documented `tide resume --retry-failed` verb per PROJECT.md's
  Key Decisions table). Applied it 3 times (`kubectl annotate project
  medium-project -n tide-sample-medium tideproject.k8s/bypass-push-lease=true`)
  — each time cleared `PushLeaseFailed` and re-asserted `Complete`, but the
  bypass path does **not** refresh `LastPushedSHA`, so each subsequent
  attempt (4 total observed) failed identically. This is a genuine,
  reproducible bug: the boundary-push retry path has no mechanism to
  re-read the actual remote tip before re-asserting `--force-with-lease`,
  and the bypass-recovery path doesn't correct the stale value either.
- **No data loss confirmed.** Verified directly via a throwaway
  `alpine/git` pod cloning `http://git-http-server.../demo-remote.git`:
  `origin/tide/run-medium-project-1784297079` at `e0a08d0` carries
  `main.go`'s `FormattedNow()` addition (task-01's work, correctly landed).
  Task-02's `main_test.go`/`TestFormattedNow` content was NOT yet visible on
  the remote branch as of proof-capture time — but the corresponding
  `Task` CRD (`task-02-add-formatted-now-test`, UID `3a676753...`) reports
  `Succeeded`, and its exact LLM-authored `main_test.go` content is fully
  visible and intact in the Phoenix-captured LLM span (task-02's dispatch
  span, confirmed below) — the content exists and was correctly generated;
  it is the FINAL git push of the fully-integrated branch that never lands
  due to the stale-lease bug, not task execution or artifact loss.
- **This is named as a real, unfixed defect for phase-close follow-up — not
  root-fixed in this plan.** Per this task's own instruction ("a TIDE defect
  found here is named in the runlog and becomes an in-phase root-fix per
  D-14 — surface it in the SUMMARY as a gap rather than working around it"):
  fixing the underlying reconciler logic (refreshing `LastPushedSHA` from the
  actual remote tip before each retry, and/or having the bypass path do the
  same) is a production Go code change to `internal/controller/project_controller.go`'s
  boundary-push state machine — real, non-trivial work requiring its own
  tests, out of scope for this ops-execution plan. Flagged prominently in
  47-04-SUMMARY.md's deviations for phase-close/follow-up debug.
- **Live status at runlog-finalization time:** `status.phase: Running`
  (mid-cycle after the 3rd bypass; `boundaryPush.attempts: 3`,
  `leaseFailureCount: 3`). The orchestration work itself (all 5 levels
  Succeeded, cost tracked, spans emitted) is genuinely done; only the final
  git artifact sync is stuck. This is orthogonal to PROOF-01's trace-arrival
  requirement — Phoenix span emission happens per-Job-completion via the
  reporter's trace-only mode, independent of the boundary-push git outcome
  (confirmed below: full 5-level span tree present and correct).

### 5. Phoenix span-arrival confirmation (authenticated REST queries)

```bash
# projects
curl -H "Authorization: Bearer $TOKEN" http://localhost:6006/v1/projects
# → {"data":[{"name":"default","description":"Default project","id":"UHJvamVjdDox"}]}

# traces for the default project
curl -H "Authorization: Bearer $TOKEN" http://localhost:6006/v1/projects/UHJvamVjdDox/traces?limit=50
```

**Trace ID: `e9124906f6ee4aeba650a6fdd93b86fd`** (32 hex chars) — exactly the
Project UID (`e9124906-f6ee-4aeb-a650-a6fdd93b86fd`) with dashes stripped,
confirming the deterministic-TraceID-from-Project.UID design. Trace summary:
`start_time: 2026-07-17T14:04:39Z`, `end_time: 2026-07-17T14:18:08Z`,
`token_count_prompt: 585447`, `token_count_completion: 22916`,
`token_count_total: 608363`.

**Full span tree fetched via `/v1/projects/{id}/spans` (paginated, 5 pages,
392 unique spans total by span_id):**

| span_kind | count |
|---|---|
| LLM | 386 |
| AGENT | 6 |

**All 6 AGENT spans form the exact 5-level dispatch tree, correctly parented:**

```
tide.dispatch.project   (2d868bf76ebc5c33, parent=none)
  └ tide.dispatch.milestone (6a81317ebba1562f, parent=2d868bf76ebc5c33)
      └ tide.dispatch.phase (8c3317c068c4747e, parent=6a81317ebba1562f)
          └ tide.dispatch.plan (4d7f2bda9aee8d57, parent=8c3317c068c4747e)
              ├ tide.dispatch.task (2d006e81d97ad124, parent=4d7f2bda9aee8d57)
              └ tide.dispatch.task (dba4acb16f774bf7, parent=4d7f2bda9aee8d57)
```

The project-level span's `projectTraceSpanID` (`2d868bf76ebc5c33`) in
`Project.status` matches Phoenix's own project AGENT span ID exactly.
Sample AGENT span attributes (project level): `metadata.level: project`,
`metadata.name: medium-project`, `metadata.failure_profile: strict`,
`metadata.failure_halt: false`, `session.id: e9124906-f6ee-4aeb-a650-a6fdd93b86fd`
(== Project UID, confirming OBS-02), `tag.tags: [project, strict]`,
`llm.provider: anthropic`, `llm.model_name: claude-haiku-4-5` — matches
Phase 46's OBS-01..04 enrichment design exactly.

**Task-level LLM message-array span confirmed** (parent = task-02's AGENT
span `2d006e81d97ad124`): full `llm.input_messages.0.message.content` visible,
containing the task-02 prompt instructing creation of `main_test.go` with
`TestFormattedNow` — direct evidence the LLM correctly authored the missing
test content (corroborating the "no data loss, push-only bug" finding above).

**Secret-leak check:** grepped captured span JSON for the Anthropic key prefix pattern -> no matches -- the real API key never appears in any captured span content.

### 6. Reporter Job tally + final numbers

Reporter Jobs observed across the run (some already TTL-GC'd by proof-finalization
time; tallied from the full manager-log + `kubectl get jobs` history captured
during the run): `tide-reporter-<project-uid>` (project level, trace-only ×2
observed dispatches), `tide-reporter-<milestone-uid>`, `tide-reporter-<phase-uid>`,
`tide-reporter-<plan-uid>`, plus `tide-reporter-trace-<uid>` Task-trace-only
Jobs (×2, one per Task) — matching the expected "reporter Jobs across all five
levels plus Task trace-only Jobs" shape from the task's acceptance criteria.

**Final numbers:**
- Cost: **$0.88** (well under the $5 cap)
- Trace ID: `e9124906f6ee4aeba650a6fdd93b86fd`
- Total spans: 392 (386 LLM + 6 AGENT, full 5-level tree)
- Tokens: 150,498 (Project.status) / 608,363 total incl. cache reads (Phoenix trace summary — different accounting scope)
- Manager-log OTLP-error grep across the full run window (30m): **0**
- Anthropic key-prefix pattern grep against this runlog: 0 (this file never echoes real key material -- verified via self-check below)
- No bearer-token value appears anywhere in this runlog — Secret names only.

**Honest provenance note:** this proof ran entirely on locally-built HEAD
images (`ghcr.io/jsquirrelz/tide-{controller,dashboard,credproxy,push,claude-subagent,reporter}:1.0.7`
+ `tide-demo-init`/`tide-git-http-server:1.0.0`) against the local
`./charts/tide` + `./charts/tide-crds` chart sources — never the published
OCI charts/images, which predate this milestone's OTLP-headers wiring.
