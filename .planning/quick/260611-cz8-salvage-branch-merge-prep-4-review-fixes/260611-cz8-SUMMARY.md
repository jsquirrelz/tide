---
quick_id: 260611-cz8
status: complete
date: 2026-06-11
merge_commit: 49e93cb
fix_commits: [883ff1d, 5a3fe67, 39053b7, 923357c]
branch: tide/run-dogfood-analytics-1781156464
---

# Quick Task 260611-cz8: Salvage Branch Merge Prep — Summary

Dogfood run 1's salvaged telemetry branch (2e28934, 16 files, +1,514) was
co-reviewed, fixed, and merged to main per the user's (b)-minimal decision.
MILESTONE.md stays at the repo root (user decision).

## Fixes applied on the branch

1. **883ff1d** — deleted both 0-byte salvage artifacts
   (`telemetry-degradation.test.tsx` failed vitest with exit 1;
   `verify-telemetry-invariants.sh` was dead weight).
2. **5a3fe67** — rewrote `telemetry_proxy_integration_test.go` against the
   production `PrometheusHandler` (the salvaged test asserted against its own
   private reimplementation, leaving the real handler with zero behavioral
   coverage); added a connection-refused subtest.
3. **39053b7** — `docs/observability.md`: fixed the 503 claim (actual contract
   is HTTP 200 + `{"status":"unavailable"}`) and marked the six token/cost
   metrics as planned-not-implemented (phase-01 never executed).
4. **923357c** — EC-7 render gate: converted the dashboard-deployment.yaml
   comment to a non-rendering `{{- /* */}}` template comment and tightened
   `assert-telemetry-render.sh` greps to the env-entry shape; gate now exits 0
   (was failing its own branch on a comment false-positive).

## Verification (echoed exits)

- Branch: `make test` MAKE_EXIT=0, no FAIL lines; `npm test` exit 0;
  `go test ./cmd/dashboard/...` exit 0; `go vet` exit 0;
  `assert-telemetry-render.sh` exit 0 (all 4 permutations);
  `assert-prometheus-env.py` PASS on both --expect-absent and --expect-endpoint.
- Merged main (49e93cb): `make test` MAKE_EXIT=0, no FAIL lines; `npm test` exit 0.
- Pushed: main 6768786..49e93cb; branch 2e28934..923357c.

## Excluded conflict branches — verdict: nothing to recover

- `wt-cba5c935`: same prometheus values block (different comment wording) +
  junk `do_commit.sh`; fully superseded.
- `wt-cc24ee71`: competing 95-line `assert-prometheus-env.py`; the salvaged
  127-line version is a strict superset.
- `wt-5efbf05a`, `wt-de741f7e`: zero commits (SIGKILLed before committing).

## Follow-ups filed (run-1 backlog, not done here)

- `PROM_ENDPOINT` dead config: helm injects it; no `main.go`/`internal/config`
  reader exists, so `Dependencies.PrometheusEndpoint` is never populated.
- `TelemetryView` is unmounted (no AppShell change) and untested; two of its
  four PromQL queries use metric names matching neither reality nor the
  MILESTONE.md locked table.
- phase-01 metric instrumentation (`internal/metrics`) never executed — the
  six token/cost metrics don't exist.
- hack/helm gate scripts unwired from Makefile; `PrometheusHandler` uses
  `http.DefaultClient` (no timeout, no ctx propagation); `upstream.Path = path`
  clobbers base paths in the endpoint URL.
