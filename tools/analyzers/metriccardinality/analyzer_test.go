package metriccardinality

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"
)

// TestMetricCardinality asserts the analyzer fires exactly on the four
// badlabels.go violations (one per *Vec constructor) and stays silent on
// the goodlabels.go fixture (which exercises all four constructors with
// clean label slices, plus a NewCounter-singular call that must be ignored).
//
// Fixtures live under testdata/src/{badlabels,goodlabels}/ so analysistest's
// GOPATH-style resolver picks them up. The stub
// github.com/prometheus/client_golang/prometheus package lives under
// testdata/src/github.com/prometheus/client_golang/prometheus/ so the
// fixtures can resolve the import without pulling the real SDK into the
// analyzer's go.mod.
func TestMetricCardinality(t *testing.T) {
	testdata := analysistest.TestData()
	// Violation fixture: 4 *Vec calls each carrying a "task" label literal.
	// Each violation is asserted by an in-file `// want` directive.
	analysistest.Run(t, testdata, Analyzer, "badlabels")
	// Clean fixture: 4 *Vec calls with safe label slices + 1 NewCounter
	// singular call. Absence of any `// want` directive means analysistest
	// will fail the test if the analyzer emits a diagnostic here.
	analysistest.Run(t, testdata, Analyzer, "goodlabels")
}
