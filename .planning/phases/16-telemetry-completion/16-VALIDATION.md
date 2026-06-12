---
phase: 16
slug: telemetry-completion
status: ready
nyquist_compliant: true
wave_0_complete: true
created: 2026-06-12
---

# Phase 16 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | go test (plain + fake client for controllers) + Vitest (dashboard/web) + helm render gates (hack/helm) |
| **Config file** | Makefile (Go tiers) / dashboard/web/vitest.config.ts |
| **Quick run command** | `go test ./internal/metrics/... ./cmd/dashboard/...` and `npm test --prefix dashboard/web` |
| **Full suite command** | `make test` + `npm test --prefix dashboard/web` + `make helm-assert` (new target this phase) |
| **Estimated runtime** | ~120 seconds |

---

## Sampling Rate

- **After every task commit:** Run the quick command for the touched surface (Go or Vitest)
- **After every plan wave:** Run `make test` + dashboard Vitest suite
- **Before `/gsd:verify-work`:** Full suite must be green
- **Max feedback latency:** 180 seconds

---

## Per-Task Verification Map

| Task ID | Plan | Wave | Requirement | Threat Ref | Secure Behavior | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-------------|------------|-----------------|-----------|-------------------|-------------|--------|
| 16-01-T1 | 16-01 | 1 | TELEM-06 | T-16-02 | Bounded 30s client + r.Context() propagation; base path preserved; no http.DefaultClient | go integration | `go test ./cmd/dashboard/api/... -count=1` | ✅ extend telemetry_proxy_integration_test.go | ⬜ pending |
| 16-01-T2 | 16-01 | 1 | TELEM-01 | T-16-01 | Endpoint is operator-set env/YAML only, never request-time user input | go unit | `go test ./internal/config/... ./cmd/dashboard/... -count=1` | ✅ extend config_test.go + router_test.go | ⬜ pending |
| 16-02-T1 | 16-02 | 1 | TELEM-03 | T-16-06 | Locked {project, phase, plan, wave} arity on all six; no `task` label (analyzer-gated) | go unit | `go test ./internal/metrics/... -count=1` | ✅ extend registry_test.go | ⬜ pending |
| 16-02-T2 | 16-02 | 1 | TELEM-03 | T-16-05 | Label values are CR names + "unknown" sentinel only — never envelope free-text | go unit (fake client) | `go test ./internal/controller/... -run 'TestResolveWave\|TestEmitTaskMetrics' -count=1` | ⬜ task_controller_metrics_test.go created in-task | ⬜ pending |
| 16-03-T1 | 16-03 | 1 | TELEM-05 | T-16-08 | Render gates pass; chart untouched (fixed contract) | shell | `make helm-telemetry-assert && make helm-assert` | ⬜ targets created in-task | ⬜ pending |
| 16-03-T2 | 16-03 | 1 | TELEM-05 | T-16-09 | helm-lint job stays cluster-credential-free | yaml/grep | `python3 -c "import yaml; yaml.safe_load(open('.github/workflows/ci.yaml'))"` + grep step placement | ✅ ci.yaml exists | ⬜ pending |
| 16-04-T1 | 16-04 | 1 | TELEM-04 | T-16-10 | Only registry.go metric names queried; values parseFloat'd, JSX-escaped | tsc + Vitest | `cd dashboard/web && npx tsc --noEmit && npm test` | ✅ suite exists | ⬜ pending |
| 16-04-T2 | 16-04 | 1 | TELEM-02, TELEM-04 | T-16-12 | Transient state only — zero localStorage; read-only GETs | tsc + Vitest | `cd dashboard/web && npx tsc --noEmit && npm test` | ✅ suite exists | ⬜ pending |
| 16-04-T3 | 16-04 | 1 | TELEM-02 | T-16-11 | Both locked degradation shapes pinned (200 sentinel + 502) | Vitest | `cd dashboard/web && npm test` | ⬜ __tests__/TelemetryView.test.tsx created in-task | ⬜ pending |
| 16-05-T1 | 16-05 | 2 | TELEM-02 | T-16-13 | Switcher mutates React state only; no new network surface | tsc + Vitest | `cd dashboard/web && npx tsc --noEmit && npm test` | ✅ suite exists | ⬜ pending |
| 16-05-T2 | 16-05 | 2 | TELEM-02 | T-16-14 | Single body tree mounted (conditional render, polling stops on unmount) | Vitest | `cd dashboard/web && npm test` | ⬜ __tests__/view-switcher.test.tsx created in-task | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

- [x] No standalone Wave 0 plan needed: every new test file is created inside the same task as its implementation (regression-test-per-fix milestone rule), and every pre-existing surface extends an existing test file (`telemetry_proxy_integration_test.go`, `config_test.go`, `router_test.go`, `registry_test.go`). No task ships without an `<automated>` verify.

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| Helm value → live endpoint change | TELEM-01 | End-to-end helm install observation | `helm upgrade --set prometheus.endpoint=... ; kubectl exec` curl the proxy and observe upstream change |

---

## Validation Sign-Off

- [x] All tasks have `<automated>` verify or Wave 0 dependencies
- [x] Sampling continuity: no 3 consecutive tasks without automated verify
- [x] Wave 0 covers all MISSING references (in-task creation)
- [x] No watch-mode flags (`npm test` runs `vitest run`)
- [x] Feedback latency < 180s
- [x] `nyquist_compliant: true` set in frontmatter

**Approval:** filled by gsd-planner 2026-06-12
