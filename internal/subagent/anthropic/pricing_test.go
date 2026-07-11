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

import (
	"testing"

	pkgdispatch "github.com/jsquirrelz/tide/pkg/dispatch"
)

// TestEstimatedCostCents covers the per-model price table and estimatedCostCents.
// All calls go through New(Options{}) to exercise the per-instance effective table
// path (D-02 / T-14-02); the package-level priceTable is never accessed directly.
func TestEstimatedCostCents(t *testing.T) {
	// a is a default Anthropic instance with no overrides — effective table
	// equals a clone of the compiled priceTable.
	a := New(Options{})

	t.Run("haiku_input_only", func(t *testing.T) {
		// claude-haiku-4-5: input=100 cents/MTok, output=500 cents/MTok
		// 1_000_000 input tokens * 100 cents/MTok / 1_000_000 = 100 cents
		u := pkgdispatch.Usage{InputTokens: 1_000_000}
		got := a.estimatedCostCents("claude-haiku-4-5", u)
		if got != 100 {
			t.Errorf("haiku input=1M: want 100 cents, got %d", got)
		}
	})

	t.Run("haiku_output_only", func(t *testing.T) {
		// 1_000_000 output tokens * 500 cents/MTok / 1_000_000 = 500 cents
		u := pkgdispatch.Usage{OutputTokens: 1_000_000}
		got := a.estimatedCostCents("claude-haiku-4-5", u)
		if got != 500 {
			t.Errorf("haiku output=1M: want 500 cents, got %d", got)
		}
	})

	t.Run("haiku_cache_read", func(t *testing.T) {
		// 1_000_000 cache-read tokens * 10 cents/MTok / 1_000_000 = 10 cents
		u := pkgdispatch.Usage{CacheReadTokens: 1_000_000}
		got := a.estimatedCostCents("claude-haiku-4-5", u)
		if got != 10 {
			t.Errorf("haiku cacheRead=1M: want 10 cents, got %d", got)
		}
	})

	t.Run("haiku_cache_creation", func(t *testing.T) {
		// 1_000_000 cache-creation tokens * 125 cents/MTok / 1_000_000 = 125 cents
		u := pkgdispatch.Usage{CacheCreationTokens: 1_000_000}
		got := a.estimatedCostCents("claude-haiku-4-5", u)
		if got != 125 {
			t.Errorf("haiku cacheCreation=1M: want 125 cents, got %d", got)
		}
	})

	t.Run("ceil_sub_cent", func(t *testing.T) {
		// 100 input tokens * 100 cents/MTok / 1_000_000 = 0.01 cents — rounds UP to 1
		u := pkgdispatch.Usage{InputTokens: 100}
		got := a.estimatedCostCents("claude-haiku-4-5", u)
		if got < 1 {
			t.Errorf("sub-cent should ceil to >=1, got %d", got)
		}
	})

	t.Run("unknown_model_nonzero", func(t *testing.T) {
		// Unknown model must return a non-zero conservative estimate (Pitfall 4 / T-09-01).
		// The exact value is the most-expensive known tier — just assert > 0.
		u := pkgdispatch.Usage{InputTokens: 1_000_000, OutputTokens: 1_000_000}
		got := a.estimatedCostCents("claude-unknown-future-model", u)
		if got <= 0 {
			t.Errorf("unknown model must return non-zero conservative estimate, got %d", got)
		}
	})

	t.Run("unknown_model_conservative_ge_haiku", func(t *testing.T) {
		// Conservative default must be >= haiku price (the most-expensive known tier
		// for the same token counts — ensures we never under-bill on table miss).
		u := pkgdispatch.Usage{InputTokens: 1_000_000, OutputTokens: 1_000_000}
		haiku := a.estimatedCostCents("claude-haiku-4-5", u)
		unknown := a.estimatedCostCents("some-unknown-model", u)
		if unknown < haiku {
			t.Errorf("unknown model conservative estimate %d < haiku %d", unknown, haiku)
		}
	})

	t.Run("sonnet_present", func(t *testing.T) {
		// claude-sonnet-4-6 must be in the table (chart per-level default for phase/plan/task).
		// Verify it returns a positive, distinct, non-zero value.
		u := pkgdispatch.Usage{InputTokens: 1_000_000}
		got := a.estimatedCostCents("claude-sonnet-4-6", u)
		if got <= 0 {
			t.Errorf("sonnet-4-6: want >0 cents, got %d", got)
		}
	})

	t.Run("opus_present", func(t *testing.T) {
		// claude-opus-4-7 must be in the table (chart milestone-level default).
		u := pkgdispatch.Usage{OutputTokens: 1_000_000}
		got := a.estimatedCostCents("claude-opus-4-7", u)
		if got <= 0 {
			t.Errorf("opus-4-7: want >0 cents, got %d", got)
		}
	})

	t.Run("zero_tokens_zero_cents", func(t *testing.T) {
		// No tokens → 0 cents; the conservative default only kicks in on table miss.
		u := pkgdispatch.Usage{}
		got := a.estimatedCostCents("claude-haiku-4-5", u)
		if got != 0 {
			t.Errorf("zero tokens should cost 0 cents, got %d", got)
		}
	})

	t.Run("fable5_input_output", func(t *testing.T) {
		// claude-fable-5: input=1000 cents/MTok, output=5000 cents/MTok
		// 1M input * 1000 / 1M = 1000 cents; 1M output * 5000 / 1M = 5000 cents → 6000 total
		u := pkgdispatch.Usage{InputTokens: 1_000_000, OutputTokens: 1_000_000}
		got := a.estimatedCostCents("claude-fable-5", u)
		if got != 6000 {
			t.Errorf("fable-5 input+output=1M each: want 6000 cents, got %d", got)
		}
	})

	t.Run("opus47_corrected", func(t *testing.T) {
		// claude-opus-4-7 must be corrected from $75/M output to $25/M output (D-01 fix).
		// Regression: old value was 7500 cents/MTok output; new value must be 2500.
		u := pkgdispatch.Usage{OutputTokens: 1_000_000}
		got := a.estimatedCostCents("claude-opus-4-7", u)
		if got != 2500 {
			t.Errorf("opus-4-7 output=1M: want 2500 cents (D-01 corrected), got %d (regression: old value was 7500)", got)
		}
	})

	t.Run("opus48_present", func(t *testing.T) {
		// claude-opus-4-8 must be in the table (new model ID, D-01).
		// 1M output at $25/M = 2500 cents; no stderr warning expected.
		u := pkgdispatch.Usage{OutputTokens: 1_000_000}
		got := a.estimatedCostCents("claude-opus-4-8", u)
		if got != 2500 {
			t.Errorf("opus-4-8 output=1M: want 2500 cents, got %d", got)
		}
	})

	t.Run("opus46_present", func(t *testing.T) {
		// claude-opus-4-6 must be in the table (new model ID, D-01).
		u := pkgdispatch.Usage{OutputTokens: 1_000_000}
		got := a.estimatedCostCents("claude-opus-4-6", u)
		if got != 2500 {
			t.Errorf("opus-4-6 output=1M: want 2500 cents, got %d", got)
		}
	})

	t.Run("conservative_tier_is_fable5", func(t *testing.T) {
		// Unknown model must fall back to fable-5 rates (most expensive after D-01 table fix).
		// fable-5 output = 5000 cents/MTok; old conservative was opus-4-7 at 7500 (wrong).
		u := pkgdispatch.Usage{OutputTokens: 1_000_000}
		got := a.estimatedCostCents("unknown-model-xyz", u)
		if got != 5000 {
			t.Errorf("unknown model conservative tier: want 5000 cents (fable-5 output rate), got %d", got)
		}
	})

	t.Run("fable5_cache_rates", func(t *testing.T) {
		// fable-5 cacheRead = 100 cents/MTok (0.10× input); cacheWrite = 1250 cents/MTok (1.25× input).
		uRead := pkgdispatch.Usage{CacheReadTokens: 1_000_000}
		gotRead := a.estimatedCostCents("claude-fable-5", uRead)
		if gotRead != 100 {
			t.Errorf("fable-5 cacheRead=1M: want 100 cents, got %d", gotRead)
		}
		uWrite := pkgdispatch.Usage{CacheCreationTokens: 1_000_000}
		gotWrite := a.estimatedCostCents("claude-fable-5", uWrite)
		if gotWrite != 1250 {
			t.Errorf("fable-5 cacheWrite=1M: want 1250 cents, got %d", gotWrite)
		}
	})

	t.Run("override_merge", func(t *testing.T) {
		// D-02: build an Anthropic with a PricingOverrides that replaces haiku's price.
		// Verify the override is used; then verify the default instance is unaffected
		// (package-level priceTable untouched, T-14-02).
		overrideHaikuInput := int64(200)
		overrideHaikuOutput := int64(1000)
		aOverride := New(Options{
			PricingOverrides: map[string]pkgdispatch.PriceOverride{
				"claude-haiku-4-5": {
					InputCentsPerMTok:  overrideHaikuInput,
					OutputCentsPerMTok: overrideHaikuOutput,
				},
			},
		})
		// Overridden instance should use new rates.
		uIn := pkgdispatch.Usage{InputTokens: 1_000_000}
		got := aOverride.estimatedCostCents("claude-haiku-4-5", uIn)
		if got != overrideHaikuInput {
			t.Errorf("override_merge: overridden haiku input=1M: want %d cents, got %d", overrideHaikuInput, got)
		}
		// Default instance (a) must still use the compiled haiku rate (100 cents/MTok input).
		gotDefault := a.estimatedCostCents("claude-haiku-4-5", uIn)
		if gotDefault != 100 {
			t.Errorf("override_merge: default instance haiku input=1M: want 100 cents, got %d (package var mutated? T-14-02)", gotDefault)
		}
	})

	t.Run("new_model_via_override", func(t *testing.T) {
		// D-02: an unknown-to-table model present in overrides prices at override rates.
		// Cache fields are auto-derived (read=input/10, write=input*125/100) when omitted.
		aNew := New(Options{
			PricingOverrides: map[string]pkgdispatch.PriceOverride{
				"claude-future-model": {
					InputCentsPerMTok:  400,
					OutputCentsPerMTok: 2000,
					// CacheRead/Write omitted → auto-derived
				},
			},
		})
		uIn := pkgdispatch.Usage{InputTokens: 1_000_000}
		gotIn := aNew.estimatedCostCents("claude-future-model", uIn)
		if gotIn != 400 {
			t.Errorf("new model via override input=1M: want 400 cents, got %d", gotIn)
		}
		uOut := pkgdispatch.Usage{OutputTokens: 1_000_000}
		gotOut := aNew.estimatedCostCents("claude-future-model", uOut)
		if gotOut != 2000 {
			t.Errorf("new model via override output=1M: want 2000 cents, got %d", gotOut)
		}
		// Cache read: derived as input/10 = 40 cents/MTok
		uCR := pkgdispatch.Usage{CacheReadTokens: 1_000_000}
		gotCR := aNew.estimatedCostCents("claude-future-model", uCR)
		if gotCR != 40 {
			t.Errorf("new model via override cacheRead=1M: want 40 cents (input/10), got %d", gotCR)
		}
		// Cache write: derived as input*125/100 = 500 cents/MTok
		uCW := pkgdispatch.Usage{CacheCreationTokens: 1_000_000}
		gotCW := aNew.estimatedCostCents("claude-future-model", uCW)
		if gotCW != 500 {
			t.Errorf("new model via override cacheWrite=1M: want 500 cents (input*1.25), got %d", gotCW)
		}
	})
}

