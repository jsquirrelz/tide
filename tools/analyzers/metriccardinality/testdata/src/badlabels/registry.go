// Synthetic registry for the analysistest "badlabels" fixture. The
// metriccardinality analyzer must flag every prometheus.New*Vec call whose
// label slice contains the literal "task" — one violation per constructor
// (CounterVec / HistogramVec / GaugeVec / SummaryVec), each carrying a
// // want directive on the offending label line.
//
// The analyzer reports the diagnostic at the position of the "task" literal
// itself (not the call expression), so the // want directive sits on the
// line that contains "task".
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
)
