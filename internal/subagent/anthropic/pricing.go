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
// Prices are in US cents per one million tokens (cents/MTok). Sources:
//   claude-api skill model table, verified 2026-06-11; drift-checked weekly
//   by hack/check-pricing-drift.sh per D-03.
//
//   - claude-fable-5:   $10/M input, $50/M output
//   - claude-opus-4-8:  $5/M input,  $25/M output
//   - claude-opus-4-7:  $5/M input,  $25/M output
//   - claude-opus-4-6:  $5/M input,  $25/M output
//   - claude-sonnet-5:  $2/M input,  $10/M output (intro rate through 2026-08-31 — see row comment)
//   - claude-sonnet-4-6: $3/M input, $15/M output
//   - claude-haiku-4-5:  $1/M input,  $5/M output
//
// Cache pricing follows the Anthropic "Prompt Caching" rate schedule:
//   - cacheWrite: cacheWriteMultNum/cacheWriteMultDen × the base input price
//     per model (currently 1.25× — the 5-minute-TTL rate, probe-verified; D-08)
//   - cacheRead:  0.10× the base input price per model
//
// Model lookup is exact-ID first, then one trailing -YYYYMMDD strip retry
// (lookupPrice, D-01). Anything still unmatched bills at conservativeTier.

import (
	"fmt"
	"os"
	"regexp"

	pkgdispatch "github.com/jsquirrelz/tide/pkg/dispatch"
)

// cacheWriteMultNum / cacheWriteMultDen encode the prompt-cache write premium
// as one exact rational: cacheWrite = input × cacheWriteMultNum / cacheWriteMultDen.
// Every priceTable row's cacheWriteCentsPerMTok derives from this multiplier
// (locked by TestCacheWriteMultiplierConsistency); it is the ONE place to flip
// on the next TTL shift (D-08). Rule: 5m TTL → 1.25× input; 1h TTL → 2× input.
//
// Value 125/100 set from the COST-03 probe evidence, recorded verbatim in
// .planning/phases/38-small-independents-pricing-accuracy-promptfile-telemetry-nud/38-01-PROBE-RESULT.md:
//   - Probe date 2026-07-11; `claude --version`: 2.1.207 (Claude Code); model
//     claude-haiku-4-5; host CLI dispatched with the production flag set
//     (subagent.go:285-294) through a teed credproxy (--tee-body-dir).
//   - Every distinct cache_control shape observed across the teed request
//     bodies was `"cache_control":{"type":"ephemeral"}` — the ONLY shape;
//     NO `"ttl":"1h"` appeared anywhere. Unambiguous 5-minute TTL.
//   - Caveat: the probe exercised the host CLI 2.1.207, not the subagent
//     image's pinned CLI (identical flag set); re-probe via the docker-image
//     variant if the pinned CLI ever diverges in cache-TTL behavior.
const (
	cacheWriteMultNum = 125
	cacheWriteMultDen = 100
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
	// Most expensive tier — serves as conservativeTier fallback (D-01).
	// $10/M input, $50/M output.
	"claude-fable-5": {
		inputCentsPerMTok:      1000,
		outputCentsPerMTok:     5000,
		cacheReadCentsPerMTok:  100,
		cacheWriteCentsPerMTok: 1250,
	},

	// Opus 4.8 — $5/M input, $25/M output (D-01, verified 2026-06-11).
	"claude-opus-4-8": {
		inputCentsPerMTok:      500,
		outputCentsPerMTok:     2500,
		cacheReadCentsPerMTok:  50,
		cacheWriteCentsPerMTok: 625,
	},

	// Opus 4.7 — $5/M input, $25/M output (D-01 correction; was $15/$75, Opus 4.1-era error).
	"claude-opus-4-7": {
		inputCentsPerMTok:      500,
		outputCentsPerMTok:     2500,
		cacheReadCentsPerMTok:  50,
		cacheWriteCentsPerMTok: 625,
	},

	// Opus 4.6 — $5/M input, $25/M output (D-01, verified 2026-06-11).
	"claude-opus-4-6": {
		inputCentsPerMTok:      500,
		outputCentsPerMTok:     2500,
		cacheReadCentsPerMTok:  50,
		cacheWriteCentsPerMTok: 625,
	},

	// Claude Sonnet 5 — the missing row behind the 2026-07-03 first-run 2.8×
	// overcount ($10.86 TIDE tally vs $3.84 console actual — COST-01).
	// Intro rates $2/M input, $10/M output (live pricing page, verified
	// 2026-07-21), billed through 2026-08-31. Adopted over the sticker
	// $3/$15 deliberately (operator decision 2026-07-21, v1.0.9 release):
	// TIDE's tally must match console actuals — COST-01's whole point — and
	// the weekly hack/check-pricing-drift.sh automation re-reddens the gate
	// the moment the live page flips back to sticker, so the post-intro
	// bump-back is structurally caught, not remembered.
	"claude-sonnet-5": {
		inputCentsPerMTok:      200,
		outputCentsPerMTok:     1000,
		cacheReadCentsPerMTok:  20,
		cacheWriteCentsPerMTok: 250, // input × 125/100 (D-08)
	},

	// Chart per-level defaults for phase, plan, and top-level fallback.
	// $3/M input, $15/M output; cache rates follow the same 1.25×/0.10× rule.
	"claude-sonnet-4-6": {
		inputCentsPerMTok:      300,
		outputCentsPerMTok:     1500,
		cacheReadCentsPerMTok:  30,
		cacheWriteCentsPerMTok: 375,
	},

	// DoD model (medium sample per-Project default, chart task-level default).
	// $1/M input, $5/M output; cache rates: write=1.25×input, read=0.10×input.
	"claude-haiku-4-5": {
		inputCentsPerMTok:      100,
		outputCentsPerMTok:     500,
		cacheReadCentsPerMTok:  10,
		cacheWriteCentsPerMTok: 125,
	},
}

