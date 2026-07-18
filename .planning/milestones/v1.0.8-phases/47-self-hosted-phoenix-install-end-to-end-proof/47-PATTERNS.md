# Phase 47: Self-Hosted Phoenix Install + End-to-End Proof - Pattern Map

**Mapped:** 2026-07-17
**Files analyzed:** 13 (2 docs, 2 chart-template sites, 1 values.yaml, 2 Deployment templates, 1 main.go, 5 reconciler call sites treated as one repeated pattern, 1 ReporterOptions/BuildReporterJob, 2-3 test/assert files)
**Analogs found:** 13 / 13 — every file this phase touches has a direct, load-bearing analog already in the codebase. This phase is almost entirely "repeat an existing wiring pattern one more time," not "invent a new pattern."

## File Classification

| New/Modified File | Role | Data Flow | Closest Analog | Match Quality |
|---|---|---|---|---|
| `docs/INSTALL.md` (new "Enable tracing (Phoenix)" section, ~line 175) | doc | request-response (operator runbook) | `docs/INSTALL.md` §"Enable telemetry (Prometheus)" (lines 175-215, same file) | exact |
| `docs/observability.md` (new "Self-hosted Phoenix" subsection under `## Tracing`; edit deep-link placeholder at line ~239) | doc | request-response (operator reference) | `docs/observability.md` §"Prometheus ServiceMonitor"/"Retention sizing" (lines 79-127, same file) | exact |
| `charts/tide/templates/NOTES.txt` (new tracing-dark nudge block) | config (Helm template, rendered post-install) | event-driven (renders once, on install/upgrade) | `charts/tide/templates/NOTES.txt` existing `prometheus.enabled` warning block (lines 9-15, same file) | exact |
| `hack/helm/augment-tide-chart.sh` step 4b heredoc (same nudge, mirrored) | config (codegen source-of-truth for NOTES.txt) | event-driven | `hack/helm/augment-tide-chart.sh` lines 108-124 (same heredoc, existing prometheus block) | exact |
| `charts/tide/values.yaml` (new `otel.exporter.headersSecretRef`-shaped key) | config | CRUD (declarative values → template env) | `charts/tide/values.yaml` `otel:` block (lines 401-421, same file) + `prometheus:` comment-block style (lines 344-399) | exact |
| `charts/tide/templates/deployment.yaml` (new `OTEL_EXPORTER_OTLP_HEADERS` env, `valueFrom: secretKeyRef`, guarded) | config (Helm template) | request-response (env injected at Pod start) | `charts/tide/templates/deployment.yaml` lines 89-96 (`OTEL_EXPORTER_OTLP_ENDPOINT` + sibling OTEL env block, same file) | exact |
| `charts/tide/templates/dashboard-deployment.yaml` (same headers env) | config (Helm template) | request-response | `charts/tide/templates/dashboard-deployment.yaml` lines 47-56 (same OTEL env block, same file) | exact |
| `cmd/manager/main.go` (read `OTEL_EXPORTER_OTLP_HEADERS`, thread into `PlannerReconcilerDeps`/`TaskReconcilerDeps`) | controller (wiring/bootstrap) | request-response | `cmd/manager/main.go` lines 285, 438, 549 (`otlpEndpoint := os.Getenv(...)` → `OTLPEndpoint:` in both Deps structs, same file) | exact |
| `internal/controller/{milestone,phase,plan,project,task}_controller.go` (5 call sites, add `OTLPHeaders:` field to each `ReporterOptions{}` literal) | controller (reconciler) | event-driven (fires on planner-Job completion) | Same 5 files' existing `OTLPEndpoint: r.Deps.OTLPEndpoint,` lines (`milestone_controller.go:656`, `phase_controller.go:609`, `plan_controller.go:664`, `project_controller.go:1924`, `task_controller.go:1106`) | exact |
| `internal/controller/reporter_jobspec.go` (new `OTLPHeaders` field on `ReporterOptions`, env construction) | service (Job-spec builder) | transform (opts struct → K8s Job spec) | `internal/controller/reporter_jobspec.go` lines 89-97 (`OTLPEndpoint` field doc comment) + lines 282-288 (env construction) | exact |
| `internal/controller/reporter_jobspec_test.go` (new `TestBuildReporterJob_OTLPHeadersEnv` / `_NoOTLPHeadersNoEnv`) | test (unit) | transform | `internal/controller/reporter_jobspec_test.go` lines 789-861 (`TestBuildReporterJob_OTLPEndpointEnv` / `TestBuildReporterJob_NoOTLPEndpointNoEnv`, same file) | exact |
| `hack/helm/assert-telemetry-render.sh` (new Permutation, mirroring G but keyed on `otel.exporter.endpoint`) | test (chart render-gate, offline) | request-response | `hack/helm/assert-telemetry-render.sh` Permutation G (lines 214-271, same file — the `tpl`+ConfigMap NOTES.txt probe trick) | exact — and corrects RESEARCH.md's Pitfall A (see Shared Patterns note below) |
| `hack/helm/assert-otlp-headers-env.py` (new, if headers wiring is chosen) | test (chart render-gate, offline) | request-response | `hack/helm/assert-phoenix-env.py` (full file, 127 lines — sibling of `assert-prometheus-env.py`) | exact |

