// Synthetic stub for github.com/prometheus/client_golang/prometheus used only
// by the metriccardinality analysistest fixtures. analysistest resolves
// imports via GOPATH-style lookups under testdata/src/, so badlabels/registry.go
// and goodlabels/registry.go resolve this import here.
//
// The analyzer only inspects the AST of NewCounterVec / NewHistogramVec /
// NewGaugeVec / NewSummaryVec call expressions for label-slice literals; the
// concrete return type and Opts shape don't need to match the real SDK
// surface for AST inspection to succeed. We provide minimal type declarations
// so the fixtures type-check under analysistest's loader.
package prometheus

// Opts shapes — minimal stubs.
type CounterOpts struct {
	Name string
	Help string
}

type HistogramOpts struct {
	Name    string
	Help    string
	Buckets []float64
}

type GaugeOpts struct {
	Name string
	Help string
}

type SummaryOpts struct {
	Name string
	Help string
}

// Vec receiver types — empty structs are sufficient; the analyzer never
// touches their methods.
type CounterVec struct{}
type HistogramVec struct{}
type GaugeVec struct{}
type SummaryVec struct{}

// Constructors — signatures mirror the real client_golang surface so the
// fixtures compile, but the bodies are empty; analysistest doesn't run the
// code, only type-checks it before invoking the analyzer.
func NewCounterVec(opts CounterOpts, labels []string) *CounterVec       { return &CounterVec{} }
func NewHistogramVec(opts HistogramOpts, labels []string) *HistogramVec { return &HistogramVec{} }
func NewGaugeVec(opts GaugeOpts, labels []string) *GaugeVec             { return &GaugeVec{} }
func NewSummaryVec(opts SummaryOpts, labels []string) *SummaryVec       { return &SummaryVec{} }

// NewCounter (no Vec) — used by the analyzer-ignore fixture to assert that
// only *Vec constructors are inspected.
type Counter struct{}

func NewCounter(opts CounterOpts) *Counter { return &Counter{} }