// conservativeTier is the fallback price applied on a table miss (T-09-01 /
// Pitfall 4): use the most expensive known tier so a missing entry never
// under-counts budget spend. After D-01, fable-5 at $50/MTok output is the
// most expensive entry — NOT opus-4-7 (now corrected to $25/MTok).
var conservativeTier = priceTable["claude-fable-5"]

// dateSuffixRe matches a trailing dash + 8-digit date suffix on a model ID
// (e.g. "claude-sonnet-5-20260514" → strip "-20260514"). Anchored at end so
// only one dated alias per row is ever implied (D-01).
var dateSuffixRe = regexp.MustCompile(`-\d{8}$`)

// lookupPrice resolves model against the per-instance effective price table
// (a.prices — the New()-built merged clone, never the package-level priceTable;
// T-14-02): exact hit first, then — if the ID carries a trailing -YYYYMMDD date
// suffix — exactly one strip retry. Anything still unmatched returns
// (conservativeTier, false).
//
// Deliberately NO prefix/family matching (D-01): a genuinely unknown model
// (e.g. "claude-sonnet-6") must never silently inherit an older, cheaper
// family rate — it falls to the most-expensive known tier so budget tracking
// never under-counts spend (T-38-16).
func (a *Anthropic) lookupPrice(model string) (modelPrice, bool) {
	if price, ok := a.prices[model]; ok {
		return price, true
	}
	if stripped := dateSuffixRe.ReplaceAllString(model, ""); stripped != model {
		if price, ok := a.prices[stripped]; ok {
			return price, true
		}
	}
	return conservativeTier, false
}

// cacheSavingsCents returns the realized savings in US cents from prompt-cache
// reads for the given model and token usage (Phase 21 OBSV-02).
//
// Formula: savings = CacheReadTokens × (inputRate − cacheReadRate) / 1_000_000.
// Division is truncation (not ceiling) — conservative for savings, never
// over-reports what was saved (Pitfall 3 / plan 21-01 action).
//
// It resolves via lookupPrice (exact → one date-strip retry — D-01; reads
// a.prices per T-14-02). On a post-normalizer miss it falls back silently to
// conservativeTier — the conservative fallback bounds the savings estimate
// without alarming the operator (savings mis-estimate is less critical than
// cost mis-estimate; stderr noise is reserved for estimatedCostCents).
//
// Returns 0 immediately when CacheReadTokens == 0 (the common case where no
// cache reads occurred — omitempty on Usage.CacheSavingsCents keeps JSON clean).
func (a *Anthropic) cacheSavingsCents(model string, u pkgdispatch.Usage) int64 {
	if u.CacheReadTokens == 0 {
		return 0
	}

	// lookupPrice already returns conservativeTier on a miss; the savings
	// helper stays silent per its contract.
	price, _ := a.lookupPrice(model)

	// Net saving per million tokens: the gap between what was paid (cacheRead
	// rate) vs what would have been paid (input rate).
	savings := u.CacheReadTokens * (price.inputCentsPerMTok - price.cacheReadCentsPerMTok)

	const million = int64(1_000_000)
	// Truncation division: floor, not ceiling — conservative for savings.
	return savings / million
}

// estimatedCostCents returns the estimated cost in US cents (rounded up to the
// nearest whole cent) for the given model and token usage.
//
// It resolves via lookupPrice — exact hit against a.prices (the per-instance
// effective table built by New() as maps.Clone(priceTable) merged with
// Options.PricingOverrides — T-14-02 / Pitfall 2), then one trailing
// -YYYYMMDD strip retry (D-01). Both cost paths (this method and
// cacheSavingsCents) share the one normalizer.
//
// On a post-normalizer miss the method:
//  1. Logs a loud warning to stderr so the operator sees it in Pod logs.
//  2. Falls back to the most-expensive known tier (conservativeTier) to
//     ensure budget tracking never silently under-reports spend (T-09-01).
//
// A zero-token usage for a known model returns 0 (no spend).
func (a *Anthropic) estimatedCostCents(model string, u pkgdispatch.Usage) int64 {
	price, ok := a.lookupPrice(model)
	if !ok {
		// Loud, operator-visible warning: unknown model → conservative default.
		// Never return 0 silently (Pitfall 4 / T-09-01).
		fmt.Fprintf(os.Stderr, "pricing: unknown model %q, using conservative default (most-expensive known tier)\n", model)
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
