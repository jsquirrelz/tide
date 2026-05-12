package crosspool

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"
)

// TestCrosspool asserts the analyzer fires on the violation fixture
// (a select waiting on both plannerPool and executorPool channels) and
// stays silent on the valid fixture (a select waiting on only one pool).
// Fixtures live under testdata/src/{valid,violation}/main.go.
func TestCrosspool(t *testing.T) {
	testdata := analysistest.TestData()
	// Valid fixture: select on one pool only — no diagnostic expected.
	analysistest.Run(t, testdata, Analyzer, "valid")
	// Violation fixture: select on both pools — diagnostic expected per
	// the directive on the select line.
	analysistest.Run(t, testdata, Analyzer, "violation")
}
