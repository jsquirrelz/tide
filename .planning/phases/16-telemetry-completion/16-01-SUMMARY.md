---
phase: 16-telemetry-completion
plan: "01"
subsystem: dashboard-proxy
tags: [telem-01, telem-06, prometheus, proxy, config]
dependency_graph:
  requires: []
  provides: [PROM_ENDPOINT env wired to proxy, bounded proxy client, base-path preservation]
  affects: [cmd/dashboard, internal/config]
tech_stack:
  added: []
  patterns: [package-level bounded HTTP client, context propagation via NewRequestWithContext, rawConfig *string nil-distinct pattern]
key_files:
  created: []
  modified:
    - cmd/dashboard/api/prometheus.go
    - cmd/dashboard/api/telemetry_proxy_integration_test.go
    - internal/config/config.go
    - internal/config/config_test.go
    - cmd/dashboard/main.go
    - cmd/dashboard/router_test.go
decisions:
  - "Use strings.TrimRight + path append (not url.JoinPath) for base-path preservation per RESEARCH Don't-Hand-Roll table"
  - "PROM_ENDPOINT env override in config.Load() applied after YAML parse, after applyAndValidate returns (empty is valid, no >= 1 constraint)"
  - "pre-existing go build ./... failure in cmd/tide-demo-init (embed pattern error) is out of scope; modified packages build cleanly"
metrics:
  duration: "6 minutes"
  completed: "2026-06-12"
  tasks_completed: 2
  files_modified: 6
---

# Phase 16 Plan 01: PromQL Proxy Hardening + PROM_ENDPOINT Wiring Summary

Hardened the PromQL proxy with a 30s-bounded package client, request-context propagation, and base-path preservation (TELEM-06); wired `PROM_ENDPOINT` env into both the dashboard binary's `Dependencies.PrometheusEndpoint` and the manager binary's `Config.PrometheusEndpoint` field (TELEM-01).

## Tasks Completed

| Task | Name | Commit | Files |
|------|------|--------|-------|
| 1 | Harden PromQL proxy (TELEM-06) | b2834b9 | prometheus.go, telemetry_proxy_integration_test.go |
| 2 | Wire PROM_ENDPOINT (TELEM-01) | 972e486 | config.go, config_test.go, main.go, router_test.go |

## What Was Built

### Task 1 — TELEM-06: Proxy Hardening

Three mechanical changes to `cmd/dashboard/api/prometheus.go`:

1. Added package-level `const proxyTimeout = 30 * time.Second` and `var proxyClient = &http.Client{Timeout: proxyTimeout}` — upstream Prometheus hangs are bounded.
2. Replaced `upstream.Path = path` with `upstream.Path = strings.TrimRight(upstream.Path, "/") + path` — operator-configured base paths (e.g. `http://prom:9090/prometheus`) survive the URL join.
3. Replaced `http.DefaultClient.Get(upstream.String()) //nolint:noctx` with `http.NewRequestWithContext(r.Context(), ...) + proxyClient.Do(req)` — browser disconnects cancel the upstream call; the `//nolint:noctx` annotation is removed.

Three regression tests added to `telemetry_proxy_integration_test.go`:
- `TestPrometheusProxyBasePath` — upstream handler asserts it received `/prometheus/api/v1/query_range` when endpoint has a `/prometheus` base path.
- `TestPrometheusProxyClientBounds` — source-level assertion (`os.ReadFile("prometheus.go")`) confirms `http.DefaultClient` is absent; direct struct assertion confirms `proxyClient.Timeout == 30s`.
- `TestPrometheusProxyContextCancellation` — pre-cancelled context against a 2s-sleep upstream returns promptly (< 1s), not after the sleep.

All three pre-existing degradation tests (`CONFIGURED+REACHABLE`, `UNCONFIGURED`, `CONFIGURED+UNREACHABLE`) pass unmodified.

### Task 2 — TELEM-01: PROM_ENDPOINT Wiring

Two seams across two binaries:

1. `internal/config/config.go` (manager binary): added `PrometheusEndpoint string` with `yaml:"prometheusEndpoint"` to `Config`; added parallel `PrometheusEndpoint *string` pointer field to `rawConfig`; in `Load()` applies YAML value when non-nil then overrides with `os.Getenv("PROM_ENDPOINT")` when non-empty.
2. `cmd/dashboard/main.go` (dashboard binary — does not call config.Load): added `PrometheusEndpoint: os.Getenv("PROM_ENDPOINT"),` to the `RegisterRoutes(Dependencies{...})` literal.

Regression tests:
- Three new config cases in `internal/config/config_test.go`: YAML value set, env override wins, empty default when key absent.
- `TestPrometheusEndpointWiringThroughRegisterRoutes` in `cmd/dashboard/router_test.go`: builds a router with a test-double upstream as `PrometheusEndpoint`, GETs `/api/v1/query`, asserts the upstream was hit (proving the dependency flows through `RegisterRoutes` to `PrometheusHandler.Endpoint`).

## Verification Results

```
go test ./cmd/dashboard/api/... -count=1          → PASS (all 20 tests including 3 new)
go test ./internal/config/... -count=1            → PASS (all 9 tests including 3 new)
go test ./cmd/dashboard/... -count=1              → PASS (all tests including new wiring test)
grep -c "http.DefaultClient" prometheus.go         → 0
grep -c "NewRequestWithContext" prometheus.go      → 1
grep -cE 'TrimRight(upstream.Path, "/")' prometheus.go → 1
grep -c "nolint:noctx" prometheus.go               → 0
grep -c 'yaml:"prometheusEndpoint"' config.go      → 2 (Config + rawConfig)
grep -cE 'PrometheusEndpoint:\s*os\.Getenv\("PROM_ENDPOINT"\)' main.go → 1
git status --porcelain charts/                     → empty (zero chart edits)
```

## Deviations from Plan

### Out-of-Scope Pre-existing Issue

**Pre-existing `go build ./...` failure in `cmd/tide-demo-init`:**
- **Found during:** Task 2 verification
- **Issue:** `cmd/tide-demo-init/main.go:112:12: pattern all:fixture: no matching files found` — embed.FS pattern references missing `fixture/` directory
- **Action:** Confirmed pre-existing (same error before any changes in this plan). Out of scope per SCOPE BOUNDARY rule. Logged here for tracking.
- **Impact:** `go build ./...` fails on unrelated package. Modified packages (`./cmd/dashboard/...`, `./internal/config/...`) build cleanly.

## Known Stubs

None — all wiring is live end-to-end in the modified files.

## Threat Flags

No new threat surface introduced. The plan's STRIDE register covers all modified paths:
- T-16-02 (TELEM-06 DoS mitigation) is now implemented — bounded client + context propagation is live.
- T-16-01 (SSRF accept) is unchanged — endpoint is operator-set, not user-controlled at request time.

## Self-Check: PASSED

Files verified present:
- cmd/dashboard/api/prometheus.go — FOUND
- cmd/dashboard/api/telemetry_proxy_integration_test.go — FOUND
- internal/config/config.go — FOUND
- internal/config/config_test.go — FOUND
- cmd/dashboard/main.go — FOUND
- cmd/dashboard/router_test.go — FOUND

Commits verified:
- b2834b9 (Task 1) — FOUND
- 972e486 (Task 2) — FOUND
