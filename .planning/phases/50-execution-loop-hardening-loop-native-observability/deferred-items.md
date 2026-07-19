# Deferred Items — Phase 50

Out-of-scope discoveries surfaced during plan execution but not fixed
(scope boundary: only auto-fix issues directly caused by the current
task's changes).

## 50-03: pre-existing `lll` (line-too-long) violations block `make lint`

**Found during:** 50-03 Task 2 verification (`make lint`).

**Issue:** `make lint` fails with 2 `lll` (line-length > 120) violations:

- `pkg/dispatch/terminal_reason.go:63` — introduced in commit `dd916dad8`
  (Plan 50-01, `feat(50-01)`), 2026-07-18.
- `pkg/otelai/attrs.go:441` — introduced in commit `551f23ab5`
  (Plan 50-02, `feat(50-02): loop.*/evaluation.*/human_intervention consts
  + helper functions`), 2026-07-19.

Both predate 50-03 and are unrelated to this plan's file set
(`tools/analyzers/metriccardinality/`, `internal/metrics/wave_label_test.go`).
`go vet ./...` is clean; `go test ./tools/analyzers/metriccardinality/...
./internal/metrics/...` is green; the `tide-lint` multichecker (which
includes the extended `metriccardinality.Analyzer`) reports zero
diagnostics against the whole tree — the `lll` failures are golangci-lint's
own `lll` linter, not the OBS-02/D-06 cardinality guard. The extended
forbidden-label set does not trip on any existing code.

**Not fixed here** — out of scope for 50-03 (guard-hardening only, per the
plan's `files_modified` list). Recommend wrapping into the next plan that
touches either file, or a small standalone cleanup pass.
