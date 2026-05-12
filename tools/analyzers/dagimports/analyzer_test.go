package dagimports

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"
)

// TestDagImports asserts the analyzer fires on the violation fixture
// (a synthetic pkg/dag importing k8s.io/...) and stays silent on the
// valid fixture (a synthetic pkg/dag with only stdlib imports). The
// fixtures live under testdata/src/{valid,violation}/pkg/dag/dag.go so
// analysistest's GOPATH-style resolver picks them up.
func TestDagImports(t *testing.T) {
	testdata := analysistest.TestData()
	// Valid fixture: stdlib-only pkg/dag — no diagnostic expected.
	analysistest.Run(t, testdata, Analyzer, "valid/pkg/dag")
	// Violation fixture: pkg/dag with a k8s.io import — diagnostic expected
	// per the `// want` comment in the fixture file.
	analysistest.Run(t, testdata, Analyzer, "violation/pkg/dag")
}
