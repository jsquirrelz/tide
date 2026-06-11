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

package dispatch

import (
	"strings"
	"testing"
)

// TestParsePricingOverrides covers the provider-agnostic override parser
// and its ASVS V5 / T-14-01 validation rules.
func TestParsePricingOverrides(t *testing.T) {
	t.Run("empty_string", func(t *testing.T) {
		got, err := ParsePricingOverrides("")
		if err != nil {
			t.Fatalf("empty string: want nil error, got %v", err)
		}
		if got == nil {
			t.Fatal("empty string: want non-nil map, got nil")
		}
		if len(got) != 0 {
			t.Errorf("empty string: want empty map, got len=%d", len(got))
		}
	})

	t.Run("empty_object", func(t *testing.T) {
		got, err := ParsePricingOverrides("{}")
		if err != nil {
			t.Fatalf("empty object: want nil error, got %v", err)
		}
		if got == nil {
			t.Fatal("empty object: want non-nil map, got nil")
		}
		if len(got) != 0 {
			t.Errorf("empty object: want empty map, got len=%d", len(got))
		}
	})

	t.Run("valid_full_override", func(t *testing.T) {
		s := `{"claude-sonnet-4-6":{"inputCentsPerMTok":300,"outputCentsPerMTok":1500,"cacheReadCentsPerMTok":30,"cacheWriteCentsPerMTok":375}}`
		got, err := ParsePricingOverrides(s)
		if err != nil {
			t.Fatalf("valid override: want nil error, got %v", err)
		}
		p, ok := got["claude-sonnet-4-6"]
		if !ok {
			t.Fatal("valid override: expected claude-sonnet-4-6 key in result")
		}
		if p.InputCentsPerMTok != 300 {
			t.Errorf("InputCentsPerMTok = %d; want 300", p.InputCentsPerMTok)
		}
		if p.OutputCentsPerMTok != 1500 {
			t.Errorf("OutputCentsPerMTok = %d; want 1500", p.OutputCentsPerMTok)
		}
		if p.CacheReadCentsPerMTok != 30 {
			t.Errorf("CacheReadCentsPerMTok = %d; want 30", p.CacheReadCentsPerMTok)
		}
		if p.CacheWriteCentsPerMTok != 375 {
			t.Errorf("CacheWriteCentsPerMTok = %d; want 375", p.CacheWriteCentsPerMTok)
		}
	})

	t.Run("valid_omitted_cache_fields", func(t *testing.T) {
		// Cache fields are optional; omitted fields stay 0.
		s := `{"my-model":{"inputCentsPerMTok":500,"outputCentsPerMTok":2500}}`
		got, err := ParsePricingOverrides(s)
		if err != nil {
			t.Fatalf("omitted cache fields: want nil error, got %v", err)
		}
		p := got["my-model"]
		if p.CacheReadCentsPerMTok != 0 {
			t.Errorf("CacheReadCentsPerMTok = %d; want 0 (omitted)", p.CacheReadCentsPerMTok)
		}
		if p.CacheWriteCentsPerMTok != 0 {
			t.Errorf("CacheWriteCentsPerMTok = %d; want 0 (omitted)", p.CacheWriteCentsPerMTok)
		}
	})

	t.Run("valid_multiple_models", func(t *testing.T) {
		s := `{"model-a":{"inputCentsPerMTok":100,"outputCentsPerMTok":500},"model-b":{"inputCentsPerMTok":300,"outputCentsPerMTok":1500}}`
		got, err := ParsePricingOverrides(s)
		if err != nil {
			t.Fatalf("multiple models: want nil error, got %v", err)
		}
		if len(got) != 2 {
			t.Errorf("multiple models: want 2 entries, got %d", len(got))
		}
	})

	t.Run("zero_input_rejected", func(t *testing.T) {
		s := `{"m":{"inputCentsPerMTok":0,"outputCentsPerMTok":100}}`
		_, err := ParsePricingOverrides(s)
		if err == nil {
			t.Fatal("zero inputCentsPerMTok: want error, got nil")
		}
		if !strings.Contains(err.Error(), "m") {
			t.Errorf("error should name the offending model ID 'm', got: %v", err)
		}
	})

	t.Run("negative_input_rejected", func(t *testing.T) {
		s := `{"bad-model":{"inputCentsPerMTok":-100,"outputCentsPerMTok":500}}`
		_, err := ParsePricingOverrides(s)
		if err == nil {
			t.Fatal("negative inputCentsPerMTok: want error, got nil")
		}
		if !strings.Contains(err.Error(), "bad-model") {
			t.Errorf("error should name the offending model ID, got: %v", err)
		}
	})

	t.Run("zero_output_rejected", func(t *testing.T) {
		s := `{"m":{"inputCentsPerMTok":100,"outputCentsPerMTok":0}}`
		_, err := ParsePricingOverrides(s)
		if err == nil {
			t.Fatal("zero outputCentsPerMTok: want error, got nil")
		}
		if !strings.Contains(err.Error(), "m") {
			t.Errorf("error should name the offending model ID 'm', got: %v", err)
		}
	})

	t.Run("negative_output_rejected", func(t *testing.T) {
		s := `{"m":{"inputCentsPerMTok":100,"outputCentsPerMTok":-500}}`
		_, err := ParsePricingOverrides(s)
		if err == nil {
			t.Fatal("negative outputCentsPerMTok: want error, got nil")
		}
	})

	t.Run("negative_cache_read_rejected", func(t *testing.T) {
		s := `{"m":{"inputCentsPerMTok":100,"outputCentsPerMTok":500,"cacheReadCentsPerMTok":-10}}`
		_, err := ParsePricingOverrides(s)
		if err == nil {
			t.Fatal("negative cacheReadCentsPerMTok: want error, got nil")
		}
		if !strings.Contains(err.Error(), "m") {
			t.Errorf("error should name the offending model ID, got: %v", err)
		}
	})

	t.Run("negative_cache_write_rejected", func(t *testing.T) {
		s := `{"m":{"inputCentsPerMTok":100,"outputCentsPerMTok":500,"cacheWriteCentsPerMTok":-125}}`
		_, err := ParsePricingOverrides(s)
		if err == nil {
			t.Fatal("negative cacheWriteCentsPerMTok: want error, got nil")
		}
	})

	t.Run("malformed_json_rejected", func(t *testing.T) {
		_, err := ParsePricingOverrides(`{"m":invalid}`)
		if err == nil {
			t.Fatal("malformed JSON: want error, got nil")
		}
	})

	t.Run("whitespace_around_empty_object", func(t *testing.T) {
		// Whitespace-padded empty object behaves like "{}".
		got, err := ParsePricingOverrides("  {}  ")
		if err != nil {
			t.Fatalf("whitespace-padded empty: want nil error, got %v", err)
		}
		if len(got) != 0 {
			t.Errorf("whitespace-padded empty: want empty map, got len=%d", len(got))
		}
	})
}