// TestCacheSavingsCents covers the realized-savings helper (Phase 21 OBSV-02).
// Formula: savings = CacheReadTokens × (inputRate − cacheReadRate) / 1_000_000,
// truncation division (conservative for savings — Pitfall 3).
// All calls go through New(Options{}) per T-14-02 (per-instance a.prices clone).
func TestCacheSavingsCents(t *testing.T) {
	a := New(Options{})

	t.Run("haiku_1M_read_tokens", func(t *testing.T) {
		// haiku: input=100 cents/MTok, cacheRead=10 cents/MTok
		// savings = 1_000_000 × (100 − 10) / 1_000_000 = 90 cents
		u := pkgdispatch.Usage{CacheReadTokens: 1_000_000}
		got := a.cacheSavingsCents("claude-haiku-4-5", u)
		if got != 90 {
			t.Errorf("haiku cacheRead=1M: want 90 cents, got %d", got)
		}
	})

	t.Run("sonnet_1M_read_tokens", func(t *testing.T) {
		// sonnet: input=300 cents/MTok, cacheRead=30 cents/MTok
		// savings = 1_000_000 × (300 − 30) / 1_000_000 = 270 cents
		u := pkgdispatch.Usage{CacheReadTokens: 1_000_000}
		got := a.cacheSavingsCents("claude-sonnet-4-6", u)
		if got != 270 {
			t.Errorf("sonnet cacheRead=1M: want 270 cents, got %d", got)
		}
	})

	t.Run("zero_read_tokens", func(t *testing.T) {
		// No cache reads → no savings; InputTokens present but irrelevant.
		u := pkgdispatch.Usage{InputTokens: 1_000_000}
		got := a.cacheSavingsCents("claude-haiku-4-5", u)
		if got != 0 {
			t.Errorf("zero CacheReadTokens: want 0 cents, got %d", got)
		}
	})

	t.Run("truncation_not_ceiling", func(t *testing.T) {
		// 100 cache-read tokens × 90 cents/MTok (haiku net) = 0.009 cents → truncates to 0.
		// Confirms truncation (not ceiling) division for savings (Pitfall 3).
		u := pkgdispatch.Usage{CacheReadTokens: 100}
		got := a.cacheSavingsCents("claude-haiku-4-5", u)
		if got != 0 {
			t.Errorf("100 read tokens haiku: want 0 (truncation), got %d", got)
		}
	})

	t.Run("unknown_model_uses_conservative", func(t *testing.T) {
		// Unknown model falls back to conservativeTier (fable-5): input=1000, cacheRead=100.
		// savings = 1_000_000 × (1000 − 100) / 1_000_000 = 900 cents.
		// No stderr warning expected (silent fallback for savings helper — see action).
		u := pkgdispatch.Usage{CacheReadTokens: 1_000_000}
		got := a.cacheSavingsCents("claude-unknown-model", u)
		if got != 900 {
			t.Errorf("unknown model cacheRead=1M: want 900 cents (conservative tier), got %d", got)
		}
	})

	t.Run("dated_sonnet5_shares_normalizer", func(t *testing.T) {
		// D-01: cacheSavingsCents resolves through the same lookupPrice
		// normalizer as estimatedCostCents — a dated sonnet-5 ID uses the
		// sonnet-5 rates, not the conservative tier.
		// sonnet-5: input=300, cacheRead=30 → savings = 1M × (300 − 30) / 1M = 270.
		u := pkgdispatch.Usage{CacheReadTokens: 1_000_000}
		got := a.cacheSavingsCents("claude-sonnet-5-20260514", u)
		if got != 270 {
			t.Errorf("dated sonnet-5 cacheRead=1M: want 270 cents (sonnet-5 rates via normalizer), got %d", got)
		}
	})
}

