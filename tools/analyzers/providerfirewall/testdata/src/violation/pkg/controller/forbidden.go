// Synthetic pkg/controller for the analysistest "violation" fixture; the
// providerfirewall analyzer must flag the forbidden anthropic SDK import below
// per the directive on the same line as that import.
package controller

import (
	_ "github.com/anthropics/anthropic-sdk-go" // want `SUB-05 violation: forbidden LLM SDK import "github.com/anthropics/anthropic-sdk-go" in violation/pkg/controller .Pitfall 14: vendor lock-in creep.`
)
