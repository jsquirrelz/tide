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
