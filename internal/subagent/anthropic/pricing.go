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

package anthropic

// pricing.go holds the per-model Anthropic price table and the
// estimatedCostCents function that multiplies parsed token counts by it.
//
// This file stays inside internal/subagent/anthropic/ (provider-specific,
// behind the Subagent interface) per CLAUDE.md anti-pattern guardrails and
// D-C1 provider firewall.
//
// Prices are in US cents per one million tokens (cents/MTok).  Sources:
//   - claude-haiku-4-5:  $1/M input, $5/M output (RESEARCH §Cost Surfacing ASSUMPTION A3)
//   - claude-sonnet-4-6: $3/M input, $15/M output (Anthropic pricing page, 2026-06)
//   - claude-opus-4-7:   $15/M input, $75/M output (Anthropic pricing page, 2026-06)
//
// Cache pricing follows the Anthropic "Prompt Caching" rate schedule:
//   - cacheWrite: 1.25× the base input price per model
//   - cacheRead:  0.10× the base input price per model

import (
	"fmt"
	"os"

	pkgdispatch "github.com/jsquirrelz/tide/pkg/dispatch"
)

// modelPrice holds per-model billing rates in US cents per one million tokens.
type modelPrice struct {
	inputCentsPerMTok      int64
	outputCentsPerMTok     int64
	cacheReadCentsPerMTok  int64
	cacheWriteCentsPerMTok int64
}

// priceTable is keyed on the exact resolved model string the orchestrator
// places in EnvelopeIn.Provider.Model.  Key strings must match the values in
// examples/projects/medium/project.yaml (subagent.model) and
// charts/tide/values.yaml (subagent per-level model defaults).
var priceTable = map[string]modelPrice{
	// DoD model (medium sample per-Project default, chart task-level default).
	// $1/M input, $5/M output; cache rates: write=1.25×input, read=0.10×input.
	// RESEARCH §Cost Surfacing ASSUMPTION A3.
	"claude-haiku-4-5": {
		inputCentsPerMTok:      100,
		outputCentsPerMTok:     500,
		cacheReadCentsPerMTok:  10,
		cacheWriteCentsPerMTok: 125,
	},

	// Chart per-level defaults for phase, plan, and top-level fallback.
	// $3/M input, $15/M output; cache rates follow the same 1.25×/0.10× rule.
	"claude-sonnet-4-6": {
		inputCentsPerMTok:      300,
		outputCentsPerMTok:     1500,
		cacheReadCentsPerMTok:  30,
		cacheWriteCentsPerMTok: 375,
	},

	// Chart milestone-level default.
	// $15/M input, $75/M output.
	"claude-opus-4-7": {
		inputCentsPerMTok:      1500,
		outputCentsPerMTok:     7500,
		cacheReadCentsPerMTok:  150,
		cacheWriteCentsPerMTok: 1875,
	},
}

// conservativeTier is the fallback price applied on a table miss (T-09-01 /
// Pitfall 4): use the most expensive known tier so a missing entry never
// under-counts budget spend.
var conservativeTier = priceTable["claude-opus-4-7"]

// estimatedCostCents returns the estimated cost in US cents (rounded up to the
// nearest whole cent) for the given model and token usage.
//
// On a table miss the function:
//  1. Logs a loud warning to stderr so the operator sees it in Pod logs.
//  2. Falls back to the most-expensive known tier (conservativeTier) to
//     ensure budget tracking never silently under-reports spend (T-09-01).
//
// A zero-token usage for a known model returns 0 (no spend).
func estimatedCostCents(model string, u pkgdispatch.Usage) int64 {
	price, ok := priceTable[model]
	if !ok {
		// Loud, operator-visible warning: unknown model → conservative default.
		// Never return 0 silently (Pitfall 4 / T-09-01).
		fmt.Fprintf(os.Stderr, "pricing: unknown model %q, using conservative default (most-expensive known tier)\n", model)
		price = conservativeTier
	}

	// Sum cost across all four token dimensions.
	// Division by 1_000_000 converts from "cents per million" to actual cents.
	// Integer arithmetic: compute the numerator first to preserve precision,
	// then apply ceiling division (ceil(n/d) = (n + d - 1) / d).
	numerator := u.InputTokens*price.inputCentsPerMTok +
		u.OutputTokens*price.outputCentsPerMTok +
		u.CacheReadTokens*price.cacheReadCentsPerMTok +
		u.CacheCreationTokens*price.cacheWriteCentsPerMTok

	if numerator == 0 {
		return 0
	}

	const million = int64(1_000_000)
	// Ceiling division: round up any sub-cent fraction to 1 cent.
	return (numerator + million - 1) / million
}