## Pattern Assignments

### `docs/INSTALL.md` — "Enable tracing (Phoenix)" section (doc, request-response)

**Analog:** `docs/INSTALL.md` §"Enable telemetry (Prometheus)", lines 175-215 (same file, insert the new section as a sibling immediately after it or before it — CONTEXT.md D-01 says "beside").

**Three-beat structure to copy exactly** (lines 175-213):
```markdown
### Enable telemetry (Prometheus)

By default TIDE's run telemetry beyond the budget tally is **dark** — `prometheus.enabled` is `false`, ...

**1. Install kube-prometheus-stack** (skip if your cluster already runs a prometheus-operator ...):

```bash
helm repo add prometheus-community https://prometheus-community.github.io/helm-charts
helm repo update
helm install kps prometheus-community/kube-prometheus-stack \
    -n monitoring --create-namespace
```

**2. Enable TIDE's telemetry surfaces.** Three values, three roles: ...

```bash
helm upgrade tide oci://ghcr.io/jsquirrelz/tide-charts/tide \
    -n tide-system --reuse-values \
    --set prometheus.enabled=true \
    ...
```

**3. Label the ServiceMonitor.** ...

**4. Verify at the Targets page** — this is the done signal:

```bash
kubectl -n monitoring port-forward svc/prometheus-operated 9090:9090
open http://localhost:9090
```

Navigate to **Status → Targets** and confirm the `tide-...-metrics` endpoint shows **UP**. ...
```

