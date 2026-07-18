# Phase 47: Self-Hosted Phoenix Install + End-to-End Proof - Research

**Researched:** 2026-07-17
**Domain:** Self-hosted Arize Phoenix Helm install (docs) + a live five-level TIDE dispatch proof against it
**Confidence:** HIGH

## Summary

This phase is almost entirely documentation plus a live operational proof — Phases 42–46 already shipped every span-emission surface PROOF-01 needs (dispatch-chain spans, propagation, redacted message arrays, sampler/session/tag enrichment, dashboard deep link). The two genuinely load-bearing pieces of research are: (1) a fresh, live re-fetch of the Phoenix Helm chart, which came back **unchanged** from the `.planning/research/STACK.md` pin two days ago (chart `10.0.0` / appVersion `18.0.0`) — good news, but two independent live fetches (OCI pull + GitHub `main` raw fetch) now confirm it rather than trusting a single two-day-old snapshot; and (2) two concrete implementation gaps this research surfaced that CONTEXT.md's decisions did not anticipate and the planner must explicitly address: the **NOTES.txt render-gate test cannot use the existing fast `helm template` pattern** (verified empirically — `helm template` never renders `NOTES.txt`, only `helm install`/`helm upgrade --dry-run` do, and even `--dry-run=client` still requires a reachable cluster on this Helm version), and **`OTEL_EXPORTER_OTLP_HEADERS` has zero chart wiring today** (no values key, no template env, no `Deps` field threading it to the reporter) — meaning D-08's "durable, auth-on" recipe as literally described is not yet functional without a small, scoped code addition mirroring the already-proven `OTLPEndpoint` threading pattern.

Every other fact CONTEXT.md's D-04 asked this research to re-verify came back confirmed, not just unchanged: `auth.enableAuth: true` is still the chart default; `persistence.enabled`/`postgresql.enabled` mutual exclusivity is not just documented but **actively enforced** by a `fail` guard in the chart's `_helpers.tpl` (`phoenix.validatePersistence`); appVersion `18.0.0` clears the `≥ 14.2.0` deep-link floor with room to spare. The `examples/projects/medium` sample (~$5 hard cap, one Milestone→one Phase→one Plan→3-5 Tasks, real `claude-haiku-4-5`, anonymous in-cluster git, 5-10 min wall time) is the right-sized driving run for D-12/PROOF-01 — it is the only bundled sample that produces real per-Task LLM traffic without touching an external repo or needing a `GIT_PAT`.

