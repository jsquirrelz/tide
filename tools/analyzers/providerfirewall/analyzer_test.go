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

package providerfirewall

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"
)

// TestProviderFirewall asserts the analyzer fires on the violation fixture
// (a synthetic pkg/controller importing github.com/anthropics/anthropic-sdk-go)
// and stays silent on:
//   - the valid fixture (pkg/dispatch with only stdlib imports)
//   - the allowed fixture (internal/subagent/anthropic — the harness-adapter
//     site that is explicitly out of scope for the firewall by construction).
//
// Fixtures live under testdata/src/{valid,violation}/... so analysistest's
// GOPATH-style resolver picks them up. The stub anthropic-sdk-go package lives
// under testdata/src/github.com/anthropics/anthropic-sdk-go/ so both the
// violation and allowed fixtures can resolve the import without pulling the
// real SDK.
func TestProviderFirewall(t *testing.T) {
	testdata := analysistest.TestData()
	// Valid fixture: stdlib-only pkg/dispatch — no diagnostic expected.
	analysistest.Run(t, testdata, Analyzer, "valid/pkg/dispatch")
	// Violation fixture: pkg/controller importing the Anthropic SDK — diagnostic expected
	// per the `// want` comment in the fixture file.
	analysistest.Run(t, testdata, Analyzer, "violation/pkg/controller")
	// Allowed fixture: internal/subagent/anthropic — the harness-adapter site is
	// out-of-scope; same SDK import must NOT trigger a diagnostic here.
	analysistest.Run(t, testdata, Analyzer, "valid/internal/subagent/anthropic")
}