**What to change for Phoenix:** same "1. Install the external chart → 2. wire TIDE's chart values at it → 3./4. verify via a concrete, copy-pasteable check with an explicit done-signal" shape. Step 1 becomes `helm install tide-phoenix oci://registry-1.docker.io/arizephoenix/phoenix-helm --version 10.0.0 ...` (SQLite-on-PVC quickstart values, per D-05/D-07). Step 2 becomes `helm upgrade tide ... --set otel.exporter.endpoint=tide-phoenix.<ns>.svc.cluster.local:4317` (bare `host:port`, no scheme — D-09's explicit warning callout belongs right here, styled like the existing "Existing Prometheus" callout at line 215). Step 3/4's done-signal is a Phoenix UI trace-tree view via `kubectl port-forward`, mirroring the Prometheus Targets-page done-signal exactly.

**Secret-sourced credential pattern to reuse for D-08's auth-on framing** (lines 217-235, Provider Secret section):
```markdown
## Provider Secret (`ANTHROPIC_API_KEY`)
...
Create the Secret in the **Project's namespace** (not `tide-system`). ...
kubectl create secret generic tide-anthropic-creds \
    --namespace tide-sample-medium \
    --from-literal=ANTHROPIC_API_KEY="$ANTHROPIC_API_KEY"
...
**Never paste the raw key into a YAML manifest** — using `--from-literal` keeps the key out of source control and out of `helm install --dry-run` output.
```
Use this exact "export env var in shell → `kubectl create secret generic ... --from-literal=` → never paste into YAML" voice for the Phoenix `PHOENIX_SECRET`/admin-password Secret and (if Pitfall B's wiring is added) the `OTEL_EXPORTER_OTLP_HEADERS` bearer-token Secret.

---

### `docs/observability.md` — "Self-hosted Phoenix" subsection (doc, request-response)

**Analog:** same file, `### Prometheus ServiceMonitor` → `#### Retention sizing` (lines 79-127) for the "both storage paths + sizing" reference-depth shape, and the existing `### Opting down from 100% sampling` (lines 180-219) for the "read this before you change the default — here's the non-obvious caveat" honesty-callout voice.

**Existing env-var table pattern to extend** (lines 162-167):
```markdown
| Env var                          | Helm value                       | Default                          |
| -------------------------------- | -------------------------------- | -------------------------------- |
| `OTEL_EXPORTER_OTLP_ENDPOINT`    | `otel.exporter.endpoint`         | `""` (→ no-op)                   |
| `OTEL_TRACES_SAMPLER`            | `otel.tracesSampler`             | `parentbased_traceidratio`       |
| `OTEL_TRACES_SAMPLER_ARG`        | `otel.tracesSamplerArg`          | `1.0`                            |
| `OTEL_SERVICE_NAME`              | `otel.serviceName`               | `tide-controller-manager`        |
```
Add a row for `OTEL_EXPORTER_OTLP_HEADERS` / `otel.exporter.headersSecretRef` (if Pitfall B's wiring is added this phase) with default `""` (→ no headers sent), matching the table's exact column shape.

**Bare-`host:port` precedent already shipped, reuse verbatim** (lines 169-174):
```bash
helm upgrade tide ./charts/tide -n tide-system \
  --set otel.exporter.endpoint=otel-collector.observability.svc:4317
```
The Phoenix example is "the concrete instance of this same pattern, not a new one" (RESEARCH.md Pattern 2) — present it as `--set otel.exporter.endpoint=tide-phoenix.<namespace>.svc.cluster.local:4317`, explicitly noting 4317 (gRPC) vs 6006 (HTTP/UI, never the target) right beside it.

**Placeholder to replace** (lines 236-260, `### Dashboard deep link to Phoenix`): the line `install instructions land in Phase 47; this section covers only the config value and what it wires up` becomes a cross-reference to the new Self-hosted Phoenix subsection above and to `docs/INSTALL.md`'s quickstart step — same file, same section, D-01's letter.

---

### `charts/tide/templates/NOTES.txt` + `hack/helm/augment-tide-chart.sh` step 4b (config, event-driven) — BOTH SITES

**Analog:** the existing `prometheus.enabled` warning block, present byte-identically in both files (NOTES.txt lines 9-15; augment script lines 117-123 inside the heredoc at 108-124):
```
{{- if not .Values.prometheus.enabled }}

WARNING: run telemetry beyond the budget tally is unavailable —
prometheus.enabled is false.
Token spend over time, dispatch counts, and per-level durations will be dark.
Enable: see the "Enable telemetry" step in docs/INSTALL.md.
{{- end }}
```

**Pattern to replicate (D-10):** same `{{- if not .Values.X }}` / blank-line / `WARNING:` / explanation / `Enable: see the "..." step in docs/INSTALL.md.` / `{{- end }}` shape, condition swapped to `not .Values.otel.exporter.endpoint` (empty-string is falsy in Go templates, matching how `prometheus.enabled: false` is falsy). CONTEXT.md D-10 leaves wording/whether to also mention `phoenix.baseURL` to discretion — the Prometheus block's voice (name the umbrella key, name the consequence, point to the doc step) is the house style to match.

**Both edit sites are mandatory** — the NOTES.txt file's own header comment (lines 1-3) says so, and RESEARCH.md's Pitfall 3/"NOTES.txt is generated" independently confirms it: `hack/helm/augment-tide-chart.sh` step 4b (lines 108-124) is the source of truth; a future `make` run that re-invokes the augment script reverts a template-only edit.

---

### `charts/tide/values.yaml` — `otel.exporter.headersSecretRef` (config, CRUD)

**Analog:** the `otel:` block itself (lines 401-421) for the comment-block voice and key placement, plus the `prometheus:` block's "three values, three roles" comment style (lines 344-399) for documenting a new key's exact effect:
```yaml
# OpenTelemetry env-driven config (Phase 4 D-O3 / plan 04-03).
#
# Read by internal/otelinit at controller boot:
#   - exporter.endpoint = ""  → no-op TracerProvider (zero overhead; default)
#   - exporter.endpoint = "otel-collector:4317" → OTLP gRPC exporter
#
# Sampler is env-driven (NOT WithSampler in code, per Pitfall 24 mitigation): ...
otel:
  exporter:
    endpoint: ""
  tracesSampler: "parentbased_traceidratio"
  tracesSamplerArg: "1.0"
  serviceName: "tide-controller-manager"
```
No existing chart values key does a Secret-ref-by-name-and-key (the closest whole-Secret analog is `signing-secret.yaml`'s auto-generated `TIDE_SIGNING_KEY`, consumed via `envFrom: secretRef:` — see `hack/helm/augment-tide-chart.sh` lines 126-130). Model the new key on the **shape**, not the mechanism: `otel.exporter.headersSecretRef: {name: "", key: ""}` (never a literal `value:` — ASVS V6 in RESEARCH.md's Security Domain table), consumed via `valueFrom: secretKeyRef:` (per-key, not whole-Secret `envFrom`) since the header value is one string among potentially-other Secret keys, matching the `providerSecretRef`/`credsSecretRef` naming convention already used at the Project-CRD level (`docs/INSTALL.md` lines 263, 276).

---

### `charts/tide/templates/deployment.yaml` + `dashboard-deployment.yaml` — `OTEL_EXPORTER_OTLP_HEADERS` env (config, request-response)

**Analog:** the sibling OTEL env block, present near-identically in both files.

`deployment.yaml` lines 85-96:
```yaml
# Phase 4 plan 04-14 (D-O3): OTel env vars read by internal/otelinit.
# Empty OTEL_EXPORTER_OTLP_ENDPOINT → no-op TracerProvider (zero
# overhead, default posture for plain clusters). Sampler is env-driven
# to honor Pitfall 24 mitigation (no WithSampler in code).
- name: OTEL_EXPORTER_OTLP_ENDPOINT
  value: {{ quote .Values.otel.exporter.endpoint }}
- name: OTEL_TRACES_SAMPLER
  value: {{ quote .Values.otel.tracesSampler }}
- name: OTEL_TRACES_SAMPLER_ARG
  value: {{ quote .Values.otel.tracesSamplerArg }}
- name: OTEL_SERVICE_NAME
  value: {{ quote .Values.otel.serviceName }}
# phase4-env-injected
```
`dashboard-deployment.yaml` lines 46-56 is the same four-entry block (`OTEL_SERVICE_NAME` hardcoded to `"tide-dashboard"` there instead of templated).

**New entry to add, guarded (never render an env with an empty secretKeyRef name):**
```yaml
{{- if .Values.otel.exporter.headersSecretRef.name }}
- name: OTEL_EXPORTER_OTLP_HEADERS
  valueFrom:
    secretKeyRef:
      name: {{ .Values.otel.exporter.headersSecretRef.name }}
      key: {{ .Values.otel.exporter.headersSecretRef.key | default "OTEL_EXPORTER_OTLP_HEADERS" }}
{{- end }}
```
This mirrors the PromQL-proxy's conditional-env pattern already in `dashboard-deployment.yaml` (the `{{- /* PromQL proxy target ... Emitted ONLY when prometheus.endpoint is non-empty */ }}` comment at line 57) — "emit the env entry only when the values key that drives it is set" is an established convention in this exact file, not a new idiom.

---

### `cmd/manager/main.go` — read + thread `OTLPHeaders` (controller wiring, request-response)

**Analog:** the existing `OTLPEndpoint` thread, three exact points in the same file:
```go
// Source: cmd/manager/main.go:281-285
// Phase 44 TRACE-03/D-06: capture the SAME env var otelinit just read
// above so the reporter Job's own TracerProvider resolves the identical
// collector (forwarded via PlannerReconcilerDeps.OTLPEndpoint below).
// Empty = tracing dark; the reporter env block is omitted entirely.
otlpEndpoint := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")
```
```go
// Source: cmd/manager/main.go:429-439 (plannerDeps, shared by Milestone/Phase/Plan/Project)
plannerDeps := controller.PlannerReconcilerDeps{
    Dispatcher:           dispatcher,
    EnvReader:            envReader,
    SigningKey:           signingKey,
    CredproxyImage:       credproxyImage,
    TidePushImage:        tidePushImage,
    ReporterImage:        reporterImage,
    HelmProviderDefaults: helmProviderDefaults,
    PricingOverridesJSON: pricingOverridesJSON,
    OTLPEndpoint:         otlpEndpoint,
}
```
```go
// Source: cmd/manager/main.go:546-549 (TaskReconcilerDeps)
// Phase 44 MSG-01/TRACE-03/D-06: trace-only reporter spawn deps —
// mirrors plannerDeps' ReporterImage/OTLPEndpoint above.
ReporterImage: reporterImage,
OTLPEndpoint:  otlpEndpoint,
```
Add `otlpHeaders := os.Getenv("OTEL_EXPORTER_OTLP_HEADERS")` beside line 285, and `OTLPHeaders: otlpHeaders,` beside every `OTLPEndpoint:` assignment above — same three call sites, same order.

---

### `internal/controller/{milestone,phase,plan,project,task}_controller.go` — 5 call sites (reconciler, event-driven)

**Analog:** every one of the 5 files already threads `r.Deps.OTLPEndpoint` into a `ReporterOptions{}` literal at the exact point where `spawnReporterIfNeeded`/`BuildReporterJob` is called. Representative excerpt:
```go
// Source: internal/controller/milestone_controller.go:653-661
isFirstCompletion, spawnErr := spawnReporterIfNeeded(ctx, r.Client, r.Scheme, ms, project, "Milestone", r.sharedPVCName(), ReporterOptions{
    ReporterImage:    r.Deps.ReporterImage,
    TraceParent:      traceparentForLevel(project, ms.Status.MilestoneTraceSpanID, sampled),
    OTLPEndpoint:     r.Deps.OTLPEndpoint,
    SkipMessageSpans: skipMessageSpans,
    SessionID:        projectUID,
    MetadataJSON:     enrichmentMD,
    Tags:             enrichmentTags,
})
```
```go
// Source: internal/controller/task_controller.go:1103-1114 (the Task-level TraceOnly call site — shape is identical modulo TraceOnly/TraceOnlyJobKey)
traceOnlyJob := BuildReporterJob(task, project, r.sharedPVCName(), string(task.UID), "Task",
    ReporterOptions{
        ReporterImage:    r.Deps.ReporterImage,
        OTLPEndpoint:     r.Deps.OTLPEndpoint,
        TraceOnly:        true,
        TraceOnlyJobKey:  string(completedJob.UID),
        TraceParent:      traceparentForLevel(project, task.Status.TaskTraceSpanID, sampled),
        SkipMessageSpans: skipMessageSpans,
        SessionID:        string(project.UID),
        MetadataJSON:     enrichmentMD,
        Tags:             enrichmentTags,
    }, r.Scheme)
```
The other 3 call sites (`phase_controller.go:609`, `plan_controller.go:664`, `project_controller.go:1924`) are byte-identical in shape to the milestone one. Add `OTLPHeaders: r.Deps.OTLPHeaders,` immediately after each `OTLPEndpoint:` line, all 5 sites, same relative position.

---

### `internal/controller/reporter_jobspec.go` — `ReporterOptions.OTLPHeaders` + env construction (service, transform)

**Analog:** the existing `OTLPEndpoint` field doc comment (lines 89-97) and its env-construction block (lines 272-288):
```go
// Source: internal/controller/reporter_jobspec.go:89-97
// OTLPEndpoint is the manager's own OTEL_EXPORTER_OTLP_ENDPOINT value,
// forwarded so the reporter's otelinit.NewTracerProvider resolves the
// SAME collector the manager uses (Phase 44 TRACE-03/D-06). Unlike
// TraceParent this IS carried as Env (not Args) — it targets the
// reporter's own TracerProvider bootstrap, mirroring how the manager
// itself reads it via os.Getenv, not a CLI flag. When empty, no Env is
// set at all and the reporter's otelinit falls back to its no-op
// provider (materialization-mode posture, D-06).
OTLPEndpoint string
```
```go
// Source: internal/controller/reporter_jobspec.go:272-288
var env []corev1.EnvVar
if opts.OTLPEndpoint != "" {
    env = []corev1.EnvVar{
        {Name: "OTEL_EXPORTER_OTLP_ENDPOINT", Value: opts.OTLPEndpoint},
        {Name: "OTEL_BSP_MAX_EXPORT_BATCH_SIZE", Value: "6"},
    }
}
```
Add `OTLPHeaders string` as a sibling field (same doc-comment voice — "forwarded so the reporter's ... resolves the SAME ... the manager uses"), and extend the `if opts.OTLPEndpoint != ""` block to also append an `OTEL_EXPORTER_OTLP_HEADERS` entry when `opts.OTLPHeaders != ""` — the reporter's `otlptracegrpc` SDK already honors this env var automatically per RESEARCH.md's verified `otlptracegrpc@v1.43.0/doc.go` citation, so no reporter-binary code change is needed, only the Job-spec plumbing.

**Error handling / empty-value pattern:** this file has no try/catch-style error handling on this path — the pattern is "empty string in ⇒ zero Env entries out" (byte-identical-to-today posture), enforced by the two paired tests below, not a runtime error path.

---

### `internal/controller/reporter_jobspec_test.go` — new OTLPHeaders test pair (test, unit)

**Analog:** `TestBuildReporterJob_OTLPEndpointEnv` / `TestBuildReporterJob_NoOTLPEndpointNoEnv`, lines 789-861:
```go
// Source: internal/controller/reporter_jobspec_test.go:793-833
func TestBuildReporterJob_OTLPEndpointEnv(t *testing.T) {
    project := &tideprojectv1alpha3.Project{ObjectMeta: metav1.ObjectMeta{Name: "proj", Namespace: "ns-l", UID: "project-uid-14"}}
    parent := &tideprojectv1alpha3.Milestone{ObjectMeta: metav1.ObjectMeta{Name: "ms-12", Namespace: "ns-l", UID: "parent-uid-14"}}
    scheme := newTestScheme()
    opts := controller.ReporterOptions{
        ReporterImage: "ghcr.io/jsquirrelz/tide-reporter:v0.1.0-dev",
        OTLPEndpoint:  "collector:4317",
    }
    job := controller.BuildReporterJob(parent, project, "tide-projects", "task-uid-14", "Milestone", opts, scheme)

    env := job.Spec.Template.Spec.Containers[0].Env
    want := map[string]string{
        "OTEL_EXPORTER_OTLP_ENDPOINT":    "collector:4317",
        "OTEL_BSP_MAX_EXPORT_BATCH_SIZE": "6",
    }
    // ... length + per-entry value assertions
}
```
Write `TestBuildReporterJob_OTLPHeadersEnv` (asserts `OTEL_EXPORTER_OTLP_HEADERS` appears with the expected value alongside the existing two entries when both `OTLPEndpoint` and `OTLPHeaders` are set) and `TestBuildReporterJob_OTLPHeadersNoEndpointNoEnv` (headers alone, with no endpoint, should probably also render nothing — pin the exact zero/nonzero decision at plan time) as new functions in the same file, same table-driven map-comparison idiom.

---

### `hack/helm/assert-telemetry-render.sh` — new NOTES.txt Permutation (test, offline render-gate)

**Analog:** Permutation G, lines 214-271 of the same file — **this is the load-bearing correction to RESEARCH.md's Pitfall A.** RESEARCH.md's live empirical check (`helm template` never renders NOTES.txt; `--dry-run` needs a reachable cluster) is accurate as a general Helm 3.16.3 fact, but the codebase **already solved this exact problem** for the exact same file (NOTES.txt) with the exact same conditional shape (`prometheus.enabled` → warning block) back in Phase 38 — a `tpl`+throwaway-ConfigMap trick that renders `NOTES.txt` through Helm's real template engine via `helm template --show-only`, with zero cluster dependency:
```bash
# Source: hack/helm/assert-telemetry-render.sh:226-271 (Permutation G, existing, unmodified)
WARNING_TEXT="run telemetry beyond the budget tally is unavailable"

NOTES_PROBE_DIR="$(mktemp -d)"
trap 'rm -rf "${NOTES_PROBE_DIR}"' EXIT
cp -R "${CHART_DIR}" "${NOTES_PROBE_DIR}/chart"
mkdir -p "${NOTES_PROBE_DIR}/chart/files"
cp "${NOTES_PROBE_DIR}/chart/templates/NOTES.txt" "${NOTES_PROBE_DIR}/chart/files/NOTES.tpl"
cat > "${NOTES_PROBE_DIR}/chart/templates/notes-probe.yaml" <<'PROBE'
apiVersion: v1
kind: ConfigMap
metadata:
  name: notes-probe
data:
  notes: |
{{ tpl (.Files.Get "files/NOTES.tpl") . | indent 4 }}
PROBE

if ! NOTES_DEFAULT="$(helm template tide-notes "${NOTES_PROBE_DIR}/chart" --show-only templates/notes-probe.yaml 2>&1)"; then
  die "[G] helm template notes-probe (default values) exited non-zero: ${NOTES_DEFAULT}"
fi
if ! echo "${NOTES_DEFAULT}" | grep -qF "${WARNING_TEXT}"; then
  die "[G] Telemetry warning missing from NOTES with default values. ..."
fi
if ! NOTES_ENABLED="$(helm template tide-notes "${NOTES_PROBE_DIR}/chart" --show-only templates/notes-probe.yaml --set prometheus.enabled=true 2>&1)"; then
  die "[G] helm template notes-probe --set prometheus.enabled=true exited non-zero: ${NOTES_ENABLED}"
fi
if echo "${NOTES_ENABLED}" | grep -qF "${WARNING_TEXT}"; then
  die "[G] Telemetry warning leaked into NOTES with prometheus.enabled=true. ..."
fi
```
**Recommendation for the planner:** add "Permutation I" immediately after G, byte-for-byte the same structure, `WARNING_TEXT` swapped to the Phoenix nudge string and the two `helm template` invocations swapped to default (empty `otel.exporter.endpoint`, nudge present) vs `--set otel.exporter.endpoint=tide-phoenix.observability.svc:4317` (nudge absent). This directly satisfies CONTEXT.md D-10's "helm-template contract test [that] gates the rendered output both ways" — the "Phase 38 TELEM-02 / Phase 46-02 render-gate precedent" CONTEXT.md cites already contains the exact mechanism needed; RESEARCH.md's "needs a live cluster" framing was based on plain `helm template` alone and missed this existing `tpl`-probe workaround in the same file. Reuse it — don't build the raw-byte or live-kind-cluster alternatives RESEARCH.md proposed as fallbacks.

**Wire into `make helm-assert`** the same way Phase 46 did (Makefile lines 710-718): no new Makefile target needed, `helm-telemetry-assert` already calls `bash hack/helm/assert-telemetry-render.sh` unconditionally — a new Permutation I inside that script is picked up automatically.

---

### `hack/helm/assert-otlp-headers-env.py` (new, only if Pitfall B option 1 / headers wiring is chosen) — test, offline render-gate

**Analog:** `hack/helm/assert-phoenix-env.py`, full file (127 lines) — itself an explicit sibling of `assert-prometheus-env.py`, created in Phase 46-02 for exactly this kind of "assert a guarded env var is present-with-value or entirely-absent on a named container" check:
```python
# Source: hack/helm/assert-phoenix-env.py:1-20 (module docstring + intent, existing)
"""Assert PHOENIX_BASE_URL env var presence/absence on the dashboard container.

Phase 46 OBS-01/OBS-04 render gate. Sibling of assert-prometheus-env.py with
PROM_ENDPOINT swapped for PHOENIX_BASE_URL throughout. Walks a `helm template`
stdout YAML dump for the Deployment that contains a container named
`dashboard` and asserts whether the `PHOENIX_BASE_URL` env entry is present
with a specific value (--expect-value) or entirely absent (--expect-absent).
"""
```
```python
# Source: hack/helm/assert-phoenix-env.py:55-90 (core walk + assertion logic, existing, condensed)
for doc in docs:
    if doc.get("kind") != "Deployment":
        continue
    containers = doc.get("spec", {}).get("template", {}).get("spec", {}).get("containers", [])
    for container in containers:
        if container.get("name") == "dashboard":
            found_dashboard_container = True
            dashboard_env = container.get("env") or []
            break
...
phoenix_entries = [e for e in dashboard_env if e.get("name") == "PHOENIX_BASE_URL"]
if mode == "absent":
    if len(phoenix_entries) == 0:
        print("PASS: ...")
        return 0
    ...
```
A headers-wiring gate needs BOTH containers checked (`manager` and `dashboard`, not just `dashboard` — headers thread through both `deployment.yaml` and `dashboard-deployment.yaml` per D-09's chain), and asserts `valueFrom.secretKeyRef.name`/`.key` rather than a literal `value:` (this is the security-relevant divergence from the Phoenix script — never assert on a literal secret value, only on the ref shape). Invoke it from `Makefile`'s `helm-telemetry-assert` target the same way the Phoenix assertion was added (lines 715-717): render once with the values key unset (`--expect-absent`) and once with it set to a fixture Secret name (`--expect-secretref <name> <key>`).

## Shared Patterns

### Documented-install posture (never bundle the external system)
**Source:** `docs/INSTALL.md` §"Enable telemetry (Prometheus)" (whole section) — TELEM-01 precedent, applies verbatim per D-02.
**Apply to:** `docs/INSTALL.md`, `docs/observability.md`. TIDE ships the wiring value + the docs; Phoenix is a separate `helm install` of its own official chart into its own namespace, never a TIDE subchart.

### Guarded, conditional env injection in Helm templates
**Source:** `charts/tide/templates/dashboard-deployment.yaml` PromQL-proxy comment at line 57 (`{{- /* ... Emitted ONLY when prometheus.endpoint is non-empty */ }}`) and `hack/helm/assert-phoenix-env.py`'s whole purpose (assert absence-by-default, presence-when-set).
**Apply to:** `charts/tide/templates/deployment.yaml`, `charts/tide/templates/dashboard-deployment.yaml`, `charts/tide/templates/NOTES.txt`. Every new conditional env/warning this phase adds must render nothing when its driving value is empty — no dead entries, no dangling buttons, matching the `phoenix.baseURL` deep-link's own "Empty means ... no dead buttons" framing (`charts/tide/values.yaml:427-428`).

### Secret-by-reference, never a literal value
**Source:** `docs/INSTALL.md` §"Provider Secret" / §"Git credentials Secret" (lines 217-246) — `providerSecretRef`/`credsSecretRef`, `--from-literal` shell-only, controller pod never sees the raw key.
**Apply to:** `charts/tide/values.yaml` (`otel.exporter.headersSecretRef`), any Phoenix `PHOENIX_SECRET`/admin-password doc example, `charts/tide/templates/{deployment,dashboard-deployment}.yaml` (`valueFrom: secretKeyRef`, never `value:`). Matches RESEARCH.md's ASVS V6 finding verbatim.

### `OTLPEndpoint`-shaped threading (values → template env → main.go → Deps → 5 reconcilers → ReporterOptions → reporter env)
**Source:** the full chain traced above — `charts/tide/values.yaml:416-421` → `templates/deployment.yaml:89-96` + `templates/dashboard-deployment.yaml:49-56` → `cmd/manager/main.go:285,438,549` → `{milestone,phase,plan,project,task}_controller.go` 5 call sites → `internal/controller/reporter_jobspec.go:97,283-288`.
**Apply to:** every file in the "OTLP-headers wiring" work stream (Pitfall B option 1). This is a single pattern applied 9 times, not 9 different patterns — the planner should treat it as one repeated mechanical edit, and any plan task list should say so explicitly (per this project's "state instruction scope explicitly" convention) rather than re-deriving the shape at each site.

### Offline, cluster-free Helm render-gates over live-cluster/install-based tests
**Source:** `hack/helm/assert-telemetry-render.sh` (whole file, all 8 existing permutations, zero `kubectl`/cluster dependency) + `hack/helm/assert-{prometheus,phoenix,dashboard-rbac}-env.py` (all offline, operate on `helm template` YAML dumps).
**Apply to:** the D-10 NOTES.txt render-gate and the Pitfall B headers render-gate. Both belong in this offline-fast-check family (`make helm-assert`, no cluster) — not in `test/integration/kind` (Layer B, live-cluster, slow). RESEARCH.md's Pitfall A correctly diagnosed that plain `helm template` can't see NOTES.txt, but the codebase's own `tpl`+ConfigMap-probe trick (Permutation G) already solves that without needing a live cluster; direct the planner there instead of Layer B.

## No Analog Found

None — every file this phase touches (or plausibly touches, per the RESEARCH.md-identified Pitfall B wiring gap) has a direct, same-file-or-sibling-file analog already in the codebase. The live-proof evidence markdown (`.planning/phases/47-.../…-evidence.md`, D-13) is a new artifact with no code analog, but it is explicitly a planning/evidence artifact, not source code, and its shape is Claude's Discretion per CONTEXT.md — no pattern mapping applies.

## Metadata

**Analog search scope:** `docs/INSTALL.md`, `docs/observability.md`, `charts/tide/templates/{NOTES.txt,deployment.yaml,dashboard-deployment.yaml}`, `charts/tide/values.yaml`, `hack/helm/{augment-tide-chart.sh,assert-telemetry-render.sh,assert-phoenix-env.py,assert-prometheus-env.py}`, `Makefile` (helm-assert targets), `cmd/manager/main.go`, `internal/controller/{milestone,phase,plan,project,task}_controller.go`, `internal/controller/reporter_jobspec.go` + `_test.go`, `test/integration/kind/projects_pvc_test.go`, `examples/projects/medium/`, `docs/live-e2e.md`, git history for Phase 46 (`git log --oneline -- .planning/phases/46-...`, `git show 62fae82`).
**Files scanned:** ~25 (direct reads) + git log/grep sweeps across `internal/controller/`, `charts/tide/`, `hack/helm/`, `test/integration/kind/`.
**Pattern extraction date:** 2026-07-17
