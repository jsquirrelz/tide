/*
Copyright 2026 TIDE Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

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
