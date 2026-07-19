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

package metriccardinality

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"
)

// TestMetricCardinality asserts the analyzer fires exactly on the
// badlabels.go violations — one per forbidden D-06 label name (task,
// run_id, loop_run_id, run, attempt, attempt_id, trace_id, task_uid, uid),
// spread across all four *Vec constructors — and stays silent on the
// goodlabels.go fixture (which exercises all four constructors with clean
// label slices, the bounded-enum positive controls terminal_reason/
// exit_reason/loop_kind/evaluator_type/risk_tier, plus a NewCounter-singular
// call that must be ignored).
//
// Fixtures live under testdata/src/{badlabels,goodlabels}/ so analysistest's
// GOPATH-style resolver picks them up. The stub
// github.com/prometheus/client_golang/prometheus package lives under
// testdata/src/github.com/prometheus/client_golang/prometheus/ so the
// fixtures can resolve the import without pulling the real SDK into the
// analyzer's go.mod.
func TestMetricCardinality(t *testing.T) {
	testdata := analysistest.TestData()
	// Violation fixture: *Vec calls each carrying one of the 9 forbidden
	// D-06 label literals. Each violation is asserted by an in-file
	// `// want` directive.
	analysistest.Run(t, testdata, Analyzer, "badlabels")
	// Clean fixture: *Vec calls with safe label slices (including the
	// bounded-enum positive controls) + 1 NewCounter singular call. Absence
	// of any `// want` directive means analysistest will fail the test if
	// the analyzer emits a diagnostic here.
	analysistest.Run(t, testdata, Analyzer, "goodlabels")
}
