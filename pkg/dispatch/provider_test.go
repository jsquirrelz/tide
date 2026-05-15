package dispatch

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestProviderSpec_RoundTrip asserts that a fully-populated ProviderSpec
// (vendor + model + params) round-trips through json.Marshal+json.Unmarshal
// without data loss. JSON field tags MUST be `vendor`, `model`,
// `params,omitempty` (D-C3).
func TestProviderSpec_RoundTrip(t *testing.T) {
	in := ProviderSpec{
		Vendor: "anthropic",
		Model:  "claude-sonnet-4-6",
		Params: map[string]string{"thinking-budget": "4096"},
	}
	data, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("json.Marshal(ProviderSpec): %v", err)
	}
	if !strings.Contains(string(data), `"vendor":"anthropic"`) {
		t.Errorf(`serialized JSON missing "vendor":"anthropic"; got: %s`, string(data))
	}
	if !strings.Contains(string(data), `"model":"claude-sonnet-4-6"`) {
		t.Errorf(`serialized JSON missing "model":"claude-sonnet-4-6"; got: %s`, string(data))
	}
	if !strings.Contains(string(data), `"params"`) {
		t.Errorf(`serialized JSON missing "params" key; got: %s`, string(data))
	}

	var got ProviderSpec
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("json.Unmarshal(ProviderSpec): %v", err)
	}
	if got.Vendor != in.Vendor {
		t.Errorf("Vendor: got %q, want %q", got.Vendor, in.Vendor)
	}
	if got.Model != in.Model {
		t.Errorf("Model: got %q, want %q", got.Model, in.Model)
	}
	if len(got.Params) != 1 || got.Params["thinking-budget"] != "4096" {
		t.Errorf("Params: got %v, want %v", got.Params, in.Params)
	}
}

// TestProviderSpec_OmitsParamsWhenEmpty asserts that the serialized JSON does
// NOT contain "params" when Params is nil (omitempty contract — keeps tiny
// envelopes for executor-level dispatches that pass no per-vendor tuning).
func TestProviderSpec_OmitsParamsWhenEmpty(t *testing.T) {
	in := ProviderSpec{
		Vendor: "anthropic",
		Model:  "claude-haiku-4-5",
	}
	data, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	if strings.Contains(string(data), `"params"`) {
		t.Errorf(`serialized JSON contains "params" key but Params was nil; got: %s`, string(data))
	}
}

// TestProviderSpec_RoundTrip_EmptyVendor asserts that the Vendor field is
// required (no omitempty); an empty-vendor ProviderSpec still serializes the
// "vendor" key with an empty string. The provider-side fail-fast Vendor check
// (D-C3, internal/subagent/anthropic) is the gate, not the JSON tag.
func TestProviderSpec_RoundTrip_EmptyVendor(t *testing.T) {
	in := ProviderSpec{
		Vendor: "",
		Model:  "claude-haiku-4-5",
	}
	data, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	if !strings.Contains(string(data), `"vendor":""`) {
		t.Errorf(`serialized JSON missing "vendor":"" (required field); got: %s`, string(data))
	}
}
