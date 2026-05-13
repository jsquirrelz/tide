// Synthetic pkg/dispatch for the analysistest "valid" fixture — stdlib only,
// so the providerfirewall analyzer must report no diagnostics here. The
// package import path resolved by analysistest is "valid/pkg/dispatch", which
// contains "/pkg/dispatch" — the analyzer's scope predicate does engage on
// this package, and the absence of LLM SDK imports proves the firewall is
// correctly silent on clean code.
package dispatch

import (
	"encoding/json"
	"fmt"
)

var _ = fmt.Sprintf
var _ = json.Marshal
