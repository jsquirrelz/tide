// Package metriccardinality implements a go/analysis Analyzer that rejects
// the literal string "task" as a label name in any of the four cardinality-
// generating Prometheus *Vec constructors:
//
//   - prometheus.NewCounterVec
//   - prometheus.NewHistogramVec
//   - prometheus.NewGaugeVec
//   - prometheus.NewSummaryVec
//
// OBS-02 / Pitfall 17 / D-X4 prevention. The TIDE orchestrator runs hundreds
// to thousands of K8s Tasks per phase; adding "task" to a Prometheus label
// slice would multiply every series by the active Task count, producing
// unbounded cardinality growth (a known Prometheus operational hazard). The
// approved bounded-cardinality label set is {project, phase, plan} plus the
// optional dimensions {reason, outcome, vendor, level}. "task" is never
// permitted as a label; per-task observability flows through OTel traces
// (one span per Task) instead.
//
// The analyzer is registered into the cmd/tide-lint multichecker so any PR
// that introduces a "task" label literal anywhere in the module fails CI at
// `make tide-lint`. The companion fixtures under testdata/src/{badlabels,
// goodlabels}/ assert positive and negative cases per analysistest's
// GOPATH-style resolver convention.
package metriccardinality
