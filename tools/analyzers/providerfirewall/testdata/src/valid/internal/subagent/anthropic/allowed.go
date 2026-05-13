// Synthetic internal/subagent/anthropic for the analysistest "allowed" fixture.
// This is the harness-adapter site: internal/subagent/anthropic/... is
// explicitly out-of-scope for the providerfirewall analyzer by construction
// (Phase 3 will place real Anthropic SDK usage here). The same LLM SDK import
// that triggers a diagnostic in violation/pkg/controller must NOT trigger one
// here — proving the firewall is precisely scoped.
package anthropic

import (
	_ "github.com/anthropics/anthropic-sdk-go" // no want directive — analyzer must be silent
)
