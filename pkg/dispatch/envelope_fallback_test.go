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
	"bytes"
	"encoding/json"
	"testing"
)

// TestUsagePricingFallbackOmitEmpty locks the wire contract for
// Usage.PricingFallbackModel (Phase 38 COST-02 / D-02): the common priced
// case must marshal WITHOUT a pricingFallbackModel key so pre-Phase-38
// envelopes and readers are byte-compatible.
func TestUsagePricingFallbackOmitEmpty(t *testing.T) {
	u := Usage{
		InputTokens:        100,
		OutputTokens:       50,
		EstimatedCostCents: 3,
	}
	data, err := json.Marshal(u)
	if err != nil {
		t.Fatalf("json.Marshal(Usage): %v", err)
	}
	if bytes.Contains(data, []byte("pricingFallbackModel")) {
		t.Errorf("empty PricingFallbackModel must be omitted from JSON, got: %s", data)
	}
}

// TestUsagePricingFallbackRoundTrip asserts a set fallback model survives a
// marshal/unmarshal round trip under the pricingFallbackModel key.
func TestUsagePricingFallbackRoundTrip(t *testing.T) {
	u := Usage{
		InputTokens:          100,
		OutputTokens:         50,
		EstimatedCostCents:   3,
		PricingFallbackModel: "claude-mystery-9",
	}
	data, err := json.Marshal(u)
	if err != nil {
		t.Fatalf("json.Marshal(Usage): %v", err)
	}
	if !bytes.Contains(data, []byte(`"pricingFallbackModel":"claude-mystery-9"`)) {
		t.Errorf("set PricingFallbackModel must appear under the pricingFallbackModel key, got: %s", data)
	}
	var got Usage
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("json.Unmarshal(Usage): %v", err)
	}
	if got.PricingFallbackModel != u.PricingFallbackModel {
		t.Errorf("PricingFallbackModel: got %q, want %q", got.PricingFallbackModel, u.PricingFallbackModel)
	}
}
