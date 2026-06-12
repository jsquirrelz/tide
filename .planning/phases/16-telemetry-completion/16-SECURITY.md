---
phase: 16
slug: telemetry-completion
status: verified
threats_open: 0
asvs_level: 1
created: 2026-06-12
---

# Phase 16 — Security

> Per-phase security contract: threat register, accepted risks, and audit trail.

---

## Trust Boundaries

| Boundary | Description | Data Crossing |
|----------|-------------|---------------|
| Dashboard → Prometheus | Server-side PromQL proxy forwards instant/range queries to the operator-configured endpoint | PromQL queries + metric series (read-only) |
| Browser → Dashboard API | TelemetryView fetches `/api/v1/query_range` same-origin | PromQL responses rendered as charts |
| Controller → Prometheus registry | TaskReconciler/PlanReconciler emit counters/histograms | CR names + bounded reason enums as label values |
| CI → Helm chart | helm-lint job runs render gates over repo-tracked scripts | Rendered manifests (no cluster credentials) |

---

## Threat Register

| Threat ID | Category | Component | Disposition | Mitigation | Status |
|-----------|----------|-----------|-------------|------------|--------|
| T-16-01 | Tampering/SSRF | PromQL proxy target | accept | Endpoint enters only via `os.Getenv("PROM_ENDPOINT")` (main.go:156 → router.go:80,163); fixed `/api/v1/query{,_range}` path suffixes; no request-derived override | closed |
| T-16-02 | DoS | Proxy upstream call | mitigate | 30s bounded `http.Client` (prometheus.go:43,49) + `NewRequestWithContext(r.Context())` (:99) | closed |
| T-16-03 | Info Disclosure | 502 error body | accept | Errors expose only the operator's own endpoint reachability; surface is GET-only (DASH-05) | closed |
| T-16-04 | Tampering | PromQL query passthrough | accept | RawQuery forwarded only to fixed read-only Prometheus query paths via GET | closed |
| T-16-05 | Info Disclosure | Metric label values | mitigate | Labels = CR names (`project.Name`, PlanRef, PhaseRef, owner-ref Wave name) with `"unknown"` sentinels (task_controller.go:1006-1085); never envelope free-text or secrets | closed |
| T-16-06 | DoS | Series cardinality | mitigate | Locked label sets, no `task`/`model` labels (registry.go:131-227); `metriccardinality` analyzer CI-gated (tide-lint → ci.yaml:80) | closed |
| T-16-07 | Tampering | Envelope usage values | accept | `emitTaskMetrics` adjacent to existing `budget.RollUpUsage` calls — identical values, no new trust exposure (D-12) | closed |
| T-16-08 | Tampering | CI executes hack/helm scripts | accept | `make helm-assert` (ci.yaml:195) runs repo-tracked scripts; same job, no new permissions | closed |
| T-16-09 | EoP | helm-lint job scope | accept | Pure `helm template`/`helm lint`; zero kubeconfig/cluster-credential references | closed |
| T-16-10 | XSS | Chart rendering | mitigate | Zero `dangerouslySetInnerHTML`; repo-wide gate `no-dangerous-html.test.ts:29-39` | closed |
| T-16-11 | Tampering | DASH-05 read-only | mitigate | TelemetryView has exactly one fetch — GET `/api/v1/query_range` (TelemetryView.tsx:255); proxy routes `r.Get` only (router.go:179-180) | closed |
| T-16-12 | Info Disclosure | localStorage scope/view | mitigate | Zero `localStorage` in TelemetryView; view state is transient `useState` (App.tsx:183) | closed |
| T-16-13 | Tampering | View switcher DASH-05 | mitigate | ViewSwitcher writes React state only — no fetch, no localStorage (App.tsx:99-183) | closed |
| T-16-14 | DoS | Double-mounting views | mitigate | Conditional render (single body tree, App.tsx:291-296); polling `clearInterval` cleanup (TelemetryView.tsx:829-837) | closed |
| T-16-20 | XSS | Legend keys from Prometheus labels | mitigate | Label values render via JSX text / recharts props only; covered by the repo-wide dangerous-HTML gate | closed |
| T-16-21 | DoS (cardinality) | TasksFailedTotal `reason` label | mitigate | `metricFailureReason` closed switch returns only `{budget, internal, exit-1}` (task_controller.go:1028-1042); envelope reason text explicitly not reused | closed |
| T-16-22 | Tampering | TaskDurationSeconds corruption | mitigate | `d >= 0` guard with V(1) log for stale-envelope/clock-skew (task_controller.go:1091-1101) | closed |
| T-16-23 | Repudiation | WavesDispatchedTotal accuracy | mitigate | `Inc()` only in the Create-success branch (plan_controller.go:1347-1351); AlreadyExists/replay excluded; pinned by plan_controller_metrics_test.go | closed |
| T-16-24 | Misleading operator surface | Dead `prometheusEndpoint` YAML key | mitigate | Config surface excised (zero `prometheus*` refs in internal/config); docs aligned to env-only mechanism | closed |
| T-16-25 | Chart drift | dashboard-deployment.yaml | mitigate | Comment-only edit; byte-identical render gated by chart-reproducibility diff + `make helm-assert` in CI | closed |
| T-16-SC (×8) | Supply chain | Per-plan package surface | mitigate/accept | Sole new dependency `recharts@3.8.1` exact-pinned + legitimacy-audited (16-RESEARCH.md); all other plans added zero dependencies | closed |

*Status: open · closed*
*Disposition: mitigate (implementation required) · accept (documented risk) · transfer (third-party)*

Note: T-16-15..19 were never assigned in the plans' registers — the register is T-16-01..14 + T-16-20..25 + per-plan T-16-SC.

---

## Accepted Risks Log

| Risk ID | Threat Ref | Rationale | Accepted By | Date |
|---------|------------|-----------|-------------|------|
| R-16-01 | T-16-01/03/04 | Proxy endpoint is operator-set deployment config (env via helm value); SSRF requires cluster-config access, at which point the attacker already controls the deployment | plan-time disposition (16-01) | 2026-06-12 |
| R-16-02 | T-16-07 | Metric values come from the same envelope the budget accountant already trusts — no new trust boundary | plan-time disposition (16-02) | 2026-06-12 |
| R-16-03 | T-16-08/09 | CI render gates execute repo-tracked scripts in the existing credential-free helm-lint job | plan-time disposition (16-03) | 2026-06-12 |

---

## Security Audit Trail

| Audit Date | Threats Total | Closed | Open | Run By |
|------------|---------------|--------|------|--------|
| 2026-06-12 | 28 (20 numbered + 8 T-16-SC) | 28 | 0 | gsd-security-auditor (verify-mitigations mode, register authored at plan time) |

Non-blocking observation: `MILESTONE.md:23,148` still mentions "prometheusEndpoint" descriptively — cosmetic residue in an internal planning artifact; the config key no longer exists in code.

---

## Sign-Off

- [x] All threats have a disposition (mitigate / accept / transfer)
- [x] Accepted risks documented in Accepted Risks Log
- [x] `threats_open: 0` confirmed
- [x] `status: verified` set in frontmatter

**Approval:** verified 2026-06-12