**Primary recommendation:** Treat this phase as three work streams — (1) docs (INSTALL.md quickstart + observability.md reference recipe, using the pin/values confirmed below), (2) a small scoped chart change (NOTES.txt nudge + the OTLP-headers wiring gap, since D-08's documented recipe requires it to actually work), (3) the live proof itself on a brand-new throwaway kind cluster (never `tide-dogfood`, which this research confirms is stale v1alpha1/v1alpha2) driving `examples/projects/medium`.

## Architectural Responsibility Map

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|-------------|----------------|-----------|
| Phoenix trace storage (SQLite-on-PVC or bundled Postgres) | Database/Storage (external system) | — | Phoenix owns its own persistence; TIDE only emits OTLP to it, per the locked documented-install posture (never a subchart) |
| `otel.exporter.endpoint` / OTLP header wiring | API/Backend (Helm chart values → manager/dashboard/reporter Pod env) | — | Pure backend transport config; `internal/otelinit/provider.go` + `internal/controller/reporter_jobspec.go` are the only consumers |
| NOTES.txt tracing-dark nudge | API/Backend (Helm chart template, rendered at install/upgrade time) | — | Post-install operator guidance rendered server-side by Helm, not app code |
| Live proof evidence capture (screenshots, trace IDs) | Browser/Client (operator driving Phoenix's UI + TIDE dashboard) | Database/Storage (Phoenix UI reads its own store) | Evidence is captured by browsing two separate UIs against their respective backing stores |
| Dashboard → Phoenix deep link (exercised, not built) | Frontend/Browser (dashboard SPA, `PhoenixTraceLink.tsx`) | API/Backend (`GET /api/v1/config` → `phoenixBaseURL`) | Already shipped in Phase 46; this phase only sets `phoenix.baseURL` and observes it work live |

## User Constraints (from CONTEXT.md)

### Locked Decisions

**Doc placement & recipe shape:**
- D-01: `docs/INSTALL.md` gains an "Enable tracing (Phoenix)" step-shaped section beside "Enable telemetry (Prometheus)" (~line 175) carrying the quickstart path; `docs/observability.md` carries the full reference recipe under Tracing, replacing the "install instructions land in Phase 47" placeholder with a cross-reference. Exact content split is planner's discretion.
- D-02: Separate `helm install` of Phoenix's official OCI chart into its own namespace — never a TIDE subchart (TELEM-01 precedent). Every example pins the exact chart version string, never `latest`.

**Chart pin & version floor:**
- D-03: Research re-fetches `Chart.yaml` fresh at research time; the fetched pin lands in every doc example. Fetch date recorded below.
- D-04: Re-verify three posture facts against the freshly-fetched chart: (a) appVersion ≥ 14.2.0, (b) current `auth.enableAuth` default, (c) storage-mode exclusivity. Divergences update doc text, not just the pin number.

**Storage paths & constrained-VM sizing:**
- D-05: Document exactly the chart's own two strategies — SQLite-on-PVC (`persistence.enabled=true`, `postgresql.enabled=false`) as quickstart/kind-dev, bundled Postgres (chart default) as durable/production.
- D-06: Right-size for the ~8 GiB dev-VM constraint: small `persistence.size` (a few Gi, not 20Gi default), bounded non-zero `database.defaultRetentionPolicyDays` (default 0 = never expire is unacceptable), fold into the existing one-heavy-workload-at-a-time VM discipline. Exact numbers are planner/research discretion.
- D-07: The live proof exercises the SQLite-on-PVC path only. Postgres ships documented as the tested happy path but is NOT live-proven on the dev VM — doc is honest about which path was proven.

**Auth posture:**
- D-08: Documented recipe keeps auth ON with Secret-sourced credentials (`PHOENIX_SECRET`/admin password from a K8s Secret, mirroring TIDE's existing git-creds/API-key Secret pattern). Chart-default-vs-raw-image-default divergence explicitly called out. A dev-only auth-off override may be mentioned honestly alongside the exposure warning (port-forward-only framing). Exact current chart default verified per D-04(b).

**OTLP wiring & NOTES.txt nudge:**
- D-09: `otel.exporter.endpoint` documented end-to-end in bare `host:port` form targeting Phoenix's 4317 gRPC port (never 6006 HTTP/UI). Scheme-prefix failure mode gets an explicit warning callout. Doc shows the full chain: `values.yaml` → manager + dashboard Deployment env → reporter Job env forwarding.
- D-10: NOTES.txt gains a tracing-dark nudge mirroring the existing `prometheus.enabled` warning block: when `otel.exporter.endpoint` is empty, print a pointer to the Phoenix step. Edit MUST land in `hack/helm/augment-tide-chart.sh` heredoc (step 4b) AND the template. A helm-template contract test gates the rendered output both ways.

**Live-proof environment, run shape & evidence:**
- D-11: Proof runs on a fresh kind cluster stood up by following the documented recipe itself. `tide-dogfood`'s currency is unverified (predates v1alpha3) — default is delete/recreate per the constrained-VM clean-run recipe, one heavy run at a time, Phoenix pod counted in the memory budget.
- D-12: Driving run is a small real project with real API spend (durable key at `~/.tide/anthropic.key`, verified present), sized to produce the complete five-level tree with Task-level message arrays at bounded cost — small-project shape (`examples/projects`), not dogfood scale. Pre-flight cheap static checks before spending: helm-template render verifying env wiring end-to-end, and a no-spend OTLP connectivity check before the first real dispatch.
- D-13: Evidence lands in the phase dir as milestone-close evidence: five-level trace-tree screenshot, Task-level LLM span detail showing redacted message arrays, a queryability demonstration (Phoenix DSL filter over OBS-03 `tag.tags`/metadata), recorded trace IDs in a small evidence markdown. Browser-driven capture. Set `phoenix.baseURL` during the proof to exercise OBS-04 live.
- D-14: Known-limitations honesty at close: single-node kind cannot surface cross-pod clock skew (Pitfall 5) — documented as a known limitation, not claimed verified. Any real defect the live proof surfaces is named and root-fixed in-phase — never worked around to get the screenshot.

### Claude's Discretion
- Phoenix namespace name in examples (`phoenix` vs `observability` — pick one, use consistently everywhere).
- Exact PVC size and retention-days numbers (D-06), grounded in fixture-scale volumes.
- INSTALL.md-vs-observability.md content split details within D-01's frame; section ordering and cross-link wording.
- Evidence markdown filename/shape; whether one hero screenshot also lands in `docs/` as operator-facing polish.
- Mechanics of the no-spend OTLP connectivity check (D-12).
- NOTES.txt nudge wording and whether it keys on `otel.exporter.endpoint` alone or also mentions `phoenix.baseURL`.

### Deferred Ideas (OUT OF SCOPE)
- Postgres path live-proof — documented as chart's tested happy path but not live-proven on the 8 GiB dev VM.
- Multi-node clock-skew validation (Pitfall 5) — unexercisable on single-node kind.
- Tail-sampling collector recipe, Grafana dashboards, alert rules — already queued in observability.md's "What's coming."
- Data-minimization toggle (`otel.redactMessageContent`/ArtifactPath-only mode) — carried from Phase 46, fast-follow candidate.

## Phase Requirements

| ID | Description | Research Support |
|----|-------------|------------------|
| PHX-01 | INSTALL.md/observability.md document a self-hosted Phoenix recipe covering both storage paths + the `auth.enableAuth=true` chart default | Fresh chart pin + values.yaml fetch below gives exact keys/defaults for both paths; D-04(b)/(c) facts re-verified live; NodePort/Ingress defaults and resource requests newly surfaced for accurate sizing guidance |
| PHX-02 | `otel.exporter.endpoint` wiring documented end-to-end (bare `host:port`) + NOTES.txt nudge | Full wiring chain traced through actual source (`values.yaml` → `deployment.yaml`/`dashboard-deployment.yaml` → `cmd/manager/main.go` → `reporter_jobspec.go`); NOTES.txt render-gate testing gap identified with a verified root cause |
| PROOF-01 | Live five-level trace tree including redacted message arrays, visible/queryable in Phoenix, with evidence captured | `examples/projects/medium` identified as the right-sized driving run (cost, level depth, no external creds); OTLP-headers wiring gap identified as a blocker for auth-on live proof if D-08's recipe is followed literally |

## Standard Stack

### Core

| Component | Version | Purpose | Why Standard |
|-----------|---------|---------|---------------|
| Phoenix Helm chart | `oci://registry-1.docker.io/arizephoenix/phoenix-helm` **chart 10.0.0**, `appVersion: "18.0.0"`, image `arizephoenix/phoenix:version-18.0.0-nonroot` | Self-hosted trace store + UI + OTLP ingest | [VERIFIED: OCI registry + Arize-ai/phoenix GitHub `main`] — re-fetched live 2026-07-17 via two independent channels: `helm show chart oci://registry-1.docker.io/arizephoenix/phoenix-helm` (no `--version` pin, resolves latest) returned digest `sha256:fa3beb12b4f2373741c4872d866a6b1a0aee1220b0d1c4b7f2e4ce07970ead33`, chart `10.0.0`/appVersion `18.0.0`; `WebFetch` of `raw.githubusercontent.com/Arize-ai/phoenix/main/helm/Chart.yaml` independently returned the identical version pair. **This is unchanged from the `10.0.0`/`18.0.0` pin STACK.md recorded 2 days earlier** — the "near-daily churn" warning was correct methodology (re-verify, don't trust), but in this instance the chart has genuinely stabilized at `10.0.0` for the two-day window. Do not skip the re-fetch on this basis in future phases — the churn pattern STACK.md observed (9 versions in ~9 days) is real, this is one stable window. |
| `postgres` (groundhog2k) subchart | `1.5.8` (Phoenix's own pinned dependency, `condition: postgresql.enabled`) | Bundled Postgres for the "durable" storage path | [VERIFIED] — confirmed in the fetched `Chart.yaml`'s `dependencies:` block, unchanged from STACK.md |
| TIDE's own `otel.*` chart values + `internal/otelinit` | Already shipped (Phase 42-46) | Manager/dashboard TracerProvider construction, OTLP gRPC export | [VERIFIED: direct source read] — zero new Go dependencies for this phase; this phase adds **no new go.mod entries** |

**No `go get` / `npm install` / `pip install` in this phase.** The only external artifact pulled is the Phoenix Helm chart via `helm install`/`helm pull` — see Package Legitimacy Audit below for its provenance check.

### Fresh values.yaml facts (fetched 2026-07-17, chart 10.0.0)

Pulled locally via `helm pull oci://registry-1.docker.io/arizephoenix/phoenix-helm --version 10.0.0 --untar` and read directly — every fact below is [VERIFIED: OCI chart pull, chart 10.0.0] unless noted.

| Key | Default | Relevance |
|-----|---------|-----------|
| `postgresql.enabled` | `true` | Chart default is the Postgres/durable path, matching D-05's framing exactly |
| `postgresql.storage.requestedSize` | `20Gi` | The Pitfall 9 sizing concern — confirmed live |
| `persistence.enabled` | `false` | Must flip to `true` for the SQLite/quickstart path (D-05) |
| `persistence.size` | `20Gi` | Right-size down per D-06 — see recommendation below |
| `persistence.inMemory` | `false` | A third, undocumented-by-CONTEXT.md mode exists (`sqlite:///:memory:`) — chart-enforced mutually exclusive with the other two, demo/testing only, data lost on restart. Not part of D-05's two documented strategies; worth a one-line "don't use this for the recipe" mention only if it comes up. |
| `database.defaultRetentionPolicyDays` | `0` (never expire) | Confirms Pitfall 8 exactly; must be overridden per D-06 |
| `database.allocatedStorageGiB` | `20` | Separate from `persistence.size`/`postgresql.storage.requestedSize` — a third, seemingly-redundant storage knob in the same values file; the two PVC-size keys above are the ones that actually provision a PVC — this one appears documentation/estimation-only. Worth a grep-confirm at plan time if the doc references it. |
| `auth.enableAuth` | `true` | D-04(b) confirmed unchanged — chart-level default IS auth-on, opposite the raw image |
| `auth.name` | `"phoenix-secret"` | The K8s Secret name the chart creates/expects |
| `auth.createSecret` | `true` | Chart auto-generates the Secret when true |
| `auth.secret[]` | list of `{key, value, valueFrom}` entries: `PHOENIX_SECRET`, `PHOENIX_ADMIN_SECRET`, `PHOENIX_POSTGRES_PASSWORD`, `PHOENIX_SMTP_PASSWORD`, `PHOENIX_DEFAULT_ADMIN_INITIAL_PASSWORD` — all autogenerated (`randAlphaNum 32`) when `value: ""` except the Postgres password, which defaults to the literal string `"postgres"` | **This is a list, not a single secret value** — STACK.md's framing of "supply `auth.secret`" undersold the actual shape. `PHOENIX_POSTGRES_PASSWORD` defaults to a real (weak) plaintext `"postgres"` even though it's templated through the same auto-b64enc mechanism — worth flagging in the durable/Postgres-path doc as a value to override, not just accept. |
| `auth.defaultAdminPassword` | `"admin"` | Fallback value if `auth.secret[].value` for `PHOENIX_DEFAULT_ADMIN_INITIAL_PASSWORD` is empty — the initial admin login password baseline; weak, should be called out |
| `server.grpcPort` / `server.port` | `4317` / `6006` | Confirms STACK.md — gRPC (4317) is TIDE's target, never 6006 |
| `service.type` | `"NodePort"` | **Not previously recorded in STACK.md/PITFALLS.md.** NodePort is more exposed than ClusterIP by default (every node's IP at a high port, no ingress needed). Recommend the recipe override to `ClusterIP` + `kubectl port-forward` for the kind/dev path, matching TIDE's own dashboard access pattern (`docs/INSTALL.md` "Verifying the install" uses `kubectl port-forward svc/tide-dashboard`). |
| `ingress.enabled` | `true`, `ingress.host: ""` | **Not previously recorded.** Chart enables an Ingress resource by default with no host set. Harmless on a kind cluster with no IngressClass (Ingress objects with no matching controller just sit inert — confirmed no chart-level `fail` guard blocks this), but worth an explicit `--set ingress.enabled=false` in the kind/dev recipe to avoid an unimplemented dangling resource in `kubectl get all` output. |
| `resources` (Phoenix app pod) | requests `500m`/`1Gi`, limits `1000m`/`2Gi` | Concrete number for the VM memory budget (see Environment Availability below) |
| `postgresql.resources` | requests `100m`/`256Mi`, limits `500m`/`512Mi` | Only relevant if documenting the durable path's resource footprint |
| `server.maxSpansQueueSize` | `20000` | Chart's own comment: "~50KiB per span means 20,000 spans = ~1GiB" — this **upgrades PITFALLS.md's MEDIUM-confidence practitioner-reported sizing estimate to HIGH confidence**, since it's now sourced directly from the chart's own values.yaml comment, not a blog post |

### Storage-mode exclusivity is chart-enforced, not just documented

[VERIFIED: chart source, `templates/_helpers.tpl:80-95`, chart 10.0.0] — PITFALLS.md characterized this as "the chart's own NOTES.txt warns on this." That is inaccurate in the specific mechanism (the fetched `NOTES.txt` warns about a *different* case — Postgres disabled with no external DB configured — not the persistence/postgresql conflict). The actual enforcement is a `{{- include "phoenix.validatePersistence" . }}` call at the top of `templates/phoenix/pvc.yaml`, which runs a `{{- fail "..." }}` with a detailed multi-strategy error message whenever `persistence.enabled=true` AND `postgresql.enabled=true` are both set (also guards the `persistence.inMemory` combinations and `persistence.enabled` + `database.url` combinations). This is **stronger** than PITFALLS.md implied: a bad recipe combination fails `helm install`/`helm template` outright with a clear message, rather than silently misconfiguring. Good news for the doc — no need to hand-hold the operator away from the mistake as carefully; the chart already refuses it.

## Package Legitimacy Audit

This phase installs no Go modules, no npm/pip/cargo packages. The only external artifact is the Phoenix Helm chart, pulled from Arize's official OCI registry.

| Package | Registry | Age | Downloads | Source Repo | slopcheck | Disposition |
|---------|----------|-----|-----------|--------------|-----------|-------------|
| `arizephoenix/phoenix-helm` (chart 10.0.0) | `oci://registry-1.docker.io/arizephoenix` | Chart actively maintained (10.x line, ~9 releases in the 9 days preceding this research per STACK.md's own tag-history check) | N/A (OCI, no npm-style download counter) | [github.com/Arize-ai/phoenix](https://github.com/Arize-ai/phoenix) — confirmed via `Chart.yaml`'s `sources:` field, `maintainers: [{name: arize, email: phoenix-devs@arize.com}]` | N/A — Helm chart, not an npm/pip/cargo ecosystem package; slopcheck does not cover OCI Helm charts | **Approved** — provenance independently verified via two live channels (OCI registry pull digest + GitHub `main` raw Chart.yaml), both agreeing on version/appVersion/maintainer identity. This is the same official chart STACK.md verified two days ago; no drift, no substitution risk. |

**Packages removed due to slopcheck [SLOP] verdict:** none (not applicable — no npm/pip/cargo packages in scope).
**Packages flagged as suspicious [SUS]:** none.

## Architecture Patterns

### System Architecture Diagram

```
Operator                    kind cluster ("tide-phoenix-proof" — NEW, never tide-dogfood)
   |                        ┌─────────────────────────────────────────────────────┐
   | 1. helm install         │  namespace: phoenix (or "observability" — pick one)   │
   |    tide-crds, tide       │  ┌─────────────┐        ┌──────────────────────┐   │
   |--------------------------→│ tide-system  │        │  Phoenix Deployment   │   │
   |                        │  │  manager Pod │        │  (SQLite-on-PVC path) │   │
   | 2. helm install Phoenix │  │  dashboard   │        │  4317 gRPC / 6006 UI  │   │
   |--------------------------→│  Pod          │        └──────────┬───────────┘   │
   |                        │  └──────┬───────┘                    │               │
   | 3. helm upgrade tide    │         │ OTEL_EXPORTER_OTLP_ENDPOINT│               │
   |    --set otel.exporter  │         │ = tide-phoenix.<ns>.svc:4317              │
   |    .endpoint=...        │         ▼                            │               │
   |--------------------------→ (manager + dashboard + reporter Jobs) ─────────────→│
   |                        │   retroactive AGENT spans (5 levels)  │  OTLP gRPC    │
   |                        │   + redacted LLM message-array spans  │  ingest       │
   |                        │                                        ▼               │
   | 4. kubectl apply         │                              SQLite on PVC          │
   |    examples/projects/    │                              (persistence.enabled)  │
   |    medium/project.yaml   │                                                     │
   |--------------------------→ (real Claude dispatch: Milestone→Phase→Plan→Task→Wave)
   |                        └─────────────────────────────────────────────────────┘
   | 5. kubectl port-forward
   |    svc/tide-phoenix 6006:6006
   |--------------------------→ browser → Phoenix UI: trace tree, span detail,
   |                                       DSL filter query
   | 6. screenshots + trace IDs → .planning/phases/47-.../evidence
```

Entry points: three sequential `helm install`/`upgrade` commands (CRDs, TIDE, Phoenix, in that order per D-04) plus one `kubectl apply` (the driving Project). Processing stages: OTLP export from three TIDE pod types (manager, dashboard, reporter Jobs) into Phoenix's ingest endpoint; Phoenix persists to its chosen storage backend; the operator queries the result via port-forward + browser. Decision points: which storage path (SQLite vs Postgres — D-05/D-07 picks SQLite for the live proof); which auth posture (D-08 — see the Common Pitfalls section for the wiring gap this creates).

### Recommended Doc Structure (within existing files, per D-01)

```
docs/INSTALL.md
└── "Enable tracing (Phoenix)"          # new section, sibling to "Enable telemetry (Prometheus)" (~line 175)
    ├── 1. helm install Phoenix (quickstart values, pinned version)
    ├── 2. helm upgrade tide --set otel.exporter.endpoint=...
    └── 3. Verify (port-forward + trace query)

docs/observability.md
└── ## Tracing                          # existing section (line ~156)
    ├── ### Self-hosted Phoenix          # NEW subsection — full recipe, both storage paths
    │   ├── Quickstart (SQLite-on-PVC)
    │   ├── Durable (bundled Postgres) — documented, not live-proven (D-07)
    │   └── Auth posture (Secret-sourced, chart default true)
    └── ### Dashboard deep link to Phoenix (`phoenix.baseURL`)  # existing (line ~236) —
        replace "install instructions land in Phase 47" placeholder with cross-reference
```

### Pattern 1: The Prometheus structural precedent (D-01/D-02's exact template)

**What:** `docs/INSTALL.md`'s "Enable telemetry (Prometheus)" section (lines 175-215) is a proven three-beat structure: (1) install the external system, (2) wire TIDE's chart values at it, (3) verify via a concrete, copy-pasteable check with an explicit "done" signal.
**When to use:** Model the new "Enable tracing (Phoenix)" section on this exactly — same three beats, same NOTES.txt-warning tie-in (`prometheus.enabled` → `otel.exporter.endpoint` empty check).
**Example (existing precedent to mirror):**
```markdown
# Source: docs/INSTALL.md lines 179-213
**1. Install kube-prometheus-stack** (skip if ...):
helm install kps prometheus-community/kube-prometheus-stack -n monitoring --create-namespace

**2. Enable TIDE's telemetry surfaces.** ...
helm upgrade tide oci://ghcr.io/jsquirrelz/tide-charts/tide -n tide-system --reuse-values \
    --set prometheus.enabled=true ...

**4. Verify at the Targets page** — this is the done signal: ...
```

### Pattern 2: The already-shipped bare-`host:port` precedent in observability.md

**What:** `docs/observability.md` line 172-174 already shows a scheme-less endpoint example for the generic-collector case.
**When to use:** The Phoenix example should be presented as the concrete instance of this same pattern, not a new one — reduces doc surface, reinforces the rule is general, not Phoenix-specific.
**Example:**
```bash
# Source: docs/observability.md:172-174 (existing, unmodified)
helm upgrade tide ./charts/tide -n tide-system \
  --set otel.exporter.endpoint=otel-collector.observability.svc:4317
# The Phoenix recipe is the same shape:
#   --set otel.exporter.endpoint=tide-phoenix.<namespace>.svc.cluster.local:4317
```

### Pattern 3: `OTLPEndpoint` threading — the model for any headers addition

**What:** `OTEL_EXPORTER_OTLP_ENDPOINT` flows: `charts/tide/values.yaml:417-418` (`otel.exporter.endpoint`) → `templates/deployment.yaml:89-90` + `templates/dashboard-deployment.yaml:49-50` (container env) → `cmd/manager/main.go:285` (`os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")`) → `PlannerReconcilerDeps.OTLPEndpoint` / `TaskReconcilerDeps.OTLPEndpoint` (`main.go:438,549`) → 5 reconciler call sites (`milestone_controller.go:656`, `phase_controller.go:609`, `plan_controller.go:664`, `project_controller.go:1924`, `task_controller.go:1106`) → `ReporterOptions.OTLPEndpoint` → `internal/controller/reporter_jobspec.go:283-286` (reporter Job env).
**When to use:** This is the exact shape any `OTEL_EXPORTER_OTLP_HEADERS` addition must replicate (see Common Pitfalls — this wiring does not exist yet).
**Example:**
```go
// Source: internal/controller/reporter_jobspec.go:280-287 (existing, unmodified)
var env []corev1.EnvVar
if opts.OTLPEndpoint != "" {
    env = []corev1.EnvVar{
        {Name: "OTEL_EXPORTER_OTLP_ENDPOINT", Value: opts.OTLPEndpoint},
        {Name: "OTEL_BSP_MAX_EXPORT_BATCH_SIZE", Value: "6"},
    }
}
```

### Anti-Patterns to Avoid

- **Don't test NOTES.txt rendering with `helm template`.** Confirmed empirically (see Common Pitfalls) — it silently produces no NOTES output at all, so a test asserting on `helm template` output for the D-10 nudge will pass vacuously (never actually checking the nudge text) rather than fail loudly.
- **Don't assume `OTEL_EXPORTER_OTLP_HEADERS` "just works" once a Secret exists.** The env var must be explicitly wired onto the manager/dashboard/reporter container specs; no such wiring exists in the chart today (see Common Pitfalls).
- **Don't reuse `tide-dogfood` for the live proof.** Confirmed live: `kubectl get crd projects.tideproject.k8s -o jsonpath='{.spec.versions[*].name}'` against `kind-tide-dogfood` returns `v1alpha1 v1alpha2` — no `v1alpha3`. The installed chart is `tide-1.0.5` (`helm list -A`). This predates the Phase 40 v1alpha3 crank and per D-11 must not be treated as current.
- **Don't leave Phoenix's `ingress.enabled=true`/`service.type=NodePort` defaults unexamined in the kind/dev recipe.** Neither breaks the install, but both are more exposed/cluttered than the port-forward pattern TIDE's own docs already use everywhere else.

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|--------------|-----|
| Trace storage/UI/query | A bespoke TIDE trace viewer | Phoenix (already locked out-of-scope in REQUIREMENTS.md's Out of Scope table) | Phoenix IS the purpose-built trace UI this milestone adopts |
| Storage-mode validation (SQLite vs Postgres exclusivity) | A doc-only warning or a TIDE-side pre-flight check | Rely on the chart's own `phoenix.validatePersistence` `fail` guard (confirmed present, chart 10.0.0) | Already enforced at `helm install`/`template` time — duplicating it in TIDE-side tooling is redundant |
| Auth/secret generation for Phoenix | A TIDE-authored Secret manifest for Phoenix credentials | The chart's own `auth.createSecret=true` + `auth.secret[]` autogeneration (`randAlphaNum 32` for unset values) | Chart already handles this correctly; TIDE's job is to override the weak defaults (`auth.defaultAdminPassword: "admin"`, `PHOENIX_POSTGRES_PASSWORD: "postgres"`), not reimplement secret creation |

**Key insight:** Nearly everything this phase needs already exists — either in Phoenix's own chart (validation, secret generation, health probes) or in TIDE's own shipped code (Phases 42-46's span emission, Phase 46's deep link). The actual net-new engineering surface is narrow: a headers env-var threading addition (mirrors existing `OTLPEndpoint` pattern exactly) and a NOTES.txt template edit (mirrors the existing `prometheus.enabled` block exactly).

## Common Pitfalls

### Pitfall A (NEW — not in PITFALLS.md): `helm template` never renders NOTES.txt; the D-10 render-gate test needs a reachable cluster

**What goes wrong:** CONTEXT.md D-10 calls for "a helm-template contract test [that] gates the rendered output both ways," modeled on "Phase 38 TELEM-02 / Phase 46-02 render-gate precedent." Verified empirically on this machine (Helm v3.16.3): `helm template tide ./charts/tide ...` (no `install`/`upgrade`, no `--dry-run`) produces **zero** NOTES.txt output — confirmed by running it against TIDE's actual chart and grepping for the known NOTES.txt text (`WARNING: run telemetry...`) with no match, then confirming the `--hide-notes` flag exists but doesn't change this (it only suppresses notes for `install`/`upgrade`, which show them by default; `template` never shows them regardless). NOTES.txt only renders via `helm install --dry-run` or `helm upgrade --dry-run` — **and even `--dry-run=client`, on this Helm version, still calls `(*kube.Client).IsReachable()` and errors `Kubernetes cluster unreachable` if no valid kubeconfig context exists** (confirmed by pointing `KUBECONFIG` at a nonexistent file and reproducing the exact stack trace in `helm.sh/helm/v3/pkg/action.(*Install).RunWithContext` → `client.go:135`). This is a **general Helm 3.16.3 behavior**, not chart-specific.

**Why it happens:** The existing fast `TestHelmDeploymentTemplate*` tests in `test/integration/kind/projects_pvc_test.go` all use plain `helm template` (confirmed by reading the file — `exec.Command("helm", "template", ...)`, no `install`/`upgrade`/`--dry-run`). That pattern works for every *manifest* template but silently cannot exercise NOTES.txt at all, because NOTES.txt isn't a manifest — it's rendered by a different code path in Helm's own `action` package that only fires on `install`/`upgrade`.

**How to avoid:** Two viable strategies, pick one explicitly at plan time:
1. **Raw-file byte-assertion** (matches the *other* existing pattern in the same file — `TestHelmDeploymentTemplateRendersManagerPodAnnotations` reads `charts/tide/templates/deployment.yaml` bytes directly and asserts a string is present, without ever invoking Helm). Read `charts/tide/templates/NOTES.txt` bytes, assert both the `{{- if not .Values.otel.exporter.endpoint }}` conditional and the nudge text exist. Fast, offline, zero cluster dependency — but weaker: proves the conditional text exists, not that it renders correctly both ways.
2. **Live-render assertion** — `helm upgrade tide ./charts/tide --dry-run --namespace tide-system --set otel.exporter.endpoint=... [--reuse-values or full required --set list]` against the already-provisioned `test/integration/kind` cluster (`kindClusterName = "tide-test"`, stood up by `make test-int-kind-prep` per `suite_test.go`). This is the only way to actually prove "nudge present when empty, absent when set" as D-10's letter requires — but it must run in the Layer B kind package where the cluster is guaranteed live, not as a standalone fast test.

**Warning signs:** A new test that calls `helm template` and asserts on NOTES.txt content will compile and always pass (or always silently produce empty output), giving false confidence that the render gate works.

### Pitfall B (NEW — not in PITFALLS.md): `OTEL_EXPORTER_OTLP_HEADERS` has no chart wiring today — D-08's "durable" recipe is not yet functional as literally described

**What goes wrong:** D-08 requires the documented recipe to keep auth ON with `OTEL_EXPORTER_OTLP_HEADERS=Authorization=Bearer <api-key>` set on TIDE's manager/reporter Pods, "sourced from a Secret ... same pattern already used for git creds and LLM API keys ... no code change" (this exact "no code change" framing also appears in `.planning/research/STACK.md`'s Integration Notes). **This framing is not accurate against current code.** Confirmed by exhaustive grep: `charts/tide/values.yaml` has no `otel.exporter.headers`-shaped key; `charts/tide/templates/deployment.yaml` and `dashboard-deployment.yaml` have no `OTEL_EXPORTER_OTLP_HEADERS` env entry (only `OTEL_EXPORTER_OTLP_ENDPOINT`/`OTEL_TRACES_SAMPLER`/`OTEL_TRACES_SAMPLER_ARG`/`OTEL_SERVICE_NAME`); `internal/controller/reporter_jobspec.go`'s `ReporterOptions` struct has no headers field; none of the 5 reconciler call sites that thread `OTLPEndpoint` thread anything header-shaped. What **is** confirmed true: TIDE's `internal/otelinit/provider.go` never calls `otlptracegrpc.WithHeaders(...)` explicitly, and the pinned SDK (`go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc@v1.43.0/doc.go:29-35`, read directly from the local module cache) documents that `OTEL_EXPORTER_OTLP_HEADERS`/`OTEL_EXPORTER_OTLP_TRACES_HEADERS` (format `"key1=value1,key2=value2"`) are honored automatically whenever `WithHeaders` isn't called — so **the SDK side of the claim is correct** [VERIFIED: pinned v1.43.0 source]. The gap is entirely on TIDE's own chart/controller side: the env var never reaches the container in the first place.

**Why it happens:** STACK.md's research (2 days prior) verified the SDK behavior correctly but extrapolated "no code change" from that without checking whether the chart already had an env-injection point for it — it didn't check the deployment templates or `ReporterOptions` the way this phase's research did.

**How to avoid:** The planner must make an explicit choice, since D-08 doesn't currently carve out an exception the way D-07 does for Postgres:
1. **Add the minimal wiring** (recommended — mirrors Pattern 3 above exactly, ~5 small edits): a new `otel.exporter.headersSecretRef`-shaped values key (referencing a Secret + key, never a literal `value:`, matching the `providerSecretRef`/git-creds convention) → `valueFrom: secretKeyRef` env entries on `deployment.yaml` + `dashboard-deployment.yaml` → `os.Getenv("OTEL_EXPORTER_OTLP_HEADERS")` in `cmd/manager/main.go` → a new `OTLPHeaders` (or similar) field threaded through the same 5 reconciler call sites as `OTLPEndpoint` → a matching env entry in `reporter_jobspec.go`. This makes the "durable" recipe real, not aspirational, and is consistent with this project's stated preference for root-fixing over shipping documented-but-nonfunctional recipes (`~/.claude/.../feedback_tide_fix_thoroughly.md`).
2. **Or explicitly descope**, mirroring D-07's Postgres carve-out: document the auth-on recipe as "chart wiring for OTLP header auth does not exist yet — this path is documented for a future phase; the live proof and any hands-on verification use the dev-only auth-off override" with the exposure warning intact. This keeps PHX-01's letter satisfied (both storage paths + the auth-default callout are still documented) without expanding this phase's code surface.

Either choice is defensible; what's **not** defensible is documenting the auth-on recipe as complete/working without either building the wiring or explicitly flagging it as unverified/future, per D-14's "any real defect ... is named and root-fixed in-phase — never worked around to get the screenshot" and the general "no known-issue-shipped" project posture.

**Warning signs:** A doc section showing `--set-string auth.enableAuth=true` plus an `OTEL_EXPORTER_OTLP_HEADERS` example with no corresponding chart values key or template diff in the same phase's plan.

### Pitfalls carried forward from `.planning/research/PITFALLS.md` (still applicable, unchanged)

The full detail for these lives in PITFALLS.md and is not repeated here — only the phase-47-relevant subset with anything this research's live re-check changed:

- **Pitfall 5 (cross-pod clock skew):** unchanged, D-14 already commits to documenting as a known limitation.
- **Pitfall 6 (sampler coin-flip):** **inert for this phase** — `docs/observability.md:180-190` confirms `otel.tracesSamplerArg` now defaults to `1.0` (Phase 46 OBS-01 shipped), so the quickstart needs no override. Still worth a one-line "you're already at 1.0 by default" reassurance in the new Phoenix section so an operator doesn't go hunting for a sampler override that no longer applies.
- **Pitfall 8 (ephemeral/infinite-retention/no-auth defaults):** `database.defaultRetentionPolicyDays: 0` re-confirmed live; `auth.enableAuth: true` re-confirmed live (the "no-auth" framing was already backwards for the *chart* default even at STACK.md's original research time — worth double-checking the doc language doesn't accidentally imply the chart ships insecure-by-default when it's actually the opposite of the raw image).
- **Pitfall 9 (VM footprint):** re-confirmed with concrete numbers now available — Phoenix app pod requests `1Gi`/limits `2Gi` (no bundled Postgres on the SQLite path). Live-measured on `kind-tide-dogfood`: the kind control-plane container alone already uses `2.65GiB` of the `7.654GiB` VM budget (`docker stats --no-stream`), leaving roughly `5GiB` headroom before any TIDE or Phoenix workload — comfortably fits Phoenix's SQLite-path request, tight but workable alongside a manager + dashboard + a handful of dispatch Jobs. Skipping the bundled Postgres subchart (per D-07/D-05) avoids the additional `256Mi`-`512Mi` Postgres pod entirely.

## Code Examples

### Verified Phoenix quickstart recipe shape (values confirmed live, chart 10.0.0)

```bash
# Source: this research's live chart pull + values.yaml read, cross-checked
# against STACK.md's Installation section shape.
export CHART_URL=oci://registry-1.docker.io/arizephoenix/phoenix-helm
export CHART_VERSION=10.0.0   # confirmed current as of 2026-07-17 — re-verify if this phase executes later

helm install tide-phoenix "$CHART_URL" --version "$CHART_VERSION" \
  --namespace phoenix --create-namespace \
  --set persistence.enabled=true \
  --set persistence.size=2Gi \
  --set postgresql.enabled=false \
  --set database.defaultRetentionPolicyDays=7 \
  --set service.type=ClusterIP \
  --set ingress.enabled=false \
  --set auth.enableAuth=true   # chart default; explicit for doc clarity
```

`persistence.size=2Gi`/`retentionDays=7` are grounded-but-discretionary per D-06 — see the Standard Stack table's `maxSpansQueueSize`-derived ~1GiB/20K-span heuristic and the `examples/projects/medium` cost/scale numbers below for the reasoning; the planner should pick final numbers.

### The full `OTEL_EXPORTER_OTLP_ENDPOINT` chain (verified end-to-end, unmodified by this phase)

```yaml
# Source: charts/tide/values.yaml:416-421 (existing)
otel:
  exporter:
    endpoint: ""
  tracesSampler: "parentbased_traceidratio"
  tracesSamplerArg: "1.0"
  serviceName: "tide-controller-manager"
```
```yaml
# Source: charts/tide/templates/deployment.yaml:89-96 (existing, manager)
- name: OTEL_EXPORTER_OTLP_ENDPOINT
  value: {{ quote .Values.otel.exporter.endpoint }}
- name: OTEL_TRACES_SAMPLER
  value: {{ quote .Values.otel.tracesSampler }}
- name: OTEL_TRACES_SAMPLER_ARG
  value: {{ quote .Values.otel.tracesSamplerArg }}
```
```go
// Source: cmd/manager/main.go:285 (existing)
otlpEndpoint := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")
```

### NOTES.txt current shape (the D-10 edit site, both places)

```
{{- /* charts/tide/templates/NOTES.txt (existing, line 1-15) */ -}}
TIDE {{ .Chart.AppVersion }} installed in {{ .Release.Namespace }}.

Dashboard:  kubectl -n {{ .Release.Namespace }} port-forward svc/{{ include "tide.fullname" . }}-dashboard 8080:80
Docs:       https://github.com/jsquirrelz/tide/blob/main/docs/INSTALL.md

{{- if not .Values.prometheus.enabled }}

WARNING: run telemetry beyond the budget tally is unavailable —
prometheus.enabled is false.
Token spend over time, dispatch counts, and per-level durations will be dark.
Enable: see the "Enable telemetry" step in docs/INSTALL.md.
{{- end }}
```
This exact block must be edited in BOTH `charts/tide/templates/NOTES.txt` AND the heredoc at `hack/helm/augment-tide-chart.sh` lines 108-124 (`cat > "${CHART_DIR}/templates/NOTES.txt" <<'EOF' ... EOF`) — confirmed by reading the script directly; the file's own header comment (line 1-3) already warns of this.

## State of the Art

| Old (research-time, 2 days prior) | Current (this research, live re-fetch) | When Changed | Impact |
|--------------------------------|------------------------------------------|---------------|--------|
| Phoenix chart `10.0.0`/appVersion `18.0.0` (STACK.md, MEDIUM confidence, flagged as likely-stale) | Chart `10.0.0`/appVersion `18.0.0` (re-fetched, HIGH confidence, two independent channels agree) | No change in 2 days | The pin is confirmed correct as-is; no doc update needed to the version string, only to the confidence level backing it |
| PITFALLS.md: "chart's own NOTES.txt warns on this [mutual exclusion]" | The exclusion is enforced by a `fail` guard in `_helpers.tpl`, NOT by NOTES.txt (NOTES.txt warns about a different case) | This research | Doesn't change the doc's guidance, but corrects the citation if anyone traces it back |
| STACK.md: OTLP headers auth is "no code change" | No chart wiring exists for `OTEL_EXPORTER_OTLP_HEADERS` — see Pitfall B | This research | Materially changes phase scope — planner must decide to add wiring or descope the auth-on live proof |

**Deprecated/outdated:** None — this is a fast-moving external dependency (Phoenix), not a deprecation-prone API surface for this phase's scope.

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|-------|---------|----------------|
| A1 | `persistence.size=2Gi` / `database.defaultRetentionPolicyDays=7` are reasonable defaults for the documented recipe | Code Examples / Standard Stack | Low — both are explicitly Claude's Discretion per CONTEXT.md; if too small, a longer-running dogfood habit could hit the PVC ceiling, easily fixed by bumping the value in a follow-up `helm upgrade` |
| A2 | `PHOENIX_ADMIN_SECRET` (auto-generated by the chart) can be used as a bearer token to mint a system API key via Phoenix's own API, which is then the value placed into `OTEL_EXPORTER_OTLP_HEADERS` | Common Pitfalls / Pitfall B remediation | [CITED: arize.com/docs/phoenix/self-hosting/features/authentication via WebSearch, MEDIUM-HIGH confidence — official docs page, not independently re-fetched via WebFetch in this session] — if the exact API-key-minting mechanics differ, the durable-recipe doc section would need a correction pass, but the core finding (no chart env wiring exists) stands regardless |
| A3 | Ingress with `ingress.enabled=true`/no host set is harmless (no chart-level `fail` guard, sits inert on kind) | Common Pitfalls / Standard Stack | Low — not independently confirmed by actually rendering the Ingress template and applying it to a live cluster in this research pass; if wrong, the recipe should add `--set ingress.enabled=false` regardless (already recommended) |

## Open Questions (RESOLVED)

> Both questions were resolved during planning (2026-07-17): OQ1 — the orchestrator resolved **build the OTLP-headers wiring in-phase** (Pitfall B option 1), planned as 47-01 (Go threading) + 47-02 (chart wiring + render gates). OQ2 — Plan 47-03's "Locked discretionary values" block pins namespace `phoenix`, `persistence.size=2Gi`, retention 7 days.

1. **RESOLVED (wiring built in-phase — Plans 47-01/47-02).** Does D-08's auth-on "durable" recipe get its own OTLP-headers chart wiring in this phase, or does the live proof use the auth-off dev override?
   - What we know: The wiring doesn't exist today (Pitfall B); D-08 doesn't carve out an explicit exception the way D-07 does for Postgres; D-14 says real defects are root-fixed in-phase, not worked around.
   - What's unclear: Whether "root-fix" here means "add the small chart wiring" (in scope) or whether documenting-but-not-wiring is an acceptable interpretation of PHX-01's letter (which asks for documentation of the auth default, not necessarily a working authenticated OTLP pipe).
   - Recommendation: Add the wiring (Pattern 3 mirror, ~5 small diffs) — it's small, consistent with the existing `OTLPEndpoint` precedent, and makes the "durable" recipe genuinely functional rather than aspirational. If the planner descopes it instead, the doc must say so explicitly (mirroring D-07's Postgres honesty framing), not silently omit the header-wiring step.

2. **RESOLVED (namespace `phoenix`, `persistence.size=2Gi`, retention 7 days — Plan 47-03).** Which storage-mode namespace name and PVC size does the planner lock in?
   - What we know: Claude's Discretion per CONTEXT.md; `persistence.size=2Gi`/retention `7` days are this research's grounded suggestion.
   - What's unclear: Exact fixture-scale span volume from a real `examples/projects/medium` run (dozens to low-hundreds of spans across 5 levels + LLM message spans for 3-5 Tasks) — not measured in this research pass since it requires a live dispatch.
   - Recommendation: Treat the suggested numbers as a starting point; the pre-flight no-spend connectivity check (D-12) is a good moment to sanity-check PVC headroom before the real spend begins.

## Environment Availability

| Dependency | Required By | Available | Version | Fallback |
|------------|--------------|-----------|---------|----------|
| `helm` | Phoenix chart install, TIDE chart upgrades | ✓ | v3.16.3+gcfd0749 | — |
| `kind` | Fresh proof cluster (D-11) | ✓ | v0.31.0 | — |
| `docker` | kind cluster runtime | ✓ | server 29.5.2 | — |
| `kubectl` | All cluster operations | ✓ | reachable via `kind-tide-dogfood` context at research time | — |
| `go` | Any code-side wiring (Pitfall B remediation) | ✓ | go1.26.3 darwin/amd64 | — |
| `~/.tide/anthropic.key` | Real API spend for D-12's driving run | ✓ | 109 bytes, present outside repo | — |
| `kind-tide-dogfood` cluster | Reuse candidate for the live proof | ✓ (reachable) but **stale** — CRDs at v1alpha1/v1alpha2, chart `tide-1.0.5` installed 2026-06-27 | n/a | Per D-11: do not reuse; delete/recreate or stand up a new throwaway cluster name (Phase 26 precedent: `tide-spec-shot`-style purpose-named throwaway cluster, not `tide-test` which is the CI harness's reserved name) |
| `cert-manager` | Prerequisite for `tide` chart install (webhook certs) | ✓ in `kind-tide-dogfood` (v1.20.2, per docs/INSTALL.md's pin) — will need reinstalling on any fresh cluster | v1.20.2 | Reinstall per `docs/INSTALL.md`'s documented `cert-manager.yaml` apply + rollout-status wait |

**Missing dependencies with no fallback:** none — every tool this phase needs is present on this machine.

**Missing dependencies with fallback:** `kind-tide-dogfood`'s currency (needs delete/recreate per D-11; documented fallback is the standard docs/INSTALL.md bootstrap sequence, already proven).

## Validation Architecture

### Test Framework
| Property | Value |
|----------|-------|
| Framework | Go `testing` (plain `Test*` funcs) for fast/offline chart-render assertions; Ginkgo v2.28 + Gomega for the Layer B `test/integration/kind` suite (live cluster) |
| Config file | `test/integration/kind/suite_test.go` (Layer B cluster bootstrap, `kindClusterName = "tide-test"`) |
| Quick run command | `go test ./test/integration/kind/... -run TestHelm -v` (offline-safe subset only) |
| Full suite command | `make test-int` (Layer A envtest + Layer B kind, requires Docker + kind) |

### Phase Requirements → Test Map
| Req ID | Behavior | Test Type | Automated Command | File Exists? |
|--------|----------|-----------|---------------------|--------------|
| PHX-02 (NOTES.txt nudge present when endpoint empty) | render-gate | integration (live cluster required — see Pitfall A) | `go test ./test/integration/kind/... -run TestHelmNotesTxt -v` (name TBD by planner) | ❌ Wave 0 — new file/test needed |
| PHX-02 (NOTES.txt nudge absent when endpoint set) | render-gate | integration (live cluster required) | same test, second case | ❌ Wave 0 |
| PHX-02 (headers wiring, if added per Pitfall B option 1) | unit + integration | `go test ./internal/controller/... -run TestReporterOptions` + a `TestHelmDeploymentTemplate...Headers` render check | ❌ Wave 0 — new field, new tests |
| PHX-01 (chart pin present, no `latest`) | doc-review, not automatable | manual grep of doc examples for `--version 10.0.0` | n/a | n/a |
| PROOF-01 (five-level trace tree, redacted messages, queryable) | live/manual | browser-driven screenshot capture, not automatable | n/a | n/a |

### Sampling Rate
- **Per task commit:** `go test ./test/integration/kind/... -run TestHelm -v` (fast, offline-safe render checks only — see Pitfall A for why the NOTES.txt case can't be included here if it needs the live-render strategy).
- **Per wave merge:** `make test-int` (full Layer A + Layer B).
- **Phase gate:** Full suite green before `/gsd:verify-work`, plus the live proof's manual evidence checklist (D-13) — this phase's acceptance bar is not test-green alone.

### Wave 0 Gaps
- [ ] NOTES.txt render-gate test (both cases) — location and strategy (raw-byte vs live-render) is an explicit planner decision per Pitfall A, not a given.
- [ ] If Pitfall B option 1 (add headers wiring) is chosen: new `ReporterOptions` field test + a chart-render test confirming the `Secret`-sourced env entry appears only when the new values key is set.
- [ ] No test framework gaps otherwise — `test/integration/kind` and Layer A envtest are both already fully wired and green per STATE.md/CLAUDE.md.

## Security Domain

### Applicable ASVS Categories

| ASVS Category | Applies | Standard Control |
|----------------|---------|--------------------|
| V2 Authentication | yes | Phoenix's own `auth.enableAuth`/`PHOENIX_SECRET`/admin-password mechanism (chart-native, not TIDE-authored) — this phase's job is to document keeping it ON and sourcing credentials from a Secret, never a literal doc value |
| V6 Cryptography / Secrets Management | yes | Never a literal `value:` in a chart example — mirrors the existing `providerSecretRef`/`GIT_PAT` pattern already enforced elsewhere in this codebase; any new `OTEL_EXPORTER_OTLP_HEADERS` wiring (if added) must use `valueFrom: secretKeyRef`, never inline |
| V4 Access Control | no | Phoenix's RBAC/roles (ADMIN/MEMBER/VIEWER) are out of this phase's scope — no TIDE-side access-control surface changes |
| V5 Input Validation | no | No new TIDE-authored input-handling code in this phase |

### Known Threat Patterns for this stack

| Pattern | STRIDE | Standard Mitigation |
|---------|--------|-----------------------|
| Unauthenticated Phoenix instance holding full (redacted-but-still-substantial) prompt/completion history reachable beyond loopback | Information Disclosure | `auth.enableAuth=true` (chart default, keep it) + `service.type=ClusterIP` + `kubectl port-forward` for the documented recipe, never NodePort/LoadBalancer without auth |
| Weak default credentials (`auth.defaultAdminPassword: "admin"`, `PHOENIX_POSTGRES_PASSWORD: "postgres"`) shipped verbatim in a copy-pasted doc example | Elevation of Privilege | Explicitly call out both in the doc as values to override, not accept — confirmed live in the chart's `values.yaml` |
| OTLP auth token (if wired) leaking via `kubectl get pod -o yaml` | Information Disclosure | `valueFrom: secretKeyRef`, never a literal env value — same pattern this codebase already enforces for `ANTHROPIC_API_KEY`/`GIT_PAT` |

## Sources

### Primary (HIGH confidence)
- `oci://registry-1.docker.io/arizephoenix/phoenix-helm` — live `helm show chart` + `helm pull --version 10.0.0 --untar`, fetched 2026-07-17, digest `sha256:fa3beb12b4f2373741c4872d866a6b1a0aee1220b0d1c4b7f2e4ce07970ead33`
- `raw.githubusercontent.com/Arize-ai/phoenix/main/helm/Chart.yaml` — independent live WebFetch, fetched 2026-07-17, values agree with the OCI pull
- Local Phoenix chart pull's `values.yaml`, `templates/_helpers.tpl`, `templates/phoenix/{pvc,secret,configmap,deployment}.yaml`, `templates/NOTES.txt` — read directly
- `go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc@v1.43.0/doc.go` (local module cache, pinned version) — `OTEL_EXPORTER_OTLP_HEADERS` env-var behavior
- Direct TIDE repo reads: `charts/tide/values.yaml`, `charts/tide/templates/{deployment,dashboard-deployment,NOTES}.{yaml,txt}`, `hack/helm/augment-tide-chart.sh`, `internal/otelinit/provider.go`, `internal/controller/reporter_jobspec.go`, `internal/controller/{milestone,phase,plan,project,task}_controller.go`, `cmd/manager/main.go`, `docs/{observability,INSTALL,live-e2e}.md`, `examples/projects/{README,medium/{README,project.yaml}}`, `test/integration/kind/{projects_pvc_test.go,suite_test.go}`
- Live commands run against this machine: `helm version --short` (v3.16.3), `kind version` (v0.31.0), `docker version`, `go version`, `kubectl --context kind-tide-dogfood get crd .../projects.tideproject.k8s`, `helm list -A`, `docker stats --no-stream`, empirical `helm template`/`helm install --dry-run`/`helm upgrade --dry-run` behavior probes (with and without a valid `KUBECONFIG`)

### Secondary (MEDIUM confidence)
- [Phoenix — Authentication](https://arize.com/docs/phoenix/self-hosting/features/authentication) (WebSearch summary, official docs domain, not independently re-fetched via WebFetch this session) — `PHOENIX_ADMIN_SECRET`-as-bearer-token API-key-minting mechanics, `Authorization: Bearer <token>` header format

### Tertiary (LOW confidence)
- None flagged.

## Metadata

**Confidence breakdown:**
- Standard stack (chart pin, values, storage/auth defaults): HIGH — two independent live fetches, direct file reads of the pulled chart
- Architecture (doc placement, wiring chain, NOTES.txt mechanics): HIGH — every claim traced to an exact file/line in this repo
- Pitfalls A/B (NOTES.txt render-gate gap, OTLP-headers wiring gap): HIGH — both empirically reproduced/grepped, not inferred
- PROOF-01 driving-run sizing (`examples/projects/medium`): HIGH for cost/shape facts (read directly from the sample's own README + project.yaml); MEDIUM for exact span-count-at-that-scale (not measured live in this research pass — would require an actual dispatch)

**Research date:** 2026-07-17
**Valid until:** ~3-7 days for the Phoenix chart pin specifically (near-daily release cadence per STACK.md's own observation, even though this fetch found it stable for 2 days) — re-verify the pin again immediately before actually authoring INSTALL.md if more than a few days elapse between this research and phase execution. Everything else (TIDE-side wiring facts, chart mechanics) is stable for the life of this phase.
