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
