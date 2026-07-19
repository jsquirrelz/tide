// Synthetic registry for the analysistest "goodlabels" fixture. The
// metriccardinality analyzer must remain SILENT on this file — every
// prometheus.New*Vec call uses a clean label set (no forbidden D-06
// literal), and the non-Vec constructor (NewCounter) is out-of-scope by
// design.
package goodlabels

import (
	"github.com/prometheus/client_golang/prometheus"
)

var (
	// All four *Vec constructors with cardinality-safe label slices.
	OkCounter = prometheus.NewCounterVec(
		prometheus.CounterOpts{Name: "ok_counter", Help: "clean"},
		[]string{"project", "phase", "plan"},
	)

	OkHistogram = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{Name: "ok_histogram", Help: "clean", Buckets: []float64{0.1, 0.5, 1, 5}},
		[]string{"level"},
	)

	OkGauge = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{Name: "ok_gauge", Help: "clean"},
		[]string{"project"},
	)

	OkSummary = prometheus.NewSummaryVec(
		prometheus.SummaryOpts{Name: "ok_summary", Help: "clean"},
		[]string{"project", "vendor"},
	)

	// NewCounter (singular) has no label-slice argument — the analyzer must
	// not even inspect it, much less fire. Including "task" here would be a
	// false-positive trap.
	OkSingletonCounter = prometheus.NewCounter(
		prometheus.CounterOpts{Name: "ok_singleton", Help: "no labels"},
	)

	// Positive controls (OBS-02 / D-06 / T-50-07): bounded enum label names
	// the guard explicitly blesses as safe — a closed, small-cardinality
	// value set, not an agent-controlled or per-attempt string. The analyzer
	// must stay silent on all of these; a diagnostic-expectation marker here
	// would fail the test the same way a missed violation would.
	OkLoopMetric = prometheus.NewCounterVec(
		prometheus.CounterOpts{Name: "ok_loop_metric", Help: "clean bounded enum labels"},
		[]string{"terminal_reason", "exit_reason", "loop_kind", "evaluator_type", "risk_tier"},
	)
)
