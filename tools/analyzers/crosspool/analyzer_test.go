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