// TestEstimatedCostCents_Sonnet5Row pins the claude-sonnet-5 price row
// (COST-01): the missing row behind the 2026-07-03 first-run 2.8× overcount.
// Sticker rates: input=300, output=1500, cacheRead=30 cents/MTok;
// cacheWrite=375 (input × 125/100, the D-08 probe-verified multiplier).
func TestEstimatedCostCents_Sonnet5Row(t *testing.T) {
	a := New(Options{})

	cases := []struct {
		name string
		u    pkgdispatch.Usage
		want int64
	}{
		{"input", pkgdispatch.Usage{InputTokens: 1_000_000}, 300},
		{"output", pkgdispatch.Usage{OutputTokens: 1_000_000}, 1500},
		{"cache_read", pkgdispatch.Usage{CacheReadTokens: 1_000_000}, 30},
		{"cache_write", pkgdispatch.Usage{CacheCreationTokens: 1_000_000}, 375},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := a.estimatedCostCents("claude-sonnet-5", tc.u)
			if got != tc.want {
				t.Errorf("sonnet-5 %s=1M: want %d cents, got %d", tc.name, tc.want, got)
			}
		})
	}
}

// TestLookupPriceNormalizer covers the D-01 date-suffix normalizer: exact
// lookup first, then exactly one trailing -YYYYMMDD strip retry, then the
// conservative tier. NO family prefix-matching — a genuinely unknown model
// must never inherit an old cheaper family rate.
func TestLookupPriceNormalizer(t *testing.T) {
	a := New(Options{})

	t.Run("dated_sonnet5_prices_as_sonnet5", func(t *testing.T) {
		// "claude-sonnet-5-20260514" strips to "claude-sonnet-5" and must price
		// identically for every token dimension.
		u := pkgdispatch.Usage{
			InputTokens:         500_000,
			OutputTokens:        100_000,
			CacheReadTokens:     800_000,
			CacheCreationTokens: 200_000,
		}
		dated := a.estimatedCostCents("claude-sonnet-5-20260514", u)
		bare := a.estimatedCostCents("claude-sonnet-5", u)
		if dated != bare {
			t.Errorf("dated sonnet-5: want %d cents (same as bare ID), got %d", bare, dated)
		}
	})

	t.Run("dated_haiku_prices_as_haiku", func(t *testing.T) {
		u := pkgdispatch.Usage{InputTokens: 1_000_000, OutputTokens: 1_000_000}
		dated := a.estimatedCostCents("claude-haiku-4-5-20250101", u)
		bare := a.estimatedCostCents("claude-haiku-4-5", u)
		if dated != bare {
			t.Errorf("dated haiku: want %d cents (same as bare ID), got %d", bare, dated)
		}
	})

	t.Run("no_family_matching_sonnet6", func(t *testing.T) {
		// D-01: "claude-sonnet-6" must NOT inherit any sonnet row — it prices
		// at the conservative tier (fable-5 output = 5000 cents/MTok).
		u := pkgdispatch.Usage{OutputTokens: 1_000_000}
		got := a.estimatedCostCents("claude-sonnet-6", u)
		if got != 5000 {
			t.Errorf("claude-sonnet-6 output=1M: want 5000 cents (conservative tier, no family matching), got %d", got)
		}
	})

	t.Run("no_family_matching_bare_sonnet", func(t *testing.T) {
		u := pkgdispatch.Usage{OutputTokens: 1_000_000}
		got := a.estimatedCostCents("claude-sonnet", u)
		if got != 5000 {
			t.Errorf("claude-sonnet output=1M: want 5000 cents (conservative tier, no family matching), got %d", got)
		}
	})

	t.Run("unknown_model_with_date_suffix", func(t *testing.T) {
		// The strip retry also misses for a genuinely unknown family →
		// conservative tier.
		u := pkgdispatch.Usage{OutputTokens: 1_000_000}
		got := a.estimatedCostCents("claude-nova-9-20270101", u)
		if got != 5000 {
			t.Errorf("claude-nova-9-20270101 output=1M: want 5000 cents (conservative tier), got %d", got)
		}
	})

	t.Run("override_resolves_through_normalizer", func(t *testing.T) {
		// The normalizer resolves against a.prices (the per-instance merged
		// clone, T-14-02) — so a dated ID picks up the sonnet-5 override.
		aOverride := New(Options{
			PricingOverrides: map[string]pkgdispatch.PriceOverride{
				"claude-sonnet-5": {
					InputCentsPerMTok:  700,
					OutputCentsPerMTok: 3500,
				},
			},
		})
		u := pkgdispatch.Usage{InputTokens: 1_000_000}
		got := aOverride.estimatedCostCents("claude-sonnet-5-20260514", u)
		if got != 700 {
			t.Errorf("dated sonnet-5 with override input=1M: want 700 cents (override rate via normalizer), got %d", got)
		}
	})
}

