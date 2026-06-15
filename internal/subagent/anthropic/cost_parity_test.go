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

// TestCostParity_FourField asserts that estimatedCostCents delegates all four
// token dimensions correctly for claude-sonnet-4-6 and that the ceiling
// division is consistent with the per-model rate table.
//
// Rates for claude-sonnet-4-6 (pricing.go priceTable):
//
//	input=300, output=1500, cacheRead=30, cacheWrite=375 cents/MTok.
func TestCostParity_FourField(t *testing.T) {
	// a is a default Anthropic instance — effective table equals priceTable clone.
	a := New(Options{})

	t.Run("four_field_nonzero", func(t *testing.T) {
		// Input:        500_000 * 300 / 1_000_000 = 150 cents
		// Output:       100_000 * 1500 / 1_000_000 = 150 cents
		// CacheRead:    800_000 * 30   / 1_000_000 = 24 cents
		// CacheCreation: 200_000 * 375 / 1_000_000 = 75 cents
		// Total numerator = 150_000_000 + 150_000_000 + 24_000_000 + 75_000_000 = 399_000_000
		// ceil(399_000_000 / 1_000_000) = 399 cents (exact, no fractional)
		u := pkgdispatch.Usage{
			InputTokens:         500_000,
			OutputTokens:        100_000,
			CacheReadTokens:     800_000,
			CacheCreationTokens: 200_000,
		}
		got := a.estimatedCostCents("claude-sonnet-4-6", u)
		if got != 399 {
			t.Errorf("four-field sonnet-4-6: want 399 cents, got %d", got)
		}
	})

	t.Run("zero_usage_zero_cents", func(t *testing.T) {
		// All-zero Usage must return 0 for a known model.
		u := pkgdispatch.Usage{}
		got := a.estimatedCostCents("claude-sonnet-4-6", u)
		if got != 0 {
			t.Errorf("zero usage should cost 0 cents, got %d", got)
		}
	})
}

// TestCostParity_RealizedSavings demonstrates REALIZED per-wave savings:
// estimatedCostCents(uncached scenario) > estimatedCostCents(cached scenario)
// for a high cache-read:write-ratio wave. The cache-write premium (375 > 300
// cents/MTok for sonnet) is absorbed by the cache-read discount (30 cents/MTok),
// proving net savings — not just a gross per-dispatch read discount.
//
// Token counts are sized so both scenarios produce distinct integer cent values
// (no ceiling-division tie). Both branches delegate entirely to estimatedCostCents;
// no hand-rolled rate arithmetic (RESEARCH anti-pattern).
func TestCostParity_RealizedSavings(t *testing.T) {
	a := New(Options{})
	model := "claude-sonnet-4-6"

	// Uncached scenario: 5_200_000 input + 200_000 output (no cache fields).
	// 5_200_000*300 + 200_000*1500 = 1_560_000_000 + 300_000_000 = 1_860_000_000
	// ceil(1_860_000_000 / 1_000_000) = 1860 cents
	uNoCaching := pkgdispatch.Usage{
		InputTokens:  5_200_000,
		OutputTokens: 200_000,
	}

	// Cached scenario: same effective work but with high cache-read hits and
	// non-zero cache creation (write premium absorbed by read savings):
	// 1_000_000 input * 300 = 300_000_000
	// 200_000 output * 1500 = 300_000_000
	// 1_600_000 cacheRead * 30 = 48_000_000
	// 2_400_000 cacheCreation * 375 = 900_000_000
	// Total numerator = 1_548_000_000 → ceil(1_548_000_000 / 1_000_000) = 1548 cents
	// Savings = 1860 - 1548 = 312 cents (even after absorbing write premium)
	uWithCaching := pkgdispatch.Usage{
		InputTokens:         1_000_000,
		OutputTokens:        200_000,
		CacheReadTokens:     1_600_000,
		CacheCreationTokens: 2_400_000,
	}

	costNoCaching := a.estimatedCostCents(model, uNoCaching)
	costWithCaching := a.estimatedCostCents(model, uWithCaching)

	if costWithCaching >= costNoCaching {
		t.Errorf("cache should save money for high read:write ratio: no-cache=%d cents, with-cache=%d cents",
			costNoCaching, costWithCaching)
	}
}

// TestCostParity_RealizedSavings_AtScale demonstrates the savings proof at a
// scale (1M token units) where the fractional cent issue doesn't interfere and
// the realized savings are unambiguous.
func TestCostParity_RealizedSavings_AtScale(t *testing.T) {
	a := New(Options{})
	model := "claude-sonnet-4-6"

	// Uncached: 2_600_000 input + 100_000 output
	// 2_600_000*300 + 100_000*1500 = 780_000_000 + 150_000_000 = 930_000_000
	// ceil(930_000_000 / 1_000_000) = 930 cents
	uNoCaching := pkgdispatch.Usage{InputTokens: 2_600_000, OutputTokens: 100_000}

	// Cached: 500_000 input + 100_000 output + 800_000 cacheRead + 1_200_000 cacheCreation
	// 500_000*300 + 100_000*1500 + 800_000*30 + 1_200_000*375
	// = 150_000_000 + 150_000_000 + 24_000_000 + 450_000_000
	// = 774_000_000 → ceil(774_000_000 / 1_000_000) = 774 cents
	// Savings = 930 - 774 = 156 cents (even after absorbing write premium)
	uWithCaching := pkgdispatch.Usage{
		InputTokens:         500_000,
		OutputTokens:        100_000,
		CacheReadTokens:     800_000,
		CacheCreationTokens: 1_200_000,
	}

	costNoCaching := a.estimatedCostCents(model, uNoCaching)
	costWithCaching := a.estimatedCostCents(model, uWithCaching)

	if costNoCaching != 930 {
		t.Errorf("no-cache baseline: want 930 cents, got %d", costNoCaching)
	}
	if costWithCaching != 774 {
		t.Errorf("with-cache cost: want 774 cents, got %d", costWithCaching)
	}
	if costWithCaching >= costNoCaching {
		t.Errorf("realized savings failed: no-cache=%d cents >= with-cache=%d cents",
			costNoCaching, costWithCaching)
	}
}
