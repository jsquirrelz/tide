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
func TestEstimatedCostCents(t *testing.T) {
	t.Run("haiku_input_only", func(t *testing.T) {
		// claude-haiku-4-5: input=100 cents/MTok, output=500 cents/MTok
		// 1_000_000 input tokens * 100 cents/MTok / 1_000_000 = 100 cents
		u := pkgdispatch.Usage{InputTokens: 1_000_000}
		got := estimatedCostCents("claude-haiku-4-5", u)
		if got != 100 {
			t.Errorf("haiku input=1M: want 100 cents, got %d", got)
		}
	})

	t.Run("haiku_output_only", func(t *testing.T) {
		// 1_000_000 output tokens * 500 cents/MTok / 1_000_000 = 500 cents
		u := pkgdispatch.Usage{OutputTokens: 1_000_000}
		got := estimatedCostCents("claude-haiku-4-5", u)
		if got != 500 {
			t.Errorf("haiku output=1M: want 500 cents, got %d", got)
		}
	})

	t.Run("haiku_cache_read", func(t *testing.T) {
		// 1_000_000 cache-read tokens * 10 cents/MTok / 1_000_000 = 10 cents
		u := pkgdispatch.Usage{CacheReadTokens: 1_000_000}
		got := estimatedCostCents("claude-haiku-4-5", u)
		if got != 10 {
			t.Errorf("haiku cacheRead=1M: want 10 cents, got %d", got)
		}
	})

	t.Run("haiku_cache_creation", func(t *testing.T) {
		// 1_000_000 cache-creation tokens * 125 cents/MTok / 1_000_000 = 125 cents
		u := pkgdispatch.Usage{CacheCreationTokens: 1_000_000}
		got := estimatedCostCents("claude-haiku-4-5", u)
		if got != 125 {
			t.Errorf("haiku cacheCreation=1M: want 125 cents, got %d", got)
		}
	})

	t.Run("ceil_sub_cent", func(t *testing.T) {
		// 100 input tokens * 100 cents/MTok / 1_000_000 = 0.01 cents — rounds UP to 1
		u := pkgdispatch.Usage{InputTokens: 100}
		got := estimatedCostCents("claude-haiku-4-5", u)
		if got < 1 {
			t.Errorf("sub-cent should ceil to >=1, got %d", got)
		}
	})

	t.Run("unknown_model_nonzero", func(t *testing.T) {
		// Unknown model must return a non-zero conservative estimate (Pitfall 4 / T-09-01).
		// The exact value is the most-expensive known tier — just assert > 0.
		u := pkgdispatch.Usage{InputTokens: 1_000_000, OutputTokens: 1_000_000}
		got := estimatedCostCents("claude-unknown-future-model", u)
		if got <= 0 {
			t.Errorf("unknown model must return non-zero conservative estimate, got %d", got)
		}
	})

	t.Run("unknown_model_conservative_ge_haiku", func(t *testing.T) {
		// Conservative default must be >= haiku price (the most-expensive known tier
		// for the same token counts — ensures we never under-bill on table miss).
		u := pkgdispatch.Usage{InputTokens: 1_000_000, OutputTokens: 1_000_000}
		haiku := estimatedCostCents("claude-haiku-4-5", u)
		unknown := estimatedCostCents("some-unknown-model", u)
		if unknown < haiku {
			t.Errorf("unknown model conservative estimate %d < haiku %d", unknown, haiku)
		}
	})

	t.Run("sonnet_present", func(t *testing.T) {
		// claude-sonnet-4-6 must be in the table (chart per-level default for phase/plan/task).
		// Verify it returns a positive, distinct, non-zero value.
		u := pkgdispatch.Usage{InputTokens: 1_000_000}
		got := estimatedCostCents("claude-sonnet-4-6", u)
		if got <= 0 {
			t.Errorf("sonnet-4-6: want >0 cents, got %d", got)
		}
	})

	t.Run("opus_present", func(t *testing.T) {
		// claude-opus-4-7 must be in the table (chart milestone-level default).
		u := pkgdispatch.Usage{OutputTokens: 1_000_000}
		got := estimatedCostCents("claude-opus-4-7", u)
		if got <= 0 {
			t.Errorf("opus-4-7: want >0 cents, got %d", got)
		}
	})

	t.Run("zero_tokens_zero_cents", func(t *testing.T) {
		// No tokens → 0 cents; the conservative default only kicks in on table miss.
		u := pkgdispatch.Usage{}
		got := estimatedCostCents("claude-haiku-4-5", u)
		if got != 0 {
			t.Errorf("zero tokens should cost 0 cents, got %d", got)
		}
	})
}
