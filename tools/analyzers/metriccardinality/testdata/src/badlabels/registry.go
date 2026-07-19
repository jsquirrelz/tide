// Synthetic registry for the analysistest "badlabels" fixture. The
// metriccardinality analyzer must flag every prometheus.New*Vec call whose
// label slice contains one of the forbidden D-06 label names — "task" plus
// the eight run-ID-shaped names (run_id, loop_run_id, run, attempt,
// attempt_id, trace_id, task_uid, uid) — each carrying a
// diagnostic-expectation marker on the offending label line, spread across
// all four constructor kinds so the set-membership check is proven per
// constructor.
//
// The analyzer reports the diagnostic at the position of the offending
// literal itself (not the call expression), so the expectation marker sits
// on the line that contains the forbidden label.
package badlabels

import (
	"github.com/prometheus/client_golang/prometheus"
)

var (
	BadCounter = prometheus.NewCounterVec(
		prometheus.CounterOpts{Name: "bad_counter", Help: "violates D-X4"},
		[]string{"project", "task"}, // want `metriccardinality: "task" label forbidden in prometheus.NewCounterVec.*`
	)

	BadHistogram = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{Name: "bad_histogram", Help: "violates D-X4"},
		[]string{"project", "task"}, // want `metriccardinality: "task" label forbidden in prometheus.NewHistogramVec.*`
	)

	BadGauge = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{Name: "bad_gauge", Help: "violates D-X4"},
		[]string{"task"}, // want `metriccardinality: "task" label forbidden in prometheus.NewGaugeVec.*`
	)

	BadSummary = prometheus.NewSummaryVec(
		prometheus.SummaryOpts{Name: "bad_summary", Help: "violates D-X4"},
		[]string{"phase", "plan", "task"}, // want `metriccardinality: "task" label forbidden in prometheus.NewSummaryVec.*`
	)

	// The eight run-ID-shaped forbidden labels (D-06 / OBS-02), spread
	// two-per-constructor so every *Vec kind proves the set-membership
	// check, not just the "task" literal above.

	BadCounterRunID = prometheus.NewCounterVec(
		prometheus.CounterOpts{Name: "bad_counter_run_id", Help: "violates D-06"},
		[]string{"project", "run_id"}, // want `metriccardinality: "run_id" label forbidden in prometheus.NewCounterVec.*`
	)

	BadCounterAttemptID = prometheus.NewCounterVec(
		prometheus.CounterOpts{Name: "bad_counter_attempt_id", Help: "violates D-06"},
		[]string{"attempt_id"}, // want `metriccardinality: "attempt_id" label forbidden in prometheus.NewCounterVec.*`
	)

	BadHistogramLoopRunID = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{Name: "bad_histogram_loop_run_id", Help: "violates D-06"},
		[]string{"project", "loop_run_id"}, // want `metriccardinality: "loop_run_id" label forbidden in prometheus.NewHistogramVec.*`
	)

	BadHistogramTraceID = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{Name: "bad_histogram_trace_id", Help: "violates D-06"},
		[]string{"trace_id"}, // want `metriccardinality: "trace_id" label forbidden in prometheus.NewHistogramVec.*`
	)

	BadGaugeRun = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{Name: "bad_gauge_run", Help: "violates D-06"},
		[]string{"run"}, // want `metriccardinality: "run" label forbidden in prometheus.NewGaugeVec.*`
	)

	BadGaugeTaskUID = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{Name: "bad_gauge_task_uid", Help: "violates D-06"},
		[]string{"project", "task_uid"}, // want `metriccardinality: "task_uid" label forbidden in prometheus.NewGaugeVec.*`
	)

	BadSummaryAttempt = prometheus.NewSummaryVec(
		prometheus.SummaryOpts{Name: "bad_summary_attempt", Help: "violates D-06"},
		[]string{"attempt"}, // want `metriccardinality: "attempt" label forbidden in prometheus.NewSummaryVec.*`
	)

	BadSummaryUID = prometheus.NewSummaryVec(
		prometheus.SummaryOpts{Name: "bad_summary_uid", Help: "violates D-06"},
		[]string{"project", "uid"}, // want `metriccardinality: "uid" label forbidden in prometheus.NewSummaryVec.*`
	)
)