// TestCacheWriteMultiplierConsistency asserts every compiled price row derives
// its cache-write rate from the single D-08 multiplier constant:
// cacheWrite = input × cacheWriteMultNum / cacheWriteMultDen. One place to
// change on the next TTL shift; this test catches any row drifting from it.
// The override derive-path case guards New()'s merge logic against the same
// drift: an override omitting cache fields must derive from the constant, not
// a hardcoded ratio, so a future flip moves compiled and override-derived
// rows together.
func TestCacheWriteMultiplierConsistency(t *testing.T) {
	for model, price := range priceTable {
		want := price.inputCentsPerMTok * cacheWriteMultNum / cacheWriteMultDen
		if price.cacheWriteCentsPerMTok != want {
			t.Errorf("%s: cacheWriteCentsPerMTok=%d, want %d (input %d × %d/%d — D-08 multiplier drift)",
				model, price.cacheWriteCentsPerMTok, want,
				price.inputCentsPerMTok, cacheWriteMultNum, cacheWriteMultDen)
		}
	}

	t.Run("override_derive_path", func(t *testing.T) {
		const overrideInput = int64(700)
		a := New(Options{
			PricingOverrides: map[string]pkgdispatch.PriceOverride{
				"claude-derive-check": {
					InputCentsPerMTok:  overrideInput,
					OutputCentsPerMTok: 3500,
					// CacheWrite omitted → must derive from the D-08 constant.
				},
			},
		})
		got := a.prices["claude-derive-check"].cacheWriteCentsPerMTok
		want := overrideInput * cacheWriteMultNum / cacheWriteMultDen
		if got != want {
			t.Errorf("override-derived cacheWriteCentsPerMTok=%d, want %d (input %d × %d/%d — D-08 multiplier drift in New())",
				got, want, overrideInput, cacheWriteMultNum, cacheWriteMultDen)
		}
	})
}
