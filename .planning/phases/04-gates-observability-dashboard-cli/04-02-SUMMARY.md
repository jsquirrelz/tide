---
plan: 04-02
phase: 04
status: complete
executed_inline: true
recovery_path: cherry-pick-RED-from-stalled-worktree
---

# Plan 04-02 Summary — metriccardinality lint analyzer

## What shipped

A new `go/analysis` Analyzer at `tools/analyzers/metriccardinality/` that
rejects the literal string `"task"` appearing as a label name in any of the
four cardinality-generating Prometheus `*Vec` constructors:

- `prometheus.NewCounterVec`
- `prometheus.NewHistogramVec`
- `prometheus.NewGaugeVec`
- `prometheus.NewSummaryVec`

The analyzer is registered into `cmd/tide-lint`'s `multichecker.Main(...)`
varargs alongside the existing `crosspool` (POOL-03 / Pitfall 6) and
`providerfirewall` (SUB-05 / Pitfall 14) analyzers — three analyzers, one
binary, one `make tide-lint` CI gate.

## Why

`OBS-02 / Pitfall 17 / D-X4` — compile-time guard backing the runtime
cardinality discipline of plan 04-01's `internal/metrics/registry.go`. The
TIDE orchestrator runs hundreds to thousands of K8s Tasks per phase; a
`"task"` Prometheus label would multiply every series by the active Task
count, producing unbounded cardinality growth (a known Prometheus
operational hazard). The approved bounded-cardinality label set is
`{project, phase, plan}` plus optional `{reason, outcome, vendor, level}`.
Per-Task observability flows through OTel spans (one per Task) instead.

## Match logic

`run(pass *analysis.Pass)` walks every `*ast.CallExpr` via `ast.Inspect`:

1. `call.Fun` must be `*ast.SelectorExpr` with `X = *ast.Ident{Name:"prometheus"}`
2. `Sel.Name` must be one of the four `New*Vec` constructors (gated by a
   `vecConstructors` map so the singular forms — `NewCounter`, `NewHistogram`,
   etc. — are out of scope by construction).
3. For each `Arg` that is a `*ast.CompositeLit` whose Type is `[]string`
   (via the `isStringSliceType` helper that walks `*ast.ArrayType` →
   `*ast.Ident{Name:"string"}`):
4. For each element that is a `*ast.BasicLit` of kind `token.STRING`,
   `strconv.Unquote` the value and compare against `"task"`.
5. On match: `pass.Reportf(bl.Pos(), ...)` with the diagnostic positioned at
   the offending string literal so `analysistest // want` directives sit on
   the label-slice line, not the call expression.

## Testdata layout

`tools/analyzers/metriccardinality/testdata/src/`:

- `badlabels/registry.go` — 4 violations, one per `*Vec` constructor, each
  carrying a `// want "metriccardinality: ..."` directive on the label-slice
  line.
- `goodlabels/registry.go` — 4 clean `*Vec` calls + 1 `NewCounter` singular
  (must be ignored — no label slice arg).
- `github.com/prometheus/client_golang/prometheus/prometheus.go` — minimal
  stub package so the fixtures type-check under `analysistest`'s GOPATH-style
  loader without pulling the real SDK.

## Multichecker insertion

`cmd/tide-lint/main.go` change is a 1-line import addition + a 3-line
reformat of the `multichecker.Main(...)` call to one-arg-per-line, so the
next analyzer addition (e.g. Phase 5's hypothetical `cardinality-budget`)
is a one-line diff.

## Verifications run

- `go test ./tools/analyzers/metriccardinality/... -race -v` — 1 test
  (`TestMetricCardinality`), all 4 + 4 + 1 sub-assertions pass via
  `analysistest.Run` on both fixture subdirs.
- `go build ./cmd/tide-lint/...` — succeeds.
- `make tide-lint` against current tree (Phase 1 + 2 + 3 + plan 04-01's
  `internal/metrics/registry.go`) — exit 0, zero diagnostics.
- `grep -c "metriccardinality.Analyzer" cmd/tide-lint/main.go` — returns 2
  (import + usage).
- Scripted negative test — injecting `"task"` into a real
  `internal/metrics/registry.go` `[]string{"level"}` slice yielded:
  ```
  internal/metrics/registry.go:125:21: metriccardinality: "task" label forbidden
    in prometheus.NewHistogramVec(...) — adds unbounded task-axis cardinality
    (Pitfall 17 / D-X4)
  ```
  exit 1. Restored → exit 0.

## Execution path

This plan was executed inline by the orchestrator after two consecutive
parallel-executor stalls (#2410 — `Stream idle timeout` on Opus 4.7 during
extended analyzer-authorship thinking blocks). The RED-phase commit (test
fixtures + `analysistest.Run` harness + Prometheus stub package) was
preserved on `worktree-agent-a33fcf537b47a9cde` and cherry-picked into main
as `168c1bd`. Inline commits:

- `168c1bd test(04-02): add failing tests + fixtures (RED, cherry-picked)`
- `447d4c3 test(04-02): fix \`// want\`-as-prose trip in badlabels header
  comment` (recovery: rephrased two header-comment lines that contained the
  substring `// want` as prose — `analysistest`'s scanner was treating them
  as malformed expectation directives, which was the symptom that stalled
  both prior executors)
- `<this commit's parent> feat(04-02): metriccardinality analyzer GREEN`
- `<this commit's parent> feat(04-02): wire metriccardinality into
  multichecker + Makefile help`

## Key links

- `cmd/tide-lint/main.go` → `tools/analyzers/metriccardinality` — via
  `multichecker.Main(... metriccardinality.Analyzer)`.
- `Makefile` → `cmd/tide-lint` — via `tide-lint:` target running
  `go run ./cmd/tide-lint ./...`.

## What this enables downstream

Subsequent plans that wire new metrics (any `.Inc()` or `.Observe()` site
in the up-stack reconcilers 04-05, the boundary push hooks 04-06, or the
dashboard backend 04-10/04-11) get the cardinality guarantee for free —
the analyzer enforces it across every package, including new ones.
